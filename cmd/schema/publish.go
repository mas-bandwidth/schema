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
// input file. A destination that does not exist yet has no identity to compare;
// publishOutputs creates it exclusively and checks again before publishing.
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
// renames them into place, in order, only once every temporary is written. A
// destination that does not exist is first created exclusively, so it has an
// identity, and the outputs are checked against each other and the inputs
// again before anything is published; a refusal removes what it created. A
// rename that fails undoes the renames before it — each destination that
// existed gets its earlier file back, each that did not is removed — so a
// failed publish leaves every destination as it found it. No temporary,
// placeholder or backup outlives the call.
func publishOutputs(verb string, outs []pairOutput, ins []outputPath) (err error) {
	temps := make([]string, len(outs))
	backups := make([]string, len(outs))
	existed := make([]bool, len(outs))
	created := make([]bool, len(outs))
	published := false
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
		if !published {
			for i, made := range created {
				if made {
					_ = os.Remove(outs[i].path)
				}
			}
		}
	}()
	for i, o := range outs {
		if temps[i], err = stageOutput(o.path, o.data); err != nil {
			return err
		}
	}
	paths := make([]outputPath, len(outs))
	for i, o := range outs {
		paths[i] = o.outputPath
		f, cerr := os.OpenFile(o.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		switch {
		case cerr == nil:
			_ = f.Close()
			created[i] = true
		case errors.Is(cerr, os.ErrExist):
			// it exists: an earlier file, or an entry this call just created
			// under another spelling, which the check below refuses
		default:
			return cerr
		}
	}
	if err := aliasError(verb, paths, ins); err != nil {
		return err
	}
	for i, o := range outs {
		if created[i] {
			continue
		}
		info, lerr := os.Lstat(o.path)
		if lerr != nil {
			return lerr
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
	for i, o := range outs {
		if rerr := os.Rename(temps[i], o.path); rerr != nil {
			for j := i - 1; j >= 0; j-- {
				if existed[j] {
					if os.Rename(backups[j], outs[j].path) == nil {
						backups[j] = ""
					}
				} else {
					_ = os.Remove(outs[j].path)
				}
			}
			return rerr
		}
		temps[i] = ""
	}
	published = true
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
