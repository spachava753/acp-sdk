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
	client, done := connectConformanceClient(t, newConformanceClient(), func(conn *acp.AgentConnection) any {
		return &basicAgent{conn: conn}
	})
	defer closeConformanceClient(t, client, done)

	_, err := client.Authenticate(t.Context(), &acp.AuthenticateRequest{MethodID: "agent"})
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

type basicAgent struct {
	noopSessionHandler
	conn *acp.AgentConnection
}

func (a *basicAgent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion(1), AuthMethods: []acp.AuthMethod{}}, nil
}

func (a *basicAgent) NewSession(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: acp.SessionId("sess-123")}, nil
}

func (a *basicAgent) Prompt(context.Context, *acp.PromptRequest) (*acp.PromptResponse, error) {
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *basicAgent) Cancel(context.Context, *acp.CancelNotification) error { return nil }
