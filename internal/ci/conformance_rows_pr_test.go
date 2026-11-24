package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// THE TEN PER-PR ROWS (2026-11-24). The whole legs' rows run nightly
// (conformance_rows_nightly_test.go), but ten rows ran per-PR before that move,
// through five verify targets ci-fast.yml's verify-leg job runs, and they keep
// running there: a per-PR verify target that loses one of them loses per-PR
// coverage nobody decided to give up. This test reads make's own database
// (make -p, nothing built) and holds, for each verify target:
//
//   - it has conformance-rows-pr-<leg> among its prerequisites;
//   - CONFORMANCE_ROWS_PR_<leg> names every one of its rows;
//   - conformance-rows-pr-<leg> runs tools/conformancerows for that leg with
//     -rows "$(CONFORMANCE_ROWS_PR_<leg>)", so the rows run against the
//     leg's ledger and its cause match, and nothing else picks the rows: the
//     recipe passes no -shard and the list holds row names only (a shard
//     would drop rows silently; the runner refuses -rows with one too).
//
// The reversed witness puts each breach into a copy of the make files and
// holds that each one is named.

var conformanceRowsPR = []struct {
	target, leg string
	rows        []string
}{
	{"_verify-c-conformance", "c", []string{"F4", "F10", "F12"}},
	{"_verify-cpp-conformance", "cpp", []string{"R15"}},
	{"_verify-dart-packets", "dart", []string{"E8", "R8", "R13"}},
	{"_verify-java-conformance", "java", []string{"F10", "R8"}},
	{"_verify-rust-conformance", "rust", []string{"F8"}},
}

var makeDBVarRe = regexp.MustCompile(`(?m)^CONFORMANCE_ROWS_PR_([a-z]+) :?= (.*)$`)

// makeRecipe is the recipe make's database prints for target: the tab lines
// after its rule, up to the blank line that ends the entry.
func makeRecipe(db, target string) (string, bool) {
	lines := strings.Split(db, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, target+":") {
			continue
		}
		var recipe []string
		for _, r := range lines[i+1:] {
			if r == "" {
				break
			}
			if after, ok := strings.CutPrefix(r, "\t"); ok {
				recipe = append(recipe, after)
			}
		}
		if len(recipe) > 0 {
			return strings.Join(recipe, "\n"), true
		}
	}
	return "", false
}

func conformanceRowsPRErrors(t *testing.T, db []byte) []string {
	t.Helper()
	graph := parseMakeGraph(t, db)
	vars := map[string][]string{}
	for _, m := range makeDBVarRe.FindAllStringSubmatch(string(db), -1) {
		vars[m[1]] = strings.Fields(m[2])
	}
	var errs []string
	for _, p := range conformanceRowsPR {
		rowsTarget := "conformance-rows-pr-" + p.leg
		if !slices.Contains(graph[p.target], rowsTarget) {
			errs = append(errs, fmt.Sprintf("%s does not run %s: the %s rows %s would run on no pull request", p.target, rowsTarget, p.leg, strings.Join(p.rows, " ")))
		}
		have, ok := vars[p.leg]
		if !ok {
			errs = append(errs, fmt.Sprintf("CONFORMANCE_ROWS_PR_%s is not set: the %s rows %s would run on no pull request", p.leg, p.leg, strings.Join(p.rows, " ")))
		}
		for _, f := range have {
			if strings.HasPrefix(f, "-") {
				errs = append(errs, fmt.Sprintf("CONFORMANCE_ROWS_PR_%s holds %q, which is not a row name: the list must name rows and nothing else", p.leg, f))
			}
		}
		for _, r := range p.rows {
			if ok && !slices.Contains(have, r) {
				errs = append(errs, fmt.Sprintf("CONFORMANCE_ROWS_PR_%s drops %s/%s: %s would no longer run it per-PR", p.leg, p.leg, r, p.target))
			}
		}
		recipe, ok := makeRecipe(string(db), rowsTarget)
		want := fmt.Sprintf(`./build/conformancerows -leg %s -rows "$(CONFORMANCE_ROWS_PR_%s)"`, p.leg, p.leg)
		if !ok || !strings.Contains(recipe, want) {
			errs = append(errs, fmt.Sprintf("%s does not run `%s`: its rows would not run as the list names them, against the %s ledger", rowsTarget, want, p.leg))
		}
		if strings.Contains(recipe, "-shard") {
			errs = append(errs, fmt.Sprintf("%s passes -shard: a shard would drop some of the %s rows %s without a word", rowsTarget, p.leg, strings.Join(p.rows, " ")))
		}
	}
	return errs
}

func TestConformanceRowsPRRunPerPR(t *testing.T) {
	root := repoRoot(t)
	if errs := conformanceRowsPRErrors(t, makeDatabase(t, root, nil)); len(errs) > 0 {
		t.Fatalf("the per-PR rows:\n  %s", strings.Join(errs, "\n  "))
	}
	// Every row named is a row file of its leg (the runner refuses a name
	// with none, so a typo here is red on the next pull request anyway).
	for _, p := range conformanceRowsPR {
		ext := map[string]string{"c": ".c", "cpp": ".cpp", "dart": ".dart", "java": ".java", "rust": ".rs"}[p.leg]
		for _, r := range p.rows {
			if _, err := os.Stat(filepath.Join(root, "test", "conformance", p.leg, "rows", r+ext)); err != nil {
				t.Errorf("%s/%s: %v", p.leg, r, err)
			}
		}
	}
}

func TestConformanceRowsPRRunPerPRNegativeControl(t *testing.T) {
	root := repoRoot(t)
	plant := func(file, from, to string) map[string]string {
		t.Helper()
		src, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), from) != 1 {
			t.Fatalf("%s: the plant's text %q does not stand once", file, from)
		}
		out := filepath.Join(t.TempDir(), filepath.Base(file))
		if err := os.WriteFile(out, []byte(strings.Replace(string(src), from, to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		return map[string]string{file: out}
	}
	for _, c := range []struct {
		name, file, from, to, want string
	}{
		{"verify target drops its rows", "make/verify-compiled.mk", " packet-finite-dart conformance-rows-pr-dart", " packet-finite-dart",
			"_verify-dart-packets does not run conformance-rows-pr-dart"},
		{"list drops a row", "make/conformance-rows.mk", "CONFORMANCE_ROWS_PR_c    := F4 F10 F12", "CONFORMANCE_ROWS_PR_c    := F4 F10",
			"CONFORMANCE_ROWS_PR_c drops c/F12"},
		{"rows not passed", "make/conformance-rows.mk", ` -rows "$$(CONFORMANCE_ROWS_PR_$(1))"`, "",
			`conformance-rows-pr-java does not run`},
		{"recipe passes a shard", "make/conformance-rows.mk", ` -rows "$$(CONFORMANCE_ROWS_PR_$(1))"`, ` -shard 1/2 -rows "$$(CONFORMANCE_ROWS_PR_$(1))"`,
			"conformance-rows-pr-c passes -shard"},
		{"list carries a shard", "make/conformance-rows.mk", "CONFORMANCE_ROWS_PR_c    := F4 F10 F12", "CONFORMANCE_ROWS_PR_c    := -shard 1/2 F4 F10 F12",
			`CONFORMANCE_ROWS_PR_c holds "-shard"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			errs := conformanceRowsPRErrors(t, makeDatabase(t, root, plant(c.file, c.from, c.to)))
			if !strings.Contains(strings.Join(errs, "\n"), c.want) {
				t.Fatalf("the plant went unnamed: want %q in\n  %s", c.want, strings.Join(errs, "\n  "))
			}
		})
	}
}
