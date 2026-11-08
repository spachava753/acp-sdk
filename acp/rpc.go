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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	rpc := newRPCConnection(ctx, raw, func(*jsonrpc2.Connection) jsonrpc2.Handler {
		return jsonrpc2.HandlerFunc(handle)
	})
	return &rpcEndpoint{conn: rpc, raw: raw}, nil
}

func call[R any](ctx context.Context, conn *jsonrpc2.Connection, method string, params any) (*R, error) {
	var result R
	ac := conn.Call(ctx, method, params)
	var err error
	if method == "session/prompt" || method == "session/load" {
		err = ac.AwaitOrdered(ctx, &result)
	} else {
		err = ac.Await(ctx, &result)
	}
	if err != nil {
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
		return fmt.Errorf("%w: calling %q: %w", ErrConnectionClosed, method, err)
	}
	return fmt.Errorf("calling %q: %w", method, err)
}

// Extension parameters keep their documented missing/null zero-value behavior.
// Known protocol methods instead run their schema decoder on an empty object.
func decodeParams[T any](req *jsonrpc.Request) (*T, error) {
	if len(req.Params) == 0 || isJSONNull(req.Params) {
		return new(T), nil
	}
	return decodeParamData[T](req.Params, req.Params)
}

func decodeProtocolParams[T any](req *jsonrpc.Request) (*T, error) {
	data := req.Params
	if len(data) == 0 || isJSONNull(data) {
		data = json.RawMessage(`{}`)
	}
	return decodeParamData[T](data, req.Params)
}

func decodeParamData[T any](data, original json.RawMessage) (*T, error) {
	var params T
	if err := unmarshalJSON(data, &params); err != nil {
		if !json.Valid(original) {
			original = json.RawMessage(`null`)
		}
		detail, _ := json.Marshal(struct {
			JSON  json.RawMessage `json:"json"`
			Phase string          `json:"phase"`
			Error string          `json:"error"`
		}{original, "deserialization", err.Error()})
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("invalid params: %v", err), Data: detail}
	}
	return &params, nil
}

func rpcResult[T any](result *T, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return result, nil
}

func methodNotFound(method string) error {
	data, _ := json.Marshal(method)
	return &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: fmt.Sprintf("method not found: %s", method), Data: data}
}

const cancelRequestMethod = "$/cancel_request"

// newRPCConnection keeps the protocol-specific cancellation binding outside the
// reusable JSON-RPC engine. Retiring a local call never waits for its peer reply.
func newRPCConnection(ctx context.Context, raw Connection, bind func(*jsonrpc2.Connection) jsonrpc2.Handler) *jsonrpc2.Connection {
	preempter := &protocolPreempter{}
	return jsonrpc2.NewConnection(ctx, jsonrpc2.ConnectionConfig{
		Reader:    raw,
		Writer:    raw,
		Closer:    raw,
		Preempter: preempter,
		Bind: func(conn *jsonrpc2.Connection) jsonrpc2.Handler {
			preempter.conn = conn
			return bind(conn)
		},
		OnCallCanceled: func(ctx context.Context, id jsonrpc2.ID) {
			// This is best effort during connection teardown. The engine owns the
			// context, so a cancellation write cannot keep local Close alive.
			_ = preempter.conn.Notify(ctx, cancelRequestMethod, struct {
				RequestID any `json:"requestId"`
			}{id.Raw()})
		},
	})
}

type protocolPreempter struct{ conn *jsonrpc2.Connection }

func (p *protocolPreempter) Preempt(_ context.Context, req *jsonrpc.Request) (any, error) {
	if req.IsCall() {
		return nil, jsonrpc2.ErrNotHandled
	}
	method, params := req.Method, req.Params
	// Accept the Rust-compatible wrapped control form, but never forward a
	// cancellation to another hop. The identifier targets this connection only.
	for method == "proxy/successor" {
		var wrapper struct {
			Method *string         `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := unmarshalJSON(params, &wrapper); err != nil || wrapper.Method == nil {
			return nil, jsonrpc2.ErrNotHandled
		}
		method, params = *wrapper.Method, wrapper.Params
	}
	if method != cancelRequestMethod {
		return nil, jsonrpc2.ErrNotHandled
	}
	var notification struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if unmarshalJSON(params, &notification) != nil || len(notification.RequestID) == 0 {
		return nil, nil
	}
	var value any
	if unmarshalJSON(notification.RequestID, &value) != nil {
		return nil, nil
	}
	if id, err := jsonrpc.MakeID(value); err == nil {
		p.conn.Cancel(id)
	}
	return nil, nil
}
