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
		m.Printf("  %s = { name = %s, size = %d, align = %d, fields = [\n",
			squirrel.TableKey(st.Name), squirrel.StringLit([]byte(st.Name)), ml.Size, ml.Align)
		for _, fl := range ml.Fields {
			m.Printf("    %s,\n", cookField(m.Unit, fl))
		}
		m.Printf("  ] },\n")
	}
	m.Printf("};\n")
	m.Printf("_M.cookOpen <- function(root, bytes, report, lead = null) { return _rt_cook.Cook(_M, root, bytes, report, lead); };\n\n")
}

// cookTable emits nothing per table beyond the layout facts the module already
// holds; the surface is whole-module, not per-table.
func cookTable(m *Module, st *ir.Struct) {
}

// cookField is one field's position inside a cooked record (docs/SPEC-TABLES.md
// §7.2): the storage offset and size the C ABI model computed, what a slot
// HOLDS, and the companions a walk needs — a count, a presence, an element
// size, and the record a reference or a nested value names. It mirrors
// internal/codegen/jstable's emitRecordDescriptor.
func cookField(u *ir.Unit, fl ir.FieldLayout) string {
	f := fl.Field
	isArray := f.KeyEnum != "" || f.Array != ir.ArrayNone
	bound := int64(1)
	elemSize := fl.Size
	countOffset := int64(-1)
	presentOffset := int64(-1)
	if isArray && f.ArrayBound > 0 {
		bound = f.ArrayBound
		elemSize = ir.FieldPieces(u, f, fl.Offset)[0].Size / bound
	}
	if f.Array == ir.ArrayCounted {
		pieces := ir.FieldPieces(u, f, fl.Offset)
		countOffset = pieces[len(pieces)-1].Offset
		if f.Type.Optional {
			countOffset = pieces[len(pieces)-2].Offset
		}
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString {
		pieces := ir.FieldPieces(u, f, fl.Offset)
		if len(pieces) > 1 {
			countOffset = pieces[1].Offset
		}
	}
	if f.Type.Optional {
		pieces := ir.FieldPieces(u, f, fl.Offset)
		presentOffset = pieces[len(pieces)-1].Offset
	}
	table := "null"
	if f.Type.Kind == ir.TNamed {
		if ref, ok := f.Type.Ref.(*ir.Struct); ok {
			table = squirrel.StringLit([]byte(ref.Name))
		}
	}
	return fmt.Sprintf("{ name = %s, offset = %d, size = %d, elemSize = %d, storage = %s, table = %s, isArray = %t, bound = %d, countOffset = %d, presentOffset = %d, isPointer = %t }",
		squirrel.StringLit([]byte(f.Name)), fl.Offset, fl.Size, elemSize, squirrel.StringLit([]byte(cookStorage(f))),
		table, isArray, bound, countOffset, presentOffset, f.Type.Pointer)
}

// cookStorage is what a cooked SLOT holds, which is not always what the wire
// carries: an enum's slot holds its ORDINAL at the enum's derived storage width
// (docs/SPEC-TABLES.md §7.2). It mirrors jstable's cookStorageKind.
func cookStorage(f *ir.Field) string {
	if f.Type.Pointer {
		return "Reference"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "Bool"
	case ir.TFloat32, ir.TFloat64:
		return "Float"
	case ir.TString:
		return "String"
	case ir.TBytes:
		return "Bytes"
	case ir.TWString:
		// WIDE TEXT's slot is char16_t[N + 1] and an int32 used length in
		// CODE UNITS (docs/SPEC-TABLES.md §7.2): two bytes a unit
		return "WString"
	case ir.TBits:
		return "Unsigned"
	case ir.TInt:
		if f.Type.Signed {
			return "Signed"
		}
		return "Unsigned"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return "Unsigned"
		case *ir.Struct:
			return "Record"
		}
	}
	return "Record"
}

// littleEndianBytes renders a uint64 as its eight little-endian bytes, the
// byte array the header comparison is done against (docs/SPEC-TABLES.md §7.1).
func littleEndianBytes(v uint64) string {
	parts := make([]string, 8)
	for i := range 8 {
		parts[i] = fmt.Sprintf("%d", (v>>uint(i*8))&0xff)
	}
	return strings.Join(parts, ", ")
}
