// THE VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1, in Dart: load
// (tolerant, §4) and save (the canonical byte layout the other backends write)
// for every table root, fixed or variable — by-value nesting of tables and
// `type`s, pointers *T, arrays of pointers []*T, counted [..N]T and unbounded
// []T, tables as list elements, bool and every integer kind, `bits(N)`, flags,
// `float32`/`float64`, `string(N)`/`bytes(N)`, `*string`/`*bytes`, maps and
// unions (wireunion.go), fixed positional and enum-keyed arrays
// (wirearrays.go), optional fields and arrays, fixed(I, F) and the 128-bit
// scalars (wirewide.go) and wstring(N). A construct none of these carries is
// refused by name in checkUnsupportedConstructs (§11) on a unit the variable
// form owns, and nothing here encodes one.
//
// THE WIRE MAP is pinned in testdata/cards/README.md against the Go leg's
// committed byte oracle tables/cards/fuzz/card-populated.wire, and the
// pointer, blob and list corpora pin the same bytes in testdata/wire/tables.
// The id table is 64-bit, first-use order (§3). Kind 31 (escape) is never
// written — no declaration maps to it — and the reader skips it by its L
// (§3, §4). Every function below that implements a rule cites the section.
package darttable

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The kind numbers of §3's closed set the card's constructs spend, named here
// rather than in the emitted text so a change is one place (docs/SPEC-TABLES.md
// §3).
const (
	wireKindBool    = 1
	wireKindI8      = 2
	wireKindI16     = 3
	wireKindI32     = 4
	wireKindI64     = 5
	wireKindU8      = 6
	wireKindU16     = 7
	wireKindU32     = 8
	wireKindU64     = 9
	wireKindF32     = 10
	wireKindF64     = 11
	wireKindString  = 12
	wireKindTable   = 13
	wireKindArray   = 14
	wireKindPtr     = 17
	wireKindEnum    = 30
	wireKindEscape  = 31
	wireKindWString = 33
)

// dartU64Literal renders a wire id (fnv1a64, §5) as a Dart int literal: the
// value's two's-complement spelling, so an id above 2^63 — the reserved
// node-table id included — is a legal Dart int and its bit pattern is the one
// the writer's putU64 and the reader's id-table read agree on (§3).
func dartU64Literal(id uint64) string {
	return fmt.Sprintf("%d", int64(id))
}

// dartBoundLiteral is a declared integer bound as a Dart int literal (§3, §4).
// A bound above 2^63-1 is the same 64-bit pattern, spelled as its signed mint,
// because a Dart int literal cannot name an unsigned value past that line.
func dartBoundLiteral(n *big.Int) string {
	if n == nil {
		return "0"
	}
	if n.Sign() < 0 || n.BitLen() > 64 {
		return n.String()
	}
	return dartU64Literal(n.Uint64())
}

// tableRef is the table a field names, or nil for a scalar, an enum, a blob,
// a union or a non-table struct (docs/SPEC-TABLES.md §2.1).
func tableRef(f *ir.Field) *ir.Struct {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	// A `type` a table reaches rides exactly as a by-value table does: kind
	// 13 over a body of id-framed fields (docs/SPEC-TABLES.md §3,
	// internal/tablewire encodeField's kind-13 arm), so it is a "table" to
	// every walk here.
	st, ok := f.Type.Ref.(*ir.Struct)
	if !ok {
		return nil
	}
	return st
}

func isPointerTable(f *ir.Field) bool {
	return f.Array == ir.ArrayNone && f.Type.Pointer && tableRef(f) != nil
}

func isByValueTable(f *ir.Field) bool {
	return f.Array == ir.ArrayNone && !f.Type.Pointer && !f.Type.Blob() && tableRef(f) != nil
}

// cardWireField reports whether a field is a construct this backend encodes
// on form 1 (docs/SPEC-TABLES.md §2.1, §2.3, §2.4, §2.5, §2.6, §2.8, §2.9, §3):
// every scalar kind (the wide ones included), bits, flags, the two floats,
// enum, string(N), bytes(N), wstring(N), *string/*bytes, a pointer or
// by-value table or `type` whose target is itself carried, a counted,
// unbounded, fixed or enum-keyed array of a carried element, a map whose
// entry is carried, a union whose every arm is, and an optional of any of
// these. seen breaks a pointer cycle — a table already on the stack is
// carried if the rest of its fields are.
func cardWireField(f *ir.Field) bool {
	return cardWireFieldSeen(f, map[string]bool{})
}

func cardWireFieldSeen(f *ir.Field, seen map[string]bool) bool {
	// An OPTIONAL (§2.3) is carried when its value is: presence rides as the
	// field's own presence on the wire (wirewide.go).
	if f.Type.Optional {
		base := nonOptional(f)
		if base.IsMap() || unionRef(base) != nil || base.KeyEnum != "" || base.Type.Blob() || base.Type.Pointer {
			return false
		}
		return cardWireFieldSeen(base, seen)
	}
	// A MAP (§2.8) rides as an array of its generated entry table, so it is
	// carried exactly when that entry is.
	if f.IsMap() {
		return f.Array == ir.ArrayNone && cardWireTableSeen(f.MapEntry, seen)
	}
	if f.Type.Kind == ir.TMap {
		return false
	}
	// A UNION (§2.6) and a counted or unbounded array of them (§3) are carried
	// when every arm is: an arm is a field line, judged by these same rules.
	if un := unionRef(f); un != nil {
		// a fixed positional array of unions rides too, every slot (§3)
		if f.Array != ir.ArrayNone && !f.CountedOnWire() && !(f.Array == ir.ArrayFixed && f.KeyEnum == "") {
			return false
		}
		return cardWireUnionSeen(un, seen)
	}
	// wstring(N) (§3, kind 33) by value as a field; not yet inside an array
	if f.Type.Kind == ir.TWString {
		return f.Array == ir.ArrayNone && !f.Type.Pointer
	}
	if f.Type.Blob() && f.Array == ir.ArrayNone {
		return true
	}
	if f.CountedOnWire() {
		return cardScalarOrTable(f, seen)
	}
	// a fixed positional array [N]T and an enum-keyed array [E]T (§2.4, §3,
	// §3.2) of a carried element
	if f.Array == ir.ArrayFixed {
		return fixedElemCarried(f, seen)
	}
	if f.Array != ir.ArrayNone {
		return false
	}
	return cardScalarOrTable(f, seen)
}

func cardScalarOrTable(f *ir.Field, seen map[string]bool) bool {
	switch f.Type.Kind {
	case ir.TBool, ir.TBits, ir.TFloat32, ir.TFloat64, ir.TString, ir.TBytes:
		return true
	case ir.TInt, ir.TFixed:
		// every integer width, the 128-bit kinds and the fixed-point family
		// included (§3, §15: wirewide.go)
		return true
	case ir.TNamed:
		if _, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
			return !f.Type.Pointer
		}
		// a flags set rides as its mask under the unsigned kind of its storage
		// width (§3), exactly as an unsigned integer does
		if isFlags(f) {
			return !f.Type.Pointer
		}
		if ref := tableRef(f); ref != nil {
			return cardWireTableSeen(ref, seen)
		}
	}
	return false
}

// cardWireTable reports whether every field of a table is a carried construct.
// A table with any other construct gets no form-1 codec (§11).
func cardWireTable(st *ir.Struct) bool {
	return cardWireTableSeen(st, map[string]bool{})
}

func cardWireTableSeen(st *ir.Struct, seen map[string]bool) bool {
	if st == nil {
		return false
	}
	if seen[st.Name] {
		return true
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if !cardWireFieldSeen(f, seen) {
			delete(seen, st.Name)
			return false
		}
	}
	return true
}

// CarriesVariableRoot reports whether the unit has a variable table this
// backend encodes on form 1. The compiler uses it so a map or a union sitting
// beside that root does not refuse the whole unit (docs/SPEC-TABLES.md §11).
func CarriesVariableRoot(u *ir.Unit) bool {
	return len(variableRoots(u)) > 0
}

// variableRoots is every table this backend encodes, fixed or variable, that
// no other such table reaches by value or as a counted/list element — the
// tables a form-1 FILE is written for. A pointer target stays a root: the edge
// is not by-value (§3.1).
func variableRoots(u *ir.Unit) []*ir.Struct {
	named := map[string]bool{}
	for _, st := range u.Tables {
		if !cardWireTable(st) {
			continue
		}
		for _, f := range st.Fields {
			if ref := tableRef(f); ref != nil && (f.CountedOnWire() || isByValueTable(f) || (f.Array == ir.ArrayFixed && !f.Type.Pointer)) {
				named[ref.Name] = true
			}
			// a map's entry and a table an arm carries by value are reached
			// by value too, so neither is a file root (§2.6, §2.8)
			for _, ref := range byValueReaches(f) {
				named[ref.Name] = true
			}
		}
	}
	var roots []*ir.Struct
	for _, name := range sortedTableNames(u) {
		st := u.Tables[name]
		// A FIXED table is a form-1 root too (docs/SPEC-TABLES.md §2.2, §3): the
		// variable form carries every table, and form 3 is a second encoding
		// of a fixed one, not a replacement for this one.
		if st.IsMapEntry() || !cardWireTable(st) || named[st.Name] {
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

// wireClosure is every table a form-1 codec reaches, fixed tables included
// when they are pointer targets or by-value members: a member's helpers are
// emitted before the table that holds it. A table this backend does not
// encode is left out, so a map or a union beside a carried root is not given
// a codec (§11).
func wireClosure(u *ir.Unit) []*ir.Struct {
	var order []*ir.Struct
	seen := map[string]bool{}
	var visit func(st *ir.Struct)
	visit = func(st *ir.Struct) {
		if st == nil || seen[st.Name] || !cardWireTable(st) {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			if ref := tableRef(f); ref != nil && (f.CountedOnWire() || f.Array == ir.ArrayNone || f.Array == ir.ArrayFixed) {
				visit(ref)
			}
			// the entry of a map and every table a union arm names (§2.6, §2.8)
			for _, ref := range fieldReaches(f) {
				visit(ref)
			}
		}
		order = append(order, st)
	}
	for _, name := range sortedTableNames(u) {
		if st := u.Tables[name]; st != nil {
			visit(st)
		}
	}
	return order
}

func typeConst(st *ir.Struct) string { return "_kType" + st.Name }

// generateWireFiles emits the form-1 wire library <Home>Wire.dart for every
// root, and nothing for a unit whose tables carry no form-1 codec. The runtime is emitted once into the
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
	cur     *ir.Struct // the table whose load body is being emitted
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

// listElementKind is the element kind an unbounded array's body opens with
// (docs/SPEC-TABLES.md §2.9, §3): 13 for `[]T` of tables, 12 for `[]string`,
// 30 for `[]E` of enums, and the scalar kind otherwise.
func listElementKind(f *ir.Field) int { return ir.TableWireElemKind(f) }

// dartListElemType is the Dart storage element type for a `[]T` field: the
// scalar/enum spellings the storage class uses, and the table name for table
// elements. Enums and every integer kind store as Dart `int`; the two float
// kinds as `double`; string/bytes as `String`.
func dartListElemType(f *ir.Field) string {
	switch {
	case isWide(f):
		return "BigInt"
	case f.Type.Kind == ir.TBool:
		return "bool"
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits || isFlags(f):
		return "int"
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		return "double"
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		return "String"
	case f.Type.Kind == ir.TNamed:
		if _, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
			return "int"
		}
		if f.Type.Pointer && tableRef(f) != nil {
			return f.Type.Name + "?"
		}
		return f.Type.Name
	}
	return "int"
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
	g.pf("// committed byte oracle tables/cards/fuzz/card-populated.wire.\n")
	// The runtime is one library for every kind the class can carry. A unit
	// that does not spend a kind leaves that helper unreferenced. That is the
	// shape of a shared runtime, not a mistake in a unit. The ignore has to
	// sit above the imports: the analyzer only honours it before any code.
	g.pf("// ignore_for_file: unused_element\n\n")
	g.pf("import 'dart:convert';\nimport 'dart:typed_data';\n\n")
	g.emitConstants()
	g.emitRuntime()
	g.emitRetainRuntime()
	g.emitNodeDispatch()
	g.emitMapRuntime()
	g.emitWideRuntime()
	for _, st := range g.closure {
		g.emitStorage(st)
	}
	for _, un := range g.closureUnions() {
		g.emitUnionStorage(un)
		g.emitUnion(un)
	}
	for _, st := range g.closure {
		g.emitSaveBody(st)
	}
	for _, st := range g.closure {
		g.emitLoadBody(st)
	}
	for _, st := range g.closure {
		g.emitNumber(st)
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
	g.pf("const int _kBytesBlobId = %s; // fnv1a64(\"bytes\")\n", dartU64Literal(ir.BytesWireTypeId))
	g.pf("const int _kStringBlobId = %s; // fnv1a64(\"string\")\n", dartU64Literal(ir.StringWireTypeId))
	for _, st := range g.closure {
		g.pf("const int %s = %s; // fnv1a64(%q)\n", typeConst(st), dartU64Literal(tableWireId(st)), st.WireName())
	}
	g.pf("\n")
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
`)
}

// emitStorage emits a variable table's Dart storage class: a field per member
// in declaration order, every value at its declared default (§5).
func (g *wireGen) emitStorage(st *ir.Struct) {
	g.pf("final class %s {\n", st.Name)
	for _, f := range st.Fields {
		g.pf("  %s\n", g.storageDecl(f, fieldMember(f)))
	}
	g.pf("}\n\n")
}

func (g *wireGen) intDefault(f *ir.Field) string {
	if f.HasDefault && f.DefInt != nil {
		return dartBoundLiteral(f.DefInt)
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
	case f.Type.Optional: // presence is the payload (§2.3, wirewide.go)
		g.emitSaveOptional(f, expr, ind, id)
	case f.IsMap(): // a map rides as kind 14 over its entry bodies (§2.8)
		g.emitSaveMap(f, expr, ind, id, ordinal)
	case isUnionField(f): // a union rides as kind 15 and its arm header (§2.6, §3)
		g.emitSaveUnionField(f, expr, ind, id)
	case isKeyed(f): // [E]T rides as kind 16 (§2.4, §3.2)
		g.emitSaveKeyed(f, expr, ind, id)
	case isFixedArray(f): // [N]T rides as kind 14, every element (§3)
		g.emitSaveFixed(f, expr, ind, id)
	case f.Type.Blob(): // *string/*bytes ride as a kind-17 node index (§2.5, §3.1)
		g.pf("%sif (%s != null) {\n", ind, expr)
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindPtr)
		if f.Type.Kind == ir.TString {
			g.pf("%s\tw.putLeb(w.state.blobIndex(_kStringBlobId, utf8.encode(%s!)));\n", ind, expr)
		} else {
			g.pf("%s\tw.putLeb(w.state.blobIndex(_kBytesBlobId, %s!));\n", ind, expr)
		}
		g.pf("%s}\n", ind)
	case isPointerTable(f):
		g.pf("%sif (%s != null) {\n", ind, expr)
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindPtr)
		g.pf("%s\tw.putLeb(w.state.indexOf(%s!));\n", ind, expr)
		g.pf("%s}\n", ind)
	case isByValueTable(f):
		g.emitSaveByValue(f, expr, ind, id)
	case f.CountedOnWire(): // []T and [..N]T ride as kind 14 (§2.9, §3)
		switch listElementKind(f) {
		case wireKindUnion:
			g.emitSaveUnionList(f, expr, ind, id)
		case wireKindPtr:
			g.emitSavePointerList(f, expr, ind, id)
		case wireKindTable:
			g.emitSaveTableList(f, expr, ind, id, ordinal)
		default:
			g.emitSaveScalarList(f, expr, ind, id)
		}
	case f.Type.Kind == ir.TWString:
		// WIDE TEXT TAKES NO SPECIFIED DEFAULT, so the empty value elides; L is
		// a BYTE length, twice the code-unit count, and each UTF-16 unit rides
		// little-endian (§3, kind 33)
		g.pf("%sif (%s.isNotEmpty) {\n", ind, expr)
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindWString)
		g.pf("%s\tw.putLeb(%s.length * 2);\n", ind, expr)
		g.pf("%s\tfor (final u in %s.codeUnits) { w.putU16(u); }\n", ind, expr)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif (%s != %s) {\n", ind, expr, g.stringDefault(f))
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindString)
		g.pf("%s\tfinal b = utf8.encode(%s);\n", ind, expr)
		g.pf("%s\tw.putLeb(b.length);\n", ind)
		g.pf("%s\tw.putBytes(b);\n", ind)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TBytes: // bytes(N) rides as kind 14 over element kind 6 (§3); the String holds one byte a code unit (latin1), so every byte value survives
		g.pf("%sif (%s.isNotEmpty) {\n", ind, expr)
		g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
		g.pf("%s\tfinal b = latin1.encode(%s);\n", ind, expr)
		g.pf("%s\tfinal s = w.sub();\n", ind)
		g.pf("%s\ts.putByte(%d);\n", ind, wireKindU8)
		g.pf("%s\ts.putLeb(b.length);\n", ind)
		g.pf("%s\ts.putBytes(b);\n", ind)
		g.pf("%s\tw.putLeb(ref);\n", ind)
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindArray)
		g.pf("%s\tw.putLeb(s.buf.length);\n", ind)
		g.pf("%s\tw.putBytes(s.buf);\n", ind)
		g.pf("%s}\n", ind)
	case isWide(f): // kinds 18-29, the raw integer at its width (§3, §15)
		g.emitSaveWide(f, expr, ind, id)
	case f.Type.Kind == ir.TBool:
		g.pf("%sif (%s != %v) {\n", ind, expr, f.HasDefault && f.DefBool)
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindBool)
		g.pf("%s\tw.putByte(%s ? 1 : 0);\n", ind, expr)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits || isFlags(f):
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

// emitSaveByValue writes a nested table as kind 13 (§3). The field id is
// interned first, the body decides, and an all-default body (its terminator
// alone) rolls the id table back so the elided nest leaves no ids.
func (g *wireGen) emitSaveByValue(f *ir.Field, expr, ind string, id uint64) {
	ref := tableRef(f)
	g.pf("%s{\n", ind)
	g.pf("%s\tfinal mark = w.state.mark();\n", ind)
	g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
	g.pf("%s\tfinal b = w.sub();\n", ind)
	g.pf("%s\t%sWireSaveBody(b, %s);\n", ind, dartName(ref.Name), expr)
	g.pf("%s\tif (b.buf.length <= 1) {\n", ind)
	g.pf("%s\t\tw.state.rollback(mark);\n", ind)
	g.pf("%s\t} else {\n", ind)
	g.pf("%s\t\tw.putLeb(ref);\n", ind)
	g.pf("%s\t\tw.putByte(%d);\n", ind, wireKindTable)
	g.pf("%s\t\tw.putLeb(b.buf.length);\n", ind)
	g.pf("%s\t\tw.putBytes(b.buf);\n", ind)
	g.pf("%s\t}\n", ind)
	g.pf("%s}\n", ind)
}

// emitSavePointerList writes a []*T as kind 14 over element kind 17 (§3.1):
// the count the value decides, then one node index a slot, and a null slot
// rides as 0. The field id is interned before the body.
func (g *wireGen) emitSavePointerList(f *ir.Field, expr, ind string, id uint64) {
	g.pf("%sif (%s.isNotEmpty) {\n", ind, expr)
	g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
	g.pf("%s\tfinal s = w.sub();\n", ind)
	g.pf("%s\ts.putByte(%d);\n", ind, wireKindPtr)
	g.pf("%s\ts.putLeb(%s.length);\n", ind, expr)
	g.pf("%s\tfor (final e in %s) {\n", ind, expr)
	g.pf("%s\t\tif (e == null) { s.putLeb(0); } else { s.putLeb(w.state.indexOf(e)); }\n", ind)
	g.pf("%s\t}\n", ind)
	g.pf("%s\tw.putLeb(ref);\n", ind)
	g.pf("%s\tw.putByte(%d);\n", ind, wireKindArray)
	g.pf("%s\tw.putLeb(s.buf.length);\n", ind)
	g.pf("%s\tw.putBytes(s.buf);\n", ind)
	g.pf("%s}\n", ind)
}

// emitSaveTableList writes a `[]T` of tables as kind 14 over element kind 13
// (docs/SPEC-TABLES.md §2.9, §3): each element is a framed body, and the
// field's reference is interned BEFORE the element bodies so the id table
// stays in first-use order (§3).
func (g *wireGen) emitSaveTableList(f *ir.Field, expr, ind string, id uint64, ordinal int) {
	g.pf("%sif (%s.isNotEmpty) {\n", ind, expr)
	g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
	g.pf("%s\tfinal s = w.sub();\n", ind)
	g.pf("%s\ts.putByte(%d);\n", ind, wireKindTable)
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
}

// emitSaveScalarList writes a `[]T` of scalars, strings or enums as kind 14
// over the element's own kind (docs/SPEC-TABLES.md §2.9, §3): a scalar rides
// at its kind's width, a bool as one byte, a string length-framed, an enum as
// its variant id's reference. The count the value decides, then the payloads.
func (g *wireGen) emitSaveScalarList(f *ir.Field, expr, ind string, id uint64) {
	elemKind := listElementKind(f)
	g.pf("%sif (%s.isNotEmpty) {\n", ind, expr)
	g.pf("%s\tfinal ref = w.refFor(%s);\n", ind, dartU64Literal(id))
	g.pf("%s\tfinal s = w.sub();\n", ind)
	g.pf("%s\ts.putByte(%d);\n", ind, elemKind)
	g.pf("%s\ts.putLeb(%s.length);\n", ind, expr)
	g.pf("%s\tfor (var i = 0; i < %s.length; i++) {\n", ind, expr)
	g.pf("%s\t\tfinal e = %s[i];\n", ind, expr)
	switch elemKind {
	case wireKindString:
		g.pf("%s\t\tfinal b = utf8.encode(e);\n", ind)
		g.pf("%s\t\ts.putLeb(b.length);\n", ind)
		g.pf("%s\t\ts.putBytes(b);\n", ind)
	case wireKindEnum:
		e := enumRef(f)
		g.pf("%s\t\tif (e == 0) {\n", ind)
		g.pf("%s\t\t\ts.putLeb(0);\n", ind)
		g.pf("%s\t\t} else {\n", ind)
		g.pf("%s\t\t\tint vid = 0;\n", ind)
		g.pf("%s\t\t\tswitch (e) {\n", ind)
		for i := range e.Variants {
			vid := ir.TableWireId(e.VariantWireName(i))
			g.pf("%s\t\t\t\tcase %d: vid = %s;\n", ind, i+1, dartU64Literal(vid))
		}
		g.pf("%s\t\t\t}\n", ind)
		g.pf("%s\t\t\ts.putLeb(s.refFor(vid));\n", ind)
		g.pf("%s\t\t}\n", ind)
	case wireKindBool:
		g.pf("%s\t\ts.putByte(e ? 1 : 0);\n", ind)
	case wireKindF32:
		g.pf("%s\t\ts.putU32(_f32Bits(e));\n", ind)
	case wireKindF64:
		g.pf("%s\t\ts.putU64(_f64Bits(e));\n", ind)
	default:
		if ir.TableKindWide(elemKind) {
			g.pf("%s\t\t_putWide(s, e, %d);\n", ind, ir.TableKindWidth(elemKind))
			break
		}
		put := "putU64"
		switch ir.TableKindWidth(elemKind) {
		case 1:
			put = "putByte"
		case 2:
			put = "putU16"
		case 4:
			put = "putU32"
		}
		g.pf("%s\t\ts.%s(e);\n", ind, put)
	}
	g.pf("%s\t}\n", ind)
	g.pf("%s\tw.putLeb(ref);\n", ind)
	g.pf("%s\tw.putByte(%d);\n", ind, wireKindArray)
	g.pf("%s\tw.putLeb(s.buf.length);\n", ind)
	g.pf("%s\tw.putBytes(s.buf);\n", ind)
	g.pf("%s}\n", ind)
}

// emitLoadBody emits <name>WireLoadBody: the tolerant read of one table body
// (§4) — unknown ids skipped and counted, absent fields at their defaults,
// kind mismatch counted, out-of-range values clamped, a widened kind counted.
func (g *wireGen) emitLoadBody(st *ir.Struct) {
	n := st.Name
	fn := dartName(n) + "WireLoadBody"
	g.cur = st
	g.emitArrayHelpers(st)
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
	if f.Type.Optional {
		// the field rode, so the optional is PRESENT (§2.3, §4); its value
		// reads exactly as the bare field's does, and an occurrence under a
		// kind (or an element kind) the field does not take leaves it at its
		// declared default, which for an optional is ABSENT
		// (internal/tablewire arrayBody, readFields)
		has := hasMember(f)
		g.pf("%s\tv.%s = true;\n", ind, has)
		start := g.body.Len()
		g.emitLoadField(nonOptional(f), ind, ordinal)
		caseText := g.body.String()[start:]
		// the bare field's own case line is dropped: this case already opened
		caseText = caseText[strings.Index(caseText, "\n")+1:]
		caseText = strings.ReplaceAll(caseText, "r.report.kindMismatch++;", "r.report.kindMismatch++; v."+has+" = false;")
		rest := g.body.String()[:start]
		g.body.Reset()
		g.body.WriteString(rest)
		g.body.WriteString(caseText)
		return
	}
	switch {
	case f.IsMap(): // kind 14 over the entry bodies (§2.8)
		g.emitLoadMap(f, ind, ordinal)
	case isKeyed(f) || isFixedArray(f): // kind 16 / kind 14 (§3, §3.2)
		g.emitLoadArray(g.cur, f, ind)
	case isUnionField(f): // kind 15, the arm header in place (§2.6, §3)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindUnion)
		g.pf("%s\tif (!%s(r, v.%s)) { r.report.malformed = true; return; }\n", ind, unionFn(unionRef(f), "LoadArm"), name)
		g.pf("%s\tbreak;\n", ind)
	case f.Type.Blob(): // *string/*bytes ride as a kind-17 node index (§2.5, §3.1)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindPtr)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal idx = r.readLeb();\n", ind)
		g.pf("%s\t\tif (idx == null) { r.report.malformed = true; return; }\n", ind)
		if f.Type.Kind == ir.TString {
			g.pf("%s\t\tv.%s = r.resolveString(idx);\n", ind, name)
		} else {
			g.pf("%s\t\tv.%s = r.resolveBytes(idx);\n", ind, name)
		}
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case isPointerTable(f):
		ref := tableRef(f)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindPtr)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal idx = r.readLeb();\n", ind)
		g.pf("%s\t\tif (idx == null) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tv.%s = r.resolveTable(idx, %s) as %s?;\n", ind, name, typeConst(ref), ref.Name)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case isByValueTable(f):
		ref := tableRef(f)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindTable)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
		g.pf("%s\t\tif (len == null || !r.has(len)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tfinal sub = TableWireReader.sub(r, r.pos, r.pos + len);\n", ind)
		g.pf("%s\t\t%s(sub, v.%s, true);\n", ind, dartName(ref.Name)+"WireLoadBody", name)
		g.pf("%s\t\tif (sub.pos != sub.limit) { r.report.malformed = true; v.%s = %s(); }\n", ind, name, ref.Name)
		g.pf("%s\t\tr.pos += len;\n", ind)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case f.CountedOnWire(): // []T and [..N]T ride as kind 14 (§2.9, §3)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindArray)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
		g.pf("%s\t\tif (len == null) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tfinal end = r.pos + len;\n", ind)
		g.pf("%s\t\tif (end > r.limit) { r.report.malformed = true; return; }\n", ind)
		et := dartListElemType(f)
		g.pf("%s\t\tv.%s = <%s>[];\n", ind, name, et)
		g.pf("%s\t\tif (end - r.pos < 2) { r.pos = end; break; }\n", ind)
		g.pf("%s\t\tfinal arr = TableWireReader.sub(r, r.pos, end);\n", ind)
		g.pf("%s\t\tfinal elemKind = arr.readByte();\n", ind)
		g.pf("%s\t\tfinal count = arr.readLeb();\n", ind)
		g.pf("%s\t\tif (count == null) { r.report.malformed = true; r.pos = end; break; }\n", ind)
		declKind := listElementKind(f)
		switch declKind {
		case wireKindTable, wireKindPtr, wireKindString, wireKindEnum, wireKindBool, wireKindUnion:
			g.pf("%s\t\tif (elemKind != %d) { r.report.kindMismatch++; r.pos = end; break; }\n", ind, declKind)
		default:
			// A smaller kind on the same ladder widens the whole array once (§4).
			g.pf("%s\t\tif (elemKind != %d) {\n", ind, declKind)
			g.pf("%s\t\t\tif (!_kindWidens(elemKind, %d)) { r.report.kindMismatch++; r.pos = end; break; }\n", ind, declKind)
			g.pf("%s\t\t\tr.report.widened++;\n", ind)
			g.pf("%s\t\t}\n", ind)
		}
		// a count past a counted array's bound keeps the bound's prefix and
		// counts one clamp; the rest skips via the length (§4,
		// internal/tablewire arrayBody)
		loop := "count"
		if f.Array == ir.ArrayCounted {
			g.pf("%s\t\tvar keep = count;\n", ind)
			g.pf("%s\t\tif (keep > %d) { keep = %d; r.report.clamped++; }\n", ind, f.ArrayBound, f.ArrayBound)
			loop = "keep"
		}
		g.pf("%s\t\tfor (var i = 0; i < %s; i++) {\n", ind, loop)
		switch listElementKind(f) {
		case wireKindUnion:
			// one element of an array of unions: the arm header in place, a
			// None element the single zero byte (§3)
			g.pf("%s\t\t\tfinal e = %s();\n", ind, f.Type.Name)
			g.pf("%s\t\t\tif (!%s(arr, e)) { r.report.malformed = true; break; }\n", ind, unionFn(unionRef(f), "LoadArm"))
			g.pf("%s\t\t\tv.%s.add(e);\n", ind, name)
		case wireKindTable:
			g.pf("%s\t\t\tfinal elen = arr.readLeb();\n", ind)
			g.pf("%s\t\t\tif (elen == null || !arr.has(elen)) { r.report.malformed = true; break; }\n", ind)
			g.pf("%s\t\t\tvar e = %s();\n", ind, f.Type.Name)
			g.pf("%s\t\t\tfinal sub = TableWireReader.sub(arr, arr.pos, arr.pos + elen);\n", ind)
			g.pf("%s\t\t\tsub.path = [...r.path, %d, i];\n", ind, ordinal)
			g.pf("%s\t\t\t%s(sub, e, true);\n", ind, dartName(f.Type.Name)+"WireLoadBody")
			g.pf("%s\t\t\tif (sub.pos != sub.limit) { r.report.malformed = true; e = %s(); }\n", ind, f.Type.Name)
			g.pf("%s\t\t\tarr.pos = arr.pos + elen;\n", ind)
			g.pf("%s\t\t\tv.%s.add(e);\n", ind, name)
		case wireKindPtr:
			ref := tableRef(f)
			g.pf("%s\t\t\tfinal idx = arr.readLeb();\n", ind)
			g.pf("%s\t\t\tif (idx == null) { r.report.malformed = true; break; }\n", ind)
			g.pf("%s\t\t\tv.%s.add(r.resolveTable(idx, %s) as %s?);\n", ind, name, typeConst(ref), ref.Name)
		default:
			g.emitLoadScalarListElem(f, "v."+name, "arr", ind)
		}
		g.pf("%s\t\t}\n", ind)
		g.pf("%s\t\tr.pos = end;\n", ind)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case f.Type.Kind == ir.TWString:
		// kind 33 (§3): an odd L is framing damage, ill-formed UTF-16 (a lone
		// surrogate) is malformed and reads the default, and a value past the
		// bound clamps at a code-point boundary, never through a pair
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindWString)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
		g.pf("%s\t\tif (len == null || !r.has(len)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tif (len.isOdd) { r.report.malformed = true; r.pos += len; break; }\n", ind)
		g.pf("%s\t\tfinal units = <int>[];\n", ind)
		g.pf("%s\t\tfor (var i = 0; i < len; i += 2) { units.add(r.data[r.pos + i] | (r.data[r.pos + i + 1] << 8)); }\n", ind)
		g.pf("%s\t\tr.pos += len;\n", ind)
		g.pf("%s\t\tif (!_wideValid(units)) { r.report.malformed = true; break; }\n", ind)
		g.pf("%s\t\tvar keep = units.length;\n", ind)
		g.pf("%s\t\tif (keep > %d) {\n", ind, f.Type.Size)
		g.pf("%s\t\t\tkeep = %d;\n", ind, f.Type.Size)
		g.pf("%s\t\t\tif (keep > 0 && units[keep - 1] >= 0xD800 && units[keep - 1] <= 0xDBFF) keep--;\n", ind)
		g.pf("%s\t\t\tr.report.clamped++;\n", ind)
		g.pf("%s\t\t}\n", ind)
		g.pf("%s\t\tv.%s = String.fromCharCodes(units.sublist(0, keep));\n", ind, name)
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
	case isWide(f): // kinds 18-29 (§3, §4, §15)
		g.emitLoadWide(f, ind)
	case f.Type.Kind == ir.TBool:
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindBool)
		g.pf("%s\tif (!r.has(1)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\tv.%s = r.readByte() != 0;\n", ind, name)
		g.pf("%s\tbreak;\n", ind)
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits || isFlags(f):
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
			lo, hi := dartBoundLiteral(f.IntMin), dartBoundLiteral(f.IntMax)
			if signed {
				g.pf("%s\t\tif (value < %s) { value = %s; r.report.clamped++; }\n", ind, lo, lo)
				g.pf("%s\t\tif (value > %s) { value = %s; r.report.clamped++; }\n", ind, hi, hi)
			} else {
				g.pf("%s\t\tif (_u64Below(value, %s)) { value = %s; r.report.clamped++; }\n", ind, lo, lo)
				g.pf("%s\t\tif (_u64Above(value, %s)) { value = %s; r.report.clamped++; }\n", ind, hi, hi)
			}
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
			lit := dartU64Literal(uint64(1)<<f.Type.Width - 1)
			g.pf("%s\t\tif (_u64Above(value, %s)) { value = %s; r.report.clamped++; }\n", ind, lit, lit)
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
		g.pf("%s\t\tv.%s = latin1.decode(arr.data.sublist(arr.pos, arr.pos + keep));\n", ind, name)
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

// emitLoadScalarListElem emits the tolerant read of ONE element of a `[]T`
// of scalars, strings or enums, appending it to the destination list (dest is
// `v.field`). A scalar reads at its kind's width, a bool one byte, a string
// length-framed, an enum a variant id reference (§2.9, §3).
func (g *wireGen) emitLoadScalarListElem(f *ir.Field, dest, arr string, ind string) {
	elemKind := listElementKind(f)
	switch elemKind {
	case wireKindString:
		g.pf("%s\t\t\tfinal elen = %s.readLeb();\n", ind, arr)
		g.pf("%s\t\t\tif (elen == null || !%s.has(elen)) { r.report.malformed = true; break; }\n", ind, arr)
		g.pf("%s\t\t\tif (!_lebValid(%s.data, %s.pos, elen)) { r.report.malformed = true; %s.pos += elen; break; }\n", ind, arr, arr, arr)
		g.pf("%s\t\t\t%s.add(utf8.decode(%s.data.sublist(%s.pos, %s.pos + elen)));\n", ind, dest, arr, arr, arr)
		g.pf("%s\t\t\t%s.pos += elen;\n", ind, arr)
	case wireKindEnum:
		e := enumRef(f)
		g.pf("%s\t\t\tfinal vref = %s.readLeb();\n", ind, arr)
		g.pf("%s\t\t\tif (vref == null) { r.report.malformed = true; break; }\n", ind)
		g.pf("%s\t\t\tif (vref == 0) {\n", ind)
		g.pf("%s\t\t\t\t%s.add(0);\n", ind, dest)
		g.pf("%s\t\t\t} else {\n", ind)
		g.pf("%s\t\t\tfinal vid = %s.refToId[vref];\n", ind, arr)
		g.pf("%s\t\t\tif (vid == null) { r.report.malformed = true; break; }\n", ind)
		g.pf("%s\t\t\tswitch (vid) {\n", ind)
		for i := range e.Variants {
			vid := ir.TableWireId(e.VariantWireName(i))
			g.pf("%s\t\t\t\tcase %s: %s.add(%d);\n", ind, dartU64Literal(vid), dest, i+1)
		}
		g.pf("%s\t\t\t\tdefault: %s.add(0); r.report.unknown++;\n", ind, dest)
		g.pf("%s\t\t\t}\n", ind)
		g.pf("%s\t\t\t}\n", ind)
	case wireKindBool:
		g.pf("%s\t\t\tif (!%s.has(1)) { r.report.malformed = true; break; }\n", ind, arr)
		g.pf("%s\t\t\t%s.add(%s.readByte() != 0);\n", ind, dest, arr)
	case wireKindF32:
		g.pf("%s\t\t\tif (!%s.has(4)) { r.report.malformed = true; break; }\n", ind, arr)
		g.pf("%s\t\t\t%s.add(_f32FromBits(readU32(%s.data, %s.pos))); %s.pos += 4;\n", ind, dest, arr, arr, arr)
	case wireKindF64:
		// f64 widens an f32 element kind (§4). The array already counted widened.
		g.pf("%s\t\t\tif (elemKind == %d) {\n", ind, wireKindF32)
		g.pf("%s\t\t\t\tif (!%s.has(4)) { r.report.malformed = true; break; }\n", ind, arr)
		g.pf("%s\t\t\t\t%s.add(_f32FromBits(readU32(%s.data, %s.pos))); %s.pos += 4;\n", ind, dest, arr, arr, arr)
		g.pf("%s\t\t\t} else if (elemKind == %d) {\n", ind, wireKindF64)
		g.pf("%s\t\t\t\tif (!%s.has(8)) { r.report.malformed = true; break; }\n", ind, arr)
		g.pf("%s\t\t\t\t%s.add(_f64FromBits(readU64(%s.data, %s.pos))); %s.pos += 8;\n", ind, dest, arr, arr, arr)
		g.pf("%s\t\t\t} else { r.report.malformed = true; break; }\n", ind)
	default:
		if ir.TableKindWide(elemKind) {
			w := ir.TableKindWidth(elemKind)
			g.pf("%s\t\t\tif (!%s.has(%d)) { r.report.malformed = true; break; }\n", ind, arr, w)
			g.pf("%s\t\t\t{\n", ind)
			g.pf("%s\t\t\t\tvar x = _getWide(%s, %d, %v);\n", ind, arr, w, ir.TableKindSigned(elemKind))
			g.emitWideClamp(f, "x", "r", ind+"\t\t\t\t")
			g.pf("%s\t\t\t\t%s.add(x);\n", ind, dest)
			g.pf("%s\t\t\t}\n", ind)
			break
		}
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		decl := listElementKind(f)
		lo := wireKindU8
		if decl >= wireKindI8 && decl <= wireKindI64 {
			lo = wireKindI8
			signed = true
		}
		for k := lo; k <= decl; k++ {
			kw := "if"
			if k != lo {
				kw = "} else if"
			}
			w := ir.TableKindWidth(k)
			g.pf("%s\t\t\t%s (elemKind == %d) {\n", ind, kw, k)
			g.pf("%s\t\t\t\tif (!%s.has(%d)) { r.report.malformed = true; break; }\n", ind, arr, w)
			switch w {
			case 1:
				if signed {
					g.pf("%s\t\t\t\t%s.add(_signExtend(%s.readByte(), 1));\n", ind, dest, arr)
				} else {
					g.pf("%s\t\t\t\t%s.add(%s.readByte());\n", ind, dest, arr)
				}
			case 2:
				g.pf("%s\t\t\t\tvar raw = 0;\n", ind)
				for b := 0; b < 2; b++ {
					g.pf("%s\t\t\t\traw |= %s.readByte() << %d;\n", ind, arr, 8*b)
				}
				if signed {
					g.pf("%s\t\t\t\t%s.add(_signExtend(raw, 2));\n", ind, dest)
				} else {
					g.pf("%s\t\t\t\t%s.add(raw);\n", ind, dest)
				}
			case 4:
				if signed {
					g.pf("%s\t\t\t\t%s.add(_signExtend(readU32(%s.data, %s.pos), 4)); %s.pos += 4;\n", ind, dest, arr, arr, arr)
				} else {
					g.pf("%s\t\t\t\t%s.add(readU32(%s.data, %s.pos)); %s.pos += 4;\n", ind, dest, arr, arr, arr)
				}
			default:
				g.pf("%s\t\t\t\t%s.add(readU64(%s.data, %s.pos)); %s.pos += 8;\n", ind, dest, arr, arr, arr)
			}
		}
		g.pf("%s\t\t\t} else { r.report.malformed = true; break; }\n", ind)
	}
}

// emitSave emits <name>WireSave: form byte 1, the root body, the node-table
// field (§3.1, written LAST in the root body), the terminator and the id-table
// trailer (§3).
func (g *wireGen) emitKnownTypes(st *ir.Struct, recv, root string) {
	g.pf("\t%s.rootObj = %s;\n", recv, root)
	g.pf("\t%s.rootType = %s;\n", recv, typeConst(st))
	reached := ir.PointerReachable(st)
	if len(reached) == 0 {
		return
	}
	g.pf("\t%s.knownTypes.addAll(<int>[", recv)
	for i, t := range reached {
		if i > 0 {
			g.pf(", ")
		}
		g.pf("%s", typeConst(t))
	}
	g.pf("]);\n")
}

func (g *wireGen) emitNumber(st *ir.Struct) {
	g.pf("void %sNumber(WireState s, %s v, Set<Object> open) {\n", dartName(st.Name), st.Name)
	for _, f := range st.Fields {
		g.emitNumberField(f, "\t")
	}
	g.pf("}\n\n")
}

func (g *wireGen) emitNumberField(f *ir.Field, ind string) {
	expr := "v." + fieldMember(f)
	switch {
	case f.IsMap():
		// a map is a by-value edge at its field's position, its entries in
		// ascending key order, each descended before the next (§2.8, §3.1)
		g.pf("%sfor (final e in %s) {\n", ind, mapSorted(f, expr))
		g.pf("%s\t%sNumber(s, e, open);\n", ind, dartName(f.MapEntry.Name))
		g.pf("%s}\n", ind)
	case isUnionField(f):
		g.pf("%s%s(s, %s, open);\n", ind, unionFn(unionRef(f), "Number"), expr)
	case f.Array == ir.ArrayFixed:
		g.emitNumberArray(f, expr, ind)
	case f.CountedOnWire() && unionRef(f) != nil:
		g.pf("%sfor (final e in %s) {\n", ind, expr)
		g.pf("%s\t%s(s, e, open);\n", ind, unionFn(unionRef(f), "Number"))
		g.pf("%s}\n", ind)
	case f.Type.Blob() && f.Array == ir.ArrayNone:
		g.pf("%sif (%s != null) {\n", ind, expr)
		if f.Type.Kind == ir.TString {
			g.pf("%s\ts.noteBlob(_kStringBlobId, utf8.encode(%s!));\n", ind, expr)
		} else {
			g.pf("%s\ts.noteBlob(_kBytesBlobId, %s!);\n", ind, expr)
		}
		g.pf("%s}\n", ind)
	case isPointerTable(f):
		ref := tableRef(f)
		g.pf("%sif (%s != null) {\n", ind, expr)
		g.pf("%s\tfinal node = %s!;\n", ind, expr)
		g.pf("%s\ts.noteTable(%s, node, open, (o) { %sNumber(s, node, o); });\n", ind, typeConst(ref), dartName(ref.Name))
		g.pf("%s}\n", ind)
	case isByValueTable(f):
		ref := tableRef(f)
		g.pf("%s%sNumber(s, %s, open);\n", ind, dartName(ref.Name), expr)
	case f.CountedOnWire():
		ref := tableRef(f)
		if ref == nil {
			return
		}
		if f.Type.Pointer {
			g.pf("%sfor (final e in %s) {\n", ind, expr)
			g.pf("%s\tif (e != null) {\n", ind)
			g.pf("%s\t\tfinal node = e;\n", ind)
			g.pf("%s\t\ts.noteTable(%s, node, open, (o) { %sNumber(s, node, o); });\n", ind, typeConst(ref), dartName(ref.Name))
			g.pf("%s\t}\n", ind)
			g.pf("%s}\n", ind)
		} else {
			g.pf("%sfor (final e in %s) {\n", ind, expr)
			g.pf("%s\t%sNumber(s, e, open);\n", ind, dartName(ref.Name))
			g.pf("%s}\n", ind)
		}
	}
}

func (g *wireGen) emitNodeDispatch() {
	g.pf("void _writeNodeTable(TableWireWriter w) {\n")
	g.pf("\tfinal state = w.state;\n")
	g.pf("\tif (state.nodes.isEmpty) return;\n")
	g.pf("\tfinal ref = state.refFor(_kNodeTableId);\n")
	g.pf("\tw.putLeb(ref);\n")
	g.pf("\tw.putByte(%d);\n", wireKindString)
	g.pf("\tfinal body = TableWireWriter(state);\n")
	g.pf("\tbody.putLeb(state.nodes.length);\n")
	g.pf("\tfor (var i = 0; i < state.nodes.length; i++) {\n")
	g.pf("\t\tbody.putLeb(state.refFor(state.nodeTypes[i]));\n")
	g.pf("\t\tfinal obj = state.nodeObjs[i];\n")
	g.pf("\t\tif (obj == null) {\n")
	g.pf("\t\t\tfinal node = state.nodes[i];\n")
	g.pf("\t\t\tbody.putLeb(node.length);\n")
	g.pf("\t\t\tbody.putBytes(node);\n")
	g.pf("\t\t} else {\n")
	g.pf("\t\t\tfinal sub = TableWireWriter(state);\n")
	g.pf("\t\t\t_writeNodeBody(sub, state.nodeTypes[i], obj);\n")
	g.pf("\t\t\tbody.putLeb(sub.buf.length);\n")
	g.pf("\t\t\tbody.putBytes(sub.buf);\n")
	g.pf("\t\t}\n")
	g.pf("\t}\n")
	g.pf("\tw.putLeb(body.buf.length);\n")
	g.pf("\tw.putBytes(body.buf);\n")
	g.pf("}\n\n")
	g.pf("void _writeNodeBody(TableWireWriter w, int typeId, Object obj) {\n")
	g.pf("\tswitch (typeId) {\n")
	for _, st := range g.closure {
		g.pf("\t\tcase %s: %sWireSaveBody(w, obj as %s); break;\n", typeConst(st), dartName(st.Name), st.Name)
	}
	g.pf("\t\tdefault: throw StateError('no node body for type $typeId (docs/SPEC-TABLES.md §3.1)');\n")
	g.pf("\t}\n}\n\n")
	g.pf("Object? _newNode(int typeId) {\n")
	g.pf("\tswitch (typeId) {\n")
	for _, st := range g.closure {
		g.pf("\t\tcase %s: return %s();\n", typeConst(st), st.Name)
	}
	g.pf("\t\tdefault: return null;\n")
	g.pf("\t}\n}\n\n")
	g.pf("void _loadNodeBody(TableWireReader r, int typeId, Object obj) {\n")
	g.pf("\tswitch (typeId) {\n")
	for _, st := range g.closure {
		g.pf("\t\tcase %s: %sWireLoadBody(r, obj as %s, true); break;\n", typeConst(st), dartName(st.Name), st.Name)
	}
	g.pf("\t}\n}\n\n")
}

func (g *wireGen) emitSave(st *ir.Struct) {
	n := st.Name
	fn := dartName(n) + "WireSave"
	g.pf("Uint8List %s(%s v) {\n", fn, n)
	g.pf("\tfinal state = WireState();\n")
	g.pf("\tstate.indexRoot(v);\n")
	g.pf("\tfinal open = <Object>{v};\n")
	g.pf("\t%sNumber(state, v, open);\n", dartName(n))
	g.pf("\tfinal w = TableWireWriter(state);\n")
	g.pf("\tw.putByte(1);\n")
	g.pf("\t%sWireSaveBodyFields(w, v);\n", dartName(n))
	g.pf("\t_writeNodeTable(w);\n")
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
	g.pf("%s %s(Uint8List data, [TableWireReport? report]) {\n", n, fn)
	g.pf("\tfinal v = %s();\n", n)
	g.pf("\tfinal r = TableWireReader(data, data.length);\n")
	bytesBlob, strBlob := ir.PointerReachableBlobs(st)
	g.pf("\tr.stringBlob = %v;\n", strBlob)
	g.pf("\tr.bytesBlob = %v;\n", bytesBlob)
	g.pf("\tr.path = [1];\n")
	g.emitKnownTypes(st, "r", "v")
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
	g.pf("\t\t\t// the LAST node table wins: a field that rides twice re-reads the same\n")
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
