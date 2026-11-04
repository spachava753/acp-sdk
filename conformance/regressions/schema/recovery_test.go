package schema_test

import (
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// x-deserialize-default-on-error means defaulting the failed field, not keeping
// allocations or partial writes left behind by a failed decode. These inputs
// are malformed but annotated for recovery; their valid siblings must survive.
func TestDefaultOnErrorRollsBackFailedFields(t *testing.T) {
	t.Run("capability_pointer", func(t *testing.T) {
		var caps acp.SessionCapabilities
		decode(t, `{"list":false,"resume":{}}`, &caps)
		if caps.List != nil {
			t.Error("invalid list capability advertised support instead of defaulting to nil")
		}
		if caps.Resume == nil {
			t.Error("valid sibling resume capability was lost")
		}
		equalJSON(t, marshal(t, caps), `{"resume":{}}`)
	})
	t.Run("partial_struct", func(t *testing.T) {
		var usage acp.UsageUpdate
		decode(t, `{"used":5,"size":10,"cost":{"amount":1.25,"currency":false}}`, &usage)
		if usage.Cost != nil {
			t.Error("invalid cost retained an allocated/partially decoded struct")
		}
		if usage.Used != 5 || usage.Size != 10 {
			t.Errorf("valid usage counters were lost: used=%d size=%d", usage.Used, usage.Size)
		}
		equalJSON(t, marshal(t, usage), `{"used":5,"size":10}`)
	})
}

// Local annotation-propagation regressions beyond rustv1's proxy-only case.
// Invalid optional metadata must not discard the ordinary text/update payload.
// Null preservation and default-on-error are independent: invalid metadata is
// absent after recovery, while the null-sensitive control retains explicit null.
func TestAnnotatedMetadataKeepsPayload(t *testing.T) {
	for _, tt := range []struct {
		name        string
		new         func() any
		input, want string
	}{
		{"content", func() any { return &acp.ContentBlock{} },
			`{"type":"text","text":"keep","_meta":42}`, `{"type":"text","text":"keep"}`},
		{"update", func() any { return &acp.SessionUpdate{} },
			`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"keep"},"_meta":42}`, `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"keep"}}`},
		{"nested_content", func() any { return &acp.SessionUpdate{} },
			`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"keep","_meta":42}}`, `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"keep"}}`},
		{"null_sensitive_invalid_control", func() any { return &acp.SessionUpdate{} },
			`{"sessionUpdate":"subagent_update","sessionId":"child","_meta":42}`, `{"sessionUpdate":"subagent_update","sessionId":"child"}`},
		{"null_sensitive_null_control", func() any { return &acp.SessionUpdate{} },
			`{"sessionUpdate":"subagent_update","sessionId":"child","_meta":null}`, `{"sessionUpdate":"subagent_update","sessionId":"child","_meta":null}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			value := tt.new()
			decode(t, tt.input, value)
			equalJSON(t, marshal(t, value), tt.want)
		})
	}
}
