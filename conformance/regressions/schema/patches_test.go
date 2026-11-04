package schema_test

import (
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// Local regressions for ACP V1 patch meaning, not SDK-workspace Rust ports.
// Schema crate 1.15.1 src/v1/client.rs::test_serialization_behavior supplies the
// SessionInfoUpdate omission/null/value model; CompactionUpdate's description
// and MaybeUndefined fields declare the same distinction for all three patches.
func TestPatchRoundTrips(t *testing.T) {
	for _, carrier := range []struct {
		name   string
		new    func() any
		prefix string
		fields []struct{ name, body string }
	}{
		{
			name: "session_info", new: func() any { return &acp.SessionInfoUpdate{} }, prefix: `{`,
			fields: []struct{ name, body string }{
				{"omitted", ``},
				{"null", `"title":null,"updatedAt":null`},
				{"value", `"title":"New title","updatedAt":"2026-11-04T00:00:00Z"`},
			},
		},
		{
			name: "session_info_union", new: func() any { return &acp.SessionUpdate{} }, prefix: `{"sessionUpdate":"session_info_update"`,
			fields: []struct{ name, body string }{
				{"omitted", ``},
				{"null", `,"title":null,"updatedAt":null`},
				{"value", `,"title":"New title","updatedAt":"2026-11-04T00:00:00Z"`},
			},
		},
		{
			name: "compaction", new: func() any { return &acp.CompactionUpdate{} }, prefix: `{"compactionId":"compact-1","status":"completed"`,
			fields: []struct{ name, body string }{
				{"omitted", ``},
				{"null", `,"summary":null,"error":null,"_meta":null`},
				{"value", `,"summary":[{"type":"text","text":"Retained context"}],"error":"Earlier failure","_meta":{"trace":"kept"}`},
				{"empty_summary", `,"summary":[]`},
			},
		},
		{
			name: "compaction_union", new: func() any { return &acp.SessionUpdate{} }, prefix: `{"sessionUpdate":"compaction_update","compactionId":"compact-1","status":"completed"`,
			fields: []struct{ name, body string }{
				{"omitted", ``},
				{"null", `,"summary":null,"error":null,"_meta":null`},
				{"value", `,"summary":[{"type":"text","text":"Retained context"}],"error":"Earlier failure","_meta":{"trace":"kept"}`},
				{"empty_summary", `,"summary":[]`},
			},
		},
	} {
		t.Run(carrier.name, func(t *testing.T) {
			for _, patch := range carrier.fields {
				t.Run(patch.name, func(t *testing.T) {
					literal := carrier.prefix + patch.body + `}`
					value := carrier.new()
					decode(t, literal, value)
					// Both omitted and explicit-null input are valid. Schema
					// validation alone cannot catch replacing clear with no-op.
					equalJSON(t, marshal(t, value), literal)
				})
			}
		})
	}
}

// The existing public union marker must also express outgoing clears. This
// does not refer to a not-yet-existing standalone SessionInfoUpdate marker.
func TestPatchNullMarkers(t *testing.T) {
	info := acp.SessionInfoUpdateSessionUpdate()
	info.NullFields = []string{"title", "updatedAt"}
	t.Run("session_info", func(t *testing.T) {
		equalJSON(t, marshal(t, info), `{"sessionUpdate":"session_info_update","title":null,"updatedAt":null}`)
	})
	compaction := acp.CompactionUpdateSessionUpdate("compact-1", acp.CompactionStatusCompleted)
	compaction.NullFields = []string{"summary", "error", "_meta"}
	t.Run("compaction", func(t *testing.T) {
		equalJSON(t, marshal(t, compaction), `{"sessionUpdate":"compaction_update","compactionId":"compact-1","status":"completed","summary":null,"error":null,"_meta":null}`)
	})
}
