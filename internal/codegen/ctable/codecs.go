// TABLE-wire storage, codec and descriptor emission in C
// (docs/SPEC-TABLES.md). Readers prefill declared defaults then overlay, skip
// unknown ids, skip kind mismatches, clamp out-of-range values, and count
// every event.
package ctable

import (
	"fmt"
	"math/big"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cFieldType maps a field type to its C storage spelling, mirroring the packet
// emitter's conventions so closure types from <Base>.h and table structs from
// this header read as one family.
func (g *tableGen) cFieldType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TInt, ir.TFixed:
		if t.Width == 128 {
			if t.Signed {
				return "serialize_int128_t"
			}
			return "serialize_uint128_t"
		}
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
	case ir.TInt, ir.TBits, ir.TFixed:
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
		if f.Type.Width == 128 {
			return tableWideLit(big.NewInt(0), f.Type.Signed)
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
			if f.HasDefault && f.DefInt != nil {
				return tableIntLit(f.DefInt, false, 8)
			}
			return "0"
		}
	}
	return "0"
}

// ---- storage (table declarations only; closure types come from <Base>.h) ----

func (g *tableGen) emitTableStruct(st *ir.Struct) {
	g.pf("%s", ir.DocComment(st.Doc, "", "//"))
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
		g.pf("%s", ir.DocComment(f.Doc, "    ", "//"))
		g.emitTableStorageField(f)
	}
	if len(st.Fields) == 0 {
		g.pf("    char unused_; /* C has no empty struct; carries no wire bits */\n")
	}
	g.pf("} %s;\n\n", st.Name)
}

func (g *tableGen) emitTableStorageField(f *ir.Field) {
	if f.IsList() {
		g.pf("    TableList %s;\n", f.Name)
		return
	}
	if f.IsMap() {
		g.pf("    TableMap %s;\n", f.Name)
		return
	}
	if f.Type.Pointer {
		// a pointer is EIGHT BYTES and no address: an arena offset while the
		// builder is mutable, a self-relative delta once packed. That is what
		// keeps a pointer-bearing table relocatable in both forms.
		g.noteRef(f.Type.Name)
		if f.Array == ir.ArrayNone {
			g.pf("    TableRef %s; /* *%s — null until assigned */\n", f.Name, f.Type.Name)
		} else {
			g.pf("    TableRef %s[%d];\n", f.Name, f.ArrayBound)
			if f.Array == ir.ArrayCounted {
				g.pf("    int32_t %s_count;\n", f.Name)
			}
		}
		return
	}
	typ := g.cFieldType(f.Type)
	if f.Type.Width == 128 {
		typ = "SCHEMA_C_ALIGN16 " + typ
	}
	switch {
	case f.Type.Kind == ir.TWString:
		g.pf("    uint16_t %s[%d + 1];\n    int32_t %s_length;\n", f.Name, f.Type.Size, f.Name)
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
	if f.IsList() || f.IsMap() {
		g.pf("    memset(&value->%s,0,sizeof(value->%s));\n", f.Name, f.Name)
		return
	}
	if f.Type.Pointer {
		if f.Array == ir.ArrayNone {
			g.pf("    value->%s.value = 0; /* *%s — null */\n", f.Name, f.Type.Name)
		} else {
			g.pf("    memset(value->%s,0,sizeof(value->%s));\n", f.Name, f.Name)
			if f.Array == ir.ArrayCounted {
				g.pf("    value->%s_count=0;\n", f.Name)
			}
		}
		return
	}
	typ := g.cFieldType(f.Type)
	selfInit := cSelfInit(f.Type)
	switch {
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		g.pf("    memset( value->%s, 0, sizeof( value->%s ) );\n", f.Name, f.Name)
		if f.HasDefault && len(f.DefBytes) != 0 {
			g.pf("    { static const uint8_t initial[] = { %s }; memcpy( value->%s, initial, sizeof( initial ) ); }\n", byteLiterals(f.DefBytes), f.Name)
		}
		g.pf("    value->%s_length = %d;\n", f.Name, len(f.DefBytes))
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

// Fixed scalars share the current wire writer; enum identities and nested
// frames are handled by wireScalarWrite and wirePayload before this helper.
func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	switch kind {
	case tkBool:
		g.pf("%stable_writer_put8( w, %s ? 1 : 0 );\n", ind, expr)
	case tkF32:
		g.pf("%stable_writer_put32( w, table_float_to_bits( %s ) );\n", ind, expr)
	case tkF64:
		g.pf("%stable_writer_put64( w, table_double_to_bits( %s ) );\n", ind, expr)
	default:
		width := tableKindWidth(kind)
		cast := fmt.Sprintf("uint%d_t", width*8)
		g.pf("%s%s( w, (%s) ( %s ) );\n", ind, tablePut(width), cast, expr)
	}
}

// emitTableReadScalarFrom decodes one fixed-width scalar from the named
// reader into a storage lvalue, with range clamps where the schema declares
// them. onTrunc is the truncation action: a scalar FIELD stops the decode
// (outer framing damage), an array ELEMENT keeps the prefix and breaks.
func (g *tableGen) emitTableReadScalarFrom(f *ir.Field, kind int, lvalue, ind, rdr, onTrunc string) {
	width := tableKindWidth(kind)
	g.pf("%sif ( !table_reader_has( &%s, %d ) ) { %s }\n", ind, rdr, width, onTrunc)
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
		signed := ir.TableKindSigned(ir.TableScalarKind(f))
		storage := fmt.Sprintf("uint%d_t", width*8)
		if signed {
			storage = fmt.Sprintf("int%d_t", width*8)
		}
		g.pf("%s{\n%s    %s decoded_v = (%s) %s( &%s );\n", ind, ind, storage, storage, tableGet(width), rdr)
		if rlo, rhi, ok := ir.TableRawRange(f); ok {
			low, high := tableClampEnds(f, width)
			if low {
				lo := tableIntLit(rlo, signed, width)
				g.pf("%s    if ( decoded_v < %s ) { decoded_v = %s; r->report->clamped++; }\n", ind, lo, lo)
			}
			if high {
				hi := tableIntLit(rhi, signed, width)
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
func tableFieldTypeName(f *ir.Field) string { return ir.TableTypeSpelling(f) }

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
	if g.outside {
		what = "outside_" + what
	}
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
		g.emitTagsStatic(g.sym(st.Name, "tags"), st.Tags)
		g.pf("const TableTypeInfo %s = { \"%s\", (uint32_t) sizeof( %s ), 0, NULL, %s%s, %s };\n\n",
			g.sym(st.Name, "info"), st.Name, st.Name, g.sym(st.Name, "reset_raw"), g.modeColumn(st),
			annotationColumns(st.Doc, st.Tags, g.sym(st.Name, "tags")))
		return
	}
	// the vocabularies first: an enum's values, a union's arms and a flags
	// field's BITS are each a named set indexed by [0, enum_max], and each
	// rides as a table rather than as a function per declaration.
	for _, f := range st.Fields {
		g.emitFieldVocabulary(st, f)
	}
	// the TAG lists (docs/SPEC-TABLES.md §8.1), one constant per tagged field
	// and one for a tagged declaration, named from the descriptor row as the
	// vocabularies are
	for _, f := range st.Fields {
		g.emitTagsStatic(g.vocabularySymbol(st.Name, f.Name, "tags"), f.Tags)
	}
	g.emitTagsStatic(g.sym(st.Name, "tags"), st.Tags)
	g.pf("static const TableFieldInfo %s[] = {\n", g.sym(st.Name, "fields"))
	for _, f := range st.Fields {
		g.emitFieldDescriptor(st, f, guards[f.Name])
	}
	g.pf("};\n\n")
	g.pf("const TableTypeInfo %s = { \"%s\", (uint32_t) sizeof( %s ), %d, %s, %s%s, %s };\n\n",
		g.sym(st.Name, "info"), st.Name, st.Name, len(st.Fields), g.sym(st.Name, "fields"), g.sym(st.Name, "reset_raw"), g.modeColumn(st),
		annotationColumns(st.Doc, st.Tags, g.sym(st.Name, "tags")))
}

// emitTagsStatic emits one tag list as a constant array of string literals
// (docs/SPEC-TABLES.md §8.1), and nothing at all for an item with no tags:
// absence is 0 and NULL in the row, never a per-row empty array.
func (g *tableGen) emitTagsStatic(name string, tags []string) {
	if len(tags) == 0 {
		return
	}
	g.pf("static const char * const %s[] = { %s };\n", name, ir.QuotedTags(tags))
}

// annotationColumns renders a row's doc, num_tags and tags columns: the shared
// empty doc and a NULL list where the item carries none.
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

// emitFieldVocabulary emits one field's variant table, its key table and its
// union arm table where it has them.
func (g *tableGen) emitFieldVocabulary(st *ir.Struct, f *ir.Field) {
	if ir.TableScalarKind(f) >= 18 && ir.TableScalarKind(f) <= 29 {
		lo, hi, ok := ir.TableRawRange(f)
		if !ok {
			lo, hi = tableStorageRange(f.Type.Signed, f.Type.Width)
		}
		llo, lhi := wideLanes(lo)
		hlo, hhi := wideLanes(hi)
		g.pf("static const TableWideRange %s = { { %sull, %sull }, { %sull, %sull } };\n", g.vocabularySymbol(st.Name, f.Name, "range"), llo, lhi, hlo, hhi)
	}

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
				id := uint64(0)
				if i != 0 {
					id = uint64(ir.VariantId(n))
					if g.fileWire() {
						id = ir.TableWireId(ref.VariantWireName(int(i - 1)))
					}
				}
				g.pf("    { \"%s\", 0x%016xull },\n", n, g.descriptorID(id))
				continue
			}
			// headroom past the declared set: a value the vocabulary does not
			// name; distinguish it by its missing name, not by its identity
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
		if f.Type.Kind != ir.TNamed {
			break
		}
		g.emitUnionArmDescriptors(ref)
		g.pf("static const TableVariantInfo %s[] = {\n", g.vocabularySymbol(st.Name, f.Name, "variants"))
		g.pf("    { \"None\", 0 },\n")
		for _, v := range ref.Variants {
			id := uint64(ir.VariantId(v.Name))
			if g.fileWire() {
				id = ir.TableWireId(v.WireName())
			}
			g.pf("    { \"%s\", 0x%016xull },\n", v.Name, g.descriptorID(id))
		}
		g.pf("};\n")
		g.pf("static const TableUnionArmInfo %s[] = {\n", g.vocabularySymbol(st.Name, f.Name, "arms"))
		g.pf("    { 0, NULL, NULL, 0 },\n")
		for _, v := range ref.Variants {
			if v.Void() {
				g.pf("    { 0, NULL, NULL, 0 },\n")
				continue
			}
			target, field := "NULL", "NULL"
			if v.Body() {
				g.noteRef(v.Type)
				target = "&" + g.sym(v.Type, "info")
			} else {
				field = g.armDescriptorSymbol(ref.Name, v.Name)
			}
			g.pf("    { (uint32_t)offsetof( %s, as.%s ), %s, %s, (uint32_t)sizeof( ((%s *)0)->as.%s ) },\n", ref.Name, v.Name, target, field, ref.Name, v.Name)
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
				id := uint64(0)
				if i != 0 {
					id = uint64(ir.VariantId(n))
					if g.fileWire() {
						id = ir.TableWireId(key.VariantWireName(int(i - 1)))
					}
				}
				g.pf("    { \"%s\", 0x%016xull },\n", n, g.descriptorID(id))
				continue
			}
			g.pf("    { NULL, 0 },\n")
		}
		g.pf("};\n")
	}
}

func (g *tableGen) emitFieldDescriptor(st *ir.Struct, f *ir.Field, guard string) {
	g.emitFieldDescriptorAt(st, f, guard, f.Name)
}
func (g *tableGen) emitFieldDescriptorAt(st *ir.Struct, f *ir.Field, guard, member string) {
	id := uint64(ir.TableFieldId(f))
	if g.fileWire() {
		id = ir.TableFieldWireId(f)
	}
	kind := tableScalarKind(f)
	if g.fileWire() {
		kind = ir.TableWireScalarKind(f)
	}
	if f.Type.Kind == ir.TBytes && !f.Type.Blob() {
		kind = tkU8
	}
	isArray := f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Blob())
	if f.Type.Pointer && !g.fileWire() {
		kind = tkTable
	}
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes || (f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString)

	// the count column, spelled the way the storage spells its own extent: a
	// keyed array DERIVES it from the key enum, so nothing outside the array
	// names its size (docs/SPEC-TABLES.md §2.4, §8.1)
	bound := "0"
	switch {
	case f.KeyEnum != "":
		bound = "(int32_t) " + enumMaxConst(f.KeyEnum)
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}

	// `( (T *) 0 )->field`, never a materialised value: an unevaluated member
	// access names the type without an object, where a braced temporary makes
	// the compiler build a whole value of T to take the size of one member.
	elemSize := fmt.Sprintf("(uint32_t) sizeof( ( (%s *) 0 )->%s )", st.Name, member)
	if isArray {
		elemSize = fmt.Sprintf("(uint32_t) sizeof( ( (%s *) 0 )->%s[0] )", st.Name, member)
	}

	countOffset := "0xffffffffu"
	if counted {
		companion := member + "_count"
		if f.Type.Kind == ir.TBytes || (f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString) {
			companion = member + "_length"
		}
		countOffset = fmt.Sprintf("(uint32_t) offsetof( %s, %s )", st.Name, companion)
	}

	if f.Type.Pointer && f.Array == ir.ArrayNone {
		// a pointer's storage IS the reference slot: offset names it,
		// elem_size is the slot's width, and there is no companion
		elemSize = "(uint32_t) sizeof( TableRef )"
		counted = false
		bound = "0"
		countOffset = "0xffffffffu"
	}
	sequence := 0
	if f.IsList() || f.IsMap() {
		sequence = 1
		if f.IsMap() {
			sequence = 2
		}
		isArray = true
		counted = true
		bound = "INT32_MAX"
		elemSize = fmt.Sprintf("(uint32_t) sizeof(%s)", g.sequenceType(f))
		countOffset = fmt.Sprintf("(uint32_t) offsetof(%s,%s.count)", st.Name, member)
	}
	table := "NULL"
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		table = "&" + g.sym(f.Type.Name, "info")
		g.noteRef(f.Type.Name)
	}

	if f.IsMap() {
		table = "&" + g.sym(f.MapEntry.Name, "info")
		kind = tkTable
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
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", len(ref.Variants))
			variants = g.vocabularySymbol(st.Name, f.Name, "variants")
			hasIds = "1"
			arms = "&" + g.vocabularySymbol(st.Name, f.Name, "union")
		}
	}

	presentOffset := "0xffffffffu"
	if f.Type.Optional {
		presentOffset = fmt.Sprintf("(uint32_t) offsetof( %s, %s_present )", st.Name, member)
	}

	keyTypeName, keys, keyMax := "NULL", "NULL", "-1"
	if f.KeyEnum != "" && f.KeyEnumRef != nil {
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keys = g.vocabularySymbol(st.Name, f.Name, "keys")
		keyMax = fmt.Sprintf("%d", f.KeyEnumRef.Max)
		g.noteRef(f.KeyEnum)
	}

	wide := "NULL"
	frac := 0
	if ir.TableScalarKind(f) >= 18 && ir.TableScalarKind(f) <= 29 {
		wide = "&" + g.vocabularySymbol(st.Name, f.Name, "range")
	}
	if f.Type.Kind == ir.TFixed {
		frac = f.Type.FracBits
	}
	place := "NULL"
	if f.IsMap() {
		place = g.sym(f.MapEntry.Name, "json_place")
	}
	pointerColumn := ""
	if g.anyVariable {
		pointerColumn = fmt.Sprintf("%s, ", boolC(f.Type.Pointer))
	}
	g.pf("    { \"%s\", %s, \"%s\", 0x%016xull, %d, %s, %s%s, %s, %s, (uint32_t) offsetof( %s, %s ), %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, \"%s\", %s, %d, %s },\n",
		f.Name, g.descriptorJSON(f), tableFieldTypeName(f), g.descriptorID(id), kind, boolC(isArray), pointerColumn,
		boolC(counted), boolC(f.Type.Optional), bound,
		st.Name, member, elemSize, countOffset, presentOffset, table,
		hasRange, rangeMin, fmt.Sprintf("%s, %s, %d", rangeMax, wide, frac), enumMax, variants, hasIds,
		keyTypeName, keys, keyMax, arms, guard,
		annotationColumns(f.Doc, f.Tags, g.vocabularySymbol(st.Name, f.Name, "tags")), sequence, place)
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

func (g *tableGen) descriptorID(id uint64) uint64 {
	if g.outside {
		return 0
	}
	return id
}
func (g *tableGen) descriptorJSON(f *ir.Field) string {
	if g.outside {
		return "NULL"
	}
	return fmt.Sprintf("%q", ir.TableFieldJsonKey(f))
}

// Each keyed accessor owns its declaration's bound, including after a caller
// receives the containing value through a pointer. Both arguments occur once.
func (g *tableGen) emitKeyedAccessors(st *ir.Struct) {
	for _, f := range st.Fields {
		if f.KeyEnum != "" {
			name := "SCHEMA_" + ir.RustConstName(g.unit.Package) + "_" + ir.RustConstName(st.Name) + "_" + ir.RustConstName(f.Name) + "_AT"
			g.pf("#define %s(value,key) SCHEMA_TABLE_KEYED_AT((value).%s,(key),%s)\n", name, f.Name, enumMaxConst(f.KeyEnum))
		}
	}
}
