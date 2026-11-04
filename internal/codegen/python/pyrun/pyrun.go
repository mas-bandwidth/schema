// Package pyrun runs generated Python under the pinned interpreters for the
// Python leg's Go tests (docs/PORT-PYTHON.md, THE HARNESS). It is a TEST
// HELPER: it writes a probe's inputs as files into the test's own directory,
// runs one interpreter process there and hands back its stdout. No network,
// no clock, no service: the interpreter reads files in dir and writes to
// stdout.
//
// The interpreters are $PYTHON and $PYTHON_FLOOR when set (make/python-
// toolchain.mk exports the pins), else the repository's dist/ builds (make
// dist-python); the analyzer is $MYPY, else the floor's mypy. A pin that does
// not resolve FAILS the test and names make dist-python; it never skips,
// because a leg that skips is a gate with no blade (issue #599).
//
// Every interpreter runs isolated and writes no bytecode (-I -B): no PYTHON*
// variable, no user site directory, and neither the script's directory nor
// the working directory on sys.path. A probe that imports a generated package
// puts its own directory on sys.path first (ImportPrelude).
package pyrun

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The pins make/python-toolchain.mk builds (docs/PORT-PYTHON.md, THE PIN).
const (
	CurrentVersion = "3.15.2"
	FloorVersion   = "3.11.18"
	MypyVersion    = "1.18.2"
)

// ImportPrelude is the first lines of a probe that imports a generated
// package from its own directory: under -I neither the script's directory
// nor the working directory is on sys.path.
const ImportPrelude = "import os, sys\nsys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))\n"

// Interpreter returns the pinned current interpreter, or fails t.
func Interpreter(t testing.TB) string {
	t.Helper()
	return pin(t, "PYTHON", "python-"+CurrentVersion, "python3.15")
}

// Floor returns the pinned floor interpreter, or fails t.
func Floor(t testing.TB) string {
	t.Helper()
	return pin(t, "PYTHON_FLOOR", "python-"+FloorVersion, "python3.11")
}

// Mypy returns the pinned analyzer, or fails t.
func Mypy(t testing.TB) string {
	t.Helper()
	return pin(t, "MYPY", "python-"+FloorVersion, "mypy")
}

func pin(t testing.TB, env, dist, exe string) string {
	t.Helper()
	if p := os.Getenv(env); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s=%s does not resolve (%v): run make dist-python (docs/PORT-PYTHON.md, THE PIN)", env, p, err)
		}
		return p
	}
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("the pinned %s does not resolve: %v; run make dist-python (docs/PORT-PYTHON.md, THE PIN)", env, err)
	}
	p := filepath.Join(root, "dist", dist, "bin", exe)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the pinned %s does not resolve: %s is absent and %s is unset; run make dist-python (docs/PORT-PYTHON.md, THE PIN)", env, p, env)
	}
	return p
}

// RepoRoot walks up from the working directory to the directory holding the
// module's go.mod.
func RepoRoot() (string, error) {
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

// Run writes each input as the file dir/<name>, runs dir/main under the
// current pin with dir as the working directory, and returns stdout. A
// nonzero exit fails t with both streams.
func Run(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	return run(t, Interpreter(t), dir, main, inputs)
}

// RunFloor is Run under the floor pin.
func RunFloor(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	return run(t, Floor(t), dir, main, inputs)
}

func run(t testing.TB, python, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	stdout, stderr, err := Try(t, python, dir, main, inputs)
	if err != nil {
		t.Fatalf("%s %s: %v\nstderr:\n%s\nstdout:\n%s", filepath.Base(python), main, err, stderr, stdout)
	}
	return []byte(stdout)
}

// Try is Run for a probe that is EXPECTED to fail, under the interpreter
// python names: a negative control sabotages what the probe reads and
// requires it to go red, so the failure is handed back with both streams.
func Try(t testing.TB, python, dir, main string, inputs map[string][]byte) (stdout, stderr string, err error) {
	t.Helper()
	for name, data := range inputs {
		if werr := os.WriteFile(filepath.Join(dir, name), data, 0o644); werr != nil {
			t.Fatal(werr)
		}
	}
	cmd := exec.Command(python, "-I", "-B", main)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

// Analyze runs the pinned mypy in strict mode, at the floor's language
// version, over the given paths relative to dir, with its cache inside dir.
// It returns the combined output and the error.
func Analyze(t testing.TB, dir string, paths ...string) (string, error) {
	t.Helper()
	args := append([]string{"--strict", "--python-version", "3.11", "--no-color-output", "--cache-dir", filepath.Join(dir, ".mypy_cache")}, paths...)
	cmd := exec.Command(Mypy(t), args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// HexLines decodes stdout as one lowercase hex string per line.
func HexLines(t testing.TB, stdout []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		b, err := hex.DecodeString(line)
		if err != nil {
			t.Fatalf("probe line %q is not hex: %v", line, err)
		}
		out = append(out, b)
	}
	return out
}

// LoadUnit parses and checks schema files, or every .schema file of a
// directory, as one unit, or fails t.
func LoadUnit(t testing.TB, paths ...string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		names := []string{p}
		if info.IsDir() {
			names = nil
			entries, err := os.ReadDir(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") {
					names = append(names, filepath.Join(p, e.Name()))
				}
			}
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			tree, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			files = append(files, check.SourceFile{
				Path: name, Name: filepath.Base(name), Base: strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data, AST: tree,
			})
		}
	}
	u, errs := check.Unit(files)
	if len(errs) > 0 {
		t.Fatalf("check: %v", errs[0])
	}
	return u
}

// WriteTree writes generated files, keyed by their path relative to dir.
func WriteTree(t testing.TB, dir string, files map[string][]byte) {
	t.Helper()
	for rel, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// LoadUnitSource is LoadUnit over one schema file written from src, as name,
// into the test's own directory.
func LoadUnitSource(t testing.TB, name, src string) *ir.Unit {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return LoadUnit(t, p)
}
