// Instance discovery and the HTTP client for the pob-core control API.
//
// Every running core writes logs/<instance>/control.json with its pid and
// control port. An instance counts as running only when GET /status on that
// port answers with the matching instance ID — stale control.json files
// (crashed instances, recycled ports) are thereby ignored.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Instance struct {
	ID        string
	Dir       string
	StartTime int64
	EndTime   int64
	Port      int
	Running   bool
}

// discoverInstances scans logs/ for instance directories, newest first.
func discoverInstances(root string) []*Instance {
	logsDir := filepath.Join(root, "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return nil
	}
	var instances []*Instance
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if inst := loadInstance(root, entry.Name()); inst != nil {
			instances = append(instances, inst)
		}
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].ID > instances[j].ID })
	return instances
}

// loadInstance reads one logs/<id> directory and probes its control port.
// Returns nil when the directory doesn't look like an instance.
func loadInstance(root, id string) *Instance {
	dir := filepath.Join(root, "logs", id)
	instanceJSON := readJSONFile(filepath.Join(dir, "instance.json"))
	controlJSON := readJSONFile(filepath.Join(dir, "control.json"))
	if instanceJSON == nil && controlJSON == nil {
		return nil
	}
	inst := &Instance{
		ID:        id,
		Dir:       dir,
		StartTime: intField(instanceJSON, "start_time"),
		EndTime:   intField(instanceJSON, "end_time"),
		Port:      int(intField(controlJSON, "port")),
	}
	inst.probe()
	return inst
}

// probe checks whether the advertised control port answers as this instance.
func (i *Instance) probe() {
	if i.Port == 0 {
		return
	}
	status, err := i.get("/status", 2*time.Second)
	if err != nil {
		return
	}
	if id, _ := status["instance"].(string); id != i.ID {
		return
	}
	i.Running = true
}

func (i *Instance) url(path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", i.Port, path)
}

func (i *Instance) get(path string, timeout time.Duration) (map[string]any, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(i.url(path))
	if err != nil {
		return nil, err
	}
	return decodeResponse(resp)
}

func (i *Instance) post(path string, body any, timeout time.Duration) (map[string]any, error) {
	payload := []byte("{}")
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(i.url(path), "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	return decodeResponse(resp)
}

func decodeResponse(resp *http.Response) (map[string]any, error) {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("bad response: %s", strings.TrimSpace(string(data)))
	}
	if resp.StatusCode >= 300 {
		if message, _ := out["error"].(string); message != "" {
			return nil, fmt.Errorf("%s", message)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return out, nil
}

// --- live commands -----------------------------------------------------

func showStatus(inst *Instance) {
	status, err := inst.get("/status", 3*time.Second)
	if err != nil {
		fail("cannot reach instance %s: %v", inst.ID, err)
	}
	executing, _ := status["executing"].(bool)
	recording, _ := status["recording"].(bool)
	session, _ := status["session"].(string)
	model, _ := status["model"].(string)

	fmt.Printf("Instance:   %s (pid %d)\n", inst.ID, int(intField(status, "pid")))
	fmt.Printf("Root:       %s\n", status["root"])
	if executing && session != "" {
		fmt.Printf("Executing:  yes (session %s)\n", session)
	} else {
		fmt.Printf("Executing:  %s\n", yesNo(executing))
	}
	fmt.Printf("Recording:  %s\n", yesNo(recording))
	fmt.Printf("Model:      %s\n", model)

	if mcp, ok := status["mcp"].(map[string]any); ok {
		if running, _ := mcp["running"].(bool); running {
			fmt.Printf("MCP:        running — %s\n", mcp["url"])
		} else {
			fmt.Printf("MCP:        stopped\n")
		}
	}
}

// cmdStart runs instruction.txt; a non-empty text replaces it first. After
// starting it polls briefly so it can print the new session's ID.
func cmdStart(inst *Instance, text string) {
	var body any
	if text != "" {
		body = map[string]any{"instruction": text}
	}
	if _, err := inst.post("/run/instruction", body, 5*time.Second); err != nil {
		fail("start failed: %v", err)
	}
	fmt.Printf("Instruction session started on instance %s.\n", inst.ID)
	if session := waitForSession(inst); session != "" {
		fmt.Printf("Session:  %s\n", session)
		fmt.Printf("Logs:     %s\n", filepath.Join(inst.Dir, session))
	}
}

func cmdMacro(inst *Instance) {
	if _, err := inst.post("/run/macro", nil, 5*time.Second); err != nil {
		fail("macro failed: %v", err)
	}
	fmt.Printf("Macro session started on instance %s.\n", inst.ID)
	if session := waitForSession(inst); session != "" {
		fmt.Printf("Session:  %s\n", session)
		fmt.Printf("Logs:     %s\n", filepath.Join(inst.Dir, session))
	}
}

// waitForSession polls /status for a moment to catch the session ID the
// runner allocates after its first screenshot; "" when it isn't up yet.
func waitForSession(inst *Instance) string {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status, err := inst.get("/status", time.Second)
		if err != nil {
			return ""
		}
		executing, _ := status["executing"].(bool)
		if !executing {
			return "" // finished (or failed) before we caught it
		}
		if session, _ := status["session"].(string); session != "" {
			return session
		}
		time.Sleep(200 * time.Millisecond)
	}
	return ""
}

func cmdStop(inst *Instance) {
	if _, err := inst.post("/run/stop", nil, 5*time.Second); err != nil {
		fail("stop failed: %v", err)
	}
	fmt.Printf("Stop signal sent to instance %s.\n", inst.ID)
}

func cmdScreenshot(inst *Instance) {
	// Generous timeout: the shell may need a moment to capture.
	result, err := inst.post("/screenshot", nil, 60*time.Second)
	if err != nil {
		fail("screenshot failed: %v", err)
	}
	fmt.Println(result["path"])
}

func cmdMCP(inst *Instance, sub string) {
	switch sub {
	case "", "status", "info":
		info, err := inst.get("/mcp", 3*time.Second)
		if err != nil {
			fail("cannot reach instance %s: %v", inst.ID, err)
		}
		printMCPInfo(info)

	case "start":
		info, err := inst.post("/mcp/start", nil, 5*time.Second)
		if err != nil {
			fail("mcp start failed: %v", err)
		}
		printMCPInfo(info)

	case "stop":
		if _, err := inst.post("/mcp/stop", nil, 5*time.Second); err != nil {
			fail("mcp stop failed: %v", err)
		}
		fmt.Println("MCP server stopped.")

	default:
		fail("unknown mcp subcommand %q — use start, stop or status", sub)
	}
}

func printMCPInfo(info map[string]any) {
	running, _ := info["running"].(bool)
	url, _ := info["url"].(string)
	fmt.Printf("MCP server: %s\n", map[bool]string{true: "running", false: "stopped"}[running])
	fmt.Printf("URL:        %s\n", url)
	if tools, ok := info["tools"].([]any); ok && len(tools) > 0 {
		names := make([]string, len(tools))
		for i, t := range tools {
			names[i], _ = t.(string)
		}
		fmt.Printf("Tools:      %s\n", strings.Join(names, ", "))
	}
	if !running {
		fmt.Println("\nStart it with: pob mcp start")
		return
	}
	fmt.Println("\nMCP client config (e.g. Claude Desktop's claude_desktop_config.json):")
	fmt.Printf(`
{
  "mcpServers": {
    "pob": { "url": "%s" }
  }
}
`, url)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
