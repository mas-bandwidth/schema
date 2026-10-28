// THE COOKED FORM's WRITE side in Squirrel (docs/SPEC-TABLES.md §7.6): the
// module surface around runtime/cookwrite.nut. CookMeasure and Cook write the
// cooked file of a loaded value, byte for byte as `schema cook` does, in both
// byte orders (docs/SPEC-TABLES.md §7.6). The layout constants are the
// cook-read side's (cookForms, the header words); this surface only wires the
// module's two write functions to the runtime.
package squirreltable

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
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
	// CookReason names a refusal, non_finite_float: <Owner>.<field> for a NaN
	// or an infinity (docs/SPEC-TABLES.md §3.3), null otherwise
	m.Printf("_M.CookReason <- null;\n")
	m.Printf("_M.CookMeasure <- function(root, value) { local n = _rt_cookwrite.CookMeasure(_M, root, value); _M.CookReason = _rt_cookwrite.CookReason; return n; };\n")
	m.Printf("_M.Cook <- function(root, value, bytes, order) { local ok = _rt_cookwrite.Cook(_M, root, value, bytes, order); _M.CookReason = _rt_cookwrite.CookReason; return ok; };\n")
	// The UNION OVERLAY's cook offsets (docs/SPEC-TABLES.md §2.6, §7.2): the
	// tag at the union's base and each arm at the union layout's arm offset.
	// The read-side cookForm carries the union field whole, so the write side
	// carries the arm facts the same ir.UnionLayout computed for the record.
	m.Printf("_M.cookWriteUnions <- {\n")
	for _, un := range unions(m.Closure) {
		_, _, tag, armOffset := ir.UnionLayout(m.Unit, un)
		m.Printf("  %s = { tagSize = %d, arms = [\n", squirrel.TableKey(un.Name), tag)
		for _, v := range un.Variants {
			if v.F == nil {
				m.Printf("    null,\n")
				continue
			}
			_, armSize := ir.ArmLayout(m.Unit, v)
			fl := ir.FieldLayout{Field: v.F, Offset: armOffset, Size: armSize}
			m.Printf("    %s,\n", cookField(m.Unit, fl))
		}
		m.Printf("  ] },\n")
	}
	m.Printf("};\n")
}

// cookWriteTable emits nothing per table; the surface is whole-module.
func cookWriteTable(m *Module, st *ir.Struct) {
}
