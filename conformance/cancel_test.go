// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package conformance_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

func TestConformanceCancelReachesAgentDuringPendingPrompt(t *testing.T) {
	agent := newCancelDuringPromptAgent()
	client, done := connectConformanceClient(t, newConformanceClient(), func(*acp.AgentConnection) any {
		return agent
	})
	defer closeConformanceClient(t, client, done)

	promptDone := make(chan promptResult, 1)
	go func() {
		res, err := client.Prompt(t.Context(), &acp.PromptRequest{
			SessionID: acp.SessionId("sess-cancel"),
			Prompt:    []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "wait"}},
		})
		promptDone <- promptResult{res: res, err: err}
	}()

	select {
	case <-agent.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt to start")
	}
	select {
	case result := <-promptDone:
		t.Fatalf("prompt completed before cancel: %#v", result)
	default:
	}
	if err := client.Cancel(t.Context(), &acp.CancelNotification{SessionID: acp.SessionId("sess-cancel")}); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-promptDone:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.res.StopReason != acp.StopReasonCancelled {
			t.Fatalf("stop reason = %q, want %q", result.res.StopReason, acp.StopReasonCancelled)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancelled prompt")
	}
	if got := agent.cancelledSession.Load(); got != acp.SessionId("sess-cancel") {
		t.Fatalf("cancelled session = %q, want sess-cancel", got)
	}
}

type cancelDuringPromptAgent struct {
	noopSessionHandler
	started          chan struct{}
	cancelled        chan struct{}
	startOnce        sync.Once
	cancelOnce       sync.Once
	cancelledSession atomic.Value
}

func newCancelDuringPromptAgent() *cancelDuringPromptAgent {
	return &cancelDuringPromptAgent{started: make(chan struct{}), cancelled: make(chan struct{})}
}

func (a *cancelDuringPromptAgent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion(1), AuthMethods: []acp.AuthMethod{}}, nil
}

func (a *cancelDuringPromptAgent) NewSession(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: acp.SessionId("sess-cancel")}, nil
}

func (a *cancelDuringPromptAgent) Prompt(ctx context.Context, _ *acp.PromptRequest) (*acp.PromptResponse, error) {
	a.startOnce.Do(func() { close(a.started) })
	select {
	case <-a.cancelled:
		return &acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Second):
		return nil, errors.New("cancel notification did not arrive while prompt was pending")
	}
}

func (a *cancelDuringPromptAgent) Cancel(_ context.Context, params *acp.CancelNotification) error {
	a.cancelledSession.Store(params.SessionID)
	a.cancelOnce.Do(func() { close(a.cancelled) })
	return nil
}

type promptResult struct {
	res *acp.PromptResponse
	err error
}
