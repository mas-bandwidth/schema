package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/internal/atomicfile"
	"github.com/mas-bandwidth/schema/v2/internal/pathid"
)

// PAIRED OUTPUTS (pack --message --announce, cook --attribution): one artifact
// in two files, read only together. A run publishes both or neither, so a
// failure anywhere leaves the earlier pair exactly as it was — never a new
// announcement beside an old message, or a new attribution beside an old cook.
// The publishing is the one publisher's (internal/atomicfile): every command
// that writes a file writes it there, and a pair is one transaction of it.
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
// identity below is os.SameFile over what Lstat and Stat return, and two
// names that do not exist yet and only the volume could call one entry are
// refused by the commit itself (atomicfile.ErrAppeared).

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
// file, and an output that resolves to the command's input file, into a
// directory tree it reads, or to a schema source the unit was loaded from
// (SPEC.md §7: fmt is the only command that writes a schema file). ins holds
// all three, each under the name the refusal gives it. A destination that does
// not exist yet cannot be an input file (it can still sit inside an input
// tree, and is refused there); publishOutputs checks again.
func refuseOutputsIntoInputs(verb string, outs []outputPath, ins []outputPath) error {
	return aliasError(verb, outs, ins)
}

// entryInfo is a path's directory entry (Lstat), the file it resolves to
// (Stat), and the directory the entry sits in (Stat of the parent); each is
// nil where the path does not reach one.
type entryInfo struct {
	entry, target, parent os.FileInfo
	base                  string
}

func infoOf(path string) entryInfo {
	var e entryInfo
	e.entry, _ = os.Lstat(path)
	e.target, _ = os.Stat(path)
	e.parent, _ = os.Stat(filepath.Dir(path))
	e.base = filepath.Base(path)
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

// slot reports whether two paths that do not exist yet name one entry in one
// directory, however the directory is reached: the same name in the same
// directory, by identity.
func (e entryInfo) slot(o entryInfo) bool {
	return e.entry == nil && o.entry == nil && e.base == o.base && sameFile(e.parent, o.parent)
}

func aliasMessage(verb string, a, b outputPath) error {
	return fmt.Errorf("%s: %s %s and %s %s name the same file: the pair needs two files, and writing both to one would leave only the second", verb, a.flag, a.path, b.flag, b.path)
}

func aliasError(verb string, outs []outputPath, ins []outputPath) error {
	infos := make([]entryInfo, len(outs))
	for i, o := range outs {
		infos[i] = infoOf(o.path)
	}
	for i := range outs {
		for j := i + 1; j < len(outs); j++ {
			if infos[i].one(infos[j]) || infos[i].slot(infos[j]) {
				return aliasMessage(verb, outs[i], outs[j])
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
				if pathid.Inside(o.path, read.target) {
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

// publishOutputs publishes every output through the one publisher, as one
// transaction: each new content is staged whole beside its destination
// before anything is moved, and then every destination is replaced, or none
// is. A destination only ever holds the earlier file whole or the new file
// whole, never an empty or partial one. The outputs are checked against each
// other, the inputs and the input trees again first, so nothing is staged for
// a refused run. A failure puts every destination back as the directory entry
// it was (the entry itself, renamed aside and renamed home: the same inode,
// its hard links, its mode and its time; a symbolic link stays the link it
// was, and the file it names is the one replaced), and one that did not exist
// is removed. No temporary outlives the call. An original that will not go
// back stays at its kept name, and the error names both paths.
//
// All of that is for the one run writing to these destinations (the contract
// at the top of this file): another process renaming into them meanwhile is
// outside it.
func publishOutputs(verb string, outs []pairOutput, ins []outputPath) error {
	return publishOutputsWith(atomicfile.OS(), verb, outs, ins)
}

// publishOutputsWith is publishOutputs through the file operations f, the
// publisher's seam for a test that fails a chosen step.
func publishOutputsWith(f atomicfile.Files, verb string, outs []pairOutput, ins []outputPath) error {
	paths := make([]outputPath, len(outs))
	for i, o := range outs {
		paths[i] = o.outputPath
	}
	if err := aliasError(verb, paths, ins); err != nil {
		return err
	}
	txn := atomicfile.New()
	defer txn.Close()
	v := txn.With(f)
	for _, o := range outs {
		v.Write(o.path, o.data, 0o644)
	}
	err := txn.Commit()
	if errors.Is(err, atomicfile.ErrAppeared) && len(outs) == 2 {
		// two names that did not exist, which this volume takes for one
		return fmt.Errorf("%w (%v)", aliasMessage(verb, outs[0].outputPath, outs[1].outputPath), err)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", verb, err)
	}
	return nil
}
