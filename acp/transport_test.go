// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/jsonrpc"
)

type testRWC struct {
	io.Reader
	io.Writer
}

func (r testRWC) Close() error { return nil }

func TestIOConnReadsNewlineDelimitedMessages(t *testing.T) {
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}` + "\n" +
		`{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"s1"}}` + "\n")
	conn := newIOConn(testRWC{Reader: input, Writer: io.Discard})
	defer conn.Close()

	first := readRequest(t, conn)
	if first.Method != MethodInitialize || !first.ID.IsValid() {
		t.Fatalf("first request = %#v", first)
	}
	second := readRequest(t, conn)
	if second.Method != MethodSessionCancel || second.ID.IsValid() {
		t.Fatalf("second request = %#v", second)
	}
}

func TestIOConnReadsFinalMessageWithoutTrailingNewline(t *testing.T) {
	input := strings.NewReader(`{"jsonrpc":"2.0","id":"last","method":"session/list","params":{}}`)
	conn := newIOConn(testRWC{Reader: input, Writer: io.Discard})
	defer conn.Close()

	req := readRequest(t, conn)
	if req.Method != MethodSessionList {
		t.Fatalf("method = %q, want %q", req.Method, MethodSessionList)
	}
}

func TestIOConnHandlesMultibyteUTF8SplitAcrossReads(t *testing.T) {
	message := `{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"sessionId":"s1","prompt":[{"type":"text","text":"h\u00e9llo"}]}}` + "\n"
	reader := io.MultiReader(strings.NewReader(message[:80]), strings.NewReader(message[80:]))
	conn := newIOConn(testRWC{Reader: reader, Writer: io.Discard})
	defer conn.Close()

	req := readRequest(t, conn)
	var params PromptRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatal(err)
	}
	if got := params.Prompt[0].Text; got != "h\u00e9llo" {
		t.Fatalf("prompt text = %q, want h\\u00e9llo", got)
	}
}

func TestIOConnWritesNewlineDelimitedMessages(t *testing.T) {
	var output bytes.Buffer
	conn := newIOConn(testRWC{Reader: strings.NewReader(""), Writer: &output})
	defer conn.Close()

	id, err := jsonrpc.MakeID(float64(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(context.Background(), &jsonrpc.Request{ID: id, Method: MethodInitialize, Params: json.RawMessage(`{"protocolVersion":1}`)}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(output.String(), "\n") {
		t.Fatalf("output %q does not end in newline", output.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &raw); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if raw["jsonrpc"] != "2.0" || raw["method"] != MethodInitialize {
		t.Fatalf("wire message = %#v", raw)
	}
}

func readRequest(t *testing.T, conn Connection) *jsonrpc.Request {
	t.Helper()
	msg, err := conn.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	req, ok := msg.(*jsonrpc.Request)
	if !ok {
		t.Fatalf("message type = %T, want *jsonrpc.Request", msg)
	}
	return req
}
