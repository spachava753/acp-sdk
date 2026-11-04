// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package rustv1_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const upstreamRevision = "050cd94c081056ace37a7b89abfed7f6220b47eb"

type scenarioMapping struct {
	ID        string            `json:"id"`
	Source    string            `json:"source"`
	Function  string            `json:"function"`
	Line      int               `json:"line"`
	Status    string            `json:"status"`
	Reason    string            `json:"reason"`
	GoTests   []string          `json:"go_tests"`
	GoPackage string            `json:"go_package,omitempty"`
	Scenarios []string          `json:"scenarios"`
	Subcases  []scenarioSubcase `json:"subcases,omitempty"`
}

type scenarioSubcase struct {
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Reason      string   `json:"reason"`
	GoTests     []string `json:"go_tests"`
}

type scenarioInventory struct {
	UpstreamRevision string            `json:"upstream_revision"`
	Entries          []scenarioMapping `json:"entries"`
	Doctests         []scenarioMapping `json:"doctests"`
	Supplemental     []scenarioMapping `json:"supplemental"`
}

// TestRustScenarioAccounting checks the port's bookkeeping, not protocol behavior.
// The independent source inventory accounts for test functions and doc fences;
// a domain must resolve each applicable entry to real tests or an explicit
// nonportable scenario. Non-Rust and ignored fences remain visible in the census. Source/assertion review is still needed: a test name in
// a manifest cannot prove that its assertions preserve the upstream scenario.
func TestRustScenarioAccounting(t *testing.T) {
	var inventory scenarioInventory
	readManifest(t, "upstream_inventory.json", &inventory)
	if inventory.UpstreamRevision != upstreamRevision {
		t.Fatalf("inventory revision = %q, want %q", inventory.UpstreamRevision, upstreamRevision)
	}
	if len(inventory.Entries) == 0 {
		t.Fatal("upstream inventory contains no test functions")
	}

	items := make([]scenarioMapping, 0, len(inventory.Entries)+len(inventory.Doctests)+len(inventory.Supplemental))
	items = append(items, inventory.Entries...)
	items = append(items, inventory.Doctests...)
	items = append(items, inventory.Supplemental...)
	upstream := make(map[string]scenarioMapping, len(items))
	for _, entry := range items {
		checkSourceIdentity(t, "upstream inventory", entry)
		if _, exists := upstream[entry.ID]; exists {
			t.Errorf("duplicate upstream entry %q", entry.ID)
		}
		upstream[entry.ID] = entry
	}

	paths, err := filepath.Glob("*/mapping.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no domain mappings found")
	}
	packageTests := make(map[string]map[string]bool, len(paths))
	for _, path := range paths {
		dir := filepath.Dir(path)
		packageTests[filepath.ToSlash(dir)] = declaredTests(t, dir)
	}
	resolved := make(map[string]string)
	ported, partial, excluded := 0, 0, 0
	for _, path := range paths {
		var domain scenarioInventory
		readManifest(t, path, &domain)
		if domain.UpstreamRevision != upstreamRevision {
			t.Errorf("%s: revision = %q, want %q", path, domain.UpstreamRevision, upstreamRevision)
		}
		for _, entry := range domain.Entries {
			checkSourceIdentity(t, path, entry)
			packageName := filepath.ToSlash(filepath.Dir(path))
			if entry.GoPackage != "" {
				packageName = strings.TrimPrefix(entry.GoPackage, "github.com/spachava753/acp-sdk/")
				packageName = strings.TrimPrefix(packageName, "conformance/rustv1/")
			}
			tests, knownPackage := packageTests[packageName]
			if !knownPackage {
				t.Errorf("%s: %q references unknown port package %q", path, entry.ID, entry.GoPackage)
			}
			original, exists := upstream[entry.ID]
			if !exists {
				t.Errorf("%s: %q is not in the upstream source inventory", path, entry.ID)
				continue
			}
			if original.Source != entry.Source || original.Function != entry.Function || original.Line != entry.Line {
				t.Errorf("%s: source identity does not match inventory for %q", path, entry.ID)
			}
			if other, exists := resolved[entry.ID]; exists {
				t.Errorf("%q mapped twice, in %s and %s", entry.ID, other, path)
			}
			resolved[entry.ID] = path
			switch entry.Status {
			case "ported", "partially-ported":
				if entry.Status == "ported" {
					ported++
				} else {
					partial++
					checkPartialMapping(t, path, entry, tests)
				}
				if len(entry.GoTests) == 0 || len(entry.Scenarios) == 0 {
					t.Errorf("%s: mapped %q needs Go test names and scenario coverage", path, entry.ID)
				}
				checkTestNames(t, path, entry.ID, entry.GoTests, tests)
			case "not-applicable":
				excluded++
				checkExclusion(t, path, entry)
			default:
				t.Errorf("%s: unresolved %q (status %q)", path, entry.ID, entry.Status)
			}
		}
	}
	for id, entry := range upstream {
		if _, exists := resolved[id]; exists {
			continue
		}
		if entry.Status != "not-applicable" {
			t.Errorf("upstream scenario %q has no domain mapping or explicit exclusion (status %q)", id, entry.Status)
			continue
		}
		excluded++
		checkExclusion(t, "upstream inventory", entry)
	}
	if ported+partial == 0 {
		t.Error("no upstream scenarios are mapped to executable Go tests")
	}
	t.Logf("source census: %d test functions, %d doc fences, %d supplemental compile probes; fully mapped source items: %d; partially mapped source items: %d; explicitly excluded source items: %d", len(inventory.Entries), len(inventory.Doctests), len(inventory.Supplemental), ported, partial, excluded)
}

func checkTestNames(t *testing.T, path, id string, names []string, tests map[string]bool) {
	t.Helper()
	for _, name := range names {
		root, _, _ := strings.Cut(name, "/")
		if !tests[root] {
			t.Errorf("%s: %q maps to undeclared test %q", path, id, name)
		}
	}
}

func checkPartialMapping(t *testing.T, path string, entry scenarioMapping, tests map[string]bool) {
	t.Helper()
	ported, excluded := 0, 0
	for _, part := range entry.Subcases {
		if strings.TrimSpace(part.Description) == "" {
			t.Errorf("%s: %q has an unnamed partial-coverage property", path, entry.ID)
		}
		switch part.Status {
		case "ported":
			ported++
			if len(part.GoTests) == 0 {
				t.Errorf("%s: %q property %q claims coverage without a test", path, entry.ID, part.Description)
			}
			checkTestNames(t, path, entry.ID, part.GoTests, tests)
		case "not-applicable":
			excluded++
			if strings.TrimSpace(part.Reason) == "" || len(part.GoTests) != 0 {
				t.Errorf("%s: %q property %q needs a nonportable rationale and no test claim", path, entry.ID, part.Description)
			}
		default:
			t.Errorf("%s: %q property %q is unresolved (%q)", path, entry.ID, part.Description, part.Status)
		}
	}
	if ported == 0 || excluded == 0 {
		t.Errorf("%s: partially ported %q must identify both covered and excluded properties", path, entry.ID)
	}
}

func readManifest(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func checkSourceIdentity(t *testing.T, path string, entry scenarioMapping) {
	t.Helper()
	want := fmt.Sprintf("%s:%d:%s", entry.Source, entry.Line, entry.Function)
	if entry.Source == "" || entry.Function == "" || entry.Line < 1 || entry.ID != want {
		t.Errorf("%s: invalid source identity for %q (want %q)", path, entry.ID, want)
	}
}

func checkExclusion(t *testing.T, path string, entry scenarioMapping) {
	t.Helper()
	if strings.TrimSpace(entry.Reason) == "" {
		t.Errorf("%s: excluded %q has no rationale", path, entry.ID)
	}
	if len(entry.GoTests) != 0 {
		t.Errorf("%s: excluded %q also claims executable Go coverage", path, entry.ID)
	}
}

func declaredTests(t *testing.T, dir string) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	tests := make(map[string]bool)
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "Test") {
				tests[function.Name.Name] = true
			}
		}
	}
	return tests
}
