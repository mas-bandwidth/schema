// Package sqrun is the Squirrel leg's test helper (docs/PORT-SQUIRREL.md, THE
// HARNESS): it runs one script under the pinned interpreter in a directory the
// test owns, hands it its inputs as a module, and returns what it printed.
//
// THE INTERPRETER EXITS 0 ON A RUNTIME ERROR. sq 3.2 prints "AN ERROR HAS
// OCCURRED" and its call stack to stderr and still exits 0 (only a compile
// error exits nonzero), so a run is a failure when it exits nonzero OR writes
// anything to stderr. A probe reports on stdout and nowhere else.
package sqrun

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

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Pin is the interpreter's path below the repository root, the one
// make dist-squirrel builds (make/squirrel.mk).
const Pin = "dist/squirrel-3.2/bin/sq"

// Interpreter resolves the pinned interpreter: $SQ when set (make exports it),
// else <repo root>/dist/squirrel-3.2/bin/sq. It returns "" and why when
// neither resolves.
func Interpreter() (string, error) {
	if sq := os.Getenv("SQ"); sq != "" {
		if path, err := exec.LookPath(sq); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("SQ=%s does not resolve", sq)
	}
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, filepath.FromSlash(Pin))
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%s is not built", path)
	}
	return path, nil
}

// RepoRoot is the nearest directory above the working directory holding the
// module's go.mod.
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(data), "module github.com/mas-bandwidth/schema/v2") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no schema go.mod above the working directory")
		}
		dir = parent
	}
}

// Require is the Squirrel leg's gate at the shared door: it skips the calling
// test unless the slow half is enabled (internal/slowtest) and then resolves
// the pinned interpreter, failing the test by name when it is not built — a
// run that is supposed to happen (SCHEMA_SLOW=1) never silently skips. Every
// test that runs sq calls it first (Run calls it itself).
func Require(t *testing.T) string {
	t.Helper()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	sq, err := Interpreter()
	if err != nil {
		t.Fatalf("the pinned Squirrel interpreter does not resolve (%v); run make dist-squirrel, or point SQ at sq64 built with -DSQUSEDOUBLE (make/squirrel.mk)", err)
	}
	return sq
}

// interpreterFor is Require's edge for a test that carries only a testing.TB:
// it gates when the concrete receiver is a *testing.T and otherwise resolves
// without gating (a helper that only ever sees the test's own *testing.T).
func interpreterFor(t testing.TB) string {
	if tt, ok := t.(*testing.T); ok {
		return Require(tt)
	}
	sq, err := Interpreter()
	if err != nil {
		t.Fatalf("the pinned Squirrel interpreter does not resolve (%v); run make dist-squirrel, or point SQ at sq64 built with -DSQUSEDOUBLE (make/squirrel.mk)", err)
	}
	return sq
}

// Run writes inputs as the module inputs.nut in dir — a script returning a
// table of name -> blob, which also defines ::hexline(b), the probe's way of
// printing bytes as one lowercase hex line — then runs dir/main under the
// pinned interpreter with dir as its working directory and returns stdout.
// It gates at Require first: under a plain go test the test skips at the
// shared door, and under SCHEMA_SLOW=1 a missing interpreter fails it by name.
func Run(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	sq := interpreterFor(t)
	if err := os.WriteFile(filepath.Join(dir, "inputs.nut"), InputsModule(inputs), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sq, main)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil || stderr.Len() > 0 {
		t.Fatalf("sq %s in %s: %v\nstderr:\n%s\nstdout:\n%s", main, dir, err, stderr.String(), stdout.String())
	}
	return stdout.Bytes()
}

// InputsModule is the inputs.nut Run writes: each input as a lowercase hex
// string decoded to a blob at load, in name order.
func InputsModule(inputs map[string][]byte) []byte {
	var b strings.Builder
	b.WriteString("// Written by internal/codegen/squirrel/sqrun. The probe's inputs, name -> blob.\n")
	b.WriteString("local nibble = function(c) { return c <= '9' ? c - '0' : c - 'a' + 10; };\n")
	b.WriteString("local unhex = function(s) {\n")
	b.WriteString("  local out = blob(s.len() / 2);\n")
	b.WriteString("  for (local i = 0; i < s.len(); i += 2) out[i / 2] = nibble(s[i]) * 16 + nibble(s[i + 1]);\n")
	b.WriteString("  return out;\n")
	b.WriteString("};\n")
	b.WriteString("::hexline <- function(b) {\n")
	b.WriteString("  local s = \"\";\n")
	b.WriteString("  for (local i = 0; i < b.len(); i++) s += format(\"%02x\", b[i] & 0xff);\n")
	b.WriteString("  print(s + \"\\n\");\n")
	b.WriteString("};\n")
	b.WriteString("return {\n")
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, "  [%q] = unhex(%q),\n", name, hex.EncodeToString(inputs[name]))
	}
	b.WriteString("};\n")
	return []byte(b.String())
}

// HexLines decodes a probe's output, one lowercase hex line per byte string.
func HexLines(out []byte) ([][]byte, error) {
	var lines [][]byte
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		b, err := hex.DecodeString(line)
		if err != nil {
			return nil, fmt.Errorf("probe line %q is not hex: %w", line, err)
		}
		lines = append(lines, b)
	}
	return lines, nil
}
