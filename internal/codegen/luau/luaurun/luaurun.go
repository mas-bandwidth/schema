// Package luaurun is THE TEST HELPER every Luau surface's tests share
// (docs/PORT-LUAU.md, THE HARNESS). The pinned interpreter has no io library,
// so a test carries its inputs IN AS SOURCE — a module inputs.luau whose
// fields are buffers made by buffer.fromstring from string literals — and
// reads its results OUT from stdout, bytes as lowercase hex lines (the
// [HexProbe] function).
package luaurun

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

// Version is the pinned Luau release (make/luau.mk).
const Version = "0.741"

// HexProbe is Luau source for the probe a test prints bytes with: hex(b,
// n?) prints the first n bytes of buffer b (all of them when n is nil) as
// one line of lowercase hex — the shape [HexLines] reads back.
const HexProbe = `local function hex(b: buffer, n: number?): ()
	local parts = table.create(n or buffer.len(b))
	for i = 0, (n or buffer.len(b)) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	print(table.concat(parts))
end
`

// Interpreter resolves the pinned interpreter: $LUAU when it is set, else
// <repo root>/dist/luau-0.741/luau. It returns the path and "" when the file
// exists, and "" and the reason when it does not.
func Interpreter() (string, string) {
	return resolve("LUAU", "luau")
}

// Analyzer resolves luau-analyze the same way: $LUAU_ANALYZE, else the
// pinned dist directory.
func Analyzer() (string, string) {
	return resolve("LUAU_ANALYZE", "luau-analyze")
}

func resolve(env, tool string) (string, string) {
	if p := os.Getenv(env); p != "" {
		if lp, err := exec.LookPath(p); err == nil {
			return lp, ""
		}
		return "", fmt.Sprintf("$%s=%s does not resolve; run make dist-luau (or unset %s)", env, p, env)
	}
	root, err := repoRoot()
	if err != nil {
		return "", err.Error() + "; run make dist-luau from the repository root"
	}
	p := filepath.Join(root, "dist", "luau-"+Version, tool)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Sprintf("the pinned %s is not at %s; run make dist-luau", tool, p)
	}
	return p, ""
}

// repoRoot walks up from the working directory to the module root.
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

// InputsModule is the Luau source of inputs.luau: a frozen table of buffers,
// one per input, each built by buffer.fromstring from an escaped literal.
func InputsModule(inputs map[string][]byte) []byte {
	names := make([]string, 0, len(inputs))
	for n := range inputs {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("--!strict\n-- inputs.luau — written by luaurun.Run (docs/PORT-LUAU.md, THE HARNESS)\nreturn table.freeze({\n")
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

// Run writes inputs as the module inputs.luau in dir, runs dir/main under
// the pinned interpreter with dir as the working directory, and returns
// stdout. It fails the test — never skips it — naming make dist-luau when
// the interpreter does not resolve, and fails with stderr when the script
// exits nonzero.
func Run(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	luau, why := Interpreter()
	if luau == "" {
		t.Fatalf("luaurun: the Luau interpreter does not resolve: %s", why)
	}
	if err := os.WriteFile(filepath.Join(dir, "inputs.luau"), InputsModule(inputs), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(luau, main)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("luaurun: luau %s: %v\nstdout:\n%s\nstderr:\n%s", main, err, stdout.Bytes(), stderr.Bytes())
	}
	return stdout.Bytes()
}

// Analyze runs luau-analyze over files (paths relative to dir) and fails
// the test on any error it reports; lint warnings are returned, not failed.
func Analyze(t testing.TB, dir string, files ...string) string {
	t.Helper()
	analyze, why := Analyzer()
	if analyze == "" {
		t.Fatalf("luaurun: luau-analyze does not resolve: %s", why)
	}
	cmd := exec.Command(analyze, files...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("luaurun: luau-analyze reports errors: %v\n%s", err, out)
	}
	return string(out)
}

// HexLines reads stdout's lowercase hex lines back into bytes; a line that
// is not hex fails the test.
func HexLines(t testing.TB, out []byte) [][]byte {
	t.Helper()
	var lines [][]byte
	for l := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if l == "" {
			lines = append(lines, nil)
			continue
		}
		b, err := hex.DecodeString(l)
		if err != nil {
			t.Fatalf("luaurun: stdout line %q is not lowercase hex: %v", l, err)
		}
		lines = append(lines, b)
	}
	return lines
}
