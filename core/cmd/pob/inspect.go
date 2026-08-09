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
	ID          string
	Dir         string
	Type        string // instruction | macro
	Start, End  int64
	TotalTokens int64
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
			Type:  "instruction",
			Start: intField(sessionJSON, "start_time"),
			End:   intField(sessionJSON, "end_time"),
		}
		if _, err := os.Stat(filepath.Join(dir, "macro.txt")); err == nil {
			info.Type = "macro"
		}
		if usage, ok := sessionJSON["usage"].(map[string]any); ok {
			info.TotalTokens = intField(usage, "total_tokens")
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

	settings := readJSONFile(filepath.Join(root, "settings.json"))
	if model, ok := settings["model"].(string); ok && !inst.Running {
		fmt.Printf("Model:      %s\n", model)
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
	fmt.Printf("%s%-13s %-12s %-20s %-10s %s\n", prefix, "SESSION", "TYPE", "STARTED", "DURATION", "TOKENS")
	for _, s := range sessions {
		tokens := "—"
		if s.TotalTokens > 0 {
			tokens = comma(s.TotalTokens)
		}
		fmt.Printf("%s%-13s %-12s %-20s %-10s %s\n",
			prefix, s.ID, s.Type, formatTime(s.Start), formatDuration(s.Start, s.End), tokens)
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
	sessionType := "instruction"
	if _, err := os.Stat(filepath.Join(dir, "macro.txt")); err == nil {
		sessionType = "macro"
	}

	fmt.Printf("Session:   %s (instance %s)\n", sessionID, instanceID)
	fmt.Printf("Type:      %s\n", sessionType)
	fmt.Printf("Started:   %s\n", formatTime(start))
	if end != 0 {
		fmt.Printf("Ended:     %s (%s)\n", formatTime(end), formatDuration(start, end))
	} else {
		fmt.Printf("Ended:     — (still running or interrupted)\n")
	}
	if usage, ok := sessionJSON["usage"].(map[string]any); ok {
		printUsage(usage)
	}

	if text, err := os.ReadFile(filepath.Join(dir, sessionType+".txt")); err == nil {
		title := strings.ToUpper(sessionType[:1]) + sessionType[1:]
		fmt.Printf("\n%s:\n%s\n", title, indent(string(text), "  "))
	}

	if shots, err := os.ReadDir(filepath.Join(dir, "screenshots")); err == nil && len(shots) > 0 {
		fmt.Printf("\nScreenshots: %d in %s\n", len(shots), filepath.Join(dir, "screenshots"))
	}

	printConditions(dir)
	printPlans(dir)
}

// printConditions renders the if conditions a macro session judged, in the
// order it judged them.
func printConditions(sessionDir string) {
	entries, err := os.ReadDir(filepath.Join(sessionDir, "conditions"))
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

	fmt.Println("\nConditions:")
	for _, seq := range seqs {
		conditionJSON := readJSONFile(filepath.Join(sessionDir, "conditions", strconv.Itoa(seq), "condition.json"))
		if conditionJSON == nil {
			continue
		}
		condition, _ := conditionJSON["condition"].(string)
		result, _ := conditionJSON["result"].(bool)
		reason, _ := conditionJSON["reason"].(string)
		verdict := "false — block skipped"
		if result {
			verdict = "true — block executed"
		}
		if line := intField(conditionJSON, "line"); line > 0 {
			fmt.Printf("  %d. [line %d] if (::%s::) → %s\n", seq, line, condition, verdict)
		} else {
			fmt.Printf("  %d. if (::%s::) → %s\n", seq, condition, verdict)
		}
		if reason != "" {
			fmt.Printf("     %s\n", reason)
		}
	}
}

func printUsage(usage map[string]any) {
	prompt := intField(usage, "prompt_tokens")
	completion := intField(usage, "completion_tokens")
	total := intField(usage, "total_tokens")
	var cached, reasoning int64
	if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		cached = intField(details, "cached_tokens")
	}
	if details, ok := usage["completion_tokens_details"].(map[string]any); ok {
		reasoning = intField(details, "reasoning_tokens")
	}
	fmt.Printf("Usage:     %s prompt (%s cached) + %s completion (%s reasoning) = %s tokens\n",
		comma(prompt), comma(cached), comma(completion), comma(reasoning), comma(total))
}

// printPlans renders the plan/step tree of an instruction session.
func printPlans(sessionDir string) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return
	}
	var planIDs []string
	for _, entry := range entries {
		// Everything else a session directory holds is a plan; "screenshots"
		// and "conditions" are the two directories that are not one.
		if entry.IsDir() && entry.Name() != "screenshots" && entry.Name() != "conditions" {
			planIDs = append(planIDs, entry.Name())
		}
	}
	if len(planIDs) == 0 {
		return
	}
	sort.Strings(planIDs)

	fmt.Println("\nPlans:")
	for _, planID := range planIDs {
		planDir := filepath.Join(sessionDir, planID)
		fmt.Printf("  %s\n", planID)
		for _, step := range readSteps(planDir) {
			status := ""
			if step.status != "" {
				status = " [" + step.status + "]"
			}
			fmt.Printf("    %d. %s%s\n", step.sequence, step.instruction, status)
			if step.expectation != "" {
				fmt.Printf("       expect: %s\n", step.expectation)
			}
		}
	}
}

type stepInfo struct {
	sequence    int
	instruction string
	expectation string
	status      string
}

func readSteps(planDir string) []stepInfo {
	entries, err := os.ReadDir(planDir)
	if err != nil {
		return nil
	}
	var steps []stepInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		seq, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stepDir := filepath.Join(planDir, entry.Name())
		stepJSON := readJSONFile(filepath.Join(stepDir, "step.json"))
		step := stepInfo{sequence: seq}
		if stepJSON != nil {
			step.instruction, _ = stepJSON["instruction"].(string)
			step.expectation, _ = stepJSON["expectation"].(string)
		}
		if data, err := os.ReadFile(filepath.Join(stepDir, "status.txt")); err == nil {
			step.status = strings.TrimSpace(string(data))
		}
		steps = append(steps, step)
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].sequence < steps[j].sequence })
	return steps
}
