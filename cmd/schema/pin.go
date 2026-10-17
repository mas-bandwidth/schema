package main

import (
	"bytes"
	"flag"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The pin directive an adopter writes in each package's generate.go; the Go
// adoption notes in docs/USAGE.md bless this one spelling.
const pinPrefix = "//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@"

// A Go pseudo-version (the `v...-yyyymmddhhmmss-<12 hex>` form) is also a
// semver tag, so one grammar covers both; refusing anything else by name keeps
// a typo from silently rewriting every pin to a version that cannot resolve.
var (
	pinSemver = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)((-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)|(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$`)
	pinPseudo = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-(0\.)?[0-9]{14}-[0-9a-f]{12}$`)
)

func validPinVersion(v string) bool {
	return pinSemver.MatchString(v) || pinPseudo.MatchString(v)
}

// cmdPin is `schema pin <version> <dir>...`: under each dir, for every
// generate.go file, it rewrites the version in each line that starts with
// pinPrefix to <version> and changes nothing else. It refuses by name a
// version that is not a Go pseudo-version or semver tag, a directory that
// does not exist, and a tree whose pins already differ from each other unless
// --mixed-ok is given. It prints one line per file changed and exits 0, or
// exits 1 having changed nothing on a refusal. No other file is touched.
func cmdPin(args []string) {
	fs := flag.NewFlagSet("pin", flag.ExitOnError)
	mixedOk := fs.Bool("mixed-ok", false, "move a tree whose pins already differ from each other")
	_ = fs.Parse(args)
	pos := fs.Args()
	if len(pos) < 1 {
		fatalf("schema pin needs a version: schema pin <version> <dir>...")
	}
	version := pos[0]
	if !validPinVersion(version) {
		fatalf("schema pin: %s is not a Go pseudo-version or semver tag", version)
	}
	dirs := pos[1:]
	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	type change struct {
		path string
		orig []byte
		next []byte
	}
	var changes []change
	versions := map[string]bool{}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			fatalf("schema pin: %s: %v", dir, err)
		}
		if !info.IsDir() {
			fatalf("schema pin: %s is not a directory", dir)
		}
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
			lines := strings.Split(string(data), "\n")
			out := make([]string, 0, len(lines))
			for _, ln := range lines {
				if strings.HasPrefix(ln, pinPrefix) {
					rest := ln[len(pinPrefix):]
					end := strings.IndexAny(rest, " \t")
					old := rest
					tail := ""
					if end >= 0 {
						old = rest[:end]
						tail = rest[end:]
					}
					versions[old] = true
					out = append(out, pinPrefix+version+tail)
				} else {
					out = append(out, ln)
				}
			}
			next := []byte(strings.Join(out, "\n"))
			if !bytes.Equal(next, data) {
				changes = append(changes, change{path: path, orig: data, next: next})
			}
			return nil
		})
		if werr != nil {
			fatalf("schema pin: %v", werr)
		}
	}

	if len(versions) > 1 && !*mixedOk {
		names := make([]string, 0, len(versions))
		for v := range versions {
			names = append(names, v)
		}
		sort.Strings(names)
		fatalf("schema pin: the tree's pins already differ from each other (%s); pass --mixed-ok to move them together", strings.Join(names, ", "))
	}

	for _, ch := range changes {
		if err := writeAtomic(ch.path, ch.next); err != nil {
			fatalf("schema pin: %v", err)
		}
		fmt.Println(ch.path)
	}
}
