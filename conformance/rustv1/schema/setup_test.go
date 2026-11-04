// SPDX-License-Identifier: Apache-2.0
// Upstream: agentclientprotocol/rust-sdk@050cd94c081056ace37a7b89abfed7f6220b47eb.
// Rust sources: src/agent-client-protocol/tests/{schema_1_11,schema_1_12}.rs.
package schema_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

type setupAgent struct{ received chan acp.NewSessionRequest }

func (a *setupAgent) SessionNew(_ context.Context, req *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	// Deliberately no application validation: the upstream test concerns typed
	// parameter decoding, not an agent's session-creation/business policy.
	a.received <- *req
	return &acp.NewSessionResponse{SessionID: "session-1"}, nil
}

var _ acp.SessionNewHandler = (*setupAgent)(nil)

func expectInvalidParams(t *testing.T, method, raw string, handler any) {
	t.Helper()
	ctx, _, peer := rawAgent(t, handler)
	res := requestToAgent(t, ctx, peer, method, raw)
	var wireErr *jsonrpc.Error
	if !errors.As(res.Error, &wireErr) || wireErr.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("%s accepted invalid typed params %s: result=%s error=%v; want InvalidParams", method, raw, res.Result, res.Error)
	}
}

// schema_1_11.rs::acted_on_session_setup_lists_reject_invalid_params:
// mixed path/string and incomplete MCP server must fail typed parsing; null lists default empty.
func TestActedOnSessionSetupLists(t *testing.T) {
	for _, tt := range []struct{ name, raw string }{
		{"additionalDirectories", `{"cwd":"/repo","mcpServers":[],"additionalDirectories":["/repo/lib",42]}`},
		{"mcpServers", `{"cwd":"/repo","mcpServers":[{"name":"incomplete"}]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &setupAgent{received: make(chan acp.NewSessionRequest, 1)}
			expectInvalidParams(t, "session/new", tt.raw, a)
			// A response is only written after this request's handler returned.
			select {
			case req := <-a.received:
				t.Errorf("invalid params reached SessionNew handler: %#v", req)
			default:
			}
		})
	}
	t.Run("null_lists", func(t *testing.T) {
		var req acp.NewSessionRequest
		if err := json.Unmarshal([]byte(`{"cwd":"/repo","mcpServers":null,"additionalDirectories":null}`), &req); err != nil {
			t.Fatal(err)
		}
		if len(req.McpServers) != 0 || len(req.AdditionalDirectories) != 0 {
			t.Fatalf("null lists did not default empty: %#v", req)
		}
		a := &setupAgent{received: make(chan acp.NewSessionRequest, 1)}
		ctx, _, peer := rawAgent(t, a)
		res := requestToAgent(t, ctx, peer, "session/new", `{"cwd":"/repo","mcpServers":null,"additionalDirectories":null}`)
		if res.Error != nil {
			t.Fatal(res.Error)
		}
		select {
		case got := <-a.received:
			if len(got.McpServers) != 0 || len(got.AdditionalDirectories) != 0 {
				t.Fatalf("receiving handler did not get empty lists: %#v", got)
			}
		default:
			t.Fatal("null-list request was not dispatched to SessionNew")
		}
	})
}

// schema_1_12.rs::system_prompts_pass_through_typed_lifecycle_requests:
// unstable-v1 append mode and text content survive a new-session parameter roundtrip.
func TestSystemPromptLifecycleRequest(t *testing.T) {
	req := acp.NewSessionRequest{Cwd: "/repo", McpServers: []acp.McpServer{}, SystemPrompt: &acp.SystemPrompt{
		Mode: acp.SystemPromptModeAppend, Content: []acp.ContentBlock{acp.TextContentBlock("Review without editing.")},
	}}
	const literal = `{"cwd":"/repo","mcpServers":[],"systemPrompt":{"mode":"append","content":[{"type":"text","text":"Review without editing."}]}}`
	assertJSON(t, encode(t, req), literal)
	var parsed acp.NewSessionRequest
	roundTrip(t, literal, &parsed)
	if parsed.SystemPrompt == nil || parsed.SystemPrompt.Mode != acp.SystemPromptModeAppend || len(parsed.SystemPrompt.Content) != 1 || parsed.SystemPrompt.Content[0].Text != "Review without editing." {
		t.Fatalf("system prompt = %#v", parsed.SystemPrompt)
	}
	ctx, client, peer := rawClient(t, nil)
	exchange(t, ctx, peer, "session/new", literal, `{"sessionId":"session-1"}`, func() (any, error) { return client.SessionNew(ctx, &req) })
	a := &setupAgent{received: make(chan acp.NewSessionRequest, 1)}
	ctx, _, peer = rawAgent(t, a)
	res := requestToAgent(t, ctx, peer, "session/new", literal)
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	select {
	case got := <-a.received:
		assertJSON(t, encode(t, got), literal)
	default:
		t.Fatal("system prompt request was not dispatched to SessionNew")
	}
}

// schema_1_12.rs::malformed_system_prompts_are_invalid_params_not_silently_omitted:
// scalar, missing mode, and a known content variant missing its required text.
func TestMalformedSystemPrompts(t *testing.T) {
	for _, tt := range []struct{ name, prompt string }{
		{"scalar", `"instructions"`},
		{"missing_mode", `{"content":[]}`},
		{"missing_text", `{"mode":"append","content":[{"type":"text"}]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &setupAgent{received: make(chan acp.NewSessionRequest, 1)}
			expectInvalidParams(t, "session/new", `{"cwd":"/repo","mcpServers":[],"systemPrompt":`+tt.prompt+`}`, a)
			select {
			case req := <-a.received:
				t.Errorf("malformed system prompt reached handler: %#v", req.SystemPrompt)
			default:
			}
		})
	}
}
