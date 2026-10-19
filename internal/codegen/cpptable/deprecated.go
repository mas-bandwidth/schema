package cpptable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- enum variants retired in place (docs/SPEC-TABLES.md §2.15) ----
//
// A variant marked `| deprecated` keeps its constant, its ordinal and its name
// hash for life. A READER that meets it lands the FIELD's declared default (the
// ELEMENT default, None, for an element) and counts nothing; a WRITER refuses a
// value holding it, because the value is the caller's. Everything here is
// emitted only for an enum that carries the marker, so a unit without one
// generates exactly the bytes it did before the rule existed.

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

// emitEnumDeprecated emits TableEnumDeprecated, the one question every
// reader and writer below asks of a value (docs/SPEC-TABLES.md §2.15). It sits
// inside the enum's identity guard, beside TableEnumNamed.
func (g *tableGen) emitEnumDeprecated(e *ir.Enum) {
	if !enumHasDeprecated(e) {
		return
	}
	g.pf("// TableEnumDeprecated: true for a variant RETIRED IN PLACE with `| deprecated`\n")
	g.pf("// (docs/SPEC-TABLES.md §2.15). A reader lands the field's default for it and\n")
	g.pf("// counts nothing; a writer refuses it. The constant stays, so code naming it\n")
	g.pf("// still compiles.\n")
	g.pf("inline bool TableEnumDeprecated( %s value )\n{\n", e.Name)
	g.pf("    switch ( value )\n    {\n")
	for i, v := range e.Variants {
		if variantDeprecated(e, i) {
			g.pf("        case %s::%s: return true;\n", e.Name, v)
		}
	}
	g.pf("        default: return false;\n")
	g.pf("    }\n}\n")
}

// deprecatedLand is what a reader stores over a deprecated variant
// (docs/SPEC-TABLES.md §2.15): the FIELD's declared default, or None for an
// element of an array, a list, a map or a keyed array.
func (g *tableGen) deprecatedLand(f *ir.Field) string {
	if f.Array != ir.ArrayNone || f.KeyEnum != "" || f.IsList() || f.IsMap() {
		return f.Type.Name + "::None"
	}
	return g.fieldDefaultExpr(f)
}

// emitDeprecatedRefuse is a WRITER's refusal of a deprecated variant
// (docs/SPEC-TABLES.md §2.15): the value is the caller's, so it is an invalid
// value and nothing rides. Nothing is emitted for an enum without the marker.
func (g *tableGen) emitDeprecatedRefuse(e *ir.Enum, expr, ind, onBad string) {
	if !enumHasDeprecated(e) {
		return
	}
	g.pf("%sif ( TableEnumDeprecated( %s ) ) { %s } // a deprecated variant is refused (docs/SPEC-TABLES.md §2.15)\n", ind, expr, onBad)
}

// emitDeprecatedLand is a READER's landing, chained as one more `else if` onto
// the enum read that just resolved dst (docs/SPEC-TABLES.md §2.15): a
// deprecated variant becomes the default and counts nothing.
func (g *tableGen) emitDeprecatedLand(f *ir.Field, dst, ind string) {
	if !enumHasDeprecated(enumRef(f)) {
		return
	}
	g.pf("%selse if ( TableEnumDeprecated( %s ) ) { %s = %s; } // a deprecated variant reads as the default (docs/SPEC-TABLES.md §2.15)\n", ind, dst, dst, g.deprecatedLand(f))
}

// variantIdLambda is the descriptor's variant_id for an enum field. The text
// writer refuses a value whose id is 0 and is not None, so answering 0 for a
// deprecated variant is the text form's write refusal (docs/SPEC-TABLES.md
// §2.15, §16.2) on every shape the walk reaches, with no runtime change.
func variantIdLambda(name string, e *ir.Enum) string {
	if enumHasDeprecated(e) {
		return "+[]( uint64_t v ) -> uint64_t { uint64_t id = 0; if ( TableEnumDeprecated( " + name + "( v ) ) ) { return 0; } TableEnumId( " + name + "( v ), id ); return id; }"
	}
	return "+[]( uint64_t v ) -> uint64_t { uint64_t id = 0; TableEnumId( " + name + "( v ), id ); return id; }"
}

// deprecatedWalkable reports whether the by-value walk below reaches a field:
// a scalar, an optional, a fixed or counted array, and a nested table held by
// value. A keyed array, a list, a map, a union and a pointer are not walked;
// their wire, message and fixed codecs apply the rule inline, so the walk is
// only the text reader's landing and the fixed and cook writers' refusal.
func deprecatedWalkable(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed || f.Type.Pointer || f.KeyEnum != "" || f.IsList() || f.IsMap() {
		return false
	}
	return f.Array == ir.ArrayNone || f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted
}

// structHoldsDeprecated reports whether the by-value walk of st can meet a
// deprecated variant, nested tables in members included.
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

// holdsDeprecated is structHoldsDeprecated over the members this file emits.
func (g *tableGen) holdsDeprecated(st *ir.Struct) bool {
	return structHoldsDeprecated(st, g.deprecatedMembers, map[*ir.Struct]bool{})
}

// emitDeprecatedWalks emits, for each member whose by-value walk can meet a
// deprecated variant, <T>HoldsDeprecated (the fixed and cook writers'
// refusal, and the reason the text writer's refusal has a twin for a whole
// value) and <T>LandDeprecated (the text reader's landing, run after the text
// walk stored a variant by name), docs/SPEC-TABLES.md §2.15.
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
	g.pf("// ---- deprecated variants, by value (docs/SPEC-TABLES.md §2.15) ----\n\n")
	for _, st := range walk {
		g.pf("inline bool %sHoldsDeprecated( const %s & value );\n", st.Name, st.Name)
		g.pf("inline void %sLandDeprecated( %s & value );\n", st.Name, st.Name)
	}
	g.pf("\n")
	for _, st := range walk {
		g.pf("// %sHoldsDeprecated: true when a field the walk reaches holds a variant\n", st.Name)
		g.pf("// marked `| deprecated`, which every writer refuses (docs/SPEC-TABLES.md §2.15).\n")
		g.pf("inline bool %sHoldsDeprecated( const %s & value )\n{\n    (void) value;\n", st.Name, st.Name)
		g.emitDeprecatedWalkFields(st, false)
		g.pf("    return false;\n}\n\n")
		g.pf("// %sLandDeprecated: every deprecated variant the walk reaches becomes the\n", st.Name)
		g.pf("// field's declared default, or None for an element (docs/SPEC-TABLES.md §2.15).\n")
		g.pf("inline void %sLandDeprecated( %s & value )\n{\n    (void) value;\n", st.Name, st.Name)
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
					g.pf("%sif ( TableEnumDeprecated( %s ) ) { %s = %s; }\n", ind, expr, expr, g.deprecatedLand(f))
				} else {
					g.pf("%sif ( TableEnumDeprecated( %s ) ) { return true; }\n", ind, expr)
				}
			}
		case *ir.Struct:
			if !g.deprecatedMembers[r] || !g.holdsDeprecated(r) {
				continue
			}
			element = func(expr, ind string) {
				if land {
					g.pf("%s%sLandDeprecated( %s );\n", ind, r.Name, expr)
				} else {
					g.pf("%sif ( %sHoldsDeprecated( %s ) ) { return true; }\n", ind, r.Name, expr)
				}
			}
		default:
			continue
		}
		ind := "    "
		if f.Type.Optional {
			g.pf("    if ( value.%s_present )\n    {\n", f.Name)
			ind = "        "
		}
		switch f.Array {
		case ir.ArrayFixed:
			g.pf("%sfor ( int32_t i = 0; i < %d; i++ )\n%s{\n", ind, f.ArrayBound, ind)
			element("value."+f.Name+"[i]", ind+"    ")
			g.pf("%s}\n", ind)
		case ir.ArrayCounted:
			g.pf("%sfor ( int32_t i = 0; i < value.%s_count && i < %d; i++ )\n%s{\n", ind, f.Name, f.ArrayBound, ind)
			element("value."+f.Name+"[i]", ind+"    ")
			g.pf("%s}\n", ind)
		default:
			element("value."+f.Name, ind)
		}
		if f.Type.Optional {
			g.pf("    }\n")
		}
	}
}
