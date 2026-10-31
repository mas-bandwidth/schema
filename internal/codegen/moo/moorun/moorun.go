// Package moorun is the moo leg's test harness (docs/PORT-MOO.md, THE
// HARNESS). The moo CLI has no io library, so a moo program can open no file
// and read no stdin: inputs go IN as source (the module inputs.moo, each
// input a buffer literal) and results come OUT on stdout as lowercase hex
// lines. The interpreter is the pinned moo, $MOO or <repo>/dist/moo/moo
// (`make dist-moo`); one that does not resolve FAILS the test, never skips.
// Nothing here uses real time, a socket or the network.
package moorun

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Pin is the moo commit `make dist-moo` builds: mas-bandwidth/moo v1.3.0.
const Pin = "f60f0239f41c6c0a827ecee44a7f9539c5798068"

// HexProbe is the moo function hex(b, n?) that prints a buffer as one line.
const HexProbe = `local function hex(b: buffer, n: number?): ()
	local count = n or buffer.len(b)
	local parts = table.create(count)
	for i = 0, count - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	print(table.concat(parts))
end
`

// Interpreter and Analyzer resolve the pinned tools, or say why not.
func Interpreter() (string, string) { return resolve("MOO", "moo") }
func Analyzer() (string, string)    { return resolve("MOO_ANALYZE", "moo-analyze") }

func resolve(env, tool string) (string, string) {
	if p := os.Getenv(env); p != "" {
		if lp, err := exec.LookPath(p); err == nil {
			return lp, ""
		}
		return "", fmt.Sprintf("$%s=%s does not resolve; run make dist-moo (or unset %s)", env, p, env)
	}
	root, err := repoRoot()
	if err != nil {
		return "", err.Error() + "; run make dist-moo from the repository root"
	}
	p := filepath.Join(root, "dist", "moo", tool)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Sprintf("the pinned %s is not at %s; run make dist-moo", tool, p)
	}
	return p, ""
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above the working directory")
		}
		dir = parent
	}
}

// InputsModule renders inputs as a frozen table of buffers, by name.
func InputsModule(inputs map[string][]byte) []byte {
	names := make([]string, 0, len(inputs))
	for n := range inputs {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("--!strict\n-- inputs.moo — written by moorun.Run (docs/PORT-MOO.md, THE HARNESS)\nreturn table.freeze({\n")
	for _, n := range names {
		fmt.Fprintf(&b, "\t[%q] = buffer.fromstring(\"", n)
		for _, c := range inputs[n] {
			fmt.Fprintf(&b, "\\x%02x", c)
		}
		b.WriteString("\"),\n")
	}
	b.WriteString("})\n")
	return []byte(b.String())
}

// Run writes inputs.moo into dir, runs dir/main under the pinned moo with dir
// as the working directory, and returns stdout. A nonzero exit fails t.
func Run(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	out, err := RunErr(t, dir, main, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// RunErr is Run returning the failure instead of failing t (a negative
// control expects one).
func RunErr(t testing.TB, dir, main string, inputs map[string][]byte) ([]byte, error) {
	t.Helper()
	moo, why := Interpreter()
	if moo == "" {
		t.Fatalf("moorun: the moo interpreter does not resolve: %s", why)
	}
	if err := os.WriteFile(filepath.Join(dir, "inputs.moo"), InputsModule(inputs), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(moo, main)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("moorun: moo %s: %w\nstdout:\n%s\nstderr:\n%s", main, err, stdout.Bytes(), stderr.Bytes())
	}
	return stdout.Bytes(), nil
}

// Analyze runs moo-analyze over files in dir and returns its output: the gate
// is that the output is EMPTY (no error and no lint warning), which the
// caller asserts, because moo-analyze exits 0 on a lint warning.
func Analyze(t testing.TB, dir string, files ...string) string {
	t.Helper()
	analyze, why := Analyzer()
	if analyze == "" {
		t.Fatalf("moorun: moo-analyze does not resolve: %s", why)
	}
	cmd := exec.Command(analyze, files...)
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// HexLines decodes stdout's hex lines; a line that is not hex fails t.
func HexLines(t testing.TB, out []byte) [][]byte {
	t.Helper()
	var lines [][]byte
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return nil
	}
	for l := range strings.SplitSeq(text, "\n") {
		b, err := hex.DecodeString(l)
		if err != nil {
			t.Fatalf("moorun: output line %q is not hex: %v", l, err)
		}
		lines = append(lines, b)
	}
	return lines
}
