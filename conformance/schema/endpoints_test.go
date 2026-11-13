// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// side names the role the SDK plays in a connection.
type side int

const (
	agentSide side = iota
	clientSide
)

func (s side) String() string {
	if s == agentSide {
		return "agent"
	}
	return "client"
}

// other returns the role that receives what s sends.
func (s side) other() side {
	if s == agentSide {
		return clientSide
	}
	return agentSide
}

func startClient(t *testing.T) (*acp.Client, *acptest.Peer) { return acptest.StartClient(t, nil) }

func startAgent(t *testing.T) (*acp.AgentConnection, *acptest.Peer) {
	agent, peer := acptest.StartAgent(t, nil)
	return agent.Conn, peer
}

// serve runs handler as the SDK side s. The returned settle func stops the
// endpoint and returns once every handler it accepted has finished, so a test
// can then check that a handler was never invoked.
func serve(t *testing.T, s side, handler any) (*acptest.Peer, func()) {
	t.Helper()
	if s == agentSide {
		agent, peer := acptest.StartAgent(t, handler)
		return peer, func() {
			t.Helper()
			peer.CloseInput()
			if err := agent.Wait(t); err != nil {
				t.Errorf("RunAgent: %v", err)
			}
		}
	}
	client, peer := acptest.StartClient(t, handler)
	return peer, func() {
		t.Helper()
		if err := client.Close(); err != nil {
			t.Errorf("closing client: %v", err)
		}
	}
}

// expectCall runs call, checks that it sends a method request with params, and
// answers it with reply. It returns what call returned.
func expectCall[R any](t *testing.T, peer *acptest.Peer, method, params, reply string, call func() (R, error)) R {
	t.Helper()
	result := acptest.Go(call)
	m := peer.Expect(method)
	if m.IsNotification() {
		t.Fatalf("%s was sent as a notification: %s", method, m)
	}
	acptest.EqualJSON(t, m.Params, params)
	peer.Reply(m.ID, json.RawMessage(reply))
	return acptest.Value(t, result)
}

// expectNotification runs send and checks that it sends a method notification
// with params.
func expectNotification(t *testing.T, peer *acptest.Peer, method, params string, send func() error) {
	t.Helper()
	result := acptest.Go(func() (struct{}, error) { return struct{}{}, send() })
	m := peer.Expect(method)
	if !m.IsNotification() {
		t.Fatalf("%s was sent with an id: %s", method, m)
	}
	acptest.EqualJSON(t, m.Params, params)
	acptest.Value(t, result)
}

// call sends a request to the SDK and returns its response.
func call(t *testing.T, peer *acptest.Peer, method, params string) acptest.Message {
	t.Helper()
	m := peer.Call("schema", method, json.RawMessage(params))
	if !m.IsResponse() {
		t.Fatalf("SDK sent %s, want the response to %s", m, method)
	}
	acptest.EqualJSON(t, m.ID, `"schema"`)
	return m
}

// expectError sends a request to the SDK and checks that it fails with code.
func expectError(t *testing.T, peer *acptest.Peer, method, params string, code int64) {
	t.Helper()
	m := call(t, peer, method, params)
	if m.Error == nil || m.Error.Code != code {
		t.Errorf("%s %s answered %s, want error code %d", method, params, m, code)
	}
}

// handled is one handler invocation seen by a recorder.
type handled struct {
	method string
	kind   string // "request" or "notification"
	params any
}

// recorder implements typed handlers on both sides and records which one ran.
// Each request handler returns a fixed result so the peer can finish its call.
type recorder struct{ calls chan handled }

func newRecorder() *recorder {
	// Spare capacity lets a wrongly routed second invocation finish, so the
	// test reports it instead of blocking.
	return &recorder{calls: make(chan handled, 32)}
}

func (r *recorder) record(method, kind string, params any) error {
	select {
	case r.calls <- handled{method, kind, params}:
		return nil
	default:
		return fmt.Errorf("too many handler calls; last was %s %s", kind, method)
	}
}

// expectNone fails if any handler ran. Settle the endpoint first.
func (r *recorder) expectNone(t *testing.T) {
	t.Helper()
	select {
	case got := <-r.calls:
		t.Errorf("unexpected call to the %s %s handler", got.method, got.kind)
	default:
	}
}

func (r *recorder) AuthStatus(_ context.Context, p *acp.AuthStatusRequest) (*acp.AuthStatusResponse, error) {
	return &acp.AuthStatusResponse{State: "not_required"}, r.record("auth/status", "request", p)
}
func (r *recorder) SessionNew(_ context.Context, p *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: "session-1"}, r.record("session/new", "request", p)
}
func (r *recorder) SessionArchive(_ context.Context, p *acp.ArchiveSessionRequest) (*acp.ArchiveSessionResponse, error) {
	return &acp.ArchiveSessionResponse{}, r.record("session/archive", "request", p)
}
func (r *recorder) SessionUnarchive(_ context.Context, p *acp.UnarchiveSessionRequest) (*acp.UnarchiveSessionResponse, error) {
	return &acp.UnarchiveSessionResponse{}, r.record("session/unarchive", "request", p)
}
func (r *recorder) SessionStatus(_ context.Context, p *acp.SessionStatusRequest) (*acp.SessionStatusResponse, error) {
	return &acp.SessionStatusResponse{Status: "open"}, r.record("session/status", "request", p)
}
func (r *recorder) SessionSetTitle(_ context.Context, p *acp.SetSessionTitleRequest) (*acp.SetSessionTitleResponse, error) {
	return &acp.SetSessionTitleResponse{Title: "Refactor"}, r.record("session/set_title", "request", p)
}
func (r *recorder) SessionDelete(_ context.Context, p *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error) {
	return &acp.DeleteSessionResponse{}, r.record("session/delete", "request", p)
}
func (r *recorder) SessionPrompt(_ context.Context, p *acp.PromptRequest) (*acp.PromptResponse, error) {
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, r.record("session/prompt", "request", p)
}
func (r *recorder) ProxyInitialize(_ context.Context, p *acp.InitializeProxyRequest) (*acp.InitializeProxyResponse, error) {
	return &acp.InitializeProxyResponse{ProtocolVersion: 1}, r.record("proxy/initialize", "request", p)
}
func (r *recorder) ProxySuccessor(_ context.Context, p *acp.ProxySuccessorRequest) (*acp.ProxySuccessorResponse, error) {
	var result acp.ProxySuccessorResponse = json.RawMessage(`{"zeta":1,"alpha":[null,1.5],"_meta":{"k":"v"}}`)
	return &result, r.record("proxy/successor", "request", p)
}
func (r *recorder) ElicitationCreate(_ context.Context, p *acp.CreateElicitationRequest) (*acp.CreateElicitationResponse, error) {
	response := acp.DeclineCreateElicitationResponse()
	return &response, r.record("elicitation/create", "request", p)
}
func (r *recorder) McpMessage(_ context.Context, p *acp.MessageMcpRequest) (*acp.MessageMcpResponse, error) {
	response := acp.ResultMessageMcpResponse(nil)
	return &response, r.record("mcp/message", "request", p)
}
func (r *recorder) ProvidersList(_ context.Context, p *acp.ListProvidersRequest) (*acp.ListProvidersResponse, error) {
	return &acp.ListProvidersResponse{Providers: []acp.ProviderInfo{}}, r.record("providers/list", "request", p)
}
func (r *recorder) ProvidersDisable(_ context.Context, p *acp.DisableProviderRequest) (*acp.DisableProviderResponse, error) {
	return &acp.DisableProviderResponse{}, r.record("providers/disable", "request", p)
}
func (r *recorder) AuthStatusUpdate(_ context.Context, p *acp.AuthStatusUpdateNotification) error {
	return r.record("auth/status_update", "notification", p)
}
func (r *recorder) SessionUpdate(_ context.Context, p *acp.SessionNotification) error {
	return r.record("session/update", "notification", p)
}
func (r *recorder) ElicitationComplete(_ context.Context, p *acp.CompleteElicitationNotification) error {
	return r.record("elicitation/complete", "notification", p)
}
func (r *recorder) ProxySuccessorNotification(_ context.Context, p *acp.ProxySuccessorNotification) error {
	return r.record("proxy/successor", "notification", p)
}
func (r *recorder) McpMessageNotification(_ context.Context, p *acp.MessageMcpNotification) error {
	return r.record("mcp/message", "notification", p)
}
func (r *recorder) SessionCancel(_ context.Context, p *acp.CancelNotification) error {
	return r.record("session/cancel", "notification", p)
}

// Dispatch finds handlers by interface, so a method with a mistyped signature
// would silently go unused. Pin every test handler to its interface.
var (
	_ acp.AuthStatusHandler                 = (*recorder)(nil)
	_ acp.SessionNewHandler                 = (*recorder)(nil)
	_ acp.SessionArchiveHandler             = (*recorder)(nil)
	_ acp.SessionUnarchiveHandler           = (*recorder)(nil)
	_ acp.SessionStatusHandler              = (*recorder)(nil)
	_ acp.SessionSetTitleHandler            = (*recorder)(nil)
	_ acp.SessionDeleteHandler              = (*recorder)(nil)
	_ acp.SessionPromptHandler              = (*recorder)(nil)
	_ acp.ProxyInitializeHandler            = (*recorder)(nil)
	_ acp.ProxySuccessorHandler             = (*recorder)(nil)
	_ acp.ElicitationCreateHandler          = (*recorder)(nil)
	_ acp.McpMessageHandler                 = (*recorder)(nil)
	_ acp.ProvidersListHandler              = (*recorder)(nil)
	_ acp.ProvidersDisableHandler           = (*recorder)(nil)
	_ acp.AuthStatusUpdateHandler           = (*recorder)(nil)
	_ acp.SessionUpdateHandler              = (*recorder)(nil)
	_ acp.ElicitationCompleteHandler        = (*recorder)(nil)
	_ acp.ProxySuccessorNotificationHandler = (*recorder)(nil)
	_ acp.McpMessageNotificationHandler     = (*recorder)(nil)
	_ acp.SessionCancelHandler              = (*recorder)(nil)

	_ acp.McpMessageHandler             = mcpRequestOnly{}
	_ acp.McpMessageNotificationHandler = mcpNotificationOnly{}

	_ acp.InitializeHandler       = (*providerAgent)(nil)
	_ acp.ProvidersListHandler    = (*providerAgent)(nil)
	_ acp.ProvidersSetHandler     = (*providerAgent)(nil)
	_ acp.ProvidersDisableHandler = (*providerAgent)(nil)

	_ acp.InitializeHandler    = sessionAgent{}
	_ acp.SessionNewHandler    = sessionAgent{}
	_ acp.SessionUpdateHandler = (*updateObserver)(nil)
)

// receiveRequest sends a request to an SDK endpoint of side s that serves a
// recorder. It checks that the response result is served, that exactly the
// matching typed handler ran, and that its params encode back to params. It
// returns those typed params.
func receiveRequest(t *testing.T, s side, method, params, served string) any {
	t.Helper()
	rec := newRecorder()
	peer, settle := serve(t, s, rec)
	m := call(t, peer, method, params)
	if m.Error != nil {
		t.Fatalf("%s at the %s failed: %s", method, s, m)
	}
	acptest.EqualJSON(t, m.Result, served)
	return expectHandled(t, rec, settle, method, "request", params)
}

// receiveNotification is receiveRequest for a notification.
func receiveNotification(t *testing.T, s side, method, params string) any {
	t.Helper()
	rec := newRecorder()
	peer, settle := serve(t, s, rec)
	peer.Notify(method, json.RawMessage(params))
	return expectHandled(t, rec, settle, method, "notification", params)
}

func expectHandled(t *testing.T, rec *recorder, settle func(), method, kind, params string) any {
	t.Helper()
	got := acptest.Await(t, rec.calls)
	if got.method != method || got.kind != kind {
		t.Fatalf("%s %s reached the %s %s handler", kind, method, got.method, got.kind)
	}
	acptest.EqualMarshaled(t, got.params, params)
	settle()
	rec.expectNone(t)
	return got.params
}

// requestMethod is a typed request method bound to the side that sends it.
type requestMethod struct {
	from side
	// newResult allocates the method's typed result.
	newResult func() any
	// send decodes params into the typed request, sends it from a new SDK
	// endpoint, answers with reply, and returns the typed request and result.
	send func(t *testing.T, method, params, reply string) (request, result any)
}

func clientRequest[P, R any](m func(*acp.Client, context.Context, *P) (*R, error)) requestMethod {
	return typedRequest(clientSide, startClient, m)
}

func agentRequest[P, R any](m func(*acp.AgentConnection, context.Context, *P) (*R, error)) requestMethod {
	return typedRequest(agentSide, startAgent, m)
}

func typedRequest[C, P, R any](from side, start func(*testing.T) (C, *acptest.Peer), m func(C, context.Context, *P) (*R, error)) requestMethod {
	return requestMethod{
		from:      from,
		newResult: alloc[R],
		send: func(t *testing.T, method, params, reply string) (any, any) {
			t.Helper()
			conn, peer := start(t)
			request := new(P)
			acptest.RoundTrip(t, params, request)
			result := expectCall(t, peer, method, params, reply, func() (*R, error) { return m(conn, t.Context(), request) })
			return request, result
		},
	}
}

// notificationMethod is a typed notification method bound to the side that
// sends it.
type notificationMethod struct {
	from side
	// send decodes params into the typed notification and sends it from a new
	// SDK endpoint.
	send func(t *testing.T, method, params string)
}

func clientNotification[P any](m func(*acp.Client, context.Context, *P) error) notificationMethod {
	return typedNotification(clientSide, startClient, m)
}

func agentNotification[P any](m func(*acp.AgentConnection, context.Context, *P) error) notificationMethod {
	return typedNotification(agentSide, startAgent, m)
}

func typedNotification[C, P any](from side, start func(*testing.T) (C, *acptest.Peer), m func(C, context.Context, *P) error) notificationMethod {
	return notificationMethod{
		from: from,
		send: func(t *testing.T, method, params string) {
			t.Helper()
			conn, peer := start(t)
			notification := new(P)
			acptest.RoundTrip(t, params, notification)
			expectNotification(t, peer, method, params, func() error { return m(conn, t.Context(), notification) })
		},
	}
}

// connectPair connects an SDK client to an SDK agent serving handler.
func connectPair(t *testing.T, handler any) *acp.Client {
	t.Helper()
	clientTransport, agentTransport := acp.NewInMemoryTransports()
	served := acptest.Go(func() (struct{}, error) {
		return struct{}{}, acp.RunAgent(context.Background(), agentTransport, func(*acp.AgentConnection) any { return handler })
	})
	client, err := acp.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Closing the client ends the agent's input, so RunAgent returns.
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("closing client: %v", err)
		}
		if r := acptest.Await(t, served); r.Err != nil {
			t.Errorf("RunAgent: %v", r.Err)
		}
	})
	return client
}
