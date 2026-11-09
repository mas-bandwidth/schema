package elixirtable

// TestElixirAllocBudgetPinned IS THE UNIT HALF OF THE PIN's COMPLETENESS. The
// audit itself needs the pinned BEAM (make tables-elixir-alloc-audit), but what
// it measures is decided here from two files and no interpreter: the
// `instance` rows of testdata/conformance/tables/MANIFEST.txt and the units
// make/elixir.mk generates. Every instance of a generated unit that the strict
// read loads and saves back is measured, so every such case must have a line
// in test/conformance/elixir/alloc-budget.txt; a case the corpus marks
// `wire-refused` is named by the audit, not measured, and must not be pinned.
//
// The pin had drifted: the strict cut added instances (the feature witnesses,
// the text boundary rows, the wide, scalar and widelist rows) that the audit
// measured unpinned, so its budget no longer described the whole corpus. This
// test goes red until `make tables-elixir-alloc-pin` covers them. Its negative
// control plants each fault in a copy of the REAL budget file, not a synthetic
// fixture, so the gate is shown to bite on the artifact the audit reads.

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type allocPinInstance struct {
	name  string
	unit  string
	root  string
	wire  string
	flags []string
}

func hasAllocPinFlag(in allocPinInstance, flag string) bool {
	for _, f := range in.flags {
		if f == flag {
			return true
		}
	}
	return false
}

// allocPinRoot reads the corpus and makefile the audit itself reads.
func allocPinRoot(t *testing.T) (map[string]allocPinInstance, map[string]bool, []byte) {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	read := func(rel string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s is unreadable: %v", rel, err)
		}
		return data
	}

	instances := map[string]allocPinInstance{}
	for _, line := range strings.Split(string(read("testdata/conformance/tables/MANIFEST.txt")), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != "instance" {
			continue
		}
		instances[f[1]] = allocPinInstance{
			name: f[1], unit: f[2], root: f[3], wire: f[4],
			flags: f[5:],
		}
	}
	if len(instances) == 0 {
		t.Fatal("testdata/conformance/tables/MANIFEST.txt names no instance: the parse, not the budget, broke")
	}

	units := map[string]bool{}
	mk := string(read("make/elixir.mk"))
	const decl = "ELIXIR_TABLE_UNITS :="
	start := strings.Index(mk, decl)
	if start < 0 {
		t.Fatalf("make/elixir.mk has no %q: the audit's unit list, not the budget, broke", decl)
	}
	rest := mk[start+len(decl):]
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		rest = rest[:end]
	}
	rest = strings.ReplaceAll(rest, "\\\n", " ")
	for _, field := range strings.Fields(rest) {
		if i := strings.IndexByte(field, ':'); i > 0 {
			units[field[:i]] = true
		}
	}
	if len(units) == 0 {
		t.Fatal("make/elixir.mk names no generated Elixir table unit: the parse, not the budget, broke")
	}

	budget := read("test/conformance/elixir/alloc-budget.txt")
	return instances, units, budget
}

// allocBudgetProblems is the rule: every measured case pinned once with three
// usable figures, and no case pinned that the audit will not measure.
func allocBudgetProblems(budget []byte, instances map[string]allocPinInstance, units map[string]bool) []string {
	var problems []string
	seen := map[string]int{}
	pinned := map[string]bool{}
	for n, line := range strings.Split(string(budget), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		where := "alloc-budget.txt:" + strconv.Itoa(n+1)
		f := strings.Fields(line)
		if len(f) != 4 {
			problems = append(problems, where+": "+strconv.Itoa(len(f))+" fields, want 4 (<case> <heap words> <binary calls> <reductions>): "+line)
			continue
		}
		name := f[0]
		if prev, dup := seen[name]; dup {
			problems = append(problems, where+": case "+name+" is pinned twice (first at line "+strconv.Itoa(prev)+")")
		}
		seen[name] = n + 1
		pinned[name] = true
		in, ok := instances[name]
		if !ok {
			problems = append(problems, where+": case "+name+" is not an `instance` of testdata/conformance/tables/MANIFEST.txt")
			continue
		}
		if !units[in.unit] {
			problems = append(problems, where+": case "+name+" belongs to unit "+in.unit+", which make/elixir.mk does not generate, so the audit cannot measure it")
			continue
		}
		if hasAllocPinFlag(in, "wire-refused") {
			problems = append(problems, where+": case "+name+" is `wire-refused` by the corpus, which the audit names and does not measure, so it must not be pinned")
			continue
		}
		for i, col := range []string{"heap words", "binary calls", "reductions"} {
			v, err := strconv.ParseUint(f[i+1], 10, 64)
			if err != nil {
				problems = append(problems, where+": case "+name+" "+col+" "+f[i+1]+" is not a non-negative integer")
				continue
			}
			if col != "binary calls" && v == 0 {
				problems = append(problems, where+": case "+name+" "+col+" pins zero; a decoded BEAM term is an allocation, so zero is a broken measurement")
			}
		}
	}

	names := make([]string, 0, len(instances))
	for name := range instances {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		in := instances[name]
		if !units[in.unit] || hasAllocPinFlag(in, "wire-refused") {
			continue
		}
		if !pinned[name] {
			problems = append(problems, "test/conformance/elixir/alloc-budget.txt: case "+name+" (unit "+in.unit+", root "+in.root+") is measured by the audit but has no pinned budget")
		}
	}
	return problems
}

func TestElixirAllocBudgetPinned(t *testing.T) {
	t.Parallel()
	instances, units, budget := allocPinRoot(t)
	if problems := allocBudgetProblems(budget, instances, units); len(problems) != 0 {
		t.Errorf("the allocation budget is stale after the strict cut: %d problem(s); re-pin deliberately with `make tables-elixir-alloc-pin`:\n%s",
			len(problems), strings.Join(problems, "\n"))
	}
}

// TestElixirAllocBudgetPinnedNegativeControl plants each fault in a copy of the
// REAL budget file and requires the rule to name it, so a gate that stopped
// biting cannot pass.
func TestElixirAllocBudgetPinnedNegativeControl(t *testing.T) {
	t.Parallel()
	instances, units, budget := allocPinRoot(t)
	if problems := allocBudgetProblems(budget, instances, units); len(problems) != 0 {
		t.Fatalf("the real budget is already red, so the control proves nothing: %s", strings.Join(problems, "\n"))
	}

	lines := strings.Split(string(budget), "\n")
	drop := -1
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := strings.Fields(line)[0]
		in, ok := instances[name]
		if ok && units[in.unit] && !hasAllocPinFlag(in, "wire-refused") {
			drop = i
			break
		}
	}
	if drop < 0 {
		t.Fatal("no measurable case found in the real budget: the control cannot plant its fault")
	}
	missingName := strings.Fields(lines[drop])[0]
	without := strings.Join(append(append([]string(nil), lines[:drop]...), lines[drop+1:]...), "\n")

	cases := []struct {
		name   string
		budget string
		want   string
	}{
		{"a measured case left unpinned", without, missingName + " (unit "},
		{"a wire-refused case pinned", string(budget) + "\nblob_shared 1 0 1\n", "blob_shared is `wire-refused`"},
		{"a case the corpus lost", string(budget) + "\nnot_a_case 1 0 1\n", "not_a_case is not an `instance`"},
		{"a case pinned twice", string(budget) + "\n" + strings.Fields(lines[drop])[0] + " 1 0 1\n", "is pinned twice"},
		{"zero heap words", replaceAllocPinFirstValue(budget, 0), "pins zero"},
	}
	for _, c := range cases {
		problems := allocBudgetProblems([]byte(c.budget), instances, units)
		found := false
		for _, problem := range problems {
			if strings.Contains(problem, c.want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("negative control %q: the gate did not name %q; problems: %v", c.name, c.want, problems)
		}
	}
}

// replaceAllocPinFirstValue rewrites the heap-words figure of the first data
// row, leaving every other byte of the real artifact as it was.
func replaceAllocPinFirstValue(budget []byte, value uint64) string {
	lines := strings.Split(string(budget), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 4 {
			f[1] = strconv.FormatUint(value, 10)
			lines[i] = strings.Join(f, " ")
		}
		break
	}
	return strings.Join(lines, "\n")
}
