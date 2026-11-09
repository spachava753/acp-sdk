// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"context"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// startSession connects a client with handler, initializes it with
// capabilities and opens session "s".
func startSession(t *testing.T, handler any, capabilities *acp.ClientCapabilities) (*acp.Client, *acptest.Peer) {
	t.Helper()
	client, peer := acptest.StartClient(t, handler)
	if got := acptest.InitializeClient(t, client, peer, capabilities, nil).ProtocolVersion; got != 1 {
		t.Fatalf("selected protocol version = %d, want 1", got)
	}
	acptest.NewSession(t, client, peer, "s")
	return client, peer
}

// prompt sends a single text block to session in the background.
func prompt(client *acp.Client, session acp.SessionId, text string) <-chan acptest.Result[*acp.PromptResponse] {
	return acptest.Go(func() (*acp.PromptResponse, error) {
		return client.SessionPrompt(context.Background(), &acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{acp.TextContentBlock(text)}})
	})
}

// endTurn answers request with an end_turn stop reason.
func endTurn(peer *acptest.Peer, request acptest.Message) {
	peer.Reply(request.ID, map[string]any{"stopReason": "end_turn"})
}

// expectStop awaits a prompt and fails unless it stopped for want.
func expectStop(t *testing.T, result <-chan acptest.Result[*acp.PromptResponse], want acp.StopReason) {
	t.Helper()
	if got := acptest.Value(t, result).StopReason; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
}

// updateRecorder records every session update. The channel is buffered so
// callbacks finish without waiting for the test to read them.
type updateRecorder struct{ updates chan acp.SessionNotification }

func newUpdateRecorder() *updateRecorder {
	return &updateRecorder{updates: make(chan acp.SessionNotification, 16)}
}

func (h *updateRecorder) SessionUpdate(_ context.Context, update *acp.SessionNotification) error {
	h.updates <- *update
	return nil
}

// next returns the next recorded update.
func (h *updateRecorder) next(t *testing.T) acp.SessionNotification {
	t.Helper()
	return acptest.Await(t, h.updates)
}

// text returns the text of an update whose content is a single block.
func text(t *testing.T, update acp.SessionNotification) string {
	t.Helper()
	content, ok := update.Update.Content.(acp.ContentBlock)
	if !ok {
		t.Fatalf("update content type = %T, want ContentBlock", update.Update.Content)
	}
	return content.Text
}

// toolContent returns the tool call content of an update.
func toolContent(update acp.SessionNotification) []acp.ToolCallContent {
	switch content := update.Update.Content.(type) {
	case []acp.ToolCallContent:
		return content
	case *[]acp.ToolCallContent:
		if content != nil {
			return *content
		}
	}
	return nil
}
