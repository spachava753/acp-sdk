// Semantic ports of agentclientprotocol/rust-sdk tests at
// 050cd94c081056ace37a7b89abfed7f6220b47eb (Apache-2.0).
package wire_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

const wait = 2 * time.Second
const customCode = 12345

// Included as an opaque v1 code by Rust src/agent-client-protocol/src/error.rs::sample_codes(), even though
// its named UnsupportedProtocolVersion meaning belongs to v2.
const opaqueUnsupportedVersionCode = -32022

type message struct {
	Message string `json:"message"`
}
type answer struct {
	Result string `json:"result"`
}
type agent struct {
	*acp.ExtensionMux
	conn    *acp.AgentConnection
	cancels chan string
}

func (a *agent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: 1, AgentCapabilities: &acp.AgentCapabilities{}}, nil
}
func (a *agent) SessionNew(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: "session"}, nil
}

func (a *agent) SessionCancel(_ context.Context, p *acp.CancelNotification) error {
	if a.cancels != nil {
		a.cancels <- string(p.SessionID)
	}
	return nil
}

func newAgent() *agent { return &agent{ExtensionMux: acp.NewExtensionMux()} }
func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// SDK panics happen after the application's handler has returned, on an SDK
// goroutine. A child test process contains them without changing runtime policy.
func isolated(t *testing.T) bool {
	t.Helper()
	if os.Getenv("ACP_RUST_WIRE_CHILD") == t.Name() {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*wait)
	defer cancel()
	parts := strings.Split(t.Name(), "/")
	for i := range parts {
		parts[i] = "^" + regexp.QuoteMeta(parts[i]) + "$"
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+strings.Join(parts, "/"), "-test.count=1", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), "ACP_RUST_WIRE_CHILD="+t.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("upstream semantic expectation failed in isolated process: %v\n%s", err, out)
	}
	return false
}

type agentResult struct {
	done chan struct{}
	err  error
}

type peer struct {
	conn  net.Conn
	read  *bufio.Reader
	agent *agentResult
}

// finish matches the upstream WirePeer.finish: close peer input, then require
// the serving task to finish cleanly. The result is cached so cleanup can also
// observe completion without consuming the result a second time.
func (p *peer) finish(t *testing.T) {
	t.Helper()
	if p.agent == nil {
		t.Fatal("finish requires an agent peer")
	}
	_ = p.conn.Close()
	select {
	case <-p.agent.done:
		check(t, p.agent.err)
	case <-time.After(wait):
		t.Fatal("RunAgent did not finish after peer EOF")
	}
}

func agentPeer(t *testing.T, a *agent, initialize bool) *peer {
	t.Helper()
	sdk, raw := net.Pipe()
	result := &agentResult{done: make(chan struct{})}
	go func() {
		result.err = acp.RunAgent(context.Background(), &acp.IOTransport{Reader: sdk, Writer: sdk}, func(c *acp.AgentConnection) any {
			if a == nil {
				return struct{}{} // No handlers, like the upstream raw-agent control.
			}
			a.conn = c
			return a
		})
		close(result.done)
	}()
	// Retain both actual stream closers. Public Close may wait for pending work;
	// closing the streams first also unblocks cleanup after a failed assertion.
	t.Cleanup(func() {
		_ = raw.Close()
		select {
		case <-result.done:
			// On an already-failed scenario this is only force-close cleanup.
			if result.err != nil && !t.Failed() {
				t.Errorf("RunAgent returned an error after peer EOF: %v", result.err)
			}
		case <-time.After(wait):
			t.Error("RunAgent did not finish after raw transport closure")
		}
		_ = sdk.Close()
	})
	p := &peer{conn: raw, read: bufio.NewReader(raw), agent: result}
	if initialize {
		p.send(t, `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":1}}`)
		r := p.receive(t)
		if string(r["id"]) != `0` || len(r["result"]) == 0 {
			t.Fatalf("initialize response: %v", r)
		}
	}
	return p
}

func clientPeer(t *testing.T) (*acp.Client, *peer) {
	t.Helper()
	return clientPeerWithHandler(t, nil)
}

func clientPeerWithHandler(t *testing.T, handler any) (*acp.Client, *peer) {
	t.Helper()
	sdk, raw := net.Pipe()
	client, err := acp.Connect(context.Background(), &acp.IOTransport{Reader: sdk, Writer: sdk}, handler)
	check(t, err)
	t.Cleanup(func() {
		_ = raw.Close()
		_ = sdk.Close()
		done := make(chan error, 1)
		go func() { done <- client.Close() }()
		select {
		case <-done:
		case <-time.After(wait):
			t.Error("Client.Close did not finish after raw transport closure")
		}
	})
	p := &peer{conn: raw, read: bufio.NewReader(raw)}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	initialized := make(chan error, 1)
	go func() {
		_, err := client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1})
		initialized <- err
	}()
	r := p.receive(t)
	p.send(t, `{"jsonrpc":"2.0","id":`+string(r["id"])+`,"result":{"protocolVersion":1,"agentCapabilities":{}}}`)
	select {
	case err := <-initialized:
		check(t, err)
	case <-ctx.Done():
		t.Fatal("client initialization did not complete")
	}
	return client, p
}

func (p *peer) send(t *testing.T, line string) {
	t.Helper()
	check(t, p.conn.SetWriteDeadline(time.Now().Add(wait)))
	_, err := fmt.Fprintln(p.conn, line)
	check(t, err)
}
func (p *peer) receive(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	check(t, p.conn.SetReadDeadline(time.Now().Add(wait)))
	line, err := p.read.ReadBytes('\n')
	if err != nil {
		t.Fatalf("reading JSON-RPC response: %v; partial=%q", err, line)
	}
	var m map[string]json.RawMessage
	check(t, json.Unmarshal(line, &m))
	return m
}
func request(id, method, params string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":%q,"params":%s}`, id, method, params)
}
func (p *peer) call(t *testing.T, id, method, params string) map[string]json.RawMessage {
	t.Helper()
	p.send(t, request(id, method, params))
	return p.receive(t)
}
func equalJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	decode := func(b []byte) any {
		if !json.Valid(b) {
			t.Fatalf("not one complete JSON value: %q", b)
		}
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber() // An independent exact-number oracle, not SDK decoding.
		var v any
		check(t, d.Decode(&v))
		return v
	}
	if !reflect.DeepEqual(decode(got), decode([]byte(want))) {
		t.Errorf("JSON value = %s, want %s", got, want)
	}
}
func code(t *testing.T, r map[string]json.RawMessage, want int64) {
	t.Helper()
	var e jsonrpc.Error
	if len(r["error"]) == 0 {
		t.Fatalf("expected error code %d, got envelope %v", want, r)
	}
	check(t, json.Unmarshal(r["error"], &e))
	if e.Code != want {
		t.Errorf("error code = %d, want %d; envelope=%v", e.Code, want, r)
	}
	if _, ok := r["result"]; ok {
		t.Errorf("error response also contains result: %v", r)
	}
}
func echoHandler(t *testing.T, a *agent, method string) {
	t.Helper()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, method, func(_ context.Context, m *message) (*answer, error) {
		return &answer{Result: "echo: " + m.Message}, nil
	}))
}
