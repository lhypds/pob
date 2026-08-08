package server

import (
	"io"
	"net/http"
	"strings"
)

// One instance runs, so one address is enough:
//
//	http://192.168.1.40:8033/pb-a703
//
// That address is the machine: GET is what it looks like right now, POST is a
// command. Its pages hang off it — /control drives the machine, /view watches
// it — and a path naming some other instance is a 404, since there is nothing
// else here.
//
// The bare root is not the machine. It is the index: what is running here, and
// the machine's own address to go on with. Serving the frame there too would
// mean the shortest address on the network answers with a picture of someone's
// screen, which is not a thing to hand out by accident. A command POSTed to
// the root still lands, though — that is a client that already has the
// address, and it is the one thing that was ever documented as working there.

// known are the paths under the instance root, and also the only leading path
// elements that are not an instance id — which is what lets /control be read
// as this machine's control page rather than as a machine that isn't here.
var known = map[string]bool{"control": true, "view": true, "status": true}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	head, rest := splitHead(r.URL.Path)
	named := head == s.instance // the path said which machine it wants
	if named {
		head, rest = splitHead(rest)
	} else if head != "" && !known[head] {
		http.Error(w, "no instance "+head+" here", http.StatusNotFound)
		return
	}
	if (head != "" && !known[head]) || (rest != "" && rest != "/") {
		http.NotFound(w, r)
		return
	}

	switch {
	case head == "control":
		servePage(w, r, controlPage)
	case head == "view":
		servePage(w, r, viewPage)
	case head == "status":
		s.serveStatus(w, r)
	case r.Method == http.MethodPost:
		s.serveCommand(w, r)
	case named:
		s.serveFrame(w, r)
	default:
		servePage(w, r, indexPage)
	}
}

// splitHead takes the leading path element apart from the rest: "/pb-a703"
// gives ("pb-a703", ""), and "/pb-a703/view" gives ("pb-a703", "/view").
func splitHead(path string) (head, rest string) {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return "", ""
	}
	head, rest, found := strings.Cut(trimmed, "/")
	if !found {
		return head, ""
	}
	return head, "/" + rest
}

// isRead reports whether the request only wants to look. HEAD is the same
// answer without the body, which net/http drops on the way out.
func isRead(r *http.Request) bool {
	return r.Method == http.MethodGet || r.Method == http.MethodHead
}

func servePage(w http.ResponseWriter, r *http.Request, body []byte) {
	if !isRead(r) {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The pages are built into this binary, so a cached copy from an older
	// version would be a page talking to a server that has moved on. They are
	// a few kilobytes over a LAN; re-fetching them costs nothing.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// serveFrame answers with what the machine looks like right now.
func (s *Server) serveFrame(w http.ResponseWriter, r *http.Request) {
	if !isRead(r) {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// No touch() here on purpose: watching is not driving. The view page asks
	// once a second, so counting it would keep the virtual cursor pinned on
	// screen for as long as a tab is left open.
	shot, err := s.target.CaptureView()
	if err != nil {
		s.logf("Server: cannot capture the view: %v", err)
		http.Error(w, "cannot capture the view", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// Every frame is a moment that has passed; a cached one is the whole point
	// of asking missed.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(shot)
}

func (s *Server) serveCommand(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		http.Error(w, "cannot read command", http.StatusBadRequest)
		return
	}
	s.touch()
	// Run before answering: the reply is what tells the client the command
	// landed, and it is what paces the next one — which is how the commands
	// stay in the order they were made.
	s.ctl.run(strings.TrimRight(string(body), "\r\n"))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "OK")
}
