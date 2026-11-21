package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/publish"
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
				if publish.Inside(o.path, read.target) {
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
// publishes them through the shared publisher (internal/publish), in
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
	return publishOutputsWith(publish.OS, verb, outs, ins)
}

func publishOutputsWith(f publish.Files, verb string, outs []pairOutput, ins []outputPath) error {
	paths := make([]outputPath, len(outs))
	for i, o := range outs {
		paths[i] = o.outputPath
	}
	if err := aliasError(verb, paths, ins); err != nil {
		return err
	}
	entries := make([]*publish.Entry, 0, len(outs))
	for _, o := range outs {
		mode := os.FileMode(0o644)
		if info, err := os.Stat(o.path); err == nil && info.Mode().IsRegular() {
			mode = info.Mode().Perm()
		}
		e := &publish.Entry{Name: o.flag + " " + o.path, Dest: o.path}
		entries = append(entries, e)
		var err error
		if e.Next, err = f.Stage(o.path, "new", o.data, mode); err == nil {
			e.Kept, err = f.Stage(o.path, "keep", nil, mode)
		}
		if err != nil {
			publish.Clean(entries)
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
	if fail := f.Publish(entries, before, func() { publishStep("renamed") }); fail != nil {
		if len(fail.Stuck) > 0 {
			return fmt.Errorf("%w; %s is HALF PUBLISHED: these outputs could not be put back as they were: %s", fail.Cause, verb, strings.Join(fail.Stuck, ", "))
		}
		return fail.Cause
	}
	if left := publish.RemoveKept(entries); len(left) > 0 {
		return fmt.Errorf("%s: every output was published, but these kept originals could not be removed: %s", verb, strings.Join(left, ", "))
	}
	return nil
}

// publishStep is a seam between the steps of publishOutputs; tests set it to
// look at the destinations while a publish is part done.
var publishStep = func(step string) {}
