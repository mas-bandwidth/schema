// Package gooserun is THE TEST HELPER every Goose surface's tests share
// (docs/PORT-GOOSE.md, THE HARNESS). A Goose program reads files through the
// standard library's os module (read_file) and writes stdout, so a test
// hands its inputs IN as files in the program's directory and reads its
// results OUT from stdout, bytes as lowercase hex lines (the [HexProbe]
// function).
package gooserun

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Version is the pinned Goose release (make/goose.mk).
const Version = "v2026.2"

// Dist is the pinned release's directory under the repository root.
const Dist = "dist/goose-" + Version + "-linux-x64"

// HexProbe is Goose source for the probe a test prints bytes with: hex(b)
// prints b as one line of lowercase hex — the shape [HexLines] reads back.
const HexProbe = `fn hex(b: const u8[:]) {
    var s: u8[>..] = [];
    let digits = "0123456789abcdef";
    for c in b { s.push(digits[c >> 4]); s.push(digits[c & 15]); }
    print(s);
}
`

// Compiler resolves the pinned compiler: $GOOSE when it is set, else
// <repo root>/dist/goose-v2026.2-linux-x64/goose. It returns the path and ""
// when it resolves, and "" and the reason when it does not.
func Compiler() (string, string) {
	if p := os.Getenv("GOOSE"); p != "" {
		if lp, err := exec.LookPath(p); err == nil {
			return lp, ""
		}
		return "", fmt.Sprintf("$GOOSE=%s does not resolve; run make dist-goose (or unset GOOSE)", p)
	}
	root, err := repoRoot()
	if err != nil {
		return "", err.Error() + "; run make dist-goose from the repository root"
	}
	p := filepath.Join(root, filepath.FromSlash(Dist), "goose")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Sprintf("the pinned goose is not at %s; run make dist-goose", p)
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

// Run writes each input as the file dir/<name>, runs dir/main with
// `goose run` (the in-process TinyCC backend, -O1) and dir as the working
// directory, and returns stdout; the compiler's own progress lines go to
// stderr and are not returned. It fails the test — never skips it — naming
// make dist-goose when the compiler does not resolve, and fails with stderr
// when the compile or the program fails.
func Run(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	return run(t, dir, main, inputs, "run")
}

// RunDebug is [Run] under `goose run --debug`: signed overflow and the
// range checks of `as` abort the program (docs/PORT-GOOSE.md, THE HARNESS).
func RunDebug(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	return run(t, dir, main, inputs, "run", "--debug")
}

func run(t testing.TB, dir, main string, inputs map[string][]byte, args ...string) []byte {
	t.Helper()
	goose, why := Compiler()
	if goose == "" {
		t.Fatalf("gooserun: the Goose compiler does not resolve: %s", why)
	}
	for name, data := range inputs {
		if strings.ContainsAny(name, `/\`) || name == "" || name[0] == '.' {
			t.Fatalf("gooserun: input name %q is not a plain file name", name)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(goose, append(args, main)...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("gooserun: goose %s %s: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), main, err, stdout.Bytes(), stderr.Bytes())
	}
	return stdout.Bytes()
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
			t.Fatalf("gooserun: stdout line %q is not lowercase hex: %v", l, err)
		}
		lines = append(lines, b)
	}
	return lines
}
