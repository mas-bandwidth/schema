package recipescan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// skipDirs are trees that hold no recipe of this repository: vendored code,
// build output, and fixtures that exist to be wrong.
var skipDirs = map[string]bool{
	".git": true, "third_party": true, "build": true, "node_modules": true,
	"testdata": true, "_build": true, "deps": true, "target": true,
}

// ScanTree reads every makefile and shell script under root. A makefile named
// in roots (slash paths relative to root) is one make is started on; every
// other makefile is an included fragment. It is an error for the tree to hold
// no makefile recipe or no script, and for a file under make/ to be neither a
// makefile, a script, nor a file a makefile or script reads by name.
func ScanTree(root string, roots []string) ([]Finding, Stats, error) {
	var out []Finding
	var st Stats
	isRoot := map[string]bool{}
	for _, r := range roots {
		isRoot[r] = false
	}
	// An included makefile runs under the shell of the makefile that includes
	// it: -e holds for the fragments only if every root sets it.
	errexit := len(roots) > 0
	for _, r := range roots {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r)))
		if err != nil {
			return nil, st, fmt.Errorf("recipescan: root makefile %s: %w", r, err)
		}
		if _, e := strictShell(string(src)); !e {
			errexit = false
		}
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		switch {
		case name == "Makefile" || name == "GNUmakefile" || strings.HasSuffix(name, ".mk"):
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, root := isRoot[rel]
			if root {
				isRoot[rel] = true
			}
			out = append(out, ScanMakefile(rel, string(src), root, errexit, &st)...)
		case strings.HasSuffix(name, ".sh") || strings.HasSuffix(name, ".bash"):
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out = append(out, ScanScript(rel, string(src), &st)...)
		default:
			// A script without the suffix is found by its first line,
			// anywhere in the tree (F-069: test/slowgate/proof, the
			// generated-tree verifier and the language driver wrappers live
			// under test/ and .github/, and a scan of make/ and tools/ alone
			// never read them).
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			head := make([]byte, 128)
			n, _ := f.Read(head)
			_ = f.Close()
			line, _, _ := strings.Cut(string(head[:n]), "\n")
			if ShellShebang(line) {
				src, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				out = append(out, ScanScript(rel, string(src), &st)...)
			}
		}
		return nil
	})
	if err != nil {
		return nil, st, err
	}
	for r, seen := range isRoot {
		if !seen {
			return nil, st, fmt.Errorf("recipescan: root makefile %s was not found under %s", r, root)
		}
	}
	if st.Recipes == 0 {
		return nil, st, fmt.Errorf("recipescan: read no recipe line under %s", root)
	}
	if st.Scripts == 0 {
		return nil, st, fmt.Errorf("recipescan: read no shell script under %s", root)
	}
	Sort(out)
	return out, st, nil
}

// ShellShebang reports whether a file's first line starts it under a POSIX
// shell: "#!/bin/sh", "#!/usr/bin/env bash", "#!/bin/bash -e" and the like.
func ShellShebang(line string) bool {
	if !strings.HasPrefix(line, "#!") {
		return false
	}
	f := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(f) == 0 {
		return false
	}
	prog := filepath.Base(f[0])
	if prog == "env" {
		for _, a := range f[1:] {
			if strings.HasPrefix(a, "-") || strings.Contains(a, "=") {
				continue
			}
			prog = filepath.Base(a)
			break
		}
	}
	switch prog {
	case "sh", "bash", "dash", "ksh", "zsh":
		return true
	}
	return false
}

// RootMakefiles are the makefiles of this repository that make is started
// on. Every other makefile is included by one of them, or (like
// .github/actions/dist-pins/print.mk) read beside the root with a second -f.
var RootMakefiles = []string{"Makefile"}
