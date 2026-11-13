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

// The route tables infer request and result types from these methods, so a
// wrong result type with identical JSON would still pass there. Pin each
// method's exact request and response types at compile time.
var (
	_ func(*acp.Client, context.Context, *acp.AuthStatusRequest) (*acp.AuthStatusResponse, error)                        = (*acp.Client).AuthStatus
	_ func(*acp.Client, context.Context, *acp.ArchiveSessionRequest) (*acp.ArchiveSessionResponse, error)                = (*acp.Client).SessionArchive
	_ func(*acp.Client, context.Context, *acp.UnarchiveSessionRequest) (*acp.UnarchiveSessionResponse, error)            = (*acp.Client).SessionUnarchive
	_ func(*acp.Client, context.Context, *acp.SessionStatusRequest) (*acp.SessionStatusResponse, error)                  = (*acp.Client).SessionStatus
	_ func(*acp.Client, context.Context, *acp.SetSessionTitleRequest) (*acp.SetSessionTitleResponse, error)              = (*acp.Client).SessionSetTitle
	_ func(*acp.Client, context.Context, *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error)                  = (*acp.Client).SessionDelete
	_ func(*acp.Client, context.Context, *acp.PromptRequest) (*acp.PromptResponse, error)                                = (*acp.Client).SessionPrompt
	_ func(*acp.Client, context.Context, *acp.ListProvidersRequest) (*acp.ListProvidersResponse, error)                  = (*acp.Client).ProvidersList
	_ func(*acp.Client, context.Context, *acp.SetProviderRequest) (*acp.SetProviderResponse, error)                      = (*acp.Client).ProvidersSet
	_ func(*acp.Client, context.Context, *acp.DisableProviderRequest) (*acp.DisableProviderResponse, error)              = (*acp.Client).ProvidersDisable
	_ func(*acp.Client, context.Context, *acp.InitializeProxyRequest) (*acp.InitializeProxyResponse, error)              = (*acp.Client).ProxyInitialize
	_ func(*acp.Client, context.Context, *acp.ProxySuccessorRequest) (*acp.ProxySuccessorResponse, error)                = (*acp.Client).ProxySuccessor
	_ func(*acp.AgentConnection, context.Context, *acp.ProxySuccessorRequest) (*acp.ProxySuccessorResponse, error)       = (*acp.AgentConnection).ProxySuccessor
	_ func(*acp.AgentConnection, context.Context, *acp.CreateElicitationRequest) (*acp.CreateElicitationResponse, error) = (*acp.AgentConnection).ElicitationCreate
	_ func(*acp.AgentConnection, context.Context, *acp.MessageMcpRequest) (*acp.MessageMcpResponse, error)               = (*acp.AgentConnection).McpMessage
)

const (
	successorPrompt = `{"method":"session/prompt","params":{"sessionId":"s","_meta":{"inner":1}},"_meta":{"outer":1}}`
	successorCancel = `{"method":"session/cancel"}`
	successorResult = `{"zeta":1,"alpha":[null,1.5],"_meta":{"k":"v"}}`
	elicitationForm = `{"mode":"form","sessionId":"sess_abc123","message":"Please enter your name","requestedSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}`
	mcpToolsList    = `{"serverId":"server-1","requestId":"request-1","method":"tools/list"}`
	mcpCustom       = `{"serverId":"server","requestId":"logical-id","method":"custom/message"}`
)

// TestRequestRoutes sends each typed request from the side that owns it and
// delivers the same params to the other side. The typed params must survive a
// round trip, the sender must use the method name with an id, the typed result
// must keep the peer's reply, and the receiver must run only the matching
// handler and encode its typed result exactly.
func TestRequestRoutes(t *testing.T) {
	for _, tt := range []struct {
		name   string
		via    requestMethod
		method string
		params string
		reply  string // the peer's result for the outgoing call
		served string // the receiving recorder's result
		check  func(t *testing.T, request, result any)
	}{
		{
			name: "auth_status", via: clientRequest((*acp.Client).AuthStatus), method: "auth/status",
			params: `{}`, reply: `{"state":"authenticated","accounts":[{"label":"Key","active":true}]}`, served: `{"state":"not_required"}`,
		},
		{
			name: "session_archive", via: clientRequest((*acp.Client).SessionArchive), method: "session/archive",
			params: `{"sessionId":"sess_1"}`, reply: `{}`, served: `{}`,
		},
		{
			name: "session_unarchive", via: clientRequest((*acp.Client).SessionUnarchive), method: "session/unarchive",
			params: `{"sessionId":"sess_1"}`, reply: `{}`, served: `{}`,
		},
		{
			name: "session_status", via: clientRequest((*acp.Client).SessionStatus), method: "session/status",
			params: `{"sessionId":"sess_1"}`, reply: `{"status":"saved"}`, served: `{"status":"open"}`,
		},
		{
			name: "session_set_title", via: clientRequest((*acp.Client).SessionSetTitle), method: "session/set_title",
			params: `{"sessionId":"sess_1","title":"Refactor"}`, reply: `{"title":"Refactor"}`, served: `{"title":"Refactor"}`,
		},
		{
			name: "session_delete", via: clientRequest((*acp.Client).SessionDelete), method: "session/delete",
			params: `{"sessionId":"sess_abc123"}`, reply: `{}`, served: `{}`,
			check: func(t *testing.T, request, _ any) {
				if id := request.(*acp.DeleteSessionRequest).SessionID; id != "sess_abc123" {
					t.Errorf("session ID = %q", id)
				}
			},
		},
		{
			name: "session_prompt", via: clientRequest((*acp.Client).SessionPrompt), method: "session/prompt",
			params: `{"sessionId":"session-1","prompt":[]}`, reply: `{"stopReason":"end_turn"}`, served: `{"stopReason":"end_turn"}`,
		},
		{
			name: "providers_list", via: clientRequest((*acp.Client).ProvidersList), method: "providers/list",
			params: `{}`, reply: `{"providers":[]}`, served: `{"providers":[]}`,
		},
		{
			name: "providers_disable", via: clientRequest((*acp.Client).ProvidersDisable), method: "providers/disable",
			params: `{"providerId":"main"}`, reply: `{}`, served: `{}`,
		},
		{
			name: "proxy_initialize", via: clientRequest((*acp.Client).ProxyInitialize), method: "proxy/initialize",
			params: `{"protocolVersion":1}`, reply: `{"protocolVersion":1}`, served: `{"protocolVersion":1}`,
		},
		// Both sides send and serve proxy/successor. The cancel envelope has
		// no params; any result passes through unchanged.
		{
			name: "proxy_successor_from_client", via: clientRequest((*acp.Client).ProxySuccessor), method: "proxy/successor",
			params: successorPrompt, reply: successorResult, served: successorResult,
		},
		{
			name: "proxy_successor_from_agent", via: agentRequest((*acp.AgentConnection).ProxySuccessor), method: "proxy/successor",
			params: successorPrompt, reply: successorResult, served: successorResult,
		},
		{
			name: "proxy_successor_without_params_from_client", via: clientRequest((*acp.Client).ProxySuccessor), method: "proxy/successor",
			params: successorCancel, reply: successorResult, served: successorResult,
		},
		{
			name: "proxy_successor_without_params_from_agent", via: agentRequest((*acp.AgentConnection).ProxySuccessor), method: "proxy/successor",
			params: successorCancel, reply: successorResult, served: successorResult,
		},
		{
			name: "elicitation_create_decline", via: agentRequest((*acp.AgentConnection).ElicitationCreate), method: "elicitation/create",
			params: elicitationForm, reply: `{"action":"decline"}`, served: `{"action":"decline"}`,
		},
		{
			name: "elicitation_create_cancel", via: agentRequest((*acp.AgentConnection).ElicitationCreate), method: "elicitation/create",
			params: elicitationForm, reply: `{"action":"cancel"}`, served: `{"action":"decline"}`,
		},
		{
			name: "mcp_message", via: agentRequest((*acp.AgentConnection).McpMessage), method: "mcp/message",
			params: mcpToolsList, reply: `{"result":{"tools":[]}}`, served: `{"result":null}`,
			check: func(t *testing.T, _, result any) {
				if r := result.(*acp.MessageMcpResponse); r.Result == nil || r.Error != nil {
					t.Errorf("MCP success decoded as %#v, want a result carrier", r)
				}
			},
		},
		{
			// The inner requestId is MCP data; it does not affect the route.
			name: "mcp_message_logical_id", via: agentRequest((*acp.AgentConnection).McpMessage), method: "mcp/message",
			params: mcpCustom, reply: `{"result":null}`, served: `{"result":null}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request, result := tt.via.send(t, tt.method, tt.params, tt.reply)
			acptest.EqualMarshaled(t, result, tt.reply)
			acptest.RoundTrip(t, tt.served, tt.via.newResult())
			receiveRequest(t, tt.via.from.other(), tt.method, tt.params, tt.served)
			if tt.check != nil {
				tt.check(t, request, result)
			}
		})
	}
}

// TestNotificationRoutes is TestRequestRoutes for typed notifications: the
// sender must omit the id, and the receiver must run only the matching
// notification handler.
func TestNotificationRoutes(t *testing.T) {
	for _, tt := range []struct {
		name   string
		via    notificationMethod
		method string
		params string
	}{
		{"session_cancel", clientNotification((*acp.Client).SessionCancel), "session/cancel", `{"sessionId":"session-1"}`},
		{"auth_status_update", agentNotification((*acp.AgentConnection).AuthStatusUpdate), "auth/status_update", `{"state":"unauthenticated"}`},
		{"elicitation_complete", agentNotification((*acp.AgentConnection).ElicitationComplete), "elicitation/complete", `{"elicitationId":"elicit_1"}`},
		{"mcp_message", clientNotification((*acp.Client).McpMessageNotification), "mcp/message", `{"serverId":"server-1","requestId":"request-1","method":"notifications/tools/list"}`},
		{"mcp_message_logical_id", clientNotification((*acp.Client).McpMessageNotification), "mcp/message", mcpCustom},
		{"proxy_successor_from_client", clientNotification((*acp.Client).ProxySuccessorNotification), "proxy/successor", successorPrompt},
		{"proxy_successor_from_agent", agentNotification((*acp.AgentConnection).ProxySuccessorNotification), "proxy/successor", successorPrompt},
		{"proxy_successor_without_params_from_client", clientNotification((*acp.Client).ProxySuccessorNotification), "proxy/successor", successorCancel},
		{"proxy_successor_without_params_from_agent", agentNotification((*acp.AgentConnection).ProxySuccessorNotification), "proxy/successor", successorCancel},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.via.send(t, tt.method, tt.params)
			receiveNotification(t, tt.via.from.other(), tt.method, tt.params)
		})
	}
}

// TestUnroutedRequests sends known or unknown methods that the receiving side
// does not handle. Each must fail with MethodNotFound, and when a recorder is
// installed none of its handlers may run, even one for a related method or for
// the same method on the other side.
func TestUnroutedRequests(t *testing.T) {
	for _, tt := range []struct {
		name      string
		to        side
		method    string
		params    string
		noHandler bool
	}{
		// The agent handles session/delete but not session/close.
		{name: "session_close", to: agentSide, method: "session/close", params: `{"sessionId":"sess_abc123"}`},
		// The agent handles providers/list but not providers/set.
		{name: "providers_set", to: agentSide, method: "providers/set", params: `{"providerId":"main","apiType":"_gateway","baseUrl":"https://llm.example.test","headers":{"X-Test-Routing":"tenant-demo"}}`},
		// Clients serve elicitation/create; session/prompt is an agent method.
		{name: "session_prompt_to_client", to: clientSide, method: "session/prompt", params: elicitationForm},
		// Methods without a leading underscore are never extensions.
		{name: "unknown_method", to: agentSide, method: "vendor/request", params: `{}`},
		{name: "unknown_method_without_handler", to: agentSide, method: "vendor/request", params: `{}`, noHandler: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.noHandler {
				peer, _ := serve(t, tt.to, nil)
				expectError(t, peer, tt.method, tt.params, jsonrpc.CodeMethodNotFound)
				return
			}
			rec := newRecorder()
			peer, settle := serve(t, tt.to, rec)
			expectError(t, peer, tt.method, tt.params, jsonrpc.CodeMethodNotFound)
			settle()
			rec.expectNone(t)
		})
	}
}

// TestExtensionMethodNames checks that extension methods keep their leading
// underscore when sent and are matched by their full name when received.
func TestExtensionMethodNames(t *testing.T) {
	const params = `{"payload":true}`
	for _, s := range []side{clientSide, agentSide} {
		t.Run(s.String()+"_sends", func(t *testing.T) {
			var conn interface {
				CallExtension(context.Context, string, any) (json.RawMessage, error)
				NotifyExtension(context.Context, string, any) error
			}
			var peer *acptest.Peer
			if s == clientSide {
				conn, peer = startClient(t)
			} else {
				conn, peer = startAgent(t)
			}
			payload := map[string]any{"payload": true}
			expectCall(t, peer, "_vendor/request", params, `{}`, func() (json.RawMessage, error) {
				return conn.CallExtension(t.Context(), "_vendor/request", payload)
			})
			expectNotification(t, peer, "_vendor/notify", params, func() error {
				return conn.NotifyExtension(t.Context(), "_vendor/notify", payload)
			})
		})
		t.Run(s.String()+"_receives", func(t *testing.T) {
			type extensionPayload struct {
				Payload bool `json:"payload"`
			}
			mux := acp.NewExtensionMux()
			if err := acp.AddExtensionRequest(mux, "_vendor/request", func(_ context.Context, p *extensionPayload) (*extensionPayload, error) { return p, nil }); err != nil {
				t.Fatal(err)
			}
			notified := make(chan extensionPayload, 1)
			if err := acp.AddExtensionNotification(mux, "_vendor/notify", func(_ context.Context, p *extensionPayload) error { notified <- *p; return nil }); err != nil {
				t.Fatal(err)
			}
			peer, _ := serve(t, s, mux)
			m := call(t, peer, "_vendor/request", params)
			if m.Error != nil {
				t.Fatalf("extension request failed: %s", m)
			}
			acptest.EqualJSON(t, m.Result, params)
			peer.Notify("_vendor/notify", json.RawMessage(params))
			if got := acptest.Await(t, notified); !got.Payload {
				t.Error("extension notification payload was changed")
			}
		})
	}
}
