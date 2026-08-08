// The machine's instance and the HTTP client for the pob-core control API.
//
// A running core writes <instance>/logs/control.json with its pid and control
// port. It counts as running only when GET /status on that port answers with
// the matching instance ID — a stale control.json (a crashed instance, a
// recycled port) is thereby ignored.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"pob/core/internal/storage"
)

type Instance struct {
	ID string
	// Dir is <root>/<id>, holding settings.json, instruction.txt and macro.txt;
	// LogsDir is its logs/ subdirectory, holding the sessions and the two JSON
	// files a running instance advertises itself with.
	Dir       string
	LogsDir   string
	StartTime int64
	EndTime   int64
	Port      int
	Running   bool
}

// theInstance loads the machine's instance, running or not. It resolves the
// id the same way the app does, so the CLI and the app always mean the same
// instance — and asking for it on a machine that has never run Pob answers
// with the id it would use, rather than nothing.
func theInstance(root string) *Instance {
	id := storage.ResolveInstanceID(root)
	if inst := loadInstance(root, id); inst != nil {
		return inst
	}
	return newInstance(root, id)
}

func newInstance(root, id string) *Instance {
	dir := filepath.Join(root, id)
	return &Instance{ID: id, Dir: dir, LogsDir: filepath.Join(dir, "logs")}
}

// loadInstance reads one <id>/logs directory and probes its control port.
// Returns nil when the directory doesn't look like an instance.
func loadInstance(root, id string) *Instance {
	inst := newInstance(root, id)
	instanceJSON := readJSONFile(filepath.Join(inst.LogsDir, "instance.json"))
	controlJSON := readJSONFile(filepath.Join(inst.LogsDir, "control.json"))
	if instanceJSON == nil && controlJSON == nil {
		return nil
	}
	inst.StartTime = intField(instanceJSON, "start_time")
	inst.EndTime = intField(instanceJSON, "end_time")
	inst.Port = int(intField(controlJSON, "port"))
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

	if pob, ok := status["server"].(map[string]any); ok {
		running, _ := pob["running"].(bool)
		urls, _ := pob["urls"].([]any)
		switch {
		case !running:
			fmt.Printf("Server:     off\n")
		case len(urls) == 0:
			fmt.Printf("Server:     %s\n", pob["url"])
		default:
			// One line per network the machine is on: which of them a phone can
			// reach depends on where the phone is.
			for i, u := range urls {
				if i == 0 {
					fmt.Printf("Server:     %v\n", u)
				} else {
					fmt.Printf("            %v\n", u)
				}
			}
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
		fmt.Printf("Logs:     %s\n", filepath.Join(inst.LogsDir, session))
	}
}

func cmdMacro(inst *Instance) {
	if _, err := inst.post("/run/macro", nil, 5*time.Second); err != nil {
		fail("macro failed: %v", err)
	}
	fmt.Printf("Macro session started on instance %s.\n", inst.ID)
	if session := waitForSession(inst); session != "" {
		fmt.Printf("Session:  %s\n", session)
		fmt.Printf("Logs:     %s\n", filepath.Join(inst.LogsDir, session))
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

func cmdMCP(inst *Instance, sub string, port int) {
	switch sub {
	case "", "status", "info":
		info, err := inst.get("/mcp", 3*time.Second)
		if err != nil {
			fail("cannot reach instance %s: %v", inst.ID, err)
		}
		printMCPInfo(info)

	case "start":
		var body any
		if port > 0 {
			body = map[string]any{"port": port}
		}
		info, err := inst.post("/mcp/start", body, 5*time.Second)
		if err != nil {
			fail("mcp start failed: %v", err)
		}
		printMCPInfo(info)
		if url, _ := info["url"].(string); url != "" {
			registerWithAgentCLIs(url)
		}

	case "stop":
		if _, err := inst.post("/mcp/stop", nil, 5*time.Second); err != nil {
			fail("mcp stop failed: %v", err)
		}
		fmt.Println("MCP server stopped.")
		unregisterFromAgentCLIs()

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

// agentCLIs are the coding-agent CLIs whose user-scope MCP settings track
// the pob server: `mcp start` registers it, `mcp stop` removes it. Both
// CLIs share the same `mcp add/remove` sub-command syntax, so registration
// goes through their own tooling and the settings file format stays their
// problem. A CLI missing from PATH is skipped.
var agentCLIs = []struct{ bin, name string }{
	{"claude", "Claude Code"},
	{"gemini", "Gemini CLI"},
}

// registerWithAgentCLIs adds the running MCP server to each installed agent
// CLI's user-scope settings, so their sessions can use the pob tools without
// manual setup. `pob mcp status` still prints the config for manual setup of
// other clients.
func registerWithAgentCLIs(url string) {
	for _, cli := range agentCLIs {
		path, err := exec.LookPath(cli.bin)
		if err != nil {
			continue
		}
		// `mcp add` refuses duplicate names, so drop any previous
		// registration (possibly pointing at another port) first.
		_ = exec.Command(path, "mcp", "remove", "-s", "user", "pob").Run()
		out, err := exec.Command(path, "mcp", "add", "-s", "user", "--transport", "sse", "pob", url).CombinedOutput()
		if err != nil {
			fmt.Printf("%s registration failed: %v\n%s", cli.name, err, out)
			continue
		}
		fmt.Printf("Registered in %s settings (user scope) as \"pob\".\n", cli.name)
	}
}

// unregisterFromAgentCLIs removes the pob entries written by
// registerWithAgentCLIs; silent for CLIs that are missing or have nothing
// registered.
func unregisterFromAgentCLIs() {
	for _, cli := range agentCLIs {
		path, err := exec.LookPath(cli.bin)
		if err != nil {
			continue
		}
		if exec.Command(path, "mcp", "remove", "-s", "user", "pob").Run() == nil {
			fmt.Printf("Removed \"pob\" from %s settings.\n", cli.name)
		}
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
