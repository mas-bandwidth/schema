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

	"github.com/mas-bandwidth/schema/v2/internal/modulemove"
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
const pinMainModule = "github.com/mas-bandwidth/schema/v2"

// pinReleases is where the prebuilt binaries of every release are.
const pinReleases = "https://github.com/mas-bandwidth/schema/releases"

var pinLine = regexp.MustCompile(`^` + regexp.QuoteMeta(pinDirectiveHead+pinModuleBase) + `(/v[0-9]+)?` + regexp.QuoteMeta(pinCommandTail))

// A Go pseudo-version (the `v...-yyyymmddhhmmss-<12 hex>` form) is also a
// semver tag, so one grammar covers both; refusing anything else by name keeps
// a typo from silently rewriting every pin to a version that cannot resolve.
var pinSemver = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)((-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)|(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$`)

func validPinVersion(v string) bool {
	return pinSemver.MatchString(v) || modulemove.IsPseudoVersion(v)
}

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
	if !validPinVersion(version) {
		return "", fmt.Errorf("schema pin: %s is not a Go pseudo-version or semver tag", version)
	}
	// The boundary is held against the CANONICAL release, the version with
	// any build metadata cut, because Go resolves v3.0.2+incompatible to the
	// v3.0.2 tag. Build metadata is refused outright as well: Go accepts none
	// but +incompatible, which names a module with no go.mod at its major,
	// and every schema tag has one, so no spelling with a + resolves.
	canonical, meta, _ := strings.Cut(version, "+")
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
// file already moved (publishPins), so the tree ends all old or all new. No
// other file is touched.
func cmdPin(args []string) {
	pos, mixedOk := splitPinArgs(args)
	if len(pos) < 1 {
		fatalf("schema pin needs a version: schema pin <version> <dir>...")
	}
	version := pos[0]
	modulePath, err := pinTarget(version, modulemove.FirstV3ModuleTag, pinMainModule)
	if err != nil {
		fatalf("%v", err)
	}
	newPrefix := pinDirectiveHead + modulePath + pinCommandTail
	dirs := pos[1:]
	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	var changes []pinChange
	var noPin []string
	versions := map[string]bool{}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			fatalf("schema pin: %s: %v", dir, err)
		}
		if !info.IsDir() {
			fatalf("schema pin: %s is not a directory", dir)
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
			fatalf("schema pin: %v", werr)
		}
		if pins == 0 {
			noPin = append(noPin, dir)
		}
	}

	if len(noPin) > 0 {
		fatalf("schema pin: no go:generate pin to move under %s", strings.Join(noPin, ", "))
	}
	if len(versions) > 1 && !mixedOk {
		names := make([]string, 0, len(versions))
		for v := range versions {
			names = append(names, v)
		}
		sort.Strings(names)
		fatalf("schema pin: the tree's pins already differ from each other (%s); pass --mixed-ok to move them together", strings.Join(names, ", "))
	}

	if err := publishPins(osPinFiles, changes); err != nil {
		fatalf("schema pin: %v", err)
	}
	for _, ch := range changes {
		fmt.Println(ch.path)
	}
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

// stage writes data to a temporary beside path, with path's mode, and
// returns its name; on a failure the temporary is gone.
func (f pinFiles) stage(path string, data []byte, mode os.FileMode) (string, error) {
	tmp, err := f.createTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
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

// restore puts a published file's original bytes back: staged and renamed
// like any write, and written in place if that cannot be done.
func (f pinFiles) restore(ch pinChange) error {
	name, err := f.stage(ch.path, ch.orig, ch.mode)
	if err == nil {
		if err = f.rename(name, ch.path); err == nil {
			return nil
		}
		_ = os.Remove(name)
	}
	return os.WriteFile(ch.path, ch.orig, ch.mode)
}

// publishPins moves every file or none. Every file's new content is staged
// first, beside the file it replaces, so the failures a pin meets in practice
// (a read-only directory, a full disk) happen before any file has changed:
// the temporaries are removed and the tree is as it was. Only then is each
// staged file renamed over its original; if a rename fails, the files already
// renamed are put back from the original bytes and the rest are never
// touched. A put-back that itself fails is named in the error, with the files
// left at the new pin, because nothing more can be done about it here.
func publishPins(f pinFiles, changes []pinChange) error {
	staged := make([]string, 0, len(changes))
	drop := func(names []string) {
		for _, name := range names {
			_ = os.Remove(name)
		}
	}
	for _, ch := range changes {
		name, err := f.stage(ch.path, ch.next, ch.mode)
		if err != nil {
			drop(staged)
			return fmt.Errorf("nothing was changed: %w", err)
		}
		staged = append(staged, name)
	}
	for i, ch := range changes {
		err := f.rename(staged[i], ch.path)
		if err == nil {
			continue
		}
		drop(staged[i:])
		var stuck []string
		for _, done := range changes[:i] {
			if rerr := f.restore(done); rerr != nil {
				stuck = append(stuck, fmt.Sprintf("%s (%v)", done.path, rerr))
			}
		}
		if len(stuck) > 0 {
			return fmt.Errorf("%w; the pin is HALF MOVED: these files hold the new pin and could not be put back: %s", err, strings.Join(stuck, ", "))
		}
		return fmt.Errorf("nothing was changed (every file already moved was put back): %w", err)
	}
	return nil
}
