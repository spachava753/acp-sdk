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

// Interface guards catch handler signature mismatches at compile time. Methods
// the agent does not implement are answered with "method not found".
var (
	_ acp.InitializeHandler    = (*agent)(nil)
	_ acp.SessionNewHandler    = (*agent)(nil)
	_ acp.SessionPromptHandler = (*agent)(nil)
	_ acp.SessionCancelHandler = (*agent)(nil)
)

func (a *agent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersion(1),
		AgentCapabilities: &acp.AgentCapabilities{
			PromptCapabilities: &acp.PromptCapabilities{EmbeddedContext: true},
		},
		AgentInfo: &acp.Implementation{Name: "example-agent", Version: "dev"},
	}, nil
}

func (a *agent) SessionNew(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
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

func (a *agent) SessionPrompt(ctx context.Context, req *acp.PromptRequest) (*acp.PromptResponse, error) {
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

func (a *agent) SessionCancel(context.Context, *acp.CancelNotification) error { return nil }

func main() {
	err := acp.RunAgent(context.Background(), &acp.StdioTransport{}, func(conn *acp.AgentConnection) any {
		return &agent{conn: conn, sessions: make(map[string]struct{})}
	})
	if err != nil {
		log.Fatal(err)
	}
}
