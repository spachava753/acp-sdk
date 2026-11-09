package wire_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// TestErrorCodeValues pins the runtime and schema error-code constants to the
// values the JSON-RPC 2.0 and ACP specifications assign. The literals are the
// expected values under test, so they are written out rather than taken from
// either set of constants.
func TestErrorCodeValues(t *testing.T) {
	for _, tc := range []struct {
		runtime int64
		schema  acp.ErrorCode
		want    int64
	}{
		{jsonrpc.CodeParseError, acp.ErrorCodeParseError, -32700},
		{jsonrpc.CodeInvalidRequest, acp.ErrorCodeInvalidRequest, -32600},
		{jsonrpc.CodeMethodNotFound, acp.ErrorCodeMethodNotFound, -32601},
		{jsonrpc.CodeInvalidParams, acp.ErrorCodeInvalidParams, -32602},
		{jsonrpc.CodeInternalError, acp.ErrorCodeInternalError, -32603},
	} {
		if tc.runtime != tc.want || int64(tc.schema) != tc.want {
			t.Errorf("runtime/schema codes = %d/%d, want %d", tc.runtime, tc.schema, tc.want)
		}
	}
	for _, tc := range []struct {
		schema acp.ErrorCode
		want   int64
	}{
		{acp.ErrorCodeRequestCancelled, -32800},
		{acp.ErrorCodeAuthenticationRequired, -32000},
		{acp.ErrorCodeResourceNotFound, -32002},
	} {
		if int64(tc.schema) != tc.want {
			t.Errorf("schema code = %d, want %d", tc.schema, tc.want)
		}
	}
	if acp.ErrorCodeInvalidParams == acp.ErrorCodeInvalidRequest {
		t.Error("InvalidParams and InvalidRequest share a code")
	}
}

// dataMembers are the three ways an error object can carry data: absent,
// explicitly null, and a value. Each member is a suffix for an error object.
var dataMembers = []struct{ name, member, value string }{
	{"absent", "", ""},
	{"null", `,"data":null`, `null`},
	{"value", `,"data":{"detail":"kept"}`, `{"detail":"kept"}`},
}

// TestErrorResponsesRoundTrip checks that decoding and re-encoding an error
// response keeps its code, message, data presence and unknown members, across
// the int32 extremes, zero, and standard, ACP and custom codes.
func TestErrorResponsesRoundTrip(t *testing.T) {
	for _, code := range []int64{math.MinInt32, int64(acp.ErrorCodeAuthenticationRequired), jsonrpc.CodeInvalidParams, 0, customCode, math.MaxInt32} {
		for _, data := range []string{"", `,"data":null`, `,"data":{"detail":[1,2]}`} {
			input := fmt.Sprintf(`{"jsonrpc":"2.0","id":"logical","error":{"code":%d,"message":"peer","extension":{"retry":true},"_meta":{"opaque":"kept"}%s}}`, code, data)
			_, out := roundTripMessage(t, input)
			acptest.EqualJSON(t, out, input)
		}
	}
}

// TestMalformedErrorObjectsRejected checks that an error member must be an
// object with an integer code and a string message.
func TestMalformedErrorObjectsRejected(t *testing.T) {
	code := acp.ErrorCodeAuthenticationRequired
	for _, e := range []string{
		`null`,
		fmt.Sprintf(`{"code":%d}`, code),
		`{"message":"peer"}`,
		`{"code":null,"message":"peer"}`,
		fmt.Sprintf(`{"code":"%d","message":"peer"}`, code),
		`{"code":1.5,"message":"peer"}`,
		fmt.Sprintf(`{"code":%d,"message":null}`, code),
	} {
		input := `{"jsonrpc":"2.0","id":1,"error":` + e + `}`
		if _, err := jsonrpc.DecodeMessage([]byte(input)); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
}

// TestNullIDErrorResponse checks that an error response without a request ID
// encodes id as null and keeps the data member absent, null or present as
// given.
func TestNullIDErrorResponse(t *testing.T) {
	for _, data := range []string{"", "null", `{"detail":"kept"}`} {
		out, err := jsonrpc.EncodeMessage(&jsonrpc.Response{Error: &jsonrpc.Error{
			Code: jsonrpc.CodeInvalidParams, Message: "Invalid params", Data: raw(data),
		}})
		check(t, err)
		member := ""
		if data != "" {
			member = `,"data":` + data
		}
		acptest.EqualJSON(t, out, fmt.Sprintf(`{"jsonrpc":"2.0","id":null,"error":{"code":%d,"message":"Invalid params"%s}}`, jsonrpc.CodeInvalidParams, member))
	}
}

// TestSchemaErrorKeepsKnownFields checks the JSON projection from the runtime
// error into the schema Error type: the runtime type keeps unknown members,
// the schema type keeps only code, message and data (with data presence
// intact), and converting back yields the same known fields.
func TestSchemaErrorKeepsKnownFields(t *testing.T) {
	for _, code := range []int64{int64(acp.ErrorCodeAuthenticationRequired), customCode} {
		for _, data := range dataMembers {
			input := fmt.Sprintf(`{"code":%d,"message":"peer","extension":true%s}`, code, data.member)
			var runtime jsonrpc.Error
			check(t, json.Unmarshal([]byte(input), &runtime))
			runtimeJSON := acptest.Marshal(t, runtime)
			// Confirm the unknown member is really carried, so its absence
			// below is due to the schema type.
			acptest.EqualJSON(t, runtimeJSON, input)

			known := fmt.Sprintf(`{"code":%d,"message":"peer"%s}`, code, data.member)
			var schema acp.Error
			check(t, json.Unmarshal(runtimeJSON, &schema))
			schemaJSON := acptest.Marshal(t, schema)
			acptest.EqualJSON(t, schemaJSON, known)

			var back jsonrpc.Error
			check(t, json.Unmarshal(schemaJSON, &back))
			acptest.EqualMarshaled(t, back, known)
		}
	}
}

// TestSchemaAndRuntimeErrorsAgree checks that the runtime and schema error
// types encode identically and convert into each other through JSON without
// loss, across codes, data values (including absent and null) and messages
// (including empty).
func TestSchemaAndRuntimeErrorsAgree(t *testing.T) {
	codes := []int64{
		jsonrpc.CodeParseError, jsonrpc.CodeInvalidRequest, jsonrpc.CodeMethodNotFound,
		jsonrpc.CodeInvalidParams, jsonrpc.CodeInternalError, int64(acp.ErrorCodeRequestCancelled),
		int64(acp.ErrorCodeAuthenticationRequired), int64(acp.ErrorCodeResourceNotFound),
		unnamedReservedCode, 0, customCode, math.MinInt32, math.MaxInt32,
	}
	for _, code := range codes {
		for _, data := range []string{"", "null", `"text"`, "42", `[1,null,{"a":[]}]`, `{"sessionId":"s","_meta":{"x":true},"extra":null}`} {
			for _, msg := range []string{"", "message", "Unknown error"} {
				runtimeJSON := acptest.Marshal(t, &jsonrpc.Error{Code: code, Message: msg, Data: raw(data)})
				var schema acp.Error
				check(t, json.Unmarshal(runtimeJSON, &schema))
				if int64(schema.Code) != code || schema.Message != msg {
					t.Errorf("schema error changed code or message: %#v", schema)
				}
				schemaJSON := acptest.Marshal(t, schema)
				acptest.EqualJSON(t, schemaJSON, string(runtimeJSON))
				var back jsonrpc.Error
				check(t, json.Unmarshal(schemaJSON, &back))
				acptest.EqualMarshaled(t, back, string(runtimeJSON))
			}
		}
	}
}

// TestHandlerErrorsOnTheWire checks the exact error response the agent writes
// for each structured error a handler returns, including a wrapped one, and
// the response for an unregistered method. All cases share one connection: an
// earlier error response must not affect later requests.
func TestHandlerErrorsOnTheWire(t *testing.T) {
	cases := []struct {
		name          string
		id            int
		code          int64
		message, data string
		wrap          bool
	}{
		{"parse_error", 1, jsonrpc.CodeParseError, "Parse error", "", false},
		{"invalid_request", 2, jsonrpc.CodeInvalidRequest, "Invalid request", "", false},
		{"method_not_found", 3, jsonrpc.CodeMethodNotFound, "Method not found", "", false},
		{"invalid_params", 4, jsonrpc.CodeInvalidParams, "Invalid params", `{"field":"timeout"}`, false},
		{"internal_error", 5, jsonrpc.CodeInternalError, "Internal error", `"details"`, false},
		{"request_cancelled", 6, int64(acp.ErrorCodeRequestCancelled), "Request cancelled", "", false},
		{"auth_required", 7, int64(acp.ErrorCodeAuthenticationRequired), "Authentication required", "", false},
		{"session_not_found", 8, int64(acp.ErrorCodeResourceNotFound), "Resource not found", `{"sessionId":"sess_1"}`, false},
		{"resource_not_found", 9, int64(acp.ErrorCodeResourceNotFound), "Resource not found", `{"uri":"file:///tmp/missing"}`, false},
		{"contextual_data", 10, jsonrpc.CodeInvalidParams, "Invalid params", `{"field":"timeout","reason":"must be positive"}`, false},
		{"custom_code", 12, customCode, "custom", `[1,{"nested":null}]`, false},
		{"null_data", 13, jsonrpc.CodeInternalError, "Internal error", `null`, false},
		// The SDK must find a structured error wrapped with %w.
		{"wrapped", 14, customCode, "custom", `{"kept":true}`, true},
	}
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/fail", func(_ context.Context, p *struct {
		Case string `json:"case"`
	}) (*struct{}, error) {
		for _, tc := range cases {
			if p.Case == tc.name {
				var err error = &jsonrpc.Error{Code: tc.code, Message: tc.message, Data: raw(tc.data)}
				if tc.wrap {
					err = fmt.Errorf("%w", err)
				}
				return nil, err
			}
		}
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "unknown test case"}
	}))
	_, peer := startAgent(t, a)
	for _, tc := range cases {
		r := peer.Call(tc.id, "_test/fail", map[string]string{"case": tc.name})
		member := ""
		if tc.data != "" {
			member = `,"data":` + tc.data
		}
		acptest.EqualJSON(t, r.Line, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":%d,"message":%q%s}}`, tc.id, tc.code, tc.message, member))
		if t.Failed() {
			t.Fatalf("stopped after case %s", tc.name)
		}
	}

	// An unknown method names itself in the error data.
	r := peer.Call(15, "_test/unknown", map[string]any{})
	wantError(t, r, jsonrpc.CodeMethodNotFound)
	acptest.EqualJSON(t, r.ID, `15`)
	acptest.EqualJSON(t, r.Error.Data, `"_test/unknown"`)
}

// TestHandlerChoosesErrorOrResult checks that a handler can answer one
// request with InvalidParams and the next with a result whose null field is
// kept.
func TestHandlerChoosesErrorOrResult(t *testing.T) {
	type result struct {
		IsValid bool    `json:"is_valid"`
		Error   *string `json:"error"`
	}
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/validate", func(_ context.Context, p *struct {
		Data string `json:"data"`
	}) (*result, error) {
		if p.Data == "" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Invalid params"}
		}
		return &result{IsValid: true}, nil
	}))
	_, peer := startAgent(t, a)
	wantError(t, peer.Call(1, "_test/validate", map[string]string{"data": ""}), jsonrpc.CodeInvalidParams)
	acptest.EqualJSON(t, peer.Call(2, "_test/validate", map[string]string{"data": "ok"}).Result, `{"is_valid":true,"error":null}`)
}

// TestClientReceivesPeerError checks that an error response from the peer
// reaches the client's caller as a structured error with the peer's code,
// message and data presence. The same frame is first decoded and re-encoded
// with the jsonrpc helpers, which must keep its unknown member.
func TestClientReceivesPeerError(t *testing.T) {
	for _, code := range []int64{int64(acp.ErrorCodeAuthenticationRequired), customCode} {
		for _, data := range dataMembers {
			t.Run(fmt.Sprintf("%d/%s", code, data.name), func(t *testing.T) {
				client, peer := startClient(t, nil)
				result := acptest.Go(func() (json.RawMessage, error) {
					return client.CallExtension(context.Background(), "_test/fail", struct{}{})
				})
				request := peer.Expect("_test/fail")
				frame := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":"peer error","extension":{"retry":true}%s}}`, request.ID, code, data.member)
				_, out := roundTripMessage(t, frame)
				acptest.EqualJSON(t, out, frame)
				peer.SendLine(frame)

				err := acptest.Await(t, result).Err
				var e *jsonrpc.Error
				if !errors.As(err, &e) {
					t.Fatalf("error = %T %v, want *jsonrpc.Error", err, err)
				}
				if e.Code != code || e.Message != "peer error" {
					t.Errorf("error = %#v", e)
				}
				if data.value == "" {
					if len(e.Data) != 0 {
						t.Errorf("absent data became %s", e.Data)
					}
				} else {
					acptest.EqualJSON(t, e.Data, data.value)
				}
			})
		}
	}
}

// TestClientSeesHandlerErrorCodes checks, with an SDK client talking to an SDK
// agent, that the caller receives the code of an unknown method and of errors
// returned by handlers.
func TestClientSeesHandlerErrorCodes(t *testing.T) {
	a := newAgent()
	for method, code := range map[string]int64{"internal": jsonrpc.CodeInternalError, "invalid": jsonrpc.CodeInvalidParams} {
		check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/"+method, func(context.Context, *struct{}) (*answer, error) {
			return nil, &jsonrpc.Error{Code: code, Message: "handler error"}
		}))
	}
	client, _ := connect(t, a)
	for _, tc := range []struct {
		method string
		code   int64
	}{
		{"unknown", jsonrpc.CodeMethodNotFound},
		{"internal", jsonrpc.CodeInternalError},
		{"invalid", jsonrpc.CodeInvalidParams},
	} {
		_, err := callExtension(t, client, "_test/"+tc.method, struct{}{})
		wantCode(t, err, tc.code)
	}
}

// TestUnencodableResultIsInternalError checks that a handler result that
// cannot be encoded as JSON becomes an InternalError response rather than a
// crash, and that the connection keeps serving. This is only reachable
// through a programming error, here a channel in Meta. It is checked for a
// generated ACP method and for an extension method.
func TestUnencodableResultIsInternalError(t *testing.T) {
	for name, method := range map[string]string{"generated": acp.MethodSessionNew, "extension": "_test/new"} {
		t.Run(name, func(t *testing.T) {
			a := newAgent()
			a.newSession = func(_ context.Context, p *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
				if p.Cwd == "/bad" {
					return &acp.NewSessionResponse{SessionID: "bad", Meta: acp.Meta{"invalid": make(chan int)}}, nil
				}
				return &acp.NewSessionResponse{SessionID: "healthy"}, nil
			}
			if method != acp.MethodSessionNew {
				check(t, acp.AddExtensionRequest(a.ExtensionMux, method, a.SessionNew))
			}
			ag, peer := startAgent(t, a)

			bad := peer.Call("bad", method, raw(`{"cwd":"/bad","mcpServers":[]}`))
			acptest.EqualJSON(t, bad.ID, `"bad"`)
			wantError(t, bad, jsonrpc.CodeInternalError)

			good := peer.Call("healthy", method, raw(`{"cwd":"/healthy","mcpServers":[]}`))
			acptest.EqualJSON(t, good.ID, `"healthy"`)
			acptest.EqualJSON(t, good.Result, `{"sessionId":"healthy"}`)

			finishClean(t, ag, peer)
			if got := a.newCalls.Load(); got != 2 {
				t.Errorf("handler calls = %d, want 2", got)
			}
		})
	}
}
