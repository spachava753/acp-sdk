// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package agentgen

import (
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

func operations(schema *jsonschema.Schema) []operation {
	byType := map[string]operation{}
	addEnvelopeOps(schema, byType, "ClientRequest")
	addEnvelopeOps(schema, byType, "ClientNotification")
	addEnvelopeOps(schema, byType, "AgentRequest")
	addEnvelopeOps(schema, byType, "AgentNotification")

	for name, def := range schema.Defs {
		method, _ := def.Extra["x-method"].(string)
		if method == "" {
			continue
		}
		side, _ := def.Extra["x-side"].(string)
		if side == "protocol" {
			continue
		}
		op := byType[name]
		if op.method == "" {
			op.method = method
			op.side = side
			op.group = methodGroup(method)
			op.constName = "Method" + pascal(method)
		}
		if strings.HasSuffix(name, "Request") {
			op.requestType = name
		} else if strings.HasSuffix(name, "Response") {
			op.responseType = name
		} else if strings.HasSuffix(name, "Notification") {
			op.notifyType = name
		}
		if op.funcName == "" {
			op.funcName = operationName(name)
		}
		byType[name] = op
	}

	byMethod := map[string]operation{}
	for _, name := range sortedKeys(byType) {
		op := byType[name]
		if op.method == "" {
			continue
		}
		merged := byMethod[op.method]
		if merged.method == "" {
			merged = op
		} else {
			if merged.description == "" {
				merged.description = op.description
			}
			if merged.funcName == "" {
				merged.funcName = op.funcName
			}
			if merged.requestType == "" {
				merged.requestType = op.requestType
			}
			if merged.responseType == "" {
				merged.responseType = op.responseType
			}
			if merged.notifyType == "" {
				merged.notifyType = op.notifyType
			}
		}
		byMethod[op.method] = merged
	}

	var ops []operation
	for _, op := range byMethod {
		if op.side == "protocol" || op.method == "" {
			continue
		}
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].constName < ops[j].constName })
	return ops
}

func addEnvelopeOps(schema *jsonschema.Schema, byType map[string]operation, envelope string) {
	def := schema.Defs[envelope]
	if def == nil || def.Properties == nil || def.Properties["params"] == nil {
		return
	}
	for _, variant := range paramVariants(def.Properties["params"]) {
		name := refName(variant.AllOf[0].Ref)
		if name == "" {
			continue
		}
		typeDef := schema.Defs[name]
		if typeDef == nil {
			continue
		}
		method, _ := typeDef.Extra["x-method"].(string)
		side, _ := typeDef.Extra["x-side"].(string)
		if method == "" || side == "protocol" {
			continue
		}
		op := byType[name]
		op.method = method
		op.side = side
		op.group = methodGroup(method)
		op.constName = "Method" + pascal(method)
		op.funcName = operationName(name)
		if strings.HasSuffix(name, "Notification") {
			_, action, ok := strings.Cut(method, "/")
			if ok {
				op.funcName = pascal(action)
			}
		}
		op.description = variant.Description
		if strings.HasSuffix(name, "Request") {
			op.requestType = name
		} else if strings.HasSuffix(name, "Notification") {
			op.notifyType = name
		}
		byType[name] = op
	}
}

func paramVariants(params *jsonschema.Schema) []*jsonschema.Schema {
	var variants []*jsonschema.Schema
	for _, outer := range params.AnyOf {
		for _, inner := range outer.AnyOf {
			if len(inner.AllOf) == 1 && inner.AllOf[0].Ref != "" {
				variants = append(variants, inner)
			}
		}
	}
	return variants
}
