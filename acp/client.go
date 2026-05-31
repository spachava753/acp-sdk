// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"context"

	"github.com/spachava753/acp-sdk/internal/jsonrpc2"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

type Client struct {
	rpc     *rpcEndpoint
	handler ClientHandler
}

type ClientHandler interface {
	SessionUpdate(context.Context, *SessionNotification) error
}

type FileSystemHandler interface {
	ReadTextFile(context.Context, *ReadTextFileRequest) (*ReadTextFileResponse, error)
	WriteTextFile(context.Context, *WriteTextFileRequest) (*WriteTextFileResponse, error)
}

type TerminalHandler interface {
	CreateTerminal(context.Context, *CreateTerminalRequest) (*CreateTerminalResponse, error)
	TerminalOutput(context.Context, *TerminalOutputRequest) (*TerminalOutputResponse, error)
	WaitForTerminalExit(context.Context, *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error)
	KillTerminal(context.Context, *KillTerminalRequest) (*KillTerminalResponse, error)
	ReleaseTerminal(context.Context, *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error)
}

type PermissionHandler interface {
	RequestPermission(context.Context, *RequestPermissionRequest) (*RequestPermissionResponse, error)
}

func Connect(ctx context.Context, transport Transport, handler ClientHandler) (*Client, error) {
	client := &Client{handler: handler}
	rpc, err := connectRPC(ctx, transport, func(ctx context.Context, req *jsonrpc.Request) (any, error) {
		return client.handle(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	client.rpc = rpc
	return client, nil
}

func (c *Client) Close() error {
	return c.rpc.conn.Close()
}

func (c *Client) Initialize(ctx context.Context, params *InitializeRequest) (*InitializeResponse, error) {
	return call[InitializeResponse](ctx, c.rpc.conn, MethodInitialize, params)
}

func (c *Client) Authenticate(ctx context.Context, params *AuthenticateRequest) (*AuthenticateResponse, error) {
	return call[AuthenticateResponse](ctx, c.rpc.conn, MethodAuthenticate, params)
}

func (c *Client) Logout(ctx context.Context, params *LogoutRequest) (*LogoutResponse, error) {
	return call[LogoutResponse](ctx, c.rpc.conn, MethodLogout, params)
}

func (c *Client) NewSession(ctx context.Context, params *NewSessionRequest) (*NewSessionResponse, error) {
	return call[NewSessionResponse](ctx, c.rpc.conn, MethodSessionNew, params)
}

func (c *Client) LoadSession(ctx context.Context, params *LoadSessionRequest) (*LoadSessionResponse, error) {
	return call[LoadSessionResponse](ctx, c.rpc.conn, MethodSessionLoad, params)
}

func (c *Client) ResumeSession(ctx context.Context, params *ResumeSessionRequest) (*ResumeSessionResponse, error) {
	return call[ResumeSessionResponse](ctx, c.rpc.conn, MethodSessionResume, params)
}

func (c *Client) ListSessions(ctx context.Context, params *ListSessionsRequest) (*ListSessionsResponse, error) {
	return call[ListSessionsResponse](ctx, c.rpc.conn, MethodSessionList, params)
}

func (c *Client) CloseSession(ctx context.Context, params *CloseSessionRequest) (*CloseSessionResponse, error) {
	return call[CloseSessionResponse](ctx, c.rpc.conn, MethodSessionClose, params)
}

func (c *Client) Prompt(ctx context.Context, params *PromptRequest) (*PromptResponse, error) {
	return call[PromptResponse](ctx, c.rpc.conn, MethodSessionPrompt, params)
}

func (c *Client) Cancel(ctx context.Context, params *CancelNotification) error {
	return notify(ctx, c.rpc.conn, MethodSessionCancel, params)
}

func (c *Client) SetSessionMode(ctx context.Context, params *SetSessionModeRequest) (*SetSessionModeResponse, error) {
	return call[SetSessionModeResponse](ctx, c.rpc.conn, MethodSessionSetMode, params)
}

func (c *Client) SetSessionConfigOption(ctx context.Context, params *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error) {
	return call[SetSessionConfigOptionResponse](ctx, c.rpc.conn, MethodSessionSetConfigOption, params)
}

func (c *Client) handle(ctx context.Context, req *jsonrpc.Request) (any, error) {
	jsonrpc2.Async(ctx)
	if c.handler == nil {
		return nil, methodNotFound(req.Method)
	}
	switch req.Method {
	case MethodSessionUpdate:
		params, err := decodeParams[SessionNotification](req)
		if err != nil {
			return nil, err
		}
		return nil, c.handler.SessionUpdate(ctx, params)
	case MethodRequestPermission:
		handler, ok := c.handler.(PermissionHandler)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[RequestPermissionRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(handler.RequestPermission(ctx, params))
	case MethodReadTextFile:
		handler, ok := c.handler.(FileSystemHandler)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[ReadTextFileRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(handler.ReadTextFile(ctx, params))
	case MethodWriteTextFile:
		handler, ok := c.handler.(FileSystemHandler)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[WriteTextFileRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(handler.WriteTextFile(ctx, params))
	case MethodCreateTerminal:
		handler, ok := c.handler.(TerminalHandler)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[CreateTerminalRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(handler.CreateTerminal(ctx, params))
	case MethodTerminalOutput:
		handler, ok := c.handler.(TerminalHandler)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[TerminalOutputRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(handler.TerminalOutput(ctx, params))
	case MethodWaitTerminalExit:
		handler, ok := c.handler.(TerminalHandler)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[WaitForTerminalExitRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(handler.WaitForTerminalExit(ctx, params))
	case MethodKillTerminal:
		handler, ok := c.handler.(TerminalHandler)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[KillTerminalRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(handler.KillTerminal(ctx, params))
	case MethodReleaseTerminal:
		handler, ok := c.handler.(TerminalHandler)
		if !ok {
			return nil, methodNotFound(req.Method)
		}
		params, err := decodeParams[ReleaseTerminalRequest](req)
		if err != nil {
			return nil, err
		}
		return rpcResult(handler.ReleaseTerminal(ctx, params))
	default:
		return nil, methodNotFound(req.Method)
	}
}
