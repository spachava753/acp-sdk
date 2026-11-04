package wire_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust src/agent-client-protocol/src/jsonrpc/raw_error.rs::raw_errors_preserve_codes_data_presence_and_extensions.
// Preserve all 6 code x 3 data-presence combinations, including raw extras.
func TestRawErrorsPreserveFields(t *testing.T) {
	for _, n := range []int64{math.MinInt32, upstreamAuthRequiredCode, upstreamInvalidParamsCode, upstreamZeroErrorCode, customCode, math.MaxInt32} {
		for _, data := range []string{"", `,"data":null`, `,"data":{"detail":[1,2]}`} {
			input := fmt.Sprintf(`{"jsonrpc":"2.0","id":"logical","error":{"code":%d,"message":"peer","extension":{"retry":true},"_meta":{"opaque":"kept"}%s}}`, n, data)
			m, err := jsonrpc.DecodeMessage([]byte(input))
			check(t, err)
			b, err := jsonrpc.EncodeMessage(m)
			check(t, err)
			equalJSON(t, b, input)
		}
	}
}

// Rust src/agent-client-protocol/src/jsonrpc/raw_error.rs::malformed_raw_errors_are_still_rejected, all seven forms.
func TestMalformedRawErrorsRejected(t *testing.T) {
	for _, e := range []string{
		`null`, fmt.Sprintf(`{"code":%d}`, upstreamAuthRequiredCode), `{"message":"peer"}`, `{"code":null,"message":"peer"}`,
		fmt.Sprintf(`{"code":"%d","message":"peer"}`, upstreamAuthRequiredCode), `{"code":1.5,"message":"peer"}`, fmt.Sprintf(`{"code":%d,"message":null}`, upstreamAuthRequiredCode),
	} {
		input := `{"jsonrpc":"2.0","id":1,"error":` + e + `}`
		if _, err := jsonrpc.DecodeMessage([]byte(input)); err == nil {
			t.Errorf("malformed error accepted: %s", input)
		}
	}
}

// Rust src/agent-client-protocol/src/jsonrpc/raw_error.rs::acp_response_constructor_preserves_explicit_null_data.
// An unknown-ID error response must retain id:null as well as data presence.
func TestNullIDErrorDataPresence(t *testing.T) {
	for _, data := range []string{"", "null", `{"detail":"kept"}`} {
		out, err := jsonrpc.EncodeMessage(&jsonrpc.Response{Error: &jsonrpc.Error{
			Code: jsonrpc.CodeInvalidParams, Message: "Invalid params", Data: json.RawMessage(data),
		}})
		check(t, err)
		member := ""
		if data != "" {
			member = `,"data":` + data
		}
		equalJSON(t, out, fmt.Sprintf(`{"jsonrpc":"2.0","id":null,"error":{"code":%d,"message":"Invalid params"%s}}`, upstreamInvalidParamsCode, member))
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_error_handling.rs::raw_peer_errors_are_interpreted_only_at_typed_acp_dispatch.
// Its actual assertions check raw roundtrip before dispatch and known error
// fields at the awaiter, not post-marshal extension removal. Go returns the same
// public raw error type here; do not invent an implicit runtime projection.
func TestPeerErrorKnownFields(t *testing.T) {
	for _, n := range []int64{upstreamAuthRequiredCode, customCode} {
		for _, data := range []string{"", `,"data":null`, `,"data":{"detail":"kept"}`} {
			c, p := clientPeer(t)
			ctx, cancel := context.WithTimeout(context.Background(), wait)
			done := make(chan error, 1)
			go func() { _, err := c.CallExtension(ctx, "_test/fail", struct{}{}); done <- err }()
			r := p.receive(t)
			wire := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":"peer error","extension":{"retry":true}%s}}`, r["id"], n, data)
			// The upstream case first checks lossless raw decoding, then typed
			// interpretation. Do not silently discard its pre-dispatch assertion.
			m, err := jsonrpc.DecodeMessage([]byte(wire))
			check(t, err)
			b, err := jsonrpc.EncodeMessage(m)
			check(t, err)
			equalJSON(t, b, wire)
			p.send(t, wire)
			select {
			case err := <-done:
				var e *jsonrpc.Error
				if !errors.As(err, &e) {
					t.Fatalf("error type = %T: %v", err, err)
				}
				if e.Code != n || e.Message != "peer error" {
					t.Errorf("typed error = %#v", e)
				}
				var expected struct {
					Data json.RawMessage `json:"data"`
				}
				check(t, json.Unmarshal([]byte(fmt.Sprintf(`{"code":%d,"message":"peer error"%s}`, n, data)), &expected))
				if len(expected.Data) == 0 {
					if len(e.Data) != 0 {
						t.Errorf("omitted error data became %s", e.Data)
					}
				} else {
					equalJSON(t, e.Data, string(expected.Data))
				}
			case <-ctx.Done():
				t.Fatal("typed peer error was not delivered")
			}
			cancel()
		}
	}
}

// Rust src/agent-client-protocol/src/error.rs::v1_round_trip_is_lossless and ::serializes_like_the_schema_errors.
// The Go schema/runtime error representations meet through their public JSON
// boundary (there is no Rust-style From conversion). Retain the full source
// samples(): 13 codes x 6 optional-data states x 3 messages.
func TestRuntimeAndSchemaErrorWireAgreement(t *testing.T) {
	codes := []int64{
		upstreamParseErrorCode, upstreamInvalidRequestCode, upstreamMethodNotFoundCode,
		upstreamInvalidParamsCode, upstreamInternalErrorCode, upstreamRequestCancelledCode,
		upstreamAuthRequiredCode, upstreamResourceNotFoundCode,
		opaqueUnsupportedVersionCode, upstreamZeroErrorCode, customCode, math.MinInt32, math.MaxInt32,
	}
	for _, n := range codes {
		for _, data := range []string{"", "null", `"text"`, "42", `[1,null,{"a":[]}]`, `{"sessionId":"s","_meta":{"x":true},"extra":null}`} {
			for _, msg := range []string{"", "message", "Unknown error"} {
				runtime := &jsonrpc.Error{Code: n, Message: msg, Data: json.RawMessage(data)}
				b, err := json.Marshal(runtime)
				check(t, err)
				var schema acp.Error
				check(t, json.Unmarshal(b, &schema))
				if int64(schema.Code) != n || schema.Message != msg {
					t.Errorf("schema error changed code/message: %#v", schema)
				}
				schemaJSON, err := json.Marshal(schema)
				check(t, err)
				equalJSON(t, schemaJSON, string(b))
				var roundtrip jsonrpc.Error
				check(t, json.Unmarshal(schemaJSON, &roundtrip))
				roundJSON, err := json.Marshal(roundtrip)
				check(t, err)
				equalJSON(t, roundJSON, string(b))
			}
		}
	}
}

// Rust src/agent-client-protocol/src/error.rs::anyhow_recovers_wrapped_errors, SDK error branch. Go's native
// wrapping idiom is fmt.Errorf("%w", err); the SDK must recover the wrapped data.
func TestWrappedStructuredError(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/fail", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, fmt.Errorf("%w", &jsonrpc.Error{Code: customCode, Message: "custom", Data: json.RawMessage(`{"kept":true}`)})
	}))
	p := agentPeer(t, a, true)
	r := p.call(t, "1", "_test/fail", `{}`)
	code(t, r, customCode)
	equalJSON(t, r["error"], fmt.Sprintf(`{"code":%d,"message":"custom","data":{"kept":true}}`, customCode))
}

// Rust src/agent-client-protocol/tests/error_wire_format.rs::v1_connection_error_wire_format, shared_error()
// handler-produced cases expressible by Go's public structured-error type.
// Dropped responders and Rust conversion constructors are mapped separately.
func TestHandlerErrorWireFormat(t *testing.T) {
	cases := []struct {
		name          string
		id            int
		code          int64
		message, data string
	}{
		{"parse_error", 1, jsonrpc.CodeParseError, "Parse error", ""},
		{"invalid_request", 2, jsonrpc.CodeInvalidRequest, "Invalid request", ""},
		{"method_not_found", 3, jsonrpc.CodeMethodNotFound, "Method not found", ""},
		{"invalid_params", 4, jsonrpc.CodeInvalidParams, "Invalid params", `{"field":"timeout"}`},
		{"internal_error", 5, jsonrpc.CodeInternalError, "Internal error", `"details"`},
		{"request_cancelled", 6, int64(acp.ErrorCodeRequestCancelled), "Request cancelled", ""},
		{"auth_required", 7, int64(acp.ErrorCodeAuthenticationRequired), "Authentication required", ""},
		{"session_not_found", 8, int64(acp.ErrorCodeResourceNotFound), "Resource not found", `{"sessionId":"sess_1"}`},
		{"resource_not_found", 9, int64(acp.ErrorCodeResourceNotFound), "Resource not found", `{"uri":"file:///tmp/missing"}`},
		{"custom_code", 12, customCode, "custom", `[1,{"nested":null}]`},
		{"null_data", 13, jsonrpc.CodeInternalError, "Internal error", `null`},
	}
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/fail", func(_ context.Context, p *struct {
		Case string `json:"case"`
	}) (*struct{}, error) {
		for _, tc := range cases {
			if p.Case == tc.name {
				return nil, &jsonrpc.Error{Code: tc.code, Message: tc.message, Data: json.RawMessage(tc.data)}
			}
		}
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "unknown fixture case"}
	}))
	// Keep one connection, as exchange() does upstream: an earlier handler error
	// must not prevent the remaining requests from receiving their own replies.
	p := agentPeer(t, a, true)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := p.call(t, fmt.Sprint(tc.id), "_test/fail", fmt.Sprintf(`{"case":%q}`, tc.name))
			member := ""
			if tc.data != "" {
				member = `,"data":` + tc.data
			}
			b, err := json.Marshal(r)
			check(t, err)
			equalJSON(t, b, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":%d,"message":%q%s}}`, tc.id, tc.code, tc.message, member))
		})
	}
	t.Run("unknown_method", func(t *testing.T) {
		r := p.call(t, "15", "_test/unknown", `{}`)
		code(t, r, jsonrpc.CodeMethodNotFound)
		equalJSON(t, r["id"], `15`)
		var e struct {
			Data json.RawMessage `json:"data"`
		}
		check(t, json.Unmarshal(r["error"], &e))
		equalJSON(t, e.Data, `"_test/unknown"`)
	})
}

// Rust src/agent-client-protocol/src/concepts/error_handling.rs doctest@151: contextual structured data.
func TestErrorDataContext(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/context", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Invalid params", Data: json.RawMessage(`{"field":"timeout","reason":"must be positive"}`)}
	}))
	p := agentPeer(t, a, true)
	r := p.call(t, "1", "_test/context", `{}`)
	code(t, r, jsonrpc.CodeInvalidParams)
	var e struct {
		Data json.RawMessage `json:"data"`
	}
	check(t, json.Unmarshal(r["error"], &e))
	equalJSON(t, e.Data, `{"field":"timeout","reason":"must be positive"}`)
}
