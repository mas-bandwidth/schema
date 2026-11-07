// THE BLOCK FORM in Squirrel (docs/SPEC-TABLES.md §19): the READ side,
// emitted as a surface of the table module. The runtime is runtime/block.nut
// (schema_table_block.nut beside the module); this file emits the per-table
// projection and out-of-line array descriptors and the row record layouts the
// runtime consults, mirroring internal/codegen/jstable/block.go, the
// scripting-language precedent.
//
// NOTHING DECLARES THE BLOCK FORM (docs/SPEC-TABLES.md §19.1, §19.2). Every
// fixed table whose closure carries no union or map entry has one: Open
// matches the prologue, bounds every out-of-line array triple inside the
// caller's extent, and returns the handle where it lies.
package squirreltable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	RegisterSurface(Surface{Name: "block", Table: blockTable, Module: blockModule})
}

// blockModule emits the unit's shared block facts once per module: the
// prologue constants, the row record layouts in the block closure, the
// per-table block projection and array descriptors, and blockOpen.
func blockModule(m *Module) {
	m.Runtime("block")
	blocks := ir.Blocks(m.Unit)
	m.Printf("// THE BLOCK FORM (docs/SPEC-TABLES.md §19): the READ surface. Open matches\n")
	m.Printf("// the prologue and bounds every out-of-line array triple in O(1); a read\n")
	m.Printf("// handle is the bytes, the used extent, and the form descriptor.\n")
	m.Printf("_M.TableBlockMagicBytes <- [%s];\n", littleEndianBytes(ir.BlockMagic))
	m.Printf("_M.TableBlockByteOrderBytes <- [1, 0, 0, 0, 0, 0, 0, 0];\n")
	m.Printf("_M.TableBlockAlign <- %d;\n", ir.BlockAlign)
	m.Printf("_M.blockRecords <- {\n")
	if blocks != nil {
		for _, name := range blocks.Order {
			ml := blocks.Layout(name)
			if ml == nil {
				continue
			}
			m.Printf("  %s = { name = %s, size = %d, align = %d, fields = [\n",
				squirrel.TableKey(name), squirrel.StringLit([]byte(name)), ml.Size, ml.Align)
			for _, fl := range ml.Fields {
				m.Printf("    %s,\n", blockField(m.Unit, fl, nil))
			}
			m.Printf("  ] },\n")
		}
	}
	m.Printf("};\n")
	m.Printf("_M.blockForms <- {\n")
	if blocks != nil {
		for _, bl := range blocks.Tables {
			name := bl.Table.Name
			m.Printf("  %s = {\n", squirrel.TableKey(name))
			m.Printf("    name = %s,\n", squirrel.StringLit([]byte(name)))
			m.Printf("    maxBytes = %d,\n", bl.MaxBytes)
			m.Printf("    projection = { name = %s, size = %d, align = %d, fields = [\n",
				squirrel.StringLit([]byte(name)), bl.Projection.Size, bl.Projection.Align)
			for _, fl := range bl.Projection.Fields {
				m.Printf("      %s,\n", blockField(m.Unit, fl, bl))
			}
			m.Printf("    ] },\n")
			m.Printf("    arrays = [\n")
			for _, a := range bl.Arrays {
				align := int64(ir.BlockAlign)
				if a.ElemAlign() > align {
					align = a.ElemAlign()
				}
				m.Printf("      { name = %s, elem = %s, stride = %d, max = %d, align = %d, offsetOfOffset = %d, countOffset = %d, strideOffset = %d },\n",
					squirrel.StringLit([]byte(a.Field.Name)),
					squirrel.StringLit([]byte(a.ElemName)),
					a.Stride, a.Max, align, a.OffsetOfOffset, a.CountOffset, a.StrideOffset)
			}
			m.Printf("    ],\n")
			m.Printf("  },\n")
		}
	}
	m.Printf("};\n")
	m.Printf("_M.blockOpen <- function(root, bytes, report, lead = null) { return _rt_block.Block(_M, root, bytes, report, lead); };\n\n")
}

// blockTable emits nothing per table beyond the layout facts the module already
// holds; the surface is whole-module, not per-table.
func blockTable(m *Module, st *ir.Struct) {
}

// blockField is one field's position inside a block projection or row record
// (docs/SPEC-TABLES.md §19.2, §19.3): the storage offset and size the C ABI
// model computed, what a slot holds, and the companions a generic walk needs.
func blockField(u *ir.Unit, fl ir.FieldLayout, bl *ir.BlockLayout) string {
	f := fl.Field
	facts := ir.BlockFieldOf(u, f, fl.Offset, bl != nil)
	if bl != nil {
		if a := bl.ArrayByName(f.Name); a != nil {
			return fmt.Sprintf("{ name = %s, offset = %d, size = %d, elemSize = 0, storage = \"Array\", table = %s, outOfLine = true, offsetOfOffset = %d, countOffset = %d, strideOffset = %d, stride = %d, isArray = true, counted = true, optional = false, bound = %d, presentOffset = -1 }",
				squirrel.StringLit([]byte(f.Name)), fl.Offset, fl.Size, squirrel.StringLit([]byte(a.ElemName)),
				a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride, a.Max)
		}
	}
	table := "null"
	if f.Type.Kind == ir.TNamed && !f.Type.Pointer {
		if ref, ok := f.Type.Ref.(*ir.Struct); ok {
			table = squirrel.StringLit([]byte(ref.Name))
		}
	}
	return fmt.Sprintf("{ name = %s, offset = %d, size = %d, elemSize = %d, storage = %s, table = %s, outOfLine = false, offsetOfOffset = -1, countOffset = %d, strideOffset = -1, stride = 0, isArray = %t, counted = %t, optional = %t, bound = %d, presentOffset = %d }",
		squirrel.StringLit([]byte(f.Name)), fl.Offset, fl.Size, facts.ElemSize, squirrel.StringLit([]byte(cookStorage(f))),
		table, facts.CountOffset, facts.IsArray, facts.Counted, facts.Optional, facts.ArrayBound, facts.PresentOffset)
}
