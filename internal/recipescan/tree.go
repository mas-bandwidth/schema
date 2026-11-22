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
	return scanTree(root, roots, nil)
}

// scanTree is ScanTree reading edits[rel] (slash paths relative to root) in
// place of the file on disk, so a test scans the tree with a mutation applied
// and writes nothing.
func scanTree(root string, roots []string, edits map[string]string) ([]Finding, Stats, error) {
	readFile := func(path string) ([]byte, error) {
		if rel, err := filepath.Rel(root, path); err == nil {
			if src, ok := edits[filepath.ToSlash(rel)]; ok {
				return []byte(src), nil
			}
		}
		return os.ReadFile(path)
	}
	var out []Finding
	var st Stats
	var scanned []string
	isRoot := map[string]bool{}
	for _, r := range roots {
		isRoot[r] = false
	}
	// An included makefile runs under the shell of the makefile that includes
	// it: -e holds for the fragments only if every root sets it.
	errexit := len(roots) > 0
	for _, r := range roots {
		src, err := readFile(filepath.Join(root, filepath.FromSlash(r)))
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
			src, err := readFile(path)
			if err != nil {
				return err
			}
			_, root := isRoot[rel]
			if root {
				isRoot[rel] = true
			}
			scanned = append(scanned, rel)
			out = append(out, ScanMakefile(rel, string(src), root, errexit, &st)...)
		case strings.HasPrefix(rel, ".github/workflows/") && (strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")):
			src, err := readFile(path)
			if err != nil {
				return err
			}
			st.Workflows++
			scanned = append(scanned, rel)
			out = append(out, ScanWorkflow(rel, string(src), &st)...)
		case strings.HasSuffix(name, ".sh") || strings.HasSuffix(name, ".bash"):
			src, err := readFile(path)
			if err != nil {
				return err
			}
			scanned = append(scanned, rel)
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
				src, err := readFile(path)
				if err != nil {
					return err
				}
				scanned = append(scanned, rel)
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
	// A tree with a workflow directory must yield run steps; the CI-area test
	// (TestCIAreaFailsLoudly) also refuses a repository scan that read none.
	if _, werr := os.Stat(filepath.Join(root, ".github", "workflows")); werr == nil && (st.Workflows == 0 || st.Steps == 0) {
		return nil, st, fmt.Errorf("recipescan: read %d workflows and %d run steps under %s: a scan must read both", st.Workflows, st.Steps, root)
	}
	out, err = keepUnexcused(root, scanned, out, readFile)
	if err != nil {
		return nil, st, err
	}
	Sort(out)
	return out, st, nil
}

// LOUD-OK. A finding that is deliberate and correct (cleanup that may find
// nothing to clean, a negative control that must fail, a probe that picks
// between two tools and refuses neither) is kept by a comment that says why:
//
//	# loud-ok: <why losing this status loses nothing>
//
// on the finding's own line or in the comment lines directly above it. The
// reason is required: a bare "loud-ok:" excuses nothing. A loud-ok that
// excuses no finding is itself a finding (stale-loud-ok), so the excuses
// cannot outlive the code they were written for.
const loudOK = "loud-ok:"

// RuleStaleLoudOK names a loud-ok comment with no finding under it.
const RuleStaleLoudOK = "stale-loud-ok"

func excuse(line string) (string, bool) {
	i := strings.Index(line, loudOK)
	if i < 0 {
		return "", false
	}
	h := strings.LastIndex(line[:i], "#")
	if h < 0 {
		return "", false
	}
	return strings.TrimSpace(line[i+len(loudOK):]), true
}

func keepUnexcused(root string, scanned []string, fs []Finding, readFile func(string) ([]byte, error)) ([]Finding, error) {
	lines := map[string][]string{}
	load := func(file string) ([]string, error) {
		if l, ok := lines[file]; ok {
			return l, nil
		}
		b, err := readFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			return nil, err
		}
		l := strings.Split(string(b), "\n")
		lines[file] = l
		return l, nil
	}
	used := map[string]map[int]bool{}
	var out []Finding
	for _, f := range fs {
		l, err := load(f.File)
		if err != nil {
			return nil, err
		}
		excused := false
		for n := f.Line - 1; n >= 0 && n < len(l); n-- {
			if n < f.Line-1 {
				// above the finding: comment lines ("# ..." in a script,
				// a workflow or between recipe lines; "@# ..." as a recipe
				// line of a define), and the earlier physical lines of a
				// backslash-continued line, whose excuse sits above its
				// first line
				t := strings.TrimSpace(l[n])
				if !strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "@#") && !strings.HasSuffix(t, `\`) {
					break
				}
			}
			if why, ok := excuse(l[n]); ok {
				if why != "" {
					excused = true
					if used[f.File] == nil {
						used[f.File] = map[int]bool{}
					}
					used[f.File][n+1] = true
				}
				break
			}
		}
		if !excused {
			out = append(out, f)
		}
	}
	// every loud-ok in a file the scan read must excuse something
	for _, file := range scanned {
		l, err := load(file)
		if err != nil {
			return nil, err
		}
		for n, s := range l {
			if why, ok := excuse(s); ok && !used[file][n+1] {
				msg := "this loud-ok excuses no finding: remove it"
				if why == "" {
					msg = "a loud-ok needs a reason after the colon"
				}
				out = append(out, Finding{File: file, Line: n + 1, Rule: RuleStaleLoudOK, Text: msg})
			}
		}
	}
	return out, nil
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
