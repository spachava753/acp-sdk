// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

// Package lifecycle_test checks how ACP connections behave over their
// lifetime: serving over the built-in transports, request cancellation in both
// directions, the end of input and read failures, Close draining accepted
// output, half-closed streams, operation contexts on blocked writes, and the
// context that bounds RunAgent. Most tests drive the SDK through its public
// API against the independent peer in conformance/internal/acptest, inside
// testing/synctest bubbles so "nothing else happened" checks run once every
// goroutine has settled. Tests whose behavior depends on the stream itself,
// such as a write blocked on a slow reader or a socket with one half shut
// down, use real pipes, net.Pipe, or TCP instead.
package lifecycle_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"testing/synctest"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// stdioAgentEnv makes the test binary act as an echo agent on its standard
// streams, for TestStdioAgent.
const stdioAgentEnv = "ACP_LIFECYCLE_STDIO_AGENT"

func TestMain(m *testing.M) {
	if os.Getenv(stdioAgentEnv) != "1" {
		os.Exit(m.Run())
	}
	mux := acp.NewExtensionMux()
	err := acp.AddExtensionRequest(mux, echoMethod, func(_ context.Context, p *echoParams) (*echoResult, error) {
		return &echoResult{Result: "echo: " + p.Message}, nil
	})
	if err == nil {
		err = acp.RunAgent(context.Background(), &acp.StdioTransport{}, func(*acp.AgentConnection) any { return mux })
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

// An agent served over StdioTransport in a child process answers a request on
// its stdout and exits cleanly when its stdin closes.
func TestStdioAgent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), acptest.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), stdioAgentEnv+"=1")
	var stderr bytes.Buffer // read only after Wait returns
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := acptest.Go(func() (struct{}, error) { return struct{}{}, cmd.Wait() })
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			acptest.Await(t, exited)
		}
	}()

	if _, err := fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":"stdio","method":%q,"params":{"message":"stdio"}}`+"\n", echoMethod); err != nil {
		t.Fatal(err)
	}
	line := acptest.Value(t, acptest.Go(func() ([]byte, error) { return bufio.NewReader(stdout).ReadBytes('\n') }))
	acptest.EqualJSON(t, line, `{"jsonrpc":"2.0","id":"stdio","result":{"result":"echo: stdio"}}`)

	_ = stdin.Close()
	waited = true
	if err := acptest.Await(t, exited).Err; err != nil {
		t.Fatalf("agent exit after stdin closed = %v; stderr: %s", err, stderr.String())
	}
}

// An agent served over one end of NewInMemoryTransports answers a request
// written to the other end, and RunAgent returns nil once that end closes.
func TestInMemoryAgent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clientSide, agentSide := acp.NewInMemoryTransports()
		peer, err := clientSide.Connect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer peer.Close()
		_, serving := serve(t, context.Background(), agentSide, echoMux(t))

		id, err := jsonrpc.MakeID("memory")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), acptest.Timeout)
		defer cancel()
		if err := peer.Write(ctx, &jsonrpc.Request{ID: id, Method: echoMethod, Params: acptest.Marshal(t, echoParams{Message: "memory"})}); err != nil {
			t.Fatal(err)
		}
		m, err := peer.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		data, err := jsonrpc.EncodeMessage(m)
		if err != nil {
			t.Fatal(err)
		}
		acptest.EqualJSON(t, data, `{"jsonrpc":"2.0","id":"memory","result":{"result":"echo: memory"}}`)

		_ = peer.Close()
		if err := acptest.Await(t, serving); err != nil {
			t.Errorf("RunAgent after the peer closed = %v, want nil", err)
		}
	})
}

// A client initializes, makes a further call, and sends a notification on the
// same connection, each reaching the peer as written and nothing more.
func TestRequestsThenNotification(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, peer := acptest.StartClient(t, echoMux(t))
		if v := acptest.InitializeClient(t, client, peer, nil, nil).ProtocolVersion; v != 1 {
			t.Errorf("initialized protocol version = %d, want 1", v)
		}

		result := acptest.Go(func() ([]byte, error) { return client.CallExtension(context.Background(), echoMethod, struct{}{}) })
		peer.Reply(peer.Expect(echoMethod).ID, map[string]any{"status": "ok"})
		acptest.EqualJSON(t, acptest.Value(t, result), `{"status":"ok"}`)

		if err := client.NotifyExtension(context.Background(), "_test/status", map[string]any{"message": "hello"}); err != nil {
			t.Fatal(err)
		}
		acptest.EqualJSON(t, peer.Next().Line, `{"jsonrpc":"2.0","method":"_test/status","params":{"message":"hello"}}`)
		expectNoFrames(peer)
	})
}

// A request gets exactly one reply, carrying the handler's result or its
// error unchanged, and the connection keeps serving afterwards.
func TestHandlerRepliesOnce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fail  bool
		reply string
	}{
		{"result", false, `{"jsonrpc":"2.0","id":1,"result":{"answered":true}}`},
		{"error", true, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"error":{"code":%d,"message":"Invalid params","data":"handler error"}}`, jsonrpc.CodeInvalidParams)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				mux := acp.NewExtensionMux()
				if err := acp.AddExtensionRequest(mux, "_test/answer", func(context.Context, *struct{}) (*map[string]bool, error) {
					if tc.fail {
						return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Invalid params", Data: []byte(`"handler error"`)}
					}
					return &map[string]bool{"answered": true}, nil
				}); err != nil {
					t.Fatal(err)
				}
				if err := acp.AddExtensionRequest(mux, "_test/ping", func(context.Context, *struct{}) (*string, error) {
					pong := "pong"
					return &pong, nil
				}); err != nil {
					t.Fatal(err)
				}
				agent, peer := acptest.StartAgent(t, mux)

				acptest.EqualJSON(t, peer.Call(1, "_test/answer", struct{}{}).Line, tc.reply)
				for _, id := range []int{2, 3} {
					acptest.EqualJSON(t, peer.Call(id, "_test/ping", struct{}{}).Line, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":"pong"}`, id))
				}
				expectNoFrames(peer)
				peer.CloseInput()
				if err := agent.Wait(t); err != nil {
					t.Errorf("RunAgent = %v, want nil", err)
				}
			})
		})
	}
}
