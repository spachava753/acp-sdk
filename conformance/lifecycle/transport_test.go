// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package lifecycle_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// halfStream is a stream an IOTransport connection runs over, with separate
// ways to end each direction.
type halfStream struct {
	transport   *acp.IOTransport
	closeOutput func() error // ends the SDK's output only
	peerIn      io.Reader    // what the SDK writes
	peerOut     io.Writer    // what the SDK reads
	closePeer   func() error // ends the SDK's input only
	cleanup     func()
}

func pipeStream(*testing.T) halfStream {
	input, peerOut := io.Pipe()
	peerIn, output := io.Pipe()
	return halfStream{
		transport:   &acp.IOTransport{Reader: input, Writer: output},
		closeOutput: output.Close,
		peerIn:      peerIn,
		peerOut:     peerOut,
		closePeer:   peerOut.Close,
		cleanup: func() {
			for _, c := range []io.Closer{input, peerOut, peerIn, output} {
				_ = c.Close()
			}
		},
	}
}

// tcpStream returns one loopback TCP socket used for both directions, so
// ending the output means shutting down its write half. Its deadlines only
// bound a failing test.
func tcpStream(t *testing.T) halfStream {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	deadline := time.Now().Add(acptest.Timeout)
	if err := listener.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	remote, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	local, err := listener.AcceptTCP()
	if err != nil {
		_ = remote.Close()
		t.Fatal(err)
	}
	for _, c := range []*net.TCPConn{local, remote} {
		if err := c.SetDeadline(deadline); err != nil {
			t.Fatal(err)
		}
	}
	return halfStream{
		transport:   &acp.IOTransport{Reader: local, Writer: local},
		closeOutput: local.CloseWrite,
		peerIn:      remote,
		peerOut:     remote,
		closePeer:   remote.CloseWrite,
		cleanup:     func() { _ = local.Close(); _ = remote.Close() },
	}
}

// An IOTransport connection keeps reading after its output is closed: the
// peer sees EOF on the SDK's output, and a reply or notification it sends
// afterwards still arrives, followed by EOF once the peer ends its side. The
// peer sends only after seeing the output EOF, so buffering cannot hide an
// output that closed the whole stream.
func TestReadOutlivesClosedOutput(t *testing.T) {
	for _, setup := range []struct {
		name string
		open func(*testing.T) halfStream
	}{
		{"pipes", pipeStream},
		{"tcp socket", tcpStream},
	} {
		t.Run(setup.name, func(t *testing.T) {
			for _, reply := range []bool{true, false} {
				name := "notification after output EOF"
				if reply {
					name = "reply after output EOF"
				}
				t.Run(name, func(t *testing.T) {
					s := setup.open(t)
					ctx, cancel := context.WithTimeout(context.Background(), acptest.Timeout)
					defer cancel()
					raw, err := s.transport.Connect(ctx)
					if err != nil {
						t.Fatal(err)
					}
					peerDone := acptest.Go(func() (struct{}, error) {
						r := bufio.NewReader(s.peerIn)
						frame := `{"jsonrpc":"2.0","method":"_test/after-output-eof"}`
						if reply {
							line, err := r.ReadBytes('\n')
							if err != nil {
								return struct{}{}, err
							}
							var request struct {
								ID     json.RawMessage `json:"id"`
								Method string          `json:"method"`
							}
							if err := json.Unmarshal(line, &request); err != nil || string(request.ID) != "41" || request.Method == "" {
								return struct{}{}, fmt.Errorf("peer got %q, want request 41", line)
							}
							frame = `{"jsonrpc":"2.0","id":41,"result":{"status":"final"}}`
						}
						if extra, err := r.ReadBytes('\n'); len(extra) != 0 || !errors.Is(err, io.EOF) {
							return struct{}{}, fmt.Errorf("SDK output did not end: %q, %v", extra, err)
						}
						if _, err := fmt.Fprintln(s.peerOut, frame); err != nil {
							return struct{}{}, err
						}
						return struct{}{}, s.closePeer()
					})
					joined := false
					defer func() {
						s.cleanup()
						_ = raw.Close()
						if !joined {
							acptest.Await(t, peerDone)
						}
					}()

					want := `{"jsonrpc":"2.0","method":"_test/after-output-eof"}`
					if reply {
						id, err := jsonrpc.MakeID(int64(41))
						if err != nil {
							t.Fatal(err)
						}
						if err := raw.Write(ctx, &jsonrpc.Request{ID: id, Method: echoMethod, Params: json.RawMessage(`{}`)}); err != nil {
							t.Fatal(err)
						}
						want = `{"jsonrpc":"2.0","id":41,"result":{"status":"final"}}`
					}
					if err := s.closeOutput(); err != nil {
						t.Fatal(err)
					}
					m, err := raw.Read(ctx)
					if err != nil {
						t.Fatalf("read after output closed: %v", err)
					}
					data, err := jsonrpc.EncodeMessage(m)
					if err != nil {
						t.Fatal(err)
					}
					acptest.EqualJSON(t, data, want)
					if _, err := raw.Read(ctx); !errors.Is(err, io.EOF) {
						t.Errorf("read after the peer ended its side = %v, want io.EOF", err)
					}
					joined = true
					acptest.Value(t, peerDone)
				})
			}
		})
	}
}

// Ending a call's context while its frame is partly written interrupts the
// write itself, not just the wait: the call returns the context's error and
// the stream's Write returns too. Reusing the stream afterwards is not
// required, since it holds a partial frame.
func TestActiveWriteHonorsContext(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				local, remote := net.Pipe()
				writer := watch(local)
				client, err := acp.Connect(context.Background(), &acp.IOTransport{Reader: local, Writer: writer}, nil)
				if err != nil {
					t.Fatal(err)
				}
				// Cleanup after the checks: closing the stream releases any
				// write still blocked, so Close cannot pass a check above.
				defer func() { _ = local.Close(); _ = remote.Close(); _ = client.Close() }()
				ctx, cancel := context.WithCancel(context.Background())
				if deadline {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				}
				defer cancel()

				calling := acptest.Go(func() (*acp.InitializeResponse, error) {
					return client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1})
				})
				acptest.Await(t, writer.entered)
				// Take one byte so the frame is partly written, then leave the
				// rest of the write blocked.
				one := make([]byte, 1)
				if n, err := remote.Read(one); err != nil || n != 1 || one[0] != '{' {
					t.Fatalf("first byte = %q, %v", one[:n], err)
				}
				synctest.Wait()
				if deadline {
					<-ctx.Done()
				} else {
					cancel()
				}
				synctest.Wait()

				if r, ok := poll(calling); !ok {
					t.Error("call stayed blocked in Write after its context ended")
				} else if !errors.Is(r.Err, ctx.Err()) {
					t.Errorf("call error = %v, want %v", r.Err, ctx.Err())
				}
				select {
				case <-writer.exited:
				default:
					t.Error("stream Write stayed blocked after the call's context ended")
				}
			})
		})
	}
}

// cancelDuringEncoding is a response error whose formatting cancels the write's
// context, placing the cancellation after Write first checks it.
type cancelDuringEncoding struct {
	cancel  context.CancelFunc // nil leaves the context alone
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

// A write whose context ends while another write holds the stream returns the
// context's error without waiting, is never sent, and leaves the stream usable.
// In one case the context is canceled while the frame is being encoded; in the
// other the context is still live after encoding, and its deadline passes
// while the write waits behind the first.
func TestCanceledQueuedWrite(t *testing.T) {
	for _, cancelEncoding := range []bool{true, false} {
		name := "deadline while waiting"
		if cancelEncoding {
			name = "cancel during encoding"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				local, remote := net.Pipe()
				writer := watch(local)
				raw, err := (&acp.IOTransport{Reader: local, Writer: writer}).Connect(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = local.Close(); _ = remote.Close(); _ = raw.Close() }()
				notify := func(method string) *jsonrpc.Request {
					return &jsonrpc.Request{Method: method, Params: json.RawMessage(`{}`)}
				}

				// Nobody reads yet, so the first write holds the stream.
				first := acptest.Go(func() (struct{}, error) { return struct{}{}, raw.Write(context.Background(), notify("_test/first")) })
				acptest.Await(t, writer.entered)

				ctx, cancel := context.WithCancel(context.Background())
				if !cancelEncoding {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				}
				defer cancel()
				encoding := &cancelDuringEncoding{encoded: make(chan struct{})}
				if cancelEncoding {
					encoding.cancel = cancel
				}
				id, err := jsonrpc.MakeID("queued-canceled")
				if err != nil {
					t.Fatal(err)
				}
				second := acptest.Go(func() (struct{}, error) {
					return struct{}{}, raw.Write(ctx, &jsonrpc.Response{ID: id, Error: encoding})
				})
				acptest.Await(t, encoding.encoded)
				if !cancelEncoding {
					if ctx.Err() != nil {
						t.Fatal("context ended before encoding finished")
					}
					<-ctx.Done()
				}
				synctest.Wait()
				if r, ok := poll(second); !ok {
					t.Error("canceled write waited behind the write holding the stream")
				} else if !errors.Is(r.Err, ctx.Err()) {
					t.Errorf("canceled write = %v, want %v", r.Err, ctx.Err())
				}

				// Reading releases the first write; a later write still works,
				// and the canceled frame never appears between them.
				received := acptest.Go(func() ([][]byte, error) {
					r := bufio.NewReader(remote)
					var lines [][]byte
					for range 4 {
						line, err := r.ReadBytes('\n')
						if err != nil {
							return lines, err
						}
						lines = append(lines, line)
						var m struct{ Method string }
						if json.Unmarshal(line, &m) == nil && m.Method == "_test/after" {
							return lines, nil
						}
					}
					return lines, fmt.Errorf("no _test/after frame in %q", lines)
				})
				if err := acptest.Await(t, first).Err; err != nil {
					t.Fatalf("first write failed: %v", err)
				}
				if err := raw.Write(context.Background(), notify("_test/after")); err != nil {
					t.Fatalf("write after the canceled one failed: %v", err)
				}
				lines := acptest.Value(t, received)
				if len(lines) != 2 {
					t.Fatalf("peer got %q, want only the first and later frames", lines)
				}
				acptest.EqualJSON(t, lines[0], `{"jsonrpc":"2.0","method":"_test/first","params":{}}`)
				acptest.EqualJSON(t, lines[1], `{"jsonrpc":"2.0","method":"_test/after","params":{}}`)
			})
		})
	}
}
