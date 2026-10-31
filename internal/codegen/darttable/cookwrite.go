package darttable

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitCookWriteFixed writes §7.6's fixed-root half, and hands a variable root
// to emitCookWriteVariable (the graph half, the same stage).
func (g *cookGen) emitCookWriteFixed(st *ir.Struct) {
	// A map's generated ENTRY (docs/SPEC-TABLES.md §2.8) is an extent inside
	// its holder's node, reached through the holder's map slot — never a cooked
	// record of its own — so it has no fixed-root cook writer, exactly as the
	// block form refuses it (§19).
	if st.MapEntryOf != "" {
		return
	}
	if ir.VariableTables(g.unit)[st.Name] {
		g.emitCookWriteVariable(st)
		return
	}
	if why := fixedRefusal(st); why != "" {
		g.pf("  // %s has no fixed-root cook writer here: %s.\n", st.Name, why)
		return
	}
	// fixedRefusal has already sent every pointer, map, unbounded array and
	// guarded branch away (a guarded table is variable, §2.2, and its writer is
	// emitCookWriteVariable's), so what this walk can still meet is a DIRECT
	// array of text, which v1's compiler refuses ("an array of string(N) is not
	// supported in v1 — wrap the element in a type"). The text array v1 admits,
	// an array of a record holding string(N) or bytes(N), is written below
	// slot by slot (TestCookWriteGuardedAndTextArrayRootsDart pins it). The
	// walk stays as the backstop for a unit the checker did not see.
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
		g.pf("  // %s has no fixed-root cook writer here: a direct array of text, which v1 refuses (wrap the element in a type).\n", st.Name)
		return
	}
	// A UNION in the closure (§2.6, §7.2): the fixed class holds it as its
	// own value class, which this writer's scalar slots do not write, so the
	// root's cook is the form-1 writer's (<Root>CookWire, cookwrite_form1.go)
	// and never a scalar store of the union's class.
	if name := fixedCookUnion(g.unit, st); name != "" {
		g.pf("  // %s has no fixed-root cook writer here: %s is a union, which this writer does not carry; %s cooks the form-1 value (docs/SPEC-TABLES.md §2.6, §7.6).\n", st.Name, name, form1CookClass(st))
		return
	}
	ml := ir.RecordLayout(g.unit, st)
	align := ir.RegionAlignOf(ml.Align)
	data := (ml.Size + align - 1) & -align
	total := cookHeaderBytes + data + 16
	g.need(g.homeBase+"Fixed.dart", st.Name)
	g.pf("  // The fixed cook's extent is constant; every storage slot is written (§7.6).\n")
	if fixedHoldsFloat(st, map[string]bool{}) {
		// A COOKED FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): the measure
		// refuses a NaN or an infinity with -1, as the cook itself does.
		head := fmt.Sprintf("  static int cookMeasure(%s value) =>", st.Name)
		expr := fmt.Sprintf("_cook%s(value, null, 64, Endian.little) ? %d : -1;", st.Name, total)
		if len(head)+1+len(expr) <= 80 {
			g.pf("%s %s\n\n", head, expr)
		} else {
			g.pf("%s\n      %s\n\n", head, expr)
		}
	} else {
		g.pf("  static int cookMeasure(%s value) => %d;\n\n", st.Name, total)
	}
	g.pf("  // Capacity and value validation precede every output write (§7.6).\n")
	g.pf("  static bool cook(\n    %s value,\n    Uint8List output,\n    Endian order, {\n    TableCookWriteReport? report,\n  }) {\n", st.Name)
	g.pf("    if (report != null) report.reason = 'ok';\n")
	g.pf("    if ((order != Endian.little && order != Endian.big) ||\n        output.length < %d ||\n        !_cook%s(value, null, 64, order)) {\n", total, st.Name)
	g.pf("      if (report != null) report.reason = 'invalid_value';\n      return false;\n    }\n")
	g.pf("    output.fillRange(0, %d, 0);\n", total)
	g.pf("    final view = ByteData.sublistView(output);\n")
	for _, word := range []struct {
		at    int64
		value string
	}{
		{0, "tableCookMagic"}, {8, "tableBuildVersion"}, {16, "order == Endian.little ? 1 : 2"},
		{24, fmt.Sprint(data)}, {32, "16"}, {40, fmt.Sprint(align)},
		{64 + data, "0"}, {64 + data + 8, dartU64Literal(ir.TableWireId(st.WireName()))},
	} {
		g.pf("    view.setUint64(%d, %s, order);\n", word.at, word.value)
	}
	g.pf("    return _cook%s(value, view, 64, order);\n  }\n\n", st.Name)
	cookWalk(g.unit, st.Name, func(name string, record *ir.MemberLayout) {
		g.needCookStorage(name)
		g.pf("  // The shared ABI's field pieces, not the fixed wire's packed widths (§7.2).\n")
		sig := fmt.Sprintf("  static bool _cook%s(%s value, ByteData? out, int at, Endian order) {", name, name)
		if len(sig) <= 80 {
			g.pf("%s\n", sig)
		} else {
			g.pf("  static bool _cook%s(\n    %s value,\n    ByteData? out,\n    int at,\n    Endian order,\n  ) {\n", name, name)
		}
		for _, fl := range record.Fields {
			g.emitCookFixedField(fl.Field, fl.Offset, "value."+dartName(fl.Field.Name), "    ")
		}
		g.pf("    return true;\n  }\n\n")
	})
	g.trimBlank()
}

// fixedCookUnion names the first field of st's closure that holds a union
// (§2.6), or "" when the closure holds none.
func fixedCookUnion(u *ir.Unit, st *ir.Struct) string {
	name := ""
	cookWalk(u, st.Name, func(record string, ml *ir.MemberLayout) {
		for _, fl := range ml.Fields {
			if _, ok := fl.Field.Type.Ref.(*ir.Union); ok && name == "" {
				name = record + "." + fl.Field.Name
			}
		}
	})
	return name
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
			chk := fmt.Sprintf("%sif (%sCount < 0 || %sCount > %d) return false;", ind, expr, expr, bound)
			if len(chk) <= 80 {
				g.pf("%s\n", chk)
			} else {
				g.pf("%sif (%sCount < 0 || %sCount > %d)\n%s  return false;\n", ind, expr, expr, bound, ind)
			}
			g.pf("%sif (out != null) out.setInt32(at + %d, %sCount, order);\n", ind, pieces[1].Offset, expr)
		}
		stride := int64(0)
		if bound > 0 {
			stride = pieces[0].Size / bound
		}
		// Typed float arrays already hold the transmitted 32-bit cells, and
		// each is held finite first (docs/SPEC-TABLES.md §3.3): a NaN or an
		// infinity refuses the cook, measure and write alike.
		if f.Type.Kind == ir.TFloat32 {
			g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, bound)
			g.cookRefuseUnless(ind+"  ", expr+"[i].isFinite", "false")
			g.pf("%s}\n", ind)
			g.pf("%sif (out != null) {\n", ind)
			rawLine := fmt.Sprintf("%s  final raw = ByteData.view(%s.buffer, %s.offsetInBytes, %s.lengthInBytes);", ind, expr, expr, expr)
			if len(rawLine) <= 80 {
				g.pf("%s\n", rawLine)
			} else {
				g.pf("%s  final raw = ByteData.view(\n%s    %s.buffer,\n%s    %s.offsetInBytes,\n%s    %s.lengthInBytes,\n%s  );\n", ind, ind, expr, ind, expr, ind, expr, ind)
			}
			g.pf("%s  for (var i = 0; i < %d; i++) {\n", ind, bound)
			setLine := fmt.Sprintf("%s    out.setUint32(at + %d + i * %d, raw.getUint32(i * 4, Endian.host), order);", ind, offset, stride)
			if len(setLine) <= 80 {
				g.pf("%s\n", setLine)
			} else {
				g.pf("%s    out.setUint32(\n%s      at + %d + i * %d,\n%s      raw.getUint32(i * 4, Endian.host),\n%s      order,\n%s    );\n", ind, ind, offset, stride, ind, ind, ind)
			}
			g.pf("%s  }\n%s}\n", ind, ind)
			return
		}
		g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, bound)
		g.emitCookFixedScalar(f, expr+"[i]", fmt.Sprintf("at + %d + i * %d", offset, stride), stride, ind+"  ")
		g.pf("%s}\n", ind)
		return
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString {
		g.pf("%sif (%sLength < 0 ||\n%s    %sLength > %d ||\n%s    %sLength > %s.length)\n%s  return false;\n", ind, expr, ind, expr, f.Type.Size, ind, expr, expr, ind)
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
		line := fmt.Sprintf("%sif (!_cook%s(%s, out, %s, order)) return false;", ind, ref.Name, expr, at)
		if len(line) <= 80 {
			g.pf("%s\n", line)
		} else if head := fmt.Sprintf("%sif (!_cook%s(%s, out, %s, order))", ind, ref.Name, expr, at); len(head) <= 80 {
			g.pf("%s\n%s  return false;\n", head, ind)
		} else {
			g.pf("%sif (!_cook%s(\n%s  %s,\n%s  out,\n%s  %s,\n%s  order,\n%s))\n%s  return false;\n",
				ind, ref.Name, ind, expr, ind, ind, at, ind, ind, ind)
		}
		return
	}
	if f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64 {
		// A COOKED FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): a NaN or an
		// infinity refuses the cook, measure and write alike, before its slot
		// is touched.
		g.cookRefuseUnless(ind, expr+".isFinite", "false")
	}
	g.pf("%sif (out != null) {\n", ind)
	g.emitCookScalarStore(f, expr, at, width, ind+"  ")
	g.pf("%s}\n", ind)
}

// cookRefuseUnless writes `if (!<cond>) return <ret>;` the way dart format
// does: one line while it fits eighty columns, the condition alone past it.
func (g *cookGen) cookRefuseUnless(ind, cond, ret string) {
	if line := fmt.Sprintf("%sif (!%s) return %s;", ind, cond, ret); len(line) <= 80 {
		g.pf("%s\n", line)
		return
	}
	g.pf("%sif (!%s)\n%s  return %s;\n", ind, cond, ind, ret)
}

// emitCookScalarStore is emitCookFixedScalar's store with no null test on
// out, for a caller already inside `if (out != null)`: the analyzer refuses
// a second test on a promoted out (unnecessary_null_comparison).
func (g *cookGen) emitCookScalarStore(f *ir.Field, expr, at string, width int64, ind string) {
	// ind is the store's own indent; the lines below are spelled one level
	// in from their enclosing statement, so step back out by one
	ind = strings.TrimSuffix(ind, "  ")
	switch {
	case f.Type.Kind == ir.TBool:
		g.pf("%s  out.setUint8(%s, %s ? 1 : 0);\n", ind, at, expr)
	case f.Type.Kind == ir.TFloat32:
		line := fmt.Sprintf("%s  out.setUint32(%s, TableCookWriteReport.float32Bits(%s), order);", ind, at, expr)
		if len(line) <= 80 {
			g.pf("%s\n", line)
		} else {
			g.pf("%s  out.setUint32(\n%s    %s,\n%s    TableCookWriteReport.float32Bits(%s),\n%s    order,\n%s  );\n", ind, ind, at, ind, expr, ind, ind)
		}
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
}

// emitCookWriteVariable writes §7.6's VARIABLE-CLASS cook: the region of a
// graph, byte for byte what `schema cook` writes, in either byte order.
//
// THE NODE ORDER IS §3.1's NUMBERING, taken from the wire library's own
// <name>Number over the value, so the cook and the wire can never number one
// graph two ways: the root is node 1, each node is numbered on first visit
// before its next sibling field, and a node two references reach is ONE node
// written once (graph_shared's five nodes for eight references). A blob
// (`*string`, `*bytes`) is a node of its own reserved type id.
//
// THE REGION (§7.1, §7.6): each node at its own alignment in numbering order —
// a record at its ABI size, a blob as a u32 length, four zero bytes and its
// bytes (a string one more, the terminator) — the data part rounded to the
// region's alignment, and then one attribution pair (offset, type id) per
// node. A reference slot holds the signed self-relative delta from the slot to
// its target, zero for null (§6.3).
//
// MEASURE BEFORE WRITE: the same walk runs with no output first, and every
// value check runs in it, so a refused write changes no byte of the caller's
// buffer. A buffer shorter than the measured extent is refused as
// `invalid_value`, the reference leg's TableRefuseInvalidValue
// (internal/codegen/gotable/cookwrite.go), and a cyclic graph as
// `data_cycle`, its TableRefuseDataCycle — never a new word.
func (g *cookGen) emitCookWriteVariable(st *ir.Struct) {
	if why := g.variableCookRefusal(st); why != "" {
		g.pf("  // %s has no variable cook writer here: %s.\n", st.Name, why)
		return
	}
	if g.prefixed == nil {
		g.prefixed = map[string]string{}
	}
	g.prefixed[runtimeHome(g.unit)+"Wire.dart"] = "wire"
	n := st.Name
	records := g.cookGraphRecords(n)
	g.pf("  // THE VARIABLE COOK (docs/SPEC-TABLES.md §7.6): the extent, measured by the\n")
	g.pf("  // walk the write repeats; negative when the value is refused.\n")
	// the spelling `dart format` writes, which a long root name splits (the
	// tables-dart-clean gate holds the cook libraries to it)
	if measure := fmt.Sprintf("  static int cookMeasure(wire.%s value, {TableCookWriteReport? report}) =>", n); len(measure) <= 80 {
		g.pf("%s\n      _writeRegion(value, null, Endian.little, report);\n\n", measure)
	} else {
		g.pf("  static int cookMeasure(\n    wire.%s value, {\n    TableCookWriteReport? report,\n  }) => _writeRegion(value, null, Endian.little, report);\n\n", n)
	}
	g.pf("  // Measure before write: a refusal changes no byte of output (§7.6).\n")
	g.pf("  static bool cook(\n    wire.%s value,\n    Uint8List output,\n    Endian order, {\n    TableCookWriteReport? report,\n  }) => _writeRegion(value, output, order, report) >= 0;\n\n", n)
	g.pf("  static int _writeRegion(\n    wire.%s value,\n    Uint8List? output,\n    Endian order,\n    TableCookWriteReport? report,\n  ) {\n", n)
	g.pf("    if (report != null) report.reason = 'ok';\n")
	g.pf("    if (order != Endian.little && order != Endian.big) {\n")
	g.pf("      if (report != null) report.reason = 'invalid_value';\n      return -1;\n    }\n")
	g.pf("    // §3.1's numbering, the wire's own: node 1 is the root.\n")
	g.pf("    final s = wire.WireState();\n    s.indexRoot(value);\n")
	g.pf("    try {\n      wire.%sNumber(s, value, <Object>{value});\n    } on StateError {\n", dartName(n))
	g.pf("      if (report != null) report.reason = 'data_cycle';\n      return -1;\n    }\n")
	g.pf("    final count = s.nodes.length + 1;\n")
	g.pf("    final ids = List<int>.filled(count, 0);\n")
	g.pf("    final c = TableCookGraph(order, count, s.indexOf, s.blobIndex);\n")
	g.pf("    var data = 0;\n    var align = %d;\n", ir.RegionAlignFloor)
	g.pf("    for (var i = 0; i < count; i++) {\n")
	g.pf("      final Object? node = i == 0 ? value : s.nodeObjs[i - 1];\n")
	g.pf("      final id = i == 0 ? %s : s.nodeTypes[i - 1];\n", dartU64Literal(ir.TableWireId(st.WireName())))
	g.pf("      ids[i] = id;\n")
	g.pf("      var size = 0;\n      var alignment = 8;\n")
	g.pf("      if (node == null) {\n")
	g.pf("        size = 8 + s.nodes[i - 1].length;\n")
	g.pf("        if (id == %s) size++;\n", dartU64Literal(ir.StringWireTypeId))
	g.pf("      } else {\n")
	g.pf("        switch (id) {\n")
	for _, r := range records {
		rs := cookMember(g.unit, r)
		ml := ir.RecordLayout(g.unit, rs)
		if recordHasLists(g.unit, rs) {
			// A record whose node carries an extent (§7.2) is sized normally its
			// record rounded to the region's alignment floor, plus the extent
			// its unbounded arrays take behind it — measured by the same walk
			// the write carves with.
			recordBytes := (ml.Size + ir.RegionAlignFloor - 1) & -ir.RegionAlignFloor
			g.pf("          case %s:\n", dartU64Literal(ir.TableWireId(rs.WireName())))
			g.pf("            size = %d + _extent%s(node as wire.%s, 0);\n", recordBytes, r, r)
			g.pf("            alignment = %d;\n", ml.Align)
		} else {
			g.pf("          case %s:\n", dartU64Literal(ir.TableWireId(rs.WireName())))
			g.pf("            size = %d;\n            alignment = %d;\n", ml.Size, ml.Align)
		}
	}
	g.pf("        }\n")
	g.pf("        if (!_writeNode(id, node, c, 0)) {\n")
	g.pf("          if (report != null) report.reason = 'invalid_value';\n          return -1;\n        }\n")
	g.pf("      }\n")
	g.pf("      if (alignment > align) align = alignment;\n")
	g.pf("      data = (data + alignment - 1) & -alignment;\n")
	g.pf("      c.offsets[i] = data;\n      data += size;\n    }\n")
	g.pf("    data = (data + align - 1) & -align;\n")
	g.pf("    final total = %d + data + count * 16;\n", cookHeaderBytes)
	g.pf("    if (output == null) return total;\n")
	g.pf("    if (output.length < total) {\n")
	g.pf("      if (report != null) report.reason = 'invalid_value';\n      return -1;\n    }\n")
	g.pf("    output.fillRange(0, total, 0);\n")
	g.pf("    final view = ByteData.sublistView(output);\n")
	g.pf("    c.out = view;\n")
	g.pf("    view.setUint64(0, tableCookMagic, order);\n")
	g.pf("    view.setUint64(8, tableBuildVersion, order);\n")
	g.pf("    view.setUint64(16, order == Endian.little ? 1 : 2, order);\n")
	g.pf("    view.setUint64(24, data, order);\n")
	g.pf("    view.setUint64(32, count * 16, order);\n")
	g.pf("    view.setUint64(40, align, order);\n")
	g.pf("    for (var i = 0; i < count; i++) {\n")
	g.pf("      final at = %d + c.offsets[i];\n", cookHeaderBytes)
	g.pf("      final Object? node = i == 0 ? value : s.nodeObjs[i - 1];\n")
	g.pf("      if (node == null) {\n")
	g.pf("        final blob = s.nodes[i - 1];\n")
	g.pf("        view.setUint32(at, blob.length, order);\n")
	g.pf("        output.setRange(at + 8, at + 8 + blob.length, blob);\n")
	g.pf("      } else {\n")
	g.pf("        _writeNode(ids[i], node, c, at);\n")
	g.pf("      }\n")
	g.pf("      view.setUint64(%d + data + i * 16, c.offsets[i], order);\n", cookHeaderBytes)
	g.pf("      view.setUint64(%d + data + i * 16 + 8, ids[i], order);\n", cookHeaderBytes)
	g.pf("    }\n    return total;\n  }\n\n")
	// _writeRegion and _writeNode are spelled outside the `_cook<Table>` space
	// the record writers take, so no table name can collide with them (§11).
	g.pf("  // One node of the graph by its type id; out null measures (§7.6).\n")
	g.pf("  static bool _writeNode(int id, Object node, TableCookGraph c, int at) {\n")
	g.pf("    switch (id) {\n")
	for _, r := range records {
		rs := cookMember(g.unit, r)
		ml := ir.RecordLayout(g.unit, rs)
		recordBytes := (ml.Size + ir.RegionAlignFloor - 1) & -ir.RegionAlignFloor
		g.pf("      case %s:\n", dartU64Literal(ir.TableWireId(rs.WireName())))
		if recordHasLists(g.unit, rs) {
			// one cast: a second `node as` on the promoted node is refused by
			// the analyzer (unnecessary_cast)
			g.pf("        final value = node as wire.%s;\n", r)
			g.pf("        if (!_cook%s(value, c, at)) return false;\n", r)
			g.pf("        if (_carve%s(value, c, at, at + %d, 0) < 0) return false;\n", r, recordBytes)
			g.pf("        return true;\n")
		} else {
			g.pf("        return _cook%s(node as wire.%s, c, at);\n", r, r)
		}
	}
	g.pf("    }\n    return false;\n  }\n\n")
	for _, r := range records {
		g.emitCookVariableRecord(r)
		g.emitCookVariableExtent(r)
		g.emitCookVariableCarve(r)
	}
	if !g.form1 && len(g.form1Unions) > 0 {
		// the element writers of the unions a list of unions queued (§2.6);
		// a fixed root's form-1 class drains its own queue after this
		g.emitCookForm1UnionWriters()
	}
	g.trimBlank()
}

// variableCookRefusal names the first storage the variable cook writer does
// not carry, or "" when it carries the root's whole closure (§7.2's storage
// kinds: scalar, record, inline string, reference, blob).
func (g *cookGen) variableCookRefusal(st *ir.Struct) string {
	why := ""
	for _, name := range g.cookGraphRecords(st.Name) {
		rs := cookMember(g.unit, name)
		if why != "" || rs == nil {
			continue
		}
		if g.form1 {
			// a fixed root's form-1 cook: every record of the closure, a
			// `type` included, is a class of the wire library (§3), and the
			// fixed class's own constructs are carried (cookwrite_form1.go)
			for _, f := range rs.Fields {
				if why == "" && !form1CookField(f) {
					why = fmt.Sprintf("%s.%s's storage (%s) is not carried by the form-1 cook writer yet (docs/SPEC-TABLES.md §7.2)", name, f.Name, tableFieldTypeName(f))
				}
			}
			continue
		}
		// A `type` a table reaches rides exactly as a by-value table does: kind
		// 13 over a body of id-framed fields (docs/SPEC-TABLES.md §3, §7.2),
		// laid out by value inside its holding node. The walk still validates
		// every field's storage below, so an unsupported construct stays
		// refused by name.
		for _, f := range rs.Fields {
			if f.Guard != "" && !guardedCookField(f) {
				why = fmt.Sprintf("%s.%s is guarded and holds a reference or a record, whose numbering under a false guard this writer does not carry yet (docs/SPEC-TABLES.md §3.1, §7.6)", name, f.Name)
				break
			}
			if un := unionRef(f); un != nil {
				if arm := variableCookUnion(un); arm != "" {
					why = fmt.Sprintf("%s.%s's union (%s) is not carried by the variable cook writer yet (docs/SPEC-TABLES.md §7.2)", name, f.Name, arm)
					break
				}
				continue
			}
			if !variableCookField(f) {
				why = fmt.Sprintf("%s.%s's storage (%s) is not carried by the variable cook writer yet (docs/SPEC-TABLES.md §7.2)", name, f.Name, tableFieldTypeName(f))
				break
			}
		}
	}
	return why
}

// variableCookUnion names the first arm of a union the variable cook writer
// does not carry, or "" when it carries every arm (§7.2's storage kinds,
// applied to an arm as a field line).
func variableCookUnion(un *ir.Union) string {
	for _, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		if u2 := unionRef(arm.F); u2 != nil {
			if why := variableCookUnion(u2); why != "" {
				return why
			}
			continue
		}
		if !variableCookField(arm.F) {
			return fmt.Sprintf("#arm %s's storage (%s)", arm.Name, tableFieldTypeName(arm.F))
		}
	}
	return ""
}

func variableCookField(f *ir.Field) bool {
	if f.Type.Optional || f.KeyEnum != "" || f.IsMap() {
		return false
	}
	if f.IsList() {
		// An unbounded array (docs/SPEC-TABLES.md §2.9) rides in its holder's
		// node extent (§7.2, §7.6): a scalar or by-value record element is
		// carried; a `[]*T`'s elements are reference slots of their own and
		// stay refused, as a pointer field does.
		if f.Type.Pointer {
			return false
		}
		switch f.Type.Kind {
		case ir.TBool, ir.TBits, ir.TFloat32, ir.TFloat64:
			return true
		case ir.TInt:
			return f.Type.Width != 128
		case ir.TNamed:
			return tableRef(f) != nil
		}
		return false
	}
	if f.Type.Blob() {
		return f.Array == ir.ArrayNone
	}
	if f.Array != ir.ArrayNone && f.Array != ir.ArrayCounted && f.Array != ir.ArrayFixed {
		return false
	}
	switch f.Type.Kind {
	case ir.TBool, ir.TBits, ir.TFloat32, ir.TFloat64:
		return true
	case ir.TFixed:
		// the wide scalar kinds (§15): a raw fixed-point value at its storage
		// width, carried by the wide write below; arrays of them are another
		// card's family
		return f.Array == ir.ArrayNone
	case ir.TString, ir.TBytes:
		return f.Array == ir.ArrayNone
	case ir.TInt:
		if f.Type.Width == 128 {
			// i128/u128 (§15): sixteen bytes, low half first, carried by the
			// wide write below; arrays of them are another card's family
			return f.Array == ir.ArrayNone
		}
		return true
	case ir.TNamed:
		if tableRef(f) != nil {
			return true
		}
		// An ENUM slot holds the ORDINAL at the enum's derived storage width
		// (§7.2); the wire value class carries it as an int, so it is a plain
		// scalar to this writer. A FLAGS slot is the same: its mask is the
		// storage, and the reference writes it as a scalar (§7.2).
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return true
		}
		return false
	}
	return false
}

// guardedCookField reports whether a GUARDED field's storage is one this
// writer carries. A guard is the wire's (docs/SPEC-TABLES.md §2.2, §16.2): it
// decides what RIDES, and the cooked region is the storage, so a scalar or an
// inline text slot under a false guard is written exactly as it is held —
// what internal/tablecook's region writer and the Go leg's tableCookWriter
// both write (§7.6). A reference is different: §3.1's numbering skips a node
// reached only through a false guard, so its slot has no node to point at,
// and a guarded record may hold such a slot; both stay refused by name.
func guardedCookField(f *ir.Field) bool {
	return !f.Type.Pointer && !f.Type.Blob() && tableRef(f) == nil
}

// emitCookVariableRecord writes one record of the graph where it lies, with
// c.out null for the measuring pass, which runs every check (§7.6).
func (g *cookGen) emitCookVariableRecord(name string) {
	rs := cookMember(g.unit, name)
	ml := ir.RecordLayout(g.unit, rs)
	outer := g.body
	g.body = strings.Builder{}
	for _, fl := range ml.Fields {
		g.emitCookGraphField(fl.Field, fl.Offset, "value."+fieldMember(fl.Field), "    ")
	}
	fields := g.body.String()
	g.body = outer
	g.pf("  // The shared ABI's field pieces at the cook's offsets (§7.2).\n")
	if sig := fmt.Sprintf("  static bool _cook%s(wire.%s value, TableCookGraph c, int at) {", name, name); len(sig) <= 80 {
		g.pf("%s\n", sig)
	} else {
		g.pf("  static bool _cook%s(\n    wire.%s value,\n    TableCookGraph c,\n    int at,\n  ) {\n", name, name)
	}
	if cookUsesOut.MatchString(fields) {
		g.pf("    final out = c.out;\n")
	}
	if cookUsesOrder.MatchString(fields) {
		g.pf("    final order = c.order;\n")
	}
	g.pf("%s", fields)
	g.pf("    return true;\n  }\n\n")
}

// recordHasLists reports a record whose node carries an extent of its own:
// any unbounded-array field reachable BY VALUE (docs/SPEC-TABLES.md §2.9,
// §7.2, §7.6). It is recursive like the reference's recordHasExtent
// (internal/tablecook/extent.go) because a record held by value, or one used as
// an element, can hold the list even when this record declares none. A pointer
// reaches its own node, not this one's extent.
func recordHasLists(u *ir.Unit, st *ir.Struct) bool {
	return recordHasListsSeen(u, st, map[*ir.Struct]bool{})
}

func recordHasListsSeen(u *ir.Unit, st *ir.Struct, seen map[*ir.Struct]bool) bool {
	if st == nil || seen[st] {
		return false
	}
	seen[st] = true
	for _, f := range st.Fields {
		if f.IsMap() {
			continue
		}
		if f.IsList() {
			return true
		}
		if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
			continue
		}
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && recordHasListsSeen(u, ref, seen) {
			return true
		}
	}
	return false
}

// fieldHasLists reports a by-value field whose own storage holds an extent.
func fieldHasLists(u *ir.Unit, f *ir.Field) bool {
	if f == nil || f.IsMap() || f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
		return false
	}
	ref, ok := f.Type.Ref.(*ir.Struct)
	return ok && recordHasLists(u, ref)
}

// emitCookVariableExtent writes the measure half of a record's extent walk: the
// bytes its unbounded arrays take behind it, in pre-order (§7.2), recursing
// into by-value records and list elements exactly as the reference's carver
// does (internal/tablecook/extent.go). The running point is a parameter so a
// nested record's arrays align to the holder's extent offset, not to zero.
func (g *cookGen) emitCookVariableExtent(name string) {
	rs := cookMember(g.unit, name)
	if !recordHasLists(g.unit, rs) {
		return
	}
	g.pf("  // The extent's measure (§7.2): the holder's unbounded arrays behind its\n")
	g.pf("  // record, pre-order, the arithmetic the write carves with.\n")
	g.pf("  static int _extent%s(wire.%s value, int point) {\n", name, name)
	g.emitCookExtentFields(rs, "value", "    ", false, "", "")
	g.pf("    return point;\n  }\n\n")
}

// emitCookVariableCarve writes the write half of a record's extent walk: it
// places each unbounded array's elements after the record and points the
// record's slot at them — a self-relative delta and the count, null for an
// empty list (§7.2, §7.6) — then carves the arrays its by-value records and
// list elements hold, in index order, which is the reference's pre-order
// (internal/tablecook/extent.go). It runs with `c.out` null while the region
// only measures, and returns the extent it carved.
func (g *cookGen) emitCookVariableCarve(name string) {
	rs := cookMember(g.unit, name)
	if !recordHasLists(g.unit, rs) {
		return
	}
	g.pf("  // The extent's carve (§7.6): the same pre-order walk, writing.\n")
	if sig := fmt.Sprintf("  static int _carve%s(wire.%s value, TableCookGraph c, int at, int base, int point) {", name, name); len(sig) <= 80 {
		g.pf("%s\n", sig)
	} else {
		g.pf("  static int _carve%s(\n    wire.%s value,\n    TableCookGraph c,\n    int at,\n    int base,\n    int point,\n  ) {\n", name, name)
	}
	// the walk state is read only where this record places an array itself;
	// a record that only descends into its fields' extents reads neither
	outer := g.body
	g.body = strings.Builder{}
	g.emitCookExtentFields(rs, "value", "    ", true, "at", "base")
	fields := g.body.String()
	g.body = outer
	if cookUsesOut.MatchString(fields) {
		g.pf("    final out = c.out;\n")
	}
	if cookUsesOrder.MatchString(fields) {
		g.pf("    final order = c.order;\n")
	}
	g.pf("%s", fields)
	g.pf("    return point;\n  }\n\n")
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// emitCookExtentFields walks one record's fields in declaration order, emitting
// the measure half (write false) or the write half (write true) of §7.2's
// extent. `expr` is the record value, `at` its own address for the write half,
// and `base` the extent's start, which a nested record keeps from its holder.
func (g *cookGen) emitCookExtentFields(st *ir.Struct, expr, ind string, write bool, at, base string) {
	ml := ir.RecordLayout(g.unit, st)
	for _, fl := range ml.Fields {
		f := fl.Field
		if f.IsList() {
			size, align := ir.ListElementLayout(g.unit, f)
			slotOff := ir.FieldPieces(g.unit, f, fl.Offset)[0].Offset
			g.pf("%s{\n", ind)
			g.pf("%s  final l = %s.%s;\n", ind, expr, fieldMember(f))
			g.pf("%s  final n = l.length;\n", ind)
			g.pf("%s  point = ((point + %d - 1) & ~(%d - 1));\n", ind, align, align)
			if write { // only the carve places the array at its start
				g.pf("%s  final start = point;\n", ind)
			}
			g.pf("%s  point += n * %d;\n", ind, size)
			if write && (f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64) && tableRef(f) == nil {
				// a cooked float element is finite (docs/SPEC-TABLES.md §3.3):
				// measure and write refuse a NaN or an infinity alike
				g.pf("%s  for (var i = 0; i < n; i++) {\n", ind)
				g.cookRefuseUnless(ind+"    ", "l[i].isFinite", "-1")
				g.pf("%s  }\n", ind)
			}
			if write {
				g.pf("%s  if (out != null) {\n", ind)
				g.pf("%s    final array = %s + start;\n", ind, base)
				g.pf("%s    out.setInt64(%s + %d, n > 0 ? array - (%s + %d) : 0, order);\n", ind, at, slotOff, at, slotOff)
				g.pf("%s    out.setInt32(%s + %d, n, order);\n", ind, at, slotOff+8)
				g.pf("%s    for (var i = 0; i < n; i++) {\n", ind)
				if ref := tableRef(f); ref != nil {
					g.pf("%s      if (!_cook%s(l[i], c, array + i * %d)) return -1;\n", ind, ref.Name, size)
				} else if un := unionRef(f); un != nil {
					// a LIST OF UNIONS (§2.6): each element is the union's own
					// storage, written where it lies by the union's element writer
					g.queueForm1Union(un)
					g.pf("%s      if (!_cook%s(l[i], c, array + i * %d)) return -1;\n", ind, un.Name, size)
				} else {
					// inside the `if (out != null)` above: out is promoted
					g.emitCookScalarStore(f, "l[i]", fmt.Sprintf("array + i * %d", size), size, ind+"      ")
				}
				g.pf("%s    }\n%s  }\n", ind, ind)
			}
			if fieldHasLists(g.unit, f) {
				ref := tableRef(f)
				g.pf("%s  for (var i = 0; i < n; i++) {\n", ind)
				if write {
					g.pf("%s    point = _carve%s(l[i], c, %s + start + i * %d, %s, point);\n", ind, ref.Name, base, size, base)
				} else {
					g.pf("%s    point = _extent%s(l[i], point);\n", ind, ref.Name)
				}
				g.pf("%s  }\n", ind)
			}
			g.pf("%s}\n", ind)
			continue
		}
		if !fieldHasLists(g.unit, f) {
			continue
		}
		ref := tableRef(f)
		// A BOUNDED array of records walks its elements too: the reference's
		// carver descends a counted or fixed array by-value, live entries only
		// for a counted one (internal/tablecook/extent.go).
		if f.Array == ir.ArrayCounted || f.Array == ir.ArrayFixed {
			limit := fmt.Sprintf("%s.%s.length", expr, fieldMember(f))
			if f.Array == ir.ArrayFixed {
				limit = fmt.Sprint(f.ArrayBound)
			}
			stride := ir.FieldPieces(g.unit, f, fl.Offset)[0].Size / maxInt64(f.ArrayBound, 1)
			if f.Array == ir.ArrayCounted {
				stride = ir.FieldPieces(g.unit, f, fl.Offset)[0].Size / maxInt64(f.ArrayBound, 1)
			}
			g.pf("%sfor (var i = 0; i < %s; i++) {\n", ind, limit)
			if write {
				g.pf("%s  point = _carve%s(%s.%s[i], c, %s + %d + i * %d, %s, point);\n", ind, ref.Name, expr, fieldMember(f), at, fl.Offset, stride, base)
			} else {
				g.pf("%s  point = _extent%s(%s.%s[i], point);\n", ind, ref.Name, expr, fieldMember(f))
			}
			g.pf("%s}\n", ind)
			continue
		}
		if write {
			g.pf("%spoint = _carve%s(%s.%s, c, %s + %d, %s, point);\n", ind, ref.Name, expr, fieldMember(f), at, fl.Offset, base)
		} else {
			g.pf("%spoint = _extent%s(%s.%s, point);\n", ind, ref.Name, expr, fieldMember(f))
		}
	}
}

// emitCookRef writes one pointer slot's c.ref call, in dart format's tall
// form when the one-line form passes 80 columns.
func (g *cookGen) emitCookRef(ind, at, target string) {
	if one := fmt.Sprintf("%sc.ref(%s, %s);", ind, at, target); len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	g.pf("%sc.ref(\n%s  %s,\n%s  %s,\n%s);\n", ind, ind, at, ind, target, ind)
}

// the two walk-state locals a record body may read.
var (
	cookUsesOut   = regexp.MustCompile(`\bout\b`)
	cookUsesOrder = regexp.MustCompile(`\border\b`)
)

func (g *cookGen) emitCookVariableField(f *ir.Field, offset int64, expr, ind string) {
	pieces := ir.FieldPieces(g.unit, f, offset)
	switch {
	case f.Type.Blob():
		if f.Type.Kind == ir.TString {
			g.pf("%sc.blob(at + %d, %s, %s);\n", ind, offset, dartU64Literal(ir.StringWireTypeId), expr)
		} else {
			g.pf("%sc.blob(at + %d, %s, %s);\n", ind, offset, dartU64Literal(ir.BytesWireTypeId), expr)
		}
	case isUnionField(f):
		// §7.2 union storage: the tag at the field's offset, then the set
		// arm's slot at the arms' overlay offset. The Go reference's tableCook
		// writer (internal/codegen/gotable/cookwrite.go) writes the tag, refuses
		// a tag naming no arm, and writes the set arm's field at the same slot.
		un := unionRef(f)
		_, _, cookTag, armOffset := ir.UnionLayout(g.unit, un)
		writeTag := fmt.Sprintf("out.setUint64(%s, %s.armTag, order)", fmt.Sprintf("at + %d", offset), expr)
		switch cookTag {
		case 1:
			writeTag = fmt.Sprintf("out.setUint8(%s, %s.armTag)", fmt.Sprintf("at + %d", offset), expr)
		case 2:
			writeTag = fmt.Sprintf("out.setUint16(%s, %s.armTag, order)", fmt.Sprintf("at + %d", offset), expr)
		case 4:
			writeTag = fmt.Sprintf("out.setUint32(%s, %s.armTag, order)", fmt.Sprintf("at + %d", offset), expr)
		}
		g.pf("%sif (%s.armTag < 0 || %s.armTag > %d) return false;\n", ind, expr, expr, len(un.Variants))
		g.pf("%sif (out != null) {\n", ind)
		g.pf("%s  %s;\n", ind, writeTag)
		g.pf("%s}\n", ind)
		g.pf("%sswitch (%s.armTag) {\n", ind, expr)
		for i, arm := range un.Variants {
			g.pf("%s  case %d:\n", ind, i+1)
			if !arm.Void() {
				g.emitCookVariableField(arm.F, offset+armOffset, expr+"."+armMember(arm), ind+"    ")
			}
			g.pf("%s    break;\n", ind)
		}
		g.pf("%s}\n", ind)
	case f.IsList():
		// An unbounded array's slot — an int64 delta and an int32 count — is
		// written by the record's extent carve (docs/SPEC-TABLES.md §7.2,
		// §7.6); the record body leaves it at zero, empty list included.
		break
	case f.Array == ir.ArrayCounted:
		bound := f.ArrayBound
		stride := pieces[0].Size / bound
		ref := tableRef(f)
		g.pf("%sif (%s.length > %d) return false;\n", ind, expr, bound)
		g.pf("%sout?.setInt32(at + %d, %s.length, order);\n", ind, pieces[1].Offset, expr)
		g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, bound)
		switch {
		case ref != nil && f.Type.Pointer:
			g.emitCookRef(ind+"  ", fmt.Sprintf("at + %d + i * %d", offset, stride), fmt.Sprintf("i < %s.length ? %s[i] : null", expr, expr))
		case ref != nil:
			// a DEAD slot holds the element's declared defaults, as the
			// reference's storage does (§7.6)
			if line := fmt.Sprintf("%s  final e = i < %s.length ? %s[i] : wire.%s();", ind, expr, expr, ref.Name); len(line) <= 80 {
				g.pf("%s\n", line)
			} else {
				g.pf("%s  final e = i < %s.length\n%s      ? %s[i]\n%s      : wire.%s();\n", ind, expr, ind, expr, ind, ref.Name)
			}
			g.pf("%s  if (!_cook%s(e, c, at + %d + i * %d)) return false;\n", ind, ref.Name, offset, stride)
		case unionRef(f) != nil:
			// an element that is a union: its own storage, a dead slot being
			// the zero storage the region already holds (§2.6)
			g.pf("%s  if (i >= %s.length) break;\n", ind, expr)
			g.emitCookForm1UnionElement(unionRef(f), expr+"[i]", fmt.Sprintf("at + %d + i * %d", offset, stride), ind+"  ")
		case isWide(f):
			// a wide element (§3, §15): a dead slot is the raw zero, which
			// the zero-filled region already holds, so only live ones write
			g.pf("%s  if (i >= %s.length) break;\n", ind, expr)
			g.pf("%s  final e = %s[i];\n", ind, expr)
			g.emitCookElementWide(f, "e", offset, stride, ind+"  ")
		default:
			zero := "0"
			switch f.Type.Kind {
			case ir.TBool:
				zero = "false"
			case ir.TFloat32, ir.TFloat64:
				zero = "0.0"
			}
			g.pf("%s  final e = i < %s.length ? %s[i] : %s;\n", ind, expr, expr, zero)
			g.emitCookFixedScalar(f, "e", fmt.Sprintf("at + %d + i * %d", offset, stride), stride, ind+"  ")
		}
		g.pf("%s}\n", ind)
	case f.Array == ir.ArrayFixed:
		bound := f.ArrayBound
		stride := pieces[0].Size / bound
		ref := tableRef(f)
		g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, bound)
		switch {
		case ref != nil && f.Type.Pointer:
			g.emitCookRef(ind+"  ", fmt.Sprintf("at + %d + i * %d", offset, stride), expr+"[i]")
		case ref != nil:
			g.pf("%s  if (!_cook%s(%s[i], c, at + %d + i * %d)) return false;\n", ind, ref.Name, expr, offset, stride)
		case unionRef(f) != nil:
			g.emitCookForm1UnionElement(unionRef(f), expr+"[i]", fmt.Sprintf("at + %d + i * %d", offset, stride), ind+"  ")
		case isWide(f):
			g.pf("%s  final e = %s[i];\n", ind, expr)
			g.emitCookElementWide(f, "e", offset, stride, ind+"  ")
		default:
			g.emitCookFixedScalar(f, expr+"[i]", fmt.Sprintf("at + %d + i * %d", offset, stride), stride, ind+"  ")
		}
		g.pf("%s}\n", ind)
	case isPointerTable(f):
		g.emitCookRef(ind, fmt.Sprintf("at + %d", offset), expr)
	case isByValueTable(f):
		g.pf("%sif (!_cook%s(%s, c, at + %d)) return false;\n", ind, tableRef(f).Name, expr, offset)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.needConvert = true
		text := dartName(f.Name) + "Text"
		g.pf("%sfinal %s = utf8.encode(%s);\n", ind, text, expr)
		g.pf("%sif (%s.length > %d) return false;\n", ind, text, f.Type.Size)
		g.pf("%sif (out != null) {\n", ind)
		g.pf("%s  out.setInt32(at + %d, %s.length, order);\n", ind, pieces[1].Offset, text)
		g.pf("%s  for (var i = 0; i < %s.length; i++) {\n", ind, text)
		g.pf("%s    out.setUint8(at + %d + i, %s[i]);\n", ind, offset, text)
		g.pf("%s  }\n%s}\n", ind, ind)
	case isWide(f):
		g.emitCookVariableWide(f, expr, fmt.Sprintf("at + %d", offset), fmt.Sprintf("at + %d", offset+8), int64(ir.TableKindWidth(wideKind(f))), ind)
	default:
		g.emitCookFixedScalar(f, expr, fmt.Sprintf("at + %d", offset), pieces[0].Size, ind)
	}
}

// emitCookVariableWide writes a wide scalar's RAW BigInt value (docs/SPEC-TABLES.md
// §3, §15) at its kind's width, byte-identical to the Go reference leg
// (internal/codegen/gotable/cookwrite.go): a 128-bit value as its low 64-bit
// half first and then the high half, each word in the target byte order, and a
// narrower fixed-point value as its width bytes in that same order. addr is
// the slot's address expression and addr8 the same plus eight (the high word
// of a 128-bit value), so an array element (`at + 8 + i * 4`) rides the same
// write as a field.
func (g *cookGen) emitCookVariableWide(f *ir.Field, expr, addr, addr8 string, width int64, ind string) {
	g.pf("%sif (out != null) {\n", ind)
	if width == 16 {
		// toSigned(64) re-spells the 64-bit half as Dart's signed int, so a
		// half whose top bit is set lands as its exact two's-complement bits
		// (toInt() on a BigInt past 2^63-1 saturates instead of wrapping). The
		// halves are written in the target word order exactly as the Go reference
		// leg does: the low half first on a little-endian target, the high half
		// first on a big-endian one (internal/codegen/gotable/cookwrite.go).
		g.pf("%s  final wLow = %s.toSigned(64).toInt();\n", ind, expr)
		g.pf("%s  final wHigh = (%s >> 64).toSigned(64).toInt();\n", ind, expr)
		g.emitDartCall(ind+"  ", "out.setUint64", addr, "order == Endian.little ? wLow : wHigh", "order")
		g.emitDartCall(ind+"  ", "out.setUint64", addr8, "order == Endian.little ? wHigh : wLow", "order")
	} else {
		// the byte index is `b`, never `i`: an array element's addr already
		// reads the element index i
		g.pf("%s  for (var b = 0; b < %d; b++) {\n", ind, width)
		g.pf("%s    var shift = order == Endian.little ? b : %d - 1 - b;\n", ind, width)
		g.emitDartCall(ind+"    ", "out.setUint8", addr+" + b", fmt.Sprintf("(%s >> (shift * 8)).toUnsigned(8).toInt()", expr))
		g.pf("%s  }\n", ind)
	}
	g.pf("%s}\n", ind)
}

// emitDartCall writes one call statement in the spelling `dart format`
// writes: on one line when it fits in 80 columns, else one argument a line
// with a trailing comma.
func (g *cookGen) emitDartCall(ind, callee string, args ...string) {
	if one := fmt.Sprintf("%s%s(%s);", ind, callee, strings.Join(args, ", ")); len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s%s(\n", ind, callee)
	for _, a := range args {
		g.pf("%s  %s,\n", ind, a)
	}
	g.pf("%s);\n", ind)
}

// emitCookElementWide writes element i of an array of a wide kind at its
// stride, the element's raw BigInt in `expr` (§3, §15, §7.2).
func (g *cookGen) emitCookElementWide(f *ir.Field, expr string, offset, stride int64, ind string) {
	addr := fmt.Sprintf("at + %d + i * %d", offset, stride)
	g.emitCookVariableWide(f, expr, addr, addr+" + 8", stride, ind)
}

const dartCookWriteReport = `
// §7.6's write refusals use the reference leg's words, never a new vocabulary.
final class TableCookWriteReport {
  String reason = 'ok';
` + dartCookFloat32Writer + "}\n" + dartCookGraph

// dartCookGraph is the variable cook's walk state (§7.6): the target order,
// where each numbered node landed, the numbering's two lookups, and the
// output, which stays null while the walk only measures.
const dartCookGraph = `
// §7.6's variable cook in progress: where each node of §3.1's numbering
// landed, and the output (null while the walk measures).
final class TableCookGraph {
  final Endian order;
  final List<int> offsets;
  final int Function(Object node) indexOf;
  final int Function(int id, Object blob) blobIndex;
  ByteData? out;

  TableCookGraph(this.order, int count, this.indexOf, this.blobIndex)
    : offsets = List<int>.filled(count, 0);

  // A reference slot holds the signed delta from the slot to its target node,
  // and a null is a delta of zero (§6.3).
  void ref(int slot, Object? node) {
    final out = this.out;
    if (out == null) return;
    var delta = 0;
    if (node != null) delta = 64 + offsets[indexOf(node) - 1] - slot;
    out.setInt64(slot, delta, order);
  }

  // A blob is a node of its own reserved type id, numbered by its identity
  // (§3.1): the String or byte list the field holds.
  void blob(int slot, int id, Object? blob) {
    final out = this.out;
    if (out == null) return;
    var delta = 0;
    if (blob != null) delta = 64 + offsets[blobIndex(id, blob) - 1] - slot;
    out.setInt64(slot, delta, order);
  }
}
`

const dartCookFloat32Writer = `
  static final Float32List _cookFloat32 = Float32List(1);
  static final Uint32List _cook32Words = _cookFloat32.buffer.asUint32List();

  // A cooked float is finite (docs/SPEC-TABLES.md §3.3): the writer refused a
  // NaN or an infinity before it reaches here, so this is the ordinary
  // narrowing, exact for every finite float32 value.
  static int float32Bits(double value) {
    _cookFloat32[0] = value;
    return _cook32Words[0];
  }
`
