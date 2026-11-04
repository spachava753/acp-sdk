package wire_test

import (
	"context"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

type helloRequest struct {
	Name string `json:"name"`
}
type helloResponse struct {
	Greeting string `json:"greeting"`
}

// Rust src/agent-client-protocol/tests/derive_macros.rs::test_jr_request_parse_message, observable parsing and
// wrong-method branches. Rust's standalone matches_method getters have no Go
// counterpart; the mapping records that portion separately.
func TestTypedRequestParsing(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/hello", func(_ context.Context, p *helloRequest) (*helloResponse, error) {
		return &helloResponse{Greeting: p.Name}, nil
	}))
	p := agentPeer(t, a, true)
	r := p.call(t, "1", "_test/hello", `{"name":"test"}`)
	equalJSON(t, r["result"], `{"greeting":"test"}`)
	wrong := p.call(t, "2", "_test/wrong", `{"name":"test"}`)
	code(t, wrong, jsonrpc.CodeMethodNotFound)
}

// Rust src/agent-client-protocol/tests/derive_macros.rs::test_jr_notification_parse_message. Observe the typed
// timestamp after normal dispatch; check absence of a second handler invocation
// only after RunAgent has drained. No procedural macro is recreated in Go.
func TestTypedNotificationParsing(t *testing.T) {
	a := newAgent()
	timestamps := make(chan uint64, 2)
	t.Cleanup(func() {
		if len(timestamps) != 1 {
			t.Errorf("typed notifications = %d, want 1", len(timestamps))
			return
		}
		if got := <-timestamps; got != 99999 {
			t.Errorf("timestamp = %d, want 99999", got)
		}
	})
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/ping", func(_ context.Context, p *struct {
		Timestamp uint64 `json:"timestamp"`
	}) error {
		timestamps <- p.Timestamp
		return nil
	}))
	echoHandler(t, a, "_test/barrier")
	p := agentPeer(t, a, true)
	p.send(t, `{"jsonrpc":"2.0","method":"_test/ping","params":{"timestamp":99999}}`)
	// The original standalone matcher rejects this method. At Go's public
	// registration boundary it must not dispatch to the typed ping handler.
	p.send(t, `{"jsonrpc":"2.0","method":"_test/wrong","params":{"timestamp":99999}}`)
	r := p.call(t, "1", "_test/barrier", `{"message":"done"}`)
	equalJSON(t, r["id"], `1`)
}

// Rust src/agent-client-protocol/tests/derive_macros.rs::test_jr_response_payload_into_json.
func TestTypedResponseEncoding(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/hello", func(context.Context, *helloRequest) (*helloResponse, error) {
		return &helloResponse{Greeting: "Hello, world!"}, nil
	}))
	p := agentPeer(t, a, true)
	r := p.call(t, "1", "_test/hello", `{"name":"world"}`)
	equalJSON(t, r["result"], `{"greeting":"Hello, world!"}`)
}

// Rust src/agent-client-protocol/tests/derive_macros.rs::test_jr_response_payload_from_value. Use the SDK's
// public typed result decoder, not a direct test of encoding/json.
func TestTypedResponseDecoding(t *testing.T) {
	c, p := clientPeer(t)
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	done := make(chan callResult[*helloResponse], 1)
	go func() {
		r, err := acp.CallExtension[helloResponse](ctx, c, "_test/hello", helloRequest{Name: "test"})
		done <- callResult[*helloResponse]{r, err}
	}()
	r := p.receive(t)
	p.send(t, `{"jsonrpc":"2.0","id":`+string(r["id"])+`,"result":{"greeting":"Hi there!"}}`)
	select {
	case got := <-done:
		check(t, got.err)
		if got.value == nil || got.value.Greeting != "Hi there!" {
			t.Errorf("decoded response = %#v", got.value)
		}
	case <-time.After(wait):
		t.Fatal("typed result was not decoded")
	}
}
