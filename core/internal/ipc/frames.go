package ipc

// The frame channel: a second, binary connection the shell pushes captured
// frames down, instead of answering with them on the JSON-RPC line.
//
// A frame does not belong on that line. Base64 is a third again as many
// bytes, and JSON has to be parsed as one enormous string before any of it
// can be used — but the real cost is that the line is shared. Every mouse
// move and keystroke answers on it too, and behind a megabyte of picture they
// wait. At one frame a second that is invisible; at thirty it is the whole
// difference between a view you can work in and one you can only watch.
//
// So control stays where it was and pictures move here. The core listens on
// loopback, tells the shell the port and a token in a frames.channel
// notification, and the shell connects back and pushes. It is a socket rather
// than an inherited pipe because the three shells are Swift, C# and C, and a
// loopback socket is the one thing all three do the same way.
//
// A shell that never connects is not a broken shell — it answers with base64
// on the JSON-RPC line as before, and CallFrame takes whichever arrives. That
// is what lets an old shell run against a new core, and it is the fallback if
// the connection is ever lost mid-session.

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// A frame on the wire:
//
//	"POBF"                 magic, so a desynchronised stream is spotted
//	uint16  idLen          length of the request id
//	bytes   id             the id of the screenshot.capture this answers
//	uint32  metaLen        length of the metadata
//	bytes   meta           JSON object: the picture's size, and the source's
//	uint32  payloadLen     length of the picture
//	bytes   payload        the encoded picture itself
//
// Big-endian throughout, since that is what every language's "write an
// integer to a socket" reaches for first.
var frameMagic = [4]byte{'P', 'O', 'B', 'F'}

// maxFrameBytes caps a single picture. A full-screen PNG at 5K is around
// 20 MB; past 64 MB something has gone wrong and reading it would only turn a
// bad length into an allocation the machine cannot make.
const maxFrameBytes = 64 << 20

// frameCallTimeout bounds a capture that was sent but never answered — on
// either channel. Without it a shell that dies mid-frame would leave the
// caller, and with it whichever HTTP request or agent step is waiting, blocked
// for good. Generous: a 5K PNG on a slow machine is a second or two.
const frameCallTimeout = 20 * time.Second

type frameChannel struct {
	mu    sync.Mutex
	token string
	ln    net.Listener
}

// ServeFrames opens the frame channel and tells the shell where to find it.
// Safe to call before the shell is listening for notifications: a shell that
// misses it, or ignores it, simply keeps answering on the JSON-RPC line.
func (c *Client) ServeFrames() error {
	// Loopback only. The token is not the security boundary — the interface
	// is — but it keeps anything else on this machine that happens to find
	// the port from feeding us frames.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		_ = ln.Close()
		return err
	}
	token := hex.EncodeToString(raw)

	c.frames.mu.Lock()
	c.frames.token = token
	c.frames.ln = ln
	c.frames.mu.Unlock()

	go c.acceptFrames(ln)
	c.Notify("frames.channel", map[string]any{
		"port":  ln.Addr().(*net.TCPAddr).Port,
		"token": token,
	})
	return nil
}

// FramePort is the port the frame channel is listening on, or 0 if it was
// never opened. Used by the shell handshake tests.
func (c *Client) FramePort() int {
	c.frames.mu.Lock()
	defer c.frames.mu.Unlock()
	if c.frames.ln == nil {
		return 0
	}
	return c.frames.ln.Addr().(*net.TCPAddr).Port
}

func (c *Client) acceptFrames(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // the listener is closed; we are shutting down
		}
		// One shell, so in practice one connection — but a reconnect after a
		// dropped one must be taken, and both are simply read from until they
		// end.
		go c.readFrames(conn)
	}
}

func (c *Client) readFrames(conn net.Conn) {
	defer conn.Close()
	// Big enough that a frame is a handful of reads rather than hundreds.
	r := bufio.NewReaderSize(conn, 256*1024)

	// The token is the first line, before anything is trusted — and it is
	// checked whatever happens with the deadline, which is only there so a
	// connection that opens and says nothing does not hold a goroutine for
	// good. Skipping the check when the deadline cannot be set would leave
	// the one case where it matters unguarded.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := r.ReadString('\n')
	c.frames.mu.Lock()
	want := c.frames.token
	c.frames.mu.Unlock()
	if err != nil || want == "" || strings.TrimSpace(line) != want {
		return
	}
	// From here the connection is idle between frames, which is not a fault.
	_ = conn.SetReadDeadline(time.Time{})

	for {
		id, meta, payload, err := readFrame(r)
		if err != nil {
			return // desynchronised or closed; a reconnect starts clean
		}
		c.deliver(id, result{value: meta, bytes: payload})
	}
}

func readFrame(r io.Reader) (id string, meta map[string]any, payload []byte, err error) {
	var magic [4]byte
	if _, err = io.ReadFull(r, magic[:]); err != nil {
		return "", nil, nil, err
	}
	if magic != frameMagic {
		// There is no resynchronising a length-prefixed stream: the only safe
		// move is to drop the connection and let the shell open a new one.
		return "", nil, nil, fmt.Errorf("frame channel out of step")
	}

	var idLen uint16
	if err = binary.Read(r, binary.BigEndian, &idLen); err != nil {
		return "", nil, nil, err
	}
	idBytes := make([]byte, idLen)
	if _, err = io.ReadFull(r, idBytes); err != nil {
		return "", nil, nil, err
	}

	metaBytes, err := readChunk(r)
	if err != nil {
		return "", nil, nil, err
	}
	payload, err = readChunk(r)
	if err != nil {
		return "", nil, nil, err
	}

	meta = map[string]any{}
	if len(metaBytes) > 0 {
		if err = json.Unmarshal(metaBytes, &meta); err != nil {
			return "", nil, nil, err
		}
	}
	return string(idBytes), meta, payload, nil
}

func readChunk(r io.Reader) ([]byte, error) {
	var n uint32
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return nil, err
	}
	if n > maxFrameBytes {
		return nil, fmt.Errorf("frame channel: %d bytes is past the cap", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// CallFrame sends a request whose answer is a picture, and takes it from
// whichever channel it arrives on: the frame channel if the shell has one
// open, the JSON-RPC line if it does not. The bytes are nil in the second
// case — the caller reads them out of the result, where they are base64.
func (c *Client) CallFrame(method string, params any) ([]byte, map[string]any, error) {
	id, ch, err := c.send(method, params)
	if err != nil {
		return nil, nil, err
	}
	select {
	case r := <-ch:
		return r.bytes, r.value, r.err
	case <-time.After(frameCallTimeout):
		// Drop the pending entry so a very late answer is discarded rather
		// than handed to whoever asks next.
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, nil, fmt.Errorf("%s: no answer in %s", method, frameCallTimeout)
	}
}
