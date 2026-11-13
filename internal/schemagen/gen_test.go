package schemagen_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/acp-sdk/internal/schemagen"
)

func TestGenerate(t *testing.T) {
	const testdata = "testdata"

	schemaPaths, err := filepath.Glob(filepath.Join(testdata, "*", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(schemaPaths) == 0 {
		t.Fatal("no schema.json test fixtures found")
	}

	for _, schemaPath := range schemaPaths {
		t.Run(filepath.Base(filepath.Dir(schemaPath)), func(t *testing.T) {
			schemaData, err := os.ReadFile(schemaPath)
			if err != nil {
				t.Fatal(err)
			}

			var schema jsonschema.Schema
			if err := json.Unmarshal(schemaData, &schema); err != nil {
				t.Fatal(err)
			}

			want := readExpectedFiles(t, filepath.Dir(schemaPath))
			got := generatedFilesByName(t, schemagen.Generate(&schema))
			typeCheckGenerated(t, got)

			for filename, wantContents := range want {
				gotContents, ok := got[filename]
				if !ok {
					t.Errorf("Generate() missing file %q", filename)
					continue
				}
				if !bytes.Equal(gotContents, wantContents) {
					t.Errorf("Generate() file %q contents mismatch\n got:\n%s\nwant:\n%s", filename, gotContents, wantContents)
				}
				delete(got, filename)
			}
			for filename := range got {
				t.Errorf("Generate() returned unexpected file %q", filename)
			}
		})
	}
}

func TestGenerateDeterministic(t *testing.T) {
	schemaData, err := os.ReadFile("schema.json")
	if err != nil {
		t.Fatal(err)
	}

	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatal(err)
	}

	want := generatedFilesByName(t, schemagen.Generate(&schema))
	typeCheckGenerated(t, want)
	for i := range 50 {
		got := generatedFilesByName(t, schemagen.Generate(&schema))
		for filename, wantContents := range want {
			gotContents, ok := got[filename]
			if !ok {
				t.Fatalf("Generate() run %d missing file %q", i+1, filename)
			}
			if !bytes.Equal(gotContents, wantContents) {
				t.Fatalf("Generate() run %d file %q changed between runs", i+1, filename)
			}
			delete(got, filename)
		}
		for filename := range got {
			t.Fatalf("Generate() run %d returned unexpected file %q", i+1, filename)
		}
	}
}

// The source importer resolves the module's internal packages. Sharing one
// caches each imported package across fixtures instead of rechecking it.
var (
	files          = token.NewFileSet()
	sourceImporter = importer.ForCompiler(files, "source", nil)
)

// typeCheckGenerated type-checks generated files as part of package acp. RPC
// glue is checked together with the handwritten runtime it calls, so a schema
// that generates conflicting or mismatched declarations fails here.
func typeCheckGenerated(t *testing.T, generated map[string][]byte) {
	t.Helper()
	if len(generated) == 0 {
		return
	}

	acpDir := filepath.Join("..", "..", "acp")
	// Type-only output needs just the JSON helpers its decoders call.
	handwritten := []string{filepath.Join(acpDir, "json.go")}
	if _, ok := generated["methods_gen.go"]; ok {
		paths, err := filepath.Glob(filepath.Join(acpDir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		handwritten = handwritten[:0]
		for _, path := range paths {
			if !strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, "_gen.go") {
				handwritten = append(handwritten, path)
			}
		}
	}

	var parsed []*ast.File
	for _, name := range sortedNames(generated) {
		file, err := parser.ParseFile(files, name, generated[name], parser.AllErrors)
		if err != nil {
			t.Fatalf("parse generated %s: %v", name, err)
		}
		parsed = append(parsed, file)
	}
	for _, path := range handwritten {
		file, err := parser.ParseFile(files, path, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		parsed = append(parsed, file)
	}
	config := types.Config{Importer: sourceImporter}
	if _, err := config.Check("acp", files, parsed, nil); err != nil {
		t.Fatalf("type-check generated code: %v", err)
	}
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func readExpectedFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(dir, "*.testdata"))
	if err != nil {
		t.Fatal(err)
	}

	files := make(map[string][]byte, len(paths))
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		filename := strings.TrimSuffix(filepath.Base(path), ".testdata")
		files[filename] = contents
	}
	return files
}

func generatedFilesByName(t *testing.T, files []schemagen.GeneratedFile) map[string][]byte {
	t.Helper()

	byName := make(map[string][]byte, len(files))
	for _, file := range files {
		if _, ok := byName[file.Filename]; ok {
			t.Fatalf("Generate() returned duplicate file %q", file.Filename)
		}
		byName[file.Filename] = file.Contents
	}
	return byName
}
