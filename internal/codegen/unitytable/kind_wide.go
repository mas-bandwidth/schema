package unitytable

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// kindWide carries the 128-bit integer kinds (18 and 19) and the fixed-point
// family (kinds 20–29): Int128Value / UInt128Value at 128 bits, and the raw
// scaled integer at narrower widths.
type kindWide struct{}

func init() { register(kindWide{}) }

func (kindWide) name() string  { return "wide" }
func (kindWide) codec() int    { return 3 }
func (kindWide) priority() int { return 25 }
func (kindWide) carried() bool { return true }

func (kindWide) claims(f *ir.Field) bool {
	if !plainScalar(f) {
		return false
	}
	switch f.Type.Kind {
	case ir.TInt:
		return !f.Type.Timestamp && f.Type.Width == 128
	case ir.TFixed:
		return true
	}
	return false
}

func (kindWide) storage(f *ir.Field) string {
	return scalarStorageType(f.Type)
}

func (kindWide) width(f *ir.Field) int {
	return ir.TableKindWidth(ir.TableWireFieldKind(f))
}

func (kindWide) defaultValue(f *ir.Field) string {
	if f.HasDefault && f.DefInt != nil {
		if f.Type.Width == 128 {
			return lit128Expr(f.DefInt, f.Type.Signed)
		}
		return literal(f, f.DefInt)
	}
	if f.Type.Width == 128 {
		if f.Type.Signed {
			return "Int128Value.Zero"
		}
		return "UInt128Value.Zero"
	}
	return literal(f, new(big.Int))
}

func (kindWide) defaultBits(f *ir.Field) uint64 {
	if f.HasDefault && f.DefInt != nil {
		u := new(big.Int).Set(f.DefInt)
		if u.Sign() < 0 {
			u.Add(u, new(big.Int).Lsh(big.NewInt(1), 128))
		}
		return new(big.Int).And(u, new(big.Int).SetUint64(math.MaxUint64)).Uint64()
	}
	return 0
}

func (k kindWide) differs(f *ir.Field, v string) string {
	return fmt.Sprintf("%s != %s", v, k.defaultValue(f))
}

func (kindWide) emitAsserts(b *strings.Builder, f *ir.Field, v string) {
	emitScalarAssert(b, f, v)
}

func (k kindWide) emitPayload(b *strings.Builder, f *ir.Field, v string) {
	if f.Type.Width == 128 {
		fmt.Fprintf(b, "TableWire.PutWidth(buffer, ref off, 8, %s.Lo); TableWire.PutWidth(buffer, ref off, 8, %s.Hi);", v, v)
		return
	}
	if f.Type.Signed {
		fmt.Fprintf(b, "TableWire.PutWidth(buffer, ref off, %d, unchecked((ulong)(long)%s));", k.width(f), v)
		return
	}
	fmt.Fprintf(b, "TableWire.PutWidth(buffer, ref off, %d, (ulong)%s);", k.width(f), v)
}

func (k kindWide) emitRead(b *strings.Builder, f *ir.Field, v string) {
	wk := ir.TableWireFieldKind(f)
	if f.Type.Width == 128 {
		if f.Type.Signed {
			fmt.Fprintf(b, "if (!TableWire.WideS(body, ref off, kind, %d, ref report.Widened, out Int128Value wv)) { reason = TableReport.KindMismatch; goto done; }\n", wk)
		} else {
			fmt.Fprintf(b, "if (!TableWire.WideU(body, ref off, kind, %d, ref report.Widened, out UInt128Value wv)) { reason = TableReport.KindMismatch; goto done; }\n", wk)
		}
		if lo, hi, ok := ir.TableRawRange(f); ok {
			fmt.Fprintf(b, "if (wv < %s || wv > %s) { reason = TableReport.OutOfRange; goto done; }\n",
				lit128Expr(lo, f.Type.Signed), lit128Expr(hi, f.Type.Signed))
		}
		fmt.Fprintf(b, "%s = wv;\n", v)
		return
	}
	fmt.Fprintf(b, "if (!TableWire.Int(body, ref off, kind, %d, ref report.Widened, out long fxv)) { reason = TableReport.KindMismatch; goto done; }\n", wk)
	if lo, hi, ok := ir.TableRawRange(f); ok {
		if f.Type.Signed {
			fmt.Fprintf(b, "if (fxv < %s || fxv > %s) { reason = TableReport.OutOfRange; goto done; }\n", lo.String(), hi.String())
		} else {
			fmt.Fprintf(b, "if (%s) { reason = TableReport.OutOfRange; goto done; }\n", unsignedTest("(ulong)fxv", lo, hi, false))
		}
	}
	fmt.Fprintf(b, "%s = unchecked((%s)fxv);\n", v, k.storage(f))
}

func (kindWide) runtime() string {
	return `
    internal static bool WideS(ReadOnlySpan<byte> body, ref int off, byte kind, byte declared, ref int widened, out Int128Value value)
    {
        value = default;
        if (kind != declared)
        {
            if (!Widens(kind, declared)) { return false; }
            widened++;
        }
        int w = Width(kind);
        if (w == 16)
        {
            ulong lo = BinaryPrimitives.ReadUInt64LittleEndian(body.Slice(off, 8));
            ulong hi = BinaryPrimitives.ReadUInt64LittleEndian(body.Slice(off + 8, 8));
            off += 16;
            value = new Int128Value(hi, lo);
            return true;
        }
        ulong raw = 0;
        for (int i = 0; i < w; i++) { raw |= (ulong)body[off + i] << (8 * i); }
        off += w;
        long sx = w < 8 ? ((long)(raw << (64 - w * 8)) >> (64 - w * 8)) : unchecked((long)raw);
        value = new Int128Value(sx < 0 ? ulong.MaxValue : 0UL, unchecked((ulong)sx));
        return true;
    }

    internal static bool WideU(ReadOnlySpan<byte> body, ref int off, byte kind, byte declared, ref int widened, out UInt128Value value)
    {
        value = default;
        if (kind != declared)
        {
            if (!Widens(kind, declared)) { return false; }
            widened++;
        }
        int w = Width(kind);
        if (w == 16)
        {
            ulong lo = BinaryPrimitives.ReadUInt64LittleEndian(body.Slice(off, 8));
            ulong hi = BinaryPrimitives.ReadUInt64LittleEndian(body.Slice(off + 8, 8));
            off += 16;
            value = new UInt128Value(hi, lo);
            return true;
        }
        ulong raw = 0;
        for (int i = 0; i < w; i++) { raw |= (ulong)body[off + i] << (8 * i); }
        off += w;
        value = new UInt128Value(0UL, raw);
        return true;
    }
`
}
