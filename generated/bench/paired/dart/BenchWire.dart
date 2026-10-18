// The VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1, in Dart:
// the card record's constructs — load (tolerant, §4) and save (the
// canonical layout the Go, C, C++ and C# backends write), against the
// committed byte oracle tables/cards/fuzz/card-populated.wire.
// ignore_for_file: unused_element

import 'dart:convert';
import 'dart:typed_data';

// The RESERVED node-table id (§3.1, §5) and the *string BLOB type id
// (§2.5, §3.1), both as their 64-bit two's-complement Dart spelling.
const int _kNodeTableId = -1; // 0xFFFFFFFFFFFFFFFF
const int _kBuildVersionId = -2; // 0xFFFFFFFFFFFFFFFE
const int _kVocabularyId = -3; // 0xFFFFFFFFFFFFFFFD
const int _kBytesBlobId = 3399866181213769700; // fnv1a64("bytes")
const int _kStringBlobId = 8091808378024623192; // fnv1a64("string")
const int _kTypeMixedEntity = -7825083188096938557; // fnv1a64("MixedEntity")
const int _kTypeMixedStat = -4311785701174641750; // fnv1a64("MixedStat")
const int _kTypeMixedHitEvent = -6007833022727458345; // fnv1a64("MixedHitEvent")
const int _kTypeMixedChatEvent = -6977028372880211830; // fnv1a64("MixedChatEvent")
const int _kTypeMixedPickupEvent = 4432242110510439820; // fnv1a64("MixedPickupEvent")
const int _kTypeBenchMixed = -1476952163658023588; // fnv1a64("BenchMixed")
const int _kTypeFixedTable = -8800255099513911629; // fnv1a64("FixedTable")

// The report a tolerant read fills: §4's six counters and the refusal
// verdict (§3). The four the card's closure can move are widened, clamped,
// unknown and kindMismatch; malformed and refused are the verdict flags.
final class TableWireReport {
  int unknown = 0;
  int kindMismatch = 0;
  int widened = 0;
  int clamped = 0;
  bool malformed = false;
  bool refused = false;
  int retained = 0;
  int retainLost = 0;
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

  // nodeIndexForBlob numbers one *bytes/*string blob node (§3.1): record k =
  // node index k + 1, keyed by the blob's reserved type id and its bytes. A
  // blob reached twice is one record — two fields that loaded the same node,
  // and so hold the same type and bytes, save the same index, matching the
  // reference backends' first-visit numbering byte for byte (§3.1). A *bytes
  // blob and a *string blob of equal bytes are two nodes: the type id tells
  // them apart.
  int? _findBlob(int typeId, List<int> bytes) {
    for (var i = 0; i < nodes.length; i++) {
      if (nodeObjs[i] != null || nodeTypes[i] != typeId) continue;
      final existing = nodes[i];
      if (existing.length != bytes.length) continue;
      var same = true;
      for (var j = 0; j < bytes.length; j++) {
        if (existing[j] != bytes[j]) {
          same = false;
          break;
        }
      }
      if (same) return i + 2;
    }
    return null;
  }

  // noteBlob numbers one *bytes/*string blob (§3.1). A blob reached twice with
  // the same type id and the same bytes is one record. Numbering does not
  // intern ids — the node-table write does, in record order (§3).
  int noteBlob(int typeId, List<int> bytes) {
    final found = _findBlob(typeId, bytes);
    if (found != null) return found;
    nodeTypes.add(typeId);
    nodes.add(List<int>.from(bytes));
    nodeObjs.add(null);
    return nodes.length + 1;
  }

  int nodeIndexForBlob(int typeId, List<int> bytes) => noteBlob(typeId, bytes);

  int blobIndex(int typeId, List<int> bytes) {
    final found = _findBlob(typeId, bytes);
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

  // resolveBlob turns a kind-17 node index into the blob bytes it names
  // (§3.1): 0 is null; 1 is the ROOT, which a blob can never be, so it is a
  // kind mismatch; records 2.. are the node table's blob records. An index
  // above node_count + 1 is malformed, and a record whose type id this reader
  // cannot name reads null — the unknown was counted once at the node table,
  // not once per pointer (§3.1). recognized is the flag that gates whether
  // this root reaches that blob kind at all.
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
    if (!recognized || nodeTypes[rec] != wantTypeId) return null;
    return Uint8List.fromList(nodes[rec]);
  }

  String? resolveString(int idx) {
    final b = resolveBlob(idx, _kStringBlobId, stringBlob);
    return b == null ? null : utf8.decode(b);
  }

  Uint8List? resolveBytes(int idx) => resolveBlob(idx, _kBytesBlobId, bytesBlob);

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
    _loadNodeBody(sub, wantType, obj);
    if (sub.pos != sub.limit) {
      report.malformed = true;
      decoded[rec] = null;
      return null;
    }
    return obj;
  }

  // open resolves the id table from the END of the wire (§3), then walks the
  // root body's framing to its terminator. 0 ok, 1 refused (a form byte this
  // reader does not carry), 2 damaged (a trailer the reader cannot resolve, or
  // a body that ends early).
  int open() {
    if (data.length == 0) return 2;
    if (data[0] != 1) return 1;
    if (data.length < 9) return 2;
    final count = readU64(data, data.length - 8);
    if (count > (data.length - 9) ~/ 8) return 2;
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
      if (ref == null) return 2;
      if (ref == 0) {
        if (pos != start) return 2;
        return 0;
      }
      if (ref < 1 || ref > entryCount) return 2;
      if (!has(1)) return 2;
      final kind = readByte();
      if (!skipKind(kind)) return 2;
    }
  }

  // scanNodeTable reads the root body's node-table field (§3.1), the reserved
  // id at kind 12, wherever it falls: node_count, then the records. The field
  // under any other kind is malformed and the whole table reads nothing — every
  // pointer in the save is null, one malformed counts (§3.1). A count the scan
  // does not match, a type reference of 0 or above the entry count, or a record
  // length past the payload is framing damage and reads nothing too (§3.1). A
  // record whose type id is the *string blob stores its bytes; any other type
  // id keeps its index, counts one unknown at the record, and every pointer
  // naming it reads null (§3.1). A string blob that is not well-formed UTF-8
  // without NUL is malformed and reads null too.
  void scanNodeTable() {
    pos = 1;
    while (true) {
      final ref = readLeb();
      if (ref == null || ref == 0) break;
      final kind = readByte();
      final id = refToId[ref] ?? 0;
      if (id == _kNodeTableId) {
        if (kind != 12) {
          report.malformed = true;
          nodeTableGood = false;
          break;
        }
        final len = readLeb();
        if (len == null) {
          report.malformed = true;
          nodeTableGood = false;
          break;
        }
        final end = pos + len;
        if (end > limit) {
          report.malformed = true;
          nodeTableGood = false;
          break;
        }
        final count = readLeb();
        if (count == null) {
          report.malformed = true;
          nodeTableGood = false;
          break;
        }
        var read = 0;
        var framed = true;
        while (pos < end) {
          final typeRef = readLeb();
          if (typeRef == null || typeRef < 1 || typeRef > entryCount) {
            framed = false;
            break;
          }
          final blen = readLeb();
          if (blen == null || blen < 0 || blen > end - pos) {
            framed = false;
            break;
          }
          final bytes = Uint8List.fromList(data.sublist(pos, pos + blen));
          pos += blen;
          final typeId = refToId[typeRef] ?? 0;
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
          read++;
        }
        if (!framed || pos != end || read != count) {
          report.malformed = true;
          nodeTableGood = false;
        }
        pos = end;
      } else {
        skipKind(kind);
      }
    }
  }
}

// _kindWidens reports whether a payload kind is a smaller kind on the same
// signed or unsigned ladder, or an f32 read into an f64 (§4). One array whose
// element kind widens counts one widened, not one per element.
bool _kindWidens(int from, int to) {
  return (from >= 2 && from <= 5 && to > from && to <= 5) ||
      (from >= 6 && from <= 9 && to > from && to <= 9) ||
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

final class _RetainRecord {
  final List<int> path;
  final int id;
  final int kind;
  final List<int> resolved;
  _RetainRecord(this.path, this.id, this.kind, this.resolved);
  int cost() => 9 + 8 * (path.length - 1) + resolved.length;
}

// the ids this build can name (docs/SPEC-TABLES.md §6.6): a retained id is by
// definition not here, and takes its entry from the caller's list rather than
// the generated table.
final Set<int> _retainKnown = <int>{80074449652715421, 142922654710085925, 350179810443481718, 912176952613754358, 990229364654832702, 1271228688681418151, 1324453635955533101, 1415871171432867950, 1768712988339112597, 2358437193313989428, 2494154474894420565, 2593226102015223570, 3185847833948484337, 3226873318306908209, 3329809749115557776, 3399866181213769700, 3707351006901076138, 3883575963473318423, 4343163795393112518, 4432242110510439820, 4534679873638431877, 5910805692604981441, 6028620492700434865, 6200723483573899615, 6294288958393922979, 6535842126802938884, 7849182299372448948, 7849183398884077159, 7849185597907333581, 7889067272199050878, 8091808378024623192, 8334276637232054488, 8535675692938965450, 8651444298976115966, 8916005070914257170, 8999596768310594794, 9179190079975030720, 9181103189905877455, -9175110139929313435, -9145686598410475159, -8800255099513911629, -8505234340531936929, -8250835097599916951, -7828083326636031446, -7825083188096938557, -7467029643251279282, -7228825333587644925, -7138431908913683369, -7025667724851298986, -6977028372880211830, -6943525758045472923, -6935561395390217098, -6866282850657997311, -6686572760759554282, -6556890747741999708, -6372330821933150452, -6181000666011064152, -6007833022727458345, -5382489739573192984, -5207116919359778736, -5199487799206016251, -4669986463835551831, -4574933642144545166, -4311785701174641750, -3797883357020869208, -3797881157997612786, -3797880058485984575, -3478843999652550171, -3018575613315307966, -2752528626751932394, -2616738830228483229, -2616737730716855018, -2616736631205226807, -1476952163658023588, -1268998401975805620, -962770241226853237, -292263051557875678, -204370544148493463, -1};

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

// _retainTail re-emits the retained records whose path names the body being
// written, in the order retained, at the end of that body (§6.6). A record
// whose id cannot be interned (the caller's list is full) or whose walk is
// damaged is dropped and counted one retainLost.
void _retainTail(TableWireWriter w) {
  final t = w.state.retain;
  if (t == null) return;
  for (final rec in t.records) {
    if (rec.path.length != w.path.length) continue;
    var same = true;
    for (var i = 0; i < w.path.length; i++) {
      if (rec.path[i] != w.path[i]) {
        same = false;
        break;
      }
    }
    if (!same) continue;
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
    case _kTypeMixedEntity: mixedEntityWireSaveBody(w, obj as MixedEntity); break;
    case _kTypeMixedStat: mixedStatWireSaveBody(w, obj as MixedStat); break;
    case _kTypeMixedHitEvent: mixedHitEventWireSaveBody(w, obj as MixedHitEvent); break;
    case _kTypeMixedChatEvent: mixedChatEventWireSaveBody(w, obj as MixedChatEvent); break;
    case _kTypeMixedPickupEvent: mixedPickupEventWireSaveBody(w, obj as MixedPickupEvent); break;
    case _kTypeBenchMixed: benchMixedWireSaveBody(w, obj as BenchMixed); break;
    case _kTypeFixedTable: fixedTableWireSaveBody(w, obj as FixedTable); break;
    default: throw StateError('no node body for type $typeId (docs/SPEC-TABLES.md §3.1)');
  }
}

Object? _newNode(int typeId) {
  switch (typeId) {
    case _kTypeMixedEntity: return MixedEntity();
    case _kTypeMixedStat: return MixedStat();
    case _kTypeMixedHitEvent: return MixedHitEvent();
    case _kTypeMixedChatEvent: return MixedChatEvent();
    case _kTypeMixedPickupEvent: return MixedPickupEvent();
    case _kTypeBenchMixed: return BenchMixed();
    case _kTypeFixedTable: return FixedTable();
    default: return null;
  }
}

void _loadNodeBody(TableWireReader r, int typeId, Object obj) {
  switch (typeId) {
    case _kTypeMixedEntity: mixedEntityWireLoadBody(r, obj as MixedEntity, true); break;
    case _kTypeMixedStat: mixedStatWireLoadBody(r, obj as MixedStat, true); break;
    case _kTypeMixedHitEvent: mixedHitEventWireLoadBody(r, obj as MixedHitEvent, true); break;
    case _kTypeMixedChatEvent: mixedChatEventWireLoadBody(r, obj as MixedChatEvent, true); break;
    case _kTypeMixedPickupEvent: mixedPickupEventWireLoadBody(r, obj as MixedPickupEvent, true); break;
    case _kTypeBenchMixed: benchMixedWireLoadBody(r, obj as BenchMixed, true); break;
    case _kTypeFixedTable: fixedTableWireLoadBody(r, obj as FixedTable, true); break;
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

final class MixedEntity {
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
  int weapon = 0;
  int damage = 0;
  bool moving = false;
  bool firing = false;
}

final class MixedStat {
  int statId = 0;
  int delta = 0;
}

final class MixedHitEvent {
  int targetId = 0;
  int damage = 0;
  int hitKind = 0;
  bool crit = false;
}

final class MixedChatEvent {
  int channel = 0;
  int speaker = 0;
}

final class MixedPickupEvent {
  int itemId = 0;
  int amount = 0;
}

final class BenchMixed {
  int sequence = 0;
  int ackSequence = 0;
  int ackBits = 0;
  int sessionId = 0;
  int clientId = 0;
  int nonce = 0;
  int worldTime = 0;
  int frameTick = 0;
  BigInt serverTime = BigInt.zero;
  List<MixedEntity> entities = <MixedEntity>[];
  List<MixedStat> stats = <MixedStat>[];
  MixedEvent gameEvent = MixedEvent();
  List<int> loadout = List<int>.generate(4, (_) => 0);
  String playerName = '';
  String payload = '';
  double aimX = 0.0;
  double aimY = 0.0;
  double aimZ = 0.0;
  double recoil = 0.0;
  double drift = 0.0;
  BigInt wideKey = BigInt.zero;
  BigInt flux = BigInt.zero;
  BigInt ping = BigInt.zero;
  int crcHint = 0;
  bool hasExtra = true;
  int extra = 0;
  int idleTicks = 0;
}

final class FixedTable {
  BenchMixed value = BenchMixed();
}

final class MixedEvent {
  int armTag = 0;
  MixedHitEvent hit = MixedHitEvent();
  MixedChatEvent chat = MixedChatEvent();
  MixedPickupEvent pickup = MixedPickupEvent();
}

void _mixedEventSaveArm(TableWireWriter w, MixedEvent v) {
  switch (v.armTag) {
    case 1:
      {
        final ref = w.refFor(3707351006901076138);
        final p = w.sub();
        mixedHitEventWireSaveBody(p, v.hit);
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
        mixedChatEventWireSaveBody(p, v.chat);
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
        mixedPickupEventWireSaveBody(p, v.pickup);
        w.putLeb(ref);
        w.putByte(13);
        w.putLeb(p.buf.length);
        w.putBytes(p.buf);
      }
      break;
    default:
      throw StateError('union MixedEvent: tag ${v.armTag} names no arm — save refuses it (docs/SPEC-TABLES.md §5)');
  }
}

bool _mixedEventArm1(TableWireReader p, MixedEvent v, int kind) {
  final b = MixedHitEvent();
  mixedHitEventWireLoadBody(p, b, true);
  if (p.pos != p.limit) { p.report.malformed = true; return false; }
  v.hit = b;
  return true;
}

bool _mixedEventArm2(TableWireReader p, MixedEvent v, int kind) {
  final b = MixedChatEvent();
  mixedChatEventWireLoadBody(p, b, true);
  if (p.pos != p.limit) { p.report.malformed = true; return false; }
  v.chat = b;
  return true;
}

bool _mixedEventArm3(TableWireReader p, MixedEvent v, int kind) {
  final b = MixedPickupEvent();
  mixedPickupEventWireLoadBody(p, b, true);
  if (p.pos != p.limit) { p.report.malformed = true; return false; }
  v.pickup = b;
  return true;
}

bool _mixedEventLoadArm(TableWireReader r, MixedEvent v) {
  final ref = r.readLeb();
  if (ref == null) { r.report.malformed = true; return false; }
  v.armTag = 0;
  if (ref == 0) return true;
  final armId = r.refToId[ref];
  if (armId == null || !r.has(1)) { r.report.malformed = true; return false; }
  final kind = r.readByte();
  final n = r.readLeb();
  if (n == null || !r.has(n)) { r.report.malformed = true; return false; }
  final p = TableWireReader.sub(r, r.pos, r.pos + n);
  switch (armId) {
    case 3707351006901076138:
      if (kind == 13) {
        if (_mixedEventArm1(p, v, kind)) v.armTag = 1;
      } else {
        r.report.kindMismatch++;
      }
      break;
    case -962770241226853237:
      if (kind == 13) {
        if (_mixedEventArm2(p, v, kind)) v.armTag = 2;
      } else {
        r.report.kindMismatch++;
      }
      break;
    case -6943525758045472923:
      if (kind == 13) {
        if (_mixedEventArm3(p, v, kind)) v.armTag = 3;
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

void _mixedEventNumber(WireState s, MixedEvent v, Set<Object> open) {
  switch (v.armTag) {
    case 1:
      mixedHitEventNumber(s, v.hit, open);
      break;
    case 2:
      mixedChatEventNumber(s, v.chat, open);
      break;
    case 3:
      mixedPickupEventNumber(s, v.pickup, open);
      break;
  }
}

void mixedEntityWireSaveBodyFields(TableWireWriter w, MixedEntity v) {
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
  if (v.weapon != 0) {
    w.putLeb(w.refFor(-6866282850657997311));
    w.putByte(30);
    int vid;
    switch (v.weapon) {
      case 1: vid = -6372330821933150452;
      case 2: vid = -6935561395390217098;
      case 3: vid = 1768712988339112597;
      case 4: vid = 3185847833948484337;
      case 5: vid = 912176952613754358;
      case 6: vid = -7467029643251279282;
      case 7: vid = 2494154474894420565;
      case 8: vid = 80074449652715421;
      case 9: vid = -8505234340531936929;
      case 10: vid = -8250835097599916951;
      case 11: vid = -2752528626751932394;
      case 12: vid = 350179810443481718;
      case 13: vid = 3883575963473318423;
      case 14: vid = 3226873318306908209;
      case 15: vid = 2358437193313989428;
      default: return;
    }
    w.putLeb(w.refFor(vid));
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
  _retainTail(w);
}

void mixedEntityWireSaveBody(TableWireWriter w, MixedEntity v) {
  mixedEntityWireSaveBodyFields(w, v);
  w.putByte(0);
}

void mixedStatWireSaveBodyFields(TableWireWriter w, MixedStat v) {
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
  _retainTail(w);
}

void mixedStatWireSaveBody(TableWireWriter w, MixedStat v) {
  mixedStatWireSaveBodyFields(w, v);
  w.putByte(0);
}

void mixedHitEventWireSaveBodyFields(TableWireWriter w, MixedHitEvent v) {
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
  _retainTail(w);
}

void mixedHitEventWireSaveBody(TableWireWriter w, MixedHitEvent v) {
  mixedHitEventWireSaveBodyFields(w, v);
  w.putByte(0);
}

void mixedChatEventWireSaveBodyFields(TableWireWriter w, MixedChatEvent v) {
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
  _retainTail(w);
}

void mixedChatEventWireSaveBody(TableWireWriter w, MixedChatEvent v) {
  mixedChatEventWireSaveBodyFields(w, v);
  w.putByte(0);
}

void mixedPickupEventWireSaveBodyFields(TableWireWriter w, MixedPickupEvent v) {
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
  _retainTail(w);
}

void mixedPickupEventWireSaveBody(TableWireWriter w, MixedPickupEvent v) {
  mixedPickupEventWireSaveBodyFields(w, v);
  w.putByte(0);
}

void benchMixedWireSaveBodyFields(TableWireWriter w, BenchMixed v) {
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
  if (v.nonce != 0) {
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
  if (v.serverTime != BigInt.zero) {
    w.putLeb(w.refFor(4343163795393112518));
    w.putByte(22);
    _putWide(w, v.serverTime, 4);
  }
  if (v.entities.isNotEmpty) {
    final ref = w.refFor(-7828083326636031446);
    final s = w.sub();
    s.putByte(13);
    s.putLeb(v.entities.length);
    for (var i = 0; i < v.entities.length; i++) {
      final e = v.entities[i];
      final b = s.sub();
      b.path = [...w.path, 9, i];
      mixedEntityWireSaveBody(b, e);
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
      b.path = [...w.path, 10, i];
      mixedStatWireSaveBody(b, e);
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
    _mixedEventSaveArm(w, v.gameEvent);
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
  if (v.aimX != 0.0) {
    w.putLeb(w.refFor(-2616736631205226807));
    w.putByte(10);
    w.putU32(_f32Bits(v.aimX));
  }
  if (v.aimY != 0.0) {
    w.putLeb(w.refFor(-2616737730716855018));
    w.putByte(10);
    w.putU32(_f32Bits(v.aimY));
  }
  if (v.aimZ != 0.0) {
    w.putLeb(w.refFor(-2616738830228483229));
    w.putByte(10);
    w.putU32(_f32Bits(v.aimZ));
  }
  if (v.recoil != 0.0) {
    w.putLeb(w.refFor(-7138431908913683369));
    w.putByte(10);
    w.putU32(_f32Bits(v.recoil));
  }
  if (v.drift != 0.0) {
    w.putLeb(w.refFor(6535842126802938884));
    w.putByte(11);
    w.putU64(_f64Bits(v.drift));
  }
  if (v.wideKey != BigInt.zero) {
    w.putLeb(w.refFor(-6686572760759554282));
    w.putByte(19);
    _putWide(w, v.wideKey, 16);
  }
  if (v.flux != BigInt.zero) {
    w.putLeb(w.refFor(-3018575613315307966));
    w.putByte(18);
    _putWide(w, v.flux, 16);
  }
  if (v.ping != BigInt.zero) {
    w.putLeb(w.refFor(-4669986463835551831));
    w.putByte(26);
    _putWide(w, v.ping, 2);
  }
  if (v.crcHint != 0) {
    w.putLeb(w.refFor(6200723483573899615));
    w.putByte(8);
    w.putU32(v.crcHint);
  }
  if (v.hasExtra != true) {
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
  _retainTail(w);
}

void benchMixedWireSaveBody(TableWireWriter w, BenchMixed v) {
  benchMixedWireSaveBodyFields(w, v);
  w.putByte(0);
}

void fixedTableWireSaveBodyFields(TableWireWriter w, FixedTable v) {
  {
    final mark = w.state.mark();
    final ref = w.refFor(8999596768310594794);
    final b = w.sub();
    benchMixedWireSaveBody(b, v.value);
    if (b.buf.length <= 1) {
      w.state.rollback(mark);
    } else {
      w.putLeb(ref);
      w.putByte(13);
      w.putLeb(b.buf.length);
      w.putBytes(b.buf);
    }
  }
  _retainTail(w);
}

void fixedTableWireSaveBody(TableWireWriter w, FixedTable v) {
  fixedTableWireSaveBodyFields(w, v);
  w.putByte(0);
}

void mixedEntityWireLoadBody(TableWireReader r, MixedEntity v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId) {
      if (nested) { r.report.malformed = true; return; }
      if (!r.skipKind(kind)) { r.report.malformed = true; return; }
      continue;
    }
    switch (id) {
      case 2593226102015223570:
        {
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          if (ref2 == 0) { v.weapon = 0; break; }
          final vid = r.refToId[ref2];
          if (vid == null) { r.report.malformed = true; return; }
          switch (vid) {
            case -6372330821933150452: v.weapon = 1;
            case -6935561395390217098: v.weapon = 2;
            case 1768712988339112597: v.weapon = 3;
            case 3185847833948484337: v.weapon = 4;
            case 912176952613754358: v.weapon = 5;
            case -7467029643251279282: v.weapon = 6;
            case 2494154474894420565: v.weapon = 7;
            case 80074449652715421: v.weapon = 8;
            case -8505234340531936929: v.weapon = 9;
            case -8250835097599916951: v.weapon = 10;
            case -2752528626751932394: v.weapon = 11;
            case 350179810443481718: v.weapon = 12;
            case 3883575963473318423: v.weapon = 13;
            case 3226873318306908209: v.weapon = 14;
            case 2358437193313989428: v.weapon = 15;
            default: v.weapon = 0; r.report.unknown++; if (r.retain != null) r.report.retainLost++;
          }
        }
        break;
      case 9179190079975030720:
        {
          var value = 0;
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
        v.moving = r.readByte() != 0;
        break;
      case 8535675692938965450:
        if (kind != 1) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        if (!r.has(1)) { r.report.malformed = true; return; }
        v.firing = r.readByte() != 0;
        break;
      default:
        r.report.unknown++;
        if (r.retain != null) {
          final start = r.pos;
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
          if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(List<int>.from(r.path), id, kind, resolved);
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

void mixedStatWireLoadBody(TableWireReader r, MixedStat v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId) {
      if (nested) { r.report.malformed = true; return; }
      if (!r.skipKind(kind)) { r.report.malformed = true; return; }
      continue;
    }
    switch (id) {
      case -9175110139929313435:
        {
          var value = 0;
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
          var value = 0;
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
          if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(List<int>.from(r.path), id, kind, resolved);
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

void mixedHitEventWireLoadBody(TableWireReader r, MixedHitEvent v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId) {
      if (nested) { r.report.malformed = true; return; }
      if (!r.skipKind(kind)) { r.report.malformed = true; return; }
      continue;
    }
    switch (id) {
      case -5207116919359778736:
        {
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
        v.crit = r.readByte() != 0;
        break;
      default:
        r.report.unknown++;
        if (r.retain != null) {
          final start = r.pos;
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
          if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(List<int>.from(r.path), id, kind, resolved);
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

void mixedChatEventWireLoadBody(TableWireReader r, MixedChatEvent v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId) {
      if (nested) { r.report.malformed = true; return; }
      if (!r.skipKind(kind)) { r.report.malformed = true; return; }
      continue;
    }
    switch (id) {
      case -6556890747741999708:
        {
          var value = 0;
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
          var value = 0;
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
          if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(List<int>.from(r.path), id, kind, resolved);
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

void mixedPickupEventWireLoadBody(TableWireReader r, MixedPickupEvent v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId) {
      if (nested) { r.report.malformed = true; return; }
      if (!r.skipKind(kind)) { r.report.malformed = true; return; }
      continue;
    }
    switch (id) {
      case -7025667724851298986:
        {
          var value = 0;
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
          var value = 0;
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
          if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(List<int>.from(r.path), id, kind, resolved);
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

bool _benchMixedLoadoutFixed(TableWireReader p, BenchMixed v) {
  if (!p.has(1)) { p.report.malformed = true; return false; }
  final elemKind = p.readByte();
  final count = p.readLeb();
  if (count == null) { p.report.malformed = true; return false; }
  if (elemKind != 6) { p.report.kindMismatch++; return true; }
  final out = <int>[];
  var keep = count;
  if (keep > 4) { keep = 4; p.report.clamped++; }
  for (var i = 0; i < keep; i++) {
    if (!p.has(1)) { p.report.malformed = true; return false; }
    var raw = 0;
    for (var b = 0; b < 1; b++) { raw |= p.readByte() << (8 * b); }
    out.add(raw);
  }
  if (keep == count && p.pos != p.limit) { p.report.malformed = true; return false; }
  final fresh = List<int>.generate(4, (_) => 0);
  for (var i = 0; i < out.length && i < 4; i++) {
    fresh[i] = out[i];
  }
  v.loadout = fresh;
  return true;
}

void benchMixedWireLoadBody(TableWireReader r, BenchMixed v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId) {
      if (nested) { r.report.malformed = true; return; }
      if (!r.skipKind(kind)) { r.report.malformed = true; return; }
      continue;
    }
    switch (id) {
      case -6181000666011064152:
        {
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          var value = 0;
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
          if (_u64Below(value, 0)) { value = 0; r.report.clamped++; }
          if (_u64Above(value, -1)) { value = -1; r.report.clamped++; }
          v.nonce = value;
        }
        break;
      case 4534679873638431877:
        {
          var value = 0;
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
          var value = 0;
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
          BigInt? value;
          switch (kind) {
            case 22:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _getWide(r, 4, true);
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value != null) {
            var x = value;
            if (x < BigInt.zero) { x = BigInt.zero; r.report.clamped++; }
            if (x > BigInt.parse('16776960')) { x = BigInt.parse('16776960'); r.report.clamped++; }
            v.serverTime = x;
          }
        }
        break;
      case -7828083326636031446:
        if (kind != 14) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        {
          final len = r.readLeb();
          if (len == null) { r.report.malformed = true; return; }
          final end = r.pos + len;
          if (end > r.limit) { r.report.malformed = true; return; }
          v.entities = <MixedEntity>[];
          if (end - r.pos < 2) { r.pos = end; break; }
          final arr = TableWireReader.sub(r, r.pos, end);
          final elemKind = arr.readByte();
          final count = arr.readLeb();
          if (count == null) { r.report.malformed = true; r.pos = end; break; }
          if (elemKind != 13) { r.report.kindMismatch++; r.pos = end; break; }
          var keep = count;
          if (keep > 8) { keep = 8; r.report.clamped++; }
          for (var i = 0; i < keep; i++) {
            final elen = arr.readLeb();
            if (elen == null || !arr.has(elen)) { r.report.malformed = true; break; }
            var e = MixedEntity();
            final sub = TableWireReader.sub(arr, arr.pos, arr.pos + elen);
            sub.path = [...r.path, 9, i];
            mixedEntityWireLoadBody(sub, e, true);
            if (sub.pos != sub.limit) { r.report.malformed = true; e = MixedEntity(); }
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
          if (len == null) { r.report.malformed = true; return; }
          final end = r.pos + len;
          if (end > r.limit) { r.report.malformed = true; return; }
          v.stats = <MixedStat>[];
          if (end - r.pos < 2) { r.pos = end; break; }
          final arr = TableWireReader.sub(r, r.pos, end);
          final elemKind = arr.readByte();
          final count = arr.readLeb();
          if (count == null) { r.report.malformed = true; r.pos = end; break; }
          if (elemKind != 13) { r.report.kindMismatch++; r.pos = end; break; }
          var keep = count;
          if (keep > 80) { keep = 80; r.report.clamped++; }
          for (var i = 0; i < keep; i++) {
            final elen = arr.readLeb();
            if (elen == null || !arr.has(elen)) { r.report.malformed = true; break; }
            var e = MixedStat();
            final sub = TableWireReader.sub(arr, arr.pos, arr.pos + elen);
            sub.path = [...r.path, 10, i];
            mixedStatWireLoadBody(sub, e, true);
            if (sub.pos != sub.limit) { r.report.malformed = true; e = MixedStat(); }
            arr.pos = arr.pos + elen;
            v.stats.add(e);
          }
          r.pos = end;
        }
        break;
      case 3329809749115557776:
        if (kind != 15) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        if (!_mixedEventLoadArm(r, v.gameEvent)) { r.report.malformed = true; return; }
        break;
      case 6294288958393922979:
        if (kind != 14) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        {
          final len = r.readLeb();
          if (len == null || !r.has(len)) { r.report.malformed = true; return; }
          _benchMixedLoadoutFixed(TableWireReader.sub(r, r.pos, r.pos + len), v);
          r.pos += len;
        }
        break;
      case 1415871171432867950:
        if (kind != 12) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        {
          final len = r.readLeb();
          if (len == null || !r.has(len)) { r.report.malformed = true; return; }
          if (!_lebValid(r.data, r.pos, len)) { r.report.malformed = true; r.pos += len; break; }
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
          if (len == null) { r.report.malformed = true; return; }
          final end = r.pos + len;
          if (end > r.limit) { r.report.malformed = true; return; }
          if (end - r.pos < 2) { r.pos = end; break; }
          final arr = TableWireReader.sub(r, r.pos, end);
          final elemKind = arr.readByte();
          final count = arr.readLeb();
          if (count == null) { r.report.malformed = true; r.pos = end; break; }
          if (elemKind != 6) { r.report.kindMismatch++; r.pos = end; break; }
          if (!arr.has(count)) { r.report.malformed = true; r.pos = end; break; }
          var keep = count;
          if (keep > 16) { keep = 16; r.report.clamped++; }
          v.payload = latin1.decode(arr.data.sublist(arr.pos, arr.pos + keep));
          r.pos = end;
        }
        break;
      case -2616736631205226807:
        {
          var d = 0.0;
          switch (kind) {
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = _f32FromBits(readU32(r.data, r.pos)); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (d < -1) { d = -1; r.report.clamped++; }
          if (d > 1) { d = 1; r.report.clamped++; }
          v.aimX = d;
        }
        break;
      case -2616737730716855018:
        {
          var d = 0.0;
          switch (kind) {
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = _f32FromBits(readU32(r.data, r.pos)); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (d < -1) { d = -1; r.report.clamped++; }
          if (d > 1) { d = 1; r.report.clamped++; }
          v.aimY = d;
        }
        break;
      case -2616738830228483229:
        {
          var d = 0.0;
          switch (kind) {
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = _f32FromBits(readU32(r.data, r.pos)); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (d < -1) { d = -1; r.report.clamped++; }
          if (d > 1) { d = 1; r.report.clamped++; }
          v.aimZ = d;
        }
        break;
      case -7138431908913683369:
        {
          var d = 0.0;
          switch (kind) {
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = _f32FromBits(readU32(r.data, r.pos)); r.pos += 4;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          v.recoil = d;
        }
        break;
      case 6535842126802938884:
        {
          var d = 0.0;
          switch (kind) {
            case 11:
              if (!r.has(8)) { r.report.malformed = true; return; }
              d = _f64FromBits(readU64(r.data, r.pos)); r.pos += 8;
              break;
            case 10:
              if (!r.has(4)) { r.report.malformed = true; return; }
              d = _f32FromBits(readU32(r.data, r.pos)); r.pos += 4;
              r.report.widened++;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          v.drift = d;
        }
        break;
      case -6686572760759554282:
        {
          BigInt? value;
          switch (kind) {
            case 19:
              if (!r.has(16)) { r.report.malformed = true; return; }
              value = _getWide(r, 16, false);
              break;
            case 6:
              if (!r.has(1)) { r.report.malformed = true; return; }
              value = _getWide(r, 1, false);
              r.report.widened++;
              break;
            case 7:
              if (!r.has(2)) { r.report.malformed = true; return; }
              value = _getWide(r, 2, false);
              r.report.widened++;
              break;
            case 8:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _getWide(r, 4, false);
              r.report.widened++;
              break;
            case 9:
              if (!r.has(8)) { r.report.malformed = true; return; }
              value = _getWide(r, 8, false);
              r.report.widened++;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value != null) {
            var x = value;
            v.wideKey = x;
          }
        }
        break;
      case -3018575613315307966:
        {
          BigInt? value;
          switch (kind) {
            case 18:
              if (!r.has(16)) { r.report.malformed = true; return; }
              value = _getWide(r, 16, true);
              break;
            case 2:
              if (!r.has(1)) { r.report.malformed = true; return; }
              value = _getWide(r, 1, true);
              r.report.widened++;
              break;
            case 3:
              if (!r.has(2)) { r.report.malformed = true; return; }
              value = _getWide(r, 2, true);
              r.report.widened++;
              break;
            case 4:
              if (!r.has(4)) { r.report.malformed = true; return; }
              value = _getWide(r, 4, true);
              r.report.widened++;
              break;
            case 5:
              if (!r.has(8)) { r.report.malformed = true; return; }
              value = _getWide(r, 8, true);
              r.report.widened++;
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value != null) {
            var x = value;
            if (x < BigInt.parse('-1267650600228229401496703205376')) { x = BigInt.parse('-1267650600228229401496703205376'); r.report.clamped++; }
            if (x > BigInt.parse('1267650600228229401496703205376')) { x = BigInt.parse('1267650600228229401496703205376'); r.report.clamped++; }
            v.flux = x;
          }
        }
        break;
      case -4669986463835551831:
        {
          BigInt? value;
          switch (kind) {
            case 26:
              if (!r.has(2)) { r.report.malformed = true; return; }
              value = _getWide(r, 2, false);
              break;
            default:
              r.report.kindMismatch++;
              if (!r.skipKind(kind)) { r.report.malformed = true; return; }
              break;
          }
          if (value != null) {
            var x = value;
            if (x < BigInt.zero) { x = BigInt.zero; r.report.clamped++; }
            if (x > BigInt.parse('64000')) { x = BigInt.parse('64000'); r.report.clamped++; }
            v.ping = x;
          }
        }
        break;
      case 6200723483573899615:
        {
          var value = 0;
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
        v.hasExtra = r.readByte() != 0;
        break;
      case -204370544148493463:
        {
          var value = 0;
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
          var value = 0;
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
          if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(List<int>.from(r.path), id, kind, resolved);
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

void fixedTableWireLoadBody(TableWireReader r, FixedTable v, bool nested) {
  while (true) {
    final ref = r.readLeb();
    if (ref == null) { r.report.malformed = true; return; }
    if (ref == 0) return;
    final id = r.refToId[ref];
    if (id == null) { r.report.malformed = true; return; }
    if (!r.has(1)) { r.report.malformed = true; return; }
    final kind = r.readByte();
    if (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }
    if (id == _kNodeTableId) {
      if (nested) { r.report.malformed = true; return; }
      if (!r.skipKind(kind)) { r.report.malformed = true; return; }
      continue;
    }
    switch (id) {
      case 8999596768310594794:
        if (kind != 13) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }
        {
          final len = r.readLeb();
          if (len == null || !r.has(len)) { r.report.malformed = true; return; }
          final sub = TableWireReader.sub(r, r.pos, r.pos + len);
          benchMixedWireLoadBody(sub, v.value, true);
          if (sub.pos != sub.limit) { r.report.malformed = true; v.value = BenchMixed(); }
          r.pos += len;
        }
        break;
      default:
        r.report.unknown++;
        if (r.retain != null) {
          final start = r.pos;
          if (!r.skipKind(kind)) { r.report.malformed = true; return; }
          if (kind == 17) {
            r.report.retainLost++;
          } else {
            final sub = TableWireReader.sub(r, start, r.pos);
            final resolved = _retainResolve(sub, kind, 0);
            if (resolved == null || sub.pos != sub.limit) {
              r.report.retainLost++;
            } else {
              final rec = _RetainRecord(List<int>.from(r.path), id, kind, resolved);
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

void mixedEntityNumber(WireState s, MixedEntity v, Set<Object> open) {
}

void mixedStatNumber(WireState s, MixedStat v, Set<Object> open) {
}

void mixedHitEventNumber(WireState s, MixedHitEvent v, Set<Object> open) {
}

void mixedChatEventNumber(WireState s, MixedChatEvent v, Set<Object> open) {
}

void mixedPickupEventNumber(WireState s, MixedPickupEvent v, Set<Object> open) {
}

void benchMixedNumber(WireState s, BenchMixed v, Set<Object> open) {
  for (final e in v.entities) {
    mixedEntityNumber(s, e, open);
  }
  for (final e in v.stats) {
    mixedStatNumber(s, e, open);
  }
  _mixedEventNumber(s, v.gameEvent, open);
}

void fixedTableNumber(WireState s, FixedTable v, Set<Object> open) {
  benchMixedNumber(s, v.value, open);
}

Uint8List fixedTableWireSave(FixedTable v) {
  final state = WireState();
  state.indexRoot(v);
  final open = <Object>{v};
  fixedTableNumber(state, v, open);
  final w = TableWireWriter(state);
  w.putByte(1);
  fixedTableWireSaveBodyFields(w, v);
  _writeNodeTable(w);
  w.putByte(0);
  for (final id in state.ids) { w.putU64(id); }
  w.putU64(state.ids.length);
  return Uint8List.fromList(w.buf);
}

FixedTable fixedTableWireLoad(Uint8List data, [TableWireReport? report]) {
  final v = FixedTable();
  final r = TableWireReader(data, data.length);
  r.stringBlob = false;
  r.bytesBlob = false;
  r.path = [1];
  r.rootObj = v;
  r.rootType = _kTypeFixedTable;
  if (report != null) r.report = report;
  final verdict = r.open();
  if (verdict == 1) { r.report.refused = true; return v; }
  if (verdict == 2) { r.report.malformed = true; return v; }
  r.scanNodeTable();
  r.pos = 1;
  fixedTableWireLoadBody(r, v, false);
  return v;
}

int fixedTableWireMeasure(Uint8List data) {
  final r = TableWireReader(data, data.length);
  if (r.open() != 0) return -1;
  r.pos = 1;
  var at = 0;
  final listFields = <int, List<int>>{
  };
  while (true) {
    final ref = r.readLeb();
    if (ref == null || ref == 0) break;
    final id = r.refToId[ref];
    if (id == null) break;
    if (!r.has(1)) break;
    final kind = r.readByte();
    final lf = listFields[id];
    if (lf == null) { r.skipKind(kind); continue; }
    if (kind != 13 && kind != 14 && kind != 16) { r.skipKind(kind); continue; }
    final L = r.readLeb();
    if (L == null) break;
    final end = r.pos + L;
    if (end > r.limit) break;
    if (kind != 14) { r.pos = end; continue; }
    if (end - r.pos < 2) { r.pos = end; continue; }
    final elemKind = r.readByte();
    final count = r.readLeb();
    if (count == null) { r.pos = end; continue; }
    if (elemKind != 13) { r.pos = end; continue; }
    if (count < 0 || count > 0x7FFFFFFF) return -1;
    if (count > (end - r.pos) ~/ 2) return -1;
    at = (at + lf[0] - 1) & -lf[0];
    at += count * lf[1];
    for (var i = 0; i < count && r.pos < end; i++) {
      final elen = r.readLeb();
      if (elen == null) break;
      r.pos += elen;
    }
    r.pos = end;
  }
  var total = _round(1376 + at);
  r.pos = 1;
  var records = 0;
  var nodeTotal = 0;
  while (true) {
    final ref = r.readLeb();
    if (ref == null || ref == 0) break;
    final kind = r.readByte();
    final id = r.refToId[ref] ?? 0;
    if (id == _kNodeTableId) {
      if (kind != 12) return -1;
      final len = r.readLeb();
      if (len == null) return -1;
      final end = r.pos + len;
      if (end > r.limit) return -1;
      final cnt = r.readLeb();
      if (cnt == null) return -1;
      // the LAST node table wins: a field that rides twice re-reads the same
      // numbering, so the records are counted once (§3.1)
      records = 0;
      nodeTotal = 0;
      var read = 0;
      while (r.pos < end) {
        final typeRef = r.readLeb();
        if (typeRef == null || typeRef < 1 || typeRef > r.entryCount) return -1;
        final blen = r.readLeb();
        if (blen == null || blen < 0 || blen > end - r.pos) return -1;
        final tid = r.refToId[typeRef] ?? 0;
        if (tid == _kStringBlobId) nodeTotal += _round(8 + blen + 1);
        r.pos += blen;
        records++;
        read++;
      }
      if (read != cnt) return -1;
      r.pos = end;
    } else {
      r.skipKind(kind);
    }
  }
  return total + nodeTotal + (records + 1) * 16;
}

FixedTable fixedTableWireLoadRetain(Uint8List data, TableRetain retain, TableWireReport report) {
  retain.reset();
  final v = FixedTable();
  final r = TableWireReader(data, data.length);
  r.report = report;
  r.stringBlob = false;
  r.retain = retain;
  r.path = [1];
  r.rootObj = v;
  r.rootType = _kTypeFixedTable;
  final verdict = r.open();
  if (verdict == 1) { r.report.refused = true; return v; }
  if (verdict == 2) { r.report.malformed = true; return v; }
  r.scanNodeTable();
  r.pos = 1;
  fixedTableWireLoadBody(r, v, false);
  return v;
}

Uint8List fixedTableWireSaveRetain(FixedTable v, TableRetain retain, TableWireReport report) {
  retain.idUsed = 0;
  final state = WireState();
  state.retain = retain;
  state.report = report;
  state.indexRoot(v);
  final open = <Object>{v};
  fixedTableNumber(state, v, open);
  final w = TableWireWriter(state);
  w.path = [1];
  w.putByte(1);
  fixedTableWireSaveBodyFields(w, v);
  _writeNodeTable(w);
  w.putByte(0);
  for (final id in state.ids) { w.putU64(id); }
  w.putU64(state.ids.length);
  return Uint8List.fromList(w.buf);
}

int fixedTableWireMeasureRetain(FixedTable v, TableRetain retain) {
  final report = TableWireReport();
  return fixedTableWireSaveRetain(v, retain, report).length;
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

// _jsonStringField reads one string(N) field (§16.2): a non-string is a kind
// mismatch; a string past N is clamped at a CODE POINT boundary and counted,
// never cut through a multi-byte character (§16.3).
String _jsonStringField(dynamic val, int bound, TableTextReport r) {
  if (val is! String) {
    r.kindMismatch++;
    return '';
  }
  if (utf8.encode(val).length <= bound) return val;
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
    if (c == 0x7d || c == 0x5d) { stack.removeLast(); continue; }
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
  if (val is String) return val;
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
  if (val.length <= bound) return val;
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

String mixedWeaponName(int v) {
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

int mixedWeaponFromName(dynamic val, TableTextReport r) {
  if (val is! String) { r.kindMismatch++; return 0; }
  switch (val) {
    case "None": return 0;
    case "Fists": return 1;
    case "Pistol": return 2;
    case "Shotgun": return 3;
    case "Rifle": return 4;
    case "Sniper": return 5;
    case "Smg": return 6;
    case "Rocket": return 7;
    case "Grenade": return 8;
    case "Plasma": return 9;
    case "Railgun": return 10;
    case "Flamer": return 11;
    case "Mine": return 12;
    case "Turret": return 13;
    case "Drone": return 14;
    case "Repair": return 15;
  }
  r.unknown++;
  return 0;
}

void _mixedDamageToJson(_JOut out, int bits, int depth) {
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
  if (first) { throw StateError('flags MixedDamage: bits $bits name no variant (docs/SPEC-TABLES.md §16.2)'); }
  out.line(depth);
  out.raw(']');
}

int _mixedDamageFromJson(dynamic val, TableTextReport r) {
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

void _mixedEventToJson(_JOut out, MixedEvent v, int depth, _Share sh) {
  switch (v.armTag) {
    case 0:
      out.raw('{}');
      return;
    case 1:
      out.raw('{');
      out.line(depth + 1);
      out.raw(_jsonString("hit"));
      out.raw(': ');
      mixedHitEventToJsonBody(out, v.hit, depth + 1, sh);
      out.line(depth);
      out.raw('}');
      return;
    case 2:
      out.raw('{');
      out.line(depth + 1);
      out.raw(_jsonString("chat"));
      out.raw(': ');
      mixedChatEventToJsonBody(out, v.chat, depth + 1, sh);
      out.line(depth);
      out.raw('}');
      return;
    case 3:
      out.raw('{');
      out.line(depth + 1);
      out.raw(_jsonString("pickup"));
      out.raw(': ');
      mixedPickupEventToJsonBody(out, v.pickup, depth + 1, sh);
      out.line(depth);
      out.raw('}');
      return;
  }
  throw StateError('union MixedEvent: tag ${v.armTag} names no arm — the writer refuses it (docs/SPEC-TABLES.md §5)');
}

MixedEvent _mixedEventFromJson(dynamic val, TableTextReport r, _Share sh) {
  final v = MixedEvent();
  if (val is! Map) { r.kindMismatch++; return v; }
  if (val.isEmpty) return v;
  if (val.length != 1) throw const FormatException('union has two keys');
  final item = val.values.first;
  switch (val.keys.first) {
    case "hit":
      v.armTag = 1;
      v.hit = _mixedHitEventFromJsonValue(item, r, sh);
    case "chat":
      v.armTag = 2;
      v.chat = _mixedChatEventFromJsonValue(item, r, sh);
    case "pickup":
      v.armTag = 3;
      v.pickup = _mixedPickupEventFromJsonValue(item, r, sh);
    default:
      r.unknown++;
  }
  return v;
}

void _mixedEventCount(_Share sh, MixedEvent v) {
  switch (v.armTag) {
    case 1:
      mixedHitEventCount(sh, v.hit);
      break;
    case 2:
      mixedChatEventCount(sh, v.chat);
      break;
    case 3:
      mixedPickupEventCount(sh, v.pickup);
      break;
  }
}

MixedEntity _mixedEntityFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = MixedEntity();
  if (val is Map<String, dynamic>) {
    mixedEntityFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    mixedEntityFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

MixedStat _mixedStatFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = MixedStat();
  if (val is Map<String, dynamic>) {
    mixedStatFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    mixedStatFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

MixedHitEvent _mixedHitEventFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = MixedHitEvent();
  if (val is Map<String, dynamic>) {
    mixedHitEventFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    mixedHitEventFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

MixedChatEvent _mixedChatEventFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = MixedChatEvent();
  if (val is Map<String, dynamic>) {
    mixedChatEventFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    mixedChatEventFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

MixedPickupEvent _mixedPickupEventFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = MixedPickupEvent();
  if (val is Map<String, dynamic>) {
    mixedPickupEventFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    mixedPickupEventFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

BenchMixed _benchMixedFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = BenchMixed();
  if (val is Map<String, dynamic>) {
    benchMixedFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    benchMixedFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

FixedTable _fixedTableFromJsonValue(dynamic val, TableTextReport r, _Share sh) {
  final v = FixedTable();
  if (val is Map<String, dynamic>) {
    fixedTableFromJsonFields(v, val, r, sh);
  } else if (val is Map) {
    fixedTableFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);
  } else {
    r.kindMismatch++;
  }
  return v;
}

void mixedEntityCount(_Share sh, MixedEntity v) {
}

void mixedEntityToJsonBody(_JOut out, MixedEntity v, int depth, _Share sh, [int? label]) {
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
  out.raw(_jsonEnum(v.weapon, mixedWeaponName, 'MixedEntity.weapon'));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("damage"));
  out.raw(': ');
  _mixedDamageToJson(out, v.damage, depth + 1);
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

void mixedEntityFromJsonFields(MixedEntity v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
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
        v.weapon = mixedWeaponFromName(val, r);
        break;
      case "damage":
        v.damage = _mixedDamageFromJson(val, r);
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

void mixedStatCount(_Share sh, MixedStat v) {
}

void mixedStatToJsonBody(_JOut out, MixedStat v, int depth, _Share sh, [int? label]) {
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

void mixedStatFromJsonFields(MixedStat v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
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

void mixedHitEventCount(_Share sh, MixedHitEvent v) {
}

void mixedHitEventToJsonBody(_JOut out, MixedHitEvent v, int depth, _Share sh, [int? label]) {
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

void mixedHitEventFromJsonFields(MixedHitEvent v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
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

void mixedChatEventCount(_Share sh, MixedChatEvent v) {
}

void mixedChatEventToJsonBody(_JOut out, MixedChatEvent v, int depth, _Share sh, [int? label]) {
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

void mixedChatEventFromJsonFields(MixedChatEvent v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
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

void mixedPickupEventCount(_Share sh, MixedPickupEvent v) {
}

void mixedPickupEventToJsonBody(_JOut out, MixedPickupEvent v, int depth, _Share sh, [int? label]) {
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

void mixedPickupEventFromJsonFields(MixedPickupEvent v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
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

void benchMixedCount(_Share sh, BenchMixed v) {
  for (final e in v.entities) { mixedEntityCount(sh, e); }
  for (final e in v.stats) { mixedStatCount(sh, e); }
  _mixedEventCount(sh, v.gameEvent);
}

void benchMixedToJsonBody(_JOut out, BenchMixed v, int depth, _Share sh, [int? label]) {
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
  out.raw(_jsonWide(v.serverTime, 8, false));
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
      mixedEntityToJsonBody(out, e, depth + 2, sh);
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
      mixedStatToJsonBody(out, e, depth + 2, sh);
    }
    out.line(depth + 1);
    out.raw(']');
  }
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("game_event"));
  out.raw(': ');
  _mixedEventToJson(out, v.gameEvent, depth + 1, sh);
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
  out.raw(_jsonWide(v.wideKey, 0, true));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("flux"));
  out.raw(': ');
  out.raw(_jsonWide(v.flux, 0, true));
  if (comma) out.raw(',');
  comma = true;
  out.line(depth + 1);
  out.raw(_jsonString("ping"));
  out.raw(': ');
  out.raw(_jsonWide(v.ping, 8, false));
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

void benchMixedFromJsonFields(BenchMixed v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
  for (final entry in obj.entries) {
    final key = entry.key;
    final val = entry.value;
    switch (key) {
      case '&node':
        if (!pointer) throw const FormatException('label outside pointer');
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
        v.nonce = _jsonIntField(val, r, 0, -1, true);
        break;
      case "world_time":
        v.worldTime = _jsonIntField(val, r, -1000000000000, 1000000000000, false);
        break;
      case "frame_tick":
        v.frameTick = _jsonIntField(val, r, null, 281474976710655, true);
        break;
      case "server_time":
        v.serverTime = _jsonWideField(val, r, 8, true, BigInt.zero, BigInt.parse('16776960'), BigInt.zero);
        break;
      case "entities":
        if (val is! List) { r.kindMismatch++; break; }
        v.entities = <MixedEntity>[];
        for (var index = 0; index < val.length; index++) {
          if (v.entities.length >= 8) { r.clamped += val.length - index; break; }
          final item = val[index];
          if (item is Map<String, dynamic>) {
            final e = MixedEntity();
            mixedEntityFromJsonFields(e, item, r, sh);
            v.entities.add(e);
          } else if (item is Map) {
            final e = MixedEntity();
            mixedEntityFromJsonFields(e, Map<String, dynamic>.from(item), r, sh);
            v.entities.add(e);
          } else {
            r.kindMismatch++;
          }
        }
        break;
      case "stats":
        if (val is! List) { r.kindMismatch++; break; }
        v.stats = <MixedStat>[];
        for (var index = 0; index < val.length; index++) {
          if (v.stats.length >= 80) { r.clamped += val.length - index; break; }
          final item = val[index];
          if (item is Map<String, dynamic>) {
            final e = MixedStat();
            mixedStatFromJsonFields(e, item, r, sh);
            v.stats.add(e);
          } else if (item is Map) {
            final e = MixedStat();
            mixedStatFromJsonFields(e, Map<String, dynamic>.from(item), r, sh);
            v.stats.add(e);
          } else {
            r.kindMismatch++;
          }
        }
        break;
      case "game_event":
        v.gameEvent = _mixedEventFromJson(val, r, sh);
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
        v.wideKey = _jsonWideField(val, r, 0, false, null, null, BigInt.zero);
        break;
      case "flux":
        v.flux = _jsonWideField(val, r, 0, true, BigInt.parse('-1267650600228229401496703205376'), BigInt.parse('1267650600228229401496703205376'), BigInt.zero);
        break;
      case "ping":
        v.ping = _jsonWideField(val, r, 8, false, BigInt.zero, BigInt.parse('64000'), BigInt.zero);
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

void fixedTableCount(_Share sh, FixedTable v) {
  benchMixedCount(sh, v.value);
}

void fixedTableToJsonBody(_JOut out, FixedTable v, int depth, _Share sh, [int? label]) {
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
  out.raw(_jsonString("value"));
  out.raw(': ');
  benchMixedToJsonBody(out, v.value, depth + 1, sh);
  if (comma) out.line(depth);
  out.raw('}');
}

void fixedTableFromJsonFields(FixedTable v, Map<String, dynamic> obj, TableTextReport r, _Share sh, [bool pointer = false]) {
  for (final entry in obj.entries) {
    final key = entry.key;
    final val = entry.value;
    switch (key) {
      case '&node':
        if (!pointer) throw const FormatException('label outside pointer');
        break;
      case "value":
        if (val is Map<String, dynamic>) { benchMixedFromJsonFields(v.value, val, r, sh); }
        else if (val is Map) { benchMixedFromJsonFields(v.value, Map<String, dynamic>.from(val), r, sh); }
        else { r.kindMismatch++; }
        break;
      default:
        r.unknown++;
    }
  }
}

String fixedTableToJson(FixedTable v) {
  final sh = _Share();
  fixedTableCount(sh, v);
  final out = _JOut();
  fixedTableToJsonBody(out, v, 0, sh);
  out.raw('\n');
  return out.toString();
}

FixedTable fixedTableFromJson(String text, [TableTextReport? report]) {
  final r = report ?? TableTextReport();
  final v = FixedTable();
  final root = _jsonDecodeExact(_jsonPrepare(text, r));
  if (root is Map<String, dynamic>) {
    fixedTableFromJsonFields(v, root, r, _Share());
  } else if (root is Map) {
    fixedTableFromJsonFields(v, Map<String, dynamic>.from(root), r, _Share());
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
  TableFieldInfo(this.name, this.kind, this.shape, this.bound, this.min, this.max, this.json, this.typeName, this.id, this.optional, this.elemSize, this.read, this.write, this.doc, this.tags);
}

final class TableTypeInfo {
  final String name;
  final String doc;
  final List<String> tags;
  final List<TableFieldInfo> fields;
  TableTypeInfo(this.name, this.fields, this.doc, this.tags);
}

Map<String, TableTypeInfo> cardDescriptors() => <String, TableTypeInfo>{
  'MixedEntity': TableTypeInfo('MixedEntity', <TableFieldInfo>[
    TableFieldInfo("entity_id", 7, "value", null, null, null, "entity_id", "bits(12)", 2593226102015223570, false, 4, (dynamic v) => v.entityId, (dynamic v, dynamic x) { v.entityId = x; }, "", <String>[]),
    TableFieldInfo("pos_x", 4, "value", null, -16383, 16383, "pos_x", "int32", -3797881157997612786, false, 4, (dynamic v) => v.posX, (dynamic v, dynamic x) { v.posX = x; }, "", <String>[]),
    TableFieldInfo("pos_y", 4, "value", null, -16383, 16383, "pos_y", "int32", -3797880058485984575, false, 4, (dynamic v) => v.posY, (dynamic v, dynamic x) { v.posY = x; }, "", <String>[]),
    TableFieldInfo("pos_z", 4, "value", null, -16383, 16383, "pos_z", "int32", -3797883357020869208, false, 4, (dynamic v) => v.posZ, (dynamic v, dynamic x) { v.posZ = x; }, "", <String>[]),
    TableFieldInfo("yaw", 7, "value", null, null, null, "yaw", "bits(9)", -5382489739573192984, false, 4, (dynamic v) => v.yaw, (dynamic v, dynamic x) { v.yaw = x; }, "", <String>[]),
    TableFieldInfo("pitch", 7, "value", null, null, null, "pitch", "bits(9)", 6028620492700434865, false, 4, (dynamic v) => v.pitch, (dynamic v, dynamic x) { v.pitch = x; }, "", <String>[]),
    TableFieldInfo("vel_x", 4, "value", null, -2048, 2047, "vel_x", "int32", 7849183398884077159, false, 4, (dynamic v) => v.velX, (dynamic v, dynamic x) { v.velX = x; }, "", <String>[]),
    TableFieldInfo("vel_y", 4, "value", null, -2048, 2047, "vel_y", "int32", 7849182299372448948, false, 4, (dynamic v) => v.velY, (dynamic v, dynamic x) { v.velY = x; }, "", <String>[]),
    TableFieldInfo("vel_z", 4, "value", null, -2048, 2047, "vel_z", "int32", 7849185597907333581, false, 4, (dynamic v) => v.velZ, (dynamic v, dynamic x) { v.velZ = x; }, "", <String>[]),
    TableFieldInfo("health", 4, "value", null, 0, 1000, "health", "int32", 9181103189905877455, false, 4, (dynamic v) => v.health, (dynamic v, dynamic x) { v.health = x; }, "", <String>[]),
    TableFieldInfo("weapon", 30, "value", null, null, null, "weapon", "MixedWeapon", -6866282850657997311, false, 1, (dynamic v) => v.weapon, (dynamic v, dynamic x) { v.weapon = x; }, "", <String>[]),
    TableFieldInfo("damage", 9, "value", null, null, null, "damage", "MixedDamage", 9179190079975030720, false, 8, (dynamic v) => v.damage, (dynamic v, dynamic x) { v.damage = x; }, "", <String>[]),
    TableFieldInfo("moving", 1, "value", null, null, null, "moving", "bool", 1271228688681418151, false, 1, (dynamic v) => v.moving, (dynamic v, dynamic x) { v.moving = x; }, "", <String>[]),
    TableFieldInfo("firing", 1, "value", null, null, null, "firing", "bool", 8535675692938965450, false, 1, (dynamic v) => v.firing, (dynamic v, dynamic x) { v.firing = x; }, "", <String>[]),
  ], "", <String>[]),
  'MixedStat': TableTypeInfo('MixedStat', <TableFieldInfo>[
    TableFieldInfo("stat_id", 6, "value", null, null, null, "stat_id", "bits(8)", -9175110139929313435, false, 4, (dynamic v) => v.statId, (dynamic v, dynamic x) { v.statId = x; }, "", <String>[]),
    TableFieldInfo("delta", 4, "value", null, -512, 511, "delta", "int32", 5910805692604981441, false, 4, (dynamic v) => v.delta, (dynamic v, dynamic x) { v.delta = x; }, "", <String>[]),
  ], "", <String>[]),
  'MixedHitEvent': TableTypeInfo('MixedHitEvent', <TableFieldInfo>[
    TableFieldInfo("target_id", 7, "value", null, null, null, "target_id", "bits(12)", -5207116919359778736, false, 4, (dynamic v) => v.targetId, (dynamic v, dynamic x) { v.targetId = x; }, "", <String>[]),
    TableFieldInfo("damage", 4, "value", null, 0, 4095, "damage", "int32", 9179190079975030720, false, 4, (dynamic v) => v.damage, (dynamic v, dynamic x) { v.damage = x; }, "", <String>[]),
    TableFieldInfo("hit_kind", 4, "value", null, 0, 7, "hit_kind", "int32", 142922654710085925, false, 4, (dynamic v) => v.hitKind, (dynamic v, dynamic x) { v.hitKind = x; }, "", <String>[]),
    TableFieldInfo("crit", 1, "value", null, null, null, "crit", "bool", 1324453635955533101, false, 1, (dynamic v) => v.crit, (dynamic v, dynamic x) { v.crit = x; }, "", <String>[]),
  ], "", <String>[]),
  'MixedChatEvent': TableTypeInfo('MixedChatEvent', <TableFieldInfo>[
    TableFieldInfo("channel", 4, "value", null, 0, 3, "channel", "int32", -6556890747741999708, false, 4, (dynamic v) => v.channel, (dynamic v, dynamic x) { v.channel = x; }, "", <String>[]),
    TableFieldInfo("speaker", 7, "value", null, null, null, "speaker", "bits(12)", -292263051557875678, false, 4, (dynamic v) => v.speaker, (dynamic v, dynamic x) { v.speaker = x; }, "", <String>[]),
  ], "", <String>[]),
  'MixedPickupEvent': TableTypeInfo('MixedPickupEvent', <TableFieldInfo>[
    TableFieldInfo("item_id", 7, "value", null, null, null, "item_id", "bits(10)", -7025667724851298986, false, 4, (dynamic v) => v.itemId, (dynamic v, dynamic x) { v.itemId = x; }, "", <String>[]),
    TableFieldInfo("amount", 4, "value", null, 0, 255, "amount", "int32", -9145686598410475159, false, 4, (dynamic v) => v.amount, (dynamic v, dynamic x) { v.amount = x; }, "", <String>[]),
  ], "", <String>[]),
  'BenchMixed': TableTypeInfo('BenchMixed', <TableFieldInfo>[
    TableFieldInfo("sequence", 7, "value", null, null, null, "sequence", "bits(16)", -6181000666011064152, false, 4, (dynamic v) => v.sequence, (dynamic v, dynamic x) { v.sequence = x; }, "", <String>[]),
    TableFieldInfo("ack_sequence", 4, "value", null, 0, 65535, "ack_sequence", "int32", 990229364654832702, false, 4, (dynamic v) => v.ackSequence, (dynamic v, dynamic x) { v.ackSequence = x; }, "", <String>[]),
    TableFieldInfo("ack_bits", 8, "value", null, null, null, "ack_bits", "bits(32)", -7228825333587644925, false, 4, (dynamic v) => v.ackBits, (dynamic v, dynamic x) { v.ackBits = x; }, "", <String>[]),
    TableFieldInfo("session_id", 9, "value", null, null, null, "session_id", "uint64", -5199487799206016251, false, 8, (dynamic v) => v.sessionId, (dynamic v, dynamic x) { v.sessionId = x; }, "", <String>[]),
    TableFieldInfo("client_id", 8, "value", null, null, null, "client_id", "uint32", 7889067272199050878, false, 4, (dynamic v) => v.clientId, (dynamic v, dynamic x) { v.clientId = x; }, "", <String>[]),
    TableFieldInfo("nonce", 9, "value", null, 0, -1, "nonce", "uint64", 8334276637232054488, false, 8, (dynamic v) => v.nonce, (dynamic v, dynamic x) { v.nonce = x; }, "", <String>[]),
    TableFieldInfo("world_time", 5, "value", null, -1000000000000, 1000000000000, "world_time", "int64", 4534679873638431877, false, 8, (dynamic v) => v.worldTime, (dynamic v, dynamic x) { v.worldTime = x; }, "", <String>[]),
    TableFieldInfo("frame_tick", 9, "value", null, null, null, "frame_tick", "bits(48)", 8916005070914257170, false, 8, (dynamic v) => v.frameTick, (dynamic v, dynamic x) { v.frameTick = x; }, "", <String>[]),
    TableFieldInfo("server_time", 22, "value", null, null, null, "server_time", "fixed(24, 8)", 4343163795393112518, false, 4, (dynamic v) => v.serverTime, (dynamic v, dynamic x) { v.serverTime = x; }, "", <String>[]),
    TableFieldInfo("entities", 14, "value", null, null, null, "entities", "MixedEntity", -7828083326636031446, false, null, (dynamic v) => v.entities, (dynamic v, dynamic x) { v.entities = x; }, "", <String>[]),
    TableFieldInfo("stats", 14, "value", null, null, null, "stats", "MixedStat", -1268998401975805620, false, null, (dynamic v) => v.stats, (dynamic v, dynamic x) { v.stats = x; }, "", <String>[]),
    TableFieldInfo("game_event", 15, "value", null, null, null, "game_event", "MixedEvent", 3329809749115557776, false, null, (dynamic v) => v.gameEvent, (dynamic v, dynamic x) { v.gameEvent = x; }, "", <String>[]),
    TableFieldInfo("loadout", 14, "value", null, null, null, "loadout", "uint8", 6294288958393922979, false, 1, (dynamic v) => v.loadout, (dynamic v, dynamic x) { v.loadout = x; }, "", <String>[]),
    TableFieldInfo("player_name", 12, "value", 15, null, null, "player_name", "string", 1415871171432867950, false, 1, (dynamic v) => v.playerName, (dynamic v, dynamic x) { v.playerName = x; }, "", <String>[]),
    TableFieldInfo("payload", 14, "value", null, null, null, "payload", "bytes", -3478843999652550171, false, 1, (dynamic v) => v.payload, (dynamic v, dynamic x) { v.payload = x; }, "", <String>[]),
    TableFieldInfo("aim_x", 10, "value", null, null, null, "aim_x", "float32", -2616736631205226807, false, 4, (dynamic v) => v.aimX, (dynamic v, dynamic x) { v.aimX = x; }, "", <String>[]),
    TableFieldInfo("aim_y", 10, "value", null, null, null, "aim_y", "float32", -2616737730716855018, false, 4, (dynamic v) => v.aimY, (dynamic v, dynamic x) { v.aimY = x; }, "", <String>[]),
    TableFieldInfo("aim_z", 10, "value", null, null, null, "aim_z", "float32", -2616738830228483229, false, 4, (dynamic v) => v.aimZ, (dynamic v, dynamic x) { v.aimZ = x; }, "", <String>[]),
    TableFieldInfo("recoil", 10, "value", null, null, null, "recoil", "float32", -7138431908913683369, false, 4, (dynamic v) => v.recoil, (dynamic v, dynamic x) { v.recoil = x; }, "", <String>[]),
    TableFieldInfo("drift", 11, "value", null, null, null, "drift", "float64", 6535842126802938884, false, 8, (dynamic v) => v.drift, (dynamic v, dynamic x) { v.drift = x; }, "", <String>[]),
    TableFieldInfo("wide_key", 19, "value", null, null, null, "wide_key", "uint128", -6686572760759554282, false, 16, (dynamic v) => v.wideKey, (dynamic v, dynamic x) { v.wideKey = x; }, "", <String>[]),
    TableFieldInfo("flux", 18, "value", null, null, null, "flux", "int128", -3018575613315307966, false, 16, (dynamic v) => v.flux, (dynamic v, dynamic x) { v.flux = x; }, "", <String>[]),
    TableFieldInfo("ping", 26, "value", null, null, null, "ping", "ufixed(8, 8)", -4669986463835551831, false, 2, (dynamic v) => v.ping, (dynamic v, dynamic x) { v.ping = x; }, "", <String>[]),
    TableFieldInfo("crc_hint", 8, "value", null, null, null, "crc_hint", "bits(24)", 6200723483573899615, false, 4, (dynamic v) => v.crcHint, (dynamic v, dynamic x) { v.crcHint = x; }, "", <String>[]),
    TableFieldInfo("has_extra", 1, "value", null, null, null, "has_extra", "bool", -4574933642144545166, false, 1, (dynamic v) => v.hasExtra, (dynamic v, dynamic x) { v.hasExtra = x; }, "", <String>[]),
    TableFieldInfo("extra", 4, "value", null, 0, 255, "extra", "int32", -204370544148493463, false, 4, (dynamic v) => v.extra, (dynamic v, dynamic x) { v.extra = x; }, "", <String>[]),
    TableFieldInfo("idle_ticks", 4, "value", null, 0, 15, "idle_ticks", "int32", 8651444298976115966, false, 4, (dynamic v) => v.idleTicks, (dynamic v, dynamic x) { v.idleTicks = x; }, "", <String>[]),
  ], "", <String>[]),
  'FixedTable': TableTypeInfo('FixedTable', <TableFieldInfo>[
    TableFieldInfo("value", 13, "value", null, null, null, "value", "BenchMixed", 8999596768310594794, false, null, (dynamic v) => v.value, (dynamic v, dynamic x) { v.value = x; }, "", <String>[]),
  ], "", <String>[]),
};

