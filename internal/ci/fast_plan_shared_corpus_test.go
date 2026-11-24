package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/inputfamilies"
)

// TestFastPlanSharedCorpusSelectsEveryLeg runs ci-fast.yml's plan selector
// over a change to the shared conformance corpus and requires a verify-leg
// row for every one of the 18 legs: each leg's verify-<leg>-conformance runs
// the conformance harness over testdata/conformance/tables/MANIFEST.txt.
//
// PR 4908 changed MANIFEST.txt (rows that reach every leg) and the plan
// selected verify rows only for the legs whose code it touched: no cpp row,
// and verify-leg (elixir, conformance) skipped, which the verdict read as
// green. testdata/pr4908-changed-files.txt is that PR's whole file list.
// The corpus paths come from tools/inputfamilies' conformance-corpus family,
// so a new input there is a new case here, never a hand list.
func TestFastPlanSharedCorpusSelectsEveryLeg(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "          # THE BROAD RULE.")
	if !ok {
		t.Fatal("fast workflow broad selector not found")
	}
	selector, _, ok := strings.Cut(tail, "          echo \"broad=$broad\"")
	if !ok {
		t.Fatal("fast workflow output boundary not found")
	}
	_, selector, _ = strings.Cut(selector, "\n")
	var script strings.Builder
	for line := range strings.SplitSeq(selector, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(`printf '%s' "$verify_legs_json"`)

	legs := slices.Clone(inputfamilies.Legs)
	if len(legs) != 18 {
		t.Fatalf("inputfamilies.Legs has %d legs; want 18", len(legs))
	}
	slices.Sort(legs)
	want := `["` + strings.Join(legs, `","`) + `"]`

	pr, err := os.ReadFile(filepath.Join(root, "internal/ci/testdata/pr4908-changed-files.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, files string }{
		{"pr4908", strings.TrimSpace(string(pr))},
	}
	var corpus *inputfamilies.Family
	for i := range inputfamilies.Families {
		if inputfamilies.Families[i].Name == "conformance-corpus" {
			corpus = &inputfamilies.Families[i]
		}
	}
	if corpus == nil || len(corpus.Inputs) == 0 {
		t.Fatal("inputfamilies has no conformance-corpus family with inputs")
	}
	for _, in := range corpus.Inputs {
		// a file inside a "dir/" input, or the pattern's own text with its
		// wildcard filled in
		f := in
		if strings.HasSuffix(in, "/") {
			f = in + "MANIFEST.txt"
		}
		f = strings.ReplaceAll(f, "*", "x")
		if !corpus.Matches(f) {
			t.Fatalf("synthetic corpus file %q does not match its family", f)
		}
		cases = append(cases, struct{ name, files string }{"corpus " + f, f})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel() // each case is its own bash over a read-only script
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script.String())
			cmd.Env = append(scriptEnv(os.Environ()), "files="+tc.files)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("plan script: %v: %s", err, out)
			}
			if got := selectorOutput(out); got != want {
				t.Fatalf("a shared conformance corpus change selected verify_legs=%s; want every leg %s", got, want)
			}
		})
	}
}
