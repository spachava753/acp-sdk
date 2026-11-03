// Package rpcgen generates ACP method constants, handler interfaces, outgoing
// connection methods, and incoming dispatch from the schema's x-method
// definitions.
package rpcgen

import (
	"bytes"

	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/acp-sdk/internal/schemagen/typegen"
)

const (
	jsonrpcPath  = "github.com/spachava753/acp-sdk/jsonrpc"
	jsonrpc2Path = "github.com/spachava753/acp-sdk/internal/jsonrpc2"
)

// File is a generated Go source file.
type File struct {
	Name     string
	Contents []byte
}

// Generate returns the RPC files for schema.
func Generate(schema *jsonschema.Schema) []File {
	if schema == nil || len(schema.Defs) == 0 {
		return nil
	}
	ops := operations(schema)
	if len(ops) == 0 {
		return nil
	}
	files := []File{{Name: "methods_gen.go", Contents: render(methodsFile(ops))}}
	for _, e := range endpoints {
		files = append(files, File{Name: e.filename, Contents: render(endpointFile(e, ops))})
	}
	return files
}

func newFile() *jen.File {
	file := jen.NewFile("acp")
	file.ImportName(jsonrpcPath, "jsonrpc")
	file.ImportName(jsonrpc2Path, "jsonrpc2")
	return file
}

func render(file *jen.File) []byte {
	var out bytes.Buffer
	if err := file.Render(&out); err != nil {
		panic(err)
	}
	return out.Bytes()
}

func methodsFile(ops []operation) *jen.File {
	file := newFile()
	var consts []jen.Code
	seen := map[string]bool{}
	for _, op := range ops {
		if seen[op.constName] {
			continue
		}
		seen[op.constName] = true
		consts = append(consts, commented(op.constName, op.description, jen.Id(op.constName).Op("=").Lit(op.method)))
	}
	file.Const().Defs(consts...)
	file.Line()

	for _, op := range ops {
		file.Commentf("%s handles %s %ss.", op.handlerInterface(), op.method, op.kind).Line().
			Type().Id(op.handlerInterface()).Interface(commented(op.name, op.description, handlerMethod(op)))
		file.Line()
	}
	return file
}

func handlerMethod(op operation) jen.Code {
	params := []jen.Code{jen.Qual("context", "Context"), jen.Op("*").Id(op.params)}
	if op.kind == notification {
		return jen.Id(op.name).Params(params...).Error()
	}
	return jen.Id(op.name).Params(params...).Params(jen.Op("*").Id(op.result), jen.Error())
}

func endpointFile(e endpoint, ops []operation) *jen.File {
	file := newFile()
	for _, op := range ops {
		if op.sentBy(e) {
			file.Add(commented(op.name, op.description, connectionMethod(e, op)))
			file.Line()
		}
	}

	file.Add(dispatchFunc(e, ops))
	return file
}

func connectionMethod(e endpoint, op operation) jen.Code {
	fn := jen.Func().Params(jen.Id("c").Op("*").Id(e.conn)).Id(op.name).Params(
		jen.Id("ctx").Qual("context", "Context"),
		jen.Id("params").Op("*").Id(op.params),
	)
	conn := jen.Id("c").Dot("rpc").Dot("conn")
	if op.kind == notification {
		return fn.Error().Block(
			jen.Return(jen.Id("notify").Call(jen.Id("ctx"), conn, jen.Id(op.constName), jen.Id("params"))),
		)
	}
	return fn.Params(jen.Op("*").Id(op.result), jen.Error()).Block(
		jen.Return(jen.Id("call").Index(jen.Id(op.result)).Call(jen.Id("ctx"), conn, jen.Id(op.constName), jen.Id("params"))),
	)
}

func dispatchFunc(e endpoint, ops []operation) jen.Code {
	byMethod := map[string][]operation{}
	for _, op := range ops {
		if op.handledBy(e) {
			byMethod[op.constName] = append(byMethod[op.constName], op)
		}
	}
	var cases []jen.Code
	for _, constName := range sortedKeys(byMethod) {
		handled := byMethod[constName]
		var body []jen.Code
		if len(handled) == 2 {
			// The same method name carries a request and a notification; only
			// requests have an ID.
			body = append(body, jen.If(jen.Id("req").Dot("IsCall").Call()).Block(dispatchBody(handled[0])...))
			body = append(body, dispatchBody(handled[1])...)
		} else {
			body = dispatchBody(handled[0])
		}
		cases = append(cases, jen.Case(jen.Id(constName)).Block(body...))
	}
	cases = append(cases, jen.Default().Block(
		jen.Return(jen.Id("handleExtension").Call(jen.Id("ctx"), jen.Id("handler"), jen.Id("req"))),
	))
	return jen.Func().Id(e.dispatch).Params(
		jen.Id("ctx").Qual("context", "Context"),
		jen.Id("handler").Any(),
		jen.Id("req").Op("*").Qual(jsonrpcPath, "Request"),
	).Params(jen.Any(), jen.Error()).Block(
		jen.Qual(jsonrpc2Path, "Async").Call(jen.Id("ctx")),
		jen.Switch(jen.Id("req").Dot("Method")).Block(cases...),
	)
}

func dispatchBody(op operation) []jen.Code {
	body := []jen.Code{
		jen.List(jen.Id("h"), jen.Id("ok")).Op(":=").Id("handler").Assert(jen.Id(op.handlerInterface())),
		jen.If(jen.Op("!").Id("ok")).Block(jen.Return(jen.Nil(), jen.Id("methodNotFound").Call(jen.Id("req").Dot("Method")))),
		jen.List(jen.Id("params"), jen.Err()).Op(":=").Id("decodeParams").Index(jen.Id(op.params)).Call(jen.Id("req")),
		jen.If(jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Nil(), jen.Err())),
	}
	invoke := jen.Id("h").Dot(op.name).Call(jen.Id("ctx"), jen.Id("params"))
	if op.kind == notification {
		return append(body, jen.Return(jen.Nil(), invoke))
	}
	return append(body, jen.Return(jen.Id("rpcResult").Call(invoke)))
}

func commented(name, description string, code jen.Code) jen.Code {
	if description == "" {
		return code
	}
	return typegen.CommentStatement(name, description).Add(code)
}
