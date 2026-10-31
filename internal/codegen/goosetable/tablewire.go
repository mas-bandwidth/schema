package goosetable

// THE TABLE WIRE's unit registry (docs/SPEC-TABLES.md §3, §3.1, §3.2, §2.15;
// docs/PORT-GOOSE.md, THE TABLE WIRE). Form 1 is ONE descriptor-driven walk
// each way, runtime/load.goose and runtime/save.goose, and the walk resolves
// every name a descriptor holds against one schema_rt::LoadUnit: every table
// and union descriptor of the unit, and the four facts a FieldDesc does not
// carry — the key enum of an enum-keyed array, the variants retired in place,
// and the if/else guards a writer evaluates. This part emits that registry
// ONCE per unit, as `TableUnit`, in the unit's package module [UnitModule],
// which is named for the package: an earlier-sorting file joining the unit
// moves no byte of any file's module (docs/PORTING.md J2).
//
// It is a [RegisterUnit] part, so it joins the unit module without editing
// goosetable.go.

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// UnitName is the registry's global: the unit as data, the first argument of
// schema_rt::load_table and schema_rt::save_table.
const UnitName = "TableUnit"

func init() {
	RegisterUnit("tablewire", "unit", emitTableWireUnit)
}

// unitDecls is every struct and union the table walk can meet, by name:
// the tables, the map entries, the text-list element records and the packet
// types a table reaches by value, in a stable order.
func unitDecls(u *ir.Unit) (structs []*ir.Struct, unions []*ir.Union) {
	reach := closure(u)
	seen := map[string]bool{}
	for _, f := range u.Files {
		for _, un := range f.TableUnions {
			if !seen[un.Name] {
				seen[un.Name] = true
				unions = append(unions, un)
			}
		}
		for _, t := range f.Tables {
			if !seen[t.Name] {
				seen[t.Name] = true
				structs = append(structs, t)
			}
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				if reach[d.Name] && !seen[d.Name] {
					seen[d.Name] = true
					structs = append(structs, d)
				}
			case *ir.Union:
				if reach[d.Name] && !seen[d.Name] {
					seen[d.Name] = true
					unions = append(unions, d)
				}
			}
		}
	}
	return structs, unions
}

// guardTerms is every guarded field of st: its ordinal and the conditions
// (the bool field's ordinal and the value it must hold) that let a writer
// write it (SPEC §4.5; tabletext.Guards).
func guardTerms(st *ir.Struct) [][3]int {
	ordinal := map[string]int{}
	for i, f := range st.Fields {
		ordinal[f.Name] = i
	}
	var out [][3]int
	var walk func(items []ir.Item, cond [][2]int)
	walk = func(items []ir.Item, cond [][2]int) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				for _, c := range cond {
					out = append(out, [3]int{ordinal[item.F.Name], c[0], c[1]})
				}
			case *ir.Branch:
				pos, neg := 1, 0
				if item.Neg {
					pos, neg = 0, 1
				}
				walk(item.Then, append(cond[:len(cond):len(cond)], [2]int{ordinal[item.Cond], pos}))
				walk(item.Else, append(cond[:len(cond):len(cond)], [2]int{ordinal[item.Cond], neg}))
			}
		}
	}
	walk(st.Items, nil)
	return out
}

func emitTableWireUnit(g *goose.Gen) error {
	// KeyDesc, LoadUnit and the rest of the unit's types live in the load
	// walk's module: import it, so this module checks on its own
	g.Import("wire_load")
	structs, unions := unitDecls(g.Unit)
	var tables, unionRefs, keys, deprecated, guards []string
	for _, st := range structs {
		requireHome(g, st.Name)
		tables = append(tables, st.Name+"Descriptor")
		for i, f := range st.Fields {
			if f.KeyEnumRef != nil {
				// the key enum's variants, declared HERE: a descriptor global of
				// another module may not have initialised yet (the Goose spec: globals initialise in declaration order, imported files first)
				e := f.KeyEnumRef
				name := fmt.Sprintf("_TableUnitKey%d", len(keys))
				parts := make([]string, len(e.Variants))
				for j := range e.Variants {
					parts[j] = fmt.Sprintf("%s::VariantDesc { name: %q, id: 0x%016x }", goose.RuntimeNamespace, e.Variants[j], ir.TableWireId(e.VariantWireName(j)))
				}
				g.Declare(name)
				g.Pf("let %s: %s::VariantDesc[%d] = [%s];\n", name, goose.RuntimeNamespace, len(parts), strings.Join(parts, ", "))
				keys = append(keys, fmt.Sprintf("%s::KeyDesc { owner: %q, ordinal: %d, variants: %s }",
					goose.RuntimeNamespace, st.Name, i, name))
			}
		}
		for _, t := range guardTerms(st) {
			guards = append(guards, fmt.Sprintf("%s::GuardDesc { owner: %q, ordinal: %d, cond: %d, want: %t }",
				goose.RuntimeNamespace, st.Name, t[0], t[1], t[2] == 1))
		}
	}
	for _, un := range unions {
		requireHome(g, un.Name)
		unionRefs = append(unionRefs, un.Name+"Descriptor")
	}
	enums := g.Unit.Enums
	names := make([]string, 0, len(enums))
	for n := range enums {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		e := enums[n]
		for i := range e.Variants {
			if i < len(e.VariantTags) && hasTag(e.VariantTags[i], "deprecated") {
				deprecated = append(deprecated, fmt.Sprintf("%s::DeprecatedVariant { enum_name: %q, id: 0x%016x }",
					goose.RuntimeNamespace, e.Name, ir.TableWireId(e.VariantWireName(i))))
			}
		}
	}
	list := func(name, typ string, items []string) string {
		if len(items) == 0 {
			return ""
		}
		g.Declare(name)
		g.Pf("let %s: %s::%s[%d] = [\n    %s,\n];\n", name, goose.RuntimeNamespace, typ, len(items), strings.Join(items, ",\n    "))
		return name
	}
	g.Pf("// %s — the unit as data for the table wire (docs/SPEC-TABLES.md §3):\n", UnitName)
	g.Pf("// every table and union descriptor, the enum-keyed arrays' key enums, the\n")
	g.Pf("// variants retired in place (§2.15) and the if/else guards (SPEC §4.5)\n")
	var l lit
	if n := list("_TableUnitTables", "TableDesc", tables); n != "" {
		l.add("tables", n)
	}
	if n := list("_TableUnitUnions", "UnionDesc", unionRefs); n != "" {
		l.add("unions", n)
	}
	if n := list("_TableUnitKeys", "KeyDesc", keys); n != "" {
		l.add("keys", n)
	}
	if n := list("_TableUnitDeprecated", "DeprecatedVariant", deprecated); n != "" {
		l.add("deprecated", n)
	}
	if n := list("_TableUnitGuards", "GuardDesc", guards); n != "" {
		l.add("guards", n)
	}
	// every id the unit can spell, ascending: a retaining save takes a
	// retained id this build can name from the file's own table, and only
	// the others from the caller's list (docs/SPEC-TABLES.md §6.6)
	known := ir.TableWireIds(g.Unit)
	slices.Sort(known)
	var knownLit []string
	for _, id := range known {
		knownLit = append(knownLit, fmt.Sprintf("0x%016x", id))
	}
	if len(knownLit) > 0 {
		g.Declare("_TableUnitKnown")
		g.Pf("let _TableUnitKnown: u64[%d] = [%s];\n", len(knownLit), strings.Join(knownLit, ", "))
		l.add("known", "_TableUnitKnown")
	}
	g.Declare(UnitName)
	if len(l.parts) == 0 {
		g.Pf("let %s: %s::LoadUnit = %s::LoadUnit { .. };\n\n", UnitName, goose.RuntimeNamespace, goose.RuntimeNamespace)
		return nil
	}
	g.Pf("let %s: %s::LoadUnit = %s;\n\n", UnitName, goose.RuntimeNamespace, l.render("LoadUnit"))
	return nil
}

func hasTag(tags []string, tag string) bool {
	return slices.Contains(tags, tag)
}
