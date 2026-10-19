package squirreltable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE TEXT LIST'S TOTAL (docs/SPEC-TABLES.md §2.13), joined to the table module
// through [RegisterSurface] as the port's design has every form join it
// (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT). The wire walk needs none of
// these facts; the text-list writers and readers (runtime/save.nut,
// runtime/load.nut, runtime/json.nut) do, and read each as an optional key:
//
//	textList  a text list field (§2.13), whose element record's one field is
//	           the list's value
//	total     the sum bound, `| total = N` (§2.13), absent when none is declared
func init() {
	RegisterSurface(Surface{Name: "textlisttotal", Table: textListTotalTable})
}

// textListTotalTable emits the two facts a text list adds to its field
// descriptor; every other field descriptor is untouched.
func textListTotalTable(m *Module, st *ir.Struct) {
	for i, f := range st.Fields {
		if !f.IsTextList() {
			continue
		}
		target := fmt.Sprintf("_M.%sDescriptor.fields[%d]", st.Name, i)
		m.Printf("%s.textList <- true;\n", target)
		if f.TextTotal > 0 {
			m.Printf("%s.total <- %d;\n", target, f.TextTotal)
		}
	}
}
