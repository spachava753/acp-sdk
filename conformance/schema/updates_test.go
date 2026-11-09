// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// TestSessionUpdates checks session/update variants. Each constructed
// notification must encode to the literal and decode from it unchanged; an
// agent must send it as session/update; and a client must deliver it to
// SessionUpdate alone (not, for example, ElicitationComplete) with the
// variant's typed fields set.
func TestSessionUpdates(t *testing.T) {
	notice := acp.NoticeSessionUpdate(acp.NoticeSeverityWarning, "Provider degraded")
	notice.Description = ptr("Retry later")
	notice.Meta = acp.Meta{"source": "provider"}
	entry := acp.PlanEntry{Content: "Implement the change", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusInProgress}
	named := func(u acp.SessionUpdate) acp.SessionUpdate {
		u.Name = ptr("read_file")
		return u
	}
	// The update variant has no title argument; the initial call's title
	// comes from its constructor.
	titledUpdate := acp.ToolCallUpdateSessionUpdate("call_1")
	titledUpdate.Title = ptr("Read configuration")
	inSession := func(u acp.SessionUpdate) acp.SessionNotification {
		return acp.SessionNotification{SessionID: "session-1", Update: u}
	}

	for _, tt := range []struct {
		name         string
		notification acp.SessionNotification
		literal      string
		check        func(t *testing.T, n *acp.SessionNotification)
	}{
		{
			name: "notice", notification: inSession(notice),
			literal: `{"sessionId":"session-1","update":{"sessionUpdate":"notice","severity":"warning","title":"Provider degraded","description":"Retry later","_meta":{"source":"provider"}}}`,
			check: func(t *testing.T, n *acp.SessionNotification) {
				if n.Update.SessionUpdate != acp.SessionUpdateTypeNotice || n.Update.Severity != acp.NoticeSeverityWarning {
					t.Errorf("notice = %#v", n.Update)
				}
			},
		},
		{
			name: "compaction_update", notification: inSession(acp.CompactionUpdateSessionUpdate("compaction-1", acp.CompactionStatusInProgress)),
			literal: `{"sessionId":"session-1","update":{"sessionUpdate":"compaction_update","compactionId":"compaction-1","status":"in_progress"}}`,
			check: func(t *testing.T, n *acp.SessionNotification) {
				if n.Update.SessionUpdate != acp.SessionUpdateTypeCompactionUpdate || n.Update.CompactionID != "compaction-1" || n.Update.Status == nil || string(*n.Update.Status) != "in_progress" {
					t.Errorf("compaction update = %#v", n.Update)
				}
			},
		},
		{
			name: "compaction_summary_chunk", notification: inSession(acp.CompactionSummaryChunkSessionUpdate("compaction-1", acp.TextContentBlock("retained context"))),
			literal: `{"sessionId":"session-1","update":{"sessionUpdate":"compaction_summary_chunk","compactionId":"compaction-1","content":{"type":"text","text":"retained context"}}}`,
			check: func(t *testing.T, n *acp.SessionNotification) {
				block, ok := n.Update.Content.(acp.ContentBlock)
				if n.Update.SessionUpdate != acp.SessionUpdateTypeCompactionSummaryChunk || !ok || block.Text != "retained context" {
					t.Errorf("compaction summary chunk = %#v", n.Update)
				}
			},
		},
		{
			name: "plan_items", notification: inSession(acp.PlanUpdateSessionUpdate(acp.ItemsPlanUpdateContent("plan-1", []acp.PlanEntry{entry}))),
			literal: `{"sessionId":"session-1","update":{"sessionUpdate":"plan_update","plan":{"type":"items","planId":"plan-1","entries":[{"content":"Implement the change","priority":"high","status":"in_progress"}]}}}`,
			check:   checkPlan("items"),
		},
		{
			name: "plan_file", notification: inSession(acp.PlanUpdateSessionUpdate(acp.FilePlanUpdateContent("plan-1", "file:///workspace/PLAN.md"))),
			literal: `{"sessionId":"session-1","update":{"sessionUpdate":"plan_update","plan":{"type":"file","planId":"plan-1","uri":"file:///workspace/PLAN.md"}}}`,
			check:   checkPlan("file"),
		},
		{
			name: "plan_markdown", notification: inSession(acp.PlanUpdateSessionUpdate(acp.MarkdownPlanUpdateContent("plan-1", "# Plan"))),
			literal: `{"sessionId":"session-1","update":{"sessionUpdate":"plan_update","plan":{"type":"markdown","planId":"plan-1","content":"# Plan"}}}`,
			check:   checkPlan("markdown"),
		},
		{
			name: "plan_removed", notification: inSession(acp.PlanRemovedSessionUpdate("plan-1")),
			literal: `{"sessionId":"session-1","update":{"sessionUpdate":"plan_removed","planId":"plan-1"}}`,
			check: func(t *testing.T, n *acp.SessionNotification) {
				if n.Update.SessionUpdate != acp.SessionUpdateTypePlanRemoved || n.Update.PlanID != "plan-1" {
					t.Errorf("plan removal = %#v", n.Update)
				}
			},
		},
		{
			// The parent session carries the update; the child is its payload.
			name: "subagent_update", notification: acp.SessionNotification{SessionID: "parent", Update: acp.SubagentUpdateSessionUpdate("child")},
			literal: `{"sessionId":"parent","update":{"sessionUpdate":"subagent_update","sessionId":"child"}}`,
			check: func(t *testing.T, n *acp.SessionNotification) {
				if n.SessionID != "parent" || n.Update.SessionID != "child" || n.Update.SessionUpdate != acp.SessionUpdateTypeSubagentUpdate {
					t.Errorf("subagent update = %#v", n)
				}
			},
		},
		{
			name: "tool_call", notification: inSession(named(acp.ToolCallSessionUpdate("call_1", "Read configuration"))),
			literal: `{"sessionId":"session-1","update":{"sessionUpdate":"tool_call","toolCallId":"call_1","title":"Read configuration","name":"read_file"}}`,
			check:   checkToolName,
		},
		{
			name: "tool_call_update", notification: inSession(named(titledUpdate)),
			literal: `{"sessionId":"session-1","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_1","title":"Read configuration","name":"read_file"}}`,
			check:   checkToolName,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			acptest.EqualMarshaled(t, tt.notification, tt.literal)
			var parsed acp.SessionNotification
			acptest.RoundTrip(t, tt.literal, &parsed)
			tt.check(t, &parsed)

			conn, peer := startAgent(t)
			expectNotification(t, peer, "session/update", tt.literal, func() error {
				return conn.SessionUpdate(t.Context(), &tt.notification)
			})
			received, ok := receiveNotification(t, clientSide, "session/update", tt.literal).(*acp.SessionNotification)
			if !ok {
				t.Fatalf("session/update handler received %T", received)
			}
			tt.check(t, received)
		})
	}
}

func checkPlan(kind string) func(*testing.T, *acp.SessionNotification) {
	return func(t *testing.T, n *acp.SessionNotification) {
		t.Helper()
		if n.Update.SessionUpdate != acp.SessionUpdateTypePlanUpdate || n.Update.Plan.PlanID != "plan-1" || string(n.Update.Plan.Type) != kind {
			t.Errorf("plan update = %#v", n.Update)
		}
	}
}

func checkToolName(t *testing.T, n *acp.SessionNotification) {
	t.Helper()
	if n.Update.Name == nil || *n.Update.Name != "read_file" {
		t.Errorf("tool call name = %v", n.Update.Name)
	}
}

// TestToolCallName checks that a tool call's name is encoded.
func TestToolCallName(t *testing.T) {
	call := acp.ToolCall{ToolCallID: "call_1", Title: "Read configuration", Name: ptr("read_file")}
	var fields map[string]json.RawMessage
	decode(t, string(acptest.Marshal(t, call)), &fields)
	acptest.EqualJSON(t, fields["name"], `"read_file"`)
}
