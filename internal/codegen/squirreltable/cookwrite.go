// THE COOKED FORM's WRITE side in Squirrel (docs/SPEC-TABLES.md §7.6): the
// module surface around runtime/cookwrite.nut. CookMeasure and Cook write the
// cooked file of a loaded value, byte for byte as `schema cook` does, in both
// byte orders (docs/SPEC-TABLES.md §7.6). The layout constants are the
// cook-read side's (cookForms, the header words); this surface only wires the
// module's two write functions to the runtime.
package squirreltable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	RegisterSurface(Surface{Name: "cookwrite", Table: cookWriteTable, Module: cookWriteModule})
}

// cookWriteModule emits the unit's shared cook write surface once per module:
// the two functions, backed by the cookwrite runtime.
func cookWriteModule(m *Module) {
	m.Runtime("cookwrite")
	m.Printf("// THE COOKED FORM's WRITE side (docs/SPEC-TABLES.md §7.6): CookMeasure\n")
	m.Printf("// answers the whole cooked file's bytes, Cook writes it into the caller's\n")
	m.Printf("// buffer in the byte order the target build reads, and both use the layout\n")
	m.Printf("// facts the read side emits (cookForms, the header words).\n")
	m.Printf("_M.CookMeasure <- function(root, value) { return _rt_cookwrite.CookMeasure(_M, root, value); };\n")
	m.Printf("_M.Cook <- function(root, value, bytes, order) { return _rt_cookwrite.Cook(_M, root, value, bytes, order); };\n")
}

// cookWriteTable emits nothing per table; the surface is whole-module.
func cookWriteTable(m *Module, st *ir.Struct) {
}
