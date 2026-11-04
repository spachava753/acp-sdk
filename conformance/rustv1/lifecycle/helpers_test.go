// Copyright 2026 ACP Go SDK contributors.
// Semantic ports of agentclientprotocol/rust-sdk (Apache-2.0), revision
// 050cd94c081056ace37a7b89abfed7f6220b47eb. See mapping.json for provenance.
package lifecycle_test

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

const (
	probeMethod = "_rust/lifecycle"
	// The Rust optional request-cancellation profile uses Error::request_cancelled.
	codeRequestCancelled = -32800
)

type readEvent struct {
	message jsonrpc.Message
	err     error
}

// frameTransport is the public Go Transport/Connection counterpart of Rust's
// raw Channel peer. EOF on input does not close its independent output half.
// It contains no dispatch, cancellation, pending-call, or shutdown policy.
// All channels belong to the enclosing synctest bubble.
type frameTransport struct {
	input  chan readEvent
	output chan jsonrpc.Message
	closed chan struct{}
	once   sync.Once
}

func newFrames() *frameTransport {
	return &frameTransport{
		input: make(chan readEvent, 64), output: make(chan jsonrpc.Message, 64), closed: make(chan struct{}),
	}
}

func (p *frameTransport) Connect(context.Context) (acp.Connection, error) { return p, nil }

func (p *frameTransport) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case event := <-p.input:
		return event.message, event.err
	case <-p.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *frameTransport) Write(ctx context.Context, message jsonrpc.Message) error {
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

func (p *frameTransport) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func (p *frameTransport) push(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	message, err := jsonrpc.DecodeMessage(data)
	if err != nil {
		t.Fatalf("test peer could not encode its valid frame: %v", err)
	}
	p.input <- readEvent{message: message}
}

func (p *frameTransport) endInput(err error) { p.input <- readEvent{err: err} }

func (p *frameTransport) next(t *testing.T) jsonrpc.Message {
	t.Helper()
	return receive(t, p.output)
}

func (p *frameTransport) response(t *testing.T, id any) *jsonrpc.Response {
	t.Helper()
	message := p.next(t)
	response, ok := message.(*jsonrpc.Response)
	if !ok {
		t.Fatalf("expected response for %v, got %#v", id, message)
	}
	if response.ID.Raw() != id {
		t.Fatalf("response id = %#v, want %#v", response.ID.Raw(), id)
	}
	return response
}

func (p *frameTransport) noOutput(t *testing.T) {
	t.Helper()
	// Call only after synctest.Wait: dispatch and response writing must have
	// settled before asserting that no duplicate/cancellation was produced.
	select {
	case message := <-p.output:
		t.Errorf("unexpected extra frame: %#v", message)
	default:
	}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		// Fake time inside synctest; also makes broken behavior fail instead of
		// leaving an unbounded request/response wait in the test.
		t.Fatal("public operation did not complete")
		var zero T
		return zero
	}
}

func request(id any, method string, params any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
}

func notification(method string, params any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
}

func reply(id jsonrpc.ID, value any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id.Raw(), "result": value}
}

type callResult struct {
	value json.RawMessage
	err   error
}

func call(peer acp.ExtensionCaller, ctx context.Context, params any) <-chan callResult {
	done := make(chan callResult, 1)
	go func() {
		value, err := peer.CallExtension(ctx, probeMethod, params)
		done <- callResult{value, err}
	}()
	return done
}

func outgoingRequest(t *testing.T, p *frameTransport) *jsonrpc.Request {
	t.Helper()
	message := p.next(t)
	req, ok := message.(*jsonrpc.Request)
	if !ok || !req.IsCall() || req.Method != probeMethod {
		t.Fatalf("expected outgoing %s request, got %#v", probeMethod, message)
	}
	return req
}

// Rust's SimpleRequest/Response echo peers derive their result from the
// received message. Validate both sides here so a fixed raw-peer reply cannot
// conceal lost parameters or a wrongly routed result. Compare typed payloads,
// not JSON whitespace (the Rust oracle asserts SimpleResponse.result).
func requireOutgoingMessage(t *testing.T, req *jsonrpc.Request, want string) {
	t.Helper()
	var params struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Message != want {
		t.Errorf("outgoing message = %s, %v; want %q", req.Params, err, want)
	}
}

func requireEcho(t *testing.T, result callResult, want string) {
	t.Helper()
	if result.err != nil {
		t.Errorf("echo request failed: %v", result.err)
		return
	}
	var response struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(result.value, &response); err != nil || response.Result != want {
		t.Errorf("echo response = %s, %v; want %q", result.value, err, want)
	}
}

func client(t *testing.T, p *frameTransport, handler any) *acp.Client {
	t.Helper()
	c, err := acp.Connect(context.Background(), p, handler)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func cleanupClient(p *frameTransport, c *acp.Client) {
	// Never rely on graceful Close to release a missing reply. Retain and
	// close the raw transport first, then join the public Close operation.
	_ = p.Close()
	done := make(chan struct{})
	go func() { _ = c.Close(); close(done) }()
	synctest.Wait()
	// Wait alone permits a durably blocked goroutine. Ensure cleanup actually
	// joined Close, rather than leaving one behind until the bubble exits.
	select {
	case <-done:
	default:
		panic("public Close did not join after the owned raw transport was closed")
	}
}

func startAgent(t *testing.T, p acp.Transport, handler any) (*acp.AgentConnection, <-chan error) {
	t.Helper()
	ready := make(chan *acp.AgentConnection, 1)
	done := make(chan error, 1)
	go func() {
		done <- acp.RunAgent(context.Background(), p, func(c *acp.AgentConnection) any {
			ready <- c
			return handler
		})
	}()
	return receive(t, ready), done
}

func echoMux(t *testing.T) *acp.ExtensionMux {
	t.Helper()
	mux := acp.NewExtensionMux()
	if err := acp.AddExtensionRequest(mux, probeMethod, func(_ context.Context, p *struct {
		Message string `json:"message"`
	}) (*struct {
		Result string `json:"result"`
	}, error) {
		return &struct {
			Result string `json:"result"`
		}{Result: "echo: " + p.Message}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return mux
}

func success(t *testing.T, response *jsonrpc.Response, want string) {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("unexpected error response: %v", response.Error)
	}
	var got any
	if err := json.Unmarshal(response.Result, &got); err != nil {
		t.Fatal(err)
	}
	var expected any
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(got)
	expect, _ := json.Marshal(expected)
	if string(actual) != string(expect) {
		t.Errorf("result = %s, want %s", actual, expect)
	}
}
