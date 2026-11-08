//go:build unix

// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.
package acp_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

// The ported TERM helper exits zero after handling TERM. Verify a real default
// signal exit is also intentional, without suppressing a nonzero exit chosen
// by a handler (or by the child on stdin EOF).
func TestCommandTransportSignalExitPolicy(t *testing.T) {
	for _, tc := range []struct {
		mode string
		code int
	}{
		{"default-term", 0},
		{"nonzero-term", 23},
		{"nonzero-eof", 17},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.Command(os.Args[0], "-test.run=^TestCommandTransportSignalHelper$")
			cmd.Env = append(os.Environ(), "ACP_COMMAND_SIGNAL_HELPER="+tc.mode)
			raw, err := (&acp.CommandTransport{Command: cmd, TerminateDuration: 25 * time.Millisecond}).Connect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var closeErr error
			closing := false
			defer func() {
				_ = cmd.Process.Kill() // Only this test-owned child.
				if !closing {
					closing = true
					go func() { closeErr = raw.Close(); close(done) }()
				}
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("owned command did not join after forced cleanup")
				}
			}()
			// The child is ready before Close starts either grace window.
			if _, err := raw.Read(ctx); err != nil {
				t.Fatal(err)
			}
			closing = true
			go func() { closeErr = raw.Close(); close(done) }()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("command shutdown did not finish")
			}
			if tc.code == 0 {
				if closeErr != nil {
					t.Fatalf("requested SIGTERM exit was reported as failure: %v", closeErr)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(closeErr, &exit) || exit.ExitCode() != tc.code {
					t.Fatalf("child-selected nonzero exit = %v, want status%d", closeErr, tc.code)
				}
			}
			if cmd.ProcessState == nil {
				t.Error("command was not reaped")
			}
		})
	}
}

func TestCommandTransportSignalHelper(t *testing.T) {
	mode := os.Getenv("ACP_COMMAND_SIGNAL_HELPER")
	if mode == "" {
		return
	}
	var terms chan os.Signal
	if mode == "nonzero-term" {
		terms = make(chan os.Signal, 1)
		signal.Notify(terms, syscall.SIGTERM)
	}
	fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","method":"_test/ready"}`)
	if mode == "nonzero-eof" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(17)
	}
	if mode == "nonzero-term" {
		<-terms
		os.Exit(23)
	}
	// A timer keeps the helper alive without installing a TERM handler.
	for {
		time.Sleep(time.Hour)
	}
}
