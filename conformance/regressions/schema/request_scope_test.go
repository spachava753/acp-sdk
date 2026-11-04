// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// RequestId has three alternatives, not merely a two-branch nullable wrapper.
// Null is a valid present ID; it must neither become forbidden nor disable
// validation of the other branches in typed containing fields.
func TestNullableRequestIDKeepsItsOtherConstraints(t *testing.T) {
	for _, raw := range []string{`null`, `"request-1"`, `7`} {
		t.Run("valid_"+raw, func(t *testing.T) {
			input := `{"requestId":` + raw + `}`
			var scope acp.ElicitationRequestScope
			decode(t, input, &scope)
			equalJSON(t, marshal(t, scope), input)
		})
	}
	for _, raw := range []string{`true`, `1.5`, `9223372036854775808`} {
		t.Run("invalid_"+raw, func(t *testing.T) {
			var scope acp.ElicitationRequestScope
			if err := json.Unmarshal([]byte(`{"requestId":`+raw+`}`), &scope); err == nil {
				t.Errorf("nullable int64/string ID accepted %s", raw)
			}
		})
	}
}

// Flattened nested anyOf branches still require one valid scope. Requiring
// neither loses the protocol contract; requiring both rejects valid requests.
func TestElicitationRetainsNestedScopeRequirements(t *testing.T) {
	for _, tt := range []struct {
		name string
		new  func() any
		base string
	}{
		{"url", func() any { return &acp.CreateElicitationRequest{} }, `"mode":"url","message":"m","elicitationId":"e","url":"https://example.com"`},
		{"form", func() any { return &acp.CreateElicitationRequest{} }, `"mode":"form","message":"m","requestedSchema":{"type":"object","properties":{"value":{"type":"string"}}}`},
		{"custom", func() any { return &acp.CreateElicitationRequest{} }, `"mode":"_vendor","message":"m","vendorData":{"kept":true}`},
		{"standalone_form", func() any { return &acp.ElicitationFormMode{} }, `"requestedSchema":{"type":"object","properties":{"value":{"type":"string"}}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte("{"+tt.base+"}"), tt.new()); err == nil {
				t.Error("elicitation without sessionId or requestId was accepted")
			}
			for _, scope := range []string{`"sessionId":"session-1"`, `"requestId":null`} {
				input := fmt.Sprintf("{%s,%s}", tt.base, scope)
				value := tt.new()
				if err := json.Unmarshal([]byte(input), value); err != nil {
					t.Errorf("valid single scope %s was rejected: %v", scope, err)
					continue
				}
				equalJSON(t, marshal(t, value), input)
			}
		})
	}
}
