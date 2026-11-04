// Local Go SDK regressions, added after the upstream-derived Rust test stage.
// These tests express Go API/lifetime policies, not new ACP wire requirements.
package lifecycle_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

const operationLimit = time.Second

type errTask struct {
	done chan struct{}
	err  error // Read only after done closes.
}

func start(fn func() error) *errTask {
	op := &errTask{done: make(chan struct{})}
	go func() {
		op.err = fn()
		close(op.done)
	}()
	return op
}

func (op *errTask) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-op.done:
		return op.err
	case <-time.After(operationLimit):
		t.Fatal("owned operation did not finish")
		return nil
	}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(operationLimit):
		t.Fatal("test peer did not make progress")
		var zero T
		return zero
	}
}

type readEvent struct {
	message jsonrpc.Message
	err     error
}

// messagePeer supplies only the public transport boundary. It does not retire
// calls, parse cancellations, dispatch methods, or implement SDK shutdown.
// Input failure and full Close are independent so tests can distinguish fatal
// read errors from clean input half-close (which is NOT asserted to cancel work).
type messagePeer struct {
	input  chan readEvent
	output chan jsonrpc.Message
	closed chan struct{}
	once   sync.Once
}

func newMessagePeer() *messagePeer {
	return &messagePeer{make(chan readEvent, 16), make(chan jsonrpc.Message, 16), make(chan struct{}), sync.Once{}}
}

func (p *messagePeer) Connect(context.Context) (acp.Connection, error) { return p, nil }

func (p *messagePeer) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case event := <-p.input:
		return event.message, event.err
	case <-p.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *messagePeer) Write(ctx context.Context, message jsonrpc.Message) error {
	select {
	case <-p.closed:
		return io.EOF
	default:
	}
	select {
	case p.output <- message:
		return nil
	case <-p.closed:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *messagePeer) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func (p *messagePeer) push(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	message, err := jsonrpc.DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	p.input <- readEvent{message: message}
}

func (p *messagePeer) nextCall(t *testing.T, method string) *jsonrpc.Request {
	t.Helper()
	for {
		message := receive(t, p.output)
		req, ok := message.(*jsonrpc.Request)
		if ok && !req.IsCall() && req.Method == "$/cancel_request" {
			// Optional protocol cancellation is independent of local cleanup.
			// This peer deliberately ignores it and NEVER answers the old ID.
			continue
		}
		if !ok || !req.IsCall() || req.Method != method {
			t.Fatalf("expected %s call, got %#v", method, message)
		}
		return req
	}
}

type fixedTransport struct{ acp.Connection }

func (t fixedTransport) Connect(context.Context) (acp.Connection, error) { return t.Connection, nil }

// observedWriter records entry/exit of the FIRST real net.Pipe.Write only.
// It does not create artificial backpressure or implement cancellation.
// net.Conn itself supplies close/deadline interruption and safe cleanup.
type observedWriter struct {
	net.Conn
	entered chan struct{}
	exited  chan struct{}
	once    sync.Once
}

func (w *observedWriter) Write(data []byte) (int, error) {
	first := false
	w.once.Do(func() { first = true; close(w.entered) })
	if first {
		defer close(w.exited)
	}
	return w.Conn.Write(data)
}

type streamFixture struct {
	local, peer net.Conn
	writer      *observedWriter
	raw         acp.Connection
	client      *acp.Client
	tasks       []*errTask
	closing     *errTask
}

func newStream(t *testing.T, withClient bool) *streamFixture {
	t.Helper()
	local, peer := net.Pipe()
	f := &streamFixture{local: local, peer: peer, writer: &observedWriter{Conn: local, entered: make(chan struct{}), exited: make(chan struct{})}}
	raw, err := (&acp.IOTransport{Reader: local, Writer: f.writer}).Connect(context.Background())
	if err != nil {
		_ = local.Close()
		_ = peer.Close()
		t.Fatal(err)
	}
	f.raw = raw
	if withClient {
		f.client, err = acp.Connect(context.Background(), fixedTransport{raw}, nil)
		if err != nil {
			_ = raw.Close()
			_ = peer.Close()
			t.Fatal(err)
		}
	}
	return f
}

func (f *streamFixture) launch(fn func() error) *errTask {
	op := start(fn)
	f.tasks = append(f.tasks, op)
	return op
}

func (f *streamFixture) cleanup(t *testing.T) {
	t.Helper()
	// Independent failure-safe cleanup, AFTER assertions. Never rely on a
	// graceful SDK Close to interrupt accepted background-context writes.
	_ = f.local.Close()
	_ = f.peer.Close()
	_ = f.raw.Close()
	if f.client != nil && f.closing == nil {
		f.closing = f.launch(f.client.Close)
	}
	for _, op := range f.tasks {
		select {
		case <-op.done:
		case <-time.After(operationLimit):
			t.Error("owned task did not join after independent stream cleanup")
		}
	}
}

func notify(method string) *jsonrpc.Request {
	return &jsonrpc.Request{Method: method, Params: []byte(`{}`)}
}
