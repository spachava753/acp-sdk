package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunErrorsWhenGeneratorProducesNoFiles(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	err := run(schemaPath, filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "produced no files") {
		t.Fatalf("error = %q, want produced no files", err)
	}
}
