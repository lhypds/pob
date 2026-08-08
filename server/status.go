package server

import (
	"encoding/json"
	"net/http"
)

// Status is what the index page shows — the same facts `pob status` prints.
// The server knows its own address and nothing else about the instance behind
// it, so the rest is handed in from where it is known (see SetStatus).
//
// It goes out on the open port along with everything else here, which is worth
// knowing before adding to it: whoever can read this can already type on the
// machine, but that is a reason to keep the list to what is useful, not a
// reason to stop caring.
type Status struct {
	Instance  string `json:"instance"`
	Root      string `json:"root"`
	Model     string `json:"model"`
	Executing bool   `json:"executing"`
	Session   string `json:"session"`
	Recording bool   `json:"recording"`
	// MCP is the MCP server's address while it runs, empty when it is stopped.
	MCP string `json:"mcp"`

	// Filled in by the server itself, whatever the instance reports.
	Port int      `json:"port"`
	URLs []string `json:"urls"`
}

// SetStatus hands the server a way to ask what the instance is doing. Until it
// is called the index page shows only what the server knows about itself,
// which is what a test — an instance with nothing behind it — gets.
func (s *Server) SetStatus(status func() Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

func (s *Server) serveStatus(w http.ResponseWriter, r *http.Request) {
	if !isRead(r) {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	ask := s.status
	s.mu.Unlock()

	var status Status
	if ask != nil {
		status = ask()
	}
	// The server's own facts are the server's to report — and the instance id
	// is one of them, since the server was told it at birth.
	status.Instance = s.instance
	status.Port = s.Port()
	status.URLs = s.URLs()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(status)
}
