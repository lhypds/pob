package ipc

// The frame channel is written four times over — here, and once in each of the
// three shells — so the wire format is the one thing in this repo that a
// change can break in a language the compiler for this one never sees. These
// tests are what that format is, spelled out from the shell's side.

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

// shell is the far end of the IPC: what the Swift, C# and C sides do, in Go.
//
// It reads the core's messages on its own goroutine, because that is what a
// shell does — and because a pipe with nobody reading it blocks the writer, so
// a test that only read when it expected something would deadlock the moment
// the core said anything it had not been asked for.
type shell struct {
	t      *testing.T
	toGo   *io.PipeWriter // the core's stdin
	sent   chan map[string]any
	client *Client
}

func newShell(t *testing.T) *shell {
	t.Helper()
	shellToCore, coreIn := io.Pipe()
	coreOut, shellFromCore := io.Pipe()
	client := &Client{
		out:      shellFromCore,
		in:       shellToCore,
		handlers: map[string]Handler{},
		pending:  map[string]chan result{},
	}
	s := &shell{t: t, toGo: coreIn, sent: make(chan map[string]any, 16), client: client}

	go client.Run()
	go func() {
		defer close(s.sent)
		reader := bufio.NewReader(coreOut)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			var msg map[string]any
			if json.Unmarshal(line, &msg) == nil {
				s.sent <- msg
			}
		}
	}()

	t.Cleanup(func() { _ = coreIn.Close() })
	return s
}

// next reads one JSON-RPC message the core sent.
func (s *shell) next() map[string]any {
	s.t.Helper()
	select {
	case msg, ok := <-s.sent:
		if !ok {
			s.t.Fatal("the core closed its output")
		}
		return msg
	case <-time.After(5 * time.Second):
		s.t.Fatal("the core said nothing")
		return nil
	}
}

// awaitChannel reads the frames.channel offer and connects to it, the way a
// shell does when it comes up.
func (s *shell) awaitChannel(token string) net.Conn {
	s.t.Helper()
	if err := s.client.ServeFrames(); err != nil {
		s.t.Fatalf("ServeFrames: %v", err)
	}
	msg := s.next()
	if msg["method"] != "frames.channel" {
		s.t.Fatalf("first message = %v, want frames.channel", msg["method"])
	}
	params, _ := msg["params"].(map[string]any)
	port, _ := params["port"].(float64)
	if token == "" {
		token, _ = params["token"].(string)
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", int(port)))
	if err != nil {
		s.t.Fatalf("dialling the frame channel: %v", err)
	}
	s.t.Cleanup(func() { _ = conn.Close() })
	if _, err := io.WriteString(conn, token+"\n"); err != nil {
		s.t.Fatalf("writing the token: %v", err)
	}
	return conn
}

// pushFrame writes a frame exactly as the three shells build one.
func pushFrame(t *testing.T, conn net.Conn, id string, meta map[string]any, payload []byte) {
	t.Helper()
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	frame := []byte("POBF")
	frame = binary.BigEndian.AppendUint16(frame, uint16(len(id)))
	frame = append(frame, id...)
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(metaBytes)))
	frame = append(frame, metaBytes...)
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(payload)))
	frame = append(frame, payload...)
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("pushing a frame: %v", err)
	}
}

func TestFrameChannelDeliversPictureAndSizes(t *testing.T) {
	s := newShell(t)
	conn := s.awaitChannel("")

	type answer struct {
		bytes []byte
		meta  map[string]any
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		data, meta, err := s.client.CallFrame("screenshot.capture", map[string]any{"format": "jpeg"})
		done <- answer{data, meta, err}
	}()

	request := s.next()
	if request["method"] != "screenshot.capture" {
		t.Fatalf("request = %v, want screenshot.capture", request["method"])
	}
	id, _ := request["id"].(string)

	payload := []byte("\xff\xd8\xff\xe0 not really a JPEG")
	pushFrame(t, conn, id, map[string]any{
		"width": 1280, "height": 800, "sourceWidth": 3200, "sourceHeight": 2000,
	}, payload)

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("CallFrame: %v", got.err)
		}
		if string(got.bytes) != string(payload) {
			t.Errorf("payload = %q, want %q", got.bytes, payload)
		}
		// The source size is the whole reason metadata rides along: without it
		// a shrunk frame gives a client no way back to Pob's coordinates.
		if got.meta["sourceWidth"] != float64(3200) {
			t.Errorf("sourceWidth = %v, want 3200", got.meta["sourceWidth"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the frame never arrived")
	}
}

// A shell that has no channel open answers on the JSON-RPC line, as every
// shell did before the channel existed. That path is what lets an old shell
// run against a new core, so it has to keep working.
func TestCallFrameFallsBackToTheJSONLine(t *testing.T) {
	s := newShell(t)

	done := make(chan []byte, 1)
	go func() {
		data, _, err := s.client.CallFrame("screenshot.capture", nil)
		if err != nil {
			t.Errorf("CallFrame: %v", err)
		}
		done <- data
	}()

	request := s.next()
	id, _ := request["id"].(string)
	response, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"result": map[string]any{"image": base64.StdEncoding.EncodeToString([]byte("PNG-ish"))},
	})
	if _, err := s.toGo.Write(append(response, '\n')); err != nil {
		t.Fatal(err)
	}

	select {
	case data := <-done:
		// Nothing came down the frame channel, so the bytes are still base64
		// in the result — decoding them is the bridge's job, not this layer's.
		if data != nil {
			t.Errorf("bytes = %q, want nil so the caller reads the base64", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the response never arrived")
	}
}

// Loopback is the security boundary, not the token — but a wrong token still
// has to be turned away, or anything else on this machine that finds the port
// could feed the core frames.
func TestFrameChannelRefusesAWrongToken(t *testing.T) {
	s := newShell(t)
	conn := s.awaitChannel("not-the-token")

	done := make(chan error, 1)
	go func() {
		_, _, err := s.client.CallFrame("screenshot.capture", nil)
		done <- err
	}()
	request := s.next()
	id, _ := request["id"].(string)
	pushFrame(t, conn, id, map[string]any{"width": 10}, []byte("ignored"))

	// The connection is dropped rather than read from, so the call is left to
	// the JSON-RPC line — which nothing is answering here. Close the core's
	// stdin to end it, and it must end in an error rather than that frame.
	time.Sleep(200 * time.Millisecond)
	_ = s.toGo.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a frame on an unauthenticated connection was accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the call never ended")
	}
}

// There is no resynchronising a length-prefixed stream, so a frame that does
// not start where one should must take the connection down rather than be
// read as garbage lengths.
func TestFrameChannelDropsADesynchronisedStream(t *testing.T) {
	s := newShell(t)
	conn := s.awaitChannel("")
	if _, err := conn.Write([]byte("NOPE............")); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, _, err := s.client.CallFrame("screenshot.capture", nil)
		done <- err
	}()
	request := s.next()
	id, _ := request["id"].(string)
	// The connection is gone, so this lands nowhere.
	_, _ = conn.Write([]byte("POBF"))
	pushFrame(t, conn, id, map[string]any{}, []byte("late"))

	time.Sleep(200 * time.Millisecond)
	_ = s.toGo.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a frame on a desynchronised connection was accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the call never ended")
	}
}
