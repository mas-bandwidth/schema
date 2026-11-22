// Package pathid answers where a path lands, by the file system's identity of
// the directories and files on the way to it, never by comparing spellings: a
// symlinked directory, a volume that folds case and one that takes two
// Unicode spellings for one name all make two paths one place, and only the
// file system knows which. Everything here reads; nothing writes.
package pathid

import (
	"os"
	"path/filepath"
)

// Inside reports whether writing path would land in the directory tree, the
// directory itself included. The directory the entry would sit in, and the
// file an existing path resolves to, are each walked up to the root, and
// every directory on the way is compared with the tree by identity
// (os.SameFile). A path whose directory does not exist yet is walked from its
// deepest existing ancestor: what does not exist yet would be made inside it.
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

// Overlap reports whether two existing directories are one, or one is inside
// the other, by identity. Each is spelled clean first, as a directory made
// for it was.
func Overlap(a, b string) (bool, error) {
	a, b = filepath.Clean(a), filepath.Clean(b)
	ia, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return Inside(a, ib) || Inside(b, ia), nil
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
