package wire_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust src/agent-client-protocol/tests/request_id_wire_format.rs::assert_out_of_range_ids_are_rejected,
// invoked by the v1 round-trip test: all three cases require a null-ID error.
func TestInvalidRequestIDsWireError(t *testing.T) {
	p := requestIDPeer(t)
	assertInvalidRequestIDs(t, p)
	if !t.Failed() {
		p.finish(t)
	}
}

// Share the caller's existing peer for all three errors, including when this
// is the final phase of the complete echo/callback sequence.
func assertInvalidRequestIDs(t *testing.T, p *peer) {
	t.Helper()
	for _, id := range []string{"9223372036854775808", "-9223372036854775809", "1.5"} {
		t.Run(id, func(t *testing.T) {
			r := p.call(t, id, "_test/echo", `{}`)
			code(t, r, jsonrpc.CodeInvalidRequest)
			equalJSON(t, r["id"], `null`)
		})
	}
}

type bufferCloser struct{ bytes.Buffer }

func (*bufferCloser) Close() error { return nil }

// Rust src/agent-client-protocol/src/jsonrpc/raw_error.rs::raw_responses_survive_framing_and_channel_relay,
// standalone frame portion. The batch/malformed-frame relay API is absent in
// Go and remains an explicit disposition, not emulated by a fixture relay.
func TestRawResponseFraming(t *testing.T) {
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":"error","error":{"code":%d,"message":"peer","extension":{"retry":true}}}`, upstreamAuthRequiredCode)
	out := &bufferCloser{}
	transport := &acp.IOTransport{Reader: io.NopCloser(strings.NewReader(input + "\n")), Writer: out}
	conn, err := transport.Connect(context.Background())
	check(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	m, err := conn.Read(ctx)
	check(t, err)
	check(t, conn.Write(ctx, m))
	equalJSON(t, out.Bytes(), input)
	if bytes.Count(out.Bytes(), []byte{'\n'}) != 1 {
		t.Errorf("response was not one newline-delimited frame: %q", out.Bytes())
	}
}

// Rust src/agent-client-protocol/src/jsonrpc/raw_id.rs doctest@32: numeric 7 converts without becoming string "7",
// and the string form remains quoted on the wire.
func TestIDConversionExample(t *testing.T) {
	for _, id := range []any{float64(7), "7"} {
		rpcID, err := jsonrpc.MakeID(id)
		check(t, err)
		out, err := jsonrpc.EncodeMessage(&jsonrpc.Request{ID: rpcID, Method: "_test/example"})
		check(t, err)
		var wire map[string]json.RawMessage
		check(t, json.Unmarshal(out, &wire))
		want := `7`
		if _, ok := id.(string); ok {
			want = `"7"`
		}
		equalJSON(t, wire["id"], want)
	}
}

// Rust src/agent-client-protocol/src/concepts/error_handling.rs doctest@20: the application chooses an
// InvalidParams response for empty data and a normal response otherwise. The
// branch is upstream fixture behavior, not a fabricated SDK validation layer.
func TestExplicitValidationHandler(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/validate", func(_ context.Context, p *struct {
		Data string `json:"data"`
	}) (*struct {
		IsValid bool    `json:"is_valid"`
		Error   *string `json:"error"`
	}, error) {
		if p.Data == "" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Invalid params"}
		}
		return &struct {
			IsValid bool    `json:"is_valid"`
			Error   *string `json:"error"`
		}{IsValid: true}, nil
	}))
	p := agentPeer(t, a, true)
	bad := p.call(t, "1", "_test/validate", `{"data":""}`)
	code(t, bad, jsonrpc.CodeInvalidParams)
	good := p.call(t, "2", "_test/validate", `{"data":"ok"}`)
	equalJSON(t, good["result"], `{"is_valid":true,"error":null}`)
}

// Rust src/agent-client-protocol/src/jsonrpc.rs doctest@4252: send a one-way typed notification. Verify its
// wire form through an independent peer rather than merely observing a handler.
func TestOutgoingNotificationWire(t *testing.T) {
	c, p := clientPeer(t)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	go func() { done <- c.NotifyExtension(ctx, "_test/status", message{"Processing..."}) }()
	r := p.receive(t)
	if _, present := r["id"]; present {
		t.Error("notification has an id")
	}
	equalJSON(t, r["method"], `"_test/status"`)
	equalJSON(t, r["params"], `{"message":"Processing..."}`)
	select {
	case err := <-done:
		check(t, err)
	case <-ctx.Done():
		t.Fatal("notification write did not finish")
	}
}

// Rust src/agent-client-protocol/src/jsonrpc.rs doctest@4520 and doctest@1322: a request handler can send a
// notification before returning its response. This does not emulate responder
// handles; it uses AgentConnection, the actual public Go callback boundary.
func TestRequestHandlerSendsNotification(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/process", func(ctx context.Context, _ *struct{}) (*answer, error) {
		if err := a.conn.NotifyExtension(ctx, "_test/status", message{"processing"}); err != nil {
			return nil, err
		}
		return &answer{"done"}, nil
	}))
	p := agentPeer(t, a, true)
	p.send(t, request("1", "_test/process", `{}`))
	n := p.receive(t)
	notice, err := json.Marshal(n)
	check(t, err)
	equalJSON(t, notice, `{"jsonrpc":"2.0","method":"_test/status","params":{"message":"processing"}}`)
	r := p.receive(t)
	equalJSON(t, r["id"], `1`)
	equalJSON(t, r["result"], `{"result":"done"}`)
}

// Rust src/agent-client-protocol/src/jsonrpc.rs doctest@1395: a notification handler can send a one-way
// acknowledgement through its connection without replying to the input notice.
func TestNotificationHandlerAcknowledges(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/update", func(ctx context.Context, _ *message) error {
		return a.conn.NotifyExtension(ctx, "_test/status", message{"Acknowledged"})
	}))
	echoHandler(t, a, "_test/echo")
	p := agentPeer(t, a, true)
	p.send(t, `{"jsonrpc":"2.0","method":"_test/update","params":{"message":"update"}}`)
	ack := p.receive(t)
	if _, present := ack["id"]; present {
		t.Error("acknowledgement must be a notification, not a response")
	}
	equalJSON(t, ack["method"], `"_test/status"`)
	equalJSON(t, ack["params"], `{"message":"Acknowledged"}`)
	r := p.call(t, "1", "_test/echo", `{"message":"after acknowledgement"}`)
	equalJSON(t, r["id"], `1`)
	equalJSON(t, r["result"], `{"result":"echo: after acknowledgement"}`)
}
