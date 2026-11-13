// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// TestConfigOptionsSkipInvalidItems checks that a config option list drops
// each option whose payload is invalid for its type, keeping the valid ones in
// order. The invalid options have known types but a currentValue of the wrong
// kind, so checking the type tag alone is not enough. Every carrier of the
// list behaves the same.
func TestConfigOptionsSkipInvalidItems(t *testing.T) {
	const (
		validSelect    = `{"type":"select","id":"model","name":"Model","currentValue":"fast","options":[{"value":"fast","name":"Fast"}]}`
		invalidSelect  = `{"type":"select","id":"bad-select","name":"Bad select","currentValue":true,"options":[{"value":"fast","name":"Fast"}]}`
		validBoolean   = `{"type":"boolean","id":"auto","name":"Auto","currentValue":false}`
		invalidBoolean = `{"type":"boolean","id":"bad-boolean","name":"Bad boolean","currentValue":"false"}`
		mixed          = `[` + validSelect + `,` + invalidSelect + `,` + validBoolean + `,` + invalidBoolean + `]`
		kept           = `[` + validSelect + `,` + validBoolean + `]`
	)
	runDecodeCases(t, []decodeCase{
		{name: "optional_field", new: alloc[acp.NewSessionResponse],
			input: `{"sessionId":"s1","configOptions":` + mixed + `}`, want: `{"sessionId":"s1","configOptions":` + kept + `}`},
		{name: "required_field", new: alloc[acp.SetSessionConfigOptionResponse],
			input: `{"configOptions":` + mixed + `}`, want: `{"configOptions":` + kept + `}`},
		{name: "session_update", new: alloc[acp.SessionUpdate],
			input: `{"sessionUpdate":"config_option_update","configOptions":` + mixed + `}`, want: `{"sessionUpdate":"config_option_update","configOptions":` + kept + `}`},
	})
}

// TestSelectConfigValueType checks that a select option's current value and a
// setter's value decode as SessionConfigValueId, the type the constructors
// use, so decoded values compare equal to option IDs.
func TestSelectConfigValueType(t *testing.T) {
	const id acp.SessionConfigValueId = "fast"
	options := acp.UngroupedSessionConfigSelectOptions{{Value: id, Name: "Fast"}}
	var option acp.SessionConfigOption
	decode(t, string(acptest.Marshal(t, acp.SelectSessionConfigOption("model", "Model", id, acp.SessionConfigSelectOptions{Ungrouped: &options}))), &option)
	if option.Options.Ungrouped == nil || len(*option.Options.Ungrouped) != 1 {
		t.Fatal("select options were lost")
	}
	if selected := (*option.Options.Ungrouped)[0].Value; option.CurrentValue != selected {
		t.Errorf("current value %v (%T) does not equal option ID %q", option.CurrentValue, option.CurrentValue, selected)
	}
	if value, ok := option.CurrentValue.(acp.SessionConfigValueId); !ok || value != id {
		t.Errorf("current value = %v (%T), want SessionConfigValueId(%q)", option.CurrentValue, option.CurrentValue, id)
	}
	acptest.EqualMarshaled(t, option, `{"type":"select","id":"model","name":"Model","currentValue":"fast","options":[{"value":"fast","name":"Fast"}]}`)

	var request acp.SetSessionConfigOptionRequest
	decode(t, string(acptest.Marshal(t, acp.ValueIdSetSessionConfigOptionRequest("s1", "model", id))), &request)
	if request.Value != id {
		t.Errorf("setter value %v (%T) does not equal %q", request.Value, request.Value, id)
	}
	if value, ok := request.Value.(acp.SessionConfigValueId); !ok || value != id {
		t.Errorf("setter value = %v (%T), want SessionConfigValueId(%q)", request.Value, request.Value, id)
	}
	acptest.EqualMarshaled(t, request, `{"sessionId":"s1","configId":"model","value":"fast"}`)
}

// TestBooleanConfigValueType checks that a boolean option and setter keep a
// false value as bool rather than omitting it or turning it into an ID.
func TestBooleanConfigValueType(t *testing.T) {
	var option acp.SessionConfigOption
	decode(t, string(acptest.Marshal(t, acp.BooleanSessionConfigOption("auto", "Auto", false))), &option)
	if value, ok := option.CurrentValue.(bool); !ok || value {
		t.Errorf("current value = %v (%T), want false", option.CurrentValue, option.CurrentValue)
	}
	acptest.EqualMarshaled(t, option, `{"type":"boolean","id":"auto","name":"Auto","currentValue":false}`)

	var request acp.SetSessionConfigOptionRequest
	decode(t, string(acptest.Marshal(t, acp.BooleanSetSessionConfigOptionRequest("s1", "auto", false))), &request)
	if value, ok := request.Value.(bool); !ok || value {
		t.Errorf("setter value = %v (%T), want false", request.Value, request.Value)
	}
	acptest.EqualMarshaled(t, request, `{"type":"boolean","sessionId":"s1","configId":"auto","value":false}`)
}
