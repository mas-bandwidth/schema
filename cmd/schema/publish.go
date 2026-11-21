package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PAIRED OUTPUTS (pack --message --announce, cook --attribution): one artifact
// in two files, read only together. A run publishes both or neither, so a
// failure anywhere leaves the earlier pair exactly as it was — never a new
// announcement beside an old message, or a new attribution beside an old cook.

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

// refuseAliasedPaths refuses two outputs that name one directory entry, and an
// output that names one of the command's input files. Paths are compared after
// resolving symlinks in their directories, so `d/x` and `link-to-d/x` are one
// file; an output that is itself a symlink is replaced by the publish rather
// than written through, so its own link is not followed. It runs before the
// command does any work.
func refuseAliasedPaths(verb string, outs []outputPath, ins []outputPath) error {
	entries := make([]resolvedEntry, len(outs))
	for i, o := range outs {
		entries[i] = resolveEntry(o.path, false)
	}
	for i := range outs {
		for j := i + 1; j < len(outs); j++ {
			if entries[i].same(entries[j]) {
				return fmt.Errorf("%s: %s %s and %s %s name the same file: the pair needs two files, and writing both to one would leave only the second", verb, outs[i].flag, outs[i].path, outs[j].flag, outs[j].path)
			}
		}
	}
	for _, in := range ins {
		if in.path == "" {
			continue
		}
		if info, err := os.Stat(in.path); err != nil || info.IsDir() {
			// a missing input fails on its own read; a tree is no file to replace
			continue
		}
		read := resolveEntry(in.path, true)
		for i, o := range outs {
			if read.same(entries[i]) {
				return fmt.Errorf("%s: %s %s names the file %s reads (%s): writing it would replace the input", verb, o.flag, o.path, in.flag, in.path)
			}
		}
	}
	return nil
}

// resolvedEntry is a directory entry with the symlinks in its directory
// resolved. dir is nil when the directory cannot be read; the entry then
// compares by its cleaned absolute path, and its write will fail on its own.
type resolvedEntry struct {
	path string
	base string
	dir  os.FileInfo
}

func resolveEntry(path string, followLeaf bool) resolvedEntry {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	if followLeaf {
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
	}
	e := resolvedEntry{path: abs, base: filepath.Base(abs)}
	dir := filepath.Dir(abs)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		e.path = filepath.Join(real, e.base)
		if info, err := os.Stat(real); err == nil {
			e.dir = info
		}
	}
	return e
}

func (e resolvedEntry) same(o resolvedEntry) bool {
	if e.path == o.path {
		return true
	}
	if e.dir == nil || o.dir == nil || !os.SameFile(e.dir, o.dir) {
		return false
	}
	// one directory: the names are one entry when the file system says so,
	// and the volumes macOS and Windows ship fold case
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.EqualFold(e.base, o.base)
	}
	return e.base == o.base
}

// publishOutputs writes every output to a temporary beside its destination and
// renames them into place, in order, only once every temporary is written. A
// rename that fails undoes the renames before it — each destination that
// existed gets its earlier file back, each that did not is removed — so a
// failed publish leaves every destination as it found it. No temporary or
// backup outlives the call.
func publishOutputs(outs []pairOutput) (err error) {
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
	for i, o := range outs {
		if temps[i], err = stageOutput(o.path, o.data); err != nil {
			return err
		}
	}
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
