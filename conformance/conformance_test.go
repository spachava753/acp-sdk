// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package conformance_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

type conformanceClient struct {
	mu        sync.Mutex
	files     map[string]string
	updates   []acp.SessionNotification
	terminals []string
}

func newConformanceClient() *conformanceClient {
	return &conformanceClient{files: map[string]string{}}
}

func (c *conformanceClient) SessionUpdate(_ context.Context, params *acp.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, *params)
	return nil
}

func (c *conformanceClient) ReadTextFile(_ context.Context, params *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &acp.ReadTextFileResponse{Content: c.files[params.Path]}, nil
}

func (c *conformanceClient) WriteTextFile(_ context.Context, params *acp.WriteTextFileRequest) (*acp.WriteTextFileResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.files[params.Path] = params.Content
	return &acp.WriteTextFileResponse{}, nil
}

func (c *conformanceClient) RequestPermission(context.Context, *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	return &acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{Outcome: "selected", OptionID: "allow"}}, nil
}

func (c *conformanceClient) CreateTerminal(_ context.Context, params *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.terminals = append(c.terminals, params.Command)
	return &acp.CreateTerminalResponse{TerminalID: "term-1"}, nil
}

func (c *conformanceClient) TerminalOutput(context.Context, *acp.TerminalOutputRequest) (*acp.TerminalOutputResponse, error) {
	return &acp.TerminalOutputResponse{Output: "ok"}, nil
}

func (c *conformanceClient) WaitForTerminalExit(context.Context, *acp.WaitForTerminalExitRequest) (*acp.WaitForTerminalExitResponse, error) {
	return &acp.WaitForTerminalExitResponse{}, nil
}

func (c *conformanceClient) KillTerminal(context.Context, *acp.KillTerminalRequest) (*acp.KillTerminalResponse, error) {
	return &acp.KillTerminalResponse{}, nil
}

func (c *conformanceClient) ReleaseTerminal(context.Context, *acp.ReleaseTerminalRequest) (*acp.ReleaseTerminalResponse, error) {
	return &acp.ReleaseTerminalResponse{}, nil
}

func (c *conformanceClient) fileContent(path string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.files[path]
}

func (c *conformanceClient) createdTerminal() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.terminals) == 0 {
		return ""
	}
	return c.terminals[0]
}

func connectConformanceClient(t *testing.T, handler acp.ClientHandler, newAgent acp.AgentFactory) (*acp.Client, chan error) {
	t.Helper()
	clientTransport, agentTransport := acp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- acp.RunAgent(t.Context(), agentTransport, newAgent) }()
	client, err := acp.Connect(t.Context(), clientTransport, handler)
	if err != nil {
		t.Fatal(err)
	}
	return client, done
}

func closeConformanceClient(t *testing.T, client *acp.Client, done chan error) {
	t.Helper()
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for agent shutdown")
	}
}

func waitForUpdate(t *testing.T, handler *conformanceClient) acp.SessionNotification {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		handler.mu.Lock()
		if len(handler.updates) > 0 {
			update := handler.updates[0]
			handler.mu.Unlock()
			return update
		}
		handler.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for session update")
	return acp.SessionNotification{}
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

func stringPtr(v string) *string { return &v }
func intPtr(v int) *int          { return &v }
func int64Ptr(v int64) *int64    { return &v }
