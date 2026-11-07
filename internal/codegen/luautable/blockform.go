// THE BLOCK FORM'S DESCRIPTORS (docs/SPEC-TABLES.md §19.1, §19.2, §8.1), in
// Luau: every fixed table's block layout as data, the Info that the reflective
// reader runtime/schema_blockread.luau opens and walks. It is emitted ON THE
// SIDE, one <Base>Block.luau module per schema file that declares a table with
// a block form, exactly as Go, C++ and C# emit <Base>Block.*: the ordinary
// <Base>Table.luau carries none of it, so a build that never opens a block
// requires nothing of the form (§2.2).
//
// Every offset, size, kind and companion comes from the one layout model
// (ir.Blocks, ir.BlockFieldOf); nothing here derives a layout of its own.
package luautable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// GenerateBlock returns <Base>Block.luau for every schema file that declares a
// table carrying the block form: one `<Table>Block` Info per such table,
// which schema_blockread's BlockOpen and Read take.
func GenerateBlock(u *ir.Unit) (map[string][]byte, error) {
	out := map[string][]byte{}
	blocks := ir.Blocks(u)
	if blocks == nil {
		return out, nil
	}
	for _, f := range u.Files {
		var mine []*ir.BlockLayout
		for _, bl := range blocks.Tables {
			if u.DeclFile[bl.Table.Name] == f.Base && bl.Table.CarriesForm("block") {
				mine = append(mine, bl)
			}
		}
		if len(mine) == 0 {
			continue
		}
		g := luau.NewGen(u, f)
		g.Self, g.Suffix, g.Kind = "_B", "Block", "block form"
		for _, bl := range mine {
			name := bl.Table.Name
			g.Pf("-- %sBlock: %s's block form as data (docs/SPEC-TABLES.md §19.1, §19.2):\n", name, name)
			g.Pf("-- the projection, prologue included, and each out-of-line array's row.\n")
			g.Pf("%s.%sBlock = table.freeze(%s)\n\n", g.Self, name, blockRecord(u, blocks, &bl.Projection, bl, true, "\t"))
		}
		out[f.Base+"Block.luau"] = g.Assemble()
	}
	return out, nil
}

// blockRecord renders one record of the block closure: the projection when
// projection is set (its out-of-line arrays are triples there), a row or a
// by-value member otherwise.
func blockRecord(u *ir.Unit, blocks *ir.BlockUnit, ml *ir.MemberLayout, bl *ir.BlockLayout, projection bool, indent string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{\n%sname = %q,\n%sbuildVersion = %s,\n%ssize = %d,\n%salign = %d,\n%sfields = {\n",
		indent, ml.Name, indent, luau.IntegerLit(new(big.Int).SetUint64(ir.BuildVersion(u))), indent, ml.Size, indent, ml.Align, indent)
	inner := indent + "\t"
	for _, fl := range ml.Fields {
		f := fl.Field
		facts := ir.BlockFieldOf(u, f, fl.Offset, projection)
		outOfLine := projection && ir.BlockOutOfLine(f)
		var elem string
		if outOfLine {
			a := bl.ArrayByName(f.Name)
			elem = blockRecord(u, blocks, blocks.Layout(a.ElemName), nil, false, inner+"\t")
		} else if ref, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
			if row := blocks.Layout(ref.Name); row != nil {
				elem = blockRecord(u, blocks, row, nil, false, inner+"\t")
			}
		}
		fmt.Fprintf(&b, "%s{ name = %q, offset = %d, size = %d, kind = %d, outOfLine = %t, isArray = %t, arrayBound = %d, elemSize = %d",
			inner, f.Name, fl.Offset, fl.Size, ir.TableScalarKind(f), outOfLine, facts.IsArray, facts.ArrayBound, facts.ElemSize)
		if facts.CountOffset >= 0 {
			fmt.Fprintf(&b, ", countOffset = %d", facts.CountOffset)
		}
		if facts.PresentOffset >= 0 {
			fmt.Fprintf(&b, ", presentOffset = %d, optional = true", facts.PresentOffset)
		}
		if outOfLine {
			a := bl.ArrayByName(f.Name)
			fmt.Fprintf(&b, ", offsetOfOffset = %d, strideOffset = %d, stride = %d, bound = %d, elemAlign = %d",
				a.OffsetOfOffset, a.StrideOffset, a.Stride, a.Max, a.ElemAlign())
		}
		if elem != "" {
			fmt.Fprintf(&b, ", element = %s", elem)
		}
		b.WriteString(" },\n")
	}
	fmt.Fprintf(&b, "%s},\n%s}", indent, indent[:len(indent)-1])
	return b.String()
}
