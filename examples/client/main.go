// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/spachava753/acp-sdk/acp"
)

type clientHandler struct{}

func (clientHandler) SessionUpdate(_ context.Context, params *acp.SessionNotification) error {
	fmt.Printf("session %s update: %s\n", params.SessionID, params.Update.SessionUpdate)
	if content, ok := params.Update.Content.(map[string]any); ok {
		if text, ok := content["text"].(string); ok {
			fmt.Println(text)
		}
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s /path/to/acp-agent [args...]", os.Args[0])
	}
	ctx := context.Background()
	client, err := acp.Connect(ctx, &acp.CommandTransport{Command: exec.Command(os.Args[1], os.Args[2:]...)}, clientHandler{})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if _, err := client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersion}); err != nil {
		log.Fatal(err)
	}
	session, err := client.NewSession(ctx, &acp.NewSessionRequest{CWD: mustGetwd(), MCPServers: []acp.MCPServer{}})
	if err != nil {
		log.Fatal(err)
	}
	result, err := client.Prompt(ctx, &acp.PromptRequest{
		SessionID: session.SessionID,
		Prompt:    []acp.ContentBlock{{Type: acp.ContentTypeText, Text: "Hello"}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("stop reason:", result.StopReason)
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}
