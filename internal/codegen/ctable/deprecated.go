package ctable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- enum variants retired in place (docs/SPEC-TABLES.md §2.15) ----
//
// A variant marked `| deprecated` keeps its constant, its ordinal and its name
// hash for life. A READER that meets it lands the FIELD's declared default (the
// ELEMENT default, NONE, for an element) and counts nothing; a WRITER refuses a
// value holding it, because the value is the caller's. C has no overload set,
// so the rule rides inline in each enum switch the codecs already emit: the
// deprecated variant's case refuses on a write and lands the default on a
// read. Everything here is emitted only for an enum that carries the marker,
// so a unit without one generates exactly the bytes it did before.

// enumHasDeprecated reports whether any variant of e carries `| deprecated`
// (docs/SPEC-TABLES.md §2.15).
func enumHasDeprecated(e *ir.Enum) bool {
	if e == nil {
		return false
	}
	for _, d := range e.VariantDeprecated {
		if d {
			return true
		}
	}
	return false
}

// variantDeprecated reads one variant's marker, parallel to Variants
// (docs/SPEC-TABLES.md §2.15).
func variantDeprecated(e *ir.Enum, i int) bool {
	return i >= 0 && i < len(e.VariantDeprecated) && e.VariantDeprecated[i]
}

// deprecatedLand is what a reader stores over a deprecated variant
// (docs/SPEC-TABLES.md §2.15): the FIELD's declared default, or NONE for an
// element of an array, a list, a map or a keyed array.
func (g *tableGen) deprecatedLand(f *ir.Field) string {
	if f.Array != ir.ArrayNone || f.KeyEnum != "" || f.IsList() || f.IsMap() {
		return enumNoneConst(f.Type.Name)
	}
	return g.fieldDefaultExpr(f)
}

// deprecatedTest is the C expression true for a deprecated variant of e held
// in expr (docs/SPEC-TABLES.md §2.15), or "" for an enum without the marker.
func deprecatedTest(e *ir.Enum, expr string) string {
	if !enumHasDeprecated(e) {
		return ""
	}
	test := ""
	for i, v := range e.Variants {
		if !variantDeprecated(e, i) {
			continue
		}
		if test != "" {
			test += " || "
		}
		test += "(" + expr + ") == " + enumConst(e.Name, v)
	}
	return test
}

// deprecatedWalkable reports whether the by-value walk below reaches a field:
// a scalar, an optional, a fixed or counted array, and a nested table held by
// value. A keyed array, a list, a map, a union and a pointer are not walked;
// their wire and message codecs apply the rule inline, so the walk is only the
// text reader's landing and the fixed, cook and text writers' refusal.
func deprecatedWalkable(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed || f.Type.Pointer || f.KeyEnum != "" || f.IsList() || f.IsMap() {
		return false
	}
	return f.Array == ir.ArrayNone || f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted
}

func structHoldsDeprecated(st *ir.Struct, members map[*ir.Struct]bool, seen map[*ir.Struct]bool) bool {
	if st == nil || seen[st] {
		return false
	}
	seen[st] = true
	for _, f := range st.Fields {
		if !deprecatedWalkable(f) {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Enum:
			if enumHasDeprecated(r) {
				return true
			}
		case *ir.Struct:
			if members[r] && structHoldsDeprecated(r, members, seen) {
				return true
			}
		}
	}
	return false
}

// holdsDeprecated reports whether the by-value walk of st can meet a
// deprecated variant (docs/SPEC-TABLES.md §2.15).
func (g *tableGen) holdsDeprecated(st *ir.Struct) bool {
	return structHoldsDeprecated(st, g.deprecatedMembers, map[*ir.Struct]bool{})
}

// emitDeprecatedWalks emits, for each member whose by-value walk can meet a
// deprecated variant, <sym>holds_deprecated_ (the fixed, cook and text
// writers' refusal) and <sym>land_deprecated_ (the text reader's landing, run
// after the text walk stored a variant by name), docs/SPEC-TABLES.md §2.15.
func (g *tableGen) emitDeprecatedWalks(members []*ir.Struct) {
	g.deprecatedMembers = map[*ir.Struct]bool{}
	for _, st := range members {
		g.deprecatedMembers[st] = true
	}
	var walk []*ir.Struct
	for _, st := range members {
		if g.holdsDeprecated(st) {
			walk = append(walk, st)
		}
	}
	if len(walk) == 0 {
		return
	}
	g.pf("/* ---- deprecated variants, by value (docs/SPEC-TABLES.md §2.15) ---- */\n\n")
	for _, st := range walk {
		g.pf("static SCHEMA_UNUSED int %s( const %s * value );\n", g.sym(st.Name, "holds_deprecated"), st.Name)
		g.pf("static SCHEMA_UNUSED void %s( %s * value );\n", g.sym(st.Name, "land_deprecated"), st.Name)
	}
	g.pf("\n")
	for _, st := range walk {
		g.pf("/* 1 when a field the walk reaches holds a variant marked `| deprecated`,\n")
		g.pf("   which every writer refuses (docs/SPEC-TABLES.md §2.15). */\n")
		g.pf("static SCHEMA_UNUSED int %s( const %s * value )\n{\n    (void) value;\n", g.sym(st.Name, "holds_deprecated"), st.Name)
		g.emitDeprecatedWalkFields(st, false)
		g.pf("    return 0;\n}\n\n")
		g.pf("/* every deprecated variant the walk reaches becomes the field's declared\n")
		g.pf("   default, or NONE for an element (docs/SPEC-TABLES.md §2.15). */\n")
		g.pf("static SCHEMA_UNUSED void %s( %s * value )\n{\n    (void) value;\n", g.sym(st.Name, "land_deprecated"), st.Name)
		g.emitDeprecatedWalkFields(st, true)
		g.pf("}\n\n")
	}
}

func (g *tableGen) emitDeprecatedWalkFields(st *ir.Struct, land bool) {
	for _, f := range st.Fields {
		if !deprecatedWalkable(f) {
			continue
		}
		var element func(expr, ind string)
		switch r := f.Type.Ref.(type) {
		case *ir.Enum:
			if !enumHasDeprecated(r) {
				continue
			}
			element = func(expr, ind string) {
				if land {
					g.pf("%sif ( %s ) { %s = %s; }\n", ind, deprecatedTest(r, expr), expr, g.deprecatedLand(f))
				} else {
					g.pf("%sif ( %s ) { return 1; }\n", ind, deprecatedTest(r, expr))
				}
			}
		case *ir.Struct:
			if !g.deprecatedMembers[r] || !g.holdsDeprecated(r) {
				continue
			}
			element = func(expr, ind string) {
				if land {
					g.pf("%s%s( &%s );\n", ind, g.sym(r.Name, "land_deprecated"), expr)
				} else {
					g.pf("%sif ( %s( &%s ) ) { return 1; }\n", ind, g.sym(r.Name, "holds_deprecated"), expr)
				}
			}
		default:
			continue
		}
		ind := "    "
		if f.Type.Optional {
			g.pf("    if ( value->%s_present )\n    {\n", f.Name)
			ind = "        "
		}
		switch f.Array {
		case ir.ArrayFixed:
			g.pf("%s{\n%s    int32_t i;\n%s    for ( i = 0; i < %d; i++ )\n%s    {\n", ind, ind, ind, f.ArrayBound, ind)
			element("value->"+f.Name+"[i]", ind+"        ")
			g.pf("%s    }\n%s}\n", ind, ind)
		case ir.ArrayCounted:
			g.pf("%s{\n%s    int32_t i;\n%s    for ( i = 0; i < value->%s_count && i < %d; i++ )\n%s    {\n", ind, ind, ind, f.Name, f.ArrayBound, ind)
			element("value->"+f.Name+"[i]", ind+"        ")
			g.pf("%s    }\n%s}\n", ind, ind)
		default:
			element("value->"+f.Name, ind)
		}
		if f.Type.Optional {
			g.pf("    }\n")
		}
	}
}
