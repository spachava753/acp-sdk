// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"

	"github.com/spachava753/acp-sdk/internal/jsonrpc2"
)

// AgentFactory constructs an agent after the bidirectional connection is ready.
type AgentFactory func(*AgentConnection) any

// AgentConnection lets an agent call client-side ACP methods on its peer.
type AgentConnection struct {
	rpc *rpcEndpoint
}

// RunAgent serves an ACP agent until the peer closes or ctx ends. Canceling ctx
// aborts transport I/O and cancels cooperative handler work; ordinary input EOF
// can still drain final responses to a peer that has only closed its write half.
func RunAgent(ctx context.Context, transport Transport, newAgent AgentFactory) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := transport.Connect(ctx)
	if err != nil {
		return err
	}
	endpoint := &rpcEndpoint{raw: raw}
	ac := &AgentConnection{rpc: endpoint}
	rpc := newRPCConnection(ctx, raw, func(conn *jsonrpc2.Connection) jsonrpc2.Handler {
		endpoint.conn = conn
		return newAgentHandler(newAgent(ac))
	})
	stop := context.AfterFunc(ctx, func() { rpc.Abort(ctx.Err()) })
	defer stop()
	return rpc.Wait()
}

// Close stops the agent-side connection, cancels pending requests and drains
// accepted writes and notifications. Do not call it synchronously from a handler
// that it must wait for. Cancel RunAgent's context to abort blocked transport I/O.
func (c *AgentConnection) Close() error {
	return c.rpc.conn.Close()
}
