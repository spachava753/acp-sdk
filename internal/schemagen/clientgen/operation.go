// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package clientgen

import (
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

func operations(schema *jsonschema.Schema) []operation {
	byType := map[string]operation{}
	goNames := goDefinitionNames(schema.Defs)
	addEnvelopeOps(schema, byType, goNames, "ClientRequest")
	addEnvelopeOps(schema, byType, goNames, "ClientNotification")
	addEnvelopeOps(schema, byType, goNames, "AgentRequest")
	addEnvelopeOps(schema, byType, goNames, "AgentNotification")

	for name, def := range schema.Defs {
		method, _ := def.Extra["x-method"].(string)
		if method == "" {
			continue
		}
		side, _ := def.Extra["x-side"].(string)
		if side == "protocol" {
			continue
		}
		goName := goNames[name]
		op := byType[name]
		if op.method == "" {
			op.method = method
			op.side = side
			op.group = methodGroup(method)
			op.constName = "Method" + pascal(method)
		}
		if strings.HasSuffix(goName, "Request") {
			op.requestType = goName
		} else if strings.HasSuffix(goName, "Response") {
			op.responseType = goName
		} else if strings.HasSuffix(goName, "Notification") {
			op.notifyType = goName
		}
		if op.funcName == "" {
			op.funcName = operationName(goName)
		}
		byType[name] = op
	}

	byMethodKind := map[string]operation{}
	for _, name := range sortedKeys(byType) {
		op := byType[name]
		if op.method == "" {
			continue
		}
		key := operationMergeKey(op)
		merged := byMethodKind[key]
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
		byMethodKind[key] = merged
	}

	var ops []operation
	for _, key := range sortedKeys(byMethodKind) {
		op := byMethodKind[key]
		if op.side == "protocol" || op.method == "" {
			continue
		}
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].constName != ops[j].constName {
			return ops[i].constName < ops[j].constName
		}
		if ops[i].funcName != ops[j].funcName {
			return ops[i].funcName < ops[j].funcName
		}
		return paramType(ops[i]) < paramType(ops[j])
	})
	return ops
}

func operationMergeKey(op operation) string {
	kind := "call"
	if op.notifyType != "" && op.requestType == "" && op.responseType == "" {
		kind = "notify"
	}
	return op.method + "\x00" + kind
}

func addEnvelopeOps(schema *jsonschema.Schema, byType map[string]operation, goNames map[string]string, envelope string) {
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
		goName := goNames[name]
		op := byType[name]
		op.method = method
		op.side = side
		op.group = methodGroup(method)
		op.constName = "Method" + pascal(method)
		op.funcName = operationName(goName)
		if strings.HasSuffix(goName, "Notification") {
			_, action, ok := strings.Cut(method, "/")
			if ok {
				op.funcName = pascal(action)
			}
		}
		op.description = variant.Description
		if strings.HasSuffix(goName, "Request") {
			op.requestType = goName
		} else if strings.HasSuffix(goName, "Notification") {
			op.notifyType = goName
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
