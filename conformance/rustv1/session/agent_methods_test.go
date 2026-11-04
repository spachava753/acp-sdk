// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
package session_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust: src/agent-client-protocol-test/tests/testy.rs::testy_handles_stable_agent_requests.
// Ports actual public method/parameter/result/error transport, not the absent
// Testy agent's authentication, validation, session-store or cancellation policy.
func TestTestyStableAgentMethodTransport(t *testing.T) {
	ctx, client, peer := clientPeer(t, nil)
	cwd := "/tmp"
	session := acp.SessionId("testy-session-1")
	const options = `[{"type":"select","id":"verbosity","name":"Verbosity","currentValue":"verbose","options":[{"value":"brief","name":"Brief"},{"value":"normal","name":"Normal"},{"value":"verbose","name":"Verbose"}]}]`
	type step struct {
		method, params, response string
		failure, notification    bool
		call                     func() (any, error)
	}
	steps := []step{
		{method: "initialize", params: `{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":true,"writeTextFile":true},"terminal":true}}`, response: `{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"promptCapabilities":{"image":true,"audio":true,"embeddedContext":true}},"authMethods":[{"id":"testy-agent-auth","name":"Testy agent auth"}]}`, call: func() (any, error) {
			return client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1, ClientCapabilities: &acp.ClientCapabilities{Fs: &acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true}, Terminal: true}})
		}},
		{method: "authenticate", params: `{"methodId":"unknown-auth-method"}`, failure: true, call: func() (any, error) {
			return client.Authenticate(ctx, &acp.AuthenticateRequest{MethodID: "unknown-auth-method"})
		}},
		{method: "authenticate", params: `{"methodId":"testy-agent-auth"}`, response: `{}`, call: func() (any, error) {
			return client.Authenticate(ctx, &acp.AuthenticateRequest{MethodID: "testy-agent-auth"})
		}},
		{method: "logout", params: `{}`, response: `{}`, call: func() (any, error) { return client.Logout(ctx, &acp.LogoutRequest{}) }},
		{method: "session/new", params: `{"cwd":"/tmp","additionalDirectories":["/var/tmp"],"mcpServers":[]}`, response: `{"sessionId":"testy-session-1","modes":{"currentModeId":"chat","availableModes":[{"id":"chat","name":"Chat"},{"id":"plan","name":"Plan"}]},"configOptions":` + options + `}`, call: func() (any, error) {
			return client.SessionNew(ctx, &acp.NewSessionRequest{Cwd: cwd, AdditionalDirectories: []string{"/var/tmp"}, McpServers: []acp.McpServer{}})
		}},
		{method: "session/set_mode", params: `{"sessionId":"missing-session","modeId":"chat"}`, failure: true, call: func() (any, error) {
			return client.SessionSetMode(ctx, &acp.SetSessionModeRequest{SessionID: "missing-session", ModeID: "chat"})
		}},
		{method: "session/set_mode", params: `{"sessionId":"testy-session-1","modeId":"invalid-mode"}`, failure: true, call: func() (any, error) {
			return client.SessionSetMode(ctx, &acp.SetSessionModeRequest{SessionID: session, ModeID: "invalid-mode"})
		}},
		{method: "session/list", params: `{"cwd":"/tmp"}`, response: `{"sessions":[{"sessionId":"testy-session-1","cwd":"/tmp","additionalDirectories":["/var/tmp"]}]}`, call: func() (any, error) { return client.SessionList(ctx, &acp.ListSessionsRequest{Cwd: &cwd}) }},
		{method: "session/set_mode", params: `{"sessionId":"testy-session-1","modeId":"plan"}`, response: `{}`, call: func() (any, error) {
			return client.SessionSetMode(ctx, &acp.SetSessionModeRequest{SessionID: session, ModeID: "plan"})
		}},
		{method: "session/set_config_option", params: `{"sessionId":"testy-session-1","configId":"verbosity","value":"verbose"}`, response: `{"configOptions":` + options + `}`, call: func() (any, error) {
			return client.SessionSetConfigOption(ctx, &acp.SetSessionConfigOptionRequest{SessionID: session, ConfigID: "verbosity", Value: "verbose"})
		}},
		{method: "session/set_config_option", params: `{"sessionId":"missing-session","configId":"verbosity","value":"verbose"}`, failure: true, call: func() (any, error) {
			return client.SessionSetConfigOption(ctx, &acp.SetSessionConfigOptionRequest{SessionID: "missing-session", ConfigID: "verbosity", Value: "verbose"})
		}},
		{method: "session/set_config_option", params: `{"sessionId":"testy-session-1","configId":"unknown","value":"verbose"}`, failure: true, call: func() (any, error) {
			return client.SessionSetConfigOption(ctx, &acp.SetSessionConfigOptionRequest{SessionID: session, ConfigID: "unknown", Value: "verbose"})
		}},
		{method: "session/set_config_option", params: `{"sessionId":"testy-session-1","configId":"verbosity","value":"loud"}`, failure: true, call: func() (any, error) {
			return client.SessionSetConfigOption(ctx, &acp.SetSessionConfigOptionRequest{SessionID: session, ConfigID: "verbosity", Value: "loud"})
		}},
		{method: "session/cancel", params: `{"sessionId":"testy-session-1"}`, notification: true, call: func() (any, error) {
			return nil, client.SessionCancel(ctx, &acp.CancelNotification{SessionID: session})
		}},
		{method: "session/prompt", params: `{"sessionId":"testy-session-1","prompt":[{"type":"text","text":"{\"command\":\"greet\"}"}]}`, response: `{"stopReason":"end_turn"}`, call: func() (any, error) {
			return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: `{"command":"greet"}`}}})
		}},
		{method: "session/prompt", params: `{"sessionId":"testy-session-1","prompt":[{"type":"text","text":"{\"command\":\"run_scenario\",\"scenario\":\"cancel_status\"}"}]}`, response: `{"stopReason":"cancelled"}`, call: func() (any, error) {
			return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: `{"command":"run_scenario","scenario":"cancel_status"}`}}})
		}},
	}
	for _, step := range steps {
		result := startCall(step.call)
		request := peer.read(t)
		if request.Method != step.method || (len(request.ID) == 0) != step.notification {
			t.Fatalf("request = %+v, expected method %q notification=%v", request, step.method, step.notification)
		}
		assertJSON(t, request.Params, step.params)
		if step.notification {
			if got := await(t, result); got.err != nil {
				t.Fatal(got.err)
			}
			continue
		}
		if step.failure {
			peer.failure(t, request, jsonrpc.CodeInvalidParams, "Invalid params", "scripted peer rejection")
			if got := await(t, result); got.err == nil {
				t.Fatalf("peer rejection for %s was lost", step.method)
			}
			continue
		}
		peer.result(t, request, json.RawMessage(step.response))
		value := valueOf(t, result)
		// Keep the source's typed observations rather than requiring an exact
		// re-encoding of optional/defaulted fields that it never asserts.
		switch response := value.(type) {
		case *acp.InitializeResponse:
			if response.AgentCapabilities == nil || !response.AgentCapabilities.LoadSession || response.AgentCapabilities.PromptCapabilities == nil || !response.AgentCapabilities.PromptCapabilities.Image || !response.AgentCapabilities.PromptCapabilities.Audio || !response.AgentCapabilities.PromptCapabilities.EmbeddedContext || len(response.AuthMethods) != 1 {
				t.Fatalf("initialize capabilities/auth methods not preserved: %#v", response)
			}
		case *acp.NewSessionResponse:
			if response.SessionID != session || response.Modes == nil || response.ConfigOptions == nil {
				t.Fatalf("new session result not preserved: %#v", response)
			}
		case *acp.ListSessionsResponse:
			if len(response.Sessions) != 1 || response.Sessions[0].SessionID != session || len(response.Sessions[0].AdditionalDirectories) != 1 || response.Sessions[0].AdditionalDirectories[0] != "/var/tmp" {
				t.Fatalf("list session/additional directories not preserved: %#v", response)
			}
		case *acp.SetSessionConfigOptionResponse:
			if len(response.ConfigOptions) != 1 {
				t.Fatalf("config options length = %d", len(response.ConfigOptions))
			}
		case *acp.PromptResponse:
			var expected struct {
				StopReason string `json:"stopReason"`
			}
			if err := json.Unmarshal([]byte(step.response), &expected); err != nil {
				t.Fatal(err)
			}
			if string(response.StopReason) != expected.StopReason {
				t.Fatalf("stop reason = %q, want %q", response.StopReason, expected.StopReason)
			}
		}
	}
}
