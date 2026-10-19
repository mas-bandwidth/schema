// THE COOKED FORM in Squirrel (docs/SPEC-TABLES.md §7): the READ side,
// emitted as a surface of the table module. The runtime is runtime/cook.nut
// (schema_table_cook.nut beside the module); this file emits the per-table
// layout facts and the build version the runtime consults, mirroring
// internal/codegen/jstable/cook.go, the scripting-language precedent.
//
// A cook is a LOAD-TRUSTED-DATA-FROM-TOOLS format, not a wire. Open matches
// the header and returns the root where it lies: O(1) in the file's size, the
// bar §7 is built for. NOTHING DECLARES IT: every table gets an Open, and the
// module carries the layout facts only (docs/SPEC-TABLES.md §7).
package squirreltable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookMagic identifies a cooked file and carries the byte-order check with it
// (docs/SPEC-TABLES.md §7.1). It is "SCHMCOOK" read as ASCII in the byte order
// a little-endian store produces.
const cookMagic = uint64(0x4B4F4F434D484353)

// cookHeaderBytes is §7.1's header: eight u64 words.
const cookHeaderBytes = int64(64)

// cookMaxAlign is the ceiling on the header's `alignment` word (§7).
const cookMaxAlign = int64(64)

func init() {
	RegisterSurface(Surface{Name: "cook", Table: cookTable, Module: cookModule})
}

// cookModule emits the unit's shared cook facts once per module: the header
// constants and the build version, then the per-table layout map.
func cookModule(m *Module) {
	m.Runtime("cook")
	m.Printf("// THE COOKED FORM (docs/SPEC-TABLES.md §7): the READ surface. Open matches\n")
	m.Printf("// the header and returns the root where it lies; a read handle is the bytes\n")
	m.Printf("// and the region's offset and length.\n")
	m.Printf("_M.TableCookMagicBytes <- [%s];\n", littleEndianBytes(cookMagic))
	m.Printf("_M.TableCookByteOrderBytes <- [1, 0, 0, 0, 0, 0, 0, 0];\n")
	m.Printf("_M.TableCookHeaderBytes <- %d;\n", cookHeaderBytes)
	m.Printf("_M.TableCookMaxAlign <- %d;\n", cookMaxAlign)
	m.Printf("_M.BuildVersionBytes <- [%s];\n", littleEndianBytes(ir.BuildVersion(m.Unit)))
	m.Printf("_M.cookForms <- {\n")
	for _, st := range m.Closure {
		ml := ir.RecordLayout(m.Unit, st)
		m.Printf("  %s = { size = %d, align = %d },\n", squirrel.TableKey(st.Name), ml.Size, ml.Align)
	}
	m.Printf("};\n")
	m.Printf("_M.cookOpen <- function(root, bytes, report) { return _rt_cook.Cook(_M, root, bytes, report); };\n\n")
}

// cookTable emits nothing per table beyond the layout facts the module already
// holds; the surface is whole-module, not per-table.
func cookTable(m *Module, st *ir.Struct) {
}

// littleEndianBytes renders a uint64 as its eight little-endian bytes, the
// byte array the header comparison is done against (docs/SPEC-TABLES.md §7.1).
func littleEndianBytes(v uint64) string {
	parts := make([]string, 8)
	for i := 0; i < 8; i++ {
		parts[i] = fmt.Sprintf("%d", (v>>uint(i*8))&0xff)
	}
	return strings.Join(parts, ", ")
}
