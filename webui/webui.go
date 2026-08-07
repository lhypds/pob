// Package webui serves the phone-in-your-hand remote control for Pob: a text
// field, a keyboard-mirror button and a trackpad, on a page reachable at
//
//	http://192.168.1.40:8033/pb-a703
//
// It is the pico-hid board's web UI pointed at Pob instead of at a USB HID
// dongle: the page in index.html is that page, and the commands it POSTs are
// that HTTP API, translated in command.go into the same cursor and keyboard
// calls the MCP server makes.
//
// One server runs with the app, on the port in settings.json — the same on
// every machine unless someone changes it, so the address can be typed from
// memory. The instance id is kept in the path so an address that names it
// stays valid, but the bare root is the same page: there is one instance to
// reach (see router.go).
package webui

import (
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultPort is where Pob is reached. Port 80 would let the address be typed
// without one, but binding it needs root on macOS and Linux, which a desktop
// app has no business asking for.
const DefaultPort = 8033

// idleAfter is how long the page must be quiet before the virtual cursor is
// allowed to go back into hiding. Long enough that putting the phone down
// mid-task doesn't lose sight of the cursor, short enough that a forgotten
// open tab doesn't pin it on screen for the rest of the session.
const idleAfter = 90 * time.Second

// maxBody caps a command body. The longest legitimate one is a paste into the
// text field; a megabyte of it is already far past what anyone types.
const maxBody = 1 << 20

//go:embed index.html
var page []byte

type Server struct {
	instance string
	ctl      *controller
	target   Target
	logf     func(string, ...any)

	mu      sync.Mutex
	running bool
	port    int
	server  *http.Server
	active  bool
	idle    *time.Timer
}

// New prepares the server. instance is the instance id, which is also a path
// it answers on: "pb-a703" serves at "/pb-a703" as well as at "/". Nothing is
// bound until Start.
func New(instance string, target Target, logf func(string, ...any)) *Server {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{
		instance: instance,
		ctl:      newController(target, logf),
		target:   target,
		logf:     logf,
	}
}

// Start binds the port and serves. Starting an already-running server is a
// no-op. A port already in use is returned as an error rather than worked
// around: only one instance runs, so something else has it, and quietly
// serving nowhere would be worse than saying so.
//
// The listener is on every interface on purpose — a page only reachable from
// the machine it drives would have no point — so anyone on the same network
// who knows the address can move this machine's pointer and type on it. That
// is the same bargain the pico-hid board makes, and the reason "webui" is a
// setting that can be turned off.
func (s *Server) Start(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	if port <= 0 {
		port = DefaultPort
	}

	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	s.port = port
	s.running = true
	s.server = &http.Server{Handler: http.HandlerFunc(s.route)}
	go func(server *http.Server) {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logf("WebUI: listener failed: %v", err)
		}
	}(s.server)

	// urlsLocked, not URLs: the lock is already held here, and sync.Mutex is
	// not reentrant — taking it again would wedge the instance before it ever
	// served a page.
	s.logf("WebUI: serving at %s", strings.Join(s.urlsLocked(), " "))
	return nil
}

// Stop closes the listener and frees the port.
func (s *Server) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	server, idle := s.server, s.idle
	s.server, s.idle = nil, nil
	wasActive := s.active
	s.active = false
	s.mu.Unlock()

	if idle != nil {
		idle.Stop()
	}
	if wasActive {
		s.target.SetRemoteActive(false)
	}
	if server != nil {
		_ = server.Close()
	}
}

// Running reports whether the server is serving.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Port is the port Pob is reached through.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// URL is the address to open, or "" when the server isn't running. Where the
// machine has more than one address, this is the first of them — see URLs.
func (s *Server) URL() string {
	if urls := s.URLs(); len(urls) > 0 {
		return urls[0]
	}
	return ""
}

// URLs is every address the page can be reached at: one per network the
// machine is on. Each names the instance in the path — the bare root serves
// the same page, but an address that says which machine it drives is the one
// worth writing down, and it is what Pob Keyboard is given.
func (s *Server) URLs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.urlsLocked()
}

func (s *Server) urlsLocked() []string {
	if !s.running {
		return nil
	}
	var urls []string
	for _, ip := range addresses() {
		urls = append(urls, fmt.Sprintf("http://%s/%s",
			net.JoinHostPort(ip.String(), strconv.Itoa(s.port)), s.instance))
	}
	return urls
}

// touch marks the page as being used, which keeps the virtual cursor visible
// while it is and lets it fade back out once the page goes quiet.
func (s *Server) touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	if !s.active {
		s.active = true
		s.target.SetRemoteActive(true)
	}
	if s.idle == nil {
		s.idle = time.AfterFunc(idleAfter, s.goIdle)
		return
	}
	s.idle.Reset(idleAfter)
}

func (s *Server) goIdle() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	s.mu.Unlock()
	s.target.SetRemoteActive(false)
}
