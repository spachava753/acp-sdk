// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// TestCapabilityMarkers checks that capabilities advertised with an empty
// marker object encode as that object rather than being omitted.
func TestCapabilityMarkers(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
		want  string
	}{
		{"session_notices", acp.ClientCapabilities{Session: &acp.ClientSessionCapabilities{Notices: ptr(acp.NoticeCapabilities{})}}, `{"session":{"notices":{}}}`},
		{"session_compaction", acp.ClientCapabilities{Session: &acp.ClientSessionCapabilities{Compaction: ptr(acp.CompactionCapabilities{})}}, `{"session":{"compaction":{}}}`},
		{"plan", acp.ClientCapabilities{Plan: &acp.PlanCapabilities{}}, `{"plan":{}}`},
		{"elicitation_modes", acp.ClientCapabilities{Elicitation: &acp.ElicitationCapabilities{Form: &acp.ElicitationFormCapabilities{}, Url: &acp.ElicitationUrlCapabilities{}}}, `{"elicitation":{"form":{},"url":{}}}`},
		{"providers", acp.AgentCapabilities{Providers: &acp.ProvidersCapabilities{}}, `{"providers":{}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			acptest.EqualMarshaled(t, tt.value, tt.want)
		})
	}
}

// TestEmptyElicitationCapability checks that an empty elicitation object stays
// present after decoding, without advertising either mode.
func TestEmptyElicitationCapability(t *testing.T) {
	var caps acp.ClientCapabilities
	decode(t, `{"elicitation":{}}`, &caps)
	if caps.Elicitation == nil {
		t.Fatal("empty elicitation capability was dropped")
	}
	if caps.Elicitation.Form != nil || caps.Elicitation.Url != nil {
		t.Errorf("empty elicitation capability advertised modes: %#v", caps.Elicitation)
	}
}
