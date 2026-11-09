// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// TestUpdateVariants checks that every session update variant sent during a
// prompt reaches the update handler, typed and for the right session.
func TestUpdateVariants(t *testing.T) {
	h := newUpdateRecorder()
	client, peer := startSession(t, h, nil)
	result := prompt(client, "s", "updates")
	request := peer.Expect("session/prompt")
	variants := []struct {
		kind   acp.SessionUpdateType
		update string
	}{
		{acp.SessionUpdateTypeSessionInfoUpdate, `{"sessionUpdate":"session_info_update","title":"Session title","updatedAt":"2026-01-01T00:00:00Z"}`},
		{acp.SessionUpdateTypeCurrentModeUpdate, `{"sessionUpdate":"current_mode_update","currentModeId":"chat"}`},
		{acp.SessionUpdateTypeConfigOptionUpdate, `{"sessionUpdate":"config_option_update","configOptions":[]}`},
		{acp.SessionUpdateTypeAvailableCommandsUpdate, `{"sessionUpdate":"available_commands_update","availableCommands":[]}`},
		{acp.SessionUpdateTypeUsageUpdate, `{"sessionUpdate":"usage_update","used":128,"size":4096,"cost":{"amount":0,"currency":"USD"}}`},
		{acp.SessionUpdateTypeUserMessageChunk, `{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"received 1 prompt content block(s)"}}`},
		{acp.SessionUpdateTypeAgentThoughtChunk, `{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking"}}`},
		{acp.SessionUpdateTypeAgentMessageChunk, `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"text content block"}}`},
		{acp.SessionUpdateTypeToolCall, `{"sessionUpdate":"tool_call","toolCallId":"read","title":"Read file","kind":"read","status":"pending"}`},
		{acp.SessionUpdateTypeToolCallUpdate, `{"sessionUpdate":"tool_call_update","toolCallId":"read","status":"in_progress"}`},
		{acp.SessionUpdateTypePlan, `{"sessionUpdate":"plan","entries":[{"content":"Emit session updates","priority":"high","status":"completed"}]}`},
	}
	for _, v := range variants {
		peer.Notify("session/update", map[string]any{"sessionId": "s", "update": json.RawMessage(v.update)})
	}
	endTurn(peer, request)
	expectStop(t, result, acp.StopReasonEndTurn)
	seen := map[acp.SessionUpdateType]bool{}
	for range variants {
		update := h.next(t)
		if update.SessionID != "s" {
			t.Errorf("update for session %q, want s", update.SessionID)
		}
		seen[update.Update.SessionUpdate] = true
	}
	for _, v := range variants {
		if !seen[v.kind] {
			t.Errorf("missing update variant %q", v.kind)
		}
	}
}

// TestMessageIDsAcrossPrompts checks that the message ID and content of user,
// thought and agent chunks reach the handler unchanged across two prompts on
// the same session.
func TestMessageIDsAcrossPrompts(t *testing.T) {
	h := newUpdateRecorder()
	client, peer := startSession(t, h, nil)
	for round := 1; round <= 2; round++ {
		result := prompt(client, "s", "greet")
		request := peer.Expect("session/prompt")
		want := map[acp.SessionUpdateType]string{}
		for _, kind := range []acp.SessionUpdateType{acp.SessionUpdateTypeUserMessageChunk, acp.SessionUpdateTypeAgentThoughtChunk, acp.SessionUpdateTypeAgentMessageChunk} {
			id := fmt.Sprintf("%s-%d", kind, round)
			want[kind] = id
			peer.Notify("session/update", map[string]any{"sessionId": "s", "update": map[string]any{"sessionUpdate": kind, "messageId": id, "content": map[string]any{"type": "text", "text": "Hello, world!"}}})
		}
		endTurn(peer, request)
		expectStop(t, result, acp.StopReasonEndTurn)
		for range 3 {
			update := h.next(t)
			id, ok := want[update.Update.SessionUpdate]
			if !ok || update.Update.MessageID == nil || string(*update.Update.MessageID) != id || text(t, update) != "Hello, world!" {
				t.Fatalf("round %d: message ID or content lost: %#v", round, update)
			}
			delete(want, update.Update.SessionUpdate)
		}
	}
}

// toolTracker projects tool call status from updates, as a client UI would.
type toolTracker struct {
	mu       sync.Mutex
	status   map[acp.ToolCallId]acp.ToolCallStatus
	unknown  []acp.ToolCallId // updates for calls not yet announced
	patches  int
	problems []string
	// applied is signaled after each update's state change; it is buffered
	// for every update so callbacks never wait for the test.
	applied chan struct{}
}

func (h *toolTracker) SessionUpdate(_ context.Context, params *acp.SessionNotification) error {
	h.mu.Lock()
	switch params.Update.SessionUpdate {
	case acp.SessionUpdateTypeToolCall:
		if params.Update.Status == nil {
			h.problems = append(h.problems, fmt.Sprintf("announcement of %s lost its status", params.Update.ToolCallID))
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
	h.applied <- struct{}{}
	return nil
}

// TestToolUpdatesFollowAnnouncements checks that updates apply in the order
// received: over two prompts with two tool calls each, every tool_call_update
// finds its tool_call already applied, and every call ends completed.
func TestToolUpdatesFollowAnnouncements(t *testing.T) {
	const updatesPerRound = 5
	h := &toolTracker{status: map[acp.ToolCallId]acp.ToolCallStatus{}, applied: make(chan struct{}, 2*updatesPerRound)}
	client, peer := startSession(t, h, nil)
	for round := 1; round <= 2; round++ {
		result := prompt(client, "s", "tools")
		request := peer.Expect("session/prompt")
		read, edit := fmt.Sprintf("read-%d", round), fmt.Sprintf("edit-%d", round)
		for _, update := range []map[string]any{
			{"sessionUpdate": "tool_call", "toolCallId": read, "title": "Read file", "status": "pending"},
			{"sessionUpdate": "tool_call_update", "toolCallId": read, "status": "in_progress"},
			{"sessionUpdate": "tool_call_update", "toolCallId": read, "status": "completed"},
			{"sessionUpdate": "tool_call", "toolCallId": edit, "title": "Edit file", "status": "pending"},
			{"sessionUpdate": "tool_call_update", "toolCallId": edit, "status": "completed"},
		} {
			peer.Notify("session/update", map[string]any{"sessionId": "s", "update": update})
		}
		endTurn(peer, request)
		expectStop(t, result, acp.StopReasonEndTurn)
	}
	for range 2 * updatesPerRound {
		acptest.Await(t, h.applied)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.patches != 6 || len(h.unknown) != 0 || len(h.status) != 4 || len(h.problems) != 0 {
		t.Fatalf("patches=%d unannounced=%v statuses=%v problems=%v; want 6 patches to 4 announced calls", h.patches, h.unknown, h.status, h.problems)
	}
	for id, status := range h.status {
		if status != acp.ToolCallStatusCompleted {
			t.Errorf("call %q status = %q, want completed", id, status)
		}
	}
}
