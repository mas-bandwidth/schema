package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fastSelectorScript returns ci-fast.yml plan's selector, from its
// `# THE BROAD RULE.` marker to `# THE PACKAGES.`, with a printf of the
// verdict (the same cut TestFastPlanSelectsTableFixtures runs).
func fastSelectorScript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "          # THE BROAD RULE.")
	if !ok {
		t.Fatal("ci-fast.yml plan broad selector not found")
	}
	selector, _, ok := strings.Cut(tail, "          # THE PACKAGES.")
	if !ok {
		t.Fatal("ci-fast.yml plan package boundary not found")
	}
	_, selector, _ = strings.Cut(selector, "\n")
	var script strings.Builder
	for line := range strings.SplitSeq(selector, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(`printf '%s|%s' "$broad" "$legs"`)
	return script.String()
}

// legpathsFailsEnv returns the environment of a selector run in which
// `go run ./tools/legpaths` fails: a `go` first on PATH exits 3 for that one
// command, naming itself on stderr, and hands every other go command to the
// real one.
func legpathsFailsEnv(t *testing.T) []string {
	t.Helper()
	real, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(real, '\'') {
		t.Fatalf("go is at %q, which this stub cannot quote", real)
	}
	dir := t.TempDir()
	stub := "#!/bin/sh\n" +
		"case \" $* \" in\n" +
		"*\" ./tools/legpaths \"*) echo \"planted: tools/legpaths failed\" >&2; exit 3 ;;\n" +
		"esac\n" +
		"exec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	env := scriptEnv(os.Environ())
	found := false
	for i, kv := range env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			env[i] = "PATH=" + dir + string(os.PathListSeparator) + path
			found = true
		}
	}
	if !found {
		env = append(env, "PATH="+dir)
	}
	return env
}

// A FAILED tools/legpaths FAILS THE SELECTOR STEP, in ci-fast.yml plan and in
// ci-full.yml pr-select. Both assign the derivation, so under the jobs' shell
// (tools/pgroup -- bash -e -o pipefail) a non-zero exit stops the step; a
// derivation swallowed by `|| true`, a `local`, an `export` or a `for` over
// the substitution would select no leg and the run would pass green over a
// change it never checked. This runs each workflow's own script with
// tools/legpaths made to fail and requires a non-zero exit that reached the
// planted failure. Its negative control plants `|| true` after the call and
// requires the same run to exit 0, so the check tells a swallowed failure
// from a loud one.
func TestALegpathsFailureFailsTheSelector(t *testing.T) {
	t.Parallel()
	const call = `derived_legs=$(go run ./tools/legpaths -changed - <<<"$files")`
	for _, sel := range []struct{ name, script string }{
		{"ci-fast.yml plan", fastSelectorScript(t)},
		{"ci-full.yml pr-select", prSelectScript(t)},
	} {
		t.Run(sel.name, func(t *testing.T) {
			t.Parallel()
			env := legpathsFailsEnv(t)
			run := func(script string) ([]byte, error) {
				cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", script)
				cmd.Env = append(env, "files=README.md")
				cmd.Dir = repoRoot(t)
				return cmd.CombinedOutput()
			}
			out, err := run(sel.script)
			if err == nil {
				t.Fatalf("%s exited 0 although tools/legpaths failed: a failed derivation selects no leg and the run passes\n%s", sel.name, out)
			}
			if !strings.Contains(string(out), "planted: tools/legpaths failed") {
				t.Fatalf("%s failed (%v), but never at tools/legpaths: the stub did not run\n%s", sel.name, err, out)
			}
			if !strings.Contains(sel.script, call) {
				t.Fatalf("%s does not derive its legs with %s", sel.name, call)
			}
			planted := strings.Replace(sel.script, call, call+" || true", 1)
			if out, err := run(planted); err != nil {
				t.Errorf("NEGATIVE CONTROL FAILED: with `|| true` planted, %s still exited non-zero (%v), so this test cannot tell a swallowed failure from a loud one\n%s", sel.name, err, out)
			}
		})
	}
}
