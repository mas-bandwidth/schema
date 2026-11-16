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
	var rows []struct{ Name, Test string }
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
	if len(patterns) != 8 {
		t.Fatalf("got %d strict rows, want 8", len(patterns))
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
