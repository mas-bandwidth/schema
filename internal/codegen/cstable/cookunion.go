package cstable

import "github.com/mas-bandwidth/schema/v2/ir"

// The containing row remains sequential; the union alone owns an explicit
// unmanaged overlay. Every arm offset is the compiler's canonical C layout.
func (g *cookGen) emitUnionRow(un *ir.Union) {
	size, _, _, armAt := ir.UnionLayout(g.unit, un)
	g.sf("[StructLayout(LayoutKind.Explicit, Size = %d)]\npublic unsafe struct %sRow\n{\n    [FieldOffset(0)] public %s.%sType Type;\n", size, un.Name, capitalize(g.unit.Package), un.Name)
	for _, v := range un.Variants {
		if v.Void() {
			continue
		}
		f := v.F
		name := member(f)
		pieces := ir.FieldPieces(g.unit, f, armAt)
		at := pieces[0].Offset
		switch {
		case f.IsList(), f.IsMap():
			g.sf("    [FieldOffset(%d)] public TableCookList %s;\n", at, name)
		case f.Type.Pointer && f.Array == ir.ArrayNone:
			g.sf("    [FieldOffset(%d)] public long %s;\n", at, name)
		case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TWString:
			typ, n := "byte", f.Type.Size
			if f.Type.Kind != ir.TBytes {
				n++
			}
			if f.Type.Kind == ir.TWString {
				typ = "char"
			}
			g.sf("    [FieldOffset(%d)] public fixed %s %s[%d];\n", at, typ, name, n)
			g.sf("    [FieldOffset(%d)] public int %sLength;\n", pieces[1].Offset, name)
		case f.Array != ir.ArrayNone:
			typ := g.cookBlittableType(f.Type)
			if csFixedBufferPrimitive(typ) {
				g.sf("    [FieldOffset(%d)] public fixed %s %s[%d];\n", at, typ, name, f.ArrayBound)
			} else {
				step := pieces[0].Size / f.ArrayBound
				if f.ArrayBound > 128 {
					g.sf("    [FieldOffset(%d)] public %s %s0;\n", at, typ, name)
					g.sf("    [FieldOffset(%d)] private fixed byte %sStorage[%d];\n", at+step, name, (f.ArrayBound-1)*step)
				} else {
					for i := int64(0); i < f.ArrayBound; i++ {
						g.sf("    [FieldOffset(%d)] public %s %s%d;\n", at+i*step, typ, name, i)
					}
				}
			}
			if f.Array == ir.ArrayCounted {
				g.sf("    [FieldOffset(%d)] public int %sCount;\n", pieces[1].Offset, name)
			}
		default:
			g.sf("    [FieldOffset(%d)] public %s %s;\n", at, g.blittableType(f.Type), name)
		}
	}
	g.sf("}\n\n")
}
