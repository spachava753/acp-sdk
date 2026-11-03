package rpcgen

import "slices"

// endpoint is one end of an ACP connection implemented by the SDK.
type endpoint struct {
	// conn is the Go type that sends requests and notifications to the peer.
	conn string
	// dispatch is the generated function that routes incoming messages.
	dispatch string
	filename string
}

var (
	agentEndpoint = endpoint{
		conn:     "AgentConnection",
		dispatch: "handleAgentRequest",
		filename: "agent_gen.go",
	}
	clientEndpoint = endpoint{
		conn:     "Client",
		dispatch: "handleClientRequest",
		filename: "client_gen.go",
	}
	endpoints = []endpoint{agentEndpoint, clientEndpoint}
)

func (e endpoint) peer() endpoint {
	if e == agentEndpoint {
		return clientEndpoint
	}
	return agentEndpoint
}

// sideRule lists the endpoints that handle methods on one x-side.
type sideRule struct {
	// handlers is empty for sides that do not generate RPC glue.
	handlers []endpoint
	// methods overrides handlers for individual methods.
	methods map[string][]endpoint
}

func (r sideRule) handlersFor(method string) []endpoint {
	if handlers, ok := r.methods[method]; ok {
		return handlers
	}
	return r.handlers
}

// sides maps every schema x-side to the endpoints that handle its methods.
// The schema groups methods into sides but only says in prose which way some of
// them travel, so that direction is recorded here. Generation fails for any
// x-side missing from this table rather than guessing.
var sides = map[string]sideRule{
	"agent":  {handlers: []endpoint{agentEndpoint}},
	"client": {handlers: []endpoint{clientEndpoint}},
	"both":   {handlers: []endpoint{agentEndpoint, clientEndpoint}},
	// Protocol-level messages such as $/cancel_request belong to no endpoint.
	"protocol": {},
	// Proxy Chains: a conductor is a client and a proxy is an agent. Both send
	// proxy/successor, but only a conductor sends proxy/initialize.
	"proxy": {
		handlers: []endpoint{agentEndpoint, clientEndpoint},
		methods: map[string][]endpoint{
			"proxy/initialize": {agentEndpoint},
		},
	},
}

func handles(handlers []endpoint, e endpoint) bool {
	return slices.Contains(handlers, e)
}
