// schemagen is a package for generating the types and rpc methods for the ACP wire.
//
// This package is not a complete JSON schema code generator, and
// instead is highly specific to the structure of the ACP schema.
// It expecs that:
// - if using `allOf`, only one option is supplied
//
// TODO: handle union types. In particular, content blocks, and session update
// 		I'm thinking we actually just pool all of the fields for a union type together, and the just define 
// TODO: can we generate the rpc methods?
// TODO: how to differentiate between notification and rpc?
// TODO: descriptions should come from schema
// TODO: handle enums like stop_reason
// TODO: when to omitzero vs omitempty
// TODO: could we get away with template based code gen somehow?
// TODO: need to handle const as well, maybe could be acheived with custom json marshal and unmarshal?
// TODO: should we consider using generics to flatten unions
//
// Notes on generation:
// - if x-side is client, means it is a method for agent-connection to invoke to send a req-resp to client
// - if x-side is agent, means it is a method for client to invoke to send a req-resp to agent
// - if x-side is client, and the type is *Notification, it is a notification from the agent to the client
// - if x-side is agent, and the type is *Notification, it is a notification from the client to the agent
// - x-method defines the name of the method, and for req-resp methods, this will appear twice (once for req and once for resp), and for notications, this will appear once in the schema
// since methods are grouped like `terminal/create` and `terminal/release`, we will use the group for generating the interfaces like TerminalHandler for client impls
package schemagen
