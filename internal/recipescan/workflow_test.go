package recipescan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// NOTHING FAILS SILENTLY (Glenn, 2026-11-22). The CI area — every workflow,
// the root Makefile and every makefile and script under make/, tools/ and
// bench/ — holds no finding of any rule. A finding that is deliberate and
// correct carries a "# loud-ok: <why>" comment (keepUnexcused), and a loud-ok
// that excuses nothing is a finding of its own. Parse only: no process,
// clock, socket or network.

func workflowFixture(t *testing.T, name string) []Finding {
	t.Helper()
	root := filepath.Join("testdata", "workflow")
	b, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	var st Stats
	fs := ScanWorkflow(name, string(b), &st)
	if st.Steps == 0 {
		t.Fatalf("%s: read no run step", name)
	}
	fs, err = keepUnexcused(root, []string{name}, fs, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// Every way red.yml loses a failure is named, on its own line.
func TestWorkflowSilentFailuresAreRed(t *testing.T) {
	fs := workflowFixture(t, "red.yml")
	want := []struct {
		line int
		rule string
	}{
		{5, RuleNoStrictShell},    // the workflow's default shell has no pipefail
		{13, RuleSwallow},         // go test ./... || true
		{15, RuleContinueOnError}, // a step that may fail
		{20, RuleToolSkip},        // a missing golangci-lint skips the lint
		{27, RuleNoStrictShell},   // shell: cmd
		{39, RuleUncheckedNeeds},  // a verdict that never reads its needs
	}
	got := map[[2]any]bool{}
	for _, f := range fs {
		got[[2]any{f.Line, f.Rule}] = true
	}
	for _, w := range want {
		if !got[[2]any{w.line, w.rule}] {
			t.Errorf("red.yml:%d: want a %s finding; got %v", w.line, w.rule, fs)
		}
	}
	if len(fs) != len(want) {
		t.Errorf("red.yml: want %d findings, got %d: %v", len(want), len(fs), fs)
	}
}

// green.yml fixes each: pipefail in the default shell, a cleanup swallow, a
// refused missing tool, an excused continue-on-error, an always() upload,
// and a verdict that reads toJSON(needs).
func TestWorkflowLoudFormsAreGreen(t *testing.T) {
	for _, f := range workflowFixture(t, "green.yml") {
		t.Errorf("%v", f)
	}
}

// A step with no shell anywhere runs under GitHub's "bash -e {0}": a pipe
// there loses its first stage. "shell: bash" is -eo pipefail.
func TestWorkflowDefaultShellPipeIsRed(t *testing.T) {
	fs := workflowFixture(t, "default-shell.yml")
	if len(fs) != 1 || fs[0].Line != 7 || fs[0].Rule != RuleNoStrictShell {
		t.Errorf("default-shell.yml: want one no-strict-shell finding on line 7, got %v", fs)
	}
}

// "( cd dir && build ) || true" swallows the build: the subshell's first
// word is the utility cd, its status the build's.
func TestSubshellSwallowIsRed(t *testing.T) {
	var st Stats
	src := "#!/bin/bash\nset -e -o pipefail\n( cd bench/go && go build . ) || true\n"
	if got := withRule(ScanScript("a.sh", src, &st), RuleSwallow); len(got) != 1 || got[0].Line != 3 {
		t.Errorf("want one swallow on line 3, got %v", got)
	}
}

// A loud-ok excuses only with a reason, and one that excuses nothing is red.
func TestLoudOKNeedsAReasonAndAFinding(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scan := func(name string) []Finding {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var st Stats
		fs, err := keepUnexcused(dir, []string{name}, ScanScript(name, string(b), &st), os.ReadFile)
		if err != nil {
			t.Fatal(err)
		}
		return fs
	}
	write("bare.sh", "#!/bin/bash\nset -e -o pipefail\n# loud-ok:\nmake test || true\n")
	if got := scan("bare.sh"); len(withRule(got, RuleSwallow)) != 1 || len(withRule(got, RuleStaleLoudOK)) != 1 {
		t.Errorf("a bare loud-ok: want the swallow kept and the loud-ok named, got %v", got)
	}
	write("why.sh", "#!/bin/bash\nset -e -o pipefail\n# loud-ok: the control must fail; the log is read below\nmake control || true\n")
	if got := scan("why.sh"); len(got) != 0 {
		t.Errorf("a loud-ok with a reason: want no finding, got %v", got)
	}
	write("stale.sh", "#!/bin/bash\nset -e -o pipefail\n# loud-ok: nothing here fails\nmake test\n")
	if got := scan("stale.sh"); len(withRule(got, RuleStaleLoudOK)) != 1 {
		t.Errorf("a loud-ok over nothing: want a stale-loud-ok finding, got %v", got)
	}
}

// inArea reports whether a file is part of the CI area this gate holds.
func inArea(file string) bool {
	return file == "Makefile" || strings.HasPrefix(file, ".github/") || strings.HasPrefix(file, "make/") ||
		strings.HasPrefix(file, "tools/") || strings.HasPrefix(file, "bench/")
}

func areaFindings(t *testing.T, edits map[string]string) []Finding {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fs, st, err := scanTree(root, RootMakefiles, edits)
	if err != nil {
		t.Fatal(err)
	}
	if st.Workflows == 0 || st.Steps == 0 || st.Recipes == 0 || st.Scripts == 0 {
		t.Fatalf("read %d workflows, %d steps, %d recipe lines, %d scripts: a scan must read all four", st.Workflows, st.Steps, st.Recipes, st.Scripts)
	}
	var out []Finding
	for _, f := range fs {
		if inArea(f.File) {
			out = append(out, f)
		}
	}
	return out
}

// The gate: the CI area holds no finding of any rule.
func TestCIAreaFailsLoudly(t *testing.T) {
	for _, f := range areaFindings(t, nil) {
		t.Errorf("%v", f)
	}
}

// Each silent failure planted in the real tree goes red.
func TestCIAreaPlantedSilenceIsRed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	fast := read(".github/workflows/ci-fast.yml")
	const strict = "\n        shell: go run ./tools/pgroup -- bash -e -o pipefail {0}\n"
	if !strings.Contains(fast, strict) {
		t.Fatalf("ci-fast.yml must run its steps under %q", strict)
	}
	gomk := read("make/go.mk")
	for name, c := range map[string]struct {
		edits map[string]string
		rule  string
	}{
		"workflow shell loses pipefail":   {map[string]string{".github/workflows/ci-fast.yml": strings.Replace(fast, strict, "\n        shell: go run ./tools/pgroup -- bash -e {0}\n", 1)}, RuleNoStrictShell},
		"workflow step swallows a test":   {map[string]string{".github/workflows/ci-fast.yml": strings.Replace(fast, "          modernize ./...\n", "          modernize ./... || true\n", 1)}, RuleSwallow},
		"workflow step continue-on-error": {map[string]string{".github/workflows/ci-fast.yml": strings.Replace(fast, "      - name: modernize\n", "      - name: modernize\n        continue-on-error: true\n", 1)}, RuleContinueOnError},
		"recipe swallows a test":          {map[string]string{"make/go.mk": strings.Replace(gomk, "go test -run Fuzz -count 1 .\n", "go test -run Fuzz -count 1 . || true\n", 1)}, RuleSwallow},
		"recipe skips a missing tool":     {map[string]string{"make/go.mk": gomk + "\nlint-planted:\n\tif command -v golangci-lint >/dev/null; then golangci-lint run; fi\n"}, RuleToolSkip},
		"recipe ignore prefix":            {map[string]string{"make/go.mk": gomk + "\ntest-planted:\n\t-go test ./...\n"}, RuleIgnorePrefix},
		"stale loud-ok":                   {map[string]string{"make/go.mk": gomk + "\n# loud-ok: planted over nothing\n"}, RuleStaleLoudOK},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for rel, src := range c.edits {
				if src == read(rel) {
					t.Fatalf("%s: the plant changed nothing in %s", name, rel)
				}
			}
			if got := withRule(areaFindings(t, c.edits), c.rule); len(got) == 0 {
				t.Errorf("%s: want a %s finding, got none", name, c.rule)
			}
		})
	}
}
