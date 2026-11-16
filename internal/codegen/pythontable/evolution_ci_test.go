package pythontable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// Derive names from the same value, reader and writer vectors as the complete
// test. This coverage gate needs no runtime probe or analyzer.
func evolutionCINames() []string {
	var names []string
	for _, c := range vtEvolutionCases {
		names = append(names, "TestPythonVtEvolution/"+c.name)
	}
	order, groups := vtWireGroups(vtWireCases)
	for i, reader := range order {
		names = append(names, fmt.Sprintf("TestPythonVtEvolution/wire_%d_%s", i, groups[reader][0].root))
	}
	seen := map[string]bool{}
	n := 0
	for _, c := range vtWriteCases {
		if !seen[c.schema] {
			seen[c.schema] = true
			names = append(names, fmt.Sprintf("TestPythonVtEvolution/write_%d_%s", n, c.root))
			n++
		}
	}
	return names
}

func evolutionCICoverage(names []string, source string) error {
	rows := regexp.MustCompile("(?m)^\\tSCHEMA_SLOW=1 go test[^\\n]+-run '([^']+)'[^\\n]*$").FindAllStringSubmatch(source, -1)
	if len(rows) != 3 || len(names) == 0 {
		return fmt.Errorf("Evolution rows=%d, cases=%d", len(rows), len(names))
	}
	used := make([]int, len(rows))
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			return fmt.Errorf("duplicate Evolution name %s", name)
		}
		seen[name] = true
		matches := 0
		for i, row := range rows {
			pattern := strings.Split(strings.ReplaceAll(row[1], "$$", "$"), "/")
			parts := strings.Split(name, "/")
			good := len(pattern) == len(parts)
			for j := 0; good && j < len(parts); j++ {
				ok, err := regexp.MatchString(pattern[j], parts[j])
				if err != nil {
					return err
				}
				good = ok
			}
			if good {
				matches++
				used[i]++
			}
		}
		if matches != 1 {
			return fmt.Errorf("Evolution case %s selected %d times", name, matches)
		}
	}
	for i, count := range used {
		if count == 0 {
			return fmt.Errorf("unused Evolution selector %s", rows[i][1])
		}
	}
	return nil
}

func TestPythonVtEvolutionCICoverage(t *testing.T) {
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "make/python-evolution-ci.mk"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	names := evolutionCINames()
	if err := evolutionCICoverage(names, source); err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("(?m)^\\tSCHEMA_SLOW=1 go test[^\\n]+$").FindString(source)
	for label, changed := range map[string]string{
		"deleted":   strings.Replace(source, row, "", 1),
		"duplicate": source + "\n" + row,
		"stale":     strings.ReplaceAll(source, "wire_[0-9]+_.*", "wire_999_.*"),
	} {
		if evolutionCICoverage(names, changed) == nil {
			t.Fatalf("%s selector escaped coverage control", label)
		}
	}
	future := append(slices.Clone(names), "TestPythonVtEvolution/future_value_case")
	if evolutionCICoverage(future, source) == nil {
		t.Fatal("unregistered future value case escaped coverage control")
	}
	var registered []string
	for line := range strings.SplitSeq(source, "\n") {
		if tail, ok := strings.CutPrefix(line, "PYTHON_CI_SUPPORT_GROUPS += "); ok {
			registered = append(registered, strings.Fields(tail)...)
		}
	}
	for _, wanted := range []string{"python-ci-vt-evolution-values", "python-ci-vt-evolution-wire", "python-ci-vt-evolution-write"} {
		n := 0
		for _, name := range registered {
			if name == wanted {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("Evolution support root %s occurs %d times", wanted, n)
		}
	}
	planner, err := os.ReadFile(filepath.Join(root, "tools/python-ci-rows.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(planner), "/^TestPythonVtEvolution$/d") != 1 {
		t.Fatal("complete Evolution parent must be delegated exactly once")
	}
	t.Logf("all %d existing Evolution subtest groups selected exactly once", len(names))
}
