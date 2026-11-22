package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/internal/atomicfile"
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

// refuseAliasedPaths is the check a command runs before any work: it refuses
// two outputs that are one existing file, and an output that is the command's
// input file. A destination that does not exist yet has no identity to compare
// and cannot be an input; publishOutputs checks again, and finds two fresh
// spellings of one entry before the second rename.
func refuseAliasedPaths(verb string, outs []outputPath, ins []outputPath) error {
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
	for _, in := range ins {
		if in.path == "" {
			continue
		}
		read := infoOf(in.path)
		if read.target == nil || read.target.IsDir() {
			// a missing input fails on its own read; a tree is no file to replace
			continue
		}
		for i, o := range outs {
			if infos[i].one(read) {
				return fmt.Errorf("%s: %s %s names the file %s reads (%s): writing it would replace the input", verb, o.flag, o.path, in.flag, in.path)
			}
		}
	}
	return nil
}

// outputFiles is the publisher's file operations for an output a person named
// by a flag: written into a directory that is there, never one made for it,
// as a plain create would be.
func outputFiles() atomicfile.Files { return atomicfile.OS().InExistingDirectories() }

// publishOutputs publishes every output through the one publisher
// (internal/atomicfile), as one distinct transaction: each new file is
// written to a temporary beside its destination and synced, and only once
// every one is written are they renamed into place, so a destination only
// ever holds the earlier file whole or the new file whole, never an empty or
// partial one. A rename that fails puts back every destination already
// published (the earlier entry itself, or no entry where there was none), so
// a failed publish leaves every destination as it found it, and no temporary
// outlives the call.
//
// Existing destinations are checked against each other and the inputs before
// anything is written (a destination that does not exist cannot be an
// input). Two fresh destinations can still be one entry, through a symbolic
// link or under two spellings a file system takes as one; the transaction is
// Distinct, so it refuses them, before any rename or, for a spelling only the
// file system can judge, before the second, putting the first back.
//
// All of that is for the one run writing to these destinations (the contract
// at the top of this file): another process renaming into them meanwhile is
// outside it.
func publishOutputs(verb string, outs []pairOutput, ins []outputPath) error {
	return publishOutputsWith(outputFiles(), verb, outs, ins)
}

// publishOutputsWith is publishOutputs through the file operations f, the
// publisher's seam.
func publishOutputsWith(f atomicfile.Files, verb string, outs []pairOutput, ins []outputPath) error {
	paths := make([]outputPath, len(outs))
	for i, o := range outs {
		paths[i] = o.outputPath
	}
	if err := aliasError(verb, paths, ins); err != nil {
		return err
	}
	txn := atomicfile.New()
	txn.Distinct()
	v := txn.With(f)
	for _, o := range outs {
		// a new file takes 0644, the mode every generated file in this tree
		// carries; an existing one keeps its own
		v.Write(o.path, o.data, 0o644)
	}
	err := txn.Commit()
	if alias, ok := errors.AsType[*atomicfile.AliasError](err); ok {
		flag := func(path string) string {
			for _, o := range outs {
				if filepath.Clean(o.path) == filepath.Clean(path) {
					return o.flag + " " + path
				}
			}
			return path
		}
		return fmt.Errorf("%s: %s and %s name the same file: the pair needs two files, and writing both to one would leave only the second; nothing was changed", verb, flag(alias.First), flag(alias.Second))
	}
	return err
}
