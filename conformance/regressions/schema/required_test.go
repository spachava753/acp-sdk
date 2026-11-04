package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// Local additions beyond rustv1's ordinary missing-key cases. A required
// non-null field must not silently become the Go zero value when its key is
// present as null. These exercise ordinary scalar, array and nested-union
// paths, not application session lookup or supported-version policy.
func TestRequiredFieldsRejectExplicitNull(t *testing.T) {
	for _, tt := range []struct {
		name  string
		new   func() any
		input string
	}{
		{"protocol_version", func() any { return &acp.InitializeRequest{} }, `{"protocolVersion":null}`},
		{"session_id", func() any { return &acp.PromptRequest{} }, `{"sessionId":null,"prompt":[]}`},
		{"prompt_array", func() any { return &acp.PromptRequest{} }, `{"sessionId":"s1","prompt":null}`},
		{"nested_text", func() any { return &acp.PromptRequest{} }, `{"sessionId":"s1","prompt":[{"type":"text","text":null}]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			value := tt.new()
			if err := json.Unmarshal([]byte(tt.input), value); err == nil {
				t.Errorf("required non-null field accepted null: %s decoded as %s", tt.input, marshal(t, value))
			}
		})
	}
	// The schema's zero/empty values remain structurally valid. In particular,
	// version 0 is a uint16 value, not a claim an agent must negotiate version 0;
	// an empty session ID is not a claim an application has such a session.
	for _, tt := range []struct {
		name        string
		new         func() any
		input, want string
	}{
		{"zero_version_control", func() any { return &acp.InitializeRequest{} }, `{"protocolVersion":0}`, `{"protocolVersion":0}`},
		{"empty_id_and_prompt_control", func() any { return &acp.PromptRequest{} }, `{"sessionId":"","prompt":[]}`, `{"sessionId":"","prompt":[]}`},
		{"empty_text_control", func() any { return &acp.PromptRequest{} }, `{"sessionId":"s1","prompt":[{"type":"text","text":""}]}`, `{"sessionId":"s1","prompt":[{"type":"text","text":""}]}`},
		// Unlike prompt, mcpServers explicitly has default-on-null. Preserve
		// that exception while adding required/non-null validation elsewhere.
		{"default_on_null_mcp_control", func() any { return &acp.NewSessionRequest{} }, `{"cwd":"/workspace","mcpServers":null}`, `{"cwd":"/workspace","mcpServers":[]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			value := tt.new()
			decode(t, tt.input, value)
			equalJSON(t, marshal(t, value), tt.want)
		})
	}
}
