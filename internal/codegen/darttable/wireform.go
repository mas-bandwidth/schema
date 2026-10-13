// THE VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1, in Dart: load
// (tolerant, §4) and save (the canonical byte layout the other backends write)
// for exactly the card record's constructs — a plain `table` root, tables as
// unbounded list elements, bool/int32/int64/enum fields, `string(N)`, `*string`
// (the string kind of §2.5) and `[]T` of tables (§2.9).
//
// THE WIRE MAP is pinned in testdata/cards/README.md against the Go leg's
// committed byte oracle tables/cards/fuzz/card-populated.wire: the form byte,
// the root body's ref/kind/payload framing, the id-table trailer in first-use
// order, and kind 17 node indices for `*string` riding a §3.1 flat node table
// of string-blob records. Every function below that implements a rule cites
// the section it reads.
//
// This is the card's SLICE of the variable-class port the conformance README
// tracks (schema#349): every other variable-class construct is refused by name
// in checkUnsupportedConstructs (§11), and nothing here reads or writes one.
package darttable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The kind numbers of §3's closed set the card's constructs spend, named here
// rather than in the emitted text so a change is one place (docs/SPEC-TABLES.md
// §3).
const (
	wireKindBool   = 1
	wireKindI8     = 2
	wireKindI16    = 3
	wireKindI32    = 4
	wireKindI64    = 5
	wireKindU8     = 6
	wireKindU16    = 7
	wireKindU32    = 8
	wireKindU64    = 9
	wireKindF32    = 10
	wireKindF64    = 11
	wireKindString = 12
	wireKindTable  = 13
	wireKindArray  = 14
	wireKindPtr    = 17
	wireKindEnum   = 30
	wireKindEscape = 31
)

// dartU64Literal renders a wire id (fnv1a64, §5) as a Dart int literal: the
// value's two's-complement spelling, so an id above 2^63 — the reserved
// node-table id included — is a legal Dart int and its bit pattern is the one
// the writer's putU64 and the reader's id-table read agree on (§3).
func dartU64Literal(id uint64) string {
	return fmt.Sprintf("%d", int64(id))
}

// cardWireField reports whether a field is one of the card record's
// constructs (docs/SPEC-TABLES.md §2.5, §2.9, §3): bool, int32, int64, enum,
// string(N), *string (the string kind) and an unbounded []T of table elements.
// Every other variable-class construct — a map, a union, a bounded/counted
// array, an optional, a keyed array, bytes, the wide kinds, wstring — is NOT a
// card construct: the wire form is not claimed for it, and the emitter must
// never mis-encode it (§11).
func cardWireField(f *ir.Field) bool {
	switch {
	case f.Type.Blob():
		return f.Type.Kind == ir.TString // *string only; *bytes is refused
	case f.IsList():
		return true
	case f.Type.Kind == ir.TBool:
		return true
	case f.Type.Kind == ir.TInt:
		// uint8..uint64, int8, int16, int32, int64 (docs/SPEC-TABLES.md §2.1,
		// §3); the 128-bit integer kinds ride the wide refusal (§15).
		return f.Type.Width != 128
	case f.Type.Kind == ir.TBits, f.Type.Kind == ir.TFloat32, f.Type.Kind == ir.TFloat64:
		return true
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		return true
	case f.Type.Kind == ir.TNamed:
		_, isEnum := f.Type.Ref.(*ir.Enum)
		return isEnum
	}
	return false
}

// cardWireTable reports whether every field of a variable table is a card
// construct. A table with any other construct keeps its fixed form and gets no
// form-1 codec here — this is the card's slice and the rest is not claimed.
func cardWireTable(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if !cardWireField(f) {
			return false
		}
	}
	return true
}

// variableRoots is every plain `table` of the unit that uses ONLY the card's
// constructs and that no other such table reaches BY VALUE — the tables a
// form-1 FILE is written for, §3's ROOT BODY being the one that carries the
// node table (§3.1). A table reached only as a list element is a by-value
// member, never a root.
func variableRoots(u *ir.Unit) []*ir.Struct {
	variable := ir.VariableTables(u)
	named := map[string]bool{}
	for _, st := range u.Tables {
		if !variable[st.Name] || !cardWireTable(st) {
			continue
		}
		for _, f := range st.Fields {
			if f.IsList() || (f.Type.Kind == ir.TNamed && !f.Type.Pointer) {
				if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
					named[ref.Name] = true
				}
			}
		}
	}
	var roots []*ir.Struct
	for _, name := range sortedTableNames(u) {
		st := u.Tables[name]
		if !variable[st.Name] || !cardWireTable(st) || named[st.Name] {
			continue
		}
		roots = append(roots, st)
	}
	return roots
}

func sortedTableNames(u *ir.Unit) []string {
	out := make([]string, 0, len(u.Tables))
	for n := range u.Tables {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// wireClosure is the set of variable tables a form-1 codec reaches: the roots
// and every by-value member, in an order where a member's helpers are emitted
// before the table that holds it.
func wireClosure(u *ir.Unit) []*ir.Struct {
	variable := ir.VariableTables(u)
	var order []*ir.Struct
	seen := map[string]bool{}
	var visit func(st *ir.Struct)
	visit = func(st *ir.Struct) {
		if seen[st.Name] {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			if f.IsList() || (f.Type.Kind == ir.TNamed && !f.Type.Pointer) {
				if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable && variable[ref.Name] && cardWireTable(ref) {
					visit(ref)
				}
			}
		}
		order = append(order, st)
	}
	for _, name := range sortedTableNames(u) {
		if variable[name] && cardWireTable(u.Tables[name]) {
			visit(u.Tables[name])
		}
	}
	return order
}

// generateWireFiles emits the form-1 wire library <Home>Wire.dart for every
// variable root, and nothing for a unit whose tables carry no form-1 codec
// (a fixed-only unit keeps its form 3). The runtime is emitted once into the
// home, and every table's storage class and save/load land beside it.
func generateWireFiles(u *ir.Unit) (map[string][]byte, error) {
	roots := variableRoots(u)
	if len(roots) == 0 {
		return map[string][]byte{}, nil
	}
	closure := wireClosure(u)
	home := runtimeHome(u)
	g := &wireGen{unit: u, home: home, closure: closure, roots: roots}
	g.emit()
	// The per-table code is written with tab indentation for brevity; the
	// leg's format gate (tables-dart-clean) holds the generated file to `dart
	// format`, which indents with two spaces, so the tabs become spaces here.
	src := strings.ReplaceAll(g.body.String(), "\t", "  ")
	return map[string][]byte{home + "Wire.dart": []byte(src)}, nil
}

type wireGen struct {
	unit    *ir.Unit
	home    string
	closure []*ir.Struct
	roots   []*ir.Struct
	body    strings.Builder
}

func (g *wireGen) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

// tableWireId is a table's NODE TYPE id (§3.1): the hash of its wire name.
func tableWireId(st *ir.Struct) uint64 { return ir.TableWireId(st.WireName()) }

// fieldMember is the Dart storage member for a field: the lowerCamelCase
// spelling every Dart surface uses, with an underscore appended when that
// spelling lands on a Dart reserved word (an ordinary schema field like `_new`
// maps to `new`, a keyword), so the member stays public and valid (§11).
func fieldMember(f *ir.Field) string {
	name := dartName(f.Name)
	if dartReserved[name] {
		return name + "_"
	}
	return name
}

// fieldId is a field's effective wire id (§5): the hash of its `was` alias when
// one is declared, of its name otherwise.
func fieldId(f *ir.Field) uint64 { return ir.TableFieldWireId(f) }

// enumRef returns the enum a field's values come from, or nil.
func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// widenKinds is the list of integer kinds a field declared at declKind WIDENS
// from (§4): every kind below it on its own signed (2..5) or unsigned (6..9)
// ladder, each with its payload width. The declaration's own kind is not here.
func widenKinds(declKind int, signed bool) []struct{ k, w int } {
	lo := wireKindI8
	if !signed {
		lo = wireKindU8
	}
	var out []struct{ k, w int }
	for k := lo; k < declKind; k++ {
		out = append(out, struct{ k, w int }{k, ir.TableKindWidth(k)})
	}
	return out
}

// fmtFloat renders a float bound as a Dart double literal (§3, §4).
func fmtFloat(v float64) string { return fmt.Sprintf("%g", v) }

func (g *wireGen) emit() {
	g.pf("// The VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1, in Dart:\n")
	g.pf("// the card record's constructs — load (tolerant, §4) and save (the\n")
	g.pf("// canonical layout the Go, C, C++ and C# backends write), against the\n")
	g.pf("// committed byte oracle tables/cards/fuzz/card-populated.wire.\n\n")
	g.pf("import 'dart:convert';\nimport 'dart:typed_data';\n\n")
	g.emitConstants()
	g.emitRuntime()
	g.emitRetainRuntime()
	for _, st := range g.closure {
		g.emitStorage(st)
	}
	for _, st := range g.closure {
		g.emitSaveBody(st)
	}
	for _, st := range g.closure {
		g.emitLoadBody(st)
	}
	for _, st := range g.roots {
		g.emitSave(st)
		g.emitLoad(st)
		g.emitMeasure(st)
		g.emitRetainMembers(st)
	}
	g.emitTextForm()
	g.emitDescriptors()
}

func (g *wireGen) emitConstants() {
	g.pf("// The RESERVED node-table id (§3.1, §5) and the *string BLOB type id\n")
	g.pf("// (§2.5, §3.1), both as their 64-bit two's-complement Dart spelling.\n")
	g.pf("const int _kNodeTableId = %s; // 0xFFFFFFFFFFFFFFFF\n", dartU64Literal(ir.TableNodeWireId))
	g.pf("const int _kBuildVersionId = %s; // 0xFFFFFFFFFFFFFFFE\n", dartU64Literal(ir.TableBuildVersionWireId))
	g.pf("const int _kVocabularyId = %s; // 0xFFFFFFFFFFFFFFFD\n", dartU64Literal(ir.TableMessageVocabularyWireId))
	g.pf("const int _kStringBlobId = %s; // fnv1a64(\"string\")\n\n", dartU64Literal(ir.StringWireTypeId))
}

// emitRuntime emits the generic form-1 writer/reader (§3, §4), shared by every
// table of the unit. It invents no contract of its own: the byte framing below
// is §3's, read down to the card's closure.
func (g *wireGen) emitRuntime() {
	g.pf(`// The report a tolerant read fills: §4's six counters and the refusal
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
  final List<List<int>> nodes = <List<int>>[];
  TableRetain? retain;
  TableWireReport? report;
  bool lost = false;

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

  // nodeIndexForString numbers one *string blob node (§3.1): record k = node
  // index k + 1. A blob reached twice is one record — two fields that loaded
  // the same node, and so hold the same bytes, save the same index, matching
  // the reference backends' first-visit numbering byte for byte (§3.1).
  int nodeIndexForString(List<int> bytes) {
    for (var i = 0; i < nodes.length; i++) {
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
    nodes.add(bytes);
    return nodes.length + 1;
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
  TableWireReport report;
  int entryCount = 0;
  bool nodeTableGood = true;
  bool stringBlob = false;
  TableRetain? retain;
  List<int> path = <int>[];

  TableWireReader(this.data, this.limit)
    : pos = 0,
      refToId = <int, int>{},
      nodeTypes = <int>[],
      nodes = <Uint8List>[],
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
      report = parent.report,
      entryCount = parent.entryCount,
      nodeTableGood = parent.nodeTableGood,
      stringBlob = parent.stringBlob,
      retain = parent.retain,
      path = parent.path;

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

  // resolveString turns a kind-17 node index into the *string it names
  // (§3.1): 0 is null; 1 is the ROOT, which a *string can never be, so it is
  // a kind mismatch; records 2.. are the node table's string blobs. An index
  // above node_count + 1 is malformed, and a record whose type id this reader
  // cannot name reads null — the unknown was counted once at the node table,
  // not once per pointer (§3.1).
  String? resolveString(int idx) {
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
    if (!stringBlob || nodeTypes[rec] != _kStringBlobId) return null;
    return utf8.decode(nodes[rec]);
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
          if (stringBlob && typeId == _kStringBlobId) {
            if (_lebValid(bytes, 0, bytes.length)) {
              nodeTypes.add(typeId);
            } else {
              report.malformed = true;
              nodeTypes.add(0);
            }
          } else {
            report.unknown++;
            if (retain != null) report.retainLost++;
            nodeTypes.add(typeId);
          }
          nodes.add(bytes);
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
`)
}

// emitStorage emits a variable table's Dart storage class: a field per member
// in declaration order, every value at its declared default (§5).
func (g *wireGen) emitStorage(st *ir.Struct) {
	g.pf("final class %s {\n", st.Name)
	for _, f := range st.Fields {
		name := fieldMember(f)
		switch {
		case f.Type.Blob():
			g.pf("  String? %s;\n", name)
		case f.IsList():
			g.pf("  List<%s> %s = <%s>[];\n", f.Type.Name, name, f.Type.Name)
		case f.Type.Kind == ir.TBool:
			g.pf("  bool %s = %v;\n", name, f.HasDefault && f.DefBool)
		case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits:
			g.pf("  int %s = %s;\n", name, g.intDefault(f))
		case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
			g.pf("  double %s = %s;\n", name, g.floatDefault(f))
		case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
			g.pf("  String %s = %s;\n", name, g.stringDefault(f))
		case f.Type.Kind == ir.TNamed && enumRef(f) != nil:
			g.pf("  int %s = %s;\n", name, g.enumDefault(f))
		default:
			g.pf("  int %s = 0;\n", name)
		}
	}
	g.pf("}\n\n")
}

func (g *wireGen) intDefault(f *ir.Field) string {
	if f.HasDefault && f.DefInt != nil {
		return f.DefInt.String()
	}
	return "0"
}

func (g *wireGen) stringDefault(f *ir.Field) string {
	if f.HasDefault && len(f.DefBytes) > 0 {
		return fmt.Sprintf("%q", string(f.DefBytes))
	}
	return "''"
}

func (g *wireGen) floatDefault(f *ir.Field) string {
	if f.HasDefault {
		return fmt.Sprintf("%g", f.DefFloat)
	}
	return "0.0"
}

func (g *wireGen) enumDefault(f *ir.Field) string {
	if f.HasDefault && f.DefVariant != "" {
		e := enumRef(f)
		for i, v := range e.Variants {
			if v == f.DefVariant {
				return fmt.Sprintf("%d", i+1)
			}
		}
	}
	return "0"
}

// emitSaveBody emits <name>WireSaveBodyFields (the field sequence, no
// terminator) and <name>WireSaveBody (the same plus the zero reference): a
// nested table's body ends at its own terminator, while the ROOT's terminator
// follows the node-table field (§3.1), so the two spellings are both owed.
func (g *wireGen) emitSaveBody(st *ir.Struct) {
	n := st.Name
	fields := dartName(n) + "WireSaveBodyFields"
	g.pf("void %s(TableWireWriter w, %s v) {\n", fields, n)
	for i, f := range st.Fields {
		g.emitSaveField(f, "\t", i)
	}
	g.pf("\t_retainTail(w);\n")
	g.pf("}\n\n")
	g.pf("void %sWireSaveBody(TableWireWriter w, %s v) {\n", dartName(n), n)
	g.pf("\t%s(w, v);\n", fields)
	g.pf("\tw.putByte(0);\n}\n\n")
}

// emitSaveField writes one field's header and payload when the field rides
// (§3): the reference, the kind byte, then the value under the kind's row.
func (g *wireGen) emitSaveField(f *ir.Field, ind string, ordinal int) {
	name := fieldMember(f)
	expr := "v." + name
	id := fieldId(f)
	switch {
	case f.Type.Blob(): // *string rides as a kind-17 node index (§2.5, §3.1)
		g.pf("%sif (%s != null) {\n", ind, expr)
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindPtr)
		g.pf("%s\tw.putLeb(w.state.nodeIndexForString(utf8.encode(%s!)));\n", ind, expr)
		g.pf("%s}\n", ind)
	case f.IsList(): // []T rides as kind 14, element kind 13 (§2.9, §3)
		// The field's reference is interned BEFORE the element bodies, so the
		// id table stays in first-use order (§3): the field's id precedes the
		// element ids a member body names.
		g.pf("%sif (%s.isNotEmpty) {\n", ind, expr)
		g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
		g.pf("%s\tfinal s = w.sub();\n", ind)
		g.pf("%s\ts.putByte(%d);\n", ind, ir.TableWireElemKind(f))
		g.pf("%s\ts.putLeb(%s.length);\n", ind, expr)
		g.pf("%s\tfor (var i = 0; i < %s.length; i++) {\n", ind, expr)
		g.pf("%s\t\tfinal e = %s[i];\n", ind, expr)
		g.pf("%s\t\tfinal b = s.sub();\n", ind)
		g.pf("%s\t\tb.path = [...w.path, %d, i];\n", ind, ordinal)
		g.pf("%s\t\t%s(b, e);\n", ind, dartName(f.Type.Name)+"WireSaveBody")
		g.pf("%s\t\ts.putLeb(b.buf.length);\n", ind)
		g.pf("%s\t\ts.putBytes(b.buf);\n", ind)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tw.putLeb(ref);\n", ind)
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindArray)
		g.pf("%s\tw.putLeb(s.buf.length);\n", ind)
		g.pf("%s\tw.putBytes(s.buf);\n", ind)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif (%s != %s) {\n", ind, expr, g.stringDefault(f))
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindString)
		g.pf("%s\tfinal b = utf8.encode(%s);\n", ind, expr)
		g.pf("%s\tw.putLeb(b.length);\n", ind)
		g.pf("%s\tw.putBytes(b);\n", ind)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TBytes: // bytes(N) rides as kind 14 over element kind 6 (§3)
		g.pf("%sif (%s.isNotEmpty) {\n", ind, expr)
		g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
		g.pf("%s\tfinal b = utf8.encode(%s);\n", ind, expr)
		g.pf("%s\tfinal s = w.sub();\n", ind)
		g.pf("%s\ts.putByte(%d);\n", ind, wireKindU8)
		g.pf("%s\ts.putLeb(b.length);\n", ind)
		g.pf("%s\ts.putBytes(b);\n", ind)
		g.pf("%s\tw.putLeb(ref);\n", ind)
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindArray)
		g.pf("%s\tw.putLeb(s.buf.length);\n", ind)
		g.pf("%s\tw.putBytes(s.buf);\n", ind)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TBool:
		g.pf("%sif (%s != %v) {\n", ind, expr, f.HasDefault && f.DefBool)
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindBool)
		g.pf("%s\tw.putByte(%s ? 1 : 0);\n", ind, expr)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits:
		// An integer or bits field rides under its own kind (§3): int8/16/32/64
		// at 2/3/4/5, uint8/16/32/64 at 6/7/8/9, bits(N) at the unsigned kind of
		// its storage width. The payload is the value at the kind's width.
		kind := ir.TableWireScalarKind(f)
		put := "putU64"
		switch ir.TableKindWidth(kind) {
		case 1:
			put = "putByte"
		case 2:
			put = "putU16"
		case 4:
			put = "putU32"
		}
		g.pf("%sif (%s != %s) {\n", ind, expr, g.intDefault(f))
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, kind)
		g.pf("%s\tw.%s(%s);\n", ind, put, expr)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		kind := wireKindF64
		bits := "_f64Bits"
		put := "putU64"
		if f.Type.Kind == ir.TFloat32 {
			kind = wireKindF32
			bits = "_f32Bits"
			put = "putU32"
		}
		g.pf("%sif (%s != %s) {\n", ind, expr, g.floatDefault(f))
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, kind)
		g.pf("%s\tw.%s(%s(%s));\n", ind, put, bits, expr)
		g.pf("%s}\n", ind)
	case enumRef(f) != nil:
		e := enumRef(f)
		g.pf("%sif (%s != %s) {\n", ind, expr, g.enumDefault(f))
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindEnum)
		g.pf("%s\tint vid;\n", ind)
		g.pf("%s\tswitch (%s) {\n", ind, expr)
		for i := range e.Variants {
			vid := ir.TableWireId(e.VariantWireName(i))
			g.pf("%s\t\tcase %d: vid = %s;\n", ind, i+1, dartU64Literal(vid))
		}
		g.pf("%s\t\tdefault: return;\n", ind)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tw.putLeb(w.refFor(vid));\n", ind)
		g.pf("%s}\n", ind)
	default:
		// No other construct reaches this emitter: checkUnsupportedConstructs
		// refuses maps, *bytes, pointers to tables outside list elements and
		// the wide kinds by name (§11), and fixed-only constructs are form 3.
		g.pf("%s// %s: not a card construct; refused by name (§11)\n", ind, name)
	}
}

// emitLoadBody emits <name>WireLoadBody: the tolerant read of one table body
// (§4) — unknown ids skipped and counted, absent fields at their defaults,
// kind mismatch counted, out-of-range values clamped, a widened kind counted.
func (g *wireGen) emitLoadBody(st *ir.Struct) {
	n := st.Name
	fn := dartName(n) + "WireLoadBody"
	g.pf("void %s(TableWireReader r, %s v, bool nested) {\n", fn, n)
	g.pf("\twhile (true) {\n")
	g.pf("\t\tfinal ref = r.readLeb();\n")
	g.pf("\t\tif (ref == null) { r.report.malformed = true; return; }\n")
	g.pf("\t\tif (ref == 0) return;\n")
	g.pf("\t\tfinal id = r.refToId[ref];\n")
	g.pf("\t\tif (id == null) { r.report.malformed = true; return; }\n")
	g.pf("\t\tif (!r.has(1)) { r.report.malformed = true; return; }\n")
	g.pf("\t\tfinal kind = r.readByte();\n")
	g.pf("\t\tif (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }\n")
	g.pf("\t\tif (id == _kNodeTableId) {\n")
	g.pf("\t\t\tif (nested) { r.report.malformed = true; return; }\n")
	g.pf("\t\t\tif (!r.skipKind(kind)) { r.report.malformed = true; return; }\n")
	g.pf("\t\t\tcontinue;\n")
	g.pf("\t\t}\n")
	g.pf("\t\tswitch (id) {\n")
	for i, f := range st.Fields {
		g.emitLoadField(f, "\t\t\t", i)
	}
	g.pf("\t\t\tdefault:\n")
	g.pf("\t\t\t\tr.report.unknown++;\n")
	g.pf("\t\t\t\tif (r.retain != null) {\n")
	g.pf("\t\t\t\t\tfinal start = r.pos;\n")
	g.pf("\t\t\t\t\tif (!r.skipKind(kind)) { r.report.malformed = true; return; }\n")
	g.pf("\t\t\t\t\tif (kind == %d) {\n", wireKindPtr)
	g.pf("\t\t\t\t\t\tr.report.retainLost++;\n")
	g.pf("\t\t\t\t\t} else {\n")
	g.pf("\t\t\t\t\t\tfinal sub = TableWireReader.sub(r, start, r.pos);\n")
	g.pf("\t\t\t\t\t\tfinal resolved = _retainResolve(sub, kind, 0);\n")
	g.pf("\t\t\t\t\t\tif (resolved == null || sub.pos != sub.limit) {\n")
	g.pf("\t\t\t\t\t\t\tr.report.retainLost++;\n")
	g.pf("\t\t\t\t\t\t} else {\n")
	g.pf("\t\t\t\t\t\t\tfinal rec = _RetainRecord(List<int>.from(r.path), id, kind, resolved);\n")
	g.pf("\t\t\t\t\t\t\tif (r.retain!.used + rec.cost() > r.retain!.capacity) {\n")
	g.pf("\t\t\t\t\t\t\t\tr.report.retainLost++;\n")
	g.pf("\t\t\t\t\t\t\t} else {\n")
	g.pf("\t\t\t\t\t\t\t\tr.retain!.records.add(rec);\n")
	g.pf("\t\t\t\t\t\t\t\tr.retain!.used += rec.cost();\n")
	g.pf("\t\t\t\t\t\t\t\tr.report.retained++;\n")
	g.pf("\t\t\t\t\t\t\t}\n")
	g.pf("\t\t\t\t\t\t}\n")
	g.pf("\t\t\t\t\t}\n")
	g.pf("\t\t\t\t} else {\n")
	g.pf("\t\t\t\t\tif (!r.skipKind(kind)) { r.report.malformed = true; return; }\n")
	g.pf("\t\t\t\t}\n")
	g.pf("\t\t}\n")
	g.pf("\t}\n}\n\n")
}

func (g *wireGen) emitLoadField(f *ir.Field, ind string, ordinal int) {
	name := fieldMember(f)
	id := fieldId(f)
	g.pf("%scase %s:\n", ind, dartU64Literal(id))
	switch {
	case f.Type.Blob(): // *string rides as a kind-17 node index (§2.5, §3.1)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindPtr)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal idx = r.readLeb();\n", ind)
		g.pf("%s\t\tif (idx == null) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tv.%s = r.resolveString(idx);\n", ind, name)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case f.IsList(): // []T of tables rides as kind 14, element kind 13 (§2.9, §3)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindArray)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
		g.pf("%s\t\tif (len == null) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tfinal end = r.pos + len;\n", ind)
		g.pf("%s\t\tif (end > r.limit) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tv.%s = <%s>[];\n", ind, name, f.Type.Name)
		g.pf("%s\t\tif (end - r.pos < 2) { r.pos = end; break; }\n", ind)
		g.pf("%s\t\tfinal arr = TableWireReader.sub(r, r.pos, end);\n", ind)
		g.pf("%s\t\tfinal elemKind = arr.readByte();\n", ind)
		g.pf("%s\t\tfinal count = arr.readLeb();\n", ind)
		g.pf("%s\t\tif (count == null) { r.report.malformed = true; r.pos = end; break; }\n", ind)
		g.pf("%s\t\tif (elemKind != %d) { r.report.kindMismatch++; r.pos = end; break; }\n", ind, ir.TableWireElemKind(f))
		g.pf("%s\t\tfor (var i = 0; i < count; i++) {\n", ind)
		g.pf("%s\t\t\tfinal elen = arr.readLeb();\n", ind)
		g.pf("%s\t\t\tif (elen == null || !arr.has(elen)) { r.report.malformed = true; break; }\n", ind)
		g.pf("%s\t\t\tvar e = %s();\n", ind, f.Type.Name)
		g.pf("%s\t\t\tfinal sub = TableWireReader.sub(arr, arr.pos, arr.pos + elen);\n", ind)
		g.pf("%s\t\t\tsub.path = [...r.path, %d, i];\n", ind, ordinal)
		g.pf("%s\t\t\t%s(sub, e, true);\n", ind, dartName(f.Type.Name)+"WireLoadBody")
		g.pf("%s\t\t\tif (sub.pos != sub.limit) { r.report.malformed = true; e = %s(); }\n", ind, f.Type.Name)
		g.pf("%s\t\t\tarr.pos = arr.pos + elen;\n", ind)
		g.pf("%s\t\t\tv.%s.add(e);\n", ind, name)
		g.pf("%s\t\t}\n", ind)
		g.pf("%s\t\tr.pos = end;\n", ind)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindString)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
		g.pf("%s\t\tif (len == null || !r.has(len)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tif (!_lebValid(r.data, r.pos, len)) { r.report.malformed = true; r.pos += len; break; }\n", ind)
		g.pf("%s\t\tfinal keep = _textBoundary(r.data, r.pos, len, %d);\n", ind, f.Type.Size)
		g.pf("%s\t\tif (keep != len) r.report.clamped++;\n", ind)
		g.pf("%s\t\tv.%s = utf8.decode(r.data.sublist(r.pos, r.pos + keep));\n", ind, name)
		g.pf("%s\t\tr.pos += len;\n", ind)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case f.Type.Kind == ir.TBool:
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindBool)
		g.pf("%s\tif (!r.has(1)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\tv.%s = r.readByte() != 0;\n", ind, name)
		g.pf("%s\tbreak;\n", ind)
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits:
		// An int/bits field's declared kind is §3's integer kind for its width
		// and signedness: int8/16/32/64 at 2/3/4/5, uint8/16/32/64 at 6/7/8/9,
		// bits(N) at the unsigned kind of its storage width. A payload under a
		// smaller kind on the same signed/unsigned ladder WIDENS (§4), one under
		// a kind this field does not take is a mismatch, and a value past the
		// declared range (or the bits mask) CLAMPS.
		declKind := ir.TableWireScalarKind(f)
		declWidth := ir.TableKindWidth(declKind)
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tvar value = 0;\n", ind)
		g.pf("%s\t\tswitch (kind) {\n", ind)
		for _, wk := range widenKinds(declKind, signed) {
			g.pf("%s\t\t\tcase %d:\n", ind, wk.k)
			g.pf("%s\t\t\t\tif (!r.has(%d)) { r.report.malformed = true; return; }\n", ind, wk.w)
			g.pf("%s\t\t\t\tvar raw = 0;\n", ind)
			for b := 0; b < wk.w; b++ {
				g.pf("%s\t\t\t\traw |= r.readByte() << %d;\n", ind, 8*b)
			}
			if signed {
				g.pf("%s\t\t\t\tvalue = _signExtend(raw, %d);\n", ind, wk.w)
			} else {
				g.pf("%s\t\t\t\tvalue = raw;\n", ind)
			}
			g.pf("%s\t\t\t\tr.report.widened++;\n", ind)
			g.pf("%s\t\t\t\tbreak;\n", ind)
		}
		g.pf("%s\t\t\tcase %d:\n", ind, declKind)
		switch declWidth {
		case 1:
			g.pf("%s\t\t\t\tif (!r.has(1)) { r.report.malformed = true; return; }\n", ind)
			if signed {
				g.pf("%s\t\t\t\tvalue = _signExtend(r.readByte(), 1);\n", ind)
			} else {
				g.pf("%s\t\t\t\tvalue = r.readByte();\n", ind)
			}
		case 2:
			g.pf("%s\t\t\t\tif (!r.has(2)) { r.report.malformed = true; return; }\n", ind)
			g.pf("%s\t\t\t\tvar raw = 0;\n", ind)
			for b := 0; b < 2; b++ {
				g.pf("%s\t\t\t\traw |= r.readByte() << %d;\n", ind, 8*b)
			}
			if signed {
				g.pf("%s\t\t\t\tvalue = _signExtend(raw, 2);\n", ind)
			} else {
				g.pf("%s\t\t\t\tvalue = raw;\n", ind)
			}
		case 4:
			g.pf("%s\t\t\t\tif (!r.has(4)) { r.report.malformed = true; return; }\n", ind)
			if signed {
				g.pf("%s\t\t\t\tvalue = _signExtend(readU32(r.data, r.pos), 4); r.pos += 4;\n", ind)
			} else {
				g.pf("%s\t\t\t\tvalue = readU32(r.data, r.pos); r.pos += 4;\n", ind)
			}
		default:
			g.pf("%s\t\t\t\tif (!r.has(8)) { r.report.malformed = true; return; }\n", ind)
			g.pf("%s\t\t\t\tvalue = readU64(r.data, r.pos); r.pos += 8;\n", ind)
		}
		g.pf("%s\t\t\t\tbreak;\n", ind)
		g.pf("%s\t\t\tdefault:\n", ind)
		g.pf("%s\t\t\t\tr.report.kindMismatch++;\n", ind)
		g.pf("%s\t\t\t\tif (!r.skipKind(kind)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\t\t\tbreak;\n", ind)
		g.pf("%s\t\t}\n", ind)
		if f.HasIntRange {
			g.pf("%s\t\tif (value < %s) { value = %s; r.report.clamped++; }\n", ind, f.IntMin.String(), f.IntMin.String())
			g.pf("%s\t\tif (value > %s) { value = %s; r.report.clamped++; }\n", ind, f.IntMax.String(), f.IntMax.String())
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
			max := uint64(1)<<f.Type.Width - 1
			g.pf("%s\t\tif (value > %d) { value = %d; r.report.clamped++; }\n", ind, max, max)
		}
		g.pf("%s\t\tv.%s = value;\n", ind, name)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		// A float field reads its own kind (§3): f32 at 10, f64 at 11; f64 also
		// WIDENS a kind-10 payload (§4). The value rides as its raw IEEE bits.
		declKind := wireKindF64
		if f.Type.Kind == ir.TFloat32 {
			declKind = wireKindF32
		}
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tvar d = 0.0;\n", ind)
		g.pf("%s\t\tswitch (kind) {\n", ind)
		g.pf("%s\t\t\tcase %d:\n", ind, declKind)
		if f.Type.Kind == ir.TFloat64 {
			g.pf("%s\t\t\t\tif (!r.has(8)) { r.report.malformed = true; return; }\n", ind)
			g.pf("%s\t\t\t\td = _f64FromBits(readU64(r.data, r.pos)); r.pos += 8;\n", ind)
		} else {
			g.pf("%s\t\t\t\tif (!r.has(4)) { r.report.malformed = true; return; }\n", ind)
			g.pf("%s\t\t\t\td = _f32FromBits(readU32(r.data, r.pos)); r.pos += 4;\n", ind)
		}
		g.pf("%s\t\t\t\tbreak;\n", ind)
		if f.Type.Kind == ir.TFloat64 {
			g.pf("%s\t\t\tcase %d:\n", ind, wireKindF32)
			g.pf("%s\t\t\t\tif (!r.has(4)) { r.report.malformed = true; return; }\n", ind)
			g.pf("%s\t\t\t\td = _f32FromBits(readU32(r.data, r.pos)); r.pos += 4;\n", ind)
			g.pf("%s\t\t\t\tr.report.widened++;\n", ind)
			g.pf("%s\t\t\t\tbreak;\n", ind)
		}
		g.pf("%s\t\t\tdefault:\n", ind)
		g.pf("%s\t\t\t\tr.report.kindMismatch++;\n", ind)
		g.pf("%s\t\t\t\tif (!r.skipKind(kind)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\t\t\tbreak;\n", ind)
		g.pf("%s\t\t}\n", ind)
		if f.HasFloatRange {
			g.pf("%s\t\tif (d < %s) { d = %s; r.report.clamped++; }\n", ind, fmtFloat(f.FMin), fmtFloat(f.FMin))
			g.pf("%s\t\tif (d > %s) { d = %s; r.report.clamped++; }\n", ind, fmtFloat(f.FMax), fmtFloat(f.FMax))
		}
		g.pf("%s\t\tv.%s = d;\n", ind, name)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case f.Type.Kind == ir.TBytes: // bytes(N) rides as kind 14 over element kind 6 (§3)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindArray)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
		g.pf("%s\t\tif (len == null) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tfinal end = r.pos + len;\n", ind)
		g.pf("%s\t\tif (end > r.limit) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tif (end - r.pos < 2) { r.pos = end; break; }\n", ind)
		g.pf("%s\t\tfinal arr = TableWireReader.sub(r, r.pos, end);\n", ind)
		g.pf("%s\t\tfinal elemKind = arr.readByte();\n", ind)
		g.pf("%s\t\tfinal count = arr.readLeb();\n", ind)
		g.pf("%s\t\tif (count == null) { r.report.malformed = true; r.pos = end; break; }\n", ind)
		g.pf("%s\t\tif (elemKind != %d) { r.report.kindMismatch++; r.pos = end; break; }\n", ind, wireKindU8)
		g.pf("%s\t\tif (!arr.has(count)) { r.report.malformed = true; r.pos = end; break; }\n", ind)
		g.pf("%s\t\tvar keep = count;\n", ind)
		g.pf("%s\t\tif (keep > %d) { keep = %d; r.report.clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s\t\tv.%s = utf8.decode(arr.data.sublist(arr.pos, arr.pos + keep));\n", ind, name)
		g.pf("%s\t\tr.pos = end;\n", ind)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case enumRef(f) != nil:
		e := enumRef(f)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindEnum)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal ref2 = r.readLeb();\n", ind)
		g.pf("%s\t\tif (ref2 == null) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tif (ref2 == 0) { v.%s = 0; break; }\n", ind, name)
		g.pf("%s\t\tfinal vid = r.refToId[ref2];\n", ind)
		g.pf("%s\t\tif (vid == null) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tswitch (vid) {\n", ind)
		for i := range e.Variants {
			vid := ir.TableWireId(e.VariantWireName(i))
			g.pf("%s\t\t\tcase %s: v.%s = %d;\n", ind, dartU64Literal(vid), name, i+1)
		}
		g.pf("%s\t\t\tdefault: v.%s = 0; r.report.unknown++; if (r.retain != null) r.report.retainLost++;\n", ind, name)
		g.pf("%s\t\t}\n", ind)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	default:
		g.pf("%s\tif (!r.skipKind(kind)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\tbreak;\n", ind)
	}
}

// emitSave emits <name>WireSave: form byte 1, the root body, the node-table
// field (§3.1, written LAST in the root body), the terminator and the id-table
// trailer (§3).
func (g *wireGen) emitSave(st *ir.Struct) {
	n := st.Name
	fn := dartName(n) + "WireSave"
	g.pf("Uint8List %s(%s v) {\n", fn, n)
	g.pf("\tfinal state = WireState();\n")
	g.pf("\tfinal w = TableWireWriter(state);\n")
	g.pf("\tw.putByte(1);\n")
	g.pf("\t%sWireSaveBodyFields(w, v);\n", dartName(n))
	g.pf("\tif (state.nodes.isNotEmpty) {\n")
	g.pf("\t\tfinal ref = state.refFor(_kNodeTableId);\n")
	g.pf("\t\tw.putLeb(ref);\n")
	g.pf("\t\tw.putByte(%d);\n", wireKindString)
	g.pf("\t\tfinal body = TableWireWriter(state);\n")
	g.pf("\t\tbody.putLeb(state.nodes.length);\n")
	g.pf("\t\tfor (final node in state.nodes) {\n")
	g.pf("\t\t\tbody.putLeb(state.refFor(_kStringBlobId));\n")
	g.pf("\t\t\tbody.putLeb(node.length);\n")
	g.pf("\t\t\tbody.putBytes(node);\n")
	g.pf("\t\t}\n")
	g.pf("\t\tw.putLeb(body.buf.length);\n")
	g.pf("\t\tw.putBytes(body.buf);\n")
	g.pf("\t}\n")
	g.pf("\tw.putByte(0);\n")
	g.pf("\tfor (final id in state.ids) { w.putU64(id); }\n")
	g.pf("\tw.putU64(state.ids.length);\n")
	g.pf("\treturn Uint8List.fromList(w.buf);\n")
	g.pf("}\n\n")
}

// emitLoad emits <name>WireLoad: open the wire (form byte + id table, §3),
// scan the node table (§3.1), then read the root body tolerantly (§4). The
// report a caller passes is the one the read fills; without one the events are
// counted silently.
func (g *wireGen) emitLoad(st *ir.Struct) {
	n := st.Name
	fn := dartName(n) + "WireLoad"
	_, strBlob := ir.PointerReachableBlobs(st)
	g.pf("%s %s(Uint8List data, [TableWireReport? report]) {\n", n, fn)
	g.pf("\tfinal v = %s();\n", n)
	g.pf("\tfinal r = TableWireReader(data, data.length);\n")
	g.pf("\tr.stringBlob = %v;\n", strBlob)
	g.pf("\tr.path = [1];\n")
	g.pf("\tif (report != null) r.report = report;\n")
	g.pf("\tfinal verdict = r.open();\n")
	g.pf("\tif (verdict == 1) { r.report.refused = true; return v; }\n")
	g.pf("\tif (verdict == 2) { r.report.malformed = true; return v; }\n")
	g.pf("\tr.scanNodeTable();\n")
	g.pf("\tr.pos = 1;\n")
	g.pf("\t%s(r, v, false);\n", dartName(n)+"WireLoadBody")
	g.pf("\treturn v;\n")
	g.pf("}\n\n")
}

// emitMeasure emits <name>WireMeasure, the region byte count a caller asks for
// before it loads (docs/SPEC-TABLES.md §6.2): the root body's fixed size plus
// its out-of-line list extents, every reachable *string blob's storage, and one
// directory entry a node. -1 is a refusal — a form byte this reader does not
// carry, a trailer it cannot resolve, a body that ends early, or a list whose
// count the framing cannot hold. It mirrors the other backends' LoadMeasure.
func (g *wireGen) emitMeasure(st *ir.Struct) {
	n := st.Name
	fn := dartName(n) + "WireMeasure"
	g.pf("int %s(Uint8List data) {\n", fn)
	g.pf("\tfinal r = TableWireReader(data, data.length);\n")
	g.pf("\tif (r.open() != 0) return -1;\n")
	g.pf("\tr.pos = 1;\n")
	g.pf("\tvar at = 0;\n")
	g.pf("\tfinal listFields = <int, List<int>>{\n")
	for _, f := range st.Fields {
		if !f.IsList() {
			continue
		}
		size, align := ir.ListElementLayout(g.unit, f)
		g.pf("\t\t%s: [%d, %d],\n", dartU64Literal(fieldId(f)), align, size)
	}
	g.pf("\t};\n")
	g.pf("\twhile (true) {\n")
	g.pf("\t\tfinal ref = r.readLeb();\n")
	g.pf("\t\tif (ref == null || ref == 0) break;\n")
	g.pf("\t\tfinal id = r.refToId[ref];\n")
	g.pf("\t\tif (id == null) break;\n")
	g.pf("\t\tif (!r.has(1)) break;\n")
	g.pf("\t\tfinal kind = r.readByte();\n")
	g.pf("\t\tfinal lf = listFields[id];\n")
	g.pf("\t\tif (lf == null) { r.skipKind(kind); continue; }\n")
	g.pf("\t\tif (kind != 13 && kind != 14 && kind != 16) { r.skipKind(kind); continue; }\n")
	g.pf("\t\tfinal L = r.readLeb();\n")
	g.pf("\t\tif (L == null) break;\n")
	g.pf("\t\tfinal end = r.pos + L;\n")
	g.pf("\t\tif (end > r.limit) break;\n")
	g.pf("\t\tif (kind != 14) { r.pos = end; continue; }\n")
	g.pf("\t\tif (end - r.pos < 2) { r.pos = end; continue; }\n")
	g.pf("\t\tfinal elemKind = r.readByte();\n")
	g.pf("\t\tfinal count = r.readLeb();\n")
	g.pf("\t\tif (count == null) { r.pos = end; continue; }\n")
	g.pf("\t\tif (elemKind != 13) { r.pos = end; continue; }\n")
	g.pf("\t\tif (count < 0 || count > 0x7FFFFFFF) return -1;\n")
	g.pf("\t\tif (count > (end - r.pos) ~/ 2) return -1;\n")
	g.pf("\t\tat = (at + lf[0] - 1) & -lf[0];\n")
	g.pf("\t\tat += count * lf[1];\n")
	g.pf("\t\tfor (var i = 0; i < count && r.pos < end; i++) {\n")
	g.pf("\t\t\tfinal elen = r.readLeb();\n")
	g.pf("\t\t\tif (elen == null) break;\n")
	g.pf("\t\t\tr.pos += elen;\n")
	g.pf("\t\t}\n")
	g.pf("\t\tr.pos = end;\n")
	g.pf("\t}\n")
	g.pf("\tvar total = _round(%d + at);\n", ir.RecordLayout(g.unit, st).Size)
	g.pf("\tr.pos = 1;\n")
	g.pf("\tvar records = 0;\n")
	g.pf("\tvar nodeTotal = 0;\n")
	g.pf("\twhile (true) {\n")
	g.pf("\t\tfinal ref = r.readLeb();\n")
	g.pf("\t\tif (ref == null || ref == 0) break;\n")
	g.pf("\t\tfinal kind = r.readByte();\n")
	g.pf("\t\tfinal id = r.refToId[ref] ?? 0;\n")
	g.pf("\t\tif (id == _kNodeTableId) {\n")
	g.pf("\t\t\tif (kind != 12) return -1;\n")
	g.pf("\t\t\tfinal len = r.readLeb();\n")
	g.pf("\t\t\tif (len == null) return -1;\n")
	g.pf("\t\t\tfinal end = r.pos + len;\n")
	g.pf("\t\t\tif (end > r.limit) return -1;\n")
	g.pf("\t\t\tfinal cnt = r.readLeb();\n")
	g.pf("\t\t\tif (cnt == null) return -1;\n")
	g.pf("\t\t\t// the LAST node table wins: a duplicated field re-reads the same\n")
	g.pf("\t\t\t// numbering, so the records are counted once (§3.1)\n")
	g.pf("\t\t\trecords = 0;\n")
	g.pf("\t\t\tnodeTotal = 0;\n")
	g.pf("\t\t\tvar read = 0;\n")
	g.pf("\t\t\twhile (r.pos < end) {\n")
	g.pf("\t\t\t\tfinal typeRef = r.readLeb();\n")
	g.pf("\t\t\t\tif (typeRef == null || typeRef < 1 || typeRef > r.entryCount) return -1;\n")
	g.pf("\t\t\t\tfinal blen = r.readLeb();\n")
	g.pf("\t\t\t\tif (blen == null || blen < 0 || blen > end - r.pos) return -1;\n")
	g.pf("\t\t\t\tfinal tid = r.refToId[typeRef] ?? 0;\n")
	g.pf("\t\t\t\tif (tid == _kStringBlobId) nodeTotal += _round(8 + blen + 1);\n")
	g.pf("\t\t\t\tr.pos += blen;\n")
	g.pf("\t\t\t\trecords++;\n")
	g.pf("\t\t\t\tread++;\n")
	g.pf("\t\t\t}\n")
	g.pf("\t\t\tif (read != cnt) return -1;\n")
	g.pf("\t\t\tr.pos = end;\n")
	g.pf("\t\t} else {\n")
	g.pf("\t\t\tr.skipKind(kind);\n")
	g.pf("\t\t}\n")
	g.pf("\t}\n")
	g.pf("\treturn total + nodeTotal + (records + 1) * 16;\n")
	g.pf("}\n\n")
}
