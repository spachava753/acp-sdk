// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
package session_test

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

type pathClient struct {
	resources chan string
	tools     chan string
	files     chan string
	cwds      chan string
}

func (h *pathClient) SessionUpdate(_ context.Context, params *acp.SessionNotification) error {
	switch params.Update.SessionUpdate {
	case acp.SessionUpdateTypeAgentMessageChunk:
		content, ok := params.Update.Content.(acp.ContentBlock)
		if !ok {
			h.resources <- "invalid content type"
			return nil
		}
		if content.Type == acp.ContentBlockTypeResource {
			h.resources <- content.Resource.URI
		} else if content.URI != nil {
			h.resources <- *content.URI
		} else {
			h.resources <- "missing resource URI"
		}
	case acp.SessionUpdateTypeToolCall:
		if params.Update.Locations != nil {
			for _, location := range *params.Update.Locations {
				h.tools <- location.Path
			}
		}
	case acp.SessionUpdateTypeToolCallUpdate:
		var content []acp.ToolCallContent
		switch value := params.Update.Content.(type) {
		case []acp.ToolCallContent:
			content = value
		case *[]acp.ToolCallContent:
			if value != nil {
				content = *value
			}
		}
		if len(content) == 1 {
			h.tools <- content[0].Path
		} else {
			h.tools <- "missing diff path"
		}
	}
	return nil
}
func (h *pathClient) FsWriteTextFile(_ context.Context, params *acp.WriteTextFileRequest) (*acp.WriteTextFileResponse, error) {
	h.files <- params.Path
	return &acp.WriteTextFileResponse{}, nil
}
func (h *pathClient) FsReadTextFile(_ context.Context, params *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	h.files <- params.Path
	return &acp.ReadTextFileResponse{Content: "written by testy\n"}, nil
}
func (h *pathClient) TerminalCreate(_ context.Context, params *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	if params.Cwd == nil {
		h.cwds <- "missing cwd"
	} else {
		h.cwds <- *params.Cwd
	}
	return &acp.CreateTerminalResponse{TerminalID: "testy-terminal-client"}, nil
}

// Rust: src/agent-client-protocol-test/tests/testy.rs::testy_full_scenario_roots_paths_in_the_session_cwd.
// Ports receiving-side URI/path/cwd preservation through actual typed callbacks.
// The independent peer supplies the literal cwd-rooted paths; Testy's path
// generator, MCP execution and application session state are not implemented here.
func TestTestyCallbackPathsPreserved(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "testy-project")
	fileURI := func(name string) string {
		return (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(cwd, name))}).String()
	}
	handler := &pathClient{resources: make(chan string, 3), tools: make(chan string, 2), files: make(chan string, 2), cwds: make(chan string, 2)}
	ctx, client, peer := clientPeer(t, handler)
	initialized := startCall(func() (*acp.InitializeResponse, error) {
		return client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1, ClientCapabilities: &acp.ClientCapabilities{Fs: &acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true}, Terminal: true}})
	})
	peer.result(t, peer.read(t), map[string]any{"protocolVersion": 1})
	valueOf(t, initialized)
	created := startCall(func() (*acp.NewSessionResponse, error) {
		return client.SessionNew(ctx, &acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	})
	creation := peer.read(t)
	var params map[string]json.RawMessage
	if err := json.Unmarshal(creation.Params, &params); err != nil {
		t.Fatal(err)
	}
	expectedCwd, _ := json.Marshal(cwd)
	assertJSON(t, params["cwd"], string(expectedCwd))
	peer.result(t, creation, map[string]any{"sessionId": "s"})
	valueOf(t, created)
	prompt := startCall(func() (*acp.PromptResponse, error) {
		return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "s", Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "full"}}})
	})
	request := peer.read(t)
	for _, content := range []map[string]any{
		{"type": "image", "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=", "mimeType": "image/png", "uri": fileURI("testy-pixel.png")},
		{"type": "resource_link", "name": "Testy reference", "uri": fileURI("testy-reference.txt")},
		{"type": "resource", "resource": map[string]any{"text": "embedded resource text from testy", "uri": fileURI("testy-embedded.txt"), "mimeType": "text/plain"}},
	} {
		peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": content}}})
	}
	peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s", "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "read", "title": "Read deterministic fixture", "locations": []any{map[string]any{"path": filepath.Join(cwd, "testy-input.txt")}}}}})
	peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s", "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "edit", "title": "Apply deterministic edit"}}})
	peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s", "update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "edit", "content": []any{map[string]any{"type": "diff", "path": filepath.Join(cwd, "testy-output.txt"), "oldText": "before\n", "newText": "after\n"}}}}})
	for index, method := range []string{"fs/write_text_file", "fs/read_text_file", "terminal/create", "terminal/create"} {
		params := map[string]any{"sessionId": "s"}
		if method == "terminal/create" {
			params["command"], params["cwd"] = "printf", cwd
		} else {
			params["path"] = filepath.Join(cwd, "testy-write.txt")
			if method == "fs/write_text_file" {
				params["content"] = "written by testy\n"
			}
		}
		peer.send(t, map[string]any{"jsonrpc": "2.0", "id": index + 1, "method": method, "params": params})
		if response := peer.read(t); response.Error != nil {
			t.Fatalf("path callback failed: %+v", response)
		}
	}
	peer.result(t, request, map[string]any{"stopReason": "end_turn"})
	if valueOf(t, prompt).StopReason != acp.StopReasonEndTurn {
		t.Fatal("wrong stop reason")
	}
	for _, name := range []string{"testy-pixel.png", "testy-reference.txt", "testy-embedded.txt"} {
		if got := await(t, handler.resources); got != fileURI(name) {
			t.Fatalf("resource URI = %q, want %q", got, fileURI(name))
		}
	}
	for _, name := range []string{"testy-input.txt", "testy-output.txt"} {
		if got := await(t, handler.tools); got != filepath.Join(cwd, name) {
			t.Fatalf("tool path = %q", got)
		}
	}
	for range 2 {
		if got := await(t, handler.files); got != filepath.Join(cwd, "testy-write.txt") {
			t.Fatalf("file path = %q", got)
		}
		if got := await(t, handler.cwds); got != cwd {
			t.Fatalf("terminal cwd = %q", got)
		}
	}
}
