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
	ml := ir.RecordLayout(g.unit, st)
	align := ir.RegionAlignOf(ml.Align)
	data := (ml.Size + align - 1) & -align
	total := cookHeaderBytes + data + 16
	g.need(g.homeBase+"Fixed.dart", st.Name)
	g.pf("  // The fixed cook's extent is constant; every storage slot is written (§7.6).\n")
	g.pf("  static int cookMeasure(%s value) => %d;\n\n", st.Name, total)
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
		{64 + data, "0"}, {64 + data + 8, dartU64Literal(tableWireId(st))},
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
		// Typed float arrays already hold the transmitted 32-bit cells. Reading
		// them as doubles would quiet signalling NaNs before narrowing can help.
		if f.Type.Kind == ir.TFloat32 {
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
	g.pf("%sif (out != null) {\n", ind)
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
	g.pf("%s}\n", ind)
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
	var records []string
	cookWalk(g.unit, n, func(name string, _ *ir.MemberLayout) { records = append(records, name) })
	g.pf("  // THE VARIABLE COOK (docs/SPEC-TABLES.md §7.6): the extent, measured by the\n")
	g.pf("  // walk the write repeats; negative when the value is refused.\n")
	g.pf("  static int cookMeasure(wire.%s value, {TableCookWriteReport? report}) =>\n", n)
	g.pf("      _writeRegion(value, null, Endian.little, report);\n\n")
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
	g.pf("      final id = i == 0 ? %s : s.nodeTypes[i - 1];\n", dartU64Literal(tableWireId(st)))
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
		g.pf("          case %s:\n", dartU64Literal(tableWireId(rs)))
		g.pf("            size = %d;\n            alignment = %d;\n", ml.Size, ml.Align)
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
		g.pf("      case %s:\n", dartU64Literal(tableWireId(cookMember(g.unit, r))))
		g.pf("        return _cook%s(node as wire.%s, c, at);\n", r, r)
	}
	g.pf("    }\n    return false;\n  }\n\n")
	for _, r := range records {
		g.emitCookVariableRecord(r)
	}
	g.trimBlank()
}

// variableCookRefusal names the first storage the variable cook writer does
// not carry, or "" when it carries the root's whole closure (§7.2's storage
// kinds: scalar, record, inline string, reference, blob).
func (g *cookGen) variableCookRefusal(st *ir.Struct) string {
	why := ""
	cookWalk(g.unit, st.Name, func(name string, _ *ir.MemberLayout) {
		rs := cookMember(g.unit, name)
		if why != "" || rs == nil {
			return
		}
		if !rs.IsTable {
			why = name + " is a `type`, whose Dart class the variable wire library does not carry"
			return
		}
		for _, f := range rs.Fields {
			if f.Guard != "" && !guardedCookField(f) {
				why = fmt.Sprintf("%s.%s is guarded and holds a reference or a record, whose numbering under a false guard this writer does not carry yet (docs/SPEC-TABLES.md §3.1, §7.6)", name, f.Name)
				return
			}
			if !variableCookField(f) {
				why = fmt.Sprintf("%s.%s's storage (%s) is not carried by the variable cook writer yet (docs/SPEC-TABLES.md §7.2)", name, f.Name, tableFieldTypeName(f))
				return
			}
		}
	})
	return why
}

func variableCookField(f *ir.Field) bool {
	if f.Type.Optional || f.KeyEnum != "" || f.IsMap() || f.IsList() {
		return false
	}
	if f.Type.Blob() {
		return f.Array == ir.ArrayNone
	}
	if f.Array != ir.ArrayNone && f.Array != ir.ArrayCounted {
		return false
	}
	switch f.Type.Kind {
	case ir.TBool, ir.TBits, ir.TFloat32, ir.TFloat64:
		return true
	case ir.TString, ir.TBytes:
		return f.Array == ir.ArrayNone
	case ir.TInt:
		return f.Type.Width != 128
	case ir.TNamed:
		return tableRef(f) != nil
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
		g.emitCookVariableField(fl.Field, fl.Offset, "value."+fieldMember(fl.Field), "    ")
	}
	fields := g.body.String()
	g.body = outer
	g.pf("  // The shared ABI's field pieces at the cook's offsets (§7.2).\n")
	g.pf("  static bool _cook%s(wire.%s value, TableCookGraph c, int at) {\n", name, name)
	if cookUsesOut.MatchString(fields) {
		g.pf("    final out = c.out;\n")
	}
	if cookUsesOrder.MatchString(fields) {
		g.pf("    final order = c.order;\n")
	}
	g.pf("%s", fields)
	g.pf("    return true;\n  }\n\n")
}

// the two walk-state locals a record body may read
var (
	cookUsesOut   = regexp.MustCompile(`\bout\b`)
	cookUsesOrder = regexp.MustCompile(`\border\b`)
)

func (g *cookGen) emitCookVariableField(f *ir.Field, offset int64, expr, ind string) {
	pieces := ir.FieldPieces(g.unit, f, offset)
	switch {
	case f.Type.Blob():
		if f.Type.Kind == ir.TString {
			g.needConvert = true
			g.pf("%sfinal %s = %s;\n", ind, dartName(f.Name)+"Text", expr)
			g.pf("%sc.blob(\n%s  at + %d,\n%s  %s,\n%s  %s == null ? null : utf8.encode(%s),\n%s);\n",
				ind, ind, offset, ind, dartU64Literal(ir.StringWireTypeId), ind, dartName(f.Name)+"Text", dartName(f.Name)+"Text", ind)
		} else {
			g.pf("%sc.blob(at + %d, %s, %s);\n", ind, offset, dartU64Literal(ir.BytesWireTypeId), expr)
		}
	case f.Array == ir.ArrayCounted:
		bound := f.ArrayBound
		stride := pieces[0].Size / bound
		ref := tableRef(f)
		g.pf("%sif (%s.length > %d) return false;\n", ind, expr, bound)
		g.pf("%sout?.setInt32(at + %d, %s.length, order);\n", ind, pieces[1].Offset, expr)
		g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, bound)
		switch {
		case ref != nil && f.Type.Pointer:
			g.pf("%s  c.ref(at + %d + i * %d, i < %s.length ? %s[i] : null);\n", ind, offset, stride, expr, expr)
		case ref != nil:
			// a DEAD slot holds the element's declared defaults, as the
			// reference's storage does (§7.6)
			g.pf("%s  final e = i < %s.length ? %s[i] : wire.%s();\n", ind, expr, expr, ref.Name)
			g.pf("%s  if (!_cook%s(e, c, at + %d + i * %d)) return false;\n", ind, ref.Name, offset, stride)
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
	case isPointerTable(f):
		g.pf("%sc.ref(at + %d, %s);\n", ind, offset, expr)
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
	default:
		g.emitCookFixedScalar(f, expr, fmt.Sprintf("at + %d", offset), pieces[0].Size, ind)
	}
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
  final int Function(int id, List<int> bytes) blobIndex;
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

  // A blob is a node of its own reserved type id, numbered by its bytes (§3.1).
  void blob(int slot, int id, List<int>? bytes) {
    final out = this.out;
    if (out == null) return;
    var delta = 0;
    if (bytes != null) delta = 64 + offsets[blobIndex(id, bytes) - 1] - slot;
    out.setInt64(slot, delta, order);
  }
}
`

const dartCookFloat32Writer = `
  static final Float32List _cookFloat32 = Float32List(2);
  static final Uint32List _cook32Words = _cookFloat32.buffer.asUint32List();
  static final Float64List _cookFloat64 = _cookFloat32.buffer.asFloat64List();
  static final Uint64List _cook64Words = _cookFloat32.buffer.asUint64List();

  static int float32Bits(double value) {
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
