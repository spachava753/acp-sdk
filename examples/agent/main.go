// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync"

	"github.com/spachava753/acp-sdk/acp"
)

type agent struct {
	conn     *acp.AgentConnection
	mu       sync.Mutex
	sessions map[string]struct{}
}

func (a *agent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersion(1),
		AgentCapabilities: &acp.AgentCapabilities{
			PromptCapabilities: &acp.PromptCapabilities{EmbeddedContext: true},
		},
		AgentInfo: &acp.Implementation{Name: "example-agent", Version: "dev"},
	}, nil
}

func (a *agent) NewSession(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	var idBytes [16]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(idBytes[:])
	a.mu.Lock()
	a.sessions[id] = struct{}{}
	a.mu.Unlock()
	return &acp.NewSessionResponse{SessionID: acp.SessionId(id)}, nil
}

func (a *agent) Prompt(ctx context.Context, req *acp.PromptRequest) (*acp.PromptResponse, error) {
	if err := a.conn.SessionUpdate(ctx, &acp.SessionNotification{
		SessionID: req.SessionID,
		Update: acp.SessionUpdate{
			SessionUpdate: "agent_message_chunk",
			Content:       acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: "Example agent received your prompt."},
		},
	}); err != nil {
		return nil, err
	}
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *agent) Cancel(context.Context, *acp.CancelNotification) error { return nil }

func (a *agent) CloseSession(context.Context, *acp.CloseSessionRequest) (*acp.CloseSessionResponse, error) {
	return &acp.CloseSessionResponse{}, nil
}

func (a *agent) DeleteSession(context.Context, *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error) {
	return &acp.DeleteSessionResponse{}, nil
}

func (a *agent) ForkSession(context.Context, *acp.ForkSessionRequest) (*acp.ForkSessionResponse, error) {
	return &acp.ForkSessionResponse{}, nil
}

func (a *agent) ListSessions(context.Context, *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
	return &acp.ListSessionsResponse{}, nil
}

func (a *agent) LoadSession(context.Context, *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
	return &acp.LoadSessionResponse{}, nil
}

func (a *agent) ResumeSession(context.Context, *acp.ResumeSessionRequest) (*acp.ResumeSessionResponse, error) {
	return &acp.ResumeSessionResponse{}, nil
}

func (a *agent) SetSessionConfigOption(context.Context, *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	return &acp.SetSessionConfigOptionResponse{}, nil
}

func (a *agent) SetSessionMode(context.Context, *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
	return &acp.SetSessionModeResponse{}, nil
}

func main() {
	err := acp.RunAgent(context.Background(), &acp.StdioTransport{}, func(conn *acp.AgentConnection) any {
		return &agent{conn: conn, sessions: make(map[string]struct{})}
	})
	if err != nil {
		log.Fatal(err)
	}
}
