package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PAIRED OUTPUTS (pack --message --announce, cook --attribution): one artifact
// in two files, read only together. A run publishes both or neither, so a
// failure anywhere leaves the earlier pair exactly as it was — never a new
// announcement beside an old message, or a new attribution beside an old cook.
//
// ONE PUBLISHER PER DESTINATION IS THE CONTRACT, as with any compiler: the
// promise holds when one run writes to an output at a time. Nothing here takes
// a lock, so two runs publishing to the same destinations at once can
// interleave their renames and leave one run's file beside the other's; each
// file is still whole. The caller gives concurrent runs their own outputs or
// runs them one after the other, and the help of pack and cook says so.
//
// WHETHER TWO PATHS ARE ONE FILE IS THE FILE SYSTEM'S ANSWER, never a string
// comparison: a symlinked directory, a case-folding volume and APFS's
// normalization-insensitive names all make two spellings one entry. Every
// identity below is os.SameFile over what Lstat and Stat return.

// outputPath is a file a command names by one of its flags.
type outputPath struct {
	flag string
	path string
}

// pairOutput is an output and the bytes it receives.
type pairOutput struct {
	outputPath
	data []byte
}

// refuseOutputsIntoInputs is the one check every command that writes a file a
// caller named runs before any work. It refuses two outputs that are one
// existing file, and an output that resolves to the command's input file, into
// a directory tree it reads, or to a schema source the unit was loaded from
// (SPEC.md §7: fmt is the only command that writes a schema file). ins holds
// all three, each under the name the refusal gives it. A destination that does
// not exist yet has no identity to compare and cannot be an input file (it can
// still sit inside an input tree, and is refused there); publishOutputs checks
// again, and finds two fresh spellings of one entry before the second rename.
func refuseOutputsIntoInputs(verb string, outs []outputPath, ins []outputPath) error {
	return aliasError(verb, outs, ins)
}

// entryInfo is a path's directory entry (Lstat) and the file it resolves to
// (Stat); either is nil where the path does not reach one.
type entryInfo struct {
	entry, target os.FileInfo
}

func infoOf(path string) entryInfo {
	var e entryInfo
	e.entry, _ = os.Lstat(path)
	e.target, _ = os.Stat(path)
	return e
}

func sameFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b)
}

// one reports whether two paths can reach one file: the same directory entry,
// or the same file through either of them.
func (e entryInfo) one(o entryInfo) bool {
	return sameFile(e.entry, o.entry) || sameFile(e.entry, o.target) ||
		sameFile(e.target, o.entry) || sameFile(e.target, o.target)
}

// insideTree reports whether writing path would land in the directory tree
// (the directory itself counts). It is asked of the file system, so a
// symlinked directory, a case-folded or a Unicode-equivalent spelling answer
// as the plain one does: the place the entry would sit, and the file an
// existing path resolves to, are each walked up to the root, and every
// directory on the way is compared with the tree by identity. A path whose
// directory does not exist yet is walked from its deepest existing ancestor.
func insideTree(path string, tree os.FileInfo) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	under := func(start string) bool {
		for d := start; ; d = filepath.Dir(d) {
			if info, err := os.Stat(d); err == nil && os.SameFile(info, tree) {
				return true
			}
			if filepath.Dir(d) == d {
				return false
			}
		}
	}
	// where the entry sits: its directory, every symbolic link followed
	for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			if under(real) {
				return true
			}
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	// what an existing path resolves to
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return under(real)
	}
	return false
}

func aliasError(verb string, outs []outputPath, ins []outputPath) error {
	infos := make([]entryInfo, len(outs))
	for i, o := range outs {
		infos[i] = infoOf(o.path)
	}
	for i := range outs {
		for j := i + 1; j < len(outs); j++ {
			if infos[i].one(infos[j]) {
				return fmt.Errorf("%s: %s %s and %s %s name the same file: the pair needs two files, and writing both to one would leave only the second", verb, outs[i].flag, outs[i].path, outs[j].flag, outs[j].path)
			}
		}
	}
	return inputsError(verb, outs, infos, ins)
}

// sourceInput is the name sourceInputs gives a schema source among a command's
// inputs; the refusal for one cites the rule that only fmt writes it.
const sourceInput = "the schema source"

// refuseSourceOutputs is refuseOutputsIntoInputs without the comparison of the
// outputs with each other, for a command whose outputs are many files under
// one directory it names (generate, testgen).
func refuseSourceOutputs(verb string, outs []outputPath, ins []outputPath) error {
	infos := make([]entryInfo, len(outs))
	for i, o := range outs {
		infos[i] = infoOf(o.path)
	}
	return inputsError(verb, outs, infos, ins)
}

func inputsError(verb string, outs []outputPath, infos []entryInfo, ins []outputPath) error {
	for _, in := range ins {
		if in.path == "" {
			continue
		}
		read := infoOf(in.path)
		if read.target == nil {
			// a missing input fails on its own read
			continue
		}
		if read.target.IsDir() {
			// AN INPUT TREE: every file in it is input, and a file added to it
			// is input to the next run, so no output may land in it
			for _, o := range outs {
				if insideTree(o.path, read.target) {
					return fmt.Errorf("%s: %s %s is inside the directory %s names (%s): writing it would change the input the command reads", verb, o.flag, o.path, in.flag, in.path)
				}
			}
			continue
		}
		for i, o := range outs {
			if !infos[i].one(read) {
				continue
			}
			if in.flag == sourceInput {
				return fmt.Errorf("%s: %s %s names the schema source %s, which this command reads: writing it would replace the source, and fmt is the only command that writes a schema file (SPEC.md §7)", verb, o.flag, o.path, in.path)
			}
			return fmt.Errorf("%s: %s %s names the file %s reads (%s): writing it would replace the input", verb, o.flag, o.path, in.flag, in.path)
		}
	}
	return nil
}

// publishOutputs writes every output to a temporary beside its destination and
// publishes them through the shared publisher (publishFiles.publish), in
// order, only once every temporary is written: a destination only ever holds
// the earlier file whole or the new file whole, never an empty or partial one.
// Existing destinations are checked against each other, the inputs and the
// input trees before anything is written. Two fresh destinations can still be
// one entry under two spellings, which only exists once the first is
// published, so before each one the destination is looked up and compared
// with every output already published; a match refuses and undoes them. A
// failure puts every destination back as it found it: a destination that
// existed gets its own directory entry back (the entry itself, renamed aside
// and renamed home, never a copy or a re-created link), one that did not is
// removed. No temporary outlives the call. An original that will not go back
// stays at its kept name and the error names both paths.
//
// All of that is for the one run writing to these destinations (the contract
// at the top of this file): another process renaming into them meanwhile is
// outside it, and undo would then restore over that process's file.
func publishOutputs(verb string, outs []pairOutput, ins []outputPath) error {
	return publishOutputsWith(osPublishFiles, verb, outs, ins)
}

func publishOutputsWith(f publishFiles, verb string, outs []pairOutput, ins []outputPath) error {
	paths := make([]outputPath, len(outs))
	for i, o := range outs {
		paths[i] = o.outputPath
	}
	if err := aliasError(verb, paths, ins); err != nil {
		return err
	}
	entries := make([]*publishEntry, 0, len(outs))
	for _, o := range outs {
		mode := os.FileMode(0o644)
		if info, err := os.Stat(o.path); err == nil && info.Mode().IsRegular() {
			mode = info.Mode().Perm()
		}
		e := &publishEntry{name: o.flag + " " + o.path, dest: o.path}
		entries = append(entries, e)
		var err error
		if e.next, err = f.stage(o.path, "new", o.data, mode); err == nil {
			e.kept, err = f.stage(o.path, "keep", nil, mode)
		}
		if err != nil {
			cleanPublish(entries)
			return err
		}
	}
	publishStep("staged")
	// two fresh spellings of one entry: the destination exists now only
	// because an earlier output of this run was published there
	before := func(i int) error {
		now, err := os.Lstat(outs[i].path)
		if err != nil {
			return nil
		}
		for j := range i {
			if done, derr := os.Lstat(outs[j].path); derr == nil && os.SameFile(now, done) {
				return fmt.Errorf("%s: %s %s and %s %s name the same file: the pair needs two files, and writing both to one would leave only the second", verb, outs[j].flag, outs[j].path, outs[i].flag, outs[i].path)
			}
		}
		return nil
	}
	if fail := f.publish(entries, before, publishStep); fail != nil {
		if len(fail.stuck) > 0 {
			return fmt.Errorf("%w; %s is HALF PUBLISHED: these outputs could not be put back as they were: %s", fail.cause, verb, strings.Join(fail.stuck, ", "))
		}
		return fail.cause
	}
	if left := removeKept(entries); len(left) > 0 {
		return fmt.Errorf("%s: every output was published, but these kept originals could not be removed: %s", verb, strings.Join(left, ", "))
	}
	return nil
}

// THE SHARED PUBLISHER: pin (generate.go files), pack and cook (their output
// pair), uncook, and unpack (its trees) all move a set of directory entries
// through the one routine below, so "every entry or none, and none means the
// entries themselves are back" is written once.

// publishFiles is the two file operations a publish can fail in, a seam so the
// tests fail each one at a chosen entry without a real read-only or full disk.
type publishFiles struct {
	createTemp func(dir, pattern string) (*os.File, error)
	rename     func(oldpath, newpath string) error
}

var osPublishFiles = publishFiles{createTemp: os.CreateTemp, rename: os.Rename}

// stage writes data to a new file beside path, with the given mode, and
// returns its name; on a failure that file is gone. Staged with no data, the
// file reserves a name beside path.
func (f publishFiles) stage(path, kind string, data []byte, mode os.FileMode) (string, error) {
	tmp, err := f.createTemp(filepath.Dir(path), "."+filepath.Base(path)+"."+kind+".*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// publishEntry is one directory entry on its way through publish.
type publishEntry struct {
	name string // as the caller named it, for messages
	dest string // the entry that is replaced, or created
	next string // the staged new content, beside dest: a file, or a directory when tree is set
	kept string // a name reserved beside dest, where the original entry is kept meanwhile
	tree bool   // the entry is a directory tree, not a file
	// aside: the original entry is at kept, not at dest
	aside bool
	// placed: the staged content is at dest
	placed bool
}

// publishFailure is a publish that did not complete: why, and every entry
// that could not be put back, each naming where its original is kept.
type publishFailure struct {
	cause error
	stuck []string
}

// publish moves every staged entry into place, or none.
//
// Each entry is published in order: the original ENTRY, when there is one, is
// renamed aside to its kept name and the staged content renamed into its
// place. If anything fails, every entry already published is undone, last
// first: the new content is taken back out and the original renamed home, so
// what returns is the entry itself (the same inode with its hard links, mode
// and times; the same symbolic link, never a new link to the same target),
// and an entry that did not exist is absent again. What was staged is then
// removed.
//
// An original that will not go home is the one case that ends neither all old
// nor all new: it is left at its kept name, never removed, and the failure
// names the entry and that name.
//
// before, when given, runs ahead of entry i and can refuse it; step is told
// "renamed" after each entry lands. On success the kept originals are still
// there: removeKept removes them.
func (f publishFiles) publish(entries []*publishEntry, before func(i int) error, step func(string)) *publishFailure {
	place := func(i int, e *publishEntry) error {
		if before != nil {
			if err := before(i); err != nil {
				return err
			}
		}
		info, err := os.Lstat(e.dest)
		switch {
		case err == nil:
			if info.IsDir() != e.tree {
				if e.tree {
					return fmt.Errorf("%s is not a directory, and a tree cannot replace it", e.dest)
				}
				return fmt.Errorf("%s is a directory, and a file cannot replace it", e.dest)
			}
			if e.tree {
				// a directory cannot be renamed onto the file that reserved the name
				if err := os.Remove(e.kept); err != nil {
					return err
				}
			}
			if err := f.rename(e.dest, e.kept); err != nil {
				return err
			}
			e.aside = true
		case !errors.Is(err, os.ErrNotExist):
			return err
		}
		if err := f.rename(e.next, e.dest); err != nil {
			return err
		}
		e.placed = true
		return nil
	}
	for i, e := range entries {
		err := place(i, e)
		if err == nil {
			if step != nil {
				step("renamed")
			}
			continue
		}
		fail := &publishFailure{cause: err}
		for j := i; j >= 0; j-- {
			back := entries[j]
			// a file's original goes home over the new file in one rename; a
			// tree, and an entry that was not there before, is taken out first
			if back.placed && (back.tree || !back.aside) {
				if rerr := os.Rename(back.dest, back.next); rerr != nil {
					if back.aside {
						fail.stuck = append(fail.stuck, fmt.Sprintf("%s (its original is kept at %s: %v)", back.name, back.kept, rerr))
					} else {
						fail.stuck = append(fail.stuck, fmt.Sprintf("%s (it did not exist before and could not be removed: %v)", back.name, rerr))
					}
					continue
				}
				back.placed = false
			}
			if !back.aside {
				continue
			}
			if rerr := f.rename(back.kept, back.dest); rerr != nil {
				fail.stuck = append(fail.stuck, fmt.Sprintf("%s (its original is kept at %s: %v)", back.name, back.kept, rerr))
				continue
			}
			back.aside = false
			back.placed = false
			back.kept = "" // the name is the original's own again
		}
		cleanPublish(entries)
		return fail
	}
	return nil
}

// cleanPublish removes what a failed publish no longer wants: every staged
// entry still there, and every kept name that does not hold an original.
func cleanPublish(entries []*publishEntry) {
	for _, e := range entries {
		if e.next != "" && !e.placed {
			_ = os.RemoveAll(e.next)
		}
		if e.kept != "" && !e.aside {
			_ = os.Remove(e.kept)
		}
	}
}

// removeKept removes the kept originals of a publish that completed (and the
// names reserved for entries that had none), and returns the ones it could
// not remove.
func removeKept(entries []*publishEntry) []string {
	var left []string
	for _, e := range entries {
		if e.kept == "" {
			continue
		}
		if err := os.RemoveAll(e.kept); err != nil {
			left = append(left, fmt.Sprintf("%s (%v)", e.kept, err))
		}
	}
	return left
}

// publishStep is a seam between the steps of publishOutputs; tests set it to
// look at the destinations while a publish is part done.
var publishStep = func(step string) {}
