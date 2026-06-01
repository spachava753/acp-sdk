package schemagen_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

func readExpectedFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(dir, "*.testdata"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no *.testdata fixtures found in %s", dir)
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
