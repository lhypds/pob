// The machine's instance and the HTTP client for the pob-core control API.
//
// A running core writes its pid and control port into <instance>/instance.json.
// It counts as running only when GET /status on that port answers with the
// matching instance ID — a stale port (a crashed instance, a recycled port) is
// thereby ignored.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"pob/core/internal/storage"
)

type Instance struct {
	ID string
	// Name is what `pob new` called it, "" for an instance that was never
	// given one.
	Name string
	// Dir is <root>/<id>, holding instance.json and the src/ tree —
	// settings.json is the machine's, at the root beside INSTANCE. LogsDir is
	// its logs/ subdirectory, holding the sessions.
	Dir       string
	LogsDir   string
	StartTime int64
	EndTime   int64
	Port      int
	Running   bool
	// Locked and ClickThrough are the window as the shell last left it. They
	// are read from instance.json rather than asked of the instance: the shell
	// owns both and writes them there on every change — so that a launch comes
	// back the way the last one was left — which makes the file the answer, and
	// one that is there with the app closed.
	Locked       bool
	ClickThrough bool
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
	// Click-through on is the overlay's resting state and what a shell that has
	// never written the field starts with, so it is the default here too.
	return &Instance{ID: id, Dir: dir, LogsDir: filepath.Join(dir, "logs"), ClickThrough: true}
}

// loadInstance reads one <root>/<id> directory and probes its control port.
// Returns nil when the directory doesn't look like an instance.
func loadInstance(root, id string) *Instance {
	inst := newInstance(root, id)
	instanceJSON := readJSONFile(filepath.Join(inst.Dir, "instance.json"))
	if instanceJSON == nil {
		return nil
	}
	inst.Name, _ = instanceJSON["name"].(string)
	inst.StartTime = intField(instanceJSON, "start_time")
	inst.EndTime = intField(instanceJSON, "end_time")
	inst.Port = int(intField(instanceJSON, "port"))
	inst.Locked, _ = instanceJSON["is_locked"].(bool)
	if through, ok := instanceJSON["is_click_through"].(bool); ok {
		inst.ClickThrough = through
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
	compiler, _ := status["psl"].(string)

	fmt.Printf("Instance:   %s (pid %d)\n", inst.ID, int(intField(status, "pid")))
	if inst.Name != "" {
		fmt.Printf("Name:       %s\n", inst.Name)
	}
	fmt.Printf("Root:       %s\n", status["root"])
	if executing && session != "" {
		fmt.Printf("Executing:  yes (session %s)\n", session)
	} else {
		fmt.Printf("Executing:  %s\n", yesNo(executing))
	}
	fmt.Printf("Recording:  %s\n", yesNo(recording))
	// The two the terminal can set and nothing else reports back. They are the
	// instance's own file rather than anything in the answer above — the shell
	// owns them and writes them there — so they are printed beside it rather
	// than read out of it.
	fmt.Printf("Locked:     %s\n", yesNo(inst.Locked))
	fmt.Printf("Clickthru:  %s\n", onOffText(inst.ClickThrough))
	fmt.Printf("psl:        %s\n", compiler)

	if mcp, ok := status["mcp"].(map[string]any); ok {
		if running, _ := mcp["running"].(bool); running {
			// Every address it answers on, one per line — the same shape as the
			// Server block below, and for the same reason: which of them works
			// depends on where the client is.
			for i, u := range mcpURLs(mcp) {
				if i == 0 {
					fmt.Printf("MCP:        running — %s\n", u)
				} else {
					fmt.Printf("                      %s\n", u)
				}
			}
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
			addr, _ := pob["url"].(string)
			fmt.Printf("Server:     %s\n", dashboardURL(addr))
		default:
			// One line per network the machine is on: which of them a phone can
			// reach depends on where the phone is.
			for i, u := range urls {
				addr, _ := u.(string)
				if i == 0 {
					fmt.Printf("Server:     %s\n", dashboardURL(addr))
				} else {
					fmt.Printf("            %s\n", dashboardURL(addr))
				}
			}
		}
	}
}

// dashboardURL is the address to open: the server's bare root, which answers
// with the index page — what is running here, and links on to the control and
// view pages. The address the core reports names the instance in its path, and
// a GET there is a picture of the screen rather than somewhere to land, so the
// path is dropped, the same way the app's instance badge does it. The
// instance-qualified address stays what the Operation API is called at.
//
// Anything that doesn't parse as an address is printed as it came: a reading
// nobody expected is still better said than swallowed.
func dashboardURL(addr string) string {
	parsed, err := url.Parse(addr)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return addr
	}
	return parsed.Scheme + "://" + parsed.Host + "/"
}

// cmdMacro replays a macro on the running instance: its own main macro, or the
// file --macropsl named, already resolved to an absolute path. After starting
// it polls briefly so it can print the new session's ID.
//
// A named file is printed back. It is the one thing about the run that is not
// what it would usually be, and a session started over the wrong file is worth
// noticing before the cursor has finished moving.
func cmdMacro(inst *Instance, macro string) {
	var body any
	if macro != "" {
		body = map[string]any{"file": macro}
	}
	if _, err := inst.post("/run/macro", body, 5*time.Second); err != nil {
		fail("macro failed: %v", err)
	}
	fmt.Printf("Macro session started on instance %s.\n", inst.ID)
	if macro != "" {
		fmt.Printf("Macro:    %s\n", macro)
	}
	if session := waitForSession(inst); session != "" {
		fmt.Printf("Session:  %s\n", session)
		fmt.Printf("Logs:     %s\n", filepath.Join(inst.LogsDir, session))
	}
}

// problemCount counts what `pob check` found, for the line above the list.
func problemCount(n int) string {
	if n == 1 {
		return "1 problem"
	}
	return fmt.Sprintf("%d problems", n)
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

// cmdStart is `pob start`: the run `pob stop` stops, and what the toolbar's
// Execute button starts. What it adds to cmdMacro is what it says when there is
// nothing to start a run on, since "start" is also the word for starting the
// app — and `pob launch --start` is that.
//
// file is what --macropsl named, "" for the instance's own macro.
func cmdStart(root, file string) {
	inst := theInstance(root)
	if !inst.Running {
		fail("Pob is not running — `pob launch` starts the app, `pob launch --start` starts it and runs the macro")
	}
	macro := ""
	if file != "" {
		macro = resolveMacroPSL(inst, file)
	}
	cmdMacro(inst, macro)
}

func cmdStop(inst *Instance) {
	if _, err := inst.post("/run/stop", nil, 5*time.Second); err != nil {
		fail("stop failed: %v", err)
	}
	fmt.Printf("Stop signal sent to instance %s.\n", inst.ID)
}

// stopAndSettle is `stop`, waited out: the signal sent, and then the runner
// watched until it is actually free.
//
// A stop is a cancel rather than a halt — the session ends when the step it is
// on comes back, which is a keystroke away or a screenshot away — so anything
// that acts on the machine afterwards has to wait for it. Without the wait,
// `restart` would be refused by the run it just stopped and `reset` would send
// the cursor home under a replay still aiming at it.
func stopAndSettle(inst *Instance) {
	if _, err := inst.post("/run/stop", nil, 5*time.Second); err != nil {
		fail("stop failed: %v", err)
	}
	if !waitUntilIdle(inst, 10*time.Second) {
		fail("the session on instance %s did not stop within 10s — `pob status` says what it is doing", inst.ID)
	}
}

// waitUntilIdle polls /status until no session is executing; false when it is
// still running at the deadline, or when the instance stopped answering.
func waitUntilIdle(inst *Instance, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		status, err := inst.get("/status", 2*time.Second)
		if err != nil {
			return false
		}
		if executing, _ := status["executing"].(bool); !executing {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// cmdRestart is `pob restart`: the run stopped and started again, which is the
// pair of commands the macro being written is read between. What it adds to
// typing both is the wait in the middle — `pob stop` returns as soon as the
// cancel is sent, and a `pob start` typed straight after it is refused by the
// session that has not finished going.
//
// file is what --macropsl named, "" for the instance's own macro; the file the
// run that was stopped was of is not remembered, so this starts the same run
// `pob start` would.
func cmdRestart(root, file string) {
	inst := theInstance(root)
	if !inst.Running {
		fail("Pob is not running — `pob launch --start` starts the app and runs the macro")
	}
	macro := ""
	if file != "" {
		macro = resolveMacroPSL(inst, file)
	}
	stopAndSettle(inst)
	cmdMacro(inst, macro)
}

// cmdReset is `pob reset`: the run stopped, and the cursor put back in the
// corner every replay starts from.
//
// The two belong together because the cursor is where a stopped run left it,
// which is nowhere in particular — and the next run starts by moving from
// (20, 20). Resetting under a live session is refused by the instance, so the
// stop is not a courtesy here: it is what makes the reset possible.
func cmdReset(inst *Instance) {
	stopAndSettle(inst)
	result, err := inst.post("/cursor/reset", nil, 10*time.Second)
	if err != nil {
		fail("reset failed: %v", err)
	}
	// Not "instance stopped", which is what `kill` says and something else
	// entirely: what stopped here is the run, and the app is still up.
	fmt.Printf("Stopped, and the cursor is back at %d, %d.\n",
		intField(result, "x"), intField(result, "y"))
}

// cmdLock is `pob lock on|off`: the window held to its size, or let go again.
func cmdLock(inst *Instance, locked bool) {
	if _, err := inst.post("/window/lock", map[string]any{"locked": locked}, 5*time.Second); err != nil {
		fail("lock failed: %v", err)
	}
	fmt.Printf("Window %s.\n", pick(locked, "locked", "unlocked"))
}

// cmdClickThrough is `pob clickthrough on|off`: whether a click on the overlay
// reaches the window underneath.
func cmdClickThrough(inst *Instance, enabled bool) {
	if _, err := inst.post("/window/clickthrough", map[string]any{"enabled": enabled}, 5*time.Second); err != nil {
		fail("clickthrough failed: %v", err)
	}
	fmt.Printf("Click-through %s.\n", onOffText(enabled))
}

// cmdRecord is `pob record start|stop`: the toolbar's record button, pressed
// from the terminal.
//
// Starting appends, and never clears. The button asks which when there is
// something in the macro already, and there is nobody at the window to answer
// it here — so the answer is the one that cannot lose work, and clearing the
// file stays something done to the file itself.
func cmdRecord(inst *Instance, recording bool) {
	if _, err := inst.post("/record", map[string]any{"recording": recording}, 5*time.Second); err != nil {
		fail("record failed: %v", err)
	}
	if recording {
		fmt.Printf("Recording into %s.\n", filepath.Join(inst.Dir, "src", "main.macro.psl"))
		return
	}
	fmt.Println("Recording stopped.")
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

// mcpURLs is every address the MCP server answers on, falling back to the
// single "url" an older instance reports.
func mcpURLs(info map[string]any) []string {
	var urls []string
	if list, ok := info["urls"].([]any); ok {
		for _, u := range list {
			if s, _ := u.(string); s != "" {
				urls = append(urls, s)
			}
		}
	}
	if len(urls) == 0 {
		if url, _ := info["url"].(string); url != "" {
			urls = append(urls, url)
		}
	}
	return urls
}

func printMCPInfo(info map[string]any) {
	running, _ := info["running"].(bool)
	urls := mcpURLs(info)
	fmt.Printf("MCP server: %s\n", map[bool]string{true: "running", false: "stopped"}[running])
	for i, u := range urls {
		if i == 0 {
			fmt.Printf("URL:        %s\n", u)
		} else {
			fmt.Printf("            %s\n", u)
		}
	}
	// Spelled out rather than left to be inferred from the addresses above: a
	// client on another machine connects or does not entirely on this, and a
	// loopback bind is the reason it cannot when everything else looks right.
	if host, _ := info["host"].(string); host != "" {
		note := "this machine only — set \"mcp_host\": \"0.0.0.0\" to open it"
		if host == "0.0.0.0" || host == "::" {
			note = "every interface — reachable from the network"
		}
		fmt.Printf("Host:       %s (%s)\n", host, note)
	}
	if tools, ok := info["tools"].([]any); ok && len(tools) > 0 {
		names := make([]string, len(tools))
		for i, t := range tools {
			names[i], _ = t.(string)
		}
		fmt.Printf("Tools:      %s\n", strings.Join(names, ", "))
	}
	if !running {
		// It starts with the instance, so a stopped one was turned off or could
		// not take its port — either way starting it by hand is the way back.
		fmt.Println("\nIt starts with the instance unless \"mcp\" is false in settings.json.")
		fmt.Println("Start it now with: pob mcp start")
		return
	}
	if len(urls) == 0 {
		return
	}
	fmt.Println("\nMCP client config (e.g. Claude Desktop's claude_desktop_config.json):")
	fmt.Printf(`
{
  "mcpServers": {
    "pob": { "type": "sse", "url": "%s" }
  }
}
`, urls[0])
	// The snippet above is the local client's; a client on another machine
	// needs one of the other addresses in its place, and saying so here is
	// cheaper than working out why localhost did not resolve to this machine.
	if len(urls) > 1 {
		fmt.Printf("\nFrom another machine, use %s in place of the address above.\n", urls[1])
	}
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

// onOffText says a state back in the words the command that sets it takes, so
// what `pob status` prints is what `pob clickthrough` would be typed with.
func onOffText(b bool) string {
	return pick(b, "on", "off")
}

func pick(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}
