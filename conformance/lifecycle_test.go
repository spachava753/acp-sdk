// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package conformance_test

import (
	"context"
	"sync"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

func TestConformanceLifecycleAndOptionalAgentMethods(t *testing.T) {
	agent := &lifecycleAgent{calls: map[string]any{}}
	client, done := connectConformanceClient(t, newConformanceClient(), func(conn *acp.AgentConnection) any {
		agent.conn = conn
		return agent
	})
	defer closeConformanceClient(t, client, done)

	ctx := t.Context()
	init, err := client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersion(1)})
	if err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != acp.ProtocolVersion(1) || len(init.AuthMethods) != 0 {
		t.Fatalf("initialize response = %#v", init)
	}
	if init.AgentCapabilities == nil || !init.AgentCapabilities.LoadSession {
		t.Fatalf("agent capabilities = %#v, want loadSession", init.AgentCapabilities)
	}

	session, err := client.NewSession(ctx, &acp.NewSessionRequest{Cwd: "/workspace", McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "sess-123" {
		t.Fatalf("session ID = %q, want sess-123", session.SessionID)
	}
	if _, err := client.LoadSession(ctx, &acp.LoadSessionRequest{SessionID: session.SessionID, Cwd: "/workspace", McpServers: []acp.McpServer{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Authenticate(ctx, &acp.AuthenticateRequest{MethodID: "password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListSessions(ctx, &acp.ListSessionsRequest{Cwd: stringPtr("/workspace")}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ResumeSession(ctx, &acp.ResumeSessionRequest{SessionID: session.SessionID, Cwd: "/workspace"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetSessionMode(ctx, &acp.SetSessionModeRequest{SessionID: session.SessionID, ModeID: "code"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetSessionConfigOption(ctx, &acp.SetSessionConfigOptionRequest{SessionID: session.SessionID, ConfigID: "model", Value: "gpt-4o-mini"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CloseSession(ctx, &acp.CloseSessionRequest{SessionID: session.SessionID}); err != nil {
		t.Fatal(err)
	}

	assertRecordedCall[acp.NewSessionRequest](t, agent, acp.MethodSessionNew)
	assertRecordedCall[acp.LoadSessionRequest](t, agent, acp.MethodSessionLoad)
	assertRecordedCall[acp.AuthenticateRequest](t, agent, acp.MethodAuthenticate)
	assertRecordedCall[acp.ListSessionsRequest](t, agent, acp.MethodSessionList)
	assertRecordedCall[acp.ResumeSessionRequest](t, agent, acp.MethodSessionResume)
	assertRecordedCall[acp.SetSessionModeRequest](t, agent, acp.MethodSessionSetMode)
	assertRecordedCall[acp.SetSessionConfigOptionRequest](t, agent, acp.MethodSessionSetConfigOption)
	assertRecordedCall[acp.CloseSessionRequest](t, agent, acp.MethodSessionClose)
}

type lifecycleAgent struct {
	noopSessionHandler
	conn  *acp.AgentConnection
	mu    sync.Mutex
	calls map[string]any
}

func (a *lifecycleAgent) record(method string, params any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls[method] = params
}

func (a *lifecycleAgent) Initialize(_ context.Context, params *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	a.record(acp.MethodInitialize, *params)
	return &acp.InitializeResponse{ProtocolVersion: params.ProtocolVersion, AgentCapabilities: &acp.AgentCapabilities{LoadSession: true}, AuthMethods: []acp.AuthMethod{}}, nil
}

func (a *lifecycleAgent) NewSession(_ context.Context, params *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	a.record(acp.MethodSessionNew, *params)
	return &acp.NewSessionResponse{SessionID: acp.SessionId("sess-123")}, nil
}

func (a *lifecycleAgent) Prompt(context.Context, *acp.PromptRequest) (*acp.PromptResponse, error) {
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *lifecycleAgent) Cancel(_ context.Context, params *acp.CancelNotification) error {
	a.record(acp.MethodSessionCancel, *params)
	return nil
}

func (a *lifecycleAgent) Authenticate(_ context.Context, params *acp.AuthenticateRequest) (*acp.AuthenticateResponse, error) {
	a.record(acp.MethodAuthenticate, *params)
	return &acp.AuthenticateResponse{}, nil
}

func (a *lifecycleAgent) LoadSession(_ context.Context, params *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
	a.record(acp.MethodSessionLoad, *params)
	return &acp.LoadSessionResponse{}, nil
}

func (a *lifecycleAgent) ResumeSession(_ context.Context, params *acp.ResumeSessionRequest) (*acp.ResumeSessionResponse, error) {
	a.record(acp.MethodSessionResume, *params)
	return &acp.ResumeSessionResponse{}, nil
}

func (a *lifecycleAgent) ListSessions(_ context.Context, params *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
	a.record(acp.MethodSessionList, *params)
	return &acp.ListSessionsResponse{Sessions: []acp.SessionInfo{{SessionID: acp.SessionId("sess-123"), Cwd: "/workspace"}}}, nil
}

func (a *lifecycleAgent) CloseSession(_ context.Context, params *acp.CloseSessionRequest) (*acp.CloseSessionResponse, error) {
	a.record(acp.MethodSessionClose, *params)
	return &acp.CloseSessionResponse{}, nil
}

func (a *lifecycleAgent) SetSessionMode(_ context.Context, params *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
	a.record(acp.MethodSessionSetMode, *params)
	return &acp.SetSessionModeResponse{}, nil
}

func (a *lifecycleAgent) SetSessionConfigOption(_ context.Context, params *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	a.record(acp.MethodSessionSetConfigOption, *params)
	return &acp.SetSessionConfigOptionResponse{ConfigOptions: []acp.SessionConfigOption{}}, nil
}

func assertRecordedCall[T any](t *testing.T, agent *lifecycleAgent, method string) T {
	t.Helper()
	agent.mu.Lock()
	defer agent.mu.Unlock()
	value, ok := agent.calls[method]
	if !ok {
		t.Fatalf("%s was not called", method)
	}
	params, ok := value.(T)
	if !ok {
		t.Fatalf("%s params type = %T", method, value)
	}
	return params
}
