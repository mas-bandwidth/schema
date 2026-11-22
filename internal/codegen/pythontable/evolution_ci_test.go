package pythontable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
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
	rows := regexp.MustCompile(`(?m)^\tSCHEMA_SLOW=1 go test[^\n]+-run '([^']+)'[^\n]*$`).FindAllStringSubmatch(source, -1)
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
	slowtest.Gate(t, "sh")
	root, err := filepath.Abs("../../..")
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
	row := regexp.MustCompile(`(?m)^\tSCHEMA_SLOW=1 go test[^\n]+$`).FindString(source)
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
	cmd := testproc.Command(t, "sh", filepath.Join(root, "tools/python-ci-rows.sh"))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual CI plan: %v\n%s", err, out)
	}
	var rows []struct{ Target, Package, Test string }
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 || len(rows) > 512 {
		t.Fatalf("actual CI plan has %d rows", len(rows))
	}
	var original []json.RawMessage
	if err := json.Unmarshal(out, &original); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, row := range original {
		counts[string(row)]++
	}
	for _, part := range []string{"0", "1"} {
		cmd := testproc.Command(t, "sh", filepath.Join(root, "tools/python-ci-partition.sh"), part)
		cmd.Dir = root
		cmd.Stdin = bytes.NewReader(out)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("partition %s: %v\n%s", part, err, data)
		}
		var shard []json.RawMessage
		if err := json.Unmarshal(data, &shard); err != nil {
			t.Fatal(err)
		}
		if len(shard) == 0 || len(shard) > 256 {
			t.Fatalf("partition %s has %d rows", part, len(shard))
		}
		for _, row := range shard {
			counts[string(row)]--
		}
	}
	for _, remaining := range counts {
		if remaining != 0 {
			t.Fatal("matrix partitions omit or duplicate a planned row")
		}
	}
	targets := map[string]int{}
	parent, coverage := 0, 0
	for _, row := range rows {
		targets[row.Target]++
		if row.Package != "github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable" || row.Target != "" {
			continue
		}
		re, err := regexp.Compile("^" + row.Test + "$")
		if err != nil {
			t.Fatal(err)
		}
		if re.MatchString("TestPythonVtEvolution") {
			parent++
		}
		if re.MatchString("TestPythonVtEvolutionCICoverage") {
			coverage++
		}
	}
	if len(registered) == 0 {
		t.Fatal("registered is empty: the loop below would check nothing")
	}
	for _, target := range registered {
		if targets[target] != 1 {
			t.Fatalf("actual support target %s occurs %d times", target, targets[target])
		}
	}
	if parent != 0 || coverage != 1 {
		t.Fatalf("actual automatic selection parent=%d, coverage=%d", parent, coverage)
	}
	t.Logf("all %d existing Evolution subtest groups selected exactly once", len(names))
}
