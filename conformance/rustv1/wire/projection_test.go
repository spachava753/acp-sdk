package wire_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust src/agent-client-protocol/src/jsonrpc/raw_error.rs
// ::acp_error_interpretation_is_explicit_and_keeps_data_presence. Go does not
// have Rust's distinct typed runtime Error conversion. Exercise the actual
// public JSON projection into the schema payload acp.Error, then back into the
// raw jsonrpc.Error representation; never copy fields or alter a runtime error.
func TestExplicitSchemaErrorProjection(t *testing.T) {
	for _, n := range []int64{upstreamAuthRequiredCode, customCode} {
		for _, data := range []string{"", `,"data":null`, `,"data":{"detail":"kept"}`} {
			input := fmt.Sprintf(`{"code":%d,"message":"peer","extension":true%s}`, n, data)
			var raw jsonrpc.Error
			check(t, json.Unmarshal([]byte(input), &raw))
			rawJSON, err := json.Marshal(raw)
			check(t, err)
			// Ensure the raw representation actually carries the extra field
			// before attributing its removal to the explicit schema projection.
			equalJSON(t, rawJSON, input)

			var schema acp.Error
			check(t, json.Unmarshal(rawJSON, &schema))
			schemaJSON, err := json.Marshal(schema)
			check(t, err)
			known := fmt.Sprintf(`{"code":%d,"message":"peer"%s}`, n, data)
			equalJSON(t, schemaJSON, known)

			var roundtrip jsonrpc.Error
			check(t, json.Unmarshal(schemaJSON, &roundtrip))
			roundJSON, err := json.Marshal(roundtrip)
			check(t, err)
			equalJSON(t, roundJSON, known)
		}
	}
}
