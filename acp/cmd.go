// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

var defaultTerminateDuration = 5 * time.Second

// CommandTransport starts a subprocess and speaks ACP over its stdin/stdout.
type CommandTransport struct {
	Command *exec.Cmd

	// TerminateDuration is the grace period before SIGTERM and again before
	// SIGKILL. It begins when the transport closes, not while an RPC connection
	// is still draining accepted work. A nonpositive value uses five seconds.
	TerminateDuration time.Duration
}

// Connect starts the command and returns an ACP connection to its stdio streams.
func (t *CommandTransport) Connect(ctx context.Context) (Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t.Command == nil {
		return nil, fmt.Errorf("nil command")
	}
	stdout, err := t.Command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := t.Command.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := t.Command.Start(); err != nil {
		return nil, err
	}
	d := t.TerminateDuration
	if d <= 0 {
		d = defaultTerminateDuration
	}
	return newIOConn(&pipeRWC{cmd: t.Command, stdout: io.NopCloser(stdout), stdin: stdin, terminateDuration: d}), nil
}

type pipeRWC struct {
	cmd               *exec.Cmd
	stdout            io.ReadCloser
	stdin             io.WriteCloser
	terminateDuration time.Duration
}

func (s *pipeRWC) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *pipeRWC) Write(p []byte) (int, error) { return s.stdin.Write(p) }

func (s *pipeRWC) SetWriteDeadline(deadline time.Time) error {
	if writer, ok := s.stdin.(writeDeadliner); ok {
		return writer.SetWriteDeadline(deadline)
	}
	return os.ErrNoDeadline
}

func (s *pipeRWC) Close() error {
	stdinErr := s.stdin.Close()
	if errors.Is(stdinErr, os.ErrClosed) {
		stdinErr = nil
	} else if stdinErr != nil {
		stdinErr = fmt.Errorf("closing stdin: %w", stdinErr)
	}
	// Reap and, if needed, terminate even when closing stdin fails. Returning
	// early would leave the process and its pipes behind.
	res := make(chan error, 1)
	go func() { res <- s.cmd.Wait() }()
	wait := func() (error, bool) {
		timer := time.NewTimer(s.terminateDuration)
		defer timer.Stop()
		select {
		case err := <-res:
			return err, true
		case <-timer.C:
			return nil, false
		}
	}
	var termSent, killSent bool
	finish := func(err error) error {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				if (termSent && status.Signal() == syscall.SIGTERM) || (killSent && status.Signal() == syscall.SIGKILL) {
					err = nil
				}
			} else if runtime.GOOS == "windows" && killSent && exit.ExitCode() == 1 {
				// Go's Windows Process.Kill uses TerminateProcess with exit code 1.
				err = nil
			}
		}
		return errors.Join(stdinErr, err)
	}
	if err, ok := wait(); ok {
		return finish(err)
	}
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err == nil {
		termSent = true
		if err, ok := wait(); ok {
			return finish(err)
		}
	}
	if err := s.cmd.Process.Kill(); err != nil {
		if !errors.Is(err, os.ErrProcessDone) {
			return errors.Join(stdinErr, err)
		}
	} else {
		killSent = true
	}
	if err, ok := wait(); ok {
		return finish(err)
	}
	return errors.Join(stdinErr, fmt.Errorf("unresponsive subprocess"))
}
