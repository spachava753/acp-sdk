// SPDX-License-Identifier: Apache-2.0
// Public receiving counterparts for the Rust aggregate parse_message assertions
// identified in mapping.json (rust-sdk@050cd94c081056ace37a7b89abfed7f6220b47eb).
package schema_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// These observations identify which native Go handler actually ran. The fixture
// does not parse, validate or dispatch messages: only the SDK does that work.
// Fixed responses merely let the independent peer finish its request.
type receivedRoute struct {
	method string
	kind   string
	params any
}

type schemaReceiver struct{ received chan receivedRoute }

func (h *schemaReceiver) record(method, kind string, params any) error {
	select {
	case h.received <- receivedRoute{method, kind, params}:
		return nil
	default:
		return fmt.Errorf("unexpected extra invocation of %s %s handler", method, kind)
	}
}

func (h *schemaReceiver) AuthStatus(_ context.Context, p *acp.AuthStatusRequest) (*acp.AuthStatusResponse, error) {
	return &acp.AuthStatusResponse{State: "not_required"}, h.record("auth/status", "request", p)
}
func (h *schemaReceiver) SessionArchive(_ context.Context, p *acp.ArchiveSessionRequest) (*acp.ArchiveSessionResponse, error) {
	return &acp.ArchiveSessionResponse{}, h.record("session/archive", "request", p)
}
func (h *schemaReceiver) SessionUnarchive(_ context.Context, p *acp.UnarchiveSessionRequest) (*acp.UnarchiveSessionResponse, error) {
	return &acp.UnarchiveSessionResponse{}, h.record("session/unarchive", "request", p)
}
func (h *schemaReceiver) SessionStatus(_ context.Context, p *acp.SessionStatusRequest) (*acp.SessionStatusResponse, error) {
	return &acp.SessionStatusResponse{Status: "open"}, h.record("session/status", "request", p)
}
func (h *schemaReceiver) SessionSetTitle(_ context.Context, p *acp.SetSessionTitleRequest) (*acp.SetSessionTitleResponse, error) {
	return &acp.SetSessionTitleResponse{Title: "Refactor"}, h.record("session/set_title", "request", p)
}
func (h *schemaReceiver) SessionDelete(_ context.Context, p *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error) {
	return &acp.DeleteSessionResponse{}, h.record("session/delete", "request", p)
}
func (h *schemaReceiver) SessionPrompt(_ context.Context, p *acp.PromptRequest) (*acp.PromptResponse, error) {
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, h.record("session/prompt", "request", p)
}
func (h *schemaReceiver) ProxyInitialize(_ context.Context, p *acp.InitializeProxyRequest) (*acp.InitializeProxyResponse, error) {
	return &acp.InitializeProxyResponse{ProtocolVersion: 1}, h.record("proxy/initialize", "request", p)
}
func (h *schemaReceiver) ProxySuccessor(_ context.Context, p *acp.ProxySuccessorRequest) (*acp.ProxySuccessorResponse, error) {
	var result acp.ProxySuccessorResponse = json.RawMessage(`{"zeta":1,"alpha":[null,1.5],"_meta":{"k":"v"}}`)
	return &result, h.record("proxy/successor", "request", p)
}
func (h *schemaReceiver) ElicitationCreate(_ context.Context, p *acp.CreateElicitationRequest) (*acp.CreateElicitationResponse, error) {
	response := acp.DeclineCreateElicitationResponse()
	return &response, h.record("elicitation/create", "request", p)
}
func (h *schemaReceiver) McpMessage(_ context.Context, p *acp.MessageMcpRequest) (*acp.MessageMcpResponse, error) {
	response := acp.ResultMessageMcpResponse(nil)
	return &response, h.record("mcp/message", "request", p)
}
func (h *schemaReceiver) ProvidersList(_ context.Context, p *acp.ListProvidersRequest) (*acp.ListProvidersResponse, error) {
	return &acp.ListProvidersResponse{Providers: []acp.ProviderInfo{}}, h.record("providers/list", "request", p)
}
func (h *schemaReceiver) AuthStatusUpdate(_ context.Context, p *acp.AuthStatusUpdateNotification) error {
	return h.record("auth/status_update", "notification", p)
}
func (h *schemaReceiver) SessionUpdate(_ context.Context, p *acp.SessionNotification) error {
	return h.record("session/update", "notification", p)
}
func (h *schemaReceiver) ElicitationComplete(_ context.Context, p *acp.CompleteElicitationNotification) error {
	return h.record("elicitation/complete", "notification", p)
}
func (h *schemaReceiver) ProxySuccessorNotification(_ context.Context, p *acp.ProxySuccessorNotification) error {
	return h.record("proxy/successor", "notification", p)
}
func (h *schemaReceiver) McpMessageNotification(_ context.Context, p *acp.MessageMcpNotification) error {
	return h.record("mcp/message", "notification", p)
}
func (h *schemaReceiver) SessionCancel(_ context.Context, p *acp.CancelNotification) error {
	return h.record("session/cancel", "notification", p)
}

var (
	_ acp.AuthStatusHandler                 = (*schemaReceiver)(nil)
	_ acp.SessionArchiveHandler             = (*schemaReceiver)(nil)
	_ acp.SessionUnarchiveHandler           = (*schemaReceiver)(nil)
	_ acp.SessionStatusHandler              = (*schemaReceiver)(nil)
	_ acp.SessionSetTitleHandler            = (*schemaReceiver)(nil)
	_ acp.SessionDeleteHandler              = (*schemaReceiver)(nil)
	_ acp.SessionPromptHandler              = (*schemaReceiver)(nil)
	_ acp.ProxyInitializeHandler            = (*schemaReceiver)(nil)
	_ acp.ProxySuccessorHandler             = (*schemaReceiver)(nil)
	_ acp.ElicitationCreateHandler          = (*schemaReceiver)(nil)
	_ acp.McpMessageHandler                 = (*schemaReceiver)(nil)
	_ acp.ProvidersListHandler              = (*schemaReceiver)(nil)
	_ acp.AuthStatusUpdateHandler           = (*schemaReceiver)(nil)
	_ acp.SessionUpdateHandler              = (*schemaReceiver)(nil)
	_ acp.ElicitationCompleteHandler        = (*schemaReceiver)(nil)
	_ acp.ProxySuccessorNotificationHandler = (*schemaReceiver)(nil)
	_ acp.McpMessageNotificationHandler     = (*schemaReceiver)(nil)
	_ acp.SessionCancelHandler              = (*schemaReceiver)(nil)
)

// Incoming enum parsing is observed by invoking the real receiving dispatcher,
// not by inspecting an outgoing request and pretending that proves dispatch.
func incoming(t *testing.T, agentSide bool, method, params, result string, isNotification bool) any {
	t.Helper()
	// More than one slot lets accidental dispatch to a second handler finish;
	// after Close drains accepted work, extra records are a failure, not lost.
	h := &schemaReceiver{received: make(chan receivedRoute, 32)}
	ctx, peer, closeSDK := receivingEndpoint(t, agentSide, h)
	kind := "request"
	if isNotification {
		kind = "notification"
		if err := peer.Write(ctx, &jsonrpc.Request{Method: method, Params: json.RawMessage(params)}); err != nil {
			t.Fatal(err)
		}
	} else {
		res := requestToAgent(t, ctx, peer, method, params)
		if res.Error != nil {
			t.Fatalf("incoming %s: %v", method, res.Error)
		}
		if result != "" {
			assertJSON(t, res.Result, result)
		}
	}
	select {
	case event := <-h.received:
		if event.method != method || event.kind != kind {
			t.Fatalf("incoming %s %s invoked %s %s", kind, method, event.kind, event.method)
		}
		assertJSON(t, encode(t, event.params), params)
		drainReceiving(t, closeSDK)
		select {
		case extra := <-h.received:
			t.Errorf("incoming %s also invoked %s %s", method, extra.kind, extra.method)
		default:
		}
		return event.params
	case <-time.After(time.Second):
		t.Fatalf("incoming %s %s did not reach its typed handler", kind, method)
		return nil
	}
}

func receivingEndpoint(t *testing.T, agentSide bool, h any) (context.Context, acp.Connection, func() error) {
	t.Helper()
	if agentSide {
		ctx, conn, peer := rawAgent(t, h)
		return ctx, peer, conn.Close
	}
	ctx, client, peer := rawClient(t, h)
	return ctx, peer, client.Close
}

func drainReceiving(t *testing.T, closeSDK func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- closeSDK() }()
	bounded(t, done, "receiving dispatch drain")
}

// A per-type Rust matches_method negative has no direct Go matcher API. Its
// observable counterpart is that an unhandled alternate known method must not
// select the installed tested handler. Responses plus Close settle dispatch
// before checking absence; native raw closers remain available on failure.
func rejectIncomingRequest(t *testing.T, agentSide bool, method, params string) {
	t.Helper()
	h := &schemaReceiver{received: make(chan receivedRoute, 32)}
	ctx, peer, closeSDK := receivingEndpoint(t, agentSide, h)
	res := requestToAgent(t, ctx, peer, method, params)
	var wireErr *jsonrpc.Error
	if !errors.As(res.Error, &wireErr) || wireErr.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("alternate known method %s response = %s, %v; want MethodNotFound", method, res.Result, res.Error)
	}
	drainReceiving(t, closeSDK)
	select {
	case event := <-h.received:
		t.Errorf("alternate known method %s invoked %s %s", method, event.kind, event.method)
	default:
	}
}

func incomingRequest(t *testing.T, agentSide bool, method, params, result string) any {
	t.Helper()
	return incoming(t, agentSide, method, params, result, false)
}
func incomingNotification(t *testing.T, agentSide bool, method, params string) any {
	t.Helper()
	return incoming(t, agentSide, method, params, "", true)
}
