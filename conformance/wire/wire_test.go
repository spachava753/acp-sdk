// Package wire_test checks the SDK's JSON-RPC wire behavior: how messages,
// request IDs and errors are encoded and decoded, how malformed or unsupported
// frames are answered, and how requests and notifications reach handlers on
// both the agent and client sides. Most tests drive an SDK endpoint from an
// independent NDJSON peer, so assertions are made on the frames the SDK
// actually writes; a few connect an SDK client to an SDK agent to check both
// ends together, and some exercise the public jsonrpc message helpers
// directly.
package wire_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// customCode is an application-defined error code outside the JSON-RPC and
// ACP ranges.
const customCode = 12345

// unnamedReservedCode lies in the JSON-RPC server-error range but has no
// protocol version 1 meaning, so the SDK must treat it as opaque.
const unnamedReservedCode = -32022

const (
	newParams    = `{"cwd":"/workspace","mcpServers":[]}`
	cancelParams = `{"sessionId":"session"}`
	readParams   = `{"sessionId":"session","path":"/workspace/example.txt"}`
	updateParams = `{"sessionId":"session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"text"}}}`
)

type message struct {
	Message string `json:"message"`
}

type answer struct {
	Result string `json:"result"`
}

// agent is a minimal ACP agent with extension routing. It records session/new
// calls and session/cancel deliveries so tests can check which handlers ran.
type agent struct {
	*acp.ExtensionMux
	conn *acp.AgentConnection
	// newSession, when set, replaces the default session/new behavior.
	newSession func(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error)
	newCalls   atomic.Int32
	// cancels is buffered so the handler never blocks; tests read it once
	// dispatch has settled.
	cancels chan acp.SessionId
}

func newAgent() *agent {
	return &agent{ExtensionMux: acp.NewExtensionMux(), cancels: make(chan acp.SessionId, 4)}
}

func (*agent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: 1, AgentCapabilities: &acp.AgentCapabilities{}}, nil
}

func (a *agent) SessionNew(ctx context.Context, p *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	a.newCalls.Add(1)
	if a.newSession != nil {
		return a.newSession(ctx, p)
	}
	return &acp.NewSessionResponse{SessionID: "session"}, nil
}

func (a *agent) SessionCancel(_ context.Context, p *acp.CancelNotification) error {
	a.cancels <- p.SessionID
	return nil
}

// echo registers an extension request that answers "echo: <message>".
func (a *agent) echo(t *testing.T, method string) {
	t.Helper()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, method, func(_ context.Context, m *message) (*answer, error) {
		return &answer{Result: "echo: " + m.Message}, nil
	}))
}

// serve runs a as an SDK agent connected to a new peer, without initializing
// it.
func serve(t *testing.T, a *agent) (*acptest.Agent, *acptest.Peer) {
	t.Helper()
	return acptest.StartAgentFunc(t, func(conn *acp.AgentConnection) any {
		a.conn = conn
		return a
	})
}

// startAgent serves a and completes initialize at protocol version 1.
func startAgent(t *testing.T, a *agent) (*acptest.Agent, *acptest.Peer) {
	t.Helper()
	ag, peer := serve(t, a)
	peer.InitializeAgent(nil)
	return ag, peer
}

// finish ends the peer's input and waits for RunAgent to return, which also
// waits for every accepted handler. It returns the frames written after the
// last one the test read, and RunAgent's error.
func finish(t *testing.T, ag *acptest.Agent, peer *acptest.Peer) ([]acptest.Message, error) {
	t.Helper()
	peer.CloseInput()
	err := ag.Wait(t)
	return peer.Drain(), err
}

// finishClean is finish for scenarios where the agent must also stop cleanly
// and write nothing further.
func finishClean(t *testing.T, ag *acptest.Agent, peer *acptest.Peer) {
	t.Helper()
	rest, err := finish(t, ag, peer)
	if err != nil {
		t.Errorf("RunAgent after input EOF: %v", err)
	}
	if len(rest) != 0 {
		t.Errorf("unexpected frames before EOF: %v", rest)
	}
}

// startClient connects an SDK client with handler to a new peer and
// initializes it with file-read capability.
func startClient(t *testing.T, handler any) (*acp.Client, *acptest.Peer) {
	t.Helper()
	client, peer := acptest.StartClient(t, handler)
	acptest.InitializeClient(t, client, peer, &acp.ClientCapabilities{Fs: &acp.FileSystemCapabilities{ReadTextFile: true}}, nil)
	return client, peer
}

// closeClient ends the peer's input and closes the client, which waits for
// its notification handlers. It returns any frames written after the last one
// the test read.
func closeClient(t *testing.T, client *acp.Client, peer *acptest.Peer) []acptest.Message {
	t.Helper()
	peer.CloseInput()
	if err := acptest.Await(t, acptest.Go(func() (struct{}, error) { return struct{}{}, client.Close() })).Err; err != nil {
		t.Errorf("closing client: %v", err)
	}
	return peer.Drain()
}

// connect serves a as an SDK agent and connects an initialized SDK client to
// it, so a test exercises both ends of the SDK. The returned stop function
// closes the client and waits for RunAgent, after which every accepted
// handler on both sides has finished. Cleanup calls it too.
func connect(t *testing.T, a *agent) (*acp.Client, func() error) {
	t.Helper()
	agentIn, clientOut := io.Pipe()
	clientIn, agentOut := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- acp.RunAgent(context.Background(), &acp.IOTransport{Reader: agentIn, Writer: agentOut}, func(conn *acp.AgentConnection) any {
			a.conn = conn
			return a
		})
	}()
	client, err := acp.Connect(context.Background(), &acp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	check(t, err)
	stop := sync.OnceValue(func() error {
		closed := client.Close()
		select {
		case err := <-done:
			return errors.Join(closed, err)
		case <-time.After(acptest.Timeout):
			return errors.New("RunAgent did not return after the client closed")
		}
	})
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Error(err)
		}
		for _, c := range []io.Closer{agentIn, clientOut, clientIn, agentOut} {
			_ = c.Close()
		}
	})
	acptest.Value(t, acptest.Go(func() (*acp.InitializeResponse, error) {
		return client.Initialize(context.Background(), &acp.InitializeRequest{ProtocolVersion: 1})
	}))
	return client, stop
}

// callExtension calls method from the client and waits for the outcome.
func callExtension(t *testing.T, client *acp.Client, method string, params any) (json.RawMessage, error) {
	t.Helper()
	r := acptest.Await(t, acptest.Go(func() (json.RawMessage, error) {
		return client.CallExtension(context.Background(), method, params)
	}))
	return r.Value, r.Err
}

// raw returns the JSON text s for use as an exact id or params token.
func raw(s string) json.RawMessage { return json.RawMessage(s) }

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// wantError fails unless m is an error response with code and no result.
func wantError(t *testing.T, m acptest.Message, code int64) {
	t.Helper()
	if m.Error == nil {
		t.Fatalf("frame %s is not an error response, want code %d", m, code)
	}
	if m.Error.Code != code {
		t.Errorf("error code = %d, want %d; frame %s", m.Error.Code, code, m)
	}
	if m.Has("result") {
		t.Errorf("error response also has a result: %s", m)
	}
}

// wantCode fails unless err carries a structured JSON-RPC error with code.
func wantCode(t *testing.T, err error, code int64) {
	t.Helper()
	var e *jsonrpc.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Errorf("error = %v, want structured code %d", err, code)
	}
}

// exactInt returns the integer value of a JSON number token, failing if the
// token is a string or not an exact int64. It compares values without float64
// rounding and without depending on one number spelling.
func exactInt(t *testing.T, token []byte) string {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(token))
	d.UseNumber()
	var v any
	check(t, d.Decode(&v))
	n, ok := v.(json.Number)
	if !ok {
		t.Errorf("id %s is not a JSON number", token)
		return ""
	}
	r, ok := new(big.Rat).SetString(n.String())
	if !ok || !r.IsInt() || !r.Num().IsInt64() {
		t.Errorf("id %s is not an exact int64", token)
		return ""
	}
	return r.Num().String()
}
