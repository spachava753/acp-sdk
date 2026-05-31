// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

// ContentBlock represents displayable ACP content. The fields mirror MCP's
// content block wire shape so MCP tool results can be forwarded directly.
type ContentBlock struct {
	Meta        Meta              `json:"_meta,omitzero"`
	Type        string            `json:"type"`
	Text        string            `json:"text,omitempty"`
	Data        []byte            `json:"data,omitempty"`
	MIMEType    string            `json:"mimeType,omitempty"`
	URI         string            `json:"uri,omitempty"`
	Name        string            `json:"name,omitempty"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Size        *int64            `json:"size,omitempty"`
	Resource    *ResourceContents `json:"resource,omitempty"`
	Annotations *Annotations      `json:"annotations,omitempty"`
}

const (
	// ContentTypeText identifies Markdown-capable text content.
	ContentTypeText = "text"
	// ContentTypeImage identifies base64-encoded image content.
	ContentTypeImage = "image"
	// ContentTypeAudio identifies base64-encoded audio content.
	ContentTypeAudio = "audio"
	// ContentTypeResource identifies embedded resource contents.
	ContentTypeResource = "resource"
	// ContentTypeLink identifies a link to a resource the agent can access.
	ContentTypeLink = "resource_link"
)

// Annotations describe optional presentation hints for content.
type Annotations struct {
	Audience []Role  `json:"audience,omitzero"`
	Priority float64 `json:"priority,omitempty"`
}

// Role identifies the intended recipient of annotated content.
type Role string

const (
	// RoleUser marks content intended for the user.
	RoleUser Role = "user"
	// RoleAssistant marks content intended for the assistant.
	RoleAssistant Role = "assistant"
)

// ResourceContents contains text or binary data embedded in a content block.
type ResourceContents struct {
	Meta     Meta   `json:"_meta,omitzero"`
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     []byte `json:"blob,omitempty"`
}

// ToolCallContent represents displayable output attached to a tool call.
type ToolCallContent struct {
	Meta       Meta          `json:"_meta,omitzero"`
	Type       string        `json:"type"`
	Content    *ContentBlock `json:"content,omitempty"`
	Path       string        `json:"path,omitempty"`
	OldText    *string       `json:"oldText,omitempty"`
	NewText    string        `json:"newText,omitempty"`
	TerminalID string        `json:"terminalId,omitempty"`
}

const (
	// ToolCallContentTypeContent embeds a standard content block in a tool call.
	ToolCallContentTypeContent = "content"
	// ToolCallContentTypeDiff describes a text edit produced by a tool call.
	ToolCallContentTypeDiff = "diff"
	// ToolCallContentTypeTerminal references terminal output produced by a tool call.
	ToolCallContentTypeTerminal = "terminal"
)
