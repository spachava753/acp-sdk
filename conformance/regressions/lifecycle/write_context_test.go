// Local Go transport/context regressions, NOT Rust workspace test ports.
package lifecycle_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Policy: the context on a public operation bounds an active write when the
// supplied stream supports interruption. This fixture uses real net.Conn I/O,
// not an arbitrary noninterruptible Writer. One byte is consumed before the
// cancellation/deadline, so an interrupted JSON frame may poison the stream;
// reuse after this ACTIVE partial-write abort is deliberately NOT required.
func TestActivePartialWriteHonorsOperationContext(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newStream(t, true)
				defer f.cleanup(t)
				ctx, cancel := context.WithCancel(context.Background())
				if mode == "deadline" {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				}
				defer cancel()
				calling := f.launch(func() error {
					_, err := f.client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1})
					return err
				})
				receive(t, f.writer.entered)
				one := make([]byte, 1)
				if n, err := f.peer.Read(one); err != nil || n != 1 || one[0] != '{' {
					t.Fatalf("could not establish partial JSON write: %q, %v", one[:n], err)
				}
				synctest.Wait() // The rest of the real pipe Write is blocked.
				if mode == "deadline" {
					time.Sleep(time.Second) // Fake clock, not a passing sleep window.
				} else {
					cancel()
				}
				synctest.Wait()
				if ctx.Err() == nil {
					t.Fatal("test operation context was not canceled")
				}
				select {
				case <-calling.done:
					if !errors.Is(calling.err, ctx.Err()) {
						t.Errorf("active write returned %v, want context cause %v", calling.err, ctx.Err())
					}
				default:
					t.Error("public call remained blocked in active Write after its context ended")
				}
				select {
				case <-f.writer.exited:
				default:
					t.Error("physical Write remained blocked after operation cancellation; returning only the caller would leak work")
				}
				// No graceful Close assertion against the accepted background
				// writer, and no reuse assertion for this partial-frame stream.
				// Independent cleanup closes owned endpoints AFTER the checks.
			})
		})
	}
}

type cancelDuringEncoding struct {
	cancel  context.CancelFunc
	encoded chan struct{}
	once    sync.Once
}

func (e *cancelDuringEncoding) Error() string {
	e.once.Do(func() {
		if e.cancel != nil {
			e.cancel()
		}
		close(e.encoded)
	})
	return "queued canceled frame"
}

func (*cancelDuringEncoding) Unwrap() error {
	return &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "queued canceled frame"}
}

// Policy: canceled prepared work must neither wait indefinitely for the SDK's
// serializer nor send later when it acquires it. The first real Write holds
// serialization while the second message's PUBLIC error-formatting callback
// cancels its context during encoding. This deterministically places cancel
// AFTER the initial Write context check, without a private mutex/stack hook.
// A second case leaves that context active at the formatting barrier and lets
// its real deadline expire while the first writer remains held. That catches
// a fix which only rechecks before Lock but cannot cancel a waiting writer.
// Neither case claims an instruction-level lock-entry observation.
//
// Mutex contention is not synctest-durably-blocked; use a bounded real-time
// failure wait here. Passing requires actual canceled completion, no late frame,
// and a successful later write on the same stream, not a quiet-period timeout.
func TestCanceledQueuedWriteNeverSendsAndStreamRemainsUsable(t *testing.T) {
	for _, mode := range []string{"cancel-during-encoding", "deadline-while-writer-held"} {
		t.Run(mode, func(t *testing.T) { queuedWrite(t, mode) })
	}
}

func queuedWrite(t *testing.T, mode string) {
	f := newStream(t, false)
	defer f.cleanup(t)
	first := f.launch(func() error { return f.raw.Write(context.Background(), notify("_regression/first")) })
	receive(t, f.writer.entered)
	ctx, cancel := context.WithCancel(context.Background())
	if mode == "deadline-while-writer-held" {
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	}
	defer cancel()
	encoding := &cancelDuringEncoding{encoded: make(chan struct{})}
	if mode == "cancel-during-encoding" {
		encoding.cancel = cancel
	}
	id, err := jsonrpc.MakeID("queued-canceled")
	if err != nil {
		t.Fatal(err)
	}
	second := f.launch(func() error {
		return f.raw.Write(ctx, &jsonrpc.Response{ID: id, Error: encoding})
	})
	receive(t, encoding.encoded)
	if mode == "deadline-while-writer-held" {
		if ctx.Err() != nil {
			t.Fatal("scheduling precondition failed: context expired before the encoding barrier")
		}
		// Unlike the encoding-cancel case, this context remains active at
		// the formatting barrier. Keep the first writer held throughout the
		// real deadline, allowing the short encoder to reach serialization
		// waiting first. This has an explicit scheduling limitation: public
		// formatting progress is observed, not a precise lock instruction.
		<-ctx.Done()
	}
	want := ctx.Err()
	if want == nil {
		t.Fatal("second operation context did not end")
	}
	select {
	case <-second.done:
		if !errors.Is(second.err, want) {
			t.Errorf("queued Write returned %v, want %v", second.err, want)
		}
	case <-time.After(operationLimit):
		t.Error("canceled queued Write did not return while another writer held serialization")
	}

	// Only now release the FIRST background-context writer by reading. Do not
	// call Close to force it out: graceful drain is allowed to wait for it.
	var frames []jsonrpc.Message
	draining := f.launch(func() error {
		reader := bufio.NewReader(f.peer)
		for range 4 {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return err
			}
			message, err := jsonrpc.DecodeMessage(line)
			if err != nil {
				return err
			}
			frames = append(frames, message)
			if req, ok := message.(*jsonrpc.Request); ok && req.Method == "_regression/after" {
				return nil
			}
		}
		return fmt.Errorf("later-write terminator not received after %d frames", len(frames))
	})
	if err := first.wait(t); err != nil {
		t.Fatalf("unrelated first write was aborted by queued cancellation: %v", err)
	}
	if err := second.wait(t); !errors.Is(err, want) {
		t.Errorf("queued canceled Write was not rejected after release: %v (want %v)", err, want)
	}
	after := f.launch(func() error { return f.raw.Write(context.Background(), notify("_regression/after")) })
	if err := after.wait(t); err != nil {
		t.Fatalf("stream became unusable after a canceled unsent write: %v", err)
	}
	if err := draining.wait(t); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Errorf("peer received %d frames, want only first and after; canceled message was sent", len(frames))
	}
	for i, message := range frames {
		req, ok := message.(*jsonrpc.Request)
		if !ok || req.IsCall() {
			t.Errorf("canceled response frame reached peer at position %d: %#v", i, message)
			continue
		}
		if (i == 0 && req.Method != "_regression/first") || (i == len(frames)-1 && req.Method != "_regression/after") {
			t.Errorf("stream order changed at %d: %#v", i, message)
		}
	}
}
