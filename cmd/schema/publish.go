package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// publishOutputs writes every output to a temporary beside its destination and
// renames them into place, in order, only once every temporary is written: a
// destination only ever holds the earlier file whole or the new file whole,
// never an empty or partial one. Existing destinations are checked against
// each other and the inputs before anything is written (a destination that
// does not exist cannot be an input). Two fresh destinations can still be one
// entry under two spellings, which only exists once the first is renamed, so
// before each rename the destination is looked up and compared with every
// output already published; a match refuses and undoes them. A rename that
// fails undoes the renames before it too — each destination that existed gets
// its earlier file back, each that did not is removed — so a failed publish
// leaves every destination as it found it. No temporary or backup outlives the
// call.
//
// All of that is for the one run writing to these destinations (the contract
// at the top of this file): another process renaming into them meanwhile is
// outside it, and undo would then restore over that process's file.
func publishOutputs(verb string, outs []pairOutput, ins []outputPath) (err error) {
	temps := make([]string, len(outs))
	backups := make([]string, len(outs))
	existed := make([]bool, len(outs))
	defer func() {
		for _, name := range temps {
			if name != "" {
				_ = os.Remove(name)
			}
		}
		for _, name := range backups {
			if name != "" {
				_ = os.Remove(name)
			}
		}
	}()
	paths := make([]outputPath, len(outs))
	for i, o := range outs {
		paths[i] = o.outputPath
	}
	if err := aliasError(verb, paths, ins); err != nil {
		return err
	}
	for i, o := range outs {
		if temps[i], err = stageOutput(o.path, o.data); err != nil {
			return err
		}
	}
	publishStep("staged")
	for i, o := range outs {
		info, lerr := os.Lstat(o.path)
		if lerr != nil {
			if !errors.Is(lerr, os.ErrNotExist) {
				return lerr
			}
			continue
		}
		if info.IsDir() {
			// the rename below fails and reports it; there is nothing to keep
			continue
		}
		existed[i] = true
		if backups[i], err = backupOutput(o.path, info); err != nil {
			return err
		}
	}
	publishStep("backed-up")
	// undo puts back every destination published before i
	undo := func(i int) {
		for j := i - 1; j >= 0; j-- {
			if existed[j] {
				if os.Rename(backups[j], outs[j].path) == nil {
					backups[j] = ""
				}
			} else {
				_ = os.Remove(outs[j].path)
			}
		}
	}
	for i, o := range outs {
		if now, lerr := os.Lstat(o.path); lerr == nil {
			for j := range i {
				if done, derr := os.Lstat(outs[j].path); derr == nil && os.SameFile(now, done) {
					undo(i)
					return fmt.Errorf("%s: %s %s and %s %s name the same file: the pair needs two files, and writing both to one would leave only the second", verb, outs[j].flag, outs[j].path, o.flag, o.path)
				}
			}
		}
		if rerr := os.Rename(temps[i], o.path); rerr != nil {
			undo(i)
			return rerr
		}
		temps[i] = ""
		publishStep("renamed")
	}
	return nil
}

// stageOutput writes data to a new temporary beside path, carrying the mode of
// an existing file (0644 for a new one, the mode every generated file in this
// tree carries), and returns its name. A failure removes the temporary.
func stageOutput(path string, data []byte) (string, error) {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := os.Chmod(name, mode); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// backupOutput keeps the file now at path under a new name beside it, so a
// failed publish can rename it back: a hard link where the file system allows
// one (the same inode, untouched), else a copy of the bytes and mode, and for a
// symlink a new link to the same target.
func backupOutput(path string, info os.FileInfo) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".keep.*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	_ = tmp.Close()
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err == nil {
			_ = os.Remove(name)
			err = os.Symlink(target, name)
		}
		if err != nil {
			_ = os.Remove(name)
			return "", err
		}
		return name, nil
	}
	_ = os.Remove(name)
	if os.Link(path, name) == nil {
		return name, nil
	}
	data, err := os.ReadFile(path)
	if err == nil {
		err = os.WriteFile(name, data, info.Mode().Perm())
	}
	if err == nil {
		err = os.Chmod(name, info.Mode().Perm())
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// publishStep is a seam between the steps of publishOutputs; tests set it to
// look at the destinations while a publish is part done.
var publishStep = func(step string) {}
