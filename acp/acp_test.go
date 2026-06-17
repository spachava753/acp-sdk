// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errUnexpectedCallbackResult = errors.New("unexpected callback result")

func ptr[T any](v T) *T { return &v }

type noopSessionHandler struct{}

func (noopSessionHandler) Cancel(context.Context, *CancelNotification) error { return nil }
func (noopSessionHandler) CloseSession(context.Context, *CloseSessionRequest) (*CloseSessionResponse, error) {
	return &CloseSessionResponse{}, nil
}
func (noopSessionHandler) DeleteSession(context.Context, *DeleteSessionRequest) (*DeleteSessionResponse, error) {
	return &DeleteSessionResponse{}, nil
}
func (noopSessionHandler) ForkSession(context.Context, *ForkSessionRequest) (*ForkSessionResponse, error) {
	return &ForkSessionResponse{}, nil
}
func (noopSessionHandler) ListSessions(context.Context, *ListSessionsRequest) (*ListSessionsResponse, error) {
	return &ListSessionsResponse{}, nil
}
func (noopSessionHandler) LoadSession(context.Context, *LoadSessionRequest) (*LoadSessionResponse, error) {
	return &LoadSessionResponse{}, nil
}
func (noopSessionHandler) NewSession(context.Context, *NewSessionRequest) (*NewSessionResponse, error) {
	return &NewSessionResponse{}, nil
}
func (noopSessionHandler) Prompt(context.Context, *PromptRequest) (*PromptResponse, error) {
	return &PromptResponse{StopReason: StopReasonEndTurn}, nil
}
func (noopSessionHandler) ResumeSession(context.Context, *ResumeSessionRequest) (*ResumeSessionResponse, error) {
	return &ResumeSessionResponse{}, nil
}
func (noopSessionHandler) SetSessionConfigOption(context.Context, *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error) {
	return &SetSessionConfigOptionResponse{}, nil
}
func (noopSessionHandler) SetSessionMode(context.Context, *SetSessionModeRequest) (*SetSessionModeResponse, error) {
	return &SetSessionModeResponse{}, nil
}
func (noopSessionHandler) SetSessionModel(context.Context, *SetSessionModelRequest) (*SetSessionModelResponse, error) {
	return &SetSessionModelResponse{}, nil
}

type testAgent struct {
	noopSessionHandler
	conn *AgentConnection
}

func (a *testAgent) Initialize(context.Context, *InitializeRequest) (*InitializeResponse, error) {
	return &InitializeResponse{
		ProtocolVersion: ProtocolVersion(1),
		AgentCapabilities: &AgentCapabilities{
			PromptCapabilities: &PromptCapabilities{Image: true},
		},
	}, nil
}

func (a *testAgent) NewSession(context.Context, *NewSessionRequest) (*NewSessionResponse, error) {
	return &NewSessionResponse{SessionID: "session-1"}, nil
}

func (a *testAgent) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	file, err := a.conn.ReadTextFile(ctx, &ReadTextFileRequest{SessionID: req.SessionID, Path: "/tmp/main.go"})
	if err != nil {
		return nil, err
	}
	if file.Content != "package main\n" {
		return nil, errUnexpectedCallbackResult
	}
	perm, err := a.conn.RequestPermission(ctx, &RequestPermissionRequest{
		SessionID: req.SessionID,
		ToolCall:  ToolCallUpdate{ToolCallID: "call-1", Title: ptr("edit file")},
		Options:   []PermissionOption{{OptionID: "allow", Name: "Allow", Kind: PermissionOptionKindAllowOnce}},
	})
	if err != nil {
		return nil, err
	}
	if perm.Outcome.Outcome != "selected" || perm.Outcome.OptionID != "allow" {
		return nil, errUnexpectedCallbackResult
	}
	term, err := a.conn.CreateTerminal(ctx, &CreateTerminalRequest{SessionID: req.SessionID, Command: "go", Args: []string{"test", "./..."}})
	if err != nil {
		return nil, err
	}
	if term.TerminalID != "terminal-1" {
		return nil, errUnexpectedCallbackResult
	}
	if err := a.conn.SessionUpdate(ctx, &SessionNotification{
		SessionID: req.SessionID,
		Update: SessionUpdate{
			SessionUpdate: "agent_message_chunk",
			Content:       ContentBlock{Type: ContentBlockTypeText, Text: "hello"},
		},
	}); err != nil {
		return nil, err
	}
	return &PromptResponse{StopReason: StopReasonEndTurn}, nil
}

func (a *testAgent) Cancel(context.Context, *CancelNotification) error { return nil }

type testClientHandler struct {
	updates chan SessionNotification
}

func newTestClientHandler() *testClientHandler {
	return &testClientHandler{updates: make(chan SessionNotification, 8)}
}

func (h *testClientHandler) Update(_ context.Context, params *SessionNotification) error {
	h.updates <- *params
	return nil
}

func (h *testClientHandler) ReadTextFile(context.Context, *ReadTextFileRequest) (*ReadTextFileResponse, error) {
	return &ReadTextFileResponse{Content: "package main\n"}, nil
}

func (h *testClientHandler) WriteTextFile(context.Context, *WriteTextFileRequest) (*WriteTextFileResponse, error) {
	return &WriteTextFileResponse{}, nil
}

func (h *testClientHandler) RequestPermission(context.Context, *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	return &RequestPermissionResponse{Outcome: RequestPermissionOutcome{Outcome: "selected", OptionID: "allow"}}, nil
}

func (h *testClientHandler) CreateTerminal(context.Context, *CreateTerminalRequest) (*CreateTerminalResponse, error) {
	return &CreateTerminalResponse{TerminalID: "terminal-1"}, nil
}

func (h *testClientHandler) TerminalOutput(context.Context, *TerminalOutputRequest) (*TerminalOutputResponse, error) {
	return &TerminalOutputResponse{Output: "ok", Truncated: false}, nil
}

func (h *testClientHandler) WaitForTerminalExit(context.Context, *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error) {
	return &WaitForTerminalExitResponse{}, nil
}

func (h *testClientHandler) KillTerminal(context.Context, *KillTerminalRequest) (*KillTerminalResponse, error) {
	return &KillTerminalResponse{}, nil
}

func (h *testClientHandler) ReleaseTerminal(context.Context, *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error) {
	return &ReleaseTerminalResponse{}, nil
}

func TestClientAgentPrompt(t *testing.T) {
	ctx := t.Context()
	clientTransport, agentTransport := NewInMemoryTransports()

	agentDone := make(chan error, 1)
	go func() {
		agentDone <- RunAgent(ctx, agentTransport, func(conn *AgentConnection) any {
			return &testAgent{conn: conn}
		})
	}()

	handler := newTestClientHandler()
	client, err := Connect(ctx, clientTransport, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	init, err := client.Initialize(ctx, &InitializeRequest{ProtocolVersion: ProtocolVersion(1)})
	if err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != ProtocolVersion(1) {
		t.Fatalf("protocol version = %d, want %d", init.ProtocolVersion, ProtocolVersion(1))
	}

	session, err := client.NewSession(ctx, &NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "session-1" {
		t.Fatalf("session ID = %q, want session-1", session.SessionID)
	}

	prompt, err := client.Prompt(ctx, &PromptRequest{
		SessionID: session.SessionID,
		Prompt:    []ContentBlock{{Type: ContentBlockTypeText, Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.StopReason != StopReasonEndTurn {
		t.Fatalf("stop reason = %q, want %q", prompt.StopReason, StopReasonEndTurn)
	}
	select {
	case update := <-handler.updates:
		if update.SessionID != session.SessionID {
			t.Fatalf("update session ID = %q, want %q", update.SessionID, session.SessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for session update")
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-agentDone; err != nil {
		t.Fatal(err)
	}
}
