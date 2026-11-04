// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
package session_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

func traceMeta() acp.Meta {
	return acp.Meta{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0b902b7-01",
		"tracestate":  "rojo=00f067aa0b902b7",
		"baggage":     "tenant=acme",
	}
}

func promptWithMeta(meta acp.Meta) acp.PromptRequest {
	return acp.PromptRequest{SessionID: "session-1", Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "hello"}}, Meta: meta}
}

func jsonObject(t *testing.T, value any) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

// Rust: src/agent-client-protocol/tests/meta_propagation.rs::prompt_request_meta_round_trips_with_root_trace_context_keys.
func TestPromptTraceContextRoundTrip(t *testing.T) {
	request := promptWithMeta(traceMeta())
	object := jsonObject(t, request)
	if _, exists := object["meta"]; exists {
		t.Fatal("metadata encoded under non-spec meta key")
	}
	assertJSON(t, object["_meta"], `{"traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0b902b7-01","tracestate":"rojo=00f067aa0b902b7","baggage":"tenant=acme"}`)
	data, _ := json.Marshal(request)
	var decoded acp.PromptRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Meta, traceMeta()) {
		t.Fatalf("decoded metadata = %#v", decoded.Meta)
	}
	ctx, client, peer := clientPeer(t, nil)
	call := startCall(func() (*acp.PromptResponse, error) { return client.SessionPrompt(ctx, &request) })
	wire := peer.read(t)
	if wire.Method != "session/prompt" {
		t.Fatalf("prompt method = %q", wire.Method)
	}
	assertJSON(t, wire.Params, string(data))
	peer.result(t, wire, map[string]any{"stopReason": "end_turn"})
	valueOf(t, call)
}

// Rust: src/agent-client-protocol/tests/meta_propagation.rs::successor_message_meta_serializes_as_reserved_meta_field.
func TestSuccessorOuterAndInnerMetadata(t *testing.T) {
	request := acp.ProxySuccessorRequest{Method: "session/prompt", Params: promptWithMeta(traceMeta()), Meta: traceMeta()}
	object := jsonObject(t, request)
	if _, exists := object["meta"]; exists {
		t.Fatal("envelope metadata encoded under meta")
	}
	var decoded acp.ProxySuccessorRequest
	data, _ := json.Marshal(request)
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Method != "session/prompt" || !reflect.DeepEqual(decoded.Meta, traceMeta()) {
		t.Fatalf("decoded successor = %#v", decoded)
	}
	inner, _ := json.Marshal(decoded.Params)
	var prompt acp.PromptRequest
	if err := json.Unmarshal(inner, &prompt); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prompt.Meta, traceMeta()) {
		t.Fatalf("inner metadata = %#v", prompt.Meta)
	}
	ctx, client, peer := clientPeer(t, nil)
	call := startCall(func() (*acp.ProxySuccessorResponse, error) { return client.ProxySuccessor(ctx, &request) })
	wire := peer.read(t)
	if wire.Method != "proxy/successor" {
		t.Fatalf("successor method = %q", wire.Method)
	}
	assertJSON(t, wire.Params, string(data))
	peer.result(t, wire, map[string]any{})
	valueOf(t, call)
}

// Rust: src/agent-client-protocol/tests/meta_propagation.rs::successor_message_ignores_non_spec_meta_key.
func TestSuccessorIgnoresNonSpecMetadataKey(t *testing.T) {
	object := jsonObject(t, acp.ProxySuccessorRequest{Method: "session/prompt", Params: promptWithMeta(acp.Meta{}), Meta: traceMeta()})
	object["meta"] = object["_meta"]
	delete(object, "_meta")
	data, _ := json.Marshal(object)
	var decoded acp.ProxySuccessorRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Meta != nil {
		t.Fatalf("non-spec meta was accepted: %#v", decoded.Meta)
	}
}

// Rust: src/agent-client-protocol/tests/meta_propagation.rs::successor_message_ignores_non_object_meta.
// This is the upstream tolerant-metadata policy, not a general strictness claim.
func TestSuccessorIgnoresNonObjectMetadata(t *testing.T) {
	object := jsonObject(t, acp.ProxySuccessorRequest{Method: "session/prompt", Params: promptWithMeta(acp.Meta{})})
	object["_meta"] = json.RawMessage(`"not an object"`)
	data, _ := json.Marshal(object)
	var decoded acp.ProxySuccessorRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("upstream expects non-object carrier metadata ignored: %v", err)
	}
	if decoded.Meta != nil {
		t.Fatalf("invalid metadata = %#v", decoded.Meta)
	}
}

// Rust: src/agent-client-protocol/tests/meta_propagation.rs::native_mcp_over_acp_message_meta_serializes_as_reserved_meta_field
// (unstable_mcp_over_acp is a v1 optional feature, not protocol v2).
func TestNativeMCPMetadataIsSeparateFromCarrier(t *testing.T) {
	inner := map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		"example/inner": "MCP metadata is independent of ACP carrier metadata",
	}
	request := acp.MessageMcpRequest{ServerID: "server-1", RequestID: "request-1", Method: "tools/list", Params: map[string]any{"cursor": "abc", "_meta": inner}, Meta: traceMeta()}
	object := jsonObject(t, request)
	if _, exists := object["meta"]; exists {
		t.Fatal("carrier used non-spec meta key")
	}
	data, _ := json.Marshal(request)
	var decoded acp.MessageMcpRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ServerID != "server-1" || decoded.RequestID != "request-1" || decoded.Method != "tools/list" || !reflect.DeepEqual(decoded.Meta, traceMeta()) {
		t.Fatalf("MCP carrier = %#v", decoded)
	}
	assertJSON(t, decoded.Params, `{"cursor":"abc","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"example/inner":"MCP metadata is independent of ACP carrier metadata"}}`)
	ready := make(chan *acp.AgentConnection, 1)
	peer := agentFactoryPeer(t, func(connection *acp.AgentConnection) any { ready <- connection; return nil })
	connection := await(t, ready)
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	t.Cleanup(cancel)
	call := startCall(func() (*acp.MessageMcpResponse, error) { return connection.McpMessage(ctx, &request) })
	wire := peer.read(t)
	if wire.Method != "mcp/message" {
		t.Fatalf("MCP method = %q", wire.Method)
	}
	assertJSON(t, wire.Params, string(data))
	peer.result(t, wire, json.RawMessage(`{"result":null}`))
	valueOf(t, call)
}

// Rust: src/agent-client-protocol/src/role/acp.rs::raw_initialize_tests::initialize_error_forwarding_preserves_raw_fields.
// The source's protocol connector uses a raw initialize response. Go exposes raw
// message helpers but not that connector; this ports only the raw-value fidelity
// policy, including absent/null/object data and unknown error extension fields.
func TestRawInitializeErrorPreservesFields(t *testing.T) {
	for _, scenario := range []struct {
		name string
		data json.RawMessage
	}{
		{"absent", nil}, {"null", json.RawMessage(`null`)}, {"object", json.RawMessage(`{"detail":"kept"}`)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			errorValue := map[string]any{"code": acp.ErrorCodeAuthenticationRequired, "message": "peer", "extension": map[string]any{"retry": true}}
			if scenario.data != nil {
				errorValue["data"] = scenario.data
			}
			wire := map[string]any{"jsonrpc": "2.0", "id": "original", "error": errorValue}
			data, _ := json.Marshal(wire)
			decoded, err := jsonrpc.DecodeMessage(data)
			if err != nil {
				t.Fatal(err)
			}
			response, ok := decoded.(*jsonrpc.Response)
			if !ok {
				t.Fatalf("decoded error type = %T", decoded)
			}
			for _, id := range []string{"original", "replacement"} {
				response.ID, err = jsonrpc.MakeID(id)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := jsonrpc.EncodeMessage(response)
				if err != nil {
					t.Fatal(err)
				}
				wire["id"] = id
				expected, _ := json.Marshal(wire)
				assertJSON(t, json.RawMessage(encoded), string(expected))
			}
		})
	}
}
