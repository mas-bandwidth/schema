package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These are the root-confinement controls (schema#1859 review item 1): an
// output path that is, or passes through, a symlink is refused, a file raced
// in between the preflight and the create is never overwritten, and a lookup
// error other than absence is reported rather than read as "free". Each one
// holds the same two promises: the path outside --root stays untouched, and
// content that was already there stays byte for byte.

// checkout is a fake schema checkout (a go.mod is all Scaffold asks of it).
func checkout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// symlink plants a link or skips where the platform refuses one (Windows
// without the privilege).
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
}

// wroteNothing: a refused scaffold leaves none of the five files behind.
func wroteNothing(t *testing.T, root string, except ...string) {
	t.Helper()
	skip := map[string]bool{}
	for _, e := range except {
		skip[e] = true
	}
	for _, f := range files {
		rel := f.path(leg{Lang: "lua"})
		if skip[rel] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a refused scaffold left %s behind (lstat: %v)", rel, err)
		}
	}
}

func TestScaffoldRefusesADanglingFinalSymlink(t *testing.T) {
	root := checkout(t)
	outside := filepath.Join(t.TempDir(), "escaped.mk")
	link := filepath.Join(root, "make", "lua.mk")
	symlink(t, outside, link)

	_, err := Scaffold(root, "lua", "", "")
	if err == nil || !strings.Contains(err.Error(), "make/lua.mk") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("a dangling make/lua.mk symlink was not refused by name: %v", err)
	}
	if _, err := os.Lstat(outside); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the symlink's target outside --root was created (lstat: %v)", err)
	}
	if got, err := os.Readlink(link); err != nil || got != outside {
		t.Errorf("the planted symlink was disturbed: %q, %v", got, err)
	}
	wroteNothing(t, root, "make/lua.mk")
}

func TestScaffoldRefusesASymlinkedParentDirectory(t *testing.T) {
	root := checkout(t)
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.mk")
	if err := os.WriteFile(keep, []byte("# not yours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, filepath.Join(root, "make"))

	_, err := Scaffold(root, "lua", "", "")
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
	wroteNothing(t, root, "make/lua.mk")
}

// TestScaffoldReportsALookupErrorThatIsNotAbsence: only "does not exist"
// means a path is free. A directory the tool cannot search is reported, and
// nothing is written, where the old os.Stat check read it as absence and
// went on writing.
func TestScaffoldReportsALookupErrorThatIsNotAbsence(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions that bind the caller")
	}
	root := checkout(t)
	mk := filepath.Join(root, "make")
	if err := os.Mkdir(mk, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(mk, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(mk, 0o755) })

	_, err := Scaffold(root, "lua", "", "")
	if err == nil || !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "make/lua.mk") {
		t.Fatalf("an unsearchable make/ was not reported as a lookup error naming make/lua.mk: %v", err)
	}
	if err := os.Chmod(mk, 0o755); err != nil {
		t.Fatal(err)
	}
	wroteNothing(t, root)
}

func TestScaffoldRefusesAFileWhereADirectoryGoes(t *testing.T) {
	root := checkout(t)
	if err := os.WriteFile(filepath.Join(root, "make"), []byte("not a dir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Scaffold(root, "lua", "", "")
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("a file named make was not refused: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "make")); string(got) != "not a dir\n" {
		t.Errorf("the file named make changed: %q", got)
	}
	wroteNothing(t, root, "make/lua.mk") // make is a file, so make/lua.mk cannot be
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
		{"dangling symlink", func(t *testing.T, at, outside string) { symlink(t, outside, at) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := checkout(t)
			outside := filepath.Join(t.TempDir(), "escaped.mk")
			at := filepath.Join(root, "make", "lua.mk")
			beforeCreate = func(rel string) {
				if rel == "make/lua.mk" {
					c.plant(t, at, outside)
				}
			}
			t.Cleanup(func() { beforeCreate = nil })

			_, err := Scaffold(root, "lua", "", "")
			if err == nil || !strings.Contains(err.Error(), "make/lua.mk") || !strings.Contains(err.Error(), "never overwrites") {
				t.Fatalf("a make/lua.mk raced in after the preflight was not refused: %v", err)
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
			wroteNothing(t, root, "make/lua.mk")
			if _, err := os.Lstat(filepath.Join(root, "compiler")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the directory this run created was left behind (lstat: %v)", err)
			}
		})
	}
}
