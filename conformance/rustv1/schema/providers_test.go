// SPDX-License-Identifier: Apache-2.0
// Upstream: agentclientprotocol/rust-sdk@050cd94c081056ace37a7b89abfed7f6220b47eb.
// Rust source: src/agent-client-protocol/tests/schema_llm_providers.rs.
package schema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// schema_llm_providers.rs::provider_requests_have_typed_v1_jsonrpc_routes:
// three typed request/result pairs, open API type and custom routing headers.
func TestProviderRequestRoutes(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		ctx, client, peer := rawClient(t, nil)
		res := exchange(t, ctx, peer, "providers/list", `{}`, `{"providers":[]}`, func() (any, error) { return client.ProvidersList(ctx, &acp.ListProvidersRequest{}) })
		assertJSON(t, encode(t, res), `{"providers":[]}`)
		incomingRequest(t, true, "providers/list", `{}`, `{"providers":[]}`)
		t.Run("not_providers_set", func(t *testing.T) {
			// Only the list provider handler is installed by this receiver;
			// providers/set must not be misclassified as a list request.
			rejectIncomingRequest(t, true, "providers/set", `{"providerId":"main","apiType":"_gateway","baseUrl":"https://llm.example.test","headers":{"X-Test-Routing":"tenant-demo"}}`)
		})
	})
	t.Run("set", func(t *testing.T) {
		ctx, client, peer := rawClient(t, nil)
		req := acp.SetProviderRequest{ProviderID: "main", ApiType: "_gateway", BaseUrl: "https://llm.example.test", Headers: map[string]string{"X-Test-Routing": "tenant-demo"}}
		const literal = `{"providerId":"main","apiType":"_gateway","baseUrl":"https://llm.example.test","headers":{"X-Test-Routing":"tenant-demo"}}`
		var parsed acp.SetProviderRequest
		roundTrip(t, literal, &parsed)
		res := exchange(t, ctx, peer, "providers/set", literal, `{}`, func() (any, error) { return client.ProvidersSet(ctx, &req) })
		assertJSON(t, encode(t, res), `{}`)
		// This source fixture uses the open _gateway value, unlike the later
		// flow's anthropic value. Exercise that exact incoming aggregate case.
		agent := &providerAgent{operations: make(chan string, 1)}
		ctx, _, peer = rawAgent(t, agent)
		// This receiver also supplies Initialize; complete its protocol setup
		// before checking provider dispatch, rather than testing the init gate.
		initialized := requestToAgent(t, ctx, peer, "initialize", `{"protocolVersion":1}`)
		if initialized.Error != nil {
			t.Fatal(initialized.Error)
		}
		reply := requestToAgent(t, ctx, peer, "providers/set", literal)
		if reply.Error != nil {
			t.Fatal(reply.Error)
		}
		assertJSON(t, reply.Result, `{}`)
		select {
		case operation := <-agent.operations:
			if operation != "set" {
				t.Fatalf("providers/set invoked %q handler", operation)
			}
		default:
			t.Fatal("open-protocol provider request did not reach ProvidersSet")
		}
	})
	t.Run("disable", func(t *testing.T) {
		ctx, client, peer := rawClient(t, nil)
		var req acp.DisableProviderRequest
		roundTrip(t, `{"providerId":"main"}`, &req)
		res := exchange(t, ctx, peer, "providers/disable", `{"providerId":"main"}`, `{}`, func() (any, error) { return client.ProvidersDisable(ctx, &req) })
		assertJSON(t, encode(t, res), `{}`)
	})
}

// schema_llm_providers.rs::provider_capabilities_and_open_protocol_values_round_trip:
// capability {}, unknown _gateway, and omitted/null optional current configuration.
func TestProviderCapabilitiesAndOpenValues(t *testing.T) {
	assertJSON(t, encode(t, acp.AgentCapabilities{Providers: &acp.ProvidersCapabilities{}}), `{"providers":{}}`)
	var protocol acp.LlmProtocol
	roundTrip(t, `"_gateway"`, &protocol)
	if protocol != "_gateway" {
		t.Fatalf("open protocol = %q", protocol)
	}
	for _, current := range []string{"", `,"current":null`} {
		var info acp.ProviderInfo
		if err := json.Unmarshal([]byte(`{"providerId":"optional","supported":["openai"],"required":false`+current+`}`), &info); err != nil {
			t.Fatal(err)
		}
		if info.Current != nil {
			t.Fatalf("optional current = %#v", info.Current)
		}
	}
}

func connectedClient(t *testing.T, handler any) (context.Context, *acp.Client) {
	t.Helper()
	ctx, clientRaw, agentRaw := endpoints(t)
	done := make(chan error, 1)
	go func() {
		done <- acp.RunAgent(ctx, openedTransport{agentRaw}, func(*acp.AgentConnection) any { return handler })
	}()
	client, err := acp.Connect(ctx, openedTransport{clientRaw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closed := make(chan error, 1)
		go func() { closed <- client.Close() }()
		bounded(t, closed, "paired client shutdown")
		clientRaw.Close() // Failure fallback; normal Close already closed this side.
		bounded(t, done, "paired agent shutdown")
		agentRaw.Close()
	})
	return ctx, client
}

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

var (
	_ acp.InitializeHandler       = (*providerAgent)(nil)
	_ acp.ProvidersListHandler    = (*providerAgent)(nil)
	_ acp.ProvidersSetHandler     = (*providerAgent)(nil)
	_ acp.ProvidersDisableHandler = (*providerAgent)(nil)
)

// schema_llm_providers.rs::v1_client_can_list_set_and_disable_providers:
// actual typed V1 initialize/list/set/disable handlers with capability negotiation.
// Provider support is optional/unstable upstream, not an unconditional ACP requirement.
func TestProviderClientAgentFlow(t *testing.T) {
	agent := &providerAgent{operations: make(chan string, 2)}
	ctx, client := connectedClient(t, agent)
	init, err := client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if init.AgentCapabilities == nil || init.AgentCapabilities.Providers == nil {
		t.Fatal("provider capability missing")
	}
	listed, err := client.ProvidersList(ctx, &acp.ListProvidersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, encode(t, listed), `{"providers":[{"providerId":"main","supported":["anthropic","_gateway"],"required":true,"current":{"apiType":"anthropic","baseUrl":"https://llm.example.test"}}]}`)
	if _, err := client.ProvidersSet(ctx, &acp.SetProviderRequest{ProviderID: "main", ApiType: acp.LlmProtocolAnthropic, BaseUrl: "https://llm.example.test", Headers: map[string]string{"X-Test-Routing": "tenant-demo"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ProvidersDisable(ctx, &acp.DisableProviderRequest{ProviderID: "main"}); err != nil {
		t.Fatal(err)
	}
	// Each response follows its handler's buffered observation; no timing-based assertion.
	for _, want := range []string{"set", "disable"} {
		select {
		case got := <-agent.operations:
			if got != want {
				t.Fatalf("operation = %q, want %q", got, want)
			}
		default:
			t.Fatalf("%s handler not observed before its response", want)
		}
	}
}
