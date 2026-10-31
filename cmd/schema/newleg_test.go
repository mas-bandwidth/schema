package main

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func runFail(t *testing.T, bin string, args ...string) (string, int) {
	t.Helper()
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err == nil {
		t.Fatalf("schema %s: exited 0, want a refusal\n%s", strings.Join(args, " "), out)
	}
	status := 1
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		status = ee.ExitCode()
	}
	return string(out), status
}

func schemaTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("# schema\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"compiler", "make", filepath.Join("internal", "codegen")} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNewLegMissingLanguageIsARefusal(t *testing.T) {
	bin := buildCLI(t)
	out, status := runFail(t, bin, "new-leg")
	if status != 1 {
		t.Fatalf("exit %d, want 1\n%s", status, out)
	}
	if !strings.Contains(out, "new-leg needs the language name: schema new-leg zig") {
		t.Fatalf("missing-language refusal: %q", out)
	}
}

func TestNewLegExistingLanguageIsARefusal(t *testing.T) {
	bin := buildCLI(t)
	out, status := runFail(t, bin, "new-leg", "go")
	if status != 1 {
		t.Fatalf("exit %d, want 1\n%s", status, out)
	}
	if !strings.Contains(out, "already a live target") {
		t.Fatalf("live-target refusal: %q", out)
	}
}

func TestNewLegZigWritesSkeletonAndFixturePasses(t *testing.T) {
	bin := buildCLI(t)
	root := schemaTree(t)
	out := run(t, bin, "new-leg", "zig", "--root", root, "--verbose")
	for _, rel := range []string{
		"compiler/target_zig.go",
		"internal/codegen/zig/zig.go",
		"internal/codegen/zig/zig_test.go",
		"make/zig.mk",
		"test/conformance/zig/driver",
		"test/conformance/zig/ci.json",
		"bench/tables/zig/leg",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
		if !strings.Contains(out, "wrote "+path) {
			t.Errorf("verbose did not name %s:\n%s", path, out)
		}
	}
	driver := filepath.Join(root, "test", "conformance", "zig", "driver")
	info, err := os.Stat(driver)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("driver is not executable: %v", info.Mode())
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	repo, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"zig.go", "zig_test.go"} {
		data, err := os.ReadFile(filepath.Join(root, "internal", "codegen", "zig", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mod := "module newleg_zig_fixture\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/schema/v2 => " + repo + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if got, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v\n%s", err, got)
	}

	_, status := runFail(t, bin, "new-leg", "zig", "--root", root)
	if status != 1 {
		t.Fatalf("overwrite exit %d, want 1", status)
	}
}

func TestHelpListsNewLeg(t *testing.T) {
	bin := buildCLI(t)
	out, _ := exec.Command(bin, "help").CombinedOutput()
	if !strings.Contains(string(out), "schema new-leg") {
		t.Fatalf("help does not list new-leg:\n%s", out)
	}
}

// THE SCAFFOLDING VERB'S WRITE DISCIPLINE (nova-tools#2498 S5, schema#1859).
//
// `schema new-leg <lang>` lays down a leg that already builds and already
// tests, so a porter spends tokens on the emitter rather than on finding the
// registries. TestNewLegZigYieldsABuildingTestingSkeleton holds that sentence:
// it copies the compiler's Go sources and the make registry to a scratch tree,
// runs the verb there, and then asks the tree what a porter would — does it
// build, does vet pass, does the new package's fixture test pass, does the
// Makefile's wildcard include discover test-zig. It builds a whole tree and
// runs the Go toolchain over it, so it sits behind slowtest.Gate.
//
// The controls below are the review's confinement (schema#1859 item 1): every
// output stays inside --root and is created exclusively. Each holds the same
// two promises — the path outside --root stays untouched, and content that was
// already there stays byte for byte.

func TestNewLegZigYieldsABuildingTestingSkeleton(t *testing.T) {
	slowtest.Gate(t, "go build, go vet and make over a copied tree")
	for _, tool := range []string{"make", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s on PATH", tool)
		}
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := buildCLI(t)
	tree := t.TempDir()
	for _, f := range []string{"go.mod", "go.sum", "Makefile"} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			copyScaffoldFile(t, filepath.Join(root, f), filepath.Join(tree, f))
		}
	}
	// the compiler's Go sources, tests left behind: the question is whether
	// the skeleton builds beside them, not whether their suites pass in a copy.
	// tables/goruntime is the shared Go runtime the generated fuzz unit under
	// internal/ imports (docs/SPEC-TABLES.md §8.4), so it comes along, and so
	// does every file beside the sources, which a port's package may embed
	// (runtime/*.goose, ...).
	for _, dir := range []string{"cmd", "compiler", "internal", "ir", filepath.Join("tables", "goruntime")} {
		copyScaffoldTree(t, filepath.Join(root, dir), filepath.Join(tree, dir), func(rel string) bool {
			return !strings.HasSuffix(rel, "_test.go")
		})
	}
	copyScaffoldTree(t, filepath.Join(root, "make"), filepath.Join(tree, "make"), func(string) bool { return true })

	// the leg is not registered before the verb runs, or the discovery below
	// would not be doing the work
	if out, err := runInErr(t, tree, "make", "-n", "test-zig"); err == nil {
		t.Fatalf("make knew test-zig before the verb ran:\n%s", out)
	}

	out := run(t, bin, "new-leg", "--root", tree, "zig", "--verbose")
	for _, want := range []string{
		"compiler/target_zig.go",
		"internal/codegen/zig/zig.go",
		"internal/codegen/zig/zig_test.go",
		"make/zig.mk",
		"test/conformance/zig/driver",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("new-leg did not report writing %s:\n%s", want, out)
		}
		if _, err := os.Stat(filepath.Join(tree, filepath.FromSlash(want))); err != nil {
			t.Errorf("new-leg did not write %s: %v", want, err)
		}
	}

	// BUILDING: the whole copied tree, the new target included, and vet clean
	runIn(t, tree, "go", "build", "./...")
	runIn(t, tree, "go", "vet", "./internal/codegen/zig/", "./compiler/")
	// TESTING: the fixture test the skeleton carries passes as laid down
	if got := runIn(t, tree, "go", "test", "-count=1", "./internal/codegen/zig/"); !strings.Contains(got, "ok") {
		t.Errorf("go test of the skeleton did not pass:\n%s", got)
	}
	// the Makefile's wildcard include discovers the leg with no shared file
	// edited, and the leg's own target runs green
	runIn(t, tree, "make", "-n", "test-zig")
	if got := runIn(t, tree, "make", "test-zig"); strings.Contains(got, "FAIL") {
		t.Errorf("make test-zig did not run green:\n%s", got)
	}

	// the write discipline: never overwrite, never shadow a leg that exists —
	// the planted zig's files are now in the tree, so a second run is refused
	// by name before any file is written
	if got, err := exec.Command(bin, "new-leg", "--root", tree, "zig").CombinedOutput(); err == nil ||
		!strings.Contains(string(got), "refuses to overwrite") {
		t.Errorf("a second new-leg zig did not refuse by name (err %v):\n%s", err, got)
	}
	if got, err := exec.Command(bin, "new-leg", "--root", tree, "go").CombinedOutput(); err == nil ||
		!strings.Contains(string(got), "already") {
		t.Errorf("new-leg go (an existing leg) was not refused (err %v):\n%s", err, got)
	}
	if got, err := exec.Command(bin, "new-leg", "--root", tree, "Zig-5").CombinedOutput(); err == nil ||
		!strings.Contains(string(got), "needs a lowercase identifier") {
		t.Errorf("new-leg with a name that is no Go package name was not refused (err %v):\n%s", err, got)
	}
}

// runIn runs a command in dir and fails the test on a nonzero exit, showing
// the output the porter would have seen.
func runIn(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, err := runInErr(t, dir, name, args...)
	if err != nil {
		t.Fatalf("%s %s in %s: %v\n%s", name, strings.Join(args, " "), dir, err, out)
	}
	return out
}

func runInErr(t *testing.T, dir, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func copyScaffoldTree(t *testing.T, from, to string, keep func(rel string) bool) {
	t.Helper()
	err := filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if info.IsDir() {
			if info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !keep(filepath.ToSlash(rel)) {
			return nil
		}
		copyScaffoldFile(t, p, filepath.Join(to, rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func copyScaffoldFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

// zigLeg is the skeleton `schema new-leg zig` lays down, keyed by the
// slash-separated path it lands at under --root.
func zigLeg(t *testing.T) map[string][]byte {
	t.Helper()
	files, err := compiler.NewLeg("zig")
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// plantSymlink plants a link, or skips where the platform refuses one.
func plantSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
}

// wroteNothing: a refused scaffold leaves none of the leg's files behind,
// except the ones named (a planted link, a raced-in file).
func wroteNothing(t *testing.T, root string, files map[string][]byte, except ...string) {
	t.Helper()
	skip := map[string]bool{}
	for _, e := range except {
		skip[e] = true
	}
	for rel := range files {
		if skip[rel] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a refused scaffold left %s behind (lstat: %v)", rel, err)
		}
	}
}

func TestScaffoldWritesTheLegFilesInForm(t *testing.T) {
	files := zigLeg(t)
	root := schemaTree(t)
	if err := writeNewLeg(root, files, false); err != nil {
		t.Fatalf("new-leg zig: %v", err)
	}
	for rel, want := range files {
		at := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(at)
		if err != nil {
			t.Errorf("missing %s: %v", rel, err)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			t.Errorf("%s is not a regular file: %v", rel, info.Mode())
		}
		got, err := os.ReadFile(at)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s is not byte for byte what the scaffolder rendered", rel)
		}
		if len(got) == 0 || got[len(got)-1] != '\n' {
			t.Errorf("%s does not end in a newline", rel)
		}
		if compiler.NewLegExec(rel) != (info.Mode()&0o111 != 0) {
			t.Errorf("%s: mode %v, NewLegExec %v", rel, info.Mode(), compiler.NewLegExec(rel))
		}
	}
}

func TestScaffoldRefusesAndWritesNothing(t *testing.T) {
	files := zigLeg(t)
	root := schemaTree(t)
	at := filepath.Join(root, "make", "zig.mk")
	if err := os.WriteFile(at, []byte("# already here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeNewLeg(root, files, false)
	if err == nil || !strings.Contains(err.Error(), "make/zig.mk") || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("an existing make/zig.mk was not refused by name: %v", err)
	}
	if got, _ := os.ReadFile(at); string(got) != "# already here\n" {
		t.Errorf("the pre-existing file changed: %q", got)
	}
	wroteNothing(t, root, files, "make/zig.mk")
}

func TestScaffoldRefusesADanglingFinalSymlink(t *testing.T) {
	files := zigLeg(t)
	root := schemaTree(t)
	outside := filepath.Join(t.TempDir(), "escaped.mk")
	link := filepath.Join(root, "make", "zig.mk")
	plantSymlink(t, outside, link)

	err := writeNewLeg(root, files, false)
	if err == nil || !strings.Contains(err.Error(), "make/zig.mk") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("a dangling make/zig.mk symlink was not refused by name: %v", err)
	}
	if _, err := os.Lstat(outside); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the symlink's target outside --root was created (lstat: %v)", err)
	}
	if got, err := os.Readlink(link); err != nil || got != outside {
		t.Errorf("the planted symlink was disturbed: %q, %v", got, err)
	}
	wroteNothing(t, root, files, "make/zig.mk")
}

func TestScaffoldRefusesASymlinkedParentDirectory(t *testing.T) {
	files := zigLeg(t)
	root := schemaTree(t)
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.mk")
	if err := os.WriteFile(keep, []byte("# not yours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "make")); err != nil {
		t.Fatal(err)
	}
	plantSymlink(t, outside, filepath.Join(root, "make"))

	err := writeNewLeg(root, files, false)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("a symlinked make/ directory was not refused: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "keep.mk" {
		t.Errorf("the directory outside --root changed: %v", entries)
	}
	if got, _ := os.ReadFile(keep); string(got) != "# not yours\n" {
		t.Errorf("pre-existing content outside --root changed: %q", got)
	}
	wroteNothing(t, root, files, "make/zig.mk")
}

// TestScaffoldReportsALookupErrorThatIsNotAbsence: only "does not exist" means
// a path is free. A directory the tool cannot search is reported, and nothing
// is written, where an os.Stat check read it as absence and went on writing.
func TestScaffoldReportsALookupErrorThatIsNotAbsence(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions that bind the caller")
	}
	files := zigLeg(t)
	root := schemaTree(t)
	mk := filepath.Join(root, "make")
	if err := os.Chmod(mk, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(mk, 0o755) })

	err := writeNewLeg(root, files, false)
	if err == nil || !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "make/zig.mk") {
		t.Fatalf("an unsearchable make/ was not reported as a lookup error naming make/zig.mk: %v", err)
	}
	if err := os.Chmod(mk, 0o755); err != nil {
		t.Fatal(err)
	}
	wroteNothing(t, root, files)
}

func TestScaffoldRefusesAFileWhereADirectoryGoes(t *testing.T) {
	files := zigLeg(t)
	root := schemaTree(t)
	if err := os.Remove(filepath.Join(root, "make")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "make"), []byte("not a dir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeNewLeg(root, files, false)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("a file named make was not refused: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "make")); string(got) != "not a dir\n" {
		t.Errorf("the file named make changed: %q", got)
	}
	wroteNothing(t, root, files, "make/zig.mk") // make is a file, so make/zig.mk cannot be
}

// TestScaffoldNeverOverwritesACollisionRacedInAfterThePreflight: a file, or a
// dangling symlink out of --root, that appears between the preflight and the
// create is refused by the exclusive create; the raced-in content and the
// outside path are untouched, and the files this run had already written are
// removed.
func TestScaffoldNeverOverwritesACollisionRacedInAfterThePreflight(t *testing.T) {
	for _, c := range []struct {
		name  string
		plant func(t *testing.T, at, outside string)
	}{
		{"file", func(t *testing.T, at, _ string) {
			if err := os.WriteFile(at, []byte("# raced in\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"dangling symlink", func(t *testing.T, at, outside string) { plantSymlink(t, outside, at) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := zigLeg(t)
			root := schemaTree(t)
			outside := filepath.Join(t.TempDir(), "escaped.mk")
			at := filepath.Join(root, "make", "zig.mk")
			newLegBeforeCreate = func(rel string) {
				if rel == "make/zig.mk" {
					c.plant(t, at, outside)
				}
			}
			t.Cleanup(func() { newLegBeforeCreate = nil })

			err := writeNewLeg(root, files, false)
			if err == nil || !strings.Contains(err.Error(), "make/zig.mk") || !strings.Contains(err.Error(), "never overwrites") {
				t.Fatalf("a make/zig.mk raced in after the preflight was not refused: %v", err)
			}
			if _, err := os.Lstat(outside); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the path outside --root was created (lstat: %v)", err)
			}
			if c.name == "file" {
				if got, _ := os.ReadFile(at); string(got) != "# raced in\n" {
					t.Errorf("the raced-in file was overwritten: %q", got)
				}
			} else if got, err := os.Readlink(at); err != nil || got != outside {
				t.Errorf("the raced-in symlink was disturbed: %q, %v", got, err)
			}
			wroteNothing(t, root, files, "make/zig.mk")
			if _, err := os.Lstat(filepath.Join(root, "bench")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the directory this run created was left behind (lstat: %v)", err)
			}
		})
	}
}
