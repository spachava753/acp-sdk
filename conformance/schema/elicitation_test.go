// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// TestFormElicitationRequest checks the form request and decline response
// constructors against their wire form. The route itself is covered by
// TestRequestRoutes.
func TestFormElicitationRequest(t *testing.T) {
	required := []string{"name"}
	request := acp.FormCreateElicitationRequest("Please enter your name", acp.ElicitationSchema{
		Type:       acp.ElicitationSchemaTypeObject,
		Properties: map[string]acp.ElicitationPropertySchema{"name": acp.StringElicitationPropertySchema()},
		Required:   &required,
	})
	request.SessionID = "sess_abc123"
	acptest.EqualMarshaled(t, request, elicitationForm)

	var parsed acp.CreateElicitationRequest
	acptest.RoundTrip(t, elicitationForm, &parsed)
	if parsed.Mode != acp.CreateElicitationRequestTypeForm || parsed.SessionID != "sess_abc123" {
		t.Errorf("form request = %#v", parsed)
	}
	acptest.EqualMarshaled(t, acp.DeclineCreateElicitationResponse(), `{"action":"decline"}`)
}

// TestElicitationResponseBranches checks that an unknown action keeps all its
// fields, even one named like a known field, while a known action still
// validates its content. Content values may be primitives or string arrays,
// not objects.
func TestElicitationResponseBranches(t *testing.T) {
	newResponse := alloc[acp.CreateElicitationResponse]
	runDecodeCases(t, []decodeCase{
		{name: "unknown_action", new: newResponse, input: `{"action":"_vendor","vendorData":{"x":1}}`},
		// content is only a known field of accept; elsewhere it is opaque.
		{name: "unknown_action_with_content", new: newResponse, input: `{"action":"_vendor","content":"opaque-vendor-value"}`},
		{name: "accept", new: newResponse, input: `{"action":"accept","content":{"name":"Ada"}}`},
		{name: "accept_with_scalar_content", new: newResponse, input: `{"action":"accept","content":"opaque-vendor-value"}`, reject: true},
		{name: "accept_with_object_value", new: newResponse, input: `{"action":"accept","content":{"name":{"nested":true}}}`, reject: true},
	})
}

// TestMultiSelectDecoding checks that a multi-select default drops items that
// are not strings, including null, which would otherwise decode as "". It also
// checks that items with an empty type are rejected unless they are valid
// titled items: the Go value marks titled items with an empty type, so taking
// them as a custom type would re-encode them as a different variant.
func TestMultiSelectDecoding(t *testing.T) {
	newProperty := alloc[acp.ElicitationPropertySchema]
	newItems := alloc[acp.MultiSelectItems]
	runDecodeCases(t, []decodeCase{
		{name: "default_skips_invalid_items", new: newProperty,
			input: `{"type":"array","items":{"type":"string","enum":["good"]},"default":["good",null,123]}`,
			want:  `{"type":"array","items":{"type":"string","enum":["good"]},"default":["good"]}`},
		{name: "custom_type", new: newItems, input: `{"type":"_vendor","enum":["a"]}`},
		{name: "empty_type", new: newItems, input: `{"type":""}`, reject: true},
		{name: "empty_type_titled", new: newItems,
			input: `{"type":"","anyOf":[{"const":"a","title":"A"}]}`, want: `{"anyOf":[{"const":"a","title":"A"}]}`},
	})
}

// TestNullableRequestID checks that a request ID may be null, a string or an
// int64, and that allowing null does not relax the other two.
func TestNullableRequestID(t *testing.T) {
	newScope := alloc[acp.ElicitationRequestScope]
	runDecodeCases(t, []decodeCase{
		{name: "null", new: newScope, input: `{"requestId":null}`},
		{name: "string", new: newScope, input: `{"requestId":"request-1"}`},
		{name: "integer", new: newScope, input: `{"requestId":7}`},
		{name: "bool", new: newScope, input: `{"requestId":true}`, reject: true},
		{name: "fraction", new: newScope, input: `{"requestId":1.5}`, reject: true},
		{name: "beyond_int64", new: newScope, input: `{"requestId":9223372036854775808}`, reject: true},
	})
}

// TestElicitationScope checks that every elicitation mode needs a session or
// request scope, and that either one alone is enough. A null request ID is a
// present scope.
func TestElicitationScope(t *testing.T) {
	newRequest := alloc[acp.CreateElicitationRequest]
	var cases []decodeCase
	for _, mode := range []struct {
		name    string
		new     func() any
		members string
	}{
		{"url", newRequest, `"mode":"url","message":"m","elicitationId":"e","url":"https://example.com"`},
		{"form", newRequest, `"mode":"form","message":"m","requestedSchema":{"type":"object","properties":{"value":{"type":"string"}}}`},
		{"custom", newRequest, `"mode":"_vendor","message":"m","vendorData":{"kept":true}`},
		{"standalone_form", alloc[acp.ElicitationFormMode], `"requestedSchema":{"type":"object","properties":{"value":{"type":"string"}}}`},
	} {
		cases = append(cases,
			decodeCase{name: mode.name + "_unscoped", new: mode.new, input: "{" + mode.members + "}", reject: true},
			decodeCase{name: mode.name + "_session", new: mode.new, input: "{" + mode.members + `,"sessionId":"session-1"}`},
			decodeCase{name: mode.name + "_request", new: mode.new, input: "{" + mode.members + `,"requestId":null}`},
		)
	}
	runDecodeCases(t, cases)
}
