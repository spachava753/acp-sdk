// SPDX-License-Identifier: Apache-2.0
// Upstream: agentclientprotocol/rust-sdk@050cd94c081056ace37a7b89abfed7f6220b47eb.
package schema_test

import (
	"context"

	"github.com/spachava753/acp-sdk/acp"
)

// Rust's assert_request_response_pair/assert_request_pair/assert_response_type
// helpers check exact public associated response types, not just equal JSON.
// These expected public method-expression signatures are Go's compile-time
// counterpart. An API returning a different response type with identical JSON
// must not pass by being hidden behind exchange's (any, error) adapter.
// Sources: tests/schema_elicitation.rs, schema_session_delete.rs,
// schema_llm_providers.rs and proxy_schema_types.rs; per-function mapping.json.
var (
	_ func(*acp.AgentConnection, context.Context, *acp.CreateElicitationRequest) (*acp.CreateElicitationResponse, error) = (*acp.AgentConnection).ElicitationCreate
	_ func(*acp.Client, context.Context, *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error)                  = (*acp.Client).SessionDelete
	_ func(*acp.Client, context.Context, *acp.ListProvidersRequest) (*acp.ListProvidersResponse, error)                  = (*acp.Client).ProvidersList
	_ func(*acp.Client, context.Context, *acp.SetProviderRequest) (*acp.SetProviderResponse, error)                      = (*acp.Client).ProvidersSet
	_ func(*acp.Client, context.Context, *acp.DisableProviderRequest) (*acp.DisableProviderResponse, error)              = (*acp.Client).ProvidersDisable
	_ func(*acp.Client, context.Context, *acp.InitializeProxyRequest) (*acp.InitializeProxyResponse, error)              = (*acp.Client).ProxyInitialize
	_ func(*acp.Client, context.Context, *acp.ProxySuccessorRequest) (*acp.ProxySuccessorResponse, error)                = (*acp.Client).ProxySuccessor
	_ func(*acp.AgentConnection, context.Context, *acp.ProxySuccessorRequest) (*acp.ProxySuccessorResponse, error)       = (*acp.AgentConnection).ProxySuccessor
)

// These methods also appear in the source aggregate-route and typed-result
// assertions. Keep their concrete Go response contract explicit as well.
var (
	_ func(*acp.Client, context.Context, *acp.AuthStatusRequest) (*acp.AuthStatusResponse, error)             = (*acp.Client).AuthStatus
	_ func(*acp.Client, context.Context, *acp.ArchiveSessionRequest) (*acp.ArchiveSessionResponse, error)     = (*acp.Client).SessionArchive
	_ func(*acp.Client, context.Context, *acp.UnarchiveSessionRequest) (*acp.UnarchiveSessionResponse, error) = (*acp.Client).SessionUnarchive
	_ func(*acp.Client, context.Context, *acp.SessionStatusRequest) (*acp.SessionStatusResponse, error)       = (*acp.Client).SessionStatus
	_ func(*acp.Client, context.Context, *acp.SetSessionTitleRequest) (*acp.SetSessionTitleResponse, error)   = (*acp.Client).SessionSetTitle
	_ func(*acp.Client, context.Context, *acp.PromptRequest) (*acp.PromptResponse, error)                     = (*acp.Client).SessionPrompt
	_ func(*acp.AgentConnection, context.Context, *acp.MessageMcpRequest) (*acp.MessageMcpResponse, error)    = (*acp.AgentConnection).McpMessage
)
