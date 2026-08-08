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

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	id, _ := splitInstance(r.URL.Path)
	if id != "" && id != s.instance {
		http.Error(w, "no instance "+id+" here", http.StatusNotFound)
		return
	}
	s.serve(w, r)
}

// splitInstance takes the leading path element apart from the rest: "/pb-a703"
// gives ("pb-a703", ""), and "/pb-a703/" gives ("pb-a703", "/").
func splitInstance(path string) (id, rest string) {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return "", ""
	}
	id, rest, found := strings.Cut(trimmed, "/")
	if !found {
		return id, ""
	}
	return id, "/" + rest
}

// serve is the server itself: the web UI page on GET, a command on POST.
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The page is built into this binary, so a cached copy from an older
		// version would be a page talking to a server that has moved on. It is
		// a few kilobytes over a LAN; re-fetching it costs nothing.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(page)

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
