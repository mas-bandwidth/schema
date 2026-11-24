// Package legpaths says which legs a changed path belongs to: the one source
// ci-fast.yml's plan (and ci-full.yml's pr-select) read the path-to-leg
// selection from, in place of a hand-written `case` per leg.
//
// A LEG'S PATH SET IS DERIVED, NEVER COPIED. The legs are
// inputfamilies.Legs, and each leg's paths come from three places:
//
//   - THE TREE'S CONVENTIONS, written once with the leg as a parameter: its
//     codegen packages, its generated trees, its test, bench and card
//     directories (Conventions).
//   - THE FILES UNDER make/ THAT NAME IT: a make file, check script, allow
//     list or negative-control record whose name carries the leg as a word
//     (make/moo-fuzz.mk, make/schema-dart-nightly-fuzz.mk,
//     make/negative-controls.d/cards-python-hash-negative-control.json).
//   - WHAT THE LEG'S OWN MAKE FILES READ: every repository file, directory or
//     glob a recipe or prerequisite of those make files names literally
//     (tools/moofuzzhost/main.cpp in make/moo-fuzz.mk). A comment is not a
//     read and is dropped first.
//
// So a new make file, a new control or a new tool a leg's targets read
// selects that leg the day it lands, with no edit here. Load reads the make
// files once; Legs answers per path.
package legpaths

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/inputfamilies"
)

// codegenDir names a leg's type-surface codegen package where it is not the
// leg's own name; the table surface is always <leg>table.
var codegenDir = map[string]string{"go": "golang", "cs": "csharp"}

// extra are the paths a leg reads that no make file of its own names: the
// root Makefile builds them (the C++ reference's test mains, the Dart
// handoff) or they are shared emitter packages. A pattern ending in "/" is a
// subtree, any other a path.Match glob.
var extra = []struct {
	pattern string
	legs    []string
}{
	{"internal/codegen/ccommon/", []string{"c", "cpp"}},
	{"internal/codegen/cfmt/", []string{"c"}},
	{"test/tables/*.cpp", []string{"cpp"}},
	{"test/tables/*.h", []string{"cpp"}},
	{".clang-format", []string{"cpp"}},
	{"make/conformance-handoff.py", []string{"dart"}},
	// the C leg's clean gate runs this Go program by file (make/c.mk), and Go
	// files are never read as a leg's (resolve)
	{"tools/sabotage/c_clean.go", []string{"c"}},
}

// Conventions are the tree's per-leg path prefixes, with {leg} for the leg
// and {cg} for its type-surface codegen directory. A prefix ending in "-"
// matches the leg's suffixed siblings (test/go-tables/, generated/cpp-x/).
var Conventions = []string{
	"internal/codegen/{cg}/",
	"internal/codegen/{leg}table/",
	"generated/{leg}/",
	"generated/{leg}-",
	"generated/bench/{leg}/",
	"generated/bench/{leg}-",
	"generated/bench/tables/{leg}/",
	"generated/bench/paired/{leg}/",
	"bench/{leg}/",
	"bench/tables/{leg}/",
	"tables/cards/{leg}/",
	"test/{leg}/",
	"test/{leg}-",
	"test/conformance/{leg}/",
	"test/packet-*/{leg}/",
}

// ref is one path a leg's make files read.
type ref struct {
	pattern string // a file, a directory with its trailing "/", or a glob
	glob    bool
	legs    []string
	from    string // the make file that names it
}

// Index is the derived selection over one tree.
type Index struct {
	refs []ref
	// files is every make file Load read, with the legs its name gives
	files map[string][]string
}

// Owners is the legs a file under make/ belongs to by its name: every word
// of its path below make/ (split on - . _ /, the extension dropped) that is
// a leg.
func Owners(rel string) []string {
	rest, ok := strings.CutPrefix(rel, "make/")
	if !ok {
		return nil
	}
	rest = strings.TrimSuffix(rest, path.Ext(rest))
	var out []string
	for _, w := range strings.FieldsFunc(rest, func(r rune) bool { return r == '-' || r == '.' || r == '_' || r == '/' }) {
		if slices.Contains(inputfamilies.Legs, w) && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// Load reads every make file under root/make and indexes what the make files
// a leg owns read.
func Load(root string) (*Index, error) {
	ix := &Index{files: map[string][]string{}}
	err := filepath.WalkDir(filepath.Join(root, "make"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".mk") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		legs := Owners(rel)
		ix.files[rel] = legs
		if len(legs) == 0 {
			return nil
		}
		toks, err := readTokens(p)
		if err != nil {
			return err
		}
		for _, tok := range toks {
			r, ok := resolve(root, tok)
			if !ok {
				continue
			}
			r.legs, r.from = legs, rel
			ix.refs = append(ix.refs, r)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("legpaths: %w", err)
	}
	if len(ix.files) == 0 {
		return nil, fmt.Errorf("legpaths: no make file under %s/make", root)
	}
	return ix, nil
}

// readTokens is every word of a make file outside its comments, split where
// a path cannot continue.
func readTokens(p string) ([]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // errdiscard: read-only; a read error is sc.Err's
	var toks []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		// a make comment runs from any #; a recipe line (tab-led) goes to the
		// shell, whose comment is a # that starts a word
		if !strings.HasPrefix(line, "\t") {
			if i := strings.IndexByte(line, '#'); i >= 0 {
				line = line[:i]
			}
		} else {
			for i := 0; i < len(line); i++ {
				if line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t' || line[i-1] == '@') {
					line = line[:i]
					break
				}
			}
		}
		toks = append(toks, strings.FieldsFunc(line, func(r rune) bool {
			return strings.ContainsRune(" \t\"'`(),;:=|<>{}\\", r)
		})...)
	}
	return toks, sc.Err()
}

// resolve turns one make word into a path the leg reads: an existing file, an
// existing directory two or more levels deep, or a glob below a directory.
// Generated output (build/, dist/), variables and pattern rules are not
// repository paths.
func resolve(root, tok string) (ref, bool) {
	tok = strings.TrimPrefix(tok, "./")
	tok = strings.TrimRight(tok, ".")
	// GO SOURCE AND PAGES ARE NOT A LEG'S FILES. A .go file a recipe names
	// (`go test ./x/y_test.go`, a `go run` script) is a build input of the
	// module, which go-test-touched and its dependents cover; a page (.md) a
	// doc gate reads is held by the doc packages (./internal/docgate,
	// ./compiler). Selecting every leg that names them would put a leg's
	// verify rows on every compiler or page edit.
	if strings.HasSuffix(tok, ".go") || strings.HasSuffix(tok, ".md") {
		return ref{}, false
	}
	if tok == "" || strings.ContainsAny(tok, "$%") || strings.HasPrefix(tok, "/") || strings.HasPrefix(tok, "-") ||
		strings.Contains(tok, "..") || strings.HasPrefix(tok, "build/") || strings.HasPrefix(tok, "dist/") {
		return ref{}, false
	}
	if strings.ContainsAny(tok, "*?[") {
		dir, _, _ := strings.Cut(tok, "*")
		if _, err := path.Match(tok, ""); err != nil || !strings.Contains(dir, "/") {
			return ref{}, false
		}
		return ref{pattern: tok, glob: true}, true
	}
	st, err := os.Stat(filepath.Join(root, filepath.FromSlash(tok)))
	if err != nil {
		return ref{}, false
	}
	if st.IsDir() {
		tok = strings.TrimSuffix(tok, "/")
		if strings.Count(tok, "/") < 1 {
			return ref{}, false // a top-level directory is every leg's, not one's
		}
		// A GO PACKAGE (OR A TREE OF THEM) IS A BUILD INPUT, NOT A LEG'S FILE: `go build ./cmd/schema`
		// or `go test ./internal/x` in a leg's recipe reads the whole module
		// through it, and a change there is go-test-touched's (with its
		// dependents), never every leg that builds the compiler.
		if holdsGo(filepath.Join(root, filepath.FromSlash(tok))) {
			return ref{}, false
		}
		return ref{pattern: tok + "/"}, true
	}
	if !strings.Contains(tok, "/") {
		// a root file (go.mod, Makefile, LICENSE) is every leg's: the broad
		// rule's, never one leg's
		return ref{}, false
	}
	return ref{pattern: tok}, true
}

// holdsGo reports whether a .go file sits in dir or below it.
func holdsGo(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error { // errdiscard: an unreadable subtree is not Go
		if err != nil || found {
			return fs.SkipAll
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// conventionMatch reports whether p sits under the leg's convention prefix.
func conventionMatch(prefix, p string) bool {
	if strings.Contains(prefix, "*") {
		// test/packet-*/{leg}/: one directory level stands for the star
		head, tail, _ := strings.Cut(prefix, "*")
		if !strings.HasPrefix(p, head) {
			return false
		}
		rest := p[len(head):]
		i := strings.IndexByte(rest, '/')
		return i >= 0 && strings.HasPrefix(rest[i:], tail)
	}
	return strings.HasPrefix(p, prefix)
}

// Legs is every leg the changed path p belongs to, in inputfamilies.Legs
// order.
func (ix *Index) Legs(p string) []string {
	set := map[string]bool{}
	for _, leg := range Owners(p) {
		set[leg] = true
	}
	for _, e := range extra {
		hit := strings.HasPrefix(p, e.pattern)
		if !strings.HasSuffix(e.pattern, "/") {
			hit, _ = path.Match(e.pattern, p)
		}
		if hit {
			for _, l := range e.legs {
				set[l] = true
			}
		}
	}
	for _, leg := range inputfamilies.Legs {
		cg := leg
		if d, ok := codegenDir[leg]; ok {
			cg = d
		}
		for _, c := range Conventions {
			if conventionMatch(strings.NewReplacer("{leg}", leg, "{cg}", cg).Replace(c), p) {
				set[leg] = true
				break
			}
		}
	}
	for _, r := range ix.refs {
		var hit bool
		switch {
		case r.glob:
			hit, _ = path.Match(r.pattern, p)
		case strings.HasSuffix(r.pattern, "/"):
			hit = strings.HasPrefix(p, r.pattern)
		default:
			hit = p == r.pattern
		}
		if hit {
			for _, l := range r.legs {
				set[l] = true
			}
		}
	}
	var out []string
	for _, leg := range inputfamilies.Legs {
		if set[leg] {
			out = append(out, leg)
		}
	}
	return out
}

// Select is the union of Legs over every changed path, in
// inputfamilies.Legs order.
func (ix *Index) Select(paths []string) []string {
	set := map[string]bool{}
	for _, p := range paths {
		for _, l := range ix.Legs(p) {
			set[l] = true
		}
	}
	var out []string
	for _, leg := range inputfamilies.Legs {
		if set[leg] {
			out = append(out, leg)
		}
	}
	return out
}

// Reads is every (leg, path) pair Load derived from a leg's make files, for
// the test that holds selection to what a leg's targets read: a literal file
// or directory, sorted.
func (ix *Index) Reads() [][2]string {
	var out [][2]string
	for _, r := range ix.refs {
		if r.glob {
			continue
		}
		for _, l := range r.legs {
			out = append(out, [2]string{l, r.pattern})
		}
	}
	for f, legs := range ix.files {
		for _, l := range legs {
			out = append(out, [2]string{l, f})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0]+" "+out[i][1] < out[j][0]+" "+out[j][1] })
	return slices.CompactFunc(out, func(a, b [2]string) bool { return a == b })
}
