// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRequiredArraysMarshalAsEmptyArrays(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "new session mcp servers",
			in:   NewSessionRequest{Cwd: "/repo"},
			want: `{"cwd":"/repo","mcpServers":[]}`,
		},
		{
			name: "load session mcp servers",
			in:   LoadSessionRequest{SessionID: "s1", Cwd: "/repo"},
			want: `{"sessionId":"s1","cwd":"/repo","mcpServers":[]}`,
		},
		{
			name: "prompt request prompt",
			in:   PromptRequest{SessionID: "s1"},
			want: `{"sessionId":"s1","prompt":[]}`,
		},
		{
			name: "session modes available modes",
			in:   SessionModeState{CurrentModeID: "code"},
			want: `{"currentModeId":"code","availableModes":[]}`,
		},
		{
			name: "stdio mcp server args and env",
			in:   McpServer{Name: "tools", Command: "tools-server"},
			want: `{"name":"tools","command":"tools-server","args":[],"env":[]}`,
		},
		{
			name: "http mcp server headers",
			in:   McpServer{Type: "http", Name: "tools", Url: "https://example.test/mcp"},
			want: `{"type":"http","name":"tools","url":"https://example.test/mcp","headers":[]}`,
		},
		{
			name: "sse mcp server headers",
			in:   McpServer{Type: "sse", Name: "tools", Url: "https://example.test/sse"},
			want: `{"type":"sse","name":"tools","url":"https://example.test/sse","headers":[]}`,
		},
		{
			name: "list sessions response",
			in:   ListSessionsResponse{},
			want: `{"sessions":[]}`,
		},
		{
			name: "set config response",
			in:   SetSessionConfigOptionResponse{},
			want: `{"configOptions":[]}`,
		},
		{
			name: "available commands update",
			in:   SessionUpdate{SessionUpdate: "available_commands_update"},
			want: `{"sessionUpdate":"available_commands_update","availableCommands":[]}`,
		},
		{
			name: "config option update",
			in:   SessionUpdate{SessionUpdate: "config_option_update"},
			want: `{"sessionUpdate":"config_option_update","configOptions":[]}`,
		},
		{
			name: "plan update entries",
			in:   SessionUpdate{SessionUpdate: "plan"},
			want: `{"sessionUpdate":"plan","entries":[]}`,
		},
		{
			name: "permission request options",
			in: RequestPermissionRequest{
				SessionID: "s1",
				ToolCall:  ToolCallUpdate{ToolCallID: "call-1"},
			},
			want: `{"sessionId":"s1","toolCall":{"toolCallId":"call-1"},"options":[]}`,
		},
		{
			name: "select config option options",
			in:   SelectSessionConfigOption("model", "Model", "fast", SessionConfigSelectOptions{}),
			want: `{"type":"select","currentValue":"fast","id":"model","name":"Model","options":null}`,
		},
		{
			name: "select config group options",
			in: SessionConfigSelectGroup{
				Group: "recommended",
				Name:  "Recommended",
			},
			want: `{"group":"recommended","name":"Recommended","options":[]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			assertJSONEqual(t, string(got), tt.want)
		})
	}
}

func TestContentBlockWireShapes(t *testing.T) {
	tests := []struct {
		name string
		in   ContentBlock
		want string
	}{
		{
			name: "text",
			in:   ContentBlock{Type: ContentBlockTypeText, Text: "hello"},
			want: `{"type":"text","text":"hello"}`,
		},
		{
			name: "image data base64",
			in:   ContentBlock{Type: ContentBlockTypeImage, Data: "AQID", MimeType: ptr("image/png")},
			want: `{"type":"image","data":"AQID","mimeType":"image/png"}`,
		},
		{
			name: "embedded resource",
			in: ContentBlock{
				Type: ContentBlockTypeResource,
				Resource: EmbeddedResourceResource{
					URI:      "file:///repo/main.go",
					MimeType: ptr("text/x-go"),
					Text:     "package main\n",
				},
			},
			want: `{"type":"resource","resource":{"uri":"file:///repo/main.go","mimeType":"text/x-go","text":"package main\n"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			assertJSONEqual(t, string(got), tt.want)
		})
	}
}

func TestSessionUpdateWireShapes(t *testing.T) {
	title := "Run tests"
	got, err := json.Marshal(SessionUpdate{
		SessionUpdate: "tool_call_update",
		ToolCallID:    "call-1",
		Title:         &title,
		Status:        ptr(ToolCallStatusCompleted),
		Content: []ToolCallContent{{
			Type:    ToolCallContentTypeContent,
			Content: ContentBlock{Type: ContentBlockTypeText, Text: "ok"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, string(got), `{
		"sessionUpdate":"tool_call_update",
		"toolCallId":"call-1",
		"title":"Run tests",
		"status":"completed",
		"content":[{"type":"content","content":{"type":"text","text":"ok"}}]
	}`)
}

func TestUnknownParamsFieldsAreIgnored(t *testing.T) {
	data := []byte(`{"protocolVersion":1,"unknown":"ignored","clientCapabilities":{"terminal":true,"unexpected":true}}`)
	var req InitializeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatal(err)
	}
	if req.ProtocolVersion != 1 {
		t.Fatalf("protocol version = %d, want 1", req.ProtocolVersion)
	}
	if req.ClientCapabilities == nil || !req.ClientCapabilities.Terminal {
		t.Fatalf("client capabilities = %#v, want terminal capability", req.ClientCapabilities)
	}
}

func assertJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("unmarshal got JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("unmarshal want JSON: %v\n%s", err, want)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON mismatch\ngot:  %s\nwant: %s", got, want)
	}
}
