package main

import (
	"bytes"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v3/internal/modulemove"
)

// The pin directive an adopter writes in each package's generate.go; the Go
// adoption notes in docs/USAGE.md bless this one spelling. The module path in
// it carries the release's major version (Go's semantic import versioning:
// `schema` for v0 and v1, `schema/vN` from v2 on), so a pin line is matched
// under any major and rewritten under the major of the version it moves to.
// The path is assembled from parts so the module-path move program
// (tools/modpath) leaves these spellings alone: they name every major, not
// this module's own.
const (
	pinDirectiveHead = "//go:generate go run "
	pinModuleBase    = "github.com/mas-bandwidth/schema"
	pinCommandTail   = "/cmd/schema@"
)

// pinMainModule is this build's own module path: the path its go.mod names,
// and so the path main resolves under. The refusal of a tag below the module
// move points at it (`go install <it>/cmd/schema@main`). It is written whole,
// unlike the spellings above, so the module-path move program DOES move it:
// #4445 turns it to /v3 with go.mod.
const pinMainModule = "github.com/mas-bandwidth/schema/v3"

// pinReleases is where the prebuilt binaries of every release are.
const pinReleases = "https://github.com/mas-bandwidth/schema/releases"

var pinLine = regexp.MustCompile(`^` + regexp.QuoteMeta(pinDirectiveHead+pinModuleBase) + `(/v[0-9]+)?` + regexp.QuoteMeta(pinCommandTail))

// pinTarget decides a pin: the module path the pin line takes for version,
// or the refusal. boundary is the first v3 tag whose go.mod names /v3 (""
// while there is none) and ownModule the module path main resolves under,
// which the refusal names; cmdPin passes modulemove.FirstV3ModuleTag and
// pinMainModule, and the tests pass their own, so the rule is checked on both
// sides of the module move.
//
// A pseudo-version never counts as below the boundary: it is a commit, whose
// go.mod pin cannot see offline. A v3 TAG below the boundary is refused: it
// was cut while go.mod still named /v2, so neither path resolves it (with no
// boundary, that is every v3 tag).
func pinTarget(version, boundary, ownModule string) (string, error) {
	// One grammar, Go's own (modulemove.ValidVersion), before any file is
	// read: a Go pseudo-version is also a semver version, so it covers both,
	// and refusing anything else by name keeps a typo from rewriting every
	// pin to a version that cannot resolve.
	if !modulemove.ValidVersion(version) {
		return "", fmt.Errorf("schema pin: %s is not a Go pseudo-version or semver tag", version)
	}
	// The boundary is held against the CANONICAL release, the version with
	// any build metadata cut, because Go resolves v3.0.2+incompatible to the
	// v3.0.2 tag. Build metadata is refused outright as well: Go accepts none
	// but +incompatible, which names a module with no go.mod at its major,
	// and every schema tag has one, so no spelling with a + resolves.
	canonical, meta, _ := strings.Cut(version, "+")
	// A version in the shape of a pseudo-version is one Go reads as a commit,
	// and resolves only when it is a lawful one: a real time, a base version
	// that exists, a canonical revision. All of that is checked offline.
	if err := modulemove.CheckPseudoVersion(canonical); err != nil {
		return "", fmt.Errorf("schema pin: %s is not a Go pseudo-version or semver tag: %w", version, err)
	}
	if modulemove.PreModuleMove(canonical, boundary) {
		return "", fmt.Errorf("schema pin: Go cannot resolve %s: the %s tag was cut before the module path moved to /v3, so its go.mod names /v2 and neither path finds it; use a prebuilt binary from %s, or `go install %s/cmd/schema@main`", version, canonical, pinReleases, ownModule)
	}
	if meta != "" {
		return "", fmt.Errorf("schema pin: Go cannot resolve %s: a module version takes no build metadata but +incompatible, and schema has a go.mod at every major; pin %s", version, canonical)
	}
	return modulemove.PathFor(version), nil
}

// splitPinArgs takes the flag out of the argument list wherever it sits:
// `schema pin v2.8.0 a b c --mixed-ok` is the same command as the flag-first
// spelling the usage line shows, because adopters type it both ways.
func splitPinArgs(args []string) (pos []string, mixedOk bool) {
	for _, arg := range args {
		switch {
		case arg == "--mixed-ok" || arg == "-mixed-ok":
			mixedOk = true
		case strings.HasPrefix(arg, "--mixed-ok=") || strings.HasPrefix(arg, "-mixed-ok="):
			switch arg[strings.IndexByte(arg, '=')+1:] {
			case "true":
				mixedOk = true
			case "false":
				mixedOk = false
			default:
				fatalf("schema pin: --mixed-ok takes true or false")
			}
		default:
			pos = append(pos, arg)
		}
	}
	return pos, mixedOk
}

// cmdPin is `schema pin <version> <dir>...`: under each dir, for every
// generate.go file, it rewrites each line that pinLine matches to run the
// compiler at <version> under the module path that version's major requires,
// and changes nothing else. It
// refuses by name a version that is not a Go pseudo-version or a
// semver tag, a tag Go cannot resolve (every v3 tag cut before the /v3 module
// move, internal/modulemove; any build metadata), a directory that
// does not exist or holds no pin line, and a tree whose pins already differ
// from each other unless --mixed-ok is given. It prints one line per file
// changed and exits 0, or exits 1 having changed nothing: on a refusal no
// file is written at all, and a write that fails part way puts back every
// file already moved, the original entry itself (publishPins), so the tree
// ends all old or all new. A generate.go that is a symbolic link is
// followed: the file it names is the one moved, and the link stays. No other
// file is touched.
func cmdPin(args []string) {
	pos, mixedOk := splitPinArgs(args)
	if len(pos) < 1 {
		fatalf("schema pin needs a version: schema pin <version> <dir>...")
	}
	changed, err := runPin(osPinFiles, pos[0], pos[1:], mixedOk, modulemove.FirstV3ModuleTag, pinMainModule)
	if err != nil {
		fatalf("%v", err)
	}
	for _, path := range changed {
		fmt.Println(path)
	}
}

// runPin is the whole command behind its arguments: the pin of dirs to
// version, with the file operations, the boundary and the build's own module
// path passed in. It returns the files it moved, or the refusal, which is the
// line the command prints. cmdPin passes the real ones; the committed corpus
// (internal/modulemove/pincorpus) runs every adopter tree through here with
// its own, in process.
func runPin(f pinFiles, version string, dirs []string, mixedOk bool, boundary, ownModule string) ([]string, error) {
	modulePath, err := pinTarget(version, boundary, ownModule)
	if err != nil {
		return nil, err
	}
	newPrefix := pinDirectiveHead + modulePath + pinCommandTail
	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	var changes []pinChange
	var noPin []string
	versions := map[string]bool{}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("schema pin: %s: %w", dir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("schema pin: %s is not a directory", dir)
		}
		pins := 0
		werr := filepath.WalkDir(dir, func(path string, d iofs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() || d.Name() != "generate.go" {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			info, rerr := os.Stat(path)
			if rerr != nil {
				return rerr
			}
			lines := strings.Split(string(data), "\n")
			out := make([]string, 0, len(lines))
			for _, ln := range lines {
				if loc := pinLine.FindStringIndex(ln); loc != nil {
					rest := ln[loc[1]:]
					end := strings.IndexAny(rest, " \t\r")
					old := rest
					tail := ""
					if end >= 0 {
						old = rest[:end]
						tail = rest[end:]
					}
					// the old pin is named with its path, so a tree that
					// already disagrees on the major is a mixed tree too
					versions[ln[len(pinDirectiveHead):loc[1]-len(pinCommandTail)]+"@"+old] = true
					pins++
					out = append(out, newPrefix+version+tail)
				} else {
					out = append(out, ln)
				}
			}
			next := []byte(strings.Join(out, "\n"))
			if !bytes.Equal(next, data) {
				changes = append(changes, pinChange{path: path, orig: data, next: next, mode: info.Mode().Perm()})
			}
			return nil
		})
		if werr != nil {
			return nil, fmt.Errorf("schema pin: %w", werr)
		}
		if pins == 0 {
			noPin = append(noPin, dir)
		}
	}

	if len(noPin) > 0 {
		return nil, fmt.Errorf("schema pin: no go:generate pin to move under %s", strings.Join(noPin, ", "))
	}
	if len(versions) > 1 && !mixedOk {
		names := make([]string, 0, len(versions))
		for v := range versions {
			names = append(names, v)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("schema pin: the tree's pins already differ from each other (%s); pass --mixed-ok to move them together", strings.Join(names, ", "))
	}

	if err := publishPins(f, changes); err != nil {
		return nil, fmt.Errorf("schema pin: %w", err)
	}
	changed := make([]string, 0, len(changes))
	for _, ch := range changes {
		changed = append(changed, ch.path)
	}
	return changed, nil
}

// pinChange is one generate.go to move: the bytes it held, the bytes it
// takes, and the mode it keeps.
type pinChange struct {
	path string
	orig []byte
	next []byte
	mode os.FileMode
}

// pinFiles is the two file operations a pin can fail in, a seam so the tests
// fail each one at a chosen file without a real read-only or full disk.
type pinFiles struct {
	createTemp func(dir, pattern string) (*os.File, error)
	rename     func(oldpath, newpath string) error
}

var osPinFiles = pinFiles{createTemp: os.CreateTemp, rename: os.Rename}

// stage writes data to a new file beside path, with the given mode, and
// returns its name; on a failure that file is gone.
func (f pinFiles) stage(path, kind string, data []byte, mode os.FileMode) (string, error) {
	tmp, err := f.createTemp(filepath.Dir(path), "."+filepath.Base(path)+"."+kind+".*")
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

// pinEntry is one file on its way through publishPins.
type pinEntry struct {
	path string // as the adopter named it
	file string // the file itself: path with every symbolic link followed
	next string // the staged new content, beside file
	kept string // where the original entry is kept while the pin is published
	// aside: the original entry is at kept, not at file
	aside bool
}

// publishPins moves every file or none.
//
// A generate.go that is a symbolic link is followed: the file it names is
// the one moved, and the link is never replaced. Two paths that reach one
// file move it once.
//
// Every file's new content is staged first, beside the file it replaces,
// with a name reserved for keeping the original, so the failures a pin meets
// in practice (a read-only directory, a full disk) happen before anything has
// changed: what was staged is removed and the tree is as it was.
//
// Only then is each file published: the original ENTRY is renamed aside to
// its kept name and the staged file renamed into its place. If either rename
// fails, every original already moved aside is renamed back over its path, so
// what returns is the entry itself (the same inode, with its hard links, its
// mode and the time it was last written), not a copy of its bytes, and
// "nothing was changed" is true. When every file is published the kept
// originals are removed.
//
// An original that will not go back is the one case that ends neither all
// old nor all new: it is left at its kept name, never removed, and the error
// names it.
func publishPins(f pinFiles, changes []pinChange) error {
	entries := make([]*pinEntry, 0, len(changes))
	// clean removes what is no longer wanted: every staged file still there,
	// and every kept name that does not hold an original.
	clean := func() {
		for _, e := range entries {
			_ = os.Remove(e.next)
			if !e.aside {
				_ = os.Remove(e.kept)
			}
		}
	}
	seen := map[string]bool{}
	for _, ch := range changes {
		file, err := filepath.EvalSymlinks(ch.path)
		if err != nil {
			clean()
			return fmt.Errorf("nothing was changed: %w", err)
		}
		if seen[file] {
			continue
		}
		seen[file] = true
		e := &pinEntry{path: ch.path, file: file}
		if e.next, err = f.stage(file, "new", ch.next, ch.mode); err == nil {
			entries = append(entries, e)
			e.kept, err = f.stage(file, "orig", nil, ch.mode)
		}
		if err != nil {
			clean()
			return fmt.Errorf("nothing was changed: %w", err)
		}
	}

	for _, e := range entries {
		err := f.rename(e.file, e.kept)
		if err == nil {
			e.aside = true
			err = f.rename(e.next, e.file)
		}
		if err == nil {
			continue
		}
		var stuck []string
		for _, back := range entries {
			if !back.aside {
				continue
			}
			if rerr := f.rename(back.kept, back.file); rerr != nil {
				stuck = append(stuck, fmt.Sprintf("%s (its original is kept at %s: %v)", back.path, back.kept, rerr))
				continue
			}
			back.aside = false
			back.kept = "" // the name is the original's own again
		}
		clean()
		if len(stuck) > 0 {
			return fmt.Errorf("%w; the pin is HALF MOVED: these files could not be put back as they were: %s", err, strings.Join(stuck, ", "))
		}
		return fmt.Errorf("nothing was changed (every file already moved was put back as it was): %w", err)
	}

	var left []string
	for _, e := range entries {
		if err := os.Remove(e.kept); err != nil {
			left = append(left, fmt.Sprintf("%s (%v)", e.kept, err))
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("every pin moved, but these kept originals could not be removed: %s", strings.Join(left, ", "))
	}
	return nil
}
