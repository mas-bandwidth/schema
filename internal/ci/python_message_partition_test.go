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

// pythonMessagePlanCheck proves the per-commit message rows partition the
// suite: every top-level ^TestPythonMessage function is run by exactly one
// registered CI row of make/python-message.mk (a python-message-N shard or a
// dedicated row such as a control), every such row runs at least one test,
// the shards are exactly PYTHON_MESSAGE_SHARDS, each registered once, the
// monolith is not registered, and the local aggregates reach every shard.
func pythonMessagePlanCheck(tests []string, makefile []byte) error {
	text := strings.ReplaceAll(string(makefile), "\\\n", " ")
	list := regexp.MustCompile(`(?m)^PYTHON_MESSAGE_SHARDS := \$\(addprefix python-message-,([0-9 ]+)\)$`).FindStringSubmatch(text)
	if list == nil {
		return fmt.Errorf("PYTHON_MESSAGE_SHARDS is not a literal shard list")
	}
	var shards []string
	for n := range strings.FieldsSeq(list[1]) {
		shards = append(shards, "python-message-"+n)
	}
	text = strings.ReplaceAll(text, "$(PYTHON_MESSAGE_SHARDS)", strings.Join(shards, " "))
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
	shardName := regexp.MustCompile(`^python-message-[0-9]+$`)
	for name := range runs {
		if shardName.MatchString(name) && !slices.Contains(shards, name) {
			return fmt.Errorf("message row %s is outside PYTHON_MESSAGE_SHARDS", name)
		}
	}
	if slices.Contains(registered, "python-message") {
		return fmt.Errorf("monolithic python-message is registered as a CI row")
	}
	for _, shard := range shards {
		if n := len(slices.DeleteFunc(slices.Clone(registered), func(s string) bool { return s != shard })); n != 1 {
			return fmt.Errorf("message row %s registered %d times for CI, want once", shard, n)
		}
		if _, ok := runs[shard]; !ok {
			return fmt.Errorf("message row %s has no recipe", shard)
		}
		if !slices.Contains(rules["python-message"], shard) {
			return fmt.Errorf("python-message aggregate does not reach %s", shard)
		}
		if !slices.Contains(rules["test-python"], shard) {
			return fmt.Errorf("test-python does not reach message row %s", shard)
		}
	}
	var claimants []string
	var expressions []*regexp.Regexp
	for _, row := range registered {
		pattern, ok := runs[row]
		if !ok || slices.Contains(claimants, row) {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("message row %s: %v", row, err)
		}
		claimants = append(claimants, row)
		expressions = append(expressions, re)
	}
	used := make([]int, len(claimants))
	for _, name := range tests {
		var by []string
		for i, re := range expressions {
			if re.MatchString(name) {
				by = append(by, claimants[i])
				used[i]++
			}
		}
		if len(by) != 1 {
			return fmt.Errorf("message test %s run by %d registered rows %v, want exactly one", name, len(by), by)
		}
	}
	for i, n := range used {
		if n == 0 {
			return fmt.Errorf("message row %s runs no test", claimants[i])
		}
	}
	return nil
}

// pythonMessageTests reads the top-level message tests from the package
// source, independently of the Make partition.
func pythonMessageTests(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot(t), "internal/codegen/pythontable", "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(source, []byte("func TestPythonMessage")) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "TestPythonMessage") {
				names = append(names, fn.Name.Name)
			}
		}
	}
	if !slices.Contains(names, "TestPythonMessage") || !slices.Contains(names, "TestPythonMessageNegativeControl") {
		t.Fatalf("message test discovery is incomplete: %v", names)
	}
	return names
}

func TestPythonMessageCIPlan(t *testing.T) {
	t.Parallel()
	tests := pythonMessageTests(t)
	makefile, err := os.ReadFile(filepath.Join(repoRoot(t), "make/python-message.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := pythonMessagePlanCheck(tests, makefile); err != nil {
		t.Fatal(err)
	}
	mk := string(makefile)
	for _, tc := range []struct {
		name     string
		tests    []string
		makefile string
		reason   string
	}{
		{"missing-test", tests, strings.Replace(mk, "|Damage|", "|", 1), "run by 0 registered rows"},
		{"bare-suite-test", tests, strings.Replace(mk, "|RootIdentity)?$$'", "|RootIdentity)$$'", 1), "TestPythonMessage run by 0"},
		{"duplicate-test", tests, strings.Replace(mk, "(Announcement|", "(Bits|Announcement|", 1), "run by 2 registered rows"},
		{"shard-repeats-control", tests, strings.Replace(mk, "(Announcement|", "(CRC|Announcement|", 1), "run by 2 registered rows"},
		{"new-test", append(slices.Clone(tests), "TestPythonMessageFuture"), mk, "run by 0 registered rows"},
		{"monolith", tests, mk + "PYTHON_CI_SUPPORT_GROUPS += python-message\n", "monolithic"},
		{"unregistered-row", tests, strings.Replace(mk, "PYTHON_CI_SUPPORT_GROUPS += $(PYTHON_MESSAGE_SHARDS) ", "PYTHON_CI_SUPPORT_GROUPS += python-message-0 ", 1), "registered 0 times"},
		{"unregistered-control", tests, strings.Replace(mk, "python-message-crc-negative-control \\\n    python-message-onesided", "\\\n    python-message-onesided", 1), "TestPythonMessageCRC run by 0"},
		{"aggregate", tests, strings.Replace(mk, "python-message: $(PYTHON_MESSAGE_SHARDS) ", "python-message: python-message-0 ", 1), "aggregate does not reach"},
		{"local-entry", tests, strings.Replace(mk, "test-python: $(PYTHON_MESSAGE_SHARDS) ", "test-python: ", 1), "test-python does not reach"},
		{"row-without-recipe", tests, strings.Replace(mk, "python-message-,0 1)", "python-message-,0 1 2)", 1), "has no recipe"},
		{"stray-row", tests, strings.Replace(mk, "python-message-,0 1)", "python-message-,0)", 1), "outside PYTHON_MESSAGE_SHARDS"},
		{"dead-row", tests, strings.Replace(mk, "'^TestPythonMessageCRC$$'", "'^TestPythonMessageCRCGone$$'", 1), "TestPythonMessageCRC run by 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.makefile == mk && slices.Equal(tc.tests, tests) {
				t.Fatal("control did not change its input")
			}
			if err := pythonMessagePlanCheck(tc.tests, []byte(tc.makefile)); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("message plan control: %v, want %s", err, tc.reason)
			}
		})
	}
	t.Run("dead-dedicated-row", func(t *testing.T) {
		withoutCRC := slices.DeleteFunc(slices.Clone(tests), func(s string) bool { return s == "TestPythonMessageCRC" })
		if err := pythonMessagePlanCheck(withoutCRC, makefile); err == nil || !strings.Contains(err.Error(), "python-message-crc-negative-control runs no test") {
			t.Fatalf("message plan control: %v, want a dead row refused", err)
		}
	})
}
