package ci

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// echoIntoGrep is a condition decided by a pipeline whose left side can die of
// SIGPIPE: `echo "$var" | grep -q ...` or `echo "$var" | grep ... >/dev/null`.
// On a value larger than the pipe buffer grep stops reading at its first match
// (-q always; GNU grep with stdout on /dev/null too), echo is killed writing
// the rest, `set -o pipefail` fails the pipeline, and the `if` or `||` takes
// the branch for NO MATCH. PR 4445's 3011-file diff touched go.mod, Makefile,
// compiler/ and ir/ and ci-fast.yml plan said `broad=no` (run 44914934707).
// A here-string, `grep -q ... <<<"$var"`, has no writer to kill.
var echoIntoGrep = regexp.MustCompile(`echo\s+(?:-[neE]+\s+)?"?\$\{?[A-Za-z_][A-Za-z0-9_]*\}?"?\s*\|\s*grep((?:\s+-[-A-Za-z]+)*)(.*)`)

// grepQuiet is a quiet flag among grep's leading options; grepToNull is
// output thrown away later on the line.
var (
	grepQuiet  = regexp.MustCompile(`(?:^|\s)(?:-[A-Za-z]*q[A-Za-z]*|--quiet|--silent)(?:\s|$)`)
	grepToNull = regexp.MustCompile(`>\s*/dev/null`)
)

// echoIntoGrepErrors names every line of the given files that decides on echo
// into a quiet grep. Comment lines are prose and are skipped.
func echoIntoGrepErrors(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []string
	for _, name := range names {
		for i, line := range strings.Split(files[name], "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for _, m := range echoIntoGrep.FindAllStringSubmatch(line, -1) {
				if grepQuiet.MatchString(m[1]) || grepToNull.MatchString(m[2]) {
					errs = append(errs, fmt.Sprintf("%s:%d decides on echo into a quiet grep, which a value larger than the pipe buffer turns false under pipefail; use grep -q ... <<<\"$var\": %s", name, i+1, strings.TrimSpace(line)))
				}
			}
		}
	}
	return errs
}

// workflowAndActionFiles reads every workflow and composite action, keyed by
// its path under .github.
func workflowAndActionFiles(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out := map[string]string{}
	for name, text := range workflows(t, root) {
		out["workflows/"+name] = text
	}
	actions, err := provesran.Glob(filepath.Join(root, ".github", "actions", "*", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range actions {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(filepath.Join(root, ".github"), path)
		out[filepath.ToSlash(rel)] = string(data)
	}
	return out
}

// TestNoWorkflowDecidesOnEchoIntoGrep holds every workflow and action to the
// here-string form for a grep that decides a condition.
func TestNoWorkflowDecidesOnEchoIntoGrep(t *testing.T) {
	t.Parallel()
	files := workflowAndActionFiles(t)
	if errs := echoIntoGrepErrors(files); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
		t.FailNow()
	}
	fast := files["workflows/ci-fast.yml"]
	full := files["workflows/ci-full.yml"]
	for _, tc := range []struct {
		name, file, old, new string
	}{
		{"ci-fast broad rule back to echo | grep -qE", "workflows/ci-fast.yml",
			`if grep -qE '^(ir/|compiler/|tables/|Makefile$|go\.mod$|docs/FIXED-FORM-|internal/codegen/cpp`,
			`if echo "$files" | grep -qE '^(ir/|compiler/|tables/|Makefile$|go\.mod$|docs/FIXED-FORM-|internal/codegen/cpp`},
		{"ci-fast codegen rule back to echo | grep -E >/dev/null", "workflows/ci-fast.yml",
			`|| grep -qE '^(internal/codegen/|ir/|compiler/|tables/|generated/|test/generated-tree/)|\.schema$' <<<"$files"; then`,
			`|| echo "$files" | grep -E '^(internal/codegen/|ir/|compiler/|tables/|generated/|test/generated-tree/)|\.schema$' >/dev/null; then`},
		{"ci-fast lint rule back to echo | grep >/dev/null after ||", "workflows/ci-fast.yml",
			`|| grep -q 'schema-dart-generated-strict-analyze' <<<"$files"; then`,
			`|| echo "$files" | grep 'schema-dart-generated-strict-analyze' >/dev/null; then`},
		{"ci-full pr-select C# rule back to echo | grep -E >/dev/null", "workflows/ci-full.yml",
			`|| grep -qE '^(internal/codegen/cstable/|internal/codegen/csharp/|generated/cs/|test/cs-tables/|make/cs\.mk$|make/cs-[^/]*\.mk$)' <<<"$files"; then`,
			`|| echo "$files" | grep -E '^(internal/codegen/cstable/|internal/codegen/csharp/|generated/cs/|test/cs-tables/|make/cs\.mk$|make/cs-[^/]*\.mk$)' >/dev/null; then`},
		{"ci-full render smoke back to echo | grep -q", "workflows/ci-full.yml",
			`grep -q "language        %" <<<"$out" ||`,
			`echo "$out" | grep -q "language        %" ||`},
	} {
		src := fast
		if tc.file == "workflows/ci-full.yml" {
			src = full
		}
		mutated := strings.Replace(src, tc.old, tc.new, 1)
		if mutated == src {
			t.Fatalf("negative control %q did not mutate %s", tc.name, tc.file)
		}
		if errs := echoIntoGrepErrors(map[string]string{tc.file: mutated}); len(errs) == 0 {
			t.Errorf("NEGATIVE CONTROL FAILED: %q left echoIntoGrepErrors green", tc.name)
		}
	}
	// a grep that reads the whole stream (no -q, output kept) decides nothing
	// by stopping early, so it is not this defect
	if errs := echoIntoGrepErrors(map[string]string{"x.yml": `rows=$(echo "$out" | grep -cE "^  [a-z]+ ")`}); len(errs) != 0 {
		t.Errorf("a counting grep was flagged: %v", errs)
	}
}

// fastPlanSelector returns ci-fast.yml plan's selector, from THE BROAD RULE to
// the output lines, reading the diff from $PLAN_FILES, with a printf of the
// verdict appended.
func fastPlanSelector(t *testing.T, wf, verdict string) string {
	t.Helper()
	_, tail, ok := strings.Cut(wf, "          # THE BROAD RULE.")
	if !ok {
		t.Fatal("fast workflow broad selector not found")
	}
	selector, _, ok := strings.Cut(tail, "          echo \"broad=$broad\"")
	if !ok {
		t.Fatal("fast workflow output boundary not found")
	}
	_, selector, _ = strings.Cut(selector, "\n")
	var script strings.Builder
	script.WriteString("files=$(cat \"$PLAN_FILES\")\n")
	for line := range strings.SplitSeq(selector, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(verdict)
	return script.String()
}

// largeDiffDirs answers up to max main-module package directories under
// internal/, cmd/ and tools/ that no leg row, fuzz, goldens or language-list
// rule names, so a .go file in one selects that directory and nothing else.
func largeDiffDirs(t *testing.T, root string, max int) []string {
	t.Helper()
	var dirs []string
	for _, top := range []string{"cmd", "internal", "tools"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			switch {
			case d.Name() == "testdata",
				rel == "internal/codegen", rel == "internal/check", rel == "internal/parser",
				rel == "tools/sabotage":
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir // a nested module
			}
			if dirHoldsGoSource(path) {
				dirs = append(dirs, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(dirs)
	if len(dirs) > max {
		dirs = dirs[:max]
	}
	return dirs
}

// TestFastPlanLargeDiff runs the plan's own selector on a diff the size of a
// repo-wide import rewrite (PR 4445: 3011 files, 2980 of them .go in about
// 130 directories): go.mod and Makefile first, then at least 3000 .go files
// spread over up to a hundred package directories (46 on 2026-11-21). The broad rule must fire, and the
// packages must be each directory once, in diff order. The old selector said
// `broad=no` here (the negative control below) and forked dirname per file.
func TestFastPlanLargeDiff(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(data)
	dirs := largeDiffDirs(t, root, 100)
	if len(dirs) < 30 {
		t.Fatalf("only %d package directories found for the large diff; want at least 30", len(dirs))
	}
	perDir := (3000 + len(dirs) - 1) / len(dirs)
	lines := []string{"go.mod", "Makefile"}
	var want []string
	for _, d := range dirs {
		for i := range perDir {
			lines = append(lines, fmt.Sprintf("%s/zz_import_path_rewrite_%03d.go", d, i))
		}
		want = append(want, "./"+d)
	}
	list := strings.Join(lines, "\n") + "\n"
	if len(list) < 128*1024 {
		t.Fatalf("the large diff is %d bytes; it must exceed every pipe buffer (64 KiB) twice over", len(list))
	}
	planFiles := filepath.Join(t.TempDir(), "files.txt")
	if err := os.WriteFile(planFiles, []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(wf string) string {
		t.Helper()
		cmd := exec.Command("bash", "-euo", "pipefail", "-c", fastPlanSelector(t, wf, `printf '%s|%s|%s' "$broad" "$packages" "$codegen_touched"`))
		cmd.Env = append(scriptEnv(os.Environ()), "PLAN_FILES="+planFiles)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("plan script: %v: %s", err, out)
		}
		return selectorOutput(out)
	}
	wantOut := "yes|" + strings.Join(want, " ") + "|no"
	if got := run(wf); got != wantOut {
		t.Fatalf("a %d-file diff selected\n  %q\nwant\n  %q", len(lines), got, wantOut)
	}

	// NEGATIVE CONTROL: the broad rule as it was, echo into grep -q, must say
	// `no` on this list; if it says yes the list no longer overflows the pipe
	// and this test has stopped holding the defect.
	broadLine := regexp.MustCompile(`(?m)^(\s+)if grep -qE ('\^\(ir/\|compiler/[^']*') <<<"\$files"; then$`)
	old := broadLine.ReplaceAllString(wf, `${1}if echo "$$files" | grep -qE $2; then`)
	if old == wf {
		t.Fatal("negative control did not mutate the broad rule")
	}
	if got := run(old); strings.HasPrefix(got, "yes|") {
		t.Errorf("NEGATIVE CONTROL FAILED: the echo-into-grep broad rule selected broad=yes on a %d-byte list", len(list))
	}
}

// dirHoldsGoSource is a question about one directory, and "no" is an answer:
// it is not a scan, so it is not held to provesran's zero-match rule.
func dirHoldsGoSource(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}
