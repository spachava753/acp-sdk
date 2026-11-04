// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
package session_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// This inner MCP code comes from the upstream raw error fixture. ACP's Go
// ErrorCode enum does not name it; it is not used as an outer JSON-RPC error.
const mcpUnsupportedProtocolVersion = -32022

// Rust: src/agent-client-protocol/src/mcp_server/active_session.rs::tests::v1_outcomes_preserve_opaque_results_and_raw_errors.
// Ports the v1 carrier-value oracle through public schema constructors/codecs;
// no native MCP service registry or factory is recreated in this fixture.
func TestV1MCPOutcomesPreserveRawErrors(t *testing.T) {
	assertJSON(t, acp.ResultMessageMcpResponse(nil), `{"result":null}`)
	// Also the opaque result slice of tests/mcp_factory.rs::optional_driver_and_accepted_terminal_output_are_preserved.
	assertJSON(t, acp.ResultMessageMcpResponse(map[string]any{"opaque": true}), `{"result":{"opaque":true}}`)
	for _, scenario := range []struct {
		name, message string
		data          json.RawMessage
	}{
		{"carrier_absent", "peer", nil}, {"carrier_null", "peer", json.RawMessage(`null`)}, {"carrier_object", "peer", json.RawMessage(`{"cause":"upstream"}`)},
		{"factory_absent", "raw peer error", nil}, {"factory_null", "raw peer error", json.RawMessage(`null`)}, {"factory_object", "raw peer error", json.RawMessage(`{"upstream":"failed"}`)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			errorValue := map[string]any{"code": mcpUnsupportedProtocolVersion, "message": scenario.message, "extension": map[string]any{"retry": false}}
			if scenario.data != nil {
				errorValue["data"] = scenario.data
			}
			encodedError, err := json.Marshal(errorValue)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(map[string]any{"error": errorValue})
			if err != nil {
				t.Fatal(err)
			}
			// Keep both paths independent: a decoder roundtrip cannot establish
			// that the public error-outcome constructor retains the converted error.
			t.Run("constructor", func(t *testing.T) {
				var converted acp.McpError
				if err := json.Unmarshal(encodedError, &converted); err != nil {
					t.Fatal(err)
				}
				assertJSON(t, acp.ErrorMessageMcpResponse(converted), string(encoded))
			})
			t.Run("decoder_roundtrip", func(t *testing.T) {
				var decoded acp.MessageMcpResponse
				if err := json.Unmarshal(encoded, &decoded); err != nil {
					t.Fatal(err)
				}
				assertJSON(t, decoded, string(encoded))
			})
		})
	}
}
