// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// codeUnsupportedProtocolVersion is a peer-defined error code with no
// JSON-RPC or ACP name; the SDK must pass it through untouched.
const codeUnsupportedProtocolVersion = -32022

const traceJSON = `{"traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0b902b7-01","tracestate":"rojo=00f067aa0b902b7","baggage":"tenant=acme"}`

func traceMeta() acp.Meta {
	return acp.Meta{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0b902b7-01",
		"tracestate":  "rojo=00f067aa0b902b7",
		"baggage":     "tenant=acme",
	}
}

func metaPrompt(meta acp.Meta) acp.PromptRequest {
	return acp.PromptRequest{SessionID: "session-1", Prompt: []acp.ContentBlock{acp.TextContentBlock("hello")}, Meta: meta}
}

// fields encodes v and returns its top-level members.
func fields(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(acptest.Marshal(t, v), &object); err != nil {
		t.Fatal(err)
	}
	return object
}

// expectRequest fails unless the next frame is a method request whose params
// equal want, and returns it.
func expectRequest(t *testing.T, peer *acptest.Peer, method string, want []byte) acptest.Message {
	t.Helper()
	request := peer.Expect(method)
	acptest.EqualJSON(t, request.Params, string(want))
	return request
}

// TestPromptMeta checks that a prompt's _meta encodes under the reserved
// "_meta" key, decodes back to the same values, and is sent unchanged.
func TestPromptMeta(t *testing.T) {
	request := metaPrompt(traceMeta())
	object := fields(t, request)
	if _, ok := object["meta"]; ok {
		t.Error(`metadata encoded under "meta"`)
	}
	acptest.EqualJSON(t, object["_meta"], traceJSON)
	data := acptest.Marshal(t, request)
	var decoded acp.PromptRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Meta, traceMeta()) {
		t.Fatalf("decoded _meta = %#v", decoded.Meta)
	}

	client, peer := acptest.StartClient(t, nil)
	result := acptest.Go(func() (*acp.PromptResponse, error) { return client.SessionPrompt(context.Background(), &request) })
	endTurn(peer, expectRequest(t, peer, "session/prompt", data))
	acptest.Value(t, result)
}

// TestSuccessorMeta checks that a proxy/successor envelope and the message it
// wraps keep their own _meta through encoding, decoding and the wire.
func TestSuccessorMeta(t *testing.T) {
	request := acp.ProxySuccessorRequest{Method: "session/prompt", Params: metaPrompt(traceMeta()), Meta: traceMeta()}
	if _, ok := fields(t, request)["meta"]; ok {
		t.Error(`envelope metadata encoded under "meta"`)
	}
	data := acptest.Marshal(t, request)
	var decoded acp.ProxySuccessorRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Method != "session/prompt" || !reflect.DeepEqual(decoded.Meta, traceMeta()) {
		t.Fatalf("decoded envelope = %#v", decoded)
	}
	var inner acp.PromptRequest
	if err := json.Unmarshal(acptest.Marshal(t, decoded.Params), &inner); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inner.Meta, traceMeta()) {
		t.Fatalf("inner _meta = %#v", inner.Meta)
	}

	client, peer := acptest.StartClient(t, nil)
	result := acptest.Go(func() (*acp.ProxySuccessorResponse, error) {
		return client.ProxySuccessor(context.Background(), &request)
	})
	peer.Reply(expectRequest(t, peer, "proxy/successor", data).ID, map[string]any{})
	acptest.Value(t, result)
}

// TestSuccessorIgnoresInvalidMeta checks that decoding an envelope ignores
// metadata under a non-reserved key or with a non-object value, rather than
// failing or keeping it.
func TestSuccessorIgnoresInvalidMeta(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]json.RawMessage)
	}{
		{"non_reserved_key", func(o map[string]json.RawMessage) { o["meta"] = o["_meta"]; delete(o, "_meta") }},
		{"non_object", func(o map[string]json.RawMessage) { o["_meta"] = json.RawMessage(`"not an object"`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := fields(t, acp.ProxySuccessorRequest{Method: "session/prompt", Params: metaPrompt(acp.Meta{}), Meta: traceMeta()})
			tc.edit(object)
			var decoded acp.ProxySuccessorRequest
			if err := json.Unmarshal(acptest.Marshal(t, object), &decoded); err != nil {
				t.Fatalf("decoding: %v", err)
			}
			if decoded.Meta != nil {
				t.Fatalf("invalid _meta kept: %#v", decoded.Meta)
			}
		})
	}
}

// TestMCPMessageMeta checks that an mcp/message carrier's _meta stays separate
// from the _meta inside the MCP params it carries, through encoding, decoding
// and an agent-side call.
func TestMCPMessageMeta(t *testing.T) {
	const params = `{"cursor":"abc","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"example/inner":"MCP metadata is independent of ACP carrier metadata"}}`
	request := acp.MessageMcpRequest{ServerID: "server-1", RequestID: "request-1", Method: "tools/list", Meta: traceMeta()}
	if err := json.Unmarshal([]byte(params), &request.Params); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields(t, request)["meta"]; ok {
		t.Error(`carrier metadata encoded under "meta"`)
	}
	data := acptest.Marshal(t, request)
	var decoded acp.MessageMcpRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ServerID != "server-1" || decoded.RequestID != "request-1" || decoded.Method != "tools/list" || !reflect.DeepEqual(decoded.Meta, traceMeta()) {
		t.Fatalf("decoded carrier = %#v", decoded)
	}
	acptest.EqualMarshaled(t, decoded.Params, params)

	agent, peer := acptest.StartAgent(t, nil)
	result := acptest.Go(func() (*acp.MessageMcpResponse, error) { return agent.Conn.McpMessage(context.Background(), &request) })
	peer.Reply(expectRequest(t, peer, "mcp/message", data).ID, json.RawMessage(`{"result":null}`))
	acptest.Value(t, result)
}

// TestMCPResponse checks that mcp/message results are carried opaquely and
// that MCP errors keep their code, message, any data (absent, null or an
// object) and unknown fields, both when built with the constructor and when
// decoded and re-encoded.
func TestMCPResponse(t *testing.T) {
	acptest.EqualMarshaled(t, acp.ResultMessageMcpResponse(nil), `{"result":null}`)
	acptest.EqualMarshaled(t, acp.ResultMessageMcpResponse(map[string]any{"opaque": true}), `{"result":{"opaque":true}}`)
	for _, tc := range []struct {
		name string
		data json.RawMessage
	}{
		{"data_absent", nil}, {"data_null", json.RawMessage(`null`)}, {"data_object", json.RawMessage(`{"cause":"peer failure"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mcpError := map[string]any{"code": codeUnsupportedProtocolVersion, "message": "peer", "extension": map[string]any{"retry": false}}
			if tc.data != nil {
				mcpError["data"] = tc.data
			}
			want := string(acptest.Marshal(t, map[string]any{"error": mcpError}))

			var converted acp.McpError
			if err := json.Unmarshal(acptest.Marshal(t, mcpError), &converted); err != nil {
				t.Fatal(err)
			}
			acptest.EqualMarshaled(t, acp.ErrorMessageMcpResponse(converted), want)

			acptest.RoundTrip(t, want, new(acp.MessageMcpResponse))
		})
	}
}

// TestRawErrorResponseFields checks that a decoded error response keeps its
// code, message, data (absent, null or an object) and unknown error fields
// when re-encoded, including after its ID is replaced.
func TestRawErrorResponseFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		data json.RawMessage
	}{
		{"data_absent", nil}, {"data_null", json.RawMessage(`null`)}, {"data_object", json.RawMessage(`{"detail":"kept"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wireError := map[string]any{"code": acp.ErrorCodeAuthenticationRequired, "message": "peer", "extension": map[string]any{"retry": true}}
			if tc.data != nil {
				wireError["data"] = tc.data
			}
			frame := map[string]any{"jsonrpc": "2.0", "id": "original", "error": wireError}
			decoded, err := jsonrpc.DecodeMessage(acptest.Marshal(t, frame))
			if err != nil {
				t.Fatal(err)
			}
			response, ok := decoded.(*jsonrpc.Response)
			if !ok {
				t.Fatalf("decoded %T, want *jsonrpc.Response", decoded)
			}
			for _, id := range []string{"original", "replacement"} {
				if response.ID, err = jsonrpc.MakeID(id); err != nil {
					t.Fatal(err)
				}
				encoded, err := jsonrpc.EncodeMessage(response)
				if err != nil {
					t.Fatal(err)
				}
				frame["id"] = id
				acptest.EqualJSON(t, encoded, string(acptest.Marshal(t, frame)))
			}
		})
	}
}
