package elixirtable

// TestAllocAuditBudgetFileIsWellFormed IS THE UNIT HALF of the Elixir leg's
// allocation audit (docs/PORTING.md I1 and I14, issue #420). The audit itself
// runs Elixir, so it is a FUNCTIONAL gate (make tables-elixir-alloc-audit);
// this test runs no Elixir and no clock and reads two files the audit's verdict
// rests on:
//
//   - test/conformance/elixir/alloc-budget.txt, the pinned budget: every line
//     is `<case> <heap words> <binary calls> <reductions>`, four fields, the
//     three figures non-negative integers, no case twice, and every case an
//     `instance` row of testdata/conformance/tables/MANIFEST.txt, so a budget
//     cannot pin a case the corpus no longer has.
//   - make/elixir.mk, the four targets: the audit and the pin run
//     test/elixir-alloc/main.exs WITH the two pins this file compares against,
//     and that script's first act is test/elixir/runtime_pin.exs, so no figure
//     is measured, pinned or certified off the pinned OTP; both controls exist
//     and a `test-elixir:` line names the audit and both controls.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAllocAuditBudgetFileIsWellFormed(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s is unreadable: %v", rel, err)
		}
		return string(data)
	}

	instances := map[string]bool{}
	for line := range strings.SplitSeq(read("testdata/conformance/tables/MANIFEST.txt"), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "instance" {
			instances[f[1]] = true
		}
	}
	if len(instances) == 0 {
		t.Fatal("testdata/conformance/tables/MANIFEST.txt names no instance: the parse, not the budget, broke")
	}

	const budget = "test/conformance/elixir/alloc-budget.txt"
	seen := map[string]int{}
	for n, line := range strings.Split(read(budget), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Errorf("%s:%d: %d fields, want 4 (<case> <heap words> <binary calls> <reductions>): %q", budget, n+1, len(f), line)
			continue
		}
		for i, col := range []string{"heap words", "binary calls", "reductions"} {
			if v, err := strconv.ParseUint(f[i+1], 10, 64); err != nil {
				t.Errorf("%s:%d: the %s column %q is not a non-negative integer", budget, n+1, col, f[i+1])
			} else if col == "heap words" && v == 0 {
				t.Errorf("%s:%d: %s pins zero heap words; a decoded BEAM term is an allocation, so zero is a broken measurement", budget, n+1, f[0])
			}
		}
		if prev, dup := seen[f[0]]; dup {
			t.Errorf("%s:%d: case %s is pinned twice (first at line %d)", budget, n+1, f[0], prev)
		}
		seen[f[0]] = n + 1
		if !instances[f[0]] {
			t.Errorf("%s:%d: case %s is not an `instance` of testdata/conformance/tables/MANIFEST.txt", budget, n+1, f[0])
		}
	}
	if len(seen) == 0 {
		t.Errorf("%s pins no case: the audit would certify nothing", budget)
	}

	mk := strings.ReplaceAll(read("make/elixir.mk"), "\\\n", " ")
	for _, target := range []string{"tables-elixir-alloc-audit", "tables-elixir-alloc-pin"} {
		recipe := makeTargetRecipe(t, mk, target)
		if !strings.Contains(recipe, "test/elixir-alloc/main.exs") {
			t.Errorf("the %s recipe does not run test/elixir-alloc/main.exs:\n%s", target, recipe)
		}
		if !strings.Contains(recipe, "$(ELIXIR_PIN_OTP) $(ELIXIR_PIN_ELIXIR)") {
			t.Errorf("the %s recipe does not hand the audit the pins $(ELIXIR_PIN_OTP) $(ELIXIR_PIN_ELIXIR), so the pin it runs first is not this leg's:\n%s", target, recipe)
		}
	}
	nc := makeTargetRecipe(t, mk, "tables-elixir-alloc-negative-control")
	if !strings.Contains(nc, "ELIXIR_SABOTAGED_BUILD") || !strings.Contains(nc, "internal/codegen/elixirtable/variableform.go") {
		t.Errorf("tables-elixir-alloc-negative-control does not sabotage the emitter's variableform.go through ELIXIR_SABOTAGED_BUILD:\n%s", nc)
	}
	for _, col := range []string{"heap words", "binary calls"} {
		if !strings.Contains(nc, col) {
			t.Errorf("tables-elixir-alloc-negative-control does not require the %q column to go red on its own:\n%s", col, nc)
		}
	}
	rc := makeTargetRecipe(t, mk, "tables-elixir-alloc-runtime-negative-control")
	if !strings.Contains(rc, "test/elixir-alloc/main.exs") || !strings.Contains(rc, "0.0.0") {
		t.Errorf("tables-elixir-alloc-runtime-negative-control does not run the audit on a pin nothing is running:\n%s", rc)
	}
	for _, target := range []string{"tables-elixir-alloc-audit", "tables-elixir-alloc-negative-control", "tables-elixir-alloc-runtime-negative-control"} {
		if !testElixirNames(t, mk, target) {
			t.Errorf("no `test-elixir:` line names %s", target)
		}
	}

	// THE PIN COMES FIRST: the first statement of the audit script (comments
	// and blank lines aside) evaluates test/elixir/runtime_pin.exs.
	script := read("test/elixir-alloc/main.exs")
	first := ""
	for line := range strings.SplitSeq(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		first = line
		break
	}
	if !strings.Contains(first, "runtime_pin") {
		t.Errorf("the first statement of test/elixir-alloc/main.exs is %q, not the runtime pin: the audit could measure before it refuses", first)
	}
}
