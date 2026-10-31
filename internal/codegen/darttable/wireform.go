// THE VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1, in Dart: load
// (tolerant, §4) and save (the canonical byte layout the other backends write)
// for every table root, fixed or variable — by-value nesting of tables and
// `type`s, pointers *T, arrays of pointers []*T, counted [..N]T and unbounded
// []T, tables as list elements, bool and every integer kind, `bits(N)`, flags,
// `float32`/`float64`, `string(N)`/`bytes(N)`, `*string`/`*bytes`, maps and
// unions (wireunion.go), fixed positional and enum-keyed arrays
// (wirearrays.go), optional fields and arrays, fixed(I, F) and the 128-bit
// scalars (wirewide.go). A construct none of these carries is
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
	"math"
	"math/big"
	"slices"
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
// enum, string(N), bytes(N), *string/*bytes, a pointer or
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
		if f.Array != ir.ArrayNone && !f.CountedOnWire() && (f.Array != ir.ArrayFixed || f.KeyEnum != "") {
			return false
		}
		return cardWireUnionSeen(un, seen)
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
	g := &wireGen{unit: u, home: home, closure: closure, roots: roots, enums: map[string]bool{}}
	g.emit()
	g.insertEnumImports()
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
	// lenientPayload is set while a keyed array's element reader is emitted
	lenientPayload bool
	// optHad names the presence local of the optional field being emitted
	optHad string
	// enums are the packet enums this library spells by name: their
	// extension types are imported, with `show`, from the declaring library
	enums map[string]bool
}

// enumType records and returns an enum's Dart type: the packet emitter's
// extension type over int, which this library imports.
func (g *wireGen) enumType(e *ir.Enum) string {
	g.enums[e.Name] = true
	return e.Name
}

// enumConst is an enum's named constant for ordinal i — None at 0, the
// i-th declared variant past it — through the imported extension type.
func (g *wireGen) enumConst(e *ir.Enum, i int) string {
	name := g.enumType(e)
	if i == 0 {
		return name + ".none"
	}
	return name + "." + dartName(e.Variants[i-1])
}

// listElemType is dartListElemType, recording an enum element's import.
func (g *wireGen) listElemType(f *ir.Field) string {
	if e, ok := f.Type.Ref.(*ir.Enum); ok && f.Type.Kind == ir.TNamed {
		g.enumType(e)
	}
	return dartListElemType(f)
}

// insertEnumImports adds the enum imports below the dart: imports, once the
// body has named every enum it uses.
func (g *wireGen) insertEnumImports() {
	if len(g.enums) == 0 {
		return
	}
	byFile := map[string][]string{}
	for name := range g.enums {
		file := fixedDeclaringFile(g.unit, name)
		byFile[file] = append(byFile[file], name)
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	var imports strings.Builder
	for _, f := range files {
		names := byFile[f]
		sort.Strings(names)
		one := fmt.Sprintf("import '%s.dart' show %s;\n", f, strings.Join(names, ", "))
		if len(one) <= 81 {
			imports.WriteString(one)
			continue
		}
		fmt.Fprintf(&imports, "import '%s.dart'\n    show\n", f)
		for i, n := range names {
			sep := ","
			if i == len(names)-1 {
				sep = ";"
			}
			fmt.Fprintf(&imports, "        %s%s\n", n, sep)
		}
	}
	const anchor = "import 'dart:typed_data';\n\n"
	src := g.body.String()
	at := strings.Index(src, anchor) + len(anchor)
	g.body.Reset()
	g.body.WriteString(src[:at] + imports.String() + "\n" + src[at:])
}

func (g *wireGen) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

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
		if e, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
			return e.Name // the packet extension type, imported by the wire library
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
	// §16: a declared table can be named directly in a text tree even when
	// another table holds it by value. Give every carried declaration public
	// entry points; generated map-entry tables remain implementation details.
	for _, st := range g.closure {
		if !st.IsTable || st.IsMapEntry() {
			continue
		}
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
		g.pf("const int %s = %s; // fnv1a64(%q)\n", typeConst(st), dartU64Literal(ir.TableWireId(st.WireName())), st.WireName())
	}
	g.pf("\n")
}

// emitRuntime emits the generic form-1 writer/reader (§3, §4), shared by every
// table of the unit. It invents no contract of its own: the byte framing below
// is §3's, read down to the card's closure.
func (g *wireGen) emitRuntime() {
	g.pf(`// The report a tolerant read fills: §4's five counters (unknown, kindMismatch,
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
    // THE ENTRY COUNT HAS A MAXIMUM (docs/SPEC-TABLES.md §3, THE DISTINCTNESS
    // CHECK): a table above 4096 entries is malformed on the whole-wire rule,
    // whatever the buffer would hold.
    if (count > 4096) return 2;
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

// dartFloatOwner names a float field <Owner>.<field> for the non_finite_float
// refusal (docs/SPEC-TABLES.md §3.3), the owner being the table, type or union
// that declares it — the Go leg's floatOwner (internal/codegen/gotable/read.go),
// spelled alike so a refusal names the same field on every leg. A field the
// unit does not hold by pointer (an element copy) is named by the record being
// emitted when that record declares a field of its name, and by its own name
// past that.
func dartFloatOwner(u *ir.Unit, f *ir.Field, cur *ir.Struct) string {
	for _, m := range []map[string]*ir.Struct{u.Tables, u.Structs} {
		for _, st := range m {
			if slices.Contains(st.Fields, f) {
				return st.DeclaredName() + "." + f.Name
			}
		}
	}
	for _, m := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
		for _, un := range m {
			for i := range un.Variants {
				if un.Variants[i].F == f {
					return un.Name + "." + f.Name
				}
			}
		}
	}
	if cur != nil {
		for _, x := range cur.Fields {
			if x.Name == f.Name {
				return cur.DeclaredName() + "." + f.Name
			}
		}
	}
	return f.Name
}

// floatRides is the condition under which a float rides past its default
// elision: its BITS differ from the default's (docs/SPEC-TABLES.md §3.3, -0
// is a value and it rides). For a finite value the bits differ exactly when
// the values differ or both are zero with different signs, so a -0 under a +0
// default rides and a +0 under a declared -0 default rides. A NaN or an
// infinity rides too, and the writer refuses it there.
func floatRides(f *ir.Field, expr, lit string) string {
	def := 0.0
	if f.HasDefault {
		def = f.DefFloat
	}
	if def != 0 {
		return expr + " != " + lit
	}
	if math.Signbit(def) {
		return expr + " != 0 || !" + expr + ".isNegative"
	}
	return expr + " != 0 || " + expr + ".isNegative"
}

func (g *wireGen) floatOwner(f *ir.Field) string { return dartFloatOwner(g.unit, f, g.cur) }

func (g *wireGen) floatDefault(f *ir.Field) string {
	if f.HasDefault {
		return fmt.Sprintf("%g", f.DefFloat)
	}
	return "0.0"
}

func (g *wireGen) enumDefault(f *ir.Field) string {
	e := enumRef(f)
	name := g.enumType(e)
	if f.HasDefault && f.DefVariant != "" {
		for _, v := range e.Variants {
			if v == f.DefVariant {
				return name + "." + dartName(v)
			}
		}
	}
	return name + ".none"
}

// emitSaveBody emits <name>WireSaveBodyFields (the field sequence, no
// terminator) and <name>WireSaveBody (the same plus the zero reference): a
// nested table's body ends at its own terminator, while the ROOT's terminator
// follows the node-table field (§3.1), so the two spellings are both owed.
func (g *wireGen) emitSaveBody(st *ir.Struct) {
	n := st.Name
	// the record a float element copy is named by (floatOwner), restored after
	prev := g.cur
	g.cur = st
	defer func() { g.cur = prev }()
	fields := dartName(n) + "WireSaveBodyFields"
	g.pf("void %s(TableWireWriter w, %s v) {\n", fields, n)
	guards := wireGuards(st)
	for i, f := range st.Fields {
		cond := g.guardCond(st, guards[f.Name])
		if cond == "" {
			g.emitSaveField(f, "\t", i)
			continue
		}
		// A FIELD UNDER A FALSE GUARD IS NOT WRITTEN (§3.1, §4): the wire says
		// what the value says, whatever a tolerant read left in the slot.
		g.pf("\tif (%s) {\n", cond)
		g.emitSaveField(f, "\t\t", i)
		g.pf("\t}\n")
	}
	g.pf("\t_retainTail(w, v);\n")
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
			g.pf("%s\tw.putLeb(w.state.blobIndex(_kStringBlobId, %s!));\n", ind, expr)
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
	case f.Type.Kind == ir.TString:
		g.pf("%sif (%s != %s) {\n", ind, expr, g.stringDefault(f))
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindString)
		g.pf("%s\tfinal b = utf8.encode(%s);\n", ind, expr)
		g.pf("%s\tw.putLeb(b.length);\n", ind)
		g.pf("%s\tw.putBytes(b);\n", ind)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TBytes: // bytes(N) rides as kind 14 over element kind 6 (§3); the String holds one byte a code unit (latin1), so every byte value survives
		// §5: a declared bytes default is storage, not a field occurrence.
		// Comparing with emptiness would write the default into an otherwise
		// empty body and change the Go oracle's canonical wire.
		g.pf("%sif (%s != %s) {\n", ind, expr, g.stringDefault(f))
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
		// ELISION COMPARES BITS (a -0 under a +0 default rides), and a NaN or
		// an infinity is refused before a byte of it is written
		// (docs/SPEC-TABLES.md §3.3).
		kind := wireKindF64
		bits := "_f64Write"
		put := "putU64"
		if f.Type.Kind == ir.TFloat32 {
			kind = wireKindF32
			bits = "_f32Write"
			put = "putU32"
		}
		g.pf("%sif (%s) {\n", ind, floatRides(f, expr, g.floatDefault(f)))
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, kind)
		g.pf("%s\tw.%s(%s(%s, '%s'));\n", ind, put, bits, expr, g.floatOwner(f))
		g.pf("%s}\n", ind)
	case enumRef(f) != nil:
		e := enumRef(f)
		g.pf("%sif (%s != %s) {\n", ind, expr, g.enumDefault(f))
		g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
		g.pf("%s\tw.putByte(%d);\n", ind, wireKindEnum)
		// The value is the variant's id reference; the zero variant (None) has
		// none and rides as reference 0 (§2.9, §3), and the fields after it
		// still follow.
		g.pf("%s\tswitch (%s) {\n", ind, expr)
		for i := range e.Variants {
			vid := ir.TableWireId(e.VariantWireName(i))
			g.pf("%s\t\tcase %d: w.putLeb(w.refFor(%s));\n", ind, i+1, dartU64Literal(vid))
		}
		g.pf("%s\t\tdefault: w.putLeb(0);\n", ind)
		g.pf("%s\t}\n", ind)
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
		g.pf("%s\t\ts.putU32(_f32Write(e, '%s'));\n", ind, g.floatOwner(f))
	case wireKindF64:
		g.pf("%s\t\ts.putU64(_f64Write(e, '%s'));\n", ind, g.floatOwner(f))
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
	// A BY-VALUE BODY IS THE SAME STORAGE across occurrences and is RESET when
	// its id comes round again (internal/tablewire field, Refill): the last
	// occurrence wins whole, so a field only the earlier one carried does not
	// stand. One flag a body field, local to this read.
	for _, f := range st.Fields {
		if isByValueTable(nonOptionalOf(f)) {
			g.pf("\tvar %s = false;\n", seenName(f))
		}
	}
	g.pf("\twhile (true) {\n")
	g.pf("\t\tfinal ref = r.readLeb();\n")
	g.pf("\t\tif (ref == null) { r.report.malformed = true; return; }\n")
	g.pf("\t\tif (ref == 0) return;\n")
	g.pf("\t\tfinal id = r.refToId[ref];\n")
	g.pf("\t\tif (id == null) { r.report.malformed = true; return; }\n")
	g.pf("\t\tif (!r.has(1)) { r.report.malformed = true; return; }\n")
	g.pf("\t\tfinal kind = r.readByte();\n")
	g.pf("\t\tif (id == _kBuildVersionId || id == _kVocabularyId) { r.report.malformed = true; return; }\n")
	// The reserved node-table id is a VARIABLE-class root's own (§3.1): it
	// steps over it and never counts it unknown. A reader that cannot name it
	// (a fixed-class body) counts it unknown like any other id it does not
	// know, which is §4's counter, so it falls to the switch below; inside a
	// nested body it is malformed in either class.
	if ir.VariableTables(g.unit)[n] {
		g.pf("\t\tif (id == _kNodeTableId) {\n")
		g.pf("\t\t\tif (nested) { r.report.malformed = true; return; }\n")
		g.pf("\t\t\tif (!r.skipKind(kind)) { r.report.malformed = true; return; }\n")
		g.pf("\t\t\tcontinue;\n")
		g.pf("\t\t}\n")
	} else {
		g.pf("\t\tif (id == _kNodeTableId && nested) { r.report.malformed = true; return; }\n")
	}
	g.pf("\t\tswitch (id) {\n")
	for i, f := range st.Fields {
		g.emitLoadField(f, "\t\t\t", i)
	}
	g.pf("\t\t\tdefault:\n")
	g.pf("\t\t\t\tr.report.unknown++;\n")
	g.pf("\t\t\t\tif (r.retain != null) {\n")
	g.pf("\t\t\t\t\tfinal start = r.pos;\n")
	g.pf("\t\t\t\t\tif (!r.skipKind(kind)) { r.report.malformed = true; return; }\n")
	// A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE (docs/SPEC-TABLES.md
	// §3.3, §6.6, ruling 3 of 2026-10-27): kind 10's or 11's raw bits, and a
	// NaN or an infinity refuses the read as non_finite_float, named
	// <Table>.0x<id>, and is not captured.
	g.pf("\t\t\t\t\tif ((kind == %d && (readU32(r.data, start) & 0x7f800000) == 0x7f800000) ||\n", wireKindF32)
	g.pf("\t\t\t\t\t\t\t(kind == %d && (readU64(r.data, start) & 0x7ff0000000000000) == 0x7ff0000000000000)) {\n", wireKindF64)
	g.pf("\t\t\t\t\t\tr.report.refuseNonFiniteFloat(_retainedFloatName('%s', id));\n", st.DeclaredName())
	g.pf("\t\t\t\t\t} else if (kind == %d) {\n", wireKindPtr)
	g.pf("\t\t\t\t\t\tr.report.retainLost++;\n")
	g.pf("\t\t\t\t\t} else {\n")
	g.pf("\t\t\t\t\t\tfinal sub = TableWireReader.sub(r, start, r.pos);\n")
	g.pf("\t\t\t\t\t\tfinal resolved = _retainResolve(sub, kind, 0);\n")
	g.pf("\t\t\t\t\t\tif (resolved == null || sub.pos != sub.limit) {\n")
	g.pf("\t\t\t\t\t\t\tr.report.retainLost++;\n")
	g.pf("\t\t\t\t\t\t} else {\n")
	g.pf("\t\t\t\t\t\t\tfinal rec = _RetainRecord(v, List<int>.from(r.path), id, kind, resolved);\n")
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
		start := g.body.Len()
		g.optHad = hadName(f)
		g.emitLoadField(nonOptional(f), ind, ordinal)
		g.optHad = ""
		caseText := g.body.String()[start:]
		// the bare field's own case line is dropped: this case already opened
		caseText = caseText[strings.Index(caseText, "\n")+1:]
		had := hadName(f)
		// an occurrence under a kind the field does not take leaves the field
		// standing, so it comes back to the presence it had (§4)
		caseText = strings.ReplaceAll(caseText, "r.report.kindMismatch++;", "r.report.kindMismatch++; v."+has+" = "+had+";")
		rest := g.body.String()[:start]
		g.body.Reset()
		g.body.WriteString(rest)
		// A READ THAT STOPS THE BODY (framing damage it cannot get past) never
		// reached the field's value: presence is the READ's to set, so the body
		// stops with the presence it had (internal/tablewire readFields sets it
		// only after the field's read returned).
		caseText = strings.ReplaceAll(caseText, "return;", "{ v."+has+" = "+had+"; return; }")
		if strings.Contains(caseText, had) {
			g.pf("%s\tfinal %s = v.%s;\n", ind, had, has)
		}
		g.pf("%s\tv.%s = true;\n", ind, has)
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
		g.pf("%s\t\tif (%s) v.%s = %s();\n", ind, seenName(f), name, ref.Name)
		g.pf("%s\t\t%s = true;\n", ind, seenName(f))
		g.pf("%s\t\t%s(sub, v.%s, true);\n", ind, dartName(ref.Name)+"WireLoadBody", name)
		g.pf("%s\t\tif (sub.pos != sub.limit) { r.report.malformed = true; v.%s = %s(); }\n", ind, name, ref.Name)
		g.pf("%s\t\tr.pos += len;\n", ind)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case f.CountedOnWire(): // []T and [..N]T ride as kind 14 (§2.9, §3)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindArray)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
		g.pf("%s\t\tif (len == null || !r.has(len)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tfinal end = r.pos + len;\n", ind)
		et := g.listElemType(f)
		g.pf("%s\t\tif (end - r.pos < 2) { r.pos = end; break; }\n", ind)
		// THE ARRAY'S HEADER IS READ AGAINST THE PARENT BUFFER, not against the
		// body's own L: a count LEB128 spelled wide enough leaves the cursor past
		// the end it was framed by, and that is a count the body cannot hold, not
		// a missing one (internal/tablewire arrayBody, subTo; \u00a73, \u00a74).
		g.pf("%s\t\tfinal elemKind = r.readByte();\n", ind)
		g.pf("%s\t\tfinal count = r.readLeb();\n", ind)
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
		// THE READ COMMITS TO REPLACE HERE (docs/SPEC-TABLES.md §6.6, §3):
		// the collection is cleared only after the element-kind check above
		// passes, exactly as internal/tablewire arrayBody empties the holder
		// after its own kind check — a repeat under the same id whose payload
		// is of another element kind leaves the previously-decoded value
		// standing rather than wiping it.
		g.pf("%s\t\tv.%s = <%s>[];\n", ind, name, et)
		// a count past a counted array's bound keeps the bound's prefix and
		// counts one clamp; the rest skips via the length (§4,
		// internal/tablewire arrayBody)
		loop := "count"
		if f.Array == ir.ArrayCounted {
			g.pf("%s\t\tvar keep = count;\n", ind)
			g.pf("%s\t\tif (keep < 0 || keep > %d) { keep = %d; r.report.clamped++; }\n", ind, f.ArrayBound, f.ArrayBound)
			loop = "keep"
		} else {
			g.pf("%s\t\tif (count < 0 || count > 0x7FFFFFFF) { r.report.malformed = true; r.pos = end; break; }\n", ind)
		}
		// The body spans from after the header to its own end; a header that
		// ran past that end covers no elements (arrayBody's subTo; \u00a73, \u00a74).
		g.pf("%s\t\tfinal arr = TableWireReader.sub(r, r.pos, end < r.pos ? r.pos : end);\n", ind)
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
	case f.Type.Kind == ir.TString:
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindString)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
		g.pf("%s\t\tif (len == null || !r.has(len)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tif (!_lebValid(r.data, r.pos, len)) { r.report.malformed = true; v.%s = %s; r.pos += len; break; }\n", ind, name, g.stringDefault(f))
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
		g.pf("%s\tv.%s = r.readBool();\n", ind, name)
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
		// A kind this field does not take is skipped and the field KEEPS the value
		// it holds (its default on a fresh read), as the engine's read does (§4).
		g.pf("%s\t\tvar value = v.%s;\n", ind, name)
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
			if !g.isMapKey(f) {
				g.pf("%s\t\t\t\tr.report.widened++;\n", ind)
			}
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
			for b := range 2 {
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
		// A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS (readF32/readF64,
		// docs/SPEC-TABLES.md §3.3), before the bits become a double and before
		// the field is written: the body stops there, and the load lands
		// nothing. A compressed float takes the test before its bounds.
		owner := g.floatOwner(f)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tvar d = v.%s;\n", ind, name)
		g.pf("%s\t\tswitch (kind) {\n", ind)
		g.pf("%s\t\t\tcase %d:\n", ind, declKind)
		if f.Type.Kind == ir.TFloat64 {
			g.pf("%s\t\t\t\tif (!r.has(8)) { r.report.malformed = true; return; }\n", ind)
			g.pf("%s\t\t\t\td = r.readF64(r.pos, '%s'); r.pos += 8;\n", ind, owner)
		} else {
			g.pf("%s\t\t\t\tif (!r.has(4)) { r.report.malformed = true; return; }\n", ind)
			g.pf("%s\t\t\t\td = r.readF32(r.pos, '%s'); r.pos += 4;\n", ind, owner)
		}
		g.pf("%s\t\t\t\tbreak;\n", ind)
		if f.Type.Kind == ir.TFloat64 {
			g.pf("%s\t\t\tcase %d:\n", ind, wireKindF32)
			g.pf("%s\t\t\t\tif (!r.has(4)) { r.report.malformed = true; return; }\n", ind)
			g.pf("%s\t\t\t\td = r.readF32(r.pos, '%s'); r.pos += 4;\n", ind, owner)
			g.pf("%s\t\t\t\tr.report.widened++;\n", ind)
			g.pf("%s\t\t\t\tbreak;\n", ind)
		}
		g.pf("%s\t\t\tdefault:\n", ind)
		g.pf("%s\t\t\t\tr.report.kindMismatch++;\n", ind)
		g.pf("%s\t\t\t\tif (!r.skipKind(kind)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\t\t\tbreak;\n", ind)
		g.pf("%s\t\t}\n", ind)
		g.pf("%s\t\tif (r.report.nonFiniteFloat.isNotEmpty) return;\n", ind)
		if f.HasFloatRange {
			// one past an end clamps and counts, and a -0 lands as +0,
			// uncounted (§3.3, A COMPRESSED FLOAT'S SPECIAL VALUES)
			g.pf("%s\t\tif (d < %s) { d = %s; r.report.clamped++; }\n", ind, fmtFloat(f.FMin), fmtFloat(f.FMin))
			g.pf("%s\t\tif (d > %s) { d = %s; r.report.clamped++; }\n", ind, fmtFloat(f.FMax), fmtFloat(f.FMax))
			g.pf("%s\t\tif (d == 0) d = 0.0;\n", ind)
		}
		g.pf("%s\t\tv.%s = d;\n", ind, name)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case f.Type.Kind == ir.TBytes: // bytes(N) rides as kind 14 over element kind 6 (§3)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindArray)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal len = r.readLeb();\n", ind)
		g.pf("%s\t\tif (len == null || !r.has(len)) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tfinal end = r.pos + len;\n", ind)
		g.pf("%s\t\tif (end - r.pos < 2) { r.pos = end; break; }\n", ind)
		// bytes(N) takes \u00a73's list header under the same rule as every other
		// list: the count is read against the PARENT (internal/tablewire
		// arrayBody), a count past the bound clamps and counts BEFORE the body
		// is consulted, and only the prefix the body covers is kept (\u00a74).
		g.pf("%s\t\tfinal elemKind = r.readByte();\n", ind)
		g.pf("%s\t\tfinal count = r.readLeb();\n", ind)
		g.pf("%s\t\tif (count == null) { r.report.malformed = true; r.pos = end; break; }\n", ind)
		g.pf("%s\t\tif (elemKind != %d) { r.report.kindMismatch++; r.pos = end; break; }\n", ind, wireKindU8)
		g.pf("%s\t\tvar keep = count;\n", ind)
		g.pf("%s\t\tif (keep > %d || keep < 0) { keep = %d; r.report.clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s\t\tfinal arr = TableWireReader.sub(r, r.pos, end < r.pos ? r.pos : end);\n", ind)
		g.pf("%s\t\tvar take = keep;\n", ind)
		g.pf("%s\t\tif (take > arr.limit - arr.pos) take = arr.limit - arr.pos;\n", ind)
		g.pf("%s\t\tif (take < keep) r.report.malformed = true;\n", ind)
		g.pf("%s\t\tv.%s = latin1.decode(arr.data.sublist(arr.pos, arr.pos + take));\n", ind, name)
		g.pf("%s\t\tr.pos = end;\n", ind)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tbreak;\n", ind)
	case enumRef(f) != nil:
		e := enumRef(f)
		g.pf("%s\tif (kind != %d) { r.report.kindMismatch++; if (!r.skipKind(kind)) { r.report.malformed = true; return; } break; }\n", ind, wireKindEnum)
		g.pf("%s\t{\n", ind)
		g.pf("%s\t\tfinal ref2 = r.readLeb();\n", ind)
		g.pf("%s\t\tif (ref2 == null) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tif (ref2 == 0) { v.%s = %s; break; }\n", ind, name, g.enumConst(e, 0))
		g.pf("%s\t\tfinal vid = r.refToId[ref2];\n", ind)
		g.pf("%s\t\tif (vid == null) { r.report.malformed = true; return; }\n", ind)
		g.pf("%s\t\tswitch (vid) {\n", ind)
		for i := range e.Variants {
			vid := ir.TableWireId(e.VariantWireName(i))
			g.pf("%s\t\t\tcase %s: v.%s = %s;\n", ind, dartU64Literal(vid), name, g.enumConst(e, i+1))
		}
		g.pf("%s\t\t\tdefault: v.%s = %s; r.report.unknown++; if (r.retain != null) r.report.retainLost++;\n", ind, name, g.enumConst(e, 0))
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
		g.pf("%s\t\t\t\t%s.add(%s);\n", ind, dest, g.enumConst(e, 0))
		g.pf("%s\t\t\t} else {\n", ind)
		g.pf("%s\t\t\tfinal vid = %s.refToId[vref];\n", ind, arr)
		g.pf("%s\t\t\tif (vid == null) { r.report.malformed = true; break; }\n", ind)
		g.pf("%s\t\t\tswitch (vid) {\n", ind)
		for i := range e.Variants {
			vid := ir.TableWireId(e.VariantWireName(i))
			g.pf("%s\t\t\t\tcase %s: %s.add(%s);\n", ind, dartU64Literal(vid), dest, g.enumConst(e, i+1))
		}
		g.pf("%s\t\t\t\tdefault: %s.add(%s); r.report.unknown++;\n", ind, dest, g.enumConst(e, 0))
		g.pf("%s\t\t\t}\n", ind)
		g.pf("%s\t\t\t}\n", ind)
	case wireKindBool:
		g.pf("%s\t\t\tif (!%s.has(1)) { r.report.malformed = true; break; }\n", ind, arr)
		g.pf("%s\t\t\t%s.add(%s.readBool());\n", ind, dest, arr)
	case wireKindF32:
		g.pf("%s\t\t\tif (!%s.has(4)) { r.report.malformed = true; break; }\n", ind, arr)
		g.pf("%s\t\t\t%s.add(%s.readF32(%s.pos, '%s')); %s.pos += 4;\n", ind, dest, arr, arr, g.floatOwner(f), arr)
	case wireKindF64:
		// f64 widens an f32 element kind (§4). The array already counted widened.
		g.pf("%s\t\t\tif (elemKind == %d) {\n", ind, wireKindF32)
		g.pf("%s\t\t\t\tif (!%s.has(4)) { r.report.malformed = true; break; }\n", ind, arr)
		g.pf("%s\t\t\t\t%s.add(%s.readF32(%s.pos, '%s')); %s.pos += 4;\n", ind, dest, arr, arr, g.floatOwner(f), arr)
		g.pf("%s\t\t\t} else if (elemKind == %d) {\n", ind, wireKindF64)
		g.pf("%s\t\t\t\tif (!%s.has(8)) { r.report.malformed = true; break; }\n", ind, arr)
		g.pf("%s\t\t\t\t%s.add(%s.readF64(%s.pos, '%s')); %s.pos += 8;\n", ind, dest, arr, arr, g.floatOwner(f), arr)
		g.pf("%s\t\t\t} else { r.report.malformed = true; break; }\n", ind)
	default:
		if ir.TableKindWide(elemKind) {
			w := ir.TableKindWidth(elemKind)
			// a 128-bit element may arrive WIDENED from a narrower kind of its
			// ladder (§4): it reads at the wire kind's width
			g.pf("%s\t\t\tfinal kw = elemKind == %d ? %d : _kindWidth(elemKind);\n", ind, elemKind, w)
			g.pf("%s\t\t\tif (!%s.has(kw)) { r.report.malformed = true; break; }\n", ind, arr)
			g.pf("%s\t\t\t{\n", ind)
			g.pf("%s\t\t\t\tvar x = _getWide(%s, kw, %v);\n", ind, arr, ir.TableKindSigned(elemKind))
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
				g.emitElemInt(f, fmt.Sprintf("_signExtend(%s.readByte(), 1)", arr), signed, dest, "r", ind+"\t\t\t\t", fmt.Sprintf("%s.readByte()", arr))
			case 2:
				g.pf("%s\t\t\t\tvar raw = 0;\n", ind)
				for b := range 2 {
					g.pf("%s\t\t\t\traw |= %s.readByte() << %d;\n", ind, arr, 8*b)
				}
				g.emitElemInt(f, "_signExtend(raw, 2)", signed, dest, "r", ind+"\t\t\t\t", "raw")
			case 4:
				g.pf("%s\t\t\t\tfinal raw = readU32(%s.data, %s.pos); %s.pos += 4;\n", ind, arr, arr, arr)
				g.emitElemInt(f, "_signExtend(raw, 4)", signed, dest, "r", ind+"\t\t\t\t", "raw")
			default:
				g.pf("%s\t\t\t\tfinal raw = readU64(%s.data, %s.pos); %s.pos += 8;\n", ind, arr, arr, arr)
				g.emitElemInt(f, "raw", signed, dest, "r", ind+"\t\t\t\t", "raw")
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
	guards := wireGuards(st)
	for _, f := range st.Fields {
		cond := g.guardCond(st, guards[f.Name])
		if cond == "" {
			g.emitNumberField(f, "\t")
			continue
		}
		// A POINTER UNDER A FALSE GUARD IS NOT AN EDGE (§3.1, schema#440)
		g.pf("\tif (%s) {\n", cond)
		g.emitNumberField(f, "\t\t")
		g.pf("\t}\n")
	}
	g.pf("}\n\n")
}

// wireGuardTerm is one conjunct of a guarded field's branch condition: a bool
// field of the same body and the value it must hold.
type wireGuardTerm struct {
	field string
	want  bool
}

// wireGuards maps each guarded field's name to its branch condition, composed
// through nesting (docs/SPEC-TABLES.md §4: a field under a false guard is not
// written). A field with no entry is unguarded and always rides.
func wireGuards(st *ir.Struct) map[string][]wireGuardTerm {
	out := map[string][]wireGuardTerm{}
	var walk func(items []ir.Item, cond []wireGuardTerm)
	walk = func(items []ir.Item, cond []wireGuardTerm) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if len(cond) > 0 {
					out[item.F.Name] = append([]wireGuardTerm(nil), cond...)
				}
			case *ir.Branch:
				then, other := true, false
				if item.Neg {
					then, other = false, true
				}
				walk(item.Then, append(cond[:len(cond):len(cond)], wireGuardTerm{item.Cond, then}))
				walk(item.Else, append(cond[:len(cond):len(cond)], wireGuardTerm{item.Cond, other}))
			}
		}
	}
	walk(st.Items, nil)
	return out
}

// guardCond is the Dart condition under which a guarded field rides: every
// term's bool field holding its wanted value. "" is an unguarded field.
func (g *wireGen) guardCond(st *ir.Struct, terms []wireGuardTerm) string {
	var parts []string
	for _, t := range terms {
		for _, gf := range st.Fields {
			if gf.Name != t.field {
				continue
			}
			if t.want {
				parts = append(parts, "v."+fieldMember(gf))
			} else {
				parts = append(parts, "!v."+fieldMember(gf))
			}
		}
	}
	return strings.Join(parts, " && ")
}

func (g *wireGen) emitNumberField(f *ir.Field, ind string) {
	expr := "v." + fieldMember(f)
	if f.Type.Optional {
		// AN ABSENT OPTIONAL'S TABLE IS NOT DESCENDED (§2.3, §3.1, schema#526):
		// the numbering gates on the presence companion exactly as the save
		// does (emitSaveOptional), so a pointer left inside an absent optional
		// takes no index and costs no record.
		g.pf("%sif (v.%s) {\n", ind, hasMember(f))
		g.emitNumberField(nonOptional(f), ind+"\t")
		g.pf("%s}\n", ind)
		return
	}
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
			g.pf("%s\ts.noteBlob(_kStringBlobId, %s!);\n", ind, expr)
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
	g.pf("\tfinal state = WireState()..refuseSharedBlob = true;\n")
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
	// this read's own refusal (docs/SPEC-TABLES.md §3.3), as the Go leg's
	// tableOpen clears its NonFiniteFloat
	g.pf("\tr.report.nonFiniteFloat = '';\n")
	g.pf("\tfinal verdict = r.open();\n")
	g.pf("\tif (verdict == 1) { r.report.refused = true; return v; }\n")
	// verdict 3 is framing damage on the root body, not the file's: the body
	// is read tolerantly up to the damage (docs/SPEC-TABLES.md §3.3, §4;
	// internal/tablewire/decode.go decodeWith), so it falls through here.
	g.pf("\tif (verdict == 2) { r.report.malformed = true; return v; }\n")
	if ir.VariableTables(g.unit)[n] {
		g.pf("\tr.scanNodeTable();\n")
	}
	g.pf("\tr.pos = 1;\n")
	g.pf("\t%s(r, v, false);\n", dartName(n)+"WireLoadBody")
	// A NON-FINITE FLOAT REFUSES THE READ, the tolerant one too, and outranks
	// any damage the walk met (docs/SPEC-TABLES.md §3.3): nothing lands, and
	// the caller holds the type's defaults.
	g.pf("\tif (r.report.nonFiniteFloat.isNotEmpty) return %s();\n", n)
	g.pf("\treturn v;\n")
	g.pf("}\n\n")
}

// measureSpec is one root's extent-walk spec (docs/SPEC-TABLES.md §6.2): the
// tables and unions Go's extentScan (internal/tablewire/extent.go) can reach
// from the root and from every node record it places, numbered in discovery
// order, and the two Dart lists of _MF maps that describe them.
type measureSpec struct {
	structsVar, unionsVar string
	index                 map[*ir.Struct]int
}

// measureSpec emits the root's _MF lists and returns their names and the
// table numbering. Each field's _MF mirrors one branch of extentScan.field:
// a pointer slot outside an array contributes nothing; a map, list, bounded
// or enum-keyed array is a container whose map or list count carries its
// entry's or element's size (ir.RecordLayout of the map entry,
// ir.ListElementLayout of a list); a by-value table is a nested record; a lone
// by-value union is walked arm by arm (extentScan.union).
func (g *wireGen) measureSpec(root *ir.Struct) measureSpec {
	spec := measureSpec{
		structsVar: "_measureStructs" + root.Name,
		unionsVar:  "_measureUnions" + root.Name,
		index:      map[*ir.Struct]int{},
	}
	var structs []*ir.Struct
	var unions []*ir.Union
	unionIndex := map[*ir.Union]int{}
	var addStruct func(*ir.Struct) int
	var addUnion func(*ir.Union) int
	var visitField func(*ir.Field)
	addStruct = func(st *ir.Struct) int {
		if i, ok := spec.index[st]; ok {
			return i
		}
		spec.index[st] = len(structs)
		structs = append(structs, st)
		for _, f := range st.Fields {
			visitField(f)
		}
		return spec.index[st]
	}
	addUnion = func(un *ir.Union) int {
		if i, ok := unionIndex[un]; ok {
			return i
		}
		unionIndex[un] = len(unions)
		unions = append(unions, un)
		for _, v := range un.Variants {
			if v.F != nil {
				visitField(v.F)
			}
		}
		return unionIndex[un]
	}
	visitField = func(f *ir.Field) {
		if f.IsMap() && f.MapEntry != nil {
			addStruct(f.MapEntry)
		}
		switch t := f.Type.Ref.(type) {
		case *ir.Struct:
			addStruct(t)
		case *ir.Union:
			addUnion(t)
		}
	}
	addStruct(root)
	for _, ps := range ir.PointerReachable(root) {
		addStruct(ps)
	}

	mf := func(f *ir.Field) string {
		mode, elemKind, size, align := 0, 0, int64(0), int64(1)
		child, uni := -1, -1
		counts, keyed, single := false, f.KeyEnum != "", false
		if un, ok := f.Type.Ref.(*ir.Union); ok {
			uni = unionIndex[un]
			single = f.Array == ir.ArrayNone && !f.Type.Pointer
		}
		switch {
		case f.Type.Pointer && f.Array == ir.ArrayNone:
		case f.IsMap() || f.IsList() || f.Array != ir.ArrayNone || f.KeyEnum != "":
			mode = 1
			elemKind = ir.TableWireElemKind(f)
			if f.IsMap() {
				elemKind = ir.TableKindTable
				ml := ir.RecordLayout(g.unit, f.MapEntry)
				size, align = ml.Size, ml.Align
				counts = true
				child = spec.index[f.MapEntry]
			} else {
				if f.IsList() {
					size, align = ir.ListElementLayout(g.unit, f)
					counts = true
				}
				if st, ok := f.Type.Ref.(*ir.Struct); ok {
					child = spec.index[st]
				}
			}
		default:
			if st, ok := f.Type.Ref.(*ir.Struct); ok {
				mode = 2
				child = spec.index[st]
			}
		}
		if align < 1 {
			align = 1
		}
		return fmt.Sprintf("_MF(%d, %d, %d, %t, %d, %d, %t, %d, %d, %t)",
			ir.TableWireFieldKind(f), mode, elemKind, counts, size, align, keyed, child, uni, single)
	}

	g.pf("const %s = <Map<int, _MF>>[\n", spec.structsVar)
	for _, st := range structs {
		g.pf("\t// %s\n\t{\n", st.Name)
		seen := map[uint64]bool{}
		for _, f := range st.Fields {
			// extentScan.record takes the FIRST field whose wire id matches
			id := fieldId(f)
			if seen[id] {
				continue
			}
			seen[id] = true
			g.pf("\t\t%s: %s,\n", dartU64Literal(id), mf(f))
		}
		g.pf("\t},\n")
	}
	g.pf("];\n\n")
	g.pf("const %s = <Map<int, _MF>>[\n", spec.unionsVar)
	for _, un := range unions {
		g.pf("\t// %s\n\t{\n", un.Name)
		seen := map[uint64]bool{}
		for _, v := range un.Variants {
			// extentScan.union walks the first by-value variant whose id matches
			if v.F == nil || v.F.Type.Pointer {
				continue
			}
			id := ir.TableWireId(v.WireName())
			if seen[id] {
				continue
			}
			seen[id] = true
			g.pf("\t\t%s: %s,\n", dartU64Literal(id), mf(v.F))
		}
		g.pf("\t},\n")
	}
	g.pf("];\n\n")
	return spec
}

// emitMeasure emits <name>WireMeasure, the region byte count a caller asks for
// before it loads (docs/SPEC-TABLES.md §6.2): the root body's fixed size plus
// its out-of-line map and list extents (walked as extentScan walks them, into
// map entries, element tables, nested tables and union arms), every reachable *string blob's storage, the
// node records of pointer-reachable tables, and one directory entry a node.
// -1 is a refusal — a form byte this reader does not carry, a trailer it
// cannot resolve, a body that ends early, or a list whose count the framing
// cannot hold. It mirrors the other backends' LoadMeasure and the reference
// tablewire.FileRegionMeasure (internal/tablewire/extent.go).
func (g *wireGen) emitMeasure(st *ir.Struct) {
	spec := g.measureSpec(st)
	n := st.Name
	fn := dartName(n) + "WireMeasure"
	g.pf("int %s(Uint8List data) {\n", fn)
	g.pf("\tfinal r = TableWireReader(data, data.length);\n")
	// The reference FileRegionMeasure reads the id table from the trailer
	// (internal/tablewire/extent.go, decode.go trailer) and does NOT walk the
	// root body to its terminator: a damaged body is §4 framing damage on the
	// BODY, not a refusal to measure (docs/SPEC-TABLES.md §6.2). A form or
	// trailer this reader cannot resolve measures 0, exactly as the reference
	// answers 0, not refused.
	g.pf("\tif (data.length < 9 || data[0] != 1) return 0;\n")
	g.pf("\tfinal idCount = readU64(data, data.length - 8);\n")
	g.pf("\tif (idCount < 0 || idCount > %d || idCount > (data.length - 9) ~/ 8) return 0;\n", ir.TableWireIdTableBound)
	g.pf("\tfinal idStart = data.length - 8 - idCount * 8;\n")
	g.pf("\tif (idStart < 1) return 0;\n")
	g.pf("\tfinal idSeen = <int>{};\n")
	g.pf("\tfor (var k = 0; k < idCount; k++) {\n")
	g.pf("\t\tfinal id = readU64(data, idStart + k * 8);\n")
	g.pf("\t\tif (!idSeen.add(id)) return 0;\n")
	g.pf("\t\tr.refToId[k + 1] = id;\n")
	g.pf("\t}\n")
	g.pf("\tr.entryCount = idCount;\n")
	g.pf("\tr.limit = idStart;\n")
	g.pf("\tr.pos = 1;\n")
	g.pf("\t// the root body and every node record walk Go's extentScan rule (§6.2):\n")
	g.pf("\t// map entries, list and array elements, nested tables and union arms\n")
	g.pf("\tfinal rootScan = _ExtentScan(%s, %s);\n", spec.structsVar, spec.unionsVar)
	g.pf("\tif (!rootScan.record(r, %d)) return -1;\n", spec.index[st])
	g.pf("\tvar total = _round(_round(%d) + rootScan.at);\n", ir.RecordLayout(g.unit, st).Size)
	reachable := ir.PointerReachable(st)
	bytesBlob, stringBlob := ir.PointerReachableBlobs(st)
	g.pf("\tfinal nodeSpec = <int, List<int>>{\n")
	for _, ps := range reachable {
		g.pf("\t\t%s: [%d, %d],\n", typeConst(ps), ir.RecordLayout(g.unit, ps).Size, spec.index[ps])
	}
	g.pf("\t};\n")
	// The node table's FRAMING is scanned to the LAST well-framed kind-12 field
	// under the reserved id (docs/SPEC-TABLES.md §3.1): the reference
	// nodeTableBytes (internal/tablewire/nodetable.go) keeps overwriting its
	// payload and only the last one is scanned, so an earlier field's records
	// are never read. A reserved-id field whose own framing is broken makes the
	// whole table malformed, which the reference measures as 0, not refused. A
	// root-body field the scan cannot skip stops the scan; what it has found is
	// the table.
	g.pf("\tr.pos = 1;\n")
	g.pf("\tvar seen = false;\n")
	g.pf("\tvar unframed = false;\n")
	g.pf("\tvar pstart = 0, pend = 0;\n")
	g.pf("\twhile (true) {\n")
	g.pf("\t\tfinal ref = r.readLeb();\n")
	g.pf("\t\tif (ref == null || ref == 0) break;\n")
	// A reference the trailer does not name is framing damage on the BODY; the
	// reference nodeTableBytes stops there and what it has found is the table
	// (internal/tablewire/nodetable.go). It is not a refusal.
	g.pf("\t\tfinal id = r.refToId[ref];\n")
	g.pf("\t\tif (id == null || !r.has(1)) break;\n")
	g.pf("\t\tfinal kind = r.readByte();\n")
	g.pf("\t\tif (id == _kNodeTableId) {\n")
	g.pf("\t\t\tseen = true;\n")
	g.pf("\t\t\tif (kind != 12) { unframed = true; continue; }\n")
	g.pf("\t\t\tfinal len = r.readLeb();\n")
	g.pf("\t\t\tif (len == null) { unframed = true; continue; }\n")
	g.pf("\t\t\tfinal end = r.pos + len;\n")
	g.pf("\t\t\tif (end > r.limit) { unframed = true; continue; }\n")
	g.pf("\t\t\tpstart = r.pos;\n")
	g.pf("\t\t\tpend = end;\n")
	g.pf("\t\t\tr.pos = end;\n")
	g.pf("\t\t} else {\n")
	g.pf("\t\t\tif (!r.skipKind(kind)) break;\n")
	g.pf("\t\t}\n")
	g.pf("\t}\n")
	g.pf("\tif (unframed) return 0;\n")
	g.pf("\tvar records = 0;\n")
	g.pf("\tvar nodeTotal = 0;\n")
	g.pf("\tif (seen) {\n")
	g.pf("\t\tr.pos = pstart;\n")
	g.pf("\t\tfinal cnt = r.readLeb();\n")
	g.pf("\t\tif (cnt == null) return 0;\n")
	g.pf("\t\tvar read = 0;\n")
	g.pf("\t\twhile (r.pos < pend) {\n")
	g.pf("\t\t\tfinal typeRef = r.readLeb();\n")
	g.pf("\t\t\tif (typeRef == null || typeRef < 1 || typeRef > r.entryCount) return 0;\n")
	g.pf("\t\t\tfinal blen = r.readLeb();\n")
	g.pf("\t\t\tif (blen == null || blen < 0 || blen > pend - r.pos) return 0;\n")
	g.pf("\t\t\tfinal tid = r.refToId[typeRef] ?? 0;\n")
	g.pf("\t\t\tfinal bodyStart = r.pos;\n")
	g.pf("\t\t\tfinal sn = nodeSpec[tid];\n")
	g.pf("\t\t\tif (sn != null) {\n")
	g.pf("\t\t\t\tfinal over = TableWireReader.over(r.data.buffer.asUint8List(r.data.offsetInBytes + bodyStart, blen), r);\n")
	g.pf("\t\t\t\tfinal nodeScan = _ExtentScan(%s, %s);\n", spec.structsVar, spec.unionsVar)
	g.pf("\t\t\t\tif (!nodeScan.record(over, sn[1])) return -1;\n")
	g.pf("\t\t\t\tnodeTotal += _round(_round(sn[0]) + nodeScan.at);\n")
	g.pf("\t\t\t} else if (tid == _kBytesBlobId) {\n")
	if bytesBlob {
		g.pf("\t\t\t\tnodeTotal += _round(8 + blen);\n")
	}
	g.pf("\t\t\t} else if (tid == _kStringBlobId) {\n")
	if stringBlob {
		g.pf("\t\t\t\tnodeTotal += _round(8 + blen + 1);\n")
	}
	g.pf("\t\t\t}\n")
	g.pf("\t\t\tr.pos += blen;\n")
	g.pf("\t\t\trecords++;\n")
	g.pf("\t\t\tread++;\n")
	g.pf("\t\t}\n")
	g.pf("\t\tif (read != cnt) return 0;\n")
	g.pf("\t}\n")
	g.pf("\treturn total + nodeTotal + (records + 1) * 16;\n")
	g.pf("}\n\n")
}

// isMapKey reports whether f is the KEY of the map entry being emitted. A key
// that rides a widening kind counts ONE `widened` for its whole MAP, at the
// map read (emitLoadMap, from the key scan), and never again in the entry's
// body decode (docs/SPEC-TABLES.md §2.8, §4; internal/tablewire bodyAt skips
// the count for a map entry's key and mapField counts `if entryWidened &&
// !widened`).
func (g *wireGen) isMapKey(f *ir.Field) bool {
	return g.cur != nil && g.cur.IsMapEntry() && f.Name == ir.MapKeyFieldName
}

// nonOptionalOf is the bare field under an optional one (a field that is not
// optional is its own bare field).
func nonOptionalOf(f *ir.Field) *ir.Field {
	if f.Type.Optional {
		return nonOptional(f)
	}
	return f
}

// seenName names the local that records a by-value body field has been read in
// this body already.
func seenName(f *ir.Field) string {
	m := fieldMember(f)
	return "seen" + strings.ToUpper(m[:1]) + m[1:]
}
