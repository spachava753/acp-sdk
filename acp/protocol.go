// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

const ProtocolVersion = 1

const (
	MethodInitialize             = "initialize"
	MethodAuthenticate           = "authenticate"
	MethodLogout                 = "logout"
	MethodSessionNew             = "session/new"
	MethodSessionLoad            = "session/load"
	MethodSessionResume          = "session/resume"
	MethodSessionList            = "session/list"
	MethodSessionClose           = "session/close"
	MethodSessionPrompt          = "session/prompt"
	MethodSessionCancel          = "session/cancel"
	MethodSessionSetMode         = "session/set_mode"
	MethodSessionSetConfigOption = "session/set_config_option"

	MethodSessionUpdate     = "session/update"
	MethodRequestPermission = "session/request_permission"
	MethodReadTextFile      = "fs/read_text_file"
	MethodWriteTextFile     = "fs/write_text_file"
	MethodCreateTerminal    = "terminal/create"
	MethodTerminalOutput    = "terminal/output"
	MethodWaitTerminalExit  = "terminal/wait_for_exit"
	MethodKillTerminal      = "terminal/kill"
	MethodReleaseTerminal   = "terminal/release"
)

type Meta map[string]any

type Implementation struct {
	Meta    Meta   `json:"_meta,omitzero"`
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

type InitializeRequest struct {
	Meta               Meta                `json:"_meta,omitzero"`
	ProtocolVersion    uint16              `json:"protocolVersion"`
	ClientCapabilities *ClientCapabilities `json:"clientCapabilities,omitempty"`
	ClientInfo         *Implementation     `json:"clientInfo,omitempty"`
}

type InitializeResponse struct {
	Meta              Meta               `json:"_meta,omitzero"`
	ProtocolVersion   uint16             `json:"protocolVersion"`
	AgentCapabilities *AgentCapabilities `json:"agentCapabilities,omitempty"`
	AgentInfo         *Implementation    `json:"agentInfo,omitempty"`
	AuthMethods       []AuthMethod       `json:"authMethods,omitzero"`
}

type ClientCapabilities struct {
	Meta     Meta                    `json:"_meta,omitzero"`
	FS       *FileSystemCapabilities `json:"fs,omitempty"`
	Terminal bool                    `json:"terminal,omitempty"`
}

type FileSystemCapabilities struct {
	Meta          Meta `json:"_meta,omitzero"`
	ReadTextFile  bool `json:"readTextFile,omitempty"`
	WriteTextFile bool `json:"writeTextFile,omitempty"`
}

type AgentCapabilities struct {
	Meta                Meta                   `json:"_meta,omitzero"`
	LoadSession         bool                   `json:"loadSession,omitempty"`
	PromptCapabilities  *PromptCapabilities    `json:"promptCapabilities,omitempty"`
	Auth                *AgentAuthCapabilities `json:"auth,omitempty"`
	MCPCapabilities     *MCPCapabilities       `json:"mcpCapabilities,omitempty"`
	SessionCapabilities *SessionCapabilities   `json:"sessionCapabilities,omitempty"`
}

type PromptCapabilities struct {
	Meta            Meta `json:"_meta,omitzero"`
	Image           bool `json:"image,omitempty"`
	Audio           bool `json:"audio,omitempty"`
	EmbeddedContext bool `json:"embeddedContext,omitempty"`
}

type AgentAuthCapabilities struct {
	Meta   Meta                `json:"_meta,omitzero"`
	Logout *LogoutCapabilities `json:"logout,omitempty"`
}

type LogoutCapabilities struct {
	Meta Meta `json:"_meta,omitzero"`
}

type MCPCapabilities struct {
	Meta Meta `json:"_meta,omitzero"`
	HTTP bool `json:"http,omitempty"`
	SSE  bool `json:"sse,omitempty"`
}

type SessionCapabilities struct {
	Meta   Meta                       `json:"_meta,omitzero"`
	List   *SessionListCapabilities   `json:"list,omitempty"`
	Resume *SessionResumeCapabilities `json:"resume,omitempty"`
	Close  *SessionCloseCapabilities  `json:"close,omitempty"`
}

type SessionListCapabilities struct {
	Meta Meta `json:"_meta,omitzero"`
}
type SessionResumeCapabilities struct {
	Meta Meta `json:"_meta,omitzero"`
}
type SessionCloseCapabilities struct {
	Meta Meta `json:"_meta,omitzero"`
}

type AuthMethod struct {
	Meta        Meta   `json:"_meta,omitzero"`
	Type        string `json:"type,omitempty"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type AuthenticateRequest struct {
	Meta     Meta   `json:"_meta,omitzero"`
	MethodID string `json:"methodId"`
}

type AuthenticateResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}
type LogoutRequest struct {
	Meta Meta `json:"_meta,omitzero"`
}
type LogoutResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

type NewSessionRequest struct {
	Meta                  Meta        `json:"_meta,omitzero"`
	CWD                   string      `json:"cwd"`
	MCPServers            []MCPServer `json:"mcpServers"`
	AdditionalDirectories []string    `json:"additionalDirectories,omitzero"`
}

type NewSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitzero"`
	SessionID     string                `json:"sessionId"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

type LoadSessionRequest struct {
	Meta       Meta        `json:"_meta,omitzero"`
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers"`
}

type LoadSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitzero"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

type ResumeSessionRequest struct {
	Meta       Meta        `json:"_meta,omitzero"`
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers,omitempty"`
}

type ResumeSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitzero"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

type ListSessionsRequest struct {
	Meta   Meta   `json:"_meta,omitzero"`
	CWD    string `json:"cwd,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type ListSessionsResponse struct {
	Meta       Meta          `json:"_meta,omitzero"`
	Sessions   []SessionInfo `json:"sessions"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type SessionInfo struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd,omitempty"`
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type CloseSessionRequest struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
}

type CloseSessionResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

type PromptRequest struct {
	Meta      Meta           `json:"_meta,omitzero"`
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type PromptResponse struct {
	Meta       Meta       `json:"_meta,omitzero"`
	StopReason StopReason `json:"stopReason"`
}

type StopReason string

const (
	StopReasonEndTurn         StopReason = "end_turn"
	StopReasonMaxTokens       StopReason = "max_tokens"
	StopReasonMaxTurnRequests StopReason = "max_turn_requests"
	StopReasonRefusal         StopReason = "refusal"
	StopReasonCancelled       StopReason = "cancelled"
)

type CancelNotification struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
}

type SetSessionModeRequest struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

type SetSessionModeResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

type SetSessionConfigOptionRequest struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

type SetSessionConfigOptionResponse struct {
	Meta          Meta                  `json:"_meta,omitzero"`
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

type SessionModeState struct {
	Meta           Meta          `json:"_meta,omitzero"`
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

type SessionMode struct {
	Meta        Meta   `json:"_meta,omitzero"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type SessionConfigOption struct {
	Meta         Meta                        `json:"_meta,omitzero"`
	Type         string                      `json:"type"`
	ID           string                      `json:"id"`
	Name         string                      `json:"name"`
	Description  string                      `json:"description,omitempty"`
	Category     string                      `json:"category,omitempty"`
	CurrentValue string                      `json:"currentValue,omitempty"`
	Options      []SessionConfigSelectOption `json:"options,omitzero"`
}

type SessionConfigSelectOption struct {
	Meta        Meta   `json:"_meta,omitzero"`
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

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

type EnvVariable struct {
	Meta  Meta   `json:"_meta,omitzero"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HTTPHeader struct {
	Meta  Meta   `json:"_meta,omitzero"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type SessionNotification struct {
	Meta      Meta          `json:"_meta,omitzero"`
	SessionID string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}

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
	ConfigOptions     []SessionConfigOption `json:"configOptions,omitempty"`
	Entries           []PlanEntry           `json:"entries,omitempty"`
	UpdatedAt         *string               `json:"updatedAt,omitempty"`
}

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

type ToolKind string

const (
	ToolKindRead       ToolKind = "read"
	ToolKindEdit       ToolKind = "edit"
	ToolKindDelete     ToolKind = "delete"
	ToolKindMove       ToolKind = "move"
	ToolKindSearch     ToolKind = "search"
	ToolKindExecute    ToolKind = "execute"
	ToolKindThink      ToolKind = "think"
	ToolKindFetch      ToolKind = "fetch"
	ToolKindSwitchMode ToolKind = "switch_mode"
	ToolKindOther      ToolKind = "other"
)

type ToolCallStatus string

const (
	ToolCallPending    ToolCallStatus = "pending"
	ToolCallInProgress ToolCallStatus = "in_progress"
	ToolCallCompleted  ToolCallStatus = "completed"
	ToolCallFailed     ToolCallStatus = "failed"
)

type ToolCallLocation struct {
	Meta Meta   `json:"_meta,omitzero"`
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

type AvailableCommand struct {
	Meta        Meta                   `json:"_meta,omitzero"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Input       *AvailableCommandInput `json:"input,omitempty"`
}

type AvailableCommandInput struct {
	Meta Meta   `json:"_meta,omitzero"`
	Type string `json:"type"`
}

type PlanEntry struct {
	Meta     Meta              `json:"_meta,omitzero"`
	ID       string            `json:"id,omitempty"`
	Content  string            `json:"content"`
	Status   PlanEntryStatus   `json:"status"`
	Priority PlanEntryPriority `json:"priority,omitempty"`
}

type PlanEntryStatus string
type PlanEntryPriority string

type SessionInfoUpdate struct {
	Meta      Meta    `json:"_meta,omitzero"`
	Title     *string `json:"title,omitempty"`
	UpdatedAt *string `json:"updatedAt,omitempty"`
}

type RequestPermissionRequest struct {
	Meta      Meta               `json:"_meta,omitzero"`
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCallUpdate     `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

type RequestPermissionResponse struct {
	Meta    Meta                     `json:"_meta,omitzero"`
	Outcome RequestPermissionOutcome `json:"outcome"`
}

type PermissionOption struct {
	Meta     Meta                 `json:"_meta,omitzero"`
	OptionID string               `json:"optionId"`
	Name     string               `json:"name"`
	Kind     PermissionOptionKind `json:"kind"`
}

type PermissionOptionKind string

const (
	PermissionAllowOnce    PermissionOptionKind = "allow_once"
	PermissionAllowAlways  PermissionOptionKind = "allow_always"
	PermissionRejectOnce   PermissionOptionKind = "reject_once"
	PermissionRejectAlways PermissionOptionKind = "reject_always"
)

type RequestPermissionOutcome struct {
	Meta     Meta   `json:"_meta,omitzero"`
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

type ReadTextFileRequest struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Line      *int   `json:"line,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
}

type ReadTextFileResponse struct {
	Meta    Meta   `json:"_meta,omitzero"`
	Content string `json:"content"`
}

type WriteTextFileRequest struct {
	Meta      Meta   `json:"_meta,omitzero"`
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

type WriteTextFileResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

type CreateTerminalRequest struct {
	Meta            Meta          `json:"_meta,omitzero"`
	SessionID       string        `json:"sessionId"`
	Command         string        `json:"command"`
	Args            []string      `json:"args,omitzero"`
	CWD             string        `json:"cwd,omitempty"`
	Env             []EnvVariable `json:"env,omitzero"`
	OutputByteLimit *uint64       `json:"outputByteLimit,omitempty"`
}

type CreateTerminalResponse struct {
	Meta       Meta   `json:"_meta,omitzero"`
	TerminalID string `json:"terminalId"`
}

type TerminalOutputRequest struct {
	Meta       Meta   `json:"_meta,omitzero"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

type TerminalOutputResponse struct {
	Meta       Meta                `json:"_meta,omitzero"`
	Output     string              `json:"output"`
	Truncated  bool                `json:"truncated"`
	ExitStatus *TerminalExitStatus `json:"exitStatus,omitempty"`
}

type TerminalExitStatus struct {
	Meta     Meta    `json:"_meta,omitzero"`
	ExitCode *uint32 `json:"exitCode,omitempty"`
	Signal   *string `json:"signal,omitempty"`
}

type WaitForTerminalExitRequest struct {
	Meta       Meta   `json:"_meta,omitzero"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

type WaitForTerminalExitResponse = TerminalExitStatus

type KillTerminalRequest struct {
	Meta       Meta   `json:"_meta,omitzero"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

type KillTerminalResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}

type ReleaseTerminalRequest struct {
	Meta       Meta   `json:"_meta,omitzero"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

type ReleaseTerminalResponse struct {
	Meta Meta `json:"_meta,omitzero"`
}
