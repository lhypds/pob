// Package ipc implements bidirectional line-delimited JSON-RPC 2.0 over
// stdin/stdout. The Swift shell spawns pob-core as a child process:
//   - Swift -> Go: notifications (run.macro, run.stop, recording.changed)
//   - Go -> Swift: requests (screenshot.capture, mouse.*, keyboard.*, ui.*) and
//     notifications (session.state)
//
// One JSON object per line. Go request ids are prefixed "go-" so they can
// never collide with ids minted by the Swift side.
//
// Captured frames are the one thing that does not travel this way: they go
// down a second, binary connection instead, so a picture never sits in front
// of a keystroke. See frames.go.
package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

type Handler func(params map[string]any)

type Client struct {
	out     io.Writer
	in      io.Reader
	writeMu sync.Mutex

	handlersMu sync.RWMutex
	handlers   map[string]Handler

	pendingMu sync.Mutex
	pending   map[string]chan result
	nextID    int

	frames frameChannel
}

type result struct {
	value map[string]any
	// bytes is set only by the frame channel: a picture that came down the
	// binary connection rather than as base64 inside value.
	bytes []byte
	err   error
}

func NewStdio() *Client {
	return &Client{
		out:      os.Stdout,
		in:       os.Stdin,
		handlers: map[string]Handler{},
		pending:  map[string]chan result{},
	}
}

// Handle registers a handler for an incoming notification method.
func (c *Client) Handle(method string, fn Handler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[method] = fn
}

// Call sends a request to the Swift side and blocks until the response
// arrives or the stream closes.
func (c *Client) Call(method string, params any) (map[string]any, error) {
	_, ch, err := c.send(method, params)
	if err != nil {
		return nil, err
	}
	r := <-ch
	return r.value, r.err
}

// send writes a request and registers what will answer it. Split out of Call
// so CallFrame can wait on the same pending entry differently — its answer
// may arrive on the frame channel instead, and it waits with a timeout.
func (c *Client) send(method string, params any) (string, chan result, error) {
	c.pendingMu.Lock()
	c.nextID++
	id := fmt.Sprintf("go-%d", c.nextID)
	ch := make(chan result, 1)
	c.pending[id] = ch
	c.pendingMu.Unlock()

	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if err := c.write(msg); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return "", nil, err
	}
	return id, ch, nil
}

// deliver hands an answer to whoever is waiting for that id, from either
// channel. The first answer wins and the entry is gone after it, so a shell
// that replies on both — or a frame that lands after its call timed out —
// cannot deliver twice.
func (c *Client) deliver(id string, r result) {
	c.pendingMu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.pendingMu.Unlock()
	if ch != nil {
		ch <- r
	}
}

// Notify sends a notification (no response expected) to the Swift side.
func (c *Client) Notify(method string, params any) {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	_ = c.write(msg)
}

func (c *Client) write(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.out.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// Run reads messages until stdin closes (i.e. the Swift parent exits).
// It must be called from the main goroutine; returning unblocks shutdown.
func (c *Client) Run() {
	scanner := bufio.NewScanner(c.in)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		c.dispatch(msg)
	}
	// Stream closed: the shell is gone, so nothing more will arrive on the
	// frame channel either.
	c.frames.mu.Lock()
	if c.frames.ln != nil {
		_ = c.frames.ln.Close()
	}
	c.frames.mu.Unlock()

	// Fail all pending calls so waiting goroutines unblock.
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		ch <- result{err: io.EOF}
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
}

func (c *Client) dispatch(msg map[string]any) {
	// Response to one of our requests?
	if id, ok := msg["id"].(string); ok {
		if _, isReq := msg["method"]; !isReq {
			if errObj, ok := msg["error"].(map[string]any); ok {
				c.deliver(id, result{err: fmt.Errorf("%v", errObj["message"])})
			} else {
				value, _ := msg["result"].(map[string]any)
				c.deliver(id, result{value: value})
			}
			return
		}
	}

	// Incoming notification/request from Swift.
	method, _ := msg["method"].(string)
	params, _ := msg["params"].(map[string]any)
	c.handlersMu.RLock()
	fn := c.handlers[method]
	c.handlersMu.RUnlock()
	if fn != nil {
		go fn(params)
	}
}
