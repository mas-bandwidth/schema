package ci

// THE PER-PR JOBS RUN WHEN THEY ARE SUPPOSED TO (the owner, 2026-11-24: "I
// have zero patience for tests not running when they are supposed to"). Main's
// push run 45431445496 showed the five `fixed form + versioning` matrices,
// newleg-scaffold and go-fixed-corpus SKIPPED; #5005 changed make/rust.mk and
// the plan left the input-family matrix out without a word; a dispatch diffed
// against a dev branch a month stale. Each test here holds one of those shut,
// and each carries a planted control that must turn it red.

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// emptySelection is every plan output a plan-gated job reads, each at the
// value that selects nothing: what a push to main whose diff touched no rule
// would write, but for all.
func emptySelection(all string) map[string]string {
	return map[string]string{
		"all":                   all,
		"lint_rows":             "[]",
		"codegen_touched":       "no",
		"clean_legs":            "[]",
		"lints":                 "[]",
		"nightly_rows":          "[]",
		"verify_legs":           "[]",
		"legs":                  "[]",
		"dart_corpus_account":   "false",
		"cpp_corpus_account":    "false",
		"elixir_corpus_account": "false",
		"input_rows":            "[]",
		"corpus_shards":         "[]",
		"newleg":                "no",
		"rows":                  "[]",
		"packages":              "",
	}
}

// skippedOnMainPush answers the ci-fast.yml jobs that would not run on a push
// to main whose plan wrote outputs, the fork guard aside.
func skippedOnMainPush(t *testing.T, wf string, outputs map[string]string) []string {
	t.Helper()
	ev := ciEvent{Name: "push", Branch: "main", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema", PlanOutputs: outputs}
	var skipped []string
	for job := range yamlJobs(wf) {
		if job == "fork-pull-request" {
			continue
		}
		runs, err := jobRunsInWorkflow(wf, job, ev)
		if err != nil {
			t.Fatalf("%s: %v", job, err)
		}
		if !runs {
			skipped = append(skipped, job)
		}
	}
	sort.Strings(skipped)
	return skipped
}

func readFastWorkflow(t *testing.T) string {
	t.Helper()
	testinputs.Files(t, ".github/workflows/ci-fast.yml")
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// runSelector runs ci-fast.yml plan's selector (fastPlanSelector) over files
// in the repository root with env, and answers its printed verdict.
func runSelector(t *testing.T, wf string, files []string, verdict string, env ...string) string {
	t.Helper()
	list := filepath.Join(t.TempDir(), "files.txt")
	if err := os.WriteFile(list, []byte(strings.Join(files, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	// the selector's packages block reads the tree's codegen directories
	testinputs.Files(t, "internal/codegen")
	cmd := exec.Command("bash", "-euo", "pipefail", "-c", fastPlanSelector(t, wf, verdict))
	cmd.Dir = repoRoot(t)
	cmd.Env = append(append(scriptEnv(os.Environ()), "PLAN_FILES="+list), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("selector: %v: %s", err, out)
	}
	return selectorOutput(out)
}

// TestNoFastJobSkipsOnAMainPush: on a push to main every ci-fast.yml job runs
// but the fork guard. A plan output of all=yes admits each plan-gated job
// with every selection empty, and the plan's own selector, run as a push to
// main, writes all=yes and a non-empty selection for every matrix, so no
// matrix comes out empty and no job skips for want of a row.
func TestNoFastJobSkipsOnAMainPush(t *testing.T) {
	t.Parallel()
	wf := readFastWorkflow(t)
	if skipped := skippedOnMainPush(t, wf, emptySelection("yes")); len(skipped) > 0 {
		t.Errorf("on a push to main these ci-fast.yml jobs would skip (only fork-pull-request may): %s", strings.Join(skipped, ", "))
	}

	// the selector, as a push to main with a diff that touches no rule
	verdict := `printf '%s|%s|%s|%s|%s|%s|%s' "$all" "$verify_legs_json" "$lint_rows" "$clean_legs_json" "$lints_json" "$codegen_touched" "${packages:+packages}"`
	got := runSelector(t, wf, []string{"README.md"}, verdict, "EVENT=push", "REF=refs/heads/main")
	f := strings.Split(got, "|")
	if len(f) != 7 || f[0] != "yes" || f[1] == "[]" || f[2] == "[]" || f[3] == "[]" || f[4] == "[]" || f[5] != "yes" || f[6] != "packages" {
		t.Errorf("a push to main must select every matrix (all|verify|lint rows|clean|lints|codegen|packages), got %q", got)
	}
	for _, leg := range []string{"cpp", "c", "go", "rust", "js", "cs", "unity", "java", "dart", "elixir", "lua", "luau", "moo", "squirrel", "goose", "lobster", "python", "ruby"} {
		if !strings.Contains(f[1], `"`+leg+`"`) {
			t.Errorf("a push to main must verify every leg: %s is not in %s", leg, f[1])
		}
	}
	for _, tc := range []struct{ name, event, ref, dispatch, want string }{
		{"a dispatch runs every job by default", "workflow_dispatch", "refs/heads/rowan/x", "true", "yes"},
		{"a dispatch with all=false selects by its diff", "workflow_dispatch", "refs/heads/rowan/x", "false", "no"},
		{"a pull request selects by its diff", "pull_request", "refs/pull/1/merge", "", "no"},
		{"a push to another branch selects by its diff", "push", "refs/heads/sprint/x", "", "no"},
	} {
		if got := runSelector(t, wf, []string{"README.md"}, `printf '%s' "$all"`, "EVENT="+tc.event, "REF="+tc.ref, "DISPATCH_ALL="+tc.dispatch); got != tc.want {
			t.Errorf("%s: all=%q, want %q", tc.name, got, tc.want)
		}
	}

	// PLANTED: a main push whose plan did not say all, and a job whose
	// condition lost its all guard. Each must leave a job skipped.
	if skipped := skippedOnMainPush(t, wf, emptySelection("no")); len(skipped) == 0 {
		t.Error("PLANTED CONTROL FAILED: with all=no and every selection empty, no job skipped: the check reads nothing")
	}
	guard := "if: ((needs.plan.outputs.all == 'yes' || needs.plan.outputs.verify_legs != '[]') &&"
	planted := strings.Replace(wf, guard, "if: ((needs.plan.outputs.verify_legs != '[]') &&", 1)
	if planted == wf {
		t.Fatalf("the planted control did not apply: %q is not in ci-fast.yml", guard)
	}
	if skipped := skippedOnMainPush(t, planted, emptySelection("yes")); !strings.Contains(strings.Join(skipped, " "), "verify-leg") {
		t.Errorf("PLANTED CONTROL FAILED: verify-leg without its all guard still ran on a main push (skipped: %v)", skipped)
	}
	plantedPlan := strings.Replace(wf, "          if [ \"${EVENT:-}\" = push ] && [ \"${REF:-}\" = refs/heads/main ]; then\n            all=yes", "          if false; then\n            all=yes", 1)
	if plantedPlan == wf {
		t.Fatal("the planted plan control did not apply: the push-to-main all rule moved")
	}
	if got := runSelector(t, plantedPlan, []string{"README.md"}, `printf '%s|%s' "$all" "$verify_legs_json"`, "EVENT=push", "REF=refs/heads/main"); got != "no|[]" {
		t.Errorf("PLANTED CONTROL FAILED: a plan that never sets all on a main push gave %q, want no|[]", got)
	}
}

// omissionLog answers the omission log block of ci-fast.yml's plan.
func omissionLog(t *testing.T, wf string) string {
	t.Helper()
	_, rest, ok := strings.Cut(wf, "          # BEGIN THE OMISSION LOG\n")
	if !ok {
		t.Fatal("ci-fast.yml has no BEGIN THE OMISSION LOG marker")
	}
	block, _, ok := strings.Cut(rest, "          # END THE OMISSION LOG\n")
	if !ok {
		t.Fatal("ci-fast.yml has no END THE OMISSION LOG marker")
	}
	var b strings.Builder
	for line := range strings.SplitSeq(block, "\n") {
		b.WriteString(strings.TrimPrefix(line, "          "))
		b.WriteByte('\n')
	}
	return b.String()
}

// planGatedJobs answers the ci-fast.yml jobs whose condition reads a plan
// output.
func planGatedJobs(wf string) []string {
	var out []string
	for job := range yamlJobs(wf) {
		if strings.Contains(jobIfCondition(wf, job), "needs.plan.outputs.") {
			out = append(out, job)
		}
	}
	sort.Strings(out)
	return out
}

// runOmissionLog runs the block with every selection empty and answers its
// output and whether it exited 0.
func runOmissionLog(t *testing.T, block, all, unmapped string) (string, bool) {
	t.Helper()
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "unmapped-files.txt"), []byte(unmapped), 0o644); err != nil {
		t.Fatal(err)
	}
	// testinputs:none — the block reads only its environment and a temp file
	cmd := exec.Command("bash", "-euo", "pipefail", "-c", block)
	cmd.Dir = tmp
	cmd.Env = append(scriptEnv(os.Environ()), "RUNNER_TEMP="+tmp, "ALL="+all,
		"LINT_ROWS=[]", "CODEGEN=no", "CLEAN=[]", "LINTS=[]", "NIGHTLY=[]", "VERIFY=[]",
		"DART_CORPUS=false", "CPP_CORPUS=false", "ELIXIR_CORPUS=false", "INPUT_ROWS=[]",
		"CORPUS_SHARDS=[]", "NEWLEG=no", "ROWS=[]", "PACKAGES=", "ROWS_LEGS=[]", "ROWS_ELIXIR=no")
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

var leftOutRe = regexp.MustCompile(`(?m)^::notice title=left out: ([a-z0-9-]+)::([a-z0-9-]+) left out: .+$`)

// unloggedJobs answers the plan-gated jobs the log never names.
func unloggedJobs(wf, out string) []string {
	named := map[string]bool{}
	for _, m := range leftOutRe.FindAllStringSubmatch(out, -1) {
		if m[1] == m[2] {
			named[m[1]] = true
		}
	}
	var missing []string
	for _, job := range planGatedJobs(wf) {
		if !named[job] {
			missing = append(missing, job)
		}
	}
	return missing
}

// TestPlanOmissionLogNamesEveryJob: when the plan selects nothing, the plan's
// omission log names every job it leaves out, each with the rule that left it
// out and the files no rule named; and on all=yes a job left out is red.
func TestPlanOmissionLogNamesEveryJob(t *testing.T) {
	t.Parallel()
	wf := readFastWorkflow(t)
	block := omissionLog(t, wf)
	if len(planGatedJobs(wf)) < 15 {
		t.Fatalf("found %d plan-gated jobs in ci-fast.yml: the scan reads nothing", len(planGatedJobs(wf)))
	}
	out, ok := runOmissionLog(t, block, "no", "make/zz-unmapped.mk\n")
	if !ok {
		t.Fatalf("the omission log failed with all=no: %s", out)
	}
	if missing := unloggedJobs(wf, out); len(missing) > 0 {
		t.Errorf("the plan's omission log does not name these plan-gated jobs when it leaves them out: %s\n%s", strings.Join(missing, ", "), out)
	}
	if !strings.Contains(out, "input-family left out: ") || !strings.Contains(out, "; no rule for make/zz-unmapped.mk") {
		t.Errorf("the log must say why input-family was left out and name the file no rule named, as in \"input-family left out: ...; no rule for make/rust.mk\":\n%s", out)
	}
	if !strings.Contains(out, "::warning title=no rule for make/zz-unmapped.mk::") {
		t.Errorf("the log must warn once per file no rule named:\n%s", out)
	}
	if out, ok := runOmissionLog(t, block, "yes", ""); ok || !strings.Contains(out, "::error::every per-PR job runs on a push to main") {
		t.Errorf("on all=yes a job left out must be red, got ok=%v:\n%s", ok, out)
	}

	// PLANTED: a log line dropped, and a plan-gated job the log does not know.
	plantedBlock := strings.Replace(block, `has "$VERIFY" goose || omit verify-goose-fuzz`, `true || omit verify-goose-fuzz`, 1)
	if plantedBlock == block {
		t.Fatal("the planted control did not apply: the verify-goose-fuzz line moved")
	}
	if out, _ := runOmissionLog(t, plantedBlock, "no", ""); !strings.Contains(strings.Join(unloggedJobs(wf, out), " "), "verify-goose-fuzz") {
		t.Error("PLANTED CONTROL FAILED: a log without its verify-goose-fuzz line still named every job")
	}
	plantedWF := strings.Replace(wf, "\n  verdict:\n", "\n  zz-planted:\n    needs: plan\n    if: needs.plan.outputs.zz != ''\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n\n  verdict:\n", 1)
	if plantedWF == wf {
		t.Fatal("the planted control did not apply: no verdict job")
	}
	if !strings.Contains(strings.Join(unloggedJobs(plantedWF, out), " "), "zz-planted") {
		t.Error("PLANTED CONTROL FAILED: a plan-gated job the log never names went unseen")
	}
}

// treeFiles answers every file under the roots, relative to the repository
// root, leaving out build output a local run leaves in the tree.
func treeFiles(t *testing.T, root string, tops ...string) []string {
	t.Helper()
	var files []string
	for _, top := range tops {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "target", "node_modules", "_build", "deps", ".dart_tool", ".zig-cache":
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// TestEveryTreePathMapsToAJob: every file under make/, test/,
// internal/codegen/ and tools/ is named by a rule of the plan, so a change to
// it selects at least one job or group. #5005 changed make/rust.mk and the
// input-family rows of the Rust leg were left out with nothing said.
func TestEveryTreePathMapsToAJob(t *testing.T) {
	t.Parallel()
	wf := readFastWorkflow(t)
	root := repoRoot(t)
	tops := []string{"make", "test", "internal/codegen", "tools"}
	testinputs.Files(t, tops...)
	files := treeFiles(t, root, tops...)
	if len(files) < 500 {
		t.Fatalf("found %d files under %v: the walk reads nothing", len(files), tops)
	}
	// one line: the selector's output is read from its last line (selectorOutput),
	// so a list printed one file a line would show only its last file
	verdict := `tr '\n' ' ' <<<"$unmapped"`
	if got := strings.TrimSpace(runSelector(t, wf, files, verdict)); got != "" {
		t.Errorf("no rule of ci-fast.yml's plan names these files, so a change to one selects no job:\n%s", got)
	}
	// every make file selects its own leg's jobs, or every leg's
	if got := runSelector(t, wf, []string{"make/rust.mk"}, `printf '%s|%s' "$touched_legs" "$make_shared"`); got != " rust|no" {
		t.Errorf("make/rust.mk must select the rust leg alone, got %q", got)
	}
	if got := runSelector(t, wf, []string{"make/verify-compiled.mk"}, `printf '%s|%s' "$make_shared" "$verify_legs_json"`); !strings.HasPrefix(got, "yes|") || !strings.Contains(got, `"rust"`) || !strings.Contains(got, `"goose"`) {
		t.Errorf("a shared make file must select every leg, got %q", got)
	}

	// PLANTED: a new file under tools/ and under test/ that no rule names.
	for _, p := range []string{"tools/zz-new/data.bin", "test/zz-new/case.txt"} {
		if got := runSelector(t, wf, append(files[:len(files):len(files)], p), verdict); !strings.Contains(got, p) {
			t.Errorf("PLANTED CONTROL FAILED: %s, which no rule names, was not reported (got %q)", p, got)
		}
	}
}

// TestFastPlanDiffsAgainstMain: a dispatch diffs against main, takes the input
// all (default true) that runs every per-PR job, and the plan never reads dev,
// a branch that sat at 2e35a95185 from 2026-10-25.
func TestFastPlanDiffsAgainstMain(t *testing.T) {
	t.Parallel()
	wf := readFastWorkflow(t)
	check := func(wf string) []string {
		var bad []string
		_, plan, _ := strings.Cut(wf, "\n  plan:\n")
		plan, _, _ = strings.Cut(plan, "\n  lint-fast:\n")
		if !strings.Contains(plan, "\n          BASE: main\n") {
			bad = append(bad, "the plan's BASE is not main")
		}
		if regexp.MustCompile(`(?m)^[ \t]*[^#\s][^\n]*('dev'|origin/dev|\bdev\.\.\.)`).MatchString(plan) {
			bad = append(bad, "the plan reads dev")
		}
		if !regexp.MustCompile(`(?m)^  workflow_dispatch:\n(?:    #[^\n]*\n)*    inputs:\n      all:\n        description: [^\n]+\n        type: boolean\n        default: true\n`).MatchString(wf) {
			bad = append(bad, "the dispatch has no boolean input all defaulting to true")
		}
		if !strings.Contains(plan, `if [ "${EVENT:-}" = workflow_dispatch ] && [ "${DISPATCH_ALL:-}" != false ]; then`) {
			bad = append(bad, "the plan does not set all on a dispatch that did not pass all=false")
		}
		return bad
	}
	if bad := check(wf); len(bad) > 0 {
		t.Errorf("ci-fast.yml: %s", strings.Join(bad, "; "))
	}
	for _, p := range []struct{ name, old, new string }{
		{"BASE back to dev on a dispatch", "\n          BASE: main\n", "\n          BASE: ${{ github.event_name == 'pull_request' && 'main' || 'dev' }}\n"},
		{"the all input defaulting to false", "        type: boolean\n        default: true\n", "        type: boolean\n        default: false\n"},
		{"a dispatch never setting all", `if [ "${EVENT:-}" = workflow_dispatch ] && [ "${DISPATCH_ALL:-}" != false ]; then`, `if false; then`},
	} {
		planted := strings.Replace(wf, p.old, p.new, 1)
		if planted == wf {
			t.Fatalf("the planted control %q did not apply", p.name)
		}
		if len(check(planted)) == 0 {
			t.Errorf("PLANTED CONTROL FAILED: %s went unseen", p.name)
		}
	}
}
