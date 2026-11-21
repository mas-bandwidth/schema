package publish

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

// WHERE A PATH LANDS IS THE FILE SYSTEM'S ANSWER, never a string comparison:
// a symlinked directory, a case-folding volume and a volume that takes two
// Unicode spellings for one name all make two paths one place. Everything
// that exists is compared by identity (os.SameFile); only the part of a path
// that does not exist yet is compared by name, under the rule the volume it
// will be created on applies.

// Inside reports whether writing path would land in the directory tree (the
// directory itself counts). The place the entry would sit, and the file an
// existing path resolves to, are each walked up to the root, and every
// directory on the way is compared with the tree by identity. A path whose
// directory does not exist yet is walked from its deepest existing ancestor.
func Inside(path string, tree os.FileInfo) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	// where the entry sits: its directory, every symbolic link followed
	for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			if under(real, tree) {
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
		return under(real, tree)
	}
	return false
}

// under walks start and every directory above it, and reports whether one of
// them is the tree.
func under(start string, tree os.FileInfo) bool {
	for d := start; ; d = filepath.Dir(d) {
		if info, err := os.Stat(d); err == nil && os.SameFile(info, tree) {
			return true
		}
		if filepath.Dir(d) == d {
			return false
		}
	}
}

// location is where a path lands: the deepest ancestor that exists, every
// symbolic link followed, and the components below it that do not exist yet.
type location struct {
	real string
	info os.FileInfo
	rest []string
}

func locate(path string) (location, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return location{}, err
	}
	var rest []string
	for dir := abs; ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			info, err := os.Stat(real)
			if err != nil {
				return location{}, err
			}
			return location{real: real, info: info, rest: rest}, nil
		}
		if filepath.Dir(dir) == dir {
			return location{}, fmt.Errorf("%s: no directory on the way to it exists", path)
		}
		rest = append([]string{filepath.Base(dir)}, rest...)
	}
}

// Overlap reports whether two directories a command is about to write are one
// place, or one inside the other, whether or not either exists yet. Two paths
// that exist are compared by identity. A path that does not exist yet is
// resolved to its deepest existing ancestor, and the names below it are
// compared under the rule of the volume they will be created on (Folds): on
// a volume that folds names, two names that differ only by case are one, and
// two that differ and are not both plain portable ASCII are taken for one
// too, because no rule short of the volume's own says which Unicode
// spellings it merges.
func Overlap(a, b string) (bool, error) {
	la, err := locate(a)
	if err != nil {
		return false, err
	}
	lb, err := locate(b)
	if err != nil {
		return false, err
	}
	return la.holds(lb) || lb.holds(la), nil
}

// holds reports whether q is p or inside it.
func (p location) holds(q location) bool {
	if len(p.rest) == 0 {
		return under(q.real, p.info)
	}
	// p does not exist yet, so anything inside it hangs off the same existing
	// directory, by at least p's own missing names
	if !os.SameFile(p.info, q.info) || len(q.rest) < len(p.rest) {
		return false
	}
	folds := Folds(p.real)
	for i, name := range p.rest {
		if !sameName(name, q.rest[i], folds) {
			return false
		}
	}
	return true
}

func sameName(a, b string, folds bool) bool {
	if a == b {
		return true
	}
	if !folds {
		return false
	}
	if portable(a) && portable(b) {
		return strings.EqualFold(a, b)
	}
	return true
}

// portable holds a name to the POSIX portable filename characters,
// [A-Za-z0-9._-]: the names every file system stores as written.
func portable(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return name != ""
}

// Folds reports whether the volume holding dir, an existing directory with
// every symbolic link followed, takes two spellings of a name for one entry.
// It is a variable so a test states the rule of a volume it does not have.
//
// The answer is read, never written: the nearest directory on the way up
// whose name has a letter is looked up under the other case, and the volume
// folds when that finds the same directory. A path with no letter in it
// answers by platform.
var Folds = func(dir string) bool {
	for d := dir; filepath.Dir(d) != d; d = filepath.Dir(d) {
		base := filepath.Base(d)
		flipped := flipCase(base)
		if flipped == base {
			continue
		}
		here, err := os.Lstat(d)
		if err != nil {
			continue
		}
		other, err := os.Lstat(filepath.Join(filepath.Dir(d), flipped))
		return err == nil && os.SameFile(here, other)
	}
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

func flipCase(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsUpper(r):
			return unicode.ToLower(r)
		case unicode.IsLower(r):
			return unicode.ToUpper(r)
		}
		return r
	}, name)
}
