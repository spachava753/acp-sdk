// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// TestInvalidSessionSetup checks that session/new params the agent would act
// on are validated: a malformed list item or system prompt is InvalidParams
// and never reaches SessionNew, rather than being dropped.
func TestInvalidSessionSetup(t *testing.T) {
	const prefix = `{"cwd":"/repo","mcpServers":[]`
	for _, tt := range []struct{ name, params string }{
		{"non_string_directory", prefix + `,"additionalDirectories":["/repo/lib",42]}`},
		{"incomplete_mcp_server", `{"cwd":"/repo","mcpServers":[{"name":"incomplete"}]}`},
		{"scalar_system_prompt", prefix + `,"systemPrompt":"instructions"}`},
		{"system_prompt_without_mode", prefix + `,"systemPrompt":{"content":[]}}`},
		{"system_prompt_text_without_text", prefix + `,"systemPrompt":{"mode":"append","content":[{"type":"text"}]}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRecorder()
			peer, settle := serve(t, agentSide, rec)
			expectError(t, peer, "session/new", tt.params, jsonrpc.CodeInvalidParams)
			settle()
			rec.expectNone(t)
		})
	}
}

// TestNullSessionSetupLists checks that null mcpServers and
// additionalDirectories decode as empty lists, both directly and in the
// params SessionNew receives.
func TestNullSessionSetupLists(t *testing.T) {
	const params = `{"cwd":"/repo","mcpServers":null,"additionalDirectories":null}`
	var request acp.NewSessionRequest
	decode(t, params, &request)
	if len(request.McpServers) != 0 || len(request.AdditionalDirectories) != 0 {
		t.Errorf("null lists decoded as %#v", request)
	}

	rec := newRecorder()
	peer, _ := serve(t, agentSide, rec)
	if m := call(t, peer, "session/new", params); m.Error != nil {
		t.Fatalf("session/new failed: %s", m)
	}
	got := acptest.Await(t, rec.calls)
	received, ok := got.params.(*acp.NewSessionRequest)
	if got.method != "session/new" || !ok {
		t.Fatalf("session/new reached the %s %s handler", got.method, got.kind)
	}
	if len(received.McpServers) != 0 || len(received.AdditionalDirectories) != 0 {
		t.Errorf("SessionNew received %#v", received)
	}
}

// TestSystemPrompt checks that a session/new system prompt with append mode
// and text content survives encoding, decoding, sending and receiving.
func TestSystemPrompt(t *testing.T) {
	const literal = `{"cwd":"/repo","mcpServers":[],"systemPrompt":{"mode":"append","content":[{"type":"text","text":"Review without editing."}]}}`
	request := acp.NewSessionRequest{Cwd: "/repo", McpServers: []acp.McpServer{}, SystemPrompt: &acp.SystemPrompt{
		Mode: acp.SystemPromptModeAppend, Content: []acp.ContentBlock{acp.TextContentBlock("Review without editing.")},
	}}
	acptest.EqualMarshaled(t, request, literal)

	var parsed acp.NewSessionRequest
	acptest.RoundTrip(t, literal, &parsed)
	if p := parsed.SystemPrompt; p == nil || p.Mode != acp.SystemPromptModeAppend || len(p.Content) != 1 || p.Content[0].Text != "Review without editing." {
		t.Errorf("system prompt = %#v", parsed.SystemPrompt)
	}

	client, peer := startClient(t)
	expectCall(t, peer, "session/new", literal, `{"sessionId":"session-1"}`, func() (*acp.NewSessionResponse, error) {
		return client.SessionNew(t.Context(), &request)
	})
	receiveRequest(t, agentSide, "session/new", literal, `{"sessionId":"session-1"}`)
}
