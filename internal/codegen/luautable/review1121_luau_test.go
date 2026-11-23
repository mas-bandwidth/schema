package luautable_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// luauReview1121 is the committed corpus of the 2026-11-21 Luau review
// (LUAU-1 to LUAU-3 and the Luau cross-leg cells): the schemas, probes,
// inputs and verdicts the regressions in this package read.
var luauReview1121 = filepath.Join("testdata", "review1121")

// review1121Rows reads a corpus index: one tab-separated row of fields
// columns per line, # lines and blank lines skipped.
func review1121Rows(t *testing.T, path string, fields int) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != fields {
			t.Fatalf("%s line %d: %d fields, want %d", path, n+1, len(f), fields)
		}
		rows = append(rows, f)
	}
	if len(rows) == 0 {
		t.Fatalf("%s holds no rows", path)
	}
	return rows
}

// review1121CaseLines is the rows as the probes read inputs.cases: one
// tab-separated line per row.
func review1121CaseLines(rows [][]string) []byte {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(strings.Join(r, "\t"))
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// review1121Verdicts reads a probe's id=verdict lines; anything after the
// verdict's first space (a reason) is logged, not kept.
func review1121Verdicts(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		id, rest, _ := strings.Cut(line, "=")
		verdict, why, _ := strings.Cut(rest, " ")
		if why != "" {
			t.Logf("%s %s (%s)", id, verdict, why)
		}
		got[id] = verdict
	}
	return got
}

// review1121Unit loads one committed schema file as a unit.
func review1121Unit(t *testing.T, path string) *ir.Unit {
	t.Helper()
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("%s does not load: %v", path, err)
	}
	return u
}

// review1121Modules writes every module luau and luautable generate for u,
// and the probe, as main.luau, into a fresh directory, and returns it.
func review1121Modules(t *testing.T, u *ir.Unit, probe string) string {
	t.Helper()
	dir := t.TempDir()
	for _, gen := range []func() (map[string][]byte, error){
		func() (map[string][]byte, error) { return luau.Generate(u) },
		func() (map[string][]byte, error) { return luautable.Generate(u) },
	} {
		files, err := gen()
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	src, err := os.ReadFile(probe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// review1121Sources reads the schema files of a committed unit directory
// (name -> source).
func review1121Sources(t *testing.T, dir string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("%s holds no schema file", dir)
	}
	sources := map[string]string{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		sources[filepath.Base(p)] = string(data)
	}
	return sources
}

// review1121Files loads the schema files (name -> source) as one unit and
// writes every module luau and luautable generate for it into a fresh
// directory, returning the directory and the module names.
func review1121Files(t *testing.T, sources map[string]string) (string, []string) {
	t.Helper()
	src := t.TempDir()
	var paths []string
	for name, text := range sources {
		p := filepath.Join(src, name)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var names []string
	for _, set := range []map[string][]byte{files, tables} {
		for name, data := range set {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, ".luau") {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return dir, names
}

// TestLuauRequireAliasNeverCollides pins LUAU-2 (review 2026-11-21): the
// local a module binds another module to never collides with the
// generator's own locals (the module table _M and the runtime _Rt) and is a
// Luau identifier for any file name schema check admits. Each unit of the
// committed corpus testdata/review1121/require-alias (files named M, Rt and
// _M beside a plain-named control; a-b is refused by schema check, SPEC §4.6) declares a type and a table another
// file's type and table reach, so the type module, the table module and the
// cross-file requires all bind those names; every module loads, the check
// holds, and every module passes luau-analyze.
func TestLuauRequireAliasNeverCollides(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(luauReview1121, "require-alias")
	for _, r := range review1121Rows(t, filepath.Join(corpus, "cases.tsv"), 3) {
		id, unit, check := r[0], r[1], r[2]
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			dir, names := review1121Files(t, review1121Sources(t, filepath.Join(corpus, unit)))
			var main strings.Builder
			main.WriteString("local Use = require(\"./Use\")\n")
			main.WriteString("assert(" + check + ", \"the cross-file types construct at their defaults\")\n")
			for _, name := range names {
				mod := strings.TrimSuffix(name, ".luau")
				main.WriteString("assert(type(require(\"./" + mod + "\")) == \"table\", \"" + mod + " loads\")\n")
			}
			main.WriteString("print(\"ok\")\n")
			if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			if out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil))); out != "ok" {
				t.Fatalf("main.luau printed %q", out)
			}
			luaurun.Analyze(t, dir, names...)
		})
	}
}

// goEscape is a \uXXXX or \UXXXXXXXX escape as Go's %q writes it, which Luau
// rejects: the backslash is not itself escaped (an odd run of backslashes).
var goEscape = regexp.MustCompile(`(?:^|[^\\])(?:\\\\)*(\\u[0-9a-fA-F]{4}|\\U[0-9a-fA-F]{8})`)

// TestLuauStringLiteralsAreLuauSyntax pins LUAU-1 (review 2026-11-21): a
// string the generator emits is spelled in Luau's own escape syntax. Each
// unit of the committed corpus testdata/review1121/string-literal carries a
// doc comment into the table module's unit view: one holding a rune Go's %q
// escapes as \uXXXX (U+00AD, U+200B), and a plain control. No module holds a
// Go escape, and every module loads.
func TestLuauStringLiteralsAreLuauSyntax(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(luauReview1121, "string-literal")
	for _, r := range review1121Rows(t, filepath.Join(corpus, "cases.tsv"), 3) {
		id, unit, marker := r[0], r[1], r[2]
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			dir, names := review1121Files(t, review1121Sources(t, filepath.Join(corpus, unit)))
			var main strings.Builder
			for _, name := range names {
				mod := strings.TrimSuffix(name, ".luau")
				main.WriteString("assert(type(require(\"./" + mod + "\")) == \"table\", \"" + mod + " loads\")\n")
			}
			main.WriteString("print(\"ok\")\n")
			if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			var carried bool
			for _, name := range names {
				data, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if e := goEscape.FindSubmatch(data); e != nil {
					t.Errorf("%s spells a rune as Go's %s, which Luau rejects", name, e[1])
				}
				if strings.Contains(string(data), marker) {
					carried = true
				}
			}
			if !carried {
				t.Fatalf("no module carries the doc text %q; the case no longer reaches a string literal", marker)
			}
			if out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil))); out != "ok" {
				t.Fatalf("main.luau printed %q", out)
			}
		})
	}
}

// TestLuauListOfPointersTypeMatchesTheLoad pins LUAU-3 (review 2026-11-21):
// the exported type of a list of pointers is what the load stores, a null
// element as false and the live length as n (the table.pack convention), so
// strict code that handles a loaded list type-checks. The schema and the
// probes (a null element beside a live-only control) are the committed corpus
// testdata/review1121/pointer-list.
func TestLuauListOfPointersTypeMatchesTheLoad(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(luauReview1121, "pointer-list")
	for _, r := range review1121Rows(t, filepath.Join(corpus, "cases.tsv"), 2) {
		id, probe := r[0], r[1]
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			dir, names := review1121Files(t, review1121Sources(t, corpus))
			data, err := os.ReadFile(filepath.Join(dir, "GraphTable.luau"))
			if err != nil || !strings.Contains(string(data), "export type Node = {") {
				t.Fatalf("GraphTable.luau does not export the Node type the probes require (%v) among %v", err, names)
			}
			src, err := os.ReadFile(filepath.Join(corpus, probe))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "probe.luau"), src, 0o644); err != nil {
				t.Fatal(err)
			}
			luaurun.Analyze(t, dir, append(names, "probe.luau")...)
			if out := strings.TrimSpace(string(luaurun.Run(t, dir, "probe.luau", nil))); out != "ok" {
				t.Fatalf("probe.luau printed %q", out)
			}
		})
	}
}
