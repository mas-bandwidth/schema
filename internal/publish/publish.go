// Package publish moves a set of directory entries into place together or not
// at all, and answers where a path lands by asking the file system.
//
// It is the one publisher behind every command that replaces files a caller
// named: pin (its generate.go files), pack and cook (their output pair),
// uncook, and unpack (the entries of its trees). "Every entry or none, and
// none means the entries themselves are back" is written here once.
//
// ONE PUBLISHER PER DESTINATION IS THE CONTRACT, as with any compiler: nothing
// here takes a lock, and another process renaming into the same destinations
// meanwhile is outside it.
package publish

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Files is the two file operations a publish can fail in, a seam so tests
// fail each one at a chosen entry without a real read-only or full disk.
type Files struct {
	CreateTemp func(dir, pattern string) (*os.File, error)
	Rename     func(oldpath, newpath string) error
}

// OS is the real file system.
var OS = Files{CreateTemp: os.CreateTemp, Rename: os.Rename}

// Stage writes data to a new file beside path, with the given mode, and
// returns its name; on a failure that file is gone. Staged with no data, the
// file reserves a name beside path.
func (f Files) Stage(path, kind string, data []byte, mode os.FileMode) (string, error) {
	tmp, err := f.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"."+kind+".*")
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

// Entry is one directory entry on its way through Publish.
type Entry struct {
	Name string // as the caller named it, for messages
	Dest string // the entry that is replaced, created or removed
	// Next is the staged new content, on the same volume as Dest: a file, or
	// a directory when Tree is set. Empty, the entry is a REMOVAL: the
	// original is taken away and nothing takes its place.
	Next string
	Kept string // a name reserved beside Dest (Stage with no data), where the original entry is kept meanwhile
	Tree bool   // the staged content is a directory tree, not a file

	aside  bool // the original entry is at Kept, not at Dest
	placed bool // the staged content is at Dest
}

// Failure is a publish that did not complete: why, and every entry that could
// not be put back, each naming where its original is kept.
type Failure struct {
	Cause error
	Stuck []string
}

// Publish moves every staged entry into place, or none.
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
// before, when given, runs ahead of entry i and can refuse it; step, when
// given, is called after each entry lands. On success the kept originals are
// still there: RemoveKept removes them.
func (f Files) Publish(entries []*Entry, before func(i int) error, step func()) *Failure {
	for i, e := range entries {
		err := f.place(i, e, before)
		if err == nil {
			if step != nil {
				step()
			}
			continue
		}
		fail := &Failure{Cause: err}
		for j := i; j >= 0; j-- {
			f.putBack(entries[j], fail)
		}
		Clean(entries)
		return fail
	}
	return nil
}

func (f Files) place(i int, e *Entry, before func(i int) error) error {
	if before != nil {
		if err := before(i); err != nil {
			return err
		}
	}
	info, err := os.Lstat(e.Dest)
	switch {
	case err == nil:
		if e.Next != "" && info.IsDir() != e.Tree {
			if e.Tree {
				return fmt.Errorf("%s is not a directory, and a tree cannot replace it", e.Dest)
			}
			return fmt.Errorf("%s is a directory, and a file cannot replace it", e.Dest)
		}
		if info.IsDir() {
			// a directory cannot be renamed onto the file that reserved the name
			if err := os.Remove(e.Kept); err != nil {
				return err
			}
		}
		if err := f.Rename(e.Dest, e.Kept); err != nil {
			return err
		}
		e.aside = true
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	if e.Next == "" {
		return nil
	}
	if err := f.Rename(e.Next, e.Dest); err != nil {
		return err
	}
	e.placed = true
	return nil
}

func (f Files) putBack(back *Entry, fail *Failure) {
	// a file's original goes home over the new file in one rename; a tree,
	// and an entry that was not there before, is taken out first
	if back.placed && (back.Tree || !back.aside) {
		if err := os.Rename(back.Dest, back.Next); err != nil {
			if back.aside {
				fail.Stuck = append(fail.Stuck, fmt.Sprintf("%s (its original is kept at %s: %v)", back.Name, back.Kept, err))
			} else {
				fail.Stuck = append(fail.Stuck, fmt.Sprintf("%s (it did not exist before and could not be removed: %v)", back.Name, err))
			}
			return
		}
		back.placed = false
	}
	if !back.aside {
		return
	}
	if err := f.Rename(back.Kept, back.Dest); err != nil {
		fail.Stuck = append(fail.Stuck, fmt.Sprintf("%s (its original is kept at %s: %v)", back.Name, back.Kept, err))
		return
	}
	back.aside = false
	back.placed = false
	back.Kept = "" // the name is the original's own again
}

// Clean removes what a publish that failed, or never started, no longer
// wants: every staged entry still there, and every kept name that does not
// hold an original.
func Clean(entries []*Entry) {
	for _, e := range entries {
		if e.Next != "" && !e.placed {
			_ = os.RemoveAll(e.Next)
		}
		if e.Kept != "" && !e.aside {
			_ = os.Remove(e.Kept)
		}
	}
}

// RemoveKept removes the kept originals of a publish that completed (and the
// names reserved for entries that had none), and returns the ones it could
// not remove.
func RemoveKept(entries []*Entry) []string {
	var left []string
	for _, e := range entries {
		if e.Kept == "" {
			continue
		}
		if err := os.RemoveAll(e.Kept); err != nil {
			left = append(left, fmt.Sprintf("%s (%v)", e.Kept, err))
		}
	}
	return left
}
