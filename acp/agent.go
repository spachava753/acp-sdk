// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"

	"github.com/spachava753/acp-sdk/internal/jsonrpc2"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Agent handles the core agent-side ACP methods.
type Agent interface {
	Initialize(context.Context, *InitializeRequest) (*InitializeResponse, error)
	NewSession(context.Context, *NewSessionRequest) (*NewSessionResponse, error)
	Prompt(context.Context, *PromptRequest) (*PromptResponse, error)
	Cancel(context.Context, *CancelNotification) error
}

// AgentFactory constructs an Agent after the bidirectional connection is ready.
type AgentFactory func(*AgentConnection) Agent

// AuthenticatingAgent handles the optional authenticate method.
type AuthenticatingAgent interface {
	Authenticate(context.Context, *AuthenticateRequest) (*AuthenticateResponse, error)
}

// LogoutAgent handles the optional logout method.
type LogoutAgent interface {
	Logout(context.Context, *LogoutRequest) (*LogoutResponse, error)
}

// SessionLoadingAgent handles the optional session/load method.
type SessionLoadingAgent interface {
	LoadSession(context.Context, *LoadSessionRequest) (*LoadSessionResponse, error)
}

// SessionResumingAgent handles the optional session/resume method.
type SessionResumingAgent interface {
	ResumeSession(context.Context, *ResumeSessionRequest) (*ResumeSessionResponse, error)
}

// SessionListingAgent handles the optional session/list method.
type SessionListingAgent interface {
	ListSessions(context.Context, *ListSessionsRequest) (*ListSessionsResponse, error)
}

// SessionClosingAgent handles the optional session/close method.
type SessionClosingAgent interface {
	CloseSession(context.Context, *CloseSessionRequest) (*CloseSessionResponse, error)
}

// ModeSettingAgent handles the optional session/set_mode method.
type ModeSettingAgent interface {
	SetSessionMode(context.Context, *SetSessionModeRequest) (*SetSessionModeResponse, error)
}

// ConfigOptionSettingAgent handles the optional session/set_config_option method.
type ConfigOptionSettingAgent interface {
	SetSessionConfigOption(context.Context, *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error)
}

// AgentConnection lets an agent call client-side ACP methods on its peer.
type AgentConnection struct {
	rpc *rpcEndpoint
}

// RunAgent serves an ACP agent over transport until the connection closes.
func RunAgent(ctx context.Context, transport Transport, newAgent AgentFactory) error {
	raw, err := transport.Connect(ctx)
	if err != nil {
		return err
	}
	endpoint := &rpcEndpoint{raw: raw}
	ac := &AgentConnection{rpc: endpoint}
	var agent Agent
	rpc := jsonrpc2.NewConnection(ctx, jsonrpc2.ConnectionConfig{
		Reader: raw,
		Writer: raw,
		Closer: raw,
		Bind: func(conn *jsonrpc2.Connection) jsonrpc2.Handler {
			endpoint.conn = conn
			agent = newAgent(ac)
			return jsonrpc2.HandlerFunc(func(ctx context.Context, req *jsonrpc.Request) (any, error) {
				return handleAgentRequest(ctx, agent, req)
			})
		},
	})
	return rpc.Wait()
}

// Close closes the agent-side ACP connection.
func (c *AgentConnection) Close() error {
	return c.rpc.conn.Close()
}

// SessionUpdate sends a session/update notification to the client.
func (c *AgentConnection) SessionUpdate(ctx context.Context, params *SessionNotification) error {
	return notify(ctx, c.rpc.conn, MethodSessionUpdate, params)
}

// RequestPermission calls session/request_permission on the client.
func (c *AgentConnection) RequestPermission(ctx context.Context, params *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	return call[RequestPermissionResponse](ctx, c.rpc.conn, MethodRequestPermission, params)
}

// ReadTextFile calls fs/read_text_file on the client.
func (c *AgentConnection) ReadTextFile(ctx context.Context, params *ReadTextFileRequest) (*ReadTextFileResponse, error) {
	return call[ReadTextFileResponse](ctx, c.rpc.conn, MethodReadTextFile, params)
}

// WriteTextFile calls fs/write_text_file on the client.
func (c *AgentConnection) WriteTextFile(ctx context.Context, params *WriteTextFileRequest) (*WriteTextFileResponse, error) {
	return call[WriteTextFileResponse](ctx, c.rpc.conn, MethodWriteTextFile, params)
}

// CreateTerminal calls terminal/create on the client.
func (c *AgentConnection) CreateTerminal(ctx context.Context, params *CreateTerminalRequest) (*CreateTerminalResponse, error) {
	return call[CreateTerminalResponse](ctx, c.rpc.conn, MethodCreateTerminal, params)
}

// TerminalOutput calls terminal/output on the client.
func (c *AgentConnection) TerminalOutput(ctx context.Context, params *TerminalOutputRequest) (*TerminalOutputResponse, error) {
	return call[TerminalOutputResponse](ctx, c.rpc.conn, MethodTerminalOutput, params)
}

// WaitForTerminalExit calls terminal/wait_for_exit on the client.
func (c *AgentConnection) WaitForTerminalExit(ctx context.Context, params *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error) {
	return call[WaitForTerminalExitResponse](ctx, c.rpc.conn, MethodWaitTerminalExit, params)
}

// KillTerminal calls terminal/kill on the client.
func (c *AgentConnection) KillTerminal(ctx context.Context, params *KillTerminalRequest) (*KillTerminalResponse, error) {
	return call[KillTerminalResponse](ctx, c.rpc.conn, MethodKillTerminal, params)
}

// ReleaseTerminal calls terminal/release on the client.
func (c *AgentConnection) ReleaseTerminal(ctx context.Context, params *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error) {
	return call[ReleaseTerminalResponse](ctx, c.rpc.conn, MethodReleaseTerminal, params)
}

func handleAgentRequest(ctx context.Context, agent Agent, req *jsonrpc.Request) (any, error) {
	jsonrpc2.Async(ctx)
	if agent == nil {
		return nil, methodNotFound(req.Method)
	}
	switch req.Method {
	case MethodInitialize:
		params, err := decodeParams[InitializeRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(agent.Initialize(ctx, params))
	case MethodSessionNew:
		params, err := decodeParams[NewSessionRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(agent.NewSession(ctx, params))
	case MethodSessionPrompt:
		params, err := decodeParams[PromptRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(agent.Prompt(ctx, params))
	case MethodSessionCancel:
		params, err := decodeParams[CancelNotification](req)
		if err != nil {
			return nil, err
		}
		return nil, agent.Cancel(ctx, params)
	case MethodAuthenticate:
		optional, ok := agent.(AuthenticatingAgent)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[AuthenticateRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(optional.Authenticate(ctx, params))
	case MethodLogout:
		optional, ok := agent.(LogoutAgent)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[LogoutRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(optional.Logout(ctx, params))
	case MethodSessionLoad:
		optional, ok := agent.(SessionLoadingAgent)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[LoadSessionRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(optional.LoadSession(ctx, params))
	case MethodSessionResume:
		optional, ok := agent.(SessionResumingAgent)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[ResumeSessionRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(optional.ResumeSession(ctx, params))
	case MethodSessionList:
		optional, ok := agent.(SessionListingAgent)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[ListSessionsRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(optional.ListSessions(ctx, params))
	case MethodSessionClose:
		optional, ok := agent.(SessionClosingAgent)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[CloseSessionRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(optional.CloseSession(ctx, params))
	case MethodSessionSetMode:
		optional, ok := agent.(ModeSettingAgent)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[SetSessionModeRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(optional.SetSessionMode(ctx, params))
	case MethodSessionSetConfigOption:
		optional, ok := agent.(ConfigOptionSettingAgent)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[SetSessionConfigOptionRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(optional.SetSessionConfigOption(ctx, params))
	default:
		return nil, methodNotFound(req.Method)
	}
}
