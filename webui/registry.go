package webui

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// Every instance serves its own web UI on a private loopback port and writes
// that port to logs/<instance>/webui.json. Whichever instance holds the shared
// public port reads these to find its siblings, so a request for one instance
// can be handed to the process that owns it.
//
// The file is removed on a clean stop. A crashed instance leaves one behind,
// so a peer only counts once its port answers — cheaper and more portable than
// asking the system whether a recorded pid is still alive.

const registryFile = "webui.json"

// peerTimeout bounds the liveness check. It is a loopback connect, so anything
// slower than this is a port that nothing is listening on.
const peerTimeout = 150 * time.Millisecond

type peer struct {
	ID   string
	Port int
}

// Addr is the loopback address this peer serves on.
func (p peer) Addr() string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(p.Port)) }

// publish records this instance's private port for the others to find.
func (s *Server) publish(port int) {
	if s.registry == "" {
		return
	}
	dir := filepath.Join(s.registry, s.instance)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.logf("WebUI: cannot create %s: %v", dir, err)
		return
	}
	data, _ := json.MarshalIndent(map[string]any{
		"port": port,
		"pid":  os.Getpid(),
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, registryFile), append(data, '\n'), 0o644); err != nil {
		s.logf("WebUI: cannot write %s: %v", registryFile, err)
	}
}

// unpublish takes this instance out of the registry, so a front door stops
// offering a link to something that has gone.
func (s *Server) unpublish() {
	if s.registry == "" {
		return
	}
	_ = os.Remove(filepath.Join(s.registry, s.instance, registryFile))
}

// peers lists the instances currently serving, this one included, in id order
// so a list of them doesn't reshuffle between page loads.
func (s *Server) peers() []peer {
	if s.registry == "" {
		return nil
	}
	entries, err := os.ReadDir(s.registry)
	if err != nil {
		return nil
	}
	var live []peer
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.registry, entry.Name(), registryFile))
		if err != nil {
			continue
		}
		var record struct {
			Port int `json:"port"`
		}
		if json.Unmarshal(data, &record) != nil || record.Port <= 0 {
			continue
		}
		p := peer{ID: entry.Name(), Port: record.Port}
		if p.ID != s.instance && !answering(p.Addr()) {
			continue // left behind by an instance that didn't shut down cleanly
		}
		live = append(live, p)
	}
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	return live
}

// peerFor finds the instance with this id, if it is serving.
func (s *Server) peerFor(id string) (peer, bool) {
	for _, p := range s.peers() {
		if p.ID == id {
			return p, true
		}
	}
	return peer{}, false
}

func answering(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, peerTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
