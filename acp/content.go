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
	Resource    *ResourceContents `json:"resource,omitempty"`
	Annotations *Annotations      `json:"annotations,omitempty"`
}

const (
	ContentTypeText     = "text"
	ContentTypeImage    = "image"
	ContentTypeAudio    = "audio"
	ContentTypeResource = "resource"
	ContentTypeLink     = "resource_link"
)

type Annotations struct {
	Audience []Role  `json:"audience,omitzero"`
	Priority float64 `json:"priority,omitempty"`
}

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type ResourceContents struct {
	Meta     Meta   `json:"_meta,omitzero"`
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     []byte `json:"blob,omitempty"`
}

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
	ToolCallContentTypeContent  = "content"
	ToolCallContentTypeDiff     = "diff"
	ToolCallContentTypeTerminal = "terminal"
)
