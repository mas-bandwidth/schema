// Package inputfamilies names the repository's shared test input families
// and derives, from the tree itself, which legs read each one.
//
// WHY (2026-11-22). f2e1c41 added two .bits pins under
// testdata/wire/packet-defaults. No path rule of ci-fast.yml's plan matched
// that directory, so the plan selected no leg, every reader's check was
// skipped, and the verdict called the skips green because the plan had
// selected nothing. A hand list of "which legs read this directory" goes stale
// the same way a hand-typed case count does, so the readers here are never
// typed: a leg reads a family when the make target that runs the family for
// that leg is defined (Target, with {leg}), and a driver directory with no
// such target is a reader that never runs, which Check refuses.
//
// The Go packages that read a changed input are derived the same way: every
// main-module .go file that names the input's directory or file selects its
// package (GoReaders). A root testdata/ directory is invisible to the go
// tool, so before this nothing selected those packages either.
package inputfamilies

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Legs is every leg: 18 targets in 17 languages.
var Legs = []string{"cpp", "c", "go", "rust", "js", "cs", "unity", "java", "dart", "elixir",
	"lua", "luau", "moo", "squirrel", "goose", "lobster", "python", "ruby"}

// Family is one shared test input family.
type Family struct {
	Name string
	// Inputs: "dir/" is the whole subtree, any other entry is a path.Match
	// pattern over a repository path ("dir/*" is the directory's own files).
	Inputs []string
	// Target is the make target that runs this family for one leg, {leg}
	// standing for the leg. The legs whose target is defined are the readers.
	Target string
	// Run is what a fast-lane row runs for one reader leg ({leg}); Target
	// when empty.
	Run string
	// Drivers matches the tracked files that are one leg's driver for this
	// family; the first non-empty group is the leg. Every such leg must have
	// Target defined, or it is a reader that never runs.
	Drivers string
	// Named is a reader whose run has another name than Target (leg ->
	// target). Check refuses an entry whose target is not defined.
	Named map[string]string
	// PR: the rows run in the pull-request tier. A family whose per-leg run
	// is a heavy workflow the owner moved off pull requests (2026-11-15) is
	// selected on a push to main and nightly only, and says so.
	PR bool
}

// Families is every family with per-leg readers. Families read only by Go
// tests need no entry: GoReaders finds their packages from the tree.
var Families = []Family{
	{Name: "packet-defaults", PR: true,
		Inputs:  []string{"testdata/wire/packet-defaults/", "test/conformance/packet/testdata/constant-defaults/", "test/packet-defaults/*", "make/checks/packet-defaults.mk"},
		Target:  "packet-defaults-{leg}",
		Drivers: `^test/packet-defaults/(?:([a-z]+)/|(c|cpp)_(?:constant_)?main\.c(?:pp)?$)`},
	{Name: "packet-end", PR: true,
		Inputs:  []string{"testdata/wire/packet-end/", "test/packet-end/*"},
		Target:  "packet-end-{leg}",
		Drivers: `^test/packet-end/([a-z]+)/`},
	{Name: "packet-finite", PR: true,
		Inputs: []string{"test/packet-finite/*"},
		Target: "packet-finite-{leg}",
		// the Squirrel reader is a Go test the negative control runs
		Named:   map[string]string{"squirrel": "packet-finite-squirrel-negative-control"},
		Drivers: `^test/packet-finite/([a-z]+)/`},
	{Name: "packet-void", PR: true,
		Inputs:  []string{"test/packet-void/*"},
		Target:  "packet-void-{leg}",
		Drivers: `^test/packet-void/(?:([a-z]+)/|(c|cpp)_main\.c(?:pp)?$)`},
	{Name: "packet-write-bareint", PR: true,
		Inputs:  []string{"test/packet-write-bareint/*"},
		Target:  "packet-write-bareint-{leg}",
		Drivers: `^test/packet-write-bareint/([a-z]+)/`},
	{Name: "packet-write-cfloat", PR: true,
		Inputs:  []string{"test/packet-write-cfloat/*"},
		Target:  "packet-write-cfloat-{leg}",
		Drivers: `^test/packet-write-cfloat/([a-z]+)/`},
	{Name: "packet-write-text", PR: true,
		Inputs:  []string{"test/packet-write-text/*"},
		Target:  "packet-write-text-{leg}",
		Drivers: `^test/packet-write-text/([a-z]+)/`},
	{Name: "conformance-corpus", PR: true,
		Inputs: []string{"testdata/conformance/tables/"},
		Target: "_verify-{leg}-conformance",
		Run:    "verify-{leg}-conformance"},
	{Name: "hostile-corpus", PR: false,
		Inputs: []string{"internal/ci/hostilecorpus/corpus/"},
		Target: "schema-{leg}-runs-hostile-corpus"},
}

// Row is one fast-lane job: a reader leg running one family.
type Row struct {
	Leg    string `json:"leg"`
	Family string `json:"family"`
	Target string `json:"target"`
}

// Tree is what selection reads from the repository: its files (slash paths
// relative to the root) and the make targets its makefiles define.
type Tree struct {
	Files   []string
	Targets map[string]bool
	// GoFiles maps each main-module .go file to its contents.
	GoFiles map[string]string
}

// Matches reports whether path p is one of the family's inputs.
func (f Family) Matches(p string) bool {
	for _, in := range f.Inputs {
		if strings.HasSuffix(in, "/") {
			if strings.HasPrefix(p, in) {
				return true
			}
			continue
		}
		if ok, _ := path.Match(in, p); ok {
			return true
		}
	}
	return false
}

func isLeg(s string) bool {
	for _, l := range Legs {
		if l == s {
			return true
		}
	}
	return false
}

func sub(tmpl, leg string) string { return strings.ReplaceAll(tmpl, "{leg}", leg) }

// Readers is every leg whose Target (or Named target) is defined, in Legs
// order.
func (f Family) Readers(t Tree) []string {
	var out []string
	for _, l := range Legs {
		if t.Targets[f.target(l)] {
			out = append(out, l)
		}
	}
	return out
}

// target is the make target that runs the family for leg l.
func (f Family) target(l string) string {
	if n, ok := f.Named[l]; ok {
		return n
	}
	return sub(f.Target, l)
}

// DriverLegs is every leg with a driver file for the family, in Legs order.
func (f Family) DriverLegs(t Tree) []string {
	if f.Drivers == "" {
		return nil
	}
	re := regexp.MustCompile(f.Drivers)
	seen := map[string]bool{}
	for _, p := range t.Files {
		m := re.FindStringSubmatch(p)
		for _, g := range m[min(1, len(m)):] {
			if g != "" {
				seen[g] = true
				break
			}
		}
	}
	var out []string
	for _, l := range Legs {
		if seen[l] {
			out = append(out, l)
		}
	}
	return out
}

// Selection is what a set of changed files selects.
type Selection struct {
	Families []string
	Rows     []Row
	Packages []string
	// Unread names the changed input files under testdata/ that no family
	// and no Go file names: an input nothing is known to read.
	Unread []string
}

// genericDirs are directory names too common to stand for one input set.
var genericDirs = map[string]bool{"testdata": true, "wire": true, "tables": true, "conformance": true, "test": true, "corpus": true, "inputs": true}

// mentions is what a Go reader of path p would spell: the directory path,
// the directory's own quoted name (filepath.Join("testdata", "wire", "x")),
// and the file's name.
func mentions(p string) []string {
	dir, base := path.Split(p)
	dir = strings.TrimSuffix(dir, "/")
	var out []string
	if dir != "" {
		out = append(out, dir)
		if last := path.Base(dir); !genericDirs[last] {
			out = append(out, `"`+last+`"`)
		}
	}
	if len(base) >= 5 {
		out = append(out, base)
	}
	return out
}

// isInput reports whether a changed path is a test input whose Go readers
// the plan must find: a non-Go file under a root testdata/ or test/
// directory, or under the shared hostile corpus.
func isInput(p string) bool {
	if strings.HasSuffix(p, ".go") {
		return false
	}
	return strings.HasPrefix(p, "testdata/") || strings.HasPrefix(p, "test/") || strings.HasPrefix(p, "internal/ci/hostilecorpus/")
}

// GoReaders is every main-module package (./dir) with a .go file naming one
// of the input's mentions, sorted.
func GoReaders(t Tree, p string) []string {
	ms := mentions(p)
	set := map[string]bool{}
	for f, body := range t.GoFiles {
		for _, m := range ms {
			if strings.Contains(body, m) {
				set["./"+path.Dir(f)] = true
				break
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Select is what the changed files select. all (a push to main) selects
// every family's every reader; pr keeps only the families that run in the
// pull-request tier.
func Select(t Tree, changed []string, all, pr bool) Selection {
	var s Selection
	fams := map[string]bool{}
	pkgs := map[string]bool{}
	for _, f := range Families {
		hit := all
		for _, p := range changed {
			if f.Matches(p) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		fams[f.Name] = true
		s.Families = append(s.Families, f.Name)
		if pr && !f.PR {
			continue
		}
		for _, l := range f.Readers(t) {
			run := f.target(l)
			if _, named := f.Named[l]; !named && f.Run != "" {
				run = sub(f.Run, l)
			}
			s.Rows = append(s.Rows, Row{Leg: l, Family: f.Name, Target: run})
		}
	}
	for _, p := range changed {
		if !isInput(p) {
			continue
		}
		rs := GoReaders(t, p)
		for _, r := range rs {
			pkgs[r] = true
		}
		if len(rs) == 0 && strings.HasPrefix(p, "testdata/") && !inFamily(p) {
			s.Unread = append(s.Unread, p)
		}
	}
	for k := range pkgs {
		s.Packages = append(s.Packages, k)
	}
	sort.Strings(s.Packages)
	return s
}

func inFamily(p string) bool {
	for _, f := range Families {
		if f.Matches(p) {
			return true
		}
	}
	return false
}

// Check refuses a family whose inputs match no file, which no leg reads, or
// with a driver whose leg has no target (a reader that never runs).
func Check(t Tree) []string {
	var bad []string
	for _, f := range Families {
		for _, in := range f.Inputs {
			n := 0
			for _, p := range t.Files {
				if (Family{Inputs: []string{in}}).Matches(p) {
					n++
				}
			}
			if n == 0 {
				bad = append(bad, fmt.Sprintf("%s: input %q matches no file", f.Name, in))
			}
		}
		for l, n := range f.Named {
			if !isLeg(l) || !t.Targets[n] {
				bad = append(bad, fmt.Sprintf("%s: named reader %s -> %s is not a defined target of a leg", f.Name, l, n))
			}
		}
		readers := f.Readers(t)
		if len(readers) == 0 {
			bad = append(bad, fmt.Sprintf("%s: no leg defines %s", f.Name, f.Target))
		}
		has := map[string]bool{}
		for _, l := range readers {
			has[l] = true
		}
		for _, l := range f.DriverLegs(t) {
			if !has[l] {
				bad = append(bad, fmt.Sprintf("%s: leg %s has a driver but no %s target: a reader that never runs", f.Name, l, sub(f.Target, l)))
			}
		}
	}
	return bad
}

var targetLine = regexp.MustCompile(`(?m)^([A-Za-z0-9_.%/-]+)[ \t]*:([^=]|$)`)

// skipDir names directories the tree walk never enters: no main-module Go
// source and no tracked input lives in them.
var skipDir = map[string]bool{".git": true, "build": true, "node_modules": true, "third_party": true}

// Load reads the tree rooted at root.
func Load(root string) (Tree, error) {
	t := Tree{Targets: map[string]bool{}, GoFiles: map[string]string{}}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (skipDir[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != ".github") {
				return filepath.SkipDir
			}
			return nil
		}
		t.Files = append(t.Files, rel)
		name := d.Name()
		if name == "Makefile" || strings.HasSuffix(name, ".mk") {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, m := range targetLine.FindAllStringSubmatch(string(b), -1) {
				t.Targets[m[1]] = true
			}
		}
		if strings.HasSuffix(name, ".go") && mainModule(root, rel) {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			t.GoFiles[rel] = string(b)
		}
		return nil
	})
	sort.Strings(t.Files)
	return t, err
}

// mainModule reports whether a .go file belongs to the root module: not
// under testdata/ or generated/, and with no go.mod between it and the root.
func mainModule(root, rel string) bool {
	if strings.HasPrefix(rel, "generated/") || strings.Contains("/"+rel, "/testdata/") {
		return false
	}
	for d := path.Dir(rel); d != "."; d = path.Dir(d) {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(d), "go.mod")); err == nil {
			return false
		}
	}
	return true
}
