// Package webui serves the phone-in-your-hand remote control for a Pob
// instance: a text field, a keyboard-mirror button and a trackpad, on a page
// reachable at
//
//	http://192.168.1.40:8033/pb-a703
//
// It is the pico-hid board's web UI pointed at Pob instead of at a USB HID
// dongle: the page in index.html is that page, and the commands it POSTs are
// that HTTP API, translated in command.go into the same cursor and keyboard
// calls the MCP server makes.
//
// One server runs per instance, started with the app, and every instance
// shares one port — the one in settings.json, the same on every machine unless
// someone changes it, so the address can be typed from memory. Sharing it
// takes two listeners: a private one on loopback that only this instance
// answers on, and the shared public one, which exactly one instance holds and
// uses to hand each request to the instance the path names (see router.go).
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

// DefaultPort is where every instance on a machine is reached. Port 80 would
// let the address be typed without one, but binding it needs root on macOS and
// Linux, which a desktop app has no business asking for.
const DefaultPort = 8033

// claimInterval is how often an instance that didn't get the shared port tries
// again. It matters when the instance holding it is closed: the next request
// after that has to find somebody still listening.
const claimInterval = 5 * time.Second

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
	registry string // the logs directory the instances publish themselves in
	ctl      *controller
	target   Target
	logf     func(string, ...any)

	mu          sync.Mutex
	running     bool
	port        int          // the shared port, held by one instance for all
	privatePort int          // this instance's own, on loopback only
	private     *http.Server // always ours
	shared      *http.Server // set only while this instance is the front door
	stopping    chan struct{}
	active      bool
	idle        *time.Timer
}

// New prepares the server for one instance. instance is the instance id, which
// is also the path it answers on: "pb-a703" serves at "/pb-a703".
// registryDir is the logs directory holding every instance's directory, which
// is how the instances find each other. Nothing is bound until Start.
func New(instance, registryDir string, target Target, logf func(string, ...any)) *Server {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{
		instance: instance,
		registry: registryDir,
		ctl:      newController(target, logf),
		target:   target,
		logf:     logf,
	}
}

// Start binds this instance's private port and takes the shared port if no
// other instance has it. Starting an already-running server is a no-op.
//
// The shared listener is on every interface on purpose — a page only reachable
// from the machine it drives would have no point — so anyone on the same
// network who knows the address can move this machine's pointer and type on
// it. That is the same bargain the pico-hid board makes, and the reason
// "webui" is a setting that can be turned off.
func (s *Server) Start(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	if port <= 0 {
		port = DefaultPort
	}

	// Loopback and an ephemeral port: this listener is never the way in from
	// the network, only the way the front door reaches this instance, so it
	// can neither clash with anything nor be reached from outside.
	private, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.privatePort = private.Addr().(*net.TCPAddr).Port
	s.private = &http.Server{Handler: http.HandlerFunc(s.route)}
	s.port = port
	s.running = true
	s.stopping = make(chan struct{})
	go s.serve(s.private, private, "private listener")

	s.publish(s.privatePort)

	s.claimLocked()
	// urlsLocked, not URLs: the lock is already held here, and sync.Mutex is
	// not reentrant — taking it again would wedge the instance before it ever
	// served a page.
	where := strings.Join(s.urlsLocked(), " ")
	if s.shared == nil {
		// Another instance already has the port and will pass requests along,
		// which is the ordinary case for a second window.
		s.logf("WebUI: serving at %s (port %d held by another instance)", where, port)
		go s.reclaim()
	} else {
		s.logf("WebUI: serving at %s", where)
	}
	return nil
}

// claimLocked tries to take the shared port, and serves the front door if it
// gets it. Failing is normal — it means another instance has it.
func (s *Server) claimLocked() {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(s.port))
	if err != nil {
		return
	}
	s.shared = &http.Server{Handler: http.HandlerFunc(s.route)}
	go s.serve(s.shared, listener, "shared listener")
}

// reclaim keeps trying for the shared port until this instance gets it or
// stops. The instance holding it is a window like any other, and when it is
// closed somebody has to take over or nothing on the machine is reachable.
func (s *Server) reclaim() {
	ticker := time.NewTicker(claimInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopping:
			return
		case <-ticker.C:
			s.mu.Lock()
			if !s.running {
				s.mu.Unlock()
				return
			}
			if s.shared == nil {
				s.claimLocked()
			}
			took := s.shared != nil
			s.mu.Unlock()
			if took {
				s.logf("WebUI: took over port %d", s.Port())
				return
			}
		}
	}
}

func (s *Server) serve(server *http.Server, listener net.Listener, what string) {
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		s.logf("WebUI: %s failed: %v", what, err)
	}
}

// Stop closes both listeners and takes this instance out of the registry —
// which also frees the shared port for whichever instance is still waiting on
// it.
func (s *Server) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	private, shared, idle, stopping := s.private, s.shared, s.idle, s.stopping
	s.private, s.shared, s.idle = nil, nil, nil
	wasActive := s.active
	s.active = false
	s.mu.Unlock()

	close(stopping)
	s.unpublish()
	if idle != nil {
		idle.Stop()
	}
	if wasActive {
		s.target.SetRemoteActive(false)
	}
	if shared != nil {
		_ = shared.Close()
	}
	if private != nil {
		_ = private.Close()
	}
}

// Running reports whether this instance is serving.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Port is the shared port every instance on this machine is reached through.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// HoldsPort reports whether this instance is the one serving the shared port
// for the machine. Nothing depends on which instance it is — it is worth
// knowing only when working out why nothing is reachable.
func (s *Server) HoldsPort() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shared != nil
}

// URL is the address to open, or "" when the server isn't running. Where the
// machine has more than one address, this is the first of them — see URLs.
func (s *Server) URL() string {
	if urls := s.URLs(); len(urls) > 0 {
		return urls[0]
	}
	return ""
}

// URLs is every address this instance can be reached at: one per network the
// machine is on, each naming this instance in the path, since one port serves
// them all.
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
