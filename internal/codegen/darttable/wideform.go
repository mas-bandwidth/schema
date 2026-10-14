package darttable

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func wireWide(f *ir.Field) bool {
	return f.Type.Kind == ir.TFixed || f.Type.Kind == ir.TInt && f.Type.Width == 128
}
func wideBounds(f *ir.Field) (*big.Int, *big.Int) {
	bits := uint(f.Type.Width)
	lo := new(big.Int)
	hi := new(big.Int).Lsh(big.NewInt(1), bits)
	if f.Type.Signed {
		hi.Rsh(hi, 1)
		lo.Neg(hi)
	}
	hi.Sub(hi, big.NewInt(1))
	if a, b, ok := ir.TableRawRange(f); ok {
		if a.Cmp(lo) > 0 {
			lo.Set(a)
		}
		if b.Cmp(hi) < 0 {
			hi.Set(b)
		}
	}
	return lo, hi
}
func (g *wireGen) emitWideBareWrite(f *ir.Field, expr, writer, ind string) {
	lo, hi := wideBounds(f)
	g.pf("%s{final raw=%s;if(raw<%s||raw>%s)throw StateError('invalid_value');%s.putBigInt(raw,%d);}\n", ind, wideWireRawValue(f, expr), wideWireBigLiteral(lo), wideWireBigLiteral(hi), writer, f.Type.Width/8)
}
func (g *wireGen) emitSaveWide(f *ir.Field, expr, ind string) {
	g.pf("%sif(%s){w.putLeb(w.refFor(%s));w.putByte(%d);\n", ind, optionalRide(f, expr+" != "+wideWireDefault(f)), dartU64Literal(fieldId(f)), ir.TableWireScalarKind(f))
	g.emitWideBareWrite(f, expr, "w", ind+" ")
	g.pf("%s}\n", ind)
}
func (g *wireGen) emitWideReadValue(f *ir.Field, reader, kind, ind, failure string) {
	g.pf("%sfinal raw=%s.readBigInt(%s==18||%s==19||%s==24||%s==29?16:(%s==2||%s==6||%s==20||%s==25?1:(%s==3||%s==7||%s==21||%s==26?2:(%s==4||%s==8||%s==22||%s==27?4:8))),%v);if(raw==null){%s;}var value=raw;\n", ind, reader, kind, kind, kind, kind, kind, kind, kind, kind, kind, kind, kind, kind, kind, kind, kind, kind, f.Type.Signed, failure)
	if lo, hi, ok := wideWireRawBounds(f); ok {
		g.pf("%sif(value<%s){value=%s;r.report.clamped++;}else if(value>%s){value=%s;r.report.clamped++;}\n", ind, lo, lo, hi, hi)
	}
}
func (g *wireGen) emitLoadWide(f *ir.Field, ind string) {
	k := ir.TableWireScalarKind(f)
	g.pf("%sif(kind!=%d&&!_kindWidens(kind,%d)){r.report.kindMismatch++;if(!r.skipKind(kind)){r.report.malformed=true;return;}break;}\n%s{\n", ind, k, k, ind)
	g.emitWideReadValue(f, "r", "kind", ind+" ", "return")
	g.pf("%sif(kind!=%d)r.report.widened++;v.%s=%s;}break;\n", ind, k, fieldMember(f), wideWireStorageValue(f, "value"))
}
func (g *wireGen) emitWideListRead(f *ir.Field, dest, reader, ind string) {
	g.pf("%s{\n", ind)
	g.emitWideReadValue(f, reader, "elemKind", ind+" ", "break")
	g.pf("%s%s.add(%s);}\n", ind, dest, wideWireStorageValue(f, "value"))
}
func (g *wireGen) wideJSONRead(f *ir.Field, value, fallback string) string {
	lo, hi := wideBounds(f)
	if f.Type.Kind == ir.TFixed {
		return wideWireStorageValue(f, fmt.Sprintf("TableTextReport.fixedField(%s,r,%d,%s,%s,%s)", value, f.Type.FracBits, wideWireBigLiteral(lo), wideWireBigLiteral(hi), wideWireRawValue(f, fallback)))
	}
	return fmt.Sprintf("TableTextReport.bigIntegerField(%s,r,%s,%s,%s)", value, wideWireBigLiteral(lo), wideWireBigLiteral(hi), fallback)
}
func wideJSONText(f *ir.Field, expr string) string {
	if f.Type.Kind == ir.TFixed {
		return fmt.Sprintf("TableTextReport.fixedText(%s,%d)", wideWireRawValue(f, expr), f.Type.FracBits)
	}
	return expr + ".toString()"
}

const textBigIntegerMembers = `
  static BigInt bigIntegerField(dynamic input,TableTextReport r,BigInt lo,BigInt hi,BigInt fallback){
    final value=integerValue(input);if(value==null){r.kindMismatch++;return fallback;}
    if(value<lo){r.clamped++;return lo;}if(value>hi){r.clamped++;return hi;}return value;
  }
`
