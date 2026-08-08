package server

import (
	"io"
	"net/http"
	"strconv"
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
var known = map[string]bool{"control": true, "view": true, "status": true, "pob.js": true}

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
		serveAsset(w, r, "text/html; charset=utf-8", controlPage)
	case head == "view":
		serveAsset(w, r, "text/html; charset=utf-8", viewPage)
	case head == "pob.js":
		// The pages ask for this relatively, so it is reached by whichever
		// address they were — with the instance in the path or without it.
		serveAsset(w, r, "text/javascript; charset=utf-8", script)
	case head == "status":
		s.serveStatus(w, r)
	case r.Method == http.MethodPost:
		s.serveCommand(w, r)
	case named:
		s.serveFrame(w, r)
	default:
		serveAsset(w, r, "text/html; charset=utf-8", indexPage)
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

func serveAsset(w http.ResponseWriter, r *http.Request, contentType string, body []byte) {
	if !isRead(r) {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", contentType)
	// The pages are built into this binary, so a cached copy from an older
	// version would be a page talking to a server that has moved on. They are
	// a few kilobytes over a LAN; re-fetching them costs nothing.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// Frames are answered to order, through the query string:
//
//	?format=jpeg&w=1280&q=70
//
// with no parameters meaning what it has always meant — a full-size PNG. That
// default is the documented API and the agent's own capture, and it stays
// exactly as it was; the parameters are for the view page, which wants
// something else entirely. It is watching, not reading: a picture no bigger
// than the box it is drawn in, encoded as cheaply as still looks like the
// screen, as often as the machine can manage. Same address, same frame, told
// what it is for.
const (
	defaultQuality = 70   // JPEG; past ~80 the bytes climb faster than the picture improves
	maxFrameWidth  = 4096 // a shrink is the point; asking to grow one is not
)

// serveFrame answers with what the machine looks like right now.
func (s *Server) serveFrame(w http.ResponseWriter, r *http.Request) {
	if !isRead(r) {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()

	format := "png"
	contentType := "image/png"
	switch strings.ToLower(query.Get("format")) {
	case "", "png":
	case "jpeg", "jpg":
		format = "jpeg"
		contentType = "image/jpeg"
	default:
		http.Error(w, "unknown format: png or jpeg", http.StatusBadRequest)
		return
	}

	width := clampParam(query.Get("w"), 1, maxFrameWidth)
	quality := clampParam(query.Get("q"), 1, 100)
	if format == "jpeg" && quality == 0 {
		quality = defaultQuality
	}

	// No touch() here on purpose: watching is not driving. Counting it would
	// keep the virtual cursor pinned on screen for as long as a tab is left
	// open — and at a watchable frame rate, permanently.
	shot, sourceW, sourceH, err := s.target.CaptureView(format, width, quality)
	if err != nil {
		s.logf("Server: cannot capture the view: %v", err)
		http.Error(w, "cannot capture the view", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", contentType)
	// Every frame is a moment that has passed; a cached one is the whole point
	// of asking missed.
	w.Header().Set("Cache-Control", "no-store")
	// How big the frame would have been unshrunk, which is the only way a
	// client can turn a click on it back into a position on the machine. Sent
	// on every frame rather than looked up once: the window is resizable, so
	// the answer changes underneath a page that is already open.
	if sourceW > 0 && sourceH > 0 {
		w.Header().Set("X-Pob-Source-Width", strconv.Itoa(sourceW))
		w.Header().Set("X-Pob-Source-Height", strconv.Itoa(sourceH))
	}
	_, _ = w.Write(shot)
}

// clampParam reads a whole-number query parameter, answering 0 — "not asked
// for" — for anything missing or unreadable. A number out of range is pulled
// into it rather than refused: these are hints about what to send back, and
// failing a whole frame over one would be a strange way to treat them.
func clampParam(raw string, low, high int) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	if n < low {
		return low
	}
	if n > high {
		return high
	}
	return n
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
