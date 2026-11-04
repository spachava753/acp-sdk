// Local Go API regression, NOT a Rust SDK workspace test port.
package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

// Policy: once a synchronous caller has abandoned an already-sent request,
// that local wait must not indefinitely hold public Close behind a peer reply
// which will never come. Optional $/cancel_request support is not required and
// the peer ignores it. This is Go resource/lifetime policy, not an ACP timeout
// requirement or a claim that the SDK cancels remote application work.
func TestAbandonedCallDoesNotHoldPublicClose(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newMessagePeer()
				c, err := acp.Connect(context.Background(), p, nil)
				if err != nil {
					t.Fatal(err)
				}
				var closing *errTask
				var tasks []*errTask
				defer func() {
					// Only failure-safe cleanup causes input EOF. No test assertion
					// is allowed to pass because this preclosed the transport.
					_ = p.Close()
					if closing == nil {
						closing = start(c.Close)
					}
					for _, op := range append(tasks, closing) {
						op.wait(t)
					}
				}()
				ctx, cancel := context.WithCancel(context.Background())
				if mode == "deadline" {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				}
				defer cancel()
				abandoned := start(func() error {
					_, err := c.CallExtension(ctx, "_regression/never-answer", struct{}{})
					return err
				})
				tasks = append(tasks, abandoned)
				old := p.nextCall(t, "_regression/never-answer")
				// Observing the complete outgoing request establishes successful
				// publication; this is not cancellation before Write began.
				if mode == "deadline" {
					time.Sleep(time.Second) // Synctest fake time, no wall-clock wait.
				} else {
					cancel()
				}
				synctest.Wait()
				if err := abandoned.wait(t); !errors.Is(err, ctx.Err()) || ctx.Err() == nil {
					t.Fatalf("abandoned call error = %v, context error = %v", err, ctx.Err())
				}

				var response json.RawMessage
				healthy := start(func() error {
					var err error
					response, err = c.CallExtension(context.Background(), "_regression/healthy", struct{}{})
					return err
				})
				tasks = append(tasks, healthy)
				req := p.nextCall(t, "_regression/healthy")
				if req.ID.Raw() == old.ID.Raw() {
					t.Fatal("later RPC reused the outstanding request ID")
				}
				// This is the ONLY reply the peer sends. In particular there is
				// no late reply, cancellation result, or error for old.ID.
				p.push(t, map[string]any{"jsonrpc": "2.0", "id": req.ID.Raw(), "result": map[string]any{"ok": true}})
				if err := healthy.wait(t); err != nil {
					t.Fatalf("connection became unusable after local abandonment: %v", err)
				}
				var got struct {
					OK bool `json:"ok"`
				}
				if err := json.Unmarshal(response, &got); err != nil || !got.OK {
					t.Fatalf("later RPC result = %s, %v", response, err)
				}
				synctest.Wait()
				closing = start(c.Close)
				synctest.Wait()
				select {
				case <-p.closed:
				default:
					t.Error("public Close did not reach the underlying closer after only abandoned work remained")
				}
				select {
				case <-closing.done:
					if closing.err != nil {
						t.Errorf("public Close failed: %v", closing.err)
					}
				default:
					t.Error("public Close still waits for the peer's permanently unanswered abandoned request")
				}
			})
		})
	}
}
