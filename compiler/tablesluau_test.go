// The Luau target refuses every construct it does not carry BY NAME
// (docs/SPEC-TABLES.md §11, docs/PORT-LUAU.md, THE RUNTIME LAYOUT): each
// refusal names what it refuses and the target, so a schema author is told
// which port lacks what.
//
// This test reads the target's Generate (compiler/target_luau.go) from its
// SOURCE and runs the chain it calls; the same constructs also go
// through Generate("luau") whole. The Luau target ports every field construct
// the corpus declares, so what it refuses is a FILE or a MODULE: a schema file
// named for a runtime module, a require cycle between modules, and a file that
// collides with its own table module. Each refusal is required to name that
// file or module (the construct's own name, where a field refusal names the
// field) and luau.
package compiler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// refusalSites is every function of a Go source file whose body builds a
// refusal of its own: a fmt.Errorf whose format wraps no other error (%w).
func refusalSites(t *testing.T, path, pkg string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		own := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Errorf" {
				return true
			}
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && !strings.Contains(lit.Value, "%w") {
				own = true
			}
			return true
		})
		if own {
			out = append(out, pkg+"."+fn.Name.Name)
		}
	}
	return out
}

// targetCalls is every call the target's Generate makes, as pkg.Func.
func targetCalls(t *testing.T) []string {
	t.Helper()
	out := methodCalls(t, "Generate")
	// Generate hands a nil lineage to GenerateLineage (docs/FIXED-FORM-ALGORITHM.md
	// §5.9 #1), so the chain is that method's calls too
	if slices.Contains(out, "t.GenerateLineage") {
		out = append(out, methodCalls(t, "GenerateLineage")...)
	}
	return out
}

func methodCalls(t *testing.T, name string) []string {
	t.Helper()
	fn := funcDeclOf(t, "target_luau.go", "luauTarget", name)
	var out []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch f := call.Fun.(type) {
		case *ast.Ident:
			out = append(out, f.Name)
		case *ast.SelectorExpr:
			if x, ok := f.X.(*ast.Ident); ok {
				out = append(out, x.Name+"."+f.Sel.Name)
			}
		}
		return true
	})
	return out
}

func funcDeclOf(t *testing.T, path, recv, name string) *ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		if id, ok := fn.Recv.List[0].Type.(*ast.Ident); ok && id.Name == recv {
			return fn
		}
	}
	t.Fatalf("%s declares no %s.%s", path, recv, name)
	return nil
}

// luauUnit loads a unit of several schema files written into a fresh directory.
func luauUnit(t *testing.T, files map[string]string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	u, err := New().Load(paths)
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	return u
}

// TestLuauRefusesUnportedConstructsByName holds every refusal the Luau target
// makes to a construct with its name: the refusal sites are read from the
// sources of the two surfaces the target's Generate calls (and a new call in
// that Generate fails here until it has its cases), each site has a construct
// here that trips it, and each refusal names the construct and luau, through
// the target's chain and, under the tag, through Generate("luau") itself.
func TestLuauRefusesUnportedConstructsByName(t *testing.T) {
	t.Parallel()
	// the target's chain, from its source: the type surface, then the table
	// surface, and nothing else that could refuse
	calls := targetCalls(t)
	for _, c := range calls {
		switch c {
		case "luau.Generate", "luautable.Generate", "luautable.GenerateLineage", "luauTableLineage",
			"t.GenerateLineage", "maps.Copy", "registerBuiltin":
		default:
			t.Fatalf("compiler/target_luau.go's Generate calls %s, which this test has no case for: add one", c)
		}
	}
	if !slices.Contains(calls, "luau.Generate") || (!slices.Contains(calls, "luautable.Generate") && !slices.Contains(calls, "luautable.GenerateLineage")) {
		t.Fatalf("the luau chain did not parse: %v", calls)
	}
	sites := append(refusalSites(t, "../internal/codegen/luau/luau.go", "luau"),
		refusalSites(t, "../internal/codegen/luautable/luautable.go", "luautable")...)
	if len(sites) == 0 {
		t.Fatal("no refusal site parsed from the luau surfaces")
	}
	chain := func(u *ir.Unit) error {
		if _, err := luau.Generate(u); err != nil {
			return err
		}
		_, err := luautable.Generate(u)
		return err
	}

	const table = "package p\n\ntable Game\n{\n    x uint8\n}\n"
	const tableWithType = "package p\n\ntype T\n{\n    x uint8\n}\n\ntable Game\n{\n    x uint8\n}\n"
	cases := []struct {
		site, name string
		files      map[string]string
		names      string // what the refusal must name
	}{
		{"luau.CheckBases", "a schema file named for the type surface's runtime",
			map[string]string{"schema_runtime.schema": "package p\n\ntype T\n{\n    x uint8\n}\n"}, "schema_runtime.luau"},
		{"luau.CheckBases", "a schema file named for the table runtime's strict read",
			map[string]string{"Schema_refusal.schema": tableWithType}, "schema_refusal.luau"},
		{"luau.CheckBases", "a schema file named for the table runtime's load",
			map[string]string{"schema_load.schema": tableWithType}, "schema_load.luau"},
		// the table surface's own RequireCycle call is the same site: a
		// cross-file TABLE cycle never reaches it, because the front end
		// refuses one first (docs/SPEC-TABLES.md §11, a unit's table files
		// form a DAG by reference)
		{"luau.RequireCycle", "type modules that require each other",
			map[string]string{
				"A.schema": "package p\n\ntype A\n{\n    x uint8\n}\n\ntype C\n{\n    b B\n}\n",
				"B.schema": "package p\n\ntype B\n{\n    a A\n}\n",
			}, "A -> B -> A"},
		{"luautable.Refuse", "a file and its own table module",
			map[string]string{"Game.schema": table, "GameTable.schema": "package p\n\ntype Other\n{\n    y uint8\n}\n"}, "GameTable"},
		{"luautable.Refuse", "a file and its own block module",
			map[string]string{"Game.schema": table, "GameBlock.schema": "package p\n\ntype Other\n{\n    y uint8\n}\n"}, "GameBlock"},
	}
	var c *Compiler
	for _, g := range builtins() {
		if slices.Contains(g.Names(), "luau") {
			c = New() // the tagged build: the target is registered
		}
	}
	named := func(tc string, err error, what string) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: luau accepted the construct", tc)
			return
		}
		if msg := err.Error(); !strings.Contains(msg, what) || !strings.Contains(strings.ToLower(msg), "luau") {
			t.Errorf("%s: refusal does not name %s and the target luau: %s", tc, what, msg)
		}
	}
	covered := map[string]bool{}
	for _, tc := range cases {
		u := luauUnit(t, tc.files)
		named(tc.name+" (the luau chain)", chain(u), tc.names)
		if c != nil {
			_, err := c.Generate(u, "luau", nil)
			named(tc.name+" (Generate luau)", err, tc.names)
		}
		covered[tc.site] = true
	}
	for _, s := range sites {
		if !covered[s] {
			t.Errorf("the luau surfaces refuse through %s, and no construct here is refused by it", s)
		}
	}
	// the control: a clean table unit generates, through both
	clean := luauUnit(t, map[string]string{"Game.schema": table})
	if err := chain(clean); err != nil {
		t.Errorf("the control, a clean table unit: %v", err)
	}
	if c != nil {
		if _, err := c.Generate(clean, "luau", nil); err != nil {
			t.Errorf("the control, a clean table unit (Generate luau): %v", err)
		}
	}
	t.Logf("%d constructs over %d refusal sites %v; the tagged target %v", len(cases), len(sites), sites, c != nil)
}

// TestLuauStrictReadNamesAreEmitted holds internal/tablenames/luau.go honest
// from the emitted side (docs/SPEC-TABLES.md §11): every name it registers for
// the Luau backend is a member the emitted strict-read runtime defines.
func TestLuauStrictReadNamesAreEmitted(t *testing.T) {
	t.Parallel()
	src, ok := luautable.Runtime()["schema_refusal.luau"]
	if !ok {
		t.Fatal("the Luau table runtime emits no schema_refusal.luau")
	}
	names := tablenames.DefinedBy(tablenames.Luau)
	if len(names) == 0 {
		t.Fatal("internal/tablenames registers no Luau name")
	}
	for _, name := range names {
		if !strings.Contains(string(src), "function R."+name+"(") && !strings.Contains(string(src), "R."+name+" = ") {
			t.Errorf("internal/tablenames says the Luau backend defines %s, but schema_refusal.luau defines no R.%s", name, name)
		}
	}
}
