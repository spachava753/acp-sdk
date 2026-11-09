// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package acp

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"

	"github.com/spachava753/acp-sdk/internal/jsonrpc2"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Notifications are handled one at a time in receive order, and a message
// starts only after the notifications received before it have been handled.
// Requests release the dispatcher once started: a request handler may run for a
// whole prompt turn and must not hold up later messages.
func beginHandler(ctx context.Context, req *jsonrpc.Request) {
	if req.IsCall() {
		jsonrpc2.Async(ctx)
	}
}

// Only a provided initialization handler opts an agent into initialization
// gating. Raw agents without that handler retain their existing request flow.
func newAgentHandler(handler any) jsonrpc2.Handler {
	return &agentHandler{handler: handler, gated: hasAgentInitializeHandler(handler)}
}

type agentHandler struct {
	handler     any
	gated       bool
	initialized atomic.Bool
}

func (h *agentHandler) Handle(ctx context.Context, req *jsonrpc.Request) (any, error) {
	if h.gated && req.IsCall() && req.Method != "initialize" && !h.initialized.Load() {
		beginHandler(ctx, req)
		return nil, fmt.Errorf("%w: initialize must complete before %q", jsonrpc2.ErrInvalidRequest, req.Method)
	}
	return handleAgentRequest(ctx, h.handler, req)
}

// BeforeResponse runs after the RPC engine selects a serialized result or its
// fallback error, before making it visible to the peer. Handler return alone
// cannot activate the connection: encoding may still fail or yield JSON null.
func (h *agentHandler) BeforeResponse(req *jsonrpc.Request, response *jsonrpc.Response) {
	if h.gated && req.IsCall() && req.Method == "initialize" && response.Error == nil {
		result := bytes.TrimSpace(response.Result)
		if len(result) != 0 && !bytes.Equal(result, []byte("null")) {
			h.initialized.Store(true)
		}
	}
}
