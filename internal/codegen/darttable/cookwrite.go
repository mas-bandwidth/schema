package darttable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitCookWriteFixed writes §7.6's fixed-root half. Variable graphs remain the
// next part of the same stage; this helper makes no claim for their surface.
func (g *cookGen) emitCookWriteFixed(st *ir.Struct) {
	if why := fixedRefusal(st); why != "" {
		g.pf("  // %s has no fixed-root cook writer here: %s.\n", st.Name, why)
		return
	}
	supported := true
	cookWalk(g.unit, st.Name, func(_ string, ml *ir.MemberLayout) {
		for _, fl := range ml.Fields {
			f := fl.Field
			if f.Type.Pointer || f.IsList() || f.IsMap() || f.Guard != "" {
				supported = false
			}
			if (f.Array != ir.ArrayNone || f.KeyEnum != "") && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString) {
				supported = false
			}
		}
	})
	if !supported {
		g.pf("  // %s has no fixed-root cook writer here: graph, guarded or text-array storage remains a separate slice.\n", st.Name)
		return
	}
	ml := ir.RecordLayout(g.unit, st)
	align := ir.RegionAlignOf(ml.Align)
	data := (ml.Size + align - 1) & -align
	total := cookHeaderBytes + data + 16
	g.need(g.homeBase+"Fixed.dart", st.Name)
	g.pf("  // The fixed cook's extent is constant; every storage slot is written (§7.6).\n")
	g.pf("  static int cookMeasure(%s value) => %d;\n\n", st.Name, total)
	g.pf("  // Capacity and value validation precede every output write (§7.6).\n")
	g.pf("  static bool cook(%s value, Uint8List output, Endian order, {TableCookWriteReport? report}) {\n", st.Name)
	g.pf("    if (report != null) report.reason = 'ok';\n")
	g.pf("    if ((order != Endian.little && order != Endian.big) || output.length < %d || !_cook%s(value, null, 64, order)) {\n", total, st.Name)
	g.pf("      if (report != null) report.reason = 'invalid_value';\n      return false;\n    }\n")
	g.pf("    output.fillRange(0, %d, 0);\n", total)
	g.pf("    final view = ByteData.sublistView(output);\n")
	for _, word := range []struct {
		at    int64
		value string
	}{
		{0, "tableCookMagic"}, {8, "tableBuildVersion"}, {16, "order == Endian.little ? 1 : 2"},
		{24, fmt.Sprint(data)}, {32, "16"}, {40, fmt.Sprint(align)},
		{64 + data, "0"}, {64 + data + 8, dartU64Literal(tableWireId(st))},
	} {
		g.pf("    view.setUint64(%d, %s, order);\n", word.at, word.value)
	}
	g.pf("    return _cook%s(value, view, 64, order);\n  }\n\n", st.Name)
	needFloat32 := false
	cookWalk(g.unit, st.Name, func(name string, record *ir.MemberLayout) {
		g.needCookStorage(name)
		g.pf("  // The shared ABI's field pieces, not the fixed wire's packed widths (§7.2).\n")
		g.pf("  static bool _cook%s(%s value, ByteData? out, int at, Endian order) {\n", name, name)
		for _, fl := range record.Fields {
			g.emitCookFixedField(fl.Field, fl.Offset, "value."+dartName(fl.Field.Name), "    ")
			needFloat32 = needFloat32 || (fl.Field.Type.Kind == ir.TFloat32 && fl.Field.Array == ir.ArrayNone && fl.Field.KeyEnum == "")
		}
		g.pf("    return true;\n  }\n\n")
	})
	if needFloat32 {
		g.pf("%s", dartCookFloat32Writer)
	}
}

// needCookStorage names the existing caller-owned value class (§6.1).
func (g *cookGen) needCookStorage(name string) {
	if g.unit.Tables[name] != nil {
		g.need(g.homeBase+"Fixed.dart", name)
		return
	}
	g.need(g.unit.DeclFile[name]+".dart", name)
}

// emitCookFixedField writes the ABI companions and every declared slot (§7.2).
func (g *cookGen) emitCookFixedField(f *ir.Field, offset int64, expr, ind string) {
	pieces := ir.FieldPieces(g.unit, f, offset)
	if f.Type.Optional {
		g.pf("%sif (out != null) out.setUint8(at + %d, %sPresent ? 1 : 0);\n", ind, pieces[len(pieces)-1].Offset, expr)
	}
	if f.Array != ir.ArrayNone || f.KeyEnum != "" {
		bound := f.ArrayBound
		if f.KeyEnum != "" {
			bound = f.KeyEnumRef.Max
		}
		g.pf("%sif (%s.length < %d) return false;\n", ind, expr, bound)
		if f.Array == ir.ArrayCounted {
			g.pf("%sif (%sCount < 0 || %sCount > %d) return false;\n", ind, expr, expr, bound)
			g.pf("%sif (out != null) out.setInt32(at + %d, %sCount, order);\n", ind, pieces[1].Offset, expr)
		}
		stride := int64(0)
		if bound > 0 {
			stride = pieces[0].Size / bound
		}
		// Typed float arrays already hold the transmitted 32-bit cells. Reading
		// them as doubles would quiet signalling NaNs before narrowing can help.
		if f.Type.Kind == ir.TFloat32 {
			g.pf("%sif (out != null) {\n", ind)
			g.pf("%s  final raw = ByteData.view(%s.buffer, %s.offsetInBytes, %s.lengthInBytes);\n", ind, expr, expr, expr)
			g.pf("%s  for (var i = 0; i < %d; i++) {\n", ind, bound)
			g.pf("%s    out.setUint32(at + %d + i * %d, raw.getUint32(i * 4, Endian.host), order);\n", ind, offset, stride)
			g.pf("%s  }\n%s}\n", ind, ind)
			return
		}
		g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, bound)
		g.emitCookFixedScalar(f, expr+"[i]", fmt.Sprintf("at + %d + i * %d", offset, stride), stride, ind+"  ")
		g.pf("%s}\n", ind)
		return
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString {
		g.pf("%sif (%sLength < 0 || %sLength > %d || %sLength > %s.length) return false;\n", ind, expr, expr, f.Type.Size, expr, expr)
		g.pf("%sif (out != null) {\n", ind)
		g.pf("%s  out.setInt32(at + %d, %sLength, order);\n", ind, pieces[1].Offset, expr)
		g.pf("%s  for (var i = 0; i < %sLength; i++) {\n", ind, expr)
		if f.Type.Kind == ir.TWString {
			g.pf("%s    out.setUint16(at + %d + i * 2, %s[i], order);\n", ind, offset, expr)
		} else {
			g.pf("%s    out.setUint8(at + %d + i, %s[i]);\n", ind, offset, expr)
		}
		g.pf("%s  }\n%s}\n", ind, ind)
		return
	}
	g.emitCookFixedScalar(f, expr, fmt.Sprintf("at + %d", offset), pieces[0].Size, ind)
}

// emitCookFixedScalar writes storage widths in the target byte order (§7.6).
func (g *cookGen) emitCookFixedScalar(f *ir.Field, expr, at string, width int64, ind string) {
	if ref, ok := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && ok {
		g.pf("%sif (!_cook%s(%s, out, %s, order)) return false;\n", ind, ref.Name, expr, at)
		return
	}
	g.pf("%sif (out != null) {\n", ind)
	switch {
	case f.Type.Kind == ir.TBool:
		g.pf("%s  out.setUint8(%s, %s ? 1 : 0);\n", ind, at, expr)
	case f.Type.Kind == ir.TFloat32:
		g.pf("%s  out.setUint32(%s, _cookFloat32Bits(%s), order);\n", ind, at, expr)
	case f.Type.Kind == ir.TFloat64:
		g.pf("%s  out.setFloat64(%s, %s, order);\n", ind, at, expr)
	case width == 16:
		g.pf("%s  out.setUint64(%s, order == Endian.little ? %s.lo : %s.hi, order);\n", ind, at, expr, expr)
		g.pf("%s  out.setUint64(%s + 8, order == Endian.little ? %s.hi : %s.lo, order);\n", ind, at, expr, expr)
	default:
		signed := "Uint"
		if fixedSigned(f.Type) {
			signed = "Int"
		}
		if width == 1 {
			g.pf("%s  out.set%s8(%s, %s);\n", ind, signed, at, expr)
		} else {
			g.pf("%s  out.set%s%d(%s, %s, order);\n", ind, signed, width*8, at, expr)
		}
	}
	g.pf("%s}\n", ind)
}

const dartCookWriteReport = `
// §7.6's write refusals use the reference leg's words, never a new vocabulary.
final class TableCookWriteReport {
  String reason = 'ok';
}
`

const dartCookFloat32Writer = `
  static final Float32List _cookFloat32 = Float32List(2);
  static final Uint32List _cook32Words = _cookFloat32.buffer.asUint32List();
  static final Float64List _cookFloat64 = _cookFloat32.buffer.asFloat64List();
  static final Uint64List _cook64Words = _cookFloat32.buffer.asUint64List();

  static int _cookFloat32Bits(double value) {
    if (!value.isNaN) {
      _cookFloat32[0] = value;
      return _cook32Words[0];
    }
    _cookFloat64[0] = value;
    final bits = _cook64Words[0];
    var mantissa = (bits >>> 29) & 0x7fffff;
    if (mantissa == 0) mantissa = 0x400000;
    return ((bits >>> 63) << 31) | 0x7f800000 | mantissa;
  }

`
