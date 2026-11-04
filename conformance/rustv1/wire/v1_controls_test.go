package wire_test

import (
	"strconv"
	"testing"

	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust src/agent-client-protocol/tests/protocol_v2.rs::v1_and_raw_agents_answer_entity_cancel_with_method_not_found.
// Despite the file's v2 feature gate, both branches are v1/raw controls. Neither
// tool_call/cancel nor terminal/cancel is a supported v1 method.
func TestV1UnknownEntityCancellation(t *testing.T) {
	for _, profile := range []string{"v1", "raw"} {
		t.Run(profile, func(t *testing.T) {
			a, initialize, firstID := newAgent(), true, 2
			if profile == "raw" {
				a, initialize, firstID = nil, false, 1
			}
			p := agentPeer(t, a, initialize)
			for i, method := range []string{"tool_call/cancel", "terminal/cancel"} {
				r := p.call(t, strconv.Itoa(firstID+i), method, `{"sessionId":"s","toolCallId":"c","terminalId":"t"}`)
				code(t, r, jsonrpc.CodeMethodNotFound)
			}
		})
	}
}
