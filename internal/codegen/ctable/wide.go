package ctable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// C's 128-bit storage is serialize.c's two explicit lanes on every host.
// No native integer extension or host byte order enters the wire.
func unitHasWideStorage(u *ir.Unit) bool {
	names := ir.TableClosure(u)
	for name := range ir.TableClosureVocabulary(u) {
		names[name] = true
	}
	for name := range names {
		un := u.TableUnions[name]
		if un == nil {
			un = u.Unions[name]
		}
		if un != nil {
			for _, v := range un.Variants {
				if !v.Void() && v.F.Type.Width == 128 {
					return true
				}
			}
		}
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.Type.Width == 128 {
				return true
			}
		}
	}
	return false
}

func wideLanes(v *big.Int) (*big.Int, *big.Int) {
	n := new(big.Int).Set(v)
	if n.Sign() < 0 {
		n.Add(n, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	return new(big.Int).And(n, mask), new(big.Int).Rsh(n, 64)
}

func tableWideLit(v *big.Int, signed bool) string {
	lo, hi := wideLanes(v)
	prefix := "serialize_uint128"
	if signed {
		prefix = "serialize_int128"
	}
	return fmt.Sprintf("%s_make( %sull, %sull )", prefix, hi, lo)
}

func byteLiterals(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return strings.Join(parts, ", ")
}

func (g *tableGen) wireDefaultEquals(f *ir.Field, expr string) string {
	if un, ok := f.Type.Ref.(*ir.Union); ok {
		return expr + ".type == " + enumNoneConst(un.Name+"Type")
	}
	if f.Type.Width == 128 {
		prefix := "serialize_uint128"
		if f.Type.Signed {
			prefix = "serialize_int128"
		}
		return fmt.Sprintf("%s_equal( %s, %s )", prefix, expr, g.fieldDefaultExpr(f))
	}
	return expr + " == " + g.fieldDefaultExpr(f)
}

func (g *tableGen) wireBytesDiffer(f *ir.Field, expr string) string {
	if len(f.DefBytes) == 0 {
		return expr + "_length != 0"
	}
	g.pf("        static const uint8_t initial[] = { %s };\n", byteLiterals(f.DefBytes))
	return fmt.Sprintf("%s_length != %d || memcmp( %s, initial, %d ) != 0", expr, len(f.DefBytes), expr, len(f.DefBytes))
}

func (g *tableGen) wireWideRead(f *ir.Field, source int, dst, rdr, ind, onBad string) {
	width := tableKindWidth(source)
	typ := g.cFieldType(f.Type)
	g.pf("%sif ( !table_reader_has( &%s, %d ) ) { %s }\n", ind, rdr, width, onBad)
	g.pf("%s%s decoded_wide;\n", ind, typ)
	if width == 16 {
		g.pf("%sdecoded_wide.lo = table_reader_get64( &%s ); decoded_wide.hi = table_reader_get64( &%s );\n", ind, rdr, rdr)
	} else if ir.TableKindSigned(source) {
		g.pf("%sint64_t narrow = (int%d_t) %s( &%s );\n", ind, width*8, tableGet(width), rdr)
		g.pf("%sdecoded_wide.lo = (uint64_t) narrow; decoded_wide.hi = narrow < 0 ? UINT64_MAX : 0;\n", ind)
	} else {
		g.pf("%sdecoded_wide.lo = %s( &%s ); decoded_wide.hi = 0;\n", ind, tableGet(width), rdr)
	}
	if lo, hi, ok := ir.TableRawRange(f); ok {
		for _, bound := range []struct {
			v  *big.Int
			op string
		}{{lo, "<"}, {hi, ">"}} {
			g.pf("%s{\n%s    %s bound = %s;\n", ind, ind, typ, tableWideLit(bound.v, f.Type.Signed))
			cmp := fmt.Sprintf("decoded_wide.hi %s bound.hi || (decoded_wide.hi == bound.hi && decoded_wide.lo %s bound.lo)", bound.op, bound.op)
			if f.Type.Signed {
				cmp = fmt.Sprintf("serialize_int128_compare( decoded_wide, bound ) %s 0", bound.op)
			}
			g.pf("%s    if ( %s ) { decoded_wide = bound; r->report->clamped++; }\n%s}\n", ind, cmp, ind)
		}
	}
	g.pf("%s%s = decoded_wide;\n", ind, dst)
}
