// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package lifecycle_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// RunAgent returns, and closes its connection, when its input ends, its
// transport fails, or its serving context ends. A clean EOF returns nil, a
// read error is returned with its cause, and an ended context returns nil or
// the context's error. A read error or an ended context also cancels the
// context of a pending handler, so a cooperative handler finishes rather than
// holding RunAgent open. Clean EOF is not included with a pending handler:
// there the final reply is drained instead (see TestFinalReplyAfterInputEOF).
func TestServingEnds(t *testing.T) {
	failure := errors.New("read failed")
	for _, tc := range []struct {
		name    string
		end     string // "eof", "read error", "cancel" or "deadline"
		pending bool   // a cooperative handler is running when serving ends
	}{
		{"input EOF", "eof", false},
		{"read error", "read error", false},
		{"read error with pending handler", "read error", true},
		{"context canceled", "cancel", false},
		{"context deadline", "deadline", false},
		{"context canceled with pending handler", "cancel", true},
		{"context deadline with pending handler", "deadline", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				if tc.end == "deadline" {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				}
				defer cancel()
				var handler any
				var h *held
				if tc.pending {
					mux := acp.NewExtensionMux()
					h = addHeld(t, mux, true)
					defer h.unblock()
					handler = mux
				}
				peer, transport := acptest.NewPeer(t)
				_, serving := serve(t, ctx, transport, handler)

				var handlerCtx context.Context
				if tc.pending {
					peer.Request("held", heldMethod, map[string]any{"tag": "held"})
					handlerCtx = acptest.Await(t, h.started).ctx
					if handlerCtx.Err() != nil {
						t.Fatal("pending handler started with a canceled context")
					}
				}
				synctest.Wait()
				switch tc.end {
				case "eof":
					peer.CloseInput()
				case "read error":
					peer.FailInput(failure)
				case "cancel":
					cancel()
				case "deadline":
					<-ctx.Done()
				}
				synctest.Wait()

				if tc.pending {
					if handlerCtx.Err() == nil {
						t.Error("pending handler's context was not canceled")
					}
					if _, ok := poll(h.finished); !ok {
						t.Error("cooperative handler did not finish")
					}
				}
				err, ok := poll(serving)
				if !ok {
					t.Fatal("RunAgent did not return")
				}
				switch tc.end {
				case "eof":
					if err != nil {
						t.Errorf("RunAgent after clean EOF = %v, want nil", err)
					}
				case "read error":
					if !errors.Is(err, failure) {
						t.Errorf("RunAgent after read error = %v, want its cause", err)
					}
				default:
					if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						t.Errorf("RunAgent after its context ended = %v, want nil or the context error", err)
					}
				}
				// Output having ended shows RunAgent closed the connection.
				drainNow(t, peer)
			})
		})
	}
}
