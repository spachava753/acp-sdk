// Copyright 2026 ACP Go SDK contributors.
// Rust SDK semantic ports, Apache-2.0; provenance and policy labels: mapping.json.
package lifecycle_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust: tests/connection_termination.rs:23, :136 and
// tests/jsonrpc_transport_close.rs:1679: clean_eof_terminates_with_incoming_closed,
// transport_read_error_is_a_transport_error, and
// incoming_read_error_is_not_reported_as_clean_eof. Go exposes RunAgent's error rather
// than Rust's ConnectionTermination enum: clean EOF and real read failure must
// remain distinguishable. No invented foreground/driver API is supplied.
func TestTransportTerminationPreservesReadErrors(t *testing.T) {
	failure := errors.New("read failed")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"clean-EOF", io.EOF},
		{"read-error", failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newFrames()
				defer func() { _ = p.Close(); synctest.Wait() }()
				p.endInput(tc.err)
				_, done := startAgent(t, p, nil)
				synctest.Wait()
				err := receive(t, done)
				if tc.err == io.EOF {
					if err != nil {
						t.Errorf("clean EOF returned an error: %v", err)
					}
				} else if !errors.Is(err, failure) {
					t.Errorf("read failure was lost: %v", err)
				}
			})
		})
	}
}

// Rust: tests/jsonrpc_transport_close.rs:1444,
// incoming_eof_fails_every_pending_request, including BOTH outstanding requests.
// Also tests/connection_termination.rs:206,
// pending_requests_keep_the_eof_error_after_clean_eof, preserved EOF error property;
// its enum/foreground observations have no public Go counterpart (mapping.json).
func TestIncomingEOFFailsEveryPendingRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, nil)
		defer cleanupClient(p, c)
		first := call(c, context.Background(), struct{}{})
		second := call(c, context.Background(), struct{}{})
		outgoingRequest(t, p)
		outgoingRequest(t, p)
		p.endInput(io.EOF)
		synctest.Wait()
		for _, done := range []<-chan callResult{first, second} {
			requireIncomingEOF(t, receive(t, done).err)
		}
	})
}

// Rust: tests/jsonrpc_transport_close.rs:1535,
// response_buffered_before_eof_is_delivered. The peer queues the response and
// then EOF without allowing test cleanup to manufacture the ordering.
func TestBufferedResponsePrecedesEOF(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, nil)
		defer cleanupClient(p, c)
		pending := call(c, context.Background(), struct{}{})
		req := outgoingRequest(t, p)
		p.push(t, reply(req.ID, map[string]any{"status": "received"}))
		p.endInput(io.EOF)
		synctest.Wait()
		result := receive(t, pending)
		if result.err != nil || string(result.value) != `{"status":"received"}` {
			t.Errorf("buffered response was replaced by EOF: %s, %v", result.value, result.err)
		}
	})
}

// Rust: tests/jsonrpc_transport_close.rs:1656,
// request_created_after_incoming_eof_fails_immediately. Go's public closed
// connection sentinel replaces Rust's incoming-transport-closed classifier.
func TestRequestAfterEOFFailsWithoutSending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, nil)
		defer cleanupClient(p, c)
		p.endInput(io.EOF)
		synctest.Wait()
		result := receive(t, call(c, context.Background(), struct{}{}))
		if !errors.Is(result.err, acp.ErrConnectionClosed) {
			t.Errorf("post-EOF request error = %v, want closed connection", result.err)
		}
		requireIncomingEOF(t, result.err)
		synctest.Wait()
		p.noOutput(t)
	})
}

type encodingGate struct {
	entered chan struct{}
	release chan struct{}
}

func (p encodingGate) MarshalJSON() ([]byte, error) {
	close(p.entered)
	<-p.release
	return []byte(`{}`), nil
}

// Rust: tests/jsonrpc_transport_close.rs:1359,
// request_finishing_conversion_after_eof_keeps_the_eof_cause. User-defined
// MarshalJSON is Go's public request-conversion boundary; no private queue,
// outgoing actor, or pending-reply state is inspected.
func TestRequestConversionFinishingAfterEOFKeepsCause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, nil)
		gate := encodingGate{make(chan struct{}), make(chan struct{})}
		released := false
		defer func() {
			if !released {
				close(gate.release)
			}
			cleanupClient(p, c)
		}()
		pending := call(c, context.Background(), gate)
		receive(t, gate.entered)
		p.endInput(io.EOF)
		synctest.Wait()
		close(gate.release)
		released = true
		synctest.Wait()
		result := receive(t, pending)
		requireIncomingEOF(t, result.err)
		p.noOutput(t)
	})
}

type bufferCloser struct{ bytes.Buffer }

func (*bufferCloser) Close() error { return nil }

type delegatingTransport struct{ acp.Transport }

// Rust: tests/jsonrpc_transport_close.rs:606, :617, :748:
// connect_to_flushes_response_queued_before_incoming_eof,
// channel_peer_receives_final_response_after_write_half_closes, and
// wrapped_byte_streams_flush_response_queued_before_incoming_eof. All require the final
// response after input write-half-close, including ByteStreams and a delegating
// wrapper. Keep the output alive; this is NOT the old audit's full-disconnect
// cancellation expectation. The method name alone is adapted to Go extensions.
func TestFinalResponseSurvivesInputHalfClose(t *testing.T) {
	for _, mode := range []string{"channel", "byte-stream", "wrapped-byte-stream"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				id := 1
				if mode == "channel" {
					id = 40 // The raw Channel case uses40; byte-stream cases use1.
				}
				frame := request(id, probeMethod, struct{}{})
				var frames *frameTransport
				var physicalOutput <-chan []byte
				var transport acp.Transport
				if mode == "channel" {
					frames = newFrames()
					frames.push(t, frame)
					frames.endInput(io.EOF)
					transport = frames
					defer func() { _ = frames.Close(); synctest.Wait() }()
				} else {
					data, err := json.Marshal(frame)
					if err != nil {
						t.Fatal(err)
					}
					reader, writer := io.Pipe()
					received := make(chan []byte, 1)
					physicalOutput = received
					// One-byte reads force the physical response write to make
					// repeated progress, like Rust's duplex(1) output buffer.
					go func() {
						var output bytes.Buffer
						one := make([]byte, 1)
						for {
							n, err := reader.Read(one)
							output.Write(one[:n])
							if err != nil {
								received <- output.Bytes()
								return
							}
						}
					}()
					defer func() { _ = reader.Close(); _ = writer.Close(); synctest.Wait() }()
					transport = &acp.IOTransport{Reader: io.NopCloser(bytes.NewReader(append(data, '\n'))), Writer: writer}
					if mode == "wrapped-byte-stream" {
						transport = delegatingTransport{transport}
					}
				}
				mux := acp.NewExtensionMux()
				if err := acp.AddExtensionRequest(mux, probeMethod, func(context.Context, *struct{}) (*struct {
					Status string `json:"status"`
				}, error) {
					return &struct {
						Status string `json:"status"`
					}{Status: "received"}, nil
				}); err != nil {
					t.Fatal(err)
				}
				_, done := startAgent(t, transport, mux)
				synctest.Wait()
				if err := receive(t, done); err != nil {
					t.Errorf("half-close failed instead of draining final response: %v", err)
				}
				var message jsonrpc.Message
				if frames != nil {
					select {
					case message = <-frames.output:
					default:
						t.Fatal("input half-close discarded the final response")
					}
				} else {
					data := receive(t, physicalOutput)
					var err error
					message, err = jsonrpc.DecodeMessage(bytes.TrimSpace(data))
					if err != nil || message == nil {
						t.Fatalf("incomplete/missing final physical response %q: %v", data, err)
					}
				}
				response, ok := message.(*jsonrpc.Response)
				if !ok || response.ID.Raw() != int64(id) {
					t.Fatalf("final response = %#v", message)
				}
				success(t, response, `{"status":"received"}`)
				if frames != nil {
					frames.noOutput(t)
				}
			})
		})
	}
}

// Rust: tests/jsonrpc_transport_close.rs:1471,
// byte_stream_eof_fails_an_in_flight_request. Real independent byte pipes are
// retained for cleanup; observing the full request precedes peer output EOF.
func TestByteStreamEOFFailsInflightRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		input, peerOutput := io.Pipe()
		peerInput, output := io.Pipe()
		transport := &acp.IOTransport{Reader: input, Writer: output}
		raw, err := transport.Connect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		c, err := acp.Connect(context.Background(), fixedTransport{raw}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = peerOutput.Close()
			_ = peerInput.Close()
			_ = raw.Close()
			closed := make(chan struct{})
			go func() { _ = c.Close(); close(closed) }()
			synctest.Wait()
			receive(t, closed)
		}()
		pending := call(c, context.Background(), struct{}{})
		line := make(chan []byte, 1)
		go func() {
			var b bytes.Buffer
			one := make([]byte, 1)
			for {
				n, err := peerInput.Read(one)
				if n != 0 {
					b.Write(one)
				}
				if (n != 0 && one[0] == '\n') || err != nil {
					line <- b.Bytes()
					return
				}
			}
		}()
		if data := receive(t, line); !bytes.Contains(data, []byte(probeMethod)) {
			t.Fatalf("peer did not receive the request: %q", data)
		}
		_ = peerOutput.Close()
		synctest.Wait()
		requireIncomingEOF(t, receive(t, pending).err)
	})
}

// Rust: tests/jsonrpc_transport_close.rs::assert_connection_closed checks BOTH
// the incoming-close classification and data.method. Go local errors have no
// Rust reason-data wrapper: use their native error chain and method context,
// rather than asserting Rust's literal message or manufacturing a wire error.
func requireIncomingEOF(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Error("pending request succeeded instead of reporting incoming EOF")
		return
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("request error lost its incoming EOF cause: %v", err)
	}
	if !strings.Contains(err.Error(), probeMethod) {
		t.Errorf("incoming-close error lost its request method: %v", err)
	}
}

// Rust: tests/jsonrpc_transport_close.rs:680,
// transport_channel_keeps_read_half_open_after_write_half_closes. Go exposes
// no Connection.CloseWrite/Channel.tx drop, so close the retained physical
// io.WriteCloser instead. The public IOTransport read-half/reverse-response
// behavior is tested; automatic SDK output-half ownership remains an absent
// subcase, not a feature reimplemented in the fixture.
func TestTransportReadHalfSurvivesPhysicalWriteHalfClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		input, peerOutput := io.Pipe()
		peerInput, output := io.Pipe()
		raw, err := (&acp.IOTransport{Reader: input, Writer: output}).Connect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = peerInput.Close()
			_ = peerOutput.Close()
			_ = raw.Close()
			synctest.Wait()
		}()
		peerDone := make(chan error, 1)
		go func() {
			reader := bufio.NewReader(peerInput)
			line, err := reader.ReadBytes('\n')
			if err != nil {
				peerDone <- err
				return
			}
			message, err := jsonrpc.DecodeMessage(line)
			req, ok := message.(*jsonrpc.Request)
			if err != nil || !ok || !req.IsCall() || req.ID.Raw() != int64(41) {
				peerDone <- fmt.Errorf("peer did not receive request41: %#v, %v", message, err)
				return
			}
			// The reverse response is sent ONLY after observing physical EOF
			// from the SDK write half, so buffering cannot conceal early close.
			if extra, err := reader.ReadBytes('\n'); len(extra) != 0 || !errors.Is(err, io.EOF) {
				peerDone <- fmt.Errorf("SDK output did not half-close: %q, %v", extra, err)
				return
			}
			_, err = fmt.Fprintln(peerOutput, `{"jsonrpc":"2.0","id":41,"result":{"status":"received"}}`)
			_ = peerOutput.Close()
			peerDone <- err
		}()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		message, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":41,"method":"_rust/lifecycle","params":{}}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := raw.Write(ctx, message); err != nil {
			t.Fatal(err)
		}
		_ = output.Close() // Not raw.Close: its independent input remains live.
		message, err = raw.Read(ctx)
		if err != nil {
			t.Fatalf("read half lost final reverse response: %v", err)
		}
		response, ok := message.(*jsonrpc.Response)
		if !ok || response.ID.Raw() != int64(41) {
			t.Fatalf("reverse response = %#v", message)
		}
		success(t, response, `{"status":"received"}`)
		if err := receive(t, peerDone); err != nil {
			t.Error(err)
		}
		if _, err := raw.Read(ctx); !errors.Is(err, io.EOF) {
			t.Errorf("input did not finish after the reverse response: %v", err)
		}
	})
}

type fixedTransport struct{ acp.Connection }

func (p fixedTransport) Connect(context.Context) (acp.Connection, error) { return p.Connection, nil }

// Rust: tests/jsonrpc_edge_cases.rs::test_server_shutdown. The original allows
// success OR failure depending on abort timing; only completion is required.
func TestServerShutdownUnblocksPendingCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, nil)
		defer cleanupClient(p, c)
		pending := call(c, context.Background(), struct{}{})
		outgoingRequest(t, p)
		_ = p.Close()
		synctest.Wait()
		_ = receive(t, pending)
	})
}

// Rust: tests/jsonrpc_error_handling.rs::test_incomplete_line and
// tests/jsonrpc_edge_cases.rs::test_client_disconnect. No newline is supplied.
// Only test_incomplete_line requires a clean successful result; disconnect's
// weaker no-hang oracle is retained, not promoted to an invented parse policy.
func TestEOFMidMessage(t *testing.T) {
	for _, tc := range []struct {
		name        string
		input       string
		mustBeClean bool
	}{
		{"incomplete-line", `{"method": "test", "id": 1`, true},
		{"client-disconnect", `{"jsonrpc":"2.0","method":"empty_method","id":1`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				output := &bufferCloser{}
				transport := &acp.IOTransport{Reader: io.NopCloser(strings.NewReader(tc.input)), Writer: output}
				_, done := startAgent(t, transport, nil)
				synctest.Wait()
				if err := receive(t, done); tc.mustBeClean && err != nil {
					t.Errorf("EOF mid-message should finish cleanly: %v", err)
				}
			})
		})
	}
}

// Rust: tests/connection_termination.rs:262,
// peer_errors_with_termination_reasons_do_not_match; BOTH reason payloads.
// Go has no universal termination event, so test its public closure classifier
// and unchanged structured peer error, then prove the connection remains usable.
func TestPeerTerminationLookingErrorsAreNotLocalClosure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, nil)
		defer cleanupClient(p, c)
		for _, tc := range []struct {
			message, reason, termination string
		}{
			{"Connection terminated", "connection_terminated", "local_shutdown"},
			{"Incoming transport closed", "incoming_transport_closed", ""},
		} {
			pending := call(c, context.Background(), struct{}{})
			req := outgoingRequest(t, p)
			data := map[string]any{"reason": tc.reason, "method": probeMethod}
			if tc.termination != "" {
				data["termination"] = tc.termination
			}
			p.push(t, map[string]any{"jsonrpc": "2.0", "id": req.ID.Raw(), "error": map[string]any{
				"code": jsonrpc.CodeInternalError, "message": tc.message, "data": data,
			}})
			synctest.Wait()
			result := receive(t, pending)
			var wire *jsonrpc.Error
			if !errors.As(result.err, &wire) || wire.Code != jsonrpc.CodeInternalError || wire.Message != tc.message {
				t.Fatalf("peer error changed: %v", result.err)
			}
			if errors.Is(result.err, acp.ErrConnectionClosed) || errors.Is(wire, acp.ErrConnectionClosed) {
				t.Error("peer-provided termination reason was treated as a local close")
			}
			var got map[string]any
			if err := json.Unmarshal(wire.Data, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, data) {
				t.Errorf("peer error data changed: %s", wire.Data)
			}
		}
		barrier := call(c, context.Background(), struct{}{})
		req := outgoingRequest(t, p)
		p.push(t, reply(req.ID, struct{}{}))
		synctest.Wait()
		if result := receive(t, barrier); result.err != nil {
			t.Errorf("peer termination-looking error closed connection: %v", result.err)
		}
	})
}
