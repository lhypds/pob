// Package storage writes the instance/session/plan/step/verification log
// tree under <root>/logs/ (see README "Logs" section). One instance runs on a
// machine and it keeps one id for good, so every session it ever writes lands
// in the same logs/<instance>/ directory — see ResolveInstanceID.
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
	logsDir      string
	instanceID   string
	settingsDict func() map[string]any
	instruction  func() string
	macro        func() string
}

// InstancePrefix is what every instance id starts with. The shells match on
// it to tell an instance directory from anything else under logs/.
const InstancePrefix = "pb-"

// instancePointer holds the machine's one instance id. It sits beside logs/
// rather than inside it, so clearing the logs doesn't change which instance
// this machine is — the id is in the toolbar and in the web UI address, and
// both should survive a tidy-up.
const instancePointer = "instance"

// New creates logs/<instance>/ for this process; every session it writes
// lives under that directory. instanceID is the one the shell resolved and
// passed in; an empty one is resolved here, which is what the CLI does when
// it runs without a shell.
func New(logsDir, instanceID string, settingsDict func() map[string]any, instruction, macro func() string) *Storage {
	_ = os.MkdirAll(logsDir, 0o755)
	if instanceID == "" {
		instanceID = ResolveInstanceID(filepath.Dir(logsDir))
	}
	_ = os.MkdirAll(filepath.Join(logsDir, instanceID), 0o755)
	return &Storage{
		logsDir:      logsDir,
		instanceID:   instanceID,
		settingsDict: settingsDict,
		instruction:  instruction,
		macro:        macro,
	}
}

// ResolveInstanceID returns the machine's instance id, the same one on every
// run. It is recorded in <root>/instance the first time it is worked out.
//
// A machine upgrading from the versions that started a fresh instance per
// launch has a logs/ full of pb-* directories. Rather than add one more, the
// most recently used is adopted as the one instance from here on; the others
// stay where they are as history.
//
// The shells resolve the id the same way before spawning pob-core, since they
// need it for the toolbar and their own settings file. Whoever gets there
// first writes the pointer and the other reads it.
func ResolveInstanceID(root string) string {
	logsDir := filepath.Join(root, "logs")
	pointer := filepath.Join(root, instancePointer)

	if id := readInstancePointer(pointer); id != "" {
		return id
	}

	id := mostRecentInstance(logsDir)
	if id == "" {
		id = newInstanceID(logsDir)
	}
	_ = os.MkdirAll(root, 0o755)
	_ = os.WriteFile(pointer, []byte(id+"\n"), 0o644)
	return id
}

// readInstancePointer reads <root>/instance, ignoring anything that isn't an
// instance id — a truncated or hand-edited file should send the machine back
// to working the id out rather than into a directory named after junk.
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

// mostRecentInstance is the pb-* directory under logsDir modified last, or ""
// when there are none. Modification time rather than the id or instance.json:
// the directory is touched every time a session is written into it, so the
// newest is the one that was actually being used.
func mostRecentInstance(logsDir string) string {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return ""
	}
	var newest string
	var newestAt time.Time
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), InstancePrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestAt) {
			newest, newestAt = entry.Name(), info.ModTime()
		}
	}
	return newest
}

// newInstanceID reserves a pb-<uid> directory under logsDir. If the ID is
// already taken, another one is drawn until a free one is found.
func newInstanceID(logsDir string) string {
	for {
		id := instanceID()
		err := os.Mkdir(filepath.Join(logsDir, id), 0o755)
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

// InstanceDir returns logs/<instance>, this process's log directory.
func (s *Storage) InstanceDir() string { return filepath.Join(s.logsDir, s.instanceID) }

func (s *Storage) instanceFile() string {
	return filepath.Join(s.logsDir, s.instanceID, "instance.json")
}

// WriteInstanceStart creates logs/<instance>/instance.json recording when
// this instance started.
func (s *Storage) WriteInstanceStart() {
	writeJSON(s.instanceFile(), map[string]any{
		"start_time": time.Now().Unix(),
	})
}

// WriteInstanceEnd records the end time into instance.json, preserving the
// recorded start time.
func (s *Storage) WriteInstanceEnd() {
	entry := readJSONFile(s.instanceFile())
	entry["end_time"] = time.Now().Unix()
	writeJSON(s.instanceFile(), entry)
}

func (s *Storage) sessionDir(sessionID string) string {
	return filepath.Join(s.logsDir, s.instanceID, sessionID)
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

// CreateSession creates logs/<instance>/<unixtime>/ with an initial session.json.
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

// CreatePlan creates logs/<instance>/<session>/<unixtime>/ and returns the plan ID.
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

func (s *Storage) WriteStepStatus(status, sessionID, planID string, stepSeq int) {
	stepDir := filepath.Join(s.sessionDir(sessionID), planID, fmt.Sprintf("%d", stepSeq))
	_ = os.MkdirAll(stepDir, 0o755)
	_ = os.WriteFile(filepath.Join(stepDir, "status.txt"), []byte(status), 0o644)
}

// SaveStepLog writes one conversation round under logs/<instance>/<session>/<plan>/<step>/<unixtime>/.
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
// logs/<instance>/screenshots/<unixtime>.png and returns the file path.
func (s *Storage) SaveUserScreenshot(png []byte) string {
	dir := filepath.Join(s.logsDir, s.instanceID, "screenshots")
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
