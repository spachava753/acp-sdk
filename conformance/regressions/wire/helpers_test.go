// Local ACP Go SDK regressions, not ports of the Rust SDK workspace tests.
package wire_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

const deadline = 2 * time.Second

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// SDK worker panics cannot be recovered on the test goroutine. A child exits
// successfully only when the desired public behavior holds; panics/timeouts
// are failures, not the characterization probes' expected success criteria.
func isolated(t *testing.T) bool {
	t.Helper()
	if os.Getenv("ACP_LOCAL_WIRE_CHILD") == t.Name() {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	parts := strings.Split(t.Name(), "/")
	for i := range parts {
		parts[i] = "^" + regexp.QuoteMeta(parts[i]) + "$"
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+strings.Join(parts, "/"), "-test.count=1", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), "ACP_LOCAL_WIRE_CHILD="+t.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("desired wire behavior failed in child: %v\n%s", err, out)
	}
	return false
}

type envelope map[string]json.RawMessage
type readEvent struct {
	line []byte
	err  error
}

// Separate pipes permit input EOF while output stays readable. The collector
// keeps SDK writes moving and retains every response through orderly shutdown.
type peer struct {
	input           *io.PipeWriter
	inputReader     *io.PipeReader
	output          *io.PipeReader
	outputWriter    *io.PipeWriter
	events          chan readEvent
	readerDone      chan struct{}
	abort           chan struct{}
	abortOnce       sync.Once
	done            chan struct{}
	err             error // read only after done closes
	active          bool
	closeClient     func() error
	clientCloseOnce sync.Once
	frames          []envelope
}

func newPeer(t *testing.T) (*peer, *acp.IOTransport) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	p := &peer{input: inW, inputReader: inR, output: outR, outputWriter: outW,
		events: make(chan readEvent, 16), readerDone: make(chan struct{}), abort: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(p.readerDone)
		defer close(p.events)
		r := bufio.NewReader(outR)
		emit := func(e readEvent) bool {
			select {
			case p.events <- e:
				return true
			case <-p.abort:
				return false
			}
		}
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 && !emit(readEvent{line: line}) {
				return
			}
			if err != nil {
				emit(readEvent{err: err})
				return
			}
		}
	}()
	t.Cleanup(func() {
		p.abortOnce.Do(func() { close(p.abort) })
		_ = inW.Close()
		_ = inR.Close()
		_ = outW.Close()
		_ = outR.Close()
		p.beginClientClose()
		if p.active {
			select {
			case <-p.done:
			case <-time.After(deadline):
				t.Error("SDK endpoint did not stop after forced raw closure")
			}
		}
		select {
		case <-p.readerDone:
		case <-time.After(deadline):
			t.Error("raw collector did not stop")
		}
	})
	return p, &acp.IOTransport{Reader: inR, Writer: outW}
}

func agentPeer(t *testing.T, h any) *peer {
	t.Helper()
	p, tr := newPeer(t)
	p.active = true
	go func() {
		p.err = acp.RunAgent(context.Background(), tr, func(*acp.AgentConnection) any { return h })
		close(p.done)
	}()
	p.send(t, callFrame(`"init"`, acp.MethodInitialize, `{"protocolVersion":1,"clientCapabilities":{}}`))
	r := p.receive(t)
	wantJSON(t, r["id"], `"init"`)
	if len(r["result"]) == 0 {
		t.Fatalf("initialize did not succeed: %v", r)
	}
	return p
}

func clientPeer(t *testing.T, h any) (*acp.Client, *peer) {
	t.Helper()
	p, tr := newPeer(t)
	c, err := acp.Connect(context.Background(), tr, h)
	must(t, err)
	p.active = true
	p.closeClient = c.Close
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	initDone := make(chan error, 1)
	go func() {
		_, err := c.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1, ClientCapabilities: &acp.ClientCapabilities{Fs: &acp.FileSystemCapabilities{ReadTextFile: true}}})
		initDone <- err
	}()
	r := p.receive(t)
	wantJSON(t, r["method"], `"initialize"`)
	p.respond(t, r, `{"protocolVersion":1,"agentCapabilities":{},"authMethods":[]}`)
	must(t, await(t, initDone))
	newDone := make(chan error, 1)
	go func() {
		_, err := c.SessionNew(ctx, &acp.NewSessionRequest{Cwd: "/workspace", McpServers: []acp.McpServer{}})
		newDone <- err
	}()
	r = p.receive(t)
	wantJSON(t, r["method"], `"session/new"`)
	p.respond(t, r, `{"sessionId":"session"}`)
	must(t, await(t, newDone))
	return c, p
}

func (p *peer) beginClientClose() {
	if p.closeClient == nil {
		return
	}
	p.clientCloseOnce.Do(func() { go func() { p.err = p.closeClient(); close(p.done) }() })
}

func (p *peer) send(t *testing.T, frame string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { _, err := fmt.Fprintln(p.input, frame); done <- err }()
	must(t, await(t, done))
}
func (p *peer) decode(t *testing.T, line []byte) envelope {
	t.Helper()
	var r envelope
	must(t, json.Unmarshal(line, &r))
	if r == nil {
		t.Fatalf("SDK wrote a non-object frame: %q", line)
	}
	p.frames = append(p.frames, r)
	return r
}
func (p *peer) poll(t *testing.T) (envelope, error) {
	t.Helper()
	select {
	case e, ok := <-p.events:
		if !ok {
			return nil, io.EOF
		}
		if e.err != nil {
			return nil, e.err
		}
		return p.decode(t, e.line), nil
	case <-time.After(deadline):
		return nil, context.DeadlineExceeded
	}
}
func (p *peer) receive(t *testing.T) envelope {
	t.Helper()
	r, err := p.poll(t)
	if err != nil {
		t.Fatalf("SDK did not produce expected frame: %v", err)
	}
	return r
}
func (p *peer) respond(t *testing.T, r envelope, result string) {
	t.Helper()
	p.send(t, `{"jsonrpc":"2.0","id":`+string(r["id"])+`,"result":`+result+`}`)
}

// The return value is endpoint policy, not an assertion that malformed-peer
// recovery must keep the connection alive. All accepted dispatch has settled
// before the caller examines handler observations or absent wire responses.
func (p *peer) finish(t *testing.T) error {
	t.Helper()
	_ = p.input.Close()
	p.beginClientClose()
	select {
	case <-p.done:
	case <-time.After(deadline):
		t.Fatal("SDK did not drain after input EOF")
	}
	for {
		select {
		case e, ok := <-p.events:
			if !ok {
				return p.err
			}
			if e.err != nil {
				if e.err != io.EOF {
					t.Logf("output ended: %v", e.err)
				}
				continue
			}
			p.decode(t, e.line)
		case <-time.After(deadline):
			t.Fatal("SDK output did not reach EOF")
		}
	}
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(deadline):
		t.Fatal("timed out waiting for public operation")
	}
	var zero T
	return zero
}
func callFrame(id, method, params string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":%q,"params":%s}`, id, method, params)
}
func noteFrame(method, params string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"params":%s}`, method, params)
}
func (p *peer) call(t *testing.T, id, method, params string) envelope {
	t.Helper()
	p.send(t, callFrame(id, method, params))
	return p.receive(t)
}
func wantJSON(t *testing.T, b []byte, want string) {
	t.Helper()
	decode := func(b []byte) any {
		if !json.Valid(b) {
			t.Fatalf("not one JSON value: %q", b)
		}
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		var v any
		must(t, d.Decode(&v))
		return v
	}
	if !reflect.DeepEqual(decode(b), decode([]byte(want))) {
		t.Errorf("JSON=%s; want %s", b, want)
	}
}
func rejectKind(t *testing.T, r envelope) {
	t.Helper()
	wantJSON(t, r["id"], `"wrong"`)
	var err jsonrpc.Error
	must(t, json.Unmarshal(r["error"], &err))
	if err.Code != jsonrpc.CodeInvalidRequest && err.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("wrong-kind error=%d; want InvalidRequest or MethodNotFound", err.Code)
	}
	if _, ok := r["result"]; ok {
		t.Error("rejection also contains result")
	}
}
