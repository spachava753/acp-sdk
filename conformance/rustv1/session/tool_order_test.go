// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// Rust: src/agent-client-protocol-test/tests/testy.rs::testy_uses_unique_message_ids_per_prompt_response
// and testy_repeated_scenarios_use_fresh_protocol_ids, receiving SDK slice only.
// The peer supplies distinct opaque user/thought/agent IDs across two prompts;
// Go is not asked to implement the Testy application's ID generator.
func TestMessageIDsSurviveRepeatedPrompts(t *testing.T) {
	handler := newUpdateHandler()
	ctx, client, peer := clientPeer(t, handler)
	prepareClientSession(t, ctx, client, peer, nil)
	for round := 1; round <= 2; round++ {
		result := startCall(func() (*acp.PromptResponse, error) {
			return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "s", Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "greet"}}})
		})
		prompt := peer.read(t)
		expected := map[acp.SessionUpdateType]string{}
		for _, kind := range []acp.SessionUpdateType{acp.SessionUpdateTypeUserMessageChunk, acp.SessionUpdateTypeAgentThoughtChunk, acp.SessionUpdateTypeAgentMessageChunk} {
			id := fmt.Sprintf("testy-%s-%d", kind, round)
			expected[kind] = id
			peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s", "update": map[string]any{"sessionUpdate": kind, "messageId": id, "content": map[string]any{"type": "text", "text": "Hello, world!"}}}})
		}
		peer.result(t, prompt, map[string]any{"stopReason": "end_turn"})
		if valueOf(t, result).StopReason != acp.StopReasonEndTurn {
			t.Fatal("wrong stop reason")
		}
		for range 3 {
			update := await(t, handler.updates)
			id, ok := expected[update.Update.SessionUpdate]
			if !ok || update.Update.MessageID == nil || string(*update.Update.MessageID) != id || updateText(t, update) != "Hello, world!" {
				t.Fatalf("message ID/content not preserved: %#v", update)
			}
			delete(expected, update.Update.SessionUpdate)
		}
	}
}

type toolProjection struct {
	mu       sync.Mutex
	status   map[acp.ToolCallId]acp.ToolCallStatus
	unknown  []acp.ToolCallId
	patches  int
	problems []string
	finished chan struct{}
}

func (h *toolProjection) SessionUpdate(_ context.Context, params *acp.SessionNotification) error {
	h.mu.Lock()
	switch params.Update.SessionUpdate {
	case acp.SessionUpdateTypeToolCall:
		if params.Update.Status == nil {
			h.problems = append(h.problems, "announcement lost explicit pending status")
		} else {
			h.status[params.Update.ToolCallID] = *params.Update.Status
		}
	case acp.SessionUpdateTypeToolCallUpdate:
		if _, ok := h.status[params.Update.ToolCallID]; !ok {
			h.unknown = append(h.unknown, params.Update.ToolCallID)
		}
		h.patches++
		if params.Update.Status != nil {
			h.status[params.Update.ToolCallID] = *params.Update.Status
		}
	}
	h.mu.Unlock()
	h.finished <- struct{}{} // Buffered; assertions wait for every callback's state write.
	return nil
}

// Rust: src/agent-client-protocol-test/tests/testy.rs::testy_repeated_scenarios_use_fresh_protocol_ids.
// Ports the source observer's two ToolCalls rounds: six patches must reference
// announced calls, and all four calls end completed. The Testy application's
// ID/message generation is not emulated: the peer supplies literal distinct IDs.
// This is source SDK callback-order policy, not a new blanket ACP callback MUST.
func TestToolUpdatesFollowAnnouncements(t *testing.T) {
	handler := &toolProjection{status: make(map[acp.ToolCallId]acp.ToolCallStatus), finished: make(chan struct{}, 10)}
	ctx, client, peer := clientPeer(t, handler)
	prepareClientSession(t, ctx, client, peer, nil)
	for round := 1; round <= 2; round++ {
		result := startCall(func() (*acp.PromptResponse, error) {
			return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "s", Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "tool_calls"}}})
		})
		prompt := peer.read(t)
		readID, editID := fmt.Sprintf("testy-tool-read-%d", round), fmt.Sprintf("testy-tool-edit-%d", round)
		for _, update := range []map[string]any{
			{"sessionUpdate": "tool_call", "toolCallId": readID, "title": "Read deterministic fixture", "status": "pending"},
			{"sessionUpdate": "tool_call_update", "toolCallId": readID, "status": "in_progress"},
			{"sessionUpdate": "tool_call_update", "toolCallId": readID, "status": "completed"},
			{"sessionUpdate": "tool_call", "toolCallId": editID, "title": "Apply deterministic edit", "status": "pending"},
			{"sessionUpdate": "tool_call_update", "toolCallId": editID, "status": "completed"},
		} {
			peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s", "update": update}})
		}
		peer.result(t, prompt, json.RawMessage(`{"stopReason":"end_turn"}`))
		if valueOf(t, result).StopReason != acp.StopReasonEndTurn {
			t.Fatal("wrong stop reason")
		}
	}
	for range 10 {
		await(t, handler.finished)
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.patches != 6 || len(handler.unknown) != 0 || len(handler.status) != 4 || len(handler.problems) != 0 {
		t.Fatalf("patches=%d unknown calls=%v final statuses=%v problems=%v", handler.patches, handler.unknown, handler.status, handler.problems)
	}
	for id, status := range handler.status {
		if status != acp.ToolCallStatusCompleted {
			t.Errorf("call %q status=%q, want completed", id, status)
		}
	}
}
