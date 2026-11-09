// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package lifecycle_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// Close writes everything already accepted, in order, and then closes the
// connection, without waiting for the peer to end its input. The wrapper
// hides the transport's concrete type.
func TestCloseFlushesAcceptedOutput(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		name := "IOTransport"
		if wrap {
			name = "wrapped transport"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				peer, transport := acptest.NewPeer(t)
				if wrap {
					transport = wrappedTransport{transport}
				}
				client, err := acp.Connect(context.Background(), transport, nil)
				if err != nil {
					t.Fatal(err)
				}
				for i := range 3 {
					if err := client.NotifyExtension(context.Background(), "_test/sequence", map[string]any{"sequence": i}); err != nil {
						t.Fatal(err)
					}
				}
				closing := acptest.Go(func() (struct{}, error) { return struct{}{}, client.Close() })
				if err := acptest.Await(t, closing).Err; err != nil {
					t.Errorf("Close = %v", err)
				}
				// Output having ended shows Close also closed the connection.
				frames := drainNow(t, peer)
				if len(frames) != 3 {
					t.Fatalf("frames = %v, want the 3 notifications", frames)
				}
				for i, m := range frames {
					acptest.EqualJSON(t, m.Line, fmt.Sprintf(`{"jsonrpc":"2.0","method":"_test/sequence","params":{"sequence":%d}}`, i))
				}
			})
		})
	}
}

// closeErrWriter returns err from Close after closing the underlying writer.
type closeErrWriter struct {
	io.WriteCloser
	err error
}

func (w closeErrWriter) Close() error {
	_ = w.WriteCloser.Close()
	return w.err
}

// Close on either side waits for a write that is blocked on a slow reader, so
// the whole frame reaches the stream before it is closed, and reports the
// stream's Close error. RunAgent reports the same error. The reader starts
// only after Close has begun, and then reads one byte at a time.
func TestCloseWaitsForBlockedWrite(t *testing.T) {
	for _, tc := range []struct {
		name     string
		agent    bool
		closeErr bool
	}{
		{"agent", true, false},
		{"client", false, false},
		{"agent with close error", true, true},
		{"client with close error", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				input, inputWriter := io.Pipe() // never written; the input stays open
				fromSDK, toPeer := io.Pipe()
				var failure error
				if tc.closeErr {
					failure = errors.New("close failed")
				}
				writer := watch(closeErrWriter{toPeer, failure})
				transport := &acp.IOTransport{Reader: input, Writer: writer}

				startReading := make(chan struct{})
				startOnce := sync.OnceFunc(func() { close(startReading) })
				defer func() {
					startOnce()
					_ = inputWriter.Close()
					_ = fromSDK.Close()
				}()
				output := make(chan []byte, 1)
				go func() {
					<-startReading
					data, _ := readByteByByte(fromSDK)
					output <- data
				}()

				var notify func(context.Context, string, any) error
				var closeSDK func() error
				var serving <-chan error
				if tc.agent {
					var conn *acp.AgentConnection
					conn, serving = serve(t, context.Background(), transport, nil)
					notify, closeSDK = conn.NotifyExtension, conn.Close
				} else {
					client, err := acp.Connect(context.Background(), transport, nil)
					if err != nil {
						t.Fatal(err)
					}
					notify, closeSDK = client.NotifyExtension, client.Close
				}

				payload := strings.Repeat("x", 1024)
				sending := acptest.Go(func() (struct{}, error) {
					return struct{}{}, notify(context.Background(), "_test/probe", map[string]any{"sequence": 0, "payload": payload})
				})
				acptest.Await(t, writer.entered)
				synctest.Wait() // the write is blocked: nothing reads the pipe yet
				closing := acptest.Go(func() (struct{}, error) { return struct{}{}, closeSDK() })
				synctest.Wait()
				if r, ok := poll(closing); ok {
					t.Fatalf("Close returned %v before its blocked write finished", r.Err)
				}

				startOnce()
				if err := acptest.Await(t, sending).Err; err != nil {
					t.Errorf("blocked notification failed: %v", err)
				}
				if err := acptest.Await(t, closing).Err; !errors.Is(err, failure) {
					t.Errorf("Close = %v, want %v", err, failure)
				}
				if serving != nil {
					if err := acptest.Await(t, serving); !errors.Is(err, failure) {
						t.Errorf("RunAgent = %v, want %v", err, failure)
					}
				}
				data := acptest.Await(t, output)
				if !strings.HasSuffix(string(data), "\n") {
					t.Fatalf("output was truncated: %q", data)
				}
				acptest.EqualJSON(t, data, fmt.Sprintf(`{"jsonrpc":"2.0","method":"_test/probe","params":{"sequence":0,"payload":%q}}`, payload))
			})
		})
	}
}

// While Close waits for a write held in flight, the client keeps reading its
// input, including the final EOF, but delivers nothing more to its handler,
// and EOF does not cut the wait short. Once the write proceeds, the frame is
// sent, Close succeeds, and nothing else is written.
func TestCloseDrainKeepsReading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var delivered atomic.Int32
		mux := acp.NewExtensionMux()
		if err := acp.AddExtensionNotification(mux, "_test/late", func(context.Context, *struct{}) error {
			delivered.Add(1)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		peer, inner := acptest.NewPeer(t)
		gate := newGate(inner)
		client, err := acp.Connect(context.Background(), gate, mux)
		if err != nil {
			t.Fatal(err)
		}
		defer gate.abort()
		defer gate.open()

		sending := acptest.Go(func() (struct{}, error) {
			return struct{}{}, client.NotifyExtension(context.Background(), "_test/accepted", struct{}{})
		})
		acptest.Await(t, gate.entered)
		closing := acptest.Go(func() (struct{}, error) { return struct{}{}, client.Close() })
		synctest.Wait()

		// The peer's writes only complete if the client keeps reading.
		for range 2 {
			peer.Notify("_test/late", struct{}{})
		}
		peer.CloseInput()
		synctest.Wait()
		select {
		case <-gate.readDone:
			if !errors.Is(gate.readErr, io.EOF) {
				t.Errorf("reading ended with %v, want io.EOF", gate.readErr)
			}
		default:
			t.Error("client stopped reading input while Close was draining")
		}
		if n := delivered.Load(); n != 0 {
			t.Errorf("handler got %d notifications after Close began", n)
		}
		if r, ok := poll(closing); ok {
			t.Fatalf("Close returned %v at input EOF, before its held write was sent", r.Err)
		}

		gate.open()
		if err := acptest.Await(t, sending).Err; err != nil {
			t.Errorf("held notification failed: %v", err)
		}
		if err := acptest.Await(t, closing).Err; err != nil {
			t.Errorf("Close = %v", err)
		}
		frames := drainNow(t, peer)
		if len(frames) != 1 {
			t.Fatalf("frames = %v, want only the held notification", frames)
		}
		acptest.EqualJSON(t, frames[0].Line, `{"jsonrpc":"2.0","method":"_test/accepted","params":{}}`)
	})
}

// While Close waits for a write blocked on a single duplex stream, the client
// keeps reading from that same stream. The peer writes all its input before
// reading anything, so it deadlocks unless reading continues.
func TestCloseDrainKeepsReadingDuplex(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		local, remote := net.Pipe()
		defer remote.Close()
		defer local.Close()
		writer := watch(local)
		client, err := acp.Connect(context.Background(), &acp.IOTransport{Reader: local, Writer: writer}, nil)
		if err != nil {
			t.Fatal(err)
		}
		payload := strings.Repeat("x", 1024)
		sending := acptest.Go(func() (struct{}, error) {
			return struct{}{}, client.NotifyExtension(context.Background(), "_test/queued", map[string]any{"payload": payload})
		})
		acptest.Await(t, writer.entered)
		closing := acptest.Go(func() (struct{}, error) { return struct{}{}, client.Close() })
		synctest.Wait()

		received := acptest.Go(func() ([]byte, error) {
			for _, n := range []int{16, 1024} {
				if _, err := fmt.Fprintf(remote, `{"jsonrpc":"2.0","method":"_test/late","params":{"payload":%q}}`+"\n", strings.Repeat("x", n)); err != nil {
					return nil, err
				}
			}
			return bufio.NewReader(remote).ReadBytes('\n')
		})
		line := acptest.Value(t, received)
		acptest.EqualJSON(t, line, fmt.Sprintf(`{"jsonrpc":"2.0","method":"_test/queued","params":{"payload":%q}}`, payload))
		if err := acptest.Await(t, sending).Err; err != nil {
			t.Errorf("blocked notification failed: %v", err)
		}
		if err := acptest.Await(t, closing).Err; err != nil {
			t.Errorf("Close = %v", err)
		}
	})
}

// A read error that arrives while Close is draining a write that cannot
// proceed is not hidden behind the drain: RunAgent returns it at once.
func TestReadErrorDuringBlockedDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		peer, inner := acptest.NewPeer(t)
		gate := newGate(inner)
		conn, serving := serve(t, context.Background(), gate, nil)
		sending := acptest.Go(func() (struct{}, error) {
			return struct{}{}, conn.NotifyExtension(context.Background(), "_test/queued", map[string]any{"payload": strings.Repeat("x", 1024)})
		})
		acptest.Await(t, gate.entered)
		closing := acptest.Go(func() (struct{}, error) { return struct{}{}, conn.Close() })
		synctest.Wait()

		failure := errors.New("read failed during drain")
		peer.FailInput(failure)
		synctest.Wait()
		err, ok := poll(serving)
		if !ok {
			t.Error("read error is hidden behind the blocked drain")
		} else if !errors.Is(err, failure) {
			t.Errorf("RunAgent = %v, want the read error", err)
		}

		// Cleanup after the checks: aborting the write lets the calls return.
		gate.abort()
		acptest.Await(t, sending)
		acptest.Await(t, closing)
	})
}
