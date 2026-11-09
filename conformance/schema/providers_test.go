// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// providerAgent serves the provider methods after initialization. Each set or
// disable handler reports itself before returning, so its response follows the
// report.
type providerAgent struct{ operations chan string }

func (*providerAgent) Initialize(_ context.Context, req *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: req.ProtocolVersion, AgentCapabilities: &acp.AgentCapabilities{Providers: &acp.ProvidersCapabilities{}}}, nil
}
func (*providerAgent) ProvidersList(context.Context, *acp.ListProvidersRequest) (*acp.ListProvidersResponse, error) {
	return &acp.ListProvidersResponse{Providers: []acp.ProviderInfo{{
		ProviderID: "main", Supported: []acp.LlmProtocol{acp.LlmProtocolAnthropic, "_gateway"}, Required: true,
		Current: &acp.ProviderCurrentConfig{ApiType: acp.LlmProtocolAnthropic, BaseUrl: "https://llm.example.test"},
	}}}, nil
}
func (a *providerAgent) ProvidersSet(_ context.Context, req *acp.SetProviderRequest) (*acp.SetProviderResponse, error) {
	if req.ProviderID != "main" || len(req.Headers) != 1 || req.Headers["X-Test-Routing"] != "tenant-demo" {
		return nil, fmt.Errorf("set provider request = %#v", req)
	}
	a.operations <- "set"
	return &acp.SetProviderResponse{}, nil
}
func (a *providerAgent) ProvidersDisable(_ context.Context, req *acp.DisableProviderRequest) (*acp.DisableProviderResponse, error) {
	if req.ProviderID != "main" {
		return nil, fmt.Errorf("disable provider ID = %q", req.ProviderID)
	}
	a.operations <- "disable"
	return &acp.DisableProviderResponse{}, nil
}

// expectOperations checks that the agent reported want, in order. The reports
// precede the responses the test has already received, so they are buffered.
func (a *providerAgent) expectOperations(t *testing.T, want ...string) {
	t.Helper()
	for _, w := range want {
		select {
		case got := <-a.operations:
			if got != w {
				t.Errorf("provider operation = %q, want %q", got, w)
			}
		default:
			t.Errorf("provider %s handler did not run before its response", w)
		}
	}
}

// TestProvidersSet checks providers/set with an open protocol value and custom
// headers: the typed request encodes to the literal, the client sends it, and
// an initialized agent routes it to ProvidersSet with the headers intact.
func TestProvidersSet(t *testing.T) {
	const literal = `{"providerId":"main","apiType":"_gateway","baseUrl":"https://llm.example.test","headers":{"X-Test-Routing":"tenant-demo"}}`
	request := acp.SetProviderRequest{ProviderID: "main", ApiType: "_gateway", BaseUrl: "https://llm.example.test", Headers: map[string]string{"X-Test-Routing": "tenant-demo"}}
	acptest.RoundTrip(t, literal, new(acp.SetProviderRequest))

	client, peer := startClient(t)
	result := expectCall(t, peer, "providers/set", literal, `{}`, func() (*acp.SetProviderResponse, error) {
		return client.ProvidersSet(t.Context(), &request)
	})
	acptest.EqualMarshaled(t, result, `{}`)

	// The agent has an Initialize handler, so it rejects other requests until
	// initialization completes.
	agent := &providerAgent{operations: make(chan string, 1)}
	_, agentPeer := acptest.StartAgent(t, agent)
	agentPeer.InitializeAgent(nil)
	m := call(t, agentPeer, "providers/set", literal)
	if m.Error != nil {
		t.Fatalf("providers/set failed: %s", m)
	}
	acptest.EqualJSON(t, m.Result, `{}`)
	agent.expectOperations(t, "set")
}

// TestProviderValues checks that unknown protocol names are kept and that an
// omitted or null current configuration decodes as absent.
func TestProviderValues(t *testing.T) {
	var protocol acp.LlmProtocol
	acptest.RoundTrip(t, `"_gateway"`, &protocol)
	if protocol != "_gateway" {
		t.Errorf("open protocol = %q", protocol)
	}
	for _, current := range []string{"", `,"current":null`} {
		var info acp.ProviderInfo
		decode(t, `{"providerId":"optional","supported":["openai"],"required":false`+current+`}`, &info)
		if info.Current != nil {
			t.Errorf("current from %q = %#v, want nil", current, info.Current)
		}
	}
}

// TestProviderClientAgentFlow connects an SDK client to an SDK agent and runs
// initialize, list, set and disable through the typed methods on both sides.
func TestProviderClientAgentFlow(t *testing.T) {
	agent := &providerAgent{operations: make(chan string, 2)}
	client := connectPair(t, agent)
	initialized, err := client.Initialize(t.Context(), &acp.InitializeRequest{ProtocolVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if initialized.AgentCapabilities == nil || initialized.AgentCapabilities.Providers == nil {
		t.Fatal("provider capability missing")
	}
	listed, err := client.ProvidersList(t.Context(), &acp.ListProvidersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	acptest.EqualMarshaled(t, listed, `{"providers":[{"providerId":"main","supported":["anthropic","_gateway"],"required":true,"current":{"apiType":"anthropic","baseUrl":"https://llm.example.test"}}]}`)
	if _, err := client.ProvidersSet(t.Context(), &acp.SetProviderRequest{ProviderID: "main", ApiType: acp.LlmProtocolAnthropic, BaseUrl: "https://llm.example.test", Headers: map[string]string{"X-Test-Routing": "tenant-demo"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ProvidersDisable(t.Context(), &acp.DisableProviderRequest{ProviderID: "main"}); err != nil {
		t.Fatal(err)
	}
	agent.expectOperations(t, "set", "disable")
}
