package gotable

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func wideLanes(v *big.Int) (lo, hi *big.Int) {
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	return new(big.Int).And(u, mask), new(big.Int).Rsh(u, 64)
}

func wideLiteral(v *big.Int, signed bool) string {
	lo, hi := wideLanes(v)
	typ := "serialize.Uint128"
	if signed {
		typ = "serialize.Int128"
	}
	return fmt.Sprintf("(%s{Lo:%s, Hi:%s})", typ, lo, hi)
}

func (g *tableGen) emitReadWide(f *ir.Field, expr, rdr, kind, ind, onBad string) {
	g.pf("%sif !%s.Has(tableKindBytes(%s)) { %s }\n", ind, rdr, kind, onBad)
	g.pf("%s{\n%s var v %s\n", ind, ind, goFieldType(f.Type))
	g.pf("%s if tableKindBytes(%s) == 16 { v.Lo = %s.Get64(); v.Hi = %s.Get64() } else {\n", ind, kind, rdr, rdr)
	if f.Type.Signed {
		g.pf("%s  v = serialize.Int128From64(%s.Signed(%s))\n", ind, rdr, kind)
	} else {
		g.pf("%s  v.Lo = %s.Unsigned(%s)\n", ind, rdr, kind)
	}
	g.pf("%s }\n", ind)
	if f.HasIntRange {
		rlo, rhi, _ := ir.TableRawRange(f)
		lo, hi := wideLiteral(rlo, f.Type.Signed), wideLiteral(rhi, f.Type.Signed)
		g.pf("%s if v.Cmp(%s) < 0 { v = %s; r.Report.Clamped++ } else if v.Cmp(%s) > 0 { v = %s; r.Report.Clamped++ }\n", ind, lo, lo, hi, hi)
	}
	g.pf("%s %s = v\n%s}\n", ind, expr, ind)
}
