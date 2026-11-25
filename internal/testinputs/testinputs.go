// Package testinputs makes go test's result cache see the repository files a
// test's child process reads.
//
// go test replays a cached PASS when the test binary, its flags, the
// environment variables the test read and the files the test opened are all
// unchanged. It learns the last two from the test process itself (the
// testing/internal/testlog record of os.Getenv, os.Open and os.Stat). A file a
// child process reads — a shell script run through sh, a makefile read by
// make, a Python helper run by python3 — is invisible to it, so a change to
// only that file replays the old verdict: a false green on a CI runner that
// keeps GOCACHE between runs.
//
// A test calls Files (or Make, or Build) on every repository file its child will read,
// before or after starting it. The file is opened through the os package, so
// the testlog records it and go test keys the cached result on it: a later run
// in which it changed runs the test again. internal/ci's
// TestEveryInterpreterChildRegistersItsInputs (it lives there because the
// registry runs it on every pull request, whatever the diff)
// refuses a _test.go file that starts sh, bash, make, python or node without
// calling Files, Make or Build in the same function (Root alone registers
// nothing), or saying in words why the child reads no repository file.
//
// go test hashes a recorded file by its size and modification time, and a
// recorded directory by its listing, and it does not record files outside the
// module root; that is the contract this package relies on.
package testinputs

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// modulePath is the module whose root relative paths are resolved against.
const modulePath = "github.com/mas-bandwidth/schema/v2"

var rootOnce struct {
	sync.Once
	dir string
	err error
}

// Root returns the repository root: the nearest directory above the working
// directory whose go.mod declares this module.
func Root(t testing.TB) string {
	t.Helper()
	rootOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			rootOnce.err = err
			return
		}
		for {
			data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err == nil && bytes.HasPrefix(data, []byte("module "+modulePath+"\n")) {
				rootOnce.dir = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				rootOnce.err = fs.ErrNotExist
				return
			}
			dir = parent
		}
	})
	if rootOnce.err != nil {
		t.Fatalf("testinputs: no go.mod for %s above the working directory: %v", modulePath, rootOnce.err)
	}
	return rootOnce.dir
}

// Files records each path as an input of the running test. A relative path is
// read from the repository root; a directory is recorded with every file
// under it. A path that does not exist fails the test: a child pointed at a
// missing file is a broken test, and recording nothing would hide it.
func Files(t testing.TB, paths ...string) {
	t.Helper()
	if len(paths) == 0 {
		t.Fatal("testinputs.Files: no path named, so nothing is recorded")
	}
	root := ""
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			if root == "" {
				root = Root(t)
			}
			p = filepath.Join(root, filepath.FromSlash(p))
		}
		if err := record(p); err != nil {
			t.Fatalf("testinputs: %v", err)
		}
	}
}

// MakeInputs are the files every make run in this repository reads, relative
// to the root: the Makefile, the makefiles it includes (make/*.mk,
// make/checks/*.mk, tools/agentsmap/map.mk, tools/roadmap/roadmap.mk), and the
// trees holding the scripts its recipes run (make/ and tools/).
var MakeInputs = []string{"Makefile", "make", "tools"}

// Make records what a make run reads: MakeInputs, then extra, the files and
// trees outside them the target's recipe reads (a leg's driver, a bench
// script). The root Makefile includes make/*.mk by wildcard, so the make/
// directory's listing is recorded too: an added or removed makefile reruns the
// test.
func Make(t testing.TB, extra ...string) {
	t.Helper()
	Files(t, append(append([]string(nil), MakeInputs...), extra...)...)
}

// Compiler are the Go sources a child compiles when it builds the schema CLI
// or runs it through go run (a leg's generated tree, build/schema-*, a
// tutorial's `schema generate`), relative to the root. The test binary links
// only the packages its own package imports, so a codegen change the child
// compiles is otherwise invisible to it.
var Compiler = []string{"go.mod", "go.sum", "cmd", "compiler", "internal"}

// Build records what a make target that builds from this repository reads:
// MakeInputs, Compiler, then extra (the leg's driver tree, its runtime).
func Build(t testing.TB, extra ...string) {
	t.Helper()
	paths := append(append([]string(nil), MakeInputs...), Compiler...)
	Files(t, append(paths, extra...)...)
}

// record opens p, and every file and directory under it, through os.
func record(p string) error {
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return touch(p)
	}
	return filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type().IsRegular() {
			return touch(path)
		}
		return nil
	})
}

// touch opens and closes one file or directory: the open is what the testlog
// records.
func touch(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	return f.Close()
}
