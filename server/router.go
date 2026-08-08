package server

import (
	"io"
	"net/http"
	"strings"
)

// One instance runs, so one address is enough:
//
//	http://192.168.1.40:8033
//
// The instance id still names it — http://192.168.1.40:8033/pb-a703 — so an
// address written down or handed to Pob Keyboard keeps working, and so the
// web UI can say which machine it is driving. Both are the same server; a
// path naming some other instance is a 404, since there is nothing else here.
//
// Three things answer at that address. The root is the machine itself: GET is
// what it looks like right now, POST is a command. The two pages sit under it
// — /control drives the machine, /view watches it — and since the bare root is
// the same server, /control and /pb-a703/control are the same page.

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	head, rest := splitHead(r.URL.Path)
	if head == s.instance {
		// The instance id names this server, so what follows names the page —
		// the same page the bare root would have served.
		head, rest = splitHead(rest)
	} else if _, ok := page(head); head != "" && !ok {
		// Not this instance, and not a page of it: nothing else is here.
		http.Error(w, "no instance "+head+" here", http.StatusNotFound)
		return
	}

	body, ok := page(head)
	if (head != "" && !ok) || (rest != "" && rest != "/") {
		http.NotFound(w, r)
		return
	}
	if head == "" {
		s.serveMachine(w, r)
		return
	}

	// The pages are pages; only the root takes commands.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
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

// page returns the page a path element names. The two names are also the only
// leading path elements that are not an instance id, which is what lets
// /control be read as this machine's control page rather than as a machine
// called "control" that isn't here.
func page(name string) ([]byte, bool) {
	switch name {
	case "control":
		return controlPage, true
	case "view":
		return viewPage, true
	}
	return nil, false
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

// serveMachine answers at the root, which is the machine itself: what it looks
// like now on GET, a command on POST.
func (s *Server) serveMachine(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		// No touch() here on purpose: watching is not driving. The view page
		// asks once a second, so counting it would keep the virtual cursor
		// pinned on screen for as long as a tab is left open.
		shot, err := s.target.CaptureView()
		if err != nil {
			s.logf("Server: cannot capture the view: %v", err)
			http.Error(w, "cannot capture the view", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		// Every frame is a moment that has passed; a cached one is the whole
		// point of asking missed.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(shot)

	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			http.Error(w, "cannot read command", http.StatusBadRequest)
			return
		}
		s.touch()
		// Run before answering: the reply is what tells the client the command
		// landed, and it is what paces the next one — which is how the
		// commands stay in the order they were made.
		s.ctl.run(strings.TrimRight(string(body), "\r\n"))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "OK")

	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
