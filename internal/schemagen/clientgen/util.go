// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package clientgen

import (
	"sort"
	"strings"
	"unicode"

	"github.com/dave/jennifer/jen"
)

func handledByClient(op operation) bool {
	return op.side == "client" || op.side == "both"
}

func paramType(op operation) string {
	if op.notifyType != "" {
		return op.notifyType
	}
	return op.requestType
}

func methodGroup(method string) string {
	group, _, ok := strings.Cut(method, "/")
	if !ok {
		return method
	}
	return group
}

func operationName(typeName string) string {
	name := strings.TrimSuffix(typeName, "Notification")
	name = strings.TrimSuffix(name, "Request")
	name = strings.TrimSuffix(name, "Response")
	return name
}

func commented(name, description string, code jen.Code) jen.Code {
	if description == "" {
		return code
	}
	return commentStatement(name, description).Add(code)
}

func commentStatement(name, description string) *jen.Statement {
	stmt := jen.Empty()
	for i, line := range strings.Split(description, "\n") {
		if line == "" {
			stmt.Comment("").Line()
			continue
		}
		if i == 0 && name != "" {
			stmt.Commentf("%s: %s", name, line).Line()
		} else {
			stmt.Comment(line).Line()
		}
	}
	return stmt
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func refName(ref string) string {
	return strings.TrimPrefix(ref, "#/$defs/")
}

func pascal(value string) string {
	var words []string
	var word strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
			continue
		}
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	for i, word := range words {
		runes := []rune(word)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, "")
}
