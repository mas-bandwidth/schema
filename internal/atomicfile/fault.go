package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrInjected is the failure a [Fault] returns.
var ErrInjected = errors.New("injected write failure")

// Fault is a write failure a test plants under one directory, so a command
// in any package can be failed at a chosen point of its publish without a
// real full disk: every transaction consults the planted faults. Outside
// the tests none is planted and nothing is consulted.
type Fault struct {
	dir string
	// limit: a staged file under dir longer than this many bytes takes that
	// many and then fails, as a file-size limit does; -1 for no limit
	limit int
	// nth: the nth rename under dir fails; 0 for none
	nth int

	mu      sync.Mutex
	renames int
	fired   int
}

var faults struct {
	mu   sync.Mutex
	list []*Fault
}

func plant(dir string, limit, nth int) *Fault {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	f := &Fault{dir: dir, limit: limit, nth: nth}
	faults.mu.Lock()
	faults.list = append(faults.list, f)
	faults.mu.Unlock()
	return f
}

// FailWritesAfter plants a fault: a staged file under dir that is longer than
// n bytes takes n bytes and then fails.
func FailWritesAfter(dir string, n int) *Fault { return plant(dir, n, 0) }

// FailNthRename plants a fault: the nth rename (from 1) of an entry under
// dir fails. Renames after it succeed, the ones that put entries back
// included.
func FailNthRename(dir string, nth int) *Fault { return plant(dir, -1, nth) }

// Remove takes the fault away.
func (f *Fault) Remove() {
	faults.mu.Lock()
	defer faults.mu.Unlock()
	for i, g := range faults.list {
		if g == f {
			faults.list = append(faults.list[:i], faults.list[i+1:]...)
			return
		}
	}
}

// Fired is how many operations the fault failed.
func (f *Fault) Fired() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fired
}

// Renames is how many renames under the fault's directory were asked for.
func (f *Fault) Renames() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renames
}

func planted() []*Fault {
	faults.mu.Lock()
	defer faults.mu.Unlock()
	return append([]*Fault(nil), faults.list...)
}

// covers reports whether the named entry is under the fault's directory.
func (f *Fault) covers(base, name string) bool {
	p := filepath.Join(base, name)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	dir, leaf := filepath.Split(p)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		p = filepath.Join(real, leaf)
	}
	return strings.HasPrefix(p, f.dir+string(filepath.Separator))
}

// faulted is f with the planted faults in front of Write and Rename.
func (f Files) faulted() Files {
	if len(planted()) == 0 {
		return f
	}
	g := f
	g.Write = func(file *os.File, name string, data []byte) error {
		for _, ft := range planted() {
			if ft.limit < 0 || len(data) <= ft.limit || !ft.covers(f.base, name) {
				continue
			}
			ft.mu.Lock()
			ft.fired++
			ft.mu.Unlock()
			if err := f.Write(file, name, data[:ft.limit]); err != nil {
				return err
			}
			return &fs.PathError{Op: "write", Path: f.shown(name), Err: ErrInjected}
		}
		return f.Write(file, name, data)
	}
	g.Rename = func(oldname, newname string) error {
		for _, ft := range planted() {
			if ft.nth <= 0 || !ft.covers(f.base, oldname) && !ft.covers(f.base, newname) {
				continue
			}
			ft.mu.Lock()
			ft.renames++
			fail := ft.renames == ft.nth
			if fail {
				ft.fired++
			}
			ft.mu.Unlock()
			if fail {
				return &os.LinkError{Op: "rename", Old: f.shown(oldname), New: f.shown(newname), Err: ErrInjected}
			}
		}
		return f.Rename(oldname, newname)
	}
	return g
}
