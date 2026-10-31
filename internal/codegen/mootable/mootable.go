// Package mootable emits the moo target's TABLE SURFACE: one
// <Base>Table.moo module per schema file that declares tables
// (docs/SPEC-TABLES.md), plus the table runtime under runtime/ emitted beside
// the modules. docs/PORT-MOO.md is the port's design; THE VALUE MAPPING is
// each field's storage, and a table record keeps it field for field.
//
// The shape is the Luau port's (internal/codegen/luautable on the dev
// branch): for every table its exported type, New<Name> and
// <Name>Descriptor — ONE DESCRIPTOR PER TABLE AS FROZEN DATA — and ONE
// RUNTIME WALK PER FORM in runtime/*.moo (load, save, fixed, message, block,
// cook, retain) reading those descriptors. The semantics are main's
// (internal/tablewire, internal/codegen/jstable, gotable). A later form joins
// every module through [Register] from its own file's init, so no later form
// edits this file.
package mootable

import (
	"embed"
	"fmt"
	"maps"
	"path"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// RuntimeModule is the table surface's descriptor runtime.
const RuntimeModule = "schema_tables"

//go:embed runtime/*.moo
var runtimeFS embed.FS

// Runtime returns every runtime/*.moo file, keyed by basename.
func Runtime() map[string][]byte {
	out := map[string][]byte{}
	entries, err := runtimeFS.ReadDir("runtime")
	if err != nil {
		panic(err) // the embed is compiled in; a read failure is a build defect
	}
	for _, e := range entries {
		data, err := runtimeFS.ReadFile(path.Join("runtime", e.Name()))
		if err != nil {
			panic(err)
		}
		out[e.Name()] = data
	}
	return out
}

// A Part is a later form's contribution to one <Base>Table.moo module.
type Part func(g *Gen) error

type part struct {
	name string
	fn   Part
}

var parts []part

// Register joins a later form to every table module; parts run in name order.
// The text forms (json*.go, text*.go) join through it.
func Register(name string, fn Part) {
	parts = append(parts, part{name, fn})
	sort.Slice(parts, func(i, j int) bool { return parts[i].name < parts[j].name })
}

// runtimeGate says which runtime modules a unit omits (docs/SPEC-TABLES.md
// §2.12: a unit with no table carrying json, block or cook omits those
// runtime surfaces).
func runtimeGate(u *ir.Unit) func(name string) bool {
	jsonCarried, blockCarried, cookCarried := false, false, false
	for _, t := range u.Tables {
		if t.IsMapEntry() {
			continue
		}
		jsonCarried = jsonCarried || t.CarriesForm("json")
		blockCarried = blockCarried || t.CarriesForm("block")
		cookCarried = cookCarried || t.CarriesForm("cook")
	}
	return func(name string) bool {
		switch name {
		case "json.moo", "text.moo":
			return jsonCarried
		case "table_block.moo", "table_blockread.moo":
			return blockCarried
		case "table_cook.moo", "table_cookread.moo", "table_cookwrite.moo":
			return cookCarried
		}
		return true
	}
}

// Generate returns <Base>Table.moo for every file that declares a table or a
// table-closure union, or a packet type a table reaches, plus the table
// runtime. A table-free unit gets nothing here, so its output is the packet
// surface's alone (docs/PORTING.md I10).
func Generate(u *ir.Unit) (map[string][]byte, error) {
	return GenerateLineage(u, nil)
}

// GenerateLineage is Generate with the lock's fixed-form lineage handed in as
// data (docs/FIXED-FORM-ALGORITHM.md §5.2, COMPILE(lock, T); §5.9 #1): per
// fixed table, its locked layouts OLDEST FIRST. nil is a unit with no lock,
// whose every fixed table reads its own layout alone. A handed entry that is a
// lock bug fails the build here (§5.9 #8, #26).
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := Refuse(u); err != nil {
		return nil, err
	}
	if err := refuseFixedLineage(u, lineage); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	reach := closure(u)
	mods := moduleFiles(u, reach)
	graph := map[string]map[string]bool{}
	var order []string
	for _, f := range u.Files {
		if !mods[f.Base] {
			continue
		}
		g := newGen(u, f)
		g.Lineage = lineage
		emitFile(g, reach)
		for _, p := range parts {
			if err := p.fn(g); err != nil {
				return nil, fmt.Errorf("moo %s: %s: %w", p.name, f.Base, err)
			}
		}
		out[f.Base+"Table.moo"] = g.Assemble()
		deps := map[string]bool{}
		for _, m := range g.Requires() {
			if strings.HasSuffix(m, "Table") {
				deps[m] = true
			}
		}
		graph[f.Base+"Table"] = deps
		order = append(order, f.Base+"Table")
	}
	if err := requireCycle(graph, order); err != nil {
		return nil, err
	}
	keep := runtimeGate(u)
	for name, data := range Runtime() {
		if keep(name) {
			out[name] = data
		}
	}
	return out, nil
}

// Refuse is the constructs this surface refuses before it writes: a module
// name that collides with a schema file's module or a runtime module (folding
// case, as macOS and Windows do).
func Refuse(u *ir.Unit) error {
	type claim struct{ name, owner string }
	var claims []claim
	for _, f := range u.Files {
		claims = append(claims, claim{f.Base + ".moo", f.Base + ".schema"})
		claims = append(claims, claim{f.Base + "Table.moo", f.Base + ".schema's table module"})
	}
	var names []string
	for name := range moo.Runtime() {
		names = append(names, name)
	}
	for name := range Runtime() {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, n := range names {
		claims = append(claims, claim{n, "the moo runtime"})
	}
	for i := range claims {
		for j := i + 1; j < len(claims); j++ {
			if strings.EqualFold(claims[i].name, claims[j].name) {
				return fmt.Errorf("moo: %s (claimed by %s) and %s (claimed by %s) are equal ignoring case; a case-insensitive file system writes one file for the pair — rename the schema file", claims[i].name, claims[i].owner, claims[j].name, claims[j].owner)
			}
		}
	}
	return nil
}

// requireCycle refuses table modules that require each other in a cycle (a
// pointer from a table in one file to a table in another and back): a moo
// require cycle does not load.
func requireCycle(deps map[string]map[string]bool, order []string) error {
	state := map[string]int{}
	var visit func(string, []string) error
	visit = func(b string, stack []string) error {
		switch state[b] {
		case 1:
			return fmt.Errorf("moo: the table modules of this unit require each other in a cycle (%s -> %s); a moo require cycle does not load — move the tables into one file", strings.Join(stack, " -> "), b)
		case 2:
			return nil
		}
		state[b] = 1
		next := make([]string, 0, len(deps[b]))
		for d := range deps[b] {
			next = append(next, d)
		}
		sort.Strings(next)
		for _, d := range next {
			if err := visit(d, append(stack, b)); err != nil {
				return err
			}
		}
		state[b] = 2
		return nil
	}
	for _, m := range order {
		if err := visit(m, nil); err != nil {
			return err
		}
	}
	return nil
}

// closure is every packet `type` and `union` a table reaches by value, by
// element or through an arm, keyed by name: a by-value packet type rides as
// a table body (docs/SPEC-TABLES.md §3, kind 13; a union, kind 15), so the
// walk needs its descriptor too.
func closure(u *ir.Unit) map[string]bool {
	return reachFrom(u, func(*ir.Struct) bool { return true }, true)
}

// reachFrom is the closure walk over the tables root keeps, and, when unions is
// set, every table-closure union besides.
func reachFrom(u *ir.Unit, root func(*ir.Struct) bool, unions bool) map[string]bool {
	seen := map[string]bool{}
	var visit func(f *ir.Field)
	var visitStruct func(st *ir.Struct)
	visitUnion := func(un *ir.Union) {
		if seen[un.Name] {
			return
		}
		seen[un.Name] = true
		for _, v := range un.Variants {
			if v.F != nil {
				visit(v.F)
			}
		}
	}
	visitStruct = func(st *ir.Struct) {
		if seen[st.Name] {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			visit(f)
		}
	}
	visit = func(f *ir.Field) {
		if f.MapEntry != nil {
			visitStruct(f.MapEntry)
		}
		if f.Type.Kind != ir.TNamed {
			return
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			visitStruct(r)
		case *ir.Union:
			visitUnion(r)
		}
	}
	for _, f := range u.Files {
		for _, t := range f.Tables {
			if root(t) {
				visitStruct(t)
			}
		}
		if unions {
			for _, un := range f.TableUnions {
				visitUnion(un)
			}
		}
	}
	return seen
}

// moduleFiles is the files that get a table module: those with a table or a
// table-closure union, and those declaring a packet type a table reaches.
func moduleFiles(u *ir.Unit, reach map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, f := range u.Files {
		if len(f.Tables) > 0 || len(f.TableUnions) > 0 {
			out[f.Base] = true
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				if reach[d.Name] {
					out[f.Base] = true
				}
			case *ir.Union:
				if reach[d.Name] {
					out[f.Base] = true
				}
			}
		}
	}
	return out
}

func emitFile(g *Gen, reach map[string]bool) {
	for _, un := range g.File.TableUnions {
		g.emitUnion(un)
		emitUnionDescriptor(g, un)
	}
	for _, t := range g.File.Tables {
		g.emitStruct(t)
		emitDescriptor(g, t)
	}
	// the packet types of this file a table reaches: their descriptors only;
	// the type and its constructor are the packet module's
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if !d.IsTable && reach[d.Name] {
				emitDescriptor(g, d)
			}
		case *ir.Union:
			if g.Unit.TableUnions[d.Name] == nil && reach[d.Name] {
				emitUnionDescriptor(g, d)
			}
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range maps.Keys(m) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
