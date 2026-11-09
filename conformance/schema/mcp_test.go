// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// mcpOnlyFailure is an MCP error code carried inside a successful ACP result.
const mcpOnlyFailure int32 = -32042

// TestMcpResponseCarriers checks that an mcp/message result carries the inner
// MCP outcome opaquely. Any JSON value is an inner success, an inner error
// stays an error carrier with its unknown fields, and the carrier's own _meta
// stays separate. Over the wire, both arrive as a successful ACP call: an
// inner error or input_required result must not become an RPC error.
func TestMcpResponseCarriers(t *testing.T) {
	const meta = `{"example/carrier":{"trace":"outer"}}`
	outerMeta := acp.Meta{"example/carrier": map[string]any{"trace": "outer"}}

	// sendMcp makes an mcp/message call from an SDK agent and answers it with
	// literal, returning the typed carrier.
	sendMcp := func(t *testing.T, literal string) *acp.MessageMcpResponse {
		conn, peer := startAgent(t)
		response := expectCall(t, peer, "mcp/message", mcpToolsList, literal, func() (*acp.MessageMcpResponse, error) {
			return conn.McpMessage(t.Context(), &acp.MessageMcpRequest{ServerID: "server-1", RequestID: "request-1", Method: "tools/list"})
		})
		acptest.EqualMarshaled(t, response, literal)
		return response
	}

	for _, tt := range []struct{ name, result string }{
		{"null", `null`},
		{"array", `["opaque",7]`},
		{"input_required", `{"resultType":"input_required","requestState":"opaque/retry?keep=exact","inputRequests":{"confirmation":{"method":"elicitation/create","params":{}}},"_meta":{"example/inner":"retained"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var result any
			decode(t, tt.result, &result)
			carrier := acp.ResultMessageMcpResponse(result)
			carrier.Meta = outerMeta
			literal := `{"result":` + tt.result + `,"_meta":` + meta + `}`
			acptest.EqualMarshaled(t, carrier, literal)

			var parsed acp.MessageMcpResponse
			acptest.RoundTrip(t, literal, &parsed)
			if parsed.Result == nil || parsed.Error != nil {
				t.Fatalf("MCP success decoded as %#v, want a result carrier", parsed)
			}
			acptest.EqualMarshaled(t, *parsed.Result, tt.result)
			acptest.EqualMarshaled(t, parsed.Meta, meta)

			if got := sendMcp(t, literal); got.Result == nil || got.Error != nil {
				t.Errorf("MCP success returned as %#v, want a result carrier", got)
			}
		})
	}
	t.Run("inner_error", func(t *testing.T) {
		// data:null and the unknown member belong to the inner MCP error, not
		// to the ACP carrier.
		const inner = `{"code":-32042,"message":"MCP-only failure","data":null,"example/detail":{"retryable":true}}`
		literal := `{"error":` + inner + `,"_meta":` + meta + `}`
		var mcpErr acp.McpError
		decode(t, inner, &mcpErr)
		carrier := acp.ErrorMessageMcpResponse(mcpErr)
		carrier.Meta = outerMeta
		acptest.EqualMarshaled(t, carrier, literal)

		var parsed acp.MessageMcpResponse
		acptest.RoundTrip(t, literal, &parsed)
		if parsed.Error == nil || parsed.Result != nil || parsed.Error.Code != mcpOnlyFailure {
			t.Fatalf("MCP error decoded as %#v, want an error carrier", parsed)
		}
		acptest.EqualMarshaled(t, parsed.Error, inner)
		acptest.EqualMarshaled(t, parsed.Meta, meta)

		if got := sendMcp(t, literal); got.Error == nil || got.Result != nil {
			t.Errorf("MCP error returned as %#v, want an error carrier", got)
		}
	})
}

// TestMcpMessageKind checks that the outer JSON-RPC id, not the inner MCP
// requestId, decides whether an mcp/message frame is a request or a
// notification, and that an invalid outer id is an error rather than a
// notification.
func TestMcpMessageKind(t *testing.T) {
	const params = mcpCustom
	for _, tt := range []struct {
		name, id string
		call     bool
	}{
		{"notification", "", false},
		{"numeric_request", `42`, true},
		{"string_request", `"outer-id"`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			frame := `{"jsonrpc":"2.0","method":"mcp/message","params":` + params
			if tt.id != "" {
				frame += `,"id":` + tt.id
			}
			msg, err := jsonrpc.DecodeMessage([]byte(frame + `}`))
			if err != nil {
				t.Fatal(err)
			}
			req, ok := msg.(*jsonrpc.Request)
			if !ok || req.ID.IsValid() != tt.call || req.Method != "mcp/message" {
				t.Fatalf("decoded %#v", msg)
			}
			acptest.EqualJSON(t, req.Params, params)
			if tt.call {
				acptest.EqualMarshaled(t, req.ID.Raw(), tt.id)
			}
		})
	}
	for _, id := range []string{`true`, `{}`, `[]`} {
		t.Run("invalid_id_"+id, func(t *testing.T) {
			if _, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","method":"mcp/message","params":` + params + `,"id":` + id + `}`)); err == nil {
				t.Errorf("outer id %s was accepted", id)
			}
		})
	}
}

// mcpRequestOnly and mcpNotificationOnly each implement one MCP handler,
// recording into rec.
type mcpRequestOnly struct{ rec *recorder }

func (h mcpRequestOnly) McpMessage(ctx context.Context, p *acp.MessageMcpRequest) (*acp.MessageMcpResponse, error) {
	return h.rec.McpMessage(ctx, p)
}

type mcpNotificationOnly struct{ rec *recorder }

func (h mcpNotificationOnly) McpMessageNotification(ctx context.Context, p *acp.MessageMcpNotification) error {
	return h.rec.McpMessageNotification(ctx, p)
}

// TestMcpWrongDirection checks that mcp/message is routed by direction. The
// request goes from agent to client and the notification from client to agent,
// so neither is handled on the other side. Each direction runs with a handler
// for the opposite kind only, and with the recorder, which implements both.
func TestMcpWrongDirection(t *testing.T) {
	for _, tt := range []struct {
		name string
		both bool // install the recorder instead of the opposite-kind handler
	}{
		{"opposite_kind_handler", false},
		{"both_handlers", true},
	} {
		handler := func(rec *recorder, opposite any) any {
			if tt.both {
				return rec
			}
			return opposite
		}
		t.Run("request_to_agent/"+tt.name, func(t *testing.T) {
			rec := newRecorder()
			peer, settle := serve(t, agentSide, handler(rec, mcpNotificationOnly{rec}))
			expectError(t, peer, "mcp/message", mcpCustom, jsonrpc.CodeMethodNotFound)
			settle()
			rec.expectNone(t)
		})
		t.Run("notification_to_client/"+tt.name, func(t *testing.T) {
			rec := newRecorder()
			peer, settle := serve(t, clientSide, handler(rec, mcpRequestOnly{rec}))
			peer.Notify("mcp/message", json.RawMessage(mcpCustom))
			// The response to a later request shows the client has read the
			// notification; settling then waits for any handler it started.
			expectError(t, peer, "_schema/barrier", `{}`, jsonrpc.CodeMethodNotFound)
			settle()
			rec.expectNone(t)
		})
	}
}
