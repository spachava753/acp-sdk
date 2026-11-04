// SPDX-License-Identifier: Apache-2.0
// Upstream: agentclientprotocol/rust-sdk@050cd94c081056ace37a7b89abfed7f6220b47eb.
// Rust sources: src/agent-client-protocol/tests/{schema_v1_enum_methods,mcp_message_deserialization}.rs
// and src/agent-client-protocol/src/schema/mod.rs (ignored macro examples).
package schema_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// schema_v1_enum_methods.rs::v1_extension_enum_parsing_preserves_method_prefix:
// request and notification methods/params retain their underscore in both directions.
// Go uses connection methods rather than Rust's standalone aggregate enum parser.
func TestExtensionMethodPrefixes(t *testing.T) {
	ctx, client, peer := rawClient(t, nil)
	exchange(t, ctx, peer, "_vendor/request", `{"payload":true}`, `{}`, func() (any, error) {
		return client.CallExtension(ctx, "_vendor/request", map[string]any{"payload": true})
	})
	notification(t, ctx, peer, "_vendor/notify", `{"payload":true}`, func() error {
		return client.NotifyExtension(ctx, "_vendor/notify", map[string]any{"payload": true})
	})
	ctx, conn, peer := rawAgent(t, nil)
	exchange(t, ctx, peer, "_vendor/request", `{"payload":true}`, `{}`, func() (any, error) {
		return conn.CallExtension(ctx, "_vendor/request", map[string]any{"payload": true})
	})
	notification(t, ctx, peer, "_vendor/notify", `{"payload":true}`, func() error {
		return conn.NotifyExtension(ctx, "_vendor/notify", map[string]any{"payload": true})
	})

	// The Rust parse assertions also cover the receiving direction. Register the
	// full names through the public mux: stripping '_' on input would now fail
	// dispatch, even if the sending convenience still emitted the correct name.
	for _, side := range []string{"client_request_and_notification", "agent_request_and_notification"} {
		t.Run(side, func(t *testing.T) {
			mux := acp.NewExtensionMux()
			received := make(chan extensionPayload, 1)
			if err := acp.AddExtensionRequest(mux, "_vendor/request", func(_ context.Context, p *extensionPayload) (*extensionPayload, error) { return p, nil }); err != nil {
				t.Fatal(err)
			}
			if err := acp.AddExtensionNotification(mux, "_vendor/notify", func(_ context.Context, p *extensionPayload) error { received <- *p; return nil }); err != nil {
				t.Fatal(err)
			}
			var ctx context.Context
			var peer acp.Connection
			if side == "client_request_and_notification" {
				ctx, _, peer = rawAgent(t, mux)
			} else {
				ctx, _, peer = rawClient(t, mux)
			}
			res := requestToAgent(t, ctx, peer, "_vendor/request", `{"payload":true}`)
			if res.Error != nil {
				t.Fatal(res.Error)
			}
			assertJSON(t, res.Result, `{"payload":true}`)
			if err := peer.Write(ctx, &jsonrpc.Request{Method: "_vendor/notify", Params: []byte(`{"payload":true}`)}); err != nil {
				t.Fatal(err)
			}
			select {
			case p := <-received:
				if !p.Payload {
					t.Fatal("extension notification payload was changed")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

type extensionPayload struct {
	Payload bool `json:"payload"`
}

// schema_v1_enum_methods.rs::v1_enum_parsing_rejects_unknown_non_extension_methods.
func TestUnknownNonExtensionMethod(t *testing.T) {
	ctx, _, peer := rawAgent(t, nil)
	res := requestToAgent(t, ctx, peer, "vendor/request", `{}`)
	var wireErr *jsonrpc.Error
	if !errors.As(res.Error, &wireErr) || wireErr.Code != jsonrpc.CodeMethodNotFound {
		t.Fatalf("unknown method error = %v, want MethodNotFound", res.Error)
	}
}

// schema_v1_enum_methods.rs::v1_enum_method_matches_schema_method_names:
// a typed prompt must publish session/prompt with its unchanged empty prompt.
func TestPromptMethodName(t *testing.T) {
	ctx, client, peer := rawClient(t, nil)
	exchange(t, ctx, peer, "session/prompt", `{"sessionId":"session-1","prompt":[]}`, `{"stopReason":"end_turn"}`, func() (any, error) {
		return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "session-1", Prompt: []acp.ContentBlock{}})
	})
	incomingRequest(t, true, "session/prompt", `{"sessionId":"session-1","prompt":[]}`, `{"stopReason":"end_turn"}`)
}

// src/schema/mod.rs:69's ignored impl_jsonrpc_notification! example declares
// CancelNotification's session/cancel route. Test the observable V1 API rather
// than attempting to reproduce a Rust macro/trait implementation in Go.
func TestCancelNotificationMethod(t *testing.T) {
	ctx, client, peer := rawClient(t, nil)
	notification(t, ctx, peer, "session/cancel", `{"sessionId":"session-1"}`, func() error {
		return client.SessionCancel(ctx, &acp.CancelNotification{SessionID: "session-1"})
	})
	incomingNotification(t, true, "session/cancel", `{"sessionId":"session-1"}`)
}

// mcp_message_deserialization.rs::mcp_message_kind_is_selected_by_outer_id:
// inner logical requestId does not choose the outer kind; numeric/string outer
// IDs are requests; bool/object/array IDs must not fall back to notifications.
func TestMcpOuterMessageKind(t *testing.T) {
	const params = `{"serverId":"server","requestId":"logical-id","method":"custom/message"}`
	for _, tt := range []struct {
		name, id string
		call     bool
	}{
		{"notification", "", false}, {"numeric_request", `42`, true}, {"string_request", `"outer-id"`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prefix := `{"jsonrpc":"2.0","method":"mcp/message","params":` + params
			if tt.id != "" {
				prefix += `,"id":` + tt.id
			}
			msg, err := jsonrpc.DecodeMessage([]byte(prefix + `}`))
			if err != nil {
				t.Fatal(err)
			}
			req, ok := msg.(*jsonrpc.Request)
			if !ok || req.ID.IsValid() != tt.call || req.Method != "mcp/message" {
				t.Fatalf("outer kind = %#v", msg)
			}
			assertJSON(t, req.Params, params)
			if tt.call {
				assertJSON(t, encode(t, req.ID.Raw()), tt.id)
			}
		})
	}
	for _, id := range []string{`true`, `{}`, `[]`} {
		if _, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","method":"mcp/message","params":` + params + `,"id":` + id + `}`)); err == nil {
			t.Errorf("invalid outer ID %s fell back to a notification", id)
		}
	}
}

// mcp_message_deserialization.rs::v1_mcp_method_is_in_separate_request_and_notification_enums:
// agent-originated request and client-originated notification share a method but
// are distinct typed sender APIs. Wrong-direction dispatch is checked below.
func TestMcpTypedRoutes(t *testing.T) {
	const params = `{"serverId":"server","requestId":"logical-id","method":"custom/message"}`
	var req acp.MessageMcpRequest
	var n acp.MessageMcpNotification
	roundTrip(t, params, &req)
	roundTrip(t, params, &n)
	ctx, conn, peer := rawAgent(t, nil)
	exchange(t, ctx, peer, "mcp/message", params, `{"result":null}`, func() (any, error) { return conn.McpMessage(ctx, &req) })
	ctx, client, peer := rawClient(t, nil)
	notification(t, ctx, peer, "mcp/message", params, func() error { return client.McpMessageNotification(ctx, &n) })
	incomingRequest(t, false, "mcp/message", params, `{"result":null}`)
	incomingNotification(t, true, "mcp/message", params)
}

type mcpReceiver struct{ calls chan struct{} }

func (h *mcpReceiver) McpMessage(context.Context, *acp.MessageMcpRequest) (*acp.MessageMcpResponse, error) {
	h.calls <- struct{}{}
	res := acp.ResultMessageMcpResponse(nil)
	return &res, nil
}

var _ acp.McpMessageHandler = (*mcpReceiver)(nil)

// mcp_message_deserialization.rs::v1_mcp_method_is_in_separate_request_and_notification_enums:
// ClientRequest must reject the agent->client MCP request route. This is method
// direction, not application MCP server binding/availability policy.
func TestMcpRequestWrongDirection(t *testing.T) {
	directionChild(t, "TestMcpRequestDirectionChild", "ACP_RUST_SCHEMA_MCP_REQUEST_DIRECTION_CHILD", "wrong-direction MCP request must not invoke the notification handler")
}

type mcpNotificationReceiver struct{ calls chan struct{} }

func (h *mcpNotificationReceiver) McpMessageNotification(context.Context, *acp.MessageMcpNotification) error {
	h.calls <- struct{}{}
	return nil
}

var _ acp.McpMessageNotificationHandler = (*mcpNotificationReceiver)(nil)

// Subprocess body for the same upstream wrong-direction request assertion. A
// legitimate notification handler is installed: otherwise MethodNotFound from
// an absent handler could conceal erroneous request/notification selection.
func TestMcpRequestDirectionChild(t *testing.T) {
	if os.Getenv("ACP_RUST_SCHEMA_MCP_REQUEST_DIRECTION_CHILD") != "1" {
		return
	}
	h := &mcpNotificationReceiver{calls: make(chan struct{}, 1)}
	ctx, conn, peer := rawAgent(t, h)
	res := requestToAgent(t, ctx, peer, "mcp/message", `{"serverId":"server","requestId":"logical-id","method":"custom/message"}`)
	var wireErr *jsonrpc.Error
	if !errors.As(res.Error, &wireErr) || wireErr.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("wrong-direction MCP request error = %v, want MethodNotFound", res.Error)
	}
	drainReceiving(t, conn.Close)
	select {
	case <-h.calls:
		t.Fatal("wrong-direction request invoked the MCP notification handler")
	default:
	}
}

// The same upstream function rejects AgentNotification::parse_message(mcp/message).
// Public Go dispatch can currently panic if it confuses that notification with a
// callback request, so isolate this scenario without weakening the expectation.
func TestMcpNotificationWrongDirection(t *testing.T) {
	directionChild(t, "TestMcpNotificationDirectionChild", "ACP_RUST_SCHEMA_MCP_DIRECTION_CHILD", "wrong-direction MCP notification must not invoke the request handler")
}

func directionChild(t *testing.T, testName, envKey, expectation string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+testName+"$", "-test.timeout=6s")
	cmd.Env = append(os.Environ(), envKey+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", expectation, err, output)
	}
}

// TestMcpNotificationDirectionChild is only the subprocess body of the upstream
// wrong-direction case, not an additional independent conformance scenario.
func TestMcpNotificationDirectionChild(t *testing.T) {
	if os.Getenv("ACP_RUST_SCHEMA_MCP_DIRECTION_CHILD") != "1" {
		return
	}
	h := &mcpReceiver{calls: make(chan struct{}, 1)}
	ctx, client, peer := rawClient(t, h)
	if err := peer.Write(ctx, &jsonrpc.Request{Method: "mcp/message", Params: []byte(`{"serverId":"server","requestId":"logical-id","method":"custom/message"}`)}); err != nil {
		t.Fatal(err)
	}
	// A later response proves the reader accepted the preceding notification.
	// Closing then drains handlers before checking non-invocation, without sleeps.
	res := requestToAgent(t, ctx, peer, "_schema_port/barrier", `{}`)
	var wireErr *jsonrpc.Error
	if !errors.As(res.Error, &wireErr) || wireErr.Code != jsonrpc.CodeMethodNotFound {
		t.Fatalf("barrier response = %#v", res)
	}
	done := make(chan error, 1)
	go func() { done <- client.Close() }()
	bounded(t, done, "notification dispatch drain")
	select {
	case <-h.calls:
		t.Fatal("wrong-direction notification invoked the MCP request handler")
	default:
	}
}
