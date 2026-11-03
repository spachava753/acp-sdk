package rpcgen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/acp-sdk/internal/schemagen/typegen"
)

type kind int

const (
	request kind = iota
	notification
)

func (k kind) String() string {
	if k == notification {
		return "notification"
	}
	return "request"
}

// operation is one request/response pair or one notification.
type operation struct {
	method    string
	kind      kind
	side      string
	handlers  []endpoint
	constName string
	// name is the Go method name used by both the sending connection and the
	// handler, so a message reads the same on either end.
	name        string
	params      string
	result      string
	description string
}

func (op operation) String() string {
	return fmt.Sprintf("%s %s", op.method, op.kind)
}

func (op operation) handledBy(e endpoint) bool {
	return handles(op.handlers, e)
}

func (op operation) sentBy(e endpoint) bool {
	return op.handledBy(e.peer())
}

func (op operation) handlerInterface() string {
	return op.name + "Handler"
}

type operationKey struct {
	method string
	kind   kind
}

// operations builds the RPC operations described by x-method definitions.
// It panics when the schema uses a side or shape the generator cannot map.
func operations(schema *jsonschema.Schema) []operation {
	typeNames := typegen.DefinitionNames(schema.Defs)
	descriptions := envelopeDescriptions(schema)

	byKey := map[operationKey]*operation{}
	for _, name := range sortedKeys(schema.Defs) {
		def := schema.Defs[name]
		method, _ := def.Extra["x-method"].(string)
		if method == "" {
			continue
		}
		side, _ := def.Extra["x-side"].(string)
		rule, ok := sides[side]
		if !ok {
			panic(fmt.Sprintf("schemagen: %s has x-side %q, which has no endpoint mapping in rpcgen.sides", name, side))
		}
		handlers := rule.handlersFor(method)
		if len(handlers) == 0 {
			continue
		}

		typeName := typeNames[name]
		var role string
		k := request
		switch {
		case strings.HasSuffix(typeName, "Request"):
			role = "params"
		case strings.HasSuffix(typeName, "Response"):
			role = "result"
		case strings.HasSuffix(typeName, "Notification"):
			role = "params"
			k = notification
		default:
			panic(fmt.Sprintf("schemagen: %s has x-method %q but is not named *Request, *Response, or *Notification", name, method))
		}

		key := operationKey{method: method, kind: k}
		op := byKey[key]
		if op == nil {
			op = &operation{method: method, kind: k, side: side, handlers: handlers}
			byKey[key] = op
		}
		if op.side != side {
			panic(fmt.Sprintf("schemagen: %s definitions use both x-side %q and %q", op, op.side, side))
		}
		target := &op.params
		if role == "result" {
			target = &op.result
		}
		if *target != "" {
			panic(fmt.Sprintf("schemagen: %s has more than one %s definition: %s and %s", op, role, *target, typeName))
		}
		*target = typeName
		if role == "params" {
			op.description = descriptions[name]
		}
	}

	ops := make([]operation, 0, len(byKey))
	for _, op := range byKey {
		if op.params == "" {
			panic(fmt.Sprintf("schemagen: %s has no params definition", op))
		}
		if op.kind == request && op.result == "" {
			panic(fmt.Sprintf("schemagen: %s has no *Response definition", op))
		}
		ops = append(ops, *op)
	}
	assignNames(ops, byKey)
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].constName != ops[j].constName {
			return ops[i].constName < ops[j].constName
		}
		return ops[i].kind < ops[j].kind
	})
	validateNames(ops, typeNames)
	return ops
}

// assignNames derives Go names from the wire method, so they are unique for
// each method. When a method has both forms, the notification is suffixed.
func assignNames(ops []operation, byKey map[operationKey]*operation) {
	methods := map[string]bool{}
	for _, op := range ops {
		methods[op.method] = true
	}
	// Distinct wire methods can share a Go spelling, as in "a/b-c" and "a/b_c".
	used := map[string]bool{}
	constNames := map[string]string{}
	for _, method := range sortedKeys(methods) {
		constNames[method] = uniqueName("Method"+typegen.PascalIdentifier(method), used)
	}
	for i := range ops {
		op := &ops[i]
		op.constName = constNames[op.method]
		op.name = strings.TrimPrefix(op.constName, "Method")
		if op.kind == notification && byKey[operationKey{method: op.method, kind: request}] != nil {
			op.name += "Notification"
		}
	}
}

// validateNames checks the generated identifiers against each other and the
// generated types. Operation names are methods on connection types and on any
// handler implementing several interfaces, so they must be unique.
func validateNames(ops []operation, typeNames map[string]string) {
	types := map[string]string{}
	for raw, name := range typeNames {
		types[name] = raw
	}
	methods := map[string]operation{}
	identifiers := map[string]string{}
	declare := func(identifier, owner string) {
		if raw, ok := types[identifier]; ok {
			panic(fmt.Sprintf("schemagen: %s generates %s, which collides with the type for schema definition %s", owner, identifier, raw))
		}
		if prev, ok := identifiers[identifier]; ok && prev != owner {
			panic(fmt.Sprintf("schemagen: %s and %s both generate %s", prev, owner, identifier))
		}
		identifiers[identifier] = owner
	}
	for _, op := range ops {
		if prev, ok := methods[op.name]; ok {
			panic(fmt.Sprintf("schemagen: %s and %s both generate Go method %s", prev, op, op.name))
		}
		methods[op.name] = op
		declare(op.constName, op.method)
		declare(op.handlerInterface(), op.String())
	}
}

// envelopeDescriptions returns RPC method documentation keyed by payload
// definition. It lives on the branches of JSON-RPC envelope unions such as
// ClientRequest and ProxyNotification, not on the payload types themselves.
func envelopeDescriptions(schema *jsonschema.Schema) map[string]string {
	descriptions := map[string]string{}
	for _, name := range sortedKeys(schema.Defs) {
		def := schema.Defs[name]
		if def.Extra["x-method"] != nil || def.Properties == nil || def.Properties["params"] == nil {
			continue
		}
		for _, group := range def.Properties["params"].AnyOf {
			for _, variant := range group.AnyOf {
				if len(variant.AllOf) != 1 || variant.AllOf[0].Ref == "" || variant.Description == "" {
					continue
				}
				ref := strings.TrimPrefix(variant.AllOf[0].Ref, "#/$defs/")
				if _, ok := descriptions[ref]; !ok {
					descriptions[ref] = variant.Description
				}
			}
		}
	}
	return descriptions
}

func uniqueName(name string, used map[string]bool) string {
	candidate := name
	for suffix := 2; used[candidate]; suffix++ {
		candidate = fmt.Sprintf("%s%d", name, suffix)
	}
	used[candidate] = true
	return candidate
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
