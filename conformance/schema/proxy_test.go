// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

func TestProxyMethodNames(t *testing.T) {
	if acp.MethodProxyInitialize != "proxy/initialize" || acp.MethodProxySuccessor != "proxy/successor" {
		t.Errorf("proxy method names = %q, %q", acp.MethodProxyInitialize, acp.MethodProxySuccessor)
	}
}

// TestProxyInitializeMatchesInitialize checks that proxy/initialize uses the
// same payloads as initialize, with the same validation: a missing or null
// protocol version is rejected and invalid _meta is dropped. Default-valued
// capability fields are omitted.
func TestProxyInitializeMatchesInitialize(t *testing.T) {
	const literal = `{"protocolVersion":1}`
	acptest.RoundTrip(t, literal, new(acp.InitializeRequest))
	var response acp.InitializeProxyResponse
	acptest.RoundTrip(t, literal, &response)
	acptest.EqualMarshaled(t, acp.InitializeResponse(response), literal)

	newRequest := alloc[acp.InitializeProxyRequest]
	runDecodeCases(t, []decodeCase{
		{name: "missing_protocol_version", new: newRequest, input: `{}`, reject: true},
		{name: "null_protocol_version", new: newRequest, input: `{"protocolVersion":null}`, reject: true},
		{name: "invalid_meta_dropped", new: newRequest, input: `{"protocolVersion":1,"_meta":"bad"}`, want: literal},
		{name: "response_missing_protocol_version", new: alloc[acp.InitializeProxyResponse], input: `{}`, reject: true},
	})
}

// TestSuccessorEnvelope checks the proxy/successor envelope: its params hold
// the wrapped message unchanged, its _meta stays separate from the wrapped
// message's, and only a missing method is invalid.
func TestSuccessorEnvelope(t *testing.T) {
	t.Run("wrapped_prompt", func(t *testing.T) {
		const literal = `{"method":"session/prompt","params":{"sessionId":"s","prompt":[]},"_meta":{"outer":true}}`
		const prompt = `{"sessionId":"s","prompt":[]}`
		var envelope acp.ProxySuccessorRequest
		acptest.RoundTrip(t, literal, &envelope)
		if envelope.Method != "session/prompt" || envelope.Meta["outer"] != true {
			t.Fatalf("successor envelope = %#v", envelope)
		}
		acptest.EqualMarshaled(t, envelope.Params, prompt)
		var wrapped acp.PromptRequest
		decode(t, string(acptest.Marshal(t, envelope.Params)), &wrapped)
		acptest.EqualMarshaled(t, wrapped, prompt)
		if wrapped.SessionID != "s" || wrapped.Prompt == nil || len(wrapped.Prompt) != 0 {
			t.Fatalf("wrapped prompt = %#v", wrapped)
		}
		client, peer := startClient(t)
		expectCall(t, peer, "proxy/successor", literal, `{}`, func() (*acp.ProxySuccessorResponse, error) {
			return client.ProxySuccessor(t.Context(), &envelope)
		})
	})
	t.Run("omitted_params_and_invalid_meta", func(t *testing.T) {
		// Invalid _meta falls back to absent instead of rejecting the envelope.
		var envelope acp.ProxySuccessorNotification
		decode(t, `{"method":"_vendor/note","_meta":"ignored"}`, &envelope)
		if envelope.Method != "_vendor/note" || envelope.Params != nil || envelope.Meta != nil {
			t.Fatalf("successor envelope = %#v", envelope)
		}
	})
	for _, tt := range []struct{ name, params string }{
		{"missing_method", `{}`},
		{"missing_method_with_params", `{"params":{}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRecorder()
			peer, settle := serve(t, agentSide, rec)
			expectError(t, peer, "proxy/successor", tt.params, jsonrpc.CodeInvalidParams)
			settle()
			rec.expectNone(t)
		})
	}
}
