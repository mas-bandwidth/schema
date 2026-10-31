// THE RETAINED ROUND TRIP (docs/SPEC-TABLES.md §6.6), the card's slice of
// retain-unknown: a load that keeps what this build cannot name in the
// caller's two bounded stores, a measure and a save that re-emit the retained
// fields at the end of the body they came from, byte-equal to the Go and C
// backends' retained save. The retained record is READER-PRIVATE, so its
// layout is this port's own; only the re-emitted bytes must match the
// reference legs byte for byte.
package darttable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitRetainRuntime emits the retain-unknown runtime once per unit
// (docs/SPEC-TABLES.md §6.6): the caller-owned TableRetain, the resolving walk
// that captures an unknown field with every reference widened to its 64-bit
// id, the reverse walk that re-emits it, and the set of ids this build can
// name. The six excluded classes (§6.6) each resolve to null and are counted
// one retainLost by the caller, never retained silently.
func (g *wireGen) emitRetainRuntime() {
	ids := make([]string, 0, len(ir.TableWireIds(g.unit)))
	for _, id := range ir.TableWireIds(g.unit) {
		ids = append(ids, dartU64Literal(id))
	}
	g.pf(dartRetainRuntime, strings.Join(ids, ", "))
}

// dartRetainRuntime is the Dart source of the retain-unknown runtime, with one
// %s placeholder for the unit's known-id set (docs/SPEC-TABLES.md §6.6). It
// reuses the wire runtime's reader and writer, so a retained record's resolved
// bytes are the same canonical framing the plain save writes.
const dartRetainRuntime = `
// ---- retain-unknown (docs/SPEC-TABLES.md §6.6) ----
//
// TableRetain is the caller's two bounded stores, threaded through the three
// verbs and never grown by the codec. A retained record is READER-PRIVATE:
// references and framed lengths ride as 8-byte little-endian, so a record
// costs 9 + 8 * steps + resolved.length bytes, and a record the remaining
// capacity cannot hold whole is dropped and counted one retainLost (§6.6).

final class TableRetain {
  final int capacity;
  final List<int> ids;
  int idUsed = 0;
  int used = 0;
  final List<_RetainRecord> records = <_RetainRecord>[];
  TableRetain(this.capacity, this.ids);
  void reset() {
    records.clear();
    used = 0;
    idUsed = 0;
  }
}

// A record names the BODY it came from by that body's storage object, the
// value its load filled: a nested table by value, an array element, a union
// arm, a map value and a pointer's node are each their own object, so the
// save re-emits a record at the end of exactly the body it was read from,
// wherever that body sits in the tree. path is the field walk that reached it,
// kept for the record's cost.
final class _RetainRecord {
  final Object owner;
  final List<int> path;
  final int id;
  final int kind;
  final List<int> resolved;
  _RetainRecord(this.owner, this.path, this.id, this.kind, this.resolved);
  int cost() => 9 + 8 * (path.length - 1) + resolved.length;
}

// the ids this build can name (docs/SPEC-TABLES.md §6.6): a retained id is by
// definition not here, and takes its entry from the caller's list rather than
// the generated table.
const Set<int> _retainKnown = <int>{%s};

int _retainWidth(int kind) {
  switch (kind) {
    case 1:
    case 2:
    case 6:
    case 20:
    case 25:
      return 1;
    case 3:
    case 7:
    case 21:
    case 26:
      return 2;
    case 4:
    case 8:
    case 10:
    case 22:
    case 27:
      return 4;
    case 5:
    case 9:
    case 11:
    case 23:
    case 28:
    case 36: // a timestamp (ir.TableKindWidth)
      return 8;
    case 18:
    case 19:
    case 24:
    case 29:
      return 16;
  }
  return 0;
}

List<int> _u64le(int v) {
  final o = <int>[];
  for (var i = 0; i < 8; i++) {
    o.add(v & 0xFF);
    v = v >> 8;
  }
  return o;
}

void _lebOut(List<int> out, int v) {
  while (true) {
    final b = v & 0x7F;
    v = v >> 7;
    if (v == 0) {
      out.add(b);
      return;
    }
    out.add(b | 0x80);
  }
}

// _retainId resolves one reference to the id it names (§6.6); null on a
// reference above the entry count, at zero, or at a reserved id.
int? _retainId(TableWireReader r, int ref) {
  final id = r.refToId[ref];
  if (id == null || id == 0 || id == _kNodeTableId || id == _kBuildVersionId || id == _kVocabularyId) {
    return null;
  }
  return id;
}

List<int>? _retainRaw(TableWireReader r, int n) {
  if (!r.has(n)) return null;
  final out = r.data.sublist(r.pos, r.pos + n);
  r.pos += n;
  return out;
}

// _retainResolve reads one field's payload in place (advancing the reader past
// it) and returns it with every reference widened to its 64-bit id (§6.6).
// Null means the field is an excluded class — kind 17, a reserved id, or a
// damaged walk — and the caller counts one retainLost and re-skips the field.
List<int>? _retainResolve(TableWireReader r, int kind, int depth) {
  if (depth > 64) return null;
  final w = _retainWidth(kind);
  if (w != 0) return _retainRaw(r, w);
  switch (kind) {
    case 12:
    case 31:
    case 32:
    case 33:
      final len = r.readLeb();
      if (len == null || !r.has(len)) return null;
      final out = <int>[];
      _lebOut(out, len);
      out.addAll(r.data.sublist(r.pos, r.pos + len));
      r.pos += len;
      return out;
    case 17:
      return null;
    case 30:
      final ref = r.readLeb();
      if (ref == null) return null;
      if (ref == 0) return _u64le(0);
      final id = _retainId(r, ref);
      if (id == null) return null;
      return _u64le(id);
    case 13:
    case 14:
    case 16:
      return _retainFramed(r, kind, depth);
    case 15:
      final ref = r.readLeb();
      if (ref == null) return null;
      if (ref == 0) return _u64le(0);
      final id = _retainId(r, ref);
      if (id == null) return null;
      if (!r.has(1)) return null;
      final ak = r.readByte();
      final sub = _retainResolve(r, ak, depth + 1);
      if (sub == null) return null;
      return <int>[..._u64le(id), ak, ...sub];
    default:
      return null;
  }
}

List<int>? _retainFramed(TableWireReader r, int kind, int depth) {
  final n = r.readLeb();
  if (n == null || !r.has(n)) return null;
  final end = r.pos + n;
  final sub = TableWireReader.sub(r, r.pos, end);
  final content = _retainContent(sub, kind, depth + 1);
  if (content == null || sub.pos != end) return null;
  r.pos = end;
  return <int>[..._u64le(content.length), ...content];
}

List<int>? _retainContent(TableWireReader r, int kind, int depth) {
  if (depth > 64) return null;
  switch (kind) {
    case 17:
      return null;
    case 13:
      return _retainBody(r, depth);
    case 14:
    case 16:
      return _retainArray(r, kind, depth);
    default:
      return _retainResolve(r, kind, depth);
  }
}

List<int>? _retainBody(TableWireReader r, int depth) {
  final out = <int>[];
  while (true) {
    final ref = r.readLeb();
    if (ref == null) return null;
    if (ref == 0) {
      out.addAll(_u64le(0));
      return out;
    }
    final id = _retainId(r, ref);
    if (id == null) return null;
    if (!r.has(1)) return null;
    final kind = r.readByte();
    final sub = _retainResolve(r, kind, depth + 1);
    if (sub == null) return null;
    out.addAll(_u64le(id));
    out.add(kind);
    out.addAll(sub);
  }
}

List<int>? _retainArray(TableWireReader r, int kind, int depth) {
  if (!r.has(1)) return null;
  final ek = r.readByte();
  final count = r.readLeb();
  if (count == null) return null;
  final out = <int>[ek, ..._u64le(count)];
  for (var i = 0; i < count; i++) {
    if (kind == 16) {
      final ref = r.readLeb();
      if (ref == null) return null;
      final id = _retainId(r, ref);
      if (id == null) return null;
      final framed = _retainFramed(r, ek, depth + 1);
      if (framed == null) return null;
      out.addAll(_u64le(id));
      out.addAll(framed);
    } else {
      final sub = _retainResolve(r, ek, depth + 1);
      if (sub == null) return null;
      out.addAll(sub);
    }
  }
  return out;
}

// ---- the resolving walk, emit side (docs/SPEC-TABLES.md §6.6) ----
//
// _retainCursor walks one retained record's resolved bytes; a reference is an
// 8-byte little-endian id, a framed length an 8-byte little-endian count.
final class _RetainCursor {
  final List<int> b;
  int at = 0;
  _RetainCursor(this.b);
  int? u64() {
    if (at + 8 > b.length) return null;
    var v = 0;
    for (var i = 0; i < 8; i++) {
      v |= b[at + i] << (8 * i);
    }
    at += 8;
    return v;
  }
  int? byte() {
    if (at >= b.length) return null;
    return b[at++];
  }
}

List<int>? _retainEmit(WireState state, int kind, List<int> resolved) {
  final c = _RetainCursor(resolved);
  final out = _retainEmitPayload(state, kind, c, 0);
  if (out == null || c.at != resolved.length) return null;
  return out;
}

List<int>? _retainEmitPayload(WireState state, int kind, _RetainCursor c, int depth) {
  if (depth > 64) return null;
  final w = _retainWidth(kind);
  if (w != 0) {
    if (c.at + w > c.b.length) return null;
    final o = c.b.sublist(c.at, c.at + w);
    c.at += w;
    return o;
  }
  switch (kind) {
    case 12:
    case 31:
    case 32:
    case 33:
      final start = c.at;
      var len = 0;
      var shift = 0;
      while (true) {
        if (c.at >= c.b.length) return null;
        final b = c.b[c.at];
        c.at++;
        len |= (b & 0x7F) << shift;
        if ((b & 0x80) == 0) break;
        shift += 7;
        if (shift >= 64) return null;
      }
      if (c.at + len > c.b.length) return null;
      final o = c.b.sublist(start, c.at + len);
      c.at += len;
      return o;
    case 17:
      return null;
    case 30:
      final id = c.u64();
      if (id == null) return null;
      if (id == 0) return <int>[0];
      final ref = state.retainRefFor(id);
      if (ref == 0) return null;
      final o = <int>[];
      _lebOut(o, ref);
      return o;
    case 13:
    case 14:
    case 16:
      return _retainEmitFramed(state, kind, c, depth);
    case 15:
      final id = c.u64();
      if (id == null) return null;
      if (id == 0) return <int>[0];
      final ref = state.retainRefFor(id);
      if (ref == 0) return null;
      final ak = c.byte();
      if (ak == null) return null;
      final sub = _retainEmitPayload(state, ak, c, depth + 1);
      if (sub == null) return null;
      final o = <int>[];
      _lebOut(o, ref);
      o.add(ak);
      o.addAll(sub);
      return o;
    default:
      return null;
  }
}

List<int>? _retainEmitFramed(WireState state, int kind, _RetainCursor c, int depth) {
  final len = c.u64();
  if (len == null) return null;
  final end = c.at + len;
  if (end > c.b.length) return null;
  final content = _retainEmitContent(state, kind, c, end, depth + 1);
  if (content == null || c.at != end) return null;
  final o = <int>[];
  _lebOut(o, content.length);
  o.addAll(content);
  return o;
}

List<int>? _retainEmitContent(WireState state, int kind, _RetainCursor c, int end, int depth) {
  switch (kind) {
    case 17:
      return null;
    case 13:
      return _retainEmitBody(state, c, end, depth);
    case 14:
    case 16:
      return _retainEmitArray(state, kind, c, end, depth);
    default:
      return _retainEmitPayload(state, kind, c, depth);
  }
}

List<int>? _retainEmitBody(WireState state, _RetainCursor c, int end, int depth) {
  final o = <int>[];
  while (true) {
    final id = c.u64();
    if (id == null) return null;
    if (id == 0) {
      o.add(0);
      return o;
    }
    final ref = state.retainRefFor(id);
    if (ref == 0) return null;
    final kind = c.byte();
    if (kind == null) return null;
    final sub = _retainEmitPayload(state, kind, c, depth + 1);
    if (sub == null) return null;
    _lebOut(o, ref);
    o.add(kind);
    o.addAll(sub);
  }
}

List<int>? _retainEmitArray(WireState state, int kind, _RetainCursor c, int end, int depth) {
  final ek = c.byte();
  if (ek == null) return null;
  final count = c.u64();
  if (count == null) return null;
  final o = <int>[ek];
  _lebOut(o, count);
  for (var i = 0; i < count; i++) {
    if (kind == 16) {
      final keyId = c.u64();
      if (keyId == null) return null;
      final ref = state.retainRefFor(keyId);
      if (ref == 0) return null;
      final framed = _retainEmitFramed(state, ek, c, depth + 1);
      if (framed == null) return null;
      _lebOut(o, ref);
      o.addAll(framed);
    } else {
      final sub = _retainEmitPayload(state, ek, c, depth + 1);
      if (sub == null) return null;
      o.addAll(sub);
    }
  }
  return o;
}

// _retainTail re-emits the retained records read from the body being written
// — the ones whose owner is that body's storage object — in the order
// retained, at the end of that body (§6.6). A record whose id cannot be
// interned (the caller's list is full) or whose walk is damaged is dropped and
// counted one retainLost.
void _retainTail(TableWireWriter w, Object owner) {
  final t = w.state.retain;
  if (t == null) return;
  for (final rec in t.records) {
    if (!identical(rec.owner, owner)) continue;
    w.state.lost = false;
    final ref = w.state.retainRefFor(rec.id);
    if (ref == 0 || w.state.lost) {
      w.state.report!.retainLost++;
      continue;
    }
    final emitted = _retainEmit(w.state, rec.kind, rec.resolved);
    if (emitted == null) {
      w.state.report!.retainLost++;
      continue;
    }
    w.putLeb(ref);
    w.putByte(rec.kind);
    w.putBytes(emitted);
  }
}
`

// emitRetainMembers emits the three retaining verbs for one variable root
// (docs/SPEC-TABLES.md §6.6): <name>WireLoadRetain, <name>WireSaveRetain and
// <name>WireMeasureRetain. LoadRetain resets the caller's store and reads the
// root body through the retaining reader; SaveRetain re-numbers the caller's
// id list from scratch and writes the root body plus its retained tail before
// the node-table field and the merged id-table trailer.
func (g *wireGen) emitRetainMembers(st *ir.Struct) {
	n := st.Name
	d := dartName(n)
	_, strBlob := ir.PointerReachableBlobs(st)

	g.pf("%s %sWireLoadRetain(Uint8List data, TableRetain retain, TableWireReport report) {\n", n, d)
	g.pf("\tretain.reset();\n")
	g.pf("\tfinal v = %s();\n", n)
	g.pf("\tfinal r = TableWireReader(data, data.length);\n")
	g.pf("\tr.report = report;\n")
	g.pf("\tr.report.nonFiniteFloat = '';\n")
	g.pf("\tr.stringBlob = %v;\n", strBlob)
	g.pf("\tr.retain = retain;\n")
	g.pf("\tr.path = [1];\n")
	g.emitKnownTypes(st, "r", "v")
	g.pf("\tfinal verdict = r.open();\n")
	g.pf("\tif (verdict == 1) { r.report.refused = true; return v; }\n")
	// verdict 3 is framing damage on the root body, not the file's: the body
	// is read tolerantly up to the damage (docs/SPEC-TABLES.md §3.3, §4;
	// internal/tablewire/decode.go decodeWith), so it falls through here.
	g.pf("\tif (verdict == 2) { r.report.malformed = true; return v; }\n")
	g.pf("\tr.scanNodeTable();\n")
	g.pf("\tr.pos = 1;\n")
	g.pf("\t%s(r, v, false);\n", d+"WireLoadBody")
	// A NON-FINITE FLOAT REFUSES THE READ (docs/SPEC-TABLES.md §3.3, §6.6):
	// nothing lands, the retained store included.
	g.pf("\tif (r.report.nonFiniteFloat.isNotEmpty) {\n")
	g.pf("\t\tretain.reset();\n")
	g.pf("\t\treturn %s();\n", n)
	g.pf("\t}\n")
	// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4, §6.6): nothing
	// decoded lands and nothing is retained.
	g.pf("\tif (r.report.malformed) {\n")
	g.pf("\t\tretain.reset();\n")
	g.pf("\t\treturn %s();\n", n)
	g.pf("\t}\n")
	g.pf("\treturn v;\n")
	g.pf("}\n\n")

	g.pf("Uint8List %sWireSaveRetain(%s v, TableRetain retain, TableWireReport report) {\n", d, n)
	g.pf("\tretain.idUsed = 0;\n")
	g.pf("\tfinal state = WireState()..refuseSharedBlob = true;\n")
	g.pf("\tstate.retain = retain;\n")
	g.pf("\tstate.report = report;\n")
	g.pf("\tstate.indexRoot(v);\n")
	g.pf("\tfinal open = <Object>{v};\n")
	g.pf("\t%sNumber(state, v, open);\n", d)
	g.pf("\tfinal w = TableWireWriter(state);\n")
	g.pf("\tw.path = [1];\n")
	g.pf("\tw.putByte(1);\n")
	g.pf("\t%sWireSaveBodyFields(w, v);\n", d)
	g.pf("\t_writeNodeTable(w);\n")
	g.pf("\tw.putByte(0);\n")
	g.pf("\tfor (final id in state.ids) { w.putU64(id); }\n")
	g.pf("\tw.putU64(state.ids.length);\n")
	g.pf("\treturn Uint8List.fromList(w.buf);\n")
	g.pf("}\n\n")

	g.pf("int %sWireMeasureRetain(%s v, TableRetain retain) {\n", d, n)
	g.pf("\tfinal report = TableWireReport();\n")
	g.pf("\treturn %sWireSaveRetain(v, retain, report).length;\n", d)
	g.pf("}\n\n")
}
