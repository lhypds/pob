package webui

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// One port serves every instance on the machine, and the path says which:
//
//	http://192.168.1.40:8033/pb-a703
//
// Only one process can hold that port, so whichever instance binds it first
// becomes the front door and hands requests for the others to the private
// loopback port each one publishes in the registry. Every instance runs this
// same router — on its own private listener it only ever sees requests for
// itself, and on the shared listener it also sees its siblings'.

// proxiedHeader marks a request the front door has already passed on. Without
// it, an id the receiving instance doesn't recognise would be sent onwards
// again, round and round.
const proxiedHeader = "X-Pob-Proxied"

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	id, rest := splitInstance(r.URL.Path)

	if id == "" {
		s.routeRoot(w, r)
		return
	}
	if id == s.instance && rest == "" {
		// A path with no trailing slash would make the page's own relative
		// requests resolve one level too high, so settle it before serving.
		// Only worth doing for the page itself — see routeRoot on why a
		// command is never redirected.
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			http.Redirect(w, r, "/"+id+"/", http.StatusFound)
			return
		}
	}
	s.dispatch(w, r, id)
}

// routeRoot answers a request that named no instance. A machine running a
// single instance has only one thing it can mean, which is what makes the
// address worth typing without the path when there is nothing to choose
// between.
func (s *Server) routeRoot(w http.ResponseWriter, r *http.Request) {
	var id string
	peers := s.peers()
	if len(peers) == 1 {
		id = peers[0].ID
	}
	if id == "" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "name an instance: POST to /<instance>", http.StatusNotFound)
			return
		}
		s.serveIndex(w, peers)
		return
	}

	// The page is redirected so that the path it was served from names the
	// instance — that path is what its own commands go back to. A command
	// itself is answered where it landed: a redirect would be turned into a
	// GET by most HTTP clients, and the keystroke would vanish on the way.
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		http.Redirect(w, r, "/"+id+"/", http.StatusFound)
		return
	}
	s.dispatch(w, r, id)
}

// dispatch serves the request here if it belongs to this instance, and hands
// it to the instance that owns it if not.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, id string) {
	if id == s.instance {
		s.serveInstance(w, r)
		return
	}
	s.proxy(w, r, id)
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

// proxy hands the request to the instance that owns it.
func (s *Server) proxy(w http.ResponseWriter, r *http.Request, id string) {
	if r.Header.Get(proxiedHeader) != "" {
		// Passed to us by mistake — answering rather than forwarding again is
		// what stops it going round in circles.
		http.Error(w, "instance "+id+" is not here", http.StatusBadGateway)
		return
	}
	target, ok := s.peerFor(id)
	if !ok {
		http.Error(w, "no running instance "+id, http.StatusNotFound)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: target.Addr()})
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		s.logf("WebUI: cannot reach instance %s: %v", id, err)
		http.Error(w, "instance "+id+" is not answering", http.StatusBadGateway)
	}
	r.Header.Set(proxiedHeader, "1")
	proxy.ServeHTTP(w, r)
}

// serveInstance is the web UI itself: the page on GET, a command on POST.
func (s *Server) serveInstance(w http.ResponseWriter, r *http.Request) {
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

// serveIndex is what a machine running several instances shows at the root:
// which ones are up, and a way into each. Deliberately plain — it is a
// signpost, not a screen anyone spends time on.
func (s *Server) serveIndex(w http.ResponseWriter, peers []peer) {
	var body strings.Builder
	body.WriteString(`<!doctype html><html lang="en"><head><meta charset="UTF-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<title>Pob</title><link rel="icon" href="data:,"><style>` +
		`body{margin:0;padding:18px 16px;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;color:#111;display:flex;justify-content:center}` +
		`main{width:100%;max-width:630px}` +
		`h1{font-size:15px;font-weight:600;margin:0 0 16px}` +
		`a{display:block;padding:0 12px;height:46px;line-height:44px;border:1px solid #111;margin-top:-1px;color:#111;text-decoration:none;font-variant-numeric:tabular-nums}` +
		`a:active{opacity:.7}p{color:#666;font-size:14px}` +
		`</style></head><body><main>`)
	if len(peers) == 0 {
		body.WriteString(`<h1>Pob</h1><p>No instance is serving.</p>`)
	} else {
		body.WriteString(`<h1>Pick an instance</h1>`)
		for _, p := range peers {
			id := html.EscapeString(p.ID)
			fmt.Fprintf(&body, `<a href="/%s/">%s</a>`, id, id)
		}
	}
	body.WriteString(`</main></body></html>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, body.String())
}
