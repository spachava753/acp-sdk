// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package agentgen

import "github.com/dave/jennifer/jen"

func emitAgentConnectionMethods(file *jen.File, ops []operation) {
	for _, op := range ops {
		if op.side == "agent" {
			continue
		}
		file.Add(commented(op.funcName, op.description, agentConnectionMethod(op)))
		file.Line()
	}
}

func agentConnectionMethod(op operation) jen.Code {
	params := []jen.Code{
		jen.Id("c").Op("*").Id("AgentConnection"),
		jen.Id("ctx").Id("context.Context"),
		jen.Id("params").Op("*").Id(paramType(op)),
	}
	if op.notifyType != "" {
		return jen.Func().Params(params[0]).Id(op.funcName).Params(params[1:]...).Error().Block(
			jen.Return(jen.Id("notify").Call(jen.Id("ctx"), jen.Id("c").Dot("rpc").Dot("conn"), jen.Id(op.constName), jen.Id("params"))),
		)
	}
	return jen.Func().Params(params[0]).Id(op.funcName).Params(params[1:]...).Params(jen.Op("*").Id(op.responseType), jen.Error()).Block(
		jen.Return(jen.Id("call").Index(jen.Id(op.responseType)).Call(jen.Id("ctx"), jen.Id("c").Dot("rpc").Dot("conn"), jen.Id(op.constName), jen.Id("params"))),
	)
}
