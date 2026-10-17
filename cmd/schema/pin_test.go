package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePinPkg writes a package directory with a generate.go named exactly as
// an adopter's, holding pin lines of the form the Go adoption notes bless.
func writePinPkg(t *testing.T, root, pkg, content string) string {
	t.Helper()
	dir := filepath.Join(root, pkg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "generate.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// pinDirective is assembled here so no line of THIS source file starts with
// the directive: `go generate ./...` scans test files too, and a fixture
// written as a column-0 raw-string line would be run as a real command.
const pinDirective = "//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@"

var (
	pinPkg1 = "package p1\n\n" +
		pinDirective + "v2.5.0 generate --lang go --out .\n" +
		pinDirective + "v2.5.0 generate --lang cpp --out generated\n" +
		"// keep me exactly, trailing spaces and all   \n" +
		"func Thing() {}\n"

	pinPkg2 = "package p2\n\n" +
		pinDirective + "v2.5.0 generate --lang go --out .\n" +
		"// an ordinary comment that is not a pin\n" +
		"func Other() {}\n"

	pinPkg3 = "package p3\n\n" +
		pinDirective + "v2.5.0 generate --lang go --out .\n" +
		pinDirective + "v2.5.0\n" +
		"func Last() {}\n"

	pinPkg4 = "package p4\n\n" +
		pinDirective + "v2.6.0 generate --lang go --out .\n" +
		"func Mixed() {}\n"

	pinPkgNoPin = "package p5\n\n" +
		"// an ordinary comment that is not a pin\n" +
		"func NoPin() {}\n"

	// CRLF line endings: the version must stop at the CR, not absorb it,
	// so a uniform tree is not refused as mixed and the trailing CR survives.
	pinPkgCRLF = "package p6\r\n\r\n" +
		pinDirective + "v2.5.0 generate --lang go --out .\r\n" +
		pinDirective + "v2.5.0\r\n" +
		"func CRLF() {}\r\n"
)

// TestPinMovesEveryGenerateLine drives the whole one-command move: every pin
// moves, every other byte stays, each refusal exits 1 with the files
// unchanged.
func TestPinMovesEveryGenerateLine(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	root := t.TempDir()

	p1 := writePinPkg(t, root, "p1", pinPkg1)
	p2 := writePinPkg(t, root, "p2", pinPkg2)
	p3 := writePinPkg(t, root, "p3", pinPkg3)

	// a CRLF generate.go: the bare pin line keeps its CR and the tree is
	// not refused as mixed
	p6 := writePinPkg(t, root, "p6", pinPkgCRLF)
	run(t, bin, "pin", "v9.9.9", p6)
	if got := mustRead(t, filepath.Join(p6, "generate.go")); got != strings.ReplaceAll(pinPkgCRLF, "v2.5.0", "v9.9.9") {
		t.Errorf("CRLF generate.go changed unexpectedly after pin:\n%q\nexpect\n%q", got, strings.ReplaceAll(pinPkgCRLF, "v2.5.0", "v9.9.9"))
	}
	if !strings.HasSuffix(mustRead(t, filepath.Join(p6, "generate.go")), "func CRLF() {}\r\n") {
		t.Errorf("CRLF file lost its CRLF endings")
	}

	out := run(t, bin, "pin", "v9.9.9", p1, p2, p3)
	want := "v9.9.9"
	for _, f := range []struct{ path, orig, exp string }{
		{filepath.Join(p1, "generate.go"), pinPkg1, strings.ReplaceAll(pinPkg1, "v2.5.0", want)},
		{filepath.Join(p2, "generate.go"), pinPkg2, strings.ReplaceAll(pinPkg2, "v2.5.0", want)},
		{filepath.Join(p3, "generate.go"), pinPkg3, strings.ReplaceAll(pinPkg3, "v2.5.0", want)},
	} {
		got, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != f.exp {
			t.Errorf("%s: unexpected content after pin\n--- got ---\n%s\n--- want ---\n%s", f.path, got, f.exp)
		}
		if string(got) == f.orig {
			t.Errorf("%s: pin did not move (file byte-identical)", f.path)
		}
		// every other token of the go:generate line is untouched
		if !strings.Contains(string(got), want+" generate --lang go --out .") {
			t.Errorf("%s: a generate line lost its tail: %q", f.path, got)
		}
		if f.path == filepath.Join(p1, "generate.go") && !strings.Contains(string(got), "keep me exactly, trailing spaces and all   ") {
			t.Errorf("%s: a non-pin line changed", f.path)
		}
		// one line per file changed
		if !strings.Contains(out, f.path) {
			t.Errorf("output does not name changed file %s:\n%s", f.path, out)
		}
	}

	// a Go pseudo-version is a valid target too
	run(t, bin, "pin", "v0.0.0-20250101000000-abcdef123456", p3)
	if got := mustRead(t, filepath.Join(p3, "generate.go")); !strings.Contains(got, "@v0.0.0-20250101000000-abcdef123456 generate") {
		t.Errorf("pseudo-version target not honored: %q", got)
	}

	// rewriting an existing file keeps the mode it had; 0644 is only for a
	// file the command creates
	if err := os.Chmod(filepath.Join(p3, "generate.go"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, bin, "pin", "v7.0.0", p3)
	info, err := os.Stat(filepath.Join(p3, "generate.go"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("pinned generate.go mode = %04o, want 0600", got)
	}

	// REFUSALS: each exits 1 and leaves every file byte-identical.
	assertTree := func() map[string]string {
		m := map[string]string{}
		for _, f := range []string{filepath.Join(p1, "generate.go"), filepath.Join(p2, "generate.go"), filepath.Join(p3, "generate.go")} {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			m[f] = string(data)
		}
		return m
	}

	// 1. a version that is neither a pseudo-version nor a semver tag
	before := assertTree()
	msg, status := runFail(t, bin, "pin", "definitely-not-a-version", p1)
	if status != 1 {
		t.Fatalf("bad-version refusal exit %d, want 1\n%s", status, msg)
	}
	if !strings.Contains(msg, "not a Go pseudo-version or semver tag") {
		t.Fatalf("bad-version refusal does not name the version: %q", msg)
	}
	for f, data := range before {
		if got := mustRead(t, f); got != data {
			t.Errorf("bad-version refusal changed %s", f)
		}
	}

	// 2. a directory that does not exist
	before = assertTree()
	msg, status = runFail(t, bin, "pin", "v9.9.9", filepath.Join(root, "nope"))
	if status != 1 {
		t.Fatalf("missing-dir refusal exit %d, want 1\n%s", status, msg)
	}
	if !strings.Contains(msg, "nope") {
		t.Fatalf("missing-dir refusal does not name the directory: %q", msg)
	}
	for f, data := range before {
		if got := mustRead(t, f); got != data {
			t.Errorf("missing-dir refusal changed %s", f)
		}
	}

	// 3. a directory with no pin line, and an empty one: refused by name,
	// never silently accepted
	unpinned := writePinPkg(t, root, "unpinned", pinPkgNoPin)
	empty := filepath.Join(root, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	before = assertTree()
	before[filepath.Join(unpinned, "generate.go")] = pinPkgNoPin
	msg, status = runFail(t, bin, "pin", "v9.9.9", unpinned)
	if status != 1 {
		t.Fatalf("no-pin refusal exit %d, want 1\n%s", status, msg)
	}
	if !strings.Contains(msg, "unpinned") || !strings.Contains(msg, "no go:generate pin") {
		t.Fatalf("no-pin refusal does not name the directory: %q", msg)
	}
	msg, status = runFail(t, bin, "pin", "v9.9.9", empty)
	if status != 1 {
		t.Fatalf("empty-dir refusal exit %d, want 1\n%s", status, msg)
	}
	if !strings.Contains(msg, "empty") || !strings.Contains(msg, "no go:generate pin") {
		t.Fatalf("empty-dir refusal does not name the directory: %q", msg)
	}
	for f, data := range before {
		if got := mustRead(t, f); got != data {
			t.Errorf("no-pin refusal changed %s", f)
		}
	}

	// 4. a tree whose pins already differ, without --mixed-ok
	p4 := writePinPkg(t, root, "p4", pinPkg4)
	before = assertTree()
	before[filepath.Join(p4, "generate.go")] = pinPkg4
	msg, status = runFail(t, bin, "pin", "v9.9.9", p1, p4)
	if status != 1 {
		t.Fatalf("mixed-pins refusal exit %d, want 1\n%s", status, msg)
	}
	if !strings.Contains(msg, "differ") && !strings.Contains(msg, "--mixed-ok") {
		t.Fatalf("mixed-pins refusal does not name the remedy: %q", msg)
	}
	for f, data := range before {
		if got := mustRead(t, f); got != data {
			t.Errorf("mixed-pins refusal changed %s", f)
		}
	}

	// --mixed-ok accepts the same tree and moves both pins
	run(t, bin, "pin", "--mixed-ok", "v8.8.8", p1, p4)
	for f, data := range before {
		got := mustRead(t, f)
		if strings.Contains(got, "v9.9.9") || strings.Contains(got, "v8.8.8") {
			continue // moved to a pin we set; fine
		}
		if got != data {
			t.Errorf("--mixed-ok changed %s unexpectedly", f)
		}
	}
	if got := mustRead(t, filepath.Join(p4, "generate.go")); !strings.Contains(got, "v8.8.8 generate --lang go --out .") {
		t.Errorf("--mixed-ok did not move p4's pin: %q", got)
	}
	if got := mustRead(t, filepath.Join(p1, "generate.go")); !strings.Contains(got, "v8.8.8 generate --lang go --out .") {
		t.Errorf("--mixed-ok did not move p1's pin: %q", got)
	}

	// the flag may sit after the directories, as the usage line invites
	run(t, bin, "pin", "v7.7.7", p1, p2, p4, "--mixed-ok")
	for _, dir := range []string{p1, p2, p4} {
		if got := mustRead(t, filepath.Join(dir, "generate.go")); !strings.Contains(got, "@v7.7.7 ") {
			t.Errorf("trailing --mixed-ok did not move %s's pin: %q", dir, got)
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
