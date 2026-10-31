package mootable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE COOKED FORM'S READ (docs/SPEC-TABLES.md §7, §7.1, §7.2), in moo: every
// record and union whose descriptor this module carries also gets its COOKED
// LAYOUT as data — each field's storage pieces at their C ABI offsets, an
// element's size, a union's tag width and arm offset — from ir's one layout
// model (ir.RecordLayout, ir.BlockFieldPieceOffsets, ir.UnionLayout; §19.3),
// registered with runtime/table_cookread.moo, which opens a cook and reads
// its region through them. Emitted only in a unit with a table carrying the
// cook form (§2.12), the same rule its runtime is emitted by.
func init() {
	Register("cookform", emitCookForm)
}

func emitCookForm(g *Gen) error {
	if !unitCarries(g.Unit, "cook") {
		return nil
	}
	ck := g.Require("table_cookread")
	g.Pf("-- the cooked form's layouts (docs/SPEC-TABLES.md §7.2): every record and union\n")
	g.Pf("-- this module describes, its storage pieces at their C ABI offsets\n")
	g.Pf("%s.unit(%s)\n", ck, g.K(WideNew("Uint64", new(big.Int).SetUint64(ir.BuildVersion(g.Unit)), 64)))
	reach := closure(g.Unit)
	for _, un := range g.File.TableUnions {
		emitCookUnion(g, ck, un)
	}
	for _, t := range g.File.Tables {
		emitCookRecord(g, ck, t)
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if !d.IsTable && reach[d.Name] {
				emitCookRecord(g, ck, d)
			}
		case *ir.Union:
			if g.Unit.TableUnions[d.Name] == nil && reach[d.Name] {
				emitCookUnion(g, ck, d)
			}
		}
	}
	g.Pf("\n")
	return nil
}

// cookPieces is one field's storage: the absolute offset of each contiguous
// piece from `at` (the value first, then a used length or count, then the
// presence bool), and the size of ONE element where the field holds several
// (a fixed or counted array's slot, a list's or a map's element record), and
// for a list or a map the element's alignment, which the write side's extent
// carve places the array at (§2.9, §7.6).
func cookPieces(g *Gen, f *ir.Field, at int64) string {
	var offs []string
	width := int64(0)
	for i, p := range ir.BlockFieldPieceOffsets(g.Unit, f, at, false) {
		offs = append(offs, fmt.Sprint(p.Offset))
		if i == 0 {
			width = p.Size
		}
	}
	elem, ealign := int64(0), int64(0)
	switch {
	case f.IsMap():
		ml := ir.RecordLayout(g.Unit, f.MapEntry)
		elem, ealign = ml.Size, ml.Align
	case f.IsList():
		// an unbounded array's elements ride in the holder's extent (§2.9),
		// at the element's own size and alignment: the write side carves them
		elem, ealign = ir.ListElementLayout(g.Unit, f)
	case f.Array != ir.ArrayNone:
		one := *f
		one.Array = ir.ArrayFixed
		one.ArrayBound = 1
		one.KeyEnum = ""
		one.KeyEnumRef = nil
		one.Type.Optional = false
		ps := ir.BlockFieldPieceOffsets(g.Unit, &one, 0, false)
		if len(ps) > 0 {
			elem = ps[0].Size
		}
	}
	if ealign != 0 {
		return fmt.Sprintf("{ p = { %s }, w = %d, elem = %d, ealign = %d }", strings.Join(offs, ", "), width, elem, ealign)
	}
	return fmt.Sprintf("{ p = { %s }, w = %d, elem = %d }", strings.Join(offs, ", "), width, elem)
}

func emitCookRecord(g *Gen, ck string, st *ir.Struct) {
	ml := ir.RecordLayout(g.Unit, st)
	var fields []string
	for _, fl := range ml.Fields {
		fields = append(fields, cookPieces(g, fl.Field, fl.Offset))
	}
	g.Pf("%s.record(%q, { size = %d, align = %d, fields = { %s } })\n", ck, st.Name, ml.Size, ml.Align, strings.Join(fields, ", "))
}

func emitCookUnion(g *Gen, ck string, un *ir.Union) {
	size, align, tag, armOffset := ir.UnionLayout(g.Unit, un)
	var arms []string
	for _, v := range un.Variants {
		if v.Void() {
			arms = append(arms, "{ p = {}, w = 0, elem = 0 }")
			continue
		}
		arms = append(arms, cookPieces(g, armField(v), 0))
	}
	g.Pf("%s.union(%q, { size = %d, align = %d, tag = %d, armOffset = %d, arms = { %s } })\n", ck, un.Name, size, align, tag, armOffset, strings.Join(arms, ", "))
}
