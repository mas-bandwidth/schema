package luautable_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

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
// Luau identifier for any file name schema check admits. The unit's files
// are named M, Rt and a-b, each declaring a type and a table another file's
// type and table reach, so the type module, the table module and the
// cross-file requires all bind those names; every module loads, constructs
// what it declares, and passes luau-analyze.
func TestLuauRequireAliasNeverCollides(t *testing.T) {
	t.Parallel()
	dir, names := review1121Files(t, map[string]string{
		"M.schema":   "package alias\n\ntype A\n{\n    x uint8 = 7\n}\n\ntable TA\n{\n    a A\n}\n",
		"Rt.schema":  "package alias\n\ntype B\n{\n    y uint16 = 9\n}\n\ntable TB\n{\n    b B\n}\n",
		"a-b.schema": "package alias\n\ntype C\n{\n    z uint8 = 3\n}\n\ntable TC\n{\n    c C\n}\n",
		"Use.schema": "package alias\n\ntype D\n{\n    a A\n    b B\n    c C\n}\n\ntable U\n{\n    d  D\n    ta TA\n    tb TB\n    tc TC\n}\n",
	})
	var main strings.Builder
	main.WriteString("local Use = require(\"./Use\")\n")
	main.WriteString("local d = Use.NewD()\n")
	main.WriteString("assert(d.A.X == 7 and d.B.Y == 9 and d.C.Z == 3, \"the cross-file types construct at their defaults\")\n")
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
}

// TestLuauStringLiteralsAreLuauSyntax pins LUAU-1 (review 2026-11-21): a
// string the generator emits is spelled in Luau's own escape syntax. A doc
// comment carrying a rune Go's %q escapes as \uXXXX (U+00AD here), which Luau
// rejects, lands in the table module's unit view; the module loads and the
// doc reads back byte for byte.
func TestLuauStringLiteralsAreLuauSyntax(t *testing.T) {
	t.Parallel()
	doc := "soft\u00adhyphen separator"
	dir, names := review1121Files(t, map[string]string{
		"Lit.schema": "package lit\n\n/// " + doc + "\ntype Held\n{\n    /// field " + doc + "\n    n uint8\n}\n\n/// " + doc + "\ntable Holder\n{\n    h Held\n}\n",
	})
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
		if strings.Contains(string(data), `\u00ad`) {
			t.Errorf("%s spells U+00AD as Go's \\u00ad, which Luau rejects", name)
		}
		if strings.Contains(string(data), "separator") {
			carried = true
		}
	}
	if !carried {
		t.Fatalf("no module carries the doc text; the test no longer reaches a string literal")
	}
	if out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil))); out != "ok" {
		t.Fatalf("main.luau printed %q", out)
	}
}

// TestLuauListOfPointersTypeMatchesTheLoad pins LUAU-3 (review 2026-11-21):
// the exported type of a list of pointers is what the load stores, a null
// element as false and the live length as n (the table.pack convention), so
// strict code that handles a loaded list type-checks.
func TestLuauListOfPointersTypeMatchesTheLoad(t *testing.T) {
	t.Parallel()
	dir, names := review1121Files(t, map[string]string{
		"Graph.schema": "package graph\n\ntable Node\n{\n    v    int32\n    kids []*Node\n}\n",
	})
	var types string
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "export type Node = {") {
			types = strings.TrimSuffix(name, ".luau")
		}
	}
	if types == "" {
		t.Fatalf("no module exports the Node type among %v", names)
	}
	probe := "--!strict\nlocal G = require(\"./" + types + "\")\n" +
		"local n: G.Node = G.NewNode()\n" +
		"n.Kids = { false, G.NewNode() }\n" +
		"n.Kids.n = 2\n" +
		"local first = n.Kids[1]\n" +
		"if first ~= false and first ~= nil then print(first.V) end\n" +
		"print(\"ok\")\n"
	if err := os.WriteFile(filepath.Join(dir, "probe.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	luaurun.Analyze(t, dir, append(names, "probe.luau")...)
	if out := strings.TrimSpace(string(luaurun.Run(t, dir, "probe.luau", nil))); out != "ok" {
		t.Fatalf("probe.luau printed %q", out)
	}
}
