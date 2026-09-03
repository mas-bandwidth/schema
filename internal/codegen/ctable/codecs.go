// TABLE-wire storage, codec and descriptor emission in C
// (docs/SPEC-TABLES.md). Readers prefill declared defaults then overlay, skip
// unknown ids, skip kind mismatches, clamp out-of-range values, and count
// every event.
package ctable

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cFieldType maps a field type to its C storage spelling, mirroring the packet
// emitter's conventions so closure types from <Base>.h and table structs from
// this header read as one family.
func (g *tableGen) cFieldType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TInt:
		if t.Signed {
			return fmt.Sprintf("int%d_t", t.Width)
		}
		return fmt.Sprintf("uint%d_t", t.Width)
	case ir.TBits:
		if t.Width <= 32 {
			return "uint32_t"
		}
		return "uint64_t"
	case ir.TBool:
		// C has no bool in C99 without <stdbool.h>, and the block form's
		// layout contract pins a bool in a row to ONE byte (§19.3). uint8_t
		// is that byte, spelled the way every other width in this family is.
		return "uint8_t"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TNamed:
		g.noteRef(t.Name)
		return t.Name
	}
	return "/* ? */"
}

// cSelfInit reports a type whose storage is a generated struct or union — the
// members a Reset descends into rather than assigns.
func cSelfInit(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	switch t.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
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

// enumConst renders an enum variant's C spelling, the flat #define family the
// packet emitter puts in <Base>.h (SPEC §6.1's C column).
func enumConst(enum, variant string) string {
	return ir.RustConstName(enum) + "_" + ir.RustConstName(variant)
}

func enumNoneConst(enum string) string { return ir.RustConstName(enum) + "_NONE" }

func enumMaxConst(enum string) string { return ir.RustConstName(enum) + "_MAX" }

// fieldDefaultExpr renders the C expression a field's default compares
// against on the write side (elision) — identical literals to the prefill.
func (g *tableGen) fieldDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "1"
		}
		return "0"
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
				return enumConst(f.Type.Name, f.DefVariant)
			}
			return enumNoneConst(f.Type.Name)
		case *ir.Flags:
			return "0"
		}
	}
	return "0"
}

// ---- storage (table declarations only; closure types come from <Base>.h) ----

func (g *tableGen) emitTableStruct(st *ir.Struct) {
	g.pf("/* table %s — TABLE-wire storage: relocatable, bounded. C has no member\n", st.Name)
	g.pf("   initializers, so the declared defaults live in %s and nowhere\n", g.api(st.Name, "reset"))
	g.pf("   else — one definition of what a default is (docs/SPEC-TABLES.md) */\n")
	g.pf("typedef struct %s {\n", st.Name)
	prevGuard := ""
	for _, f := range st.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				g.pf("\n    /* %s — guarded fields stay off the wire when the guard says so;\n", f.Guard)
				g.pf("       a read's prefilled defaults stand in for the untaken side */\n")
			} else {
				g.pf("\n")
			}
			prevGuard = f.Guard
		}
		g.emitTableStorageField(f)
	}
	if len(st.Fields) == 0 {
		g.pf("    char unused_; /* C has no empty struct; carries no wire bits */\n")
	}
	g.pf("} %s;\n\n", st.Name)
}

func (g *tableGen) emitTableStorageField(f *ir.Field) {
	if f.Type.Pointer {
		// a pointer is EIGHT BYTES and no address: an arena offset while the
		// builder is mutable, a self-relative delta once packed. That is what
		// keeps a pointer-bearing table relocatable in both forms.
		g.noteRef(f.Type.Name)
		g.pf("    TableRef %s; /* *%s — null until assigned */\n", f.Name, f.Type.Name)
		return
	}
	typ := g.cFieldType(f.Type)
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("    char %s[%d + 1]; /* string(%d): N + 1 for the terminator the wire does not carry */\n", f.Name, f.Type.Size, f.Type.Size)
		g.pf("    int32_t %s_length;\n", f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    uint8_t %s[%d]; /* bytes(%d): fixed buffer, used length beside it */\n", f.Name, f.Type.Size, f.Type.Size)
		g.pf("    int32_t %s_length;\n", f.Name)
	case f.KeyEnum != "":
		// ONE SLOT PER NAMED VARIANT, the key k at index k-1: nothing is
		// stored for None, and SCHEMA_TABLE_KEYED_AT is the only place the shift
		// appears. Every named slot exists, so there is no count companion,
		// and the extent comes from the key enum's own _MAX — nothing outside
		// the array names its size (docs/SPEC-TABLES.md §2.4).
		g.noteRef(f.KeyEnum)
		g.pf("    %s %s[%s]; /* [%s]: one slot per named variant, the key k at index k-1 */\n",
			typ, f.Name, enumMaxConst(f.KeyEnum), f.KeyEnum)
	case f.Array == ir.ArrayFixed:
		g.pf("    %s %s[%d];\n", typ, f.Name, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		g.pf("    %s %s[%d]; /* used count beside it; count in [0, %d] */\n", typ, f.Name, f.ArrayBound, f.ArrayBound)
		g.pf("    int32_t %s_count;\n", f.Name)
	default:
		g.pf("    %s %s;\n", typ, f.Name)
	}
	if f.Type.Optional {
		// `?T` — the value plus its presence flag, and nothing else: the
		// holder stays a fixed-size struct (docs/SPEC-TABLES.md §2.3). PRESENCE,
		// not content, decides whether the field rides.
		g.pf("    uint8_t %s_present; /* ?%s: absent until set */\n", f.Name, tableFieldTypeName(f))
	}
}

// ---- prefill: the declared defaults, one member at a time ----
//
// Every site that needs a closure member's declared defaults calls its
// `<T>Reset` — the read path before it overlays, and the descriptor's reset
// column. C has no member initializers, so this is not merely the cheap way
// to establish them: it is the ONE definition of what they are.
func (g *tableGen) emitTableResetDeclarations(members []*ir.Struct) {
	g.pf("/* ---- prefill: the declared defaults, in place (docs/SPEC-TABLES.md) ---- */\n\n")
	for _, st := range members {
		g.pf("static SCHEMA_UNUSED void %s( %s * value );\n", g.api(st.Name, "reset"), st.Name)
		g.pf("static SCHEMA_UNUSED void %s( void * storage );\n", g.sym(st.Name, "reset_raw"))
	}
	g.pf("\n")
}

func (g *tableGen) emitTableReset(st *ir.Struct) {
	g.pf("static SCHEMA_UNUSED void %s( %s * value )\n{\n", g.api(st.Name, "reset"), st.Name)
	if len(st.Fields) == 0 {
		g.pf("    memset( value, 0, sizeof( *value ) );\n")
	}
	for _, f := range st.Fields {
		g.emitTableResetField(f)
	}
	g.pf("}\n\n")
	// the descriptor's reset column, which is typed void * and cannot be the
	// typed entry above: a function pointer conversion is not a cast a caller
	// may then CALL through. Two spellings, one body.
	g.pf("static SCHEMA_UNUSED void %s( void * storage ) { %s( (%s *) storage ); }\n\n", g.sym(st.Name, "reset_raw"), g.api(st.Name, "reset"), st.Name)
}

func (g *tableGen) emitTableResetField(f *ir.Field) {
	if f.Type.Pointer {
		g.pf("    value->%s.value = 0; /* *%s — null */\n", f.Name, f.Type.Name)
		return
	}
	typ := g.cFieldType(f.Type)
	selfInit := cSelfInit(f.Type)
	switch {
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("    memset( value->%s, 0, sizeof( value->%s ) );\n", f.Name, f.Name)
		g.pf("    value->%s_length = 0;\n", f.Name)
	case f.KeyEnum != "":
		g.emitTableResetArray("value->"+f.Name, enumMaxConst(f.KeyEnum), typ, selfInit, f)
	case f.Array == ir.ArrayFixed:
		g.emitTableResetArray("value->"+f.Name, strconv.FormatInt(f.ArrayBound, 10), typ, selfInit, f)
	case f.Array == ir.ArrayCounted:
		g.emitTableResetArray("value->"+f.Name, strconv.FormatInt(f.ArrayBound, 10), typ, selfInit, f)
		g.pf("    value->%s_count = 0;\n", f.Name)
	case selfInit:
		g.emitTableResetOne("value->"+f.Name, typ, f)
	default:
		g.pf("    value->%s = %s;\n", f.Name, g.fieldDefaultExpr(f))
	}
	if f.Type.Optional {
		g.pf("    value->%s_present = 0;\n", f.Name)
	}
}

// emitTableResetArray gives ONE element the declared defaults and copies it
// across the rest. A scalar element's declared default is the type's zero on
// this wire, so a memset says it exactly and in one instruction stream.
func (g *tableGen) emitTableResetArray(expr, bound, typ string, selfInit bool, f *ir.Field) {
	if !selfInit {
		g.pf("    memset( %s, 0, sizeof( %s ) );\n", expr, expr)
		return
	}
	g.emitTableResetOne(expr+"[0]", typ, f)
	g.pf("    { int32_t i; for ( i = 1; i < (int32_t) ( %s ); i++ ) { %s[i] = %s[0]; } }\n", bound, expr, expr)
}

// emitTableResetOne gives one struct or union member its declared defaults:
// through the member type's own Reset where this header emits one, and
// otherwise — a union, which is not a struct — through a zeroing memset, which
// is what an unset union means (tag None, no arm).
func (g *tableGen) emitTableResetOne(expr, typ string, f *ir.Field) {
	if _, ok := f.Type.Ref.(*ir.Struct); ok {
		g.pf("    %s( &%s );\n", g.api(typ, "reset"), expr)
		return
	}
	g.pf("    memset( &%s, 0, sizeof( %s ) );\n", expr, expr)
}

// ---- enum identity on the table wire (docs/SPEC-TABLES.md §5) ----
//
// C++ resolves a value to its wire id through an overload set, one function
// per enum. C has no overloading, and a named function per enum would claim a
// name per enum for every schema in the unit to avoid (§11). The switch is
// emitted AT THE USE SITE instead: the same switch, resolving no call.

// emitEnumIdSwitch resolves a value to its table-wire id, running onBad when
// no variant names it. The id local is DECLARED here.
func (g *tableGen) emitEnumIdSwitch(e *ir.Enum, valueExpr, idVar, ind, onBad string) {
	g.pf("%suint16_t %s = 0;\n", ind, idVar)
	g.pf("%sswitch ( %s ) /* %s rides as the u16 hash of its VARIANT NAME (§5) */\n%s{\n", ind, valueExpr, e.Name, ind)
	g.pf("%s    case %s: %s = 0; break;\n", ind, enumNoneConst(e.Name), idVar)
	for _, v := range e.Variants {
		g.pf("%s    case %s: %s = 0x%04x; break;\n", ind, enumConst(e.Name, v), idVar, ir.VariantId(v))
	}
	g.pf("%s    default: %s /* no variant names this value: no wire identity */\n", ind, onBad)
	g.pf("%s}\n", ind)
}

// emitEnumValueSwitch places a wire id back into typed storage. An id this
// build cannot name reads as None and counts as unknown, exactly as an unknown
// FIELD id does — same event, one counter (§5).
func (g *tableGen) emitEnumValueSwitch(e *ir.Enum, idExpr, lvalue, ind string) {
	g.pf("%sswitch ( %s )\n%s{\n", ind, idExpr, ind)
	g.pf("%s    case 0: %s = %s; break;\n", ind, lvalue, enumNoneConst(e.Name))
	for _, v := range e.Variants {
		g.pf("%s    case 0x%04x: %s = %s; break;\n", ind, ir.VariantId(v), lvalue, enumConst(e.Name, v))
	}
	g.pf("%s    default: %s = %s; r->report->unknown++; break; /* an id this build cannot name */\n",
		ind, lvalue, enumNoneConst(e.Name))
	g.pf("%s}\n", ind)
}

// enumRef returns the enum a field's values come from, or nil.
func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// ---- guards ----

// tableGuardExprs composes each guarded field's branch condition from the
// wire tree ("value->a && !value->b" for nesting) so the writer and measurer
// keep untaken-branch fields off the wire — TLV's native optionality carries
// the branch, and the reader's prefilled defaults stand in for the untaken
// side.
func tableGuardExprs(st *ir.Struct) map[string]string {
	return guardWalk(st, "value->")
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
// EXACT encoded size of a value, no writing — the parallel-generation lever.
// Every nested table on the wire is length-prefixed, so a caller can measure
// subtables in parallel, prefix-sum offsets, and scatter-write disjoint ranges
// from N workers. Mirrors the write side's elision decisions branch for
// branch: for any value, Save writes exactly this many bytes into a buffer of
// exactly this size. A value violating its storage invariants (a count or
// length outside its bound, an out-of-range union tag) measures as -1, exactly
// as the write side refuses it.
func (g *tableGen) emitTableMeasure(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("static SCHEMA_UNUSED int64_t %s( const TableCtx * ctx, const %s * value, int32_t depth )\n{\n", g.api(st.Name, "measure_body"), st.Name)
		g.pf("    int64_t bytes = 2; /* terminator */\n")
		g.pf("    if ( depth > kTableMaxDepth ) { return -1; } /* a data cycle, or a chain past the cap */\n")
		if len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 {
			g.pf("    (void) ctx;\n")
		}
	} else {
		g.pf("static SCHEMA_UNUSED int64_t %s( const %s * value )\n{\n", g.api(st.Name, "measure"), st.Name)
		g.pf("    int64_t bytes = 2; /* terminator */\n")
	}
	if len(st.Fields) == 0 {
		g.pf("    (void) value; /* empty type: presence is the payload */\n")
	}
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
		g.pf("    if ( value->%s_present ) /* ?%s: presence decides, not content */\n    {\n", f.Name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "&value->"+f.Name, depthSame))
			g.pf("        if ( body_%s < 0 ) { return -1; }\n", f.Name)
			g.pf("        bytes += 3 + 4 + body_%s; /* %s */\n", f.Name, f.Name)
		case enumRef(f) != nil:
			g.emitEnumIdSwitch(enumRef(f), "value->"+f.Name, "id_"+f.Name, "        ", "return -1;")
			g.pf("        (void) id_%s;\n", f.Name)
			g.pf("        bytes += 3 + 2; /* %s: the variant's name hash */\n", f.Name)
		default:
			g.pf("        bytes += 3 + %d; /* %s */\n", width, f.Name)
		}
		g.pf("    }\n")
	case f.KeyEnum != "":
		// enum-keyed: the body carries (variant id, length-prefixed element)
		// pairs, so a slot lands by NAME however the enum moved. A slot at its
		// default elides like any default, and an empty array elides whole.
		g.pf("    {\n")
		g.pf("        int64_t pairs_%s = 0, body_%s = 0;\n", f.Name, f.Name)
		g.pf("        int32_t i;\n")
		g.pf("        for ( i = 0; i < %s; i++ ) /* [%s]: every stored slot is a named variant's */\n        {\n", enumMaxConst(f.KeyEnum), f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "            ", "return -1;")
		if kind == tkTable {
			g.pf("            pairs_%s++; body_%s += 2 + 4 + elem_bytes; /* key, length, body */\n", f.Name, f.Name)
		} else {
			g.pf("            pairs_%s++; body_%s += 2 + 4 + %d; /* key, length, element */\n", f.Name, f.Name, width)
		}
		g.pf("        }\n")
		g.pf("        if ( pairs_%s > 0 ) { bytes += 3 + 4 + 5 + body_%s; } /* %s */\n", f.Name, f.Name, f.Name)
		g.pf("    }\n")
	case f.Type.Pointer:
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee_%s = %s( ctx, &value->%s ); /* *%s */\n", t, f.Name, g.api(t, "at"), f.Name, t)
		g.pf("        if ( pointee_%s != NULL )\n        {\n", f.Name)
		g.pf("            int64_t body_%s = %s;\n", f.Name, g.measureCall(t, "pointee_"+f.Name, depthDown))
		g.pf("            if ( body_%s < 0 ) { return -1; }\n", f.Name)
		g.pf("            /* a pointer's PRESENCE is the payload: it rides even when the\n")
		g.pf("               pointee is all-default, or null and non-null would be one */\n")
		g.pf("            bytes += 3 + 4 + body_%s;\n", f.Name)
		g.pf("        }\n    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if ( value->%s_length < 0 || value->%s_length > %d ) { return -1; } /* storage invariant */\n", f.Name, f.Name, f.Type.Size)
		g.pf("    if ( value->%s_length > 0 ) { bytes += 3 + 4 + value->%s_length; } /* %s */\n", f.Name, f.Name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if ( value->%s_length < 0 || value->%s_length > %d ) { return -1; } /* storage invariant */\n", f.Name, f.Name, f.Type.Size)
		g.pf("    if ( value->%s_length > 0 ) { bytes += 3 + 4 + 5 + value->%s_length; } /* %s */\n", f.Name, f.Name, f.Name)
	case f.Array == ir.ArrayCounted && kind == tkTable:
		g.pf("    if ( value->%s_count < 0 || value->%s_count > %d ) { return -1; } /* storage invariant */\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value->%s_count > 0 )\n    {\n", f.Name)
		g.pf("        int32_t i;\n")
		g.pf("        bytes += 3 + 4 + 5; /* %s */\n", f.Name)
		g.pf("        for ( i = 0; i < value->%s_count; i++ )\n        {\n", f.Name)
		g.pf("            int64_t elem_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, fmt.Sprintf("&value->%s[i]", f.Name), depthSame))
		g.pf("            if ( elem_%s < 0 ) { return -1; }\n", f.Name)
		g.pf("            bytes += 4 + elem_%s;\n", f.Name)
		g.pf("        }\n    }\n")
	case f.Array == ir.ArrayCounted:
		g.pf("    if ( value->%s_count < 0 || value->%s_count > %d ) { return -1; } /* storage invariant */\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value->%s_count > 0 )\n    {\n", f.Name)
		g.emitEnumElementCheck(f, fmt.Sprintf("value->%s[i]", f.Name), fmt.Sprintf("value->%s_count", f.Name), "        ", "return -1;")
		g.pf("        bytes += 3 + 4 + 5 + (int64_t) value->%s_count * %d; /* %s */\n", f.Name, width, f.Name)
		g.pf("    }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		g.pf("    {\n")
		g.pf("        int32_t i;\n")
		g.pf("        bytes += 3 + 4 + 5; /* %s (fixed [%d]) */\n", f.Name, f.ArrayBound)
		g.pf("        for ( i = 0; i < %d; i++ )\n        {\n", f.ArrayBound)
		g.pf("            int64_t elem_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, fmt.Sprintf("&value->%s[i]", f.Name), depthSame))
		g.pf("            if ( elem_%s < 0 ) { return -1; }\n", f.Name)
		g.pf("            bytes += 4 + elem_%s;\n", f.Name)
		g.pf("        }\n    }\n")
	case f.Array == ir.ArrayFixed:
		g.pf("    {\n")
		g.pf("        int all_default_%s = 1;\n", f.Name)
		g.pf("        int32_t i;\n")
		g.pf("        for ( i = 0; i < %d; i++ ) { if ( value->%s[i] != %s ) { all_default_%s = 0; break; } }\n",
			f.ArrayBound, f.Name, g.fieldDefaultExpr(f), f.Name)
		g.pf("        if ( !all_default_%s )\n        {\n", f.Name)
		g.emitEnumElementCheck(f, fmt.Sprintf("value->%s[i]", f.Name), fmt.Sprintf("%d", f.ArrayBound), "            ", "return -1;")
		g.pf("            bytes += 3 + 4 + 5 + %d; /* %s */\n", f.ArrayBound*int64(width), f.Name)
		g.pf("        }\n    }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("    switch ( value->%s.type ) /* %s */\n    {\n", f.Name, f.Name)
		g.pf("        case %s: break; /* None elides — TLV absence is the None */\n", enumConst(un.Name+"Type", "None"))
		for _, v := range un.Variants {
			g.pf("        case %s:\n        {\n", enumConst(un.Name+"Type", v.Name))
			g.pf("            int64_t arm_%s = %s;\n", f.Name, g.measureCall(v.Type, fmt.Sprintf("&value->%s.as.%s", f.Name, v.Name), depthSame))
			g.pf("            if ( arm_%s < 0 ) { return -1; }\n", f.Name)
			g.pf("            bytes += 3 + 2 + 4 + arm_%s; /* the u16 ARM ID, then the arm length-prefixed */\n            break;\n        }\n", f.Name)
		}
		g.pf("        default: return -1; /* invalid tag — the write side refuses it too */\n")
		g.pf("    }\n")
	case kind == tkTable:
		g.pf("    {\n")
		g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "&value->"+f.Name, depthSame))
		g.pf("        if ( body_%s < 0 ) { return -1; }\n", f.Name)
		g.pf("        if ( body_%s > 2 ) { bytes += 3 + 4 + body_%s; } /* %s: all-default nested elides */\n", f.Name, f.Name, f.Name)
		g.pf("    }\n")
	case enumRef(f) != nil:
		g.pf("    if ( value->%s != %s )\n    {\n", f.Name, g.fieldDefaultExpr(f))
		g.emitEnumIdSwitch(enumRef(f), "value->"+f.Name, "id_"+f.Name, "        ", "return -1;")
		g.pf("        (void) id_%s;\n", f.Name)
		g.pf("        bytes += 3 + 2; /* %s: the variant's name hash */\n    }\n", f.Name)
	default:
		g.pf("    if ( value->%s != %s ) { bytes += 3 + %d; } /* %s */\n", f.Name, g.fieldDefaultExpr(f), width, f.Name)
	}
}

// emitKeyedSlotRides emits the head of an enum-keyed array's per-slot loop
// (docs/SPEC-TABLES.md §2.4, §3.2). It elides a slot holding its default, refuses a
// slot whose value or whose KEY no variant names — a value with no wire
// identity is refused rather than silently renamed, the enum rule applied to
// slots — and leaves `key_id` holding the slot's wire id. For a table element
// `elem_bytes` holds the measured body, so measure and save decide elision on
// the same number; for an enum element `element_id` holds the resolved id, and
// the save path writes THAT rather than resolving the same value twice.
func (g *tableGen) emitKeyedSlotRides(f *ir.Field, kind int, ind, onBad string) {
	expr := "value->" + f.Name + "[i]"
	switch {
	case kind == tkTable:
		g.pf("%sint64_t elem_bytes = %s;\n", ind, g.measureCall(f.Type.Name, "&"+expr, depthSame))
		g.pf("%sif ( elem_bytes < 0 ) { %s }\n", ind, onBad)
		g.pf("%sif ( elem_bytes <= 2 ) { continue; } /* an all-default slot elides */\n", ind)
	case enumRef(f) != nil:
		g.pf("%sif ( %s == %s ) { continue; } /* a default slot elides */\n", ind, expr, g.fieldDefaultExpr(f))
		g.emitEnumIdSwitch(enumRef(f), expr, "element_id", ind, onBad)
		// measure counts bytes and never writes the id; the save pass writes
		// THIS one rather than resolving the same value a second time
		g.pf("%s(void) element_id;\n", ind)
	default:
		g.pf("%sif ( %s == %s ) { continue; } /* a default slot elides */\n", ind, expr, g.fieldDefaultExpr(f))
	}
	// i is the STORAGE index; the key it holds is i + 1
	g.emitEnumIdSwitch(f.KeyEnumRef, fmt.Sprintf("(%s) ( i + 1 )", f.KeyEnum), "key_id", ind, onBad)
	// the measure pass counts pairs and never writes the key; reading it here
	// keeps one loop head for both passes without a set-but-unused diagnostic
	g.pf("%s(void) key_id;\n", ind)
}

// emitEnumElementCheck validates an enum ARRAY's elements before they ride: a
// value no variant names has no wire identity, so the value is refused rather
// than silently written as None (the union tag's rule, applied to enums).
func (g *tableGen) emitEnumElementCheck(f *ir.Field, expr, count, ind, onBad string) {
	e := enumRef(f)
	if e == nil {
		return
	}
	// `element_i`, not `i`: this loop is emitted INSIDE another that already
	// has one, and -Wshadow rides on every tables leg (docs/SPEC-TABLES.md
	// §19.5's flags) — a warning here is a build failure in a consumer's tree.
	g.pf("%s{\n%s    int32_t element_i;\n", ind, ind)
	g.pf("%s    for ( element_i = 0; element_i < %s; element_i++ ) /* %s: every element must be nameable */\n%s    {\n", ind, count, f.Name, ind)
	g.emitEnumIdSwitch(e, strings.ReplaceAll(expr, "[i]", "[element_i]"), "element_id", ind+"        ", onBad)
	g.pf("%s        (void) element_id;\n", ind)
	g.pf("%s    }\n%s}\n", ind, ind)
}

// ---- write / save ----

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("static SCHEMA_UNUSED int %s( const TableCtx * ctx, TableWriter * w, const %s * value, int32_t depth )\n{\n", g.api(st.Name, "save_body"), st.Name)
		g.pf("    if ( depth > kTableMaxDepth ) { return 0; } /* a data cycle, or a chain past the cap */\n")
		if len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 {
			g.pf("    (void) ctx;\n")
		}
	} else {
		g.pf("static SCHEMA_UNUSED %s int %s( TableWriter * w, const %s * value )\n{\n", tableInlineMacro(g.unit.Package), g.api(st.Name, "save_body"), st.Name)
	}
	if len(st.Fields) == 0 {
		g.pf("    (void) value; /* empty type: presence is the payload */\n")
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
	g.pf("    table_writer_put16( w, 0 ); /* terminator */\n")
	g.pf("    return !w->overflow;\n}\n\n")
}

// emitTableSave emits the buffer-level entry of the measure/save pair:
// <X>Save writes into a caller-provided buffer and returns the bytes written —
// exactly <X>Measure's answer — or -1 when the buffer is too small. No
// allocation anywhere: the caller owns the buffer.
func (g *tableGen) emitTableSave(st *ir.Struct) {
	if g.isVar(st.Name) {
		return // a variable-length table's Save takes a builder or a region root
	}
	g.pf("static SCHEMA_UNUSED int64_t %s( const %s * value, uint8_t * buffer, int64_t capacity )\n{\n", g.api(st.Name, "save"), st.Name)
	g.pf("    TableWriter w = table_writer_make( buffer, capacity );\n")
	g.pf("    if ( !%s( &w, value ) ) { return -1; }\n", g.api(st.Name, "save_body"))
	g.pf("    return w.offset; /* == %s( value ) */\n}\n\n", g.api(st.Name, "measure"))
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
		g.pf("    if ( value->%s_present ) /* ?%s */\n    {\n", f.Name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "&value->"+f.Name, depthSame))
			g.pf("        if ( body_%s < 0 ) { return 0; } /* storage invariant, refused as measure refuses it */\n", f.Name)
			g.pf("        table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s */\n", id, tkTable, f.Name)
			g.pf("        table_writer_put32( w, (uint32_t) body_%s );\n", f.Name)
			g.pf("        if ( !%s ) { return 0; }\n", g.saveCall(f.Type.Name, "&value->"+f.Name, depthSame))
		case enumRef(f) != nil:
			g.emitEnumIdSwitch(enumRef(f), "value->"+f.Name, "id_"+f.Name, "        ", "return 0;")
			g.pf("        table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s */\n", id, kind, f.Name)
			g.pf("        table_writer_put16( w, id_%s );\n", f.Name)
		default:
			g.pf("        table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s */\n", id, kind, f.Name)
			g.emitTableWriteElement(f, kind, "value->"+f.Name, "        ")
		}
		g.pf("    }\n")
	case f.KeyEnum != "":
		// the slots ride KEYED: (variant id, length-prefixed element) pairs,
		// counted like any array's elements. Two passes so the count is known
		// before the header rides, and so measure and save agree byte for byte.
		g.pf("    {\n")
		g.pf("        uint32_t pairs_%s = 0;\n", f.Name)
		g.pf("        int32_t i;\n")
		g.pf("        for ( i = 0; i < %s; i++ ) /* [%s]: every stored slot is a named variant's */\n        {\n", enumMaxConst(f.KeyEnum), f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "            ", "return 0;")
		g.pf("            pairs_%s++;\n", f.Name)
		g.pf("        }\n")
		g.pf("        if ( pairs_%s > 0 )\n        {\n", f.Name)
		g.pf("            int64_t len_at_%s;\n", f.Name)
		g.pf("            /* KIND 16, not 14: a keyed body and a positional one are\n")
		g.pf("               incompatible, so a reader of the other kind must see a kind\n")
		g.pf("               mismatch and skip, never misdecode (docs/SPEC-TABLES.md §3.2) */\n")
		g.pf("            table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s (keyed by %s) */\n", id, tkKeyed, f.Name, f.KeyEnum)
		g.pf("            len_at_%s = w->offset; table_writer_put32( w, 0 );\n", f.Name)
		g.pf("            table_writer_put8( w, %d ); table_writer_put32( w, pairs_%s );\n", kind, f.Name)
		g.pf("            /* ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
		g.pf("               writer's choice, and a reader must not rely on it: every\n")
		g.pf("               slot is found by its key (docs/SPEC-TABLES.md §3.2) */\n")
		g.pf("            for ( i = 0; i < %s; i++ )\n            {\n", enumMaxConst(f.KeyEnum))
		g.pf("                int64_t elem_len_at_%s;\n", f.Name)
		g.emitKeyedSlotRides(f, kind, "                ", "return 0;")
		g.pf("                table_writer_put16( w, key_id ); /* the slot's VARIANT id, not its position */\n")
		g.pf("                elem_len_at_%s = w->offset; table_writer_put32( w, 0 );\n", f.Name)
		switch {
		case kind == tkTable:
			g.pf("                if ( !%s ) { return 0; }\n", g.saveCall(f.Type.Name, fmt.Sprintf("&value->%s[i]", f.Name), depthSame))
		case enumRef(f) != nil:
			g.pf("                table_writer_put16( w, element_id );\n")
		default:
			g.emitTableWriteElement(f, kind, fmt.Sprintf("value->%s[i]", f.Name), "                ")
		}
		g.pf("                table_writer_patch32( w, elem_len_at_%s, (uint32_t) ( w->offset - elem_len_at_%s - 4 ) );\n", f.Name, f.Name)
		g.pf("            }\n")
		g.pf("            table_writer_patch32( w, len_at_%s, (uint32_t) ( w->offset - len_at_%s - 4 ) );\n", f.Name, f.Name)
		g.pf("        }\n    }\n")
	case f.Type.Pointer:
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee_%s = %s( ctx, &value->%s ); /* *%s */\n", t, f.Name, g.api(t, "at"), f.Name, t)
		g.pf("        if ( pointee_%s != NULL )\n        {\n", f.Name)
		g.pf("            int64_t body_%s = %s;\n", f.Name, g.measureCall(t, "pointee_"+f.Name, depthDown))
		g.pf("            if ( body_%s < 0 ) { return 0; }\n", f.Name)
		g.pf("            table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s — the pointee rides as a nested body */\n", id, tkTable, f.Name)
		g.pf("            table_writer_put32( w, (uint32_t) body_%s );\n", f.Name)
		g.pf("            if ( !%s ) { return 0; }\n", g.saveCall(t, "pointee_"+f.Name, depthDown))
		g.pf("        }\n    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if ( value->%s_length < 0 || value->%s_length > %d ) { return 0; } /* storage invariant */\n", f.Name, f.Name, f.Type.Size)
		g.pf("    if ( value->%s_length > 0 )\n    {\n", f.Name)
		g.pf("        table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s */\n", id, tkString, f.Name)
		g.pf("        table_writer_put32( w, (uint32_t) value->%s_length );\n", f.Name)
		g.pf("        table_writer_raw( w, value->%s, value->%s_length );\n    }\n", f.Name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if ( value->%s_length < 0 || value->%s_length > %d ) { return 0; } /* storage invariant */\n", f.Name, f.Name, f.Type.Size)
		g.pf("    if ( value->%s_length > 0 )\n    {\n", f.Name)
		g.pf("        table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s */\n", id, tkArray, f.Name)
		g.pf("        table_writer_put32( w, (uint32_t) ( 5 + value->%s_length ) );\n", f.Name)
		g.pf("        table_writer_put8( w, %d ); table_writer_put32( w, (uint32_t) value->%s_length );\n", tkU8, f.Name)
		g.pf("        table_writer_raw( w, value->%s, value->%s_length );\n    }\n", f.Name, f.Name)
	case f.Array == ir.ArrayCounted:
		g.pf("    if ( value->%s_count < 0 || value->%s_count > %d ) { return 0; } /* storage invariant */\n", f.Name, f.Name, f.ArrayBound)
		g.pf("    if ( value->%s_count > 0 )\n    {\n", f.Name)
		g.pf("        int64_t len_at_%s;\n        int32_t i;\n", f.Name)
		g.pf("        table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s */\n", id, tkArray, f.Name)
		g.pf("        len_at_%s = w->offset; table_writer_put32( w, 0 );\n", f.Name)
		g.pf("        table_writer_put8( w, %d ); table_writer_put32( w, (uint32_t) value->%s_count );\n", kind, f.Name)
		g.pf("        for ( i = 0; i < value->%s_count; i++ )\n        {\n", f.Name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value->%s[i]", f.Name), "            ")
		g.pf("        }\n")
		g.pf("        table_writer_patch32( w, len_at_%s, (uint32_t) ( w->offset - len_at_%s - 4 ) );\n    }\n", f.Name, f.Name)
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — no cheap element-default
		// compare (an all-default element costs 6 bytes)
		g.pf("    {\n")
		g.pf("        int64_t len_at_%s;\n        int32_t i;\n", f.Name)
		g.pf("        table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s (fixed [%d]) */\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("        len_at_%s = w->offset; table_writer_put32( w, 0 );\n", f.Name)
		g.pf("        table_writer_put8( w, %d ); table_writer_put32( w, %d );\n", kind, f.ArrayBound)
		g.pf("        for ( i = 0; i < %d; i++ )\n        {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value->%s[i]", f.Name), "            ")
		g.pf("        }\n")
		g.pf("        table_writer_patch32( w, len_at_%s, (uint32_t) ( w->offset - len_at_%s - 4 ) );\n    }\n", f.Name, f.Name)
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		// (parity with the reader's prefill)
		g.pf("    {\n")
		g.pf("        int all_default_%s = 1;\n", f.Name)
		g.pf("        int32_t i;\n")
		g.pf("        for ( i = 0; i < %d; i++ ) { if ( value->%s[i] != %s ) { all_default_%s = 0; break; } }\n",
			f.ArrayBound, f.Name, g.fieldDefaultExpr(f), f.Name)
		g.pf("        if ( !all_default_%s )\n        {\n", f.Name)
		g.pf("            int64_t len_at_%s;\n", f.Name)
		g.pf("            table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s (fixed [%d]) */\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("            len_at_%s = w->offset; table_writer_put32( w, 0 );\n", f.Name)
		g.pf("            table_writer_put8( w, %d ); table_writer_put32( w, %d );\n", kind, f.ArrayBound)
		g.pf("            for ( i = 0; i < %d; i++ )\n            {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value->%s[i]", f.Name), "                ")
		g.pf("            }\n")
		g.pf("            table_writer_patch32( w, len_at_%s, (uint32_t) ( w->offset - len_at_%s - 4 ) );\n        }\n    }\n", f.Name, f.Name)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		for _, v := range un.Variants {
			g.noteRef(v.Type)
		}
		g.pf("    if ( value->%s.type != %s )\n    {\n", f.Name, enumConst(un.Name+"Type", "None"))
		g.pf("        int64_t len_at_%s;\n", f.Name)
		g.pf("        table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s */\n", id, tkUnion, f.Name)
		g.pf("        /* the ARM ID is the hash of the arm's NAME (docs/SPEC-TABLES.md §5), so\n")
		g.pf("           arms may be added anywhere, removed and reordered */\n")
		g.pf("        switch ( value->%s.type )\n        {\n", f.Name)
		for _, v := range un.Variants {
			g.pf("            case %s: table_writer_put16( w, 0x%04x ); break;\n", enumConst(un.Name+"Type", v.Name), ir.VariantId(v.Name))
		}
		g.pf("            default: return 0; /* write validates the tag before it rides */\n")
		g.pf("        }\n")
		g.pf("        len_at_%s = w->offset; table_writer_put32( w, 0 );\n", f.Name)
		g.pf("        switch ( value->%s.type )\n        {\n", f.Name)
		for _, v := range un.Variants {
			g.pf("            case %s: if ( !%s ) { return 0; } break;\n",
				enumConst(un.Name+"Type", v.Name), g.saveCall(v.Type, fmt.Sprintf("&value->%s.as.%s", f.Name, v.Name), depthSame))
		}
		g.pf("            default: return 0; /* write validates the tag before it rides */\n")
		g.pf("        }\n")
		g.pf("        table_writer_patch32( w, len_at_%s, (uint32_t) ( w->offset - len_at_%s - 4 ) );\n    }\n", f.Name, f.Name)
	case kind == tkTable:
		// elision is decided BEFORE any byte is emitted: measuring first
		// keeps an all-default nested field from touching the buffer at all,
		// so saving into a buffer of exactly Measure's size never trips
		// overflow on transient header bytes
		g.pf("    {\n")
		g.pf("        int64_t body_%s = %s;\n", f.Name, g.measureCall(f.Type.Name, "&value->"+f.Name, depthSame))
		g.pf("        if ( body_%s < 0 ) { return 0; } /* storage invariant, refused as measure refuses it */\n", f.Name)
		g.pf("        if ( body_%s > 2 ) /* all-default nested elides */\n        {\n", f.Name)
		g.pf("            table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s */\n", id, tkTable, f.Name)
		g.pf("            table_writer_put32( w, (uint32_t) body_%s );\n", f.Name)
		g.pf("            if ( !%s ) { return 0; }\n", g.saveCall(f.Type.Name, "&value->"+f.Name, depthSame))
		g.pf("        }\n    }\n")
	case enumRef(f) != nil:
		// the id is resolved BEFORE the header rides: a value no variant names
		// has no wire identity, and the write refuses it rather than writing None
		g.pf("    if ( value->%s != %s )\n    {\n", f.Name, g.fieldDefaultExpr(f))
		g.emitEnumIdSwitch(enumRef(f), "value->"+f.Name, "id_"+f.Name, "        ", "return 0;")
		g.pf("        table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s */\n", id, kind, f.Name)
		g.pf("        table_writer_put16( w, id_%s );\n    }\n", f.Name)
	default:
		g.pf("    if ( value->%s != %s )\n    {\n", f.Name, g.fieldDefaultExpr(f))
		g.pf("        table_writer_put16( w, 0x%04x ); table_writer_put8( w, %d ); /* %s */\n", id, kind, f.Name)
		g.emitTableWriteElement(f, kind, "value->"+f.Name, "        ")
		g.pf("    }\n")
	}
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	if e := enumRef(f); e != nil {
		g.pf("%s{\n", ind)
		g.emitEnumIdSwitch(e, expr, "element_id", ind+"    ", "return 0;")
		g.pf("%s    table_writer_put16( w, element_id );\n%s}\n", ind, ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%stable_writer_put8( w, %s ? 1 : 0 );\n", ind, expr)
	case tkF32:
		g.pf("%stable_writer_put32( w, table_float_to_bits( %s ) );\n", ind, expr)
	case tkF64:
		g.pf("%stable_writer_put64( w, table_double_to_bits( %s ) );\n", ind, expr)
	case tkTable:
		g.pf("%s{\n%s    int64_t elem_len_at = w->offset;\n%s    table_writer_put32( w, 0 );\n", ind, ind, ind)
		g.pf("%s    if ( !%s ) { return 0; }\n", ind, g.saveCall(f.Type.Name, "&"+expr, depthSame))
		g.pf("%s    table_writer_patch32( w, elem_len_at, (uint32_t) ( w->offset - elem_len_at - 4 ) );\n%s}\n", ind, ind)
	default:
		width := tableKindWidth(kind)
		cast := fmt.Sprintf("uint%d_t", width*8)
		g.pf("%s%s( w, (%s) ( %s ) );\n", ind, tablePut(width), cast, expr)
	}
}

// ---- read ----

func (g *tableGen) emitTableRead(st *ir.Struct) {
	if g.isVar(st.Name) {
		g.pf("static SCHEMA_UNUSED int %s( TableReader * r, TableSink * sink, %s * value, int32_t depth )\n{\n", g.api(st.Name, "load_body"), st.Name)
		if len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 {
			g.pf("    (void) sink; (void) depth;\n")
		}
	} else {
		g.pf("static SCHEMA_UNUSED %s int %s( TableReader * r, %s * value )\n{\n", tableInlineMacro(g.unit.Package), g.api(st.Name, "load_body"), st.Name)
	}
	g.pf("    %s( value ); /* prefill declared defaults in place, then overlay */\n", g.api(st.Name, "reset"))
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint16_t field_id;\n        uint8_t kind;\n")
	g.pf("        if ( !table_reader_has( r, 2 ) ) { r->report->malformed = 1; return 0; }\n")
	g.pf("        field_id = table_reader_get16( r );\n")
	g.pf("        if ( field_id == 0 ) { return 1; }\n")
	g.pf("        if ( !table_reader_has( r, 1 ) ) { r->report->malformed = 1; return 0; }\n")
	g.pf("        kind = table_reader_get8( r );\n")
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
			g.pf("            case 0x%04x: /* %s */\n            {\n", id, f.Name)
			g.pf("                if ( kind != %d )\n                {\n", wireKind)
			g.pf("                    r->report->kind_mismatch++;\n")
			g.pf("                    if ( !table_reader_skip( r, kind ) ) { r->report->malformed = 1; return 0; }\n")
			g.pf("                    break;\n                }\n")
			g.emitTableReadField(f, kind)
			if f.Type.Optional {
				// the field rode, so it is PRESENT — content decides nothing
				// here either (docs/SPEC-TABLES.md §2.3)
				g.pf("                value->%s_present = 1;\n", f.Name)
			}
			g.pf("                break;\n            }\n")
		}
		g.pf("            default:\n            {\n")
		g.pf("                r->report->unknown++;\n")
		g.pf("                if ( !table_reader_skip( r, kind ) ) { r->report->malformed = 1; return 0; }\n")
		g.pf("                break;\n            }\n")
		g.pf("        }\n    }\n}\n\n")
	} else {
		g.pf("        r->report->unknown++;\n")
		g.pf("        if ( !table_reader_skip( r, kind ) ) { r->report->malformed = 1; return 0; }\n")
		g.pf("    }\n}\n\n")
	}

	// buffer-level convenience entry. A VARIABLE-LENGTH table has none: it is
	// never held by value, so its Load takes the caller's region and hands back
	// the root instead (docs/SPEC-TABLES.md §2).
	if g.isVar(st.Name) {
		return
	}
	g.pf("static SCHEMA_UNUSED int %s( %s * value, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", g.api(st.Name, "load"), st.Name)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReader r;\n")
	g.pf("    memset( &ignored, 0, sizeof( ignored ) );\n")
	g.pf("    r = table_reader_make( buffer, bytes, report != NULL ? report : &ignored );\n")
	g.pf("    return %s( &r, value );\n}\n\n", g.api(st.Name, "load_body"))
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
		g.pf("%suint32_t body_len;\n%sint64_t body_end;\n", ind, ind)
		g.pf("%sif ( !table_reader_has( r, 4 ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sbody_len = table_reader_get32( r );\n", ind)
		g.pf("%sif ( !table_reader_has( r, body_len ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sbody_end = r->offset + body_len;\n", ind)
		g.pf("%sif ( body_len >= 5 )\n%s{\n", ind, ind)
		g.pf("%s    uint8_t elem_kind = table_reader_get8( r );\n", ind)
		g.pf("%s    uint32_t count = table_reader_get32( r );\n", ind)
		g.pf("%s    TableReader sub;\n%s    uint32_t i;\n", ind, ind)
		g.pf("%s    if ( elem_kind != %d ) { r->report->kind_mismatch++; r->offset = body_end; break; }\n", ind, kind)
		g.pf("%s    sub = table_reader_make( r->buffer + r->offset, body_end - r->offset, r->report );\n", ind)
		g.pf("%s    for ( i = 0; i < count; i++ )\n%s    {\n", ind, ind)
		g.pf("%s        uint16_t key;\n%s        uint32_t elem_len;\n%s        %s slot;\n", ind, ind, ind, f.KeyEnum)
		g.pf("%s        if ( !table_reader_has( &sub, 2 ) ) { r->report->malformed = 1; break; }\n", ind)
		g.pf("%s        key = table_reader_get16( &sub );\n", ind)
		g.pf("%s        if ( !table_reader_has( &sub, 4 ) ) { r->report->malformed = 1; break; }\n", ind)
		g.pf("%s        elem_len = table_reader_get32( &sub );\n", ind)
		g.pf("%s        if ( !table_reader_has( &sub, elem_len ) ) { r->report->malformed = 1; break; }\n", ind)
		g.pf("%s        if ( key == 0 )\n%s        {\n", ind, ind)
		g.pf("%s            /* None is the NULL KEY: 0 is the reserved id no declared\n", ind)
		g.pf("%s               name can fold to, so a body carrying one is DAMAGED, not\n", ind)
		g.pf("%s               merely foreign. Framing damage stops this body, keeps what\n", ind)
		g.pf("%s               it decoded, and the parent reads on past the length\n", ind)
		g.pf("%s               (docs/SPEC-TABLES.md §3.2, §4). */\n", ind)
		g.pf("%s            r->report->malformed = 1;\n%s            break;\n%s        }\n", ind, ind, ind)
		g.pf("%s        slot = %s;\n", ind, enumNoneConst(f.KeyEnum))
		g.pf("%s        {\n%s            int32_t before = r->report->unknown;\n", ind, ind)
		g.emitEnumValueSwitch(f.KeyEnumRef, "key", "slot", ind+"            ")
		g.pf("%s            if ( r->report->unknown != before )\n%s            {\n", ind, ind)
		g.pf("%s                sub.offset += elem_len; /* a slot this reader cannot name */\n", ind)
		g.pf("%s                continue;\n%s            }\n%s        }\n", ind, ind, ind)
		g.pf("%s        {\n%s            TableReader elem = table_reader_make( sub.buffer + sub.offset, elem_len, r->report );\n", ind, ind)
		// the key k lives at STORAGE INDEX k-1 (docs/SPEC-TABLES.md §2.4)
		slot := fmt.Sprintf("value->%s[(int32_t) slot - 1]", f.Name)
		if kind == tkTable {
			g.pf("%s            %s;\n", ind, g.loadCall(f.Type.Name, "&elem", "&"+slot, depthSame))
		} else {
			g.emitTableReadScalarFrom(f, kind, slot, ind+"            ", "elem",
				"r->report->malformed = 1; sub.offset += elem_len; continue;")
		}
		g.pf("%s        }\n", ind)
		g.pf("%s        sub.offset += elem_len;\n", ind)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.pf("%sr->offset = body_end; /* unread pairs and slack skip via the length */\n", ind)
	case f.Type.Pointer:
		t := f.Type.Name
		g.pf("%suint32_t body_len;\n", ind)
		g.pf("%sif ( !table_reader_has( r, 4 ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sbody_len = table_reader_get32( r );\n", ind)
		g.pf("%sif ( !table_reader_has( r, body_len ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sif ( depth >= kTableMaxDepth )\n%s{\n", ind, ind)
		g.pf("%s    /* past the nesting cap: the subtree is refused, the pointer stays\n", ind)
		g.pf("%s       null, and the parent reads on (docs/SPEC-TABLES.md §4) */\n", ind)
		g.pf("%s    r->report->malformed = 1;\n", ind)
		g.pf("%s    r->offset += body_len;\n%s    break;\n%s}\n", ind, ind, ind)
		g.pf("%s{\n%s    %s * pointee = %s( sink, &value->%s );\n", ind, ind, t, g.api(t, "emplace"), f.Name)
		g.pf("%s    TableReader sub;\n", ind)
		g.pf("%s    if ( pointee == NULL )\n%s    {\n", ind, ind)
		g.pf("%s        r->report->malformed = 1; /* the caller's region was short */\n", ind)
		g.pf("%s        r->offset += body_len;\n%s        break;\n%s    }\n", ind, ind, ind)
		g.pf("%s    sub = table_reader_make( r->buffer + r->offset, body_len, r->report );\n", ind)
		g.pf("%s    %s;\n", ind, g.loadCall(t, "&sub", "pointee", depthDown))
		g.pf("%s}\n", ind)
		g.pf("%sr->offset += body_len;\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%suint32_t len;\n%suint32_t keep;\n", ind, ind)
		g.pf("%sif ( !table_reader_has( r, 4 ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%slen = table_reader_get32( r );\n", ind)
		g.pf("%sif ( !table_reader_has( r, len ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%skeep = len;\n", ind)
		g.pf("%sif ( keep > %d ) { keep = %d; r->report->clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%smemcpy( value->%s, r->buffer + r->offset, keep );\n", ind, f.Name)
		g.pf("%svalue->%s[keep] = 0;\n", ind, f.Name)
		g.pf("%svalue->%s_length = (int32_t) keep;\n", ind, f.Name)
		g.pf("%sr->offset += len;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
		g.pf("%suint32_t body_len;\n%sint64_t body_end;\n", ind, ind)
		g.pf("%sif ( !table_reader_has( r, 4 ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sbody_len = table_reader_get32( r );\n", ind)
		g.pf("%sif ( !table_reader_has( r, body_len ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sbody_end = r->offset + body_len;\n", ind)
		g.pf("%sif ( body_len >= 5 )\n%s{\n", ind, ind)
		g.pf("%s    uint8_t elem_kind = table_reader_get8( r );\n", ind)
		g.pf("%s    uint32_t count = table_reader_get32( r );\n", ind)
		g.pf("%s    uint32_t keep;\n%s    TableReader sub;\n%s    uint32_t i;\n", ind, ind, ind)
		if counted {
			g.pf("%s    uint32_t decoded = 0;\n", ind)
		}
		g.pf("%s    if ( elem_kind != %d ) { r->report->kind_mismatch++; r->offset = body_end; break; }\n", ind, kind)
		g.pf("%s    keep = count;\n", ind)
		g.pf("%s    if ( keep > %d ) { keep = %d; r->report->clamped++; }\n", ind, bound, bound)
		g.pf("%s    /* elements are BOUNDED by the field body: a count the length\n", ind)
		g.pf("%s       cannot cover keeps the decoded prefix, flags malformed, and\n", ind)
		g.pf("%s       the parent continues at the next field — following fields'\n", ind)
		g.pf("%s       bytes are never fabricated into elements */\n", ind)
		g.pf("%s    sub = table_reader_make( r->buffer + r->offset, body_end - r->offset, r->report );\n", ind)
		g.pf("%s    for ( i = 0; i < keep; i++ )\n%s    {\n", ind, ind)
		g.emitTableReadElement(f, kind, ind+"        ")
		if counted {
			g.pf("%s        decoded = i + 1;\n", ind)
		}
		g.pf("%s    }\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s    value->%s_length = (int32_t) decoded;\n", ind, f.Name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s    value->%s_count = (int32_t) decoded;\n", ind, f.Name)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr->offset = body_end; /* excess elements and slack skip via the length */\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("%suint16_t arm_id;\n%suint32_t body_len;\n", ind, ind)
		g.pf("%sif ( !table_reader_has( r, 2 ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sarm_id = table_reader_get16( r );\n", ind)
		g.pf("%sif ( arm_id == 0 ) { value->%s.type = %s; break; } /* empty: the id is the whole payload */\n",
			ind, f.Name, enumConst(un.Name+"Type", "None"))
		g.pf("%sif ( !table_reader_has( r, 4 ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sbody_len = table_reader_get32( r );\n", ind)
		g.pf("%sif ( !table_reader_has( r, body_len ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%s{\n%s    TableReader sub = table_reader_make( r->buffer + r->offset, body_len, r->report );\n", ind, ind)
		g.pf("%s    switch ( arm_id ) /* the arm's NAME hash (docs/SPEC-TABLES.md §5) */\n%s    {\n", ind, ind)
		for _, v := range un.Variants {
			g.pf("%s        case 0x%04x: /* %s */\n%s            value->%s.type = %s;\n%s            %s;\n%s            break;\n",
				ind, ir.VariantId(v.Name), v.Name, ind, f.Name, enumConst(un.Name+"Type", v.Name),
				ind, g.loadCall(v.Type, "&sub", fmt.Sprintf("&value->%s.as.%s", f.Name, v.Name), depthSame), ind)
		}
		g.pf("%s        default:\n", ind)
		g.pf("%s            /* an arm this reader cannot name: the value reads EMPTY and\n", ind)
		g.pf("%s               the body is skipped by its length, never misdecoded. The\n", ind)
		g.pf("%s               reset is explicit, not the prefill's: a repeated field id\n", ind)
		g.pf("%s               must not leave an arm decoded by an earlier occurrence\n", ind)
		g.pf("%s               standing (docs/SPEC-TABLES.md §4). */\n", ind)
		g.pf("%s            value->%s.type = %s;\n", ind, f.Name, enumConst(un.Name+"Type", "None"))
		g.pf("%s            r->report->unknown++;\n%s            break;\n", ind, ind)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.pf("%sr->offset += body_len;\n", ind)
	case kind == tkTable:
		g.pf("%suint32_t body_len;\n", ind)
		g.pf("%sif ( !table_reader_has( r, 4 ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%sbody_len = table_reader_get32( r );\n", ind)
		g.pf("%sif ( !table_reader_has( r, body_len ) ) { r->report->malformed = 1; return 0; }\n", ind)
		g.pf("%s{\n%s    TableReader sub = table_reader_make( r->buffer + r->offset, body_len, r->report );\n", ind, ind)
		g.pf("%s    %s;\n", ind, g.loadCall(f.Type.Name, "&sub", "&value->"+f.Name, depthSame))
		g.pf("%s}\n", ind)
		g.pf("%sr->offset += body_len;\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, "value->"+f.Name, ind,
			"(*r)", "r->report->malformed = 1; return 0;")
	}
}

// emitTableReadElement decodes one array element from the field-body
// sub-reader; truncation keeps the decoded prefix and flags malformed
// without stopping the parent decode.
func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	switch kind {
	case tkTable:
		g.pf("%suint32_t elem_len;\n", ind)
		g.pf("%sif ( !table_reader_has( &sub, 4 ) ) { r->report->malformed = 1; break; }\n", ind)
		g.pf("%selem_len = table_reader_get32( &sub );\n", ind)
		g.pf("%sif ( !table_reader_has( &sub, elem_len ) ) { r->report->malformed = 1; break; }\n", ind)
		g.pf("%s{\n%s    TableReader elem = table_reader_make( sub.buffer + sub.offset, elem_len, r->report );\n", ind, ind)
		g.pf("%s    %s;\n", ind, g.loadCall(f.Type.Name, "&elem", fmt.Sprintf("&value->%s[i]", f.Name), depthSame))
		g.pf("%s}\n", ind)
		g.pf("%ssub.offset += elem_len;\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, fmt.Sprintf("value->%s[i]", f.Name), ind,
			"sub", "r->report->malformed = 1; break;")
	}
}

// emitTableReadScalarFrom decodes one fixed-width scalar from the named
// reader into a storage lvalue, with range clamps where the schema declares
// them. onTrunc is the truncation action: a scalar FIELD stops the decode
// (outer framing damage), an array ELEMENT keeps the prefix and breaks.
func (g *tableGen) emitTableReadScalarFrom(f *ir.Field, kind int, lvalue, ind, rdr, onTrunc string) {
	width := tableKindWidth(kind)
	g.pf("%sif ( !table_reader_has( &%s, %d ) ) { %s }\n", ind, rdr, width, onTrunc)
	if enum := enumRef(f); enum != nil {
		// identity is the variant's NAME (docs/SPEC-TABLES.md §5)
		g.pf("%s{\n%s    uint16_t variant = table_reader_get16( &%s );\n", ind, ind, rdr)
		g.emitEnumValueSwitch(enum, "variant", lvalue, ind+"    ")
		g.pf("%s}\n", ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%s%s = table_reader_get8( &%s ) != 0;\n", ind, lvalue, rdr)
	case tkF32:
		if f.HasFloatRange {
			g.pf("%s{\n%s    float decoded_f = table_bits_to_float( table_reader_get32( &%s ) );\n", ind, ind, rdr)
			g.pf("%s    if ( decoded_f < %s ) { decoded_f = %s; r->report->clamped++; }\n", ind, formatFloat(f.FMin, true), formatFloat(f.FMin, true))
			g.pf("%s    else if ( decoded_f > %s ) { decoded_f = %s; r->report->clamped++; }\n", ind, formatFloat(f.FMax, true), formatFloat(f.FMax, true))
			g.pf("%s    %s = decoded_f;\n%s}\n", ind, lvalue, ind)
			return
		}
		g.pf("%s%s = table_bits_to_float( table_reader_get32( &%s ) );\n", ind, lvalue, rdr)
	case tkF64:
		g.pf("%s%s = table_bits_to_double( table_reader_get64( &%s ) );\n", ind, lvalue, rdr)
	default:
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		storage := fmt.Sprintf("uint%d_t", width*8)
		if signed {
			storage = fmt.Sprintf("int%d_t", width*8)
		}
		g.pf("%s{\n%s    %s decoded_v = (%s) %s( &%s );\n", ind, ind, storage, storage, tableGet(width), rdr)
		if f.HasIntRange {
			low, high := tableClampEnds(f, width)
			if low {
				lo := tableIntLit(f.IntMin, signed, width)
				g.pf("%s    if ( decoded_v < %s ) { decoded_v = %s; r->report->clamped++; }\n", ind, lo, lo)
			}
			if high {
				hi := tableIntLit(f.IntMax, signed, width)
				lead := "if"
				if low {
					lead = "else if"
				}
				g.pf("%s    %s ( decoded_v > %s ) { decoded_v = %s; r->report->clamped++; }\n", ind, lead, hi, hi)
			}
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
			maxv := (uint64(1) << f.Type.Width) - 1
			g.pf("%s    if ( decoded_v > %dull ) { decoded_v = %dull; r->report->clamped++; } /* bits(%d) width clamp */\n", ind, maxv, maxv, f.Type.Width)
		}
		g.pf("%s    %s = decoded_v;\n%s}\n", ind, lvalue, ind)
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

// bigToDouble renders a big.Int as a C double literal for the descriptor's
// range fields (precision past 2^53 is documented as lost).
func bigToDouble(v *big.Int) string {
	f, _ := new(big.Float).SetInt(v).Float64()
	return formatFloat(f, false)
}

// vocabularySymbol names one field's variant table inside <Base>Table.c.
// Internal, and spelled the way every other generated symbol with a linker
// name is (see sym): the package, the owner, the field, and a trailing
// underscore. Nothing a schema can declare collides with it, which is why the
// vocabularies claim no name of their own (docs/SPEC-TABLES.md §11).
func (g *tableGen) vocabularySymbol(owner, field, what string) string {
	return g.sym(owner, ir.RustSnake(field)+"_"+what)
}

// emitTableDescriptor emits <X>TableInfo and its field table into
// <Base>Table.c — CONSTANT data, one definition per program, so a field's
// target is the ADDRESS of another descriptor rather than a call. That is
// what lets a self- or mutually-referential graph — Node naming itself
// through *Node — be expressed as constant data instead of a lazy link, which
// could not have been written race-free OR recursion-safe. The whole
// reflection surface is therefore immutable: read it from any thread, any
// time.
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	guards := tableGuardStrings(st)
	if len(st.Fields) == 0 {
		g.pf("const TableTypeInfo %s = { \"%s\", (uint32_t) sizeof( %s ), 0, NULL, %s%s };\n\n",
			g.sym(st.Name, "info"), st.Name, st.Name, g.sym(st.Name, "reset_raw"), g.modeColumn(st))
		return
	}
	// the vocabularies first: an enum's values, a union's arms and a flags
	// field's BITS are each a named set indexed by [0, enum_max], and each
	// rides as a table rather than as a function per declaration.
	for _, f := range st.Fields {
		g.emitFieldVocabulary(st, f)
	}
	g.pf("static const TableFieldInfo %s[] = {\n", g.sym(st.Name, "fields"))
	for _, f := range st.Fields {
		g.emitFieldDescriptor(st, f, guards[f.Name])
	}
	g.pf("};\n\n")
	g.pf("const TableTypeInfo %s = { \"%s\", (uint32_t) sizeof( %s ), %d, %s, %s%s };\n\n",
		g.sym(st.Name, "info"), st.Name, st.Name, len(st.Fields), g.sym(st.Name, "fields"), g.sym(st.Name, "reset_raw"), g.modeColumn(st))
}

// emitFieldVocabulary emits one field's variant table, its key table and its
// union arm table where it has them.
func (g *tableGen) emitFieldVocabulary(st *ir.Struct, f *ir.Field) {
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind != ir.TNamed {
			break
		}
		g.pf("static const TableVariantInfo %s[] = {\n", g.vocabularySymbol(st.Name, f.Name, "variants"))
		names := map[int64]string{0: "None"}
		for i, v := range ref.Variants {
			names[int64(i)+1] = v
		}
		for i := int64(0); i <= ref.Max; i++ {
			if n, ok := names[i]; ok {
				id := uint16(0)
				if i != 0 {
					id = ir.VariantId(n)
				}
				g.pf("    { \"%s\", 0x%04x },\n", n, id)
				continue
			}
			// headroom past the declared set: a value the vocabulary does not
			// name, and the reserved id 0 no declared name can fold to (§5)
			g.pf("    { NULL, 0 },\n")
		}
		g.pf("};\n")
	case *ir.Flags:
		if f.Type.Kind != ir.TNamed {
			break
		}
		// a flags mask is the wire's one POSITIONAL vocabulary
		// (docs/SPEC-TABLES.md §4): its variants are BIT POSITIONS, so the
		// table names bits and carries no per-variant wire id.
		g.pf("static const TableVariantInfo %s[] = {\n", g.vocabularySymbol(st.Name, f.Name, "variants"))
		for _, v := range ref.Variants {
			g.pf("    { \"%s\", 0 },\n", v)
		}
		g.pf("};\n")
	case *ir.Union:
		if f.Type.Kind != ir.TNamed || f.Array != ir.ArrayNone {
			break
		}
		g.pf("static const TableVariantInfo %s[] = {\n", g.vocabularySymbol(st.Name, f.Name, "variants"))
		g.pf("    { \"None\", 0 },\n")
		for _, v := range ref.Variants {
			g.pf("    { \"%s\", 0x%04x },\n", v.Name, ir.VariantId(v.Name))
		}
		g.pf("};\n")
		g.pf("static const TableUnionArmInfo %s[] = {\n", g.vocabularySymbol(st.Name, f.Name, "arms"))
		g.pf("    { 0, NULL },\n")
		for _, v := range ref.Variants {
			g.noteRef(v.Type)
			g.pf("    { (uint32_t) offsetof( %s, as.%s ), &%s },\n", ref.Name, v.Name, g.sym(v.Type, "info"))
		}
		g.pf("};\n")
		g.pf("static const TableUnionInfo %s = { (uint32_t) offsetof( %s, type ), (uint32_t) sizeof( ( (%s *) 0 )->type ), %s };\n",
			g.vocabularySymbol(st.Name, f.Name, "union"), ref.Name, ref.Name, g.vocabularySymbol(st.Name, f.Name, "arms"))
	}
	if f.KeyEnum != "" && f.KeyEnumRef != nil {
		// the KEY's vocabulary on an enum-keyed array (docs/SPEC-TABLES.md §8),
		// indexed by the KEY — a walker stepping [0, array_bound) asks about
		// index + 1 and prints slots by name without the schema files
		key := f.KeyEnumRef
		g.pf("static const TableVariantInfo %s[] = {\n", g.vocabularySymbol(st.Name, f.Name, "keys"))
		names := map[int64]string{0: "None"}
		for i, v := range key.Variants {
			names[int64(i)+1] = v
		}
		for i := int64(0); i <= key.Max; i++ {
			if n, ok := names[i]; ok {
				id := uint16(0)
				if i != 0 {
					id = ir.VariantId(n)
				}
				g.pf("    { \"%s\", 0x%04x },\n", n, id)
				continue
			}
			g.pf("    { NULL, 0 },\n")
		}
		g.pf("};\n")
	}
}

func (g *tableGen) emitFieldDescriptor(st *ir.Struct, f *ir.Field, guard string) {
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

	// the count column, spelled the way the storage spells its own extent: a
	// keyed array DERIVES it from the key enum, so nothing outside the array
	// names its size (docs/SPEC-TABLES.md §2.4, §8.1)
	bound := "0"
	switch {
	case f.KeyEnum != "":
		bound = "(int32_t) " + enumMaxConst(f.KeyEnum)
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}

	// `( (T *) 0 )->field`, never a materialized value: an unevaluated member
	// access names the type without an object, where a braced temporary makes
	// the compiler build a whole value of T to take the size of one member.
	elemSize := fmt.Sprintf("(uint32_t) sizeof( ( (%s *) 0 )->%s )", st.Name, f.Name)
	if isArray {
		elemSize = fmt.Sprintf("(uint32_t) sizeof( ( (%s *) 0 )->%s[0] )", st.Name, f.Name)
	}

	countOffset := "0xffffffffu"
	if counted {
		companion := f.Name + "_count"
		if f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString {
			companion = f.Name + "_length"
		}
		countOffset = fmt.Sprintf("(uint32_t) offsetof( %s, %s )", st.Name, companion)
	}

	if f.Type.Pointer {
		// a pointer's storage IS the reference slot: offset names it,
		// elem_size is the slot's width, and there is no companion
		elemSize = "(uint32_t) sizeof( TableRef )"
		counted = false
		bound = "0"
		countOffset = "0xffffffffu"
	}
	table := "NULL"
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		table = "&" + g.sym(f.Type.Name, "info")
		g.noteRef(f.Type.Name)
	}

	hasRange := "0"
	rangeMin, rangeMax := "0.0", "0.0"
	if f.Type.Kind == ir.TBits && !f.HasIntRange {
		// bits(N) declares its range by its WIDTH: [0, 2^N - 1]. The codec has
		// always clamped a read to it (docs/SPEC-TABLES.md §4); carrying it here
		// is what lets a generic walker apply the same bound without
		// re-deriving it from the type name.
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		hasRange = "1"
		rangeMin, rangeMax = "0.0", bigToDouble(max)
	}
	if f.HasIntRange {
		hasRange = "1"
		rangeMin, rangeMax = bigToDouble(f.IntMin), bigToDouble(f.IntMax)
	} else if f.HasFloatRange {
		hasRange = "1"
		rangeMin, rangeMax = formatFloat(f.FMin, false), formatFloat(f.FMax, false)
	}

	enumMax := "-1"
	variants := "NULL"
	hasIds := "0"
	arms := "NULL"
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", ref.Max)
			variants = g.vocabularySymbol(st.Name, f.Name, "variants")
			hasIds = "1"
		}
	case *ir.Flags:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", len(ref.Variants)-1)
			variants = g.vocabularySymbol(st.Name, f.Name, "variants")
		}
	case *ir.Union:
		if f.Type.Kind == ir.TNamed && f.Array == ir.ArrayNone {
			enumMax = fmt.Sprintf("%d", len(ref.Variants))
			variants = g.vocabularySymbol(st.Name, f.Name, "variants")
			hasIds = "1"
			arms = "&" + g.vocabularySymbol(st.Name, f.Name, "union")
		}
	}

	presentOffset := "0xffffffffu"
	if f.Type.Optional {
		presentOffset = fmt.Sprintf("(uint32_t) offsetof( %s, %s_present )", st.Name, f.Name)
	}

	keyTypeName, keys, keyMax := "NULL", "NULL", "-1"
	if f.KeyEnum != "" && f.KeyEnumRef != nil {
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keys = g.vocabularySymbol(st.Name, f.Name, "keys")
		keyMax = fmt.Sprintf("%d", f.KeyEnumRef.Max)
		g.noteRef(f.KeyEnum)
	}

	pointerColumn := ""
	if g.anyVariable {
		pointerColumn = fmt.Sprintf("%s, ", boolC(f.Type.Pointer))
	}
	g.pf("    { \"%s\", \"%s\", \"%s\", 0x%04x, %d, %s, %s%s, %s, %s, (uint32_t) offsetof( %s, %s ), %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, \"%s\" },\n",
		f.Name, ir.TableFieldJsonKey(f), tableFieldTypeName(f), id, kind, boolC(isArray), pointerColumn,
		boolC(counted), boolC(f.Type.Optional), bound,
		st.Name, f.Name, elemSize, countOffset, presentOffset, table,
		hasRange, rangeMin, rangeMax, enumMax, variants, hasIds,
		keyTypeName, keys, keyMax, arms, guard)
}

func boolC(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// THE FORCE-INLINE LINE, and why it falls where it does (schema#343).
//
// The FIXED class's bodies carry the qualifier and the VARIABLE-LENGTH class's
// do not, and that boundary is the one place a table's save/load call graph can
// hold a cycle. A fixed table has no pointer in its by-value closure, so its
// bodies nest by value and a cycle would make `sizeof` infinite — the graph is a
// DAG by construction and forcing it flat always terminates. A pointered body
// reaches its pointee through the depth-carrying form (docs/SPEC-TABLES.md
// §3.1), which a self-referential declaration makes directly recursive, and a
// recursive always_inline is a compile error under gcc. So the switch that
// already separates the two classes is the guard, exactly as it is in the
// reference.
//
// `Measure` is NOT force-inlined, in either class and in either backend: it is
// called once per nested body to decide elision and its result is a number, so
// it neither holds the cursor nor merges stores.
