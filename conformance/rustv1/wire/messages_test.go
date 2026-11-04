package wire_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

var ids = []struct{ name, wire string }{
	{"zero", "0"}, {"one", "1"}, {"negative", "-1"},
	{"above_f64", "9007199254740993"}, {"max", "9223372036854775807"},
	{"min", "-9223372036854775808"}, {"numeric_string", `"1"`},
	{"empty_string", `""`}, {"string", `"req-1"`},
	{"unicode", `"ünïcødé \"quoted\""`}, {"null", "null"},
}

func requestIDPeer(t *testing.T) *peer {
	t.Helper()
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/echo", func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	}))
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/callback", func(ctx context.Context, _ *struct{}) (*json.RawMessage, error) {
		r, err := a.conn.CallExtension(ctx, "_test/echo", struct{}{})
		return &r, err
	}))
	return agentPeer(t, a, true)
}

func exchangeRequestID(t *testing.T, p *peer, direction, id string) {
	t.Helper()
	p.send(t, request(id, "_test/"+direction, `{}`))
	if direction == "callback" {
		r := p.receive(t)
		b, err := json.Marshal(r)
		check(t, err)
		equalJSON(t, b, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"_test/echo","params":{}}`, r["id"]))
		// Go chooses integer IDs rather than Rust's UUID strings. Echo its
		// chosen ID verbatim, testing correlation rather than ID allocation.
		p.send(t, `{"jsonrpc":"2.0","id":`+string(r["id"])+`,"result":{}}`)
	}
	r := p.receive(t)
	b, err := json.Marshal(r)
	check(t, err)
	equalJSON(t, b, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{}}`, id))
}

// Rust src/agent-client-protocol/tests/request_id_wire_format.rs::v1_request_ids_round_trip_byte_for_byte:
// initialize once, then 11 echoes, 11 callbacks, 3 invalid-ID errors and clean
// EOF on the SAME peer. Isolate the complete sequence so a null-ID SDK panic
// cannot terminate other tests. A failed phase stops this source sequence.
func TestRequestIDWireSequence(t *testing.T) {
	if !isolated(t) {
		return
	}
	p := requestIDPeer(t)
	for _, direction := range []string{"echo", "callback"} {
		for _, id := range ids {
			if !t.Run(direction+"/"+id.name, func(t *testing.T) {
				exchangeRequestID(t, p, direction, id.wire)
			}) {
				return
			}
		}
	}
	if !t.Run("invalid_ids", func(t *testing.T) { assertInvalidRequestIDs(t, p) }) {
		return
	}
	// The source ends here: do not add a new RPC after the final invalid ID.
	p.finish(t)
}

// Per-ID diagnostics for the same upstream scenarios. These retain useful
// independent failure reports when the complete source sequence stops early;
// they are not a substitute for TestRequestIDWireSequence's shared state.
func TestRequestIDWireFormat(t *testing.T) {
	for _, direction := range []string{"echo", "callback"} {
		for _, id := range ids {
			t.Run(direction+"/"+id.name, func(t *testing.T) {
				if !isolated(t) {
					return
				}
				exchangeRequestID(t, requestIDPeer(t), direction, id.wire)
			})
		}
	}
}

// Rust src/agent-client-protocol/src/jsonrpc/raw_id.rs::wire_form_round_trips_exactly and
// v1_conversions_are_lossless_and_match_the_wire, including 1 != "1".
func TestRawIDRoundTrip(t *testing.T) {
	for _, id := range ids {
		t.Run(id.name, func(t *testing.T) {
			input := request(id.wire, "_test/echo", `{}`)
			m, err := jsonrpc.DecodeMessage([]byte(input))
			check(t, err)
			r, ok := m.(*jsonrpc.Request)
			if !ok || !r.IsCall() {
				t.Fatalf("an explicitly present v1 ID must denote a request: %#v", m)
			}
			out, err := jsonrpc.EncodeMessage(m)
			check(t, err)
			equalJSON(t, out, input)
			// Go's schema RequestId is raw JSON rather than a Rust enum. Its
			// public payload serialization must agree with the envelope value.
			var payload acp.RequestId = json.RawMessage(id.wire)
			b, err := json.Marshal(payload)
			check(t, err)
			equalJSON(t, b, id.wire)
		})
	}
}

// Rust src/agent-client-protocol/src/jsonrpc/raw_id.rs::non_ids_are_rejected. Rejection of exponent-form 1e3 is
// Rust's signed-integer representation policy, not a general JSON-RPC MUST.
func TestNonIDsRejected(t *testing.T) {
	for _, value := range []string{"9223372036854775808", "-9223372036854775809", "1.5", "1e3", "true", "[]", "{}"} {
		t.Run(value, func(t *testing.T) {
			if _, err := jsonrpc.DecodeMessage([]byte(request(value, "_test/echo", `{}`))); err == nil {
				t.Errorf("unsupported Rust-v1 ID accepted: %s", value)
			}
		})
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_edge_cases.rs::raw_jsonrpc_message_rejects_scalar_params and
// raw_jsonrpc_message_rejects_conflicting_envelope_fields; src/agent-client-protocol/src/jsonrpc/transport_actor.rs
// preserves_malformed_{standalone_response,call_shaped_standalone_message}.
func TestRawMessageShape(t *testing.T) {
	for _, tc := range []struct{ name, wire string }{
		{"scalar_request", `{"jsonrpc":"2.0","id":1,"method":"scalar_params","params":1}`},
		{"scalar_notification", `{"jsonrpc":"2.0","method":"scalar_params","params":true}`},
		{"request_result", `{"jsonrpc":"2.0","id":1,"method":"request_with_result","result":{}}`},
		{"response_both", fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":%d,"message":"Internal error"}}`, upstreamInternalErrorCode)},
		{"notification_error", fmt.Sprintf(`{"jsonrpc":"2.0","method":"notification_with_error","error":{"code":%d,"message":"Internal error"}}`, upstreamInternalErrorCode)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := jsonrpc.DecodeMessage([]byte(tc.wire)); err == nil {
				t.Errorf("conflicting/invalid envelope accepted: %s", tc.wire)
			}
		})
	}
	t.Run("scalar_result", func(t *testing.T) {
		input := `{"jsonrpc":"2.0","id":1,"result":1}`
		m, err := jsonrpc.DecodeMessage([]byte(input))
		check(t, err)
		b, err := jsonrpc.EncodeMessage(m)
		check(t, err)
		equalJSON(t, b, input)
	})
}

// Rust src/agent-client-protocol/src/jsonrpc.rs::raw_notification_round_trips_and_drops_unknown_envelope_members.
func TestRawNotificationRoundTrip(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"_vendor/ping","params":{"n":1},"extra":true}`
	m, err := jsonrpc.DecodeMessage([]byte(input))
	check(t, err)
	r, ok := m.(*jsonrpc.Request)
	if !ok || r.IsCall() {
		t.Fatalf("notification decoded as %#v", m)
	}
	b, err := jsonrpc.EncodeMessage(m)
	check(t, err)
	equalJSON(t, b, `{"jsonrpc":"2.0","method":"_vendor/ping","params":{"n":1}}`)
	b, err = jsonrpc.EncodeMessage(&jsonrpc.Request{Method: "ping"})
	check(t, err)
	equalJSON(t, b, `{"jsonrpc":"2.0","method":"ping"}`)
}

// Rust src/agent-client-protocol/src/jsonrpc/transport_actor.rs::parses_valid_standalone_response.
func TestValidStandaloneResponse(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`
	m, err := jsonrpc.DecodeMessage([]byte(input))
	check(t, err)
	if _, ok := m.(*jsonrpc.Response); !ok {
		t.Fatalf("decoded response as %T", m)
	}
	b, err := jsonrpc.EncodeMessage(m)
	check(t, err)
	equalJSON(t, b, input)
}
