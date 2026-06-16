// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package agentgen

import "github.com/dave/jennifer/jen"

func emitMethodConsts(file *jen.File, ops []operation) {
	var consts []jen.Code
	for _, op := range ops {
		consts = append(consts, commented(op.constName, op.description, jen.Id(op.constName).Op("=").Lit(op.method)))
	}
	file.Const().Defs(consts...)
	file.Line()
}
