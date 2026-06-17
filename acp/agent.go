// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"

	"github.com/spachava753/acp-sdk/internal/jsonrpc2"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// AgentFactory constructs an agent after the bidirectional connection is ready.
type AgentFactory func(*AgentConnection) any

// AgentConnection lets an agent call client-side ACP methods on its peer.
type AgentConnection struct {
	rpc *rpcEndpoint
}

// RunAgent serves an ACP agent over transport until the connection closes.
func RunAgent(ctx context.Context, transport Transport, newAgent AgentFactory) error {
	raw, err := transport.Connect(ctx)
	if err != nil {
		return err
	}
	endpoint := &rpcEndpoint{raw: raw}
	ac := &AgentConnection{rpc: endpoint}
	var agent any
	rpc := jsonrpc2.NewConnection(ctx, jsonrpc2.ConnectionConfig{
		Reader: raw,
		Writer: raw,
		Closer: raw,
		Bind: func(conn *jsonrpc2.Connection) jsonrpc2.Handler {
			endpoint.conn = conn
			agent = newAgent(ac)
			return jsonrpc2.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) (any, error) {
				return handleAgentRequest(ctx, agent, req)
			})
		},
	})
	return rpc.Wait()
}

// SessionUpdate sends a session/update notification to the client.
func (c *AgentConnection) SessionUpdate(ctx context.Context, params *SessionNotification) error {
	return c.Update(ctx, params)
}

// Close closes the agent-side ACP connection.
func (c *AgentConnection) Close() error {
	return c.rpc.conn.Close()
}
