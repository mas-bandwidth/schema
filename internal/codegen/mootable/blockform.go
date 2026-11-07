package mootable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE BLOCK FORM (docs/SPEC-TABLES.md §19), in moo: every block-form table
// this file declares gets <Name>BlockInfo, its projection's layout AS DATA —
// the descriptors §19.2's reflective read walks, with each row record and
// each nested record hung off its field's element column. The numbers are
// ir's one layout model (ir.Blocks, ir.BlockFieldOf, §19.3); the runtime,
// runtime/table_blockread.moo, opens a block against them and reads it.
func init() {
	Register("blockform", emitBlockForm)
}

func emitBlockForm(g *Gen) error {
	if !unitCarries(g.Unit, "block") {
		return nil
	}
	blocks := ir.Blocks(g.Unit)
	if blocks == nil {
		return nil
	}
	for _, bl := range blocks.Tables {
		if g.Unit.DeclFile[bl.Table.Name] != g.File.Base {
			continue
		}
		g.Pf("-- %sBlockInfo — the block form's projection of %s as data: offsets, sizes,\n", bl.Table.Name, bl.Table.Name)
		g.Pf("-- kinds and every row's layout (docs/SPEC-TABLES.md §19.2, §19.3)\n")
		g.Pf("%s.%sBlockInfo = %s.info({\n", g.Self, bl.Table.Name, g.Require("table_blockread"))
		g.Pf("\tname = %q,\n", bl.Table.DeclaredName())
		g.Pf("\tbuildVersion = %s,\n", g.K(WideNew("Uint64", new(big.Int).SetUint64(ir.BuildVersion(g.Unit)), 64)))
		if bl.Table.DeclaredForms() {
			g.Pf("\tforms = {")
			for _, form := range bl.Table.Forms {
				g.Pf(" %s = true,", form)
			}
			g.Pf(" },\n")
		}
		g.Pf("\tsize = %d,\n\talign = %d,\n", bl.Projection.Size, bl.Projection.Align)
		g.Pf("\tfields = %s,\n", blockFields(g, blocks, &bl.Projection, bl, "\t"))
		g.Pf("})\n\n")
	}
	return nil
}

// unitCarries reports whether any table of the unit carries a form
// (docs/SPEC-TABLES.md §2.12): the same rule runtimeGate emits the form's
// runtime by.
func unitCarries(u *ir.Unit, form string) bool {
	for _, t := range u.Tables {
		if !t.IsMapEntry() && t.CarriesForm(form) {
			return true
		}
	}
	return false
}

func blockOffset(v int64) string {
	if v < 0 {
		return "nil"
	}
	return fmt.Sprint(v)
}

// blockFields renders one record's field descriptors; bl is non-nil for a
// projection, whose out-of-line arrays are their (offset_of, count, stride)
// triples.
func blockFields(g *Gen, blocks *ir.BlockUnit, ml *ir.MemberLayout, bl *ir.BlockLayout, ind string) string {
	var b strings.Builder
	b.WriteString("{\n")
	projection := bl != nil
	for _, fl := range ml.Fields {
		f := fl.Field
		kind := ir.TableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = ir.TableKindU8
		}
		outOfLine := projection && ir.BlockOutOfLine(f)
		facts := ir.BlockFieldOf(g.Unit, f, fl.Offset, projection)
		fmt.Fprintf(&b, "%s\t{ name = %q, offset = %d, size = %d, kind = %d, ", ind, f.Name, fl.Offset, fl.Size, kind)
		if f.Type.Kind == ir.TWString {
			b.WriteString("wide = true, ")
		}
		fmt.Fprintf(&b, "isArray = %v, counted = %v, optional = %v, arrayBound = %d, elemSize = %d, countOffset = %s, presentOffset = %s",
			facts.IsArray, facts.Counted, facts.Optional, facts.ArrayBound, facts.ElemSize, blockOffset(facts.CountOffset), blockOffset(facts.PresentOffset))
		element := ""
		if outOfLine {
			a := bl.ArrayByName(f.Name)
			start := int64(ir.BlockAlign)
			if a.ElemAlign() > start {
				start = a.ElemAlign()
			}
			fmt.Fprintf(&b, ", outOfLine = true, offsetOfOffset = %d, strideOffset = %d, stride = %d, bound = %d, startAlign = %d",
				a.OffsetOfOffset, a.StrideOffset, a.Stride, a.Max, start)
			element = a.ElemName
		} else if ref, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed && blocks.Layout(ref.Name) != nil {
			element = ref.Name
		}
		if element != "" {
			el := blocks.Layout(element)
			st := g.Unit.Tables[element]
			if st == nil {
				st = g.Unit.Structs[element]
			}
			name := element
			if st != nil {
				name = st.DeclaredName()
			}
			fmt.Fprintf(&b, ", element = { name = %q, size = %d, align = %d, fields = %s }",
				name, el.Size, el.Align, blockFields(g, blocks, el, nil, ind+"\t"))
		}
		b.WriteString(" },\n")
	}
	b.WriteString(ind + "}")
	return b.String()
}
