package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePinPkg writes a package directory with a generate.go named exactly as
// an adopter's, holding pin lines of the form the Go adoption notes bless:
// `//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@<ver> ...`.
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

const pinPkg1 = `package p1

//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.5.0 generate --lang go --out .
//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.5.0 generate --lang cpp --out generated
// keep me exactly, trailing spaces and all   
func Thing() {}
`

const pinPkg2 = `package p2

//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.5.0 generate --lang go --out .
// an ordinary comment that is not a pin
func Other() {}
`

const pinPkg3 = `package p3

//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.5.0 generate --lang go --out .
//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.5.0
func Last() {}
`

const pinPkg4 = `package p4

//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.6.0 generate --lang go --out .
func Mixed() {}
`

// funcTestPinMovesEveryGenerateLine drives the whole one-command move: every
// pin moves, every other byte stays, each refusal exits 1 with the files
// unchanged.
func TestPinMovesEveryGenerateLine(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	root := t.TempDir()

	p1 := writePinPkg(t, root, "p1", pinPkg1)
	p2 := writePinPkg(t, root, "p2", pinPkg2)
	p3 := writePinPkg(t, root, "p3", pinPkg3)

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

	// 3. a tree whose pins already differ, without --mixed-ok
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
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
