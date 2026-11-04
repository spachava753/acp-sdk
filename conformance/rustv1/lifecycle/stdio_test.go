// Copyright 2026 ACP Go SDK contributors.
// Rust SDK semantic ports, Apache-2.0; provenance and policy labels: mapping.json.
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
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

const stdioChildEnv = "ACP_RUST_V1_LIFECYCLE_STDIO_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(stdioChildEnv) != "1" {
		os.Exit(m.Run())
	}
	// Only application echo behavior is supplied. Stdio framing, dispatch,
	// connection lifetime, and stream closure remain the real Go SDK's job.
	mux := acp.NewExtensionMux()
	err := acp.AddExtensionRequest(mux, probeMethod, func(_ context.Context, p *struct {
		Message string `json:"message"`
	}) (*struct {
		Result string `json:"result"`
	}, error) {
		return &struct {
			Result string `json:"result"`
		}{Result: "echo: " + p.Message}, nil
	})
	if err == nil {
		err = acp.RunAgent(context.Background(), &acp.StdioTransport{}, func(*acp.AgentConnection) any { return mux })
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	// Do not run testing's normal stdout reporter inside an ACP subprocess.
	os.Exit(0)
}

// Rust: src/jsonrpc.rs:7230, ByteStreams::new(stdout,stdin) server example.
// Execute the real public StdioTransport in an owned subprocess, rather than
// borrowing the user's stdin/stdout or simulating framing in a transport mock.
func TestDocumentedStandardStreamConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), stdioChildEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan struct{})
	var reading <-chan struct{}
	var waitError error
	go func() { waitError = cmd.Wait(); close(waited) }()
	t.Cleanup(func() {
		_ = input.Close()
		_ = cmd.Process.Kill() // Only this test-owned child, including on RED.
		select {
		case <-waited:
		case <-time.After(3 * time.Second):
			t.Error("owned stdio child did not reap after kill")
		}
		if reading != nil {
			select {
			case <-reading:
			case <-time.After(3 * time.Second):
				t.Error("stdio response reader did not join after owned child exit")
			}
		}
	})
	if _, err := fmt.Fprintf(input, "{\"jsonrpc\":\"2.0\",\"id\":\"stdio\",\"method\":%q,\"params\":{\"message\":\"stdio\"}}\n", probeMethod); err != nil {
		t.Fatal(err)
	}
	type lineResult struct {
		line []byte
		err  error
	}
	line := make(chan lineResult, 1)
	readDone := make(chan struct{})
	reading = readDone
	go func() {
		defer close(readDone)
		data, err := bufio.NewReader(output).ReadBytes('\n')
		line <- lineResult{data, err}
	}()
	select {
	case result := <-line:
		if result.err != nil {
			t.Fatalf("stdio response failed: %v", result.err)
		}
		message, err := jsonrpc.DecodeMessage(result.line)
		if err != nil {
			t.Fatal(err)
		}
		response, ok := message.(*jsonrpc.Response)
		if !ok || response.ID.Raw() != "stdio" {
			t.Fatalf("stdio response = %#v", message)
		}
		success(t, response, `{"result":"echo: stdio"}`)
	case <-ctx.Done():
		t.Fatal("stdio response did not finish before test-owned process deadline")
	}
	_ = input.Close()
	select {
	case <-waited:
		if waitError != nil {
			t.Fatalf("stdio EOF did not stop the owned agent cleanly: %v; stderr=%s", waitError, stderr.String())
		}
	case <-ctx.Done():
		t.Fatal("stdio agent did not finish after input EOF")
	}
}

// Rust: src/jsonrpc.rs:7332, Channel::duplex server example. Go's native
// NewInMemoryTransports pair is stream-based, not an unbounded frame Channel;
// the constructor/serving behavior is portable, its queue/driver contract is not.
func TestDocumentedInMemoryConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clientTransport, agentTransport := acp.NewInMemoryTransports()
		peer, err := clientTransport.Connect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_, done := startAgent(t, agentTransport, echoMux(t))
		defer func() { _ = peer.Close(); synctest.Wait() }()
		id, err := jsonrpc.MakeID("memory")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := peer.Write(ctx, &jsonrpc.Request{ID: id, Method: probeMethod, Params: []byte(`{"message":"memory"}`)}); err != nil {
			t.Fatal(err)
		}
		message, err := peer.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		response, ok := message.(*jsonrpc.Response)
		if !ok || response.ID.Raw() != "memory" {
			t.Fatalf("in-memory response = %#v", message)
		}
		success(t, response, `{"result":"echo: memory"}`)
		_ = peer.Close()
		synctest.Wait()
		if err := receive(t, done); err != nil {
			t.Errorf("in-memory serving did not finish on EOF: %v", err)
		}
	})
}
