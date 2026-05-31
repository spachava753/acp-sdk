// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestProtocolValuesValidateAgainstACPSchema(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		value    any
	}{
		{
			name:     "initialize request",
			typeName: "InitializeRequest",
			value: InitializeRequest{
				ProtocolVersion: ProtocolVersion,
				ClientCapabilities: &ClientCapabilities{
					FS:       &FileSystemCapabilities{ReadTextFile: true},
					Terminal: true,
				},
				ClientInfo: &Implementation{Name: "test-client", Version: "1.0.0"},
			},
		},
		{
			name:     "initialize response",
			typeName: "InitializeResponse",
			value: InitializeResponse{
				ProtocolVersion: ProtocolVersion,
				AgentCapabilities: &AgentCapabilities{
					PromptCapabilities: &PromptCapabilities{Image: true, EmbeddedContext: true},
					MCPCapabilities:    &MCPCapabilities{HTTP: true, SSE: true},
				},
				AgentInfo:   &Implementation{Name: "test-agent", Version: "1.0.0"},
				AuthMethods: []AuthMethod{{ID: "oauth", Name: "OAuth"}},
			},
		},
		{
			name:     "new session request",
			typeName: "NewSessionRequest",
			value: NewSessionRequest{
				CWD: "/repo",
				MCPServers: []MCPServer{
					{Name: "stdio-tools", Command: "tools-server"},
					{Type: "http", Name: "http-tools", URL: "https://example.test/mcp"},
					{Type: "sse", Name: "sse-tools", URL: "https://example.test/sse"},
				},
			},
		},
		{
			name:     "prompt request",
			typeName: "PromptRequest",
			value: PromptRequest{SessionID: "s1", Prompt: []ContentBlock{
				{Type: ContentTypeText, Text: "hello"},
				{Type: ContentTypeLink, URI: "file:///repo/main.go", Name: "main.go"},
			}},
		},
		{
			name:     "list sessions response with session item",
			typeName: "ListSessionsResponse",
			value:    ListSessionsResponse{Sessions: []SessionInfo{{SessionID: "s1", CWD: "/repo"}}},
		},
		{
			name:     "session mode state",
			typeName: "SessionModeState",
			value:    SessionModeState{CurrentModeID: "code"},
		},
		{
			name:     "stdio mcp server",
			typeName: "McpServer",
			value:    MCPServer{Name: "stdio-tools", Command: "tools-server"},
		},
		{
			name:     "http mcp server",
			typeName: "McpServer",
			value:    MCPServer{Type: "http", Name: "http-tools", URL: "https://example.test/mcp"},
		},
		{
			name:     "sse mcp server",
			typeName: "McpServer",
			value:    MCPServer{Type: "sse", Name: "sse-tools", URL: "https://example.test/sse"},
		},
		{
			name:     "available commands update",
			typeName: "SessionNotification",
			value: SessionNotification{
				SessionID: "s1",
				Update: SessionUpdate{
					SessionUpdate: "available_commands_update",
					AvailableCommands: []AvailableCommand{{
						Name:        "research_codebase",
						Description: "Research the codebase",
						Input:       &AvailableCommandInput{Hint: "topic"},
					}},
				},
			},
		},
		{
			name:     "plan update",
			typeName: "SessionNotification",
			value: SessionNotification{
				SessionID: "s1",
				Update: SessionUpdate{
					SessionUpdate: "plan",
					Entries: []PlanEntry{{
						Content:  "Add schema tests",
						Status:   PlanEntryStatus("pending"),
						Priority: PlanEntryPriority("medium"),
					}},
				},
			},
		},
		{
			name:     "config option update",
			typeName: "SessionNotification",
			value: SessionNotification{
				SessionID: "s1",
				Update: SessionUpdate{
					SessionUpdate: "config_option_update",
					ConfigOptions: []SessionConfigOption{{
						Type:         "select",
						ID:           "model",
						Name:         "Model",
						CurrentValue: "fast",
						Options:      SessionConfigSelectOptions{Flat: []SessionConfigSelectOption{{Value: "fast", Name: "Fast"}}},
					}},
				},
			},
		},
		{
			name:     "flat select config option",
			typeName: "SessionConfigOption",
			value: SessionConfigOption{
				Type:         "select",
				ID:           "model",
				Name:         "Model",
				CurrentValue: "fast",
				Options:      SessionConfigSelectOptions{Flat: []SessionConfigSelectOption{{Value: "fast", Name: "Fast"}}},
			},
		},
		{
			name:     "grouped select config option",
			typeName: "SessionConfigOption",
			value: SessionConfigOption{
				Type:         "select",
				ID:           "model",
				Name:         "Model",
				CurrentValue: "fast",
				Options: SessionConfigSelectOptions{Groups: []SessionConfigSelectGroup{{
					Group:   "recommended",
					Name:    "Recommended",
					Options: []SessionConfigSelectOption{{Value: "fast", Name: "Fast"}},
				}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validateACPValue(t, tt.typeName, tt.value)
		})
	}
}

func TestInvalidProtocolValuesFailACPSchema(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		data     string
	}{
		{
			name:     "select config missing current value and options",
			typeName: "SessionConfigOption",
			data:     `{"type":"select","id":"model","name":"Model"}`,
		},
		{
			name:     "available command input missing hint",
			typeName: "AvailableCommandInput",
			data:     `{}`,
		},
		{
			name:     "prompt request missing prompt array",
			typeName: "PromptRequest",
			data:     `{"sessionId":"s1"}`,
		},
		{
			name:     "list sessions item missing cwd",
			typeName: "ListSessionsResponse",
			data:     `{"sessions":[{"sessionId":"s1"}]}`,
		},
		{
			name:     "plan entry missing priority",
			typeName: "SessionNotification",
			data:     `{"sessionId":"s1","update":{"sessionUpdate":"plan","entries":[{"content":"do it","status":"pending"}]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var value any
			if err := json.Unmarshal([]byte(tt.data), &value); err != nil {
				t.Fatal(err)
			}
			if err := acpSchema(t, tt.typeName).Validate(value); err == nil {
				t.Fatal("schema validation succeeded, want failure")
			}
		})
	}
}

func validateACPValue(t *testing.T, typeName string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if err := acpSchema(t, typeName).Validate(raw); err != nil {
		t.Fatalf("%s does not validate against %s: %v\n%s", t.Name(), typeName, err, data)
	}
}

func acpSchema(t *testing.T, typeName string) *jsonschema.Resolved {
	t.Helper()
	schemaData, err := os.ReadFile("testdata/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schemaDoc jsonschema.Schema
	if err := json.Unmarshal(schemaData, &schemaDoc); err != nil {
		t.Fatal(err)
	}
	schema, err := (&jsonschema.Schema{
		Schema: schemaDoc.Schema,
		Defs:   schemaDoc.Defs,
		Ref:    "#/$defs/" + typeName,
	}).Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}
