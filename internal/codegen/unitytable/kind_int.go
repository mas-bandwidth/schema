package unitytable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// kindInt carries every integer that is at most 64 bits on the wire: the
// signed and unsigned integers (kinds 2–9) and `bits(N)` (the unsigned kind of
// its storage width). A narrower writer's kind of the same signedness WIDENS
// into the declared one (docs/SPEC-TABLES.md §4) and counts one `widened`. The
// declared range is held on read (out_of_range) and asserted on write. The
// fixed-point family and the 128-bit integers are the WIDE kinds, which the
// target refuses by name (ir.RefuseWideTableKinds) until their card lands.
type kindInt struct{}

func init() { register(kindInt{}) }

func (kindInt) name() string    { return "int" }
func (kindInt) codec() int      { return 2 }
func (kindInt) priority() int   { return 20 }
func (kindInt) carried() bool   { return true }
func (kindInt) runtime() string { return "" }

func (kindInt) claims(f *ir.Field) bool {
	if !plainScalar(f) {
		return false
	}
	switch f.Type.Kind {
	case ir.TInt:
		if f.Type.Timestamp {
			return false
		}
	case ir.TBits:
	default:
		return false
	}
	k := ir.TableWireFieldKind(f)
	return k >= ir.TableKindI8 && k <= ir.TableKindU64
}

// signedStorage reports whether the member holds two's complement.
func signedStorage(f *ir.Field) bool {
	return f.Type.Kind != ir.TBits && f.Type.Signed
}

func (kindInt) storage(f *ir.Field) string {
	if f.Type.Kind == ir.TBits {
		// the packet types' spelling of the same field (internal/codegen/unity)
		if f.Type.Width <= 32 {
			return "uint"
		}
		return "ulong"
	}
	switch ir.TableKindWidth(ir.TableWireFieldKind(f)) {
	case 1:
		return pick(f.Type.Signed, "sbyte", "byte")
	case 2:
		return pick(f.Type.Signed, "short", "ushort")
	case 4:
		return pick(f.Type.Signed, "int", "uint")
	}
	return pick(f.Type.Signed, "long", "ulong")
}

func pick(signed bool, s, u string) string {
	if signed {
		return s
	}
	return u
}

// literal spells v in the member's own type.
func literal(f *ir.Field, v *big.Int) string {
	t := kindInt{}.storage(f)
	if signedStorage(f) {
		return fmt.Sprintf("(%s)(%s)", t, v.String())
	}
	return fmt.Sprintf("(%s)(%sUL)", t, v.String())
}

func (k kindInt) defaultValue(f *ir.Field) string {
	if f.HasDefault && f.DefInt != nil {
		return literal(f, f.DefInt)
	}
	return literal(f, new(big.Int))
}

func (kindInt) defaultBits(f *ir.Field) uint64 {
	if f.HasDefault && f.DefInt != nil {
		if f.DefInt.Sign() < 0 {
			return uint64(f.DefInt.Int64())
		}
		return f.DefInt.Uint64()
	}
	return 0
}

func (k kindInt) differs(f *ir.Field, v string) string {
	return fmt.Sprintf("%s != %s", v, k.defaultValue(f))
}

func (kindInt) width(f *ir.Field) int { return ir.TableKindWidth(ir.TableWireFieldKind(f)) }

// valueRange is the range a value of the field must lie in beyond its storage:
// a ranged integer's bounds, and bits(N)'s 2^N − 1 where N is narrower than
// the wire kind. ok is false where the storage is the only limit.
func valueRange(f *ir.Field) (lo, hi *big.Int, ok bool) {
	switch f.Type.Kind {
	case ir.TInt:
		return ir.TableRawRange(f)
	case ir.TBits:
		if f.Type.Width < 8*(kindInt{}).width(f) {
			return new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1)), true
		}
	}
	return nil, nil, false
}

// descriptorRange is the descriptor's Lo and Hi: valueRange, or the wire
// kind's own range.
func descriptorRange(f *ir.Field) (lo int64, hi uint64) {
	if l, h, ok := valueRange(f); ok {
		return l.Int64(), h.Uint64()
	}
	bits := 8 * kindInt{}.width(f)
	if signedStorage(f) {
		return -(int64(1) << uint(bits-1)), uint64(1)<<uint(bits-1) - 1
	}
	if bits == 64 {
		return 0, ^uint64(0)
	}
	return 0, uint64(1)<<uint(bits) - 1
}

func (kindInt) emitAsserts(b *strings.Builder, f *ir.Field, v string) {
	lo, hi, ok := valueRange(f)
	if !ok {
		return
	}
	if signedStorage(f) {
		fmt.Fprintf(b, "Debug.Assert(%s >= %s && %s <= %s, \"%s is outside its declared range [%s, %s]\");\n",
			v, lo.String(), v, hi.String(), f.Name, lo.String(), hi.String())
		return
	}
	fmt.Fprintf(b, "Debug.Assert(%s, \"%s is outside its declared range [%s, %s]\");\n",
		unsignedTest(v, lo, hi, true), f.Name, lo.String(), hi.String())
}

// unsignedTest is `v` inside [lo, hi] (inside) or outside it, with no compare
// against a zero low bound, which an unsigned value always meets.
func unsignedTest(v string, lo, hi *big.Int, inside bool) string {
	if inside {
		if lo.Sign() == 0 {
			return fmt.Sprintf("%s <= %sUL", v, hi.String())
		}
		return fmt.Sprintf("%s >= %sUL && %s <= %sUL", v, lo.String(), v, hi.String())
	}
	if lo.Sign() == 0 {
		return fmt.Sprintf("%s > %sUL", v, hi.String())
	}
	return fmt.Sprintf("%s < %sUL || %s > %sUL", v, lo.String(), v, hi.String())
}

func (k kindInt) emitPayload(b *strings.Builder, f *ir.Field, v string) {
	if signedStorage(f) {
		fmt.Fprintf(b, "TableWire.PutWidth(buffer, ref off, %d, unchecked((ulong)(long)%s));", k.width(f), v)
		return
	}
	fmt.Fprintf(b, "TableWire.PutWidth(buffer, ref off, %d, (ulong)%s);", k.width(f), v)
}

func (k kindInt) emitRead(b *strings.Builder, f *ir.Field, v string) {
	fmt.Fprintf(b, "if (!TableWire.Int(body, ref off, kind, %d, ref report.Widened, out iv)) { reason = TableReport.KindMismatch; goto done; }\n", ir.TableWireFieldKind(f))
	if lo, hi, ok := valueRange(f); ok {
		if signedStorage(f) {
			fmt.Fprintf(b, "if (iv < %s || iv > %s) { reason = TableReport.OutOfRange; goto done; }\n", lo.String(), hi.String())
		} else {
			fmt.Fprintf(b, "if (%s) { reason = TableReport.OutOfRange; goto done; }\n", unsignedTest("(ulong)iv", lo, hi, false))
		}
	}
	fmt.Fprintf(b, "%s = unchecked((%s)iv);\n", v, k.storage(f))
}
