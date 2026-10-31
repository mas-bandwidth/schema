// The VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1, in Dart:
// the card record's constructs — load (tolerant, §4) and save (the
// canonical layout the Go, C, C++ and C# backends write), against the
// committed byte oracle tables/cards/fuzz/card-populated.wire.
// ignore_for_file: unused_element

import 'dart:convert';
import 'dart:typed_data';

import 'BenchTable.dart' show TableWeapon;

// The RESERVED node-table id (§3.1, §5) and the *string BLOB type id
// (§2.5, §3.1), both as their 64-bit two's-complement Dart spelling.
const int _kNodeTableId = -1; // 0xFFFFFFFFFFFFFFFF
const int _kBuildVersionId = -2; // 0xFFFFFFFFFFFFFFFE
const int _kVocabularyId = -3; // 0xFFFFFFFFFFFFFFFD
const int _kBytesBlobId = 3399866181213769700; // fnv1a64("bytes")
const int _kStringBlobId = 8091808378024623192; // fnv1a64("string")
const int _kTypeTableEntity = 3558250755087846312; // fnv1a64("TableEntity")
const int _kTypeTableStat = 2984383410656091285; // fnv1a64("TableStat")
const int _kTypeTableHitEvent = 8389190336562899880; // fnv1a64("TableHitEvent")
const int _kTypeTableChatEvent = -7595037280231051973; // fnv1a64("TableChatEvent")
const int _kTypeTablePickupEvent = 8694226076464086509; // fnv1a64("TablePickupEvent")
const int _kTypeTableMixed = 4871457021464447630; // fnv1a64("TableMixed")

// The report a tolerant read fills: §4's five counters (unknown, kindMismatch,
// widened, clamped, duplicate) and the refusal verdict (§3). malformed and
// refused are the verdict flags; duplicate rises on an EQUAL map key (§2.8),
// with Go's last-wins whole semantics.
final class TableWireReport {
  int unknown = 0;
  int kindMismatch = 0;
  int widened = 0;
  int clamped = 0;
  int duplicate = 0;
  bool malformed = false;
  bool refused = false;
  int retained = 0;
  int retainLost = 0;

  // nonFiniteFloat is the STORED refusal of a float whose raw bits are a NaN
  // or an infinity (docs/SPEC-TABLES.md §3.3): the <Table>.<field> the read
  // met first, '' when it met none. Every read refuses one, the tolerant one
  // too: refused rises with it and the load lands nothing, answering the
  // type's defaults.
  String nonFiniteFloat = '';

  void refuseNonFiniteFloat(String field) {
    if (nonFiniteFloat.isEmpty) nonFiniteFloat = field;
    refused = true;
  }

  // reason is the read's refusal BY NAME, in the reference leg's vocabulary
  // (internal/codegen/gotable/refusal.go, the TableRefuse* Error() strings;
  // docs/SPEC-TABLES.md §3, §4): a non-finite float is non_finite_float naming
  // the field, and it outranks every later verdict; a form byte this reader
  // does not carry is unknown_form, framing the reader cannot follow is
  // wire_damaged, and a read that kept its verdict is ok — the mapping the Go
  // leg's measure gives its two verdicts. It is DERIVED from the flags and the
  // stored field rather than kept beside them, so the verdict and its reason
  // can never disagree. A widened, clamped or unknown field is not a refusal:
  // the read still answers ok.
  String get reason {
    if (nonFiniteFloat.isNotEmpty) return 'non_finite_float: $nonFiniteFloat';
    if (refused) return 'unknown_form';
    if (malformed) return 'wire_damaged';
    return 'ok';
  }
}

// WireState is the id table (§3) and node numbering (§3.1) a save accumulates
// in FIRST-USE order: one per file, shared by the writer and every sub-writer.
final class WireState {
  final Map<int, int> refs = <int, int>{};
  final List<int> ids = <int>[];
  final List<int> nodeTypes = <int>[];
  final List<List<int>> nodes = <List<int>>[];
  final List<Object?> nodeObjs = <Object?>[];
  final Map<Object, int> _objIndex = <Object, int>{};
  final Map<Object, int> _blobIndex = Map<Object, int>.identity();
  // A save is STRICT about a blob two slots name (§2.5): the wire writer
  // refuses it by name, blob_shared. The cook numbers the same graph and does not.
  bool refuseSharedBlob = false;
  TableRetain? retain;
  TableWireReport? report;
  bool lost = false;

  int mark() => ids.length;

  // rollback drops ids interned after mark, so an elided by-value body leaves
  // nothing in the id table (§3).
  void rollback(int m) {
    while (ids.length > m) {
      refs.remove(ids.removeLast());
    }
  }

  void indexRoot(Object v) {
    _objIndex[v] = 1;
  }

  int refFor(int id) {
    final r = refs[id];
    if (r != null) return r;
    ids.add(id);
    final n = ids.length;
    refs[id] = n;
    return n;
  }

  // retainRefFor interns one id in first-use order, taking a retained id's
  // entry from the caller's list rather than the generated table (§6.6): an
  // id this build can name is here, any other is bounded by the caller's id
  // capacity. Returns 0 and sets lost when the caller's list is full.
  int retainRefFor(int id) {
    final r = refs[id];
    if (r != null) return r;
    if (!_retainKnown.contains(id)) {
      final t = retain;
      if (t == null || t.idUsed >= t.ids.length) {
        lost = true;
        return 0;
      }
      t.ids[t.idUsed] = id;
      t.idUsed++;
    }
    return refFor(id);
  }

  // noteBlob numbers one *bytes/*string blob node (§3.1): record k = node
  // index k + 1, keyed by the blob's IDENTITY — the String or byte list the
  // field holds — never by its content and never per slot (§2.5, §3.1). A
  // blob reached twice is one record, because two fields that loaded the same
  // node hold the same object; two distinct blobs carrying equal bytes are two
  // records, as the reference numbers them by pointer
  // (testdata/wire/tables/blob_equal_bytes.bin). Numbering does not intern
  // ids — the node-table write does, in record order (§3). On the web a
  // String is a value, so two equal strings there are one blob.
  int noteBlob(int typeId, Object blob) {
    final found = _blobIndex[blob];
    if (found != null) {
      if (refuseSharedBlob) {
        throw StateError('blob_shared: a blob is owned by one slot and every writer refuses it (docs/SPEC-TABLES.md §2.5)');
      }
      return found;
    }
    nodeTypes.add(typeId);
    nodes.add(blob is String ? utf8.encode(blob) : List<int>.from(blob as List<int>));
    nodeObjs.add(null);
    final idx = nodes.length + 1;
    _blobIndex[blob] = idx;
    return idx;
  }

  int nodeIndexForBlob(int typeId, Object blob) => noteBlob(typeId, blob);

  int blobIndex(int typeId, Object blob) {
    final found = _blobIndex[blob];
    if (found == null) {
      throw StateError('blob was not numbered (docs/SPEC-TABLES.md §3.1)');
    }
    return found;
  }

  // noteTable numbers one table node on first visit and descends immediately,
  // so a child is numbered before the next sibling field (§3.1). A reference
  // to a node still open is a cycle and save refuses it. A node already
  // numbered and closed is shared: one index, no second record.
  void noteTable(int typeId, Object obj, Set<Object> open, void Function(Set<Object> open) descend) {
    if (open.contains(obj)) {
      throw StateError('data cycle: a pointer reaches a node still open (docs/SPEC-TABLES.md §3.1)');
    }
    if (_objIndex.containsKey(obj)) return;
    _objIndex[obj] = nodes.length + 2;
    nodeTypes.add(typeId);
    nodes.add(<int>[]);
    nodeObjs.add(obj);
    open.add(obj);
    descend(open);
    open.remove(obj);
  }

  int indexOf(Object obj) {
    final i = _objIndex[obj];
    if (i == null) {
      throw StateError('node was not numbered (docs/SPEC-TABLES.md §3.1)');
    }
    return i;
  }
}

final class TableWireWriter {
  final List<int> buf = <int>[];
  final WireState state;
  List<int> path = <int>[];
  TableWireWriter(this.state);

  TableWireWriter sub() => TableWireWriter(state)..path = List<int>.from(path);

  void putByte(int b) => buf.add(b & 0xFF);

  void putLeb(int v) {
    while (true) {
      final b = v & 0x7F;
      v = v >> 7;
      if (v == 0) {
        buf.add(b);
        return;
      }
      buf.add(b | 0x80);
    }
  }

  void putU32(int v) {
    for (var i = 0; i < 4; i++) {
      buf.add(v & 0xFF);
      v = v >> 8;
    }
  }

  void putU16(int v) {
    for (var i = 0; i < 2; i++) {
      buf.add(v & 0xFF);
      v = v >> 8;
    }
  }

  void putU64(int v) {
    for (var i = 0; i < 8; i++) {
      buf.add(v & 0xFF);
      v = v >> 8;
    }
  }

  void putBytes(List<int> b) => buf.addAll(b);

  void putString(String s) => putBytes(utf8.encode(s));

  int refFor(int id) => state.refFor(id);
}

// _round is the region's 8-byte alignment (§6.2).
int _round(int n) => (n + 7) & -8;

// _kindFloor is the minimum byte cost of one element of a counted array body,
// the floor the count sanity check divides by (docs/SPEC-TABLES.md §6.2,
// mirroring ir.TableKindWidth): a pointer or union slot is one LEB, a table
// body two bytes, a scalar its payload width.
int _kindFloor(int k) {
  switch (k) {
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
    case 36:
      return 8;
    case 18:
    case 19:
    case 24:
    case 29:
      return 16;
    case 13:
      return 2;
    default:
      return 1;
  }
}

// _extentKindWidens reports whether a payload under kind decodes exactly into
// a list declared at declared (docs/SPEC-TABLES.md §4), the same kind-pair
// rule as ir.TableKindWidens and extentScan.field.
bool _extentKindWidens(int kind, int declared) {
  switch (declared) {
    case 3:
    case 4:
    case 5:
      return kind >= 2 && kind < declared;
    case 18:
      return kind >= 2 && kind <= 5;
    case 7:
    case 8:
    case 9:
      return kind >= 6 && kind < declared;
    case 19:
      return kind >= 6 && kind <= 9;
    case 11:
      return kind == 10;
  }
  return false;
}

// _MF is one field of the region measure's extent walk, as Go's extentScan
// sees it (internal/tablewire/extent.go, docs/SPEC-TABLES.md §6.2). fieldKind
// is the kind the field's payload must ride under (ir.TableWireFieldKind).
// mode 0 contributes nothing (a pointer slot, a scalar, text), 1 is a
// container (a map, a list, a bounded or enum-keyed array), 2 a by-value table
// whose record is walked. A container's elemKind is the element kind it
// declares (a map's entries are tables), counts marks a map or a list, whose
// count-times-size extent the holder carries at align, keyed marks an
// enum-keyed array whose elements ride behind their key, child is the element,
// by-value or entry table (an index into the spec's structs, -1 none), and uni
// the union (an index into the spec's unions, -1 none). single marks a lone
// by-value union, which the walk reads arm by arm.
final class _MF {
  final int fieldKind;
  final int mode;
  final int elemKind;
  final bool counts;
  final int size;
  final int align;
  final bool keyed;
  final int child;
  final int uni;
  final bool single;
  const _MF(this.fieldKind, this.mode, this.elemKind, this.counts, this.size, this.align, this.keyed, this.child, this.uni,
      this.single);
}

// _ExtentScan is Go's extentScan (internal/tablewire/extent.go) over one
// root's spec: structs[i] maps a field id to its _MF for the i-th table the
// walk can reach, unions[i] maps an arm id to its variant's _MF. at is the
// out-of-line storage the walked bodies carry (docs/SPEC-TABLES.md §6.2): every
// map's and list's aligned count-times-size, reached by value through nested
// tables, element tables, map entries and union arms. A false answer is a
// count the framing cannot hold; a caller refuses.
final class _ExtentScan {
  final List<Map<int, _MF>> structs;
  final List<Map<int, _MF>> unions;
  int at = 0;
  _ExtentScan(this.structs, this.unions);

  // _body reads one length-prefixed payload (extentBody): null, with the
  // cursor at the limit, when the length cannot be held.
  (int, int)? _body(TableWireReader r) {
    final n = r.readLeb();
    if (n == null || n < 0 || n > r.limit - r.pos) {
      r.pos = r.limit;
      return null;
    }
    final start = r.pos;
    r.pos += n;
    return (start, r.pos);
  }

  // record is extentScan.record: a body read to its zero reference, where
  // damage ends the walk of this body and keeps what it counted.
  bool record(TableWireReader r, int st) {
    final fields = structs[st];
    while (true) {
      final ref = r.readLeb();
      if (ref == null || ref == 0) return true;
      final id = r.refToId[ref];
      if (id == null || !r.has(1)) return true;
      final kind = r.readByte();
      final f = fields[id];
      if (f == null) {
        if (!r.skipKind(kind)) return true;
        continue;
      }
      if (kind == 15 && f.single) {
        if (!union(r, f.uni)) return false;
        continue;
      }
      if (kind != 13 && kind != 14 && kind != 16) {
        if (!r.skipKind(kind)) return true;
        continue;
      }
      final b = _body(r);
      if (b == null) return true;
      if (kind != f.fieldKind) continue;
      if (!field(r, f, b.$1, b.$2)) return false;
    }
  }

  // field is extentScan.field over the payload [start, end).
  bool field(TableWireReader p, _MF f, int start, int end) {
    if (f.mode == 0) return true;
    final r = TableWireReader.sub(p, start, end);
    if (f.mode == 2) return record(r, f.child);
    if (!r.has(2)) return true;
    final kind = r.readByte();
    if (kind != f.elemKind && !_extentKindWidens(kind, f.elemKind)) return true;
    final count = r.readLeb();
    if (count == null) return true;
    if (f.counts) {
      if (count < 0 || count > 0x7FFFFFFF) return false;
      if (count > (r.limit - r.pos) ~/ _kindFloor(kind)) return false;
      at = (at + f.align - 1) & -f.align;
      at += count * f.size;
    }
    if (kind != 13 && kind != 15) return true;
    // a count above 2^63 reads negative here; Go's loop runs on to the end of
    // the body, as this one does
    for (var i = 0; (count < 0 || i < count) && r.has(1); i++) {
      if (f.keyed && r.readLeb() == null) return true;
      if (kind == 15) {
        if (f.uni >= 0 && !union(r, f.uni)) return false;
        continue;
      }
      final b = _body(r);
      if (b == null) return true;
      if (f.child >= 0 && !record(TableWireReader.sub(r, b.$1, b.$2), f.child)) return false;
    }
    return true;
  }

  // union is extentScan.union: one arm reference, its kind byte and payload,
  // walked as the variant's field (a nested lone union as a union again).
  bool union(TableWireReader r, int un) {
    final ref = r.readLeb();
    if (ref == null) {
      r.pos = r.limit;
      return true;
    }
    if (ref == 0) return true;
    final id = r.refToId[ref];
    if (id == null || !r.has(1)) {
      r.pos = r.limit;
      return true;
    }
    r.readByte();
    final b = _body(r);
    if (b == null) return true;
    final v = unions[un][id];
    if (v == null) return true;
    if (v.single) return union(TableWireReader.sub(r, b.$1, b.$2), v.uni);
    return field(r, v, b.$1, b.$2);
  }
}

// _signExtend widens a width-byte two's-complement value to 64 bits (§4).
int _signExtend(int raw, int width) {
  final shift = 64 - width * 8;
  return (raw << shift) >> shift;
}

// _lebValid reports whether one payload is well-formed UTF-8 with no zero
// byte among its bytes (§3, §4).
bool _lebValid(Uint8List d, int off, int len) {
  if (off < 0 || len < 0 || off + len > d.length) return false;
  for (var i = 0; i < len; i++) {
    if (d[off + i] == 0) return false;
  }
  try {
    utf8.decode(d.sublist(off, off + len));
    return true;
  } catch (_) {
    return false;
  }
}

// _textBoundary is where a clamp cuts a payload longer than the bound: the last
// whole code point that fits (§3).
int _textBoundary(Uint8List d, int off, int len, int bound) {
  if (len <= bound) return len;
  var cut = bound;
  while (cut > 0 && (d[off + cut] & 0xC0) == 0x80) {
    cut--;
  }
  return cut;
}

// TableWireReader holds the whole buffer and resolves the id table once at
// open (§3): a body names an id by POSITION and the reader dispatches on it.
final class TableWireReader {
  final Uint8List data;
  int pos;
  int limit;
  final Map<int, int> refToId;
  final List<int> nodeTypes;
  final List<Uint8List> nodes;
  final Set<int> knownTypes;
  final List<Object?> decoded;
  TableWireReport report;
  int entryCount = 0;
  bool nodeTableGood = true;
  bool stringBlob = false;
  bool bytesBlob = false;
  TableRetain? retain;
  List<int> path = <int>[];
  Object? rootObj;
  int rootType = 0;

  TableWireReader(this.data, this.limit)
    : pos = 0,
      refToId = <int, int>{},
      nodeTypes = <int>[],
      nodes = <Uint8List>[],
      knownTypes = <int>{},
      decoded = <Object?>[],
      report = TableWireReport();

  // A sub-reader shares the parent's resolved id table, node records and
  // report — a nested body reads the SAME ids (§3) and resolves the SAME
  // numbering (§3.1) — and bounds itself to the body's length.
  TableWireReader.sub(TableWireReader parent, int start, int end)
    : data = parent.data,
      pos = start,
      limit = end,
      refToId = parent.refToId,
      nodeTypes = parent.nodeTypes,
      nodes = parent.nodes,
      knownTypes = parent.knownTypes,
      decoded = parent.decoded,
      report = parent.report,
      entryCount = parent.entryCount,
      nodeTableGood = parent.nodeTableGood,
      stringBlob = parent.stringBlob,
      bytesBlob = parent.bytesBlob,
      retain = parent.retain,
      path = parent.path,
      rootObj = parent.rootObj,
      rootType = parent.rootType;

  // over reads one node-record body against the parent's numbering. The bytes
  // are the record, not a slice of the file, so the cursor starts at 0 (§3.1).
  TableWireReader.over(Uint8List bytes, TableWireReader parent)
    : data = bytes,
      pos = 0,
      limit = bytes.length,
      refToId = parent.refToId,
      nodeTypes = parent.nodeTypes,
      nodes = parent.nodes,
      knownTypes = parent.knownTypes,
      decoded = parent.decoded,
      report = parent.report,
      entryCount = parent.entryCount,
      nodeTableGood = parent.nodeTableGood,
      stringBlob = parent.stringBlob,
      bytesBlob = parent.bytesBlob,
      retain = parent.retain,
      path = parent.path,
      rootObj = parent.rootObj,
      rootType = parent.rootType;

  bool has(int n) => n >= 0 && pos + n <= limit;

  int readByte() {
    final v = data[pos];
    pos++;
    return v;
  }

  // readF32/readF64 read one float payload at an offset the caller has bounded,
  // TESTING THE RAW BITS FIRST (docs/SPEC-TABLES.md §3.3, a plain float's
  // special values, the same rule as packets): a NaN or an
  // infinity never becomes a double. The read is refused as non_finite_float,
  // naming the field, and 0 stands in for the value the load then discards.
  // Every finite float32 widens to a double exactly, so the float rung (kind 10
  // into a float64 field) is the ordinary conversion.
  double readF32(int at, String field) {
    final b = readU32(data, at);
    if ((b & 0x7f800000) == 0x7f800000) {
      report.refuseNonFiniteFloat(field);
      return 0.0;
    }
    return _f32FromBits(b);
  }

  double readF64(int at, String field) {
    final b = readU64(data, at);
    if ((b & 0x7ff0000000000000) == 0x7ff0000000000000) {
      report.refuseNonFiniteFloat(field);
      return 0.0;
    }
    return _f64FromBits(b);
  }

  // readBool reads a bool payload: any nonzero byte reads true, and a byte
  // above 1 is malformed (docs/SPEC-TABLES.md §3, internal/tablewire scalarAt).
  bool readBool() {
    final v = data[pos];
    pos++;
    if (v > 1) report.malformed = true;
    return v != 0;
  }

  // readLeb decodes one canonical LEB128; null on a truncated, over-long or
  // non-minimal encoding (§3). On any of those the cursor does not move, so a
  // nested body that ends short of its own length is seen to do so (§4), the
  // same answer the engine gives when a bad LEB leaves its cursor short.
  int? readLeb() {
    var v = 0;
    var p = pos;
    for (var i = 0; ; i++) {
      if (p >= limit || i >= 10) return null;
      final b = data[p];
      p++;
      if (i == 9 && b > 1) return null;
      v |= (b & 0x7F) << (7 * i);
      if ((b & 0x80) == 0) {
        if (i > 0 && b == 0) return null;
        pos = p;
        return v;
      }
    }
  }

  // skipKind steps over a field of this kind (§3, §4); false is framing damage.
  bool skipKind(int kind) {
    switch (kind) {
      case 1:
      case 2:
      case 6:
      case 20:
      case 25:
        if (!has(1)) return false;
        pos += 1;
        return true;
      case 3:
      case 7:
      case 21:
      case 26:
        if (!has(2)) return false;
        pos += 2;
        return true;
      case 4:
      case 8:
      case 10:
      case 22:
      case 27:
        if (!has(4)) return false;
        pos += 4;
        return true;
      case 5:
      case 9:
      case 11:
      case 23:
      case 28:
      case 36: // a timestamp, eight bytes (§3; ir.TableKindWidth)
        if (!has(8)) return false;
        pos += 8;
        return true;
      case 18:
      case 19:
      case 24:
      case 29:
        if (!has(16)) return false;
        pos += 16;
        return true;
      case 17:
      case 30:
        return readLeb() != null;
      case 12:
      case 13:
      case 14:
      case 16:
      case 31:
      case 32:
      case 33:
        final sl = readLeb();
        if (sl == null) return false;
        if (!has(sl)) return false;
        pos += sl;
        return true;
      case 15:
        final arm = readLeb();
        if (arm == null) return false;
        if (arm == 0) return true;
        if (!has(1)) return false;
        readByte();
        final al = readLeb();
        if (al == null) return false;
        if (!has(al)) return false;
        pos += al;
        return true;
      default:
        return false;
    }
  }

  // mapEntryScan is the KEY SCAN of one map entry body (docs/SPEC-TABLES.md
  // §2.8), the twin of internal/tablewire (*wireReader).mapKey: before the
  // body decodes, it walks the body's field headers on its own cursor,
  // decoding nothing and counting nothing. The low two bits answer 0 when the
  // body frames whole to its terminator, 1 when it does not (the MAP is
  // malformed), and 2 when the LAST occurrence of the key rides a kind the
  // declaration neither is nor widens from (the MAP is a kind mismatch, which
  // outranks damage after it). A string key that is not well-formed text is
  // damage (§2.8). Bit 4 is set when the scan met an occurrence of the key
  // under a kind the declaration WIDENS from, whatever the low bits say: the
  // Go scan sets its widened flag before it reads that occurrence, so a key
  // that widened before the damage still counts the map's one widened
  // (docs/SPEC-TABLES.md §2.8, §4; internal/tablewire mapKey and mapField).
  // Bit 8 is set when the LAST occurrence the scan keeps of the key is a
  // string(N) whose wire length is over the declaration's bound N: KEYS
  // NEVER CLAMP, the entry is dropped whole and counted one clamped
  // (docs/SPEC-TABLES.md §2.8; internal/tabletext MapKeyFits). keySize is
  // the string bound, or -1 for a key that is not a bounded string.
  int mapEntryScan(int keyId, int keyKind, List<int> keyWidens, int keySize) {
    final s = TableWireReader.sub(this, pos, limit);
    var kindBad = false;
    var widened = 0;
    var clamped = 0;
    while (true) {
      final ref = s.readLeb();
      if (ref == null) return (kindBad ? 2 : 1) | widened;
      if (ref == 0) return (kindBad ? 2 : (s.pos == s.limit ? 0 : 1)) | widened | clamped;
      final id = s.refToId[ref];
      if (id == null || !s.has(1)) return (kindBad ? 2 : 1) | widened;
      final kind = s.readByte();
      if (id == keyId) {
        if (kind != keyKind && keyWidens.contains(kind)) {
          widened = 4;
          if (!s.skipKind(kind)) return (kindBad ? 2 : 1) | widened;
          continue;
        }
        kindBad = kind != keyKind;
        if (!kindBad && kind == 12) {
          final n = s.readLeb();
          if (n == null || !s.has(n) || !_lebValid(s.data, s.pos, n)) return 1 | widened;
          clamped = (keySize >= 0 && n > keySize) ? 8 : 0;
          s.pos += n;
          continue;
        }
      }
      if (!s.skipKind(kind)) return (kindBad ? 2 : 1) | widened;
    }
  }

  // resolveBlob turns a kind-17 node index into the blob bytes it names
  // (§3.1): 0 is null; 1 is the ROOT, which a blob can never be, so it is a
  // kind mismatch; records 2.. are the node table's blob records. An index
  // above node_count + 1 is malformed, and a record whose type id this reader
  // cannot name reads null — the unknown was counted once at the node table,
  // not once per pointer (§3.1). recognized is the flag that gates whether
  // this root reaches that blob kind at all. A PLACED record of another kind
  // (a table, or the other blob kind) under this slot is a kind mismatch, as
  // internal/tablewire/decodenodes.go resolveCell counts one when a blob slot
  // names a node whose Blob is nil or whose Kind differs (§3.1, §2.5).
  Uint8List? resolveBlob(int idx, int wantTypeId, bool recognized) {
    if (!nodeTableGood) return null;
    if (idx == 0) return null;
    if (idx == 1) {
      report.kindMismatch++;
      return null;
    }
    final rec = idx - 2;
    if (rec < 0 || rec >= nodes.length) {
      report.malformed = true;
      return null;
    }
    final tid = nodeTypes[rec];
    if (!_placedNode(tid)) return null;
    if (!recognized || tid != wantTypeId) {
      report.kindMismatch++;
      return null;
    }
    return Uint8List.fromList(nodes[rec]);
  }

  // A blob record resolves to ONE object however many slots name it (§3.1):
  // the first resolve stores it in decoded, so a save numbers it once again
  // by identity and two distinct records of equal bytes stay two.
  String? resolveString(int idx) {
    final rec = idx - 2;
    if (rec >= 0 && rec < decoded.length) {
      final have = decoded[rec];
      if (have is String) return have;
    }
    final b = resolveBlob(idx, _kStringBlobId, stringBlob);
    if (b == null) return null;
    final s = utf8.decode(b);
    decoded[rec] = s;
    return s;
  }

  Uint8List? resolveBytes(int idx) {
    final rec = idx - 2;
    if (rec >= 0 && rec < decoded.length) {
      final have = decoded[rec];
      if (have is Uint8List) return have;
    }
    final b = resolveBlob(idx, _kBytesBlobId, bytesBlob);
    if (b != null) decoded[rec] = b;
    return b;
  }

  // _placedNode is true for a node record the node table scan placed: a
  // table this root can name, or a blob of a kind some pointer below the root
  // targets (docs/SPEC-TABLES.md §2.5, §3.1). It is the Dart side of the
  // Inst/Blob test in internal/tablewire/decodenodes.go resolveCell: every
  // other record — counted unknown, or a refused text blob — is an empty node.
  bool _placedNode(int typeId) =>
      knownTypes.contains(typeId) ||
      (typeId == _kStringBlobId && stringBlob) ||
      (typeId == _kBytesBlobId && bytesBlob);

  // resolveTable turns a kind-17 node index into the table it names (§3.1).
  // 0 is null. 1 is the root, and only when the field's target is the root's
  // own type. A record is decoded once and stored before its body is read, so
  // a second slot names the same object. A type this reader cannot place, or
  // a type other than the field's target, is a kind mismatch and reads null.
  Object? resolveTable(int idx, int wantType) {
    if (!nodeTableGood) return null;
    if (idx == 0) return null;
    if (idx == 1) {
      if (wantType != rootType) {
        report.kindMismatch++;
        return null;
      }
      return rootObj;
    }
    final rec = idx - 2;
    if (rec < 0 || rec >= nodes.length) {
      report.malformed = true;
      return null;
    }
    // A RECORD WHOSE TYPE ID THIS READER CANNOT PLACE IS NOT A MISMATCH
    // (docs/SPEC-TABLES.md §3.1, §4): the node table scan counted the unknown
    // once at the record, and every pointer naming it reads null — the same
    // answer internal/tablewire/decodenodes.go resolveCell gives when the
    // node it resolves has neither an instance nor a blob. A PLACED BLOB is
    // not that case (§2.5): a string or bytes record under a root with an
    // edge of that kind carries no table id, yet the scan counted no unknown
    // for it, and resolveCell counts one kind_mismatch for a *T slot naming a
    // node whose Blob is set. Only an unplaced record — an unknown, or a text
    // blob the scan refused as malformed (type 0), which Go leaves empty —
    // reads null silently.
    if (!_placedNode(nodeTypes[rec])) return null;
    if (nodeTypes[rec] != wantType) {
      report.kindMismatch++;
      return null;
    }
    final have = decoded[rec];
    if (have != null) return have;
    final obj = _newNode(wantType);
    if (obj == null) {
      report.kindMismatch++;
      return null;
    }
    decoded[rec] = obj;
    final sub = TableWireReader.over(nodes[rec], this);
    // A node record's body is read to its zero reference, and the reference
    // stops the body: bytes left after it inside the record's own length are
    // kept and do not damage the node (docs/SPEC-TABLES.md §3.1, "the body is
    // an ordinary table body"; internal/tablewire/decodenodes.go leaves
    // records[i].Body after pass two unchecked, unlike a by-value field's L).
    _loadNodeBody(sub, wantType, obj);
    return obj;
  }

  // open resolves the id table from the END of the wire (§3), then walks the
  // root body's framing to its terminator. 0 ok, 1 refused (a form byte this
  // reader does not carry), 2 damaged (a trailer the reader cannot resolve, or
  // a body that ENDS EARLY: a terminator with bytes after it that no field
  // claims), 3 framing damage on the root body (the walk cannot reach a
  // terminator). 3 is §4's ordinary damage on that body and not the file's:
  // the load reads the body tolerantly, the fields before the damage stand and
  // nothing after it does (docs/SPEC-TABLES.md §3.3, §4; the reference is
  // internal/tablewire/decode.go decodeWith and bodyExtent, whose
  // terminated=false falls through to the body read). A measure reads the
  // framing with the reference's extentScan, which treats a walk that cannot
  // reach its terminator as that body's ordinary damage and keeps the bytes
  // before it (internal/tablewire/extent.go extentScan.record), so
  // strict=false answers 0 where strict=true answers 2 or 3.
  int open({bool strict = true}) {
    final damaged = strict ? 3 : 0;
    if (data.length == 0) return 2;
    if (data[0] != 1) return 1;
    if (data.length < 9) return 2;
    final count = readU64(data, data.length - 8);
    if (count < 0 || count > (data.length - 9) ~/ 8) return 2;
    final start = data.length - 8 - count * 8;
    if (start < 1) return 2;
    final seen = <int>{};
    for (var k = 0; k < count; k++) {
      final id = readU64(data, start + k * 8);
      if (!seen.add(id)) return 2;
      refToId[k + 1] = id;
    }
    entryCount = count;
    limit = start;
    pos = 1;
    while (true) {
      final ref = readLeb();
      if (ref == null) return damaged;
      if (ref == 0) {
        if (pos != start) return strict ? 2 : 0;
        return 0;
      }
      if (ref < 1 || ref > entryCount) return damaged;
      if (!has(1)) return damaged;
      final kind = readByte();
      if (!skipKind(kind)) return damaged;
    }
  }

  // scanNodeTable gathers the root body's node table exactly as the reference's
  // internal/tablewire/nodetable.go nodeTableBytes does (docs/SPEC-TABLES.md
  // §3.1, §3.3): the reserved id's LAST well-framed kind-12 occurrence is the
  // table, and it REPLACES the payload an earlier occurrence supplied. A
  // node-table field under any other kind, or with a length past the body, is
  // malformed before a later occurrence is read, and the whole table reads
  // nothing — every pointer in the save is null, one malformed counts (§3.1).
  // Framing damage to the root body elsewhere — an id this reader cannot name,
  // a field it cannot skip — stops the gather and keeps the table found so far,
  // as nodeTableBytes does. Only the gathered payload is scanned, on its own
  // length: a count the scan does not match, a type reference of 0 or above the
  // entry count, or a record length past the payload is malformed and reads
  // nothing (§3.1). A record whose type id is the *string blob stores its
  // bytes; any other type id keeps its index, counts one unknown at the
  // record, and every pointer naming it reads null (§3.1). A string blob that
  // is not well-formed UTF-8 without NUL is malformed and reads null too.
  void scanNodeTable() {
    pos = 1;
    var present = false;
    var tableStart = 0;
    var tableEnd = 0;
    // THE GATHER (nodeTableBytes): each kind-12 occurrence replaces the copy.
    while (true) {
      final ref = readLeb();
      if (ref == null || ref == 0) break;
      final id = refToId[ref];
      if (id == null) break;
      if (!has(1)) break;
      final kind = readByte();
      if (id == _kNodeTableId) {
        present = true;
        if (kind != 12) {
          report.malformed = true;
          nodeTableGood = false;
          return;
        }
        final len = readLeb();
        if (len == null || !has(len)) {
          report.malformed = true;
          nodeTableGood = false;
          return;
        }
        final end = pos + len;
        tableStart = pos;
        tableEnd = end;
        pos = end;
      } else if (!skipKind(kind)) {
        break;
      }
    }
    if (!present) return;
    // THE SCAN (scanNodeRecords over the gathered payload): bound the reader
    // to the payload, so a count or record cannot read past its field.
    final bodyLimit = limit;
    limit = tableEnd;
    pos = tableStart;
    final count = readLeb();
    if (count == null) {
      limit = bodyLimit;
      report.malformed = true;
      nodeTableGood = false;
      return;
    }
    // THE SCAN IS FRAMING ONLY (scanNodeRecords): a record's type is judged
    // after the whole payload has framed, so a table that cannot be read whole
    // counts nothing unknown and resolves nothing.
    var read = 0;
    var framed = true;
    final scanTypes = <int>[];
    final scanBytes = <Uint8List>[];
    while (pos < tableEnd) {
      final typeRef = readLeb();
      if (typeRef == null || typeRef < 1 || typeRef > entryCount) {
        framed = false;
        break;
      }
      final blen = readLeb();
      if (blen == null || blen < 0 || blen > tableEnd - pos) {
        framed = false;
        break;
      }
      scanBytes.add(Uint8List.fromList(data.sublist(pos, pos + blen)));
      pos += blen;
      scanTypes.add(refToId[typeRef] ?? 0);
      read++;
    }
    if (!framed || pos != tableEnd || read != count) {
      report.malformed = true;
      nodeTableGood = false;
    } else {
      for (var i = 0; i < scanTypes.length; i++) {
        final typeId = scanTypes[i];
        final bytes = scanBytes[i];
        if (typeId == _kStringBlobId && stringBlob) {
          if (_lebValid(bytes, 0, bytes.length)) {
            nodeTypes.add(typeId);
          } else {
            report.malformed = true;
            nodeTypes.add(0);
          }
        } else if (typeId == _kBytesBlobId && bytesBlob) {
          nodeTypes.add(typeId);
        } else if (knownTypes.contains(typeId)) {
          nodeTypes.add(typeId);
        } else {
          report.unknown++;
          if (retain != null) report.retainLost++;
          nodeTypes.add(typeId);
        }
        nodes.add(bytes);
        decoded.add(null);
      }
    }
    pos = tableEnd;
    limit = bodyLimit;
    // PASS TWO, before the root body: LOAD IS A SCAN (docs/SPEC-TABLES.md
    // §3.1) and the reference decodes every placed record's body in index
    // order, reachable or not (internal/tablewire/decodenodes.go
    // decodeVariable). A table the scan could not read whole resolves
    // nothing and has no records to decode.
    if (nodeTableGood) decodePlacedNodes();
  }

  // decodePlacedNodes decodes every PLACED table record's body once, in index
  // order, into the storage resolveTable hands out (docs/SPEC-TABLES.md §3.1;
  // internal/tablewire/decodenodes.go decodeVariable PASS TWO). A pointer that
  // later names the record finds it already decoded and counts nothing again.
  // A blob record is complete in the scan (§2.5), and a record whose type id
  // this reader cannot place was counted unknown there and is skipped.
  void decodePlacedNodes() {
    for (var rec = 0; rec < nodes.length; rec++) {
      final typeId = nodeTypes[rec];
      if (typeId == _kStringBlobId || typeId == _kBytesBlobId) continue;
      if (!knownTypes.contains(typeId)) continue;
      if (decoded[rec] != null) continue;
      final obj = _newNode(typeId);
      if (obj == null) continue;
      decoded[rec] = obj;
      final sub = TableWireReader.over(nodes[rec], this);
      _loadNodeBody(sub, typeId, obj);
    }
  }
}

// _kindWidens reports whether a payload kind is a smaller kind on the same
// signed or unsigned ladder, or an f32 read into an f64 (§4). One array whose
// element kind widens counts one widened, not one per element.
bool _kindWidens(int from, int to) {
  return (from >= 2 && from <= 5 && to > from && to <= 5) ||
      (from >= 6 && from <= 9 && to > from && to <= 9) ||
      (from >= 2 && from <= 5 && to == 18) ||
      (from >= 6 && from <= 9 && to == 19) ||
      (from == 10 && to == 11);
}

// _u64Below/_u64Above compare two 64-bit patterns stored as Dart ints (§4).
// Bit 63 sets the mint negative; unsigned order puts that pattern above every
// pattern with the bit clear, and matches signed order when the signs agree.
bool _u64Below(int a, int b) {
  final ah = a < 0;
  final bh = b < 0;
  if (ah != bh) return bh;
  return a < b;
}

bool _u64Above(int a, int b) {
  final ah = a < 0;
  final bh = b < 0;
  if (ah != bh) return ah;
  return a > b;
}

int readU64(Uint8List d, int off) {
  var v = 0;
  for (var i = 0; i < 8; i++) {
    v |= d[off + i] << (8 * i);
  }
  return v;
}

int readU32(Uint8List d, int off) {
  var v = 0;
  for (var i = 0; i < 4; i++) {
    v |= d[off + i] << (8 * i);
  }
  return v;
}

// _f32Bits/_f64Bits render a Dart double as its IEEE 754 bit pattern
// (docs/SPEC-TABLES.md §3, kinds 10 and 11): the value rides as its raw bits,
// little-endian, exactly as the other backends write it.
int _f32Bits(double d) {
  final bd = ByteData(4);
  bd.setFloat32(0, d, Endian.little);
  return bd.getUint32(0, Endian.little);
}

double _f32FromBits(int b) {
  final bd = ByteData(4);
  bd.setUint32(0, b, Endian.little);
  return bd.getFloat32(0, Endian.little);
}

int _f64Bits(double d) {
  final bd = ByteData(8);
  bd.setFloat64(0, d, Endian.little);
  return bd.getUint64(0, Endian.little);
}

double _f64FromBits(int b) {
  final bd = ByteData(8);
  bd.setUint64(0, b, Endian.little);
  return bd.getFloat64(0, Endian.little);
}

// _f32Write/_f64Write are every save's float payload: a NaN or an infinity is
// REFUSED, never written (docs/SPEC-TABLES.md §3.3, a plain float's special
// values: no binary form carries one), and the save throws naming the field,
// as it refuses a data cycle. A finite value rides as its bits.
int _f32Write(double d, String field) {
  if (!d.isFinite) _refuseNonFiniteFloat(field);
  return _f32Bits(d);
}

int _f64Write(double d, String field) {
  if (!d.isFinite) _refuseNonFiniteFloat(field);
  return _f64Bits(d);
}

Never _refuseNonFiniteFloat(String field) => throw StateError(
  'non_finite_float: $field — a NaN or an infinity (docs/SPEC-TABLES.md §3.3)',
);

// _retainedFloatName names a retained unknown float for the non_finite_float
// refusal (docs/SPEC-TABLES.md §3.3, §6.6): the body's table and the id it
// cannot name, as sixteen hex digits.
String _retainedFloatName(String owner, int id) {
  const digits = '0123456789abcdef';
  final hex = List<String>.filled(16, '0');
  var v = id;
  for (var i = 15; i >= 0; i--) {
    hex[i] = digits[v & 0xf];
    v >>>= 4;
  }
  return '$owner.0x${hex.join()}';
}

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
const Set<int> _retainKnown = <int>{80074449652715421, 142922654710085925, 350179810443481718, 912176952613754358, 990229364654832702, 1271228688681418151, 1324453635955533101, 1415871171432867950, 1768712988339112597, 2358437193313989428, 2494154474894420565, 2593226102015223570, 2984383410656091285, 3185847833948484337, 3226873318306908209, 3329809749115557776, 3399866181213769700, 3558250755087846312, 3707351006901076138, 3883575963473318423, 4343163795393112518, 4534679873638431877, 4871457021464447630, 5910805692604981441, 6028620492700434865, 6200723483573899615, 6294288958393922979, 6535842126802938884, 7663561793681298941, 7849182299372448948, 7849183398884077159, 7849185597907333581, 7889067272199050878, 8091808378024623192, 8334276637232054488, 8389190336562899880, 8535675692938965450, 8651444298976115966, 8694226076464086509, 8916005070914257170, 9179190079975030720, 9181103189905877455, -9175110139929313435, -9145686598410475159, -8505234340531936929, -8250835097599916951, -7828083326636031446, -7595037280231051973, -7467029643251279282, -7228825333587644925, -7138431908913683369, -7025667724851298986, -6943525758045472923, -6935561395390217098, -6866282850657997311, -6686572760759554282, -6556890747741999708, -6372330821933150452, -6181000666011064152, -5382489739573192984, -5207116919359778736, -5199487799206016251, -4669986463835551831, -4574933642144545166, -3797883357020869208, -3797881157997612786, -3797880058485984575, -3478843999652550171, -3018575613315307966, -2752528626751932394, -2616738830228483229, -2616737730716855018, -2616736631205226807, -1268998401975805620, -962770241226853237, -292263051557875678, -204370544148493463, -1};

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
void _writeNodeTable(TableWireWriter w) {
  final state = w.state;
  if (state.nodes.isEmpty) return;
  final ref = state.refFor(_kNodeTableId);
  w.putLeb(ref);
  w.putByte(12);
  final body = TableWireWriter(state);
  body.putLeb(state.nodes.length);
  for (var i = 0; i < state.nodes.length; i++) {
    body.putLeb(state.refFor(state.nodeTypes[i]));
    final obj = state.nodeObjs[i];
    if (obj == null) {
      final node = state.nodes[i];
      body.putLeb(node.length);
      body.putBytes(node);
    } else {
      final sub = TableWireWriter(state);
      _writeNodeBody(sub, state.nodeTypes[i], obj);
      body.putLeb(sub.buf.length);
      body.putBytes(sub.buf);
    }
  }
  w.putLeb(body.buf.length);
  w.putBytes(body.buf);
}

void _writeNodeBody(TableWireWriter w, int typeId, Object obj) {
  switch (typeId) {
    case _kTypeTableEntity: tableEntityWireSaveBody(w, obj as TableEntity); break;
    case _kTypeTableStat: tableStatWireSaveBody(w, obj as TableStat); break;
    case _kTypeTableHitEvent: tableHitEventWireSaveBody(w, obj as TableHitEvent); break;
    case _kTypeTableChatEvent: tableChatEventWireSaveBody(w, obj as TableChatEvent); break;
    case _kTypeTablePickupEvent: tablePickupEventWireSaveBody(w, obj as TablePickupEvent); break;
    case _kTypeTableMixed: tableMixedWireSaveBody(w, obj as TableMixed); break;
    default: throw StateError('no node body for type $typeId (docs/SPEC-TABLES.md §3.1)');
  }
}

Object? _newNode(int typeId) {
  switch (typeId) {
    case _kTypeTableEntity: return TableEntity();
    case _kTypeTableStat: return TableStat();
    case _kTypeTableHitEvent: return TableHitEvent();
    case _kTypeTableChatEvent: return TableChatEvent();
    case _kTypeTablePickupEvent: return TablePickupEvent();
    case _kTypeTableMixed: return TableMixed();
    default: return null;
  }
}

void _loadNodeBody(TableWireReader r, int typeId, Object obj) {
  switch (typeId) {
    case _kTypeTableEntity: tableEntityWireLoadBody(r, obj as TableEntity, true); break;
    case _kTypeTableStat: tableStatWireLoadBody(r, obj as TableStat, true); break;
    case _kTypeTableHitEvent: tableHitEventWireLoadBody(r, obj as TableHitEvent, true); break;
    case _kTypeTableChatEvent: tableChatEventWireLoadBody(r, obj as TableChatEvent, true); break;
    case _kTypeTablePickupEvent: tablePickupEventWireLoadBody(r, obj as TablePickupEvent, true); break;
    case _kTypeTableMixed: tableMixedWireLoadBody(r, obj as TableMixed, true); break;
  }
}

// _bytesCompare orders two keys by their UTF-8 bytes (docs/SPEC-TABLES.md §2.8).
int _bytesCompare(List<int> a, List<int> b) {
  final n = a.length < b.length ? a.length : b.length;
  for (var i = 0; i < n; i++) {
    if (a[i] != b[i]) return a[i] < b[i] ? -1 : 1;
  }
  return a.length.compareTo(b.length);
}

// _stableSorted is a copy of a map's entries in ascending key order, ties in
// their stored order (docs/SPEC-TABLES.md §2.8).
List<T> _stableSorted<T>(List<T> l, int Function(T a, T b) cmp) {
  final out = List<T>.from(l);
  for (var i = 1; i < out.length; i++) {
    final x = out[i];
    var j = i - 1;
    while (j >= 0 && cmp(out[j], x) > 0) {
      out[j + 1] = out[j];
      j--;
    }
    out[j + 1] = x;
  }
  return out;
}

// _wideValid reports whether a run of UTF-16 code units is well-formed: every
// high surrogate followed by a low one, no low surrogate alone, no NUL
// (docs/SPEC-TABLES.md §3, kind 33).
bool _wideValid(List<int> u) {
  for (var i = 0; i < u.length; i++) {
    final c = u[i];
    if (c == 0) return false;
    if (c >= 0xD800 && c <= 0xDBFF) {
      if (i + 1 >= u.length || u[i + 1] < 0xDC00 || u[i + 1] > 0xDFFF) return false;
      i++;
    } else if (c >= 0xDC00 && c <= 0xDFFF) {
      return false;
    }
  }
  return true;
}

// _kindWidth is a fixed-width kind's payload size in bytes (docs/SPEC-TABLES.md §3).
int _kindWidth(int kind) {
  switch (kind) {
    case 1:
    case 2:
    case 6:
      return 1;
    case 3:
    case 7:
      return 2;
    case 4:
    case 8:
    case 10:
      return 4;
    case 5:
    case 9:
    case 11:
      return 8;
  }
  return 0;
}

// _putWide writes a raw wide value (kinds 18–29) at its width, two's
// complement, little-endian (docs/SPEC-TABLES.md §3).
void _putWide(TableWireWriter w, BigInt v, int width) {
  var x = v.toUnsigned(width * 8);
  final mask = BigInt.from(0xff);
  for (var i = 0; i < width; i++) {
    w.putByte((x & mask).toInt());
    x = x >> 8;
  }
}

// _getWide reads a raw wide value at its width, sign-extended for a signed
// kind (docs/SPEC-TABLES.md §3). The caller has checked the bytes are there.
BigInt _getWide(TableWireReader r, int width, bool signed) {
  var x = BigInt.zero;
  for (var i = width - 1; i >= 0; i--) {
    x = (x << 8) | BigInt.from(r.data[r.pos + i]);
  }
  r.pos += width;
  return signed ? x.toSigned(width * 8) : x;
}

final class TableEntity {
  int entityId = 0;
  int posX = 0;
  int posY = 0;
  int posZ = 0;
  int yaw = 0;
  int pitch = 0;
  int velX = 0;
  int velY = 0;
  int velZ = 0;
  int health = 0;
  TableWeapon weapon = TableWeapon.none;
  int damage = 0;
  bool moving = false;
  bool firing = false;
}

final class TableStat {
  int statId = 0;
  int delta = 0;
}

final class TableHitEvent {
  int targetId = 0;
  int damage = 0;
  int hitKind = 0;
  bool crit = false;
}

final class TableChatEvent {
  int channel = 0;
  int speaker = 0;
}

final class TablePickupEvent {
  int itemId = 0;
  int amount = 0;
}

final class TableMixed {
  int protocolMagic = 0;
  int sequence = 0;
  int ackSequence = 0;
  int ackBits = 0;
  int sessionId = 0;
  int clientId = 0;
  int nonce = 1;
  int worldTime = 0;
  int frameTick = 0;
  double serverTime = 0.0;
  List<TableEntity> entities = <TableEntity>[];
  List<TableStat> stats = <TableStat>[];
  TableEvent gameEvent = TableEvent();
  List<int> loadout = List<int>.generate(4, (_) => 0);
  String playerName = '';
  String payload = '';
  double aimX = 0.0;
  double aimY = 0.0;
  double aimZ = 0.0;
  double recoil = 0.0;
  double drift = 0.0;
  int wideKey = 0;
  int flux = 0;
  double ping = 0.0;
  int crcHint = 0;
  bool hasExtra = false;
  int extra = 0;
  int idleTicks = 0;
}

final class TableEvent {
  int armTag = 0;
  TableHitEvent hit = TableHitEvent();
  TableChatEvent chat = TableChatEvent();
  TablePickupEvent pickup = TablePickupEvent();
}

void _tableEventSaveArm(TableWireWriter w, TableEvent v) {
  switch (v.armTag) {
    case 1:
      {
        final ref = w.refFor(3707351006901076138);
        final p = w.sub();
        tableHitEventWireSaveBody(p, v.hit);
        w.putLeb(ref);
        w.putByte(13);
        w.putLeb(p.buf.length);
        w.putBytes(p.buf);
      }
      break;
    case 2:
      {
        final ref = w.refFor(-962770241226853237);
        final p = w.sub();
        tableChatEventWireSaveBody(p, v.chat);
        w.putLeb(ref);
        w.putByte(13);
        w.putLeb(p.buf.length);
        w.putBytes(p.buf);
      }
      break;
    case 3:
      {
        final ref = w.refFor(-6943525758045472923);
        final p = w.sub();
        tablePickupEventWireSaveBody(p, v.pickup);
        w.putLeb(ref);
        w.putByte(13);
        w.putLeb(p.buf.length);
        w.putBytes(p.buf);
      }
      break;
    default:
      throw StateError('union TableEvent: tag ${v.armTag} names no arm — save refuses it (docs/SPEC-TABLES.md §5)');
  }
}

bool _tableEventArm1(TableWireReader p, TableEvent v, int kind) {
  final b = TableHitEvent();
  tableHitEventWireLoadBody(p, b, true);
  if (p.pos != p.limit) { p.report.malformed = true; return false; }
  v.hit = b;
  return true;
}

bool _tableEventArm2(TableWireReader p, TableEvent v, int kind) {
  final b = TableChatEvent();
  tableChatEventWireLoadBody(p, b, true);
  if (p.pos != p.limit) { p.report.malformed = true; return false; }
  v.chat = b;
  return true;
}

bool _tableEventArm3(TableWireReader p, TableEvent v, int kind) {
  final b = TablePickupEvent();
  tablePickupEventWireLoadBody(p, b, true);
  if (p.pos != p.limit) { p.report.malformed = true; return false; }
  v.pickup = b;
  return true;
}

bool _tableEventLoadArm(TableWireReader r, TableEvent v) {
  final ref = r.readLeb();
  if (ref == null) { r.report.malformed = true; return false; }
  if (ref == 0) { v.armTag = 0; return true; }
  final armId = r.refToId[ref];
  if (armId == null || !r.has(1)) { r.report.malformed = true; return false; }
  final kind = r.readByte();
  final n = r.readLeb();
  if (n == null || !r.has(n)) { r.report.malformed = true; return false; }
  v.armTag = 0;
  final p = TableWireReader.sub(r, r.pos, r.pos + n);
  switch (armId) {
    case 3707351006901076138:
      if (kind == 13) {
        if (_tableEventArm1(p, v, kind)) v.armTag = 1;
      } else {
        r.report.kindMismatch++;
      }
      break;
    case -962770241226853237:
      if (kind == 13) {
        if (_tableEventArm2(p, v, kind)) v.armTag = 2;
      } else {
        r.report.kindMismatch++;
      }
      break;
    case -6943525758045472923:
      if (kind == 13) {
        if (_tableEventArm3(p, v, kind)) v.armTag = 3;
      } else {
        r.report.kindMismatch++;
      }
      break;
    default:
      r.report.unknown++;
      if (r.retain != null) r.report.retainLost++;
  }
  r.pos += n;
  return true;
}

void _tableEventNumber(WireState s, TableEvent v, Set<Object> open) {
  switch (v.armTag) {
    case 1:
      tableHitEventNumber(s, v.hit, open);
      break;
    case 2:
      tableChatEventNumber(s, v.chat, open);
      break;
    case 3:
      tablePickupEventNumber(s, v.pickup, open);
      break;
  }
}

void tableEntityWireSaveBodyFields(TableWireWriter w, TableEntity v) {
  if (v.entityId != 0) {
    w.putLeb(w.refFor(2593226102015223570));
    w.putByte(7);
    w.putU16(v.entityId);
  }
  if (v.posX != 0) {
    w.putLeb(w.refFor(-3797881157997612786));
    w.putByte(4);
    w.putU32(v.posX);
  }
  if (v.posY != 0) {
    w.putLeb(w.refFor(-3797880058485984575));
    w.putByte(4);
    w.putU32(v.posY);
  }
  if (v.posZ != 0) {
    w.putLeb(w.refFor(-3797883357020869208));
    w.putByte(4);
    w.putU32(v.posZ);
  }
  if (v.yaw != 0) {
    w.putLeb(w.refFor(-5382489739573192984));
    w.putByte(7);
    w.putU16(v.yaw);
  }
  if (v.pitch != 0) {
    w.putLeb(w.refFor(6028620492700434865));
    w.putByte(7);
    w.putU16(v.pitch);
  }
  if (v.velX != 0) {
    w.putLeb(w.refFor(7849183398884077159));
    w.putByte(4);
    w.putU32(v.velX);
  }
  if (v.velY != 0) {
    w.putLeb(w.refFor(7849182299372448948));
    w.putByte(4);
    w.putU32(v.velY);
  }
  if (v.velZ != 0) {
    w.putLeb(w.refFor(7849185597907333581));
    w.putByte(4);
    w.putU32(v.velZ);
  }
  if (v.health != 0) {
    w.putLeb(w.refFor(9181103189905877455));
    w.putByte(4);
    w.putU32(v.health);
  }
  if (v.weapon != TableWeapon.none) {
    w.putLeb(w.refFor(-6866282850657997311));
    w.putByte(30);
    switch (v.weapon) {
      case 1: w.putLeb(w.refFor(-6372330821933150452));
      case 2: w.putLeb(w.refFor(-6935561395390217098));
      case 3: w.putLeb(w.refFor(1768712988339112597));
      case 4: w.putLeb(w.refFor(3185847833948484337));
      case 5: w.putLeb(w.refFor(912176952613754358));
      case 6: w.putLeb(w.refFor(-7467029643251279282));
      case 7: w.putLeb(w.refFor(2494154474894420565));
      case 8: w.putLeb(w.refFor(80074449652715421));
      case 9: w.putLeb(w.refFor(-8505234340531936929));
      case 10: w.putLeb(w.refFor(-8250835097599916951));
      case 11: w.putLeb(w.refFor(-2752528626751932394));
      case 12: w.putLeb(w.refFor(350179810443481718));
      case 13: w.putLeb(w.refFor(3883575963473318423));
      case 14: w.putLeb(w.refFor(3226873318306908209));
      case 15: w.putLeb(w.refFor(2358437193313989428));
      default: w.putLeb(0);
    }
  }
  if (v.damage != 0) {
    w.putLeb(w.refFor(9179190079975030720));
    w.putByte(9);
    w.putU64(v.damage);
  }
  if (v.moving != false) {
    w.putLeb(w.refFor(1271228688681418151));
    w.putByte(1);
    w.putByte(v.moving ? 1 : 0);
  }
  if (v.firing != false) {
    w.putLeb(w.refFor(8535675692938965450));
    w.putByte(1);
    w.putByte(v.firing ? 1 : 0);
  }
  _retainTail(w, v);
}

void tableEntityWireSaveBody(TableWireWriter w, TableEntity v) {
  tableEntityWireSaveBodyFields(w, v);
  w.putByte(0);
}

void tableStatWireSaveBodyFields(TableWireWriter w, TableStat v) {
  if (v.statId != 0) {
    w.putLeb(w.refFor(-9175110139929313435));
    w.putByte(6);
    w.putByte(v.statId);
  }
  if (v.delta != 0) {
    w.putLeb(w.refFor(5910805692604981441));
    w.putByte(4);
    w.putU32(v.delta);
  }
  _retainTail(w, v);
}

void tableStatWireSaveBody(TableWireWriter w, TableStat v) {
  tableStatWireSaveBodyFields(w, v);
  w.putByte(0);
}

void tableHitEventWireSaveBodyFields(TableWireWriter w, TableHitEvent v) {
  if (v.targetId != 0) {
    w.putLeb(w.refFor(-5207116919359778736));
    w.putByte(7);
    w.putU16(v.targetId);
  }
  if (v.damage != 0) {
    w.putLeb(w.refFor(9179190079975030720));
    w.putByte(4);
    w.putU32(v.damage);
  }
  if (v.hitKind != 0) {
    w.putLeb(w.refFor(142922654710085925));
    w.putByte(4);
    w.putU32(v.hitKind);
  }
  if (v.crit != false) {
    w.putLeb(w.refFor(1324453635955533101));
    w.putByte(1);
    w.putByte(v.crit ? 1 : 0);
  }
  _retainTail(w, v);
}

void tableHitEventWireSaveBody(TableWireWriter w, TableHitEvent v) {
  tableHitEventWireSaveBodyFields(w, v);
  w.putByte(0);
}

void tableChatEventWireSaveBodyFields(TableWireWriter w, TableChatEvent v) {
  if (v.channel != 0) {
    w.putLeb(w.refFor(-6556890747741999708));
    w.putByte(4);
    w.putU32(v.channel);
  }
  if (v.speaker != 0) {
    w.putLeb(w.refFor(-292263051557875678));
    w.putByte(7);
    w.putU16(v.speaker);
  }
  _retainTail(w, v);
}

void tableChatEventWireSaveBody(TableWireWriter w, TableChatEvent v) {
  tableChatEventWireSaveBodyFields(w, v);
  w.putByte(0);
}

void tablePickupEventWireSaveBodyFields(TableWireWriter w, TablePickupEvent v) {
  if (v.itemId != 0) {
    w.putLeb(w.refFor(-7025667724851298986));
    w.putByte(7);
    w.putU16(v.itemId);
  }
  if (v.amount != 0) {
    w.putLeb(w.refFor(-9145686598410475159));
    w.putByte(4);
    w.putU32(v.amount);
  }
  _retainTail(w, v);
}

void tablePickupEventWireSaveBody(TableWireWriter w, TablePickupEvent v) {
  tablePickupEventWireSaveBodyFields(w, v);
  w.putByte(0);
}

void tableMixedWireSaveBodyFields(TableWireWriter w, TableMixed v) {
  if (v.protocolMagic != 0) {
    w.putLeb(w.refFor(7663561793681298941));
    w.putByte(7);
    w.putU16(v.protocolMagic);
  }
  if (v.sequence != 0) {
    w.putLeb(w.refFor(-6181000666011064152));
    w.putByte(7);
    w.putU16(v.sequence);
  }
  if (v.ackSequence != 0) {
    w.putLeb(w.refFor(990229364654832702));
    w.putByte(4);
    w.putU32(v.ackSequence);
  }
  if (v.ackBits != 0) {
    w.putLeb(w.refFor(-7228825333587644925));
    w.putByte(8);
    w.putU32(v.ackBits);
  }
  if (v.sessionId != 0) {
    w.putLeb(w.refFor(-5199487799206016251));
    w.putByte(9);
    w.putU64(v.sessionId);
  }
  if (v.clientId != 0) {
    w.putLeb(w.refFor(7889067272199050878));
    w.putByte(8);
    w.putU32(v.clientId);
  }
  if (v.nonce != 1) {
    w.putLeb(w.refFor(8334276637232054488));
    w.putByte(9);
    w.putU64(v.nonce);
  }
  if (v.worldTime != 0) {
    w.putLeb(w.refFor(4534679873638431877));
    w.putByte(5);
    w.putU64(v.worldTime);
  }
  if (v.frameTick != 0) {
    w.putLeb(w.refFor(8916005070914257170));
    w.putByte(9);
    w.putU64(v.frameTick);
  }
  if (v.serverTime != 0 || v.serverTime.isNegative) {
    w.putLeb(w.refFor(4343163795393112518));
    w.putByte(10);
    w.putU32(_f32Write(v.serverTime, 'TableMixed.server_time'));
  }
  if (v.entities.isNotEmpty) {
    final ref = w.refFor(-7828083326636031446);
    final s = w.sub();
    s.putByte(13);
    s.putLeb(v.entities.length);
    for (var i = 0; i < v.entities.length; i++) {
      final e = v.entities[i];
      final b = s.sub();
      b.path = [...w.path, 10, i];
      tableEntityWireSaveBody(b, e);
      s.putLeb(b.buf.length);
      s.putBytes(b.buf);
    }
    w.putLeb(ref);
    w.putByte(14);
    w.putLeb(s.buf.length);
    w.putBytes(s.buf);
  }
  if (v.stats.isNotEmpty) {
    final ref = w.refFor(-1268998401975805620);
    final s = w.sub();
    s.putByte(13);
    s.putLeb(v.stats.length);
    for (var i = 0; i < v.stats.length; i++) {
      final e = v.stats[i];
      final b = s.sub();
      b.path = [...w.path, 11, i];
      tableStatWireSaveBody(b, e);
      s.putLeb(b.buf.length);
      s.putBytes(b.buf);
    }
    w.putLeb(ref);
    w.putByte(14);
    w.putLeb(s.buf.length);
    w.putBytes(s.buf);
  }
  if (v.gameEvent.armTag != 0) {
    w.putLeb(w.refFor(3329809749115557776));
    w.putByte(15);
    _tableEventSaveArm(w, v.gameEvent);
  }
  if (v.loadout.any((e) => e != 0)) {
    final ref = w.refFor(6294288958393922979);
    final s = w.sub();
    s.putByte(6);
    s.putLeb(v.loadout.length);
    for (final e in v.loadout) {
      s.putByte(e);
    }
    w.putLeb(ref);
    w.putByte(14);
    w.putLeb(s.buf.length);
    w.putBytes(s.buf);
  }
  if (v.playerName != '') {
    w.putLeb(w.refFor(1415871171432867950));
    w.putByte(12);
    final b = utf8.encode(v.playerName);
    w.putLeb(b.length);
    w.putBytes(b);
  }
  if (v.payload != '') {
    final ref = w.refFor(-3478843999652550171);
    final b = latin1.encode(v.payload);
    final s = w.sub();
    s.putByte(6);
    s.putLeb(b.length);
    s.putBytes(b);
    w.putLeb(ref);
    w.putByte(14);
    w.putLeb(s.buf.length);
    w.putBytes(s.buf);
  }
  if (v.aimX != 0 || v.aimX.isNegative) {
    w.putLeb(w.refFor(-2616736631205226807));
    w.putByte(10);
    w.putU32(_f32Write(v.aimX, 'TableMixed.aim_x'));
  }
  if (v.aimY != 0 || v.aimY.isNegative) {
    w.putLeb(w.refFor(-2616737730716855018));
    w.putByte(10);
    w.putU32(_f32Write(v.aimY, 'TableMixed.aim_y'));
  }
  if (v.aimZ != 0 || v.aimZ.isNegative) {
    w.putLeb(w.refFor(-2616738830228483229));
    w.putByte(10);
    w.putU32(_f32Write(v.aimZ, 'TableMixed.aim_z'));
  }
  if (v.recoil != 0 || v.recoil.isNegative) {
    w.putLeb(w.refFor(-7138431908913683369));
    w.putByte(10);
    w.putU32(_f32Write(v.recoil, 'TableMixed.recoil'));
  }
  if (v.drift != 0 || v.drift.isNegative) {
    w.putLeb(w.refFor(6535842126802938884));
    w.putByte(11);
    w.putU64(_f64Write(v.drift, 'TableMixed.drift'));
  }
  if (v.wideKey != 0) {
    w.putLeb(w.refFor(-6686572760759554282));
    w.putByte(9);
    w.putU64(v.wideKey);
  }
  if (v.flux != 0) {
    w.putLeb(w.refFor(-3018575613315307966));
    w.putByte(5);
    w.putU64(v.flux);
  }
  if (v.ping != 0 || v.ping.isNegative) {
    w.putLeb(w.refFor(-4669986463835551831));
    w.putByte(10);
    w.putU32(_f32Write(v.ping, 'TableMixed.ping'));
  }
  if (v.crcHint != 0) {
    w.putLeb(w.refFor(6200723483573899615));
    w.putByte(8);
    w.putU32(v.crcHint);
  }
  if (v.hasExtra != false) {
    w.putLeb(w.refFor(-4574933642144545166));
    w.putByte(1);
    w.putByte(v.hasExtra ? 1 : 0);
  }
  if (v.extra != 0) {
    w.putLeb(w.refFor(-204370544148493463));
    w.putByte(4);
    w.putU32(v.extra);
  }
  if (v.idleTicks != 0) {
    w.putLeb(w.refFor(8651444298976115966));
    w.putByte(4);
    w.putU32(v.idleTicks);
  }
  _retainTail(w, v);
}

void tableMixedWireSaveBody(TableWireWriter w, TableMixed v) {
  tableMixedWireSaveBodyFields(w, v);
  w.putByte(0);
}

void tableEntityWireLoadBody(TableWireReader r, TableEntity v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId && nested) { r.report.malformed = true; return; }
    switch (id) {
      case 2593226102015223570:
        {
          var value = v.entityId;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 4095)) { value = 4095; r.report.clamped++; }
          v.entityId = value;
        }
        break;
      case -3797881157997612786:
        {
          var value = v.posX;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < -16383) { value = -16383; r.report.clamped++; }
          if (value > 16383) { value = 16383; r.report.clamped++; }
          v.posX = value;
        }
        break;
      case -3797880058485984575:
        {
          var value = v.posY;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < -16383) { value = -16383; r.report.clamped++; }
          if (value > 16383) { value = 16383; r.report.clamped++; }
          v.posY = value;
        }
        break;
      case -3797883357020869208:
        {
          var value = v.posZ;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < -16383) { value = -16383; r.report.clamped++; }
          if (value > 16383) { value = 16383; r.report.clamped++; }
          v.posZ = value;
        }
        break;
      case -5382489739573192984:
        {
          var value = v.yaw;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 511)) { value = 511; r.report.clamped++; }
          v.yaw = value;
        }
        break;
      case 6028620492700434865:
        {
          var value = v.pitch;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 511)) { value = 511; r.report.clamped++; }
          v.pitch = value;
        }
        break;
      case 7849183398884077159:
        {
          var value = v.velX;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < -2048) { value = -2048; r.report.clamped++; }
          if (value > 2047) { value = 2047; r.report.clamped++; }
          v.velX = value;
        }
        break;
      case 7849182299372448948:
        {
          var value = v.velY;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < -2048) { value = -2048; r.report.clamped++; }
          if (value > 2047) { value = 2047; r.report.clamped++; }
          v.velY = value;
        }
        break;
      case 7849185597907333581:
        {
          var value = v.velZ;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < -2048) { value = -2048; r.report.clamped++; }
          if (value > 2047) { value = 2047; r.report.clamped++; }
          v.velZ = value;
        }
        break;
      case 9181103189905877455:
        {
          var value = v.health;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < 0) { value = 0; r.report.clamped++; }
          if (value > 1000) { value = 1000; r.report.clamped++; }
          v.health = value;
        }
        break;
      case -6866282850657997311:
        if (kind != 30) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        {
          final ref2 = r.readLeb();
          if (ref2 == null) { r.report.malformed = true; return; }
          if (ref2 == 0) { v.weapon = TableWeapon.none; break; }
          final vid = r.refToId[ref2];
          if (vid == null) { r.report.malformed = true; return; }
          switch (vid) {
            case -6372330821933150452: v.weapon = TableWeapon.fists;
            case -6935561395390217098: v.weapon = TableWeapon.pistol;
            case 1768712988339112597: v.weapon = TableWeapon.shotgun;
            case 3185847833948484337: v.weapon = TableWeapon.rifle;
            case 912176952613754358: v.weapon = TableWeapon.sniper;
            case -7467029643251279282: v.weapon = TableWeapon.smg;
            case 2494154474894420565: v.weapon = TableWeapon.rocket;
            case 80074449652715421: v.weapon = TableWeapon.grenade;
            case -8505234340531936929: v.weapon = TableWeapon.plasma;
            case -8250835097599916951: v.weapon = TableWeapon.railgun;
            case -2752528626751932394: v.weapon = TableWeapon.flamer;
            case 350179810443481718: v.weapon = TableWeapon.mine;
            case 3883575963473318423: v.weapon = TableWeapon.turret;
            case 3226873318306908209: v.weapon = TableWeapon.drone;
            case 2358437193313989428: v.weapon = TableWeapon.repair;
            default: v.weapon = TableWeapon.none; r.report.unknown++; if (r.retain != null) r.report.retainLost++;
          }
        }
        break;
      case 9179190079975030720:
        {
          var value = v.damage;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              r.report.widened++;
              break;
            case 8:
              if (!r.has(4)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              raw |= r.readByte() << 16;
              raw |= r.readByte() << 24;
              value = raw;
              r.report.widened++;
              break;
            case 9:
              if (!r.has(8)) { r.report.malformed = true; return; }
              value = readU64(r.data, r.pos); r.pos += 8;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          v.damage = value;
        }
        break;
      case 1271228688681418151:
        if (kind != 1) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        if (!r.has(1)) { r.report.malformed = true; return; }
        v.moving = r.readBool();
        break;
      case 8535675692938965450:
        if (kind != 1) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        if (!r.has(1)) { r.report.malformed = true; return; }
        v.firing = r.readBool();
        break;
      default:
        r.report.unknown++;
        if (r.retain != null) {
          final start = r.pos;
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
          if ((kind == 10 && (readU32(r.data, start) & 0x7f800000) == 0x7f800000) ||
              (kind == 11 && (readU64(r.data, start) & 0x7ff0000000000000) == 0x7ff0000000000000)) {
            r.report.refuseNonFiniteFloat(_retainedFloatName('TableEntity', id));
          } else if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(v, List<int>.from(r.path), id, kind, resolved);
              if (r.retain!.used + rec.cost() > r.retain!.capacity) {
                r.report.retainLost++;
              } else {
                r.retain!.records.add(rec);
                r.retain!.used += rec.cost();
                r.report.retained++;
              }
            }
          }
        } else {
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
        }
    }
  }
}

void tableStatWireLoadBody(TableWireReader r, TableStat v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId && nested) { r.report.malformed = true; return; }
    switch (id) {
      case -9175110139929313435:
        {
          var value = v.statId;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              value = r.readByte();
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 255)) { value = 255; r.report.clamped++; }
          v.statId = value;
        }
        break;
      case 5910805692604981441:
        {
          var value = v.delta;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < -512) { value = -512; r.report.clamped++; }
          if (value > 511) { value = 511; r.report.clamped++; }
          v.delta = value;
        }
        break;
      default:
        r.report.unknown++;
        if (r.retain != null) {
          final start = r.pos;
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
          if ((kind == 10 && (readU32(r.data, start) & 0x7f800000) == 0x7f800000) ||
              (kind == 11 && (readU64(r.data, start) & 0x7ff0000000000000) == 0x7ff0000000000000)) {
            r.report.refuseNonFiniteFloat(_retainedFloatName('TableStat', id));
          } else if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(v, List<int>.from(r.path), id, kind, resolved);
              if (r.retain!.used + rec.cost() > r.retain!.capacity) {
                r.report.retainLost++;
              } else {
                r.retain!.records.add(rec);
                r.retain!.used += rec.cost();
                r.report.retained++;
              }
            }
          }
        } else {
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
        }
    }
  }
}

void tableHitEventWireLoadBody(TableWireReader r, TableHitEvent v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId && nested) { r.report.malformed = true; return; }
    switch (id) {
      case -5207116919359778736:
        {
          var value = v.targetId;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 4095)) { value = 4095; r.report.clamped++; }
          v.targetId = value;
        }
        break;
      case 9179190079975030720:
        {
          var value = v.damage;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < 0) { value = 0; r.report.clamped++; }
          if (value > 4095) { value = 4095; r.report.clamped++; }
          v.damage = value;
        }
        break;
      case 142922654710085925:
        {
          var value = v.hitKind;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < 0) { value = 0; r.report.clamped++; }
          if (value > 7) { value = 7; r.report.clamped++; }
          v.hitKind = value;
        }
        break;
      case 1324453635955533101:
        if (kind != 1) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        if (!r.has(1)) { r.report.malformed = true; return; }
        v.crit = r.readBool();
        break;
      default:
        r.report.unknown++;
        if (r.retain != null) {
          final start = r.pos;
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
          if ((kind == 10 && (readU32(r.data, start) & 0x7f800000) == 0x7f800000) ||
              (kind == 11 && (readU64(r.data, start) & 0x7ff0000000000000) == 0x7ff0000000000000)) {
            r.report.refuseNonFiniteFloat(_retainedFloatName('TableHitEvent', id));
          } else if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(v, List<int>.from(r.path), id, kind, resolved);
              if (r.retain!.used + rec.cost() > r.retain!.capacity) {
                r.report.retainLost++;
              } else {
                r.retain!.records.add(rec);
                r.retain!.used += rec.cost();
                r.report.retained++;
              }
            }
          }
        } else {
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
        }
    }
  }
}

void tableChatEventWireLoadBody(TableWireReader r, TableChatEvent v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId && nested) { r.report.malformed = true; return; }
    switch (id) {
      case -6556890747741999708:
        {
          var value = v.channel;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < 0) { value = 0; r.report.clamped++; }
          if (value > 3) { value = 3; r.report.clamped++; }
          v.channel = value;
        }
        break;
      case -292263051557875678:
        {
          var value = v.speaker;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 4095)) { value = 4095; r.report.clamped++; }
          v.speaker = value;
        }
        break;
      default:
        r.report.unknown++;
        if (r.retain != null) {
          final start = r.pos;
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
          if ((kind == 10 && (readU32(r.data, start) & 0x7f800000) == 0x7f800000) ||
              (kind == 11 && (readU64(r.data, start) & 0x7ff0000000000000) == 0x7ff0000000000000)) {
            r.report.refuseNonFiniteFloat(_retainedFloatName('TableChatEvent', id));
          } else if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(v, List<int>.from(r.path), id, kind, resolved);
              if (r.retain!.used + rec.cost() > r.retain!.capacity) {
                r.report.retainLost++;
              } else {
                r.retain!.records.add(rec);
                r.retain!.used += rec.cost();
                r.report.retained++;
              }
            }
          }
        } else {
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
        }
    }
  }
}

void tablePickupEventWireLoadBody(TableWireReader r, TablePickupEvent v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId && nested) { r.report.malformed = true; return; }
    switch (id) {
      case -7025667724851298986:
        {
          var value = v.itemId;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 1023)) { value = 1023; r.report.clamped++; }
          v.itemId = value;
        }
        break;
      case -9145686598410475159:
        {
          var value = v.amount;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < 0) { value = 0; r.report.clamped++; }
          if (value > 255) { value = 255; r.report.clamped++; }
          v.amount = value;
        }
        break;
      default:
        r.report.unknown++;
        if (r.retain != null) {
          final start = r.pos;
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
          if ((kind == 10 && (readU32(r.data, start) & 0x7f800000) == 0x7f800000) ||
              (kind == 11 && (readU64(r.data, start) & 0x7ff0000000000000) == 0x7ff0000000000000)) {
            r.report.refuseNonFiniteFloat(_retainedFloatName('TablePickupEvent', id));
          } else if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(v, List<int>.from(r.path), id, kind, resolved);
              if (r.retain!.used + rec.cost() > r.retain!.capacity) {
                r.report.retainLost++;
              } else {
                r.retain!.records.add(rec);
                r.retain!.used += rec.cost();
                r.report.retained++;
              }
            }
          }
        } else {
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
        }
    }
  }
}

bool _tableMixedLoadoutFixed(TableWireReader p, TableMixed v, int elemKind, int count) {
  if (elemKind != 6) { p.report.kindMismatch++; return true; }
  final out = <int>[];
  var keep = count;
  if (keep > 4 || keep < 0) { keep = 4; p.report.clamped++; }
  for (var i = 0; i < keep; i++) {
    if (!p.has(1)) { p.report.malformed = true; break; }
    var raw = 0;
    for (var b = 0; b < 1; b++) { raw |= p.readByte() << (8 * b); }
    out.add(raw);
  }
  final slots = v.loadout;
  for (var i = 0; i < out.length && i < 4; i++) {
    slots[i] = out[i];
  }
  return true;
}

void tableMixedWireLoadBody(TableWireReader r, TableMixed v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId && nested) { r.report.malformed = true; return; }
    switch (id) {
      case 7663561793681298941:
        {
          var value = v.protocolMagic;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          v.protocolMagic = value;
        }
        break;
      case -6181000666011064152:
        {
          var value = v.sequence;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 65535)) { value = 65535; r.report.clamped++; }
          v.sequence = value;
        }
        break;
      case 990229364654832702:
        {
          var value = v.ackSequence;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < 0) { value = 0; r.report.clamped++; }
          if (value > 65535) { value = 65535; r.report.clamped++; }
          v.ackSequence = value;
        }
        break;
      case -7228825333587644925:
        {
          var value = v.ackBits;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              r.report.widened++;
              break;
            case 8:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = readU32(r.data, r.pos); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 4294967295)) { value = 4294967295; r.report.clamped++; }
          v.ackBits = value;
        }
        break;
      case -5199487799206016251:
        {
          var value = v.sessionId;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              r.report.widened++;
              break;
            case 8:
              if (!r.has(4)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              raw |= r.readByte() << 16;
              raw |= r.readByte() << 24;
              value = raw;
              r.report.widened++;
              break;
            case 9:
              if (!r.has(8)) { r.report.malformed = true; return; }
              value = readU64(r.data, r.pos); r.pos += 8;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          v.sessionId = value;
        }
        break;
      case 7889067272199050878:
        {
          var value = v.clientId;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              r.report.widened++;
              break;
            case 8:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = readU32(r.data, r.pos); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          v.clientId = value;
        }
        break;
      case 8334276637232054488:
        {
          var value = v.nonce;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              r.report.widened++;
              break;
            case 8:
              if (!r.has(4)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              raw |= r.readByte() << 16;
              raw |= r.readByte() << 24;
              value = raw;
              r.report.widened++;
              break;
            case 9:
              if (!r.has(8)) { r.report.malformed = true; return; }
              value = readU64(r.data, r.pos); r.pos += 8;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Below(value, 1)) { value = 1; r.report.clamped++; }
          if (_u64Above(value, 9223372036854775807)) { value = 9223372036854775807; r.report.clamped++; }
          v.nonce = value;
        }
        break;
      case 4534679873638431877:
        {
          var value = v.worldTime;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              raw |= r.readByte() << 16;
              raw |= r.readByte() << 24;
              value = _signExtend(raw, 4);
              r.report.widened++;
              break;
            case 5:
              if (!r.has(8)) { r.report.malformed = true; return; }
              value = readU64(r.data, r.pos); r.pos += 8;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < -1000000000000) { value = -1000000000000; r.report.clamped++; }
          if (value > 1000000000000) { value = 1000000000000; r.report.clamped++; }
          v.worldTime = value;
        }
        break;
      case 8916005070914257170:
        {
          var value = v.frameTick;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              r.report.widened++;
              break;
            case 8:
              if (!r.has(4)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              raw |= r.readByte() << 16;
              raw |= r.readByte() << 24;
              value = raw;
              r.report.widened++;
              break;
            case 9:
              if (!r.has(8)) { r.report.malformed = true; return; }
              value = readU64(r.data, r.pos); r.pos += 8;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 281474976710655)) { value = 281474976710655; r.report.clamped++; }
          v.frameTick = value;
        }
        break;
      case 4343163795393112518:
        {
          var d = v.serverTime;
          switch (kind) {
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = r.readF32(r.pos, 'TableMixed.server_time'); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (r.report.nonFiniteFloat.isNotEmpty) return;
          if (d < 0) { d = 0; r.report.clamped++; }
          if (d > 65535) { d = 65535; r.report.clamped++; }
          if (d == 0) d = 0.0;
          v.serverTime = d;
        }
        break;
      case -7828083326636031446:
        if (kind != 14) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        {
          final len = r.readLeb();
          if (len == null || !r.has(len)) { r.report.malformed = true; return; }
          final end = r.pos + len;
          if (end - r.pos < 2) { r.pos = end; break; }
          final elemKind = r.readByte();
          final count = r.readLeb();
          if (count == null) { r.report.malformed = true; r.pos = end; break; }
          if (elemKind != 13) { r.report.kindMismatch++; r.pos = end; break; }
          v.entities = <TableEntity>[];
          var keep = count;
          if (keep < 0 || keep > 8) { keep = 8; r.report.clamped++; }
          final arr = TableWireReader.sub(r, r.pos, end < r.pos ? r.pos : end);
          for (var i = 0; i < keep; i++) {
            final elen = arr.readLeb();
            if (elen == null || !arr.has(elen)) { r.report.malformed = true; break; }
            var e = TableEntity();
            final sub = TableWireReader.sub(arr, arr.pos, arr.pos + elen);
            sub.path = [...r.path, 10, i];
            tableEntityWireLoadBody(sub, e, true);
            if (sub.pos != sub.limit) { r.report.malformed = true; e = TableEntity(); }
            arr.pos = arr.pos + elen;
            v.entities.add(e);
          }
          r.pos = end;
        }
        break;
      case -1268998401975805620:
        if (kind != 14) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        {
          final len = r.readLeb();
          if (len == null || !r.has(len)) { r.report.malformed = true; return; }
          final end = r.pos + len;
          if (end - r.pos < 2) { r.pos = end; break; }
          final elemKind = r.readByte();
          final count = r.readLeb();
          if (count == null) { r.report.malformed = true; r.pos = end; break; }
          if (elemKind != 13) { r.report.kindMismatch++; r.pos = end; break; }
          v.stats = <TableStat>[];
          var keep = count;
          if (keep < 0 || keep > 80) { keep = 80; r.report.clamped++; }
          final arr = TableWireReader.sub(r, r.pos, end < r.pos ? r.pos : end);
          for (var i = 0; i < keep; i++) {
            final elen = arr.readLeb();
            if (elen == null || !arr.has(elen)) { r.report.malformed = true; break; }
            var e = TableStat();
            final sub = TableWireReader.sub(arr, arr.pos, arr.pos + elen);
            sub.path = [...r.path, 11, i];
            tableStatWireLoadBody(sub, e, true);
            if (sub.pos != sub.limit) { r.report.malformed = true; e = TableStat(); }
            arr.pos = arr.pos + elen;
            v.stats.add(e);
          }
          r.pos = end;
        }
        break;
      case 3329809749115557776:
        if (kind != 15) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        if (!_tableEventLoadArm(r, v.gameEvent)) { r.report.malformed = true; return; }
        break;
      case 6294288958393922979:
        if (kind != 14) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        {
          final len = r.readLeb();
          if (len == null || !r.has(len)) { r.report.malformed = true; return; }
          final end = r.pos + len;
          if (end - r.pos < 2) { r.pos = end; break; }
          final elemKind = r.readByte();
          final count = r.readLeb();
          if (count == null) { r.report.malformed = true; r.pos = end; break; }
          _tableMixedLoadoutFixed(TableWireReader.sub(r, r.pos, end < r.pos ? r.pos : end), v, elemKind, count);
          r.pos = end;
        }
        break;
      case 1415871171432867950:
        if (kind != 12) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        {
          final len = r.readLeb();
          if (len == null || !r.has(len)) { r.report.malformed = true; return; }
          if (!_lebValid(r.data, r.pos, len)) { r.report.malformed = true; v.playerName = ''; r.pos += len; break; }
          final keep = _textBoundary(r.data, r.pos, len, 15);
          if (keep != len) r.report.clamped++;
          v.playerName = utf8.decode(r.data.sublist(r.pos, r.pos + keep));
          r.pos += len;
        }
        break;
      case -3478843999652550171:
        if (kind != 14) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        {
          final len = r.readLeb();
          if (len == null || !r.has(len)) { r.report.malformed = true; return; }
          final end = r.pos + len;
          if (end - r.pos < 2) { r.pos = end; break; }
          final elemKind = r.readByte();
          final count = r.readLeb();
          if (count == null) { r.report.malformed = true; r.pos = end; break; }
          if (elemKind != 6) { r.report.kindMismatch++; r.pos = end; break; }
          var keep = count;
          if (keep > 16 || keep < 0) { keep = 16; r.report.clamped++; }
          final arr = TableWireReader.sub(r, r.pos, end < r.pos ? r.pos : end);
          var take = keep;
          if (take > arr.limit - arr.pos) take = arr.limit - arr.pos;
          if (take < keep) r.report.malformed = true;
          v.payload = latin1.decode(arr.data.sublist(arr.pos, arr.pos + take));
          r.pos = end;
        }
        break;
      case -2616736631205226807:
        {
          var d = v.aimX;
          switch (kind) {
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = r.readF32(r.pos, 'TableMixed.aim_x'); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (r.report.nonFiniteFloat.isNotEmpty) return;
          if (d < -1) { d = -1; r.report.clamped++; }
          if (d > 1) { d = 1; r.report.clamped++; }
          if (d == 0) d = 0.0;
          v.aimX = d;
        }
        break;
      case -2616737730716855018:
        {
          var d = v.aimY;
          switch (kind) {
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = r.readF32(r.pos, 'TableMixed.aim_y'); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (r.report.nonFiniteFloat.isNotEmpty) return;
          if (d < -1) { d = -1; r.report.clamped++; }
          if (d > 1) { d = 1; r.report.clamped++; }
          if (d == 0) d = 0.0;
          v.aimY = d;
        }
        break;
      case -2616738830228483229:
        {
          var d = v.aimZ;
          switch (kind) {
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = r.readF32(r.pos, 'TableMixed.aim_z'); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (r.report.nonFiniteFloat.isNotEmpty) return;
          if (d < -1) { d = -1; r.report.clamped++; }
          if (d > 1) { d = 1; r.report.clamped++; }
          if (d == 0) d = 0.0;
          v.aimZ = d;
        }
        break;
      case -7138431908913683369:
        {
          var d = v.recoil;
          switch (kind) {
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = r.readF32(r.pos, 'TableMixed.recoil'); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (r.report.nonFiniteFloat.isNotEmpty) return;
          v.recoil = d;
        }
        break;
      case 6535842126802938884:
        {
          var d = v.drift;
          switch (kind) {
            case 11:
              if (!r.has(8)) { r.report.malformed = true; return; }
              d = r.readF64(r.pos, 'TableMixed.drift'); r.pos += 8;
              break;
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = r.readF32(r.pos, 'TableMixed.drift'); r.pos += 4;
              r.report.widened++;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (r.report.nonFiniteFloat.isNotEmpty) return;
          v.drift = d;
        }
        break;
      case -6686572760759554282:
        {
          var value = v.wideKey;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              r.report.widened++;
              break;
            case 8:
              if (!r.has(4)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              raw |= r.readByte() << 16;
              raw |= r.readByte() << 24;
              value = raw;
              r.report.widened++;
              break;
            case 9:
              if (!r.has(8)) { r.report.malformed = true; return; }
              value = readU64(r.data, r.pos); r.pos += 8;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          v.wideKey = value;
        }
        break;
      case -3018575613315307966:
        {
          var value = v.flux;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              raw |= r.readByte() << 16;
              raw |= r.readByte() << 24;
              value = _signExtend(raw, 4);
              r.report.widened++;
              break;
            case 5:
              if (!r.has(8)) { r.report.malformed = true; return; }
              value = readU64(r.data, r.pos); r.pos += 8;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < -1000000000000000000) { value = -1000000000000000000; r.report.clamped++; }
          if (value > 1000000000000000000) { value = 1000000000000000000; r.report.clamped++; }
          v.flux = value;
        }
        break;
      case -4669986463835551831:
        {
          var d = v.ping;
          switch (kind) {
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = r.readF32(r.pos, 'TableMixed.ping'); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (r.report.nonFiniteFloat.isNotEmpty) return;
          if (d < 0) { d = 0; r.report.clamped++; }
          if (d > 250) { d = 250; r.report.clamped++; }
          if (d == 0) d = 0.0;
          v.ping = d;
        }
        break;
      case 6200723483573899615:
        {
          var value = v.crcHint;
          switch (kind) {
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = raw;
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = raw;
              r.report.widened++;
              break;
            case 8:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = readU32(r.data, r.pos); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (_u64Above(value, 16777215)) { value = 16777215; r.report.clamped++; }
          v.crcHint = value;
        }
        break;
      case -4574933642144545166:
        if (kind != 1) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        if (!r.has(1)) { r.report.malformed = true; return; }
        v.hasExtra = r.readBool();
        break;
      case -204370544148493463:
        {
          var value = v.extra;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < 0) { value = 0; r.report.clamped++; }
          if (value > 255) { value = 255; r.report.clamped++; }
          v.extra = value;
        }
        break;
      case 8651444298976115966:
        {
          var value = v.idleTicks;
          switch (kind) {
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              value = _signExtend(raw, 1);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              var raw = 0;
              raw |= r.readByte() << 0;
              raw |= r.readByte() << 8;
              value = _signExtend(raw, 2);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value < 0) { value = 0; r.report.clamped++; }
          if (value > 15) { value = 15; r.report.clamped++; }
          v.idleTicks = value;
        }
        break;
      default:
        r.report.unknown++;
        if (r.retain != null) {
          final start = r.pos;
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
          if ((kind == 10 && (readU32(r.data, start) & 0x7f800000) == 0x7f800000) ||
              (kind == 11 && (readU64(r.data, start) & 0x7ff0000000000000) == 0x7ff0000000000000)) {
            r.report.refuseNonFiniteFloat(_retainedFloatName('TableMixed', id));
          } else if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(v, List<int>.from(r.path), id, kind, resolved);
              if (r.retain!.used + rec.cost() > r.retain!.capacity) {
                r.report.retainLost++;
              } else {
                r.retain!.records.add(rec);
                r.retain!.used += rec.cost();
                r.report.retained++;
              }
            }
          }
        } else {
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
        }
    }
  }
}

void tableEntityNumber(WireState s, TableEntity v, Set<Object> open) {
}

void tableStatNumber(WireState s, TableStat v, Set<Object> open) {
}

void tableHitEventNumber(WireState s, TableHitEvent v, Set<Object> open) {
}

void tableChatEventNumber(WireState s, TableChatEvent v, Set<Object> open) {
}

void tablePickupEventNumber(WireState s, TablePickupEvent v, Set<Object> open) {
}

void tableMixedNumber(WireState s, TableMixed v, Set<Object> open) {
  for (final e in v.entities) {
    tableEntityNumber(s, e, open);
  }
  for (final e in v.stats) {
    tableStatNumber(s, e, open);
  }
  _tableEventNumber(s, v.gameEvent, open);
}

Uint8List tableEntityWireSave(TableEntity v) {
  final state = WireState()..refuseSharedBlob = true;
  state.indexRoot(v);
  final open = <Object>{v};
  tableEntityNumber(state, v, open);
  final w = TableWireWriter(state);
  w.putByte(1);
  tableEntityWireSaveBodyFields(w, v);
  _writeNodeTable(w);
  w.putByte(0);
  for (final id in state.ids) { w.putU64(id); }
  w.putU64(state.ids.length);
  return Uint8List.fromList(w.buf);
}

TableEntity tableEntityWireLoad(Uint8List data, [TableWireReport? report]) {
  final v = TableEntity();
  final r = TableWireReader(data, data.length);
  r.stringBlob = false;
  r.bytesBlob = false;
  r.path = [1];
  r.rootObj = v;
  r.rootType = _kTypeTableEntity;
  if (report != null) r.report = report;
  r.report.nonFiniteFloat = '';
  final verdict = r.open();
  if (verdict == 1) { r.report.refused = true; return v; }
  if (verdict == 2) { r.report.malformed = true; return v; }
  r.pos = 1;
  tableEntityWireLoadBody(r, v, false);
  if (r.report.nonFiniteFloat.isNotEmpty) return TableEntity();
  return v;
}

const _measureStructsTableEntity = <Map<int, _MF>>[
  // TableEntity
  {
    2593226102015223570: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
    -3797881157997612786: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    -3797880058485984575: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    -3797883357020869208: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    -5382489739573192984: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
    6028620492700434865: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
    7849183398884077159: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    7849182299372448948: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    7849185597907333581: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    9181103189905877455: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    -6866282850657997311: _MF(30, 0, 0, false, 0, 1, false, -1, -1, false),
    9179190079975030720: _MF(9, 0, 0, false, 0, 1, false, -1, -1, false),
    1271228688681418151: _MF(1, 0, 0, false, 0, 1, false, -1, -1, false),
    8535675692938965450: _MF(1, 0, 0, false, 0, 1, false, -1, -1, false),
  },
];

const _measureUnionsTableEntity = <Map<int, _MF>>[
];

int tableEntityWireMeasure(Uint8List data) {
  final r = TableWireReader(data, data.length);
  if (data.length < 9 || data[0] != 1) return 0;
  final idCount = readU64(data, data.length - 8);
  if (idCount < 0 || idCount > (data.length - 9) ~/ 8) return 0;
  final idStart = data.length - 8 - idCount * 8;
  if (idStart < 1) return 0;
  final idSeen = <int>{};
  for (var k = 0; k < idCount; k++) {
    final id = readU64(data, idStart + k * 8);
    if (!idSeen.add(id)) return 0;
    r.refToId[k + 1] = id;
  }
  r.entryCount = idCount;
  r.limit = idStart;
  r.pos = 1;
  // the root body and every node record walk Go's extentScan rule (§6.2):
  // map entries, list and array elements, nested tables and union arms
  final rootScan = _ExtentScan(_measureStructsTableEntity, _measureUnionsTableEntity);
  if (!rootScan.record(r, 0)) return -1;
  var total = _round(_round(64) + rootScan.at);
  final nodeSpec = <int, List<int>>{
  };
  r.pos = 1;
  var seen = false;
  var unframed = false;
  var pstart = 0, pend = 0;
  while (true) {
    final ref = r.readLeb();
    if (ref == null || ref == 0) break;
    final id = r.refToId[ref];
    if (id == null || !r.has(1)) break;
    final kind = r.readByte();
    if (id == _kNodeTableId) {
      seen = true;
      if (kind != 12) { unframed = true; continue; }
      final len = r.readLeb();
      if (len == null) { unframed = true; continue; }
      final end = r.pos + len;
      if (end > r.limit) { unframed = true; continue; }
      pstart = r.pos;
      pend = end;
      r.pos = end;
    } else {
      if (!r.skipKind(kind)) break;
    }
  }
  if (unframed) return 0;
  var records = 0;
  var nodeTotal = 0;
  if (seen) {
    r.pos = pstart;
    final cnt = r.readLeb();
    if (cnt == null) return 0;
    var read = 0;
    while (r.pos < pend) {
      final typeRef = r.readLeb();
      if (typeRef == null || typeRef < 1 || typeRef > r.entryCount) return 0;
      final blen = r.readLeb();
      if (blen == null || blen < 0 || blen > pend - r.pos) return 0;
      final tid = r.refToId[typeRef] ?? 0;
      final bodyStart = r.pos;
      final sn = nodeSpec[tid];
      if (sn != null) {
        final over = TableWireReader.over(r.data.buffer.asUint8List(r.data.offsetInBytes + bodyStart, blen), r);
        final nodeScan = _ExtentScan(_measureStructsTableEntity, _measureUnionsTableEntity);
        if (!nodeScan.record(over, sn[1])) return -1;
        nodeTotal += _round(_round(sn[0]) + nodeScan.at);
      } else if (tid == _kBytesBlobId) {
      } else if (tid == _kStringBlobId) {
      }
      r.pos += blen;
      records++;
      read++;
    }
    if (read != cnt) return 0;
  }
  return total + nodeTotal + (records + 1) * 16;
}

TableEntity tableEntityWireLoadRetain(Uint8List data, TableRetain retain, TableWireReport report) {
  retain.reset();
  final v = TableEntity();
  final r = TableWireReader(data, data.length);
  r.report = report;
  r.report.nonFiniteFloat = '';
  r.stringBlob = false;
  r.retain = retain;
  r.path = [1];
  r.rootObj = v;
  r.rootType = _kTypeTableEntity;
  final verdict = r.open();
  if (verdict == 1) { r.report.refused = true; return v; }
  if (verdict == 2) { r.report.malformed = true; return v; }
  r.scanNodeTable();
  r.pos = 1;
  tableEntityWireLoadBody(r, v, false);
  if (r.report.nonFiniteFloat.isNotEmpty) {
    retain.reset();
    return TableEntity();
  }
  return v;
}

Uint8List tableEntityWireSaveRetain(TableEntity v, TableRetain retain, TableWireReport report) {
  retain.idUsed = 0;
  final state = WireState()..refuseSharedBlob = true;
  state.retain = retain;
  state.report = report;
  state.indexRoot(v);
  final open = <Object>{v};
  tableEntityNumber(state, v, open);
  final w = TableWireWriter(state);
  w.path = [1];
  w.putByte(1);
  tableEntityWireSaveBodyFields(w, v);
  _writeNodeTable(w);
  w.putByte(0);
  for (final id in state.ids) { w.putU64(id); }
  w.putU64(state.ids.length);
  return Uint8List.fromList(w.buf);
}

int tableEntityWireMeasureRetain(TableEntity v, TableRetain retain) {
  final report = TableWireReport();
  return tableEntityWireSaveRetain(v, retain, report).length;
}

Uint8List tableStatWireSave(TableStat v) {
  final state = WireState()..refuseSharedBlob = true;
  state.indexRoot(v);
  final open = <Object>{v};
  tableStatNumber(state, v, open);
  final w = TableWireWriter(state);
  w.putByte(1);
  tableStatWireSaveBodyFields(w, v);
  _writeNodeTable(w);
  w.putByte(0);
  for (final id in state.ids) { w.putU64(id); }
  w.putU64(state.ids.length);
  return Uint8List.fromList(w.buf);
}

TableStat tableStatWireLoad(Uint8List data, [TableWireReport? report]) {
  final v = TableStat();
  final r = TableWireReader(data, data.length);
  r.stringBlob = false;
  r.bytesBlob = false;
  r.path = [1];
  r.rootObj = v;
  r.rootType = _kTypeTableStat;
  if (report != null) r.report = report;
  r.report.nonFiniteFloat = '';
  final verdict = r.open();
  if (verdict == 1) { r.report.refused = true; return v; }
  if (verdict == 2) { r.report.malformed = true; return v; }
  r.pos = 1;
  tableStatWireLoadBody(r, v, false);
  if (r.report.nonFiniteFloat.isNotEmpty) return TableStat();
  return v;
}

const _measureStructsTableStat = <Map<int, _MF>>[
  // TableStat
  {
    -9175110139929313435: _MF(6, 0, 0, false, 0, 1, false, -1, -1, false),
    5910805692604981441: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
  },
];

const _measureUnionsTableStat = <Map<int, _MF>>[
];

int tableStatWireMeasure(Uint8List data) {
  final r = TableWireReader(data, data.length);
  if (data.length < 9 || data[0] != 1) return 0;
  final idCount = readU64(data, data.length - 8);
  if (idCount < 0 || idCount > (data.length - 9) ~/ 8) return 0;
  final idStart = data.length - 8 - idCount * 8;
  if (idStart < 1) return 0;
  final idSeen = <int>{};
  for (var k = 0; k < idCount; k++) {
    final id = readU64(data, idStart + k * 8);
    if (!idSeen.add(id)) return 0;
    r.refToId[k + 1] = id;
  }
  r.entryCount = idCount;
  r.limit = idStart;
  r.pos = 1;
  // the root body and every node record walk Go's extentScan rule (§6.2):
  // map entries, list and array elements, nested tables and union arms
  final rootScan = _ExtentScan(_measureStructsTableStat, _measureUnionsTableStat);
  if (!rootScan.record(r, 0)) return -1;
  var total = _round(_round(8) + rootScan.at);
  final nodeSpec = <int, List<int>>{
  };
  r.pos = 1;
  var seen = false;
  var unframed = false;
  var pstart = 0, pend = 0;
  while (true) {
    final ref = r.readLeb();
    if (ref == null || ref == 0) break;
    final id = r.refToId[ref];
    if (id == null || !r.has(1)) break;
    final kind = r.readByte();
    if (id == _kNodeTableId) {
      seen = true;
      if (kind != 12) { unframed = true; continue; }
      final len = r.readLeb();
      if (len == null) { unframed = true; continue; }
      final end = r.pos + len;
      if (end > r.limit) { unframed = true; continue; }
      pstart = r.pos;
      pend = end;
      r.pos = end;
    } else {
      if (!r.skipKind(kind)) break;
    }
  }
  if (unframed) return 0;
  var records = 0;
  var nodeTotal = 0;
  if (seen) {
    r.pos = pstart;
    final cnt = r.readLeb();
    if (cnt == null) return 0;
    var read = 0;
    while (r.pos < pend) {
      final typeRef = r.readLeb();
      if (typeRef == null || typeRef < 1 || typeRef > r.entryCount) return 0;
      final blen = r.readLeb();
      if (blen == null || blen < 0 || blen > pend - r.pos) return 0;
      final tid = r.refToId[typeRef] ?? 0;
      final bodyStart = r.pos;
      final sn = nodeSpec[tid];
      if (sn != null) {
        final over = TableWireReader.over(r.data.buffer.asUint8List(r.data.offsetInBytes + bodyStart, blen), r);
        final nodeScan = _ExtentScan(_measureStructsTableStat, _measureUnionsTableStat);
        if (!nodeScan.record(over, sn[1])) return -1;
        nodeTotal += _round(_round(sn[0]) + nodeScan.at);
      } else if (tid == _kBytesBlobId) {
      } else if (tid == _kStringBlobId) {
      }
      r.pos += blen;
      records++;
      read++;
    }
    if (read != cnt) return 0;
  }
  return total + nodeTotal + (records + 1) * 16;
}

TableStat tableStatWireLoadRetain(Uint8List data, TableRetain retain, TableWireReport report) {
  retain.reset();
  final v = TableStat();
  final r = TableWireReader(data, data.length);
  r.report = report;
  r.report.nonFiniteFloat = '';
  r.stringBlob = false;
  r.retain = retain;
  r.path = [1];
  r.rootObj = v;
  r.rootType = _kTypeTableStat;
  final verdict = r.open();
  if (verdict == 1) { r.report.refused = true; return v; }
  if (verdict == 2) { r.report.malformed = true; return v; }
  r.scanNodeTable();
  r.pos = 1;
  tableStatWireLoadBody(r, v, false);
  if (r.report.nonFiniteFloat.isNotEmpty) {
    retain.reset();
    return TableStat();
  }
  return v;
}

Uint8List tableStatWireSaveRetain(TableStat v, TableRetain retain, TableWireReport report) {
  retain.idUsed = 0;
  final state = WireState()..refuseSharedBlob = true;
  state.retain = retain;
  state.report = report;
  state.indexRoot(v);
  final open = <Object>{v};
  tableStatNumber(state, v, open);
  final w = TableWireWriter(state);
  w.path = [1];
  w.putByte(1);
  tableStatWireSaveBodyFields(w, v);
  _writeNodeTable(w);
  w.putByte(0);
  for (final id in state.ids) { w.putU64(id); }
  w.putU64(state.ids.length);
  return Uint8List.fromList(w.buf);
}

int tableStatWireMeasureRetain(TableStat v, TableRetain retain) {
  final report = TableWireReport();
  return tableStatWireSaveRetain(v, retain, report).length;
}

Uint8List tableMixedWireSave(TableMixed v) {
  final state = WireState()..refuseSharedBlob = true;
  state.indexRoot(v);
  final open = <Object>{v};
  tableMixedNumber(state, v, open);
  final w = TableWireWriter(state);
  w.putByte(1);
  tableMixedWireSaveBodyFields(w, v);
  _writeNodeTable(w);
  w.putByte(0);
  for (final id in state.ids) { w.putU64(id); }
  w.putU64(state.ids.length);
  return Uint8List.fromList(w.buf);
}

TableMixed tableMixedWireLoad(Uint8List data, [TableWireReport? report]) {
  final v = TableMixed();
  final r = TableWireReader(data, data.length);
  r.stringBlob = false;
  r.bytesBlob = false;
  r.path = [1];
  r.rootObj = v;
  r.rootType = _kTypeTableMixed;
  if (report != null) r.report = report;
  r.report.nonFiniteFloat = '';
  final verdict = r.open();
  if (verdict == 1) { r.report.refused = true; return v; }
  if (verdict == 2) { r.report.malformed = true; return v; }
  r.pos = 1;
  tableMixedWireLoadBody(r, v, false);
  if (r.report.nonFiniteFloat.isNotEmpty) return TableMixed();
  return v;
}

const _measureStructsTableMixed = <Map<int, _MF>>[
  // TableMixed
  {
    7663561793681298941: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
    -6181000666011064152: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
    990229364654832702: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    -7228825333587644925: _MF(8, 0, 0, false, 0, 1, false, -1, -1, false),
    -5199487799206016251: _MF(9, 0, 0, false, 0, 1, false, -1, -1, false),
    7889067272199050878: _MF(8, 0, 0, false, 0, 1, false, -1, -1, false),
    8334276637232054488: _MF(9, 0, 0, false, 0, 1, false, -1, -1, false),
    4534679873638431877: _MF(5, 0, 0, false, 0, 1, false, -1, -1, false),
    8916005070914257170: _MF(9, 0, 0, false, 0, 1, false, -1, -1, false),
    4343163795393112518: _MF(10, 0, 0, false, 0, 1, false, -1, -1, false),
    -7828083326636031446: _MF(14, 1, 13, false, 0, 1, false, 1, -1, false),
    -1268998401975805620: _MF(14, 1, 13, false, 0, 1, false, 2, -1, false),
    3329809749115557776: _MF(15, 0, 0, false, 0, 1, false, -1, 0, true),
    6294288958393922979: _MF(14, 1, 6, false, 0, 1, false, -1, -1, false),
    1415871171432867950: _MF(12, 0, 0, false, 0, 1, false, -1, -1, false),
    -3478843999652550171: _MF(14, 0, 0, false, 0, 1, false, -1, -1, false),
    -2616736631205226807: _MF(10, 0, 0, false, 0, 1, false, -1, -1, false),
    -2616737730716855018: _MF(10, 0, 0, false, 0, 1, false, -1, -1, false),
    -2616738830228483229: _MF(10, 0, 0, false, 0, 1, false, -1, -1, false),
    -7138431908913683369: _MF(10, 0, 0, false, 0, 1, false, -1, -1, false),
    6535842126802938884: _MF(11, 0, 0, false, 0, 1, false, -1, -1, false),
    -6686572760759554282: _MF(9, 0, 0, false, 0, 1, false, -1, -1, false),
    -3018575613315307966: _MF(5, 0, 0, false, 0, 1, false, -1, -1, false),
    -4669986463835551831: _MF(10, 0, 0, false, 0, 1, false, -1, -1, false),
    6200723483573899615: _MF(8, 0, 0, false, 0, 1, false, -1, -1, false),
    -4574933642144545166: _MF(1, 0, 0, false, 0, 1, false, -1, -1, false),
    -204370544148493463: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    8651444298976115966: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
  },
  // TableEntity
  {
    2593226102015223570: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
    -3797881157997612786: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    -3797880058485984575: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    -3797883357020869208: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    -5382489739573192984: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
    6028620492700434865: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
    7849183398884077159: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    7849182299372448948: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    7849185597907333581: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    9181103189905877455: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    -6866282850657997311: _MF(30, 0, 0, false, 0, 1, false, -1, -1, false),
    9179190079975030720: _MF(9, 0, 0, false, 0, 1, false, -1, -1, false),
    1271228688681418151: _MF(1, 0, 0, false, 0, 1, false, -1, -1, false),
    8535675692938965450: _MF(1, 0, 0, false, 0, 1, false, -1, -1, false),
  },
  // TableStat
  {
    -9175110139929313435: _MF(6, 0, 0, false, 0, 1, false, -1, -1, false),
    5910805692604981441: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
  },
  // TableHitEvent
  {
    -5207116919359778736: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
    9179190079975030720: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    142922654710085925: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    1324453635955533101: _MF(1, 0, 0, false, 0, 1, false, -1, -1, false),
  },
  // TableChatEvent
  {
    -6556890747741999708: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
    -292263051557875678: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
  },
  // TablePickupEvent
  {
    -7025667724851298986: _MF(7, 0, 0, false, 0, 1, false, -1, -1, false),
    -9145686598410475159: _MF(4, 0, 0, false, 0, 1, false, -1, -1, false),
  },
];

const _measureUnionsTableMixed = <Map<int, _MF>>[
  // TableEvent
  {
    3707351006901076138: _MF(13, 2, 0, false, 0, 1, false, 3, -1, false),
    -962770241226853237: _MF(13, 2, 0, false, 0, 1, false, 4, -1, false),
    -6943525758045472923: _MF(13, 2, 0, false, 0, 1, false, 5, -1, false),
  },
];

int tableMixedWireMeasure(Uint8List data) {
  final r = TableWireReader(data, data.length);
  if (data.length < 9 || data[0] != 1) return 0;
  final idCount = readU64(data, data.length - 8);
  if (idCount < 0 || idCount > (data.length - 9) ~/ 8) return 0;
  final idStart = data.length - 8 - idCount * 8;
  if (idStart < 1) return 0;
  final idSeen = <int>{};
  for (var k = 0; k < idCount; k++) {
    final id = readU64(data, idStart + k * 8);
    if (!idSeen.add(id)) return 0;
    r.refToId[k + 1] = id;
  }
  r.entryCount = idCount;
  r.limit = idStart;
  r.pos = 1;
  // the root body and every node record walk Go's extentScan rule (§6.2):
  // map entries, list and array elements, nested tables and union arms
  final rootScan = _ExtentScan(_measureStructsTableMixed, _measureUnionsTableMixed);
  if (!rootScan.record(r, 0)) return -1;
  var total = _round(_round(1352) + rootScan.at);
  final nodeSpec = <int, List<int>>{
  };
  r.pos = 1;
  var seen = false;
  var unframed = false;
  var pstart = 0, pend = 0;
  while (true) {
    final ref = r.readLeb();
    if (ref == null || ref == 0) break;
    final id = r.refToId[ref];
    if (id == null || !r.has(1)) break;
    final kind = r.readByte();
    if (id == _kNodeTableId) {
      seen = true;
      if (kind != 12) { unframed = true; continue; }
      final len = r.readLeb();
      if (len == null) { unframed = true; continue; }
      final end = r.pos + len;
      if (end > r.limit) { unframed = true; continue; }
      pstart = r.pos;
      pend = end;
      r.pos = end;
    } else {
      if (!r.skipKind(kind)) break;
    }
  }
  if (unframed) return 0;
  var records = 0;
  var nodeTotal = 0;
  if (seen) {
    r.pos = pstart;
    final cnt = r.readLeb();
    if (cnt == null) return 0;
    var read = 0;
    while (r.pos < pend) {
      final typeRef = r.readLeb();
      if (typeRef == null || typeRef < 1 || typeRef > r.entryCount) return 0;
      final blen = r.readLeb();
      if (blen == null || blen < 0 || blen > pend - r.pos) return 0;
      final tid = r.refToId[typeRef] ?? 0;
      final bodyStart = r.pos;
      final sn = nodeSpec[tid];
      if (sn != null) {
        final over = TableWireReader.over(r.data.buffer.asUint8List(r.data.offsetInBytes + bodyStart, blen), r);
        final nodeScan = _ExtentScan(_measureStructsTableMixed, _measureUnionsTableMixed);
        if (!nodeScan.record(over, sn[1])) return -1;
        nodeTotal += _round(_round(sn[0]) + nodeScan.at);
      } else if (tid == _kBytesBlobId) {
      } else if (tid == _kStringBlobId) {
      }
      r.pos += blen;
      records++;
      read++;
    }
    if (read != cnt) return 0;
  }
  return total + nodeTotal + (records + 1) * 16;
}

TableMixed tableMixedWireLoadRetain(Uint8List data, TableRetain retain, TableWireReport report) {
  retain.reset();
  final v = TableMixed();
  final r = TableWireReader(data, data.length);
  r.report = report;
  r.report.nonFiniteFloat = '';
  r.stringBlob = false;
  r.retain = retain;
  r.path = [1];
  r.rootObj = v;
  r.rootType = _kTypeTableMixed;
  final verdict = r.open();
  if (verdict == 1) { r.report.refused = true; return v; }
  if (verdict == 2) { r.report.malformed = true; return v; }
  r.scanNodeTable();
  r.pos = 1;
  tableMixedWireLoadBody(r, v, false);
  if (r.report.nonFiniteFloat.isNotEmpty) {
    retain.reset();
    return TableMixed();
  }
  return v;
}

Uint8List tableMixedWireSaveRetain(TableMixed v, TableRetain retain, TableWireReport report) {
  retain.idUsed = 0;
  final state = WireState()..refuseSharedBlob = true;
  state.retain = retain;
  state.report = report;
  state.indexRoot(v);
  final open = <Object>{v};
  tableMixedNumber(state, v, open);
  final w = TableWireWriter(state);
  w.path = [1];
  w.putByte(1);
  tableMixedWireSaveBodyFields(w, v);
  _writeNodeTable(w);
  w.putByte(0);
  for (final id in state.ids) { w.putU64(id); }
  w.putU64(state.ids.length);
  return Uint8List.fromList(w.buf);
}

int tableMixedWireMeasureRetain(TableMixed v, TableRetain retain) {
  final report = TableWireReport();
  return tableMixedWireSaveRetain(v, retain, report).length;
}

// ---- the JSON text form (docs/SPEC-TABLES.md §16) ----

// TableTextReport is a tolerant text read's report (§16.3): the three events
// the card's constructs can move — an unknown property, a kind mismatch, a
// clamp.
final class TableTextReport {
  int unknown = 0;
  int kindMismatch = 0;
  int clamped = 0;
  int duplicate = 0;
  final Map<String, int> duplicateKeys = <String, int>{};
  final Set<int> textLabels = <int>{};
}

// _JOut is the writer's sink (§16.1): raw text and the two-space indent.
final class _JOut {
  final StringBuffer b = StringBuffer();
  void raw(String s) => b.write(s);
  void line(int depth) {
    b.write('\n');
    for (var i = 0; i < depth; i++) {
      b.write('  ');
    }
  }

  @override
  String toString() => b.toString();
}

const String _jsonHex = '0123456789abcdef';

// _jsonString renders one Dart string as one JSON string (§16.3): the escapes
// JSON names and every other code point whole, so a multi-byte character rides
// as itself.
String _jsonString(String s) {
  final b = StringBuffer();
  b.write('"');
  for (final rune in s.runes) {
    if (rune == 0x22) {
      b.write('\\"');
    } else if (rune == 0x5C) {
      b.write('\\\\');
    } else if (rune == 0x08) {
      b.write('\\b');
    } else if (rune == 0x0C) {
      b.write('\\f');
    } else if (rune == 0x0A) {
      b.write('\\n');
    } else if (rune == 0x0D) {
      b.write('\\r');
    } else if (rune == 0x09) {
      b.write('\\t');
    } else if (rune < 0x20) {
      b.write('\\u00');
      b.write(_jsonHex[rune >> 4]);
      b.write(_jsonHex[rune & 0xF]);
    } else {
      b.write(String.fromCharCode(rune));
    }
  }
  b.write('"');
  return b.toString();
}

// _jsonEnum spells an enum value by NAME (§16.2); a value no variant names has
// no text spelling and the writer refuses it, exactly as measure and save do
// (docs/SPEC-TABLES.md §5, §16.3).
String _jsonEnum(int v, String Function(int) name, String where) {
  final n = name(v);
  if (n.isEmpty) {
    throw StateError('$where: enum value $v names no variant (docs/SPEC-TABLES.md §5, §16.2)');
  }
  return _jsonString(n);
}

// _jsonText is the ZERO RULE (§16.3, text_holds_zero), one helper for every
// place the read stores text: a string(N) or wstring(N) value, a *string
// value, a text-list element, a map key. A stored string that decodes to
// U+0000 refuses the read, because no wire carries a zero in text. A string
// the read only SKIPS (an unknown key's value) never reaches here.
String _jsonText(String s) {
  if (s.contains('\u0000')) throw const FormatException('text_holds_zero (docs/SPEC-TABLES.md §16.3)');
  return s;
}

// _jsonStringField reads one string(N) field (§16.2): a non-string is a kind
// mismatch; a string past N is clamped at a CODE POINT boundary and counted,
// never cut through a multi-byte character (§16.3).
String _jsonStringField(dynamic val, int bound, TableTextReport r) {
  if (val is! String) {
    r.kindMismatch++;
    return '';
  }
  if (utf8.encode(_jsonText(val)).length <= bound) return val;
  final kept = StringBuffer();
  var used = 0;
  for (final rune in val.runes) {
    final len = utf8.encode(String.fromCharCode(rune)).length;
    if (used + len > bound) break;
    kept.write(String.fromCharCode(rune));
    used += len;
  }
  r.clamped++;
  return kept.toString();
}

// _jsonIntField reads one int field (§16.2): an integer token, or an integral
// float, placed and range-clamped; anything else is a kind mismatch.
int _jsonIntField(dynamic val, TableTextReport r, int? lo, int? hi, [bool unsigned = false]) {
  int clamp(int v) {
    if (unsigned) {
      if (v < 0 && hi != null && hi >= 0) { v = lo ?? 0; r.clamped++; }
      if (lo != null && _u64Below(v, lo)) { v = lo; r.clamped++; }
      if (hi != null && _u64Above(v, hi)) { v = hi; r.clamped++; }
    } else {
      if (lo != null && v < lo) { v = lo; r.clamped++; }
      if (hi != null && v > hi) { v = hi; r.clamped++; }
    }
    return v;
  }
  if (val is int) return clamp(val);
  if (unsigned && val is _JNum && (hi == null || hi < 0)) {
    final wide = BigInt.tryParse(val.token);
    if (wide != null && wide >= BigInt.zero && wide <= BigInt.parse('18446744073709551615')) {
      final signed = wide > BigInt.from(9223372036854775807)
          ? wide - (BigInt.one << 64) : wide;
      return clamp(signed.toInt());
    }
  }
  if (val is num && val.isFinite && val.toDouble() == val.toDouble().truncateToDouble()) {
    if (val.toDouble() > 9223372036854775807.0) { r.clamped++; return clamp(9223372036854775807); }
    if (val.toDouble() < -9223372036854775808.0) { r.clamped++; return clamp(-9223372036854775808); }
    return clamp(val.toInt());
  }
  if (val is _JNum && val.d.isFinite && val.d == val.d.truncateToDouble()) {
    if (val.d > 9223372036854775807.0) { r.clamped++; return clamp(9223372036854775807); }
    if (val.d < -9223372036854775808.0) { r.clamped++; return clamp(-9223372036854775808); }
    return clamp(val.d.toInt());
  }
  r.kindMismatch++;
  return 0;
}

// _jsonFloat spells a float at the shortest precision that reads back the same
// value AT ITS OWN WIDTH (§16.2): a float32 from six significant digits up to
// nine, a float64 from fifteen up to seventeen, each candidate C's %.*g
// (_formatG). single is a float32 field.
// A non-finite float has no JSON spelling and the writer refuses it.
String _jsonFloat(double v, bool single) {
  if (v.isNaN || v.isInfinite) {
    throw StateError('float $v has no JSON spelling (docs/SPEC-TABLES.md §16.2)');
  }
  final low = single ? 6 : 15;
  final high = single ? 9 : 17;
  for (var digits = low; ; digits++) {
    final text = _formatG(v, digits);
    if (digits >= high) return text;
    final back = double.tryParse(text);
    if (back == null) continue;
    if (single) {
      if (_f32Bits(back) == _f32Bits(v)) return text;
    } else if (back == v) {
      return text;
    }
  }
}

// _formatG is C's %.*g for a finite double (docs/SPEC-TABLES.md §16.2), the
// engine's formatG (internal/tabletext/write.go) spelled out: the digits are
// the EXACT binary value rounded half to even at prec significant digits, the
// exponent form when the decimal exponent is below -4 or at least prec, the
// plain form otherwise, trailing zeros and a trailing point removed, and an
// exponent of at least two digits. Dart's toStringAsFixed and
// toStringAsExponential break an exact tie AWAY from even (ECMAScript's
// "larger n"), so they cannot stand in for it: the float32 3624545.25 at seven
// digits is 3624545.2 in C and 3624545.3 through toStringAsFixed (§16.2's
// spelling is C's; the text differential against the Go engine found it).
String _formatG(double value, int prec) {
  if (prec < 1) prec = 1;
  final bd = ByteData(8);
  bd.setFloat64(0, value);
  final hi = bd.getUint32(0);
  final lo = bd.getUint32(4);
  final sign = (hi >> 31) != 0 ? '-' : '';
  final biased = (hi >> 20) & 0x7FF;
  var mant = (BigInt.from(hi & 0xFFFFF) << 32) | BigInt.from(lo);
  var e2 = -1074;
  if (biased != 0) {
    mant |= BigInt.one << 52;
    e2 = biased - 1075;
  }
  final ten = BigInt.from(10);
  var digits = '0' * prec;
  var exp = 0;
  if (mant != BigInt.zero) {
    // value = numer / denom exactly; exp is the decimal exponent with
    // 10^exp <= |value| < 10^(exp+1).
    var numer = mant;
    var denom = BigInt.one;
    if (e2 >= 0) {
      numer = numer << e2;
    } else {
      denom = denom << -e2;
    }
    bool atLeast(int k) =>
        k >= 0 ? numer >= denom * ten.pow(k) : numer * ten.pow(-k) >= denom;
    exp = numer.toString().length - denom.toString().length;
    while (!atLeast(exp)) {
      exp--;
    }
    while (atLeast(exp + 1)) {
      exp++;
    }
    final shift = exp - prec + 1;
    var n = numer;
    var d = denom;
    if (shift >= 0) {
      d = d * ten.pow(shift);
    } else {
      n = n * ten.pow(-shift);
    }
    var q = n ~/ d;
    final twice = (n - q * d) * BigInt.two;
    if (twice > d || (twice == d && q.isOdd)) {
      q += BigInt.one;
    }
    if (q == ten.pow(prec)) {
      q = q ~/ ten;
      exp++;
    }
    digits = q.toString();
  }
  if (exp < -4 || exp >= prec) {
    final m = prec == 1 ? digits : digits[0] + '.' + digits.substring(1);
    return sign + _trimZeros(m) + 'e' + _expDigits(exp);
  }
  if (exp < 0) {
    return sign + _trimZeros('0.' + '0' * (-exp - 1) + digits);
  }
  if (exp + 1 >= prec) {
    return sign + digits;
  }
  return sign +
      _trimZeros(digits.substring(0, exp + 1) + '.' + digits.substring(exp + 1));
}

String _trimZeros(String s) {
  if (!s.contains('.')) return s;
  while (s.endsWith('0')) {
    s = s.substring(0, s.length - 1);
  }
  if (s.endsWith('.')) s = s.substring(0, s.length - 1);
  return s;
}

String _expDigits(int exp) {
  var sign = '+';
  if (exp < 0) {
    sign = '-';
    exp = -exp;
  }
  if (exp < 10) return sign + '0' + exp.toString();
  return sign + exp.toString();
}

// _Share is one text's node identity (§16.7): how many slots name each node,
// the label a shared node took at its first write, and the object a label
// defined on the way back in.
final class _Share {
  final Map<Object, int> count = <Object, int>{};
  final Map<Object, int> label = <Object, int>{};
  final Map<int, Object> defined = <int, Object>{};
  final Set<int> pending = <int>{};
  int next = 0;

  void see(Object obj) {
    count[obj] = (count[obj] ?? 0) + 1;
  }

  int define(Object obj) {
    next++;
    label[obj] = next;
    return next;
  }
}

// _writePtr writes one pointer slot (§16.7): null, a node named once in place,
// a shared node defined at its first write with "&node" as the first key, and
// a reference as "&node" alone after that.
void _writePtr(_JOut out, Object? node, int depth, _Share sh, void Function(int? label) body) {
  if (node == null) {
    out.raw('null');
    return;
  }
  if ((sh.count[node] ?? 0) <= 1) {
    body(null);
    return;
  }
  final existing = sh.label[node];
  if (existing != null) {
    out.raw('{');
    out.line(depth + 1);
    out.raw('"&node": ');
    out.raw(existing.toString());
    out.line(depth);
    out.raw('}');
    return;
  }
  body(sh.define(node));
}

Object? _readPtr(dynamic val, _Share sh, TableTextReport r, Object Function() make, void Function(Object obj, Map<String, dynamic> m) fill) {
  if (val == null) return null;
  if (val is! Map) {
    r.kindMismatch++;
    return null;
  }
  final obj = Map<String, dynamic>.from(val);
  if (obj.containsKey('&node')) {
    if (obj.keys.first != '&node') throw const FormatException('node label is not first');
    final raw = obj['&node'];
    if (raw is! int || raw < 1) {
      throw const FormatException('invalid node label');
    }
    if (obj.length == 1) {
      final have = sh.defined[raw];
      if (have == null) {
        if (!sh.pending.contains(raw) && r.textLabels.contains(raw)) return null;
        throw const FormatException('undefined node label');
      }
      if (have.runtimeType != make().runtimeType) { r.kindMismatch++; return null; }
      return have;
    }
    final have = sh.defined[raw];
    if (have != null) throw const FormatException('node label defined twice');
    final made = make();
    sh.pending.add(raw);
    fill(made, obj);
    sh.pending.remove(raw);
    sh.defined[raw] = made;
    return made;
  }
  final made = make();
  fill(made, obj);
  return made;
}

// _jsonFloatField reads one float field (§16.2): a number, placed; anything
// else is a kind mismatch.
double _jsonFloatField(dynamic val, TableTextReport r, [bool single = false]) {
  double? d;
  if (val is num) d = val.toDouble();
  if (val is _JNum) d = val.d;
  if (val is String) {
    d = double.tryParse(val);
  }
  if (d != null && d.isFinite) {
    if (!single) return d;
    // The field's storage encoder rounds; do not round a value on the read
    // path before the caller can apply its own range and shape checks.
    if (d.abs() < 3.4028235677973366e38) return d;
    if (val is _JNum && !val.token.contains('.') && !val.token.contains('e') && !val.token.contains('E')) {
      final integer = BigInt.tryParse(val.token);
      final edge = BigInt.parse('340282356779733661637539395458142568448');
      if (integer != null && integer.abs() < edge) return d;
    }
  }
  r.kindMismatch++;
  return 0.0;
}
// §16.2: comments and trailing commas are layout, not values. The
// duplicate count is taken before jsonDecode folds an object's repeated key.
// String tokens are copied byte-for-byte so comment marks inside them stay text.
String _jsonPrepare(String text, TableTextReport r) {
  final source = text.codeUnits;
  final stripped = <int>[];
  var quoted = false;
  var depth = 0;
  for (var i = 0; i < source.length; i++) {
    final c = source[i];
    if (quoted) {
      stripped.add(c);
      if (c == 0x5c && i + 1 < source.length) {
        stripped.add(source[++i]);
      } else if (c == 0x22) {
        quoted = false;
      }
      continue;
    }
    if (c == 0x22) {
      quoted = true;
      stripped.add(c);
      continue;
    }
    if (c == 0x2f && i + 1 < source.length && source[i + 1] == 0x2f) {
      i += 2;
      while (i < source.length && source[i] != 0x0a) i++;
      stripped.add(0x20);
      if (i < source.length) stripped.add(source[i]);
      continue;
    }
    if (c == 0x2f && i + 1 < source.length && source[i + 1] == 0x2a) {
      i += 2;
      var closed = false;
      for (; i + 1 < source.length; i++) {
        if (source[i] == 0x2a && source[i + 1] == 0x2f) {
          i++;
          closed = true;
          break;
        }
      }
      if (!closed) throw const FormatException('unclosed JSON comment');
      stripped.add(0x20);
      continue;
    }
    if (c == 0x7b || c == 0x5b) {
      if (++depth > 128) throw const FormatException('JSON nesting past 128');
    } else if (c == 0x7d || c == 0x5d) {
      depth--;
    }
    stripped.add(c);
  }
  if (quoted || depth != 0) throw const FormatException('incomplete JSON text');
  final clean = <int>[];
  quoted = false;
  for (var i = 0; i < stripped.length; i++) {
    final c = stripped[i];
    if (quoted) {
      clean.add(c);
      if (c == 0x5c && i + 1 < stripped.length) clean.add(stripped[++i]);
      else if (c == 0x22) quoted = false;
      continue;
    }
    if (c == 0x22) { quoted = true; clean.add(c); continue; }
    if (c == 0x2c) {
      var j = i + 1;
      while (j < stripped.length && stripped[j] <= 0x20) j++;
      if (j < stripped.length && (stripped[j] == 0x7d || stripped[j] == 0x5d)) continue;
    }
    clean.add(c);
  }
  final result = String.fromCharCodes(clean);
  final stack = <Set<String>?>[];
  final firstLabels = <int>{};
  for (var i = 0; i < clean.length; i++) {
    final c = clean[i];
    if (c == 0x7b) { stack.add(<String>{}); continue; }
    if (c == 0x5b) { stack.add(null); continue; }
    if (c == 0x7d || c == 0x5d) { if (stack.isNotEmpty) stack.removeLast(); continue; }
    if (c != 0x22) continue;
    final begin = i;
    i++;
    for (; i < clean.length; i++) {
      if (clean[i] == 0x5c) { i++; continue; }
      if (clean[i] == 0x22) break;
    }
    if (i >= clean.length) throw const FormatException('unclosed JSON string');
    var j = i + 1;
    while (j < clean.length && clean[j] <= 0x20) j++;
    if (j >= clean.length || clean[j] != 0x3a || stack.isEmpty || stack.last == null) continue;
    final key = jsonDecode(result.substring(begin, i + 1)) as String;
    if (!stack.last!.add(key)) {
      r.duplicate++;
      r.duplicateKeys[key] = (r.duplicateKeys[key] ?? 0) + 1;
    }
    if (key == '&node') {
      var at = j + 1;
      while (at < clean.length && clean[at] <= 0x20) at++;
      final start = at;
      while (at < clean.length && clean[at] >= 0x30 && clean[at] <= 0x39) at++;
      final label = int.tryParse(result.substring(start, at));
      while (at < clean.length && clean[at] <= 0x20) at++;
      if (label != null && label > 0 && firstLabels.add(label) && at < clean.length && clean[at] == 0x2c) {
        r.textLabels.add(label);
      }
    }
  }
  return result;
}

// _JNum is a JSON number the int type cannot hold exactly, kept as its
// token so a wide field reads it without rounding (docs/SPEC-TABLES.md §16.2).
final class _JNum {
  final String token;
  final double d;
  _JNum(this.token) : d = double.parse(token);
}

// _jsonDecodeExact is JSON decoding that keeps every number's token
// (docs/SPEC-TABLES.md §16.2): an integer token an int holds is an int, and
// every other number is a _JNum.
dynamic _jsonDecodeExact(String text) {
  final p = _JParse(text);
  p.ws();
  final v = p.value(0);
  p.ws();
  if (p.i != text.length) throw const FormatException('trailing text after the JSON value');
  return v;
}

final class _JParse {
  final String s;
  int i = 0;
  _JParse(this.s);

  void ws() {
    while (i < s.length) {
      final c = s.codeUnitAt(i);
      if (c != 0x20 && c != 0x09 && c != 0x0A && c != 0x0D) return;
      i++;
    }
  }

  void expect(int c) {
    if (i >= s.length || s.codeUnitAt(i) != c) throw FormatException('expected ${String.fromCharCode(c)}', s, i);
    i++;
  }

  dynamic value(int depth) {
    if (depth > 128) throw const FormatException('nested past the depth cap of 128');
    if (i >= s.length) throw const FormatException('unexpected end of text');
    final c = s.codeUnitAt(i);
    if (c == 0x7B) {
      i++;
      final m = <String, dynamic>{};
      ws();
      if (i < s.length && s.codeUnitAt(i) == 0x7D) {
        i++;
        return m;
      }
      while (true) {
        ws();
        final k = str();
        ws();
        expect(0x3A);
        ws();
        m[k] = value(depth + 1);
        ws();
        if (i < s.length && s.codeUnitAt(i) == 0x2C) {
          i++;
          continue;
        }
        expect(0x7D);
        return m;
      }
    }
    if (c == 0x5B) {
      i++;
      final l = <dynamic>[];
      ws();
      if (i < s.length && s.codeUnitAt(i) == 0x5D) {
        i++;
        return l;
      }
      while (true) {
        ws();
        l.add(value(depth + 1));
        ws();
        if (i < s.length && s.codeUnitAt(i) == 0x2C) {
          i++;
          continue;
        }
        expect(0x5D);
        return l;
      }
    }
    if (c == 0x22) return str();
    if (s.startsWith('true', i)) {
      i += 4;
      return true;
    }
    if (s.startsWith('false', i)) {
      i += 5;
      return false;
    }
    if (s.startsWith('null', i)) {
      i += 4;
      return null;
    }
    final number = RegExp(r'-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?').matchAsPrefix(s, i);
    if (number == null) throw FormatException('unexpected character', s, i);
    i = number.end;
    if (i < s.length) {
      final next = s.codeUnitAt(i);
      if (next != 0x2C && next != 0x5D && next != 0x7D && next != 0x20 && next != 0x09 && next != 0x0A && next != 0x0D) {
        throw FormatException('invalid JSON number', s, i);
      }
    }
    final token = number.group(0)!;
    if (!token.contains('.') && !token.contains('e') && !token.contains('E')) {
      final n = int.tryParse(token);
      if (n != null) return n;
    }
    return _JNum(token);
  }

  String str() {
    expect(0x22);
    final b = StringBuffer();
    while (true) {
      if (i >= s.length) throw const FormatException('unterminated string');
      final c = s.codeUnitAt(i++);
      if (c == 0x22) return b.toString();
      if (c != 0x5C) {
        b.writeCharCode(c);
        continue;
      }
      if (i >= s.length) throw const FormatException('unterminated escape');
      final e = s.codeUnitAt(i++);
      switch (e) {
        case 0x22:
        case 0x5C:
        case 0x2F:
          b.writeCharCode(e);
        case 0x62:
          b.writeCharCode(0x08);
        case 0x66:
          b.writeCharCode(0x0C);
        case 0x6E:
          b.writeCharCode(0x0A);
        case 0x72:
          b.writeCharCode(0x0D);
        case 0x74:
          b.writeCharCode(0x09);
        case 0x75:
          if (i + 4 > s.length) throw const FormatException('short \\u escape');
          b.writeCharCode(int.parse(s.substring(i, i + 4), radix: 16));
          i += 4;
        default:
          throw FormatException('bad escape', s, i);
      }
    }
  }
}

// _jsonWide spells a wide raw value (docs/SPEC-TABLES.md §16.2,
// internal/tabletext FormatWide): a 128-bit integer as its decimal integer, a
// fixed value as the shortest exact decimal with at least one fraction digit.
String _jsonWide(BigInt raw, int frac, bool integer) {
  if (integer) return raw.toString();
  final b = StringBuffer();
  final mag = raw.abs();
  if (raw.isNegative) b.write('-');
  final mask = (BigInt.one << frac) - BigInt.one;
  b.write((mag >> frac).toString());
  b.write('.');
  var fr = mag & mask;
  if (fr == BigInt.zero) {
    b.write('0');
    return b.toString();
  }
  final ten = BigInt.from(10);
  while (fr != BigInt.zero) {
    fr = fr * ten;
    b.write((fr >> frac).toString());
    fr = fr & mask;
  }
  return b.toString();
}

// _jsonWideField reads one wide value from its token (docs/SPEC-TABLES.md
// §16.2, internal/tabletext ParseWide and WideClamp): the value times 2^frac,
// exactly; a value this Q I.F cannot spell is a kind mismatch and places
// nothing; a magnitude past the storage domain saturates and counts a clamp,
// and the declared raw range clamps after.
BigInt _jsonWideField(dynamic val, TableTextReport r, int frac, bool signed, BigInt? lo, BigInt? hi, BigInt def) {
  String token;
  if (val is int) {
    token = val.toString();
  } else if (val is _JNum) {
    token = val.token;
  } else if (val is double) {
    token = val.toString();
  } else {
    r.kindMismatch++;
    return def;
  }
  var i = 0;
  var negative = false;
  if (i < token.length && (token[i] == '-' || token[i] == '+')) {
    negative = token[i] == '-';
    i++;
  }
  final intDigits = StringBuffer();
  final fracDigits = StringBuffer();
  while (i < token.length && token.codeUnitAt(i) >= 0x30 && token.codeUnitAt(i) <= 0x39) {
    intDigits.write(token[i]);
    i++;
  }
  if (i < token.length && token[i] == '.') {
    i++;
    while (i < token.length && token.codeUnitAt(i) >= 0x30 && token.codeUnitAt(i) <= 0x39) {
      fracDigits.write(token[i]);
      i++;
    }
  }
  var exp = 0;
  if (i < token.length && (token[i] == 'e' || token[i] == 'E')) {
    i++;
    var expNegative = false;
    if (i < token.length && (token[i] == '-' || token[i] == '+')) {
      expNegative = token[i] == '-';
      i++;
    }
    while (i < token.length && token.codeUnitAt(i) >= 0x30 && token.codeUnitAt(i) <= 0x39) {
      if (exp < 100000) exp = exp * 10 + (token.codeUnitAt(i) - 0x30);
      i++;
    }
    if (expNegative) exp = -exp;
  }
  var digits = intDigits.toString() + fracDigits.toString();
  var point = intDigits.length + exp;
  while (digits.isNotEmpty && digits[0] == '0') {
    digits = digits.substring(1);
    point--;
  }
  while (digits.isNotEmpty && digits[digits.length - 1] == '0') {
    digits = digits.substring(0, digits.length - 1);
  }
  final domainLo = signed ? -(BigInt.one << 127) : BigInt.zero;
  final domainHi = signed ? (BigInt.one << 127) - BigInt.one : (BigInt.one << 128) - BigInt.one;
  BigInt raw;
  if (digits.isEmpty) {
    raw = BigInt.zero;
  } else if (point > 40) {
    raw = negative ? domainLo : domainHi;
    r.clamped++;
  } else if (point < -40) {
    r.kindMismatch++;
    return def;
  } else {
    final scale = point - digits.length;
    var v = BigInt.parse(digits) << frac;
    if (scale >= 0) {
      v = v * BigInt.from(10).pow(scale);
    } else {
      final den = BigInt.from(10).pow(-scale);
      if (v.remainder(den) != BigInt.zero) {
        r.kindMismatch++;
        return def;
      }
      v = v ~/ den;
    }
    raw = negative ? -v : v;
    if (raw < domainLo) {
      raw = domainLo;
      r.clamped++;
    } else if (raw > domainHi) {
      raw = domainHi;
      r.clamped++;
    }
  }
  if (lo != null && raw < lo) {
    raw = lo;
    r.clamped++;
  }
  if (hi != null && raw > hi) {
    raw = hi;
    r.clamped++;
  }
  return raw;
}

bool _jsonBoolField(dynamic val, TableTextReport r) {
  if (val is bool) return val;
  r.kindMismatch++;
  return false;
}

String _jsonBytesField(dynamic val, TableTextReport r, int bound) {
  if (val is String) {
    try {
      final b = base64Decode(base64.normalize(val));
      if (b.length > bound) {
        r.clamped++;
        return latin1.decode(b.sublist(0, bound));
      }
      return latin1.decode(b);
    } on FormatException {
      // falls through to the mismatch below
    }
  }
  r.kindMismatch++;
  return '';
}

String? _jsonStringBlob(dynamic val, TableTextReport r) {
  if (val == null) return null;
  if (val is String) return _jsonText(val);
  r.kindMismatch++;
  return null;
}

Uint8List? _jsonBytesBlob(dynamic val, TableTextReport r) {
  if (val == null) return null;
  if (val is String) {
    try {
      return base64Decode(base64.normalize(val));
    } on FormatException {
      // falls through to the mismatch below
    }
  }
  r.kindMismatch++;
  return null;
}

// _jsonWStringField reads wide text (docs/SPEC-TABLES.md §16.2): a string,
// clamped at its bound in CODE UNITS and never through a surrogate pair.
String _jsonWStringField(dynamic val, int bound, TableTextReport r) {
  if (val is! String) {
    r.kindMismatch++;
    return '';
  }
  if (_jsonText(val).length <= bound) return val;
  var keep = bound;
  if (keep > 0) {
    final u = val.codeUnitAt(keep - 1);
    if (u >= 0xD800 && u <= 0xDBFF) keep--;
  }
  r.clamped++;
  return val.substring(0, keep);
}

List<T> _jsonList<T>(dynamic val, TableTextReport r, int bound, T Function(dynamic) f) {
  final out = <T>[];
  if (val is! List) {
    r.kindMismatch++;
    return out;
  }
  for (final item in val) {
    if (bound >= 0 && out.length >= bound) {
      r.clamped += val.length - out.length;
      break;
    }
    out.add(f(item));
  }
  return out;
}

List<T> _jsonFixed<T>(dynamic val, TableTextReport r, int n, T Function() def, T Function(dynamic) f) {
  final out = List<T>.generate(n, (_) => def());
  if (val is! List) {
    r.kindMismatch++;
    return out;
  }
  for (var i = 0; i < val.length; i++) {
    if (i >= n) {
      r.clamped += val.length - i;
      break;
    }
    out[i] = f(val[i]);
  }
  return out;
}

List<T> _jsonKeyed<T>(dynamic val, TableTextReport r, int n, T Function() def, Map<String, int> slots, T Function(dynamic) f) {
  final out = List<T>.generate(n, (_) => def());
  if (val is! Map) {
    r.kindMismatch++;
    return out;
  }
  for (final entry in val.entries) {
    final slot = slots[entry.key];
    if (slot == null) {
      final extra = r.duplicateKeys.remove(entry.key) ?? 0;
      r.unknown += 1 + extra;
      r.duplicate -= extra;
      continue;
    }
    out[slot] = f(entry.value);
  }
  return out;
}

List<T> _jsonMap<T>(dynamic val, TableTextReport r, T Function(String, dynamic) f) {
  final out = <T>[];
  if (val is! Map) {
    r.kindMismatch++;
    return out;
  }
  for (final entry in val.entries) {
    out.add(f(entry.key as String, entry.value));
  }
  return out;
}

int _jsonIntKey(String k, TableTextReport r, int? lo, int? hi, bool unsigned) {
  final v = int.tryParse(k);
  if (v == null) {
    r.kindMismatch++;
    return 0;
  }
  return _jsonIntField(v, r, lo, hi, unsigned);
}

String tableWeaponName(int v) {
  switch (v) {
    case 0: return "None";
    case 1: return "Fists";
    case 2: return "Pistol";
    case 3: return "Shotgun";
    case 4: return "Rifle";
    case 5: return "Sniper";
    case 6: return "Smg";
    case 7: return "Rocket";
    case 8: return "Grenade";
    case 9: return "Plasma";
    case 10: return "Railgun";
    case 11: return "Flamer";
    case 12: return "Mine";
    case 13: return "Turret";
    case 14: return "Drone";
    case 15: return "Repair";
  }
  return '';
}

TableWeapon tableWeaponFromName(dynamic val, TableTextReport r) {
  if (val is! String) { r.kindMismatch++; return TableWeapon.none; }
  switch (val) {
    case "None": return TableWeapon.none;
    case "Fists": return TableWeapon.fists;
    case "Pistol": return TableWeapon.pistol;
    case "Shotgun": return TableWeapon.shotgun;
    case "Rifle": return TableWeapon.rifle;
    case "Sniper": return TableWeapon.sniper;
    case "Smg": return TableWeapon.smg;
    case "Rocket": return TableWeapon.rocket;
    case "Grenade": return TableWeapon.grenade;
    case "Plasma": return TableWeapon.plasma;
    case "Railgun": return TableWeapon.railgun;
    case "Flamer": return TableWeapon.flamer;
    case "Mine": return TableWeapon.mine;
    case "Turret": return TableWeapon.turret;
    case "Drone": return TableWeapon.drone;
    case "Repair": return TableWeapon.repair;
  }
  r.unknown++;
  return TableWeapon.none;
}

void _tableDamageToJson(_JOut out, int bits, int depth) {
  if (bits == 0) { out.raw('[]'); return; }
  out.raw('[');
  var first = true;
  if (bits & 1 != 0) { first = false; out.line(depth + 1); out.raw(_jsonString("Bleeding")); }
  if (bits & 2 != 0) { if (!first) out.raw(','); first = false; out.line(depth + 1); out.raw(_jsonString("Burning")); }
  if (bits & 4 != 0) { if (!first) out.raw(','); first = false; out.line(depth + 1); out.raw(_jsonString("Stunned")); }
  if (bits & 8 != 0) { if (!first) out.raw(','); first = false; out.line(depth + 1); out.raw(_jsonString("Slowed")); }
  if (bits & 16 != 0) { if (!first) out.raw(','); first = false; out.line(depth + 1); out.raw(_jsonString("Poisoned")); }
  if (bits & 32 != 0) { if (!first) out.raw(','); first = false; out.line(depth + 1); out.raw(_jsonString("Shielded")); }
  if (bits & 64 != 0) { if (!first) out.raw(','); first = false; out.line(depth + 1); out.raw(_jsonString("Airborne")); }
  if (bits & 128 != 0) { if (!first) out.raw(','); first = false; out.line(depth + 1); out.raw(_jsonString("Downed")); }
  if (first) { throw StateError('flags TableDamage: bits $bits name no variant (docs/SPEC-TABLES.md §16.2)'); }
  out.line(depth);
  out.raw(']');
}

int _tableDamageFromJson(dynamic val, TableTextReport r) {
  if (val is! List) { r.kindMismatch++; return 0; }
  var bits = 0;
  for (final item in val) {
    switch (item) {
      case "Bleeding": bits |= 1;
      case "Burning": bits |= 2;
      case "Stunned": bits |= 4;
      case "Slowed": bits |= 8;
      case "Poisoned": bits |= 16;
      case "Shielded": bits |= 32;
      case "Airborne": bits |= 64;
      case "Downed": bits |= 128;
      default: r.unknown++;
    }
  }
  return bits;
}

void _tableEventToJson(_JOut out, TableEvent v, int depth, _Share sh) {
  switch (v.armTag) {
    case 0:
      out.raw('{}');
      return;
    case 1:
      out.raw('{');
      out.line(depth + 1);
      out.raw(_jsonString("hit"));
      out.raw(': ');
      tableHitEventToJsonBody(out, v.hit, depth + 1, sh);
      out.line(depth);
      out.raw('}');
      return;
    case 2:
      out.raw('{');
      out.line(depth + 1);
      out.raw(_jsonString("chat"));
      out.raw(': ');
      tableChatEventToJsonBody(out, v.chat, depth + 1, sh);
      out.line(depth);
      out.raw('}');
      return;
    case 3:
      out.raw('{');
      out.line(depth + 1);
      out.raw(_jsonString("pickup"));
      out.raw(': ');
      tablePickupEventToJsonBody(out, v.pickup, depth + 1, sh);
      out.line(depth);
      out.raw('}');
      return;
  }
  throw StateError('union TableEvent: tag ${v.armTag} names no arm — the writer refuses it (docs/SPEC-TABLES.md §5)');
}

TableEvent _tableEventFromJson(dynamic val, TableTextReport r, _Share sh) {
  final v = TableEvent();
  if (val is! Map) { r.kindMismatch++; return v; }
  if (val.isEmpty) return v;
  if (val.length != 1) throw const FormatException('union has two keys');
  final item = val.values.first;
  switch (val.keys.first) {
    case "hit":
      v.armTag = 1;
      v.hit = _tableHitEventFromJsonValue(item, r, sh);
    case "chat":
      v.armTag = 2;
      v.chat = _tableChatEventFromJsonValue(item, r, sh);
    case "pickup":
      v.armTag = 3;
      v.pickup = _tablePickupEventFromJsonValue(item, r, sh);
    default:
      r.unknown++;
  }
  return v;
}

void _tableEventCount(_Share sh, TableEvent v) {
  switch (v.armTag) {
    case 1:
      tableHitEventCount(sh, v.hit);
      break;
    case 2:
      tableChatEventCount(sh, v.chat);
      break;
    case 3:
      tablePickupEventCount(sh, v.pickup);
      break;
  }
}

TableEntity _tableEntityFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = TableEntity();
  if (val is Map<String, dynamic>) {
    tableEntityFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    tableEntityFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

TableStat _tableStatFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = TableStat();
  if (val is Map<String, dynamic>) {
    tableStatFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    tableStatFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

TableHitEvent _tableHitEventFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = TableHitEvent();
  if (val is Map<String, dynamic>) {
    tableHitEventFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    tableHitEventFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

TableChatEvent _tableChatEventFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = TableChatEvent();
  if (val is Map<String, dynamic>) {
    tableChatEventFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    tableChatEventFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

TablePickupEvent _tablePickupEventFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = TablePickupEvent();
  if (val is Map<String, dynamic>) {
    tablePickupEventFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    tablePickupEventFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

TableMixed _tableMixedFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = TableMixed();
  if (val is Map<String, dynamic>) {
    tableMixedFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    tableMixedFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

void tableEntityCount(_Share sh, TableEntity v) {
}

void tableEntityToJsonBody(_JOut out, TableEntity v, int depth, _Share sh, [int? label]) {
  out.raw('{');
  var comma = false;
  if (label != null) {
    out.line(depth + 1);
    out.raw('"&node": ');
    out.raw(label.toString());
    comma = true;
  }
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("entity_id"));
  out.raw(': ');
  out.raw(v.entityId.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("pos_x"));
  out.raw(': ');
  out.raw(v.posX.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("pos_y"));
  out.raw(': ');
  out.raw(v.posY.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("pos_z"));
  out.raw(': ');
  out.raw(v.posZ.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("yaw"));
  out.raw(': ');
  out.raw(v.yaw.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("pitch"));
  out.raw(': ');
  out.raw(v.pitch.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("vel_x"));
  out.raw(': ');
  out.raw(v.velX.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("vel_y"));
  out.raw(': ');
  out.raw(v.velY.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("vel_z"));
  out.raw(': ');
  out.raw(v.velZ.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("health"));
  out.raw(': ');
  out.raw(v.health.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("weapon"));
  out.raw(': ');
  out.raw(_jsonEnum(v.weapon, tableWeaponName, 'TableEntity.weapon'));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("damage"));
  out.raw(': ');
  _tableDamageToJson(out, v.damage, depth + 1);
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("moving"));
  out.raw(': ');
  out.raw(v.moving ? 'true' : 'false');
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("firing"));
  out.raw(': ');
  out.raw(v.firing ? 'true' : 'false');
  if (comma) out.line(depth);
  out.raw('}');
}

void tableEntityFromJsonFields(TableEntity v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
  for (final entry in obj.entries) {
    final key = entry.key;
    final val = entry.value;
    switch (key) {
      case '&node':
        if (!pointer) throw const FormatException('label outside pointer');
        break;
      case "entity_id":
        v.entityId = _jsonIntField(val, r, null, 4095, true);
        break;
      case "pos_x":
        v.posX = _jsonIntField(val, r, -16383, 16383, false);
        break;
      case "pos_y":
        v.posY = _jsonIntField(val, r, -16383, 16383, false);
        break;
      case "pos_z":
        v.posZ = _jsonIntField(val, r, -16383, 16383, false);
        break;
      case "yaw":
        v.yaw = _jsonIntField(val, r, null, 511, true);
        break;
      case "pitch":
        v.pitch = _jsonIntField(val, r, null, 511, true);
        break;
      case "vel_x":
        v.velX = _jsonIntField(val, r, -2048, 2047, false);
        break;
      case "vel_y":
        v.velY = _jsonIntField(val, r, -2048, 2047, false);
        break;
      case "vel_z":
        v.velZ = _jsonIntField(val, r, -2048, 2047, false);
        break;
      case "health":
        v.health = _jsonIntField(val, r, 0, 1000, false);
        break;
      case "weapon":
        v.weapon = tableWeaponFromName(val, r);
        break;
      case "damage":
        v.damage = _tableDamageFromJson(val, r);
        break;
      case "moving":
        if (val is bool) { v.moving = val; } else { r.kindMismatch++; }
        break;
      case "firing":
        if (val is bool) { v.firing = val; } else { r.kindMismatch++; }
        break;
      default:
        r.unknown++;
    }
  }
}

void tableStatCount(_Share sh, TableStat v) {
}

void tableStatToJsonBody(_JOut out, TableStat v, int depth, _Share sh, [int? label]) {
  out.raw('{');
  var comma = false;
  if (label != null) {
    out.line(depth + 1);
    out.raw('"&node": ');
    out.raw(label.toString());
    comma = true;
  }
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("stat_id"));
  out.raw(': ');
  out.raw(v.statId.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("delta"));
  out.raw(': ');
  out.raw(v.delta.toString());
  if (comma) out.line(depth);
  out.raw('}');
}

void tableStatFromJsonFields(TableStat v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
  for (final entry in obj.entries) {
    final key = entry.key;
    final val = entry.value;
    switch (key) {
      case '&node':
        if (!pointer) throw const FormatException('label outside pointer');
        break;
      case "stat_id":
        v.statId = _jsonIntField(val, r, null, 255, true);
        break;
      case "delta":
        v.delta = _jsonIntField(val, r, -512, 511, false);
        break;
      default:
        r.unknown++;
    }
  }
}

void tableHitEventCount(_Share sh, TableHitEvent v) {
}

void tableHitEventToJsonBody(_JOut out, TableHitEvent v, int depth, _Share sh, [int? label]) {
  out.raw('{');
  var comma = false;
  if (label != null) {
    out.line(depth + 1);
    out.raw('"&node": ');
    out.raw(label.toString());
    comma = true;
  }
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("target_id"));
  out.raw(': ');
  out.raw(v.targetId.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("damage"));
  out.raw(': ');
  out.raw(v.damage.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("hit_kind"));
  out.raw(': ');
  out.raw(v.hitKind.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("crit"));
  out.raw(': ');
  out.raw(v.crit ? 'true' : 'false');
  if (comma) out.line(depth);
  out.raw('}');
}

void tableHitEventFromJsonFields(TableHitEvent v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
  for (final entry in obj.entries) {
    final key = entry.key;
    final val = entry.value;
    switch (key) {
      case '&node':
        if (!pointer) throw const FormatException('label outside pointer');
        break;
      case "target_id":
        v.targetId = _jsonIntField(val, r, null, 4095, true);
        break;
      case "damage":
        v.damage = _jsonIntField(val, r, 0, 4095, false);
        break;
      case "hit_kind":
        v.hitKind = _jsonIntField(val, r, 0, 7, false);
        break;
      case "crit":
        if (val is bool) { v.crit = val; } else { r.kindMismatch++; }
        break;
      default:
        r.unknown++;
    }
  }
}

void tableChatEventCount(_Share sh, TableChatEvent v) {
}

void tableChatEventToJsonBody(_JOut out, TableChatEvent v, int depth, _Share sh, [int? label]) {
  out.raw('{');
  var comma = false;
  if (label != null) {
    out.line(depth + 1);
    out.raw('"&node": ');
    out.raw(label.toString());
    comma = true;
  }
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("channel"));
  out.raw(': ');
  out.raw(v.channel.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("speaker"));
  out.raw(': ');
  out.raw(v.speaker.toString());
  if (comma) out.line(depth);
  out.raw('}');
}

void tableChatEventFromJsonFields(TableChatEvent v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
  for (final entry in obj.entries) {
    final key = entry.key;
    final val = entry.value;
    switch (key) {
      case '&node':
        if (!pointer) throw const FormatException('label outside pointer');
        break;
      case "channel":
        v.channel = _jsonIntField(val, r, 0, 3, false);
        break;
      case "speaker":
        v.speaker = _jsonIntField(val, r, null, 4095, true);
        break;
      default:
        r.unknown++;
    }
  }
}

void tablePickupEventCount(_Share sh, TablePickupEvent v) {
}

void tablePickupEventToJsonBody(_JOut out, TablePickupEvent v, int depth, _Share sh, [int? label]) {
  out.raw('{');
  var comma = false;
  if (label != null) {
    out.line(depth + 1);
    out.raw('"&node": ');
    out.raw(label.toString());
    comma = true;
  }
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("item_id"));
  out.raw(': ');
  out.raw(v.itemId.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("amount"));
  out.raw(': ');
  out.raw(v.amount.toString());
  if (comma) out.line(depth);
  out.raw('}');
}

void tablePickupEventFromJsonFields(TablePickupEvent v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
  for (final entry in obj.entries) {
    final key = entry.key;
    final val = entry.value;
    switch (key) {
      case '&node':
        if (!pointer) throw const FormatException('label outside pointer');
        break;
      case "item_id":
        v.itemId = _jsonIntField(val, r, null, 1023, true);
        break;
      case "amount":
        v.amount = _jsonIntField(val, r, 0, 255, false);
        break;
      default:
        r.unknown++;
    }
  }
}

void tableMixedCount(_Share sh, TableMixed v) {
  for (final e in v.entities) { tableEntityCount(sh, e); }
  for (final e in v.stats) { tableStatCount(sh, e); }
  _tableEventCount(sh, v.gameEvent);
}

void tableMixedToJsonBody(_JOut out, TableMixed v, int depth, _Share sh, [int? label]) {
  out.raw('{');
  var comma = false;
  if (label != null) {
    out.line(depth + 1);
    out.raw('"&node": ');
    out.raw(label.toString());
    comma = true;
  }
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("protocol_magic"));
  out.raw(': ');
  out.raw(v.protocolMagic.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("sequence"));
  out.raw(': ');
  out.raw(v.sequence.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("ack_sequence"));
  out.raw(': ');
  out.raw(v.ackSequence.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("ack_bits"));
  out.raw(': ');
  out.raw(v.ackBits.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("session_id"));
  out.raw(': ');
  out.raw(BigInt.from(v.sessionId).toUnsigned(64).toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("client_id"));
  out.raw(': ');
  out.raw(v.clientId.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("nonce"));
  out.raw(': ');
  out.raw(BigInt.from(v.nonce).toUnsigned(64).toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("world_time"));
  out.raw(': ');
  out.raw(v.worldTime.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("frame_tick"));
  out.raw(': ');
  out.raw(v.frameTick.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("server_time"));
  out.raw(': ');
  out.raw(_jsonFloat(v.serverTime, true));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("entities"));
  out.raw(': ');
  if (v.entities.isEmpty) {
    out.raw('[]');
  } else {
    out.raw('[');
    var first = true;
    for (final e in v.entities) {
      if (!first) { out.raw(','); }
      first = false;
      out.line(depth + 2);
      tableEntityToJsonBody(out, e, depth + 2, sh);
    }
    out.line(depth + 1);
    out.raw(']');
  }
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("stats"));
  out.raw(': ');
  if (v.stats.isEmpty) {
    out.raw('[]');
  } else {
    out.raw('[');
    var first = true;
    for (final e in v.stats) {
      if (!first) { out.raw(','); }
      first = false;
      out.line(depth + 2);
      tableStatToJsonBody(out, e, depth + 2, sh);
    }
    out.line(depth + 1);
    out.raw(']');
  }
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("game_event"));
  out.raw(': ');
  _tableEventToJson(out, v.gameEvent, depth + 1, sh);
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("loadout"));
  out.raw(': ');
  if (v.loadout.isEmpty) {
    out.raw('[]');
  } else {
    out.raw('[');
    var first1 = true;
    for (final e1 in v.loadout) {
      if (!first1) out.raw(',');
      first1 = false;
      out.line(depth + 2);
      out.raw(e1.toString());
    }
    out.line(depth + 1);
    out.raw(']');
  }
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("player_name"));
  out.raw(': ');
  out.raw(_jsonString(v.playerName));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("payload"));
  out.raw(': ');
  out.raw(_jsonString(base64Encode(latin1.encode(v.payload))));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("aim_x"));
  out.raw(': ');
  out.raw(_jsonFloat(v.aimX, true));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("aim_y"));
  out.raw(': ');
  out.raw(_jsonFloat(v.aimY, true));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("aim_z"));
  out.raw(': ');
  out.raw(_jsonFloat(v.aimZ, true));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("recoil"));
  out.raw(': ');
  out.raw(_jsonFloat(v.recoil, true));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("drift"));
  out.raw(': ');
  out.raw(_jsonFloat(v.drift, false));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("wide_key"));
  out.raw(': ');
  out.raw(BigInt.from(v.wideKey).toUnsigned(64).toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("flux"));
  out.raw(': ');
  out.raw(v.flux.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("ping"));
  out.raw(': ');
  out.raw(_jsonFloat(v.ping, true));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("crc_hint"));
  out.raw(': ');
  out.raw(v.crcHint.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("has_extra"));
  out.raw(': ');
  out.raw(v.hasExtra ? 'true' : 'false');
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("extra"));
  out.raw(': ');
  out.raw(v.extra.toString());
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("idle_ticks"));
  out.raw(': ');
  out.raw(v.idleTicks.toString());
  if (comma) out.line(depth);
  out.raw('}');
}

void tableMixedFromJsonFields(TableMixed v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
  for (final entry in obj.entries) {
    final key = entry.key;
    final val = entry.value;
    switch (key) {
      case '&node':
        if (!pointer) throw const FormatException('label outside pointer');
        break;
      case "protocol_magic":
        v.protocolMagic = _jsonIntField(val, r, 0, 65535, true);
        break;
      case "sequence":
        v.sequence = _jsonIntField(val, r, null, 65535, true);
        break;
      case "ack_sequence":
        v.ackSequence = _jsonIntField(val, r, 0, 65535, false);
        break;
      case "ack_bits":
        v.ackBits = _jsonIntField(val, r, null, 4294967295, true);
        break;
      case "session_id":
        v.sessionId = _jsonIntField(val, r, null, null, true);
        break;
      case "client_id":
        v.clientId = _jsonIntField(val, r, 0, 4294967295, true);
        break;
      case "nonce":
        v.nonce = _jsonIntField(val, r, 1, 9223372036854775807, true);
        break;
      case "world_time":
        v.worldTime = _jsonIntField(val, r, -1000000000000, 1000000000000, false);
        break;
      case "frame_tick":
        v.frameTick = _jsonIntField(val, r, null, 281474976710655, true);
        break;
      case "server_time":
        v.serverTime = _jsonFloatField(val, r, true);
        break;
      case "entities":
        if (val is! List) { r.kindMismatch++; break; }
        v.entities = <TableEntity>[];
        for (var index = 0; index < val.length; index++) {
          if (v.entities.length >= 8) { r.clamped += val.length - index; break; }
          final item = val[index];
          if (item is Map<String, dynamic>) {
            final e = TableEntity();
            tableEntityFromJsonFields(e, item, r, sh);
            v.entities.add(e);
          } else if (item is Map) {
            final e = TableEntity();
            tableEntityFromJsonFields(e, Map<String, dynamic>.from(item), r, sh);
            v.entities.add(e);
          } else {
            r.kindMismatch++;
          }
        }
        break;
      case "stats":
        if (val is! List) { r.kindMismatch++; break; }
        v.stats = <TableStat>[];
        for (var index = 0; index < val.length; index++) {
          if (v.stats.length >= 80) { r.clamped += val.length - index; break; }
          final item = val[index];
          if (item is Map<String, dynamic>) {
            final e = TableStat();
            tableStatFromJsonFields(e, item, r, sh);
            v.stats.add(e);
          } else if (item is Map) {
            final e = TableStat();
            tableStatFromJsonFields(e, Map<String, dynamic>.from(item), r, sh);
            v.stats.add(e);
          } else {
            r.kindMismatch++;
          }
        }
        break;
      case "game_event":
        v.gameEvent = _tableEventFromJson(val, r, sh);
        break;
      case "loadout":
        v.loadout = _jsonFixed<int>(val, r, 4, () => 0, (i1) => _jsonIntField(i1, r, null, null, true));
        break;
      case "player_name":
        v.playerName = _jsonStringField(val, 15, r);
        break;
      case "payload":
        v.payload = _jsonBytesField(val, r, 16);
        break;
      case "aim_x":
        v.aimX = _jsonFloatField(val, r, true);
        break;
      case "aim_y":
        v.aimY = _jsonFloatField(val, r, true);
        break;
      case "aim_z":
        v.aimZ = _jsonFloatField(val, r, true);
        break;
      case "recoil":
        v.recoil = _jsonFloatField(val, r, true);
        break;
      case "drift":
        v.drift = _jsonFloatField(val, r, false);
        break;
      case "wide_key":
        v.wideKey = _jsonIntField(val, r, null, null, true);
        break;
      case "flux":
        v.flux = _jsonIntField(val, r, -1000000000000000000, 1000000000000000000, false);
        break;
      case "ping":
        v.ping = _jsonFloatField(val, r, true);
        break;
      case "crc_hint":
        v.crcHint = _jsonIntField(val, r, null, 16777215, true);
        break;
      case "has_extra":
        if (val is bool) { v.hasExtra = val; } else { r.kindMismatch++; }
        break;
      case "extra":
        v.extra = _jsonIntField(val, r, 0, 255, false);
        break;
      case "idle_ticks":
        v.idleTicks = _jsonIntField(val, r, 0, 15, false);
        break;
      default:
        r.unknown++;
    }
  }
}

String tableEntityToJson(TableEntity v) {
  final sh = _Share();
  tableEntityCount(sh, v);
  final out = _JOut();
  tableEntityToJsonBody(out, v, 0, sh);
  out.raw('\n');
  return out.toString();
}

TableEntity tableEntityFromJson(String text, [TableTextReport? report]) {
  final r = report ?? TableTextReport();
  final v = TableEntity();
  final root = _jsonDecodeExact(_jsonPrepare(text, r));
  if (root is Map<String, dynamic>) {
    tableEntityFromJsonFields(v, root, r, _Share());
  } else if (root is Map) {
    tableEntityFromJsonFields(v, Map<String, dynamic>.from(root), r, _Share());
  }
  return v;
}

String tableStatToJson(TableStat v) {
  final sh = _Share();
  tableStatCount(sh, v);
  final out = _JOut();
  tableStatToJsonBody(out, v, 0, sh);
  out.raw('\n');
  return out.toString();
}

TableStat tableStatFromJson(String text, [TableTextReport? report]) {
  final r = report ?? TableTextReport();
  final v = TableStat();
  final root = _jsonDecodeExact(_jsonPrepare(text, r));
  if (root is Map<String, dynamic>) {
    tableStatFromJsonFields(v, root, r, _Share());
  } else if (root is Map) {
    tableStatFromJsonFields(v, Map<String, dynamic>.from(root), r, _Share());
  }
  return v;
}

String tableMixedToJson(TableMixed v) {
  final sh = _Share();
  tableMixedCount(sh, v);
  final out = _JOut();
  tableMixedToJsonBody(out, v, 0, sh);
  out.raw('\n');
  return out.toString();
}

TableMixed tableMixedFromJson(String text, [TableTextReport? report]) {
  final r = report ?? TableTextReport();
  final v = TableMixed();
  final root = _jsonDecodeExact(_jsonPrepare(text, r));
  if (root is Map<String, dynamic>) {
    tableMixedFromJsonFields(v, root, r, _Share());
  } else if (root is Map) {
    tableMixedFromJsonFields(v, Map<String, dynamic>.from(root), r, _Share());
  }
  return v;
}

// THE DESCRIPTORS (docs/SPEC-TABLES.md §8.1, §8.3): one TableFieldInfo
// per carried field, with its storage accessors, annotation and wire
// identity; one TableTypeInfo per table. The optional unit registry
// adds declarations outside the table-wire closure.
final class TableFieldInfo {
  final String name;
  final String json, typeName, doc;
  final int id;
  final int kind;
  final String shape;
  final int? bound;
  final int? min;
  final int? max;
  final bool optional;
  final int? elemSize;
  final List<String> tags;
  final dynamic Function(dynamic) read;
  final void Function(dynamic, dynamic) write;
  const TableFieldInfo(this.name, this.kind, this.shape, this.bound, this.min, this.max, this.json, this.typeName, this.id, this.optional, this.elemSize, this.read, this.write, this.doc, this.tags);
  static dynamic _r0(dynamic v) => v.entityId; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w0(dynamic v, dynamic x) { v.entityId = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r1(dynamic v) => v.posX; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w1(dynamic v, dynamic x) { v.posX = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r2(dynamic v) => v.posY; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w2(dynamic v, dynamic x) { v.posY = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r3(dynamic v) => v.posZ; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w3(dynamic v, dynamic x) { v.posZ = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r4(dynamic v) => v.yaw; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w4(dynamic v, dynamic x) { v.yaw = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r5(dynamic v) => v.pitch; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w5(dynamic v, dynamic x) { v.pitch = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r6(dynamic v) => v.velX; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w6(dynamic v, dynamic x) { v.velX = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r7(dynamic v) => v.velY; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w7(dynamic v, dynamic x) { v.velY = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r8(dynamic v) => v.velZ; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w8(dynamic v, dynamic x) { v.velZ = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r9(dynamic v) => v.health; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w9(dynamic v, dynamic x) { v.health = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r10(dynamic v) => v.weapon; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w10(dynamic v, dynamic x) { v.weapon = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r11(dynamic v) => v.damage; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w11(dynamic v, dynamic x) { v.damage = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r12(dynamic v) => v.moving; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w12(dynamic v, dynamic x) { v.moving = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r13(dynamic v) => v.firing; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w13(dynamic v, dynamic x) { v.firing = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r14(dynamic v) => v.statId; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w14(dynamic v, dynamic x) { v.statId = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r15(dynamic v) => v.delta; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w15(dynamic v, dynamic x) { v.delta = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r16(dynamic v) => v.targetId; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w16(dynamic v, dynamic x) { v.targetId = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r17(dynamic v) => v.damage; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w17(dynamic v, dynamic x) { v.damage = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r18(dynamic v) => v.hitKind; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w18(dynamic v, dynamic x) { v.hitKind = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r19(dynamic v) => v.crit; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w19(dynamic v, dynamic x) { v.crit = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r20(dynamic v) => v.channel; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w20(dynamic v, dynamic x) { v.channel = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r21(dynamic v) => v.speaker; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w21(dynamic v, dynamic x) { v.speaker = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r22(dynamic v) => v.itemId; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w22(dynamic v, dynamic x) { v.itemId = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r23(dynamic v) => v.amount; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w23(dynamic v, dynamic x) { v.amount = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r24(dynamic v) => v.protocolMagic; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w24(dynamic v, dynamic x) { v.protocolMagic = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r25(dynamic v) => v.sequence; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w25(dynamic v, dynamic x) { v.sequence = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r26(dynamic v) => v.ackSequence; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w26(dynamic v, dynamic x) { v.ackSequence = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r27(dynamic v) => v.ackBits; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w27(dynamic v, dynamic x) { v.ackBits = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r28(dynamic v) => v.sessionId; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w28(dynamic v, dynamic x) { v.sessionId = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r29(dynamic v) => v.clientId; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w29(dynamic v, dynamic x) { v.clientId = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r30(dynamic v) => v.nonce; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w30(dynamic v, dynamic x) { v.nonce = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r31(dynamic v) => v.worldTime; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w31(dynamic v, dynamic x) { v.worldTime = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r32(dynamic v) => v.frameTick; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w32(dynamic v, dynamic x) { v.frameTick = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r33(dynamic v) => v.serverTime; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w33(dynamic v, dynamic x) { v.serverTime = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r34(dynamic v) => v.entities; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w34(dynamic v, dynamic x) { v.entities = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r35(dynamic v) => v.stats; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w35(dynamic v, dynamic x) { v.stats = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r36(dynamic v) => v.gameEvent; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w36(dynamic v, dynamic x) { v.gameEvent = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r37(dynamic v) => v.loadout; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w37(dynamic v, dynamic x) { v.loadout = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r38(dynamic v) => v.playerName; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w38(dynamic v, dynamic x) { v.playerName = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r39(dynamic v) => v.payload; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w39(dynamic v, dynamic x) { v.payload = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r40(dynamic v) => v.aimX; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w40(dynamic v, dynamic x) { v.aimX = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r41(dynamic v) => v.aimY; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w41(dynamic v, dynamic x) { v.aimY = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r42(dynamic v) => v.aimZ; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w42(dynamic v, dynamic x) { v.aimZ = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r43(dynamic v) => v.recoil; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w43(dynamic v, dynamic x) { v.recoil = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r44(dynamic v) => v.drift; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w44(dynamic v, dynamic x) { v.drift = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r45(dynamic v) => v.wideKey; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w45(dynamic v, dynamic x) { v.wideKey = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r46(dynamic v) => v.flux; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w46(dynamic v, dynamic x) { v.flux = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r47(dynamic v) => v.ping; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w47(dynamic v, dynamic x) { v.ping = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r48(dynamic v) => v.crcHint; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w48(dynamic v, dynamic x) { v.crcHint = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r49(dynamic v) => v.hasExtra; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w49(dynamic v, dynamic x) { v.hasExtra = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r50(dynamic v) => v.extra; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w50(dynamic v, dynamic x) { v.extra = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static dynamic _r51(dynamic v) => v.idleTicks; // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
  static void _w51(dynamic v, dynamic x) { v.idleTicks = x; } // ignore: avoid_dynamic_calls -- the descriptor registry is type-agnostic by design
}

final class TableTypeInfo {
  final String name;
  final String doc;
  final List<String> tags;
  final List<TableFieldInfo> fields;
  const TableTypeInfo(this.name, this.fields, this.doc, this.tags);
  static const Map<String, TableTypeInfo> _all = <String, TableTypeInfo>{
    'TableEntity': TableTypeInfo('TableEntity', <TableFieldInfo>[
      TableFieldInfo("entity_id", 7, "value", null, null, null, "entity_id", "bits(12)", 2593226102015223570, false, 4, TableFieldInfo._r0, TableFieldInfo._w0, "", <String>[]),
      TableFieldInfo("pos_x", 4, "value", null, -16383, 16383, "pos_x", "int32", -3797881157997612786, false, 4, TableFieldInfo._r1, TableFieldInfo._w1, "", <String>[]),
      TableFieldInfo("pos_y", 4, "value", null, -16383, 16383, "pos_y", "int32", -3797880058485984575, false, 4, TableFieldInfo._r2, TableFieldInfo._w2, "", <String>[]),
      TableFieldInfo("pos_z", 4, "value", null, -16383, 16383, "pos_z", "int32", -3797883357020869208, false, 4, TableFieldInfo._r3, TableFieldInfo._w3, "", <String>[]),
      TableFieldInfo("yaw", 7, "value", null, null, null, "yaw", "bits(9)", -5382489739573192984, false, 4, TableFieldInfo._r4, TableFieldInfo._w4, "", <String>[]),
      TableFieldInfo("pitch", 7, "value", null, null, null, "pitch", "bits(9)", 6028620492700434865, false, 4, TableFieldInfo._r5, TableFieldInfo._w5, "", <String>[]),
      TableFieldInfo("vel_x", 4, "value", null, -2048, 2047, "vel_x", "int32", 7849183398884077159, false, 4, TableFieldInfo._r6, TableFieldInfo._w6, "", <String>[]),
      TableFieldInfo("vel_y", 4, "value", null, -2048, 2047, "vel_y", "int32", 7849182299372448948, false, 4, TableFieldInfo._r7, TableFieldInfo._w7, "", <String>[]),
      TableFieldInfo("vel_z", 4, "value", null, -2048, 2047, "vel_z", "int32", 7849185597907333581, false, 4, TableFieldInfo._r8, TableFieldInfo._w8, "", <String>[]),
      TableFieldInfo("health", 4, "value", null, 0, 1000, "health", "int32", 9181103189905877455, false, 4, TableFieldInfo._r9, TableFieldInfo._w9, "", <String>[]),
      TableFieldInfo("weapon", 30, "value", null, null, null, "weapon", "TableWeapon", -6866282850657997311, false, 1, TableFieldInfo._r10, TableFieldInfo._w10, "", <String>[]),
      TableFieldInfo("damage", 9, "value", null, null, null, "damage", "TableDamage", 9179190079975030720, false, 8, TableFieldInfo._r11, TableFieldInfo._w11, "", <String>[]),
      TableFieldInfo("moving", 1, "value", null, null, null, "moving", "bool", 1271228688681418151, false, 1, TableFieldInfo._r12, TableFieldInfo._w12, "", <String>[]),
      TableFieldInfo("firing", 1, "value", null, null, null, "firing", "bool", 8535675692938965450, false, 1, TableFieldInfo._r13, TableFieldInfo._w13, "", <String>[]),
    ], "", <String>[]),
    'TableStat': TableTypeInfo('TableStat', <TableFieldInfo>[
      TableFieldInfo("stat_id", 6, "value", null, null, null, "stat_id", "bits(8)", -9175110139929313435, false, 4, TableFieldInfo._r14, TableFieldInfo._w14, "", <String>[]),
      TableFieldInfo("delta", 4, "value", null, -512, 511, "delta", "int32", 5910805692604981441, false, 4, TableFieldInfo._r15, TableFieldInfo._w15, "", <String>[]),
    ], "", <String>[]),
    'TableHitEvent': TableTypeInfo('TableHitEvent', <TableFieldInfo>[
      TableFieldInfo("target_id", 7, "value", null, null, null, "target_id", "bits(12)", -5207116919359778736, false, 4, TableFieldInfo._r16, TableFieldInfo._w16, "", <String>[]),
      TableFieldInfo("damage", 4, "value", null, 0, 4095, "damage", "int32", 9179190079975030720, false, 4, TableFieldInfo._r17, TableFieldInfo._w17, "", <String>[]),
      TableFieldInfo("hit_kind", 4, "value", null, 0, 7, "hit_kind", "int32", 142922654710085925, false, 4, TableFieldInfo._r18, TableFieldInfo._w18, "", <String>[]),
      TableFieldInfo("crit", 1, "value", null, null, null, "crit", "bool", 1324453635955533101, false, 1, TableFieldInfo._r19, TableFieldInfo._w19, "", <String>[]),
    ], "", <String>[]),
    'TableChatEvent': TableTypeInfo('TableChatEvent', <TableFieldInfo>[
      TableFieldInfo("channel", 4, "value", null, 0, 3, "channel", "int32", -6556890747741999708, false, 4, TableFieldInfo._r20, TableFieldInfo._w20, "", <String>[]),
      TableFieldInfo("speaker", 7, "value", null, null, null, "speaker", "bits(12)", -292263051557875678, false, 4, TableFieldInfo._r21, TableFieldInfo._w21, "", <String>[]),
    ], "", <String>[]),
    'TablePickupEvent': TableTypeInfo('TablePickupEvent', <TableFieldInfo>[
      TableFieldInfo("item_id", 7, "value", null, null, null, "item_id", "bits(10)", -7025667724851298986, false, 4, TableFieldInfo._r22, TableFieldInfo._w22, "", <String>[]),
      TableFieldInfo("amount", 4, "value", null, 0, 255, "amount", "int32", -9145686598410475159, false, 4, TableFieldInfo._r23, TableFieldInfo._w23, "", <String>[]),
    ], "", <String>[]),
    'TableMixed': TableTypeInfo('TableMixed', <TableFieldInfo>[
      TableFieldInfo("protocol_magic", 7, "value", null, null, null, "protocol_magic", "uint16", 7663561793681298941, false, 2, TableFieldInfo._r24, TableFieldInfo._w24, "", <String>[]),
      TableFieldInfo("sequence", 7, "value", null, null, null, "sequence", "bits(16)", -6181000666011064152, false, 4, TableFieldInfo._r25, TableFieldInfo._w25, "", <String>[]),
      TableFieldInfo("ack_sequence", 4, "value", null, 0, 65535, "ack_sequence", "int32", 990229364654832702, false, 4, TableFieldInfo._r26, TableFieldInfo._w26, "", <String>[]),
      TableFieldInfo("ack_bits", 8, "value", null, null, null, "ack_bits", "bits(32)", -7228825333587644925, false, 4, TableFieldInfo._r27, TableFieldInfo._w27, "", <String>[]),
      TableFieldInfo("session_id", 9, "value", null, null, null, "session_id", "uint64", -5199487799206016251, false, 8, TableFieldInfo._r28, TableFieldInfo._w28, "", <String>[]),
      TableFieldInfo("client_id", 8, "value", null, null, null, "client_id", "uint32", 7889067272199050878, false, 4, TableFieldInfo._r29, TableFieldInfo._w29, "", <String>[]),
      TableFieldInfo("nonce", 9, "value", null, 1, 9223372036854775807, "nonce", "uint64", 8334276637232054488, false, 8, TableFieldInfo._r30, TableFieldInfo._w30, "", <String>[]),
      TableFieldInfo("world_time", 5, "value", null, -1000000000000, 1000000000000, "world_time", "int64", 4534679873638431877, false, 8, TableFieldInfo._r31, TableFieldInfo._w31, "", <String>[]),
      TableFieldInfo("frame_tick", 9, "value", null, null, null, "frame_tick", "bits(48)", 8916005070914257170, false, 8, TableFieldInfo._r32, TableFieldInfo._w32, "", <String>[]),
      TableFieldInfo("server_time", 10, "value", null, null, null, "server_time", "float32", 4343163795393112518, false, 4, TableFieldInfo._r33, TableFieldInfo._w33, "", <String>[]),
      TableFieldInfo("entities", 14, "value", null, null, null, "entities", "TableEntity", -7828083326636031446, false, null, TableFieldInfo._r34, TableFieldInfo._w34, "", <String>[]),
      TableFieldInfo("stats", 14, "value", null, null, null, "stats", "TableStat", -1268998401975805620, false, null, TableFieldInfo._r35, TableFieldInfo._w35, "", <String>[]),
      TableFieldInfo("game_event", 15, "value", null, null, null, "game_event", "TableEvent", 3329809749115557776, false, null, TableFieldInfo._r36, TableFieldInfo._w36, "", <String>[]),
      TableFieldInfo("loadout", 14, "value", null, null, null, "loadout", "uint8", 6294288958393922979, false, 1, TableFieldInfo._r37, TableFieldInfo._w37, "", <String>[]),
      TableFieldInfo("player_name", 12, "value", 15, null, null, "player_name", "string", 1415871171432867950, false, 1, TableFieldInfo._r38, TableFieldInfo._w38, "", <String>[]),
      TableFieldInfo("payload", 14, "value", null, null, null, "payload", "bytes", -3478843999652550171, false, 1, TableFieldInfo._r39, TableFieldInfo._w39, "", <String>[]),
      TableFieldInfo("aim_x", 10, "value", null, null, null, "aim_x", "float32", -2616736631205226807, false, 4, TableFieldInfo._r40, TableFieldInfo._w40, "", <String>[]),
      TableFieldInfo("aim_y", 10, "value", null, null, null, "aim_y", "float32", -2616737730716855018, false, 4, TableFieldInfo._r41, TableFieldInfo._w41, "", <String>[]),
      TableFieldInfo("aim_z", 10, "value", null, null, null, "aim_z", "float32", -2616738830228483229, false, 4, TableFieldInfo._r42, TableFieldInfo._w42, "", <String>[]),
      TableFieldInfo("recoil", 10, "value", null, null, null, "recoil", "float32", -7138431908913683369, false, 4, TableFieldInfo._r43, TableFieldInfo._w43, "", <String>[]),
      TableFieldInfo("drift", 11, "value", null, null, null, "drift", "float64", 6535842126802938884, false, 8, TableFieldInfo._r44, TableFieldInfo._w44, "", <String>[]),
      TableFieldInfo("wide_key", 9, "value", null, null, null, "wide_key", "uint64", -6686572760759554282, false, 8, TableFieldInfo._r45, TableFieldInfo._w45, "", <String>[]),
      TableFieldInfo("flux", 5, "value", null, -1000000000000000000, 1000000000000000000, "flux", "int64", -3018575613315307966, false, 8, TableFieldInfo._r46, TableFieldInfo._w46, "", <String>[]),
      TableFieldInfo("ping", 10, "value", null, null, null, "ping", "float32", -4669986463835551831, false, 4, TableFieldInfo._r47, TableFieldInfo._w47, "", <String>[]),
      TableFieldInfo("crc_hint", 8, "value", null, null, null, "crc_hint", "bits(24)", 6200723483573899615, false, 4, TableFieldInfo._r48, TableFieldInfo._w48, "", <String>[]),
      TableFieldInfo("has_extra", 1, "value", null, null, null, "has_extra", "bool", -4574933642144545166, false, 1, TableFieldInfo._r49, TableFieldInfo._w49, "", <String>[]),
      TableFieldInfo("extra", 4, "value", null, 0, 255, "extra", "int32", -204370544148493463, false, 4, TableFieldInfo._r50, TableFieldInfo._w50, "", <String>[]),
      TableFieldInfo("idle_ticks", 4, "value", null, 0, 15, "idle_ticks", "int32", 8651444298976115966, false, 4, TableFieldInfo._r51, TableFieldInfo._w51, "", <String>[]),
    ], "", <String>[]),
  };
}

Map<String, TableTypeInfo> cardDescriptors() => TableTypeInfo._all;

