// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package conformance_test

import "github.com/spachava753/acp-sdk/acp"

type goldenCase struct {
	raw    string
	decode func() any
	value  func() any
}

func goldenFixtures() map[string]goldenCase {
	sess := acp.SessionId("sess_abc123def456")
	return map[string]goldenCase{
		"cancel_notification": {
			raw:    `{"sessionId":"sess_abc123def456"}`,
			decode: func() any { return &acp.CancelNotification{} },
			value:  func() any { return acp.CancelNotification{SessionID: sess} },
		},
		"content_resource_link": {
			raw:    `{"type":"resource_link","uri":"file:///home/user/document.pdf","name":"document.pdf","mimeType":"application/pdf","size":1024000}`,
			decode: func() any { return &acp.ContentBlock{} },
			value: func() any {
				return acp.ContentBlock{Type: acp.ContentBlockTypeResourceLink, URI: stringPtr("file:///home/user/document.pdf"), Name: "document.pdf", MimeType: stringPtr("application/pdf"), Size: int64Ptr(1024000)}
			},
		},
		"content_resource_text": {
			raw:    `{"type":"resource","resource":{"uri":"file:///home/user/script.py","mimeType":"text/x-python","text":"def hello():\n    print('Hello, world!')"}}`,
			decode: func() any { return &acp.ContentBlock{} },
			value: func() any {
				return acp.ContentBlock{Type: acp.ContentBlockTypeResource, Resource: acp.EmbeddedResourceResource{URI: "file:///home/user/script.py", MimeType: stringPtr("text/x-python"), Text: "def hello():\n    print('Hello, world!')"}}
			},
		},
		"content_text": {
			raw:    `{"type":"text","text":"What's the weather like today?"}`,
			decode: func() any { return &acp.ContentBlock{} },
			value: func() any {
				return acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: "What's the weather like today?"}
			},
		},
		"fs_read_text_file_request": {
			raw:    `{"sessionId":"sess_abc123def456","path":"/home/user/project/src/main.py","line":10,"limit":50}`,
			decode: func() any { return &acp.ReadTextFileRequest{} },
			value: func() any {
				return acp.ReadTextFileRequest{SessionID: sess, Path: "/home/user/project/src/main.py", Line: int64Ptr(10), Limit: int64Ptr(50)}
			},
		},
		"fs_read_text_file_response": {
			raw:    `{"content":"def hello_world():\n    print('Hello, world!')\n"}`,
			decode: func() any { return &acp.ReadTextFileResponse{} },
			value: func() any {
				return acp.ReadTextFileResponse{Content: "def hello_world():\n    print('Hello, world!')\n"}
			},
		},
		"fs_write_text_file_request": {
			raw:    `{"sessionId":"sess_abc123def456","path":"/home/user/project/config.json","content":"{\n  \"debug\": true,\n  \"version\": \"1.0.0\"\n}"}`,
			decode: func() any { return &acp.WriteTextFileRequest{} },
			value: func() any {
				return acp.WriteTextFileRequest{SessionID: sess, Path: "/home/user/project/config.json", Content: "{\n  \"debug\": true,\n  \"version\": \"1.0.0\"\n}"}
			},
		},
		"initialize_request": {
			raw:    `{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":true,"writeTextFile":true}}}`,
			decode: func() any { return &acp.InitializeRequest{} },
			value: func() any {
				return acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersion(1), ClientCapabilities: &acp.ClientCapabilities{Fs: &acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true}}}
			},
		},
		"initialize_response": {
			raw:    `{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{},"promptCapabilities":{"image":true,"audio":true,"embeddedContext":true},"sessionCapabilities":{}}}`,
			decode: func() any { return &acp.InitializeResponse{} },
			value: func() any {
				return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion(1), AgentCapabilities: &acp.AgentCapabilities{LoadSession: true, McpCapabilities: &acp.McpCapabilities{}, PromptCapabilities: &acp.PromptCapabilities{Image: true, Audio: true, EmbeddedContext: true}, SessionCapabilities: &acp.SessionCapabilities{}}, AuthMethods: []acp.AuthMethod{}}
			},
		},
		"new_session_request": {
			raw:    `{"cwd":"/home/user/project","mcpServers":[{"name":"filesystem","command":"/path/to/mcp-server","args":["--stdio"]}]}`,
			decode: func() any { return &acp.NewSessionRequest{} },
			value: func() any {
				return acp.NewSessionRequest{Cwd: "/home/user/project", McpServers: []acp.McpServer{{Name: "filesystem", Command: "/path/to/mcp-server", Args: []string{"--stdio"}, Env: []acp.EnvVariable{}}}}
			},
		},
		"new_session_response": {
			raw:    `{"sessionId":"sess_abc123def456"}`,
			decode: func() any { return &acp.NewSessionResponse{} },
			value:  func() any { return acp.NewSessionResponse{SessionID: sess} },
		},
		"permission_outcome_cancelled": {
			raw:    `{"outcome":"cancelled"}`,
			decode: func() any { return &acp.RequestPermissionOutcome{} },
			value:  func() any { return acp.RequestPermissionOutcome{Outcome: "cancelled"} },
		},
		"permission_outcome_selected": {
			raw:    `{"outcome":"selected","optionId":"allow-once"}`,
			decode: func() any { return &acp.RequestPermissionOutcome{} },
			value:  func() any { return acp.RequestPermissionOutcome{Outcome: "selected", OptionID: "allow-once"} },
		},
		"prompt_request": {
			raw:    `{"sessionId":"sess_abc123def456","prompt":[{"type":"text","text":"Can you analyze this code for potential issues?"},{"type":"resource","resource":{"uri":"file:///home/user/project/main.py","mimeType":"text/x-python","text":"def process_data(items):\n    for item in items:\n        print(item)"}}]}`,
			decode: func() any { return &acp.PromptRequest{} },
			value: func() any {
				return acp.PromptRequest{SessionID: sess, Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "Can you analyze this code for potential issues?"}, {Type: acp.ContentBlockTypeResource, Resource: acp.EmbeddedResourceResource{URI: "file:///home/user/project/main.py", MimeType: stringPtr("text/x-python"), Text: "def process_data(items):\n    for item in items:\n        print(item)"}}}}
			},
		},
		"request_permission_request": {
			raw:    `{"sessionId":"sess_abc123def456","toolCall":{"toolCallId":"call_001"},"options":[{"optionId":"allow-once","name":"Allow once","kind":"allow_once"},{"optionId":"reject-once","name":"Reject","kind":"reject_once"}]}`,
			decode: func() any { return &acp.RequestPermissionRequest{} },
			value: func() any {
				return acp.RequestPermissionRequest{SessionID: sess, ToolCall: acp.ToolCallUpdate{ToolCallID: "call_001"}, Options: []acp.PermissionOption{{OptionID: "allow-once", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce}, {OptionID: "reject-once", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce}}}
			},
		},
		"request_permission_response_selected": {
			raw:    `{"outcome":{"outcome":"selected","optionId":"allow-once"}}`,
			decode: func() any { return &acp.RequestPermissionResponse{} },
			value: func() any {
				return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{Outcome: "selected", OptionID: "allow-once"}}
			},
		},
		"session_update_agent_message_chunk":          sessionUpdateGolden(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"The capital of France is Paris."}}`, acp.SessionUpdate{SessionUpdate: "agent_message_chunk", Content: acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: "The capital of France is Paris."}}),
		"session_update_agent_thought_chunk":          sessionUpdateGolden(`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"Thinking about best approach..."}}`, acp.SessionUpdate{SessionUpdate: "agent_thought_chunk", Content: acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: "Thinking about best approach..."}}),
		"session_update_config_option_update":         sessionUpdateGolden(`{"sessionUpdate":"config_option_update","configOptions":[{"type":"select","id":"model","name":"Model","currentValue":"gpt-4o-mini","options":[{"name":"GPT-4o Mini","value":"gpt-4o-mini"},{"name":"GPT-4o","value":"gpt-4o","description":"Highest quality"}]},{"type":"select","id":"effort","name":"Effort","currentValue":"fast","options":[{"group":"speed","name":"Speed","options":[{"name":"Fast","value":"fast"},{"name":"Balanced","value":"balanced"}]},{"group":"quality","name":"Quality","options":[{"name":"High","value":"high"}]}]}]}`, acp.SessionUpdate{SessionUpdate: "config_option_update", ConfigOptions: []acp.SessionConfigOption{{Type: "select", ID: "model", Name: "Model", CurrentValue: acp.SessionConfigValueId("gpt-4o-mini"), Options: acp.SessionConfigSelectOptions{Ungrouped: genericPtr(acp.UngroupedSessionConfigSelectOptions{{Name: "GPT-4o Mini", Value: "gpt-4o-mini"}, {Name: "GPT-4o", Value: "gpt-4o", Description: stringPtr("Highest quality")}})}}, {Type: "select", ID: "effort", Name: "Effort", CurrentValue: acp.SessionConfigValueId("fast"), Options: acp.SessionConfigSelectOptions{Groups: genericPtr(acp.GroupedSessionConfigSelectOptions{{Group: "speed", Name: "Speed", Options: []acp.SessionConfigSelectOption{{Name: "Fast", Value: "fast"}, {Name: "Balanced", Value: "balanced"}}}, {Group: "quality", Name: "Quality", Options: []acp.SessionConfigSelectOption{{Name: "High", Value: "high"}}}})}}}}),
		"session_update_plan":                         sessionUpdateGolden(`{"sessionUpdate":"plan","entries":[{"content":"Check for syntax errors","priority":"high","status":"pending"},{"content":"Identify potential type issues","priority":"medium","status":"pending"}]}`, acp.SessionUpdate{SessionUpdate: "plan", Entries: []acp.PlanEntry{{Content: "Check for syntax errors", Priority: acp.PlanEntryPriority("high"), Status: acp.PlanEntryStatus("pending")}, {Content: "Identify potential type issues", Priority: acp.PlanEntryPriority("medium"), Status: acp.PlanEntryStatus("pending")}}}),
		"session_update_tool_call":                    sessionUpdateGolden(`{"sessionUpdate":"tool_call","toolCallId":"call_001","title":"Reading configuration file","kind":"read","status":"pending"}`, acp.SessionUpdate{SessionUpdate: "tool_call", ToolCallID: "call_001", Title: stringPtr("Reading configuration file"), Kind: genericPtr(acp.ToolKindRead), Status: genericPtr(acp.ToolCallStatusPending)}),
		"session_update_tool_call_edit":               sessionUpdateGolden(`{"sessionUpdate":"tool_call","toolCallId":"call_003","title":"Apply edit","kind":"edit","status":"pending","locations":[{"path":"/home/user/project/src/config.json"}],"rawInput":{"content":"print('hello')","path":"/home/user/project/src/config.json"}}`, acp.SessionUpdate{SessionUpdate: "tool_call", ToolCallID: "call_003", Title: stringPtr("Apply edit"), Kind: genericPtr(acp.ToolKindEdit), Status: genericPtr(acp.ToolCallStatusPending), Locations: genericPtr([]acp.ToolCallLocation{{Path: "/home/user/project/src/config.json"}}), RawInput: map[string]any{"path": "/home/user/project/src/config.json", "content": "print('hello')"}}),
		"session_update_tool_call_locations_rawinput": sessionUpdateGolden(`{"sessionUpdate":"tool_call","toolCallId":"call_lr","title":"Tracking file","locations":[{"path":"/home/user/project/src/config.json"}],"rawInput":{"path":"/home/user/project/src/config.json"}}`, acp.SessionUpdate{SessionUpdate: "tool_call", ToolCallID: "call_lr", Title: stringPtr("Tracking file"), Locations: genericPtr([]acp.ToolCallLocation{{Path: "/home/user/project/src/config.json"}}), RawInput: map[string]any{"path": "/home/user/project/src/config.json"}}),
		"session_update_tool_call_read":               sessionUpdateGolden(`{"sessionUpdate":"tool_call","toolCallId":"call_001","title":"Reading configuration file","kind":"read","status":"pending","locations":[{"path":"/home/user/project/src/config.json"}],"rawInput":{"path":"/home/user/project/src/config.json"}}`, acp.SessionUpdate{SessionUpdate: "tool_call", ToolCallID: "call_001", Title: stringPtr("Reading configuration file"), Kind: genericPtr(acp.ToolKindRead), Status: genericPtr(acp.ToolCallStatusPending), Locations: genericPtr([]acp.ToolCallLocation{{Path: "/home/user/project/src/config.json"}}), RawInput: map[string]any{"path": "/home/user/project/src/config.json"}}),
		"session_update_tool_call_update_content":     sessionUpdateGolden(`{"sessionUpdate":"tool_call_update","toolCallId":"call_001","status":"in_progress","content":[{"type":"content","content":{"type":"text","text":"Found 3 configuration files..."}}]}`, acp.SessionUpdate{SessionUpdate: "tool_call_update", ToolCallID: "call_001", Status: genericPtr(acp.ToolCallStatusInProgress), Content: []acp.ToolCallContent{{Type: acp.ToolCallContentTypeContent, Content: acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: "Found 3 configuration files..."}}}}),
		"session_update_tool_call_update_more_fields": sessionUpdateGolden(`{"sessionUpdate":"tool_call_update","toolCallId":"call_010","title":"Processing changes","kind":"edit","status":"completed","locations":[{"path":"/home/user/project/src/config.json"}],"rawInput":{"path":"/home/user/project/src/config.json"},"rawOutput":{"result":"ok"},"content":[{"type":"content","content":{"type":"text","text":"Edit completed."}}]}`, acp.SessionUpdate{SessionUpdate: "tool_call_update", ToolCallID: "call_010", Title: stringPtr("Processing changes"), Kind: genericPtr(acp.ToolKindEdit), Status: genericPtr(acp.ToolCallStatusCompleted), Locations: genericPtr([]acp.ToolCallLocation{{Path: "/home/user/project/src/config.json"}}), RawInput: map[string]any{"path": "/home/user/project/src/config.json"}, RawOutput: map[string]any{"result": "ok"}, Content: []acp.ToolCallContent{{Type: acp.ToolCallContentTypeContent, Content: acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: "Edit completed."}}}}),
		"session_update_user_message_chunk":           sessionUpdateGolden(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"What's the capital of France?"}}`, acp.SessionUpdate{SessionUpdate: "user_message_chunk", Content: acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: "What's the capital of France?"}}),
		"set_session_config_option_request": {
			raw:    `{"sessionId":"sess_abc123def456","configId":"model","value":"gpt-4o-mini"}`,
			decode: func() any { return &acp.SetSessionConfigOptionRequest{} },
			value: func() any {
				return acp.SetSessionConfigOptionRequest{SessionID: sess, ConfigID: "model", Value: "gpt-4o-mini"}
			},
		},
		"set_session_config_option_response": {
			raw:    `{"configOptions":[{"type":"select","id":"model","name":"Model","currentValue":"balanced","options":[{"name":"Fast","value":"fast"},{"name":"Balanced","value":"balanced"}]}]}`,
			decode: func() any { return &acp.SetSessionConfigOptionResponse{} },
			value: func() any {
				return acp.SetSessionConfigOptionResponse{ConfigOptions: []acp.SessionConfigOption{{Type: "select", ID: "model", Name: "Model", CurrentValue: acp.SessionConfigValueId("balanced"), Options: acp.SessionConfigSelectOptions{Ungrouped: genericPtr(acp.UngroupedSessionConfigSelectOptions{{Name: "Fast", Value: "fast"}, {Name: "Balanced", Value: "balanced"}})}}}}
			},
		},
		"tool_content_content_text": toolContentGolden(`{"type":"content","content":{"type":"text","text":"Analysis complete. Found 3 issues."}}`, acp.ToolCallContent{Type: acp.ToolCallContentTypeContent, Content: acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: "Analysis complete. Found 3 issues."}}),
		"tool_content_diff":         toolContentGolden(`{"type":"diff","path":"/home/user/project/src/config.json","oldText":"{\n  \"debug\": false\n}","newText":"{\n  \"debug\": true\n}"}`, acp.ToolCallContent{Type: acp.ToolCallContentTypeDiff, Path: "/home/user/project/src/config.json", OldText: stringPtr("{\n  \"debug\": false\n}"), NewText: "{\n  \"debug\": true\n}"}),
		"tool_content_diff_no_old":  toolContentGolden(`{"type":"diff","path":"/home/user/project/src/config.json","newText":"{\n  \"debug\": true\n}"}`, acp.ToolCallContent{Type: acp.ToolCallContentTypeDiff, Path: "/home/user/project/src/config.json", NewText: "{\n  \"debug\": true\n}"}),
		"tool_content_terminal":     toolContentGolden(`{"type":"terminal","terminalId":"term_001"}`, acp.ToolCallContent{Type: acp.ToolCallContentTypeTerminal, TerminalID: "term_001"}),
	}
}

func sessionUpdateGolden(raw string, value acp.SessionUpdate) goldenCase {
	return goldenCase{raw: raw, decode: func() any { return &acp.SessionUpdate{} }, value: func() any { return value }}
}

func toolContentGolden(raw string, value acp.ToolCallContent) goldenCase {
	return goldenCase{raw: raw, decode: func() any { return &acp.ToolCallContent{} }, value: func() any { return value }}
}
