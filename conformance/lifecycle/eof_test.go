// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// checkEOF checks that a call failed because the peer's input ended: its
// error wraps io.EOF and names the method.
func checkEOF(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Error("call succeeded, want an input EOF error")
		return
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("call error %v does not wrap io.EOF", err)
	}
	if !strings.Contains(err.Error(), echoMethod) {
		t.Errorf("call error %v does not name the method", err)
	}
}

// encodingGate is call parameters whose encoding blocks until released, so a
// test can end the input while the call is still being prepared.
type encodingGate struct{ entered, release chan struct{} }

func (g encodingGate) MarshalJSON() ([]byte, error) {
	close(g.entered)
	<-g.release
	return []byte(`{}`), nil
}

// When the peer ends its input, the client settles every call: a reply sent
// immediately before EOF is still delivered, calls awaiting a reply fail with
// an EOF error, a call whose parameters were still being encoded fails the
// same way without being sent, and calls made afterwards fail with
// ErrConnectionClosed without being sent.
func TestInputEOFSettlesCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, peer := acptest.StartClient(t, nil)
		ctx := context.Background()
		// Start the calls one at a time so they reach the peer in order.
		first := callEcho(client, ctx, "first")
		expectEcho(t, peer, "first")
		second := callEcho(client, ctx, "second")
		expectEcho(t, peer, "second")
		gate := encodingGate{make(chan struct{}), make(chan struct{})}
		release := sync.OnceFunc(func() { close(gate.release) })
		defer release()
		encoding := acptest.Go(func() (json.RawMessage, error) { return client.CallExtension(ctx, echoMethod, gate) })
		acptest.Await(t, gate.entered)
		answered := callEcho(client, ctx, "answered")
		sent := expectEcho(t, peer, "answered")

		// The reply and EOF follow each other with nothing in between, so the
		// client reads EOF right behind the reply.
		replyEcho(peer, sent, "answered")
		peer.CloseInput()
		synctest.Wait()
		checkEcho(t, acptest.Await(t, answered), "answered")
		checkEOF(t, acptest.Await(t, first).Err)
		checkEOF(t, acptest.Await(t, second).Err)
		release()
		checkEOF(t, acptest.Await(t, encoding).Err)

		r := acptest.Await(t, callEcho(client, ctx, "after EOF"))
		if !errors.Is(r.Err, acp.ErrConnectionClosed) {
			t.Errorf("call after EOF error = %v, want ErrConnectionClosed", r.Err)
		}
		checkEOF(t, r.Err)
		expectNoFrames(peer)
	})
}

// A request whose input ends right after it still gets its reply: input EOF
// is a half-close, and RunAgent drains the final response before returning
// without error. The output is read one byte at a time, so the reply has to
// make repeated progress against a slow reader. The wrapper hides the
// transport's concrete type.
func TestFinalReplyAfterInputEOF(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		name := "IOTransport"
		if wrap {
			name = "wrapped transport"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				input := `{"jsonrpc":"2.0","id":1,"method":"_test/echo","params":{"message":"final"}}` + "\n"
				fromSDK, toPeer := io.Pipe()
				defer fromSDK.Close()
				output := make(chan []byte, 1)
				go func() {
					data, _ := readByteByByte(fromSDK)
					output <- data
				}()
				var transport acp.Transport = &acp.IOTransport{Reader: io.NopCloser(strings.NewReader(input)), Writer: toPeer}
				if wrap {
					transport = wrappedTransport{transport}
				}
				_, serving := serve(t, context.Background(), transport, echoMux(t))
				if err := acptest.Await(t, serving); err != nil {
					t.Errorf("RunAgent = %v, want nil after input EOF", err)
				}
				// Exactly one complete frame was written.
				acptest.EqualJSON(t, acptest.Await(t, output), `{"jsonrpc":"2.0","id":1,"result":{"result":"echo: final"}}`)
			})
		})
	}
}

// readByteByByte reads r one byte at a time until it fails, returning what it
// read and the error.
func readByteByByte(r io.Reader) ([]byte, error) {
	var data []byte
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		data = append(data, one[:n]...)
		if err != nil {
			return data, err
		}
	}
}

// Input that ends partway through a frame, with no closing newline, does not
// hang the agent. For an incomplete line RunAgent must also return without
// error; for a peer disconnecting mid-request only completion is required.
func TestEOFMidFrame(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     string
		wantClean bool
	}{
		{"incomplete line", `{"method": "test", "id": 1`, true},
		{"disconnect mid request", `{"jsonrpc":"2.0","method":"empty_method","id":1`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				agent, peer := acptest.StartAgent(t, nil)
				peer.SendBytes([]byte(tc.input))
				peer.CloseInput()
				if err := agent.Wait(t); tc.wantClean && err != nil {
					t.Errorf("RunAgent = %v, want nil", err)
				}
			})
		})
	}
}

// A peer error reply whose message and data look like a connection closing
// is just an error reply: the caller gets the peer's code, message, and data
// unchanged, it does not match ErrConnectionClosed, and the connection keeps
// working.
func TestPeerErrorsAreNotClosure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, peer := acptest.StartClient(t, nil)
		for _, tc := range []struct {
			message string
			data    map[string]any
		}{
			{"Connection terminated", map[string]any{"reason": "connection_terminated", "termination": "local_shutdown", "method": echoMethod}},
			{"Incoming transport closed", map[string]any{"reason": "incoming_transport_closed", "method": echoMethod}},
		} {
			result := callEcho(client, context.Background(), tc.message)
			sent := expectEcho(t, peer, tc.message)
			peer.ReplyError(sent.ID, jsonrpc.CodeInternalError, tc.message, tc.data)
			err := acptest.Await(t, result).Err
			var wire *jsonrpc.Error
			if !errors.As(err, &wire) || wire.Code != jsonrpc.CodeInternalError || wire.Message != tc.message {
				t.Fatalf("peer error became %v", err)
			}
			if errors.Is(err, acp.ErrConnectionClosed) {
				t.Errorf("peer error %q matches ErrConnectionClosed", tc.message)
			}
			acptest.EqualMarshaled(t, tc.data, string(wire.Data))
		}
		roundTrip(t, client, peer, "after peer errors")
	})
}

// Closing the transport in both directions under a live client, so its reads
// end and its writes fail, settles a pending call with an error, and Close
// still returns.
func TestTransportClosedUnderClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		peer, inner := acptest.NewPeer(t)
		conn := newGate(inner)
		conn.open() // no gating: the wrapper is only used to close the connection
		client, err := acp.Connect(context.Background(), conn, nil)
		if err != nil {
			t.Fatal(err)
		}
		pending := callEcho(client, context.Background(), "pending")
		expectEcho(t, peer, "pending")

		conn.abort()
		synctest.Wait()
		r, ok := poll(pending)
		if !ok {
			t.Fatal("pending call did not settle after the transport closed")
		}
		if r.Err == nil {
			t.Errorf("pending call succeeded with %s after the transport closed", r.Value)
		}
		closing := acptest.Go(func() (struct{}, error) { return struct{}{}, client.Close() })
		synctest.Wait()
		if _, ok := poll(closing); !ok {
			t.Fatal("Close did not return after the transport closed")
		}
	})
}
