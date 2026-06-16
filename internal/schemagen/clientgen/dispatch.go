// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package clientgen

import "github.com/dave/jennifer/jen"

func emitClientHandle(file *jen.File, ops []operation) {
	var cases []jen.Code
	for _, op := range ops {
		if handledByClient(op) {
			cases = append(cases, handleCase(op))
		}
	}
	if len(cases) == 0 {
		return
	}
	cases = append(cases, jen.Default().Block(jen.Return(jen.Nil(), jen.Id("methodNotFound").Call(jen.Id("req").Dot("Method")))))
	file.Func().Params(jen.Id("c").Op("*").Id("Client")).Id("handle").Params(
		jen.Id("ctx").Id("context.Context"),
		jen.Id("req").Op("*").Id("jsonrpc.Request"),
	).Params(jen.Any(), jen.Error()).Block(
		jen.Id("jsonrpc2.Async").Call(jen.Id("ctx")),
		jen.If(jen.Id("c").Dot("handler").Op("==").Nil()).Block(jen.Return(jen.Nil(), jen.Id("methodNotFound").Call(jen.Id("req").Dot("Method")))),
		jen.Switch(jen.Id("req").Dot("Method")).Block(cases...),
	)
}

func handleCase(op operation) jen.Code {
	handlerName := pascal(op.group) + "ClientHandler"
	body := []jen.Code{
		jen.Id("handler").Op(",").Id("ok").Op(":=").Id("c").Dot("handler").Assert(jen.Id(handlerName)),
		jen.If(jen.Op("!").Id("ok")).Block(jen.Return(jen.Nil(), jen.Id("methodNotFound").Call(jen.Id("req").Dot("Method")))),
		jen.Id("params").Op(",").Id("err").Op(":=").Id("decodeParams").Index(jen.Id(paramType(op))).Call(jen.Id("req")),
		jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Return(jen.Nil(), jen.Id("err"))),
	}
	if op.notifyType != "" {
		body = append(body, jen.Return(jen.Nil(), jen.Id("handler").Dot(op.funcName).Call(jen.Id("ctx"), jen.Id("params"))))
	} else {
		body = append(body, jen.Return(jen.Id("rpcResult").Call(jen.Id("handler").Dot(op.funcName).Call(jen.Id("ctx"), jen.Id("params")))))
	}
	return jen.Case(jen.Id(op.constName)).Block(body...)
}
