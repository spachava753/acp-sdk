// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
package session_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust: src/agent-client-protocol/tests/protocol_v2.rs::non_acp_initialize_is_not_rewritten.
// This is the version-neutral raw-transport control, not V2 support. Go's typed
// ACP Initialize request cannot represent a foreign string protocol version.
func TestNonACPInitializeIsNotRewritten(t *testing.T) {
	local, remote := net.Pipe()
	t.Cleanup(func() { local.Close(); remote.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	t.Cleanup(cancel)
	raw, err := (&acp.IOTransport{Reader: local, Writer: local}).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := jsonrpc.MakeID("foreign-initialize")
	if err != nil {
		t.Fatal(err)
	}
	result := startCall(func() (jsonrpc.Message, error) {
		if err := raw.Write(ctx, &jsonrpc.Request{ID: id, Method: "initialize", Params: json.RawMessage(`{"protocolVersion":"2025-06-18"}`)}); err != nil {
			return nil, err
		}
		return raw.Read(ctx)
	})
	peer := &wirePeer{conn: remote, reader: bufio.NewReader(remote)}
	request := peer.read(t)
	if request.Method != "initialize" {
		t.Fatalf("method = %q", request.Method)
	}
	assertJSON(t, request.Params, `{"protocolVersion":"2025-06-18"}`)
	peer.result(t, request, json.RawMessage(`{"protocolVersion":"2025-06-18"}`))
	message := valueOf(t, result)
	response, ok := message.(*jsonrpc.Response)
	if !ok || response.Error != nil {
		t.Fatalf("foreign response = %#v", message)
	}
	assertJSON(t, response.Result, `{"protocolVersion":"2025-06-18"}`)
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
}

// Rust: src/agent-client-protocol/tests/protocol_v2.rs::builder_new_agent_applies_no_version_guard and
// builder_new_with_agent_applies_no_version_guard, version-neutral subcase.
// This verifies transparent version data, not support for V2 protocol methods.
func TestVersionNeutralAgentPreservesPeerVersion(t *testing.T) {
	peer := agentPeer(t, initializeAgent{})
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": 2}})
	response := peer.read(t)
	if response.Error != nil {
		t.Fatalf("version-neutral initialize = %+v", response)
	}
	assertJSON(t, response.Result, `{"protocolVersion":2}`)
}

// Rust: src/agent-client-protocol/tests/protocol_v2.rs::raw_proxy_session_new_returns_results_unchanged.
// Ports the raw v1-relevant result property with explicit ProxySuccessor calls.
// The missing automatic Proxy router/session registration is not recreated.
func TestRawProxySessionResultPreservation(t *testing.T) {
	ctx, client, peer := clientPeer(t, nil)
	for _, scenario := range []struct{ name, response string }{
		{"malformed", `{"_futureResponseField":{"preserved":false}}`},
		{"valid", `{"sessionId":"raw-session","_futureResponseField":{"preserved":true}}`},
	} {
		result := startCall(func() (*acp.ProxySuccessorResponse, error) {
			return client.ProxySuccessor(ctx, &acp.ProxySuccessorRequest{Method: "session/new", Params: map[string]any{"cwd": "/" + scenario.name, "_case": scenario.name}})
		})
		request := peer.read(t)
		if request.Method != "proxy/successor" {
			t.Fatalf("method = %q", request.Method)
		}
		assertJSON(t, request.Params, `{"method":"session/new","params":{"cwd":"/`+scenario.name+`","_case":"`+scenario.name+`"}}`)
		peer.result(t, request, json.RawMessage(scenario.response))
		assertJSON(t, valueOf(t, result), scenario.response)
	}
}

// Rust: src/agent-client-protocol/tests/protocol_v2.rs::send_initialize_reports_v1_selection_as_outcome,
// v1 selection/response subcase only. Go has no V2 InitializeOutcome wrapper or
// fixed-version state machine; those source properties are separately excluded.
func TestInitializeReturnsPeerSelectedV1(t *testing.T) {
	ctx, client, peer := clientPeer(t, nil)
	result := startCall(func() (*acp.InitializeResponse, error) {
		return client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 2})
	})
	request := peer.read(t)
	assertJSON(t, request.Params, `{"protocolVersion":2}`)
	peer.result(t, request, json.RawMessage(`{"protocolVersion":1}`))
	if got := valueOf(t, result).ProtocolVersion; got != 1 {
		t.Fatalf("selected protocol version = %d", got)
	}
}

// Rust: src/agent-client-protocol/tests/protocol_v2.rs::v1_entry_point_client_is_rejected_by_v2_agent,
// protocol_router_v2_only_rejects_v1_client and protocol_router_rejection_is_initialize_request_error.
// Ports the V1 caller's exact peer-error propagation, not automatic V2-only
// negotiation/rejection policy (Go has no V2 builder or ProtocolRouter).
func TestInitializePreservesVersionRejection(t *testing.T) {
	const unsupportedProtocolVersion = -32022 // Upstream V2 peer's error; no V1 Go enum name.
	ctx, client, peer := clientPeer(t, nil)
	result := startCall(func() (*acp.InitializeResponse, error) {
		return client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1})
	})
	request := peer.read(t)
	peer.failure(t, request, unsupportedProtocolVersion, "Unsupported protocol version", map[string]any{"requested": 1, "supported": []int{2}})
	got := await(t, result)
	var remote *jsonrpc.Error
	if !errors.As(got.err, &remote) || remote.Code != unsupportedProtocolVersion || remote.Message != "Unsupported protocol version" {
		t.Fatalf("initialize rejection = %v", got.err)
	}
	assertJSON(t, remote.Data, `{"requested":1,"supported":[2]}`)
}

// Rust: src/agent-client-protocol/tests/proxy_protocol_router_v2.rs::routes_exact_versions_to_their_implementation
// and v2_only_router_rejects_v1_like_a_v2_agent, V1 caller/transport slices.
// Source router selection and V2-only rejection policy are not reimplemented.
func TestV1ProxyInitializeTransport(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		name := "selected_v1"
		if rejected {
			name = "peer_rejects_v1"
		}
		t.Run(name, func(t *testing.T) {
			ctx, client, peer := clientPeer(t, nil)
			result := startCall(func() (*acp.InitializeProxyResponse, error) {
				return client.ProxyInitialize(ctx, &acp.InitializeProxyRequest{ProtocolVersion: 1, ClientCapabilities: &acp.ClientCapabilities{}, ClientInfo: &acp.Implementation{Name: "router-test-client", Version: "1.0.0"}})
			})
			request := peer.read(t)
			if request.Method != "proxy/initialize" {
				t.Fatalf("proxy method = %q", request.Method)
			}
			assertJSON(t, request.Params, `{"protocolVersion":1,"clientCapabilities":{},"clientInfo":{"name":"router-test-client","version":"1.0.0"}}`)
			if !rejected {
				peer.result(t, request, json.RawMessage(`{"protocolVersion":1}`))
				if got := valueOf(t, result).ProtocolVersion; got != 1 {
					t.Fatalf("selected proxy version = %d", got)
				}
				return
			}
			const unsupportedProtocolVersion = -32022
			peer.failure(t, request, unsupportedProtocolVersion, "Unsupported protocol version", map[string]any{"requested": 1, "supported": []int{2}})
			got := await(t, result)
			var remote *jsonrpc.Error
			if !errors.As(got.err, &remote) || remote.Code != unsupportedProtocolVersion {
				t.Fatalf("proxy rejection = %v", got.err)
			}
			assertJSON(t, remote.Data, `{"requested":1,"supported":[2]}`)
		})
	}
}

type proxyInitializeAgent struct{}

func (proxyInitializeAgent) ProxyInitialize(_ context.Context, params *acp.InitializeProxyRequest) (*acp.InitializeProxyResponse, error) {
	return &acp.InitializeProxyResponse{ProtocolVersion: params.ProtocolVersion}, nil
}

// Rust: src/agent-client-protocol/tests/proxy_protocol_router_v2.rs::answers_malformed_frames_before_proxy_initialize_and_keeps_reading.
// Ports the public RPC runtime slice with a real typed ProxyInitialize handler:
// parse error, one invalid-batch response, then successful individual dispatch.
// Automatic forwarding of the complete frame and V1/V2 router choice are absent
// Go APIs and are not manufactured by this fixture's ordinary handler.
func TestMalformedFramesBeforeProxyInitialize(t *testing.T) {
	peer := agentPeer(t, proxyInitializeAgent{})
	peer.sendRaw(t, []byte("{not json\n"))
	response := peer.read(t)
	if string(response.ID) != "null" || response.Error == nil || response.Error.Code != jsonrpc.CodeParseError {
		t.Fatalf("malformed-line response = %+v, want ParseError/null ID", response)
	}
	params := map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}, "clientInfo": map[string]any{"name": "router-test-client", "version": "1.0.0"}, "_futureInitializeField": map[string]any{"preserved": true}}
	peer.send(t, []any{map[string]any{"jsonrpc": "2.0", "id": 7, "method": "proxy/initialize", "params": params}})
	response = peer.read(t)
	if string(response.ID) != "null" || response.Error == nil || response.Error.Code != jsonrpc.CodeInvalidRequest {
		t.Fatalf("batch response = %+v, want one InvalidRequest/null ID", response)
	}
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "proxy/initialize", "params": params})
	response = peer.read(t)
	if string(response.ID) != "1" || response.Error != nil {
		t.Fatalf("individual proxy initialize after errors = %+v", response)
	}
	assertJSON(t, response.Result, `{"protocolVersion":1}`)
}

// Rust: src/agent-client-protocol/tests/protocol_v2.rs::protocol_router_ignores_retained_standalone_response_before_initialize.
// Ports ignoring the unsolicited response and then serving V1 initialize;
// no routing/normalization layer is supplied by the test fixture.
func TestStrayResponseBeforeInitialize(t *testing.T) {
	peer := agentPeer(t, initializeAgent{})
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 99, "result": nil, "error": map[string]any{"code": jsonrpc.CodeInternalError, "message": "Internal error"}})
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": 1}})
	response := peer.read(t)
	if string(response.ID) != "1" || response.Error != nil {
		t.Fatalf("initialize after stray response = %+v", response)
	}
	assertJSON(t, response.Result, `{"protocolVersion":1}`)
}

type cookbookAgent struct{ connection *acp.AgentConnection }

func (a *cookbookAgent) Initialize(_ context.Context, request *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: request.ProtocolVersion, AgentCapabilities: &acp.AgentCapabilities{}}, nil
}
func (a *cookbookAgent) SessionNew(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: "session-1"}, nil
}
func (a *cookbookAgent) SessionPrompt(ctx context.Context, request *acp.PromptRequest) (*acp.PromptResponse, error) {
	for _, text := range []string{"Hello, ", "world!"} {
		if err := a.connection.SessionUpdate(ctx, &acp.SessionNotification{SessionID: request.SessionID, Update: acp.AgentMessageChunkSessionUpdate(acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: text})}); err != nil {
			return nil, err
		}
	}
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// Rust src/agent-client-protocol-cookbook/src/lib.rs doctests at446 (basic agent) and488 (two streaming
// chunks), plus v1 initialize/new/prompt controls shared by the client recipes.
func TestCookbookAgentLifecycleAndStreaming(t *testing.T) {
	peer := agentFactoryPeer(t, func(connection *acp.AgentConnection) any { return &cookbookAgent{connection: connection} })
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": 1}})
	assertJSON(t, peer.read(t).Result, `{"protocolVersion":1,"agentCapabilities":{}}`)
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "session/new", "params": map[string]any{"cwd": "/tmp", "mcpServers": []any{}}})
	assertJSON(t, peer.read(t).Result, `{"sessionId":"session-1"}`)
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "session/prompt", "params": map[string]any{"sessionId": "session-1", "prompt": []any{map[string]any{"type": "text", "text": "Hello, agent!"}}}})
	for _, text := range []string{"Hello, ", "world!"} {
		message := peer.read(t)
		if message.Method != "session/update" || len(message.ID) != 0 {
			t.Fatalf("expected streamed notification, got %+v", message)
		}
		expected, _ := json.Marshal(map[string]any{"sessionId": "session-1", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}}})
		assertJSON(t, message.Params, string(expected))
	}
	assertJSON(t, peer.read(t).Result, `{"stopReason":"end_turn"}`)
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "_rust/unknown", "params": map[string]any{}})
	response := peer.read(t)
	if response.Error == nil || response.Error.Code != jsonrpc.CodeMethodNotFound {
		t.Fatalf("unknown request response = %+v", response)
	}
	peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "_rust/unhandled", "params": map[string]any{}})
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 5, "method": "session/new", "params": map[string]any{"cwd": "/tmp", "mcpServers": []any{}}})
	response = peer.read(t)
	if string(response.ID) != "5" {
		t.Fatalf("unhandled notification unexpectedly produced a reply: %+v", response)
	}
}
