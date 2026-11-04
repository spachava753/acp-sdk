package wire_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust src/agent-client-protocol/src/jsonrpc/transport_actor.rs parser tests: malformed response/call shapes use
// InvalidRequest, malformed JSON uses ParseError, and a primitive JSON value
// uses InvalidRequest. The Go boundary returns error rather than a retained
// TransportFrame; inspect its public structured error, not private helpers.
func TestMalformedFrameClassification(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		code        int64
	}{
		{"response_shape", fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":null,"error":{"code":%d,"message":"Internal error"}}`, jsonrpc.CodeInternalError), jsonrpc.CodeInvalidRequest},
		{"call_shape", `{"jsonrpc":"2.0","id":1,"method":"one","result":null}`, jsonrpc.CodeInvalidRequest},
		{"malformed_json", `[`, jsonrpc.CodeParseError},
		{"primitive", `17`, jsonrpc.CodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := jsonrpc.DecodeMessage([]byte(tc.input))
			var e *jsonrpc.Error
			if !errors.As(err, &e) || e.Code != tc.code {
				t.Errorf("parser error = %v, want structured code %d", err, tc.code)
			}
		})
	}
}
