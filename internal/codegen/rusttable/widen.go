package rusttable

import "github.com/mas-bandwidth/schema/v2/ir"

func widenable(f *ir.Field) bool {
	if f.Type.Pointer {
		return false
	}
	k := ir.TableWireScalarKind(f)
	return k >= tkI16 && k <= tkI64 || k >= tkU16 && k <= tkU64 || k == tkF64
}

func (g *gen) emitWidenedScalar(f *ir.Field, reader, target, kind, onBad string) {
	g.pf("let bits = match %s.widened(%s) { Some(bits) => bits, None => { %s } };\n", reader, kind, onBad)
	typ := rustFieldType(f.Type)
	if f.Type.Kind == ir.TFloat64 {
		mutable := ""
		if f.HasFloatRange {
			mutable = "mut "
		}
		g.pf("let %sdecoded = f64::from_bits(bits);\n", mutable)
		if f.HasFloatRange {
			g.pf("if decoded < %s { decoded = %s; report.clamped += 1; } else if decoded > %s { decoded = %s; report.clamped += 1; }\n", formatFloat(f.FMin, false), formatFloat(f.FMin, false), formatFloat(f.FMax, false), formatFloat(f.FMax, false))
		}
	} else {
		mutable := ""
		if f.HasIntRange {
			lo, hi := clampEnds(f, tableKindWidth(ir.TableScalarKind(f)))
			if lo || hi {
				mutable = "mut "
			}
		}
		g.pf("let %sdecoded = bits as %s;\n", mutable, typ)
		if f.HasIntRange {
			lo, hi := clampEnds(f, tableKindWidth(ir.TableScalarKind(f)))
			if lo {
				g.pf("if decoded < %s { decoded = %s; report.clamped += 1; }\n", intLit(f.IntMin, typ), intLit(f.IntMin, typ))
			}
			if hi {
				g.pf("if decoded > %s { decoded = %s; report.clamped += 1; }\n", intLit(f.IntMax, typ), intLit(f.IntMax, typ))
			}
		}
	}
	g.pf("%s = decoded;\n", target)
}
