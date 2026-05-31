// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

// ProtocolVersion is the ACP protocol version implemented by this SDK.
const ProtocolVersion = 1

const (
	// MethodInitialize initializes the ACP session and negotiates capabilities.
	MethodInitialize = "initialize"
	// MethodAuthenticate asks the agent to authenticate with a selected auth method.
	MethodAuthenticate = "authenticate"
	// MethodLogout asks the agent to clear authentication state.
	MethodLogout = "logout"
	// MethodSessionNew asks the agent to create a new session.
	MethodSessionNew = "session/new"
	// MethodSessionLoad asks the agent to load an existing session.
	MethodSessionLoad = "session/load"
	// MethodSessionResume asks the agent to resume an existing session.
	MethodSessionResume = "session/resume"
	// MethodSessionList asks the agent to list known sessions.
	MethodSessionList = "session/list"
	// MethodSessionClose asks the agent to close a session.
	MethodSessionClose = "session/close"
	// MethodSessionPrompt sends a user prompt to an agent session.
	MethodSessionPrompt = "session/prompt"
	// MethodSessionCancel notifies the agent to cancel work for a session.
	MethodSessionCancel = "session/cancel"
	// MethodSessionSetMode asks the agent to change a session mode.
	MethodSessionSetMode = "session/set_mode"
	// MethodSessionSetConfigOption asks the agent to set a session configuration option.
	MethodSessionSetConfigOption = "session/set_config_option"

	// MethodSessionUpdate notifies the client about a session update.
	MethodSessionUpdate = "session/update"
	// MethodRequestPermission asks the client to approve or reject a tool action.
	MethodRequestPermission = "session/request_permission"
	// MethodReadTextFile asks the client to read a text file.
	MethodReadTextFile = "fs/read_text_file"
	// MethodWriteTextFile asks the client to write a text file.
	MethodWriteTextFile = "fs/write_text_file"
	// MethodCreateTerminal asks the client to create a terminal.
	MethodCreateTerminal = "terminal/create"
	// MethodTerminalOutput asks the client for terminal output.
	MethodTerminalOutput = "terminal/output"
	// MethodWaitTerminalExit asks the client to wait for a terminal to exit.
	MethodWaitTerminalExit = "terminal/wait_for_exit"
	// MethodKillTerminal asks the client to kill a terminal.
	MethodKillTerminal = "terminal/kill"
	// MethodReleaseTerminal asks the client to release terminal resources.
	MethodReleaseTerminal = "terminal/release"
)

// Meta stores protocol extension metadata under the reserved _meta field.
type Meta map[string]any

// Implementation identifies an ACP client or agent implementation.
type Implementation struct {
	Meta    Meta   `json:"_meta,omitzero"`
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// InitializeRequest is sent by the client to initialize an ACP connection.
type InitializeRequest struct {
	Meta               Meta                `json:"_meta,omitzero"`
	ProtocolVersion    uint16              `json:"protocolVersion"`
	ClientCapabilities *ClientCapabilities `json:"clientCapabilities,omitempty"`
	ClientInfo         *Implementation     `json:"clientInfo,omitempty"`
}

// InitializeResponse is returned by the agent after initialization.
type InitializeResponse struct {
	Meta              Meta               `json:"_meta,omitzero"`
	ProtocolVersion   uint16             `json:"protocolVersion"`
	AgentCapabilities *AgentCapabilities `json:"agentCapabilities,omitempty"`
	AgentInfo         *Implementation    `json:"agentInfo,omitempty"`
	AuthMethods       []AuthMethod       `json:"authMethods,omitzero"`
}

// ClientCapabilities describes optional methods supported by the client.
type ClientCapabilities struct {
	Meta     Meta                    `json:"_meta,omitzero"`
	FS       *FileSystemCapabilities `json:"fs,omitempty"`
	Terminal bool                    `json:"terminal,omitempty"`
}

// FileSystemCapabilities describes client filesystem support.
type FileSystemCapabilities struct {
	Meta          Meta `json:"_meta,omitzero"`
	ReadTextFile  bool `json:"readTextFile,omitempty"`
	WriteTextFile bool `json:"writeTextFile,omitempty"`
}

// AgentCapabilities describes optional methods and features supported by the agent.
type AgentCapabilities struct {
	Meta                Meta                   `json:"_meta,omitzero"`
	LoadSession         bool                   `json:"loadSession,omitempty"`
	PromptCapabilities  *PromptCapabilities    `json:"promptCapabilities,omitempty"`
	Auth                *AgentAuthCapabilities `json:"auth,omitempty"`
	MCPCapabilities     *MCPCapabilities       `json:"mcpCapabilities,omitempty"`
	SessionCapabilities *SessionCapabilities   `json:"sessionCapabilities,omitempty"`
}

// PromptCapabilities describes content types accepted in session/prompt.
type PromptCapabilities struct {
	Meta            Meta `json:"_meta,omitzero"`
	Image           bool `json:"image,omitempty"`
	Audio           bool `json:"audio,omitempty"`
	EmbeddedContext bool `json:"embeddedContext,omitempty"`
}

// AgentAuthCapabilities describes agent authentication features.
type AgentAuthCapabilities struct {
	Meta   Meta                `json:"_meta,omitzero"`
	Logout *LogoutCapabilities `json:"logout,omitempty"`
}

// LogoutCapabilities marks support for the logout method.
type LogoutCapabilities struct {
	Meta Meta `json:"_meta,omitzero"`
}

// MCPCapabilities describes MCP server transports supported by the agent.
type MCPCapabilities struct {
	Meta Meta `json:"_meta,omitzero"`
	HTTP bool `json:"http,omitempty"`
	SSE  bool `json:"sse,omitempty"`
}

// SessionCapabilities describes optional session management features.
type SessionCapabilities struct {
	Meta   Meta                       `json:"_meta,omitzero"`
	List   *SessionListCapabilities   `json:"list,omitempty"`
	Resume *SessionResumeCapabilities `json:"resume,omitempty"`
	Close  *SessionCloseCapabilities  `json:"close,omitempty"`
}

// SessionListCapabilities marks support for session/list.
type SessionListCapabilities struct {
	Meta Meta `json:"_meta,omitzero"`
}

// SessionResumeCapabilities marks support for session/resume.
type SessionResumeCapabilities struct {
	Meta Meta `json:"_meta,omitzero"`
}

// SessionCloseCapabilities marks support for session/close.
type SessionCloseCapabilities struct {
	Meta Meta `json:"_meta,omitzero"`
}

// AuthMethod describes an authentication option advertised by the agent.
type AuthMethod struct {
	Meta        Meta   `json:"_meta,omitzero"`
	Type        string `json:"type,omitempty"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AuthenticateRequest selects an advertised authentication method.
type AuthenticateRequest struct {
	Meta     Meta   `json:"_meta,omitzero"`
	MethodID string `json:"methodId"`
}

// AuthenticateResponse acknowledges successful authentication.
type AuthenticateResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

// LogoutRequest asks the agent to clear authentication state.
type LogoutRequest struct {
	Meta Meta `json:"_meta,omitzero"`
}

// LogoutResponse acknowledges logout completion.
type LogoutResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

// NewSessionRequest asks the agent to create a new session rooted at CWD.
type NewSessionRequest struct {
	Meta                  Meta        `json:"_meta,omitzero"`
	CWD                   string      `json:"cwd"`
	MCPServers            []MCPServer `json:"mcpServers"`
	AdditionalDirectories []string    `json:"additionalDirectories,omitzero"`
}

// NewSessionResponse describes the session created by session/new.
type NewSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitzero"`
	SessionID     string                `json:"sessionId"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// LoadSessionRequest asks the agent to load a known session.
type LoadSessionRequest struct {
	Meta       Meta        `json:"_meta,omitzero"`
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers"`
}

// LoadSessionResponse describes state returned after loading a session.
type LoadSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitzero"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// ResumeSessionRequest asks the agent to resume a known session.
type ResumeSessionRequest struct {
	Meta       Meta        `json:"_meta,omitzero"`
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers,omitempty"`
}

// ResumeSessionResponse describes state returned after resuming a session.
type ResumeSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitzero"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// ListSessionsRequest asks the agent to list sessions, optionally filtered by CWD.
type ListSessionsRequest struct {
	Meta   Meta   `json:"_meta,omitzero"`
	CWD    string `json:"cwd,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// ListSessionsResponse contains a page of known sessions.
type ListSessionsResponse struct {
	Meta       Meta          `json:"_meta,omitzero"`
	Sessions   []SessionInfo `json:"sessions"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

// SessionInfo describes a session returned by session/list.
type SessionInfo struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd,omitempty"`
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// CloseSessionRequest asks the agent to close a session.
type CloseSessionRequest struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
}

// CloseSessionResponse acknowledges session closure.
type CloseSessionResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

// PromptRequest sends user content to an agent session.
type PromptRequest struct {
	Meta      Meta           `json:"_meta,omitzero"`
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// PromptResponse reports why the agent stopped processing a prompt.
type PromptResponse struct {
	Meta       Meta       `json:"_meta,omitzero"`
	StopReason StopReason `json:"stopReason"`
}

// StopReason identifies why prompt processing stopped.
type StopReason string

const (
	// StopReasonEndTurn means the agent completed its turn normally.
	StopReasonEndTurn StopReason = "end_turn"
	// StopReasonMaxTokens means generation stopped after reaching a token limit.
	StopReasonMaxTokens StopReason = "max_tokens"
	// StopReasonMaxTurnRequests means the agent reached a turn request limit.
	StopReasonMaxTurnRequests StopReason = "max_turn_requests"
	// StopReasonRefusal means the agent refused the request.
	StopReasonRefusal StopReason = "refusal"
	// StopReasonCancelled means prompt processing was cancelled.
	StopReasonCancelled StopReason = "cancelled"
)

// CancelNotification asks the agent to cancel work for a session.
type CancelNotification struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
}

// SetSessionModeRequest asks the agent to change a session mode.
type SetSessionModeRequest struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

// SetSessionModeResponse acknowledges a mode change.
type SetSessionModeResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

// SetSessionConfigOptionRequest asks the agent to set a session configuration option.
type SetSessionConfigOptionRequest struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

// SetSessionConfigOptionResponse returns the updated configuration options.
type SetSessionConfigOptionResponse struct {
	Meta          Meta                  `json:"_meta,omitzero"`
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

// SessionModeState describes the current and available modes for a session.
type SessionModeState struct {
	Meta           Meta          `json:"_meta,omitzero"`
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

// SessionMode describes a selectable session mode.
type SessionMode struct {
	Meta        Meta   `json:"_meta,omitzero"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionConfigOption describes a configurable session setting.
type SessionConfigOption struct {
	Meta         Meta                       `json:"_meta,omitzero"`
	Type         string                     `json:"type"`
	ID           string                     `json:"id"`
	Name         string                     `json:"name"`
	Description  string                     `json:"description,omitempty"`
	Category     string                     `json:"category,omitempty"`
	CurrentValue string                     `json:"currentValue,omitempty"`
	Options      SessionConfigSelectOptions `json:"options,omitzero"`
}

// SessionConfigSelectOptions stores flat or grouped select options.
type SessionConfigSelectOptions struct {
	Flat   []SessionConfigSelectOption `json:"-"`
	Groups []SessionConfigSelectGroup  `json:"-"`
}

// SessionConfigSelectGroup groups related select options.
type SessionConfigSelectGroup struct {
	Meta    Meta                        `json:"_meta,omitzero"`
	Group   string                      `json:"group"`
	Name    string                      `json:"name"`
	Options []SessionConfigSelectOption `json:"options"`
}

// SessionConfigSelectOption describes one selectable configuration value.
type SessionConfigSelectOption struct {
	Meta        Meta   `json:"_meta,omitzero"`
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// MCPServer describes an MCP server made available to the agent.
type MCPServer struct {
	Meta    Meta          `json:"_meta,omitzero"`
	Type    string        `json:"type,omitempty"`
	Name    string        `json:"name"`
	Command string        `json:"command,omitempty"`
	Args    []string      `json:"args,omitzero"`
	Env     []EnvVariable `json:"env,omitzero"`
	URL     string        `json:"url,omitempty"`
	Headers []HTTPHeader  `json:"headers,omitzero"`
}

// EnvVariable describes an environment variable for a subprocess.
type EnvVariable struct {
	Meta  Meta   `json:"_meta,omitzero"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HTTPHeader describes an HTTP header for a remote server.
type HTTPHeader struct {
	Meta  Meta   `json:"_meta,omitzero"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SessionNotification carries an agent-to-client session/update notification.
type SessionNotification struct {
	Meta      Meta          `json:"_meta,omitzero"`
	SessionID string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}

// SessionUpdate describes a typed update within a session/update notification.
type SessionUpdate struct {
	Meta              Meta                  `json:"_meta,omitzero"`
	SessionUpdate     string                `json:"sessionUpdate"`
	Content           any                   `json:"content,omitempty"`
	ToolCallID        string                `json:"toolCallId,omitempty"`
	Title             *string               `json:"title,omitempty"`
	Kind              ToolKind              `json:"kind,omitempty"`
	Status            ToolCallStatus        `json:"status,omitempty"`
	Locations         []ToolCallLocation    `json:"locations,omitzero"`
	RawInput          any                   `json:"rawInput,omitempty"`
	RawOutput         any                   `json:"rawOutput,omitempty"`
	AvailableCommands []AvailableCommand    `json:"availableCommands,omitzero"`
	CurrentModeID     string                `json:"currentModeId,omitempty"`
	ConfigOptions     []SessionConfigOption `json:"configOptions,omitzero"`
	Entries           []PlanEntry           `json:"entries,omitzero"`
	UpdatedAt         *string               `json:"updatedAt,omitempty"`
}

// ToolCallUpdate describes tool call state for updates and permission requests.
type ToolCallUpdate struct {
	Meta       Meta               `json:"_meta,omitzero"`
	ToolCallID string             `json:"toolCallId"`
	Title      *string            `json:"title,omitempty"`
	Kind       ToolKind           `json:"kind,omitempty"`
	Status     ToolCallStatus     `json:"status,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitzero"`
	RawInput   any                `json:"rawInput,omitempty"`
	RawOutput  any                `json:"rawOutput,omitempty"`
	Content    []ToolCallContent  `json:"content,omitzero"`
}

// ToolKind categorizes the type of work represented by a tool call.
type ToolKind string

const (
	// ToolKindRead marks a read operation.
	ToolKindRead ToolKind = "read"
	// ToolKindEdit marks an edit operation.
	ToolKindEdit ToolKind = "edit"
	// ToolKindDelete marks a delete operation.
	ToolKindDelete ToolKind = "delete"
	// ToolKindMove marks a move operation.
	ToolKindMove ToolKind = "move"
	// ToolKindSearch marks a search operation.
	ToolKindSearch ToolKind = "search"
	// ToolKindExecute marks command execution.
	ToolKindExecute ToolKind = "execute"
	// ToolKindThink marks internal reasoning work.
	ToolKindThink ToolKind = "think"
	// ToolKindFetch marks network or resource fetching.
	ToolKindFetch ToolKind = "fetch"
	// ToolKindSwitchMode marks a session mode switch.
	ToolKindSwitchMode ToolKind = "switch_mode"
	// ToolKindOther marks a tool call that does not fit another category.
	ToolKindOther ToolKind = "other"
)

// ToolCallStatus identifies the lifecycle state of a tool call.
type ToolCallStatus string

const (
	// ToolCallPending means the tool call has not started.
	ToolCallPending ToolCallStatus = "pending"
	// ToolCallInProgress means the tool call is running.
	ToolCallInProgress ToolCallStatus = "in_progress"
	// ToolCallCompleted means the tool call completed successfully.
	ToolCallCompleted ToolCallStatus = "completed"
	// ToolCallFailed means the tool call failed.
	ToolCallFailed ToolCallStatus = "failed"
)

// ToolCallLocation identifies a file location related to a tool call.
type ToolCallLocation struct {
	Meta Meta   `json:"_meta,omitzero"`
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// AvailableCommand describes a command the client may present to the user.
type AvailableCommand struct {
	Meta        Meta                   `json:"_meta,omitzero"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Input       *AvailableCommandInput `json:"input,omitempty"`
}

// AvailableCommandInput describes user input requested by an available command.
type AvailableCommandInput struct {
	Meta Meta   `json:"_meta,omitzero"`
	Hint string `json:"hint"`
}

// PlanEntry describes a single task in the agent's execution plan.
type PlanEntry struct {
	Meta     Meta              `json:"_meta,omitzero"`
	ID       string            `json:"id,omitempty"`
	Content  string            `json:"content"`
	Status   PlanEntryStatus   `json:"status"`
	Priority PlanEntryPriority `json:"priority,omitempty"`
}

// PlanEntryStatus identifies the state of a plan entry.
type PlanEntryStatus string

// PlanEntryPriority identifies the relative importance of a plan entry.
type PlanEntryPriority string

// SessionInfoUpdate updates display information for a session.
type SessionInfoUpdate struct {
	Meta      Meta    `json:"_meta,omitzero"`
	Title     *string `json:"title,omitempty"`
	UpdatedAt *string `json:"updatedAt,omitempty"`
}

// RequestPermissionRequest asks the client to approve or reject a tool action.
type RequestPermissionRequest struct {
	Meta      Meta               `json:"_meta,omitzero"`
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCallUpdate     `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// RequestPermissionResponse reports the user's permission decision.
type RequestPermissionResponse struct {
	Meta    Meta                     `json:"_meta,omitzero"`
	Outcome RequestPermissionOutcome `json:"outcome"`
}

// PermissionOption describes one permission choice presented to the user.
type PermissionOption struct {
	Meta     Meta                 `json:"_meta,omitzero"`
	OptionID string               `json:"optionId"`
	Name     string               `json:"name"`
	Kind     PermissionOptionKind `json:"kind"`
}

// PermissionOptionKind identifies the effect of a permission option.
type PermissionOptionKind string

const (
	// PermissionAllowOnce allows the requested action once.
	PermissionAllowOnce PermissionOptionKind = "allow_once"
	// PermissionAllowAlways allows this class of action persistently.
	PermissionAllowAlways PermissionOptionKind = "allow_always"
	// PermissionRejectOnce rejects the requested action once.
	PermissionRejectOnce PermissionOptionKind = "reject_once"
	// PermissionRejectAlways rejects this class of action persistently.
	PermissionRejectAlways PermissionOptionKind = "reject_always"
)

// RequestPermissionOutcome describes the selected or cancelled permission result.
type RequestPermissionOutcome struct {
	Meta     Meta   `json:"_meta,omitzero"`
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// ReadTextFileRequest asks the client to read text from a file.
type ReadTextFileRequest struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Line      *int   `json:"line,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
}

// ReadTextFileResponse returns text read from a file.
type ReadTextFileResponse struct {
	Meta    Meta   `json:"_meta,omitzero"`
	Content string `json:"content"`
}

// WriteTextFileRequest asks the client to write text to a file.
type WriteTextFileRequest struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

// WriteTextFileResponse acknowledges a file write.
type WriteTextFileResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

// CreateTerminalRequest asks the client to start a terminal command.
type CreateTerminalRequest struct {
	Meta            Meta          `json:"_meta,omitzero"`
	SessionID       string        `json:"sessionId"`
	Command         string        `json:"command"`
	Args            []string      `json:"args,omitzero"`
	CWD             string        `json:"cwd,omitempty"`
	Env             []EnvVariable `json:"env,omitzero"`
	OutputByteLimit *uint64       `json:"outputByteLimit,omitempty"`
}

// CreateTerminalResponse identifies the created terminal.
type CreateTerminalResponse struct {
	Meta       Meta   `json:"_meta,omitzero"`
	TerminalID string `json:"terminalId"`
}

// TerminalOutputRequest asks the client for output from a terminal.
type TerminalOutputRequest struct {
	Meta       Meta   `json:"_meta,omitzero"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// TerminalOutputResponse returns terminal output and status information.
type TerminalOutputResponse struct {
	Meta       Meta                `json:"_meta,omitzero"`
	Output     string              `json:"output"`
	Truncated  bool                `json:"truncated"`
	ExitStatus *TerminalExitStatus `json:"exitStatus,omitempty"`
}

// TerminalExitStatus describes how a terminal process exited.
type TerminalExitStatus struct {
	Meta     Meta    `json:"_meta,omitzero"`
	ExitCode *uint32 `json:"exitCode,omitempty"`
	Signal   *string `json:"signal,omitempty"`
}

// WaitForTerminalExitRequest asks the client to wait for a terminal to exit.
type WaitForTerminalExitRequest struct {
	Meta       Meta   `json:"_meta,omitzero"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// WaitForTerminalExitResponse describes how a terminal process exited.
type WaitForTerminalExitResponse = TerminalExitStatus

// KillTerminalRequest asks the client to kill a terminal process.
type KillTerminalRequest struct {
	Meta       Meta   `json:"_meta,omitzero"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// KillTerminalResponse acknowledges a terminal kill request.
type KillTerminalResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

// ReleaseTerminalRequest asks the client to release terminal resources.
type ReleaseTerminalRequest struct {
	Meta       Meta   `json:"_meta,omitzero"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// ReleaseTerminalResponse acknowledges terminal resource release.
type ReleaseTerminalResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}
