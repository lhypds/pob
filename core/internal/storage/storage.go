// Package storage writes the instance/session/plan/step/verification log
// tree under <root>/<instance>/logs/ (see README "Logs" section). One instance
// runs on a machine and it keeps one id for good, so every session it ever
// writes lands in the same directory — see ResolveInstanceID.
package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Storage struct {
	root         string
	instanceID   string
	settingsDict func() map[string]any
	instruction  func() string
	macro        func() string
}

// InstancePrefix is what every instance id starts with. The shells match on
// it to tell an instance directory from anything else under the root.
const InstancePrefix = "pb-"

// instancePointer names the instance directory the machine is working in. It
// sits at the root rather than inside that directory, since it is what points
// at it: aim it somewhere else and Pob starts from clean files, which is what
// changing it is for. Named in capitals like VERSION and SYSTEM: a marker the
// programs write and read, not a file to edit.
const instancePointer = "INSTANCE"

// New creates <root>/<instance>/logs/ for this process; every session it
// writes lives under that directory. instanceID is the one the shell resolved
// and passed in; an empty one is resolved here, which is what the CLI does
// when it runs without a shell.
func New(root, instanceID string, settingsDict func() map[string]any, instruction, macro func() string) *Storage {
	if instanceID == "" {
		instanceID = ResolveInstanceID(root)
	}
	s := &Storage{
		root:         root,
		instanceID:   instanceID,
		settingsDict: settingsDict,
		instruction:  instruction,
		macro:        macro,
	}
	_ = os.MkdirAll(s.LogsDir(), 0o755)
	return s
}

// ResolveInstanceID returns the machine's instance id, the same one on every
// run. It is recorded in <root>/INSTANCE the first time it is worked out.
//
// The pointer is the only thing that says which instance a machine is: with
// no readable one a fresh id is drawn and a new directory reserved, rather
// than an existing pb-* directory adopted. Deleting the file is therefore a
// way to start clean, and the directories already there stay as history.
//
// The shells resolve the id the same way before spawning pob-core, since they
// need it for the toolbar and their own settings file. Whoever gets there
// first writes the pointer and the other reads it.
func ResolveInstanceID(root string) string {
	pointer := filepath.Join(root, instancePointer)

	if id := readInstancePointer(pointer); id != "" {
		return id
	}

	id := newInstanceID(root)
	_ = os.WriteFile(pointer, []byte(id+"\n"), 0o644)
	return id
}

// readInstancePointer reads <root>/INSTANCE, ignoring anything that isn't an
// instance id — a truncated or hand-edited file should start a new instance
// rather than send the machine into a directory named after junk.
func readInstancePointer(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(data))
	if !strings.HasPrefix(id, InstancePrefix) || strings.ContainsAny(id, `/\`) {
		return ""
	}
	return id
}

// newInstanceID reserves a pb-<uid> directory under root. If the ID is
// already taken, another one is drawn until a free one is found.
func newInstanceID(root string) string {
	_ = os.MkdirAll(root, 0o755)
	for {
		id := instanceID()
		err := os.Mkdir(filepath.Join(root, id), 0o755)
		if err == nil || !os.IsExist(err) {
			return id
		}
	}
}

// instanceID builds a "pb-<4 hex>" id: the last two bytes of a fresh UID as
// lowercase hex, the same scheme the pico-hid firmware uses for its "ph-"
// board id. The shells show it in the toolbar beside the window buttons.
func instanceID() string {
	var uid [2]byte
	if _, err := rand.Read(uid[:]); err != nil {
		// crypto/rand does not fail in practice; fall back to the clock so an
		// instance still gets a directory rather than none.
		n := time.Now().UnixNano()
		uid[0] = byte(n >> 8)
		uid[1] = byte(n)
	}
	return fmt.Sprintf("%s%02x%02x", InstancePrefix, uid[0], uid[1])
}

func (s *Storage) InstanceID() string { return s.instanceID }

// InstanceDir returns <root>/<instance>, everything this instance owns.
func (s *Storage) InstanceDir() string { return filepath.Join(s.root, s.instanceID) }

// LogsDir returns <root>/<instance>/logs, this process's log directory.
func (s *Storage) LogsDir() string { return filepath.Join(s.InstanceDir(), "logs") }

// instanceFile is <root>/<instance>/instance.json — who this instance is (its
// id and the name it was given) and when it last ran. It sits at the top of the
// instance directory rather than inside logs/, because it says what the
// directory is rather than what happened in it.
func (s *Storage) instanceFile() string {
	return filepath.Join(s.InstanceDir(), "instance.json")
}

// WriteInstanceStart records when this instance started, keeping the id and
// name already written there — an instance made by `pob new` is named before
// it has ever run.
func (s *Storage) WriteInstanceStart() {
	entry := readJSONFile(s.instanceFile())
	entry["id"] = s.instanceID
	entry["start_time"] = time.Now().Unix()
	delete(entry, "end_time")
	writeJSON(s.instanceFile(), entry)
}

// WriteInstanceEnd records the end time into instance.json, preserving the
// recorded start time.
func (s *Storage) WriteInstanceEnd() {
	entry := readJSONFile(s.instanceFile())
	entry["end_time"] = time.Now().Unix()
	writeJSON(s.instanceFile(), entry)
}

// WriteControl records the process id and the control API port, which is how a
// running instance advertises itself to the `pob` CLI. It goes into
// instance.json rather than a file of its own: the port is one more thing about
// this instance, and one file is one thing for the CLI to read.
func (s *Storage) WriteControl(pid, port int) {
	entry := readJSONFile(s.instanceFile())
	entry["id"] = s.instanceID
	entry["pid"] = pid
	entry["port"] = port
	writeJSON(s.instanceFile(), entry)
}

// ClearControl drops the pid and port so a stopped instance no longer
// advertises itself, keeping the rest of what instance.json records.
func (s *Storage) ClearControl() {
	entry := readJSONFile(s.instanceFile())
	delete(entry, "pid")
	delete(entry, "port")
	writeJSON(s.instanceFile(), entry)
}

func (s *Storage) sessionDir(sessionID string) string {
	return filepath.Join(s.LogsDir(), sessionID)
}

// PrettyJSON marshals without HTML escaping, indented — the format used for
// every *.json log file.
func PrettyJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func writeJSON(path string, v any) {
	data, err := PrettyJSON(v)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func unixNow() string { return fmt.Sprintf("%d", time.Now().Unix()) }

// CreateSession creates logs/<unixtime>/ with an initial session.json.
func (s *Storage) CreateSession() string {
	sessionID := unixNow()
	dir := s.sessionDir(sessionID)
	_ = os.MkdirAll(dir, 0o755)
	writeJSON(filepath.Join(dir, "session.json"), map[string]any{
		"start_time": time.Now().Unix(),
		"settings":   s.settingsDict(),
	})
	return sessionID
}

// CreatePlan creates logs/<session>/<unixtime>/ and returns the plan ID.
func (s *Storage) CreatePlan(sessionID string) string {
	planID := unixNow()
	_ = os.MkdirAll(filepath.Join(s.sessionDir(sessionID), planID), 0o755)
	return planID
}

func (s *Storage) SaveInstruction(sessionID string) {
	_ = os.WriteFile(filepath.Join(s.sessionDir(sessionID), "instruction.txt"), []byte(s.instruction()), 0o644)
}

func (s *Storage) SaveMacro(sessionID string) {
	_ = os.WriteFile(filepath.Join(s.sessionDir(sessionID), "macro.txt"), []byte(s.macro()), 0o644)
}

// SavePlan writes plan.json, messages.json, response.json, screenshot.png and
// creates numbered step directories with step.json.
func (s *Storage) SavePlan(plan string, messages []map[string]any, response map[string]any, sessionID, planID string, screenshotPNG []byte) {
	planDir := filepath.Join(s.sessionDir(sessionID), planID)
	_ = os.MkdirAll(planDir, 0o755)
	_ = os.WriteFile(filepath.Join(planDir, "plan.json"), []byte(plan), 0o644)
	writeJSON(filepath.Join(planDir, "messages.json"), stripImages(messages))
	writeJSON(filepath.Join(planDir, "response.json"), response)
	if len(screenshotPNG) > 0 {
		_ = os.WriteFile(filepath.Join(planDir, "screenshot.png"), screenshotPNG, 0o644)
	}

	var parsed struct {
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal([]byte(plan), &parsed); err != nil {
		return
	}
	for _, step := range parsed.Steps {
		seq, ok := step["sequence"].(float64)
		if !ok {
			continue
		}
		stepDir := filepath.Join(planDir, fmt.Sprintf("%d", int(seq)))
		_ = os.MkdirAll(stepDir, 0o755)
		inst, _ := step["instruction"].(string)
		exp, _ := step["expectation"].(string)
		writeJSON(filepath.Join(stepDir, "step.json"), map[string]any{
			"sequence":    int(seq),
			"instruction": inst,
			"expectation": exp,
		})
	}
}

func (s *Storage) SaveVerification(sessionID, planID string, stepSeq int, messages []map[string]any, response map[string]any, screenshotPNG []byte) {
	dir := filepath.Join(s.sessionDir(sessionID), planID, fmt.Sprintf("%d", stepSeq), "verification")
	_ = os.MkdirAll(dir, 0o755)
	writeJSON(filepath.Join(dir, "messages.json"), stripImages(messages))
	writeJSON(filepath.Join(dir, "response.json"), response)
	if len(screenshotPNG) > 0 {
		_ = os.WriteFile(filepath.Join(dir, "screenshot.png"), screenshotPNG, 0o644)
	}
}

// SaveMacroCondition writes one IF judgement of a macro session under
// logs/<session>/conditions/<n>/. The directories are numbered in the order the
// conditions were judged, and condition.json carries the line of macro.txt each
// one came from.
func (s *Storage) SaveMacroCondition(sessionID string, seq, line int, condition string, result bool, reason string, messages []map[string]any, response map[string]any, screenshotPNG []byte) {
	dir := filepath.Join(s.sessionDir(sessionID), "conditions", fmt.Sprintf("%d", seq))
	_ = os.MkdirAll(dir, 0o755)
	writeJSON(filepath.Join(dir, "condition.json"), map[string]any{
		"sequence":  seq,
		"line":      line,
		"condition": condition,
		"result":    result,
		"reason":    reason,
	})
	writeJSON(filepath.Join(dir, "messages.json"), stripImages(messages))
	writeJSON(filepath.Join(dir, "response.json"), response)
	if len(screenshotPNG) > 0 {
		_ = os.WriteFile(filepath.Join(dir, "screenshot.png"), screenshotPNG, 0o644)
	}
}

func (s *Storage) WriteStepStatus(status, sessionID, planID string, stepSeq int) {
	stepDir := filepath.Join(s.sessionDir(sessionID), planID, fmt.Sprintf("%d", stepSeq))
	_ = os.MkdirAll(stepDir, 0o755)
	_ = os.WriteFile(filepath.Join(stepDir, "status.txt"), []byte(status), 0o644)
}

// SaveStepLog writes one conversation round under logs/<session>/<plan>/<step>/<unixtime>/.
func (s *Storage) SaveStepLog(sessionID, planID string, stepSeq int, messages []map[string]any, response map[string]any, screenshotPNG []byte) {
	dir := filepath.Join(s.sessionDir(sessionID), planID, fmt.Sprintf("%d", stepSeq), unixNow())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	if len(screenshotPNG) > 0 {
		_ = os.WriteFile(filepath.Join(dir, "screenshot.png"), screenshotPNG, 0o644)
	}
	writeJSON(filepath.Join(dir, "messages.json"), stripImages(messages))
	writeJSON(filepath.Join(dir, "response.json"), response)
}

func (s *Storage) SaveScreenshot(png []byte, sessionID string) {
	dir := filepath.Join(s.sessionDir(sessionID), "screenshots")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, unixNow()+".png"), png, 0o644)
}

// SaveUserScreenshot writes a toolbar-button capture (outside any session) to
// logs/screenshots/<unixtime>.png and returns the file path.
func (s *Storage) SaveUserScreenshot(png []byte) string {
	dir := filepath.Join(s.LogsDir(), "screenshots")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, unixNow()+".png")
	_ = os.WriteFile(path, png, 0o644)
	return path
}

func (s *Storage) SaveSessionStartEndTimes(sessionID string, start, end time.Time) {
	dest := filepath.Join(s.sessionDir(sessionID), "session.json")
	entry := readJSONFile(dest)
	entry["start_time"] = start.Unix()
	entry["end_time"] = end.Unix()
	writeJSON(dest, entry)
}

// SaveSessionUsage sums the usage blocks of every response.json under the
// session directory and writes the total into session.json.
func (s *Storage) SaveSessionUsage(sessionID string) {
	sessionDir := s.sessionDir(sessionID)

	var promptTokens, completionTokens, totalTokens, reasoningTokens, cachedTokens int
	_ = filepath.WalkDir(sessionDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "response.json" {
			return nil
		}
		usage, ok := readJSONFile(path)["usage"].(map[string]any)
		if !ok {
			return nil
		}
		promptTokens += intField(usage, "prompt_tokens")
		completionTokens += intField(usage, "completion_tokens")
		totalTokens += intField(usage, "total_tokens")
		if details, ok := usage["completion_tokens_details"].(map[string]any); ok {
			reasoningTokens += intField(details, "reasoning_tokens")
		}
		if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
			cachedTokens += intField(details, "cached_tokens")
		}
		return nil
	})

	dest := filepath.Join(sessionDir, "session.json")
	summary := readJSONFile(dest)
	summary["usage"] = map[string]any{
		"prompt_tokens":             promptTokens,
		"completion_tokens":         completionTokens,
		"total_tokens":              totalTokens,
		"completion_tokens_details": map[string]any{"reasoning_tokens": reasoningTokens},
		"prompt_tokens_details":     map[string]any{"cached_tokens": cachedTokens},
	}
	writeJSON(dest, summary)
}

func readJSONFile(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func intField(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

// stripImages replaces image_url payloads with a placeholder so messages.json
// stays readable.
func stripImages(messages []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		m := make(map[string]any, len(msg))
		for k, v := range msg {
			m[k] = v
		}
		if parts, ok := m["content"].([]any); ok {
			newParts := make([]any, 0, len(parts))
			for _, part := range parts {
				if p, ok := part.(map[string]any); ok && p["type"] == "image_url" {
					cp := make(map[string]any, len(p))
					for k, v := range p {
						cp[k] = v
					}
					cp["image_url"] = map[string]any{"url": "<image_stripped>"}
					newParts = append(newParts, cp)
				} else {
					newParts = append(newParts, part)
				}
			}
			m["content"] = newParts
		}
		out = append(out, m)
	}
	return out
}
