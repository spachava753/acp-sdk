// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"

	"github.com/spachava753/acp-sdk/internal/jsonrpc2"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

type Agent interface {
	Initialize(context.Context, *InitializeRequest) (*InitializeResponse, error)
	NewSession(context.Context, *NewSessionRequest) (*NewSessionResponse, error)
	Prompt(context.Context, *PromptRequest) (*PromptResponse, error)
	Cancel(context.Context, *CancelNotification) error
}

type AgentFactory func(*AgentConnection) Agent

type AuthenticatingAgent interface {
	Authenticate(context.Context, *AuthenticateRequest) (*AuthenticateResponse, error)
}

type LogoutAgent interface {
	Logout(context.Context, *LogoutRequest) (*LogoutResponse, error)
}

type SessionLoadingAgent interface {
	LoadSession(context.Context, *LoadSessionRequest) (*LoadSessionResponse, error)
}

type SessionResumingAgent interface {
	ResumeSession(context.Context, *ResumeSessionRequest) (*ResumeSessionResponse, error)
}

type SessionListingAgent interface {
	ListSessions(context.Context, *ListSessionsRequest) (*ListSessionsResponse, error)
}

type SessionClosingAgent interface {
	CloseSession(context.Context, *CloseSessionRequest) (*CloseSessionResponse, error)
}

type ModeSettingAgent interface {
	SetSessionMode(context.Context, *SetSessionModeRequest) (*SetSessionModeResponse, error)
}

type ConfigOptionSettingAgent interface {
	SetSessionConfigOption(context.Context, *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error)
}

type AgentConnection struct {
	rpc *rpcEndpoint
}

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

func (c *AgentConnection) Close() error {
	return c.rpc.conn.Close()
}

func (c *AgentConnection) SessionUpdate(ctx context.Context, params *SessionNotification) error {
	return notify(ctx, c.rpc.conn, MethodSessionUpdate, params)
}

func (c *AgentConnection) RequestPermission(ctx context.Context, params *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	return call[RequestPermissionResponse](ctx, c.rpc.conn, MethodRequestPermission, params)
}

func (c *AgentConnection) ReadTextFile(ctx context.Context, params *ReadTextFileRequest) (*ReadTextFileResponse, error) {
	return call[ReadTextFileResponse](ctx, c.rpc.conn, MethodReadTextFile, params)
}

func (c *AgentConnection) WriteTextFile(ctx context.Context, params *WriteTextFileRequest) (*WriteTextFileResponse, error) {
	return call[WriteTextFileResponse](ctx, c.rpc.conn, MethodWriteTextFile, params)
}

func (c *AgentConnection) CreateTerminal(ctx context.Context, params *CreateTerminalRequest) (*CreateTerminalResponse, error) {
	return call[CreateTerminalResponse](ctx, c.rpc.conn, MethodCreateTerminal, params)
}

func (c *AgentConnection) TerminalOutput(ctx context.Context, params *TerminalOutputRequest) (*TerminalOutputResponse, error) {
	return call[TerminalOutputResponse](ctx, c.rpc.conn, MethodTerminalOutput, params)
}

func (c *AgentConnection) WaitForTerminalExit(ctx context.Context, params *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error) {
	return call[WaitForTerminalExitResponse](ctx, c.rpc.conn, MethodWaitTerminalExit, params)
}

func (c *AgentConnection) KillTerminal(ctx context.Context, params *KillTerminalRequest) (*KillTerminalResponse, error) {
	return call[KillTerminalResponse](ctx, c.rpc.conn, MethodKillTerminal, params)
}

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
