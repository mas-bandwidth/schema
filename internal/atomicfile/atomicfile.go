// Package atomicfile is the one way a schema command changes a file it did
// not just create: the one publisher. No command writes into a file in place,
// so a write that fails part way (a full disk, a file-size limit, a read-only
// entry) never leaves a file shorter than it found it.
//
// A [Txn] collects the changes of one command: files to write, entries to
// remove, a directory to respell. [Txn.Commit] stages every new content in a
// temporary file beside the file it replaces, written whole and synced, and
// only then publishes by rename, so the failures a write meets in practice
// happen before anything has changed. A command that changes several entries
// gets all of them or none: when a publishing rename fails, every original
// entry already moved aside is renamed back, the entry itself (the same
// inode, with its hard links, its mode and the time it was last written),
// not a copy of its bytes.
//
// A path that is a symbolic link is followed: the file it names is the one
// replaced, and the link stays. An existing file keeps its mode. A file its
// owner has made read-only is not replaced: the commit is refused with
// nothing changed.
//
// One publisher per destination is the contract: the package takes no lock.
package atomicfile

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// Files is the file operations a publish is made of, on names: plain paths
// for [OS], names inside the root for [InRoot]. The fields are a seam: a test
// replaces one to fail a publish at a chosen point without a real read-only
// or full disk.
type Files struct {
	// Create makes a new file for writing and fails if the name exists.
	Create func(name string, mode fs.FileMode) (*os.File, error)
	// Write writes the whole of data to the staged file f, created as name.
	Write func(f *os.File, name string, data []byte) error
	// Rename renames an entry; newname is replaced if it exists.
	Rename func(oldname, newname string) error
	// Remove removes one file or one empty directory.
	Remove func(name string) error
	// RemoveAll removes an entry and, for a directory, everything under it.
	RemoveAll func(name string) error
	// Lstat describes an entry without following a symbolic link.
	Lstat func(name string) (fs.FileInfo, error)
	// Readlink reads a symbolic link.
	Readlink func(name string) (string, error)
	// Mkdir makes one directory.
	Mkdir func(name string, mode fs.FileMode) error
	// Probe reports whether an existing file may be written: it opens the
	// file for writing, changes nothing, and closes it.
	Probe func(name string) error

	// base is what a name is relative to: "" for plain paths, the root's
	// own path for a rooted set. rooted names never leave the root.
	base   string
	rooted bool
}

// OS is the file operations on plain paths.
func OS() Files {
	return Files{
		Create: func(name string, mode fs.FileMode) (*os.File, error) {
			return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		},
		Write:     writeAll,
		Rename:    os.Rename,
		Remove:    os.Remove,
		RemoveAll: os.RemoveAll,
		Lstat:     os.Lstat,
		Readlink:  os.Readlink,
		Mkdir:     os.Mkdir,
		Probe: func(name string) error {
			f, err := os.OpenFile(name, os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			return f.Close()
		},
	}
}

// InRoot is the file operations confined to r: every name is relative to the
// root, and none resolves outside it, a symbolic link planted in the tree
// included.
func InRoot(r *os.Root) Files {
	return Files{
		Create: func(name string, mode fs.FileMode) (*os.File, error) {
			return r.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		},
		Write:     writeAll,
		Rename:    r.Rename,
		Remove:    r.Remove,
		RemoveAll: r.RemoveAll,
		Lstat:     r.Lstat,
		Readlink:  r.Readlink,
		Mkdir:     r.Mkdir,
		Probe: func(name string) error {
			f, err := r.OpenFile(name, os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			return f.Close()
		},
		base:   r.Name(),
		rooted: true,
	}
}

// InExistingDirectories is f that makes no directory: a write whose directory
// is not there is refused with nothing changed, as creating the file there
// would be. A command writing to a path a person named (pack --out, cook
// --attribution) uses it; a command that owns an output tree does not.
func (f Files) InExistingDirectories() Files {
	shown := f.shown
	f.Mkdir = func(name string, _ fs.FileMode) error {
		return &fs.PathError{Op: "write into", Path: shown(name), Err: fs.ErrNotExist}
	}
	return f
}

func writeAll(f *os.File, _ string, data []byte) error {
	_, err := f.Write(data)
	return err
}

// shown is the name as a person reads it in an error: a rooted name with its
// root in front.
func (f Files) shown(name string) string {
	if f.rooted {
		return filepath.Join(f.base, name)
	}
	return name
}

// errEscapes is a name that leaves a root.
var errEscapes = errors.New("path escapes the output directory")

// resolve follows every symbolic link on path and answers the name of the
// file itself. A component that does not exist ends the walk: what is below
// it is new, and is taken as written. In a root, a link to an absolute path
// and a path that climbs above the root are refused.
func (f Files) resolve(path string) (string, error) {
	vol := filepath.VolumeName(path)
	abs := filepath.IsAbs(path)
	split := func(p string) []string { return strings.Split(filepath.ToSlash(p), "/") }
	rest := split(path[len(vol):])
	var done []string
	name := func(parts []string) string {
		s := strings.Join(parts, string(filepath.Separator))
		switch {
		case abs:
			return vol + string(filepath.Separator) + s
		case s == "":
			return "."
		}
		return s
	}
	for links := 0; len(rest) > 0; {
		c := rest[0]
		rest = rest[1:]
		switch c {
		case "", ".":
			continue
		case "..":
			switch n := len(done); {
			case n > 0 && done[n-1] != "..":
				done = done[:n-1]
			case f.rooted:
				return "", fmt.Errorf("%s: %w", f.shown(path), errEscapes)
			case !abs:
				done = append(done, "..")
			}
			continue
		}
		at := name(append(done[:len(done):len(done)], c))
		info, err := f.Lstat(at)
		if errors.Is(err, fs.ErrNotExist) {
			done = append(done, c)
			for _, r := range rest {
				if r != "" && r != "." {
					done = append(done, r)
				}
			}
			return name(done), nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			done = append(done, c)
			continue
		}
		if links++; links > 255 {
			return "", fmt.Errorf("%s: too many levels of symbolic links", f.shown(path))
		}
		target, err := f.Readlink(at)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(target) {
			if f.rooted {
				return "", fmt.Errorf("%s: %s is a symbolic link to %s: %w", f.shown(path), f.shown(at), target, errEscapes)
			}
			vol, abs, done = filepath.VolumeName(target), true, nil
			target = target[len(vol):]
		}
		rest = append(split(target), rest...)
	}
	return name(done), nil
}

// kind is what one change does.
type kind int

const (
	write kind = iota
	remove
	respell
)

// change is one entry on its way through a commit.
type change struct {
	kind kind
	f    Files
	path string // as the caller named it
	data []byte
	mode fs.FileMode
	to   string // respell: the spelling the entry takes

	file string // write: path with every symbolic link followed
	next string // write: the staged new content, beside file
	kept string // where the original entry is kept while the commit publishes; respell: the name it passes through
	// aside: the original entry is at kept, not at its own name
	aside bool
	// placed: the new entry is at its own name
	placed bool
	// dropped: an earlier write to the same file, superseded by a later one
	dropped bool
}

// Txn is the changes of one command, published together by Commit.
type Txn struct {
	changes  []*change
	roots    []*os.Root
	made     []made // directories staging created, outermost first
	done     bool
	distinct bool
}

// Distinct makes every write of t a file of its own: two writes that reach
// one file refuse the commit with nothing changed, where otherwise the later
// would win. The paths are compared after every symbolic link is followed,
// and again by the file system's own answer as each file is published, since
// two names a case-folding or normalizing file system takes as one entry are
// one only once the first is there. A command whose outputs are one artifact
// in several files (a message and its announcement) needs each one.
func (t *Txn) Distinct() { t.distinct = true }

// AliasError is two writes of a [Txn.Distinct] transaction that reach one
// file, by the paths the caller named.
type AliasError struct {
	First, Second string
}

func (e *AliasError) Error() string {
	return fmt.Sprintf("%s and %s name the same file", e.First, e.Second)
}

// aliased answers the AliasError of steps[i] against a write published before
// it, if the entry it is about to replace is the one that write placed.
func aliased(steps []*change, i int) error {
	c := steps[i]
	now, err := c.f.Lstat(c.file)
	if err != nil {
		return nil // nothing there yet: it is no file already published
	}
	for _, d := range steps[:i] {
		if d.kind != write || !d.placed {
			continue
		}
		if was, err := d.f.Lstat(d.file); err == nil && os.SameFile(now, was) {
			return &AliasError{First: d.f.shown(d.path), Second: c.f.shown(c.path)}
		}
	}
	return nil
}

type made struct {
	f    Files
	name string
}

// New starts a transaction.
func New() *Txn { return &Txn{} }

// View is a transaction's changes through one set of file operations.
type View struct {
	t    *Txn
	f    Files
	root *os.Root
	dir  string // every name is taken inside it
}

// With answers the view of t that works through f.
func (t *Txn) With(f Files) View { return View{t: t, f: f.faulted()} }

// OpenRoot opens dir as a root and answers the view confined to it. The
// transaction owns the root and closes it when it ends.
func (t *Txn) OpenRoot(dir string) (View, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return View{}, err
	}
	t.roots = append(t.roots, r)
	v := t.With(InRoot(r))
	v.root = r
	return v, nil
}

// Under answers the view of the directory dir inside this one: every name is
// taken inside dir, which the commit makes if it is not there. It has no
// root of its own to read through.
func (v View) Under(dir string) View {
	return View{t: v.t, f: v.f, dir: filepath.Join(v.dir, dir)}
}

// Root is the root this view is confined to, for reading; nil for a view
// made by With or Under.
func (v View) Root() *os.Root { return v.root }

// Write stages data as the content of path. An existing file keeps its mode;
// a new one takes mode. A later Write to the same file replaces an earlier
// one.
func (v View) Write(path string, data []byte, mode fs.FileMode) {
	v.t.changes = append(v.t.changes, &change{kind: write, f: v.f, path: filepath.Join(v.dir, path), data: data, mode: mode})
}

// Delete stages the removal of the entry at path: a file, a symbolic link
// itself, or a directory with everything under it. An entry that is not
// there when the commit reaches it is no change.
func (v View) Delete(path string) {
	v.t.changes = append(v.t.changes, &change{kind: remove, f: v.f, path: filepath.Join(v.dir, path)})
}

// Respell stages the rename of the entry at path to another spelling of the
// same name, for a filesystem that folds case and keeps the old spelling.
func (v View) Respell(path, to string) {
	v.t.changes = append(v.t.changes, &change{kind: respell, f: v.f, path: filepath.Join(v.dir, path), to: filepath.Join(v.dir, to)})
}

// Write stages data as the content of the plain path. See [View.Write].
func (t *Txn) Write(path string, data []byte, mode fs.FileMode) {
	t.With(OS()).Write(path, data, mode)
}

// Delete stages the removal of the entry at the plain path. See [View.Delete].
func (t *Txn) Delete(path string) { t.With(OS()).Delete(path) }

// WriteFile replaces the content of one file, or creates it: the whole new
// content, or the file as it was.
func WriteFile(path string, data []byte, mode fs.FileMode) error {
	t := New()
	t.Write(path, data, mode)
	return t.Commit()
}

// Close ends a transaction that will not commit, releasing what it holds. It
// is safe after Commit.
func (t *Txn) Close() {
	for _, r := range t.roots {
		_ = r.Close()
	}
	t.roots = nil
	t.done = true
}

// sibling answers a name beside name that no entry has: ".<base>.<tag>.<n>".
func sibling(f Files, name, tag string) (string, error) {
	dir, base := filepath.Split(name)
	for range 1000 {
		s := dir + "." + base + "." + tag + "." + fmt.Sprintf("%016x", rand.Uint64())
		_, err := f.Lstat(s)
		if errors.Is(err, fs.ErrNotExist) {
			return s, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("%s: no free name beside it", f.shown(name))
}

// mkdirs makes the directories on dir that are missing, recording each.
func (t *Txn) mkdirs(f Files, dir string) error {
	if dir == "." || dir == "" || dir == string(filepath.Separator) || dir == filepath.VolumeName(dir) {
		return nil
	}
	if _, err := f.Lstat(dir); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := t.mkdirs(f, filepath.Dir(dir)); err != nil {
		return err
	}
	if err := f.Mkdir(dir, 0o755); err != nil {
		return err
	}
	t.made = append(t.made, made{f, dir})
	return nil
}

// stage writes one file's new content beside the file it replaces.
func (t *Txn) stage(c *change) error {
	f := c.f
	if err := t.mkdirs(f, filepath.Dir(c.file)); err != nil {
		return err
	}
	mode := c.mode
	exists := false
	switch info, err := f.Lstat(c.file); {
	case err == nil:
		if info.IsDir() {
			return &fs.PathError{Op: "write", Path: f.shown(c.file), Err: syscall.EISDIR}
		}
		// a file its owner made read-only is not replaced
		if err := f.Probe(c.file); err != nil {
			return err
		}
		mode, exists = info.Mode().Perm(), true
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	var tmp *os.File
	for range 1000 {
		name, err := sibling(f, c.file, "new")
		if err != nil {
			return err
		}
		tmp, err = f.Create(name, mode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		c.next = name
		break
	}
	if tmp == nil {
		return fmt.Errorf("%s: no free name beside it", f.shown(c.file))
	}
	err := f.Write(tmp, c.next, c.data)
	if err == nil && exists {
		// the mode the file has, exactly; a new file's is the creation's own
		err = tmp.Chmod(mode)
	}
	if err == nil {
		err = syncFile(tmp)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	return err
}

// syncFile flushes a staged file to the disk. A filesystem that has no such
// operation is not a failed write.
func syncFile(f *os.File) error {
	err := f.Sync()
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return nil
	}
	return err
}

// publish moves one change into place.
func (c *change) publish() error {
	f := c.f
	switch c.kind {
	case write:
		switch info, err := f.Lstat(c.file); {
		case err == nil:
			if info.IsDir() {
				return &fs.PathError{Op: "write", Path: f.shown(c.file), Err: syscall.EISDIR}
			}
			if err := f.Rename(c.file, c.kept); err != nil {
				return err
			}
			c.aside = true
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
		if err := f.Rename(c.next, c.file); err != nil {
			return err
		}
		c.placed = true
	case remove:
		if _, err := f.Lstat(c.path); errors.Is(err, fs.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		if err := f.Rename(c.path, c.kept); err != nil {
			return err
		}
		c.aside = true
	case respell:
		// through another name: a filesystem that folds case may take a
		// rename between two spellings of one name as a rename onto itself
		if err := f.Rename(c.path, c.kept); err != nil {
			return err
		}
		c.aside = true
		if err := f.Rename(c.kept, c.to); err != nil {
			return err
		}
		c.aside, c.placed = false, true
	}
	return nil
}

// putBack undoes publish, as far as it got.
func (c *change) putBack() error {
	f := c.f
	switch c.kind {
	case write:
		if c.aside {
			// over the new file, if it is there
			if err := f.Rename(c.kept, c.file); err != nil {
				return err
			}
			c.aside, c.placed = false, false
		} else if c.placed {
			if err := f.Remove(c.file); err != nil {
				return err
			}
			c.placed = false
		}
	case remove:
		if c.aside {
			if err := f.Rename(c.kept, c.path); err != nil {
				return err
			}
			c.aside = false
		}
	case respell:
		if c.placed {
			if err := f.Rename(c.to, c.kept); err != nil {
				return err
			}
			c.aside, c.placed = true, false
		}
		if c.aside {
			if err := f.Rename(c.kept, c.path); err != nil {
				return err
			}
			c.aside = false
		}
	}
	return nil
}

// plain is err without the temporary name a staged file was written under:
// what failed, said of the file the caller named.
func plain(c *change, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) && c.next != "" && strings.HasSuffix(pe.Path, filepath.Base(c.next)) {
		return &fs.PathError{Op: pe.Op, Path: c.f.shown(c.path), Err: pe.Err}
	}
	if le, ok := errors.AsType[*os.LinkError](err); ok {
		return &fs.PathError{Op: "replace", Path: c.f.shown(c.path), Err: le.Err}
	}
	return err
}

// Commit publishes every change or none.
//
// Every write's new content is staged first, beside the file it replaces, so
// the failures a write meets in practice (a full disk, a size limit, a
// read-only file or directory) happen before anything has changed: what was
// staged is removed and the tree is as it was.
//
// Only then is each change published, in the order it was made. One file
// alone is replaced by a single rename. With more, each original entry is
// renamed aside to a kept name and the staged file renamed into its place;
// if a rename fails, every entry already moved is put back, last first, and
// the error says nothing was changed. When every change is published the
// kept originals are removed.
//
// In a [Txn.Distinct] transaction two writes that reach one file are an
// [AliasError]: found from the paths before anything is staged, or, for two
// names the file system alone takes as one, before the second is published,
// which then puts back the first like any other failure.
//
// An original that will not go back is the one case that ends neither all
// old nor all new: it is left at its kept name, never removed, and the error
// names it.
func (t *Txn) Commit() error {
	if t.done {
		return errors.New("atomicfile: the transaction has already ended")
	}
	defer t.Close()

	// clean removes what is no longer wanted: every staged file still there
	// and every directory staging made (one that took an entry stays).
	clean := func() {
		for _, c := range t.changes {
			if c.next != "" && !c.placed {
				_ = c.f.Remove(c.next)
			}
		}
		for _, m := range slices.Backward(t.made) {
			_ = m.f.Remove(m.name)
		}
	}
	unchanged := func(c *change, err error) error {
		clean()
		return fmt.Errorf("%w; nothing was changed", plain(c, err))
	}

	// resolve, and let a later write to one file replace an earlier one
	last := map[string]*change{}
	var steps []*change
	for _, c := range t.changes {
		if c.kind == write {
			file, err := c.f.resolve(c.path)
			if err != nil {
				return unchanged(c, err)
			}
			c.file = file
			key := filepath.Join(c.f.base, file)
			if prev := last[key]; prev != nil {
				if t.distinct {
					clean()
					return fmt.Errorf("%w; nothing was changed", &AliasError{First: prev.f.shown(prev.path), Second: c.f.shown(c.path)})
				}
				prev.dropped = true
			}
			last[key] = c
		}
	}
	for _, c := range t.changes {
		if !c.dropped {
			steps = append(steps, c)
		}
	}
	for _, c := range steps {
		if c.kind == write {
			if err := t.stage(c); err != nil {
				return unchanged(c, err)
			}
		}
	}

	if len(steps) == 1 && steps[0].kind == write {
		// one file: replaced by one rename, and never absent
		c := steps[0]
		if err := c.f.Rename(c.next, c.file); err != nil {
			return unchanged(c, err)
		}
		return nil
	}

	for _, c := range steps {
		at := c.path
		if c.kind == write {
			at = c.file
		}
		tag := "orig"
		if c.kind == respell {
			tag = "respell"
		}
		kept, err := sibling(c.f, at, tag)
		if err != nil {
			return unchanged(c, err)
		}
		c.kept = kept
	}

	for i, c := range steps {
		var err error
		if t.distinct && c.kind == write {
			err = aliased(steps, i)
		}
		if err == nil {
			err = c.publish()
		}
		if err == nil {
			continue
		}
		var stuck []string
		for j := i; j >= 0; j-- {
			back := steps[j]
			if rerr := back.putBack(); rerr != nil {
				where := "it is left as the commit moved it"
				if back.aside {
					where = "its original is kept at " + back.f.shown(back.kept)
				}
				stuck = append(stuck, fmt.Sprintf("%s (%s: %v)", back.f.shown(back.path), where, rerr))
			}
		}
		clean()
		if len(stuck) > 0 {
			return fmt.Errorf("%w; the change is HALF MOVED: these entries could not be put back as they were: %s", plain(c, err), strings.Join(stuck, ", "))
		}
		return fmt.Errorf("%w; nothing was changed (every entry already moved was put back as it was)", plain(c, err))
	}

	var left []string
	for _, c := range steps {
		if c.aside {
			if err := c.f.RemoveAll(c.kept); err != nil {
				left = append(left, fmt.Sprintf("%s (%v)", c.f.shown(c.kept), err))
			}
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("every change was published, but these kept originals could not be removed: %s", strings.Join(left, ", "))
	}
	return nil
}
