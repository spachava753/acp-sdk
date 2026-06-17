// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"

	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Client is an ACP client connection used to call agent-side methods.
type Client struct {
	rpc     *rpcEndpoint
	handler any
}

// Connect opens an ACP client connection over transport.
func Connect(ctx context.Context, transport Transport, handler any) (*Client, error) {
	client := &Client{handler: handler}
	rpc, err := connectRPC(ctx, transport, func(ctx context.Context, req *jsonrpc.Request) (any, error) {
		return client.handle(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	client.rpc = rpc
	return client, nil
}

// Close closes the client ACP connection.
func (c *Client) Close() error {
	return c.rpc.conn.Close()
}
