package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// Local V1 fidelity regressions informed by external schema crate 1.15.1
// src/v1/elicitation.rs::unknown_action_response_serialization and
// ::unknown_action_does_not_hide_known_action. They are not Rust SDK-workspace
// ports. The unknown branch preserves fields; its `not` excludes known tags.
// This does not require an agent to execute an unknown action, nor validate
// application-specific answers against a particular requested form schema.
func TestElicitationResponseBranches(t *testing.T) {
	for _, tt := range []struct{ name, input string }{
		{"unknown_vendor_data", `{"action":"_vendor","vendorData":{"x":1}}`},
		// content is a known field only on accept. In an unknown branch it is
		// opaque payload and is not constrained to accept's object/map shape.
		{"unknown_known_field_name", `{"action":"_vendor","content":"opaque-vendor-value"}`},
		{"known_accept_control", `{"action":"accept","content":{"name":"Ada"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var response acp.CreateElicitationResponse
			decode(t, tt.input, &response)
			equalJSON(t, marshal(t, response), tt.input)
		})
	}
	for _, tt := range []struct{ name, input string }{
		{"known_bad_container", `{"action":"accept","content":"opaque-vendor-value"}`},
		// ElicitationContentValue allows primitives and string arrays, not
		// objects. No recovery annotation applies to this known payload.
		{"known_bad_value", `{"action":"accept","content":{"name":{"nested":true}}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var response acp.CreateElicitationResponse
			if err := json.Unmarshal([]byte(tt.input), &response); err == nil {
				t.Errorf("malformed known accept content was accepted (action=%q); known-branch validation must not be bypassed", response.Action)
			}
		})
	}
}
