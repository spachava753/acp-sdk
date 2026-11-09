// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

const (
	echoMethod   = "_test/echo"
	heldMethod   = "_test/held"
	cancelMethod = "$/cancel_request"
)

type echoParams struct {
	Message string `json:"message"`
}

type echoResult struct {
	Result string `json:"result"`
}

// echoMux answers echoMethod with "echo: " and the request's message.
func echoMux(t *testing.T) *acp.ExtensionMux {
	t.Helper()
	mux := acp.NewExtensionMux()
	if err := acp.AddExtensionRequest(mux, echoMethod, func(_ context.Context, p *echoParams) (*echoResult, error) {
		return &echoResult{Result: "echo: " + p.Message}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return mux
}

// callEcho starts an echo call from the SDK side.
func callEcho(peer acp.ExtensionCaller, ctx context.Context, message string) <-chan acptest.Result[json.RawMessage] {
	return acptest.Go(func() (json.RawMessage, error) {
		return peer.CallExtension(ctx, echoMethod, echoParams{Message: message})
	})
}

// expectEcho reads the next SDK frame and checks that it is an echo call
// carrying message.
func expectEcho(t *testing.T, peer *acptest.Peer, message string) acptest.Message {
	t.Helper()
	m := peer.ExpectCall(echoMethod)
	acptest.EqualMarshaled(t, echoParams{Message: message}, string(m.Params))
	return m
}

// replyEcho answers an echo call the way an echo agent would.
func replyEcho(peer *acptest.Peer, m acptest.Message, message string) {
	peer.Reply(m.ID, echoResult{Result: "echo: " + message})
}

// checkEcho checks that an echo call succeeded with the echo of message.
func checkEcho(t *testing.T, r acptest.Result[json.RawMessage], message string) {
	t.Helper()
	if r.Err != nil {
		t.Errorf("echo %q failed: %v", message, r.Err)
		return
	}
	acptest.EqualMarshaled(t, echoResult{Result: "echo: " + message}, string(r.Value))
}

// roundTrip completes an echo call through client, showing the connection is
// still usable, and returns the call as the peer saw it.
func roundTrip(t *testing.T, client acp.ExtensionCaller, peer *acptest.Peer, message string) acptest.Message {
	t.Helper()
	result := callEcho(client, context.Background(), message)
	m := expectEcho(t, peer, message)
	replyEcho(peer, m, message)
	checkEcho(t, acptest.Await(t, result), message)
	return m
}

// expectCancel checks that, once the bubble has settled, the SDK has already
// written a cancellation notification for the request with the given wire ID.
func expectCancel(t *testing.T, peer *acptest.Peer, id json.RawMessage) {
	t.Helper()
	m := nextNow(t, peer)
	if m.Method != cancelMethod || !m.IsNotification() {
		t.Fatalf("cancellation is not a notification: %s", m)
	}
	acptest.EqualJSON(t, m.Params, fmt.Sprintf(`{"requestId":%s}`, id))
}

// expectNoFrames lets every goroutine in the bubble settle, then fails if the
// SDK wrote anything the test has not read.
func expectNoFrames(peer *acptest.Peer) {
	synctest.Wait()
	peer.ExpectQuiet(0)
}

// nextNow lets every goroutine in the bubble settle and returns the next frame
// the SDK has written, failing if there is none. Unlike Peer.Next it does not
// wait on the fake clock, so a frame the SDK sends only after a timer fails.
func nextNow(t *testing.T, peer *acptest.Peer) acptest.Message {
	t.Helper()
	synctest.Wait()
	m, err := peer.TryNext(0)
	if err != nil {
		t.Fatalf("no SDK frame once settled: %v", err)
	}
	return m
}

// drainNow lets every goroutine in the bubble settle and returns the frames
// the SDK has written, failing unless its output has already ended. It shows
// the connection was closed by the time the operation under test returned.
func drainNow(t *testing.T, peer *acptest.Peer) []acptest.Message {
	t.Helper()
	synctest.Wait()
	var frames []acptest.Message
	for {
		m, err := peer.TryNext(0)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Errorf("SDK output has not ended once settled: %v", err)
			}
			return frames
		}
		frames = append(frames, m)
	}
}

// settledFrames lets every goroutine in the bubble settle and returns the
// frames the SDK has written but the test has not read.
func settledFrames(peer *acptest.Peer) []acptest.Message {
	synctest.Wait()
	var frames []acptest.Message
	for {
		m, err := peer.TryNext(0)
		if err != nil {
			return frames
		}
		frames = append(frames, m)
	}
}

// poll returns a value from ch if one is ready, without waiting.
func poll[T any](ch <-chan T) (T, bool) {
	select {
	case v := <-ch:
		return v, true
	default:
		var zero T
		return zero, false
	}
}

// serve runs RunAgent with ctx and handler in the background and returns the
// agent's connection once RunAgent has built it. Unlike acptest.StartAgent, it
// lets the test choose the serving context and check RunAgent's result without
// waiting. The channel is buffered so RunAgent can return after the test stops
// listening.
func serve(t *testing.T, ctx context.Context, transport acp.Transport, handler any) (*acp.AgentConnection, <-chan error) {
	t.Helper()
	conns := make(chan *acp.AgentConnection, 1)
	result := make(chan error, 1)
	go func() {
		result <- acp.RunAgent(ctx, transport, func(conn *acp.AgentConnection) any {
			conns <- conn
			return handler
		})
	}()
	return acptest.Await(t, conns), result
}

// heldCall is one call to a held handler.
type heldCall struct {
	tag string
	ctx context.Context
}

// held serves heldMethod with calls that stay pending. A cooperative handler
// returns once its context ends; any handler returns when the test releases
// it. Each finished call's tag is sent on finished.
type held struct {
	started  chan heldCall
	finished chan string
	release  chan struct{}
	once     sync.Once
}

func addHeld(t *testing.T, mux *acp.ExtensionMux, cooperative bool) *held {
	t.Helper()
	h := &held{started: make(chan heldCall, 8), finished: make(chan string, 8), release: make(chan struct{})}
	if err := acp.AddExtensionRequest(mux, heldMethod, func(ctx context.Context, p *struct {
		Tag string `json:"tag"`
	}) (*struct{}, error) {
		h.started <- heldCall{p.Tag, ctx}
		defer func() { h.finished <- p.Tag }()
		if cooperative {
			select {
			case <-ctx.Done():
			case <-h.release:
			}
		} else {
			<-h.release
		}
		return &struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return h
}

// unblock releases every held call. Tests defer it so a failing assertion
// cannot leave a handler blocked; it is never the event a check relies on.
func (h *held) unblock() { h.once.Do(func() { close(h.release) }) }

// gatedTransport wraps a transport so every write waits for release, holding
// accepted output in flight while the test drives the rest of the connection.
// It also records the error that ends reading. Closing the connection aborts
// a waiting write, and with the gate open the wrapper is simply a connection
// the test can close in both directions.
type gatedTransport struct {
	acp.Transport
	entered  chan struct{} // closed when the first write starts
	release  chan struct{} // writes proceed once closed
	readDone chan struct{} // closed when Read first returns an error
	readErr  error         // that error; read only after readDone is closed

	conn                                     acp.Connection
	closed                                   chan struct{}
	enterOnce, readOnce, closeOnce, openOnce sync.Once
}

func newGate(inner acp.Transport) *gatedTransport {
	return &gatedTransport{
		Transport: inner,
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
		readDone:  make(chan struct{}),
		closed:    make(chan struct{}),
	}
}

func (g *gatedTransport) Connect(ctx context.Context) (acp.Connection, error) {
	conn, err := g.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	g.conn = conn
	return gatedConn{g}, nil
}

// open lets writes proceed.
func (g *gatedTransport) open() { g.openOnce.Do(func() { close(g.release) }) }

// abort closes the wrapped connection in both directions.
func (g *gatedTransport) abort() { _ = gatedConn{g}.Close() }

type gatedConn struct{ g *gatedTransport }

func (c gatedConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	m, err := c.g.conn.Read(ctx)
	if err != nil {
		c.g.readOnce.Do(func() {
			c.g.readErr = err
			close(c.g.readDone)
		})
	}
	return m, err
}

func (c gatedConn) Write(ctx context.Context, m jsonrpc.Message) error {
	c.g.enterOnce.Do(func() { close(c.g.entered) })
	select {
	case <-c.g.release:
	case <-c.g.closed:
		return io.ErrClosedPipe
	case <-ctx.Done():
		return ctx.Err()
	}
	return c.g.conn.Write(ctx, m)
}

func (c gatedConn) Close() error {
	c.g.closeOnce.Do(func() { close(c.g.closed) })
	if c.g.conn == nil {
		return nil
	}
	return c.g.conn.Close()
}

// wrappedTransport hides the concrete type of the transport it wraps.
type wrappedTransport struct{ acp.Transport }

// watchedWriter records when its first Write starts and returns. It forwards
// write deadlines when the underlying writer supports them, so the SDK can
// interrupt it the same way it would interrupt the underlying writer.
type watchedWriter struct {
	io.WriteCloser
	entered, exited chan struct{}
	once            sync.Once
}

func watch(w io.WriteCloser) *watchedWriter {
	return &watchedWriter{WriteCloser: w, entered: make(chan struct{}), exited: make(chan struct{})}
}

func (w *watchedWriter) Write(data []byte) (int, error) {
	first := false
	w.once.Do(func() { first = true; close(w.entered) })
	if first {
		defer close(w.exited)
	}
	return w.WriteCloser.Write(data)
}

func (w *watchedWriter) SetWriteDeadline(deadline time.Time) error {
	if d, ok := w.WriteCloser.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return d.SetWriteDeadline(deadline)
	}
	return os.ErrNoDeadline
}
