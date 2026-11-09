package wire_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// validIDs are request IDs protocol version 1 accepts: any string, null, or an
// integer in the int64 range, including values a float64 cannot represent.
var validIDs = []struct{ name, wire string }{
	{"zero", "0"}, {"one", "1"}, {"negative", "-1"},
	{"above_float64", "9007199254740993"}, {"max", "9223372036854775807"},
	{"min", "-9223372036854775808"}, {"numeric_string", `"1"`},
	{"empty_string", `""`}, {"string", `"req-1"`},
	{"unicode", `"ünïcødé \"quoted\""`}, {"null", "null"},
}

func requestLine(id, method, params string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":%q,"params":%s}`, id, method, params)
}

func notificationLine(method, params string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"params":%s}`, method, params)
}

// roundTripMessage decodes input with the jsonrpc helpers, re-encodes it and
// returns the decoded message and its encoding.
func roundTripMessage(t *testing.T, input string) (jsonrpc.Message, []byte) {
	t.Helper()
	m, err := jsonrpc.DecodeMessage([]byte(input))
	check(t, err)
	out, err := jsonrpc.EncodeMessage(m)
	check(t, err)
	return m, out
}

// TestRequestIDsRoundTrip checks that every valid ID marks a frame as a call
// and survives decoding and encoding exactly, so 1 and "1" stay distinct and
// large integers are not rounded. The schema RequestId type must encode the
// same value.
func TestRequestIDsRoundTrip(t *testing.T) {
	for _, id := range validIDs {
		t.Run(id.name, func(t *testing.T) {
			input := requestLine(id.wire, "_test/echo", `{}`)
			m, out := roundTripMessage(t, input)
			if r, ok := m.(*jsonrpc.Request); !ok || !r.IsCall() {
				t.Fatalf("a frame with an id must decode as a call, got %#v", m)
			}
			acptest.EqualJSON(t, out, input)
			acptest.EqualMarshaled(t, acp.RequestId(raw(id.wire)), id.wire)
		})
	}
}

// TestInvalidRequestIDsRejected checks that IDs outside protocol version 1's
// string, null or int64 forms are rejected, including integers just outside
// int64 and exponent spellings.
func TestInvalidRequestIDsRejected(t *testing.T) {
	for _, id := range []string{"9223372036854775808", "-9223372036854775809", "1.5", "1e3", "true", "[]", "{}"} {
		t.Run(id, func(t *testing.T) {
			if _, err := jsonrpc.DecodeMessage([]byte(requestLine(id, "_test/echo", `{}`))); err == nil {
				t.Errorf("id %s accepted", id)
			}
		})
	}
}

// TestMakeIDKeepsType checks that MakeID keeps a numeric ID numeric and a
// string ID quoted on the wire.
func TestMakeIDKeepsType(t *testing.T) {
	for _, tc := range []struct {
		id   any
		want string
	}{
		{float64(7), `7`},
		{"7", `"7"`},
	} {
		id, err := jsonrpc.MakeID(tc.id)
		check(t, err)
		out, err := jsonrpc.EncodeMessage(&jsonrpc.Request{ID: id, Method: "_test/example"})
		check(t, err)
		var frame map[string]json.RawMessage
		check(t, json.Unmarshal(out, &frame))
		acptest.EqualJSON(t, frame["id"], tc.want)
	}
}

// TestMalformedMessagesClassified checks the structured error code
// DecodeMessage reports: invalid JSON is a parse error, while valid JSON that
// is not a single well-formed message is an invalid request.
func TestMalformedMessagesClassified(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		code        int64
	}{
		{"response_with_result_and_error", fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":null,"error":{"code":%d,"message":"Internal error"}}`, jsonrpc.CodeInternalError), jsonrpc.CodeInvalidRequest},
		{"call_with_result", `{"jsonrpc":"2.0","id":1,"method":"one","result":null}`, jsonrpc.CodeInvalidRequest},
		{"malformed_json", `[`, jsonrpc.CodeParseError},
		{"primitive", `17`, jsonrpc.CodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := jsonrpc.DecodeMessage([]byte(tc.input))
			wantCode(t, err, tc.code)
		})
	}
}

// TestInvalidEnvelopesRejected checks that scalar params and conflicting
// request, response and notification members are rejected, while a scalar
// result is a valid response.
func TestInvalidEnvelopesRejected(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"scalar_request_params", `{"jsonrpc":"2.0","id":1,"method":"scalar_params","params":1}`},
		{"scalar_notification_params", `{"jsonrpc":"2.0","method":"scalar_params","params":true}`},
		{"request_with_result", `{"jsonrpc":"2.0","id":1,"method":"request_with_result","result":{}}`},
		{"response_with_result_and_error", fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":%d,"message":"Internal error"}}`, jsonrpc.CodeInternalError)},
		{"notification_with_error", fmt.Sprintf(`{"jsonrpc":"2.0","method":"notification_with_error","error":{"code":%d,"message":"Internal error"}}`, jsonrpc.CodeInternalError)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := jsonrpc.DecodeMessage([]byte(tc.input)); err == nil {
				t.Errorf("accepted %s", tc.input)
			}
		})
	}
	t.Run("scalar_result", func(t *testing.T) {
		input := `{"jsonrpc":"2.0","id":1,"result":1}`
		_, out := roundTripMessage(t, input)
		acptest.EqualJSON(t, out, input)
	})
}

// TestFrameWithTrailingDataRejected checks that a valid message followed by
// more data on the same line is rejected as a whole rather than decoded as its
// valid prefix.
func TestFrameWithTrailingDataRejected(t *testing.T) {
	prefix := requestLine(`"prefix"`, acp.MethodSessionNew, `{"cwd":"/prefix","mcpServers":[]}`)
	for _, suffix := range []string{` garbage`, " " + notificationLine(acp.MethodSessionCancel, cancelParams)} {
		if _, err := jsonrpc.DecodeMessage([]byte(prefix + suffix)); err == nil {
			t.Errorf("accepted the valid prefix of %q", prefix+suffix)
		}
	}
}

// TestNotificationRoundTrip checks that a frame without an id decodes as a
// notification, that unknown envelope members are dropped on encoding, and
// that a notification without params omits the member.
func TestNotificationRoundTrip(t *testing.T) {
	m, out := roundTripMessage(t, `{"jsonrpc":"2.0","method":"_vendor/ping","params":{"n":1},"extra":true}`)
	if r, ok := m.(*jsonrpc.Request); !ok || r.IsCall() {
		t.Fatalf("decoded as %#v, want a notification", m)
	}
	acptest.EqualJSON(t, out, `{"jsonrpc":"2.0","method":"_vendor/ping","params":{"n":1}}`)

	out, err := jsonrpc.EncodeMessage(&jsonrpc.Request{Method: "ping"})
	check(t, err)
	acptest.EqualJSON(t, out, `{"jsonrpc":"2.0","method":"ping"}`)
}

// TestResponseRoundTrip checks that a standalone result frame decodes as a
// response and encodes back unchanged.
func TestResponseRoundTrip(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`
	m, out := roundTripMessage(t, input)
	if _, ok := m.(*jsonrpc.Response); !ok {
		t.Fatalf("decoded as %T, want a response", m)
	}
	acptest.EqualJSON(t, out, input)
}

// TestEmptyMethodIsARequest checks that an explicitly empty method string is a
// method, not a missing one: the frame stays a call, and both re-encoding it
// and encoding a fresh Request keep the empty method member.
func TestEmptyMethodIsARequest(t *testing.T) {
	input := requestLine(`"empty"`, "", `{}`)
	m, out := roundTripMessage(t, input)
	if r, ok := m.(*jsonrpc.Request); !ok {
		t.Errorf("decoded as %T, want a request", m)
	} else if !r.IsCall() || r.Method != "" {
		t.Errorf("decoded request lost its id or method: %#v", r)
	}
	// Comparing whole objects catches a dropped method member, which a zero
	// Method value alone would not.
	acptest.EqualJSON(t, out, input)

	// Encode independently too, so a decoder fault cannot mask an encoder
	// that omits an empty method.
	id, err := jsonrpc.MakeID("empty")
	check(t, err)
	out, err = jsonrpc.EncodeMessage(&jsonrpc.Request{ID: id, Method: "", Params: raw(`{}`)})
	check(t, err)
	acptest.EqualJSON(t, out, input)
}

type nopCloser struct{ bytes.Buffer }

func (*nopCloser) Close() error { return nil }

// TestTransportRelaysResponseFrame checks that IOTransport reads an error
// response with extra members and writes it back as one newline-terminated
// frame with the same JSON value.
func TestTransportRelaysResponseFrame(t *testing.T) {
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":"error","error":{"code":%d,"message":"peer","extension":{"retry":true}}}`, acp.ErrorCodeAuthenticationRequired)
	out := &nopCloser{}
	transport := &acp.IOTransport{Reader: io.NopCloser(strings.NewReader(input + "\n")), Writer: out}
	conn, err := transport.Connect(context.Background())
	check(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), acptest.Timeout)
	defer cancel()
	m, err := conn.Read(ctx)
	check(t, err)
	check(t, conn.Write(ctx, m))
	acptest.EqualJSON(t, out.Bytes(), input)
	if bytes.Count(out.Bytes(), []byte{'\n'}) != 1 {
		t.Errorf("wrote %q, want one newline-terminated frame", out.Bytes())
	}
}
