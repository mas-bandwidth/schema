package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// TestFastPlanSelectsNewLegScaffold runs ci-fast.yml's own NEW-LEG SCAFFOLD
// block (in `plan`'s pick step) over the committed cases in
// testdata/newleg-plan.json, from the repository root where the block reads
// compiler/newleg.go and asks `go list -deps` for the scaffold's import
// closure. A change to the templates, to the fixture test or to a package the
// scaffold compiles against selects the newleg-scaffold job; documentation, an
// emitter outside the closure and a test-only change do not (#4541: #4428
// changed the scaffold's imports and no per-commit row ran its fixture).
func TestFastPlanSelectsNewLegScaffold(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(data)
	_, tail, ok := strings.Cut(wf, "          # THE NEW-LEG SCAFFOLD (newleg-scaffold, #4541).")
	if !ok {
		t.Fatal("ci-fast.yml has no NEW-LEG SCAFFOLD block in plan")
	}
	block, _, ok := strings.Cut(tail, "          # END OF THE NEW-LEG SCAFFOLD.")
	if !ok {
		t.Fatal("ci-fast.yml's NEW-LEG SCAFFOLD block has no end marker")
	}
	_, block, _ = strings.Cut(block, "\n")
	var script strings.Builder
	for line := range strings.SplitSeq(block, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(`printf '%s' "$newleg"`)

	// the job the block's output gates runs the fixture test, under the slow
	// gate, and is gated on exactly that output
	for _, want := range []string{
		"      newleg: ${{ steps.pick.outputs.newleg }}",
		`          echo "newleg=$newleg"             >> "$GITHUB_OUTPUT"`,
		"  newleg-scaffold:\n    needs: plan\n",
		"needs.plan.outputs.newleg != 'no'",
		"        run: go test -count=1 ./cmd/schema -run '^TestNewLegZigWritesSkeletonAndFixturePasses$'",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("ci-fast.yml lacks %q", want)
		}
	}
	_, job, _ := strings.Cut(wf, "  newleg-scaffold:\n")
	job, _, _ = strings.Cut(job, "\n\n  # ")
	if !strings.Contains(job, "SCHEMA_SLOW: '1'") || !strings.Contains(job, "timeout-minutes: 2") {
		t.Errorf("newleg-scaffold must set SCHEMA_SLOW=1 and keep the two-minute bound:\n%s", job)
	}

	raw, err := os.ReadFile(filepath.Join("testdata", "newleg-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID     string   `json:"id"`
		Files  []string `json:"files"`
		NewLeg string   `json:"newleg"`
		Note   string   `json:"note"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	verdicts := map[string]int{}
	for _, c := range cases {
		verdicts[c.NewLeg]++
	}
	if len(cases) == 0 || verdicts["yes"] == 0 || verdicts["no"] == 0 || verdicts["yes"]+verdicts["no"] != len(cases) {
		t.Fatalf("testdata/newleg-plan.json holds %d cases (%v); want both verdicts and nothing else", len(cases), verdicts)
	}
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel() // each case is its own bash over a read-only script
			for _, f := range c.Files {
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil && strings.HasSuffix(f, ".go") {
					t.Fatalf("case names %s, which the tree does not hold: %v", f, err)
				}
			}
			cmd := testproc.Command(t, "bash", "-euo", "pipefail", "-c", script.String())
			cmd.Dir = root
			cmd.Env = append(scriptEnv(os.Environ()), "files="+strings.Join(c.Files, "\n"))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("selector: %v: %s", err, out)
			}
			if got := selectorOutput(out); got != c.NewLeg {
				t.Fatalf("%v selected newleg=%q; want %q (%s)", c.Files, got, c.NewLeg, c.Note)
			}
		})
	}
}
