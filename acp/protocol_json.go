// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"encoding/json"
)

func (r NewSessionRequest) MarshalJSON() ([]byte, error) {
	type alias NewSessionRequest
	a := alias(r)
	if a.MCPServers == nil {
		a.MCPServers = []MCPServer{}
	}
	return json.Marshal(a)
}

func (r LoadSessionRequest) MarshalJSON() ([]byte, error) {
	type alias LoadSessionRequest
	a := alias(r)
	if a.MCPServers == nil {
		a.MCPServers = []MCPServer{}
	}
	return json.Marshal(a)
}

func (r PromptRequest) MarshalJSON() ([]byte, error) {
	type alias PromptRequest
	a := alias(r)
	if a.Prompt == nil {
		a.Prompt = []ContentBlock{}
	}
	return json.Marshal(a)
}

func (s SessionModeState) MarshalJSON() ([]byte, error) {
	type alias SessionModeState
	a := alias(s)
	if a.AvailableModes == nil {
		a.AvailableModes = []SessionMode{}
	}
	return json.Marshal(a)
}

func (o SessionConfigOption) MarshalJSON() ([]byte, error) {
	type alias SessionConfigOption
	a := alias(o)
	if a.Type == "select" && a.Options.Flat == nil && a.Options.Groups == nil {
		a.Options.Flat = []SessionConfigSelectOption{}
	}
	return json.Marshal(a)
}

func (o SessionConfigSelectOptions) MarshalJSON() ([]byte, error) {
	if o.Groups != nil {
		return json.Marshal(o.Groups)
	}
	if o.Flat != nil {
		return json.Marshal(o.Flat)
	}
	return []byte("null"), nil
}

func (o *SessionConfigSelectOptions) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*o = SessionConfigSelectOptions{}
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) == 0 {
		o.Flat = []SessionConfigSelectOption{}
		return nil
	}
	var probe struct {
		Group string `json:"group"`
	}
	if err := json.Unmarshal(raw[0], &probe); err != nil {
		return err
	}
	if probe.Group != "" {
		return json.Unmarshal(data, &o.Groups)
	}
	return json.Unmarshal(data, &o.Flat)
}

func (g SessionConfigSelectGroup) MarshalJSON() ([]byte, error) {
	type alias SessionConfigSelectGroup
	a := alias(g)
	if a.Options == nil {
		a.Options = []SessionConfigSelectOption{}
	}
	return json.Marshal(a)
}

func (s MCPServer) MarshalJSON() ([]byte, error) {
	type alias MCPServer
	a := alias(s)
	switch a.Type {
	case "", "stdio":
		if a.Args == nil {
			a.Args = []string{}
		}
		if a.Env == nil {
			a.Env = []EnvVariable{}
		}
	case "http", "sse":
		if a.Headers == nil {
			a.Headers = []HTTPHeader{}
		}
	}
	return json.Marshal(a)
}

func (r ListSessionsResponse) MarshalJSON() ([]byte, error) {
	type alias ListSessionsResponse
	a := alias(r)
	if a.Sessions == nil {
		a.Sessions = []SessionInfo{}
	}
	return json.Marshal(a)
}

func (r SetSessionConfigOptionResponse) MarshalJSON() ([]byte, error) {
	type alias SetSessionConfigOptionResponse
	a := alias(r)
	if a.ConfigOptions == nil {
		a.ConfigOptions = []SessionConfigOption{}
	}
	return json.Marshal(a)
}

func (u SessionUpdate) MarshalJSON() ([]byte, error) {
	type alias SessionUpdate
	a := alias(u)
	switch a.SessionUpdate {
	case "available_commands_update":
		if a.AvailableCommands == nil {
			a.AvailableCommands = []AvailableCommand{}
		}
	case "config_option_update":
		if a.ConfigOptions == nil {
			a.ConfigOptions = []SessionConfigOption{}
		}
	case "plan":
		if a.Entries == nil {
			a.Entries = []PlanEntry{}
		}
	}
	return json.Marshal(a)
}

func (r RequestPermissionRequest) MarshalJSON() ([]byte, error) {
	type alias RequestPermissionRequest
	a := alias(r)
	if a.Options == nil {
		a.Options = []PermissionOption{}
	}
	return json.Marshal(a)
}
