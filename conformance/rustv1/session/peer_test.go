// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
// Upstream revision: 050cd94c081056ace37a7b89abfed7f6220b47eb.
package session_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

const operationTimeout = 3 * time.Second

type wireError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type wireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *wireError      `json:"error,omitempty"`
}

// The peer uses the standard JSON codec, independently of the SDK's message
// encoder/decoder. Retained pipe endpoints force cleanup even if SDK Close waits.
type wirePeer struct {
	conn   net.Conn
	reader *bufio.Reader
}

func clientPeer(t *testing.T, handler any) (context.Context, *acp.Client, *wirePeer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	local, remote := net.Pipe()
	client, err := acp.Connect(ctx, &acp.IOTransport{Reader: local, Writer: local}, handler)
	if err != nil {
		local.Close()
		remote.Close()
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		local.Close()
		remote.Close()
		cancel()
		done := make(chan struct{})
		go func() { client.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(operationTimeout):
			t.Error("client cleanup did not finish after forcing transport closure")
		}
	})
	return ctx, client, &wirePeer{conn: remote, reader: bufio.NewReader(remote)}
}

func agentPeer(t *testing.T, handler any) *wirePeer {
	t.Helper()
	return agentFactoryPeer(t, func(*acp.AgentConnection) any { return handler })
}

func agentFactoryPeer(t *testing.T, factory acp.AgentFactory) *wirePeer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	local, remote := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- acp.RunAgent(ctx, &acp.IOTransport{Reader: local, Writer: local}, factory)
	}()
	t.Cleanup(func() {
		local.Close()
		remote.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(operationTimeout):
			t.Error("agent cleanup did not finish after forcing transport closure")
		}
	})
	return &wirePeer{conn: remote, reader: bufio.NewReader(remote)}
}

func (p *wirePeer) read(t *testing.T) wireMessage {
	t.Helper()
	p.conn.SetReadDeadline(time.Now().Add(operationTimeout))
	line, err := p.reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("peer reading message: %v", err)
	}
	var message wireMessage
	if err := json.Unmarshal(line, &message); err != nil {
		t.Fatalf("peer decoding %q: %v", line, err)
	}
	return message
}

func (p *wirePeer) send(t *testing.T, message any) {
	t.Helper()
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	p.sendRaw(t, append(data, '\n'))
}

func (p *wirePeer) sendRaw(t *testing.T, data []byte) {
	t.Helper()
	p.conn.SetWriteDeadline(time.Now().Add(operationTimeout))
	if _, err := p.conn.Write(data); err != nil {
		t.Fatalf("peer writing message: %v", err)
	}
}

func (p *wirePeer) result(t *testing.T, request wireMessage, result any) {
	t.Helper()
	p.send(t, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
}

func (p *wirePeer) failure(t *testing.T, request wireMessage, code int, message string, data any) {
	t.Helper()
	p.send(t, map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": code, "message": message, "data": data}})
}

func (p *wirePeer) update(t *testing.T, session, text string) {
	t.Helper()
	p.send(t, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
		"sessionId": session, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}},
	}})
}

type callResult[T any] struct {
	value T
	err   error
}

func startCall[T any](call func() (T, error)) <-chan callResult[T] {
	result := make(chan callResult[T], 1)
	go func() {
		value, err := call()
		result <- callResult[T]{value: value, err: err}
	}()
	return result
}

func await[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(operationTimeout):
		t.Fatal("timed out waiting for public operation")
		var zero T
		return zero
	}
}

func valueOf[T any](t *testing.T, result <-chan callResult[T]) T {
	t.Helper()
	got := await(t, result)
	if got.err != nil {
		t.Fatal(got.err)
	}
	return got.value
}

func prepareClientSession(t *testing.T, ctx context.Context, client *acp.Client, peer *wirePeer, capabilities *acp.ClientCapabilities) {
	t.Helper()
	initialized := startCall(func() (*acp.InitializeResponse, error) {
		return client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1, ClientCapabilities: capabilities})
	})
	request := peer.read(t)
	if request.Method != "initialize" {
		t.Fatalf("expected initialize, got %q", request.Method)
	}
	peer.result(t, request, map[string]any{"protocolVersion": 1})
	if selected := valueOf(t, initialized).ProtocolVersion; selected != 1 {
		t.Fatalf("selected protocol version = %d, want v1", selected)
	}
	created := startCall(func() (*acp.NewSessionResponse, error) {
		return client.SessionNew(ctx, &acp.NewSessionRequest{Cwd: "/tmp", McpServers: []acp.McpServer{}})
	})
	peer.result(t, peer.read(t), map[string]any{"sessionId": "s"})
	if valueOf(t, created).SessionID != "s" {
		t.Fatal("wrong created session ID")
	}
}

type updateHandler struct{ updates chan acp.SessionNotification }

func (h *updateHandler) SessionUpdate(_ context.Context, update *acp.SessionNotification) error {
	h.updates <- *update // Buffered so callback completion does not depend on test consumption.
	return nil
}

func newUpdateHandler() *updateHandler {
	return &updateHandler{updates: make(chan acp.SessionNotification, 16)}
}

func updateText(t *testing.T, update acp.SessionNotification) string {
	t.Helper()
	content, ok := update.Update.Content.(acp.ContentBlock)
	if !ok {
		t.Fatalf("update content type = %T, want ContentBlock", update.Update.Content)
	}
	return content.Text
}

func assertJSON(t *testing.T, value any, expected string) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decode := func(data []byte) any {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatalf("expected exactly one JSON value, trailing decode error = %v", err)
		}
		return value
	}
	got, want := decode(data), decode([]byte(expected))
	// Comparing canonical JSON keeps Rust ownership and Go named-string
	// differences out of wire-value preservation assertions.
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("JSON = %s, want %s", gotJSON, wantJSON)
	}
}
