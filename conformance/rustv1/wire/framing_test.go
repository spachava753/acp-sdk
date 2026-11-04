package wire_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust src/agent-client-protocol/tests/jsonrpc_batch.rs::every_batch_shape_gets_one_invalid_request_error and
// src/agent-client-protocol/src/jsonrpc/transport_actor.rs::batches_are_one_invalid_request. Unsupported-array
// rejection and continued service are Rust SDK policies, not batch support
// required by ACP. Port the policy explicitly, rather than implementing batches.
func TestUnsupportedBatches(t *testing.T) {
	a := newAgent()
	echoHandler(t, a, "_test/echo")
	invoked := make(chan struct{}, 1)
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/notify", func(context.Context, *message) error {
		invoked <- struct{}{}
		return nil
	}))
	p := agentPeer(t, a, true)
	// The source sends every batch on one Peer. Do not reset the connection
	// after each error: later errors and the final ordinary RPC must still work.
	for _, tc := range []struct{ name, frame string }{
		{"empty", `[]`}, {"primitives", `[1,2,3]`},
		{"requests", `[{"jsonrpc":"2.0","id":1,"method":"_test/echo","params":{"message":"one"}},{"jsonrpc":"2.0","id":2,"method":"_test/echo","params":{"message":"two"}}]`},
		{"notification", `[{"jsonrpc":"2.0","method":"_test/notify","params":{"message":"inside a batch"}}]`},
		{"response", `[{"jsonrpc":"2.0","id":7,"result":null}]`},
	} {
		if !t.Run(tc.name, func(t *testing.T) {
			p.send(t, tc.frame)
			r := p.receive(t)
			code(t, r, jsonrpc.CodeInvalidRequest)
			equalJSON(t, r["id"], `null`)
		}) {
			return
		}
	}
	r := p.call(t, "3", "_test/echo", `{"message":"after batches"}`)
	equalJSON(t, r["id"], `3`)
	equalJSON(t, r["result"], `{"result":"echo: after batches"}`)
	p.finish(t)
	// The serving task has finished, so a buffered notice cannot arrive later.
	select {
	case <-invoked:
		t.Error("batch entry invoked notification handler")
	default:
	}
}

// Rust src/agent-client-protocol/src/jsonrpc/transport_actor.rs::batches_are_one_invalid_request,
// single-request array case (not an extra step in jsonrpc_batch.rs's five-batch sequence).
func TestSingleRequestBatchRejected(t *testing.T) {
	p := agentPeer(t, newAgent(), true)
	p.send(t, `[{"jsonrpc":"2.0","id":1,"method":"one","params":{}}]`)
	r := p.receive(t)
	code(t, r, jsonrpc.CodeInvalidRequest)
	equalJSON(t, r["id"], `null`)
	p.finish(t)
}

// Rust src/agent-client-protocol/tests/jsonrpc_batch.rs::v1_agent_rejects_a_batched_initialize_and_accepts_a_retry.
func TestBatchedInitializeRetry(t *testing.T) {
	p := agentPeer(t, newAgent(), false)
	init := request("1", "initialize", `{"protocolVersion":1}`)
	p.send(t, "["+init+"]")
	r := p.receive(t)
	code(t, r, jsonrpc.CodeInvalidRequest)
	equalJSON(t, r["id"], `null`)
	p.send(t, init)
	r = p.receive(t)
	equalJSON(t, r["id"], `1`)
	var result struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	check(t, json.Unmarshal(r["result"], &result))
	if result.ProtocolVersion != 1 {
		t.Errorf("retry selected version %d", result.ProtocolVersion)
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_error_handling.rs::test_invalid_json and src/agent-client-protocol/src/jsonrpc/transport_actor.rs
// ::malformed_json_is_a_parse_error / ::valid_json_with_an_invalid_envelope_is_an_invalid_request.
// Error data containing the original line is a Rust diagnostic policy. It is
// not permission to continue processing a valid JSON prefix from that line.
func TestMalformedFrameErrors(t *testing.T) {
	for _, tc := range []struct {
		name, frame string
		code        int64
		data        bool
	}{
		{"invalid_json", `{"method": "test", "id": 1, INVALID}`, jsonrpc.CodeParseError, true},
		{"open_array", `[`, jsonrpc.CodeParseError, true},
		{"primitive", `17`, jsonrpc.CodeInvalidRequest, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := agentPeer(t, newAgent(), true)
			p.send(t, tc.frame)
			r := p.receive(t)
			code(t, r, tc.code)
			equalJSON(t, r["id"], `null`)
			if tc.data {
				var e struct {
					Data map[string]string `json:"data"`
				}
				check(t, json.Unmarshal(r["error"], &e))
				if e.Data["line"] != tc.frame {
					t.Errorf("parse diagnostic did not preserve source line: %s", r["error"])
				}
			}
		})
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_error_handling.rs::malformed_standalone_response_is_ignored.
// No response is generated for an unsolicited result+error response; a later
// normal request still gets its own answer. Survival is the upstream policy.
func TestMalformedStandaloneResponseIgnored(t *testing.T) {
	a := newAgent()
	echoHandler(t, a, "_test/simple")
	p := agentPeer(t, a, true)
	p.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":99,"result":null,"error":{"code":%d,"message":"Internal error"}}`, upstreamInternalErrorCode))
	r := p.call(t, "41", "_test/simple", `{"message":"after malformed response"}`)
	equalJSON(t, r["id"], `41`)
	equalJSON(t, r["result"], `{"result":"echo: after malformed response"}`)
}

// Rust src/agent-client-protocol/src/jsonrpc/protocol_compat.rs::v2_connections_reject_null_request_ids includes an
// explicit v1 control despite its cfg(v2): v1 initialize accepts id:null.
// Also src/agent-client-protocol/tests/protocol_v2.rs::v1_responder_keeps_null_request_ids,
// wire-response portion; Go handler signatures do not expose responder ID accessors.
func TestV1NullInitialize(t *testing.T) {
	if !isolated(t) {
		return
	}
	p := agentPeer(t, newAgent(), false)
	r := p.call(t, "null", "initialize", `{"protocolVersion":1,"clientCapabilities":{}}`)
	equalJSON(t, r["id"], `null`)
	var result struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	check(t, json.Unmarshal(r["result"], &result))
	if result.ProtocolVersion != 1 {
		t.Errorf("v1 null-ID initialize selected protocol %d", result.ProtocolVersion)
	}
}
