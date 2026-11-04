// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// This is the client callback fixture from Testy tests, not a Go reimplementation
// of the Testy agent/session store. The independent peer authors each request.
type callbackClient struct {
	callbacks   chan string
	released    chan string
	updates     chan acp.SessionNotification
	elicitation chan acp.CreateElicitationRequest
	completed   chan acp.ElicitationId
	creates     atomic.Int64
}

func newCallbackClient() *callbackClient {
	return &callbackClient{callbacks: make(chan string, 16), released: make(chan string, 2), updates: make(chan acp.SessionNotification, 16), elicitation: make(chan acp.CreateElicitationRequest, 5), completed: make(chan acp.ElicitationId, 1)}
}

func (h *callbackClient) SessionUpdate(_ context.Context, params *acp.SessionNotification) error {
	h.updates <- *params
	return nil
}
func (h *callbackClient) SessionRequestPermission(_ context.Context, params *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	h.callbacks <- "session/request_permission"
	option := acp.PermissionOptionId("allow_once")
	if len(params.Options) != 0 {
		option = params.Options[0].OptionID
	}
	return &acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{Outcome: "selected", OptionID: option}}, nil
}
func (h *callbackClient) FsWriteTextFile(context.Context, *acp.WriteTextFileRequest) (*acp.WriteTextFileResponse, error) {
	h.callbacks <- "fs/write_text_file"
	return &acp.WriteTextFileResponse{}, nil
}
func (h *callbackClient) FsReadTextFile(context.Context, *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	h.callbacks <- "fs/read_text_file"
	return &acp.ReadTextFileResponse{Content: "read by testy client"}, nil
}
func (h *callbackClient) TerminalCreate(context.Context, *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	h.callbacks <- "terminal/create"
	return &acp.CreateTerminalResponse{TerminalID: acp.TerminalId(fmt.Sprintf("testy-terminal-client-%d", h.creates.Add(1)))}, nil
}
func (h *callbackClient) TerminalOutput(context.Context, *acp.TerminalOutputRequest) (*acp.TerminalOutputResponse, error) {
	h.callbacks <- "terminal/output"
	zero := uint32(0)
	return &acp.TerminalOutputResponse{Output: "terminal output", Truncated: false, ExitStatus: &acp.TerminalExitStatus{ExitCode: &zero}}, nil
}
func (h *callbackClient) TerminalWaitForExit(context.Context, *acp.WaitForTerminalExitRequest) (*acp.WaitForTerminalExitResponse, error) {
	h.callbacks <- "terminal/wait_for_exit"
	zero := uint32(0)
	return &acp.WaitForTerminalExitResponse{ExitCode: &zero}, nil
}
func (h *callbackClient) TerminalKill(context.Context, *acp.KillTerminalRequest) (*acp.KillTerminalResponse, error) {
	h.callbacks <- "terminal/kill"
	return &acp.KillTerminalResponse{}, nil
}
func (h *callbackClient) TerminalRelease(_ context.Context, params *acp.ReleaseTerminalRequest) (*acp.ReleaseTerminalResponse, error) {
	h.callbacks <- "terminal/release"
	h.released <- string(params.TerminalID)
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
	case "Accept the Testy session-scoped form elicitation":
		response = acp.AcceptCreateElicitationResponse()
		content := map[string]acp.ElicitationContentValue{"name": json.RawMessage(`"Ada"`), "age": json.RawMessage(`42`), "confidence": json.RawMessage(`0.95`), "confirmed": json.RawMessage(`true`), "tags": json.RawMessage(`["rust","acp"]`)}
		response.Content = &content
	case "Decline the Testy session-scoped form elicitation", "Decline the Testy request-scoped URL elicitation":
		response = acp.DeclineCreateElicitationResponse()
	case "Cancel the Testy request-scoped form elicitation":
		response = acp.CancelCreateElicitationResponse()
	case "Accept the Testy session-scoped URL elicitation":
		response = acp.AcceptCreateElicitationResponse()
	default:
		return nil, fmt.Errorf("unexpected upstream elicitation message %q", params.Message)
	}
	return &response, nil
}

// Rust: src/agent-client-protocol-test/tests/testy.rs::testy_full_scenario_exercises_updates_and_callbacks.
// Ports the complete ten-callback public SDK slice and terminal-content reference
// assertions. Testy's command/report generation and session store are not emulated.
func TestTestyFullCallbackDispatch(t *testing.T) {
	handler := newCallbackClient()
	ctx, client, peer := clientPeer(t, handler)
	prepareClientSession(t, ctx, client, peer, &acp.ClientCapabilities{Fs: &acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true}, Terminal: true})
	promptResult := startCall(func() (*acp.PromptResponse, error) {
		return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "s", Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "full"}}})
	})
	prompt := peer.read(t)
	for index, scenario := range []struct{ method, params, response string }{
		{"terminal/create", `{"sessionId":"s","command":"printf","args":["testy"]}`, `{"terminalId":"testy-terminal-client-1"}`},
		{"terminal/release", `{"sessionId":"s","terminalId":"testy-terminal-client-1"}`, `{}`},
		{"session/request_permission", `{"sessionId":"s","toolCall":{"toolCallId":"testy-tool-read"},"options":[{"optionId":"allow_once","name":"Allow once","kind":"allow_once"}]}`, `{"outcome":{"outcome":"selected","optionId":"allow_once"}}`},
		{"fs/write_text_file", `{"sessionId":"s","path":"/tmp/testy-output.txt","content":"testy"}`, `{}`},
		{"fs/read_text_file", `{"sessionId":"s","path":"/tmp/testy-input.txt"}`, `{"content":"read by testy client"}`},
		{"terminal/create", `{"sessionId":"s","command":"printf","args":["testy"]}`, `{"terminalId":"testy-terminal-client-2"}`},
		{"terminal/output", `{"sessionId":"s","terminalId":"testy-terminal-client-2"}`, `{"output":"terminal output","truncated":false,"exitStatus":{"exitCode":0}}`},
		{"terminal/wait_for_exit", `{"sessionId":"s","terminalId":"testy-terminal-client-2"}`, `{"exitCode":0}`},
		{"terminal/kill", `{"sessionId":"s","terminalId":"testy-terminal-client-2"}`, `{}`},
		{"terminal/release", `{"sessionId":"s","terminalId":"testy-terminal-client-2"}`, `{}`},
	} {
		id := fmt.Sprintf("callback-%d", index)
		peer.send(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": scenario.method, "params": json.RawMessage(scenario.params)})
		response := peer.read(t)
		if response.Error != nil || string(response.ID) != `"`+id+`"` {
			t.Fatalf("callback response = %+v", response)
		}
		assertJSON(t, response.Result, scenario.response)
		if got := await(t, handler.callbacks); got != scenario.method {
			t.Fatalf("callback = %q, want %q", got, scenario.method)
		}
	}
	for _, id := range []string{"testy-terminal-client-1", "testy-terminal-client-2"} {
		if got := await(t, handler.released); got != id {
			t.Fatalf("released terminal = %q, want %q", got, id)
		}
	}
	peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": json.RawMessage(`{"sessionId":"s","update":{"sessionUpdate":"tool_call_update","toolCallId":"testy-tool-terminal","status":"completed","content":[{"type":"terminal","terminalId":"testy-terminal-client-1"}]}}`)})
	update := await(t, handler.updates)
	var content []acp.ToolCallContent
	switch value := update.Update.Content.(type) {
	case []acp.ToolCallContent:
		content = value
	case *[]acp.ToolCallContent:
		if value != nil {
			content = *value
		}
	}
	if len(content) != 1 || content[0].Type != acp.ToolCallContentTypeTerminal || content[0].TerminalID != "testy-terminal-client-1" {
		t.Fatalf("terminal content = %#v", update.Update.Content)
	}
	peer.result(t, prompt, map[string]any{"stopReason": "end_turn"})
	if valueOf(t, promptResult).StopReason != acp.StopReasonEndTurn {
		t.Fatal("wrong full-scenario stop reason")
	}
}

const testyFormSchema = `{"type":"object","title":"Testy elicitation form","description":"Deterministic form covering ACP elicitation field and value shapes","properties":{"name":{"type":"string"},"email":{"type":"string","format":"email"},"homepage":{"type":"string","format":"uri"},"birthday":{"type":"string","format":"date"},"available_at":{"type":"string","format":"date-time"},"confidence":{"type":"number","minimum":0,"maximum":1},"age":{"type":"integer","minimum":0,"maximum":120},"confirmed":{"type":"boolean"},"priority":{"type":"string","enum":["low","normal","high"]},"tags":{"type":"array","items":{"type":"string","enum":["rust","acp","testy"]}}},"required":["name","confidence","age","confirmed"]}`

// Rust: src/agent-client-protocol-test/tests/testy.rs::testy_elicitations_prompt_exercises_all_elicitation_create_and_complete_paths,
// shared helpers testy_elicitation_request_label and testy/v1.rs::testy_elicitation_schema.
// Also covers the independently portable elicitation slice of full_scenario.
func TestTestyElicitationCallbackPaths(t *testing.T) {
	handler := newCallbackClient()
	ctx, client, peer := clientPeer(t, handler)
	prepareClientSession(t, ctx, client, peer, &acp.ClientCapabilities{Elicitation: &acp.ElicitationCapabilities{Form: &acp.ElicitationFormCapabilities{}, Url: &acp.ElicitationUrlCapabilities{}}})
	promptResult := startCall(func() (*acp.PromptResponse, error) {
		return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "s", Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "elicitations"}}})
	})
	prompt := peer.read(t)
	for index, scenario := range []struct{ message, scope, mode, id, url, response string }{
		{"Accept the Testy session-scoped form elicitation", "session", "form", "", "", `{"action":"accept","content":{"name":"Ada","age":42,"confidence":0.95,"confirmed":true,"tags":["rust","acp"]}}`},
		{"Decline the Testy session-scoped form elicitation", "session", "form", "", "", `{"action":"decline"}`},
		{"Cancel the Testy request-scoped form elicitation", "request", "form", "", "", `{"action":"cancel"}`},
		{"Accept the Testy session-scoped URL elicitation", "session", "url", "testy-url-session", "https://example.com/testy/session", `{"action":"accept"}`},
		{"Decline the Testy request-scoped URL elicitation", "request", "url", "testy-url-request", "https://example.com/testy/request", `{"action":"decline"}`},
	} {
		params := map[string]any{"mode": scenario.mode, "message": scenario.message}
		if scenario.scope == "session" {
			params["sessionId"] = "s"
		} else {
			params["requestId"] = prompt.ID
		}
		if scenario.mode == "form" {
			params["requestedSchema"] = json.RawMessage(testyFormSchema)
			if index == 0 {
				params["toolCallId"] = "testy-tool-elicitation"
			}
		} else {
			params["elicitationId"], params["url"] = scenario.id, scenario.url
		}
		id := fmt.Sprintf("elicitation-%d", index)
		peer.send(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": "elicitation/create", "params": params})
		response := peer.read(t)
		if response.Error != nil {
			t.Fatalf("elicitation returned error: %+v", response.Error)
		}
		assertJSON(t, response.Result, scenario.response)
		request := await(t, handler.elicitation)
		if string(request.Mode) != scenario.mode || request.Message != scenario.message {
			t.Fatalf("elicitation = %#v", request)
		}
		if scenario.scope == "session" && (request.SessionID != "s" || request.RequestID != nil) {
			t.Fatalf("session scope = %#v", request)
		}
		if scenario.scope == "request" {
			assertJSON(t, request.RequestID, string(prompt.ID))
		}
		if index == 0 {
			if request.ToolCallID == nil {
				t.Fatal("session accept form lost tool-call scope")
			}
			for _, property := range []string{"name", "email", "homepage", "birthday", "available_at", "confidence", "age", "confirmed", "priority", "tags"} {
				if _, ok := request.RequestedSchema.Properties[property]; !ok {
					t.Errorf("missing form property %q", property)
				}
			}
		}
		if index == 1 && request.ToolCallID != nil {
			t.Fatal("declined session form acquired a tool-call scope")
		}
		if scenario.mode == "url" && (string(request.ElicitationID) != scenario.id || request.Url != scenario.url) {
			t.Fatalf("URL elicitation = %#v", request)
		}
	}
	peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "elicitation/complete", "params": map[string]any{"elicitationId": "testy-url-session"}})
	if got := await(t, handler.completed); got != "testy-url-session" {
		t.Fatalf("completed elicitation = %q", got)
	}
	peer.result(t, prompt, map[string]any{"stopReason": "end_turn"})
	if valueOf(t, promptResult).StopReason != acp.StopReasonEndTurn {
		t.Fatal("wrong elicitation-scenario stop reason")
	}
}

type cancellingCallbackClient struct {
	client      *acp.Client
	permissions chan []acp.PermissionOption
}

func (h *cancellingCallbackClient) SessionRequestPermission(ctx context.Context, params *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	h.permissions <- append([]acp.PermissionOption(nil), params.Options...)
	if err := h.client.SessionCancel(ctx, &acp.CancelNotification{SessionID: params.SessionID}); err != nil {
		return nil, err
	}
	option := acp.PermissionOptionId("allow_once")
	if len(params.Options) != 0 {
		option = params.Options[0].OptionID
	}
	return &acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{Outcome: "selected", OptionID: option}}, nil
}
func (h *cancellingCallbackClient) TerminalCreate(ctx context.Context, params *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	if err := h.client.SessionCancel(ctx, &acp.CancelNotification{SessionID: params.SessionID}); err != nil {
		return nil, err
	}
	return &acp.CreateTerminalResponse{TerminalID: "testy-terminal-client"}, nil
}
func (h *cancellingCallbackClient) TerminalOutput(ctx context.Context, params *acp.TerminalOutputRequest) (*acp.TerminalOutputResponse, error) {
	if err := h.client.SessionCancel(ctx, &acp.CancelNotification{SessionID: params.SessionID}); err != nil {
		return nil, err
	}
	zero := uint32(0)
	return &acp.TerminalOutputResponse{Output: "terminal output", Truncated: false, ExitStatus: &acp.TerminalExitStatus{ExitCode: &zero}}, nil
}

// Rust src/agent-client-protocol-test/tests/testy.rs functions:
// testy_full_scenario_returns_cancelled_after_session_cancel,
// testy_full_scenario_stops_after_terminal_create_cancellation,
// testy_callbacks_scenario_returns_cancelled_after_session_cancel, and
// testy_callbacks_stop_after_terminal_output_cancellation. The portable slice is
// sending cancel before the callback's successful reply without deadlock while
// an outgoing prompt for the same session is pending. Testy's stop/reset policy
// is not manufactured here; the peer's final cancelled response is a fixture value.
// The selected permission reply after cancel deliberately mirrors the upstream
// adverse fixture, not recommended client behavior: ACP clients MUST answer
// pending permission requests with cancelled after sending session/cancel.
func TestCallbacksCanSendSessionCancel(t *testing.T) {
	for _, scenario := range []struct{ name, prompt, method, params, response string }{
		{"permission", `{"command":"run_scenario","scenario":"callbacks"}`, "session/request_permission", `{"sessionId":"s","toolCall":{"toolCallId":"call"},"options":[{"optionId":"allow_once","name":"Allow once","kind":"allow_once"}]}`, `{"outcome":{"outcome":"selected","optionId":"allow_once"}}`},
		{"permission_full", `{"command":"run_scenario","scenario":"full"}`, "session/request_permission", `{"sessionId":"s","toolCall":{"toolCallId":"call"},"options":[{"optionId":"allow_once","name":"Allow once","kind":"allow_once"}]}`, `{"outcome":{"outcome":"selected","optionId":"allow_once"}}`},
		{"terminal_create", `{"command":"run_scenario","scenario":"full"}`, "terminal/create", `{"sessionId":"s","command":"printf"}`, `{"terminalId":"testy-terminal-client"}`},
		{"terminal_output", `{"command":"run_scenario","scenario":"callbacks"}`, "terminal/output", `{"sessionId":"s","terminalId":"testy-terminal-client"}`, `{"output":"terminal output","truncated":false,"exitStatus":{"exitCode":0}}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			handler := &cancellingCallbackClient{permissions: make(chan []acp.PermissionOption, 1)}
			ctx, client, peer := clientPeer(t, handler)
			handler.client = client
			// All four Rust source cases use InitializeRequest::new(V1) with
			// default capabilities, not explicit fs/terminal advertisement.
			prepareClientSession(t, ctx, client, peer, nil)
			pendingPrompt := startCall(func() (*acp.PromptResponse, error) {
				return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "s", Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: scenario.prompt}}})
			})
			prompt := peer.read(t)
			if prompt.Method != "session/prompt" || len(prompt.ID) == 0 {
				t.Fatalf("expected pending prompt before callback: %+v", prompt)
			}
			expected, _ := json.Marshal(map[string]any{"sessionId": "s", "prompt": []any{map[string]any{"type": "text", "text": scenario.prompt}}})
			assertJSON(t, prompt.Params, string(expected))
			// Withhold the prompt response until cancel and callback reply have
			// both progressed; idle-only cancellation would not test this condition.
			peer.send(t, map[string]any{"jsonrpc": "2.0", "id": "callback", "method": scenario.method, "params": json.RawMessage(scenario.params)})
			cancel := peer.read(t)
			if cancel.Method != "session/cancel" || len(cancel.ID) != 0 {
				t.Fatalf("callback did not send cancel before replying: %+v", cancel)
			}
			assertJSON(t, cancel.Params, `{"sessionId":"s"}`)
			response := peer.read(t)
			if response.Error != nil || string(response.ID) != `"callback"` {
				t.Fatalf("callback response = %+v", response)
			}
			assertJSON(t, response.Result, scenario.response)
			if scenario.method == "session/request_permission" {
				assertJSON(t, await(t, handler.permissions), `[{"optionId":"allow_once","name":"Allow once","kind":"allow_once"}]`)
			}
			peer.result(t, prompt, map[string]any{"stopReason": "cancelled"})
			completed := valueOf(t, pendingPrompt)
			if completed == nil || completed.StopReason != acp.StopReasonCancelled {
				t.Fatalf("pending prompt result = %#v, want cancelled fixture response", completed)
			}
		})
	}
}

// Rust: src/agent-client-protocol-test/tests/testy.rs::testy_session_updates_scenario_emits_every_update_variant.
// Ports receipt/typed dispatch of every expected variant, not Testy's app emission policy.
func TestTestySessionUpdateVariants(t *testing.T) {
	handler := newUpdateHandler()
	ctx, client, peer := clientPeer(t, handler)
	prepareClientSession(t, ctx, client, peer, nil)
	result := startCall(func() (*acp.PromptResponse, error) {
		return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "s", Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "session_updates"}}})
	})
	prompt := peer.read(t)
	updates := []string{
		`{"sessionUpdate":"session_info_update","title":"Testy deterministic session","updatedAt":"2026-01-01T00:00:00Z"}`,
		`{"sessionUpdate":"current_mode_update","currentModeId":"chat"}`,
		`{"sessionUpdate":"config_option_update","configOptions":[]}`,
		`{"sessionUpdate":"available_commands_update","availableCommands":[]}`,
		`{"sessionUpdate":"usage_update","used":128,"size":4096,"cost":{"amount":0,"currency":"USD"}}`,
		`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"received 1 prompt content block(s)"}}`,
		`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking deterministically"}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"text content block"}}`,
		`{"sessionUpdate":"tool_call","toolCallId":"testy-tool-read","title":"Read deterministic fixture","kind":"read","status":"pending"}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"testy-tool-read","status":"in_progress"}`,
		`{"sessionUpdate":"plan","entries":[{"content":"Emit session updates","priority":"high","status":"completed"}]}`,
	}
	for _, update := range updates {
		peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s", "update": json.RawMessage(update)}})
	}
	peer.result(t, prompt, map[string]any{"stopReason": "end_turn"})
	if valueOf(t, result).StopReason != acp.StopReasonEndTurn {
		t.Fatal("wrong stop reason")
	}
	seen := make(map[acp.SessionUpdateType]bool)
	for range updates {
		update := await(t, handler.updates)
		if update.SessionID != "s" {
			t.Fatalf("update for wrong session: %q", update.SessionID)
		}
		seen[update.Update.SessionUpdate] = true
	}
	for _, expected := range []acp.SessionUpdateType{acp.SessionUpdateTypeSessionInfoUpdate, acp.SessionUpdateTypeCurrentModeUpdate, acp.SessionUpdateTypeConfigOptionUpdate, acp.SessionUpdateTypeAvailableCommandsUpdate, acp.SessionUpdateTypeUsageUpdate, acp.SessionUpdateTypeUserMessageChunk, acp.SessionUpdateTypeAgentThoughtChunk, acp.SessionUpdateTypeAgentMessageChunk, acp.SessionUpdateTypeToolCall, acp.SessionUpdateTypeToolCallUpdate, acp.SessionUpdateTypePlan} {
		if !seen[expected] {
			t.Errorf("missing update variant %q", expected)
		}
	}
}
