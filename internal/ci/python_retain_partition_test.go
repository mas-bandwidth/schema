package ci

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// pythonRetainPlanCheck proves the per-commit retention rows partition the
// suite: every top-level ^TestPythonRetain function is selected by exactly one
// python-retain-N row, every row selects something, the rows are exactly the
// shards registered for CI, and the local aggregates still reach every row.
func pythonRetainPlanCheck(tests []string, makefile []byte) error {
	text := string(makefile)
	list := regexp.MustCompile(`(?m)^PYTHON_RETAIN_SHARDS := \$\(addprefix python-retain-,([0-9 ]+)\)$`).FindStringSubmatch(text)
	if list == nil {
		return fmt.Errorf("PYTHON_RETAIN_SHARDS is not a literal shard list")
	}
	var shards []string
	for n := range strings.FieldsSeq(list[1]) {
		shards = append(shards, "python-retain-"+n)
	}
	text = strings.ReplaceAll(text, "$(PYTHON_RETAIN_SHARDS)", strings.Join(shards, " "))
	runPattern := regexp.MustCompile(`-run '([^']+)'`)
	rules := map[string][]string{}
	runs := map[string]string{}
	var registered []string
	current := ""
	for line := range strings.SplitSeq(text, "\n") {
		if values, ok := strings.CutPrefix(line, "PYTHON_CI_SUPPORT_GROUPS +="); ok {
			registered = append(registered, strings.Fields(values)...)
			current = ""
			continue
		}
		if strings.HasPrefix(line, "\t") {
			if r := runPattern.FindStringSubmatch(line); r != nil && current != "" {
				runs[current] = strings.ReplaceAll(r[1], "$$", "$")
			}
			continue
		}
		name, deps, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, "#") || strings.HasPrefix(deps, "=") {
			current = ""
			continue
		}
		current = strings.TrimSpace(name)
		if current == ".PHONY" {
			current = ""
			continue
		}
		rules[current] = append(rules[current], strings.Fields(deps)...)
	}
	for name := range runs {
		if regexp.MustCompile(`^python-retain-[0-9]+$`).MatchString(name) && !slices.Contains(shards, name) {
			return fmt.Errorf("retention row %s is outside PYTHON_RETAIN_SHARDS", name)
		}
	}
	if slices.Contains(registered, "python-retain") {
		return fmt.Errorf("monolithic python-retain is registered as a CI row")
	}
	for _, want := range append(slices.Clone(shards), "python-retain-negative-control") {
		if n := len(slices.DeleteFunc(slices.Clone(registered), func(s string) bool { return s != want })); n != 1 {
			return fmt.Errorf("retention row %s registered %d times for CI, want once", want, n)
		}
	}
	aggregate := slices.Clone(rules["python-retain"])
	slices.Sort(aggregate)
	sorted := slices.Clone(shards)
	slices.Sort(sorted)
	if !slices.Equal(aggregate, sorted) {
		return fmt.Errorf("python-retain aggregate reaches %v, want every shard %v", aggregate, sorted)
	}
	for _, shard := range append(slices.Clone(shards), "python-retain-negative-control") {
		if !slices.Contains(rules["test-python"], shard) {
			return fmt.Errorf("test-python does not reach retention row %s", shard)
		}
	}
	expressions := make([]*regexp.Regexp, len(shards))
	for i, shard := range shards {
		pattern, ok := runs[shard]
		if !ok {
			return fmt.Errorf("retention row %s has no recipe", shard)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("retention row %s: %v", shard, err)
		}
		expressions[i] = re
	}
	used := make([]int, len(shards))
	for _, name := range tests {
		matches := 0
		for i, re := range expressions {
			if re.MatchString(name) {
				matches++
				used[i]++
			}
		}
		if matches != 1 {
			return fmt.Errorf("retention test %s selected %d times, want once", name, matches)
		}
	}
	for i, n := range used {
		if n == 0 {
			return fmt.Errorf("retention row %s selects no test", shards[i])
		}
	}
	control, err := regexp.Compile(runs["python-retain-negative-control"])
	if err != nil || runs["python-retain-negative-control"] == "" {
		return fmt.Errorf("retention control row has no valid recipe: %v", err)
	}
	for _, name := range tests {
		if strings.HasSuffix(name, "NegativeControl") && !control.MatchString(name) {
			return fmt.Errorf("retention control %s is absent from the control row", name)
		}
	}
	return nil
}

// pythonRetainTests reads the top-level retention tests from the package
// source, independently of the Make partition.
func pythonRetainTests(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "internal/codegen/pythontable")
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(source, []byte("func TestPythonRetain")) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "TestPythonRetain") {
				names = append(names, fn.Name.Name)
			}
		}
	}
	if !slices.Contains(names, "TestPythonRetainCRC") || !slices.Contains(names, "TestPythonRetainZeroIDNegativeControl") {
		t.Fatalf("retention test discovery is incomplete: %v", names)
	}
	return names
}

func TestPythonRetainCIPlan(t *testing.T) {
	t.Parallel()
	tests := pythonRetainTests(t)
	makefile, err := os.ReadFile(filepath.Join(repoRoot(t), "make/python-retain.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := pythonRetainPlanCheck(tests, makefile); err != nil {
		t.Fatal(err)
	}
	mk := string(makefile)
	for _, tc := range []struct {
		name     string
		tests    []string
		makefile string
		reason   string
	}{
		{"missing-test", tests, strings.Replace(mk, "|Names|", "|", 1), "selected 0 times"},
		{"duplicate-test", tests, strings.Replace(mk, "(ResolvedPayload|", "(CRC|ResolvedPayload|", 1), "selected 2 times"},
		{"new-test", append(slices.Clone(tests), "TestPythonRetainFuture"), mk, "selected 0 times"},
		{"unregistered-row", tests, strings.Replace(mk, "PYTHON_CI_SUPPORT_GROUPS += $(PYTHON_RETAIN_SHARDS) ", "PYTHON_CI_SUPPORT_GROUPS += python-retain-0 python-retain-1 python-retain-2 ", 1), "registered 0 times"},
		{"monolith", tests, mk + "PYTHON_CI_SUPPORT_GROUPS += python-retain\n", "monolithic"},
		{"aggregate", tests, strings.Replace(mk, "python-retain: $(PYTHON_RETAIN_SHARDS)", "python-retain: python-retain-0", 1), "aggregate"},
		{"local-entry", tests, strings.Replace(mk, "test-python: $(PYTHON_RETAIN_SHARDS) ", "test-python: ", 1), "test-python does not reach"},
		{"row-without-recipe", tests, strings.Replace(mk, "python-retain-,0 1 2 3)", "python-retain-,0 1 2 3 4)", 1), "has no recipe"},
		{"stray-row", tests, strings.Replace(mk, "python-retain-,0 1 2 3)", "python-retain-,0 1 2)", 1), "outside PYTHON_RETAIN_SHARDS"},
		{"control-row", tests, strings.Replace(mk, "|ZeroIDNegativeControl)$$' ./internal/codegen/pythontable/\ntest-python", ")$$' ./internal/codegen/pythontable/\ntest-python", 1), "absent from the control row"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.makefile == mk && slices.Equal(tc.tests, tests) {
				t.Fatal("control did not change its input")
			}
			if err := pythonRetainPlanCheck(tc.tests, []byte(tc.makefile)); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("retention plan control: %v, want %s", err, tc.reason)
			}
		})
	}
}
