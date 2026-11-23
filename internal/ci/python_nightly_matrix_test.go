package ci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// pythonNightlyRows checks the actual workflow axes and the command that maps
// each tuple to one full, unsharded make row. It refuses extra axes and matrix
// include/exclude entries so no scheduled tuple can silently disappear.
func pythonNightlyRows(wf string) ([]string, error) {
	for _, line := range []string{
		"if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
		"PGROUP_GO_TEST_TIMEOUT: '0'", "fail-fast: false", "max-parallel: 4",
		"shell: go run ./tools/pgroup -- bash -e -o pipefail {0}",
		"uses: ./.github/actions/python", "make serialize-siblings", "sh tools/python-ci-corpus.sh build",
		"FUZZ_SEED: ${{ matrix.seed }}", "FUZZ_PIN: ${{ matrix.pin }}", "FUZZ_MODE: ${{ matrix.mode }}", "FUZZ_ARM: ${{ matrix.arm }}",
		`run: make "python-wirefuzz-${FUZZ_PIN}-${FUZZ_MODE}-${FUZZ_ARM}" "SEED=${FUZZ_SEED}" N=500000 PYTHON_WIRE_FUZZ_DIR=build/python-wirefuzz-nightly`,
	} {
		if !strings.Contains(wf, line) {
			return nil, fmt.Errorf("missing workflow contract %q", line)
		}
	}
	if strings.Contains(wf, "continue-on-error:") {
		return nil, fmt.Errorf("nightly row can ignore errors")
	}
	start := strings.Index(wf, "      matrix:\n")
	end := strings.Index(wf, "    defaults:\n")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("missing matrix block")
	}
	block := wf[start+len("      matrix:\n") : end]
	pattern := regexp.MustCompile(`^        (seed|pin|mode|arm): \[([^]]+)\]$`)
	axes := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimSuffix(block, "\n"), "\n") {
		m := pattern.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("unexpected matrix entry %q", line)
		}
		if _, ok := axes[m[1]]; ok {
			return nil, fmt.Errorf("duplicate matrix axis %s", m[1])
		}
		for value := range strings.SplitSeq(m[2], ",") {
			axes[m[1]] = append(axes[m[1]], strings.Trim(strings.TrimSpace(value), "'\""))
		}
	}
	want := map[string][]string{"seed": {"24845619678", "2"}, "pin": {"current", "floor"}, "mode": {"normal", "dev"}, "arm": {"plain", "mixed", "retain"}}
	for name, values := range want {
		if !slices.Equal(axes[name], values) {
			return nil, fmt.Errorf("wrong %s axis: %v", name, axes[name])
		}
	}
	var rows []string
	for _, seed := range axes["seed"] {
		for _, pin := range axes["pin"] {
			for _, mode := range axes["mode"] {
				for _, arm := range axes["arm"] {
					rows = append(rows, seed+"/python-wirefuzz-"+pin+"-"+mode+"-"+arm)
				}
			}
		}
	}
	return rows, nil
}

func TestPythonNightlyFuzzMatrix(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/schema-python-nightly-fuzz.yml"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := pythonNightlyRows(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 24 {
		t.Fatalf("nightly rows = %d, want 24", len(rows))
	}
	// Ask the real make fragment for its unsharded target names, then compare
	// the entire Cartesian product, including multiplicity and both seeds.
	probe := filepath.Join(t.TempDir(), "probe.mk")
	if err := os.WriteFile(probe, []byte("python-nightly-target-list:;\n\t@printf '%s\\n' $(PYTHON_WIRE_FUZZ_ROWS)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("make", "-s", "-f", "Makefile", "-f", probe, "python-nightly-target-list")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list make rows: %v\n%s", err, out)
	}
	targets := strings.Fields(string(out))
	if len(targets) != 12 {
		t.Fatalf("unsharded targets = %d, want 12", len(targets))
	}
	want := map[string]int{}
	for _, seed := range []string{"24845619678", "2"} {
		for _, target := range targets {
			want[seed+"/"+target]++
		}
	}
	for _, row := range rows {
		want[row]--
	}
	for row, count := range want {
		if count != 0 {
			t.Errorf("nightly row %s count difference %d", row, count)
		}
	}
	for _, ev := range []ciEvent{{Name: "schedule", Branch: "main", Repo: "mas-bandwidth/schema"}, {Name: "workflow_dispatch", Branch: "main", Repo: "mas-bandwidth/schema"}} {
		if ok, err := jobRunsInWorkflow(string(data), "nightly", ev); err != nil || !ok {
			t.Errorf("nightly not admitted for %+v: %v", ev, err)
		}
	}
	for _, ev := range []ciEvent{{Name: "push", Branch: "main", Repo: "mas-bandwidth/schema"}, {Name: "pull_request", Branch: "main", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}} {
		if ok, _ := jobRunsInWorkflow(string(data), "nightly", ev); ok {
			t.Errorf("nightly unexpectedly admitted for %+v", ev)
		}
	}
}

func TestPythonNightlyFuzzMatrixNegativeControl(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/schema-python-nightly-fuzz.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, new string }{
		{"missing-arm", "arm: [plain, mixed, retain]", "arm: [plain, mixed]"},
		{"duplicate-seed", "seed: ['24845619678', '2']", "seed: ['2', '2']"},
		{"reduced-count", "N=500000", "N=100000"},
		{"unbounded-parallel", "max-parallel: 4", "max-parallel: 5"},
		{"extra-axis", "arm: [plain, mixed, retain]", "arm: [plain, mixed, retain]\n        extra: [x]"},
		{"ignored-failure", "fail-fast: false", "fail-fast: false\n    continue-on-error: true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := string(data)
			if strings.Count(base, tc.old) != 1 {
				t.Fatalf("control anchor %q changed", tc.old)
			}
			if _, err := pythonNightlyRows(strings.Replace(base, tc.old, tc.new, 1)); err == nil {
				t.Fatal("mutant escaped the matrix guard")
			}
		})
	}
}
