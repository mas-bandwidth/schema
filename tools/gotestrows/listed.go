package main

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testselect"
)

// NO ROW SKIPS (Glenn, 2026-11-25: "NO SKIPS"; "if you don't want to run a
// test, don't run it (i mean at the root)"). plan cuts a split package into
// rows the way it always has, a -run pattern and the earlier patterns to
// leave out; listRows then writes each such row as the tests it runs, by name
// (internal/testselect), so the matrix carries `run` and never `skip`. The
// same step leaves the nightly-tier tests out of every row of a package that
// has one: the per-change rows are the pr group's, and nightly is selected by
// the nightly lane.

// lister answers the -run pattern naming every test of pkgs that run matches
// (empty: all) and not does not (empty: none), less nightly-tier tests.
type lister func(pkgs []string, run, not string) (string, error)

// listRows rewrites every row with a Skip, and every row holding a package
// in nightly, as the tests it runs by name; a subsplit parent's complement
// becomes the child names its buckets leave (subtestComplement).
func listRows(rows []Row, nightly map[string]bool, list lister) ([]Row, error) {
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if r.Make != "" {
			out = append(out, r)
			continue
		}
		pkgs := strings.Fields(r.Packages)
		hasNightly := false
		for _, p := range pkgs {
			if nightly[p] {
				hasNightly = true
			}
		}
		switch {
		case strings.Contains(r.Run, "/"):
			// a subsplit bucket names its parent and its children; a ride is
			// a top-level test, never the parent
			r.Skip = ""
		case strings.Contains(r.Skip, "/"):
			parent, children, ok := strings.Cut(r.Skip, "/")
			if !ok || parent != r.Run || !strings.HasPrefix(children, "(") || !strings.HasSuffix(children, ")") {
				return nil, fmt.Errorf("row %+v: a child complement is <parent>/(<child>|...)", r)
			}
			comp, err := subtestComplement(strings.Split(children[1:len(children)-1], "|"))
			if err != nil {
				return nil, err
			}
			r.Run, r.Skip = parent+"/"+comp, ""
		case r.Skip != "" || hasNightly:
			run, err := list(pkgs, r.Run, r.Skip)
			if err != nil {
				return nil, fmt.Errorf("row %s: %w", r.Packages, err)
			}
			r.Run, r.Skip = run, ""
		}
		out = append(out, r)
	}
	return out, nil
}

// childBucket is a subsplit child the complement can be listed for:
// ^<prefix>[0-9]*[<digits>]$, a bucket of numbered seeds by last digit.
var childBucket = regexp.MustCompile(`^\^([^\\\[\](){}.*+?^$|]+)\[0-9\]\*\[([0-9]+)\]\$$`)

// subtestComplement is the one-level pattern naming every child name the
// buckets do not: a name that does not start with the prefix, the prefix
// alone, the prefix and digits then anything else, and the prefix and digits
// ending in a digit no bucket names.
func subtestComplement(children []string) (string, error) {
	prefix := ""
	used := map[byte]bool{}
	for _, c := range children {
		m := childBucket.FindStringSubmatch(c)
		if m == nil {
			return "", fmt.Errorf("the subsplit child %q is not ^<prefix>[0-9]*[<digits>]$, so the names it leaves cannot be listed", c)
		}
		if prefix != "" && m[1] != prefix {
			return "", fmt.Errorf("the subsplit children name two prefixes, %q and %q", prefix, m[1])
		}
		prefix = m[1]
		for i := 0; i < len(m[2]); i++ {
			if used[m[2][i]] {
				return "", fmt.Errorf("the digit %c is in two subsplit buckets", m[2][i])
			}
			used[m[2][i]] = true
		}
	}
	var alts []string
	for i := 0; i < len(prefix); i++ {
		alts = append(alts, regexp.QuoteMeta(prefix[:i])+"($|[^"+regexp.QuoteMeta(prefix[i:i+1])+"])")
	}
	p := regexp.QuoteMeta(prefix)
	alts = append(alts, p+"$", p+"[0-9]*[^0-9]")
	var rest string
	for d := byte('0'); d <= '9'; d++ {
		if !used[d] {
			rest += string(d)
		}
	}
	if rest != "" {
		alts = append(alts, p+"[0-9]*["+rest+"]$")
	}
	return "^(" + strings.Join(alts, "|") + ")", nil
}

// selectLister lists from the checkout at root, under this host's build
// constraints, the way the row will run.
func selectLister(root string) lister {
	compile := func(s string) *regexp.Regexp {
		if s == "" {
			return nil
		}
		return regexp.MustCompile(s)
	}
	return func(pkgs []string, run, not string) (string, error) {
		dirs := make([]string, len(pkgs))
		for i, p := range pkgs {
			dirs[i] = filepath.Join(root, filepath.FromSlash(p))
		}
		return testselect.Select(build.Default, root, dirs, compile(run), compile(not), false)
	}
}

// nightlyPackages are the packages, as ./<dir>, holding a nightly-tier test.
func nightlyPackages(root string) (map[string]bool, error) {
	tests, err := testselect.Nightly(root)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for place := range tests {
		dir, _, _ := strings.Cut(place, ":")
		out["./"+dir] = true
	}
	return out, nil
}

// moduleRoot is the directory, at or above this one, whose go.mod is the
// main module's: the package paths ./<dir> are relative to it.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && strings.HasPrefix(string(b), "module "+modulePath+"\n") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod of %s at or above this directory", modulePath)
		}
		dir = parent
	}
}
