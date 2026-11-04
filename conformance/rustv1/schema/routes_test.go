// SPDX-License-Identifier: Apache-2.0
// Upstream: agentclientprotocol/rust-sdk@050cd94c081056ace37a7b89abfed7f6220b47eb.
// Rust sources: src/agent-client-protocol/tests/{schema_1_15,schema_session_delete,schema_elicitation}.rs.
package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// schema_1_15.rs::v1_auth_status_round_trips: typed request and aggregate route,
// authenticated/accounts and not_required responses, unauthenticated notification.
func TestAuthStatusRoundTrips(t *testing.T) {
	ctx, client, peer := rawClient(t, nil)
	res := exchange(t, ctx, peer, "auth/status", `{}`, `{"state":"authenticated","accounts":[{"label":"Key","active":true}]}`,
		func() (any, error) { return client.AuthStatus(ctx, &acp.AuthStatusRequest{}) })
	assertJSON(t, encode(t, res), `{"state":"authenticated","accounts":[{"label":"Key","active":true}]}`)
	var response acp.AuthStatusResponse
	roundTrip(t, `{"state":"not_required"}`, &response)
	var request acp.AuthStatusRequest
	roundTrip(t, `{}`, &request)
	var n acp.AuthStatusUpdateNotification
	roundTrip(t, `{"state":"unauthenticated"}`, &n)
	ctx, conn, agentPeer := rawAgent(t, nil)
	notification(t, ctx, agentPeer, "auth/status_update", `{"state":"unauthenticated"}`, func() error { return conn.AuthStatusUpdate(ctx, &n) })
	incomingRequest(t, true, "auth/status", `{}`, `{"state":"not_required"}`)
	incomingNotification(t, false, "auth/status_update", `{"state":"unauthenticated"}`)
}

// schema_1_15.rs::v1_session_archive_round_trips: archive and unarchive requests,
// typed params and empty aggregate responses. These are upstream unstable-v1 features.
func TestSessionArchiveRoundTrips(t *testing.T) {
	for _, method := range []string{"session/archive", "session/unarchive"} {
		t.Run(method, func(t *testing.T) {
			ctx, client, peer := rawClient(t, nil)
			var response any
			if method == "session/archive" {
				var request acp.ArchiveSessionRequest
				roundTrip(t, `{"sessionId":"sess_1"}`, &request)
				response = exchange(t, ctx, peer, method, `{"sessionId":"sess_1"}`, `{}`, func() (any, error) { return client.SessionArchive(ctx, &request) })
			} else {
				var request acp.UnarchiveSessionRequest
				roundTrip(t, `{"sessionId":"sess_1"}`, &request)
				response = exchange(t, ctx, peer, method, `{"sessionId":"sess_1"}`, `{}`, func() (any, error) { return client.SessionUnarchive(ctx, &request) })
			}
			assertJSON(t, encode(t, response), `{}`)
			incomingRequest(t, true, method, `{"sessionId":"sess_1"}`, `{}`)
		})
	}
}

// schema_1_15.rs::v1_session_status_round_trips: saved aggregate result and open typed result.
func TestSessionStatusRoundTrips(t *testing.T) {
	var request acp.SessionStatusRequest
	roundTrip(t, `{"sessionId":"sess_1"}`, &request)
	ctx, client, peer := rawClient(t, nil)
	res := exchange(t, ctx, peer, "session/status", `{"sessionId":"sess_1"}`, `{"status":"saved"}`,
		func() (any, error) { return client.SessionStatus(ctx, &request) })
	assertJSON(t, encode(t, res), `{"status":"saved"}`)
	var response acp.SessionStatusResponse
	roundTrip(t, `{"status":"open"}`, &response)
	incomingRequest(t, true, "session/status", `{"sessionId":"sess_1"}`, `{"status":"open"}`)
}

// schema_1_15.rs::v1_session_set_title_round_trips: Refactor title is retained in request and result.
func TestSessionSetTitleRoundTrips(t *testing.T) {
	var request acp.SetSessionTitleRequest
	roundTrip(t, `{"sessionId":"sess_1","title":"Refactor"}`, &request)
	ctx, client, peer := rawClient(t, nil)
	res := exchange(t, ctx, peer, "session/set_title", `{"sessionId":"sess_1","title":"Refactor"}`, `{"title":"Refactor"}`,
		func() (any, error) { return client.SessionSetTitle(ctx, &request) })
	assertJSON(t, encode(t, res), `{"title":"Refactor"}`)
	incomingRequest(t, true, "session/set_title", `{"sessionId":"sess_1","title":"Refactor"}`, `{"title":"Refactor"}`)
}

// schema_session_delete.rs::{delete_session_request_has_jsonrpc_metadata,
// delete_session_participates_in_client_request_enum,delete_session_response_round_trips_json}:
// concrete request/result association, outgoing method metadata and actual
// receiving SessionDeleteHandler dispatch cover the aggregate parse assertion.
func TestDeleteSessionRoute(t *testing.T) {
	var request acp.DeleteSessionRequest
	roundTrip(t, `{"sessionId":"sess_abc123"}`, &request)
	if request.SessionID != "sess_abc123" {
		t.Fatalf("session ID = %q", request.SessionID)
	}
	ctx, client, peer := rawClient(t, nil)
	res := exchange(t, ctx, peer, "session/delete", `{"sessionId":"sess_abc123"}`, `{}`,
		func() (any, error) { return client.SessionDelete(ctx, &request) })
	assertJSON(t, encode(t, res), `{}`)
	var response acp.DeleteSessionResponse
	roundTrip(t, `{}`, &response)
	incomingRequest(t, true, "session/delete", `{"sessionId":"sess_abc123"}`, `{}`)
	t.Run("not_session_close", func(t *testing.T) {
		rejectIncomingRequest(t, true, "session/close", `{"sessionId":"sess_abc123"}`)
	})
}

func formRequest() acp.CreateElicitationRequest {
	required := []string{"name"}
	r := acp.FormCreateElicitationRequest("Please enter your name", acp.ElicitationSchema{
		Type: acp.ElicitationSchemaTypeObject, Properties: map[string]acp.ElicitationPropertySchema{"name": acp.StringElicitationPropertySchema()}, Required: &required,
	})
	r.SessionID = "sess_abc123"
	return r
}

// schema_elicitation.rs::{create_elicitation_request_has_jsonrpc_metadata,
// elicitation_participates_in_agent_request_enum,create_elicitation_response_round_trips_json}:
// form/session/required string property, concrete/aggregate route and decline/cancel results.
func TestElicitationRequestAndResponses(t *testing.T) {
	request := formRequest()
	const params = `{"mode":"form","sessionId":"sess_abc123","message":"Please enter your name","requestedSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}`
	assertJSON(t, encode(t, request), params)
	var parsed acp.CreateElicitationRequest
	roundTrip(t, params, &parsed)
	if parsed.Mode != acp.CreateElicitationRequestTypeForm || parsed.SessionID != "sess_abc123" {
		t.Fatalf("form request = %#v", parsed)
	}
	for _, action := range []string{"decline", "cancel"} {
		t.Run(action, func(t *testing.T) {
			ctx, conn, peer := rawAgent(t, nil)
			res := exchange(t, ctx, peer, "elicitation/create", params, `{"action":"`+action+`"}`,
				func() (any, error) { return conn.ElicitationCreate(ctx, &request) })
			assertJSON(t, encode(t, res), `{"action":"`+action+`"}`)
		})
	}
	assertJSON(t, encode(t, acp.DeclineCreateElicitationResponse()), `{"action":"decline"}`)
	// Rust AgentRequest::parse_message selects the form request on receipt;
	// an outgoing ElicitationCreate call alone cannot establish that behavior.
	incomingRequest(t, false, "elicitation/create", params, `{"action":"decline"}`)
	t.Run("not_session_prompt", func(t *testing.T) {
		rejectIncomingRequest(t, false, "session/prompt", params)
	})
}

// schema_elicitation.rs::complete_elicitation_notification_has_jsonrpc_metadata:
// completion carries its ID, uses elicitation/complete, and has no outer request ID.
func TestElicitationCompletion(t *testing.T) {
	var n acp.CompleteElicitationNotification
	roundTrip(t, `{"elicitationId":"elicit_1"}`, &n)
	ctx, conn, peer := rawAgent(t, nil)
	notification(t, ctx, peer, "elicitation/complete", `{"elicitationId":"elicit_1"}`, func() error { return conn.ElicitationComplete(ctx, &n) })
	incomingNotification(t, false, "elicitation/complete", `{"elicitationId":"elicit_1"}`)
	t.Run("not_session_update", func(t *testing.T) {
		// Source's negative matcher names session/update. A valid alternate
		// notification must reach SessionUpdate, never ElicitationComplete;
		// incomingNotification drains before checking there were no extra calls.
		incomingNotification(t, false, "session/update", `{"sessionId":"session-1","update":{"sessionUpdate":"compaction_update","compactionId":"compaction-1","status":"in_progress"}}`)
	})
}

// schema_elicitation.rs::client_capabilities_can_declare_elicitation_modes:
// both form/url mode markers are emitted. An empty elicitation object remains
// present, but does not by itself advertise either form or URL support.
func TestElicitationCapabilities(t *testing.T) {
	caps := acp.ClientCapabilities{Elicitation: &acp.ElicitationCapabilities{Form: &acp.ElicitationFormCapabilities{}, Url: &acp.ElicitationUrlCapabilities{}}}
	assertJSON(t, encode(t, caps), `{"elicitation":{"form":{},"url":{}}}`)
	var parsed acp.ClientCapabilities
	if err := json.Unmarshal([]byte(`{"elicitation":{}}`), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Elicitation == nil {
		t.Fatal("explicit empty elicitation marker was lost")
	}
}
