// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package clientgen

import "github.com/dave/jennifer/jen"

func emitClientHandlers(file *jen.File, ops []operation) {
	byGroup := map[string][]operation{}
	for _, op := range ops {
		if handledByClient(op) {
			byGroup[op.group] = append(byGroup[op.group], op)
		}
	}
	for _, group := range sortedKeys(byGroup) {
		handlerName := pascal(group) + "ClientHandler"
		var methods []jen.Code
		for i, op := range byGroup[group] {
			if i > 0 {
				methods = append(methods, jen.Line())
			}
			methods = append(methods, commented(op.funcName, op.description, handlerMethod(op)))
		}
		file.Commentf("%s handles all %s related client methods.", handlerName, group).Line().Type().Id(handlerName).Interface(methods...)
		file.Line()
	}
}

func handlerMethod(op operation) jen.Code {
	params := []jen.Code{jen.Id("context.Context"), jen.Op("*").Id(paramType(op))}
	if op.notifyType != "" {
		return jen.Id(op.funcName).Params(params...).Error()
	}
	return jen.Id(op.funcName).Params(params...).Params(jen.Op("*").Id(op.responseType), jen.Error())
}
