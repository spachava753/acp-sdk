// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package conformance_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

func TestConformanceBidirectionalCallbacksDuringPrompt(t *testing.T) {
	handler := newConformanceClient()
	handler.files["/workspace/main.go"] = "package main\n"
	agent := &callbackAgent{}
	client, done := connectConformanceClient(t, handler, func(conn *acp.AgentConnection) any {
		agent.conn = conn
		return agent
	})
	defer closeConformanceClient(t, client, done)

	res, err := client.Prompt(t.Context(), &acp.PromptRequest{
		SessionID: acp.SessionId("sess-123"),
		Prompt:    []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "inspect"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("stop reason = %q, want %q", res.StopReason, acp.StopReasonEndTurn)
	}
	if got := handler.fileContent("/workspace/generated.go"); got != "package generated\n" {
		t.Fatalf("written file = %q, want generated file", got)
	}
	if got := waitForUpdate(t, handler); got.SessionID != "sess-123" || got.Update.SessionUpdate != "agent_message_chunk" {
		t.Fatalf("session update = %#v", got)
	}
	if got := handler.createdTerminal(); got != "go" {
		t.Fatalf("created terminal command = %q, want go", got)
	}
}

type callbackAgent struct {
	noopSessionHandler
	conn *acp.AgentConnection
}

func (a *callbackAgent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion(1), AuthMethods: []acp.AuthMethod{}}, nil
}

func (a *callbackAgent) NewSession(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: acp.SessionId("sess-123")}, nil
}

func (a *callbackAgent) Prompt(ctx context.Context, req *acp.PromptRequest) (*acp.PromptResponse, error) {
	file, err := a.conn.ReadTextFile(ctx, &acp.ReadTextFileRequest{SessionID: req.SessionID, Path: "/workspace/main.go"})
	if err != nil {
		return nil, err
	}
	if file.Content != "package main\n" {
		return nil, fmt.Errorf("read file content = %q", file.Content)
	}
	if _, err := a.conn.WriteTextFile(ctx, &acp.WriteTextFileRequest{SessionID: req.SessionID, Path: "/workspace/generated.go", Content: "package generated\n"}); err != nil {
		return nil, err
	}
	permission, err := a.conn.RequestPermission(ctx, &acp.RequestPermissionRequest{
		SessionID: req.SessionID,
		ToolCall:  acp.ToolCallUpdate{ToolCallID: "call-1", Title: stringPtr("Write File")},
		Options:   []acp.PermissionOption{{OptionID: "allow", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce}},
	})
	if err != nil {
		return nil, err
	}
	if permission.Outcome.Outcome != "selected" || permission.Outcome.OptionID != "allow" {
		return nil, fmt.Errorf("permission response = %#v", permission)
	}
	terminal, err := a.conn.CreateTerminal(ctx, &acp.CreateTerminalRequest{SessionID: req.SessionID, Command: "go", Args: []string{"test", "./..."}})
	if err != nil {
		return nil, err
	}
	if terminal.TerminalID != "term-1" {
		return nil, fmt.Errorf("terminal ID = %q", terminal.TerminalID)
	}
	if err := a.conn.SessionUpdate(ctx, &acp.SessionNotification{SessionID: req.SessionID, Update: acp.SessionUpdate{SessionUpdate: "agent_message_chunk", Content: acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: "done"}}}); err != nil {
		return nil, err
	}
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *callbackAgent) Cancel(context.Context, *acp.CancelNotification) error { return nil }
