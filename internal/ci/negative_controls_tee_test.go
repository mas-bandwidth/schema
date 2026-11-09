package ci

// The negative-controls step piped make through tee:
//
//	make -k $targets ... 2>&1 | tee controls.log
//
// On the fleet's Ubuntu 26.04 runners tee is Rust coreutils, and it panicked
// mid-run (src/uu/tee/src/tee.rs:188, assertion `left == right` failed) in CI
// run 42554833739, aborting the pipe on the lua and base-c-discard rows with
// no control at fault. The step writes the log by redirecting make, then
// prints it, so make's own status still reaches `exit $status` and the
// NEGATIVE CONTROL grep still reads controls.log. This gate refuses a pipe of
// that make back through tee in both workflows that run the step.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const negativeControlsStepName = "every negative control in the ${{ matrix.name }} group"

func TestNegativeControlsStepDoesNotPipeMakeThroughTee(t *testing.T) {
	t.Parallel()

	piped := "make -k $targets \\\n  2>&1 | tee controls.log || status=1\n"
	if !pipesMakeThroughTee(piped) {
		t.Fatal("PLANTED PATTERN: make piped through tee left the gate green")
	}
	redirected := "make -k $targets \\\n  >controls.log 2>&1 || status=1\ncat controls.log\n"
	if pipesMakeThroughTee(redirected) {
		t.Fatal("PLANTED PATTERN: a redirect that does not use tee was refused")
	}

	root := repoRoot(t)
	for _, rel := range []string{".github/workflows/ci-full.yml", ".github/workflows/certify.yml"} {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		scripts := negativeControlsScripts(string(body))
		if len(scripts) != 1 {
			t.Errorf("%s: found %d negative-controls steps, want 1 — this gate would pass over nothing", rel, len(scripts))
			continue
		}
		script := scripts[0]
		if !strings.Contains(script, "make -k") || !strings.Contains(script, "controls.log") || !strings.Contains(script, "NEGATIVE CONTROL") || !strings.Contains(script, "exit $status") {
			t.Errorf("%s: the negative-controls step no longer runs make, writes controls.log, greps NEGATIVE CONTROL, and exits $status", rel)
		}
		if pipesMakeThroughTee(script) {
			t.Errorf("%s: the negative-controls step pipes make through tee; redirect make to controls.log and print the log", rel)
		}
	}
}

// negativeControlsScripts returns the run body of each step named
// negativeControlsStepName. A `run: |` body is the following lines indented
// deeper than the `run:` key.
func negativeControlsScripts(workflow string) []string {
	lines := strings.Split(workflow, "\n")
	var out []string
	for i, line := range lines {
		if !strings.Contains(line, negativeControlsStepName) || !strings.Contains(line, "name:") {
			continue
		}
		runAt := -1
		runIndent := 0
		for j := i + 1; j < len(lines); j++ {
			trim := strings.TrimSpace(lines[j])
			if trim == "" {
				continue
			}
			if strings.HasPrefix(trim, "run:") {
				runAt = j
				runIndent = indentOf(lines[j])
				break
			}
			if strings.HasPrefix(trim, "- ") {
				break
			}
		}
		if runAt < 0 {
			out = append(out, "")
			continue
		}
		var b strings.Builder
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[runAt]), "run:"))
		if rest != "|" && rest != ">" && rest != "|-" && rest != "|+" && rest != ">-" && rest != ">+" {
			b.WriteString(rest)
			b.WriteByte('\n')
		}
		for k := runAt + 1; k < len(lines); k++ {
			l := lines[k]
			if strings.TrimSpace(l) == "" {
				b.WriteByte('\n')
				continue
			}
			if indentOf(l) <= runIndent {
				break
			}
			b.WriteString(l)
			b.WriteByte('\n')
		}
		out = append(out, b.String())
	}
	return out
}

// pipesMakeThroughTee reports whether a shell script pipes a command through
// tee, or invokes tee by name. Comment text is ignored: prose may name the
// command this gate refuses.
func pipesMakeThroughTee(script string) bool {
	joined := strings.ReplaceAll(script, "\\\n", " ")
	for _, line := range strings.Split(joined, "\n") {
		code, _, _ := strings.Cut(line, "#")
		compact := strings.ReplaceAll(code, " ", "")
		if strings.Contains(compact, "|tee") || strings.Contains(compact, "|&tee") {
			return true
		}
		for _, tok := range strings.Fields(code) {
			base := tok
			if i := strings.LastIndex(tok, "/"); i >= 0 {
				base = tok[i+1:]
			}
			if base == "tee" {
				return true
			}
		}
	}
	return false
}

func indentOf(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' {
			break
		}
		n++
	}
	return n
}
