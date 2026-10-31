// The BLOCK form in Lobster: the builder (docs/SPEC-TABLES.md §19.1) and the
// reflective read (§19.2), against the layout model ir.Blocks computes
// (docs/PORT-LOBSTER.md, THE REGISTER: M7, I5).
//
// Nothing declares the form. Every FIXED table has one, and this file emits
// its layout AS DATA into the table module beside the descriptors the load and
// save walks read. The runtime is runtime/block_form.lobster: one walk over
// the data, never per-field codec logic.
package lobstertable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("block-form", emitBlockForm) }

// emitBlockForm joins every table module with the block layout of each fixed
// table it declares. A table without a block form emits nothing: it is
// refused by absence (§19), not by a diagnostic.
func emitBlockForm(g *lobster.Gen) error {
	blocks := ir.Blocks(g.Unit)
	if blocks == nil {
		return nil
	}
	emitted := false
	for _, st := range g.File.Tables {
		bl := blocks.Block(st.Name)
		if bl == nil {
			continue
		}
		emitBlockLayout(g, blocks, bl)
		emitted = true
	}
	if emitted {
		g.Import("block_form")
	}
	return nil
}

// emitBlockLayout writes <Table>Block: the projection, its out-of-line arrays'
// triples and every row record, as data the one runtime walk reads (§19.2).
func emitBlockLayout(g *lobster.Gen, blocks *ir.BlockUnit, bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.Pf("// %sBlock is %s's block layout as data (docs/SPEC-TABLES.md §19.1,\n", name, name)
	g.Pf("// §19.2): the projection's own offsets, each out-of-line array's triple,\n")
	g.Pf("// every row record, and the storage the declared maxima size.\n")
	g.Pf("let %sBlock = %s.register_block(%s.BlockLayout {\n", name, rt, rt)
	g.Pf("    table: %s,\n", lobster.StringLit([]byte(name)))
	g.Pf("    max_bytes: %d,\n", bl.MaxBytes)
	g.Pf("    build_version: %s,\n", lobster.IntLit(new(big.Int).SetUint64(ir.BuildVersion(g.Unit))))
	g.Pf("    projection: %s,\n", blockRecord(g, blocks, &bl.Projection, name, true))
	g.Pf("    records: [")
	for _, rn := range blocks.Order {
		g.Pf("\n        %s,", blockRecord(g, blocks, blocks.Layout(rn), rn, false))
	}
	g.Pf("\n    ]::%s.BlockRecord,\n", rt)
	g.Pf("})\n\n")
}

// blockRecord is one record's C-ABI layout as data: its size and alignment,
// and one line per field carrying where it starts and everything a generic
// row walk needs after that (ir.BlockFieldFacts, §19.3).
func blockRecord(g *lobster.Gen, blocks *ir.BlockUnit, ml *ir.MemberLayout, name string, projection bool) string {
	if ml == nil {
		return fmt.Sprintf("%s.BlockRecord { name: %s }", rt, lobster.StringLit([]byte(name)))
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s.BlockRecord {\n                name: %s,\n                size: %d,\n                align: %d,\n                fields: [",
		rt, lobster.StringLit([]byte(name)), ml.Size, ml.Align)
	for i, fl := range ml.Fields {
		f := fl.Field
		facts := ir.BlockFieldOf(g.Unit, f, fl.Offset, projection)
		outOfLine := projection && ir.BlockOutOfLine(f)
		elem := ""
		if outOfLine {
			elem = f.Type.Name
		} else if f.Type.Kind == ir.TNamed {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && blocks.Layout(ref.Name) != nil {
				elem = ref.Name
			}
		}
		offOf, countOff, strideOff, stride := int64(-1), facts.CountOffset, int64(-1), int64(0)
		maxRows, startAlign := int64(0), int64(ir.BlockAlign)
		if outOfLine {
			a := blArray(blocks, name, f.Name)
			if a != nil {
				offOf, countOff, strideOff, stride = a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride
				maxRows = a.Max
				if a.ElemAlign() > startAlign {
					startAlign = a.ElemAlign()
				}
			}
		}
		sep := ","
		if i == len(ml.Fields)-1 {
			sep = ""
		}
		fmt.Fprintf(&out, "\n                    %s.BlockField { name: %s, offset: %d, size: %d, kind: %d, out_of_line: %v, offset_of_offset: %d, count_offset: %d, stride_offset: %d, stride: %d, is_array: %v, counted: %v, optional: %v, bound: %d, elem_size: %d, present_offset: %d, elem: %s, max: %d, start_align: %d }%s",
			rt, lobster.StringLit([]byte(f.Name)), fl.Offset, fl.Size, ir.TableWireFieldKind(f),
			outOfLine, offOf, countOff, strideOff, stride,
			facts.IsArray, facts.Counted, facts.Optional, facts.ArrayBound, facts.ElemSize, facts.PresentOffset,
			lobster.StringLit([]byte(elem)), maxRows, startAlign, sep)
	}
	out.WriteString("\n                ]::" + rt + ".BlockField,\n            }")
	return out.String()
}

// blArray finds one out-of-line array by field name in the table's layout.
func blArray(blocks *ir.BlockUnit, table, field string) *ir.BlockArray {
	bl := blocks.Block(table)
	if bl == nil {
		return nil
	}
	return bl.ArrayByName(field)
}
