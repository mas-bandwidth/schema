package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestRubyDesignPage holds the Ruby design to the page the other legs are
// written in: every section and decision row is present, and every layer of
// the plan has a task in the port/ruby work-set. Decision rulings and task
// progress may change; roadmap-check validates task states and their evidence.
func TestRubyDesignPage(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	page, err := os.ReadFile(filepath.Join(root, "docs", "PORT-RUBY.md"))
	if err != nil {
		t.Fatalf("docs/PORT-RUBY.md: %v", err)
	}
	text := string(page)
	for _, section := range []string{
		"- **One read.**",
		"## THE DECISIONS",
		"## THE PIN",
		"## THE VALUE MAPPING",
		"### Packet types (SPEC §4)",
		"### Tables (SPEC-TABLES §2)",
		"| schema | Ruby value | born as |",
		"## THE NUMBER MODEL",
		"## TEXT AND BYTES",
		"## ERRORS",
		"## THE BIT LAYER",
		"## THE RUNTIME LAYOUT",
		"## IDENTIFIERS",
		"## THE HARNESS",
		"## THE REGISTER",
		"## THE CARD MAP",
	} {
		if !strings.Contains(text, section) {
			t.Errorf("docs/PORT-RUBY.md is missing %q", section)
		}
	}

	for d := 1; d <= 8; d++ {
		row := "| D" + strconv.Itoa(d) + " |"
		found := false
		for _, line := range strings.Split(text, "\n") {
			if !strings.HasPrefix(line, row) {
				continue
			}
			found = true
			break
		}
		if !found {
			t.Errorf("docs/PORT-RUBY.md is missing decision row %q", row)
		}
	}

	plan, err := os.ReadFile(filepath.Join(root, "docs", "plans", "ruby-leg.md"))
	if err != nil {
		t.Fatalf("docs/plans/ruby-leg.md: %v", err)
	}
	taskRx := regexp.MustCompile(`port/ruby/r[0-9]+-[a-z0-9-]+`)
	seen := map[string]bool{}
	var ids []string
	for _, id := range taskRx.FindAllString(string(plan), -1) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		t.Fatalf("docs/plans/ruby-leg.md names no port/ruby layer tasks")
	}

	raw, err := os.ReadFile(filepath.Join(root, "docs", "roadmap.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	var workSet string
	tasks := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, `(:id "port/ruby" :type :work-set`) {
			workSet = line
		}
		for _, id := range ids {
			if strings.Contains(line, `(:id "`+id+`" :type :task`) {
				tasks[id] = line
			}
		}
	}
	if workSet == "" {
		t.Fatalf("docs/roadmap.sexp has no port/ruby work-set")
	}
	for _, id := range ids {
		_, ok := tasks[id]
		if !ok {
			t.Errorf("docs/roadmap.sexp has no task %q for a plan layer", id)
			continue
		}
		if !strings.Contains(workSet, `"`+id+`"`) {
			t.Errorf("the port/ruby work-set does not list %q", id)
		}
	}
}
