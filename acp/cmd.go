// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"
)

var defaultTerminateDuration = 5 * time.Second

type CommandTransport struct {
	Command           *exec.Cmd
	TerminateDuration time.Duration
}

func (t *CommandTransport) Connect(ctx context.Context) (Connection, error) {
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

func (s *pipeRWC) Close() error {
	if err := s.stdin.Close(); err != nil {
		return fmt.Errorf("closing stdin: %w", err)
	}
	res := make(chan error, 1)
	go func() { res <- s.cmd.Wait() }()
	wait := func() (error, bool) {
		select {
		case err := <-res:
			return err, true
		case <-time.After(s.terminateDuration):
			return nil, false
		}
	}
	if err, ok := wait(); ok {
		return err
	}
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err == nil {
		if err, ok := wait(); ok {
			return err
		}
	}
	if err := s.cmd.Process.Kill(); err != nil {
		return err
	}
	if err, ok := wait(); ok {
		return err
	}
	return fmt.Errorf("unresponsive subprocess")
}
