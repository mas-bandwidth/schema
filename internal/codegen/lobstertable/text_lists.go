// Text lists, their total and unbounded text in the Lobster table surface
// (docs/SPEC-TABLES.md §2.13, §2.13.1; docs/PORT-LOBSTER.md, THE TYPE
// MAPPING and THE LOAD).
//
// This file JOINS the descriptor emitter from its own init: the descriptor's
// classes and field lines are written by lobstertable.go, and the facts a
// text list adds — its `| total = N`, the `Table.field` a clamp names, and
// whether the element or a map's key is unbounded text — are registered by
// the generated module through schema_rt.text_list. The runtime file
// text_lists.lobster reads those facts from the same registry and replaces
// the "arr", "textlist", "str" and "map" shapes through the hook
// load.lobster fixed. Neither load.lobster nor schema_save.lobster is edited.
package lobstertable

import (
	"slices"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("text_lists", emitTextListFacts)
}

// TextListsModule is the runtime file this surface joins: the text-list
// shapes and the fact registry (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
const TextListsModule = "text_lists"

// emitTextListFacts writes one `schema_rt.text_list(...)` per field that
// needs a fact, after the descriptors it names. A text list carries its total
// and its unbounded element (§2.13, §2.13.1); a map carries an unbounded key
// (§2.13.1). A unit with none imports nothing and writes nothing.
func emitTextListFacts(g *lobster.Gen) error {
	var facts []string
	for _, t := range g.File.Tables {
		for i, f := range t.Fields {
			total, of, unboundedValue, unboundedKey := int64(0), "", false, false
			switch {
			case f.IsTextList():
				total = f.TextTotal
				of = f.TextListElem().MapEntryOf
				unboundedValue = ir.TextListUnbounded(f)
			case f.IsMap() && ir.MapKeyUnbounded(f):
				unboundedKey = true
			default:
				continue
			}
			facts = append(facts, "let "+factName(t.Name, f.Name)+" = "+rt+".text_list("+t.Name+"Descriptor.fields["+itoa(i)+"], "+
				itoa64(total)+", "+lobster.StringLit([]byte(of))+", "+boolLit(unboundedValue)+", "+boolLit(unboundedKey)+")")
		}
	}
	if len(facts) == 0 {
		return nil
	}
	g.Runtimes = appendModule(g.Runtimes, TextListsModule)
	for _, fact := range facts {
		g.Pf("%s\n", fact)
	}
	g.Pf("\n")
	return nil
}

func appendModule(mods []string, name string) []string {
	if slices.Contains(mods, name) {
		return mods
	}
	return append(mods, name)
}

func factName(table, field string) string {
	return table + ir.GoExportName(field) + "TextFact"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func boolLit(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
