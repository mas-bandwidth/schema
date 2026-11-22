package main

import (
	"bytes"
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
// written as a column-0 raw-string line would be run as a real command. The
// module path is split at its major suffix so the module-path move program
// (tools/modpath) leaves these fixtures alone: they name a major on purpose.
const (
	pinDirectiveBase = "//go:generate go run github.com/mas-bandwidth/schema"
	pinDirective     = pinDirectiveBase + "/v" + "2/cmd/schema@"
	pinDirectiveV1   = pinDirectiveBase + "/cmd/schema@"
	pinDirectiveV3   = pinDirectiveBase + "/v" + "3/cmd/schema@"
)

// TestPinWritesAPathGoCanResolve: the pin line's module path always carries
// the major of the version it names, as Go's semantic import versioning
// requires, and a tag Go cannot resolve under any path is refused by name
// with every file unchanged.
func TestPinWritesAPathGoCanResolve(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	root := t.TempDir()
	gen := func(dir string) string { return mustRead(t, filepath.Join(dir, "generate.go")) }

	// v2 to a v3 commit: the path moves to /v3 with the version, tails kept.
	// No v3 TAG resolves until the first one cut after the /v3 module move
	// (internal/modulemove), but a v3 pseudo-version is a commit and is never
	// below that boundary.
	p := writePinPkg(t, root, "major", pinPkg1)
	run(t, bin, "pin", "v3.0.4-0.20261121000000-abcdef123456", p)
	want := strings.ReplaceAll(pinPkg1, pinDirective+"v2.5.0", pinDirectiveV3+"v3.0.4-0.20261121000000-abcdef123456")
	if got := gen(p); got != want {
		t.Errorf("v2 to v3 pseudo-version pin:\n%q\nwant\n%q", got, want)
	}

	// back to v1: the path loses its suffix
	run(t, bin, "pin", "v1.16.0", p)
	if got := gen(p); !strings.Contains(got, pinDirectiveV1+"v1.16.0 generate --lang cpp --out generated") || strings.Contains(got, "/v3/") {
		t.Errorf("v1 pin: %q", got)
	}

	// and to v2 again from a suffix-less line
	run(t, bin, "pin", "v2.5.0", p)
	if got := gen(p); got != pinPkg1 {
		t.Errorf("v1 to v2 pin did not restore the original:\n%q", got)
	}

	// Every v3 tag cut before the /v3 module move carries a go.mod naming /v2:
	// no path resolves it, under any spelling Go reads as the same tag. That
	// is v3.0.0 to v3.0.4 now; with no boundary set, every v3
	// tag. And no version with build metadata resolves at all (schema has a
	// go.mod at every major). Each refusal leaves the adopter's file byte for
	// byte as it was, with its mode, and leaves no other file behind.
	info, err := os.Stat(filepath.Join(p, "generate.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"v3.0.0", "v3.0.1", "v3.0.2", "v3.0.3", "v3.0.4", "v3.1.0", "v3.1.0-rc.1", "v3.0.0+incompatible", "v3.0.2+incompatible", "v3.0.2+build.7", "v3.0.3+incompatible", "v2.9.0+incompatible"} {
		msg, status := runFail(t, bin, "pin", v, p)
		if status != 1 {
			t.Fatalf("pin %s exit %d, want 1\n%s", v, status, msg)
		}
		if !strings.Contains(msg, v) || !strings.Contains(msg, "cannot resolve") {
			t.Errorf("pin %s refusal does not name the version and the reason: %q", v, msg)
		}
		got, err := os.ReadFile(filepath.Join(p, "generate.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, []byte(pinPkg1)) {
			t.Errorf("pin %s refusal changed the file:\n%q", v, got)
		}
		after, err := os.Stat(filepath.Join(p, "generate.go"))
		if err != nil {
			t.Fatal(err)
		}
		if after.Mode() != info.Mode() || !after.ModTime().Equal(info.ModTime()) {
			t.Errorf("pin %s refusal rewrote the file (mode %v, mtime %v; was %v, %v)", v, after.Mode(), after.ModTime(), info.Mode(), info.ModTime())
		}
		if entries, err := os.ReadDir(p); err != nil || len(entries) != 1 {
			t.Errorf("pin %s refusal left the package directory holding %v (err %v); want generate.go alone", v, entries, err)
		}
	}

	// a tree that already disagrees on the major is mixed, even at one version
	q := writePinPkg(t, root, "split", "package q\n\n"+pinDirective+"v2.5.0\n"+pinDirectiveV3+"v2.5.0\n")
	msg, status := runFail(t, bin, "pin", "v2.6.0", q)
	if status != 1 || !strings.Contains(msg, "differ") {
		t.Errorf("a tree mixed on the major path was not refused as mixed (exit %d): %q", status, msg)
	}
}

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
	run(t, bin, "pin", "v2.9.9", p6)
	if got := mustRead(t, filepath.Join(p6, "generate.go")); got != strings.ReplaceAll(pinPkgCRLF, "v2.5.0", "v2.9.9") {
		t.Errorf("CRLF generate.go changed unexpectedly after pin:\n%q\nexpect\n%q", got, strings.ReplaceAll(pinPkgCRLF, "v2.5.0", "v2.9.9"))
	}
	if !strings.HasSuffix(mustRead(t, filepath.Join(p6, "generate.go")), "func CRLF() {}\r\n") {
		t.Errorf("CRLF file lost its CRLF endings")
	}

	out := run(t, bin, "pin", "v2.9.9", p1, p2, p3)
	want := "v2.9.9"
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
	run(t, bin, "pin", "v2.0.0-20250101000000-abcdef123456", p3)
	if got := mustRead(t, filepath.Join(p3, "generate.go")); !strings.Contains(got, "@v2.0.0-20250101000000-abcdef123456 generate") {
		t.Errorf("pseudo-version target not honored: %q", got)
	}

	// rewriting an existing file keeps the mode it had; 0644 is only for a
	// file the command creates
	if err := os.Chmod(filepath.Join(p3, "generate.go"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, bin, "pin", "v2.7.0", p3)
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
	msg, status = runFail(t, bin, "pin", "v2.9.9", filepath.Join(root, "nope"))
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
	msg, status = runFail(t, bin, "pin", "v2.9.9", unpinned)
	if status != 1 {
		t.Fatalf("no-pin refusal exit %d, want 1\n%s", status, msg)
	}
	if !strings.Contains(msg, "unpinned") || !strings.Contains(msg, "no go:generate pin") {
		t.Fatalf("no-pin refusal does not name the directory: %q", msg)
	}
	msg, status = runFail(t, bin, "pin", "v2.9.9", empty)
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
	msg, status = runFail(t, bin, "pin", "v2.9.9", p1, p4)
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
	run(t, bin, "pin", "--mixed-ok", "v2.8.8", p1, p4)
	for f, data := range before {
		got := mustRead(t, f)
		if strings.Contains(got, "v2.9.9") || strings.Contains(got, "v2.8.8") {
			continue // moved to a pin we set; fine
		}
		if got != data {
			t.Errorf("--mixed-ok changed %s unexpectedly", f)
		}
	}
	if got := mustRead(t, filepath.Join(p4, "generate.go")); !strings.Contains(got, "v2.8.8 generate --lang go --out .") {
		t.Errorf("--mixed-ok did not move p4's pin: %q", got)
	}
	if got := mustRead(t, filepath.Join(p1, "generate.go")); !strings.Contains(got, "v2.8.8 generate --lang go --out .") {
		t.Errorf("--mixed-ok did not move p1's pin: %q", got)
	}

	// the flag may sit after the directories, as the usage line invites
	run(t, bin, "pin", "v2.7.7", p1, p2, p4, "--mixed-ok")
	for _, dir := range []string{p1, p2, p4} {
		if got := mustRead(t, filepath.Join(dir, "generate.go")); !strings.Contains(got, "@v2.7.7 ") {
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

// TestPinTargetBoundary drives the pin decision as a pure function, the
// boundary and the build's own module path passed in (the seam the real
// command fills from internal/modulemove.FirstV3ModuleTag and pinMainModule),
// so it runs in every build with no binary, no network and no clock.
func TestPinTargetBoundary(t *testing.T) {
	t.Parallel()
	base := "github.com/mas-bandwidth/schema"
	v2mod, v3mod := base+"/v"+"2", base+"/v"+"3"

	// No boundary (today: go.mod still names /v2): every v3 tag is refused,
	// v3.0.3 and v3.0.4 included, and the refusal says why and where to go.
	for _, v := range []string{"v3.0.0", "v3.0.2", "v3.0.3", "v3.0.4", "v3.5.0", "v3.1.0-rc.1"} {
		path, err := pinTarget(v, "", v2mod)
		if err == nil {
			t.Errorf("pinTarget(%s) with no boundary = %q; want a refusal", v, path)
			continue
		}
		msg := err.Error()
		for _, want := range []string{
			v,
			"cannot resolve",
			"before the module path moved to /v" + "3",
			"https://github.com/mas-bandwidth/schema/releases",
			"go install " + v2mod + "/cmd/schema@main",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("pinTarget(%s) refusal does not say %q: %s", v, want, msg)
			}
		}
	}

	// Once #4445 sets the boundary: tags from it on are accepted under /v3,
	// tags below it are still refused.
	for v, want := range map[string]string{
		"v3.1.0": v3mod,
		"v3.1.1": v3mod,
		"v3.2.0": v3mod,
		"v3.0.4": "",
		"v3.0.9": "",
	} {
		path, err := pinTarget(v, "v3.1.0", v3mod)
		if want == "" {
			if err == nil {
				t.Errorf("pinTarget(%s, boundary v3.1.0) = %q; want a refusal", v, path)
			}
			continue
		}
		if err != nil || path != want {
			t.Errorf("pinTarget(%s, boundary v3.1.0) = %q, %v; want %q", v, path, err, want)
		}
	}

	// The window after #4445 lands and before the first /v3 tag: /v3
	// commits are accepted, boundary set or not, and never count as below it.
	for _, boundary := range []string{"", "v3.1.0"} {
		for v, want := range map[string]string{
			"v3.0.4-0.20261121000000-abcdef123456":      v3mod,
			"v3.0.0-20261121000000-abcdef123456":        v3mod,
			"v3.1.0-rc.1.0.20261121000000-abcdef123456": v3mod,
		} {
			path, err := pinTarget(v, boundary, v3mod)
			if err != nil || path != want {
				t.Errorf("pinTarget(%s, boundary %q) = %q, %v; want %q", v, boundary, path, err, want)
			}
		}
	}
	// other majors are untouched by the boundary
	for v, want := range map[string]string{"v2.9.0": v2mod, "v1.16.0": base} {
		if path, err := pinTarget(v, "", v2mod); err != nil || path != want {
			t.Errorf("pinTarget(%s) = %q, %v; want %q", v, path, err, want)
		}
	}
	// and the grammar refusals stand
	for _, v := range []string{"definitely-not-a-version", "v3.0.4+incompatible", "main"} {
		if path, err := pinTarget(v, "v3.1.0", v3mod); err == nil {
			t.Errorf("pinTarget(%s) = %q; want a refusal", v, path)
		}
	}
}

// TestPinReadOnlyDirectoryChangesNothing is the reviewer's two-package case
// through the built command: a pin from v2.5.0 to v2.6.0 across two packages,
// the second directory read-only, exits 1 with BOTH files as they were (the
// first used to be left at v2.6.0), leaves no temporary behind, and the same
// command succeeds once the directory is writable again (it used to refuse
// the mixed tree the first attempt had made).
func TestPinReadOnlyDirectoryChangesNothing(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only directory; TestPinPublishAllOrNothing covers the same rule through its seam")
	}
	bin := buildCLI(t)
	root := t.TempDir()
	p1 := writePinPkg(t, root, "p1", pinPkg1)
	p2 := writePinPkg(t, root, "p2", pinPkg2)
	if err := os.Chmod(p2, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p2, 0o755) })

	msg, status := runFail(t, bin, "pin", "v2.6.0", p1, p2)
	if status != 1 {
		t.Fatalf("pin into a read-only directory exit %d, want 1\n%s", status, msg)
	}
	if !strings.Contains(msg, "p2") || !strings.Contains(msg, "nothing was changed") {
		t.Errorf("the refusal does not name the directory and say nothing changed: %q", msg)
	}
	if got := mustRead(t, filepath.Join(p1, "generate.go")); got != pinPkg1 {
		t.Errorf("a failed two-package pin changed the first package:\n%q", got)
	}
	if got := mustRead(t, filepath.Join(p2, "generate.go")); got != pinPkg2 {
		t.Errorf("a failed two-package pin changed the second package:\n%q", got)
	}
	for _, dir := range []string{p1, p2} {
		if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
			t.Errorf("a failed pin left %s holding %v (err %v); want generate.go alone", dir, entries, err)
		}
	}

	if err := os.Chmod(p2, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, bin, "pin", "v2.6.0", p1, p2)
	if got := mustRead(t, filepath.Join(p1, "generate.go")); got != strings.ReplaceAll(pinPkg1, "v2.5.0", "v2.6.0") {
		t.Errorf("the retry did not move the first package:\n%q", got)
	}
	if got := mustRead(t, filepath.Join(p2, "generate.go")); got != strings.ReplaceAll(pinPkg2, "v2.5.0", "v2.6.0") {
		t.Errorf("the retry did not move the second package:\n%q", got)
	}
}
