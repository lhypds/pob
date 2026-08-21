// Offline views over the <instance>/logs/ tree (see README "Logs"): instance and
// session listings and the per-session detail view. These read the disk
// directly so they work whether or not the instance is running.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"pob/core/internal/config"
	"pob/core/internal/psl"
	"pob/core/internal/storage"
)

// --- shared helpers -----------------------------------------------------

// readJSONFile returns nil when the file is missing or unparsable.
func readJSONFile(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func intField(m map[string]any, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}

func formatTime(unix int64) string {
	if unix == 0 {
		return "—"
	}
	return time.Unix(unix, 0).Format("2006-01-02 15:04:05")
}

func formatDuration(start, end int64) string {
	if start == 0 || end == 0 || end < start {
		return "—"
	}
	d := time.Duration(end-start) * time.Second
	if d >= time.Hour {
		return fmt.Sprintf("%dh %dm %ds", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// comma formats 1234567 as "1,234,567".
func comma(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func indent(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// --- sessions on disk ---------------------------------------------------

type sessionInfo struct {
	ID         string
	Dir        string
	Start, End int64
	// Slots is how many :: … :: psl filled during the session — the only part
	// of a replay that is not free, now that the model calls are psl's.
	Slots int
}

func listSessions(instanceDir string) []sessionInfo {
	entries, err := os.ReadDir(instanceDir)
	if err != nil {
		return nil
	}
	var sessions []sessionInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(instanceDir, entry.Name())
		sessionJSON := readJSONFile(filepath.Join(dir, "session.json"))
		if sessionJSON == nil {
			continue
		}
		info := sessionInfo{
			ID:    entry.Name(),
			Dir:   dir,
			Start: intField(sessionJSON, "start_time"),
			End:   intField(sessionJSON, "end_time"),
		}
		// One directory per fill, and only the directories: browsing a session in
		// Finder leaves a .DS_Store in slots/, and counting the entries rather
		// than the directories reported one fill more than the session made.
		if slots, err := os.ReadDir(filepath.Join(dir, "slots")); err == nil {
			for _, slot := range slots {
				if slot.IsDir() {
					info.Slots++
				}
			}
		}
		sessions = append(sessions, info)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID > sessions[j].ID })
	return sessions
}

// --- views ----------------------------------------------------------------

func showInstance(root string) {
	inst := theInstance(root)

	if inst.Running {
		showStatus(inst)
	} else {
		fmt.Printf("Instance:   %s (stopped)\n", inst.ID)
		if inst.Name != "" {
			fmt.Printf("Name:       %s\n", inst.Name)
		}
		fmt.Printf("Started:    %s\n", formatTime(inst.StartTime))
		fmt.Printf("Ended:      %s\n", formatTime(inst.EndTime))
	}

	fmt.Printf("Logs:       %s\n", inst.LogsDir)

	sessions := listSessions(inst.LogsDir)
	if len(sessions) == 0 {
		fmt.Println("\nNo sessions.")
		return
	}
	fmt.Println("\nSessions:")
	printSessionTable(sessions, "  ")
}

func listSessionsCmd(root, instanceID string) {
	sessions := listSessions(filepath.Join(root, instanceID, "logs"))
	if len(sessions) == 0 {
		fmt.Printf("No sessions for instance %s.\n", instanceID)
		return
	}
	printSessionTable(sessions, "")
}

func printSessionTable(sessions []sessionInfo, prefix string) {
	fmt.Printf("%s%-13s %-20s %-10s %s\n", prefix, "SESSION", "STARTED", "DURATION", "SLOTS")
	for _, s := range sessions {
		slots := "—"
		if s.Slots > 0 {
			slots = strconv.Itoa(s.Slots)
		}
		fmt.Printf("%s%-13s %-20s %-10s %s\n",
			prefix, s.ID, formatTime(s.Start), formatDuration(s.Start, s.End), slots)
	}
}

func showSession(root, instanceID, sessionID string) {
	dir := filepath.Join(root, instanceID, "logs", sessionID)
	sessionJSON := readJSONFile(filepath.Join(dir, "session.json"))
	if sessionJSON == nil {
		fail("session %s not found under instance %s", sessionID, instanceID)
	}

	start := intField(sessionJSON, "start_time")
	end := intField(sessionJSON, "end_time")

	fmt.Printf("Session:   %s (instance %s)\n", sessionID, instanceID)
	fmt.Printf("Started:   %s\n", formatTime(start))
	if end != 0 {
		fmt.Printf("Ended:     %s (%s)\n", formatTime(end), formatDuration(start, end))
	} else {
		fmt.Printf("Ended:     — (still running or interrupted)\n")
	}
	// The macro as it stood when this session ran, which is not necessarily
	// the one in the instance directory now.
	if name, text, ok := sessionMacro(dir); ok {
		fmt.Printf("\nMacro:     %s\n%s\n", name, indent(text, "  "))
	}

	if shots, err := os.ReadDir(filepath.Join(dir, "screenshots")); err == nil && len(shots) > 0 {
		fmt.Printf("\nScreenshots: %d in %s\n", len(shots), filepath.Join(dir, "screenshots"))
	}

	printSlots(dir)
}

// sessionMacro reads the copy of the macro a session kept, and the name it kept
// it under — which is the entry point's own, so a session that ran another file
// says which one.
//
// The two names Pob has used for the instance's own macro are tried first, in
// that order, since that is nearly every session and the older tree is one
// people still open. Failing those it is whatever PSL file is in the directory:
// a session started with `--macropsl` keeps it under that file's name, and the
// name is not known from here.
func sessionMacro(dir string) (name, text string, ok bool) {
	names := []string{storage.SessionMacroName, storage.LegacySessionMacroName}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && isMacroName(entry.Name()) {
				names = append(names, entry.Name())
			}
		}
	}
	for _, candidate := range names {
		if data, err := os.ReadFile(filepath.Join(dir, candidate)); err == nil {
			return candidate, string(data), true
		}
	}
	return "", "", false
}

// isMacroName reports whether a file in a session directory is the macro it
// replayed. A macro is written under `.psl` — `.macro.psl` among them — or under
// the slotless `.macro` (see psl.MacroExt), and nothing else in a session
// directory ends in either, so the name is enough to tell.
func isMacroName(name string) bool {
	return filepath.Ext(name) == ".psl" || strings.HasSuffix(name, psl.MacroExt)
}

// printSlots renders the ::…:: slots a macro session filled, in the order it
// filled them — the statement as written, and what the AI put in it.
func printSlots(sessionDir string) {
	entries, err := os.ReadDir(filepath.Join(sessionDir, "slots"))
	if err != nil || len(entries) == 0 {
		return
	}
	var seqs []int
	for _, entry := range entries {
		if seq, err := strconv.Atoi(entry.Name()); entry.IsDir() && err == nil {
			seqs = append(seqs, seq)
		}
	}
	sort.Ints(seqs)

	fmt.Println("\nSlots:")
	for _, seq := range seqs {
		slotJSON := readJSONFile(filepath.Join(sessionDir, "slots", strconv.Itoa(seq), "slot.json"))
		if slotJSON == nil {
			continue
		}
		prompt, _ := slotJSON["prompt"].(string)
		statement, _ := slotJSON["statement"].(string)
		value, _ := slotJSON["value"].(string)
		model, _ := slotJSON["model"].(string)
		file, _ := slotJSON["file"].(string)
		wasFilled, _ := slotJSON["filled"].(bool)

		filled := value
		if !wasFilled {
			filled = "— unfilled, statement skipped"
		}
		// A slot written on a line of its own fills to statements rather than to a
		// value, and a block of them printed on the arrow line would run off the end
		// of it. How much came back goes there instead, and the statements go under
		// it — so the list reads one slot at a time however much one of them filled
		// to.
		var block []string
		if wasFilled && strings.Contains(value, "\n") {
			block = strings.Split(value, "\n")
			filled = fmt.Sprintf("a block of %d lines", len(block))
		}
		if line := intField(slotJSON, "line"); line > 0 {
			// The file is named only when it is not the macro itself: the numbers
			// start again in each file a call() brought in, and a bare line number
			// would be a line of whichever one the reader assumed.
			where := fmt.Sprintf("line %d", line)
			if file != "" && file != config.MainMacroName {
				where = fmt.Sprintf("%s line %d", file, line)
			}
			fmt.Printf("  %d. [%s] :: %s :: → %s\n", seq, where, prompt, filled)
		} else {
			fmt.Printf("  %d. :: %s :: → %s\n", seq, prompt, filled)
		}
		if statement != "" {
			fmt.Printf("     in: %s\n", statement)
		}
		for _, line := range block {
			fmt.Printf("         %s\n", line)
		}
		if model != "" {
			fmt.Printf("     filled by %s\n", model)
		}
	}
}
