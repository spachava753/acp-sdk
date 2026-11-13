// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package jsonrpc2

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Once a successfully written call's own context expires, its pending entry
// must be released even though the peer never responds. This is SDK resource
// behavior rather than an ACP wire rule. acp.call supplies the same context to
// Call and Await, so exercising that pair covers the SDK's real usage.
//
// Close can hide retention by provoking EOF, so the one internal observation is
// outgoingCalls under stateMu, before a peer reply or raw transport closure.
func TestWrittenCallContextCancellationReleasesPendingState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		boundary := &retentionBoundary{
			incoming: make(chan Message),
			written:  make(chan Message, 8),
			closed:   make(chan struct{}),
		}
		connection := NewConnection(context.Background(), ConnectionConfig{
			Reader: boundary,
			Writer: boundary,
			Closer: boundary,
			Bind: func(*Connection) Handler {
				return HandlerFunc(func(context.Context, *Request) (any, error) {
					return nil, ErrNotHandled
				})
			},
		})
		defer func() {
			// This must happen even after a RED assertion: forcing Reader EOF
			// clears abandoned entries and prevents graceful Close from hanging.
			boundary.Close()
			closed := make(chan error, 1)
			go func() { closed <- connection.Close() }()
			synctest.Wait()
			select {
			case err := <-closed:
				if err != nil {
					t.Errorf("cleanup Close: %v", err)
				}
			default:
				t.Error("cleanup Close did not finish after forcing raw EOF")
			}
		}()

		pending := func() int {
			connection.stateMu.Lock()
			defer connection.stateMu.Unlock()
			return len(connection.state.outgoingCalls)
		}
		baseline := pending()

		for _, phase := range []string{"cancel", "deadline"} {
			var ctx context.Context
			var cancel context.CancelFunc
			wantError := context.Canceled
			if phase == "deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				wantError = context.DeadlineExceeded
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()

			call := connection.Call(ctx, "_local/abandoned", map[string]string{"phase": phase})
			request := boundary.nextCall(t)
			if request.ID != call.ID() || request.Method != "_local/abandoned" {
				t.Fatalf("%s: wrong written call: %+v", phase, request)
			}
			if got := pending(); got != baseline+1 {
				t.Fatalf("%s: successfully written call not tracked: pending=%d want=%d", phase, got, baseline+1)
			}
			awaited := make(chan error, 1)
			go func() { awaited <- call.Await(ctx, nil) }()
			synctest.Wait()
			select {
			case err := <-awaited:
				t.Fatalf("%s: Await returned before cancellation or response: %v", phase, err)
			default:
			}

			if phase == "deadline" {
				// Bubble time, not a scheduling sleep: expire the real context timer.
				time.Sleep(time.Second)
			} else {
				cancel()
			}
			// Drain runnable cancellation/AfterFunc work before observing absence.
			// This remains deterministic if retirement is scheduled asynchronously.
			synctest.Wait()
			select {
			case err := <-awaited:
				if !errors.Is(err, wantError) {
					t.Errorf("%s: Await error=%v want=%v", phase, err, wantError)
				}
			default:
				t.Fatalf("%s: Await did not return after its own context ended", phase)
			}
			select {
			case <-boundary.closed:
				t.Fatal("transport closed before retention assertion; EOF would mask the leak")
			default:
			}
			if got := pending(); got != baseline {
				t.Errorf("%s: pending after context cancellation=%d want baseline=%d (no peer response or transport close)", phase, got, baseline)
			}

			// Continue after RED: a late response must be harmless, and a later
			// live call must still correlate correctly on this same connection.
			late, err := NewResponse(call.ID(), "late result", nil)
			if err != nil {
				t.Fatal(err)
			}
			boundary.incoming <- late
			synctest.Wait()
			if got := pending(); got != baseline {
				t.Errorf("%s: late response left pending=%d want=%d", phase, got, baseline)
			}

			liveCtx, cancelLive := context.WithCancel(context.Background())
			defer cancelLive()
			live := connection.Call(liveCtx, "_local/live", map[string]string{"phase": phase})
			liveRequest := boundary.nextCall(t)
			if liveRequest.Method != "_local/live" || liveRequest.ID != live.ID() || live.ID() == call.ID() {
				t.Fatalf("%s: wrong live request/ID after late reply: %+v", phase, liveRequest)
			}
			type liveResult struct {
				value string
				err   error
			}
			liveDone := make(chan liveResult, 1)
			go func() {
				var value string
				err := live.Await(liveCtx, &value)
				liveDone <- liveResult{value: value, err: err}
			}()
			response, err := NewResponse(live.ID(), "live "+phase, nil)
			if err != nil {
				t.Fatal(err)
			}
			boundary.incoming <- response
			synctest.Wait()
			select {
			case got := <-liveDone:
				if got.err != nil || got.value != "live "+phase {
					t.Errorf("%s: live result=%q error=%v", phase, got.value, got.err)
				}
			default:
				t.Fatalf("%s: live call did not finish after its response", phase)
			}
			if got := pending(); got != baseline {
				t.Errorf("%s: live completion left pending=%d want=%d", phase, got, baseline)
			}
			t.Logf("%s: late reply drained and next live request succeeded before raw closure", phase)
		}
	})
}

// Interrupting a canceled call's partial write closes the stream, so the reader
// can retire that call with EOF before the writer returns. The caller must still
// see its own cancellation rather than the teardown it caused. With both outcomes
// ready, select chooses at random; repeat so a wrong choice cannot pass by chance.
func TestCanceledCallReportsContextOverStreamTeardown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for range 32 {
			boundary := &interruptedWriteBoundary{
				retentionBoundary: retentionBoundary{closed: make(chan struct{})},
				entered:           make(chan struct{}),
			}
			connection := NewConnection(context.Background(), ConnectionConfig{
				Reader: boundary,
				Writer: boundary,
				Closer: boundary,
				Bind: func(*Connection) Handler {
					return HandlerFunc(func(context.Context, *Request) (any, error) {
						return nil, ErrNotHandled
					})
				},
			})
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			go func() { result <- connection.Call(ctx, "test", nil).Await(ctx, nil) }()
			<-boundary.entered
			cancel()
			if err := <-result; !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled call returned %v, want context.Canceled", err)
			}
			if err := connection.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
		}
	})
}

// interruptedWriteBoundary reproduces a write that is interrupted by its context
// and lets the reader finish handling the resulting EOF before returning.
type interruptedWriteBoundary struct {
	retentionBoundary
	entered chan struct{}
}

func (b *interruptedWriteBoundary) Write(ctx context.Context, _ Message) error {
	close(b.entered)
	<-ctx.Done()
	_ = b.Close()
	synctest.Wait()
	return errors.Join(ctx.Err(), io.ErrClosedPipe)
}

// Only the transport boundary is controlled; the real Connection performs all
// request publication, cancellation observation, response routing and cleanup.
type retentionBoundary struct {
	incoming chan Message
	written  chan Message
	closed   chan struct{}
	once     sync.Once
}

func (b *retentionBoundary) Read(ctx context.Context) (Message, error) {
	select {
	case message := <-b.incoming:
		return message, nil
	case <-b.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *retentionBoundary) Write(ctx context.Context, message Message) error {
	select {
	case b.written <- message:
		return nil
	case <-b.closed:
		return io.ErrClosedPipe
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *retentionBoundary) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func (b *retentionBoundary) nextCall(t *testing.T) *Request {
	t.Helper()
	for {
		select {
		case message := <-b.written:
			if request, ok := message.(*Request); ok && request.IsCall() {
				return request
			}
			// Optional cancellation notifications do not define retirement.
		default:
			t.Fatal("Call returned without a successfully written request")
			return nil
		}
	}
}
