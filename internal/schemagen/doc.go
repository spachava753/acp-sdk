// Package schemagen generates ACP protocol types and JSON-RPC glue from the ACP
// JSON Schema.
//
// This package is intentionally not a general-purpose JSON Schema code generator.
// It is built around the shape of internal/schemagen/schema.json, which mirrors
// acp/schema.json. Unsupported schema forms should fail loudly rather than
// producing approximate Go code.
//
// # Schema structure
//
// The schema has two layers:
//
//   - Concrete protocol definitions under $defs. These become Go types and, when
//     they include x-method and x-side, define RPC payloads.
//   - JSON-RPC envelope definitions under $defs, such as ClientRequest,
//     ClientResponse, ClientNotification, AgentRequest, AgentResponse, and
//     AgentNotification. These describe which concrete payloads can appear in
//     request, response, and notification envelopes, and provide RPC method
//     documentation.
//
// The top-level schema has title, $schema, and anyOf entries for Agent, Client,
// and ProtocolLevel messages. The top-level Agent and Client titles describe
// the side that sends the JSON-RPC envelope, not the side that implements a
// method.
//
// # RPC direction
//
// Concrete request, response, and notification payload definitions use x-method
// and x-side:
//
//   - x-method is the JSON-RPC method name, such as "session/new".
//   - x-side "agent" means the agent implements the method. A client sends the
//     request or notification to the agent.
//   - x-side "client" means the client implements the method. An agent sends the
//     request or notification to the client.
//   - x-side "both" means both sides implement the same method shape.
//   - x-side "protocol" is for protocol-level messages such as
//     "$/cancel_request" and should be handled separately from ACP app methods.
//
// A request/response method is represented by two concrete definitions with the
// same x-method and x-side: one *Request and one *Response. A notification is
// represented by one concrete *Notification definition with x-method and x-side.
// Method grouping is derived from the path segment before '/', so methods like
// "terminal/create" and "terminal/release" belong to a Terminal handler group.
//
// # Method descriptions
//
// Do not use the concrete request or response type description as the RPC method
// description. Concrete $defs descriptions are type comments only.
//
// RPC method descriptions live on the JSON-RPC envelope union branches that
// reference concrete payload definitions. The generator should find these by
// matching the branch allOf $ref, not by assuming a fixed branch index. Examples:
//
//   - Agent-implemented request methods are described under
//     $defs.ClientRequest.properties.params.anyOf[0].anyOf[i].description, where
//     anyOf[i].allOf[0].$ref references the concrete *Request def.
//   - Agent-implemented notifications are described under
//     $defs.ClientNotification.properties.params.anyOf[0].anyOf[i].description.
//   - Client-implemented request methods are described under
//     $defs.AgentRequest.properties.params.anyOf[0].anyOf[i].description.
//   - Client-implemented notifications are described under
//     $defs.AgentNotification.properties.params.anyOf[0].anyOf[i].description.
//
// Response descriptions remain on the generated response type comments; they
// should not be repeated on the generated RPC method comments.
//
// # Type generation
//
// All concrete definitions referenced by generated payload types must be emitted
// or intentionally mapped to a known existing Go type. Do not drop nested $defs
// references such as SessionId just because they are reached through allOf or
// a property reference.
//
// The schema forms currently expected by the first implementation are:
//
//   - type "object" with properties and required.
//   - primitive types string, integer, number, boolean, object, and null.
//   - type arrays for nullable values, especially ["object", "null"] for
//     _meta-like extension maps.
//   - array items with a single item schema.
//   - $ref to #/$defs/Name.
//   - allOf with exactly one entry, used as a wrapped $ref.
//   - anyOf for JSON-RPC envelope unions and simple nullable fields.
//
// Object field names are derived from JSON property names. JSON tags should omit
// optional fields and keep required fields non-omitempty. The _meta property maps
// to Meta and should use omitzero to match the existing ACP package style.
//
// # Generated files
//
// Generate should produce separate files for stable concerns:
//
//   - types_gen.go: generated Go types for concrete schema definitions.
//   - agent_gen.go: constants, handler interfaces, AgentConnection outbound
//     helpers, and agent-side request dispatch.
//   - client_gen.go: client handler interfaces, Client outbound helpers, and
//     client-side request dispatch.
//
// Constants for JSON-RPC method names should be generated once per method name
// and shared by agent and client glue.
//
// # Testdata authoring
//
// Each fixture directory under testdata should contain one schema.json input and
// one or more expected output files using the .testdata suffix. The test strips
// .testdata from expected filenames before comparing with Generate output. For
// example, agent_gen.go.testdata expects a generated file named agent_gen.go.
//
// Fixture schemas should be small but structurally faithful to acp/schema.json:
//
//   - Include top-level $schema, title, and Agent/Client/ProtocolLevel anyOf
//     structure.
//   - Include ClientRequest, ClientResponse, ClientNotification, AgentRequest,
//     AgentResponse, and AgentNotification envelope defs when testing RPC glue.
//   - Put x-method and x-side on concrete request, response, and notification
//     payload defs.
//   - Put RPC method descriptions on envelope union branches, and type
//     descriptions on concrete $defs.
//   - Include nested $defs references such as SessionId to prove referenced
//     definitions are generated.
//   - Keep payload fields simple in early fixtures so failures isolate schema
//     traversal and RPC generation before covering complex JSON Schema features.
//
// # Current fixtures
//
// Existing fixture directories cover:
//
//   - testdata/a: a compact ACP-shaped schema with agent and client request,
//     response, and notification payloads; nested $defs references; shared type
//     generation; and distinct type descriptions versus RPC method descriptions.
//   - testdata/same_group: multiple request/response methods in the same
//     method group for both agent-implemented and client-implemented methods.
//     This verifies grouped handler generation, grouped outbound helpers, and
//     switch ordering within one group.
//
// # Additional test cases to add
//
// Future fixtures should cover:
//
//   - Methods across multiple groups to verify interface naming.
//   - x-side "both" methods, such as mcp/message.
//   - x-side "protocol" notifications and whether they are generated, skipped,
//     or routed through a separate protocol handler.
//   - Optional fields, required fields, nullable fields, and default values.
//   - allOf $ref wrappers in properties and response result unions.
//   - anyOf unions used for real protocol variants, such as content blocks and
//     session updates.
//   - enums and const discriminators.
//   - arrays of primitives, arrays of refs, maps/additionalProperties, and raw
//     arbitrary JSON values.
//   - Naming edge cases: initialisms, method names with underscores, method
//     names with '$/', and JSON field names that need Go initialism handling.
//   - Comment generation for multiline Markdown descriptions.
package schemagen
