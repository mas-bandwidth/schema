package pythontable

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Every strict-types corpus unit must occur in exactly one bounded CI row.
func TestPythonReflectionCISelection(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python CI planner")
	cmd := exec.Command("sh", "tools/python-ci-rows.sh")
	cmd.Dir = "../../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("planner: %v\n%s", err, out)
	}
	var rows []struct{ Name, Test, Target, Package string }
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatal(err)
	}
	var patterns []*regexp.Regexp
	var named []string
	for _, row := range rows {
		if !strings.HasPrefix(row.Name, "python-reflection-strict-types-") {
			continue
		}
		_, pattern, ok := strings.Cut(row.Test, "/")
		if !ok {
			t.Fatalf("bad row %s: %q", row.Name, row.Test)
		}
		patterns = append(patterns, regexp.MustCompile(pattern))
		term := strings.TrimSuffix(strings.TrimPrefix(pattern, "^("), ")$")
		named = append(named, strings.Split(term, "|")...)
	}
	corpusRows := 0
	for _, row := range rows {
		if row.Target == "python-reflection-ci-corpus" {
			corpusRows++
		}
		if strings.HasSuffix(row.Package, "/internal/codegen/pythontable") && !strings.Contains(row.Test, "/") && row.Test != "" {
			pattern := regexp.MustCompile(row.Test)
			for _, name := range []string{"TestPythonReflectionCorpus", "TestPythonReflectionLayout", "TestPythonReflectionStrictTypes"} {
				if pattern.MatchString(name) {
					t.Fatalf("unbounded or duplicate CI parent %s in %s", name, row.Name)
				}
			}
		}
	}
	if corpusRows != 1 {
		t.Fatalf("got %d corpus support rows, want 1", corpusRows)
	}
	namesCmd := exec.Command("make", "-s", "python-reflection-ci-names")
	namesCmd.Dir = "../../.."
	selectedOut, err := namesCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list non-corpus reflection tests: %v\n%s", err, selectedOut)
	}
	listCmd := exec.Command("go", "test", "-list", "^TestPythonReflection", "./internal/codegen/pythontable/")
	listCmd.Dir = "../../.."
	allOut, err := listCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list reflection tests: %v\n%s", err, allOut)
	}
	selected := map[string]int{}
	for name := range strings.FieldsSeq(string(selectedOut)) {
		selected[name]++
	}
	for name := range strings.FieldsSeq(string(allOut)) {
		if !strings.HasPrefix(name, "TestPythonReflection") {
			continue
		}
		want := 1
		if name == "TestPythonReflectionCorpus" || name == "TestPythonReflectionLayout" || name == "TestPythonReflectionStrictTypes" {
			want = 0
		}
		if selected[name] != want {
			t.Fatalf("non-corpus test %s selected %d times, want %d", name, selected[name], want)
		}
		delete(selected, name)
	}
	if len(selected) != 0 {
		t.Fatalf("unknown non-corpus selections: %v", selected)
	}
	if len(patterns) != 12 {
		t.Fatalf("got %d strict rows, want 12", len(patterns))
	}
	units := reflectionUnits(t)
	allowed := map[string]bool{}
	for _, unit := range units {
		allowed[unit.name] = true
	}
	for _, name := range named {
		if !allowed[name] {
			t.Fatalf("extra strict CI unit %q", name)
		}
	}
	check := func(ps []*regexp.Regexp) bool {
		for _, unit := range units {
			matches := 0
			for _, pattern := range ps {
				if pattern.MatchString(unit.name) {
					matches++
				}
			}
			if matches != 1 {
				return false
			}
		}
		return true
	}
	if !check(patterns) {
		t.Fatal("strict CI rows omit or repeat a corpus unit")
	}
	if check(patterns[1:]) || check(append(patterns, patterns[0])) {
		t.Fatal("omission/duplication control escaped")
	}
}
