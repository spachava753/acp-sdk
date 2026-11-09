// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// TestRequiredFields checks that a required, non-nullable field rejects an
// explicit null instead of decoding it as the Go zero value, at the top level,
// for arrays and inside union variants. Zero and empty values stay valid, and
// a field that defaults on null still does.
func TestRequiredFields(t *testing.T) {
	newPrompt := alloc[acp.PromptRequest]
	runDecodeCases(t, []decodeCase{
		{name: "null_protocol_version", new: alloc[acp.InitializeRequest], input: `{"protocolVersion":null}`, reject: true},
		{name: "null_session_id", new: newPrompt, input: `{"sessionId":null,"prompt":[]}`, reject: true},
		{name: "null_prompt", new: newPrompt, input: `{"sessionId":"s1","prompt":null}`, reject: true},
		{name: "null_text", new: newPrompt, input: `{"sessionId":"s1","prompt":[{"type":"text","text":null}]}`, reject: true},
		// Version 0 is a valid uint16, whatever an agent then negotiates.
		{name: "zero_protocol_version", new: alloc[acp.InitializeRequest], input: `{"protocolVersion":0}`},
		{name: "empty_session_id_and_prompt", new: newPrompt, input: `{"sessionId":"","prompt":[]}`},
		{name: "empty_text", new: newPrompt, input: `{"sessionId":"s1","prompt":[{"type":"text","text":""}]}`},
		{name: "null_mcp_servers_defaults_empty", new: alloc[acp.NewSessionRequest], input: `{"cwd":"/workspace","mcpServers":null}`, want: `{"cwd":"/workspace","mcpServers":[]}`},
	})
}

// TestStopReasons checks that a prompt response rejects the "error" stop
// reason, which is not a protocol version 1 value; failures are JSON-RPC
// errors instead.
func TestStopReasons(t *testing.T) {
	runDecodeCases(t, []decodeCase{
		{name: "end_turn", new: alloc[acp.PromptResponse], input: `{"stopReason":"end_turn"}`},
		{name: "error", new: alloc[acp.PromptResponse], input: `{"stopReason":"error"}`, reject: true},
	})
}

// TestDefaultOnErrorRollsBack checks that a field which defaults on invalid
// input is reset completely: no allocation or partial write from the failed
// decode remains, and valid sibling fields are kept.
func TestDefaultOnErrorRollsBack(t *testing.T) {
	t.Run("capability_pointer", func(t *testing.T) {
		var caps acp.SessionCapabilities
		decode(t, `{"list":false,"resume":{}}`, &caps)
		if caps.List != nil {
			t.Error("invalid list capability was advertised instead of reset to nil")
		}
		if caps.Resume == nil {
			t.Error("valid resume capability was lost")
		}
		acptest.EqualMarshaled(t, caps, `{"resume":{}}`)
	})
	t.Run("partial_struct", func(t *testing.T) {
		var usage acp.UsageUpdate
		decode(t, `{"used":5,"size":10,"cost":{"amount":1.25,"currency":false}}`, &usage)
		if usage.Cost != nil {
			t.Error("invalid cost kept a partly decoded struct")
		}
		if usage.Used != 5 || usage.Size != 10 {
			t.Errorf("valid counters were lost: used=%d size=%d", usage.Used, usage.Size)
		}
		acptest.EqualMarshaled(t, usage, `{"used":5,"size":10}`)
	})
}

// TestInvalidMetadataKeepsPayload checks that invalid _meta is dropped without
// losing the content or update around it, including in nested content. Where a
// variant distinguishes null _meta, an explicit null is still kept.
func TestInvalidMetadataKeepsPayload(t *testing.T) {
	newUpdate := alloc[acp.SessionUpdate]
	runDecodeCases(t, []decodeCase{
		{name: "content", new: alloc[acp.ContentBlock],
			input: `{"type":"text","text":"keep","_meta":42}`, want: `{"type":"text","text":"keep"}`},
		{name: "update", new: newUpdate,
			input: `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"keep"},"_meta":42}`, want: `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"keep"}}`},
		{name: "nested_content", new: newUpdate,
			input: `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"keep","_meta":42}}`, want: `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"keep"}}`},
		{name: "null_sensitive_variant", new: newUpdate,
			input: `{"sessionUpdate":"subagent_update","sessionId":"child","_meta":42}`, want: `{"sessionUpdate":"subagent_update","sessionId":"child"}`},
		{name: "null_sensitive_variant_with_null", new: newUpdate,
			input: `{"sessionUpdate":"subagent_update","sessionId":"child","_meta":null}`},
	})
}
