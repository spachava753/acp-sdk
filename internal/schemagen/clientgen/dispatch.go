// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package clientgen

import "github.com/dave/jennifer/jen"

func emitClientHandle(file *jen.File, ops []operation) {
	byMethod := map[string][]operation{}
	for _, op := range ops {
		if handledByClient(op) {
			byMethod[op.constName] = append(byMethod[op.constName], op)
		}
	}

	var cases []jen.Code
	for _, method := range sortedKeys(byMethod) {
		cases = append(cases, handleCase(byMethod[method]))
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

func handleCase(ops []operation) jen.Code {
	if len(ops) == 1 {
		return handleSingleCase(ops[0])
	}

	var callOp, notifyOp operation
	for _, op := range ops {
		if op.notifyType != "" {
			notifyOp = op
		} else {
			callOp = op
		}
	}
	if callOp.method != "" && notifyOp.method != "" {
		return handleOverloadedCase(callOp, notifyOp)
	}
	return handleSingleCase(ops[0])
}

func handleSingleCase(op operation) jen.Code {
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

func handleOverloadedCase(callOp, notifyOp operation) jen.Code {
	handlerName := pascal(callOp.group) + "ClientHandler"
	body := []jen.Code{
		jen.Id("handler").Op(",").Id("ok").Op(":=").Id("c").Dot("handler").Assert(jen.Id(handlerName)),
		jen.If(jen.Op("!").Id("ok")).Block(jen.Return(jen.Nil(), jen.Id("methodNotFound").Call(jen.Id("req").Dot("Method")))),
		jen.If(jen.Id("req").Dot("IsCall").Call()).Block(
			jen.Id("params").Op(",").Id("err").Op(":=").Id("decodeParams").Index(jen.Id(paramType(callOp))).Call(jen.Id("req")),
			jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Return(jen.Nil(), jen.Id("err"))),
			jen.Return(jen.Id("rpcResult").Call(jen.Id("handler").Dot(callOp.funcName).Call(jen.Id("ctx"), jen.Id("params")))),
		),
		jen.Id("params").Op(",").Id("err").Op(":=").Id("decodeParams").Index(jen.Id(paramType(notifyOp))).Call(jen.Id("req")),
		jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Return(jen.Nil(), jen.Id("err"))),
		jen.Return(jen.Nil(), jen.Id("handler").Dot(notifyOp.funcName).Call(jen.Id("ctx"), jen.Id("params"))),
	}
	return jen.Case(jen.Id(callOp.constName)).Block(body...)
}
