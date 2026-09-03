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
			return "0"
		}
	}
	return "0"
}

// ---- storage (table declarations only; closure types come from <Base>.h) ----

func (g *tableGen) emitTableStruct(st *ir.Struct) {
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
		g.emitTableStorageField(f)
	}
	g.pf("};\n\n")
}

func (g *tableGen) emitTableStorageField(f *ir.Field) {
	if f.Type.Pointer {
		// a pointer is EIGHT BYTES and no address: an arena offset while the
		// builder is mutable, a self-relative delta once packed. That is what
		// keeps a pointer-bearing table relocatable in both forms. An ARRAY
		// of pointers is that slot per element (docs/SPEC-TABLES.md §2.1).
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
	case f.Type.Kind == ir.TString:
		g.pf("    char %s[%d + 1] = {}; // string(%d): max length, used length beside it\n", f.Name, f.Type.Size, f.Type.Size)
		g.pf("    int32_t %s_length = 0;\n", f.Name)
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
	if f.Type.Pointer {
		switch f.Array {
		case ir.ArrayNone:
			g.pf("    value.%s.value = 0; // *%s — null\n", f.Name, f.Type.Name)
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
		}
	}
	return out
}

// emitEnumIdentity emits one enum's value <-> table-wire id pair. Behind a
// macro guard, not `#pragma once`: two files of a unit may both reach the same
// enum, and each emits the pair into its own header — one definition survives
// per translation unit whatever the include order.
func (g *tableGen) emitEnumIdentity(e *ir.Enum) {
	guard := strings.ToUpper(g.unit.Package) + "_SCHEMA_TABLE_ENUM_" + strings.ToUpper(e.Name)
	g.pf("// %s on the TABLE wire: a value rides as the u16 hash of its VARIANT\n", e.Name)
	g.pf("// NAME, so a variant may be added anywhere, removed, or reordered and old\n")
	g.pf("// data still reads (docs/SPEC-TABLES.md §5). None is the one reserved id, 0.\n")
	g.pf("#ifndef %s\n#define %s\n", guard, guard)
	g.pf("inline bool TableEnumId( %s value, uint16_t & id )\n{\n", e.Name)
	g.pf("    switch ( value )\n    {\n")
	g.pf("        case %s::None: id = 0; return true;\n", e.Name)
	for _, v := range e.Variants {
		g.pf("        case %s::%s: id = 0x%04x; return true;\n", e.Name, v, ir.VariantId(v))
	}
	g.pf("        default: return false; // no variant names this value: no wire identity\n")
	g.pf("    }\n}\n")
	g.pf("inline bool TableEnumValue( uint16_t id, %s & out )\n{\n", e.Name)
	g.pf("    switch ( id )\n    {\n")
	g.pf("        case 0: out = %s::None; return true;\n", e.Name)
	for _, v := range e.Variants {
		g.pf("        case 0x%04x: out = %s::%s; return true;\n", ir.VariantId(v), e.Name, v)
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
func (g *tableGen) emitTableMeasure(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("template <typename Ctx>\ninline int64_t %sMeasureBody( const Ctx & ctx, const %s & value )\n{\n", st.Name, st.Name)
		if g.noVariableEdges(st) {
			g.pf("    (void) ctx;\n")
		}
	} else {
		g.pf("inline int64_t %sMeasure( const %s & value )\n{\n", st.Name, st.Name)
	}
	if len(st.Fields) == 0 {
		g.pf("    (void) value; // empty type: presence is the payload\n")
	}
	g.pf("    int64_t bytes = 2; // terminator\n")
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
	g.pf("    return bytes;\n}\n\n")
}

func (g *tableGen) emitTableMeasureField(f *ir.Field) {
	kind := tableScalarKind(f)
	width := tableKindWidth(kind)
	switch {
	case f.Type.Optional:
		// an optional's PRESENCE is the payload: it rides even when the value
		// is entirely default, exactly as a pointer's pointee does — otherwise
		// absent and present-at-default would be one value on the wire
		g.pf("    if ( value.%s_present ) // ?%s: presence decides, not content\n    {\n", f.Name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "value."+f.Name))
			g.pf("        if ( body_%s < 0 ) { return -1; }\n", f.Name)
			g.pf("        bytes += 3 + 4 + body_%s; // %s\n", f.Name, f.Name)
		case enumRef(f) != nil:
			g.pf("        uint16_t id_%s = 0;\n", f.Name)
			g.pf("        if ( !TableEnumId( value.%s, id_%s ) ) { return -1; } // no variant names this value\n", f.Name, f.Name)
			g.pf("        bytes += 3 + 2; // %s: the variant's name hash\n", f.Name)
		default:
			g.pf("        bytes += 3 + %d; // %s\n", width, f.Name)
		}
		g.pf("    }\n")
	case f.KeyEnum != "":
		// enum-keyed: the body carries (variant id, length-prefixed element)
		// pairs, so a slot lands by NAME however the enum moved. A slot at its
		// default elides like any default, and an empty array elides whole.
		g.pf("    {\n")
		g.pf("        int64_t pairs_%s = 0, body_%s = 0;\n", f.Name, f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) // [%s]: every stored slot is a named variant's\n        {\n", f.ArrayBound, f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "            ", "return -1;")
		if kind == tkTable {
			g.pf("            pairs_%s++; body_%s += 2 + 4 + elem_bytes; // key, length, body\n", f.Name, f.Name)
		} else {
			g.pf("            pairs_%s++; body_%s += 2 + 4 + %d; // key, length, element\n", f.Name, f.Name, width)
		}
		g.pf("        }\n")
		g.pf("        if ( pairs_%s > 0 ) { bytes += 3 + 4 + 5 + body_%s; } // %s\n", f.Name, f.Name, f.Name)
		g.pf("    }\n")
	case f.Type.Pointer && f.Array == ir.ArrayCounted:
		// an ARRAY OF POINTERS (docs/SPEC-TABLES.md §2.1, §3.1): kind 14 with element
		// kind 17, N, then N node indices. CONTENT decides, as for any by-value
		// array: an empty one elides, and a live slot rides as its index, null as 0.
		g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value.%s_count > 0 ) { bytes += 3 + 4 + 5 + 4 * (int64_t) value.%s_count; } // %s: [..%d]*%s\n", f.Name, f.Name, f.Name, f.ArrayBound, f.Type.Name)
	case f.Type.Pointer && f.Array == ir.ArrayFixed:
		// a FIXED array of pointers holding only null is all-default and elides;
		// one non-null slot makes it ride whole, every slot as its index (§3.1)
		g.pf("    {\n")
		g.pf("        bool any_%s = false;\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( %sAt( ctx, value.%s[i] ) != NULL ) { any_%s = true; break; } }\n", f.ArrayBound, f.Type.Name, f.Name, f.Name)
		g.pf("        if ( any_%s ) { bytes += 3 + 4 + 5 + 4 * %d; } // %s: [%d]*%s\n", f.Name, f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
		g.pf("    }\n")
	case f.Type.Pointer:
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee_%s = %sAt( ctx, value.%s ); // *%s\n", t, f.Name, t, f.Name, t)
		g.pf("        // A POINTER RIDES AS A u32 NODE INDEX (docs/SPEC-TABLES.md §3.1):\n")
		g.pf("        // seven bytes and nothing below it, because the pointee's body is\n")
		g.pf("        // in the node table and not here. NULL IS ELIDED — absence and null\n")
		g.pf("        // are one value — and a non-null pointer ALWAYS rides, even when its\n")
		g.pf("        // node's body is entirely default.\n")
		g.pf("        if ( pointee_%s != NULL ) { bytes += 3 + 4; }\n", f.Name)
		g.pf("    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if ( value.%s_length < 0 || value.%s_length > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.Type.Size)
		g.pf("    if ( value.%s_length > 0 ) { bytes += 3 + 4 + value.%s_length; } // %s\n", f.Name, f.Name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if ( value.%s_length < 0 || value.%s_length > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.Type.Size)
		g.pf("    if ( value.%s_length > 0 ) { bytes += 3 + 4 + 5 + value.%s_length; } // %s\n", f.Name, f.Name, f.Name)
	case f.Array == ir.ArrayCounted && kind == tkTable:
		g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value.%s_count > 0 )\n    {\n", f.Name)
		g.pf("        bytes += 3 + 4 + 5; // %s\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < value.%s_count; i++ )\n        {\n", f.Name)
		g.pf("            int64_t elem_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, fmt.Sprintf("value.%s[i]", f.Name)))
		g.pf("            if ( elem_%s < 0 ) { return -1; }\n", f.Name)
		g.pf("            bytes += 4 + elem_%s;\n", f.Name)
		g.pf("        }\n    }\n")
	case f.Array == ir.ArrayCounted && kind == tkUnion:
		// an ARRAY OF UNIONS (docs/SPEC-TABLES.md §2.6, §3): kind 14 with element
		// kind 15, each element the union payload in its place. CONTENT decides,
		// as for any by-value array: an empty one elides, and a live None
		// element rides as the two-byte arm id 0.
		g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value.%s_count > 0 )\n    {\n", f.Name)
		g.pf("        bytes += 3 + 4 + 5; // %s\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < value.%s_count; i++ )\n        {\n", f.Name)
		g.emitUnionElementMeasure(f, fmt.Sprintf("value.%s[i]", f.Name), "            ")
		g.pf("        }\n    }\n")
	case f.Array == ir.ArrayFixed && kind == tkUnion:
		// a FIXED array of unions holding only None is all-default and elides;
		// one set element makes it ride whole, None elements in place (§3)
		g.pf("    {\n")
		g.pf("        bool any_%s = false;\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i].type != %sType::None ) { any_%s = true; break; } }\n", f.ArrayBound, f.Name, f.Type.Name, f.Name)
		g.pf("        if ( any_%s )\n        {\n", f.Name)
		g.pf("            bytes += 3 + 4 + 5; // %s (fixed [%d])\n", f.Name, f.ArrayBound)
		g.pf("            for ( int32_t i = 0; i < %d; i++ )\n            {\n", f.ArrayBound)
		g.emitUnionElementMeasure(f, fmt.Sprintf("value.%s[i]", f.Name), "                ")
		g.pf("            }\n        }\n    }\n")
	case f.Array == ir.ArrayCounted:
		g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return -1; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value.%s_count > 0 )\n    {\n", f.Name)
		g.emitEnumElementCheck(f, fmt.Sprintf("value.%s[i]", f.Name), fmt.Sprintf("value.%s_count", f.Name), "        ", "return -1;")
		g.pf("        bytes += 3 + 4 + 5 + int64_t( value.%s_count ) * %d; // %s\n", f.Name, width, f.Name)
		g.pf("    }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		g.pf("    {\n")
		g.pf("        bytes += 3 + 4 + 5; // %s (fixed [%d])\n", f.Name, f.ArrayBound)
		g.pf("        for ( int32_t i = 0; i < %d; i++ )\n        {\n", f.ArrayBound)
		g.pf("            int64_t elem_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, fmt.Sprintf("value.%s[i]", f.Name)))
		g.pf("            if ( elem_%s < 0 ) { return -1; }\n", f.Name)
		g.pf("            bytes += 4 + elem_%s;\n", f.Name)
		g.pf("        }\n    }\n")
	case f.Array == ir.ArrayFixed:
		g.pf("    {\n")
		g.pf("        bool all_default_%s = true;\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i] != %s ) { all_default_%s = false; break; } }\n",
			f.ArrayBound, f.Name, g.fieldDefaultExpr(f), f.Name)
		g.pf("        if ( !all_default_%s )\n        {\n", f.Name)
		g.emitEnumElementCheck(f, fmt.Sprintf("value.%s[i]", f.Name), fmt.Sprintf("%d", f.ArrayBound), "            ", "return -1;")
		g.pf("            bytes += 3 + 4 + 5 + %d; // %s\n", f.ArrayBound*int64(width), f.Name)
		g.pf("        }\n    }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("    switch ( value.%s.type ) // %s\n    {\n", f.Name, f.Name)
		g.pf("        case %sType::None: break; // None elides — TLV absence is the None\n", un.Name)
		for _, v := range un.Variants {
			g.pf("        case %sType::%s:\n        {\n", un.Name, ir.GoExportName(v.Name))
			g.pf("            int64_t arm_%s = %s;\n", f.Name, g.measureCall(v.Type, fmt.Sprintf("value.%s.%s", f.Name, v.Name)))
			g.pf("            if ( arm_%s < 0 ) { return -1; }\n", f.Name)
			g.pf("            bytes += 3 + 2 + 4 + arm_%s; // the u16 ARM ID, then the arm length-prefixed\n            break;\n        }\n", f.Name)
		}
		g.pf("        default: return -1; // invalid tag — the write side refuses it too\n")
		g.pf("    }\n")
	case kind == tkTable:
		g.pf("    {\n")
		g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "value."+f.Name))
		g.pf("        if ( body_%s < 0 ) { return -1; }\n", f.Name)
		g.pf("        if ( body_%s > 2 ) { bytes += 3 + 4 + body_%s; } // %s: all-default nested elides\n", f.Name, f.Name, f.Name)
		g.pf("    }\n")
	case enumRef(f) != nil:
		g.pf("    if ( value.%s != %s )\n    {\n", f.Name, g.fieldDefaultExpr(f))
		g.pf("        uint16_t id_%s = 0;\n", f.Name)
		g.pf("        if ( !TableEnumId( value.%s, id_%s ) ) { return -1; } // no variant names this value\n", f.Name, f.Name)
		g.pf("        bytes += 3 + 2; // %s: the variant's name hash\n    }\n", f.Name)
	default:
		g.pf("    if ( value.%s != %s ) { bytes += 3 + %d; } // %s\n", f.Name, g.fieldDefaultExpr(f), width, f.Name)
	}
}

// emitUnionElementMeasure adds one element of an ARRAY OF UNIONS to `bytes`
// (docs/SPEC-TABLES.md §2.6, §3): the two-byte arm id for None, and the arm
// id, the length and the arm body for a set arm. A tag no arm names measures
// as -1, exactly as the write side refuses it.
func (g *tableGen) emitUnionElementMeasure(f *ir.Field, expr, ind string) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
	g.pf("%s    case %sType::None: bytes += 2; break; // a None element is the arm id 0 in its place\n", ind, un.Name)
	for _, v := range un.Variants {
		g.noteRef(v.Type)
		g.pf("%s    case %sType::%s:\n%s    {\n", ind, un.Name, ir.GoExportName(v.Name), ind)
		g.pf("%s        int64_t arm_bytes = %s;\n", ind, g.measureCall(v.Type, expr+"."+v.Name))
		g.pf("%s        if ( arm_bytes < 0 ) { return -1; }\n", ind)
		g.pf("%s        bytes += 2 + 4 + arm_bytes; // the u16 ARM ID, then the arm length-prefixed\n%s        break;\n%s    }\n", ind, ind, ind)
	}
	g.pf("%s    default: return -1; // invalid tag — the write side refuses it too\n%s}\n", ind, ind)
}

// emitKeyedSlotRides emits the head of an enum-keyed array's per-slot loop
// (docs/SPEC-TABLES.md §2.4, §3.2). It elides a slot holding its default, refuses a
// slot whose value or whose KEY no variant names — a value with no wire
// identity is refused rather than silently renamed, the enum rule applied to
// slots — and leaves `key_id` holding the slot's wire id. For a table element
// `elem_bytes` holds the measured body, so measure and save decide elision on
// the same number; for an enum element `element_id` holds the resolved id, and
// the save path writes THAT rather than resolving the same value twice — a
// second `element_id` in the loop would shadow this one, which cl refuses
// under /W4 (C4456) and the POSIX legs' -Wshadow now refuses too.
func (g *tableGen) emitKeyedSlotRides(f *ir.Field, kind int, ind, onBad string) {
	expr := g.keyedSlots("value.", f) + "[i]"
	switch {
	case kind == tkTable:
		g.pf("%sint64_t elem_bytes = %s;\n", ind, g.measureCall(f.Type.Name, expr))
		g.pf("%sif ( elem_bytes < 0 ) { %s }\n", ind, onBad)
		g.pf("%sif ( elem_bytes <= 2 ) { continue; } // an all-default slot elides\n", ind)
	case enumRef(f) != nil:
		g.pf("%sif ( %s == %s ) { continue; } // a default slot elides\n", ind, expr, g.fieldDefaultExpr(f))
		g.pf("%suint16_t element_id = 0;\n", ind)
		g.pf("%sif ( !TableEnumId( %s, element_id ) ) { %s } // no variant names this value\n", ind, expr, onBad)
	default:
		g.pf("%sif ( %s == %s ) { continue; } // a default slot elides\n", ind, expr, g.fieldDefaultExpr(f))
	}
	g.pf("%suint16_t key_id = 0;\n", ind)
	g.pf("%sif ( !TableEnumId( %s( i + 1 ), key_id ) ) { %s } // i is the STORAGE index; the key it holds is i + 1\n",
		ind, f.KeyEnum, onBad)
}

// emitEnumElementCheck validates an enum ARRAY's elements before they ride: a
// value no variant names has no wire identity, so the value is refused rather
// than silently written as None (the union tag's rule, applied to enums).
func (g *tableGen) emitEnumElementCheck(f *ir.Field, expr, count, ind, onBad string) {
	if enumRef(f) == nil {
		return
	}
	g.pf("%sfor ( int32_t i = 0; i < %s; i++ ) // %s: every element must be nameable\n", ind, count, f.Name)
	g.pf("%s{\n%s    uint16_t element_id = 0;\n", ind, ind)
	g.pf("%s    if ( !TableEnumId( %s, element_id ) ) { %s }\n%s}\n", ind, expr, onBad, ind)
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
		// The ROOT's fields and the node table's fields are fields of ONE body,
		// so the terminator is written by whoever knows the body is finished:
		// the wrapper below for a nested body, and the wire surface for a root
		// that still owes its node table (docs/SPEC-TABLES.md §3.1).
		g.pf("template <typename Ctx>\ninline bool %sSaveBodyFields( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, const %s & value )\n{\n", st.Name, st.Name)
		if g.noVariableEdges(st) {
			g.pf("    (void) ctx; (void) numbering;\n")
		}
	} else {
		g.pf("%s bool %sSaveBody( TableWriter & w, const %s & value )\n{\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
	}
	if len(st.Fields) == 0 {
		g.pf("    (void) value; // empty type: presence is the payload\n")
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
	if g.isVar(st.Name) {
		g.pf("    return !w.overflow;\n}\n\n")
		// the ordinary body: the fields, then the terminator. A nested body is
		// finished when its fields are, and only a ROOT owes a node table.
		g.pf("template <typename Ctx>\ninline bool %sSaveBody( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, const %s & value )\n{\n", st.Name, st.Name)
		g.pf("    if ( !%sSaveBodyFields( ctx, numbering, w, value ) ) { return false; }\n", st.Name)
		g.pf("    w.put16( 0 ); // terminator\n")
		g.pf("    return !w.overflow;\n}\n\n")
		return
	}
	g.pf("    w.put16( 0 ); // terminator\n")
	g.pf("    return !w.overflow;\n}\n\n")
}

// emitTableSave emits the buffer-level entry of the measure/save pair:
// <X>Save writes into a caller-provided buffer and returns the bytes written —
// exactly <X>Measure's answer — or -1 when the buffer is too small. No allocation anywhere: the caller owns the buffer.
func (g *tableGen) emitTableSave(st *ir.Struct) {
	if g.isVar(st.Name) {
		return // a variable-length table's Save takes a builder or a region root
	}
	g.pf("inline int64_t %sSave( const %s & value, uint8_t * buffer, int64_t capacity )\n{\n", st.Name, st.Name)
	g.pf("    TableWriter w( buffer, capacity );\n")
	g.pf("    if ( !%sSaveBody( w, value ) ) { return -1; }\n", st.Name)
	g.pf("    return w.offset; // == %sMeasure( value )\n}\n\n", st.Name)
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	id := ir.TableFieldId(f)
	kind := tableScalarKind(f)
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
		case kind == tkTable:
			g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "value."+f.Name))
			g.pf("        if ( body_%s < 0 ) return false; // storage invariant, refused as measure refuses it\n", f.Name)
			g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkTable, f.Name)
			g.pf("        w.put32( uint32_t( body_%s ) );\n", f.Name)
			g.pf("        if ( !%s ) return false;\n", g.saveCall(f.Type.Name, "value."+f.Name))
		case enumRef(f) != nil:
			g.pf("        uint16_t id_%s = 0;\n", f.Name)
			g.pf("        if ( !TableEnumId( value.%s, id_%s ) ) { return false; }\n", f.Name, f.Name)
			g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, kind, f.Name)
			g.pf("        w.put16( id_%s );\n", f.Name)
		default:
			g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, kind, f.Name)
			g.emitTableWriteElement(f, kind, "value."+f.Name, "        ")
		}
		g.pf("    }\n")
	case f.KeyEnum != "":
		// the slots ride KEYED: (variant id, length-prefixed element) pairs,
		// counted like any array's elements. Two passes so the count is known
		// before the header rides, and so measure and save agree byte for byte.
		g.pf("    {\n")
		g.pf("        uint32_t pairs_%s = 0;\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) // [%s]: every stored slot is a named variant's\n        {\n", f.ArrayBound, f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "            ", "return false;")
		g.pf("            pairs_%s++;\n", f.Name)
		g.pf("        }\n")
		g.pf("        if ( pairs_%s > 0 )\n        {\n", f.Name)
		g.pf("            // KIND 16, not 14: a keyed body and a positional one are\n")
		g.pf("            // incompatible, so a reader of the other kind must see a kind\n")
		g.pf("            // mismatch and skip, never misdecode (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("            w.put16( 0x%04x ); w.put8( %d ); // %s (keyed by %s)\n", id, tkKeyed, f.Name, f.KeyEnum)
		g.pf("            int64_t len_at_%s = w.offset; w.put32( 0 );\n", f.Name)
		g.pf("            w.put8( %d ); w.put32( pairs_%s );\n", kind, f.Name)
		g.pf("            // ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
		g.pf("            // writer's choice, and a reader must not rely on it: every\n")
		g.pf("            // slot is found by its key (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("            for ( int32_t i = 0; i < %d; i++ )\n            {\n", f.ArrayBound)
		g.emitKeyedSlotRides(f, kind, "                ", "return false;")
		g.pf("                w.put16( key_id ); // the slot's VARIANT id, not its position\n")
		g.pf("                int64_t elem_len_at_%s = w.offset; w.put32( 0 );\n", f.Name)
		switch {
		case kind == tkTable:
			g.pf("                if ( !%s ) return false;\n", g.saveCall(f.Type.Name, g.keyedSlots("value.", f)+"[i]"))
		case enumRef(f) != nil:
			// the slot's id is already resolved above, like key_id: writing it
			// here rather than resolving the same value a second time
			g.pf("                w.put16( element_id );\n")
		default:
			g.emitTableWriteElement(f, kind, g.keyedSlots("value.", f)+"[i]", "                ")
		}
		g.pf("                w.patch32( elem_len_at_%s, uint32_t( w.offset - elem_len_at_%s - 4 ) );\n", f.Name, f.Name)
		g.pf("            }\n")
		g.pf("            w.patch32( len_at_%s, uint32_t( w.offset - len_at_%s - 4 ) );\n", f.Name, f.Name)
		g.pf("        }\n    }\n")
	case f.Type.Pointer && f.Array != ir.ArrayNone:
		// an ARRAY OF POINTERS (§2.1, §3.1): the array framing with element kind
		// 17, one node index per slot, null as 0. A counted array rides its live
		// slots when it has any; a fixed one rides whole when any slot is non-null.
		count := fmt.Sprintf("value.%s_count", f.Name)
		if f.Array == ir.ArrayCounted {
			g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return false; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
			g.pf("    if ( value.%s_count > 0 )\n    {\n", f.Name)
		} else {
			count = strconv.FormatInt(f.ArrayBound, 10)
			g.pf("    {\n")
			g.pf("        bool any_%s = false;\n", f.Name)
			g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( %sAt( ctx, value.%s[i] ) != NULL ) { any_%s = true; break; } }\n", f.ArrayBound, f.Type.Name, f.Name, f.Name)
			g.pf("        if ( any_%s )\n        {\n", f.Name)
		}
		g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkArray, f.Name)
		g.pf("        w.put32( uint32_t( 5 + 4 * (int64_t) %s ) );\n", count)
		g.pf("        w.put8( %d ); w.put32( uint32_t( %s ) ); // element kind 17: node indices\n", tkNodeIndex, count)
		g.pf("        for ( int32_t i = 0; i < %s; i++ )\n        {\n", count)
		g.emitTableWriteElement(f, tkNodeIndex, fmt.Sprintf("value.%s[i]", f.Name), "            ")
		g.pf("        }\n")
		if f.Array == ir.ArrayFixed {
			g.pf("        }\n")
		}
		g.pf("    }\n")
	case f.Type.Pointer:
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee_%s = %sAt( ctx, value.%s ); // *%s\n", t, f.Name, t, f.Name, t)
		g.pf("        if ( pointee_%s != NULL )\n        {\n", f.Name)
		g.pf("            uint32_t index_%s = 0;\n", f.Name)
		g.pf("            if ( !TableNumberingIndex( numbering, (const void *) pointee_%s, index_%s ) ) { return false; }\n", f.Name, f.Name)
		g.pf("            w.put16( 0x%04x ); w.put8( %d ); // %s — a NODE INDEX into the flat node table\n", id, tkNodeIndex, f.Name)
		g.pf("            w.put32( index_%s );\n", f.Name)
		g.pf("        }\n    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if ( value.%s_length < 0 || value.%s_length > %d ) { return false; } // storage invariant\n", f.Name, f.Name, f.Type.Size)
		g.pf("    if ( value.%s_length > 0 )\n    {\n", f.Name)
		g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkString, f.Name)
		g.pf("        w.put32( uint32_t( value.%s_length ) );\n", f.Name)
		g.pf("        w.raw( value.%s, value.%s_length );\n    }\n", f.Name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if ( value.%s_length < 0 || value.%s_length > %d ) { return false; } // storage invariant\n", f.Name, f.Name, f.Type.Size)
		g.pf("    if ( value.%s_length > 0 )\n    {\n", f.Name)
		g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkArray, f.Name)
		g.pf("        w.put32( uint32_t( 5 + value.%s_length ) );\n", f.Name)
		g.pf("        w.put8( %d ); w.put32( uint32_t( value.%s_length ) );\n", tkU8, f.Name)
		g.pf("        w.raw( value.%s, value.%s_length );\n    }\n", f.Name, f.Name)
	case f.Array == ir.ArrayCounted:
		g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return false; } // storage invariant\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value.%s_count > 0 )\n    {\n", f.Name)
		g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkArray, f.Name)
		g.pf("        int64_t len_at_%s = w.offset; w.put32( 0 );\n", f.Name)
		g.pf("        w.put8( %d ); w.put32( uint32_t( value.%s_count ) );\n", kind, f.Name)
		g.pf("        for ( int32_t i = 0; i < value.%s_count; i++ )\n        {\n", f.Name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", f.Name), "            ")
		g.pf("        }\n")
		g.pf("        w.patch32( len_at_%s, uint32_t( w.offset - len_at_%s - 4 ) );\n    }\n", f.Name, f.Name)
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — no cheap element-default
		// compare in C++ (an all-default element costs 6 bytes)
		g.pf("    {\n")
		g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("        int64_t len_at_%s = w.offset; w.put32( 0 );\n", f.Name)
		g.pf("        w.put8( %d ); w.put32( %d );\n", kind, f.ArrayBound)
		g.pf("        for ( int32_t i = 0; i < %d; i++ )\n        {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", f.Name), "            ")
		g.pf("        }\n")
		g.pf("        w.patch32( len_at_%s, uint32_t( w.offset - len_at_%s - 4 ) );\n    }\n", f.Name, f.Name)
	case f.Array == ir.ArrayFixed && kind == tkUnion:
		// a fixed array of unions is positional too: all None elides, and one
		// set element makes every element ride in its place (§2.6, §3)
		g.pf("    {\n")
		g.pf("        bool any_%s = false;\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i].type != %sType::None ) { any_%s = true; break; } }\n", f.ArrayBound, f.Name, f.Type.Name, f.Name)
		g.pf("        if ( any_%s )\n        {\n", f.Name)
		g.pf("            w.put16( 0x%04x ); w.put8( %d ); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("            int64_t len_at_%s = w.offset; w.put32( 0 );\n", f.Name)
		g.pf("            w.put8( %d ); w.put32( %d );\n", kind, f.ArrayBound)
		g.pf("            for ( int32_t i = 0; i < %d; i++ )\n            {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", f.Name), "                ")
		g.pf("            }\n")
		g.pf("            w.patch32( len_at_%s, uint32_t( w.offset - len_at_%s - 4 ) );\n        }\n    }\n", f.Name, f.Name)
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		// (parity with the reader's prefill)
		g.pf("    {\n")
		g.pf("        bool all_default_%s = true;\n", f.Name)
		g.pf("        for ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i] != %s ) { all_default_%s = false; break; } }\n",
			f.ArrayBound, f.Name, g.fieldDefaultExpr(f), f.Name)
		g.pf("        if ( !all_default_%s )\n        {\n", f.Name)
		g.pf("            w.put16( 0x%04x ); w.put8( %d ); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("            int64_t len_at_%s = w.offset; w.put32( 0 );\n", f.Name)
		g.pf("            w.put8( %d ); w.put32( %d );\n", kind, f.ArrayBound)
		g.pf("            for ( int32_t i = 0; i < %d; i++ )\n            {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", f.Name), "                ")
		g.pf("            }\n")
		g.pf("            w.patch32( len_at_%s, uint32_t( w.offset - len_at_%s - 4 ) );\n        }\n    }\n", f.Name, f.Name)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		for _, v := range un.Variants {
			g.noteRef(v.Type)
		}
		g.pf("    if ( value.%s.type != %sType::None )\n    {\n", f.Name, un.Name)
		g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkUnion, f.Name)
		g.pf("        // the ARM ID is the hash of the arm's NAME (docs/SPEC-TABLES.md §5), so\n")
		g.pf("        // arms may be added anywhere, removed and reordered\n")
		g.pf("        switch ( value.%s.type )\n        {\n", f.Name)
		for _, v := range un.Variants {
			g.pf("            case %sType::%s: w.put16( 0x%04x ); break;\n", un.Name, ir.GoExportName(v.Name), ir.VariantId(v.Name))
		}
		g.pf("            default: return false; // write validates the tag before it rides\n")
		g.pf("        }\n")
		g.pf("        int64_t len_at_%s = w.offset; w.put32( 0 );\n", f.Name)
		g.pf("        switch ( value.%s.type )\n        {\n", f.Name)
		for _, v := range un.Variants {
			g.pf("            case %sType::%s: if ( !%s ) return false; break;\n",
				un.Name, ir.GoExportName(v.Name), g.saveCall(v.Type, fmt.Sprintf("value.%s.%s", f.Name, v.Name)))
		}
		g.pf("            default: return false; // write validates the tag before it rides\n")
		g.pf("        }\n")
		g.pf("        w.patch32( len_at_%s, uint32_t( w.offset - len_at_%s - 4 ) );\n    }\n", f.Name, f.Name)
	case kind == tkTable:
		// elision is decided BEFORE any byte is emitted: measuring first
		// keeps an all-default nested field from touching the buffer at all,
		// so saving into a buffer of exactly TableMeasure's size never
		// trips overflow on transient header bytes
		g.pf("    {\n")
		g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "value."+f.Name))
		g.pf("        if ( body_%s < 0 ) return false; // storage invariant, refused as measure refuses it\n", f.Name)
		g.pf("        if ( body_%s > 2 ) // all-default nested elides\n        {\n", f.Name)
		g.pf("            w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkTable, f.Name)
		g.pf("            w.put32( uint32_t( body_%s ) );\n", f.Name)
		g.pf("            if ( !%s ) return false;\n", g.saveCall(f.Type.Name, "value."+f.Name))
		g.pf("        }\n    }\n")
	case enumRef(f) != nil:
		// the id is resolved BEFORE the header rides: a value no variant names
		// has no wire identity, and the write refuses it rather than writing None
		g.pf("    if ( value.%s != %s )\n    {\n", f.Name, g.fieldDefaultExpr(f))
		g.pf("        uint16_t id_%s = 0;\n", f.Name)
		g.pf("        if ( !TableEnumId( value.%s, id_%s ) ) { return false; }\n", f.Name, f.Name)
		g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, kind, f.Name)
		g.pf("        w.put16( id_%s );\n    }\n", f.Name)
	default:
		g.pf("    if ( value.%s != %s )\n    {\n", f.Name, g.fieldDefaultExpr(f))
		g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, kind, f.Name)
		g.emitTableWriteElement(f, kind, "value."+f.Name, "        ")
		g.pf("    }\n")
	}
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	if enumRef(f) != nil {
		g.pf("%s{\n%s    uint16_t element_id = 0;\n", ind, ind)
		g.pf("%s    if ( !TableEnumId( %s, element_id ) ) { return false; }\n", ind, expr)
		g.pf("%s    w.put16( element_id );\n%s}\n", ind, ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%sw.put8( %s ? 1 : 0 );\n", ind, expr)
	case tkF32:
		g.pf("%sw.put32( table_float_to_bits( %s ) );\n", ind, expr)
	case tkF64:
		g.pf("%sw.put64( table_double_to_bits( %s ) );\n", ind, expr)
	case tkTable:
		g.pf("%s{\n%s    int64_t elem_len_at = w.offset; w.put32( 0 );\n", ind, ind)
		g.pf("%s    if ( !%s ) return false;\n", ind, g.saveCall(f.Type.Name, expr))
		g.pf("%s    w.patch32( elem_len_at, uint32_t( w.offset - elem_len_at - 4 ) );\n%s}\n", ind, ind)
	case tkUnion:
		// one element of an array of unions: the union payload in its place —
		// the arm id, then the arm length-prefixed; None is the arm id 0 (§3)
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
		g.pf("%s    case %sType::None: w.put16( 0 ); break; // a None element rides in its place\n", ind, un.Name)
		for _, v := range un.Variants {
			g.noteRef(v.Type)
			g.pf("%s    case %sType::%s:\n%s    {\n", ind, un.Name, ir.GoExportName(v.Name), ind)
			g.pf("%s        w.put16( 0x%04x ); // the arm's NAME hash (docs/SPEC-TABLES.md §5)\n", ind, ir.VariantId(v.Name))
			g.pf("%s        int64_t arm_len_at = w.offset; w.put32( 0 );\n", ind)
			g.pf("%s        if ( !%s ) return false;\n", ind, g.saveCall(v.Type, expr+"."+v.Name))
			g.pf("%s        w.patch32( arm_len_at, uint32_t( w.offset - arm_len_at - 4 ) );\n%s        break;\n%s    }\n", ind, ind, ind)
		}
		g.pf("%s    default: return false; // write validates the tag before it rides\n%s}\n", ind, ind)
	case tkNodeIndex:
		// one slot of an array of pointers: its node index, null as 0 (§3.1)
		g.pf("%s{\n%s    const %s * slot_pointee = %sAt( ctx, %s );\n", ind, ind, f.Type.Name, f.Type.Name, expr)
		g.pf("%s    uint32_t slot_index = 0;\n", ind)
		g.pf("%s    if ( slot_pointee != NULL && !TableNumberingIndex( numbering, (const void *) slot_pointee, slot_index ) ) { return false; }\n", ind)
		g.pf("%s    w.put32( slot_index );\n%s}\n", ind, ind)
	default:
		width := tableKindWidth(kind)
		if width == 16 {
			// the two lanes of the raw value, low half first — the type
			// wire's own order (docs/SPEC-TABLES.md §3); the unsigned
			// conversion is bit-preserving for the signed kinds
			g.pf("%s{ serialize::uint128_t raw_v = serialize::uint128_t( %s ); w.put128( uint64_t( raw_v ), uint64_t( raw_v >> 64 ) ); }\n", ind, expr)
			return
		}
		cast := fmt.Sprintf("uint%d_t", width*8)
		g.pf("%sw.%s( %s( %s ) );\n", ind, tablePut(width), cast, expr)
	}
}

// ---- read ----

func (g *tableGen) emitTableRead(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("inline bool %sLoadBody( TableReader & r, const TableNodeMap & nodes, %s & value )\n{\n", st.Name, st.Name)
		if g.noVariableEdges(st) {
			g.pf("    (void) nodes;\n")
		}
	} else {
		g.pf("%s bool %sLoadBody( TableReader & r, %s & value )\n{\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
	}
	// `<T>Reset`, NOT `value = T{}` and not `new ( &value ) T{}`: assignment
	// materializes a temporary, and generated types can be large — a stack
	// bomb on worker threads — while a whole-object value-init costs cl
	// O(bytes) to COMPILE (#320). Reset applies the same declared defaults in
	// place, with neither cost.
	g.pf("    %sReset( value ); // prefill declared defaults in place, then overlay\n", st.Name)
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        if ( !r.has( 2 ) ) { r.report->malformed = true; return false; }\n")
	g.pf("        uint16_t field_id = r.get16();\n")
	g.pf("        if ( field_id == 0 ) return true;\n")
	g.pf("        if ( !r.has( 1 ) ) { r.report->malformed = true; return false; }\n")
	g.pf("        uint8_t kind = r.get8();\n")
	if len(st.Fields) > 0 {
		g.pf("        switch ( field_id )\n        {\n")
		for _, f := range st.Fields {
			id := ir.TableFieldId(f)
			kind := tableScalarKind(f)
			wireKind := kind
			if f.Type.Pointer {
				// a pointer rides as a u32 NODE INDEX under its own kind, so an
				// edit between a by-value nesting and a pointer reads as an
				// ordinary kind mismatch and is counted, never decoded as the
				// other shape (docs/SPEC-TABLES.md §3.1)
				kind, wireKind = tkNodeIndex, tkNodeIndex
			}
			if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
				wireKind = tkArray
			}
			if f.KeyEnum != "" {
				// a KEYED body is its own kind, so the positional-to-keyed edit
				// (and its reverse) reads as a kind mismatch and is counted,
				// never decoded as the other body (docs/SPEC-TABLES.md §3.2)
				wireKind = tkKeyed
			}
			if f.Type.Kind == ir.TBytes {
				kind = tkU8 // bytes travel as an array of u8 elements
			}
			g.pf("            case 0x%04x: // %s\n            {\n", id, f.Name)
			g.pf("                if ( kind != %d )\n                {\n", wireKind)
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
			// here and NEVER counted unknown: 0xFFFF is not a field of the
			// table, it is where the numbering travelled. An `unknown` here
			// would mean "a build without kind 17", which is the difference §4
			// exists to report and not one this reader has.
			g.pf("            case 0x%04x:\n            {\n", ir.NodeTableFieldId)
			g.pf("                if ( !r.skip( kind ) ) { r.report->malformed = true; return false; }\n")
			g.pf("                break;\n            }\n")
		}
		g.pf("            default:\n            {\n")
		g.pf("                r.report->unknown++;\n")
		g.pf("                if ( !r.skip( kind ) ) { r.report->malformed = true; return false; }\n")
		g.pf("                break;\n            }\n")
		g.pf("        }\n    }\n}\n\n")
	} else {
		g.pf("        r.report->unknown++;\n")
		g.pf("        if ( !r.skip( kind ) ) { r.report->malformed = true; return false; }\n")
		g.pf("    }\n}\n\n")
	}

	// buffer-level convenience entry. A VARIABLE-LENGTH table has none: it is
	// never held by value, so its Load takes the caller's region and hands back
	// the root instead (docs/SPEC-TABLES.md §2).
	if g.isVar(st.Name) {
		return
	}
	g.pf("inline bool %sLoad( %s & value, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", st.Name, st.Name)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReader r( buffer, bytes, report != NULL ? report : &ignored );\n")
	g.pf("    return %sLoadBody( r, value );\n}\n\n", st.Name)
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	ind := "                "
	switch {
	case f.KeyEnum != "":
		// each pair is placed by its VARIANT id, so a slot lands by name
		// however the enum moved; an id this reader cannot name is skipped by
		// its length and counted unknown, and a slot the writer never sent
		// keeps the prefill's default (docs/SPEC-TABLES.md §3.2)
		g.noteRef(f.KeyEnum)
		g.pf("%sif ( !r.has( 4 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint32_t body_len = r.get32();\n", ind)
		g.pf("%sif ( !r.has( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%sint64_t body_end = r.offset + body_len;\n", ind)
		g.pf("%sif ( body_len >= 5 )\n%s{\n", ind, ind)
		g.pf("%s    uint8_t elem_kind = r.get8();\n", ind)
		g.pf("%s    uint32_t count = r.get32();\n", ind)
		g.pf("%s    if ( elem_kind != %d ) { r.report->kind_mismatch++; r.offset = body_end; break; }\n", ind, kind)
		g.pf("%s    TableReader sub( r.buffer + r.offset, body_end - r.offset, r.report );\n", ind)
		g.pf("%s    for ( uint32_t i = 0; i < count; i++ )\n%s    {\n", ind, ind)
		g.pf("%s        if ( !sub.has( 2 ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%s        uint16_t key = sub.get16();\n", ind)
		g.pf("%s        if ( !sub.has( 4 ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%s        uint32_t elem_len = sub.get32();\n", ind)
		g.pf("%s        if ( !sub.has( elem_len ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%s        if ( key == 0 )\n%s        {\n", ind, ind)
		g.pf("%s            // None is the NULL KEY: 0 is the reserved id no declared\n", ind)
		g.pf("%s            // name can fold to, so a body carrying one is DAMAGED, not\n", ind)
		g.pf("%s            // merely foreign. Framing damage stops this body, keeps what\n", ind)
		g.pf("%s            // it decoded, and the parent reads on past the length\n", ind)
		g.pf("%s            // (docs/SPEC-TABLES.md §3.2, §4).\n", ind)
		g.pf("%s            r.report->malformed = true;\n%s            break;\n%s        }\n", ind, ind, ind)
		g.pf("%s        %s slot = %s::None;\n", ind, f.KeyEnum, f.KeyEnum)
		g.pf("%s        if ( !TableEnumValue( key, slot ) )\n%s        {\n", ind, ind)
		g.pf("%s            r.report->unknown++; // a slot this reader cannot name\n", ind)
		g.pf("%s            sub.offset += elem_len;\n%s            continue;\n%s        }\n", ind, ind, ind)
		g.pf("%s        {\n%s            TableReader elem( sub.buffer + sub.offset, elem_len, r.report );\n", ind, ind)
		// the key k lives at STORAGE INDEX k-1 (docs/SPEC-TABLES.md §2.4)
		slot := g.keyedSlots("value.", f) + "[int32_t( slot ) - 1]"
		if kind == tkTable {
			g.pf("%s            %s;\n", ind, g.loadCall(f.Type.Name, "elem", slot))
		} else {
			g.emitTableReadScalarFrom(f, kind, slot, ind+"            ", "elem",
				"r.report->malformed = true; sub.offset += elem_len; continue;")
		}
		g.pf("%s        }\n", ind)
		g.pf("%s        sub.offset += elem_len;\n", ind)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.pf("%sr.offset = body_end; // unread pairs and slack skip via the length\n", ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		t := f.Type.Name
		g.pf("%s// A POINTER FIELD'S PAYLOAD IS A NUMBER (docs/SPEC-TABLES.md §3.1): it is\n", ind)
		g.pf("%s// bounds-checked and resolved through the numbering, never FOLLOWED, so\n", ind)
		g.pf("%s// there is no traversal here and therefore no traversal bound.\n", ind)
		g.pf("%sif ( !r.has( 4 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%sTableNodeResolve( nodes, value.%s, r.get32(), 0x%016xull, r.report ); // *%s\n", ind, f.Name, ir.TableTypeId(t), t)
	case f.Type.Kind == ir.TString:
		g.pf("%sif ( !r.has( 4 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint32_t len = r.get32();\n", ind)
		g.pf("%sif ( !r.has( len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint32_t keep = len;\n", ind)
		g.pf("%sif ( keep > %d ) { keep = %d; r.report->clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%smemcpy( value.%s, r.buffer + r.offset, keep );\n", ind, f.Name)
		g.pf("%svalue.%s[keep] = 0;\n", ind, f.Name)
		g.pf("%svalue.%s_length = (int32_t) keep;\n", ind, f.Name)
		g.pf("%sr.offset += len;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
		g.pf("%sif ( !r.has( 4 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint32_t body_len = r.get32();\n", ind)
		g.pf("%sif ( !r.has( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%sint64_t body_end = r.offset + body_len;\n", ind)
		g.pf("%sif ( body_len >= 5 )\n%s{\n", ind, ind)
		g.pf("%s    uint8_t elem_kind = r.get8();\n", ind)
		g.pf("%s    uint32_t count = r.get32();\n", ind)
		g.pf("%s    if ( elem_kind != %d ) { r.report->kind_mismatch++; r.offset = body_end; break; }\n", ind, kind)
		g.pf("%s    uint32_t keep = count;\n", ind)
		g.pf("%s    if ( keep > %d ) { keep = %d; r.report->clamped++; }\n", ind, bound, bound)
		g.pf("%s    // elements are BOUNDED by the field body: a count the length\n", ind)
		g.pf("%s    // cannot cover keeps the decoded prefix, flags malformed, and\n", ind)
		g.pf("%s    // the parent continues at the next field — following fields'\n", ind)
		g.pf("%s    // bytes are never fabricated into elements\n", ind)
		g.pf("%s    TableReader sub( r.buffer + r.offset, body_end - r.offset, r.report );\n", ind)
		if counted {
			g.pf("%s    uint32_t decoded = 0;\n", ind)
		}
		g.pf("%s    for ( uint32_t i = 0; i < keep; i++ )\n%s    {\n", ind, ind)
		g.emitTableReadElement(f, kind, ind+"        ")
		if counted {
			g.pf("%s        decoded = i + 1;\n", ind)
		}
		g.pf("%s    }\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s    value.%s_length = (int32_t) decoded;\n", ind, f.Name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s    value.%s_count = (int32_t) decoded;\n", ind, f.Name)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr.offset = body_end; // excess elements and slack skip via the length\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sif ( !r.has( 2 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint16_t arm_id = r.get16();\n", ind)
		g.pf("%sif ( arm_id == 0 ) { value.%s.type = %sType::None; break; } // empty: the id is the whole payload\n", ind, f.Name, un.Name)
		g.pf("%sif ( !r.has( 4 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint32_t body_len = r.get32();\n", ind)
		g.pf("%sif ( !r.has( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%s{\n%s    TableReader sub( r.buffer + r.offset, body_len, r.report );\n", ind, ind)
		g.pf("%s    switch ( arm_id ) // the arm's NAME hash (docs/SPEC-TABLES.md §5)\n%s    {\n", ind, ind)
		for _, v := range un.Variants {
			g.pf("%s        case 0x%04x: // %s\n%s            value.%s.type = %sType::%s;\n%s            %s;\n%s            break;\n",
				ind, ir.VariantId(v.Name), v.Name, ind, f.Name, un.Name, ir.GoExportName(v.Name),
				ind, g.loadCall(v.Type, "sub", fmt.Sprintf("value.%s.%s", f.Name, v.Name)), ind)
		}
		g.pf("%s        default:\n", ind)
		g.pf("%s            // an arm this reader cannot name: the value reads EMPTY and\n", ind)
		g.pf("%s            // the body is skipped by its length, never misdecoded. The\n", ind)
		g.pf("%s            // reset is explicit, not the prefill's: a repeated field id\n", ind)
		g.pf("%s            // must not leave an arm decoded by an earlier occurrence\n", ind)
		g.pf("%s            // standing (docs/SPEC-TABLES.md §4).\n", ind)
		g.pf("%s            value.%s.type = %sType::None;\n", ind, f.Name, un.Name)
		g.pf("%s            r.report->unknown++;\n%s            break;\n", ind, ind)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.pf("%sr.offset += body_len;\n", ind)
	case kind == tkTable:
		g.pf("%sif ( !r.has( 4 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint32_t body_len = r.get32();\n", ind)
		g.pf("%sif ( !r.has( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%s{\n%s    TableReader sub( r.buffer + r.offset, body_len, r.report );\n", ind, ind)
		g.pf("%s    %s;\n", ind, g.loadCall(f.Type.Name, "sub", "value."+f.Name))
		g.pf("%s}\n", ind)
		g.pf("%sr.offset += body_len;\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, "value."+f.Name, ind,
			"r", "r.report->malformed = true; return false;")
	}
}

// emitTableReadElement decodes one array element from the field-body
// sub-reader; truncation keeps the decoded prefix and flags malformed
// without stopping the parent decode.
func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	switch kind {
	case tkUnion:
		// one element of an array of unions: the union payload in its place
		// (§3). The element is re-established as None before the arm is read,
		// so a repeated field id leaves no earlier arm standing (§4).
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sif ( !sub.has( 2 ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%s{\n", ind)
		g.pf("%s    uint16_t arm_id = sub.get16();\n", ind)
		g.pf("%s    value.%s[i].type = %sType::None;\n", ind, f.Name, un.Name)
		g.pf("%s    if ( arm_id != 0 ) // 0 is a None element in its place\n%s    {\n", ind, ind)
		g.pf("%s        if ( !sub.has( 4 ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%s        uint32_t arm_len = sub.get32();\n", ind)
		g.pf("%s        if ( !sub.has( arm_len ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%s        TableReader arm( sub.buffer + sub.offset, arm_len, r.report );\n", ind)
		g.pf("%s        switch ( arm_id ) // the arm's NAME hash (docs/SPEC-TABLES.md §5)\n%s        {\n", ind, ind)
		for _, v := range un.Variants {
			g.pf("%s            case 0x%04x: // %s\n%s                value.%s[i].type = %sType::%s;\n%s                %s;\n%s                break;\n",
				ind, ir.VariantId(v.Name), v.Name, ind, f.Name, un.Name, ir.GoExportName(v.Name),
				ind, g.loadCall(v.Type, "arm", fmt.Sprintf("value.%s[i].%s", f.Name, v.Name)), ind)
		}
		g.pf("%s            default: r.report->unknown++; break; // an arm this reader cannot name: the element reads None, the body skips by its length\n", ind)
		g.pf("%s        }\n", ind)
		g.pf("%s        sub.offset += arm_len;\n", ind)
		g.pf("%s    }\n%s}\n", ind, ind)
	case tkNodeIndex:
		// one slot of an array of pointers: a node index, bounds-checked and
		// resolved through the numbering, never followed (§3.1)
		g.pf("%sif ( !sub.has( 4 ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%sTableNodeResolve( nodes, value.%s[i], sub.get32(), 0x%016xull, r.report ); // *%s\n", ind, f.Name, ir.TableTypeId(f.Type.Name), f.Type.Name)
	case tkTable:
		g.pf("%sif ( !sub.has( 4 ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%suint32_t elem_len = sub.get32();\n", ind)
		g.pf("%sif ( !sub.has( elem_len ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%s{\n%s    TableReader elem( sub.buffer + sub.offset, elem_len, r.report );\n", ind, ind)
		g.pf("%s    %s;\n", ind, g.loadCall(f.Type.Name, "elem", fmt.Sprintf("value.%s[i]", f.Name)))
		g.pf("%s}\n", ind)
		g.pf("%ssub.offset += elem_len;\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, fmt.Sprintf("value.%s[i]", f.Name), ind,
			"sub", "r.report->malformed = true; break;")
	}
}

// emitTableReadScalarFrom decodes one fixed-width scalar from the named
// reader into a storage lvalue, with range clamps where the schema declares
// them. onTrunc is the truncation action: a scalar FIELD stops the decode
// (outer framing damage), an array ELEMENT keeps the prefix and breaks.
func (g *tableGen) emitTableReadScalarFrom(f *ir.Field, kind int, lvalue, ind, rdr, onTrunc string) {
	width := tableKindWidth(kind)
	g.pf("%sif ( !%s.has( %d ) ) { %s }\n", ind, rdr, width, onTrunc)
	if enum := enumRef(f); enum != nil {
		// identity is the variant's NAME (docs/SPEC-TABLES.md §5): an id this build
		// cannot name reads as None and counts as unknown, exactly as an
		// unknown FIELD id does — same event, one counter
		g.pf("%s{\n%s    uint16_t variant = %s.get16();\n", ind, ind, rdr)
		g.pf("%s    if ( !TableEnumValue( variant, %s ) )\n%s    {\n", ind, lvalue, ind)
		g.pf("%s        %s = %s::None;\n", ind, lvalue, f.Type.Name)
		g.pf("%s        r.report->unknown++;\n%s    }\n%s}\n", ind, ind, ind)
		return
	}
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
	b.WriteString("+[]() -> const TableUnionInfo * { static const TableUnionArmInfo arms[] = { { 0, NULL },")
	for _, v := range un.Variants {
		table := v.Type + "TableType()"
		if hoisted {
			table = "&" + v.Type + "TableInfo"
		}
		fmt.Fprintf(&b, " { (uint32_t) offsetof( %s, %s ), %s },", un.Name, v.Name, table)
	}
	fmt.Fprintf(&b, " }; static const TableUnionInfo info = { (uint32_t) offsetof( %s, type ), (uint32_t) sizeof( %s::type ), arms }; return &info; }",
		un.Name, un.Name)
	return b.String()
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
		if hoisted {
			g.pf("inline const TableFieldInfo %sTableFields[] = {\n", st.Name)
		} else {
			g.pf("    %s TableFieldInfo fields[] = {\n", qualifier)
		}
		for _, f := range st.Fields {
			id := ir.TableFieldId(f)
			kind := tableScalarKind(f)
			if f.Type.Kind == ir.TBytes {
				kind = tkU8
			}
			isArray := f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes
			if f.Type.Pointer {
				kind = tkNodeIndex // the descriptor states the WIRE (§8.1, §3.1)
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
			elemSize := fmt.Sprintf("(uint32_t) sizeof( %s::%s )", st.Name, f.Name)
			if isArray {
				elemSize = fmt.Sprintf("(uint32_t) sizeof( %s::%s[0] )", st.Name, f.Name)
			}
			if f.KeyEnum != "" {
				// a keyed field's storage is the keyed type; its SLOTS are what
				// a walker steps through, and offset already names the first
				elemSize = fmt.Sprintf("(uint32_t) sizeof( %s )", g.keyedSlots(st.Name+"::", f)+"[0]")
			}

			countOffset := "0xffffffffu"
			if counted {
				companion := f.Name + "_count"
				if f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString {
					companion = f.Name + "_length"
				}
				countOffset = fmt.Sprintf("(uint32_t) offsetof( %s, %s )", st.Name, companion)
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
			table := "NULL"
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
					variantId = fmt.Sprintf("+[]( uint64_t v ) -> uint16_t { uint16_t id = 0; TableEnumId( %s( v ), id ); return id; }", f.Type.Name)
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
					variantId = unionArmLambda(ref, "uint16_t", func(v ir.UnionVariant) string {
						return fmt.Sprintf("0x%04x", ir.VariantId(v.Name))
					}, "0", "0")
					arms = g.unionArmsLambda(ref, hoisted)
					for _, v := range ref.Variants {
						g.noteRef(v.Type)
					}
				}
			}

			presentOffset := "0xffffffffu"
			if f.Type.Optional {
				presentOffset = fmt.Sprintf("(uint32_t) offsetof( %s, %s_present )", st.Name, f.Name)
			}

			// the KEY's vocabulary on an enum-keyed array (docs/SPEC-TABLES.md §8):
			// functions of the KEY, not of the storage index — a walker
			// stepping [0, array_bound) asks about index + 1 and prints slots
			// by name without the schema files
			keyTypeName, keyName, keyId := "NULL", "NULL", "NULL"
			if f.KeyEnum != "" {
				keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
				keyName = fmt.Sprintf("+[]( uint64_t v ) { return EnumName( %s( v ) ); }", f.KeyEnum)
				keyId = fmt.Sprintf("+[]( uint64_t v ) -> uint16_t { uint16_t id = 0; TableEnumId( %s( v ), id ); return id; }", f.KeyEnum)
				g.noteRef(f.KeyEnum)
			}

			pointerColumn := ""
			if g.anyVariable {
				// the three columns a pointered unit's descriptors carry: the
				// flag, and the two thunks the ONE walk cannot spell for itself
				// (docs/SPEC-TABLES.md §16.7)
				resolve, emplace := "NULL", "NULL"
				if f.Type.Pointer {
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
				wide = fmt.Sprintf("&%s_%s_wide", st.Name, f.Name)
			}
			g.pf("%s    { \"%s\", \"%s\", \"%s\", 0x%04x, %d, %v, %s%v, %v, %s, (uint32_t) offsetof( %s, %s ), %s, %s, %s, %s, %s, %s, %s, %d, %s, %s, %s, %s, %s, %s, %s, %s, \"%s\" },\n",
				indent, f.Name, ir.TableFieldJsonKey(f), tableFieldTypeName(f), id, kind, isArray, pointerColumn, counted, f.Type.Optional, bound,
				st.Name, f.Name, elemSize, countOffset, presentOffset, table,
				hasRange, rangeMin, rangeMax, fracBits, wide, enumMax, enumName, variantId,
				keyTypeName, keyName, keyId, arms, guards[f.Name])
		}
		if hoisted {
			g.pf("};\n")
			g.pf("inline const TableTypeInfo %sTableInfo = { \"%s\", (uint32_t) sizeof( %s ), %d, %sTableFields, %s%s };\n",
				st.Name, st.Name, st.Name, len(st.Fields), st.Name, resetLambda(st.Name), g.modeColumn(st))
		} else {
			g.pf("    };\n")
			g.pf("    %s TableTypeInfo info = { \"%s\", (uint32_t) sizeof( %s ), %d, fields, %s%s };\n",
				infoQualifier, st.Name, st.Name, len(st.Fields), resetLambda(st.Name), g.modeColumn(st))
		}
	case hoisted:
		g.pf("inline const TableTypeInfo %sTableInfo = { \"%s\", (uint32_t) sizeof( %s ), 0, NULL, %s%s };\n",
			st.Name, st.Name, st.Name, resetLambda(st.Name), g.modeColumn(st))
	default:
		g.pf("    %s TableTypeInfo info = { \"%s\", (uint32_t) sizeof( %s ), 0, NULL, %s%s };\n",
			infoQualifier, st.Name, st.Name, resetLambda(st.Name), g.modeColumn(st))
	}
	if hoisted {
		g.pf("inline const TableTypeInfo * %sTableType() { return &%sTableInfo; }\n\n", st.Name, st.Name)
		return
	}
	g.pf("    return &info;\n}\n\n")
}
