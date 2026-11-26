package ci

// EVERY GO CONFORMANCE ROW RUNS EXACTLY ONCE. The halves conformance-go-rows-a
// and -b (make/go.mk) run every `func TestRow…` of test/conformance/go/rows,
// read off the directory. A row the registry also holds as its own gotest row
// (TestRowR14GrownTagReadsTheWritersTagWidth was added so) runs through
// schema-test, and ran a second time in the halves. go.mk leaves a registry-held
// row out of the halves, so each runs once and none is lost.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

const goRowsPackage = "./test/conformance/go/rows"

var goRowFuncRe = regexp.MustCompile(`(?m)^func (TestRow[A-Za-z0-9_]*)\(t \*testing\.T\)`)

// goRowRuns counts the runs of each Go row: its places in the halves plus
// its registry rows.
func goRowRuns(rows, halves []string, registryRows map[string]int) map[string]int {
	runs := map[string]int{}
	for _, r := range rows {
		runs[r] = registryRows[r]
	}
	for _, h := range halves {
		if _, ok := runs[h]; ok {
			runs[h]++
		}
	}
	return runs
}

func TestEveryGoConformanceRowRunsExactlyOnce(t *testing.T) {
	t.Parallel()
	testinputs.Make(t, "test/conformance/go/rows", "test/registry/registry.jsonl")
	root := repoRoot(t)

	files, err := provesran.Glob(filepath.Join(root, "test", "conformance", "go", "rows", "*_test.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no row file under test/conformance/go/rows: %v", err)
	}
	var rows []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range goRowFuncRe.FindAllStringSubmatch(string(b), -1) {
			rows = append(rows, m[1])
		}
	}

	reg, err := testreg.Load(filepath.Join(root, filepath.FromSlash(testreg.DefaultPath)))
	if err != nil {
		t.Fatal(err)
	}
	registryRows := map[string]int{}
	for _, row := range reg.Tests {
		if row.Adapter != "gotest" {
			continue
		}
		var in struct{ Package, Test string }
		if json.Unmarshal(row.Inputs, &in) == nil && in.Package == goRowsPackage {
			registryRows[in.Test]++
		}
	}
	if len(registryRows) == 0 {
		t.Fatalf("the registry holds no gotest row of %s; the check no longer reaches the row it exists for", goRowsPackage)
	}

	cmd := exec.Command("make", "--no-print-directory", "-pq", "__no_such_target__")
	cmd.Dir = root
	var halves []string
	for _, line := range strings.Split(string(makeQueryOutput(t, cmd)), "\n") {
		for _, v := range []string{"GO_ROWS_A", "GO_ROWS_B"} {
			if rest, ok := strings.CutPrefix(line, v+" := "); ok {
				halves = append(halves, strings.Fields(rest)...)
			}
		}
	}
	if len(halves) == 0 {
		t.Fatal("make go.mk prints no GO_ROWS_A / GO_ROWS_B value")
	}
	for _, h := range halves {
		if !slices.Contains(rows, h) {
			t.Errorf("the halves run %s, which no file of %s declares", h, goRowsPackage)
		}
	}

	for _, r := range rows {
		if n := goRowRuns(rows, halves, registryRows)[r]; n != 1 {
			t.Errorf("%s runs %d times (the registry holds it %d times, the halves %d); each row runs exactly once", r, n, registryRows[r], n-registryRows[r])
		}
	}

	// the fixture pair: a row in both places is caught, one in each alone is not
	if got := goRowRuns([]string{"TestRowA", "TestRowB"}, []string{"TestRowA", "TestRowB"}, map[string]int{"TestRowB": 1}); got["TestRowA"] != 1 || got["TestRowB"] != 2 {
		t.Errorf("a row in the halves and in the registry counts %v, want TestRowA 1 and TestRowB 2", got)
	}
	if got := goRowRuns([]string{"TestRowA", "TestRowB"}, []string{"TestRowA"}, map[string]int{"TestRowB": 1}); got["TestRowA"] != 1 || got["TestRowB"] != 1 {
		t.Errorf("a row in one place counts %v, want 1 each", got)
	}
	// a row in neither place is no run: it counts 0 and is named
	if got := goRowRuns([]string{"TestRowA"}, nil, nil); got["TestRowA"] != 0 {
		t.Errorf("a row run nowhere counts %v, want 0", got)
	}
}
