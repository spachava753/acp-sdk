// SPDX-License-Identifier: Apache-2.0
// Upstream: agentclientprotocol/rust-sdk@050cd94c081056ace37a7b89abfed7f6220b47eb.
// Rust source: src/agent-client-protocol/tests/protocol_v2.rs:1042 and :1093.
// Explicit V1 bodies in a V2-gated file; MCP-over-ACP is optional/unstable.
package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// protocol_v2.rs::mcp_over_acp_v1_variants_are_jsonrpc_mapped:
// ClientNotification, AgentRequest and ClientResponse mappings for V1 MCP.
// The local assert_message_mapping!/assert_response_mapping! macros check method
// metadata, serialization and the concrete aggregate variant, not native server binding.
func TestMcpV1VariantsMapped(t *testing.T) {
	const noteParams = `{"serverId":"server-1","requestId":"request-1","method":"notifications/tools/list"}`
	var n acp.MessageMcpNotification
	roundTrip(t, noteParams, &n)
	ctx, client, peer := rawClient(t, nil)
	notification(t, ctx, peer, "mcp/message", noteParams, func() error { return client.McpMessageNotification(ctx, &n) })
	incomingNotification(t, true, "mcp/message", noteParams)

	const reqParams = `{"serverId":"server-1","requestId":"request-1","method":"tools/list"}`
	var req acp.MessageMcpRequest
	roundTrip(t, reqParams, &req)
	ctx, conn, peer := rawAgent(t, nil)
	res := exchange(t, ctx, peer, "mcp/message", reqParams, `{"result":{"tools":[]}}`, func() (any, error) { return conn.McpMessage(ctx, &req) })
	carrier, ok := res.(*acp.MessageMcpResponse)
	if !ok || carrier.Result == nil || carrier.Error != nil {
		t.Fatalf("V1 MCP ClientResponse did not select its success carrier: %#v", res)
	}
	assertJSON(t, encode(t, carrier), `{"result":{"tools":[]}}`)
	incomingRequest(t, false, "mcp/message", reqParams, `{"result":null}`)
}

const mcpOnlyFailureCode int32 = -32042

// protocol_v2.rs::mcp_carriers_preserve_inner_outcomes_in_both_protocol_versions:
// assert_carriers!(v1, identity) expands to three opaque successes plus an inner
// error, each retaining outer metadata through ClientResponse parsing. The V2
// expansion is explicitly excluded in mapping.json, not mistaken for V1 coverage.
func TestMcpInnerOutcomes(t *testing.T) {
	const metaLiteral = `{"example/carrier":{"trace":"outer"}}`
	for _, tt := range []struct{ name, result string }{
		{"null", `null`},
		{"array", `["opaque",7]`},
		{"input_required", `{"resultType":"input_required","requestState":"opaque/retry?keep=exact","inputRequests":{"confirmation":{"method":"elicitation/create","params":{}}},"_meta":{"example/inner":"retained"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var result any
			if err := json.Unmarshal([]byte(tt.result), &result); err != nil {
				t.Fatal(err)
			}
			carrier := acp.ResultMessageMcpResponse(result)
			carrier.Meta = acp.Meta{"example/carrier": map[string]any{"trace": "outer"}}
			literal := `{"result":` + tt.result + `,"_meta":` + metaLiteral + `}`
			assertJSON(t, encode(t, carrier), literal)
			var parsed acp.MessageMcpResponse
			roundTrip(t, literal, &parsed)
			if parsed.Result == nil || parsed.Error != nil {
				t.Fatalf("opaque MCP success changed carrier kind: %#v", parsed)
			}
			assertJSON(t, encode(t, *parsed.Result), tt.result)
			assertJSON(t, encode(t, parsed.Meta), metaLiteral)

			// A literal independent peer supplies a successful outer ACP result.
			// Opaque input_required data must not turn into a callback or RPC error.
			ctx, conn, peer := rawAgent(t, nil)
			res := exchange(t, ctx, peer, "mcp/message", `{"serverId":"server-1","requestId":"request-1","method":"tools/list"}`, literal,
				func() (any, error) {
					return conn.McpMessage(ctx, &acp.MessageMcpRequest{ServerID: "server-1", RequestID: "request-1", Method: "tools/list"})
				})
			assertJSON(t, encode(t, res), literal)
		})
	}
	t.Run("inner_error", func(t *testing.T) {
		// Exact upstream fixture: data:null is present, and the unknown vendor
		// field is part of the opaque inner error, not outer ACP error metadata.
		const innerLiteral = `{"code":-32042,"message":"MCP-only failure","data":null,"example/detail":{"retryable":true}}`
		literal := `{"error":` + innerLiteral + `,"_meta":` + metaLiteral + `}`
		var inner acp.McpError
		if err := json.Unmarshal([]byte(innerLiteral), &inner); err != nil {
			t.Fatal(err)
		}
		carrier := acp.ErrorMessageMcpResponse(inner)
		carrier.Meta = acp.Meta{"example/carrier": map[string]any{"trace": "outer"}}
		assertJSON(t, encode(t, carrier), literal)
		var parsed acp.MessageMcpResponse
		roundTrip(t, literal, &parsed)
		if parsed.Error == nil || parsed.Result != nil || parsed.Error.Code != mcpOnlyFailureCode {
			t.Fatalf("inner error changed carrier kind: %#v", parsed)
		}
		assertJSON(t, encode(t, parsed.Error), innerLiteral)
		assertJSON(t, encode(t, parsed.Meta), metaLiteral)

		ctx, conn, peer := rawAgent(t, nil)
		res := exchange(t, ctx, peer, "mcp/message", `{"serverId":"server-1","requestId":"request-1","method":"tools/list"}`, literal,
			func() (any, error) {
				return conn.McpMessage(ctx, &acp.MessageMcpRequest{ServerID: "server-1", RequestID: "request-1", Method: "tools/list"})
			})
		// exchange already requires the outer ACP call to succeed. Its returned
		// value must still be the inner MCP error carrier with all fields intact.
		assertJSON(t, encode(t, res), literal)
	})
}
