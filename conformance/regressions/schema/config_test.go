package schema_test

import (
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// x-deserialize-skip-invalid-items applies to each full variant payload, not
// only its recognized tag. currentValue has no default-on-error annotation.
// These literals interleave two valid options and two known-tag type errors.
func TestConfigOptionsFilterInvalidBranches(t *testing.T) {
	const validSelect = `{"type":"select","id":"model","name":"Model","currentValue":"fast","options":[{"value":"fast","name":"Fast"}]}`
	const invalidSelect = `{"type":"select","id":"bad-select","name":"Bad select","currentValue":true,"options":[{"value":"fast","name":"Fast"}]}`
	const validBoolean = `{"type":"boolean","id":"auto","name":"Auto","currentValue":false}`
	const invalidBoolean = `{"type":"boolean","id":"bad-boolean","name":"Bad boolean","currentValue":"false"}`
	mixed := `[` + validSelect + `,` + invalidSelect + `,` + validBoolean + `,` + invalidBoolean + `]`
	wantItems := `[` + validSelect + `,` + validBoolean + `]`
	for _, carrier := range []struct {
		name        string
		new         func() any
		input, want string
	}{
		{"optional", func() any { return &acp.NewSessionResponse{} }, `{"sessionId":"s1","configOptions":` + mixed + `}`, `{"sessionId":"s1","configOptions":` + wantItems + `}`},
		{"required", func() any { return &acp.SetSessionConfigOptionResponse{} }, `{"configOptions":` + mixed + `}`, `{"configOptions":` + wantItems + `}`},
		{"update_union", func() any { return &acp.SessionUpdate{} }, `{"sessionUpdate":"config_option_update","configOptions":` + mixed + `}`, `{"sessionUpdate":"config_option_update","configOptions":` + wantItems + `}`},
	} {
		t.Run(carrier.name, func(t *testing.T) {
			value := carrier.new()
			decode(t, carrier.input, value)
			// Exact survivors, order and false value are independent literals.
			// Merely accepting the whole list or retaining known tags is weaker.
			equalJSON(t, marshal(t, value), carrier.want)
		})
	}
}

// Go API consistency, not an ACP wire-format requirement: constructors place a
// named SessionConfigValueId in these mixed scalar fields. Its JSON string must
// survive, and decoded values should support the same typed-ID comparisons.
func TestSelectValueIDIdentity(t *testing.T) {
	const id acp.SessionConfigValueId = "fast"
	options := acp.UngroupedSessionConfigSelectOptions{{Value: id, Name: "Fast"}}
	option := acp.SelectSessionConfigOption("model", "Model", id, acp.SessionConfigSelectOptions{Ungrouped: &options})
	var decoded acp.SessionConfigOption
	decode(t, string(marshal(t, option)), &decoded)
	if decoded.Options.Ungrouped == nil || len(*decoded.Options.Ungrouped) != 1 {
		t.Fatal("selectable ID disappeared")
	}
	selectedID := (*decoded.Options.Ungrouped)[0].Value
	if decoded.CurrentValue != selectedID {
		t.Errorf("selected value %v (%T) no longer compares equal to option ID %q", decoded.CurrentValue, decoded.CurrentValue, selectedID)
	}
	if value, ok := decoded.CurrentValue.(acp.SessionConfigValueId); !ok || value != id {
		t.Errorf("decoded CurrentValue = %v (%T), want SessionConfigValueId(%q)", decoded.CurrentValue, decoded.CurrentValue, id)
	}
	equalJSON(t, marshal(t, decoded), `{"type":"select","id":"model","name":"Model","currentValue":"fast","options":[{"value":"fast","name":"Fast"}]}`)

	request := acp.ValueIdSetSessionConfigOptionRequest("s1", "model", id)
	var received acp.SetSessionConfigOptionRequest
	decode(t, string(marshal(t, request)), &received)
	if received.Value != id {
		t.Errorf("setter value %v (%T) no longer compares equal to constructor ID %q", received.Value, received.Value, id)
	}
	if value, ok := received.Value.(acp.SessionConfigValueId); !ok || value != id {
		t.Errorf("decoded Value = %v (%T), want SessionConfigValueId(%q)", received.Value, received.Value, id)
	}
	equalJSON(t, marshal(t, received), `{"sessionId":"s1","configId":"model","value":"fast"}`)
}

// A branch-aware named-ID fix must not convert the boolean branch to an ID or
// omit false. These are SDK union/constructor controls, not JSON-library tests.
func TestBooleanConfigRoundTrips(t *testing.T) {
	option := acp.BooleanSessionConfigOption("auto", "Auto", false)
	var decoded acp.SessionConfigOption
	decode(t, string(marshal(t, option)), &decoded)
	if value, ok := decoded.CurrentValue.(bool); !ok || value {
		t.Errorf("false option became %v (%T)", decoded.CurrentValue, decoded.CurrentValue)
	}
	equalJSON(t, marshal(t, decoded), `{"type":"boolean","id":"auto","name":"Auto","currentValue":false}`)

	request := acp.BooleanSetSessionConfigOptionRequest("s1", "auto", false)
	var received acp.SetSessionConfigOptionRequest
	decode(t, string(marshal(t, request)), &received)
	if value, ok := received.Value.(bool); !ok || value {
		t.Errorf("false setter became %v (%T)", received.Value, received.Value)
	}
	equalJSON(t, marshal(t, received), `{"type":"boolean","sessionId":"s1","configId":"auto","value":false}`)
}
