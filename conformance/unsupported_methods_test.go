// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package conformance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

func TestConformanceUnsupportedOptionalMethodsReturnMethodNotFound(t *testing.T) {
	client, done := connectConformanceClient(t, newConformanceClient(), func(conn *acp.AgentConnection) acp.Agent {
		return &basicAgent{conn: conn}
	})
	defer closeConformanceClient(t, client, done)

	_, err := client.LoadSession(t.Context(), &acp.LoadSessionRequest{SessionID: "sess-123", CWD: "/workspace", MCPServers: []acp.MCPServer{}})
	if err == nil {
		t.Fatal("LoadSession succeeded, want method not found")
	}
	var wireErr *jsonrpc.Error
	if !errors.As(err, &wireErr) {
		t.Fatalf("error %v does not wrap jsonrpc error", err)
	}
	if wireErr.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", wireErr.Code)
	}
}

type basicAgent struct {
	conn *acp.AgentConnection
}

func (a *basicAgent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion, AuthMethods: []acp.AuthMethod{}}, nil
}

func (a *basicAgent) NewSession(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: "sess-123"}, nil
}

func (a *basicAgent) Prompt(context.Context, *acp.PromptRequest) (*acp.PromptResponse, error) {
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *basicAgent) Cancel(context.Context, *acp.CancelNotification) error { return nil }
