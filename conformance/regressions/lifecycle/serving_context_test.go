// Local Go serving-lifetime regressions, NOT Rust workspace test ports.
package lifecycle_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

type heldHandler struct {
	*acp.ExtensionMux
	started  chan context.Context
	finished chan struct{}
	release  chan struct{}
	once     sync.Once
}

func newHeldHandler(t *testing.T) *heldHandler {
	t.Helper()
	h := &heldHandler{ExtensionMux: acp.NewExtensionMux(), started: make(chan context.Context, 1), finished: make(chan struct{}), release: make(chan struct{})}
	if err := acp.AddExtensionRequest(h.ExtensionMux, "_regression/held", func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		h.started <- ctx
		defer close(h.finished)
		select {
		case <-ctx.Done():
		case <-h.release: // Cleanup escape hatch, never the passing event.
		}
		return &struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *heldHandler) unblock() { h.once.Do(func() { close(h.release) }) }

func serving(t *testing.T, ctx context.Context, p *messagePeer, handler any) *errTask {
	t.Helper()
	ready := make(chan struct{})
	op := start(func() error {
		return acp.RunAgent(ctx, p, func(*acp.AgentConnection) any {
			close(ready)
			return handler
		})
	})
	receive(t, ready)
	return op
}

// Policy: RunAgent's context owns its serving lifetime. A local cancel or
// deadline must finish an idle loop and release cooperative pending handlers.
// This is a selected Go API policy, NOT an automatic session/cancel operation,
// not a Rust port, and not an assertion that clean input EOF cancels all work.
func TestRunAgentContextOwnsServingLifetime(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pending bool
		expired bool
	}{
		{"idle-cancel", false, false},
		{"idle-deadline", false, true},
		{"pending-handler-cancel", true, false},
		{"pending-handler-deadline", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newMessagePeer()
				ctx, cancel := context.WithCancel(context.Background())
				if tc.expired {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				}
				defer cancel()
				var handler *heldHandler
				var value any
				if tc.pending {
					handler = newHeldHandler(t)
					value = handler
				}
				op := serving(t, ctx, p, value)
				defer func() {
					if handler != nil {
						handler.unblock()
					}
					_ = p.Close()
					op.wait(t)
				}()
				var handlerCtx context.Context
				if tc.pending {
					p.push(t, map[string]any{"jsonrpc": "2.0", "id": "held", "method": "_regression/held", "params": map[string]any{}})
					handlerCtx = receive(t, handler.started)
					if handlerCtx.Err() != nil {
						t.Fatal("pending handler started with a canceled context")
					}
				}
				synctest.Wait()
				if tc.expired {
					time.Sleep(time.Second) // Fake clock advances the real ctx timer.
				} else {
					cancel()
				}
				synctest.Wait()
				if ctx.Err() == nil {
					t.Fatal("serving context did not end")
				}
				if tc.pending {
					if handlerCtx.Err() == nil {
						t.Error("RunAgent context ended without canceling its pending handler context")
					}
					select {
					case <-handler.finished:
					default:
						t.Error("cooperative handler did not finish after serving context cancellation")
					}
				}
				select {
				case <-op.done:
					// Permit graceful completion or a wrapping of cancellation;
					// do not invent an exact error message or wire error code.
					if op.err != nil && !errors.Is(op.err, context.Canceled) && !errors.Is(op.err, context.DeadlineExceeded) {
						t.Errorf("serving cancellation returned an unrelated error: %v", op.err)
					}
				default:
					t.Error("RunAgent did not complete after its serving context ended")
				}
				select {
				case <-p.closed:
				default:
					t.Error("serving cancellation did not close its underlying connection")
				}
			})
		})
	}
}

// A genuine non-EOF read error means the transport has failed, not that only
// its input half closed. Cooperative handler cancellation here must therefore
// not be generalized to clean input EOF/final-response drain (covered by the
// committed Rust-derived half-close tests, which this package does not alter).
func TestFatalReadErrorCancelsCooperativeHandler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newMessagePeer()
		handler := newHeldHandler(t)
		op := serving(t, context.Background(), p, handler)
		defer func() { handler.unblock(); _ = p.Close(); op.wait(t) }()
		p.push(t, map[string]any{"jsonrpc": "2.0", "id": "held", "method": "_regression/held", "params": map[string]any{}})
		ctx := receive(t, handler.started)
		failure := errors.New("fatal read failure")
		p.input <- readEvent{err: failure}
		synctest.Wait()
		if ctx.Err() == nil {
			t.Error("fatal transport read error did not cancel the active handler context")
		}
		select {
		case <-handler.finished:
		default:
			t.Error("cooperative handler remained blocked after fatal transport failure")
		}
		select {
		case <-op.done:
			if !errors.Is(op.err, failure) {
				t.Errorf("fatal transport failure lost its cause: %v", op.err)
			}
		default:
			t.Error("RunAgent stayed blocked behind a handler after fatal read failure")
		}
	})
}
