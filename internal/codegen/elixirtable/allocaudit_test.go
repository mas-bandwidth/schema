package elixirtable

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAllocAuditBudgetFileIsWellFormed(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")

	manifestBytes, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatalf("read MANIFEST.txt: %v", err)
	}
	instances := make(map[string]bool)
	for _, raw := range strings.Split(string(manifestBytes), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "instance" {
			instances[fields[1]] = true
		}
	}
	if len(instances) == 0 {
		t.Fatal("MANIFEST.txt contains no instance rows")
	}

	budgetPath := filepath.Join(root, "test", "conformance", "elixir", "alloc-budget.txt")
	budgetBytes, err := os.ReadFile(budgetPath)
	if err != nil {
		t.Errorf("read alloc-budget.txt: %v", err)
	} else {
		seen := make(map[string]int)
		for lineNo, raw := range strings.Split(string(budgetBytes), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) != 4 {
				t.Errorf("alloc-budget.txt:%d: want 4 fields (<case> <heap_words> <binary_calls> <reductions>), got %d (%q)", lineNo+1, len(fields), line)
				continue
			}
			name := fields[0]
			if prev, dup := seen[name]; dup {
				t.Errorf("alloc-budget.txt:%d: duplicate case %q (first seen on line %d)", lineNo+1, name, prev)
			}
			seen[name] = lineNo + 1
			if !instances[name] {
				t.Errorf("alloc-budget.txt:%d: case %q is not an instance row in testdata/conformance/tables/MANIFEST.txt", lineNo+1, name)
			}
			words, err := strconv.Atoi(fields[1])
			if err != nil || words <= 0 {
				t.Errorf("alloc-budget.txt:%d: case %q heap_words %q is not a positive integer", lineNo+1, name, fields[1])
			}
			bins, err := strconv.Atoi(fields[2])
			if err != nil || bins < 0 {
				t.Errorf("alloc-budget.txt:%d: case %q binary_calls %q is not a non-negative integer", lineNo+1, name, fields[2])
			}
			reds, err := strconv.Atoi(fields[3])
			if err != nil || reds <= 0 {
				t.Errorf("alloc-budget.txt:%d: case %q reductions %q is not a positive integer", lineNo+1, name, fields[3])
			}
		}
		if len(seen) < 20 {
			t.Errorf("alloc-budget.txt: expected at least 20 pinned cases, got %d", len(seen))
		}
	}

	mkBytes, err := os.ReadFile(filepath.Join(root, "make", "elixir.mk"))
	if err != nil {
		t.Fatalf("read make/elixir.mk: %v", err)
	}
	mk := string(mkBytes)
	for _, target := range []string{
		"tables-elixir-alloc-audit:",
		"tables-elixir-alloc-pin:",
		"tables-elixir-alloc-negative-control:",
		"tables-elixir-alloc-runtime-negative-control:",
	} {
		if !strings.Contains(mk, target) {
			t.Errorf("make/elixir.mk missing target recipe %s", target)
		}
	}

	auditRecipe := recipeBlock(mk, "tables-elixir-alloc-audit:")
	if auditRecipe == "" {
		t.Fatalf("make/elixir.mk has no recipe for tables-elixir-alloc-audit")
	}
	pinIdx := strings.Index(auditRecipe, "test/elixir/runtime_pin.exs")
	mainIdx := strings.Index(auditRecipe, "test/elixir-alloc/main.exs")
	if mainIdx < 0 {
		t.Errorf("tables-elixir-alloc-audit recipe does not invoke test/elixir-alloc/main.exs:\n%s", auditRecipe)
	}
	if pinIdx < 0 {
		// Either the makefile recipe runs runtime_pin.exs before main.exs or main.exs runs it first.
		mainBytes, err := os.ReadFile(filepath.Join(root, "test", "elixir-alloc", "main.exs"))
		if err != nil {
			t.Fatalf("read test/elixir-alloc/main.exs: %v", err)
		}
		mainSrc := string(mainBytes)
		pinInMain := strings.Index(mainSrc, "test/elixir/runtime_pin.exs")
		measureInMain := strings.Index(mainSrc, ":garbage_collection")
		if pinInMain < 0 || (measureInMain >= 0 && pinInMain > measureInMain) {
			t.Errorf("neither tables-elixir-alloc-audit nor test/elixir-alloc/main.exs runs test/elixir/runtime_pin.exs before measuring")
		}
	} else if mainIdx >= 0 && pinIdx > mainIdx {
		t.Errorf("tables-elixir-alloc-audit recipe runs test/elixir-alloc/main.exs before test/elixir/runtime_pin.exs:\n%s", auditRecipe)
	}
}

func recipeBlock(mk, header string) string {
	idx := strings.Index(mk, "\n"+header)
	if idx < 0 {
		if strings.HasPrefix(mk, header) {
			idx = 0
		} else {
			return ""
		}
	} else {
		idx++
	}
	lines := strings.Split(mk[idx:], "\n")
	var out []string
	for i, line := range lines {
		if i == 0 {
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(line, "\t") {
			out = append(out, line)
			continue
		}
		break
	}
	return strings.Join(out, "\n")
}
