// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package conformance_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

func TestConformanceConcurrentPrompts(t *testing.T) {
	agent := &concurrentAgent{}
	client, done := connectConformanceClient(t, newConformanceClient(), func(*acp.AgentConnection) any {
		return agent
	})
	defer closeConformanceClient(t, client, done)

	const n = 8
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, err := client.Prompt(t.Context(), &acp.PromptRequest{
				SessionID: acp.SessionId(fmt.Sprintf("sess-%d", i)),
				Prompt:    []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "hello"}},
			})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := agent.prompts.Load(); got != n {
		t.Fatalf("prompt count = %d, want %d", got, n)
	}
}

type concurrentAgent struct {
	noopSessionHandler
	prompts atomic.Int64
}

func (a *concurrentAgent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion(1), AuthMethods: []acp.AuthMethod{}}, nil
}

func (a *concurrentAgent) NewSession(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: acp.SessionId("sess-123")}, nil
}

func (a *concurrentAgent) Prompt(context.Context, *acp.PromptRequest) (*acp.PromptResponse, error) {
	a.prompts.Add(1)
	time.Sleep(25 * time.Millisecond)
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *concurrentAgent) Cancel(context.Context, *acp.CancelNotification) error { return nil }
