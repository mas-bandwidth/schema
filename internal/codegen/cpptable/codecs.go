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
	case ir.TInt:
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
	signed := f.Type.Kind == ir.TInt && f.Type.Signed
	lo, hi := tableStorageRange(signed, widthBytes*8)
	return f.IntMin.Cmp(lo) > 0, f.IntMax.Cmp(hi) < 0
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
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			signed := f.Type.Kind == ir.TInt && f.Type.Signed
			width := 4
			if f.Type.Width > 32 {
				width = 8
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
		// a pointer is FOUR BYTES and no address: an arena offset while the
		// builder is mutable, a self-relative delta once packed. That is what
		// keeps a pointer-bearing table relocatable in both forms.
		g.noteRef(f.Type.Name)
		g.pf("    TableRef %s; // *%s — null until assigned\n", f.Name, f.Type.Name)
		return
	}
	typ, selfInit := g.cppFieldType(f.Type)
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
		g.pf("    value.%s.value = 0; // *%s — null\n", f.Name, f.Type.Name)
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
		g.pf("template <typename Ctx>\ninline int64_t %sMeasureBody( const Ctx & ctx, const %s & value, int32_t depth )\n{\n", st.Name, st.Name)
		g.pf("    if ( depth > kTableMaxDepth ) { return -1; } // a data cycle, or a chain past the cap\n")
		if len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 {
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
			g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "value."+f.Name, depthSame))
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
	case f.Type.Pointer:
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee_%s = %sAt( ctx, value.%s ); // *%s\n", t, f.Name, t, f.Name, t)
		g.pf("        if ( pointee_%s != NULL )\n        {\n", f.Name)
		g.pf("            int64_t body_%s = %s;\n", f.Name, g.measureCall(t, "*pointee_"+f.Name, depthDown))
		g.pf("            if ( body_%s < 0 ) { return -1; }\n", f.Name)
		g.pf("            // a pointer's PRESENCE is the payload: it rides even when the\n")
		g.pf("            // pointee is all-default, or null and non-null would be one\n")
		g.pf("            bytes += 3 + 4 + body_%s;\n", f.Name)
		g.pf("        }\n    }\n")
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
		g.pf("            int64_t elem_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, fmt.Sprintf("value.%s[i]", f.Name), depthSame))
		g.pf("            if ( elem_%s < 0 ) { return -1; }\n", f.Name)
		g.pf("            bytes += 4 + elem_%s;\n", f.Name)
		g.pf("        }\n    }\n")
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
		g.pf("            int64_t elem_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, fmt.Sprintf("value.%s[i]", f.Name), depthSame))
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
			g.pf("            int64_t arm_%s = %s;\n", f.Name, g.measureCall(v.Type, fmt.Sprintf("value.%s.%s", f.Name, v.Name), depthSame))
			g.pf("            if ( arm_%s < 0 ) { return -1; }\n", f.Name)
			g.pf("            bytes += 3 + 2 + 4 + arm_%s; // the u16 ARM ID, then the arm length-prefixed\n            break;\n        }\n", f.Name)
		}
		g.pf("        default: return -1; // invalid tag — the write side refuses it too\n")
		g.pf("    }\n")
	case kind == tkTable:
		g.pf("    {\n")
		g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "value."+f.Name, depthSame))
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
		g.pf("%sint64_t elem_bytes = %s;\n", ind, g.measureCall(f.Type.Name, expr, depthSame))
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

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("template <typename Ctx>\ninline bool %sSaveBody( const Ctx & ctx, TableWriter & w, const %s & value, int32_t depth )\n{\n", st.Name, st.Name)
		g.pf("    if ( depth > kTableMaxDepth ) { return false; } // a data cycle, or a chain past the cap\n")
		if len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 {
			g.pf("    (void) ctx;\n")
		}
	} else {
		g.pf("inline bool %sSaveBody( TableWriter & w, const %s & value )\n{\n", st.Name, st.Name)
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
			g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "value."+f.Name, depthSame))
			g.pf("        if ( body_%s < 0 ) return false; // storage invariant, refused as measure refuses it\n", f.Name)
			g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkTable, f.Name)
			g.pf("        w.put32( uint32_t( body_%s ) );\n", f.Name)
			g.pf("        if ( !%s ) return false;\n", g.saveCall(f.Type.Name, "value."+f.Name, depthSame))
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
			g.pf("                if ( !%s ) return false;\n", g.saveCall(f.Type.Name, g.keyedSlots("value.", f)+"[i]", depthSame))
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
	case f.Type.Pointer:
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee_%s = %sAt( ctx, value.%s ); // *%s\n", t, f.Name, t, f.Name, t)
		g.pf("        if ( pointee_%s != NULL )\n        {\n", f.Name)
		g.pf("            int64_t body_%s = %s;\n", f.Name, g.measureCall(t, "*pointee_"+f.Name, depthDown))
		g.pf("            if ( body_%s < 0 ) { return false; }\n", f.Name)
		g.pf("            w.put16( 0x%04x ); w.put8( %d ); // %s — the pointee rides as a nested body\n", id, tkTable, f.Name)
		g.pf("            w.put32( uint32_t( body_%s ) );\n", f.Name)
		g.pf("            if ( !%s ) { return false; }\n", g.saveCall(t, "*pointee_"+f.Name, depthDown))
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
				un.Name, ir.GoExportName(v.Name), g.saveCall(v.Type, fmt.Sprintf("value.%s.%s", f.Name, v.Name), depthSame))
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
		g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "value."+f.Name, depthSame))
		g.pf("        if ( body_%s < 0 ) return false; // storage invariant, refused as measure refuses it\n", f.Name)
		g.pf("        if ( body_%s > 2 ) // all-default nested elides\n        {\n", f.Name)
		g.pf("            w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkTable, f.Name)
		g.pf("            w.put32( uint32_t( body_%s ) );\n", f.Name)
		g.pf("            if ( !%s ) return false;\n", g.saveCall(f.Type.Name, "value."+f.Name, depthSame))
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
		g.pf("%s    if ( !%s ) return false;\n", ind, g.saveCall(f.Type.Name, expr, depthSame))
		g.pf("%s    w.patch32( elem_len_at, uint32_t( w.offset - elem_len_at - 4 ) );\n%s}\n", ind, ind)
	default:
		width := tableKindWidth(kind)
		cast := fmt.Sprintf("uint%d_t", width*8)
		g.pf("%sw.%s( %s( %s ) );\n", ind, tablePut(width), cast, expr)
	}
}

// ---- read ----

func (g *tableGen) emitTableRead(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("template <typename Sink>\ninline bool %sLoadBody( TableReader & r, Sink & sink, %s & value, int32_t depth )\n{\n", st.Name, st.Name)
		if len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 {
			g.pf("    (void) sink; (void) depth;\n")
		}
	} else {
		g.pf("inline bool %sLoadBody( TableReader & r, %s & value )\n{\n", st.Name, st.Name)
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
				// a pointer rides as a nested table body — framing identical to
				// a by-value nesting, which is why a field can change between
				// the two without moving a byte (docs/SPEC-TABLES.md §3)
				kind, wireKind = tkTable, tkTable
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
			g.pf("%s            %s;\n", ind, g.loadCall(f.Type.Name, "elem", slot, depthSame))
		} else {
			g.emitTableReadScalarFrom(f, kind, slot, ind+"            ", "elem",
				"r.report->malformed = true; sub.offset += elem_len; continue;")
		}
		g.pf("%s        }\n", ind)
		g.pf("%s        sub.offset += elem_len;\n", ind)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.pf("%sr.offset = body_end; // unread pairs and slack skip via the length\n", ind)
	case f.Type.Pointer:
		t := f.Type.Name
		g.pf("%sif ( !r.has( 4 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint32_t body_len = r.get32();\n", ind)
		g.pf("%sif ( !r.has( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%sif ( depth >= kTableMaxDepth )\n%s{\n", ind, ind)
		g.pf("%s    // past the nesting cap: the subtree is refused, the pointer stays\n", ind)
		g.pf("%s    // null, and the parent reads on (docs/SPEC-TABLES.md §4)\n", ind)
		g.pf("%s    r.report->malformed = true;\n", ind)
		g.pf("%s    r.offset += body_len;\n%s    break;\n%s}\n", ind, ind, ind)
		g.pf("%s{\n%s    %s * pointee = %sEmplace( sink, value.%s );\n", ind, ind, t, t, f.Name)
		g.pf("%s    if ( pointee == NULL )\n%s    {\n", ind, ind)
		g.pf("%s        r.report->malformed = true; // the caller's region was short\n", ind)
		g.pf("%s        r.offset += body_len;\n%s        break;\n%s    }\n", ind, ind, ind)
		g.pf("%s    TableReader sub( r.buffer + r.offset, body_len, r.report );\n", ind)
		g.pf("%s    %s;\n", ind, g.loadCall(t, "sub", "*pointee", depthDown))
		g.pf("%s}\n", ind)
		g.pf("%sr.offset += body_len;\n", ind)
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
				ind, g.loadCall(v.Type, "sub", fmt.Sprintf("value.%s.%s", f.Name, v.Name), depthSame), ind)
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
		g.pf("%s    %s;\n", ind, g.loadCall(f.Type.Name, "sub", "value."+f.Name, depthSame))
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
	case tkTable:
		g.pf("%sif ( !sub.has( 4 ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%suint32_t elem_len = sub.get32();\n", ind)
		g.pf("%sif ( !sub.has( elem_len ) ) { r.report->malformed = true; break; }\n", ind)
		g.pf("%s{\n%s    TableReader elem( sub.buffer + sub.offset, elem_len, r.report );\n", ind, ind)
		g.pf("%s    %s;\n", ind, g.loadCall(f.Type.Name, "elem", fmt.Sprintf("value.%s[i]", f.Name), depthSame))
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
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		storage := fmt.Sprintf("uint%d_t", width*8)
		if signed {
			storage = fmt.Sprintf("int%d_t", width*8)
		}
		g.pf("%s%s decoded_v = %s( %s.%s( ) );\n", ind, storage, storage, rdr, tableGet(width))
		if f.HasIntRange {
			low, high := tableClampEnds(f, width)
			if low {
				lo := tableIntLit(f.IntMin, signed, width)
				g.pf("%sif ( decoded_v < %s ) { decoded_v = %s; r.report->clamped++; }\n", ind, lo, lo)
			}
			if high {
				hi := tableIntLit(f.IntMax, signed, width)
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
func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		prefix := "int"
		if !f.Type.Signed {
			prefix = "uint"
		}
		return fmt.Sprintf("%s%d", prefix, f.Type.Width)
	case ir.TBits:
		return fmt.Sprintf("bits(%d)", f.Type.Width)
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}

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
				kind = tkTable
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
			if f.Type.Pointer {
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
				if f.Type.Kind == ir.TNamed && f.Array == ir.ArrayNone {
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
				pointerColumn = fmt.Sprintf("%v, ", f.Type.Pointer)
			}
			g.pf("%s    { \"%s\", \"%s\", \"%s\", 0x%04x, %d, %v, %s%v, %v, %s, (uint32_t) offsetof( %s, %s ), %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, \"%s\" },\n",
				indent, f.Name, ir.TableFieldJsonKey(f), tableFieldTypeName(f), id, kind, isArray, pointerColumn, counted, f.Type.Optional, bound,
				st.Name, f.Name, elemSize, countOffset, presentOffset, table,
				hasRange, rangeMin, rangeMax, enumMax, enumName, variantId,
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
