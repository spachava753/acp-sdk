// SPDX-License-Identifier: Apache-2.0
// Semantic ports of agentclientprotocol/rust-sdk tests at
// 050cd94c081056ace37a7b89abfed7f6220b47eb. See mapping.json for source cases.
package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

func ptr[T any](v T) *T { return &v }

func encode(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	canonical := func(data []byte) []byte {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return encode(t, value)
	}
	if !bytes.Equal(canonical(got), canonical([]byte(want))) {
		t.Errorf("JSON mismatch\n got: %s\nwant: %s", got, want)
	}
}

func roundTrip(t *testing.T, raw string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		t.Fatalf("decode %T: %v", target, err)
	}
	assertJSON(t, encode(t, target), raw)
}

// The endpoints are opened before starting the SDK so cleanup retains both raw
// closers even when a failing call never returns or graceful Close cannot drain.
type openedTransport struct{ conn acp.Connection }

func (t openedTransport) Connect(context.Context) (acp.Connection, error) { return t.conn, nil }

func endpoints(t *testing.T) (context.Context, acp.Connection, acp.Connection) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	ct, pt := acp.NewInMemoryTransports()
	sdk, err := ct.Connect(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	peer, err := pt.Connect(ctx)
	if err != nil {
		sdk.Close()
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close(); sdk.Close(); cancel() })
	return ctx, sdk, peer
}

func bounded(t *testing.T, done <-chan error, label string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("%s: %v", label, err)
		}
	case <-time.After(time.Second):
		t.Errorf("timed out waiting for %s", label)
	}
}

func rawClient(t *testing.T, handler any) (context.Context, *acp.Client, acp.Connection) {
	t.Helper()
	ctx, sdk, peer := endpoints(t)
	client, err := acp.Connect(ctx, openedTransport{sdk}, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		peer.Close()
		sdk.Close()
		done := make(chan error, 1)
		go func() { done <- client.Close() }()
		bounded(t, done, "client shutdown")
	})
	return ctx, client, peer
}

func rawAgent(t *testing.T, handler any) (context.Context, *acp.AgentConnection, acp.Connection) {
	t.Helper()
	ctx, sdk, peer := endpoints(t)
	ready := make(chan *acp.AgentConnection, 1)
	done := make(chan error, 1)
	go func() {
		done <- acp.RunAgent(ctx, openedTransport{sdk}, func(conn *acp.AgentConnection) any {
			ready <- conn
			return handler
		})
	}()
	t.Cleanup(func() { peer.Close(); sdk.Close(); bounded(t, done, "agent shutdown") })
	select {
	case conn := <-ready:
		return ctx, conn, peer
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return nil, nil, nil
	}
}

func readRequest(t *testing.T, ctx context.Context, peer acp.Connection) *jsonrpc.Request {
	t.Helper()
	msg, err := peer.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	req, ok := msg.(*jsonrpc.Request)
	if !ok {
		t.Fatalf("got %T, want JSON-RPC request/notification", msg)
	}
	return req
}

type outcome struct {
	value any
	err   error
}

func exchange(t *testing.T, ctx context.Context, peer acp.Connection, method, params, result string, call func() (any, error)) any {
	t.Helper()
	done := make(chan outcome, 1)
	go func() { value, err := call(); done <- outcome{value, err} }()
	req := readRequest(t, ctx, peer)
	if req.Method != method || !req.ID.IsValid() {
		t.Fatalf("outgoing request = %q (id=%v), want %q with ID", req.Method, req.ID.Raw(), method)
	}
	assertJSON(t, req.Params, params)
	if err := peer.Write(ctx, &jsonrpc.Response{ID: req.ID, Result: json.RawMessage(result)}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got.value
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return nil
	}
}

func notification(t *testing.T, ctx context.Context, peer acp.Connection, method, params string, send func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- send() }()
	req := readRequest(t, ctx, peer)
	if req.Method != method || req.ID.IsValid() {
		t.Fatalf("notification = %q (id=%v), want %q without ID", req.Method, req.ID.Raw(), method)
	}
	assertJSON(t, req.Params, params)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func requestToAgent(t *testing.T, ctx context.Context, peer acp.Connection, method, params string) *jsonrpc.Response {
	t.Helper()
	id, err := jsonrpc.MakeID("schema-port")
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.Write(ctx, &jsonrpc.Request{ID: id, Method: method, Params: json.RawMessage(params)}); err != nil {
		t.Fatal(err)
	}
	msg, err := peer.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	res, ok := msg.(*jsonrpc.Response)
	if !ok || res.ID.Raw() != "schema-port" {
		t.Fatalf("response = %#v, want matching response", msg)
	}
	return res
}
