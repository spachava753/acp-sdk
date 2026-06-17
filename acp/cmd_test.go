// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestCommandTransport(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestCommandTransportHelper", "--")
	cmd.Env = append(os.Environ(), "ACP_COMMAND_TRANSPORT_HELPER=1")

	client, err := Connect(t.Context(), &CommandTransport{Command: cmd}, newTestClientHandler())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	init, err := client.Initialize(t.Context(), &InitializeRequest{ProtocolVersion: ProtocolVersion(1)})
	if err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != ProtocolVersion(1) {
		t.Fatalf("protocol version = %d, want %d", init.ProtocolVersion, ProtocolVersion(1))
	}
	session, err := client.NewSession(t.Context(), &NewSessionRequest{Cwd: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "helper-session" {
		t.Fatalf("session ID = %q, want helper-session", session.SessionID)
	}
}

func TestCommandTransportHelper(t *testing.T) {
	if os.Getenv("ACP_COMMAND_TRANSPORT_HELPER") != "1" {
		return
	}
	err := RunAgent(context.Background(), &StdioTransport{}, func(conn *AgentConnection) any {
		return &commandTransportAgent{conn: conn}
	})
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

type commandTransportAgent struct {
	noopSessionHandler
	conn *AgentConnection
}

func (a *commandTransportAgent) Initialize(context.Context, *InitializeRequest) (*InitializeResponse, error) {
	return &InitializeResponse{ProtocolVersion: ProtocolVersion(1)}, nil
}

func (a *commandTransportAgent) NewSession(context.Context, *NewSessionRequest) (*NewSessionResponse, error) {
	return &NewSessionResponse{SessionID: "helper-session"}, nil
}

func (a *commandTransportAgent) Prompt(context.Context, *PromptRequest) (*PromptResponse, error) {
	return &PromptResponse{StopReason: StopReasonEndTurn}, nil
}

func (a *commandTransportAgent) Cancel(context.Context, *CancelNotification) error { return nil }
