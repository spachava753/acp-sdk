// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// Session info and compaction updates are patches: an omitted field is left
// unchanged, null clears it, and a value replaces it. Both omitted and null
// are valid, so decoding must keep them apart for a client to apply the patch.

// TestPatchRoundTrips decodes each patch form, standalone and inside the
// SessionUpdate union, and checks that it encodes back unchanged.
func TestPatchRoundTrips(t *testing.T) {
	type patch struct{ name, members string }
	sessionInfo := []patch{
		{"omitted", ``},
		{"null", `"title":null,"updatedAt":null`},
		{"value", `"title":"New title","updatedAt":"2026-11-04T00:00:00Z"`},
	}
	compaction := []patch{
		{"omitted", ``},
		{"null", `"summary":null,"error":null,"_meta":null`},
		{"value", `"summary":[{"type":"text","text":"Retained context"}],"error":"Earlier failure","_meta":{"trace":"kept"}`},
		{"empty_summary", `"summary":[]`},
	}
	for _, carrier := range []struct {
		name    string
		new     func() any
		fixed   string // members every patch of this carrier has
		patches []patch
	}{
		{"session_info", alloc[acp.SessionInfoUpdate], ``, sessionInfo},
		{"session_info_union", alloc[acp.SessionUpdate], `"sessionUpdate":"session_info_update"`, sessionInfo},
		{"compaction", alloc[acp.CompactionUpdate], `"compactionId":"compact-1","status":"completed"`, compaction},
		{"compaction_union", alloc[acp.SessionUpdate], `"sessionUpdate":"compaction_update","compactionId":"compact-1","status":"completed"`, compaction},
	} {
		t.Run(carrier.name, func(t *testing.T) {
			var cases []decodeCase
			for _, p := range carrier.patches {
				members := slices.DeleteFunc([]string{carrier.fixed, p.members}, func(m string) bool { return m == "" })
				literal := "{" + strings.Join(members, ",") + "}"
				cases = append(cases, decodeCase{name: p.name, new: carrier.new, input: literal})
			}
			runDecodeCases(t, cases)
		})
	}
}

// TestPatchNullMarkers checks that NullFields on a constructed update sends
// explicit nulls, so a sender can clear fields.
func TestPatchNullMarkers(t *testing.T) {
	info := acp.SessionInfoUpdateSessionUpdate()
	info.NullFields = []string{"title", "updatedAt"}
	compaction := acp.CompactionUpdateSessionUpdate("compact-1", acp.CompactionStatusCompleted)
	compaction.NullFields = []string{"summary", "error", "_meta"}
	for _, tt := range []struct {
		name   string
		update acp.SessionUpdate
		want   string
	}{
		{"session_info", info, `{"sessionUpdate":"session_info_update","title":null,"updatedAt":null}`},
		{"compaction", compaction, `{"sessionUpdate":"compaction_update","compactionId":"compact-1","status":"completed","summary":null,"error":null,"_meta":null}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			acptest.EqualMarshaled(t, tt.update, tt.want)
		})
	}
}

const (
	omittedInfoPatch = `{"sessionId":"session-1","update":{"sessionUpdate":"session_info_update"}}`
	clearedInfoPatch = `{"sessionId":"session-1","update":{"sessionUpdate":"session_info_update","title":null,"updatedAt":null}}`
)

// sessionAgent completes initialization and creates session-1.
type sessionAgent struct{}

func (sessionAgent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: 1}, nil
}

func (sessionAgent) SessionNew(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: "session-1"}, nil
}

type updateObserver struct{ updates chan *acp.SessionNotification }

func (o *updateObserver) SessionUpdate(_ context.Context, n *acp.SessionNotification) error {
	o.updates <- n
	return nil
}

// TestSessionInfoPatchDelivery checks that a cleared session-info patch stays
// distinct from an omitted one through a live session in both directions.
// Each phase sets up a session first and waits for each update on its own, so
// it does not depend on notification ordering.
func TestSessionInfoPatchDelivery(t *testing.T) {
	patches := []struct {
		name    string
		literal string
		nulls   []string
	}{
		{"omitted", omittedInfoPatch, nil},
		{"cleared", clearedInfoPatch, []string{"title", "updatedAt"}},
	}
	t.Run("receive", func(t *testing.T) {
		observer := &updateObserver{updates: make(chan *acp.SessionNotification, len(patches))}
		client, peer := acptest.StartClient(t, observer)
		if init := acptest.InitializeClient(t, client, peer, nil, nil); init.ProtocolVersion != 1 {
			t.Fatalf("initialized protocol version %d, want 1", init.ProtocolVersion)
		}
		acptest.NewSession(t, client, peer, "session-1")
		for _, p := range patches {
			peer.Notify("session/update", json.RawMessage(p.literal))
			got := acptest.Await(t, observer.updates)
			// The handler must see clear markers for nulls and none for
			// omitted fields, and encoding must keep the same meaning.
			for _, field := range []string{"title", "updatedAt"} {
				if marked, want := slices.Contains(got.Update.NullFields, field), slices.Contains(p.nulls, field); marked != want {
					t.Errorf("%s: null marker for %s = %v, want %v", p.name, field, marked, want)
				}
			}
			acptest.EqualMarshaled(t, got, p.literal)
		}
	})
	t.Run("send", func(t *testing.T) {
		agent, peer := acptest.StartAgent(t, sessionAgent{})
		acptest.EqualJSON(t, peer.InitializeAgent(nil).Result, `{"protocolVersion":1}`)
		created := call(t, peer, "session/new", `{"cwd":"/workspace","mcpServers":[]}`)
		if created.Error != nil {
			t.Fatalf("session/new failed: %s", created)
		}
		acptest.EqualJSON(t, created.Result, `{"sessionId":"session-1"}`)
		for _, p := range patches {
			update := acp.SessionInfoUpdateSessionUpdate()
			update.NullFields = p.nulls
			expectNotification(t, peer, "session/update", p.literal, func() error {
				return agent.Conn.SessionUpdate(t.Context(), &acp.SessionNotification{SessionID: "session-1", Update: update})
			})
		}
	})
}
