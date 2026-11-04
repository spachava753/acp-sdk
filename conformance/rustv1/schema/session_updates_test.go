// SPDX-License-Identifier: Apache-2.0
// Upstream: agentclientprotocol/rust-sdk@050cd94c081056ace37a7b89abfed7f6220b47eb.
// Rust sources: src/agent-client-protocol/tests/{schema_1_11,schema_session_compaction,
// schema_plan_operations,schema_subagents,schema_tool_call_name}.rs.
package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

func sessionUpdate(t *testing.T, update acp.SessionUpdate, literal string) acp.SessionNotification {
	t.Helper()
	n := acp.SessionNotification{SessionID: "session-1", Update: update}
	assertJSON(t, encode(t, n), literal)
	var parsed acp.SessionNotification
	roundTrip(t, literal, &parsed)
	ctx, conn, peer := rawAgent(t, nil)
	notification(t, ctx, peer, "session/update", literal, func() error { return conn.SessionUpdate(ctx, &n) })
	// Rust's AgentNotification::parse_message assertion needs a real incoming
	// receiving path as well; an outgoing raw capture cannot prove dispatch.
	received, ok := incomingNotification(t, false, "session/update", literal).(*acp.SessionNotification)
	if !ok || received == nil {
		t.Fatalf("session/update reached the wrong typed handler: %#v", received)
	}
	return *received
}

// schema_1_11.rs::stable_notices_pass_through_typed_session_notifications:
// advertised notice marker, warning payload, description/meta, typed notification route.
func TestStableNotices(t *testing.T) {
	caps := acp.ClientCapabilities{Session: &acp.ClientSessionCapabilities{Notices: ptr(acp.NoticeCapabilities{})}}
	assertJSON(t, encode(t, caps), `{"session":{"notices":{}}}`)
	const literal = `{"sessionId":"session-1","update":{"sessionUpdate":"notice","severity":"warning","title":"Provider degraded","description":"Retry later","_meta":{"source":"provider"}}}`
	var n acp.SessionNotification
	roundTrip(t, literal, &n)
	if n.Update.SessionUpdate != acp.SessionUpdateTypeNotice || n.Update.Severity != acp.NoticeSeverityWarning {
		t.Fatalf("notice variant = %#v", n.Update)
	}
	sessionUpdate(t, n.Update, literal)
}

// schema_1_11.rs::v2_error_and_custom_stop_reasons_pass_through_typed_notifications
// contains this explicit V1 assertion even though the enclosing Rust test is v2-gated.
func TestV1PromptRejectsV2ErrorStopReason(t *testing.T) {
	var response acp.PromptResponse
	if err := json.Unmarshal([]byte(`{"stopReason":"error"}`), &response); err == nil {
		t.Fatal("V1 PromptResponse accepted V2's error stop reason; upstream requires a JSON-RPC error instead")
	}
}

// schema_session_compaction.rs::v1_compaction_capability_and_updates_pass_through_session_notifications:
// capability marker, in-progress upsert and summary chunk, method and typed union routing.
func TestCompactionNotifications(t *testing.T) {
	caps := acp.ClientCapabilities{Session: &acp.ClientSessionCapabilities{Compaction: ptr(acp.CompactionCapabilities{})}}
	assertJSON(t, encode(t, caps), `{"session":{"compaction":{}}}`)
	n := sessionUpdate(t, acp.CompactionUpdateSessionUpdate("compaction-1", acp.CompactionStatusInProgress),
		`{"sessionId":"session-1","update":{"sessionUpdate":"compaction_update","compactionId":"compaction-1","status":"in_progress"}}`)
	if n.Update.SessionUpdate != acp.SessionUpdateTypeCompactionUpdate || n.Update.CompactionID != "compaction-1" || n.Update.Status == nil || string(*n.Update.Status) != "in_progress" {
		t.Fatalf("compaction upsert = %#v", n.Update)
	}
	n = sessionUpdate(t, acp.CompactionSummaryChunkSessionUpdate("compaction-1", acp.TextContentBlock("retained context")),
		`{"sessionId":"session-1","update":{"sessionUpdate":"compaction_summary_chunk","compactionId":"compaction-1","content":{"type":"text","text":"retained context"}}}`)
	block, ok := n.Update.Content.(acp.ContentBlock)
	if n.Update.SessionUpdate != acp.SessionUpdateTypeCompactionSummaryChunk || !ok || block.Text != "retained context" {
		t.Fatalf("summary chunk = %#v", n.Update)
	}
}

// schema_plan_operations.rs::v1_plan_operations_are_capability_gated_and_routed:
// advertised marker, all three content variants, plan IDs and removal notification.
// Like Rust, this checks capability representation, not automatic runtime gating.
func TestPlanOperations(t *testing.T) {
	assertJSON(t, encode(t, acp.ClientCapabilities{Plan: &acp.PlanCapabilities{}}), `{"plan":{}}`)
	entry := acp.PlanEntry{Content: "Implement the change", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusInProgress}
	for _, tt := range []struct {
		name    string
		content acp.PlanUpdateContent
		literal string
	}{
		{"items", acp.ItemsPlanUpdateContent("plan-1", []acp.PlanEntry{entry}), `{"sessionId":"session-1","update":{"sessionUpdate":"plan_update","plan":{"type":"items","planId":"plan-1","entries":[{"content":"Implement the change","priority":"high","status":"in_progress"}]}}}`},
		{"file", acp.FilePlanUpdateContent("plan-1", "file:///workspace/PLAN.md"), `{"sessionId":"session-1","update":{"sessionUpdate":"plan_update","plan":{"type":"file","planId":"plan-1","uri":"file:///workspace/PLAN.md"}}}`},
		{"markdown", acp.MarkdownPlanUpdateContent("plan-1", "# Plan"), `{"sessionId":"session-1","update":{"sessionUpdate":"plan_update","plan":{"type":"markdown","planId":"plan-1","content":"# Plan"}}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			n := sessionUpdate(t, acp.PlanUpdateSessionUpdate(tt.content), tt.literal)
			if n.Update.SessionUpdate != acp.SessionUpdateTypePlanUpdate || n.Update.Plan.PlanID != "plan-1" || string(n.Update.Plan.Type) != tt.name {
				t.Fatalf("plan update = %#v", n.Update)
			}
		})
	}
	n := sessionUpdate(t, acp.PlanRemovedSessionUpdate("plan-1"), `{"sessionId":"session-1","update":{"sessionUpdate":"plan_removed","planId":"plan-1"}}`)
	if n.Update.SessionUpdate != acp.SessionUpdateTypePlanRemoved || n.Update.PlanID != "plan-1" {
		t.Fatalf("plan removal = %#v", n.Update)
	}
}

// schema_subagents.rs::v1_subagent_update_uses_existing_session_notification_route:
// parent and child session IDs remain distinct in the existing notification route.
func TestSubagentNotification(t *testing.T) {
	n := acp.SessionNotification{SessionID: "parent", Update: acp.SubagentUpdateSessionUpdate("child")}
	const literal = `{"sessionId":"parent","update":{"sessionUpdate":"subagent_update","sessionId":"child"}}`
	assertJSON(t, encode(t, n), literal)
	var parsed acp.SessionNotification
	roundTrip(t, literal, &parsed)
	if parsed.SessionID != "parent" || parsed.Update.SessionID != "child" || parsed.Update.SessionUpdate != acp.SessionUpdateTypeSubagentUpdate {
		t.Fatalf("subagent = %#v", parsed)
	}
	ctx, conn, peer := rawAgent(t, nil)
	notification(t, ctx, peer, "session/update", literal, func() error { return conn.SessionUpdate(ctx, &n) })
	incomingNotification(t, false, "session/update", literal)
}

// schema_tool_call_name.rs::v1_tool_call_name_serializes_and_updates:
// initial name serialization. Rust's subsequent ToolCall::update operation has
// no Go counterpart; that portion is explicitly accounted for in mapping.json.
func TestToolCallName(t *testing.T) {
	call := acp.ToolCall{ToolCallID: "call_1", Title: "Read configuration", Name: ptr("read_file")}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encode(t, call), &fields); err != nil {
		t.Fatal(err)
	}
	assertJSON(t, fields["name"], `"read_file"`)
}

// schema_tool_call_name.rs::v1_tool_call_names_survive_session_notifications:
// initial and update variants both preserve the name through typed notification encoding/decoding.
func TestToolCallNamesInNotifications(t *testing.T) {
	for _, kind := range []string{"tool_call", "tool_call_update"} {
		t.Run(kind, func(t *testing.T) {
			update := acp.ToolCallSessionUpdate("call_1", "Read configuration")
			if kind == "tool_call_update" {
				update = acp.ToolCallUpdateSessionUpdate("call_1")
				update.Title = ptr("Read configuration")
			}
			update.Name = ptr("read_file")
			n := sessionUpdate(t, update, `{"sessionId":"session-1","update":{"sessionUpdate":"`+kind+`","toolCallId":"call_1","title":"Read configuration","name":"read_file"}}`)
			if n.Update.Name == nil || *n.Update.Name != "read_file" {
				t.Fatalf("decoded name = %v", n.Update.Name)
			}
		})
	}
}
