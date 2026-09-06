// TABLE-wire storage, codec and descriptor emission (docs/SPEC-TABLES.md).
// Readers prefill declared defaults then overlay, skip unknown ids, skip
// kind mismatches, clamp out-of-range values, and count every event.
package cpptable

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cppFieldType maps a field type to its C++ storage spelling, mirroring the
// packet emitter's conventions so closure types from <Base>.h and table
// structs from this header read as one family. The bool reports a
// self-initializing type (a generated struct or union needs no initializer).
func (g *tableGen) cppFieldType(t ir.FieldType) (string, bool) {
	switch t.Kind {
	case ir.TInt, ir.TFixed:
		// a fixed field's storage is the RAW scaled integer of exactly I+F
		// bits in the type's own signedness (SPEC.md §4.3); at 128 bits both
		// families are serialize's pair — native __int128 where the compiler
		// has it, the emulated two-lane struct where it does not — which the
		// packet header already brought in
		if t.Width == 128 {
			if t.Signed {
				return "serialize::int128_t", false
			}
			return "serialize::uint128_t", false
		}
		if t.Signed {
			return fmt.Sprintf("int%d_t", t.Width), false
		}
		return fmt.Sprintf("uint%d_t", t.Width), false
	case ir.TBits:
		if t.Width <= 32 {
			return "uint32_t", false
		}
		return "uint64_t", false
	case ir.TBool:
		return "bool", false
	case ir.TFloat32:
		return "float", false
	case ir.TFloat64:
		return "double", false
	case ir.TNamed:
		g.noteRef(t.Name)
		if _, isUnion := t.Ref.(*ir.Union); isUnion {
			return t.Name, true
		}
		st, isStruct := t.Ref.(*ir.Struct)
		if isStruct && st.CppNative != "" && g.unit.DeclFile[t.Name] != g.file.Base {
			// native type mapping (SPEC §4.2): storage speaks the hand type
			// deriving from the generated basis — same layout, so the
			// relocatability asserts and descriptor offsets still hold
			g.nativeIncludes[st.CppInclude] = true
			return "::" + st.CppNative, true
		}
		return t.Name, isStruct
	}
	return "/* ? */", false
}

// tableIntLit renders an integer literal safely at 64-bit width: unsigned
// values past INT64_MAX need ull, and INT64_MIN has no single-literal form.
func tableIntLit(v *big.Int, signed bool, widthBytes int) string {
	if widthBytes == 16 {
		return tableWideLit(v, signed)
	}
	s := v.String()
	if widthBytes < 8 {
		return s
	}
	if !signed {
		return s + "ull"
	}
	if s == "-9223372036854775808" {
		return "( -9223372036854775807ll - 1 )"
	}
	return s + "ll"
}

// wideLanes is a 128-bit value's two's-complement halves, low lane first —
// the form the descriptors' exact ranges and the composed literals share.
func wideLanes(v *big.Int) (lo, hi *big.Int) {
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	return new(big.Int).And(u, mask), new(big.Int).Rsh(u, 64)
}

// tableWideLit renders a 128-bit constant the way the packet emitter does:
// C++ has no 128-bit literal, so the value composes from its two lanes in the
// unsigned domain, exact for native and emulated storage alike, and a signed
// value is the bit-preserving conversion of that.
func tableWideLit(v *big.Int, signed bool) string {
	lo, hi := wideLanes(v)
	composed := fmt.Sprintf("( ( serialize::uint128_t( %sull ) << 64 ) | serialize::uint128_t( %sull ) )", hi, lo)
	if signed {
		return "serialize::int128_t" + composed
	}
	return composed
}

// tableStorageRange is the inclusive range an integer storage of the given
// width can hold.
func tableStorageRange(signed bool, bits int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(bits-1))
		return new(big.Int).Neg(hi), new(big.Int).Sub(hi, one)
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

// tableClampEnds answers which ends of a declared min/max range a read can
// actually clamp at. The decode local is the wire kind's own width, so a
// bound sitting ON that width's limit is a comparison no decoded value can
// satisfy and the emitter drops it — the same "this check cannot fire" test
// the bits(N) width clamp already applies when N is the storage width.
// docs/SPEC-TABLES.md §4's semantics are untouched: an elided end is one
// that could never have clamped or counted. It is also a build error to keep
// it, and the two compilers split the halves: gcc reds `decoded_v < 0ull`
// (-Wtype-limits) and clang reds `decoded_v < -128`
// (-Wtautological-type-limit-compare), neither catching the other's
// (issue #342, `make tables-clamp-limits`).
func tableClampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	lo, hi := tableStorageRange(signed, widthBytes*8)
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return false, false
	}
	return rlo.Cmp(lo) > 0, rhi.Cmp(hi) < 0
}

// fieldDefaultExpr renders the C++ expression a field's default compares
// against on the write side (elision) — identical literals to the NSDMIs.
func (g *tableGen) fieldDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return formatFloat(f.DefFloat, true)
		}
		return "0.0f"
	case ir.TFloat64:
		if f.HasDefault {
			return formatFloat(f.DefFloat, false)
		}
		return "0.0"
	case ir.TInt, ir.TBits, ir.TFixed:
		// a fixed default is held RAW in the IR (units × 2^F, SPEC.md §4.6),
		// which is what the storage holds, so it renders like any integer
		if f.HasDefault && f.DefInt != nil {
			signed := ir.TableKindSigned(ir.TableScalarKind(f))
			width := 4
			if f.Type.Width > 32 {
				width = 8
			}
			if f.Type.Width == 128 {
				width = 16
			}
			return tableIntLit(f.DefInt, signed, width)
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return f.Type.Name + "::" + f.DefVariant
			}
			return f.Type.Name + "::None"
		case *ir.Flags:
			return flagsDefaultExpr(f)
		}
	}
	return "0"
}

// ---- storage (table declarations only; closure types come from <Base>.h) ----

func (g *tableGen) emitTableStruct(st *ir.Struct) {
	g.pf("%s", ir.DocComment(st.Doc, "", "//"))
	g.pf("// table %s — TABLE-wire storage: relocatable, bounded, defaults in the\n", st.Name)
	g.pf("// member initializers (docs/SPEC-TABLES.md)\n")
	g.pf("struct %s {\n", st.Name)
	prevGuard := ""
	for _, f := range st.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				g.pf("\n    // %s — guarded fields stay off the wire when the guard says so;\n", f.Guard)
				g.pf("    // a read's prefilled defaults stand in for the untaken side\n")
			} else {
				g.pf("\n")
			}
			prevGuard = f.Guard
		}
		g.pf("%s", ir.DocComment(f.Doc, "    ", "//"))
		g.emitTableStorageField(f)
	}
	g.pf("};\n\n")
}

func (g *tableGen) emitTableStorageField(f *ir.Field) {
	if f.IsMap() {
		// A MAP FIELD IS SIXTEEN BYTES (docs/SPEC-TABLES.md §2.8, §7.2): a
		// self-relative reference to the entry array and the live count, then
		// padding to eight — one member of a TableMap, so a port that walked
		// two pieces would account for twelve bytes where sixteen are written.
		// The ENTRIES are not here: they are by-value records inside the
		// holder's node extent, laid after the record's own storage.
		g.pf("    TableMap<%s> %s; // %s — the sorted entry array, empty until an insert\n", f.MapEntry.Name, f.Name, ir.TableTypeSpelling(f))
		return
	}
	if f.IsList() {
		// AN UNBOUNDED ARRAY FIELD IS SIXTEEN BYTES (docs/SPEC-TABLES.md §2.9,
		// §7.2): the map's slot exactly, a self-relative reference to the
		// element array and the live count, then padding to eight. The ELEMENTS
		// are not here: they are by-value records inside the holder's node
		// extent, laid after the record's own storage.
		g.pf("    %s %s; // %s: the element array, empty until an Add\n", g.listStorageType(f), f.Name, ir.TableTypeSpelling(f))
		return
	}
	if f.Type.Pointer {
		// a pointer is EIGHT BYTES and no address: an arena offset while the
		// builder is mutable, a self-relative delta once packed. That is what
		// keeps a pointer-bearing table relocatable in both forms. An ARRAY
		// of pointers is that slot per element (docs/SPEC-TABLES.md §2.1). A BYTE
		// BUFFER is the same slot naming a blob node (docs/SPEC-TABLES.md §2.5).
		if f.Type.Blob() {
			g.pf("    TableRef %s; // *%s — a byte buffer at its used size, null until assigned\n", f.Name, blobWord(f))
			return
		}
		g.noteRef(f.Type.Name)
		switch f.Array {
		case ir.ArrayFixed:
			g.pf("    TableRef %s[%d]; // [%d]*%s — every slot null until assigned\n", f.Name, f.ArrayBound, f.ArrayBound, f.Type.Name)
		case ir.ArrayCounted:
			g.pf("    TableRef %s[%d]; // [..%d]*%s — used count beside it; every slot null until assigned\n", f.Name, f.ArrayBound, f.ArrayBound, f.Type.Name)
			g.pf("    int32_t %s_count = 0;\n", f.Name)
		default:
			g.pf("    TableRef %s; // *%s — null until assigned\n", f.Name, f.Type.Name)
		}
		return
	}
	typ, selfInit := g.cppFieldType(f.Type)
	if f.Type.Width == 128 && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) {
		// SIXTEEN BYTES AT SIXTEEN on every compiler (docs/SPEC-TABLES.md §7.2,
		// §19.3): native __int128 is already 16-aligned and the emulated pair
		// is not, and the layout the cook and the block are laid out by is
		// the compiler's one model, asserted below
		typ = "alignas( 16 ) " + typ
	}
	switch {
	case f.Type.Kind == ir.TString && hasByteDefault(f):
		g.pf("    char %s[%d + 1] = %s; // string(%d): max length, used length beside it; the declared default\n", f.Name, f.Type.Size, cStringLit(f.DefBytes), f.Type.Size)
		g.pf("    int32_t %s_length = %d;\n", f.Name, len(f.DefBytes))
	case f.Type.Kind == ir.TString:
		g.pf("    char %s[%d + 1] = {}; // string(%d): max length, used length beside it\n", f.Name, f.Type.Size, f.Type.Size)
		g.pf("    int32_t %s_length = 0;\n", f.Name)
	case f.Type.Kind == ir.TBytes && hasByteDefault(f):
		g.pf("    uint8_t %s[%d] = %s; // bytes(%d): fixed buffer, used length beside it; the declared default\n", f.Name, f.Type.Size, byteListLit(f.DefBytes), f.Type.Size)
		g.pf("    int32_t %s_length = %d;\n", f.Name, len(f.DefBytes))
	case f.Type.Kind == ir.TBytes:
		g.pf("    uint8_t %s[%d] = {}; // bytes(%d): fixed buffer, used length beside it\n", f.Name, f.Type.Size, f.Type.Size)
		g.pf("    int32_t %s_length = 0;\n", f.Name)
	case f.KeyEnum != "":
		// ONE SLOT PER NAMED VARIANT, the key k at index k-1: nothing is
		// stored for None, and the accessor is the only place the shift
		// appears. Every named slot exists, so there is no count companion,
		// and the type derives its own extent from the enum — nothing outside
		// the array names its size (docs/SPEC-TABLES.md §2.4).
		g.noteRef(f.KeyEnum)
		g.pf("    TableKeyed<%s, %s> %s; // [%s]: one slot per named variant, keyed by the value\n",
			typ, f.KeyEnum, f.Name, f.KeyEnum)
	case f.Array == ir.ArrayFixed:
		g.pf("    %s %s[%d]%s;\n", typ, f.Name, f.ArrayBound, tableArrayInit(selfInit))
	case f.Array == ir.ArrayCounted:
		g.pf("    %s %s[%d]%s; // used count beside it; count in [0, %d]\n", typ, f.Name, f.ArrayBound, tableArrayInit(selfInit), f.ArrayBound)
		g.pf("    int32_t %s_count = 0;\n", f.Name)
	default:
		init := ""
		if !selfInit {
			init = " = " + g.fieldDefaultExpr(f)
		}
		g.pf("    %s %s%s;\n", typ, f.Name, init)
	}
	if f.Type.Optional {
		// `?T` — the value plus its presence bool, and nothing else: the
		// holder stays a fixed-size struct (docs/SPEC-TABLES.md §2.3). PRESENCE,
		// not content, decides whether the field rides.
		g.pf("    bool %s_present = false; // ?%s: absent until set\n", f.Name, tableFieldTypeName(f))
	}
}

// tableArrayInit renders an array member's initializer. An element type that
// initializes itself — a generated struct, a union, a native mapping — needs
// none: default-initializing the array runs the element type's own member
// initializers, and value-initializing it does the same, so ` = {}` states
// what already holds. It is also the expensive half of #320 (below), which is
// why the redundant form is not kept for symmetry. A scalar element type has
// no initializers of its own and keeps the braces.
func tableArrayInit(selfInit bool) string {
	if selfInit {
		return ""
	}
	return " = {}"
}

// ---- prefill: the declared defaults, one member at a time ----
//
// Every site that needs a closure member's declared defaults calls its
// `<T>Reset` — the read path before it overlays, and the descriptor's reset
// column. None of them writes `T{}` over the whole object.
//
// THE REASON IS A COMPILE-TIME ONE, MEASURED (#320). cl 19.51 expands a
// value-initialisation of a large aggregate element by element in its front
// end, at O(bytes) rather than O(declarations). Against blockdemo::RenderFrame
// — 7,879,320 bytes over ~105,000 rows — each `T{}` cost ~6 s and ` = {}` on
// the array members another ~5 s, so a translation unit with that table in
// scope cost cl ~20 s against clang's 0.15 s, all of it in the front end
// (c1xx 19.85 s, c2 0.03 s, zero functions generated). Giving one element the
// defaults and copying it across the array is the same work at run time and
// the cost of a single element at compile time.
func (g *tableGen) emitTableResetDeclarations(members []*ir.Struct) {
	g.pf("// ---- prefill: the declared defaults, in place (docs/SPEC-TABLES.md) ----\n\n")
	for _, st := range members {
		g.pf("inline void %sReset( %s & value );\n", st.Name, st.Name)
	}
	g.pf("\n")
}

func (g *tableGen) emitTableReset(st *ir.Struct) {
	if !st.IsTable {
		// a closure `type` is declared in <Base>.h and bounded by a packet: one
		// value-init says exactly what its own initializers say, and costs the
		// size of a packet field to compile
		g.pf("inline void %sReset( %s & value ) { value = %s(); }\n\n", st.Name, st.Name, st.Name)
		return
	}
	g.pf("inline void %sReset( %s & value )\n{\n", st.Name, st.Name)
	if len(st.Fields) == 0 {
		g.pf("    (void) value;\n")
	}
	for _, f := range st.Fields {
		g.emitTableResetField(f)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitTableResetField(f *ir.Field) {
	if f.IsMap() {
		// a fresh map is EMPTY (docs/SPEC-TABLES.md §2.8): the reference is
		// null in both encodings and the live count is zero. The builder's
		// head and its segments are the arena's, and Reset does not free them
		// — the arena's own reset is what reclaims a dead entry's storage.
		g.pf("    value.%s.entries.value = 0; // %s: empty\n", f.Name, ir.TableTypeSpelling(f))
		g.pf("    value.%s.count = 0;\n", f.Name)
		g.pf("    value.%s.padding = 0;\n", f.Name)
		return
	}
	if f.IsList() {
		// a fresh list is EMPTY (docs/SPEC-TABLES.md §2.9), on the map's terms:
		// the reference is null in both encodings and the live count is zero
		g.pf("    value.%s.elements.value = 0; // %s: empty\n", f.Name, ir.TableTypeSpelling(f))
		g.pf("    value.%s.count = 0;\n", f.Name)
		g.pf("    value.%s.padding = 0;\n", f.Name)
		return
	}
	if f.Type.Pointer {
		switch f.Array {
		case ir.ArrayNone:
			g.pf("    value.%s.value = 0; // *%s — null\n", f.Name, pointeeName(f))
		default:
			g.pf("    for ( int32_t i = 0; i < %d; i++ ) { value.%s[i].value = 0; } // [%d]*%s — every slot null\n", f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
			if f.Array == ir.ArrayCounted {
				g.pf("    value.%s_count = 0;\n", f.Name)
			}
		}
		return
	}
	typ, selfInit := g.cppFieldType(f.Type)
	switch {
	case f.Type.Kind == ir.TString && hasByteDefault(f):
		g.pf("    memset( value.%s, 0, sizeof( value.%s ) );\n", f.Name, f.Name)
		g.pf("    memcpy( value.%s, %s, %d ); // the declared default\n", f.Name, cStringLit(f.DefBytes), len(f.DefBytes))
		g.pf("    value.%s_length = %d;\n", f.Name, len(f.DefBytes))
	case f.Type.Kind == ir.TBytes && hasByteDefault(f):
		g.emitBytesDefaultLocal(f)
		g.pf("    memset( value.%s, 0, sizeof( value.%s ) );\n", f.Name, f.Name)
		g.pf("    memcpy( value.%s, %s_default, %d ); // the declared default\n", f.Name, f.Name, len(f.DefBytes))
		g.pf("    value.%s_length = %d;\n", f.Name, len(f.DefBytes))
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("    memset( value.%s, 0, sizeof( value.%s ) );\n", f.Name, f.Name)
		g.pf("    value.%s_length = 0;\n", f.Name)
	case f.KeyEnum != "":
		g.emitTableResetArray("value."+f.Name+".slots", f.ArrayBound, typ, selfInit, f)
	case f.Array == ir.ArrayFixed:
		g.emitTableResetArray("value."+f.Name, f.ArrayBound, typ, selfInit, f)
	case f.Array == ir.ArrayCounted:
		g.emitTableResetArray("value."+f.Name, f.ArrayBound, typ, selfInit, f)
		g.pf("    value.%s_count = 0;\n", f.Name)
	case selfInit:
		g.emitTableResetOne("value."+f.Name, typ, f)
	default:
		g.pf("    value.%s = %s;\n", f.Name, g.fieldDefaultExpr(f))
	}
	if f.Type.Optional {
		g.pf("    value.%s_present = false;\n", f.Name)
	}
}

// emitTableResetArray gives ONE element the declared defaults and copies it
// across the rest. Compiling this costs one element whatever the bound is.
func (g *tableGen) emitTableResetArray(expr string, bound int64, typ string, selfInit bool, f *ir.Field) {
	if bound <= 0 {
		return
	}
	if !selfInit {
		// a scalar element's array carries ` = {}`, which gives every element a
		// zero whatever the field's own default says (docs/SPEC-TABLES.md): a memset
		// is that, exactly
		g.pf("    memset( %s, 0, sizeof( %s ) );\n", expr, expr)
		return
	}
	g.emitTableResetOne(expr+"[0]", typ, f)
	if bound > 1 {
		g.pf("    for ( int32_t i = 1; i < %d; i++ ) { %s[i] = %s[0]; }\n", bound, expr, expr)
	}
}

// emitTableResetOne gives one self-initializing member its declared defaults:
// through the element type's own Reset where this header emits one — so a
// table nested inside a table stays O(declarations) too — and otherwise, for a
// union or a native mapping, through the value-init its own declaration means.
func (g *tableGen) emitTableResetOne(expr, typ string, f *ir.Field) {
	if name, ok := g.tableResetName(f.Type, typ); ok {
		g.pf("    %sReset( %s );\n", name, expr)
		return
	}
	g.pf("    %s = %s();\n", expr, typ)
}

// tableResetName names the member type's Reset when the storage spelling IS
// the generated type. A native mapping stores a hand type whose Reset no
// header emits, and a union is not a struct; both take the value-init their
// own declaration means.
func (g *tableGen) tableResetName(t ir.FieldType, typ string) (string, bool) {
	if _, ok := t.Ref.(*ir.Struct); !ok || typ != t.Name {
		return "", false
	}
	return t.Name, true
}

// ---- enum identity on the table wire (docs/SPEC-TABLES.md §5) ----

// keyedSlots renders a keyed array's RAW slot storage, the form the codecs
// index by slot number rather than by variant.
//
// A TABLE's keyed field is a TableKeyed<>, whose slots sit behind `.slots`; a
// closure `type`'s field is its PACKET storage — a plain array — because a
// type's struct is a raw struct emitted by the packet backend, and nothing on
// this wire changes that (docs/SPEC-TABLES.md §2.4). Both are `E.Max` elements with
// the key k at index k-1, so only the spelling differs.
func (g *tableGen) keyedSlots(owner string, f *ir.Field) string {
	if g.owner != nil && g.owner.IsTable {
		return owner + f.Name + ".slots"
	}
	return owner + f.Name
}

// arrayBase renders the indexable storage of any array field: a keyed one's
// raw slots, and every other one's plain array. `access` is the owner
// expression WITH its member operator — "value.", "src.", "node->".
func (g *tableGen) arrayBase(access string, f *ir.Field) string {
	name := access + f.Name
	if f.KeyEnum != "" && g.owner != nil && g.owner.IsTable {
		return name + ".slots"
	}
	return name
}

// enumRef returns the enum a field's values come from, or nil.
func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// tableEnums collects, in first-use order, every enum whose values ride in the
// members this file emits codecs for.
func tableEnums(members []*ir.Struct) []*ir.Enum {
	seen := map[string]bool{}
	var out []*ir.Enum
	add := func(e *ir.Enum) {
		if e != nil && !seen[e.Name] {
			seen[e.Name] = true
			out = append(out, e)
		}
	}
	for _, st := range members {
		for _, f := range st.Fields {
			// an enum-keyed array's KEY rides as a variant hash too, so its
			// enum needs the identity pair even when no field has that type
			add(f.KeyEnumRef)
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if e, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
				add(e)
			}
			// AN ENUM ARM rides as a variant hash exactly as an enum field
			// does (docs/SPEC-TABLES.md §2.6), through a nested union arm
			// too, so its identity pair is owed here as well
			if un, isUnion := f.Type.Ref.(*ir.Union); isUnion {
				addArmEnums(un, add, map[*ir.Union]bool{})
			}
		}
	}
	return out
}

func addArmEnums(un *ir.Union, add func(*ir.Enum), seen map[*ir.Union]bool) {
	if seen[un] {
		return
	}
	seen[un] = true
	for _, v := range un.Variants {
		if v.F == nil || v.F.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := v.F.Type.Ref.(type) {
		case *ir.Enum:
			add(ref)
		case *ir.Union:
			addArmEnums(ref, add, seen)
		}
	}
}

// emitEnumIdentity emits one enum's value <-> table-wire id pair. Behind a
// macro guard, not `#pragma once`: two files of a unit may both reach the same
// enum, and each emits the pair into its own header — one definition survives
// per translation unit whatever the include order.
func (g *tableGen) emitEnumIdentity(e *ir.Enum) {
	guard := strings.ToUpper(g.unit.Package) + "_SCHEMA_TABLE_ENUM_" + strings.ToUpper(e.Name)
	g.pf("// %s on the TABLE wire: a value rides under its OWN kind 30, carrying the\n", e.Name)
	g.pf("// REFERENCE to its variant name's id, whatever the declaration-side storage\n")
	g.pf("// width — so a variant may be added anywhere, removed, or reordered and old\n")
	g.pf("// data still reads (docs/SPEC-TABLES.md §3, §5). None is the ZERO REFERENCE,\n")
	g.pf("// the one value that names no id, so no declared variant can be mistaken for it.\n")
	g.pf("#ifndef %s\n#define %s\n", guard, guard)
	g.pf("inline bool TableEnumRef( TableIds & ids, %s value, uint64_t & ref )\n{\n", e.Name)
	g.pf("    switch ( value )\n    {\n")
	g.pf("        case %s::None: ref = 0; return true;\n", e.Name)
	for i, v := range e.Variants {
		g.pf("        case %s::%s: ref = %s; return true;\n", e.Name, v, g.wireRef(ir.TableWireId(e.VariantWireName(i))))
	}
	g.pf("        default: return false; // no variant names this value: no wire identity\n")
	g.pf("    }\n}\n")
	if g.anyVariable {
		// the same answer over the RETAIN family's merged table
		// (docs/SPEC-TABLES.md §6.6): a key or a variant this build can name
		// takes its entry from the generated table exactly as it always did,
		// and the overload is what lets one codec serve both families.
		g.pf("inline bool TableEnumRef( TableRetainIds & ids, %s value, uint64_t & ref )\n{\n", e.Name)
		g.pf("    switch ( value )\n    {\n")
		g.pf("        case %s::None: ref = 0; return true;\n", e.Name)
		for i, v := range e.Variants {
			g.pf("        case %s::%s: ref = %s; return true;\n", e.Name, v, g.wireRef(ir.TableWireId(e.VariantWireName(i))))
		}
		g.pf("        default: return false;\n")
		g.pf("    }\n}\n")
	}
	g.pf("inline bool TableEnumNamed( %s value )\n{\n", e.Name)
	g.pf("    switch ( value )\n    {\n")
	g.pf("        case %s::None: return true;\n", e.Name)
	for _, v := range e.Variants {
		g.pf("        case %s::%s: return true;\n", e.Name, v)
	}
	g.pf("        default: return false;\n")
	g.pf("    }\n}\n")
	// THE MESSAGE FORM's half: the SLOT, a literal, with no id table in sight
	// (docs/SPEC-TABLES.md §3.3)
	g.pf("inline bool TableEnumSlot( %s value, uint64_t & slot )\n{\n", e.Name)
	g.pf("    switch ( value )\n    {\n")
	g.pf("        case %s::None: slot = 0; return true;\n", e.Name)
	for _, v := range e.Variants {
		g.pf("        case %s::%s: slot = %d; return true;\n", e.Name, v, g.slots[ir.TableVocabularyEntry{Id: ir.TableWireId(v)}.Key()])
	}
	g.pf("        default: return false; // no variant names this value: no wire identity\n")
	g.pf("    }\n}\n")
	g.pf("inline bool TableEnumId( %s value, uint64_t & id )\n{\n", e.Name)
	g.pf("    switch ( value )\n    {\n")
	g.pf("        case %s::None: id = 0; return true;\n", e.Name)
	for i, v := range e.Variants {
		g.pf("        case %s::%s: id = 0x%016xull; return true;\n", e.Name, v, ir.TableWireId(e.VariantWireName(i)))
	}
	g.pf("        default: return false; // no variant names this value: no wire identity\n")
	g.pf("    }\n}\n")
	g.pf("inline bool TableEnumValue( uint64_t id, %s & out )\n{\n", e.Name)
	g.pf("    switch ( id )\n    {\n")
	for i, v := range e.Variants {
		g.pf("        case 0x%016xull: out = %s::%s; return true;\n", ir.TableWireId(e.VariantWireName(i)), e.Name, v)
	}
	g.pf("        default: return false; // an id this build cannot name\n")
	g.pf("    }\n}\n")
	g.pf("#endif // %s\n\n", guard)
}

// ---- guards ----

// tableGuardExprs composes each guarded field's branch condition from the
// wire tree ("value.a && !value.b" for nesting) so the writer and measurer
// keep untaken-branch fields off the wire — TLV's native optionality carries
// the branch, and the reader's prefilled defaults stand in for the untaken
// side.
func tableGuardExprs(st *ir.Struct) map[string]string {
	return guardWalk(st, "value.")
}

// tableGuardStrings is the value-free twin for the reflection descriptors
// ("at_rest", "!at_rest", "active && has_target").
func tableGuardStrings(st *ir.Struct) map[string]string {
	return guardWalk(st, "")
}

func guardWalk(st *ir.Struct, prefix string) map[string]string {
	guards := map[string]string{}
	var walk func(items []ir.Item, cond string)
	walk = func(items []ir.Item, cond string) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if cond != "" {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				pos, neg := prefix+item.Cond, "!"+prefix+item.Cond
				if item.Neg {
					pos, neg = neg, pos
				}
				and := func(a, b string) string {
					if a == "" {
						return b
					}
					return a + " && " + b
				}
				walk(item.Then, and(cond, pos))
				walk(item.Else, and(cond, neg))
			}
		}
	}
	walk(st.Items, "")
	return guards
}

// ---- measure ----

// emitTableMeasure emits <X>Measure (<X>MeasureBody where pointers reach): the
// EXACT encoded size of a value,
// no writing — the parallel-generation lever. Every nested table on the wire
// is length-prefixed, so a caller can measure subtables in parallel,
// prefix-sum offsets, and scatter-write disjoint ranges from N workers.
// Mirrors TableWrite's elision decisions branch for branch: for any value,
// Save writes exactly this many bytes into a buffer of exactly this size. A value violating its storage invariants (a count or length outside
// its bound, an out-of-range union tag) measures as -1, exactly as the
// write side refuses it.
// framed is a length-shaped payload's cost: its own canonical length, then the
// bytes.
func framed(length string) string {
	return "TableLebBytes( (uint64_t) ( " + length + " ) ) + ( " + length + " )"
}

func (g *tableGen) emitTableMeasure(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("template <typename Ctx>\ninline int64_t %s( const Ctx & ctx, const TableNumbering & numbering, %s & ids, const %s & value%s )\n{\n",
			g.verb(st.Name, "MeasureBody"), g.idsType(), st.Name, g.retainParams())
		if g.noVariableEdges(st) {
			g.pf("    (void) ctx; (void) numbering;\n")
		}
	} else {
		g.pf("inline int64_t %s( %s & ids, const %s & value%s )\n{\n",
			g.verb(st.Name, "MeasureBody"), g.idsType(), st.Name, g.retainParams())
	}
	if len(st.Fields) == 0 {
		g.pf("    (void) value; (void) ids; // empty type: presence is the payload\n")
	}
	g.pf("    int64_t bytes = 1; // the ZERO REFERENCE that ends the body\n")
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if ( %s )\n    {\n", cond)
			g.indent = "    "
			g.emitTableMeasureField(f)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitTableMeasureField(f)
	}
	if g.retain {
		// WHERE THEY GO BACK: at the END of their own body, in the order
		// retained (docs/SPEC-TABLES.md §6.6). A body carrying a retained
		// field therefore does not ELIDE either, and needs no rule of its own
		// to say so: the tail is bytes, and elision asks whether a body has any.
		g.pf("    bytes += TableRetainTailMeasure( retain, ids, path );\n")
	}
	g.pf("    return bytes;\n}\n\n")
	if g.retain || g.isVar(st.Name) || st.IsMapEntry() {
		return
	}
	// the buffer-level entry: the FORM BYTE, the ROOT BODY and the ID TABLE,
	// which is the whole of a saved table (docs/SPEC-TABLES.md §3)
	g.pf("inline int64_t %sMeasure( const %s & value )\n{\n", st.Name, st.Name)
	g.pf("    TableIds ids;\n")
	g.pf("    const int64_t body = %sMeasureBody( ids, value );\n", st.Name)
	g.pf("    if ( body < 0 || ids.overflow ) { return -1; }\n")
	g.pf("    return 1 + body + TableIdsBytes( ids );\n}\n\n")
}

// emitArrayBodyMeasure accumulates one ARRAY BODY's length into `into`: the
// element kind byte, the count, and the elements at their own framing
// (docs/SPEC-TABLES.md §3). `n` is the count expression and `access` the
// element expression carrying one %s for the loop variable.
func (g *tableGen) emitArrayBodyMeasure(f *ir.Field, elemKind int, into, n, access, ind, onBad, sfx string) {
	i := "elem_i" + sfx
	// an element is a STEP, and its index is the second half of the pair
	// (docs/SPEC-TABLES.md §6.6)
	was := g.elemIndex
	g.elemIndex = i
	defer func() { g.elemIndex = was }()
	g.pf("%s%s += 1 + TableLebBytes( (uint64_t) ( %s ) ); // the element kind byte and the count\n", ind, into, n)
	switch elemKind {
	case tkTable:
		g.pf("%sfor ( int32_t %s = 0; %s < %s; %s++ )\n%s{\n", ind, i, i, n, i, ind)
		g.pf("%s    const int64_t elem_bytes%s = %s;\n", ind, sfx, g.measureCall(f, f.Type.Name, fmt.Sprintf(access, i)))
		g.pf("%s    if ( elem_bytes%s < 0 ) { %s }\n", ind, sfx, onBad)
		g.pf("%s    %s += %s;\n%s}\n", ind, into, framed("elem_bytes"+sfx), ind)
	case tkEnum:
		g.pf("%sfor ( int32_t %s = 0; %s < %s; %s++ )\n%s{\n", ind, i, i, n, i, ind)
		g.pf("%s    uint64_t elem_ref%s = 0;\n", ind, sfx)
		g.pf("%s    if ( !TableEnumRef( ids, %s, elem_ref%s ) ) { %s } // no variant names this value\n", ind, fmt.Sprintf(access, i), sfx, onBad)
		g.pf("%s    %s += TableLebBytes( elem_ref%s );\n%s}\n", ind, into, sfx, ind)
	case tkNodeIndex:
		g.pf("%sfor ( int32_t %s = 0; %s < %s; %s++ )\n%s{\n", ind, i, i, n, i, ind)
		g.emitSlotIndexMeasure(f, fmt.Sprintf(access, i), into, ind+"    ", onBad, sfx)
		g.pf("%s}\n", ind)
	case tkUnion:
		g.pf("%sfor ( int32_t %s = 0; %s < %s; %s++ )\n%s{\n", ind, i, i, n, i, ind)
		g.emitUnionElementMeasure(f, fmt.Sprintf(access, i), into, ind+"    ", sfx+"u")
		g.pf("%s}\n", ind)
	default:
		g.emitEnumElementCheckAt(f, access, n, ind, onBad, sfx)
		g.pf("%s%s += (int64_t) ( %s ) * %d;\n", ind, into, n, tableKindWidth(elemKind))
	}
}

// emitSlotIndexMeasure adds ONE node index's canonical length to `into`: an
// index is a LEB128 like every other number on this wire, so its width depends
// on the numbering and a measure has to resolve it exactly as a save does
// (docs/SPEC-TABLES.md §3.1).
func (g *tableGen) emitSlotIndexMeasure(f *ir.Field, expr, into, ind, onBad, sfx string) {
	t, pointee, index := f.Type.Name, "slot_pointee"+sfx, "slot_index"+sfx
	g.pf("%s{\n%s    const %s * %s = %sAt( ctx, %s );\n", ind, ind, t, pointee, t, expr)
	g.pf("%s    uint64_t %s = 0;\n", ind, index)
	g.pf("%s    if ( %s != NULL && !TableNumberingIndex( numbering, (const void *) %s, %s ) ) { %s }\n", ind, pointee, pointee, index, onBad)
	g.pf("%s    %s += TableLebBytes( %s );\n%s}\n", ind, into, index, ind)
}

func (g *tableGen) emitTableMeasureField(f *ir.Field) {
	if f.IsMap() {
		g.emitMapMeasureField(f)
		return
	}
	if f.IsList() {
		g.emitListMeasureField(f)
		return
	}
	id := tableFieldWireId(f)
	kind := tableScalarKind(f)
	width := tableKindWidth(kind)
	elemKind := kind
	if f.Type.Pointer && f.Array != ir.ArrayNone {
		elemKind = tkNodeIndex
	}
	switch {
	case f.Type.Optional:
		// an optional's PRESENCE is the payload: it rides even when the value
		// is entirely default, exactly as a pointer's pointee does — otherwise
		// absent and present-at-default would be one value on the wire
		g.pf("    if ( value.%s_present ) // ?%s: presence decides, not content\n    {\n", f.Name, tableFieldTypeName(f))
		switch {
		case f.Array != ir.ArrayNone:
			// an OPTIONAL ARRAY rides whole when present (docs/SPEC-TABLES.md
			// §2.3): the live count, ZERO INCLUDED — the two-byte body — where
			// the plain counted array elides at zero
			count := strconv.FormatInt(f.ArrayBound, 10)
			if f.Array == ir.ArrayCounted {
				g.pf("        if ( value.%s_count < 0 || value.%s_count > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
				count = fmt.Sprintf("value.%s_count", f.Name)
			}
			g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
			g.pf("        int64_t body_%s = 0;\n", f.Name)
			g.emitArrayBodyMeasure(f, elemKind, "body_"+f.Name, count, "value."+f.Name+"[%s]", "        ", "return -1;", "")
			g.pf("        bytes += TableLebBytes( ref_%s ) + 1 + %s; // %s\n", f.Name, framed("body_"+f.Name), f.Name)
		case kind == tkTable:
			g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
			g.pf("        const int64_t body_%s = %s;\n", f.Name, g.measureCall(f, f.Type.Name, "value."+f.Name))
			g.pf("        if ( body_%s < 0 ) { return -1; }\n", f.Name)
			g.pf("        bytes += TableLebBytes( ref_%s ) + 1 + %s; // %s\n", f.Name, framed("body_"+f.Name), f.Name)
		case kind == tkEnum:
			g.pf("        uint64_t variant_%s = 0;\n", f.Name)
			g.pf("        if ( !TableEnumNamed( value.%s ) ) { return -1; } // no variant names this value\n", f.Name)
			g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
			g.pf("        if ( !TableEnumRef( ids, value.%s, variant_%s ) ) { return -1; }\n", f.Name, f.Name)
			g.pf("        bytes += TableLebBytes( ref_%s ) + 1 + TableLebBytes( variant_%s ); // %s: the variant's reference\n", f.Name, f.Name, f.Name)
		default:
			g.pf("        bytes += %s + %d; // %s\n", g.hdrBytes(id), width, f.Name)
		}
		g.pf("    }\n")
	case f.KeyEnum != "":
		// enum-keyed: the body carries (key reference, L, element) triples, so
		// a slot lands by NAME however the enum moved. A slot at its default
		// elides like any default, and an empty array elides whole.
		g.pf("    {\n")
		g.pf("        const int32_t mark_%s = ids.count;\n", f.Name)
		g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
		g.pf("        int64_t pairs_%s = 0, body_%s = 0;\n", f.Name, f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) // [%s]: every stored slot is a named variant's\n        {\n", f.ArrayBound, f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "            ", "return -1;")
		switch kind {
		case tkTable:
			g.pf("            pairs_%s++; body_%s += TableLebBytes( key_ref ) + %s;\n", f.Name, f.Name, framed("elem_bytes"))
		case tkEnum:
			g.pf("            pairs_%s++; body_%s += TableLebBytes( key_ref ) + %s;\n", f.Name, f.Name, framed("TableLebBytes( element_ref )"))
		default:
			g.pf("            pairs_%s++; body_%s += TableLebBytes( key_ref ) + TableLebBytes( %d ) + %d;\n", f.Name, f.Name, width, width)
		}
		g.pf("        }\n")
		g.pf("        if ( pairs_%s > 0 )\n        {\n", f.Name)
		g.pf("            const int64_t whole_%s = 1 + TableLebBytes( (uint64_t) pairs_%s ) + body_%s;\n", f.Name, f.Name, f.Name)
		g.pf("            bytes += TableLebBytes( ref_%s ) + 1 + %s; // %s\n", f.Name, framed("whole_"+f.Name), f.Name)
		g.pf("        }\n")
		g.pf("        else { ids.truncate( mark_%s ); } // an ELIDED field costs nothing in the id table either\n", f.Name)
		g.pf("    }\n")
	case f.Type.Pointer && f.Array == ir.ArrayCounted:
		// an ARRAY OF POINTERS (docs/SPEC-TABLES.md §2.1, §3.1): kind 14 with element
		// kind 17, N, then N node indices. CONTENT decides, as for any by-value
		// array: an empty one elides, and a live slot rides as its index, null as 0.
		g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value.%s_count > 0 )\n    {\n", f.Name)
		g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
		g.pf("        int64_t body_%s = 0;\n", f.Name)
		g.emitArrayBodyMeasure(f, tkNodeIndex, "body_"+f.Name, fmt.Sprintf("value.%s_count", f.Name), "value."+f.Name+"[%s]", "        ", "return -1;", "")
		g.pf("        bytes += TableLebBytes( ref_%s ) + 1 + %s; // %s: [..%d]*%s\n", f.Name, framed("body_"+f.Name), f.Name, f.ArrayBound, f.Type.Name)
		g.pf("    }\n")
	case f.Type.Pointer && f.Array == ir.ArrayFixed:
		// a FIXED array of pointers holding only null is all-default and elides;
		// one non-null slot makes it ride whole, every slot as its index (§3.1)
		g.pf("    {\n")
		g.pf("        bool any_%s = false;\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( %sAt( ctx, value.%s[i] ) != NULL ) { any_%s = true; break; } }\n", f.ArrayBound, f.Type.Name, f.Name, f.Name)
		g.pf("        if ( any_%s )\n        {\n", f.Name)
		g.pf("            const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
		g.pf("            int64_t body_%s = 0;\n", f.Name)
		g.emitArrayBodyMeasure(f, tkNodeIndex, "body_"+f.Name, strconv.FormatInt(f.ArrayBound, 10), "value."+f.Name+"[%s]", "            ", "return -1;", "")
		g.pf("            bytes += TableLebBytes( ref_%s ) + 1 + %s; // %s: [%d]*%s\n", f.Name, framed("body_"+f.Name), f.Name, f.ArrayBound, f.Type.Name)
		g.pf("        }\n")
		g.pf("    }\n")
	case f.Type.Blob():
		g.pf("    {\n")
		g.pf("        const TableBlob * blob_%s = TableBlobAt( ctx, value.%s ); // *%s\n", f.Name, f.Name, blobWord(f))
		g.pf("        // A BYTE BUFFER RIDES AS A NODE INDEX too (docs/SPEC-TABLES.md §2.5,\n")
		g.pf("        // §3.1): the header and the index here, the bytes themselves as a\n")
		g.pf("        // record in the node table. Null is elided and a non-null blob always\n")
		g.pf("        // rides, even at length zero — null and empty are two values.\n")
		g.pf("        if ( blob_%s != NULL )\n        {\n", f.Name)
		g.pf("            uint64_t index_%s = 0;\n", f.Name)
		g.pf("            if ( !TableNumberingIndex( numbering, (const void *) blob_%s, index_%s ) ) { return -1; }\n", f.Name, f.Name)
		g.pf("            bytes += %s + TableLebBytes( index_%s );\n", g.hdrBytes(id), f.Name)
		g.pf("        }\n    }\n")
	case f.Type.Pointer:
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee_%s = %sAt( ctx, value.%s ); // *%s\n", t, f.Name, t, f.Name, t)
		g.pf("        // A POINTER RIDES AS A NODE INDEX (docs/SPEC-TABLES.md §3.1): the\n")
		g.pf("        // header and the index and nothing below it, because the pointee's\n")
		g.pf("        // body is in the node table and not here. NULL IS ELIDED — absence\n")
		g.pf("        // and null are one value — and a non-null pointer ALWAYS rides, even\n")
		g.pf("        // when its node's body is entirely default.\n")
		g.pf("        if ( pointee_%s != NULL )\n        {\n", f.Name)
		g.pf("            uint64_t index_%s = 0;\n", f.Name)
		g.pf("            if ( !TableNumberingIndex( numbering, (const void *) pointee_%s, index_%s ) ) { return -1; }\n", f.Name, f.Name)
		g.pf("            bytes += %s + TableLebBytes( index_%s );\n", g.hdrBytes(id), f.Name)
		g.pf("        }\n    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if ( value.%s_length < 0 || value.%s_length > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.Type.Size)
		g.pf("    if ( %s ) { bytes += %s + %s; } // %s\n", g.lengthRidesTest(f), g.hdrBytes(id), framed(fmt.Sprintf("value.%s_length", f.Name)), f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if ( value.%s_length < 0 || value.%s_length > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.Type.Size)
		g.emitBytesDefaultLocal(f)
		g.pf("    if ( %s )\n    {\n", g.lengthRidesTest(f))
		g.pf("        const int64_t body_%s = 1 + TableLebBytes( (uint64_t) value.%s_length ) + value.%s_length;\n", f.Name, f.Name, f.Name)
		g.pf("        bytes += %s + %s; // %s\n", g.hdrBytes(id), framed("body_"+f.Name), f.Name)
		g.pf("    }\n")
	case f.Array == ir.ArrayCounted:
		g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value.%s_count > 0 )\n    {\n", f.Name)
		g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
		g.pf("        int64_t body_%s = 0;\n", f.Name)
		g.emitArrayBodyMeasure(f, elemKind, "body_"+f.Name, fmt.Sprintf("value.%s_count", f.Name), "value."+f.Name+"[%s]", "        ", "return -1;", "")
		g.pf("        bytes += TableLebBytes( ref_%s ) + 1 + %s; // %s\n", f.Name, framed("body_"+f.Name), f.Name)
		g.pf("    }\n")
	case f.Array == ir.ArrayFixed && (kind == tkTable || kind == tkUnion):
		// a fixed array of tables always rides — position is identity there —
		// and a fixed array of unions holding only None is all-default and
		// elides, one set element making it ride whole (§3)
		g.pf("    {\n")
		guard := ""
		if kind == tkUnion {
			g.pf("        bool any_%s = false;\n", f.Name)
			g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i].type != %sType::None ) { any_%s = true; break; } }\n", f.ArrayBound, f.Name, f.Type.Name, f.Name)
			g.pf("        if ( any_%s )\n        {\n", f.Name)
			guard = "    "
		}
		g.pf("        %sconst uint64_t ref_%s = %s;\n", guard, f.Name, g.wireRef(id))
		g.pf("        %sint64_t body_%s = 0;\n", guard, f.Name)
		g.emitArrayBodyMeasure(f, elemKind, "body_"+f.Name, strconv.FormatInt(f.ArrayBound, 10), "value."+f.Name+"[%s]", "        "+guard, "return -1;", "")
		g.pf("        %sbytes += TableLebBytes( ref_%s ) + 1 + %s; // %s (fixed [%d])\n", guard, f.Name, framed("body_"+f.Name), f.Name, f.ArrayBound)
		if kind == tkUnion {
			g.pf("        }\n")
		}
		g.pf("    }\n")
	case f.Array == ir.ArrayFixed:
		g.pf("    {\n")
		g.pf("        bool all_default_%s = true;\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i] != %s ) { all_default_%s = false; break; } }\n",
			f.ArrayBound, f.Name, g.fieldDefaultExpr(f), f.Name)
		g.pf("        if ( !all_default_%s )\n        {\n", f.Name)
		g.pf("            const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
		g.pf("            int64_t body_%s = 0;\n", f.Name)
		g.emitArrayBodyMeasure(f, elemKind, "body_"+f.Name, strconv.FormatInt(f.ArrayBound, 10), "value."+f.Name+"[%s]", "            ", "return -1;", "")
		g.pf("            bytes += TableLebBytes( ref_%s ) + 1 + %s; // %s\n", f.Name, framed("body_"+f.Name), f.Name)
		g.pf("        }\n    }\n")
	case kind == tkUnion:
		// AN ARM HEADER IS A FIELD HEADER (§3): the arm id reference, the arm's
		// KIND byte, L, then L bytes of arm payload — one framing for a field
		// and an arm alike
		un := f.Type.Ref.(*ir.Union)
		g.pf("    if ( value.%s.type != %sType::None ) // None elides — the absence of the field is the None\n    {\n", f.Name, un.Name)
		g.pf("        bytes += %s;\n", g.hdrBytes(id))
		g.emitUnionPayloadMeasure(f, "value."+f.Name, "bytes", "        ", "")
		g.pf("    }\n")
	case kind == tkTable:
		g.pf("    {\n")
		g.pf("        const int32_t mark_%s = ids.count;\n", f.Name)
		g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
		g.pf("        const int64_t body_%s = %s;\n", f.Name, g.measureCall(f, f.Type.Name, "value."+f.Name))
		g.pf("        if ( body_%s < 0 ) { return -1; }\n", f.Name)
		g.pf("        if ( body_%s > 1 ) { bytes += TableLebBytes( ref_%s ) + 1 + %s; } // %s\n", f.Name, f.Name, framed("body_"+f.Name), f.Name)
		g.pf("        else { ids.truncate( mark_%s ); } // an all-default nested table elides, and costs no entry\n", f.Name)
		g.pf("    }\n")
	case kind == tkEnum:
		g.pf("    if ( value.%s != %s )\n    {\n", f.Name, g.fieldDefaultExpr(f))
		g.pf("        if ( !TableEnumNamed( value.%s ) ) { return -1; } // no variant names this value\n", f.Name)
		g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
		g.pf("        uint64_t variant_%s = 0;\n", f.Name)
		g.pf("        if ( !TableEnumRef( ids, value.%s, variant_%s ) ) { return -1; }\n", f.Name, f.Name)
		g.pf("        bytes += TableLebBytes( ref_%s ) + 1 + TableLebBytes( variant_%s ); // %s: the variant's reference\n    }\n", f.Name, f.Name, f.Name)
	default:
		g.pf("    if ( value.%s != %s ) { bytes += %s + %d; } // %s\n", f.Name, g.fieldDefaultExpr(f), g.hdrBytes(id), width, f.Name)
	}
}

// emitUnionPayloadMeasure adds a SET union's payload to `into`: the arm id
// reference, the arm's kind byte, its L and its L bytes (docs/SPEC-TABLES.md
// §3). The caller has already established that the tag is not None.
func (g *tableGen) emitUnionPayloadMeasure(f *ir.Field, expr, into, ind, sfx string) {
	un := f.Type.Ref.(*ir.Union)
	body := "arm_payload" + sfx
	wasField := g.unionField
	g.unionField = f
	defer func() { g.unionField = wasField }()
	g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
	g.pf("%s    case %sType::None: break;\n", ind, un.Name)
	for ai, v := range un.Variants {
		g.noteRef(v.Type)
		g.pf("%s    case %sType::%s:\n%s    {\n", ind, un.Name, ir.GoExportName(v.Name), ind)
		g.pf("%s        int64_t %s = 0;\n", ind, body)
		g.pf("%s        const uint64_t arm_ref%s = %s;\n", ind, sfx, g.wireRef(ir.TableWireId(v.WireName())))
		g.inStep(strconv.Itoa(ai), func() { g.emitArmMeasure(v, expr, body, ind+"        ", "return -1;", sfx) })
		g.pf("%s        %s += TableLebBytes( arm_ref%s ) + 1 + %s;\n", ind, into, sfx, framed(body))
		g.pf("%s        break;\n%s    }\n", ind, ind)
	}
	g.pf("%s    default: return -1; // invalid tag — the write side refuses it too\n%s}\n", ind, ind)
}

// emitUnionElementMeasure adds one element of an ARRAY OF UNIONS to `into`
// (docs/SPEC-TABLES.md §2.6, §3): the single zero byte for None, and the arm
// header and payload for a set arm. A tag no arm names measures as -1, exactly
// as the write side refuses it.
func (g *tableGen) emitUnionElementMeasure(f *ir.Field, expr, into, ind, sfx string) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%sif ( %s.type == %sType::None ) { %s += 1; } // a None element is the zero reference in its place\n", ind, expr, un.Name, into)
	g.pf("%selse\n%s{\n", ind, ind)
	g.inElementStep(f, func() { g.emitUnionPayloadMeasure(f, expr, into, ind+"    ", sfx) })
	g.pf("%s}\n", ind)
}

// emitKeyedSlotRides emits the head of an enum-keyed array's per-slot loop
// (docs/SPEC-TABLES.md §2.4, §3.2). It elides a slot holding its default, refuses a
// slot whose value or whose KEY no variant names — a value with no wire
// identity is refused rather than silently renamed, the enum rule applied to
// slots — and leaves `key_ref` holding the slot's key REFERENCE. For a table
// element `elem_bytes` holds the measured body, so measure and save decide
// elision on the same number; for an enum element `element_ref` holds the
// resolved reference, and the save path writes THAT rather than resolving the
// same value twice.
//
// THE KEY'S REFERENCE IS INTERNED BEFORE THE ELEMENT'S IDS, because the key
// rides first, and a slot that turns out to elide undoes both.
func (g *tableGen) emitKeyedSlotRides(f *ir.Field, kind int, ind, onBad string) {
	// a KEYED slot's own index is the element index of the step (§6.6, §3.2)
	was := g.elemIndex
	g.elemIndex = "i"
	defer func() { g.elemIndex = was }()
	expr := g.keyedSlots("value.", f) + "[i]"
	if kind != tkTable {
		switch kind {
		case tkEnum:
			g.pf("%sif ( %s == %s ) { continue; } // a default slot elides\n", ind, expr, g.fieldDefaultExpr(f))
			g.pf("%sif ( !TableEnumNamed( %s ) ) { %s } // no variant names this value\n", ind, expr, onBad)
		default:
			g.pf("%sif ( %s == %s ) { continue; } // a default slot elides\n", ind, expr, g.fieldDefaultExpr(f))
		}
	}
	if kind == tkTable {
		// only a TABLE element decides elision on a measured body, so only it
		// can intern a key that then has to be undone
		g.pf("%sconst int32_t slot_mark = ids.count;\n", ind)
	}
	g.pf("%suint64_t key_ref = 0;\n", ind)
	g.pf("%sif ( !TableEnumRef( ids, %s( i + 1 ), key_ref ) || key_ref == 0 ) { %s } // i is the STORAGE index; the key it holds is i + 1\n",
		ind, f.KeyEnum, onBad)
	switch kind {
	case tkTable:
		g.pf("%sconst int64_t elem_bytes = %s;\n", ind, g.measureCall(f, f.Type.Name, expr))
		g.pf("%sif ( elem_bytes < 0 ) { %s }\n", ind, onBad)
		g.pf("%sif ( elem_bytes <= 1 ) { ids.truncate( slot_mark ); continue; } // an all-default slot elides\n", ind)
	case tkEnum:
		g.pf("%suint64_t element_ref = 0;\n", ind)
		g.pf("%sif ( !TableEnumRef( ids, %s, element_ref ) ) { %s }\n", ind, expr, onBad)
	}
}

// emitEnumElementCheckAt validates an enum ARRAY's elements before they ride: a
// value no variant names has no wire identity, so the value is refused rather
// than silently written as None (the union tag's rule, applied to enums). Its
// locals carry the `sfx` SUFFIX, which an ARM's elements need: an arm nests
// inside walks that already spell `i` (docs/SPEC-TABLES.md §2.6). `expr` may
// carry one %s, which takes the loop variable.
func (g *tableGen) emitEnumElementCheckAt(f *ir.Field, expr, count, ind, onBad, sfx string) {
	if enumRef(f) == nil {
		return
	}
	i := "named_i" + sfx
	if strings.Contains(expr, "%s") {
		expr = fmt.Sprintf(expr, i)
	}
	g.pf("%sfor ( int32_t %s = 0; %s < %s; %s++ ) // %s: every element must be nameable\n", ind, i, i, count, i, f.Name)
	g.pf("%s{\n%s    if ( !TableEnumNamed( %s ) ) { %s }\n%s}\n", ind, ind, expr, onBad, ind)
}

// ---- write / save ----

// The FIXED class's bodies are force-inlined and the VARIABLE-LENGTH class's
// are not, and the line between them is the one place a table's save/load call
// graph can hold a cycle. A fixed table has no pointer in its by-value closure,
// so its bodies nest by value and a cycle would make `sizeof` infinite — the
// graph is a DAG by construction and forcing it flat always terminates. A
// pointered body reaches its pointee through the depth-carrying template form
// (docs/SPEC-TABLES.md §3.1), which a self-referential declaration makes
// directly recursive, and a recursive always_inline is a compile error under
// gcc. So the switch that already separates the two classes is the guard.
func (g *tableGen) emitTableWrite(st *ir.Struct) {
	if g.isVar(st.Name) {
		// The ROOT's fields and the node table's field are fields of ONE body,
		// so the terminator is written by whoever knows the body is finished:
		// the wrapper below for a nested body, and the wire surface for a root
		// that still owes its node table (docs/SPEC-TABLES.md §3.1).
		g.pf("template <typename Ctx>\ninline bool %s( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, %s & ids, const %s & value%s )\n{\n",
			g.verb(st.Name, "SaveBodyFields"), g.idsType(), st.Name, g.retainParams())
		if g.noVariableEdges(st) {
			g.pf("    (void) ctx; (void) numbering;\n")
		}
	} else {
		g.pf("%s bool %s( TableWriter & w, %s & ids, const %s & value%s )\n{\n",
			tableInlineMacro(g.unit.Package), g.verb(st.Name, "SaveBody"), g.idsType(), st.Name, g.retainParams())
	}
	if len(st.Fields) == 0 {
		g.pf("    (void) value; (void) ids; // empty type: presence is the payload\n")
	}
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if ( %s )\n    {\n", cond)
			g.indent = "    "
			g.emitTableWriteField(f)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitTableWriteField(f)
	}
	if g.retain {
		// the RETAINED TAIL, and in a ROOT body it lands here rather than
		// after the node table: the order is the root's declared fields, then
		// the retained tail, then the node table (docs/SPEC-TABLES.md §6.6)
		g.pf("    if ( !TableRetainTailSave( retain, ids, w, path ) ) { return false; }\n")
	}
	if g.isVar(st.Name) {
		g.pf("    return !w.overflow;\n}\n\n")
		// the ordinary body: the fields, then the terminator. A nested body is
		// finished when its fields are, and only a ROOT owes a node table.
		g.pf("template <typename Ctx>\ninline bool %s( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, %s & ids, const %s & value%s )\n{\n",
			g.verb(st.Name, "SaveBody"), g.idsType(), st.Name, g.retainParams())
		if g.retain {
			g.pf("    if ( !%s( ctx, numbering, w, ids, value, retain, path ) ) { return false; }\n", g.verb(st.Name, "SaveBodyFields"))
		} else {
			g.pf("    if ( !%sSaveBodyFields( ctx, numbering, w, ids, value ) ) { return false; }\n", st.Name)
		}
		g.pf("    w.put8( 0 ); // the ZERO REFERENCE that ends the body\n")
		g.pf("    return !w.overflow;\n}\n\n")
		return
	}
	g.pf("    w.put8( 0 ); // the ZERO REFERENCE that ends the body\n")
	g.pf("    return !w.overflow;\n}\n\n")
}

// emitTableSave emits the buffer-level entry of the measure/save pair:
// <X>Save writes into a caller-provided buffer and returns the bytes written —
// exactly <X>Measure's answer — or -1 when the buffer is too small. No
// allocation anywhere: the caller owns the buffer, and the id table is a local
// sized by the unit's own closure.
func (g *tableGen) emitTableSave(st *ir.Struct) {
	if g.retain {
		return // the retain family's buffer-level entry is the ROOT's own (§6.6)
	}
	if g.isVar(st.Name) {
		return // a variable-length table's Save takes a builder or a region root
	}
	if st.IsMapEntry() {
		// a map's ENTRY is not a root (docs/SPEC-TABLES.md §2.8): it is
		// reached only through the map that generates it, so its walk is all
		// it carries and there is no buffer-level entry point to it
		return
	}
	g.pf("inline int64_t %sSave( const %s & value, uint8_t * buffer, int64_t capacity )\n{\n", st.Name, st.Name)
	g.pf("    TableWriter w( buffer, capacity );\n")
	g.pf("    TableIds ids;\n")
	g.pf("    w.put8( kTableWireForm ); // the FORM BYTE is the whole header (§3)\n")
	g.pf("    if ( !%sSaveBody( w, ids, value ) || ids.overflow ) { return -1; }\n", st.Name)
	g.pf("    TableIdsWrite( w, ids ); // the ID TABLE is the last thing in the file\n")
	g.pf("    if ( w.overflow ) { return -1; }\n")
	g.pf("    return w.offset; // == %sMeasure( value )\n}\n\n", st.Name)
}

// emitArrayBodyWrite writes one ARRAY BODY: the element kind byte, the count,
// and the elements at their own framing (docs/SPEC-TABLES.md §3).
func (g *tableGen) emitArrayBodyWrite(f *ir.Field, elemKind int, n, access, ind, sfx string) {
	i := "elem_i" + sfx
	was := g.elemIndex
	g.elemIndex = i
	defer func() { g.elemIndex = was }()
	g.pf("%sw.put8( %d ); w.putleb( (uint64_t) ( %s ) );\n", ind, elemKind, n)
	g.pf("%sfor ( int32_t %s = 0; %s < %s; %s++ )\n%s{\n", ind, i, i, n, i, ind)
	g.emitTableWriteElement(f, elemKind, fmt.Sprintf(access, i), ind+"    ", sfx)
	g.pf("%s}\n", ind)
}

// emitArrayField writes a whole array FIELD: the header, the body's own
// length — measured first, because a canonical LEB128 cannot be patched in
// place — and the body.
func (g *tableGen) emitArrayField(f *ir.Field, id uint64, elemKind int, n, access, ind string) {
	g.pf("%sconst uint64_t ref_%s = %s;\n", ind, f.Name, g.wireRef(id))
	g.pf("%sint64_t body_%s = 0;\n", ind, f.Name)
	g.emitArrayBodyMeasure(f, elemKind, "body_"+f.Name, n, access, ind, "return false;", "")
	g.pf("%sw.putleb( ref_%s ); w.put8( %d ); w.putleb( (uint64_t) body_%s ); // %s\n", ind, f.Name, tkArray, f.Name, f.Name)
	g.emitArrayBodyWrite(f, elemKind, n, access, ind, "")
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	if f.IsMap() {
		g.emitMapWriteField(f)
		return
	}
	if f.IsList() {
		g.emitListWriteField(f)
		return
	}
	id := tableFieldWireId(f)
	kind := tableScalarKind(f)
	elemKind := kind
	if f.Type.Pointer && f.Array != ir.ArrayNone {
		elemKind = tkNodeIndex
	}
	if f.Type.Kind == ir.TNamed {
		g.noteRef(f.Type.Name)
	}
	switch {
	case f.Type.Optional:
		// present: the payload ALWAYS rides, all-default included — the
		// pointer's rule, and what makes ?T, *T and a plain nesting
		// wire-identical (docs/SPEC-TABLES.md §2.3, §3.1)
		g.pf("    if ( value.%s_present ) // ?%s\n    {\n", f.Name, tableFieldTypeName(f))
		switch {
		case f.Array != ir.ArrayNone:
			count := strconv.FormatInt(f.ArrayBound, 10)
			if f.Array == ir.ArrayCounted {
				g.pf("        if ( value.%s_count < 0 || value.%s_count > %d ) { return false; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
				count = fmt.Sprintf("value.%s_count", f.Name)
			}
			g.emitArrayField(f, id, elemKind, count, "value."+f.Name+"[%s]", "        ")
		case kind == tkTable:
			g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
			g.pf("        const int64_t body_%s = %s;\n", f.Name, g.measureCall(f, f.Type.Name, "value."+f.Name))
			g.pf("        if ( body_%s < 0 ) return false; // storage invariant, refused as measure refuses it\n", f.Name)
			g.pf("        w.putleb( ref_%s ); w.put8( %d ); w.putleb( (uint64_t) body_%s ); // %s\n", f.Name, tkTable, f.Name, f.Name)
			g.pf("        if ( !%s ) return false;\n", g.saveCall(f, f.Type.Name, "value."+f.Name))
		case kind == tkEnum:
			g.pf("        if ( !TableEnumNamed( value.%s ) ) { return false; }\n", f.Name)
			g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
			g.pf("        uint64_t variant_%s = 0;\n", f.Name)
			g.pf("        if ( !TableEnumRef( ids, value.%s, variant_%s ) ) { return false; }\n", f.Name, f.Name)
			g.pf("        w.putleb( ref_%s ); w.put8( %d ); w.putleb( variant_%s ); // %s\n", f.Name, tkEnum, f.Name, f.Name)
		default:
			g.pf("        w.putleb( %s ); w.put8( %d ); // %s\n", g.wireRef(id), kind, f.Name)
			g.emitTableWriteElement(f, kind, "value."+f.Name, "        ", "")
		}
		g.pf("    }\n")
	case f.KeyEnum != "":
		// the slots ride KEYED: (key reference, L, element) triples, counted
		// like any array's elements. Two passes so the count and the body's
		// length are known before the header rides, and so measure and save
		// agree byte for byte.
		g.pf("    {\n")
		g.pf("        const int32_t mark_%s = ids.count;\n", f.Name)
		g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
		g.pf("        int64_t pairs_%s = 0, body_%s = 0;\n", f.Name, f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) // [%s]: every stored slot is a named variant's\n        {\n", f.ArrayBound, f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "            ", "return false;")
		switch kind {
		case tkTable:
			g.pf("            pairs_%s++; body_%s += TableLebBytes( key_ref ) + %s;\n", f.Name, f.Name, framed("elem_bytes"))
		case tkEnum:
			g.pf("            pairs_%s++; body_%s += TableLebBytes( key_ref ) + %s;\n", f.Name, f.Name, framed("TableLebBytes( element_ref )"))
		default:
			g.pf("            pairs_%s++; body_%s += TableLebBytes( key_ref ) + TableLebBytes( %d ) + %d;\n", f.Name, f.Name, tableKindWidth(kind), tableKindWidth(kind))
		}
		g.pf("        }\n")
		g.pf("        if ( pairs_%s > 0 )\n        {\n", f.Name)
		g.pf("            // KIND 16, not 14: a keyed body and a positional one are\n")
		g.pf("            // incompatible, so a reader of the other kind must see a kind\n")
		g.pf("            // mismatch and skip, never misdecode (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("            const int64_t whole_%s = 1 + TableLebBytes( (uint64_t) pairs_%s ) + body_%s;\n", f.Name, f.Name, f.Name)
		g.pf("            w.putleb( ref_%s ); w.put8( %d ); w.putleb( (uint64_t) whole_%s ); // %s (keyed by %s)\n", f.Name, tkKeyed, f.Name, f.Name, f.KeyEnum)
		g.pf("            w.put8( %d ); w.putleb( (uint64_t) pairs_%s );\n", ir.TableWireElemKind(f), f.Name)
		g.pf("            // ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
		g.pf("            // writer's choice, and a reader must not rely on it: every\n")
		g.pf("            // slot is found by its key (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("            for ( int32_t i = 0; i < %d; i++ )\n            {\n", f.ArrayBound)
		g.emitKeyedSlotRides(f, kind, "                ", "return false;")
		g.pf("                w.putleb( key_ref ); // the slot's VARIANT reference, not its position\n")
		switch kind {
		case tkTable:
			g.pf("                w.putleb( (uint64_t) elem_bytes );\n")
			g.inStep("i", func() {
				g.pf("                if ( !%s ) return false;\n", g.saveCall(f, f.Type.Name, g.keyedSlots("value.", f)+"[i]"))
			})
		case tkEnum:
			// the slot's reference is already resolved above, like key_ref:
			// writing it here rather than resolving the same value twice
			g.pf("                w.putleb( TableLebBytes( element_ref ) ); w.putleb( element_ref );\n")
		default:
			g.pf("                w.putleb( %d );\n", tableKindWidth(kind))
			g.emitTableWriteElement(f, kind, g.keyedSlots("value.", f)+"[i]", "                ", "")
		}
		g.pf("            }\n")
		g.pf("        }\n")
		g.pf("        else { ids.truncate( mark_%s ); } // an ELIDED field costs nothing in the id table either\n", f.Name)
		g.pf("    }\n")
	case f.Type.Pointer && f.Array != ir.ArrayNone:
		// an ARRAY OF POINTERS (§2.1, §3.1): the array framing with element kind
		// 17, one node index per slot, null as 0. A counted array rides its live
		// slots when it has any; a fixed one rides whole when any slot is non-null.
		count := strconv.FormatInt(f.ArrayBound, 10)
		ind := "        "
		if f.Array == ir.ArrayCounted {
			count = fmt.Sprintf("value.%s_count", f.Name)
			g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return false; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
			g.pf("    if ( value.%s_count > 0 )\n    {\n", f.Name)
		} else {
			g.pf("    {\n")
			g.pf("        bool any_%s = false;\n", f.Name)
			g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( %sAt( ctx, value.%s[i] ) != NULL ) { any_%s = true; break; } }\n", f.ArrayBound, f.Type.Name, f.Name, f.Name)
			g.pf("        if ( any_%s )\n        {\n", f.Name)
			ind = "            "
		}
		g.emitArrayField(f, id, tkNodeIndex, count, "value."+f.Name+"[%s]", ind)
		if f.Array == ir.ArrayFixed {
			g.pf("        }\n")
		}
		g.pf("    }\n")
	case f.Type.Blob():
		g.pf("    {\n")
		g.pf("        const TableBlob * blob_%s = TableBlobAt( ctx, value.%s ); // *%s\n", f.Name, f.Name, blobWord(f))
		g.pf("        if ( blob_%s != NULL )\n        {\n", f.Name)
		g.pf("            uint64_t index_%s = 0;\n", f.Name)
		g.pf("            if ( !TableNumberingIndex( numbering, (const void *) blob_%s, index_%s ) ) { return false; }\n", f.Name, f.Name)
		g.pf("            w.putleb( %s ); w.put8( %d ); // %s — a NODE INDEX into the flat node table\n", g.wireRef(id), tkNodeIndex, f.Name)
		g.pf("            w.putleb( index_%s );\n", f.Name)
		g.pf("        }\n    }\n")
	case f.Type.Pointer:
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee_%s = %sAt( ctx, value.%s ); // *%s\n", t, f.Name, t, f.Name, t)
		g.pf("        if ( pointee_%s != NULL )\n        {\n", f.Name)
		g.pf("            uint64_t index_%s = 0;\n", f.Name)
		g.pf("            if ( !TableNumberingIndex( numbering, (const void *) pointee_%s, index_%s ) ) { return false; }\n", f.Name, f.Name)
		g.pf("            w.putleb( %s ); w.put8( %d ); // %s — a NODE INDEX into the flat node table\n", g.wireRef(id), tkNodeIndex, f.Name)
		g.pf("            w.putleb( index_%s );\n", f.Name)
		g.pf("        }\n    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if ( value.%s_length < 0 || value.%s_length > %d ) { return false; } // storage invariant\n", f.Name, f.Name, f.Type.Size)
		g.pf("    if ( %s )\n    {\n", g.lengthRidesTest(f))
		g.pf("        w.putleb( %s ); w.put8( %d ); // %s\n", g.wireRef(id), tkString, f.Name)
		g.pf("        w.putleb( (uint64_t) value.%s_length );\n", f.Name)
		g.pf("        w.raw( value.%s, value.%s_length );\n    }\n", f.Name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if ( value.%s_length < 0 || value.%s_length > %d ) { return false; } // storage invariant\n", f.Name, f.Name, f.Type.Size)
		g.emitBytesDefaultLocal(f)
		g.pf("    if ( %s )\n    {\n", g.lengthRidesTest(f))
		g.pf("        const int64_t body_%s = 1 + TableLebBytes( (uint64_t) value.%s_length ) + value.%s_length;\n", f.Name, f.Name, f.Name)
		g.pf("        w.putleb( %s ); w.put8( %d ); // %s\n", g.wireRef(id), tkArray, f.Name)
		g.pf("        w.putleb( (uint64_t) body_%s );\n", f.Name)
		g.pf("        w.put8( %d ); w.putleb( (uint64_t) value.%s_length );\n", tkU8, f.Name)
		g.pf("        w.raw( value.%s, value.%s_length );\n    }\n", f.Name, f.Name)
	case f.Array == ir.ArrayCounted:
		g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return false; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value.%s_count > 0 )\n    {\n", f.Name)
		g.emitArrayField(f, id, elemKind, fmt.Sprintf("value.%s_count", f.Name), "value."+f.Name+"[%s]", "        ")
		g.pf("    }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — position is identity there, so
		// no element-default compare can elide one
		g.pf("    {\n")
		g.emitArrayField(f, id, elemKind, strconv.FormatInt(f.ArrayBound, 10), "value."+f.Name+"[%s]", "        ")
		g.pf("    }\n")
	case f.Array == ir.ArrayFixed && kind == tkUnion:
		// a fixed array of unions is positional too: all None elides, and one
		// set element makes every element ride in its place (§2.6, §3)
		g.pf("    {\n")
		g.pf("        bool any_%s = false;\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i].type != %sType::None ) { any_%s = true; break; } }\n", f.ArrayBound, f.Name, f.Type.Name, f.Name)
		g.pf("        if ( any_%s )\n        {\n", f.Name)
		g.emitArrayField(f, id, elemKind, strconv.FormatInt(f.ArrayBound, 10), "value."+f.Name+"[%s]", "            ")
		g.pf("        }\n    }\n")
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		// (parity with the reader's prefill)
		g.pf("    {\n")
		g.pf("        bool all_default_%s = true;\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i] != %s ) { all_default_%s = false; break; } }\n",
			f.ArrayBound, f.Name, g.fieldDefaultExpr(f), f.Name)
		g.pf("        if ( !all_default_%s )\n        {\n", f.Name)
		g.emitArrayField(f, id, elemKind, strconv.FormatInt(f.ArrayBound, 10), "value."+f.Name+"[%s]", "            ")
		g.pf("        }\n    }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		for _, v := range un.Variants {
			g.noteRef(v.Type)
		}
		g.pf("    if ( value.%s.type != %sType::None )\n    {\n", f.Name, un.Name)
		g.pf("        w.putleb( %s ); w.put8( %d ); // %s\n", g.wireRef(id), tkUnion, f.Name)
		g.emitUnionPayloadSave(f, "value."+f.Name, "        ", "return false;", "")
		g.pf("    }\n")
	case kind == tkTable:
		// elision is decided BEFORE any byte is emitted: measuring first
		// keeps an all-default nested field from touching the buffer at all,
		// so saving into a buffer of exactly TableMeasure's size never
		// trips overflow on transient header bytes
		g.pf("    {\n")
		g.pf("        const int32_t mark_%s = ids.count;\n", f.Name)
		g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
		g.pf("        const int64_t body_%s = %s;\n", f.Name, g.measureCall(f, f.Type.Name, "value."+f.Name))
		g.pf("        if ( body_%s < 0 ) return false; // storage invariant, refused as measure refuses it\n", f.Name)
		g.pf("        if ( body_%s > 1 ) // all-default nested elides\n        {\n", f.Name)
		g.pf("            w.putleb( ref_%s ); w.put8( %d ); w.putleb( (uint64_t) body_%s ); // %s\n", f.Name, tkTable, f.Name, f.Name)
		g.pf("            if ( !%s ) return false;\n", g.saveCall(f, f.Type.Name, "value."+f.Name))
		g.pf("        }\n")
		g.pf("        else { ids.truncate( mark_%s ); }\n", f.Name)
		g.pf("    }\n")
	case kind == tkEnum:
		// the reference is resolved BEFORE the header rides: a value no variant
		// names has no wire identity, and the write refuses it rather than
		// writing None over it
		g.pf("    if ( value.%s != %s )\n    {\n", f.Name, g.fieldDefaultExpr(f))
		g.pf("        if ( !TableEnumNamed( value.%s ) ) { return false; }\n", f.Name)
		g.pf("        const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
		g.pf("        uint64_t variant_%s = 0;\n", f.Name)
		g.pf("        if ( !TableEnumRef( ids, value.%s, variant_%s ) ) { return false; }\n", f.Name, f.Name)
		g.pf("        w.putleb( ref_%s ); w.put8( %d ); w.putleb( variant_%s ); // %s\n    }\n", f.Name, tkEnum, f.Name, f.Name)
	default:
		g.pf("    if ( value.%s != %s )\n    {\n", f.Name, g.fieldDefaultExpr(f))
		g.pf("        w.putleb( %s ); w.put8( %d ); // %s\n", g.wireRef(id), kind, f.Name)
		g.emitTableWriteElement(f, kind, "value."+f.Name, "        ", "")
		g.pf("    }\n")
	}
}

// emitUnionPayloadSave writes a SET union's payload: the ARM HEADER — the arm
// id reference, the arm's KIND byte and its `L` — then the arm's payload
// (docs/SPEC-TABLES.md §3). The length is measured before it rides, because a
// canonical LEB128 cannot be patched in place.
func (g *tableGen) emitUnionPayloadSave(f *ir.Field, expr, ind, onBad, sfx string) {
	un := f.Type.Ref.(*ir.Union)
	body := "arm_payload" + sfx
	wasField := g.unionField
	g.unionField = f
	defer func() { g.unionField = wasField }()
	g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
	for ai, v := range un.Variants {
		g.noteRef(v.Type)
		g.pf("%s    case %sType::%s:\n%s    {\n", ind, un.Name, ir.GoExportName(v.Name), ind)
		g.pf("%s        const uint64_t arm_ref%s = %s;\n", ind, sfx, g.wireRef(ir.TableWireId(v.WireName())))
		g.pf("%s        int64_t %s = 0;\n", ind, body)
		g.inStep(strconv.Itoa(ai), func() { g.emitArmMeasure(v, expr, body, ind+"        ", onBad, sfx) })
		g.pf("%s        w.putleb( arm_ref%s ); w.put8( %d ); w.putleb( (uint64_t) %s ); // %s\n", ind, sfx, armWireKind(v), body, v.Name)
		g.inStep(strconv.Itoa(ai), func() { g.emitArmSave(v, expr, ind+"        ", onBad, sfx) })
		g.pf("%s        break;\n%s    }\n", ind, ind)
	}
	g.pf("%s    default: %s // write validates the tag before it rides\n%s}\n", ind, onBad, ind)
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind, sfx string) {
	switch kind {
	case tkEnum:
		g.pf("%s{\n%s    uint64_t element_ref%s = 0;\n", ind, ind, sfx)
		g.pf("%s    if ( !TableEnumRef( ids, %s, element_ref%s ) ) { return false; }\n", ind, expr, sfx)
		g.pf("%s    w.putleb( element_ref%s );\n%s}\n", ind, sfx, ind)
	case tkBool:
		g.pf("%sw.put8( %s ? 1 : 0 );\n", ind, expr)
	case tkF32:
		g.pf("%sw.put32( table_float_to_bits( %s ) );\n", ind, expr)
	case tkF64:
		g.pf("%sw.put64( table_double_to_bits( %s ) );\n", ind, expr)
	case tkTable:
		g.pf("%s{\n%s    const int64_t elem_len%s = %s;\n", ind, ind, sfx, g.measureCall(f, f.Type.Name, expr))
		g.pf("%s    if ( elem_len%s < 0 ) return false;\n", ind, sfx)
		g.pf("%s    w.putleb( (uint64_t) elem_len%s );\n", ind, sfx)
		g.pf("%s    if ( !%s ) return false;\n%s}\n", ind, g.saveCall(f, f.Type.Name, expr), ind)
	case tkUnion:
		// one element of an array of unions: an ARM HEADER in its place, and
		// None is the single zero reference (§3)
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sif ( %s.type == %sType::None ) { w.putleb( 0 ); } // a None element rides in its place\n", ind, expr, un.Name)
		g.pf("%selse\n%s{\n", ind, ind)
		g.inElementStep(f, func() { g.emitUnionPayloadSave(f, expr, ind+"    ", "return false;", sfx+"u") })
		g.pf("%s}\n", ind)
	case tkNodeIndex:
		// one slot of an array of pointers: its node index, null as 0 (§3.1)
		g.pf("%s{\n%s    const %s * slot_pointee%s = %sAt( ctx, %s );\n", ind, ind, f.Type.Name, sfx, f.Type.Name, expr)
		g.pf("%s    uint64_t slot_index%s = 0;\n", ind, sfx)
		g.pf("%s    if ( slot_pointee%s != NULL && !TableNumberingIndex( numbering, (const void *) slot_pointee%s, slot_index%s ) ) { return false; }\n", ind, sfx, sfx, sfx)
		g.pf("%s    w.putleb( slot_index%s );\n%s}\n", ind, sfx, ind)
	default:
		width := tableKindWidth(kind)
		if width == 16 {
			// the two lanes of the raw value, low half first — the type
			// wire's own order (docs/SPEC-TABLES.md §3); the unsigned
			// conversion is bit-preserving for the signed kinds
			g.pf("%s{ serialize::uint128_t raw_v%s = serialize::uint128_t( %s ); w.put128( uint64_t( raw_v%s ), uint64_t( raw_v%s >> 64 ) ); }\n", ind, sfx, expr, sfx, sfx)
			return
		}
		cast := fmt.Sprintf("uint%d_t", width*8)
		g.pf("%sw.%s( %s( %s ) );\n", ind, tablePut(width), cast, expr)
	}
}

// ---- read ----

func (g *tableGen) emitTableRead(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("inline bool %s( TableReader & r, const TableNodeMap & nodes, %s & value%s )\n{\n",
			g.verb(st.Name, "LoadBody"), st.Name, g.retainParams())
		if g.noVariableEdges(st) {
			g.pf("    (void) nodes;\n")
		}
	} else {
		g.pf("%s bool %s( TableReader & r, %s & value%s )\n{\n",
			tableInlineMacro(g.unit.Package), g.verb(st.Name, "LoadBody"), st.Name, g.retainParams())
	}
	// `<T>Reset`, NOT `value = T{}` and not `new ( &value ) T{}`: assignment
	// materializes a temporary, and generated types can be large — a stack
	// bomb on worker threads — while a whole-object value-init costs cl
	// O(bytes) to COMPILE (#320). Reset applies the same declared defaults in
	// place, with neither cost.
	g.pf("    %sReset( value ); // prefill declared defaults in place, then overlay\n", st.Name)
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint64_t field_ref = 0;\n")
	g.pf("        if ( !r.getleb( field_ref ) ) { r.report->malformed = true; return false; }\n")
	g.pf("        if ( field_ref == 0 ) return true; // the body ENDS AT ITS OWN ZERO REFERENCE\n")
	g.pf("        if ( r.ids == NULL || field_ref > (uint64_t) r.ids->count ) { r.report->malformed = true; return false; } // a reference ABOVE the entry count\n")
	g.pf("        const uint64_t field_id = r.ids->at( field_ref );\n")
	g.pf("        if ( !r.has( 1 ) ) { r.report->malformed = true; return false; }\n")
	g.pf("        uint8_t kind = r.get8();\n")
	g.pf("        if ( ( field_id == kTableNodeTableFieldId && r.nested ) || field_id == kTableBuildVersionFieldId || field_id == kTableMessageVocabularyFieldId )\n        {\n")
	g.pf("            // A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS,\n")
	g.pf("            // IS MALFORMED (docs/SPEC-TABLES.md §3.1, §3.3). The node\n")
	g.pf("            // table's is the ROOT body's alone, on the numbering's own\n")
	g.pf("            // rule — a second numbering cannot exist — and the BUILD\n")
	g.pf("            // VERSION's rides in the announcement and nowhere else. That\n")
	g.pf("            // body stops and the parent reads on past its L.\n")
	g.pf("            r.report->malformed = true;\n")
	g.pf("            return false;\n        }\n")
	if len(st.Fields) > 0 {
		g.pf("        switch ( field_id )\n        {\n")
		for _, f := range st.Fields {
			id := tableFieldWireId(f)
			kind := tableScalarKind(f)
			wireKind := kind
			if f.Type.Pointer {
				// a pointer rides as a NODE INDEX under its own kind, so an
				// edit between a by-value nesting and a pointer reads as an
				// ordinary kind mismatch and is counted, never decoded as the
				// other shape (docs/SPEC-TABLES.md §3.1)
				kind, wireKind = tkNodeIndex, tkNodeIndex
			}
			if f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Blob()) {
				wireKind = tkArray
			}
			if f.IsMap() {
				// A MAP RIDES AS AN ARRAY OF TABLES (docs/SPEC-TABLES.md §2.8):
				// kind 14 over element kind 13, so a reader that declares the
				// same name as a bounded array of a two-field table decodes it
				// as that array and neither spends a kind on the other.
				kind, wireKind = tkTable, tkArray
			}
			if f.KeyEnum != "" {
				// a KEYED body is its own kind, so the positional-to-keyed edit
				// (and its reverse) reads as a kind mismatch and is counted,
				// never decoded as the other body (docs/SPEC-TABLES.md §3.2)
				wireKind = tkKeyed
			}
			if f.Type.Kind == ir.TBytes && !f.Type.Blob() {
				kind = tkU8 // bytes travel as an array of u8 elements; a *bytes is a node index (§2.5)
			}
			g.pf("            case 0x%016xull: // %s\n            {\n", id, f.Name)
			g.pf("                if ( kind != %d )\n                {\n", wireKind)
			if plainScalar(f) {
				// THE WIDENING BRANCH (docs/SPEC-TABLES.md §4) lives inside
				// the mismatch branch, so a payload under the declared kind
				// never pays for it
				g.pf("                    if ( TableKindWidens( kind, %d ) )\n                    {\n", wireKind)
				g.pf("                        // WIDENED (§4): a kind that grew since the writer decodes\n")
				g.pf("                        // exactly at its own width, the value lands, one widened counts\n")
				g.emitWidenedScalar(f, wireKind, "kind", "value."+f.Name, "                        ", "r", "r.report->malformed = true; return false;")
				g.pf("                        r.report->widened++;\n")
				if f.Type.Optional {
					g.pf("                        value.%s_present = true;\n", f.Name)
				}
				g.pf("                        break;\n                    }\n")
			}
			g.pf("                    // AT A POSITION THE READER DOES NAME, a field under\n")
			g.pf("                    // kind 31 or kind 32 takes this same rule and no other (§3)\n")
			g.pf("                    r.report->kind_mismatch++;\n")
			g.pf("                    if ( !r.skip( kind ) ) { r.report->malformed = true; return false; }\n")
			g.pf("                    break;\n                }\n")
			g.emitTableReadField(f, kind)
			if f.Type.Optional {
				// the field rode, so it is PRESENT — content decides nothing
				// here either (docs/SPEC-TABLES.md §2.3)
				g.pf("                value.%s_present = true;\n", f.Name)
			}
			g.pf("                break;\n            }\n")
		}
		if g.isVar(st.Name) {
			// THE RESERVED NODE-TABLE ID (docs/SPEC-TABLES.md §3.1). A reader
			// that HOLDS the numbering has already consumed the table before it
			// decodes any body, so the transport it rode in is stepped over
			// here and NEVER counted unknown: the reserved id is not a field of
			// the table, it is where the numbering travelled. An `unknown` here
			// would mean "a build without kind 17", which is the difference §4
			// exists to report and not one this reader has.
			g.pf("            case 0x%016xull:\n            {\n", ir.TableNodeWireId)
			g.pf("                if ( !r.skip( kind ) ) { r.report->malformed = true; return false; }\n")
			g.pf("                break;\n            }\n")
		}
		g.pf("            default:\n            {\n")
		g.pf("                r.report->unknown++;\n")
		g.emitUnknownArm("                ")
		g.pf("                break;\n            }\n")
		g.pf("        }\n    }\n}\n\n")
	} else {
		g.pf("        r.report->unknown++;\n")
		g.emitUnknownArm("        ")
		g.pf("    }\n}\n\n")
	}

	// buffer-level convenience entry. A VARIABLE-LENGTH table has none: it is
	// never held by value, so its Load takes the caller's region and hands back
	// the root instead (docs/SPEC-TABLES.md §2).
	if g.retain || g.isVar(st.Name) || st.IsMapEntry() {
		return
	}
	g.pf("// THE FORM BYTE IS READ FIRST, before the trailer and before any body, so a\n")
	g.pf("// file that is both a newer form and damaged is a REFUSAL and never damage.\n")
	g.pf("// A refusal moves none of the report's five counters, because nothing was\n")
	g.pf("// decoded and there is nothing to count (docs/SPEC-TABLES.md §3).\n")
	g.pf("inline TableOpenVerdict %sLoadVerdict( %s & value, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", st.Name, st.Name)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * to = report != NULL ? report : &ignored;\n")
	g.pf("    TableIdTable table;\n")
	g.pf("    int64_t body_bytes = 0;\n")
	g.pf("    const TableOpenVerdict verdict = TableOpen( buffer, bytes, table, body_bytes );\n")
	g.pf("    if ( verdict != TableOpenOk )\n    {\n")
	g.pf("        %sReset( value );\n", st.Name)
	g.pf("        if ( verdict == TableOpenDamaged ) { to->malformed = true; }\n")
	g.pf("        else\n        {\n")
	g.pf("            // FORM 2 IS A STREAM FORM AND NEVER A FILE FORM: a message\n")
	g.pf("            // stored on its own is not readable, because its table is\n")
	g.pf("            // somewhere else, and the refusal says so BY NAME rather than\n")
	g.pf("            // merely by form byte (docs/SPEC-TABLES.md §3.3).\n")
	g.pf("            to->refused = true;\n")
	g.pf("            to->reason = bytes > 0 && buffer[0] == kTableWireMessageForm ? message_form_as_file : newer_form;\n")
	g.pf("        }\n")
	g.pf("        return verdict;\n    }\n")
	g.pf("    // ANY BYTE BETWEEN THE ROOT'S TERMINATOR AND THE TABLE'S FIRST ENTRY IS\n")
	g.pf("    // MALFORMED, because no field claims it and the two ends of the file\n")
	g.pf("    // have met: nothing is decoded and one event is counted (§3).\n")
	g.pf("    if ( TableBodyEndsEarly( buffer + 1, body_bytes, table ) )\n    {\n")
	g.pf("        %sReset( value );\n", st.Name)
	g.pf("        to->malformed = true;\n")
	g.pf("        return TableOpenDamaged;\n    }\n")
	g.pf("    TableReader r( buffer + 1, body_bytes, to, &table );\n")
	g.pf("    r.nested = false; // the ROOT body, the one that may carry a node table\n")
	g.pf("    if ( !%sLoadBody( r, value ) ) { return TableOpenBodyStopped; }\n", st.Name)
	g.pf("    return TableOpenOk;\n}\n\n")
	g.pf("// The bool is the BODY reaching its own terminator, and it is what it has\n")
	g.pf("// always been: framing damage inside a field keeps what it decoded, flags\n")
	g.pf("// the report and reads on, so this answers true (docs/SPEC-TABLES.md §4).\n")
	g.pf("// False is a wire nothing could be decoded from: a refusal, a table that\n")
	g.pf("// cannot be read whole, or a root body the walk could not finish.\n")
	g.pf("inline bool %sLoad( %s & value, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", st.Name, st.Name)
	g.pf("    return %sLoadVerdict( value, buffer, bytes, report ) == TableOpenOk;\n}\n\n", st.Name)
}

// emitUnknownArm is the field this reader cannot name: skipped by its framing
// and counted, exactly as it always was. Under RETENTION the same skip runs
// inside the capture, which copies the field out with every reference resolved
// — or drops it, counting one retain_lost, where an excluded class, a full
// buffer or damage the plain read never looked at says it cannot be kept
// (docs/SPEC-TABLES.md §6.6). Either way the read continues and the reader's
// own data is exactly what it would have been with retention off.
func (g *tableGen) emitUnknownArm(ind string) {
	if !g.retain {
		g.pf("%sif ( !r.skip( kind ) ) { r.report->malformed = true; return false; }\n", ind)
		return
	}
	g.pf("%sif ( TableRetainReservedId( field_id ) )\n%s{\n", ind, ind)
	g.pf("%s    // THE RESERVED NODE-TABLE FIELD IS THE WRITER'S WHOLE NUMBERING\n", ind)
	g.pf("%s    // and is never retained: re-emitting it would put a second\n", ind)
	g.pf("%s    // numbering in a file whose own numbering the writer re-derives.\n", ind)
	g.pf("%s    r.report->retain_lost++;\n", ind)
	g.pf("%s    if ( !r.skip( kind ) ) { r.report->malformed = true; return false; }\n", ind)
	g.pf("%s}\n%selse if ( !TableRetainCapture( retain, r, path, field_id, kind ) ) { r.report->malformed = true; return false; }\n", ind, ind)
}

// emitNodeIndexLoad reads one NODE INDEX and resolves it through the
// numbering, never following it (docs/SPEC-TABLES.md §3.1). `exact` is the ARM
// position's extra check: a reference-shaped arm's `L` must be the byte count
// of the reference it frames, and anything else is that arm's own framing
// damage (§3).
func (g *tableGen) emitNodeIndexLoad(f *ir.Field, dst, ind, rdr, onBad, sfx string, exact bool) {
	target := fmt.Sprintf("0x%016xull", ir.TableWireId(ir.PointeeWireName(f)))
	comment := "*" + f.Type.Name
	if f.Type.Blob() {
		target = blobTypeIdConst(f)
		comment = "*" + blobWord(f)
	}
	index := "node_index" + sfx
	g.pf("%s{\n%s    uint64_t %s = 0;\n", ind, ind, index)
	g.pf("%s    if ( !%s.getleb( %s ) ) { %s }\n", ind, rdr, index, onBad)
	if exact {
		g.pf("%s    if ( %s.offset != %s.size ) { %s }\n", ind, rdr, rdr, onBad)
	}
	g.pf("%s    TableNodeResolve( nodes, %s, %s, %s, r.report ); // %s\n%s}\n", ind, dst, index, target, comment, ind)
}

// emitEnumRefLoad reads an enum's payload: the REFERENCE to its variant name's
// id, `0` for None (docs/SPEC-TABLES.md §3). A reference this reader's enum
// cannot name is §4's ordinary `unknown` — the field reads None and one event
// counts — and a reference ABOVE the entry count is framing damage.
func (g *tableGen) emitEnumRefLoad(f *ir.Field, dst, ind, rdr, onBad string) {
	e := f.Type.Name
	g.pf("%suint64_t variant_ref = 0;\n", ind)
	g.pf("%sif ( !%s.getleb( variant_ref ) ) { %s }\n", ind, rdr, onBad)
	g.pf("%sif ( variant_ref == 0 ) { %s = %s::None; } // the zero reference is the enum's None\n", ind, dst, e)
	g.pf("%selse if ( variant_ref > (uint64_t) r.ids->count ) { %s }\n", ind, onBad)
	g.pf("%selse if ( !TableEnumValue( r.ids->at( variant_ref ), %s ) )\n%s{\n", ind, dst, ind)
	g.pf("%s    %s = %s::None;\n%s    r.report->unknown++;%s\n%s}\n", ind, dst, e, ind, g.retainLostInline(), ind)
}

// retainLostInline is one EXCLUDED CLASS counted (docs/SPEC-TABLES.md §6.6),
// emitted into the RETAIN family only and beside the unknown the plain read
// already counts. The thing excluded is not a self-contained field, so putting
// it back is a splice into something the reader rebuilds rather than a field
// appended to a body — and every exclusion counts, so a caller that needs to
// know retention held reads one number.
func (g *tableGen) retainLostInline() string {
	if !g.retain {
		return ""
	}
	return " r.report->retain_lost++;"
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	ind := "                "
	if f.IsMap() {
		g.emitMapReadField(f)
		return
	}
	if f.IsList() {
		g.emitListReadField(f)
		return
	}
	switch {
	case f.KeyEnum != "":
		// each triple is placed by its KEY REFERENCE, so a slot lands by name
		// however the enum moved; a key this reader cannot name is skipped by
		// its length and counted unknown, and a slot the writer never sent
		// keeps the prefill's default (docs/SPEC-TABLES.md §3.2)
		g.noteRef(f.KeyEnum)
		g.pf("%suint64_t body_len = 0;\n", ind)
		g.pf("%sif ( !r.getleb( body_len ) || !r.room( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%sint64_t body_end = r.offset + (int64_t) body_len;\n", ind)
		g.pf("%sif ( body_len >= 2 )\n%s{\n", ind, ind)
		g.pf("%s    uint8_t elem_kind = r.get8();\n", ind)
		g.pf("%s    uint64_t count = 0;\n", ind)
		g.pf("%s    if ( !r.getleb( count ) ) { r.report->malformed = true; r.offset = body_end; break; }\n", ind)
		if widenableElement(f) {
			// THE WIDENING BRANCH at a keyed body's element kind (§4), inside
			// the mismatch branch and with a loop of its own, so the matching
			// loop below decodes at the declared width and nothing else
			g.pf("%s    if ( elem_kind != %d )\n%s    {\n", ind, ir.TableWireElemKind(f), ind)
			g.pf("%s        if ( !TableKindWidens( elem_kind, %d ) ) { r.report->kind_mismatch++; r.offset = body_end; break; }\n", ind, ir.TableWireElemKind(f))
			g.pf("%s        r.report->widened++; // ONE count for the field, every slot at the wire kind's width\n", ind)
			g.emitKeyedTriples(f, kind, ind+"    ", true)
			g.pf("%s    }\n%s    else\n%s    {\n", ind, ind, ind)
			g.emitKeyedTriples(f, kind, ind+"    ", false)
			g.pf("%s    }\n", ind)
		} else {
			g.pf("%s    if ( elem_kind != %d ) { r.report->kind_mismatch++; r.offset = body_end; break; }\n", ind, ir.TableWireElemKind(f))
			g.emitKeyedTriples(f, kind, ind, false)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr.offset = body_end; // unread triples and slack skip via the length\n", ind)
	case f.Type.Blob(), f.Type.Pointer && f.Array == ir.ArrayNone:
		g.pf("%s// A POINTER FIELD'S PAYLOAD IS A NUMBER (docs/SPEC-TABLES.md §3.1): it is\n", ind)
		g.pf("%s// bounds-checked and resolved through the numbering, never FOLLOWED, so\n", ind)
		g.pf("%s// there is no traversal here and therefore no traversal bound.\n", ind)
		g.emitNodeIndexLoad(f, "value."+f.Name, ind, "r", "r.report->malformed = true; return false;", "", false)
	case f.Type.Kind == ir.TString:
		g.pf("%suint64_t len = 0;\n", ind)
		g.pf("%sif ( !r.getleb( len ) || !r.room( len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%s// ILL-FORMED TEXT IS DAMAGE (§3, §4): the field reads its declared\n%s// default, one malformed counts, and the parent reads on past L\n", ind, ind)
		g.pf("%sif ( !TableUtf8Valid( r.buffer + r.offset, (int64_t) len ) ) { r.report->malformed = true; value.%s[0] = 0; value.%s_length = 0; r.offset += (int64_t) len; break; }\n", ind, f.Name, f.Name)
		g.pf("%suint64_t keep = len;\n", ind)
		g.pf("%sif ( keep > %d ) { keep = (uint64_t) TableUtf8Clamp( r.buffer + r.offset, (int64_t) len, %d ); r.report->clamped++; } // at a code point boundary (§3)\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%smemcpy( value.%s, r.buffer + r.offset, (size_t) keep );\n", ind, f.Name)
		g.pf("%svalue.%s[keep] = 0;\n", ind, f.Name)
		g.pf("%svalue.%s_length = (int32_t) keep;\n", ind, f.Name)
		g.pf("%sr.offset += (int64_t) len;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
		g.pf("%suint64_t body_len = 0;\n", ind)
		g.pf("%sif ( !r.getleb( body_len ) || !r.room( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%sint64_t body_end = r.offset + (int64_t) body_len;\n", ind)
		g.pf("%s// A BODY TOO SHORT FOR ITS OWN HEADER — the element kind byte and the\n", ind)
		g.pf("%s// count, so fewer than two bytes — is INERT (§4): the field keeps the\n", ind)
		g.pf("%s// value it has, no counter is raised, and the walk continues past L.\n", ind)
		g.pf("%sif ( body_len >= 2 )\n%s{\n", ind, ind)
		g.pf("%s    uint8_t elem_kind = r.get8();\n", ind)
		g.pf("%s    uint64_t count = 0;\n", ind)
		g.pf("%s    const bool counted_ok = r.getleb( count );\n", ind)
		g.pf("%s    // A DAMAGED COUNT stops the elements and nothing else: the field\n", ind)
		g.pf("%s    // RODE, so an optional is still PRESENT (§2.3) — only a foreign\n", ind)
		g.pf("%s    // ELEMENT KIND says the payload is not this array's at all.\n", ind)
		g.pf("%s    if ( !counted_ok ) { r.report->malformed = true; }\n", ind)
		if widenableElement(f) {
			// THE WIDENING BRANCH at an element kind (docs/SPEC-TABLES.md
			// §4), inside the mismatch branch: ONE count for the field,
			// every element decoded at the wire kind's width
			g.pf("%s    else if ( elem_kind != %d )\n%s    {\n", ind, ir.TableWireElemKind(f), ind)
			g.pf("%s        if ( !TableKindWidens( elem_kind, %d ) ) { r.report->kind_mismatch++; r.offset = body_end; break; }\n", ind, ir.TableWireElemKind(f))
			g.pf("%s        uint64_t widened_keep = count;\n", ind)
			g.pf("%s        if ( widened_keep > %d ) { widened_keep = %d; r.report->clamped++; }\n", ind, bound, bound)
			g.pf("%s        TableReader widened_sub( r.buffer + r.offset, body_end - r.offset, r.report, r.ids );\n", ind)
			countLvalue := ""
			if f.Array == ir.ArrayCounted {
				countLvalue = fmt.Sprintf("value.%s_count", f.Name)
			}
			g.emitWidenedElements(f, ir.TableWireElemKind(f), "elem_kind", "value."+f.Name+"[%s]", countLvalue, "widened_keep", ind+"        ", "widened_sub")
			g.pf("%s    }\n", ind)
		} else {
			g.pf("%s    else if ( elem_kind != %d ) { r.report->kind_mismatch++; r.offset = body_end; break; }\n", ind, ir.TableWireElemKind(f))
		}
		g.pf("%s    else\n%s    {\n", ind, ind)
		g.pf("%s    uint64_t keep = count;\n", ind)
		g.pf("%s    if ( keep > %d ) { keep = %d; r.report->clamped++; }\n", ind, bound, bound)
		g.pf("%s    // elements are BOUNDED by the field body: a count the length\n", ind)
		g.pf("%s    // cannot cover keeps the decoded prefix, flags malformed, and\n", ind)
		g.pf("%s    // the parent continues at the next field — following fields'\n", ind)
		g.pf("%s    // bytes are never fabricated into elements\n", ind)
		g.pf("%s    TableReader sub( r.buffer + r.offset, body_end - r.offset, r.report, r.ids );\n", ind)
		if counted {
			g.pf("%s    uint64_t decoded = 0;\n", ind)
		}
		g.pf("%s    for ( uint64_t i = 0; i < keep; i++ )\n%s    {\n", ind, ind)
		g.inStep("i", func() { g.emitTableReadElement(f, kind, ind+"        ") })
		if counted {
			g.pf("%s        decoded = i + 1;\n", ind)
		}
		g.pf("%s    }\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s    value.%s_length = (int32_t) decoded;\n", ind, f.Name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s    value.%s_count = (int32_t) decoded;\n", ind, f.Name)
		}
		g.pf("%s    }\n", ind)
		g.pf("%s}\n", ind)
		g.pf("%sr.offset = body_end; // excess elements and slack skip via the length\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("%suint64_t arm_ref = 0;\n", ind)
		g.pf("%sif ( !r.getleb( arm_ref ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%sif ( arm_ref == 0 ) { value.%s.type = %sType::None; break; } // empty: the reference is the whole payload\n", ind, f.Name, un.Name)
		g.pf("%sif ( arm_ref > (uint64_t) r.ids->count ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%sconst uint64_t arm_id = r.ids->at( arm_ref );\n", ind)
		g.pf("%sif ( !r.has( 1 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%sconst uint8_t arm_kind = r.get8();\n", ind)
		g.pf("%suint64_t body_len = 0;\n", ind)
		g.pf("%sif ( !r.getleb( body_len ) || !r.room( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%s{\n%s    TableReader sub( r.buffer + r.offset, (int64_t) body_len, r.report, r.ids );\n", ind, ind)
		g.pf("%s    switch ( arm_id ) // the arm's NAME hash (docs/SPEC-TABLES.md §5)\n%s    {\n", ind, ind)
		wasField := g.unionField
		g.unionField = f
		defer func() { g.unionField = wasField }()
		for ai, v := range un.Variants {
			g.pf("%s        case 0x%016xull: // %s\n%s        {\n", ind, ir.TableWireId(v.WireName()), v.Name, ind)
			g.pf("%s            if ( arm_kind != %d )\n%s            {\n", ind, armWireKind(v), ind)
			g.emitArmWiden(v, "value."+f.Name, "arm_kind", "sub", fmt.Sprintf("value.%s.type", f.Name),
				un.Name+"Type::"+ir.GoExportName(v.Name), un.Name+"Type::None", ind+"                ")
			g.pf("%s                // A RETYPED ARM IS JUDGED BY THE FIELD RULES (§3): the\n", ind)
			g.pf("%s                // arm skips by L, the union reads None, and the parent reads on\n", ind)
			g.pf("%s                value.%s.type = %sType::None;\n", ind, f.Name, un.Name)
			g.pf("%s                r.report->kind_mismatch++;\n%s                break;\n%s            }\n", ind, ind, ind)
			g.pf("%s            value.%s.type = %sType::%s;\n", ind, f.Name, un.Name, ir.GoExportName(v.Name))
			g.inStep(strconv.Itoa(ai), func() {
				g.emitArmLoad(v, "value."+f.Name, ind+"            ", "sub",
					fmt.Sprintf("value.%s.type", f.Name), un.Name+"Type::None", "")
			})
			g.pf("%s            break;\n%s        }\n", ind, ind)
		}
		g.pf("%s        default:\n", ind)
		g.pf("%s            // an arm this reader cannot name: the value reads EMPTY and\n", ind)
		g.pf("%s            // the body is skipped by its length, never misdecoded. The\n", ind)
		g.pf("%s            // reset is explicit, not the prefill's: a repeated field id\n", ind)
		g.pf("%s            // must not leave an arm decoded by an earlier occurrence\n", ind)
		g.pf("%s            // standing (docs/SPEC-TABLES.md §4).\n", ind)
		g.pf("%s            value.%s.type = %sType::None;\n", ind, f.Name, un.Name)
		g.pf("%s            r.report->unknown++;%s\n%s            break;\n", ind, g.retainLostInline(), ind)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.pf("%sr.offset += (int64_t) body_len;\n", ind)
	case kind == tkTable:
		g.pf("%suint64_t body_len = 0;\n", ind)
		g.pf("%sif ( !r.getleb( body_len ) || !r.room( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%s{\n%s    TableReader sub( r.buffer + r.offset, (int64_t) body_len, r.report, r.ids );\n", ind, ind)
		g.pf("%s    %s;\n", ind, g.loadCall(f, f.Type.Name, "sub", "value."+f.Name))
		// A BODY'S TERMINATOR IS THE END OF ITS PAYLOAD (§3): a body whose
		// terminator is not the last byte of its `L` is framing damage —
		// the payload stops, the field reads its declared defaults, and the
		// enclosing body continues past it by `L`.
		g.pf("%s    if ( sub.offset != sub.size )\n%s    {\n", ind, ind)
		g.pf("%s        r.report->malformed = true;\n", ind)
		g.pf("%s        %sReset( value.%s );\n", ind, f.Type.Name, f.Name)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.pf("%sr.offset += (int64_t) body_len;\n", ind)
	case kind == tkEnum:
		g.emitEnumRefLoad(f, "value."+f.Name, ind, "r", "r.report->malformed = true; return false;")
	default:
		g.emitTableReadScalarFrom(f, kind, "value."+f.Name, ind,
			"r", "r.report->malformed = true; return false;")
	}
}

// emitTableReadElement decodes one array element from the field-body
// sub-reader; truncation keeps the decoded prefix and flags malformed
// without stopping the parent decode.
func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	g.emitTableReadElementInto(f, kind, fmt.Sprintf("value.%s[(int32_t) i]", f.Name), ind, "sub", "")
}

// emitTableReadElementInto is that decode into an explicit slot, which is what
// an ARRAY ARM needs: the elements live in the union overlay rather than in a
// field of the enclosing table (docs/SPEC-TABLES.md §2.6).
func (g *tableGen) emitTableReadElementInto(f *ir.Field, kind int, dst, ind, rdr, sfx string) {
	switch kind {
	case tkUnion:
		// AN ELEMENT OF AN ARRAY OF UNIONS IS AN ARM HEADER and carries its own
		// kind, so the arm rules apply once per element (§3). The element is
		// re-established as None before the arm is read, so a repeated field id
		// leaves no earlier arm standing (§4).
		un := f.Type.Ref.(*ir.Union)
		ref, id, length := "elem_arm_ref"+sfx, "elem_arm_id"+sfx, "elem_arm_len"+sfx
		armKind := "elem_arm_kind" + sfx
		wasPath, wasIndex := g.pathExpr, g.elemIndex
		g.pathExpr, g.elemIndex = g.step(f), "0"
		defer func() { g.pathExpr, g.elemIndex = wasPath, wasIndex }()
		g.pf("%s{\n%s    uint64_t %s = 0;\n", ind, ind, ref)
		g.pf("%s    if ( !%s.getleb( %s ) ) { r.report->malformed = true; break; }\n", ind, rdr, ref)
		g.pf("%s    %s.type = %sType::None;\n", ind, dst, un.Name)
		g.pf("%s    if ( %s != 0 ) // the zero reference is a None element in its place\n%s    {\n", ind, ref, ind)
		g.pf("%s        if ( %s > (uint64_t) r.ids->count ) { r.report->malformed = true; break; }\n", ind, ref)
		g.pf("%s        const uint64_t %s = r.ids->at( %s );\n", ind, id, ref)
		g.pf("%s        if ( !%s.has( 1 ) ) { r.report->malformed = true; break; }\n", ind, rdr)
		g.pf("%s        const uint8_t %s = %s.get8();\n", ind, armKind, rdr)
		g.pf("%s        uint64_t %s = 0;\n", ind, length)
		g.pf("%s        if ( !%s.getleb( %s ) || !%s.room( %s ) ) { r.report->malformed = true; break; }\n", ind, rdr, length, rdr, length)
		g.pf("%s        TableReader elem_arm%s( %s.buffer + %s.offset, (int64_t) %s, r.report, r.ids );\n", ind, sfx, rdr, rdr, length)
		g.pf("%s        switch ( %s ) // the arm's NAME hash (docs/SPEC-TABLES.md §5)\n%s        {\n", ind, id, ind)
		wasField := g.unionField
		g.unionField = f
		defer func() { g.unionField = wasField }()
		for ai, v := range un.Variants {
			g.pf("%s            case 0x%016xull: // %s\n%s            {\n", ind, ir.TableWireId(v.WireName()), v.Name, ind)
			g.pf("%s                if ( %s != %d )\n%s                {\n", ind, armKind, armWireKind(v), ind)
			g.emitArmWiden(v, dst, armKind, "elem_arm"+sfx, dst+".type",
				un.Name+"Type::"+ir.GoExportName(v.Name), un.Name+"Type::None", ind+"                    ")
			g.pf("%s                    %s.type = %sType::None; r.report->kind_mismatch++; break;\n%s                }\n", ind, dst, un.Name, ind)
			g.pf("%s                %s.type = %sType::%s;\n", ind, dst, un.Name, ir.GoExportName(v.Name))
			g.inStep(strconv.Itoa(ai), func() {
				g.emitArmLoad(v, dst, ind+"                ", "elem_arm"+sfx, dst+".type", un.Name+"Type::None", sfx+"a")
			})
			g.pf("%s                break;\n%s            }\n", ind, ind)
		}
		g.pf("%s            default: r.report->unknown++;%s break; // an arm this reader cannot name: the element reads None, the body skips by its length\n", ind, g.retainLostInline())
		g.pf("%s        }\n", ind)
		g.pf("%s        %s.offset += (int64_t) %s;\n", ind, rdr, length)
		g.pf("%s    }\n%s}\n", ind, ind)
	case tkNodeIndex:
		// one slot of an array of pointers: a node index, bounds-checked and
		// resolved through the numbering, never followed (§3.1)
		g.emitNodeIndexLoad(f, dst, ind, rdr, "r.report->malformed = true; break;", sfx, false)
	case tkTable:
		g.pf("%suint64_t elem_len%s = 0;\n", ind, sfx)
		g.pf("%sif ( !%s.getleb( elem_len%s ) || !%s.room( elem_len%s ) ) { r.report->malformed = true; break; }\n", ind, rdr, sfx, rdr, sfx)
		g.pf("%s{\n%s    TableReader elem%s( %s.buffer + %s.offset, (int64_t) elem_len%s, r.report, r.ids );\n", ind, ind, sfx, rdr, rdr, sfx)
		g.pf("%s    %s;\n", ind, g.loadCall(f, f.Type.Name, "elem"+sfx, dst))
		g.pf("%s}\n", ind)
		g.pf("%s%s.offset += (int64_t) elem_len%s;\n", ind, rdr, sfx)
	case tkEnum:
		g.pf("%s{\n", ind)
		g.emitEnumRefLoad(f, dst, ind+"    ", rdr, "r.report->malformed = true; break;")
		g.pf("%s}\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, dst, ind,
			rdr, "r.report->malformed = true; break;")
	}
}

// emitTableReadScalarFrom decodes one fixed-width scalar from the named
// reader into a storage lvalue, with range clamps where the schema declares
// them. onTrunc is the truncation action: a scalar FIELD stops the decode
// (outer framing damage), an array ELEMENT keeps the prefix and breaks.
func (g *tableGen) emitTableReadScalarFrom(f *ir.Field, kind int, lvalue, ind, rdr, onTrunc string) {
	width := tableKindWidth(kind)
	g.pf("%sif ( !%s.has( %d ) ) { %s }\n", ind, rdr, width, onTrunc)
	switch kind {
	case tkBool:
		g.pf("%s%s = %s.get8() != 0;\n", ind, lvalue, rdr)
	case tkF32:
		if f.HasFloatRange {
			g.pf("%sfloat decoded_f = table_bits_to_float( %s.get32() );\n", ind, rdr)
			g.pf("%sif ( decoded_f < %s ) { decoded_f = %s; r.report->clamped++; }\n", ind, formatFloat(f.FMin, true), formatFloat(f.FMin, true))
			g.pf("%selse if ( decoded_f > %s ) { decoded_f = %s; r.report->clamped++; }\n", ind, formatFloat(f.FMax, true), formatFloat(f.FMax, true))
			g.pf("%s%s = decoded_f;\n", ind, lvalue)
			return
		}
		g.pf("%s%s = table_bits_to_float( %s.get32() );\n", ind, lvalue, rdr)
	case tkF64:
		g.pf("%s%s = table_bits_to_double( %s.get64() );\n", ind, lvalue, rdr)
	default:
		signed := ir.TableKindSigned(kind)
		storage := fmt.Sprintf("uint%d_t", width*8)
		if signed {
			storage = fmt.Sprintf("int%d_t", width*8)
		}
		if width == 16 {
			// the two lanes back into serialize's pair, low half first; the
			// signed conversion is bit-preserving
			storage = "serialize::uint128_t"
			if signed {
				storage = "serialize::int128_t"
			}
			g.pf("%suint64_t lo_v = 0, hi_v = 0; %s.get128( lo_v, hi_v );\n", ind, rdr)
			g.pf("%s%s decoded_v = %s( ( serialize::uint128_t( hi_v ) << 64 ) | serialize::uint128_t( lo_v ) );\n", ind, storage, storage)
		} else {
			g.pf("%s%s decoded_v = %s( %s.%s( ) );\n", ind, storage, storage, rdr, tableGet(width))
		}
		// the declared range on the RAW scale — a fixed field's whole-unit
		// bounds shifted by F — clamps and counts as every bounded scalar
		// does (docs/SPEC-TABLES.md §4)
		if rlo, rhi, ok := ir.TableRawRange(f); ok {
			low, high := tableClampEnds(f, width)
			if low {
				lo := tableIntLit(rlo, signed, width)
				g.pf("%sif ( decoded_v < %s ) { decoded_v = %s; r.report->clamped++; }\n", ind, lo, lo)
			}
			if high {
				hi := tableIntLit(rhi, signed, width)
				lead := "if"
				if low {
					lead = "else if"
				}
				g.pf("%s%s ( decoded_v > %s ) { decoded_v = %s; r.report->clamped++; }\n", ind, lead, hi, hi)
			}
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
			maxv := (uint64(1) << f.Type.Width) - 1
			g.pf("%sif ( decoded_v > %dull ) { decoded_v = %dull; r.report->clamped++; } // bits(%d) width clamp\n", ind, maxv, maxv, f.Type.Width)
		}
		g.pf("%s%s = decoded_v;\n", ind, lvalue)
	}
}

// ---- reflection descriptors ----

// tableFieldTypeName renders a field's schema-facing type name for the
// descriptor ("float32", "bits(9)", "Grade", "GunnerSettings").
func tableFieldTypeName(f *ir.Field) string { return ir.TableTypeSpelling(f) }

// unionArmLambda renders a descriptor lambda over a union's tag values: 0 is
// the empty arm, [1, N] the declared arms in tag order.
func unionArmLambda(un *ir.Union, result string, arm func(ir.UnionVariant) string, unknown, none string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "+[]( uint64_t v ) -> %s { switch ( v ) { case 0: return %s;", result, none)
	for i, v := range un.Variants {
		fmt.Fprintf(&b, " case %d: return %s;", i+1, arm(v))
	}
	fmt.Fprintf(&b, " default: return %s; } }", unknown)
	return b.String()
}

// resetLambda renders a type descriptor's reset column: the same in-place
// prefill the read path uses, behind a captureless lambda so the descriptor
// stays constant-initialisable. The storage a walk hands it is a live object
// of the type, so this assigns rather than starting a lifetime.
func resetLambda(name string) string {
	return fmt.Sprintf("+[]( void * p ) { %sReset( *(%s *) p ); }", name, name)
}

// unionArmsLambda renders a union field's arms column: a captureless lambda
// whose function-pointer conversion is a constant expression, so a descriptor
// that names it stays constant-initialised (docs/SPEC-TABLES.md §8). The arms table
// is a static inside it — no namespace-scope name to claim, and no first-use
// state on the surface a caller sees.
func (g *tableGen) unionArmsLambda(un *ir.Union, hoisted bool) string {
	var b strings.Builder
	b.WriteString("+[]() -> const TableUnionInfo * {")
	// AN ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6): an arm that names no
	// declaration carries the FIELD row a field of its type would carry, its
	// offsets taken inside the union's storage. The rows are a static of this
	// lambda, so no namespace-scope name is claimed for them.
	general := false
	for _, v := range un.Variants {
		if !v.Body() && !v.Void() {
			general = true
		}
	}
	if general {
		// the rows are emitted through the ONE field-row emitter and captured,
		// so an arm's descriptor can never drift from a field's
		saved := g.body.String()
		g.body.Reset()
		for _, v := range un.Variants {
			if v.Body() || v.Void() {
				continue
			}
			g.emitTagsStatic(fieldSpelling{owner: un.Name}.tagsName(v.F), v.F.Tags, "static const", "")
		}
		for _, v := range un.Variants {
			if v.Body() || v.Void() {
				continue
			}
			g.emitArmFieldInfo(un, v, hoisted)
		}
		rows := oneLine(g.body.String())
		g.body.Reset()
		g.body.WriteString(saved)
		// the array is named for its UNION: a NESTED-union arm's own rows are
		// emitted inside this initializer, where this name is already in scope,
		// so one name for both would shadow (-Wshadow is an error here)
		fmt.Fprintf(&b, " static const TableFieldInfo arm_fields_%s[] = { %s };", un.Name, rows)
	}
	b.WriteString(" static const TableUnionArmInfo arms[] = { { 0, NULL, NULL, 0 },")
	row := 0
	for _, v := range un.Variants {
		table, field := "NULL", "NULL"
		switch {
		case v.Void():
			// a PAYLOAD-FREE arm has neither: no record, no field row, no
			// storage (SPEC §4.8)
		case v.Body() && hoisted:
			table = "&" + v.Type + "TableInfo"
		case v.Body():
			table = v.Type + "TableType()"
		default:
			field = fmt.Sprintf("&arm_fields_%s[%d]", un.Name, row)
			row++
		}
		size, _ := ir.ArmLayout(g.unit, v)
		// THE ARM OFFSET IS FROM THE UNION'S OWN BASE, with the tag at zero:
		// a payload-free arm has no member to take an offset of, and its arm
		// row is the offset the other arms share (docs/SPEC-TABLES.md §8.1)
		offset := fmt.Sprintf("(uint32_t) offsetof( %s, %s )", un.Name, v.Name)
		if v.Void() {
			_, _, _, armOffset := ir.UnionLayout(g.unit, un)
			offset = fmt.Sprintf("%d", armOffset)
		}
		fmt.Fprintf(&b, " { %s, %s, %s, %d },", offset, table, field, size)
	}
	fmt.Fprintf(&b, " }; static const TableUnionInfo info = { (uint32_t) offsetof( %s, type ), (uint32_t) sizeof( %s::type ), arms }; return &info; }",
		un.Name, un.Name)
	return b.String()
}

// emitArmFieldInfo emits one general arm's TableFieldInfo row, spelled inside
// the union's storage (docs/SPEC-TABLES.md §2.6, §8.1).
func (g *tableGen) emitArmFieldInfo(un *ir.Union, v ir.UnionVariant, hoisted bool) {
	member := v.Name
	if armCompanioned(v) {
		member = v.Name + ".value"
	}
	g.emitFieldInfo(v.F, fieldSpelling{owner: un.Name, member: member, indent: "", guard: ""}, hoisted)
}

// oneLine folds an emitted block onto one line: the descriptor rows ride
// inside a lambda that is itself one line of a field's initializer.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// bigToDouble renders a big.Int as a C++ double literal for the descriptor's
// range fields (precision past 2^53 is documented as lost).
func bigToDouble(v *big.Int) string {
	f, _ := new(big.Float).SetInt(v).Float64()
	return formatFloat(f, false)
}

// emitTableDescriptor emits TableType<X>() — the reflection descriptor: a
// function-local static (one instance per process, ODR-safe in inline
// functions), built once on first use.
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	guards := tableGuardStrings(st)
	// In a unit with pointers the descriptors are CONSTANT-INITIALISED data at
	// namespace scope, and a field's target is the ADDRESS of another
	// descriptor rather than a call. That is what makes a self- or
	// mutually-referential graph expressible at all: a lazy link cannot
	// describe Node -> *Node without re-entering its own initialisation, and
	// the read-modify-write it needed to try was a data race on first use. As
	// constant data the whole reflection surface is immutable and readable from
	// any thread at any time with no synchronisation.
	//
	// A pointer-free unit keeps the function-local statics it always had, to
	// the byte (docs/SPEC-TABLES.md §2.2, the zero-cost gate).
	hoisted := g.anyVariable
	indent := "    "
	if hoisted {
		indent = ""
	} else {
		g.pf("inline const TableTypeInfo * %sTableType()\n{\n", st.Name)
	}
	qualifier := "static const"
	infoQualifier := "static const"
	switch {
	case len(st.Fields) > 0:
		// the WIDE kinds' exact raw ranges (docs/SPEC-TABLES.md §8.2), one
		// constant per bounded wide field, named from the descriptor row
		for _, f := range st.Fields {
			if !ir.TableKindWide(tableScalarKind(f)) {
				continue
			}
			rlo, rhi, ok := ir.TableRawRange(f)
			if !ok {
				continue
			}
			lo0, lo1 := wideLanes(rlo)
			hi0, hi1 := wideLanes(rhi)
			g.pf("%s%s TableWideRange %s_%s_wide = { { %sull, %sull }, { %sull, %sull } };\n",
				indent, qualifier, st.Name, f.Name, lo0, lo1, hi0, hi1)
		}
		// the TAG lists (docs/SPEC-TABLES.md §8.1), one constant per tagged
		// field and one for a tagged declaration, named from the descriptor
		// row as the wide ranges are
		for _, f := range st.Fields {
			g.emitTagsStatic(fieldSpelling{owner: st.Name}.tagsName(f), f.Tags, qualifier, indent)
		}
		g.emitTagsStatic(st.Name+"_tags", st.Tags, qualifier, indent)
		if hoisted {
			g.pf("inline const TableFieldInfo %sTableFields[] = {\n", st.Name)
		} else {
			g.pf("    %s TableFieldInfo fields[] = {\n", qualifier)
		}
		for _, f := range st.Fields {
			g.emitFieldInfo(f, fieldSpelling{owner: st.Name, member: f.Name, indent: indent, guard: guards[f.Name]}, hoisted)
		}
		if hoisted {
			g.pf("};\n")
			g.pf("inline const TableTypeInfo %sTableInfo = { \"%s\", (uint32_t) sizeof( %s ), %d, %sTableFields, %s%s, %s };\n",
				st.Name, st.Name, st.Name, len(st.Fields), st.Name, resetLambda(st.Name), g.modeColumn(st), annotationColumns(st.Doc, st.Tags, st.Name+"_tags"))
		} else {
			g.pf("    };\n")
			g.pf("    %s TableTypeInfo info = { \"%s\", (uint32_t) sizeof( %s ), %d, fields, %s%s, %s };\n",
				infoQualifier, st.Name, st.Name, len(st.Fields), resetLambda(st.Name), g.modeColumn(st), annotationColumns(st.Doc, st.Tags, st.Name+"_tags"))
		}
	case hoisted:
		g.emitTagsStatic(st.Name+"_tags", st.Tags, qualifier, indent)
		g.pf("inline const TableTypeInfo %sTableInfo = { \"%s\", (uint32_t) sizeof( %s ), 0, NULL, %s%s, %s };\n",
			st.Name, st.Name, st.Name, resetLambda(st.Name), g.modeColumn(st), annotationColumns(st.Doc, st.Tags, st.Name+"_tags"))
	default:
		g.emitTagsStatic(st.Name+"_tags", st.Tags, qualifier, indent)
		g.pf("    %s TableTypeInfo info = { \"%s\", (uint32_t) sizeof( %s ), 0, NULL, %s%s, %s };\n",
			infoQualifier, st.Name, st.Name, resetLambda(st.Name), g.modeColumn(st), annotationColumns(st.Doc, st.Tags, st.Name+"_tags"))
	}
	if hoisted {
		g.pf("inline const TableTypeInfo * %sTableType() { return &%sTableInfo; }\n\n", st.Name, st.Name)
		return
	}
	g.pf("    return &info;\n}\n\n")
}

// fieldSpelling is HOW one descriptor row names its storage: the type
// offsetof takes, the member path inside it, and the indentation and guard
// columns of the row. A table's field spells `Owner, field`; a UNION ARM
// spells `Union, arm` — or `Union, arm.value` where the arm carries a
// companion (docs/SPEC-TABLES.md §2.6) — so one emitter serves both and an
// arm's descriptor can never drift from a field's.
type fieldSpelling struct {
	owner  string // the type offsetof and sizeof name
	member string // the member path within it
	indent string
	guard  string
}

// wideName is the name of the TableWideRange static a wide-kind row points at.
func (sp fieldSpelling) wideName(f *ir.Field) string { return sp.owner + "_" + f.Name + "_wide" }

// tagsName is the name of the tag-list static a tagged row points at.
func (sp fieldSpelling) tagsName(f *ir.Field) string { return sp.owner + "_" + f.Name + "_tags" }

// emitTagsStatic emits one tag list as a constant array of string literals
// (docs/SPEC-TABLES.md §8.1), and nothing at all for an item with no tags:
// absence is 0 and NULL in the row, never a per-row empty array.
func (g *tableGen) emitTagsStatic(name string, tags []string, qualifier, indent string) {
	if len(tags) == 0 {
		return
	}
	g.pf("%s%s char * const %s[] = { %s };\n", indent, qualifier, name, ir.QuotedTags(tags))
}

// annotationColumns renders a row's doc, num_tags and tags columns: the
// shared empty doc and a NULL list where the item carries none.
func annotationColumns(doc string, tags []string, tagsName string) string {
	docColumn := "TableDocNone"
	if doc != "" {
		docColumn = ir.QuoteDoc(doc)
	}
	list := "NULL"
	if len(tags) > 0 {
		list = tagsName
	}
	return fmt.Sprintf("%s, %d, %s", docColumn, len(tags), list)
}

// emitFieldInfo emits ONE TableFieldInfo row (docs/SPEC-TABLES.md §8.1). It is
// the whole descriptor vocabulary in one place: a field of a table and an arm
// of a union are the same row, differing only in how the storage is spelled.
func (g *tableGen) emitFieldInfo(f *ir.Field, sp fieldSpelling, hoisted bool) {
	id := tableFieldWireId(f)
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes && !f.Type.Blob() {
		kind = tkU8
	}
	isArray := f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Blob())
	if f.Type.Pointer {
		kind = tkNodeIndex // the descriptor states the WIRE (§8.1, §3.1); a byte buffer's slot is a node index too (§2.5)
	}
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString

	// the count column, spelled the way the storage spells its own
	// extent: a keyed array DERIVES it from the key enum, so nothing
	// outside the array names its size (docs/SPEC-TABLES.md §2.4, §8.1)
	bound := "0"
	switch {
	case f.KeyEnum != "":
		bound = fmt.Sprintf("(int32_t) %s::Max", f.KeyEnum)
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}

	// `T::field`, never `T{}.field`: a member id-expression in an
	// unevaluated operand names the type without an object, where the
	// braced form makes the compiler materialise a whole value of T to
	// take the size of one member of it. cl runs out of heap space
	// doing that for a multi-megabyte aggregate (C1060 on the block
	// corpus, whose RenderFrame is 7.9 MB across ten fields).
	elemSize := fmt.Sprintf("(uint32_t) sizeof( %s::%s )", sp.owner, sp.member)
	if isArray {
		elemSize = fmt.Sprintf("(uint32_t) sizeof( %s::%s[0] )", sp.owner, sp.member)
	}
	if f.KeyEnum != "" {
		// a keyed field's storage is the keyed type; its SLOTS are what
		// a walker steps through, and offset already names the first
		elemSize = fmt.Sprintf("(uint32_t) sizeof( %s )", g.keyedSlots(sp.owner+"::", f)+"[0]")
	}

	countOffset := "0xffffffffu"
	if counted {
		companion := sp.member + "_count"
		if f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString {
			companion = sp.member + "_length"
		}
		countOffset = fmt.Sprintf("(uint32_t) offsetof( %s, %s )", sp.owner, companion)
	}

	elemSizeOverride := ""
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		// a pointer's storage IS the reference slot: offset names it,
		// elem_size is the slot's width, and there is no companion
		elemSizeOverride = "(uint32_t) sizeof( TableRef )"
		counted = false
		bound = "0"
	}
	if elemSizeOverride != "" {
		elemSize = elemSizeOverride
		countOffset = "0xffffffffu"
	}
	if f.IsMap() || f.IsList() {
		// AN OUT-OF-LINE ARRAY (docs/SPEC-TABLES.md §8.1): an array field whose
		// elements are not inline, and array_bound = 0 is what says so. kind is
		// the ELEMENT's, as on every array line: 13 for a map's entry, the
		// element's own for a list, 17 for a []*T. is_array and counted are
		// set, elem_size is the pitch, count_offset names the int32 count
		// beside the reference in the sixteen-byte slot, and offset names the
		// REFERENCE, which a walker resolves before it steps.
		isArray = true
		counted = true
		bound = "0"
		countOffset = fmt.Sprintf("(uint32_t) offsetof( %s, %s.count )", sp.owner, sp.member)
		if f.IsMap() {
			kind = tkTable
			elemSize = fmt.Sprintf("(uint32_t) sizeof( %s )", f.MapEntry.Name)
		} else {
			kind = listElementWireKind(f)
			elemSize = fmt.Sprintf("(uint32_t) sizeof( %s )", g.listElementType(f))
		}
	}
	table := "NULL"
	if f.IsMap() {
		// the generated ENTRY's descriptor: fields[0] is the key and fields[1]
		// the value (§2.8, §8.1)
		if hoisted {
			table = "&" + f.MapEntry.Name + "TableInfo"
		} else {
			table = fmt.Sprintf("%sTableType()", f.MapEntry.Name)
		}
	}
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {

		if hoisted {
			// an ADDRESS, not a call: constant-initialisable, so a
			// self-reference (Node -> *Node) is simply &NodeTableInfo
			table = "&" + f.Type.Name + "TableInfo"
		} else {
			table = fmt.Sprintf("%sTableType()", f.Type.Name)
		}
		g.noteRef(f.Type.Name)
	}

	hasRange := "false"
	rangeMin, rangeMax := "0.0", "0.0"
	if f.Type.Kind == ir.TBits && !f.HasIntRange {
		// bits(N) declares its range by its WIDTH: [0, 2^N - 1]. The
		// codec has always clamped a read to it (docs/SPEC-TABLES.md §4);
		// carrying it here is what lets a generic walker apply the
		// same bound without re-deriving it from the type name.
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		hasRange = "true"
		rangeMin, rangeMax = "0.0", bigToDouble(max)
	}
	if f.HasIntRange {
		hasRange = "true"
		rangeMin, rangeMax = bigToDouble(f.IntMin), bigToDouble(f.IntMax)
	} else if f.HasFloatRange {
		hasRange = "true"
		rangeMin, rangeMax = formatFloat(f.FMin, false), formatFloat(f.FMax, false)
	}

	// the VOCABULARY columns: an enum's values, a union's arms and a
	// flags field's BITS are each a named set indexed by
	// [0, enum_max]. An enum's and a union's names carry the
	// table-wire id they ride under; a flags variant has none, and
	// that missing id is what tells the two apart at runtime
	// (docs/SPEC-TABLES.md §4, §5, §8).
	enumMax := "-1"
	enumName := "NULL"
	variantId := "NULL"
	arms := "NULL"
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", ref.Max)
			enumName = fmt.Sprintf("+[]( uint64_t v ) { return EnumName( %s( v ) ); }", f.Type.Name)
			variantId = fmt.Sprintf("+[]( uint64_t v ) -> uint64_t { uint64_t id = 0; TableEnumId( %s( v ), id ); return id; }", f.Type.Name)
		}
	case *ir.Flags:
		if f.Type.Kind == ir.TNamed {
			// a flags mask is the wire's one POSITIONAL vocabulary
			// (docs/SPEC-TABLES.md §4): its variants are BIT POSITIONS, so
			// the descriptor names bits, and there is no variant id.
			enumMax = fmt.Sprintf("%d", len(ref.Variants)-1)
			enumName = fmt.Sprintf("+[]( uint64_t v ) { return FlagName%s( (int) v ); }", f.Type.Name)
		}
	case *ir.Union:
		// on an ARRAY of unions too (§2.6): the walker asks the same
		// column per element, at elem_size strides
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", len(ref.Variants))
			enumName = unionArmLambda(ref, "const char *", func(v ir.UnionVariant) string {
				return fmt.Sprintf("\"%s\"", v.Name)
			}, "\"???\"", "\"None\"")
			variantId = unionArmLambda(ref, "uint64_t", func(v ir.UnionVariant) string {
				return fmt.Sprintf("0x%016xull", ir.TableWireId(v.WireName()))
			}, "0", "0")
			arms = g.unionArmsLambda(ref, hoisted)
			for _, v := range ref.Variants {
				g.noteRef(v.Type)
			}
		}
	}

	presentOffset := "0xffffffffu"
	if f.Type.Optional {
		presentOffset = fmt.Sprintf("(uint32_t) offsetof( %s, %s_present )", sp.owner, sp.member)
	}

	// the KEY's vocabulary on an enum-keyed array (docs/SPEC-TABLES.md §8):
	// functions of the KEY, not of the storage index — a walker
	// stepping [0, array_bound) asks about index + 1 and prints slots
	// by name without the schema files
	keyTypeName, keyName, keyId := "NULL", "NULL", "NULL"
	if f.KeyEnum != "" {
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keyName = fmt.Sprintf("+[]( uint64_t v ) { return EnumName( %s( v ) ); }", f.KeyEnum)
		keyId = fmt.Sprintf("+[]( uint64_t v ) -> uint64_t { uint64_t id = 0; TableEnumId( %s( v ), id ); return id; }", f.KeyEnum)
		g.noteRef(f.KeyEnum)
	}

	pointerColumn := ""
	if g.anyVariable {
		// the three columns a pointered unit's descriptors carry: the
		// flag, and the two thunks the ONE walk cannot spell for itself
		// (docs/SPEC-TABLES.md §16.7)
		resolve, emplace := "NULL", "NULL"
		if f.Type.Blob() {
			// a byte buffer's slot resolves to its blob's header; the
			// walk allocates one through the runtime's own Emplace,
			// which takes a length no descriptor thunk could carry
			resolve = "[]( const void * slot ) -> const void * { return (const void *) TableBlobAt( *(const TableRef *) slot ); }"
		} else if f.Type.Pointer {
			t := f.Type.Name
			resolve = fmt.Sprintf("[]( const void * slot ) -> const void * { return (const void *) %sAt( *(const TableRef *) slot ); }", t)
			emplace = fmt.Sprintf("[]( TableWorker & worker, void * slot ) -> void * { return (void *) %sEmplace( worker, *(TableRef *) slot ); }", t)
		}
		pointerColumn = fmt.Sprintf("%v, %s, %s, ", f.Type.Pointer, resolve, emplace)
	}
	// the wide columns: a fixed field's F, and the exact raw range
	// where the declaration bounds one (docs/SPEC-TABLES.md §8.2)
	fracBits := 0
	if f.Type.Kind == ir.TFixed {
		fracBits = f.Type.FracBits
	}
	wide := "NULL"
	if _, _, ok := ir.TableRawRange(f); ok && ir.TableKindWide(tableScalarKind(f)) {
		wide = "&" + sp.wideName(f)
	}
	g.pf("%s    { \"%s\", \"%s\", \"%s\", 0x%016xull, %d, %v, %s%v, %v, %s, (uint32_t) offsetof( %s, %s ), %s, %s, %s, %s, %s, %s, %s, %d, %s, %s, %s, %s, %s, %s, %s, %s, %s\"%s\", %s },\n",
		sp.indent, f.Name, ir.TableFieldJsonKey(f), tableFieldTypeName(f), id, kind, isArray, pointerColumn, counted, f.Type.Optional, bound,
		sp.owner, sp.member, elemSize, countOffset, presentOffset, table,
		hasRange, rangeMin, rangeMax, fracBits, wide, enumMax, enumName, variantId,
		keyTypeName, keyName, keyId, arms, g.placeColumn(f), sp.guard, annotationColumns(f.Doc, f.Tags, sp.tagsName(f)))
}

// ---- string, bytes and flags defaults (SPEC §4.2) ----
//
// A string(N) or bytes(N) field may declare the bytes a fresh value holds,
// and a flags field the mask. The storage initializer, Reset and the writer's
// elision compare all read the same bytes, so a field holding its default is
// elided exactly as a scalar at its default is, and an absent field reads as
// it (docs/SPEC-TABLES.md §4). A field with no default keeps the empty and
// zero forms it always had, emitted by the same lines as before.

// cStringLit renders bytes as a C++ string literal: printable ASCII as
// itself, a quote and a backslash escaped, and every other byte as a
// three-digit octal escape, which no following digit can extend.
func cStringLit(b []byte) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, c := range b {
		switch {
		case c == '"':
			sb.WriteString(`\"`)
		case c == '\\':
			sb.WriteString(`\\`)
		case c >= 0x20 && c < 0x7f:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, "\\%03o", c)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// byteListLit renders bytes as a braced list of hex octets, the initializer
// a uint8_t array takes.
func byteListLit(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("0x%02x", c)
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// flagsDefaultExpr renders a flags default as the declaration's own masks
// ored together, `( Perks_Shielded | Perks_Turbo )`, and `0` for the empty
// set.
func flagsDefaultExpr(f *ir.Field) string {
	fl, ok := f.Type.Ref.(*ir.Flags)
	if !ok || !f.HasDefault || f.DefInt == nil || f.DefInt.Sign() == 0 {
		return "0"
	}
	var names []string
	for i, v := range fl.Variants {
		if f.DefInt.Bit(i) == 1 {
			names = append(names, fl.Name+"_"+v)
		}
	}
	return "( " + strings.Join(names, " | ") + " )"
}

// lengthRidesTest is the condition under which whether a string or bytes
// field RIDES: with no default, a non-empty value; with one, a value that is
// not the default, length and bytes both.
// hasByteDefault reports a string or bytes default with at least one byte: an
// empty default is the zero form, and the zero form's lines already say it.
func hasByteDefault(f *ir.Field) bool {
	return f.HasDefault && len(f.DefBytes) > 0
}

func (g *tableGen) lengthRidesTest(f *ir.Field) string {
	if !hasByteDefault(f) {
		return fmt.Sprintf("value.%s_length > 0", f.Name)
	}
	lit := cStringLit(f.DefBytes)
	if f.Type.Kind == ir.TBytes {
		lit = fmt.Sprintf("%s_default", f.Name)
	}
	return fmt.Sprintf("!( value.%s_length == %d && memcmp( value.%s, %s, %d ) == 0 )", f.Name, len(f.DefBytes), f.Name, lit, len(f.DefBytes))
}

// emitBytesDefaultLocal declares the local a bytes default is compared
// against, ahead of the test that reads it. A string compares against its
// literal directly.
func (g *tableGen) emitBytesDefaultLocal(f *ir.Field) {
	if hasByteDefault(f) && f.Type.Kind == ir.TBytes {
		g.pf("    static const uint8_t %s_default[%d] = %s;\n", f.Name, len(f.DefBytes), byteListLit(f.DefBytes))
	}
}
