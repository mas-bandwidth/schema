package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/atomicfile"
)

// GENERATED OUTPUT: AFTER ONE RUN THE DIRECTORY HOLDS THIS RUN'S FILES, ONE
// PER OUTPUT, UNDER THIS RUN'S SPELLING, on a volume that keeps two spellings
// of a name apart and on one that takes them for one entry alike (review
// finding F-014).
//
// WHICH ENTRY A NAME IS, IS THE FILE SYSTEM'S ANSWER, never a comparison of
// spellings. A volume that folds names answers a write to player.go with the
// entry it already holds as Player.go and keeps the old spelling; a cleanup
// that then compared names found "Player.go" in no list of what this run
// wrote, read this run's own header in it, and deleted it. So an entry that
// answers to an output's name under another spelling takes this run's
// spelling, and the stale cleanup asks the file system whether an entry is
// one this run writes before it looks at anything else.

// outLook is what a run reads of its output directory, as it stands before
// the run changes anything. The real one is the run's os.Root, so even a
// third-party generator's output and existing symlinks stay inside the
// requested tree; a test gives one that folds names, so the rules of a
// case-insensitive volume are held on any machine.
type outLook interface {
	// names lists a directory's entries as the file system spells them.
	names(dir string) ([]string, error)
	// exists reports whether a name resolves to an entry at all.
	exists(name string) bool
	// isDir reports whether a name is a directory.
	isDir(name string) bool
	// same reports whether two names both resolve, to one directory entry.
	same(a, b string) bool
	readFile(name string) ([]byte, error)
}

// outChanges is what a run changes in its output directory: the one
// publisher's view of it (atomicfile.View), which stages each change and
// publishes them all, or none, when the run commits.
type outChanges interface {
	Write(path string, data []byte, mode fs.FileMode)
	Delete(path string)
	Respell(path, to string)
}

// rootLook is an os.Root as an outLook. Names are slash-separated and local.
type rootLook struct{ root *os.Root }

func (t rootLook) names(dir string) ([]string, error) {
	entries, err := fs.ReadDir(t.root.FS(), dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

func (t rootLook) exists(name string) bool {
	_, err := t.root.Lstat(name)
	return err == nil
}

func (t rootLook) isDir(name string) bool {
	info, err := t.root.Lstat(name)
	return err == nil && info.IsDir()
}

func (t rootLook) same(a, b string) bool {
	ia, err := t.root.Lstat(a)
	if err != nil {
		return false
	}
	ib, err := t.root.Lstat(b)
	return err == nil && os.SameFile(ia, ib)
}

func (t rootLook) readFile(name string) ([]byte, error) { return t.root.ReadFile(name) }

// writeGenerated confines even third-party generator output and existing
// symlinks to the requested output tree, and publishes the run whole
// (internal/atomicfile): every file is staged beside the one it replaces
// before any is moved, so a file that cannot be written (a read-only file,
// a full disk) refuses the run with the directory as it was, never with
// some files new and the rest old.
func writeGenerated(dir string, files map[string][]byte, lang string, removeStale bool, pkg string, schemaBases []string, verbose bool) error {
	for name := range files {
		if !filepath.IsLocal(name) || strings.Contains(name, "\\") {
			return fmt.Errorf("generator output %q is not a local path", name)
		}
	}
	txn := atomicfile.New()
	defer txn.Close()
	out, err := txn.OpenRoot(dir)
	if err != nil {
		return err
	}
	stale, err := planGenerated(rootLook{out.Root()}, out, files, lang, removeStale, pkg, schemaBases)
	if err != nil {
		return err
	}
	if err := txn.Commit(); err != nil {
		return err
	}
	if verbose {
		for _, name := range sortedKeys(files) {
			fmt.Printf("wrote %s\n", filepath.Join(dir, name))
		}
		for _, name := range stale {
			fmt.Printf("removed %s\n", filepath.Join(dir, name))
		}
	}
	return nil
}

func sortedKeys(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// planGenerated stages one run's changes to its output directory, every
// target's alike, and answers the stale files it removes: each output under
// this run's spelling, then (a default go generate) the stale surface files.
// It reads look and changes only through put.
func planGenerated(look outLook, put outChanges, files map[string][]byte, lang string, removeStale bool, pkg string, schemaBases []string) ([]string, error) {
	names := sortedKeys(files)
	respelled := map[string]bool{}
	for _, name := range names {
		// this run's spelling, for the file and every directory on the way
		// to it, on every target
		if err := claimSpelling(look, put, name, respelled); err != nil {
			return nil, err
		}
		put.Write(name, files[name], 0o644)
	}
	// A default go generate removes stale surface files it wrote on an earlier
	// run and this run did not write (SPEC-TABLES.md §8.8). With --surface
	// (removeStale false) nothing is removed; no other file is ever touched.
	// It is the only target with a cleanup: no other target's output is ever
	// removed, so no other target can lose a file this run wrote.
	if !removeStale || lang != "go" {
		return nil, nil
	}
	stale, err := staleGoFiles(look, names, pkg, schemaBases)
	if err != nil {
		return nil, err
	}
	for _, name := range stale {
		put.Delete(name)
	}
	return stale, nil
}

// claimSpelling makes name, and every directory on the way to it, answer
// under exactly this spelling once the run commits. A component that
// resolves to an entry the file system spells otherwise (a volume that folds
// names keeps the old spelling when a write lands on it) is respelled to this
// run's spelling, a directory with every child in it; the publisher renames
// through a temporary name, because such a volume may take a rename between
// two spellings of one name as a rename onto itself. A component that does
// not resolve, or is already spelled as written, is left alone. done holds
// the components already respelled by this run.
func claimSpelling(look outLook, put outChanges, name string, done map[string]bool) error {
	parent := "."
	for comp := range strings.SplitSeq(name, "/") {
		at := path.Join(parent, comp)
		if !look.exists(at) {
			return nil // nothing answers to the name: the write creates it as spelled
		}
		entries, err := look.names(parent)
		if err != nil {
			return err
		}
		if !slices.Contains(entries, comp) && !done[at] {
			for _, e := range entries {
				if look.same(path.Join(parent, e), at) {
					put.Respell(path.Join(parent, e), at)
					done[at] = true
					break
				}
			}
		}
		parent = at
	}
	return nil
}

const (
	goHeaderFrom = "// Code generated by the schema compiler from "
	goHeaderTail = ".schema. DO NOT EDIT."
)

// staleGoFiles names the .go files an earlier run of the compiler wrote for
// this unit and this run does not write. It reads and removes nothing.
//
// AN ENTRY THIS RUN WRITES IS NEVER STALE, and whether it is one is asked of
// the file system (the entry is one of the names written, by identity), not
// read off its spelling: on a volume that folds names an entry can hold this
// run's output under a name this run never spelled.
//
// Anything else is stale when its first line is exactly the compiler's own
// header for this unit: the package line, or the line naming one of the
// unit's .schema files. The file name in that line is compared without
// regard to case, so a source renamed only in case (Player.schema to
// player.schema) leaves no Player.go beside this run's player.go on a volume
// that keeps the two apart, where a Go build refuses the pair as a
// case-insensitive collision. No other file is ever touched.
func staleGoFiles(look outLook, written []string, pkg string, schemaBases []string) ([]string, error) {
	pkgHeader := fmt.Sprintf("// Code generated by the schema compiler for package %s. DO NOT EDIT.", pkg)
	owned := func(firstLine string) bool {
		if firstLine == pkgHeader {
			return true
		}
		base, ok := strings.CutPrefix(firstLine, goHeaderFrom)
		if !ok {
			return false
		}
		if base, ok = strings.CutSuffix(base, goHeaderTail); !ok {
			return false
		}
		for _, b := range schemaBases {
			if strings.EqualFold(base, b) {
				return true
			}
		}
		return false
	}
	wrote := map[string]bool{}
	var top []string // the names written at this level
	for _, name := range written {
		if !strings.Contains(name, "/") {
			wrote[name] = true
			top = append(top, name)
		}
	}
	mine := func(name string) bool {
		if wrote[name] {
			return true
		}
		for _, w := range top {
			if look.same(name, w) {
				return true
			}
		}
		return false
	}
	entries, err := look.names(".")
	if err != nil {
		return nil, err
	}
	var stale []string
	for _, name := range entries {
		if !strings.HasSuffix(name, ".go") || look.isDir(name) || mine(name) {
			continue
		}
		data, err := look.readFile(name)
		if err != nil {
			return nil, err
		}
		firstLine := data
		if before, _, ok := bytes.Cut(data, []byte{'\n'}); ok {
			firstLine = before
		}
		if owned(string(firstLine)) {
			stale = append(stale, name)
		}
	}
	return stale, nil
}
