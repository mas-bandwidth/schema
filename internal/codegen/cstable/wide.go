package cstable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Native 128-bit values keep the standalone table runtime on the BCL. Packet
// closure storage converts through the runtime pair's .NET 7+ operators.
func wideLiteral(v *big.Int, signed bool) string {
	n := new(big.Int).Set(v)
	if n.Sign() < 0 {
		n.Add(n, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	hi := new(big.Int).Rsh(new(big.Int).Set(n), 64).Uint64()
	typ := "UInt128"
	if signed {
		typ = "Int128"
	}
	return fmt.Sprintf("unchecked((%s)(((UInt128)0x%xul << 64) | 0x%xul))", typ, hi, n.Uint64())
}

func wideColumns(f *ir.Field) string {
	def := big.NewInt(0)
	if f.HasDefault && f.DefInt != nil {
		def = f.DefInt
	}
	var b strings.Builder
	fmt.Fprintf(&b, ", DefaultWide = %s, WideSigned = %v, FracBits = %d", wideLiteral(def, false), f.Type.Signed, f.Type.FracBits)
	lo, hi, bounded := ir.TableRawRange(f)
	if bounded {
		typ := "UInt128"
		if f.Type.Signed {
			typ = "Int128"
		}
		fmt.Fprintf(&b, ", ClampWide = delegate(UInt128 raw, TableReport r) { %s v = unchecked((%s)raw); if (v < %s) { r.Clamped++; return %s; } if (v > %s) { r.Clamped++; return %s; } return raw; }", typ, typ, wideLiteral(lo, f.Type.Signed), wideLiteral(lo, false), wideLiteral(hi, f.Type.Signed), wideLiteral(hi, false))
	}
	return b.String()
}

func byteLiterals(bytes []byte) string {
	parts := make([]string, len(bytes))
	for i, v := range bytes {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return strings.Join(parts, ", ")
}

func (g *tableGen) emitBufferDefault(f *ir.Field, prefix string, emit func(string, ...any)) {
	if len(f.DefBytes) == 0 {
		return
	}
	for i, b := range f.DefBytes {
		emit("        %s%s[%d] = 0x%02x;\n", prefix, member(f), i, b)
	}
	emit("        %s%sLength = %d;\n", prefix, member(f), len(f.DefBytes))
}
