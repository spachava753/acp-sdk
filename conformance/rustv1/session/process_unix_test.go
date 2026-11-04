//go:build unix

// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
package session_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

// TestSubprocessAgent is a contained peer, not a second SDK implementation.
// The parent owns and can kill this exact exec.Cmd even when a test assertion fails.
func TestSubprocessAgent(t *testing.T) {
	mode := os.Getenv("ACP_RUST_SESSION_HELPER")
	if mode == "" {
		return
	}
	dir := os.Getenv("ACP_RUST_SESSION_MARKERS")
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			os.Exit(93)
		}
	}
	terms := make(chan os.Signal, 1)
	signal.Notify(terms, syscall.SIGTERM)
	write("pid", fmt.Sprint(os.Getpid()))
	if mode == "stdout-eof" || mode == "exit17" {
		os.Stdout.Close()
	}
	if mode == "exit17" {
		io.Copy(io.Discard, os.Stdin)
		fmt.Fprint(os.Stderr, "ACP_TEST_FAILURE_AFTER_STDOUT_EOF")
		os.Exit(17)
	}
	if mode == "eof" {
		go func() {
			<-terms
			write("term", "term")
		}()
		io.Copy(io.Discard, os.Stdin)
		write("pid.eof", "eof")
		os.Exit(0)
	}
	for range terms {
		write("term", "term")
		if mode != "kill" {
			os.Exit(0)
		}
	}
	os.Exit(94)
}

func markerExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func waitMarker(t *testing.T, dir, name string) {
	t.Helper()
	deadline := time.After(operationTimeout)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if markerExists(dir, name) {
			return
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatalf("child did not report marker %q", name)
		}
	}
}

func assertChildExited(t *testing.T, process *os.Process) {
	t.Helper()
	deadline := time.After(operationTimeout)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if process.Signal(syscall.Signal(0)) != nil {
			return
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("owned child remained alive after shutdown completed")
		}
	}
}

func commandPeer(t *testing.T, mode string, grace time.Duration) (*acp.CommandTransport, acp.Connection, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cmd := exec.Command(executable, "-test.run=^TestSubprocessAgent$")
	cmd.Env = append(os.Environ(), "ACP_RUST_SESSION_HELPER="+mode, "ACP_RUST_SESSION_MARKERS="+dir)
	transport := &acp.CommandTransport{Command: cmd, TerminateDuration: grace}
	raw, err := transport.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		done := make(chan struct{})
		go func() { raw.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(operationTimeout):
			t.Error("subprocess raw closer did not finish after forced kill")
		}
	})
	waitMarker(t, dir, "pid")
	return transport, raw, dir
}

// Rust: src/agent-client-protocol/src/acp_agent.rs::tests::graceful_shutdown_child_exiting_on_stdin_eof_never_receives_sigterm.
func TestCommandShutdownOnStdinEOF(t *testing.T) {
	transport, raw, dir := commandPeer(t, "eof", 5*time.Second)
	started := time.Now()
	result := startCall(func() (struct{}, error) { return struct{}{}, raw.Close() })
	if got := await(t, result); got.err != nil {
		t.Fatal(got.err)
	}
	if elapsed := time.Since(started); elapsed >= 5*time.Second {
		t.Fatalf("EOF shutdown took %v, want less than grace", elapsed)
	}
	if markerExists(dir, "term") || !markerExists(dir, "pid.eof") {
		t.Fatal("child must observe stdin EOF without receiving SIGTERM")
	}
	assertChildExited(t, transport.Command.Process)
}

// Rust: src/agent-client-protocol/src/acp_agent.rs::tests::graceful_shutdown_sigterms_child_ignoring_stdin_eof.
func TestCommandShutdownEscalatesToSIGTERM(t *testing.T) {
	transport, raw, dir := commandPeer(t, "term", 200*time.Millisecond)
	result := startCall(func() (struct{}, error) { return struct{}{}, raw.Close() })
	if got := await(t, result); got.err != nil {
		t.Fatal(got.err)
	}
	if !markerExists(dir, "term") {
		t.Fatal("child never received SIGTERM")
	}
	assertChildExited(t, transport.Command.Process)
}

// Rust: src/agent-client-protocol/src/acp_agent.rs::tests::graceful_shutdown_sigkills_child_ignoring_sigterm.
// Go exposes one shared grace, not separate exit/terminate windows. Both windows
// must elapse, SIGTERM must precede SIGKILL, and intentional shutdown is clean
// under the upstream supervisor's policy (not a universal ACP protocol rule).
func TestCommandShutdownEscalatesToSIGKILL(t *testing.T) {
	const grace = 100 * time.Millisecond
	transport, raw, dir := commandPeer(t, "kill", grace)
	started := time.Now()
	result := startCall(func() (struct{}, error) { return struct{}{}, raw.Close() })
	got := await(t, result)
	if !markerExists(dir, "term") {
		t.Error("SIGTERM did not precede SIGKILL")
	}
	if elapsed := time.Since(started); elapsed < 2*grace {
		t.Errorf("SIGKILL was not preceded by both grace windows: %v", elapsed)
	}
	if got.err != nil {
		t.Errorf("intentional SIGKILL shutdown should be clean under Rust supervisor policy: %v", got.err)
	}
	assertChildExited(t, transport.Command.Process)
}

// Rust: src/agent-client-protocol/src/acp_agent.rs::tests::protocol_eof_still_reports_nonzero_child_exit,
// exit-status subcase. Go has no automatic stderr-tail/error-data capture API;
// that separate source assertion is explicitly not claimed by this test.
func TestCommandPreservesNonzeroExitStatusAfterStdoutEOF(t *testing.T) {
	transport, raw, _ := commandPeer(t, "exit17", 100*time.Millisecond)
	result := startCall(func() (struct{}, error) { return struct{}{}, raw.Close() })
	got := await(t, result)
	var exit *exec.ExitError
	if !errors.As(got.err, &exit) || exit.ExitCode() != 17 {
		t.Fatalf("shutdown error = %v, want child exit status 17", got.err)
	}
	assertChildExited(t, transport.Command.Process)
}

type retainedConnection struct{ acp.Connection }

func (t retainedConnection) Connect(context.Context) (acp.Connection, error) {
	return t.Connection, nil
}

// Rust: src/agent-client-protocol/src/acp_agent.rs::tests::protocol_eof_terminates_a_child_that_does_not_exit.
// No Close is initiated until the child has been terminated by stdout EOF handling.
func TestProtocolEOFTerminatesNonexitingChild(t *testing.T) {
	transport, raw, dir := commandPeer(t, "stdout-eof", 100*time.Millisecond)
	client, err := acp.Connect(context.Background(), retainedConnection{raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var closeOnce sync.Once
	closed := make(chan struct{})
	closeClient := func() { closeOnce.Do(func() { go func() { client.Close(); close(closed) }() }) }
	t.Cleanup(func() {
		// commandPeer registered its retained kill/closer first; explicitly force
		// the raw transport here before asking the RPC layer to finish.
		raw.Close()
		closeClient()
		select {
		case <-closed:
		case <-time.After(operationTimeout):
			t.Error("client cleanup did not finish after child termination")
		}
	})
	waitMarker(t, dir, "term")
	closeClient()
	await(t, closed)
	assertChildExited(t, transport.Command.Process)
}
