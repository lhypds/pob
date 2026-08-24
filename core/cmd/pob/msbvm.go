// The microVMs `pob launch --msb` has started, as `pob` lists them. Two
// sources, because neither knows the whole of it: microsandbox holds what a
// machine is — running or stopped, and which host port its screen is published
// on — and the launch leaves a record of which instance went into it, since the
// guest reads that from a copy of INSTANCE the host cannot see afterwards.
//
// Everything here is best-effort by design. A machine with no msb on the PATH,
// a sandbox microsandbox describes in a shape this does not recognise, a VM
// somebody started by hand: each of those is a row missing or a dash in a
// column, never an error over a listing that is otherwise correct.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// msbVM is one microVM as `pob` shows it.
type msbVM struct {
	// Name is the sandbox's, which is the name every msb command takes.
	Name string
	// Instance is the id the guest came up on, "" when nothing recorded it.
	Instance string
	Running  bool
	// VNCPort is the host side of the guest's VNC port, 0 when the sandbox
	// publishes none — a stopped one still names its mapping, so this is a
	// screen that will be there again rather than one that is.
	VNCPort     int
	VNCPassword string
	Created     time.Time
}

// Screen is the address to watch the guest at, and how it signs in. Empty for
// a machine that is not up: a stopped VM's port is nobody's to connect to.
func (v msbVM) Screen() string {
	if !v.Running || v.VNCPort == 0 {
		return "—"
	}
	url := fmt.Sprintf("vnc://127.0.0.1:%d", v.VNCPort)
	if v.VNCPassword == "" {
		return url
	}
	return url + "  (password: " + v.VNCPassword + ")"
}

// InstanceLabel is the instance column: the id, or a dash for the VM that was
// started without leaving a record — by hand, or by a Pob older than this.
func (v msbVM) InstanceLabel() string {
	if v.Instance == "" {
		return "—"
	}
	return v.Instance
}

// msbVMs lists the Pob microVMs microsandbox has, newest first. nil when there
// is no msb on this machine, which is every machine that has never launched
// one — the whole section is then left out rather than reported as empty.
func msbVMs(root string) []msbVM {
	if _, err := exec.LookPath("msb"); err != nil {
		return nil
	}
	out, err := msbJSON("list", "--format", "json")
	if err != nil {
		return nil
	}
	var listed []struct {
		Name      string `json:"name"`
		Image     string `json:"image"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
	}
	if json.Unmarshal(out, &listed) != nil {
		return nil
	}

	records := msbRecords(root)
	var vms []msbVM
	for _, s := range listed {
		if !isPobSandbox(s.Name, s.Image) {
			continue
		}
		vm := msbVM{
			Name:     s.Name,
			Instance: records[s.Name],
			Running:  strings.EqualFold(s.Status, "running"),
		}
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
			vm.Created = t
		}
		// The ports and the password are asked of the sandbox itself: they are
		// what it was actually started with, which is not always what the
		// launch wanted — a port in use moves the mapping one along.
		vm.VNCPort, vm.VNCPassword = msbScreenOf(s.Name)
		vms = append(vms, vm)
	}
	sort.Slice(vms, func(a, b int) bool {
		if vms[a].Running != vms[b].Running {
			return vms[a].Running // what is up comes first
		}
		if !vms[a].Created.Equal(vms[b].Created) {
			return vms[a].Created.After(vms[b].Created)
		}
		return vms[a].Name < vms[b].Name
	})
	return vms
}

// isPobSandbox tells Pob's machines from whatever else microsandbox is running
// on this host. The image is what actually makes one — it is the desktop the
// guest needs — and the name is how a launch that was given another image is
// still recognised.
//
// Both name shapes: msb-<4 hex> is what a launch draws now, and pob-msb is what
// the launches before it were called — a machine from one of those is still a
// Pob to stop and still worth a row in the listing.
func isPobSandbox(name, image string) bool {
	return strings.HasPrefix(image, "pob-msb") ||
		strings.HasPrefix(name, "msb-") || strings.HasPrefix(name, "pob-msb")
}

// msbScreenOf reads the host port the guest's VNC is published on, and the
// password x11vnc was told to ask for. 0 and "" for anything unreadable.
//
// The VNC mapping is found by the guest port the launch put in the sandbox's
// environment rather than by its position in the list: the other two mappings
// are the web UI's and MCP's, and which number those have is the guest's
// settings.json rather than anything fixed.
func msbScreenOf(name string) (port int, password string) {
	out, err := msbJSON("inspect", name, "--format", "json")
	if err != nil {
		return 0, ""
	}
	var detail struct {
		ActiveConfig struct {
			Env []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"env"`
			Network struct {
				Ports []struct {
					GuestPort int `json:"guest_port"`
					HostPort  int `json:"host_port"`
				} `json:"ports"`
			} `json:"network"`
		} `json:"active_config"`
	}
	if json.Unmarshal(out, &detail) != nil {
		return 0, ""
	}

	guestVNC := 5900 // what run.sh serves on when the launch names no other
	for _, env := range detail.ActiveConfig.Env {
		switch env.Key {
		case "POB_MSB_VNC_PORT":
			if n := atoiOr(env.Value, 0); n > 0 {
				guestVNC = n
			}
		case "POB_MSB_VNC_PASSWORD":
			password = env.Value
		}
	}
	for _, p := range detail.ActiveConfig.Network.Ports {
		if p.GuestPort == guestVNC {
			return p.HostPort, password
		}
	}
	return 0, password
}

// msbRecords reads what the launches left under ~/.pob/msb/vms — sandbox name
// to instance id. Missing directory, unreadable file, JSON that is not what it
// was: all of them are simply an instance this cannot name.
func msbRecords(root string) map[string]string {
	records := map[string]string{}
	entries, err := os.ReadDir(filepath.Join(root, "msb", "vms"))
	if err != nil {
		return records
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		record := readJSONFile(filepath.Join(root, "msb", "vms", entry.Name()))
		if record == nil {
			continue
		}
		instance, _ := record["instance"].(string)
		if instance == "" {
			continue
		}
		name, _ := record["name"].(string)
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), ".json")
		}
		records[name] = instance
	}
	return records
}

// msbInstalled is whether there is an msb to ask at all. Every machine that
// has never launched a microVM answers no, and that is the difference between
// "no VMs" and "no microsandbox" — worth telling apart in what is printed.
func msbInstalled() bool {
	_, err := exec.LookPath("msb")
	return err == nil
}

// msbJSON runs one msb command for its output. The timeout is what keeps a
// listing from hanging on it: these are local reads that take milliseconds, and
// a `pob` that sat there waiting for one would be a `pob` that looked broken.
func msbJSON(args ...string) ([]byte, error) {
	return msbCommand(3*time.Second, args...)
}

// msbCommand runs one msb command with a ceiling on how long it may take, and
// kills it if it overruns — the same ceiling vm/msb/launch.sh puts on its own
// msb calls, and for the same reason: one that never returns would otherwise
// be a command of Pob's that never returns.
func msbCommand(timeout time.Duration, args ...string) ([]byte, error) {
	cmd := exec.Command("msb", args...)
	cmd.Stderr = nil
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.Output()
		close(done)
	}()
	select {
	case <-done:
		return out, err
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, fmt.Errorf("msb %s did not answer within %s", strings.Join(args, " "), timeout)
	}
}

// msbStop shuts a sandbox down. Stopped, not removed: the machine is still
// there to be looked at and started again, which is what a stop means anywhere
// else. Nothing takes the name back — a launch draws a new one every time — so a
// stopped machine stays in `msb list` until `msb rm <name>` is asked for it.
//
// A minute of ceiling rather than the three seconds a read gets: this one waits
// for a guest to come down, and a Pob with a macro mid-replay in it takes
// longer to let go than a listing does to answer.
func msbStop(name string) error {
	_, err := msbCommand(60*time.Second, "stop", name)
	return err
}

// msbRemove takes a machine away for good: -f is the guest stopped first if it
// is up, and then the sandbox and the disk it was given are gone.
//
// This is `pob purge`'s, where a stop is not enough — a stopped sandbox is
// still a sandbox, still listed and still holding its disk, and what purge means
// for a machine is that it is not there any more. `pob kill` stays a stop: one
// machine put down is one that can be looked at and started again.
//
// The same minute of ceiling msbStop gets, and for the same reason: what it
// waits for is a guest coming down.
func msbRemove(name string) error {
	_, err := msbCommand(60*time.Second, "rm", "-f", name)
	return err
}

func atoiOr(s string, fallback int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return fallback
		}
		n = n*10 + int(c-'0')
	}
	if s == "" {
		return fallback
	}
	return n
}
