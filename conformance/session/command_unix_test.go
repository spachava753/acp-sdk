// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package session_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

// Agent process behaviors for TestHelperProcess.
const (
	exitOnEOF     = "exit-on-eof"    // exits when stdin closes
	exitOnTerm    = "exit-on-term"   // ignores stdin EOF, exits on SIGTERM
	ignoreTerm    = "ignore-term"    // survives SIGTERM until killed
	closeStdout   = "close-stdout"   // closes stdout, then exits on SIGTERM
	exit17OnEOF   = "exit-17-on-eof" // closes stdout, exits 17 when stdin closes
	helperProcess = "GO_WANT_HELPER_PROCESS"
)

// TestHelperProcess is not a real test: it is the agent process that the
// CommandTransport tests start. It reports "ready" once its SIGTERM handler
// is installed, "term" on each SIGTERM and "eof" when stdin closes, one per
// line on file descriptor 3.
func TestHelperProcess(*testing.T) {
	if os.Getenv(helperProcess) != "1" {
		return
	}
	behavior := os.Args[len(os.Args)-1]
	events := os.NewFile(3, "events")
	report := func(event string) {
		if _, err := io.WriteString(events, event+"\n"); err != nil {
			os.Exit(93)
		}
	}
	terms := make(chan os.Signal, 1)
	signal.Notify(terms, syscall.SIGTERM)
	if behavior == closeStdout || behavior == exit17OnEOF {
		_ = os.Stdout.Close()
	}
	report("ready")
	switch behavior {
	case exit17OnEOF:
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(17)
	case exitOnEOF:
		go func() {
			<-terms
			report("term")
		}()
		_, _ = io.Copy(io.Discard, os.Stdin)
		report("eof")
		os.Exit(0)
	}
	for range terms {
		report("term")
		if behavior != ignoreTerm {
			os.Exit(0)
		}
	}
}

// agentProcess is a helper process started through a CommandTransport.
type agentProcess struct {
	transport   *acp.CommandTransport
	eventsWrite *os.File
	events      chan string // closed once the process and its pipe are gone
}

// newAgentProcess prepares a helper process with the given behavior and grace
// period. The caller connects the transport, then calls ready.
func newAgentProcess(t *testing.T, behavior string, grace time.Duration) *agentProcess {
	t.Helper()
	eventsRead, eventsWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "--", behavior)
	cmd.Env = append(os.Environ(), helperProcess+"=1")
	cmd.ExtraFiles = []*os.File{eventsWrite}
	p := &agentProcess{transport: &acp.CommandTransport{Command: cmd, TerminateDuration: grace}, eventsWrite: eventsWrite, events: make(chan string, 8)}
	go func() {
		defer close(p.events)
		defer eventsRead.Close()
		for scanner := bufio.NewScanner(eventsRead); scanner.Scan(); {
			p.events <- scanner.Text()
		}
	}()
	t.Cleanup(func() {
		// Close the parent's copy in case the test failed before ready, then
		// make sure no process outlives a failed test.
		_ = eventsWrite.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	return p
}

// connect starts the process and returns its raw connection. Cleanup kills
// the child and closes the connection again; that Close must still return
// promptly, whether or not the test already closed it.
func (p *agentProcess) connect(t *testing.T) acp.Connection {
	t.Helper()
	conn, err := p.transport.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = p.transport.Command.Process.Kill()
		closed := acptest.Go(func() (struct{}, error) { return struct{}{}, conn.Close() })
		select {
		case <-closed:
		case <-time.After(acptest.Timeout):
			t.Error("Close after killing the child did not return")
		}
	})
	p.ready(t)
	return conn
}

// connected is a transport that hands out an existing connection.
type connected struct{ acp.Connection }

func (c connected) Connect(context.Context) (acp.Connection, error) { return c.Connection, nil }

// ready waits until the started process has installed its SIGTERM handler.
func (p *agentProcess) ready(t *testing.T) {
	t.Helper()
	// Only the child may hold the write end, so the event stream ends when the
	// child exits.
	_ = p.eventsWrite.Close()
	if got := acptest.Await(t, p.events); got != "ready" {
		t.Fatalf("first child event = %q, want ready", got)
	}
}

// exited fails unless the process has exited and been reaped, and returns the
// events it reported after ready.
func (p *agentProcess) exited(t *testing.T) []string {
	t.Helper()
	if err := p.transport.Command.Process.Signal(syscall.Signal(0)); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("child not reaped after shutdown (signal 0: %v)", err)
	}
	var events []string
	timeout := time.After(acptest.Timeout)
	for {
		select {
		case event, ok := <-p.events:
			if !ok {
				return events
			}
			events = append(events, event)
		case <-timeout:
			t.Fatal("child event stream did not end")
			return nil
		}
	}
}

// TestCommandShutdown closes a CommandTransport connection to agents that
// react differently to shutdown. A child that exits on stdin EOF must do so
// within the grace period without receiving SIGTERM. A child that ignores EOF
// must receive SIGTERM, and one that also ignores SIGTERM must be killed only
// after two grace periods. Close must not return until the child has been
// reaped. Shutdown that needed SIGTERM or SIGKILL is clean; a nonzero exit
// status of the child's own is reported.
func TestCommandShutdown(t *testing.T) {
	for _, tc := range []struct {
		behavior string
		grace    time.Duration
		events   []string
		check    func(t *testing.T, err error, elapsed, grace time.Duration)
	}{
		{
			// The child exits on EOF, well before the long grace period, and so
			// never sees SIGTERM.
			behavior: exitOnEOF, grace: 5 * time.Second, events: []string{"eof"},
			check: func(t *testing.T, err error, elapsed, grace time.Duration) {
				if err != nil {
					t.Error(err)
				}
				if elapsed >= grace {
					t.Errorf("shutdown took %v, want less than the %v grace period", elapsed, grace)
				}
			},
		},
		{
			behavior: exitOnTerm, grace: 200 * time.Millisecond, events: []string{"term"},
			check: func(t *testing.T, err error, _, _ time.Duration) {
				if err != nil {
					t.Error(err)
				}
			},
		},
		{
			behavior: ignoreTerm, grace: 100 * time.Millisecond, events: []string{"term"},
			check: func(t *testing.T, err error, elapsed, grace time.Duration) {
				if err != nil {
					t.Errorf("shutdown by SIGKILL returned %v, want nil", err)
				}
				if elapsed < 2*grace {
					t.Errorf("SIGKILL after %v, want both %v grace periods first", elapsed, grace)
				}
			},
		},
		{
			// Closing stdout first leaves only the exit status to report.
			behavior: exit17OnEOF, grace: 100 * time.Millisecond, events: nil,
			check: func(t *testing.T, err error, _, _ time.Duration) {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 17 {
					t.Errorf("shutdown error = %v, want exit status 17", err)
				}
			},
		},
	} {
		t.Run(tc.behavior, func(t *testing.T) {
			p := newAgentProcess(t, tc.behavior, tc.grace)
			conn := p.connect(t)
			start := time.Now()
			result := acptest.Go(func() (struct{}, error) { return struct{}{}, conn.Close() })
			err := acptest.Await(t, result).Err
			tc.check(t, err, time.Since(start), tc.grace)
			if events := p.exited(t); !slices.Equal(events, tc.events) {
				t.Errorf("child events after ready = %q, want %q", events, tc.events)
			}
		})
	}
}

// TestCommandStdoutEOF checks that when a child closes its stdout but keeps
// running, the connection treats the EOF as the end of the session: it shuts
// the child down, with SIGTERM once the child ignores stdin EOF, before the
// client is closed.
func TestCommandStdoutEOF(t *testing.T) {
	p := newAgentProcess(t, closeStdout, 100*time.Millisecond)
	client, err := acp.Connect(context.Background(), connected{p.connect(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := acptest.Await(t, p.events); got != "term" {
		t.Fatalf("child event = %q, want term", got)
	}
	closed := acptest.Go(func() (struct{}, error) { return struct{}{}, client.Close() })
	acptest.Await(t, closed)
	if events := p.exited(t); len(events) != 0 {
		t.Errorf("unexpected child events after term: %q", events)
	}
}
