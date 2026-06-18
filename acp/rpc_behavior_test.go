// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/jsonrpc"
)

type optionalAgent struct {
	*testAgent

	mu    sync.Mutex
	calls map[string]any
}

func (a *optionalAgent) record(method string, params any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.calls == nil {
		a.calls = make(map[string]any)
	}
	a.calls[method] = params
}

func (a *optionalAgent) Authenticate(_ context.Context, params *AuthenticateRequest) (*AuthenticateResponse, error) {
	a.record(MethodAuthenticate, *params)
	return &AuthenticateResponse{}, nil
}

func (a *optionalAgent) Logout(_ context.Context, params *LogoutRequest) (*LogoutResponse, error) {
	a.record(MethodLogout, *params)
	return &LogoutResponse{}, nil
}

func (a *optionalAgent) LoadSession(_ context.Context, params *LoadSessionRequest) (*LoadSessionResponse, error) {
	a.record(MethodSessionLoad, *params)
	return &LoadSessionResponse{ConfigOptions: &[]SessionConfigOption{
		SelectSessionConfigOption("model", "Model", "fast", SessionConfigSelectOptions{Ungrouped: ptr(UngroupedSessionConfigSelectOptions{{Value: "fast", Name: "Fast"}})}),
	}}, nil
}

func (a *optionalAgent) ResumeSession(_ context.Context, params *ResumeSessionRequest) (*ResumeSessionResponse, error) {
	a.record(MethodSessionResume, *params)
	return &ResumeSessionResponse{Modes: &SessionModeState{CurrentModeID: "code", AvailableModes: []SessionMode{{ID: "code", Name: "Code"}}}}, nil
}

func (a *optionalAgent) ListSessions(_ context.Context, params *ListSessionsRequest) (*ListSessionsResponse, error) {
	a.record(MethodSessionList, *params)
	return &ListSessionsResponse{Sessions: []SessionInfo{{SessionID: "s1", Cwd: "/repo"}}}, nil
}

func (a *optionalAgent) CloseSession(_ context.Context, params *CloseSessionRequest) (*CloseSessionResponse, error) {
	a.record(MethodSessionClose, *params)
	return &CloseSessionResponse{}, nil
}

func (a *optionalAgent) SetSessionMode(_ context.Context, params *SetSessionModeRequest) (*SetSessionModeResponse, error) {
	a.record(MethodSessionSetMode, *params)
	return &SetSessionModeResponse{}, nil
}

func (a *optionalAgent) SetSessionConfigOption(_ context.Context, params *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error) {
	a.record(MethodSessionSetConfigOption, *params)
	return &SetSessionConfigOptionResponse{ConfigOptions: []SessionConfigOption{
		SelectSessionConfigOption("mode", "Mode", "code", SessionConfigSelectOptions{Ungrouped: ptr(UngroupedSessionConfigSelectOptions{{Value: "code", Name: "Code"}})}),
	}}, nil
}

func TestOptionalAgentMethods(t *testing.T) {
	agent := &optionalAgent{}
	client, done := connectTestClient(t, func(conn *AgentConnection) any {
		agent.testAgent = &testAgent{conn: conn}
		return agent
	})
	defer closeClient(t, client, done)

	ctx := t.Context()
	if _, err := client.Authenticate(ctx, &AuthenticateRequest{MethodID: "agent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Logout(ctx, &LogoutRequest{}); err != nil {
		t.Fatal(err)
	}
	if got, err := client.LoadSession(ctx, &LoadSessionRequest{SessionID: "s1", Cwd: "/repo"}); err != nil {
		t.Fatal(err)
	} else if got.ConfigOptions == nil || len(*got.ConfigOptions) != 1 {
		count := 0
		if got.ConfigOptions != nil {
			count = len(*got.ConfigOptions)
		}
		t.Fatalf("load config options = %d, want 1", count)
	}
	if got, err := client.ResumeSession(ctx, &ResumeSessionRequest{SessionID: "s1", Cwd: "/repo"}); err != nil {
		t.Fatal(err)
	} else if got.Modes == nil || got.Modes.CurrentModeID != "code" {
		t.Fatalf("resume modes = %#v, want code mode", got.Modes)
	}
	if got, err := client.ListSessions(ctx, &ListSessionsRequest{Cwd: ptr("/repo")}); err != nil {
		t.Fatal(err)
	} else if len(got.Sessions) != 1 || got.Sessions[0].SessionID != "s1" {
		t.Fatalf("sessions = %#v, want s1", got.Sessions)
	}
	if _, err := client.CloseSession(ctx, &CloseSessionRequest{SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetSessionMode(ctx, &SetSessionModeRequest{SessionID: "s1", ModeID: "code"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetSessionConfigOption(ctx, &SetSessionConfigOptionRequest{SessionID: "s1", ConfigID: "mode", Value: "code"}); err != nil {
		t.Fatal(err)
	}

	if got := recordedCall[AuthenticateRequest](t, agent, MethodAuthenticate).MethodID; got != "agent" {
		t.Fatalf("authenticate method ID = %q, want agent", got)
	}
	if got := recordedCall[LoadSessionRequest](t, agent, MethodSessionLoad).SessionID; got != "s1" {
		t.Fatalf("load session ID = %q, want s1", got)
	}
	if got := recordedCall[ResumeSessionRequest](t, agent, MethodSessionResume).Cwd; got != "/repo" {
		t.Fatalf("resume cwd = %q, want /repo", got)
	}
	if got := recordedCall[ListSessionsRequest](t, agent, MethodSessionList).Cwd; got == nil || *got != "/repo" {
		t.Fatalf("list cwd = %v, want /repo", got)
	}
	if got := recordedCall[CloseSessionRequest](t, agent, MethodSessionClose).SessionID; got != "s1" {
		t.Fatalf("close session ID = %q, want s1", got)
	}
	if got := recordedCall[SetSessionModeRequest](t, agent, MethodSessionSetMode).ModeID; got != "code" {
		t.Fatalf("mode ID = %q, want code", got)
	}
	configRequest := recordedCall[SetSessionConfigOptionRequest](t, agent, MethodSessionSetConfigOption)
	if configRequest.SessionID != "s1" || configRequest.ConfigID != "mode" || configRequest.Value != "code" {
		t.Fatalf("config option request = %#v, want session s1 config mode value code", configRequest)
	}
}

func recordedCall[T any](t *testing.T, agent *optionalAgent, method string) T {
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

func TestUnsupportedOptionalAgentMethodReturnsMethodNotFound(t *testing.T) {
	client, done := connectTestClient(t, func(conn *AgentConnection) any {
		return &testAgent{conn: conn}
	})
	defer closeClient(t, client, done)

	_, err := client.Authenticate(t.Context(), &AuthenticateRequest{MethodID: "agent"})
	if err == nil {
		t.Fatal("Authenticate succeeded, want method not found")
	}
	var wireErr *jsonrpc.Error
	if !errors.As(err, &wireErr) {
		t.Fatalf("error %v does not wrap jsonrpc error", err)
	}
	if wireErr.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", wireErr.Code)
	}
}

type cancelAgent struct {
	*testAgent
	cancelled chan string
}

func (a *cancelAgent) Cancel(_ context.Context, params *CancelNotification) error {
	a.cancelled <- string(params.SessionID)
	return nil
}

func TestCancelNotificationDispatchesToAgent(t *testing.T) {
	cancelled := make(chan string, 1)
	client, done := connectTestClient(t, func(conn *AgentConnection) any {
		return &cancelAgent{testAgent: &testAgent{conn: conn}, cancelled: cancelled}
	})
	defer closeClient(t, client, done)

	if err := client.Cancel(t.Context(), &CancelNotification{SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-cancelled:
		if got != "s1" {
			t.Fatalf("cancelled session = %q, want s1", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancel notification")
	}
}

type countingAgent struct {
	*testAgent
	prompts atomic.Int64
}

func (a *countingAgent) Prompt(context.Context, *PromptRequest) (*PromptResponse, error) {
	a.prompts.Add(1)
	return &PromptResponse{StopReason: StopReasonEndTurn}, nil
}

func TestMultipleClientRequestsComplete(t *testing.T) {
	agent := &countingAgent{}
	client, done := connectTestClient(t, func(conn *AgentConnection) any {
		agent.testAgent = &testAgent{conn: conn}
		return agent
	})
	defer closeClient(t, client, done)

	ctx := t.Context()
	const n = 8
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, err := client.Prompt(ctx, &PromptRequest{SessionID: "s1", Prompt: []ContentBlock{{Type: ContentBlockTypeText, Text: "hi"}}})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := agent.prompts.Load(); got != n {
		t.Fatalf("prompts = %d, want %d", got, n)
	}
}

func TestClientCallbackTypedNilErrorsDoNotProduceResults(t *testing.T) {
	client, done := connectTestClientWithHandler(t, &failingFileHandler{}, func(conn *AgentConnection) any {
		return &readFileAgent{conn: conn}
	})
	defer closeClient(t, client, done)

	_, err := client.Prompt(t.Context(), &PromptRequest{SessionID: "s1", Prompt: []ContentBlock{{Type: ContentBlockTypeText, Text: "read"}}})
	if err == nil {
		t.Fatal("Prompt succeeded, want callback error")
	}
	if !strings.Contains(err.Error(), errReadTextFileFailed.Error()) {
		t.Fatalf("error = %v, want it to contain %v", err, errReadTextFileFailed)
	}
}

type failingFileHandler struct{}

func (*failingFileHandler) Update(context.Context, *SessionNotification) error { return nil }

func (*failingFileHandler) ReadTextFile(context.Context, *ReadTextFileRequest) (*ReadTextFileResponse, error) {
	return nil, errReadTextFileFailed
}

func (*failingFileHandler) WriteTextFile(context.Context, *WriteTextFileRequest) (*WriteTextFileResponse, error) {
	return nil, errUnexpectedCallbackResult
}

var errReadTextFileFailed = errors.New("read text file failed")

func TestClientRejectsRequestsAfterClose(t *testing.T) {
	client, done := connectTestClient(t, func(conn *AgentConnection) any {
		return &testAgent{conn: conn}
	})
	closeClient(t, client, done)

	_, err := client.NewSession(t.Context(), &NewSessionRequest{Cwd: "/repo"})
	if err == nil {
		t.Fatal("NewSession after close succeeded")
	}
	if !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("error = %v, want ErrConnectionClosed", err)
	}
}

func TestMissingClientCallbackReturnsMethodNotFound(t *testing.T) {
	client, done := connectTestClientWithHandler(t, &sessionOnlyHandler{}, func(conn *AgentConnection) any {
		return &readFileAgent{conn: conn}
	})
	defer closeClient(t, client, done)

	_, err := client.Prompt(t.Context(), &PromptRequest{SessionID: "s1", Prompt: []ContentBlock{{Type: ContentBlockTypeText, Text: "read"}}})
	if err == nil {
		t.Fatal("Prompt succeeded, want method not found from missing client callback")
	}
	var wireErr *jsonrpc.Error
	if !errors.As(err, &wireErr) {
		t.Fatalf("error %v does not wrap jsonrpc error", err)
	}
	if wireErr.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", wireErr.Code)
	}
}

type sessionOnlyHandler struct{}

func (*sessionOnlyHandler) Update(context.Context, *SessionNotification) error { return nil }

type readFileAgent struct {
	noopSessionHandler
	conn *AgentConnection
}

func (a *readFileAgent) Initialize(context.Context, *InitializeRequest) (*InitializeResponse, error) {
	return &InitializeResponse{ProtocolVersion: ProtocolVersion(1)}, nil
}

func (a *readFileAgent) NewSession(context.Context, *NewSessionRequest) (*NewSessionResponse, error) {
	return &NewSessionResponse{SessionID: "s1"}, nil
}

func (a *readFileAgent) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	_, err := a.conn.ReadTextFile(ctx, &ReadTextFileRequest{SessionID: req.SessionID, Path: "/repo/main.go"})
	if err != nil {
		return nil, err
	}
	return &PromptResponse{StopReason: StopReasonEndTurn}, nil
}

func (a *readFileAgent) Cancel(context.Context, *CancelNotification) error { return nil }

func connectTestClient(t *testing.T, newAgent AgentFactory) (*Client, chan error) {
	t.Helper()
	return connectTestClientWithHandler(t, newTestClientHandler(), newAgent)
}

func connectTestClientWithHandler(t *testing.T, handler any, newAgent AgentFactory) (*Client, chan error) {
	t.Helper()
	clientTransport, agentTransport := NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- RunAgent(t.Context(), agentTransport, newAgent) }()
	client, err := Connect(t.Context(), clientTransport, handler)
	if err != nil {
		t.Fatal(err)
	}
	return client, done
}

func closeClient(t *testing.T, client *Client, done chan error) {
	t.Helper()
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
