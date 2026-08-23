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
	// What is true of this machine, above the lists of what is on it: the
	// version, where it all lives, and whether psl is there — three things that
	// belong to no single instance and that nothing in the tables would say.
	//
	// psl earns its line by being the one piece a run needs that goes missing
	// quietly: a macro with a `:: … ::` in it stops at the slot, and until then
	// everything looks installed. `pob check` is the long version.
	fmt.Printf("Pob %s\n", version)
	fmt.Printf("Root:       %s\n", root)
	fmt.Printf("psl:        %s\n", pslLine(root))

	// Every instance on this machine, and every machine of its own that is up.
	//
	// The current instance's own details used to be here — its id, when it last
	// started and ended, its log directory — and they answered a question this
	// view is not asked: a bare `pob` is read to find out what there *is*, and
	// what one instance is *doing* is `pob status`, which is live and one word
	// away. The instance a launch would start is still marked in the table.
	//
	// The sessions used to be here too and are `pob sessions` now: an instance
	// that has been run for a week answers with hundreds of them.
	printInstanceTable(root, theInstance(root))
	printVMTable(root)
}

// pslLine is the compiler and where it was found, or the name and that it was
// not — psl.Compiler's own words, the same ones `pob status` and `pob check`
// print, read straight from settings.json rather than through config, which
// would create the files it reads.
func pslLine(root string) string {
	settings := readJSONFile(filepath.Join(root, "settings.json"))
	return psl.Compiler{Binary: pslBinary(settings), Dir: root}.Describe()
}

// printInstanceTable lists the instances under ~/.pob, marking the one INSTANCE
// names — the one a bare `pob launch` starts. That one arrives already probed,
// since the block above this asked it for everything it has just printed.
func printInstanceTable(root string, current *Instance) {
	instances := storage.ListInstances(root)
	if len(instances) == 0 {
		return
	}
	fmt.Println("\nInstances:")
	fmt.Printf("%-2s %-10s %-20s %-9s %s\n", "", "ID", "NAME", "STATE", "LAST RUN")
	for _, info := range instances {
		mark := " "
		running := false
		if current != nil && info.ID == current.ID {
			mark = "*"
			running = current.Running
		} else {
			// Asked of the instance rather than read off its file:
			// instance.json records the run that started, and a Pob that was
			// killed rather than quit never came back to write that it had
			// ended. The probe is a connection to a port on this machine —
			// refused at once when there is nothing behind it, which is the
			// answer for every stopped instance.
			if inst := loadInstance(root, info.ID); inst != nil && inst.Running {
				running = true
			}
		}
		name := info.Name
		if name == "" {
			name = "—"
		}
		state := "stopped"
		if running {
			state = "running"
		}
		fmt.Printf("%-2s %-10s %-20s %-9s %s\n", mark, info.ID, name, state, formatTime(info.StartTime))
	}
}

// printVMTable lists the microVMs, which are instances too — a copy of one of
// the above, running on a Linux machine of its own. The screen is the address a
// VNC viewer opens: it is the only way to look at one of these, so it belongs
// in the listing rather than only in the launch that printed it once.
func printVMTable(root string) {
	vms := msbVMs(root)
	if len(vms) == 0 {
		return
	}
	// The sandbox column is as wide as the widest name: POB_MSB_NAME takes
	// whatever it is given, and a name longer than the column pushes every
	// column after it along on that row alone — a listing that reads as
	// misaligned rather than as one long name.
	// The floor is the width the drawn names sit in — msb-<4 hex> is shorter
	// than the heading, and a column that shrank to it would put the next one a
	// space away from it.
	width := 14
	for _, vm := range vms {
		if len(vm.Name) > width {
			width = len(vm.Name)
		}
	}
	row := fmt.Sprintf("%%-2s %%-%ds %%-10s %%-9s %%s\n", width)

	fmt.Println("\nVMs:")
	fmt.Printf(row, "", "SANDBOX", "INSTANCE", "STATE", "SCREEN")
	for _, vm := range vms {
		state := "stopped"
		if vm.Running {
			state = "running"
		}
		fmt.Printf(row, "", vm.Name, vm.InstanceLabel(), state, vm.Screen())
	}
}

// listSessionsCmd lists every instance's sessions, under a heading for each.
// One instance's are rarely the question on a machine with several — which run
// left the screenshots, which one was yesterday — and the id in the heading is
// what `pob --session` is asked with.
func listSessionsCmd(root string) {
	instances := storage.ListInstances(root)
	if len(instances) == 0 {
		fmt.Println("No instances.")
		return
	}
	found := 0
	for _, info := range instances {
		sessions := listSessions(filepath.Join(root, info.ID, "logs"))
		if len(sessions) == 0 {
			continue
		}
		if found > 0 {
			fmt.Println()
		}
		found++
		if info.Name != "" {
			fmt.Printf("Instance %s (%s)\n", info.ID, info.Name)
		} else {
			fmt.Printf("Instance %s\n", info.ID)
		}
		printSessionTable(sessions, "  ")
	}
	if found == 0 {
		fmt.Println("No sessions.")
	}
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
	// A session id from another instance is still an id off the listing `pob
	// sessions` prints, which is every instance's now — so it is looked for
	// where it was listed rather than answered as missing.
	if sessionJSON == nil {
		for _, info := range storage.ListInstances(root) {
			if info.ID == instanceID {
				continue
			}
			other := filepath.Join(root, info.ID, "logs", sessionID)
			if found := readJSONFile(filepath.Join(other, "session.json")); found != nil {
				dir, instanceID, sessionJSON = other, info.ID, found
				break
			}
		}
	}
	if sessionJSON == nil {
		fail("no session %s under any instance — `pob sessions` lists them", sessionID)
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
