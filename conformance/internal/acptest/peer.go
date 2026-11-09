// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

// Package acptest provides an independent ACP peer for conformance tests.
//
// A Peer plays the other end of a connection served by the SDK. It speaks
// newline-delimited JSON-RPC over in-memory pipes using only the standard JSON
// codec, so the SDK's own encoder and decoder never check themselves. The pipes
// are synchronous and work inside testing/synctest bubbles.
package acptest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

// Timeout bounds every wait in this package. It is a failure bound, never an
// ordering assumption; inside a synctest bubble it runs on the fake clock.
const Timeout = 5 * time.Second

// Message is one frame written by the SDK, decoded with the standard library.
type Message struct {
	// Line is the frame exactly as written, without its trailing newline.
	Line []byte
	// Fields holds every top-level member, for envelope-shape assertions.
	Fields map[string]json.RawMessage

	ID     json.RawMessage // nil when the frame has no id member
	Method string
	Params json.RawMessage
	Result json.RawMessage
	Error  *Error
}

// Error is a JSON-RPC error object as written on the wire.
type Error struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Has reports whether the frame contains the named top-level member.
func (m Message) Has(name string) bool {
	_, ok := m.Fields[name]
	return ok
}

// IsResponse reports whether the frame is a response rather than a request or
// notification.
func (m Message) IsResponse() bool { return !m.Has("method") && m.Has("id") }

// IsNotification reports whether the frame is a notification.
func (m Message) IsNotification() bool { return m.Has("method") && !m.Has("id") }

func (m Message) String() string { return string(m.Line) }

// Peer is the remote end of an SDK connection.
type Peer struct {
	t testing.TB

	toSDK      *io.PipeWriter
	sdkInput   *io.PipeReader
	fromSDK    *io.PipeReader
	sdkOutput  *io.PipeWriter
	readerDone chan struct{}

	mu      sync.Mutex
	queue   [][]byte      // frames written by the SDK and not yet read
	readErr error         // set once SDK output ends
	ready   chan struct{} // signaled when queue or readErr changes
}

// NewPeer returns a peer and the transport the SDK side should use. Input and
// output are separate pipes, so the peer can close its output (the SDK's input)
// while still reading what the SDK writes. Cleanup closes every pipe.
func NewPeer(t testing.TB) (*Peer, acp.Transport) {
	t.Helper()
	sdkInput, toSDK := io.Pipe()
	fromSDK, sdkOutput := io.Pipe()
	p := &Peer{
		t:          t,
		toSDK:      toSDK,
		sdkInput:   sdkInput,
		fromSDK:    fromSDK,
		sdkOutput:  sdkOutput,
		readerDone: make(chan struct{}),
		ready:      make(chan struct{}, 1),
	}
	go p.collect()
	t.Cleanup(p.shutdown)
	return p, &acp.IOTransport{Reader: sdkInput, Writer: sdkOutput}
}

// collect reads SDK output into an unbounded queue, so SDK writes never wait
// for the test to read them.
func (p *Peer) collect() {
	defer close(p.readerDone)
	r := bufio.NewReader(p.fromSDK)
	for {
		line, err := r.ReadBytes('\n')
		p.mu.Lock()
		if len(line) > 0 {
			p.queue = append(p.queue, bytes.TrimSuffix(line, []byte("\n")))
		}
		if err != nil {
			if errors.Is(err, io.ErrClosedPipe) {
				err = io.EOF
			}
			p.readErr = err
		}
		p.mu.Unlock()
		select {
		case p.ready <- struct{}{}:
		default:
		}
		if err != nil {
			return
		}
	}
}

func (p *Peer) shutdown() {
	_ = p.toSDK.Close()
	_ = p.sdkInput.Close()
	_ = p.sdkOutput.Close()
	_ = p.fromSDK.Close()
	select {
	case <-p.readerDone:
	case <-time.After(Timeout):
		p.t.Error("acptest: output collector did not stop")
	}
}

// SendLine writes one raw frame followed by a newline. Use it for malformed or
// byte-exact input; Send is simpler for well-formed values.
func (p *Peer) SendLine(line string) {
	p.t.Helper()
	p.write([]byte(line + "\n"))
}

// SendBytes writes data exactly as given, without adding a newline.
func (p *Peer) SendBytes(data []byte) {
	p.t.Helper()
	p.write(data)
}

func (p *Peer) write(data []byte) {
	p.t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := p.toSDK.Write(data)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			p.t.Fatalf("acptest: writing to SDK: %v", err)
		}
	case <-time.After(Timeout):
		p.t.Fatalf("acptest: SDK did not read %q", data)
	}
}

// Send marshals v with encoding/json and writes it as one frame.
func (p *Peer) Send(v any) {
	p.t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		p.t.Fatalf("acptest: encoding frame: %v", err)
	}
	p.write(append(data, '\n'))
}

// Request sends a request. The id and params are marshaled with encoding/json;
// pass json.RawMessage for exact tokens such as large integers. Nil params
// omit the member.
func (p *Peer) Request(id any, method string, params any) {
	p.t.Helper()
	frame := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		frame["params"] = params
	}
	p.Send(frame)
}

// Notify sends a notification. Nil params omit the member.
func (p *Peer) Notify(method string, params any) {
	p.t.Helper()
	frame := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		frame["params"] = params
	}
	p.Send(frame)
}

// Reply answers a request with a result.
func (p *Peer) Reply(id json.RawMessage, result any) {
	p.t.Helper()
	p.Send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

// ReplyError answers a request with an error. Nil data omits the member.
func (p *Peer) ReplyError(id json.RawMessage, code int64, message string, data any) {
	p.t.Helper()
	e := map[string]any{"code": code, "message": message}
	if data != nil {
		e["data"] = data
	}
	p.Send(map[string]any{"jsonrpc": "2.0", "id": id, "error": e})
}

// Next returns the next frame the SDK writes and fails the test if none
// arrives within Timeout or the SDK output ends.
func (p *Peer) Next() Message {
	p.t.Helper()
	m, err := p.TryNext(Timeout)
	if err != nil {
		p.t.Fatalf("acptest: waiting for SDK frame: %v", err)
	}
	return m
}

// TryNext waits up to d for the next frame. It returns io.EOF once the SDK's
// output has ended and context.DeadlineExceeded when nothing arrives in time.
func (p *Peer) TryNext(d time.Duration) (Message, error) {
	p.t.Helper()
	timeout := time.After(d)
	for {
		p.mu.Lock()
		if len(p.queue) > 0 {
			line := p.queue[0]
			p.queue = p.queue[1:]
			p.mu.Unlock()
			return p.decode(line), nil
		}
		err := p.readErr
		p.mu.Unlock()
		if err != nil {
			return Message{}, err
		}
		select {
		case <-p.ready:
		case <-timeout:
			return Message{}, context.DeadlineExceeded
		}
	}
}

func (p *Peer) decode(line []byte) Message {
	p.t.Helper()
	m := Message{Line: line}
	if err := json.Unmarshal(line, &m.Fields); err != nil || m.Fields == nil {
		p.t.Fatalf("acptest: SDK wrote a frame that is not a JSON object: %q", line)
	}
	if version := string(m.Fields["jsonrpc"]); version != `"2.0"` {
		p.t.Fatalf("acptest: SDK wrote jsonrpc %s, want \"2.0\": %q", version, line)
	}
	m.ID = m.Fields["id"]
	if m.Has("id") && !validID(m.ID) {
		p.t.Fatalf("acptest: SDK wrote an id that is not a string, integer or null: %q", line)
	}
	if raw, ok := m.Fields["method"]; ok {
		if err := json.Unmarshal(raw, &m.Method); err != nil {
			p.t.Fatalf("acptest: SDK wrote a non-string method: %q", line)
		}
		if m.Has("result") || m.Has("error") {
			p.t.Fatalf("acptest: SDK wrote a request with a result or error: %q", line)
		}
	} else if !m.Has("id") || m.Has("result") == m.Has("error") {
		p.t.Fatalf("acptest: SDK wrote a response without an id or exactly one of result and error: %q", line)
	}
	m.Params = m.Fields["params"]
	m.Result = m.Fields["result"]
	if raw, ok := m.Fields["error"]; ok {
		m.Error = new(Error)
		if err := json.Unmarshal(raw, m.Error); err != nil {
			p.t.Fatalf("acptest: SDK wrote a malformed error object: %q", line)
		}
	}
	return m
}

// validID reports whether raw is a JSON-RPC id: a string, an integer or null.
func validID(raw json.RawMessage) bool {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&v) != nil {
		return false
	}
	switch v := v.(type) {
	case nil, string:
		return true
	case json.Number:
		_, err := v.Int64()
		return err == nil
	}
	return false
}

// Expect returns the next frame and fails unless it is a request or
// notification for method.
func (p *Peer) Expect(method string) Message {
	p.t.Helper()
	m := p.Next()
	if m.Method != method {
		p.t.Fatalf("acptest: SDK sent %s, want %s", m, method)
	}
	return m
}

// ExpectCall returns the next frame and fails unless it is a request, with an
// id, for method.
func (p *Peer) ExpectCall(method string) Message {
	p.t.Helper()
	m := p.Expect(method)
	if !m.Has("id") {
		p.t.Fatalf("acptest: SDK sent %s as a notification, want a request", m)
	}
	return m
}

// Call sends a request and returns the next frame, which is usually its
// response.
func (p *Peer) Call(id any, method string, params any) Message {
	p.t.Helper()
	p.Request(id, method, params)
	return p.Next()
}

// CloseInput ends the SDK's input with a clean EOF. The SDK may keep writing,
// and the peer keeps reading.
func (p *Peer) CloseInput() { _ = p.toSDK.Close() }

// FailInput makes the SDK's next read fail with err.
func (p *Peer) FailInput(err error) { _ = p.toSDK.CloseWithError(err) }

// Drain reads frames until the SDK's output ends and returns them. It fails if
// the output does not end within Timeout.
func (p *Peer) Drain() []Message {
	p.t.Helper()
	var frames []Message
	for {
		m, err := p.TryNext(Timeout)
		switch {
		case err == nil:
			frames = append(frames, m)
		case errors.Is(err, io.EOF):
			return frames
		default:
			p.t.Fatalf("acptest: SDK output did not end: %v", err)
			return frames
		}
	}
}

// ExpectQuiet fails if the SDK writes a frame within d.
func (p *Peer) ExpectQuiet(d time.Duration) {
	p.t.Helper()
	if m, err := p.TryNext(d); err == nil {
		p.t.Fatalf("acptest: unexpected SDK frame %s", m)
	}
}
