package unitytable

import (
	"fmt"
	"math"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// kindFloat carries float32 (kind 10) and float64 (kind 11), including
// compressed float ranges.
type kindFloat struct{}

func init() { register(kindFloat{}) }

func (kindFloat) name() string  { return "float" }
func (kindFloat) codec() int    { return 4 }
func (kindFloat) priority() int { return 30 }
func (kindFloat) carried() bool { return true }

func (kindFloat) claims(f *ir.Field) bool {
	return plainScalar(f) && (f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64)
}

func (kindFloat) storage(f *ir.Field) string {
	if f.Type.Kind == ir.TFloat32 {
		return "float"
	}
	return "double"
}

func (kindFloat) width(f *ir.Field) int {
	if f.Type.Kind == ir.TFloat32 {
		return 4
	}
	return 8
}

func (kindFloat) defaultValue(f *ir.Field) string {
	if f.Type.Kind == ir.TFloat32 {
		if f.HasDefault {
			return formatFloat32Lit(f.DefFloat)
		}
		return "0.0f"
	}
	if f.HasDefault {
		return formatFloat64Lit(f.DefFloat)
	}
	return "0.0"
}

func (kindFloat) defaultBits(f *ir.Field) uint64 {
	if !f.HasDefault {
		return 0
	}
	if f.Type.Kind == ir.TFloat32 {
		v := float32(f.DefFloat)
		if f.HasFloatRange && v == 0 {
			v = 0
		}
		return uint64(math.Float32bits(v))
	}
	v := f.DefFloat
	if f.HasFloatRange && v == 0 {
		v = 0
	}
	return math.Float64bits(v)
}

func (k kindFloat) differs(f *ir.Field, v string) string {
	bits := k.defaultBits(f)
	if f.Type.Kind == ir.TFloat32 {
		expr := v
		if f.HasFloatRange {
			expr = fmt.Sprintf("(%s == 0.0f ? 0.0f : %s)", v, v)
		}
		return fmt.Sprintf("unchecked((uint)BitConverter.SingleToInt32Bits(%s)) != 0x%08xu", expr, uint32(bits))
	}
	expr := v
	if f.HasFloatRange {
		expr = fmt.Sprintf("(%s == 0.0 ? 0.0 : %s)", v, v)
	}
	return fmt.Sprintf("unchecked((ulong)BitConverter.DoubleToInt64Bits(%s)) != 0x%016xUL", expr, bits)
}

func (kindFloat) emitAsserts(b *strings.Builder, f *ir.Field, v string) {
	emitScalarAssert(b, f, v)
}

func (kindFloat) emitPayload(b *strings.Builder, f *ir.Field, v string) {
	if f.Type.Kind == ir.TFloat32 {
		expr := v
		if f.HasFloatRange {
			expr = fmt.Sprintf("(%s == 0.0f ? 0.0f : %s)", v, v)
		}
		fmt.Fprintf(b, "TableWire.PutWidth(buffer, ref off, 4, unchecked((uint)BitConverter.SingleToInt32Bits(%s)));", expr)
		return
	}
	expr := v
	if f.HasFloatRange {
		expr = fmt.Sprintf("(%s == 0.0 ? 0.0 : %s)", v, v)
	}
	fmt.Fprintf(b, "TableWire.PutWidth(buffer, ref off, 8, unchecked((ulong)BitConverter.DoubleToInt64Bits(%s)));", expr)
}

func (kindFloat) emitRead(b *strings.Builder, f *ir.Field, v string) {
	if f.Type.Kind == ir.TFloat32 {
		fmt.Fprintf(b, "if (kind != %d) { reason = TableReport.KindMismatch; goto done; }\n", ir.TableKindF32)
		b.WriteString("float fv32 = BitConverter.Int32BitsToSingle(BinaryPrimitives.ReadInt32LittleEndian(body.Slice(off, 4))); off += 4;\n")
		if f.HasFloatRange {
			fmt.Fprintf(b, "if (!float.IsNaN(fv32) && !float.IsInfinity(fv32) && (fv32 < %s || fv32 > %s)) { reason = TableReport.OutOfRange; goto done; }\n",
				formatFloat32Lit(f.FMin), formatFloat32Lit(f.FMax))
			b.WriteString("if (fv32 == 0.0f) { fv32 = 0.0f; }\n")
		}
		fmt.Fprintf(b, "%s = fv32;\n", v)
		return
	}
	b.WriteString("double fv64;\n")
	fmt.Fprintf(b, "if (kind == %d) { fv64 = BitConverter.Int64BitsToDouble(BinaryPrimitives.ReadInt64LittleEndian(body.Slice(off, 8))); off += 8; }\n", ir.TableKindF64)
	fmt.Fprintf(b, "else if (kind == %d) { report.Widened++; fv64 = BitConverter.Int32BitsToSingle(BinaryPrimitives.ReadInt32LittleEndian(body.Slice(off, 4))); off += 4; }\n", ir.TableKindF32)
	b.WriteString("else { reason = TableReport.KindMismatch; goto done; }\n")
	if f.HasFloatRange {
		fmt.Fprintf(b, "if (!double.IsNaN(fv64) && !double.IsInfinity(fv64) && (fv64 < %s || fv64 > %s)) { reason = TableReport.OutOfRange; goto done; }\n",
			formatFloat64Lit(f.FMin), formatFloat64Lit(f.FMax))
		b.WriteString("if (fv64 == 0.0) { fv64 = 0.0; }\n")
	}
	fmt.Fprintf(b, "%s = fv64;\n", v)
}

func (kindFloat) runtime() string { return "" }
