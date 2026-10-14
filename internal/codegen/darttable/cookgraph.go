package darttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitCookWriteGraph uses the wire's existing numbering, so shared nodes and
// blobs have exactly the same first-use order (§3.1, §7.6).
func (g *cookGen) emitCookWriteGraph(st *ir.Struct) {
	if !cardWireTable(st) {
		return
	}
	g.needWire = true
	n := st.Name
	g.pf("  static int cookMeasure(TableCookWire.%s value) {\n", n)
	g.pf("    try { return _cookPlan(value, Endian.little).$1; } on StateError { return -1; }\n  }\n\n")
	g.pf("  static bool cook(TableCookWire.%s value, Uint8List output, Endian order, {TableCookWriteReport? report}) {\n", n)
	g.pf("    if (report != null) report.reason = 'ok';\n    try {\n")
	g.pf("      final plan = _cookPlan(value, order);\n      if (output.length < plan.$1) { if (report != null) report.reason = 'invalid_value'; return false; }\n")
	g.pf("      output.fillRange(0, plan.$1, 0);\n      final out = ByteData.sublistView(output);\n")
	g.pf("      out.setUint64(0, tableCookMagic, order);\n      out.setUint64(8, tableBuildVersion, order);\n      out.setUint64(16, order == Endian.little ? 1 : 2, order);\n      out.setUint64(24, plan.$2, order);\n      out.setUint64(32, plan.$4.length * 16, order);\n      out.setUint64(40, plan.$3, order);\n")
	g.pf("      _cookNodes(value, plan.$5, plan.$4, out, order, plan.$2);\n      return true;\n    } on StateError catch (error) {\n      if (report != null) report.reason = error.message.toString().startsWith('data cycle:') ? 'data_cycle' : 'invalid_value';\n      return false;\n    }\n  }\n\n")
	g.pf("  static (int, int, int, List<int>, TableCookWire.WireState) _cookPlan(TableCookWire.%s value, Endian order) {\n", n)
	g.pf("    final state = TableCookWire.WireState()..indexRoot(value);\n    TableCookWire.%sNumber(state, value, <Object>{value});\n", dartName(n))
	g.pf("    final offsets = List<int>.filled(state.nodes.length + 1, 0);\n    var data = 0;\n    var alignment = 8;\n")
	g.pf("    for (var i = 0; i < offsets.length; i++) {\n      final obj = i == 0 ? value : state.nodeObjs[i - 1];\n      final type = i == 0 ? %s : state.nodeTypes[i - 1];\n      var align = 8;\n      var size = 0;\n      if (obj == null) {\n        final bytes = state.nodes[i - 1];\n        if (bytes.length > 0xffffffff) throw StateError('invalid_value');\n        size = 8 + bytes.length + (type == %s ? 1 : 0);\n      } else {\n        switch (type) {\n", dartU64Literal(tableWireId(st)), dartU64Literal(ir.StringTypeId))
	cookWalk(g.unit, n, func(name string, ml *ir.MemberLayout) {
		rec := ml.Size
		if cookGraphHasExtent(g.unit, cookMember(g.unit, name)) {
			rec = (rec + 7) & -8
		}
		g.pf("          case %s: size = %d; align = %d; break;\n", dartU64Literal(tableWireId(cookMember(g.unit, name))), rec, ml.Align)
	})
	g.pf("          default: throw StateError('invalid_value');\n        }\n      }\n      if (align > alignment) alignment = align;\n      data = (data + align - 1) & -align;\n      offsets[i] = data;\n      final tail = <int>[64 + data + size, 0];\n      if (obj != null) _cookRecord(type, obj, null, 64 + data, order, state, offsets, tail);\n      data += size + tail[1];\n    }\n    data = (data + alignment - 1) & -alignment;\n    return (64 + data + offsets.length * 16, data, alignment, offsets, state);\n  }\n\n")
	g.pf("  static void _cookNodes(TableCookWire.%s value, TableCookWire.WireState state, List<int> offsets, ByteData out, Endian order, int data) {\n", n)
	g.pf("    for (var i = 0; i < offsets.length; i++) {\n      final obj = i == 0 ? value : state.nodeObjs[i - 1];\n      final type = i == 0 ? %s : state.nodeTypes[i - 1];\n      final at = 64 + offsets[i];\n      if (obj == null) {\n        final bytes = state.nodes[i - 1];\n        out.setUint32(at, bytes.length, order);\n        for (var j = 0; j < bytes.length; j++) { out.setUint8(at + 8 + j, bytes[j]); }\n      } else {\n        var size = 0;\n        switch (type) {\n", dartU64Literal(tableWireId(st)))
	cookWalk(g.unit, n, func(name string, ml *ir.MemberLayout) {
		rec := ml.Size
		if cookGraphHasExtent(g.unit, cookMember(g.unit, name)) {
			rec = (rec + 7) & -8
		}
		g.pf("          case %s: size = %d; break;\n", dartU64Literal(tableWireId(cookMember(g.unit, name))), rec)
	})
	g.pf("        }\n        _cookRecord(type, obj, out, at, order, state, offsets, <int>[at + size, 0]);\n      }\n      out.setUint64(64 + data + i * 16, offsets[i], order);\n      out.setUint64(64 + data + i * 16 + 8, type, order);\n    }\n  }\n\n")
	g.pf("  static void _cookRecord(int type, Object obj, ByteData? out, int at, Endian order, TableCookWire.WireState state, List<int> offsets, List<int> tail) {\n    switch (type) {\n")
	cookWalk(g.unit, n, func(name string, _ *ir.MemberLayout) {
		g.pf("      case %s: _cookValue%s(obj as TableCookWire.%s, out, at, order, state, offsets, tail); break;\n", dartU64Literal(tableWireId(cookMember(g.unit, name))), name, name)
	})
	g.pf("      default: throw StateError('invalid_value');\n    }\n  }\n\n")
	cookWalk(g.unit, n, func(name string, ml *ir.MemberLayout) {
		g.pf("  static void _cookValue%s(TableCookWire.%s value, ByteData? out, int at, Endian order, TableCookWire.WireState state, List<int> offsets, List<int> tail) {\n", name, name)
		for _, fl := range ml.Fields {
			g.emitCookGraphField(fl.Field, fl.Offset, "value."+fieldMember(fl.Field), "    ")
		}
		g.pf("  }\n\n")
	})
	needReference := false
	cookWalk(g.unit, n, func(_ string, ml *ir.MemberLayout) {
		for _, fl := range ml.Fields {
			if fl.Field.Type.Pointer && !fl.Field.Type.Blob() {
				needReference = true
			}
		}
	})
	if needReference {
		g.pf("  static void _cookReference(Object? obj, int slot, ByteData? out, Endian order, TableCookWire.WireState state, List<int> offsets) {\n    if (obj == null) return;\n    final index = state.indexOf(obj) - 1;\n    if (out != null) out.setInt64(slot, 64 + offsets[index] - slot, order);\n  }\n\n")
	}
}

func cookGraphHasExtent(u *ir.Unit, st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.IsList() {
			return true
		}
		if !f.Type.Pointer && f.Type.Kind == ir.TNamed {
			if sub, ok := f.Type.Ref.(*ir.Struct); ok && cookGraphHasExtent(u, sub) {
				return true
			}
		}
	}
	return false
}

func (g *cookGen) emitCookGraphField(f *ir.Field, offset int64, expr, ind string) {
	pieces := ir.FieldPieces(g.unit, f, offset)
	if f.IsList() {
		size, align := ir.ListElementLayout(g.unit, f)
		g.pf("%s{\n%s  if (%s.length > 0x7fffffff) throw StateError('invalid_value');\n", ind, ind, expr)
		g.pf("%s  tail[1] = (tail[1] + %d) & -%d;\n%s  final start = tail[0] + tail[1];\n%s  tail[1] += %s.length * %d;\n", ind, align-1, align, ind, ind, expr, size)
		g.pf("%s  if (out != null) {\n%s    out.setInt64(at + %d, %s.isEmpty ? 0 : start - (at + %d), order);\n%s    out.setInt32(at + %d, %s.length, order);\n%s  }\n", ind, ind, offset, expr, offset, ind, offset+8, expr, ind)
		g.pf("%s  for (var i = 0; i < %s.length; i++) {\n", ind, expr)
		g.emitCookGraphValue(f, expr+"[i]", fmt.Sprintf("start + i * %d", size), size, ind+"    ")
		g.pf("%s  }\n%s}\n", ind, ind)
		return
	}
	if f.Array == ir.ArrayCounted {
		size := pieces[0].Size / f.ArrayBound
		g.pf("%sif (%s.length > %d) throw StateError('invalid_value');\n", ind, expr, f.ArrayBound)
		g.pf("%sif (out != null) out.setInt32(at + %d, %s.length, order);\n", ind, pieces[1].Offset, expr)
		g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, f.ArrayBound)
		value := expr + "[i]"
		if ref := tableRef(f); ref != nil && !f.Type.Pointer {
			value = fmt.Sprintf("(i < %s.length ? %s[i] : TableCookWire.%s())", expr, expr, ref.Name)
		} else if f.Type.Pointer {
			value = fmt.Sprintf("(i < %s.length ? %s[i] : null)", expr, expr)
		} else {
			wg := wireGen{unit: g.unit}
			def := "0"
			switch f.Type.Kind {
			case ir.TBool:
				def = "false"
			case ir.TFloat32, ir.TFloat64:
				def = "0.0"
			case ir.TString, ir.TBytes:
				def = wg.stringDefault(f)
			}
			value = fmt.Sprintf("(i < %s.length ? %s[i] : %s)", expr, expr, def)
		}
		g.emitCookGraphValue(f, value, fmt.Sprintf("at + %d + i * %d", offset, size), size, ind+"  ")
		g.pf("%s}\n", ind)
		return
	}
	g.emitCookGraphValue(f, expr, fmt.Sprintf("at + %d", offset), pieces[0].Size, ind)
}

func (g *cookGen) emitCookGraphValue(f *ir.Field, expr, at string, width int64, ind string) {
	switch {
	case f.Type.Blob():
		encoded := expr + "!"
		id := ir.BytesTypeId
		if f.Type.Kind == ir.TString {
			g.needConvert = true
			encoded = "utf8.encode(" + expr + "!)"
			id = ir.StringTypeId
		}
		g.pf("%sif (%s != null) {\n%s  final index = state.blobIndex(%s, %s) - 1;\n%s  if (out != null) out.setInt64(%s, 64 + offsets[index] - (%s), order);\n%s}\n", ind, expr, ind, dartU64Literal(id), encoded, ind, at, at, ind)
	case f.Type.Pointer:
		g.pf("%s_cookReference(%s, %s, out, order, state, offsets);\n", ind, expr, at)
	case tableRef(f) != nil:
		g.pf("%s_cookValue%s(%s, out, %s, order, state, offsets, tail);\n", ind, f.Type.Name, expr, at)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.needConvert = true
		// Text in the variable authoring class is UTF-8; bounded cook storage is
		// the bytes plus its ABI count companion, never a UTF-16 host string.
		count := ir.FieldPieces(g.unit, &ir.Field{Type: f.Type}, 0)[1].Offset
		g.pf("%s{\n%s  final bytes = utf8.encode(%s);\n%s  if (bytes.length > %d) throw StateError('invalid_value');\n%s  if (out != null) {\n%s    out.setInt32((%s) + %d, bytes.length, order);\n%s    for (var j = 0; j < bytes.length; j++) { out.setUint8((%s) + j, bytes[j]); }\n%s  }\n%s}\n", ind, ind, expr, ind, f.Type.Size, ind, ind, at, count, ind, at, ind, ind)
	default:
		g.emitCookFixedScalar(f, expr, at, width, ind)
	}
}
