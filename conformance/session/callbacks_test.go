// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// callbackClient implements every client callback and records each call.
// Its channels are buffered so callbacks never wait for the test.
type callbackClient struct {
	updateRecorder
	called      chan string
	released    chan acp.TerminalId
	elicitation chan acp.CreateElicitationRequest
	completed   chan acp.ElicitationId
	terminals   atomic.Int64
}

func newCallbackClient() *callbackClient {
	return &callbackClient{
		updateRecorder: *newUpdateRecorder(),
		called:         make(chan string, 16),
		released:       make(chan acp.TerminalId, 2),
		elicitation:    make(chan acp.CreateElicitationRequest, 5),
		completed:      make(chan acp.ElicitationId, 1),
	}
}

func (h *callbackClient) SessionRequestPermission(_ context.Context, params *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	h.called <- "session/request_permission"
	return &acp.RequestPermissionResponse{Outcome: acp.SelectedRequestPermissionOutcome(params.Options[0].OptionID)}, nil
}

func (h *callbackClient) FsWriteTextFile(context.Context, *acp.WriteTextFileRequest) (*acp.WriteTextFileResponse, error) {
	h.called <- "fs/write_text_file"
	return &acp.WriteTextFileResponse{}, nil
}

func (h *callbackClient) FsReadTextFile(context.Context, *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	h.called <- "fs/read_text_file"
	return &acp.ReadTextFileResponse{Content: "file contents"}, nil
}

func (h *callbackClient) TerminalCreate(context.Context, *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	h.called <- "terminal/create"
	return &acp.CreateTerminalResponse{TerminalID: acp.TerminalId(fmt.Sprintf("terminal-%d", h.terminals.Add(1)))}, nil
}

func (h *callbackClient) TerminalOutput(context.Context, *acp.TerminalOutputRequest) (*acp.TerminalOutputResponse, error) {
	h.called <- "terminal/output"
	return &acp.TerminalOutputResponse{Output: "terminal output", ExitStatus: &acp.TerminalExitStatus{ExitCode: new(uint32)}}, nil
}

func (h *callbackClient) TerminalWaitForExit(context.Context, *acp.WaitForTerminalExitRequest) (*acp.WaitForTerminalExitResponse, error) {
	h.called <- "terminal/wait_for_exit"
	return &acp.WaitForTerminalExitResponse{ExitCode: new(uint32)}, nil
}

func (h *callbackClient) TerminalKill(context.Context, *acp.KillTerminalRequest) (*acp.KillTerminalResponse, error) {
	h.called <- "terminal/kill"
	return &acp.KillTerminalResponse{}, nil
}

func (h *callbackClient) TerminalRelease(_ context.Context, params *acp.ReleaseTerminalRequest) (*acp.ReleaseTerminalResponse, error) {
	h.called <- "terminal/release"
	h.released <- params.TerminalID
	return &acp.ReleaseTerminalResponse{}, nil
}

func (h *callbackClient) ElicitationComplete(_ context.Context, params *acp.CompleteElicitationNotification) error {
	h.completed <- params.ElicitationID
	return nil
}

func (h *callbackClient) ElicitationCreate(_ context.Context, params *acp.CreateElicitationRequest) (*acp.CreateElicitationResponse, error) {
	h.elicitation <- *params
	var response acp.CreateElicitationResponse
	switch params.Message {
	case "Accept the session-scoped form":
		response = acp.AcceptCreateElicitationResponse()
		content := map[string]acp.ElicitationContentValue{"name": json.RawMessage(`"Ada"`), "age": json.RawMessage(`42`), "confidence": json.RawMessage(`0.95`), "confirmed": json.RawMessage(`true`), "tags": json.RawMessage(`["go","acp"]`)}
		response.Content = &content
	case "Decline the session-scoped form", "Decline the request-scoped URL":
		response = acp.DeclineCreateElicitationResponse()
	case "Cancel the request-scoped form":
		response = acp.CancelCreateElicitationResponse()
	case "Accept the session-scoped URL":
		response = acp.AcceptCreateElicitationResponse()
	default:
		return nil, fmt.Errorf("unexpected elicitation message %q", params.Message)
	}
	return &response, nil
}

// TestClientCallbacks sends every client callback while a prompt is pending.
// Each must reach its handler and answer with the handler's result. A later
// tool call update must still deliver terminal content to the update handler.
func TestClientCallbacks(t *testing.T) {
	h := newCallbackClient()
	client, peer := startSession(t, h, &acp.ClientCapabilities{Fs: &acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true}, Terminal: true})
	result := prompt(client, "s", "run callbacks")
	request := peer.Expect("session/prompt")
	for i, step := range []struct{ method, params, response string }{
		{"terminal/create", `{"sessionId":"s","command":"printf","args":["hello"]}`, `{"terminalId":"terminal-1"}`},
		{"terminal/release", `{"sessionId":"s","terminalId":"terminal-1"}`, `{}`},
		{"session/request_permission", `{"sessionId":"s","toolCall":{"toolCallId":"read"},"options":[{"optionId":"allow_once","name":"Allow once","kind":"allow_once"}]}`, `{"outcome":{"outcome":"selected","optionId":"allow_once"}}`},
		{"fs/write_text_file", `{"sessionId":"s","path":"/tmp/output.txt","content":"hello"}`, `{}`},
		{"fs/read_text_file", `{"sessionId":"s","path":"/tmp/input.txt"}`, `{"content":"file contents"}`},
		{"terminal/create", `{"sessionId":"s","command":"printf","args":["hello"]}`, `{"terminalId":"terminal-2"}`},
		{"terminal/output", `{"sessionId":"s","terminalId":"terminal-2"}`, `{"output":"terminal output","truncated":false,"exitStatus":{"exitCode":0}}`},
		{"terminal/wait_for_exit", `{"sessionId":"s","terminalId":"terminal-2"}`, `{"exitCode":0}`},
		{"terminal/kill", `{"sessionId":"s","terminalId":"terminal-2"}`, `{}`},
		{"terminal/release", `{"sessionId":"s","terminalId":"terminal-2"}`, `{}`},
	} {
		id := fmt.Sprintf("callback-%d", i)
		response := peer.Call(id, step.method, json.RawMessage(step.params))
		if !response.IsResponse() || response.Error != nil || string(response.ID) != string(acptest.RawID(id)) {
			t.Fatalf("%s response = %s", step.method, response)
		}
		acptest.EqualJSON(t, response.Result, step.response)
		if got := acptest.Await(t, h.called); got != step.method {
			t.Fatalf("handler called = %q, want %q", got, step.method)
		}
	}
	for _, want := range []acp.TerminalId{"terminal-1", "terminal-2"} {
		if got := acptest.Await(t, h.released); got != want {
			t.Fatalf("released terminal = %q, want %q", got, want)
		}
	}

	peer.Notify("session/update", json.RawMessage(`{"sessionId":"s","update":{"sessionUpdate":"tool_call_update","toolCallId":"terminal","status":"completed","content":[{"type":"terminal","terminalId":"terminal-1"}]}}`))
	update := h.next(t)
	if content := toolContent(update); len(content) != 1 || content[0].Type != acp.ToolCallContentTypeTerminal || content[0].TerminalID != "terminal-1" {
		t.Fatalf("terminal content = %#v", update.Update.Content)
	}
	endTurn(peer, request)
	expectStop(t, result, acp.StopReasonEndTurn)
}

// formSchema covers each elicitation field and value shape.
const formSchema = `{"type":"object","title":"Elicitation form","description":"Form covering ACP elicitation field and value shapes","properties":{"name":{"type":"string"},"email":{"type":"string","format":"email"},"homepage":{"type":"string","format":"uri"},"birthday":{"type":"string","format":"date"},"available_at":{"type":"string","format":"date-time"},"confidence":{"type":"number","minimum":0,"maximum":1},"age":{"type":"integer","minimum":0,"maximum":120},"confirmed":{"type":"boolean"},"priority":{"type":"string","enum":["low","normal","high"]},"tags":{"type":"array","items":{"type":"string","enum":["go","acp","sdk"]}}},"required":["name","confidence","age","confirmed"]}`

// TestElicitation sends form and URL elicitations scoped to the session or
// to the pending prompt request. The handler must see the mode, message,
// scope, tool call, schema and URL fields as sent, and its accept, decline
// or cancel answer must reach the wire. elicitation/complete must reach its
// handler too.
func TestElicitation(t *testing.T) {
	h := newCallbackClient()
	client, peer := startSession(t, h, &acp.ClientCapabilities{Elicitation: &acp.ElicitationCapabilities{Form: &acp.ElicitationFormCapabilities{}, Url: &acp.ElicitationUrlCapabilities{}}})
	result := prompt(client, "s", "elicit")
	request := peer.Expect("session/prompt")
	for i, step := range []struct {
		message, scope, mode string
		toolCall             bool
		id, url, response    string
	}{
		{message: "Accept the session-scoped form", scope: "session", mode: "form", toolCall: true, response: `{"action":"accept","content":{"name":"Ada","age":42,"confidence":0.95,"confirmed":true,"tags":["go","acp"]}}`},
		{message: "Decline the session-scoped form", scope: "session", mode: "form", response: `{"action":"decline"}`},
		{message: "Cancel the request-scoped form", scope: "request", mode: "form", response: `{"action":"cancel"}`},
		{message: "Accept the session-scoped URL", scope: "session", mode: "url", id: "url-session", url: "https://example.com/session", response: `{"action":"accept"}`},
		{message: "Decline the request-scoped URL", scope: "request", mode: "url", id: "url-request", url: "https://example.com/request", response: `{"action":"decline"}`},
	} {
		params := map[string]any{"mode": step.mode, "message": step.message}
		if step.scope == "session" {
			params["sessionId"] = "s"
		} else {
			params["requestId"] = request.ID
		}
		if step.mode == "form" {
			params["requestedSchema"] = json.RawMessage(formSchema)
		} else {
			params["elicitationId"], params["url"] = step.id, step.url
		}
		if step.toolCall {
			params["toolCallId"] = "elicitation-tool"
		}
		response := peer.Call(fmt.Sprintf("elicitation-%d", i), "elicitation/create", params)
		if response.Error != nil {
			t.Fatalf("%q: error %s", step.message, response)
		}
		acptest.EqualJSON(t, response.Result, step.response)

		got := acptest.Await(t, h.elicitation)
		if string(got.Mode) != step.mode || got.Message != step.message {
			t.Fatalf("elicitation = %#v, want mode %s message %q", got, step.mode, step.message)
		}
		switch step.scope {
		case "session":
			if got.SessionID != "s" || got.RequestID != nil {
				t.Errorf("%q: scope = session %q request %s, want session s only", step.message, got.SessionID, got.RequestID)
			}
		case "request":
			acptest.EqualMarshaled(t, got.RequestID, string(request.ID))
		}
		if (got.ToolCallID != nil) != step.toolCall {
			t.Errorf("%q: tool call ID = %v, want present=%v", step.message, got.ToolCallID, step.toolCall)
		}
		if step.toolCall {
			for _, property := range []string{"name", "email", "homepage", "birthday", "available_at", "confidence", "age", "confirmed", "priority", "tags"} {
				if _, ok := got.RequestedSchema.Properties[property]; !ok {
					t.Errorf("form schema lost property %q", property)
				}
			}
		}
		if step.mode == "url" && (string(got.ElicitationID) != step.id || got.Url != step.url) {
			t.Errorf("URL elicitation = %#v, want id %s url %s", got, step.id, step.url)
		}
	}

	peer.Notify("elicitation/complete", map[string]any{"elicitationId": "url-session"})
	if got := acptest.Await(t, h.completed); got != "url-session" {
		t.Fatalf("completed elicitation = %q, want url-session", got)
	}
	endTurn(peer, request)
	expectStop(t, result, acp.StopReasonEndTurn)
}

// cancellingClient sends session/cancel from inside each callback before
// answering it.
type cancellingClient struct {
	client  *acp.Client
	options chan []acp.PermissionOption
}

func (h *cancellingClient) cancel(ctx context.Context, session acp.SessionId) error {
	return h.client.SessionCancel(ctx, &acp.CancelNotification{SessionID: session})
}

// SessionRequestPermission answers with the first option after cancelling.
// A real client answers pending permission requests with cancelled once it
// has cancelled; this test only needs the reply to complete.
func (h *cancellingClient) SessionRequestPermission(ctx context.Context, params *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	h.options <- params.Options
	if err := h.cancel(ctx, params.SessionID); err != nil {
		return nil, err
	}
	return &acp.RequestPermissionResponse{Outcome: acp.SelectedRequestPermissionOutcome(params.Options[0].OptionID)}, nil
}

func (h *cancellingClient) TerminalCreate(ctx context.Context, params *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	if err := h.cancel(ctx, params.SessionID); err != nil {
		return nil, err
	}
	return &acp.CreateTerminalResponse{TerminalID: "terminal"}, nil
}

func (h *cancellingClient) TerminalOutput(ctx context.Context, params *acp.TerminalOutputRequest) (*acp.TerminalOutputResponse, error) {
	if err := h.cancel(ctx, params.SessionID); err != nil {
		return nil, err
	}
	return &acp.TerminalOutputResponse{Output: "terminal output", ExitStatus: &acp.TerminalExitStatus{ExitCode: new(uint32)}}, nil
}

// TestCallbackCanCancelSession checks that a callback can send session/cancel
// while the prompt it belongs to is still pending. The peer holds the prompt
// response until both the cancel notification and the callback's reply have
// arrived, in that order, so cancelling cannot rely on an idle connection.
func TestCallbackCanCancelSession(t *testing.T) {
	for _, tc := range []struct{ name, method, params, response string }{
		{"permission", "session/request_permission", `{"sessionId":"s","toolCall":{"toolCallId":"call"},"options":[{"optionId":"allow_once","name":"Allow once","kind":"allow_once"}]}`, `{"outcome":{"outcome":"selected","optionId":"allow_once"}}`},
		{"terminal_create", "terminal/create", `{"sessionId":"s","command":"printf"}`, `{"terminalId":"terminal"}`},
		{"terminal_output", "terminal/output", `{"sessionId":"s","terminalId":"terminal"}`, `{"output":"terminal output","truncated":false,"exitStatus":{"exitCode":0}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &cancellingClient{options: make(chan []acp.PermissionOption, 1)}
			client, peer := startSession(t, h, nil)
			h.client = client
			result := prompt(client, "s", tc.name)
			request := peer.ExpectCall("session/prompt")
			acptest.EqualJSON(t, request.Params, `{"sessionId":"s","prompt":[{"type":"text","text":"`+tc.name+`"}]}`)

			peer.Request("callback", tc.method, json.RawMessage(tc.params))
			cancel := peer.Expect("session/cancel")
			if !cancel.IsNotification() {
				t.Fatalf("cancel sent as request: %s", cancel)
			}
			acptest.EqualJSON(t, cancel.Params, `{"sessionId":"s"}`)
			response := peer.Next()
			if !response.IsResponse() || response.Error != nil || string(response.ID) != `"callback"` {
				t.Fatalf("callback response = %s", response)
			}
			acptest.EqualJSON(t, response.Result, tc.response)
			if tc.method == "session/request_permission" {
				acptest.EqualMarshaled(t, acptest.Await(t, h.options), `[{"optionId":"allow_once","name":"Allow once","kind":"allow_once"}]`)
			}

			peer.Reply(request.ID, map[string]any{"stopReason": "cancelled"})
			expectStop(t, result, acp.StopReasonCancelled)
		})
	}
}

// TestUnhandledClientRequest checks that a client without a handler for a
// session request answers it with MethodNotFound naming the method.
func TestUnhandledClientRequest(t *testing.T) {
	_, peer := acptest.StartClient(t, nil)
	response := peer.Call(1, "session/request_permission", map[string]any{"sessionId": "untracked", "toolCall": map[string]any{"toolCallId": "call-1"}, "options": []any{}})
	if string(response.ID) != "1" || response.Error == nil || response.Error.Code != jsonrpc.CodeMethodNotFound {
		t.Fatalf("response = %s, want MethodNotFound for id 1", response)
	}
	acptest.EqualJSON(t, response.Error.Data, `"session/request_permission"`)
}

// pathClient records the URIs, paths and working directories its callbacks
// receive, in order.
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
		switch {
		case !ok:
			h.resources <- fmt.Sprintf("content type %T", params.Update.Content)
		case content.Type == acp.ContentBlockTypeResource:
			h.resources <- content.Resource.URI
		case content.URI != nil:
			h.resources <- *content.URI
		default:
			h.resources <- "missing resource URI"
		}
	case acp.SessionUpdateTypeToolCall:
		if params.Update.Locations != nil {
			for _, location := range *params.Update.Locations {
				h.tools <- location.Path
			}
		}
	case acp.SessionUpdateTypeToolCallUpdate:
		if content := toolContent(*params); len(content) == 1 {
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
	return &acp.ReadTextFileResponse{Content: "written\n"}, nil
}

func (h *pathClient) TerminalCreate(_ context.Context, params *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	if params.Cwd == nil {
		h.cwds <- "missing cwd"
	} else {
		h.cwds <- *params.Cwd
	}
	return &acp.CreateTerminalResponse{TerminalID: "terminal"}, nil
}

// TestCallbackPaths checks that file URIs in content, tool call locations and
// diff paths, file paths and terminal working directories rooted in the
// session's cwd all reach the handlers unchanged.
func TestCallbackPaths(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "project")
	path := func(name string) string { return filepath.Join(cwd, name) }
	fileURI := func(name string) string {
		return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path(name))}).String()
	}
	h := &pathClient{resources: make(chan string, 3), tools: make(chan string, 2), files: make(chan string, 2), cwds: make(chan string, 2)}
	client, peer := acptest.StartClient(t, h)
	acptest.InitializeClient(t, client, peer, &acp.ClientCapabilities{Fs: &acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true}, Terminal: true}, nil)
	created := acptest.Go(func() (*acp.NewSessionResponse, error) {
		return client.SessionNew(context.Background(), &acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	})
	request := peer.Expect("session/new")
	var params struct {
		Cwd json.RawMessage `json:"cwd"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatal(err)
	}
	acptest.EqualJSON(t, params.Cwd, string(acptest.Marshal(t, cwd)))
	peer.Reply(request.ID, map[string]any{"sessionId": "s"})
	acptest.Value(t, created)

	result := prompt(client, "s", "paths")
	request = peer.Expect("session/prompt")
	update := func(update map[string]any) {
		peer.Notify("session/update", map[string]any{"sessionId": "s", "update": update})
	}
	for _, content := range []map[string]any{
		{"type": "image", "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=", "mimeType": "image/png", "uri": fileURI("pixel.png")},
		{"type": "resource_link", "name": "Reference", "uri": fileURI("reference.txt")},
		{"type": "resource", "resource": map[string]any{"text": "embedded resource text", "uri": fileURI("embedded.txt"), "mimeType": "text/plain"}},
	} {
		update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": content})
	}
	update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "read", "title": "Read file", "locations": []any{map[string]any{"path": path("input.txt")}}})
	update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "edit", "title": "Edit file"})
	update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "edit", "content": []any{map[string]any{"type": "diff", "path": path("output.txt"), "oldText": "before\n", "newText": "after\n"}}})
	for i, call := range []struct {
		method string
		params map[string]any
	}{
		{"fs/write_text_file", map[string]any{"sessionId": "s", "path": path("write.txt"), "content": "written\n"}},
		{"fs/read_text_file", map[string]any{"sessionId": "s", "path": path("write.txt")}},
		{"terminal/create", map[string]any{"sessionId": "s", "command": "printf", "cwd": cwd}},
		{"terminal/create", map[string]any{"sessionId": "s", "command": "printf", "cwd": cwd}},
	} {
		if response := peer.Call(i+1, call.method, call.params); response.Error != nil {
			t.Fatalf("%s failed: %s", call.method, response)
		}
	}
	endTurn(peer, request)
	expectStop(t, result, acp.StopReasonEndTurn)

	for _, name := range []string{"pixel.png", "reference.txt", "embedded.txt"} {
		if got := acptest.Await(t, h.resources); got != fileURI(name) {
			t.Fatalf("resource URI = %q, want %q", got, fileURI(name))
		}
	}
	for _, name := range []string{"input.txt", "output.txt"} {
		if got := acptest.Await(t, h.tools); got != path(name) {
			t.Fatalf("tool path = %q, want %q", got, path(name))
		}
	}
	for range 2 {
		if got := acptest.Await(t, h.files); got != path("write.txt") {
			t.Fatalf("file path = %q, want %q", got, path("write.txt"))
		}
		if got := acptest.Await(t, h.cwds); got != cwd {
			t.Fatalf("terminal cwd = %q, want %q", got, cwd)
		}
	}
}
