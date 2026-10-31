// RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6): the per-root LoadRetain,
// MeasureRetain and SaveRetain, and the KNOWN-ID SET the save-side interner
// reads to tell an id this build can name (its entry comes from the generated
// table) from a retained one (its entry comes from the caller's list, bounded
// by the store's idCapacity).
package squirreltable

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	RegisterSurface(Surface{Name: "retain", Table: retainTable, Module: retainModule})
}

func retainModule(m *Module) {
	m.Runtime("retain")
	m.Printf("// THE KNOWN IDS (§6.6): every id this unit's closure can name.\n")
	m.Printf("_M.TableRetainKnown <- {")
	for _, id := range ir.TableWireIds(m.Unit) {
		m.Printf(" [%s] = true,", squirrel.HexLit(id))
	}
	m.Printf(" };\n\n")
}

func retainTable(m *Module, st *ir.Struct) {
	if !st.IsTable {
		return
	}
	rt := m.Runtime("retain")
	l := squirrel.StringLit([]byte(st.Name))
	m.Printf("_M.%sLoadRetain <- function(bytes, store, report) { return %s.LoadRetain(_M, %s, bytes, store, report); };\n", st.Name, rt, l)
	m.Printf("_M.%sMeasureRetain <- function(v, store) { return %s.MeasureRetain(_M, %s, v, store); };\n", st.Name, rt, l)
	m.Printf("_M.%sSaveRetain <- function(v, store, out, report) { return %s.SaveRetain(_M, %s, v, store, out, report); };\n", st.Name, rt, l)
}
