// Package lobsterrun is THE TEST HELPER every Lobster surface's tests share
// (docs/PORT-LOBSTER.md, THE HARNESS). A Lobster program reads files
// (read_file) and arguments (command_line_arguments()), so a test hands its
// inputs IN as files in the program's directory and reads its results OUT
// from stdout, bytes as lowercase hex lines ([HexProbe], [HexLines]).
package lobsterrun

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Version is the pinned Lobster release (make/lobster.mk).
const Version = "v2026.8"

// HexProbe is Lobster source for the probe a test prints bytes with:
// hex(b) prints byte string b as one line of lowercase hex — the shape
// [HexLines] reads back.
const HexProbe = `def hex(b:string):
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    print(lowercase(s))
`

// Interpreter resolves the pinned toolchain: $LOBSTER when it is set, else
// <repo root>/dist/lobster-v2026.8/bin/lobster. It returns the path and ""
// when it resolves, and "" and the reason when it does not.
func Interpreter() (string, string) {
	if p := os.Getenv("LOBSTER"); p != "" {
		if lp, err := exec.LookPath(p); err == nil {
			return lp, ""
		}
		return "", fmt.Sprintf("$LOBSTER=%s does not resolve; run make dist-lobster (or unset LOBSTER)", p)
	}
	root, err := repoRoot()
	if err != nil {
		return "", err.Error() + "; run make dist-lobster from the repository root"
	}
	p := filepath.Join(root, "dist", "lobster-"+Version, "bin", "lobster")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Sprintf("the pinned lobster is not at %s; run make dist-lobster", p)
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

// Run writes each input as a file of that name in dir, runs dir/main under
// the pinned toolchain (its default JIT through TinyCC) with dir as the
// working directory, and returns stdout. It fails the test — never skips it —
// naming make dist-lobster when the toolchain does not resolve, and fails
// with both streams when the program does not compile or exits nonzero.
func Run(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	stdout, stderr, err := run(t, dir, main, inputs, nil)
	if err != nil {
		t.Fatalf("lobsterrun: lobster %s: %v\nstdout:\n%s\nstderr:\n%s", main, err, stdout, stderr)
	}
	return stdout
}

// RunExit is [Run] for a program whose exit status is the result: it passes
// args to the program (command_line_arguments()) and returns stdout and the
// exit code instead of failing the test on a nonzero one. A run that does not
// start, or that the toolchain does not compile (exit 1 with a compile
// error), still fails the test. It is how a test observes a write-side trap:
// serialize.lobster's serialize_abort prints the rule and exits 70.
func RunExit(t testing.TB, dir, main string, inputs map[string][]byte, args ...string) ([]byte, int) {
	t.Helper()
	return RunExitFlags(t, dir, main, nil, inputs, args...)
}

// RunExitFlags is [RunExit] with compiler flags before the program, such as
// --runtime-no-asserts, which compiles every assert away.
func RunExitFlags(t testing.TB, dir, main string, flags []string, inputs map[string][]byte, args ...string) ([]byte, int) {
	t.Helper()
	stdout, stderr, err := runFlags(t, dir, main, flags, inputs, args)
	if err == nil {
		return stdout, 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || strings.Contains(string(stdout), ": error: ") {
		t.Fatalf("lobsterrun: lobster %s: %v\nstdout:\n%s\nstderr:\n%s", main, err, stdout, stderr)
	}
	return stdout, exit.ExitCode()
}

func run(t testing.TB, dir, main string, inputs map[string][]byte, args []string) ([]byte, []byte, error) {
	t.Helper()
	return runFlags(t, dir, main, nil, inputs, args)
}

func runFlags(t testing.TB, dir, main string, flags []string, inputs map[string][]byte, args []string) ([]byte, []byte, error) {
	t.Helper()
	// a test that shells out to a toolchain is the slow half (internal/slowtest):
	// make test-lobster and ci-full set SCHEMA_REQUIRE_CORPUS / SCHEMA_SLOW, so
	// the gates always run it, and a bare `go test ./...` without the pin skips
	// by name rather than failing
	if !slowtest.Enabled() {
		t.Skipf("SCHEMA_SLOW: this test shells out to the Lobster toolchain; set SCHEMA_SLOW=1 to run it (ci-full.yml and the make leg gates always do)")
	}
	lobster, why := Interpreter()
	if lobster == "" {
		t.Fatalf("lobsterrun: the Lobster toolchain does not resolve: %s", why)
	}
	names := make([]string, 0, len(inputs))
	for n := range inputs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if strings.ContainsAny(n, `/\`) || n == main {
			t.Fatalf("lobsterrun: input name %q is not a plain file name beside %s", n, main)
		}
		if err := os.WriteFile(filepath.Join(dir, n), inputs[n], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	argv := append(append([]string{}, flags...), main)
	if len(args) > 0 {
		argv = append(argv, "--")
		argv = append(argv, args...)
	}
	cmd := exec.Command(lobster, argv...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
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
			t.Fatalf("lobsterrun: stdout line %q is not lowercase hex: %v", l, err)
		}
		lines = append(lines, b)
	}
	return lines
}
