// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spachava753/acp-sdk/internal/jsonrpc2"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

type rpcEndpoint struct {
	conn *jsonrpc2.Connection
	raw  Connection
}

func connectRPC(ctx context.Context, transport Transport, handle func(context.Context, *jsonrpc.Request) (any, error)) (*rpcEndpoint, error) {
	raw, err := transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	rpc := jsonrpc2.NewConnection(ctx, jsonrpc2.ConnectionConfig{
		Reader: raw,
		Writer: raw,
		Closer: raw,
		Bind: func(*jsonrpc2.Connection) jsonrpc2.Handler {
			return jsonrpc2.HandlerFunc(handle)
		},
	})
	return &rpcEndpoint{conn: rpc, raw: raw}, nil
}

func call[R any](ctx context.Context, conn *jsonrpc2.Connection, method string, params any) (*R, error) {
	var result R
	ac := conn.Call(ctx, method, params)
	if err := ac.Await(ctx, &result); err != nil {
		return nil, translateCallError(method, err)
	}
	return &result, nil
}

func notify(ctx context.Context, conn *jsonrpc2.Connection, method string, params any) error {
	if err := conn.Notify(ctx, method, params); err != nil {
		return translateCallError(method, err)
	}
	return nil
}

func translateCallError(method string, err error) error {
	if errors.Is(err, jsonrpc2.ErrClientClosing) || errors.Is(err, jsonrpc2.ErrServerClosing) {
		return fmt.Errorf("%w: %s", ErrConnectionClosed, err)
	}
	return fmt.Errorf("calling %q: %w", method, err)
}

func decodeParams[T any](req *jsonrpc.Request) (*T, error) {
	var params T
	if len(req.Params) == 0 {
		return &params, nil
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("%w: %v", jsonrpc2.ErrInvalidParams, err)
	}
	return &params, nil
}

func methodNotFound(method string) error {
	return fmt.Errorf("%w: %s", jsonrpc2.ErrMethodNotFound, method)
}
