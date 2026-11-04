// SPDX-License-Identifier: Apache-2.0
// Upstream: agentclientprotocol/rust-sdk@050cd94c081056ace37a7b89abfed7f6220b47eb.
// Rust source: src/agent-client-protocol/tests/proxy_schema_types.rs.
package schema_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// proxy_schema_types.rs::v1_initialize_proxy_round_trips:
// proxy initialization is the same V1 payload/result, with the proxy method route.
// Go omits default-valued capability fields; the literal uses their equivalent
// omitted representation, rather than importing Rust's serializer default layout.
func TestProxyInitializeRoundTrips(t *testing.T) {
	const literal = `{"protocolVersion":1}`
	var req acp.InitializeProxyRequest
	roundTrip(t, literal, &req)
	var init acp.InitializeRequest
	roundTrip(t, literal, &init)
	ctx, client, peer := rawClient(t, nil)
	res := exchange(t, ctx, peer, "proxy/initialize", literal, literal, func() (any, error) { return client.ProxyInitialize(ctx, &req) })
	assertJSON(t, encode(t, res), literal)
	var proxyResponse acp.InitializeProxyResponse
	roundTrip(t, literal, &proxyResponse)
	assertJSON(t, encode(t, acp.InitializeResponse(proxyResponse)), literal)
	incomingRequest(t, true, "proxy/initialize", literal, literal)
}

// proxy_schema_types.rs::v1_proxy_successor_round_trips:
// both envelope fixtures, request/notification shapes, aggregate routes, and an
// arbitrary unchanged response. Both Go sides expose the shared proxy routes.
func TestProxySuccessorRoundTrips(t *testing.T) {
	for _, tt := range []struct{ name, envelope string }{
		{"prompt", `{"method":"session/prompt","params":{"sessionId":"s","_meta":{"inner":1}},"_meta":{"outer":1}}`},
		{"cancel_without_params", `{"method":"session/cancel"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var req acp.ProxySuccessorRequest
			var n acp.ProxySuccessorNotification
			roundTrip(t, tt.envelope, &req)
			roundTrip(t, tt.envelope, &n)
			const result = `{"zeta":1,"alpha":[null,1.5],"_meta":{"k":"v"}}`
			ctx, client, peer := rawClient(t, nil)
			res := exchange(t, ctx, peer, "proxy/successor", tt.envelope, result, func() (any, error) { return client.ProxySuccessor(ctx, &req) })
			assertJSON(t, encode(t, res), result)
			notification(t, ctx, peer, "proxy/successor", tt.envelope, func() error { return client.ProxySuccessorNotification(ctx, &n) })
			ctx, conn, agentPeer := rawAgent(t, nil)
			res = exchange(t, ctx, agentPeer, "proxy/successor", tt.envelope, result, func() (any, error) { return conn.ProxySuccessor(ctx, &req) })
			assertJSON(t, encode(t, res), result)
			notification(t, ctx, agentPeer, "proxy/successor", tt.envelope, func() error { return conn.ProxySuccessorNotification(ctx, &n) })
			for _, agentSide := range []bool{true, false} {
				incomingRequest(t, agentSide, "proxy/successor", tt.envelope, result)
				incomingNotification(t, agentSide, "proxy/successor", tt.envelope)
			}
		})
	}
	t.Run("missing_method", func(t *testing.T) {
		expectInvalidParams(t, "proxy/successor", `{"params":{}}`, successorReceiver{})
	})
}

type successorReceiver struct{}

func (successorReceiver) ProxySuccessor(context.Context, *acp.ProxySuccessorRequest) (*acp.ProxySuccessorResponse, error) {
	var result acp.ProxySuccessorResponse = map[string]any{}
	return &result, nil
}

var _ acp.ProxySuccessorHandler = successorReceiver{}

// proxy_schema_types.rs::successor_message_matches_schema_envelope:
// the raw envelope counterpart retains outer metadata separately from typed
// prompt params; omitted inner params and malformed outer metadata are tolerated.
// Go has no generic SuccessorMessage<M>; its generic response association is
// explicitly classified separately in mapping.json, not simulated by a fixture.
func TestSuccessorEnvelopeSemantics(t *testing.T) {
	t.Run("typed_prompt_and_outer_meta", func(t *testing.T) {
		const literal = `{"method":"session/prompt","params":{"sessionId":"s","prompt":[]},"_meta":{"outer":true}}`
		var envelope acp.ProxySuccessorRequest
		roundTrip(t, literal, &envelope)
		if envelope.Method != "session/prompt" || envelope.Meta["outer"] != true {
			t.Fatalf("successor envelope = %#v", envelope)
		}
		const promptLiteral = `{"sessionId":"s","prompt":[]}`
		assertJSON(t, encode(t, envelope.Params), promptLiteral)
		var prompt acp.PromptRequest
		if err := json.Unmarshal(encode(t, envelope.Params), &prompt); err != nil {
			t.Fatal(err)
		}
		assertJSON(t, encode(t, prompt), promptLiteral)
		if prompt.SessionID != "s" || prompt.Prompt == nil || len(prompt.Prompt) != 0 {
			t.Fatalf("wrapped prompt = %#v", prompt)
		}
		ctx, client, peer := rawClient(t, nil)
		exchange(t, ctx, peer, "proxy/successor", literal, `{}`, func() (any, error) { return client.ProxySuccessor(ctx, &envelope) })
	})
	t.Run("omitted_params_and_invalid_meta", func(t *testing.T) {
		var envelope acp.ProxySuccessorNotification
		if err := json.Unmarshal([]byte(`{"method":"_vendor/note","_meta":"ignored"}`), &envelope); err != nil {
			t.Fatalf("upstream accepts this envelope and defaults only metadata: %v", err)
		}
		if envelope.Method != "_vendor/note" || envelope.Params != nil || envelope.Meta != nil {
			t.Fatalf("untyped envelope = %#v", envelope)
		}
	})
	t.Run("missing_method", func(t *testing.T) {
		expectInvalidParams(t, "proxy/successor", `{}`, successorReceiver{})
	})
}

// proxy_schema_types.rs::proxy_method_names_come_from_the_schema:
// generated public method names; actual typed routes are exercised above.
func TestProxyMethodNames(t *testing.T) {
	if acp.MethodProxyInitialize != "proxy/initialize" || acp.MethodProxySuccessor != "proxy/successor" {
		t.Fatalf("proxy method names = %q, %q", acp.MethodProxyInitialize, acp.MethodProxySuccessor)
	}
}
