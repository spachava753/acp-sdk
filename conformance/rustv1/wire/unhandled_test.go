package wire_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust src/agent-client-protocol/tests/jsonrpc_unhandled_messages.rs::unhandled_notifications_are_ignored,
// all four unknown/protocol/vendor notification forms, followed by a request.
func TestUnhandledNotificationsIgnored(t *testing.T) {
	a := newAgent()
	echoHandler(t, a, "_test/simple")
	p := agentPeer(t, a, true)
	for _, line := range []string{
		`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":"req-1"}}`,
		`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":true}}`,
		`{"jsonrpc":"2.0","method":"_x.ai/settings/update","params":{"theme":"auto"}}`,
		`{"jsonrpc":"2.0","method":"unknown/notification","params":{"value":1}}`,
	} {
		p.send(t, line)
	}
	r := p.call(t, "2", "_test/simple", `{"message":"after notifications"}`)
	equalJSON(t, r["id"], `2`)
	equalJSON(t, r["result"], `{"result":"echo: after notifications"}`)
}

// Rust src/agent-client-protocol/tests/jsonrpc_unhandled_messages.rs::unhandled_requests_are_rejected_with_method_not_found,
// src/agent-client-protocol/tests/error_wire_format.rs::_test/unknown snapshot.
// The method in error.data is an upstream SDK diagnostic policy.
func TestUnhandledRequestErrorData(t *testing.T) {
	for _, method := range []string{"unknown/request", "_test/unknown"} {
		p := agentPeer(t, newAgent(), true)
		r := p.call(t, "1", method, `{"value":1}`)
		code(t, r, jsonrpc.CodeMethodNotFound)
		var e struct {
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		}
		check(t, json.Unmarshal(r["error"], &e))
		if e.Message == "" {
			t.Error("method-not-found message is empty")
		}
		b, err := json.Marshal(method)
		check(t, err)
		equalJSON(t, e.Data, string(b))
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_error_handling.rs::test_invalid_params_keeps_connection_alive
// and ::test_bad_request_params_return_invalid_params_and_connection_stays_alive.
// Use the SDK's schema-declared required cwd rather than inventing validation
// for a Go extension struct: AddExtensionRequest explicitly leaves field
// validation to its application handler. No validator is added to the fixture.
func TestRequiredParamsErrorAndRecovery(t *testing.T) {
	for _, params := range []string{`{"wrong_field":"hello"}`, `{"content":"hello"}`} {
		p := agentPeer(t, newAgent(), true)
		r := p.call(t, "3", "session/new", params)
		code(t, r, jsonrpc.CodeInvalidParams)
		equalJSON(t, r["id"], `3`)
		if params == `{"content":"hello"}` {
			// Rust's second case additionally pins deserialization diagnostics.
			// The missing field is cwd on the Go ACP schema, not Rust's message.
			var e struct {
				Data struct {
					Error string          `json:"error"`
					JSON  json.RawMessage `json:"json"`
					Phase string          `json:"phase"`
				} `json:"data"`
			}
			check(t, json.Unmarshal(r["error"], &e))
			if e.Data.Phase != "deserialization" || e.Data.Error == "" {
				t.Errorf("missing deserialization diagnostic: %s", r["error"])
			}
			equalJSON(t, e.Data.JSON, params)
		}
		good := p.call(t, "4", "session/new", `{"cwd":"/tmp","mcpServers":[]}`)
		equalJSON(t, good["id"], `4`)
		equalJSON(t, good["result"], `{"sessionId":"session"}`)
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_error_handling.rs::test_notification_errors_are_ignored_and_connection_stays_alive.
// Preserve both source cases: missing a schema-required string and handler
// error. For required presence use ACP's sessionId, not an extension struct
// whose validation Go explicitly delegates to its application. Check handler
// observations after RunAgent drains, not after the wire barrier alone.
func TestNotificationErrorsIgnored(t *testing.T) {
	a := newAgent()
	a.cancels = make(chan string, 1)
	invoked := make(chan string, 1)
	t.Cleanup(func() {
		select {
		case id := <-a.cancels:
			t.Errorf("missing required sessionId reached typed handler: %q", id)
		default:
		}
		select {
		case m := <-invoked:
			if m != "handler error" {
				t.Errorf("handler payload = %q", m)
			}
		default:
			t.Error("valid handler-error notification was not delivered")
		}
	})
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/notification", func(_ context.Context, p *message) error {
		invoked <- p.Message
		return &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "handler error"}
	}))
	echoHandler(t, a, "_test/simple")
	p := agentPeer(t, a, true)
	p.send(t, `{"jsonrpc":"2.0","method":"session/cancel","params":{"wrong_field":"hello"}}`)
	p.send(t, `{"jsonrpc":"2.0","method":"_test/notification","params":{"message":"handler error"}}`)
	r := p.call(t, "10", "_test/simple", `{"message":"after bad notification"}`)
	equalJSON(t, r["id"], `10`)
	equalJSON(t, r["result"], `{"result":"echo: after bad notification"}`)
}

// Rust src/agent-client-protocol/tests/jsonrpc_unhandled_messages.rs::late_response_to_dropped_request_is_discarded_without_closing_connection.
// Go has no droppable SentRequest handle; cancellation of a successfully written
// call is its public abandonment operation. Release that late response before
// the release request's response, then verify another request still succeeds.
func TestLateResponseAfterAbandonment(t *testing.T) {
	c, p := clientPeer(t)
	ctx, cancel := context.WithCancel(context.Background())
	abandoned := make(chan error, 1)
	go func() { _, err := c.CallExtension(ctx, "_test/simple", message{"late"}); abandoned <- err }()
	late := p.receive(t)
	cancel()
	select {
	case err := <-abandoned:
		if err == nil {
			t.Fatal("abandoned request unexpectedly succeeded")
		}
	case <-time.After(wait):
		t.Fatal("abandoned call did not return")
	}
	for _, s := range []string{"release", "after"} {
		ctx, stop := context.WithTimeout(context.Background(), wait)
		done := make(chan callResult[json.RawMessage], 1)
		go func() {
			r, err := c.CallExtension(ctx, "_test/simple", message{s})
			done <- callResult[json.RawMessage]{r, err}
		}()
		r := p.receive(t)
		if s == "release" {
			p.send(t, `{"jsonrpc":"2.0","id":`+string(late["id"])+`,"result":{"result":"late response"}}`)
		}
		b, err := json.Marshal("echo: " + s)
		check(t, err)
		p.send(t, `{"jsonrpc":"2.0","id":`+string(r["id"])+`,"result":{"result":`+string(b)+`}}`)
		select {
		case got := <-done:
			check(t, got.err)
			equalJSON(t, got.value, `{"result":`+string(b)+`}`)
		case <-ctx.Done():
			t.Fatal("connection did not survive late response")
		}
		stop()
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_request_ids.rs::the_same_request_id_can_be_in_flight_in_both_directions,
// including cancellation of only the inbound request. This is an optional
// request-cancellation profile: the public handler context is Go's cancellation
// signal, and the fixture never processes $/cancel_request itself.
func TestRequestIDsSeparatePerDirection(t *testing.T) {
	a := newAgent()
	release := make(chan struct{})
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/ask", func(ctx context.Context, _ *struct{}) (*json.RawMessage, error) {
		r, err := a.conn.CallExtension(ctx, "_test/question", struct{}{})
		return &r, err
	}))
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/slow", func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		select {
		case <-ctx.Done():
			return nil, &jsonrpc.Error{Code: int64(acp.ErrorCodeRequestCancelled), Message: "Request cancelled"}
		case <-release:
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "fixture cleanup"}
		}
	}))
	p := agentPeer(t, a, true)
	// This releases a broken handler only during failure cleanup, not as the
	// source of cancellation the test expects from the SDK.
	t.Cleanup(func() { close(release) })
	p.send(t, request(`"ask"`, "_test/ask", `{}`))
	question := p.receive(t)
	equalJSON(t, question["method"], `"_test/question"`)
	shared := string(question["id"])
	p.send(t, request(shared, "_test/slow", `{}`))
	p.send(t, `{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":`+shared+`}}`)
	incoming := p.receive(t)
	equalJSON(t, incoming["id"], shared)
	code(t, incoming, int64(acp.ErrorCodeRequestCancelled))
	p.send(t, `{"jsonrpc":"2.0","id":`+shared+`,"result":{"answer":"42"}}`)
	r := p.receive(t)
	equalJSON(t, r["id"], `"ask"`)
	equalJSON(t, r["result"], `{"answer":"42"}`)
}
