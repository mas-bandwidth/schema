package ir

// The WIRE SHAPE PROJECTION: a canonical rendering of exactly the facts that
// determine the bytes on the wire, and nothing else.
//
// WHY THIS EXISTS
//
// The protocol id used to hash the schema SOURCE TEXT. That was safe in the
// only direction that matters — it can produce a spurious MISMATCH (two peers
// refuse to talk when they actually could), never a spurious MATCH (two peers
// agree when their bytes differ). But it moved the id for a comment, a blank
// line, a renamed file, or a field added at zero wire cost, and each of those
// costs a coordinated redeploy for nothing.
//
// Hashing the IR structs directly would fix that and introduce the dangerous
// direction: any fact the hash forgot to include becomes two incompatible
// builds shaking hands. So the id hashes a PROJECTION instead — a text a
// human can read, print and diff, listing every wire-affecting fact
// explicitly. A field omitted from this file is a field the id ignores, which
// is exactly the review question you want to be asked.
//
// WHAT IS INCLUDED, and why each one moves the wire:
//
//   package                  peers of different packages are different
//                            protocols; conservative, and can only add ids
//   every type's fields      order IS the wire order
//   field names              FROZEN input (see projectField) — a rename
//                            moves the id
//   type kind/width/sign     the bit width and the encoding
//   declared bounds          the width follows from the range
//   array kind and bounds    the count field's width, and the element count
//   string/bytes capacity    the length field's width
//   float range + resolution the quantized step count
//   fixed I and F            the Q format and the raw bounds
//   specified defaults       FROZEN input (see projectField)
//   branch structure         a guard removes fields from the wire
//   const/reserved/align     literal bits, zero bits, and padding
//   enum max, storage bits   the tag's wire range
//   flags wire bits          the mask width
//   union variant order,     the tag is positional and the payload is the
//     count, payload types   wire (SPEC §4.8)
//
// WHAT IS EXCLUDED, and why each one does NOT move the wire:
//
//   comments, whitespace     no bytes
//   file names and layout    a type's wire does not depend on which file it
//                            is declared in, nor on declaration order
//   enum VARIANT names       ordinals are the wire; renaming Red to Crimson
//                            leaves every byte identical
//   union VARIANT names      the same rule — the ordinal is the wire
//   const declarations       their values are already resolved into the
//                            bounds that appear above
//   type tags, native-type   generation-time only
//     attributes
//
// The projection is VERSIONED. A change to the rendering itself moves every
// id, so the version line makes that deliberate and visible rather than a
// silent break.

import (
	"fmt"
	"sort"
	"strings"
)

// ProjectionVersion is the rendering's own version. BUMPING IT MOVES EVERY
// PROTOCOL ID, so it changes only when the projection must describe something
// it previously did not.
const ProjectionVersion = 1

// WireProjection renders the unit's wire-affecting facts as canonical text.
// Two units with the same projection produce the same bytes; that is the
// property the protocol id rests on, and internal/check tests it.
func WireProjection(u *Unit) string {
	var b strings.Builder

	fmt.Fprintf(&b, "schema-wire-projection %d\n", ProjectionVersion)
	fmt.Fprintf(&b, "package %s\n", u.Package)

	names := make([]string, 0, len(u.Enums))
	for n := range u.Enums {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		e := u.Enums[n]
		// variant NAMES are deliberately absent: the ordinal is the wire
		fmt.Fprintf(&b, "enum %s max=%d storage=%d variants=%d\n", e.Name, e.Max, e.StorageBits, len(e.Variants))
	}

	names = names[:0]
	for n := range u.Flags {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := u.Flags[n]
		fmt.Fprintf(&b, "flags %s wirebits=%d\n", f.Name, f.WireBits)
	}

	names = names[:0]
	for n := range u.Structs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		st := u.Structs[n]
		// `table=false` and `message=false` are FROZEN tokens: tables and
		// messages left the language, and keeping the literals keeps the
		// removals id-neutral for every unit that never declared one.
		// Changing this line is a ProjectionVersion bump, taken deliberately
		// or not at all.
		fmt.Fprintf(&b, "type %s table=false message=false\n", st.Name)
		projectItems(&b, st.Items, "  ")
	}

	// unions: variant ORDER, count and payload type references project —
	// variant names do not (the ordinal is the wire, the enum-variant rule;
	// SPEC §3.1, §4.8). Rendering a section only for units that declare
	// unions leaves every union-free unit's id untouched, so this needed no
	// ProjectionVersion bump: no existing fact renders differently.
	names = names[:0]
	for n := range u.Unions {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		un := u.Unions[n]
		fmt.Fprintf(&b, "union %s max=%d\n", un.Name, un.Max)
		for i, v := range un.Variants {
			fmt.Fprintf(&b, "  variant %d payload=%s\n", i+1, v.Type)
		}
	}

	return b.String()
}

func projectItems(b *strings.Builder, items []Item, ind string) {
	for _, item := range items {
		switch n := item.(type) {
		case *FieldItem:
			projectField(b, n.F, ind)
		case *ConstItem:
			fmt.Fprintf(b, "%sconst-bits value=%s bits=%d\n", ind, n.Value.String(), n.Bits)
		case *ReservedItem:
			fmt.Fprintf(b, "%sreserved bits=%d\n", ind, n.Bits)
		case *AlignItem:
			fmt.Fprintf(b, "%salign\n", ind)
		case *Branch:
			fmt.Fprintf(b, "%sbranch cond=%s neg=%t\n", ind, n.Cond, n.Neg)
			fmt.Fprintf(b, "%s then\n", ind)
			projectItems(b, n.Then, ind+"  ")
			fmt.Fprintf(b, "%s else\n", ind)
			projectItems(b, n.Else, ind+"  ")
		}
	}
}

func projectField(b *strings.Builder, f *Field, ind string) {
	// the NAME rides as a FROZEN token: it projected for the retired table
	// wire (field identity was its name hash), and keeping it keeps every
	// existing id stable — so a rename moves the id even though the message
	// wire is unmoved. Dropping it is a ProjectionVersion bump, taken
	// deliberately or not at all.
	fmt.Fprintf(b, "%sfield %s kind=%d", ind, f.Name, int(f.Type.Kind))

	if f.Type.Kind == TNamed {
		fmt.Fprintf(b, " type=%s", f.Type.Name)
	}
	if f.Type.Width != 0 {
		fmt.Fprintf(b, " width=%d signed=%t", f.Type.Width, f.Type.Signed)
	}
	if f.Type.Size != 0 {
		fmt.Fprintf(b, " size=%d", f.Type.Size)
	}
	if f.Type.Kind == TFixed {
		fmt.Fprintf(b, " I=%d F=%d", f.Type.IntBits, f.Type.FracBits)
	}

	if f.Array != ArrayNone {
		fmt.Fprintf(b, " array=%d bound=%d min=%d", int(f.Array), f.ArrayBound, f.ArrayMin)
	}
	if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
		fmt.Fprintf(b, " intrange=[%s,%s]", f.IntMin.String(), f.IntMax.String())
	}
	if f.HasFloatRange {
		fmt.Fprintf(b, " floatrange=[%v,%v] res=%v steps=%d", f.FMin, f.FMax, f.Resolution, f.Steps)
	}
	// `round=nearest` rides every compressed-float line as a FROZEN token:
	// the rounding spelling left the language (half away from zero is the
	// one fixed-point rule), and keeping the literal keeps every
	// compressed-float unit's id stable. Dropping it is a ProjectionVersion
	// bump, taken deliberately or not at all.
	if f.Round != "" {
		fmt.Fprintf(b, " round=%s", f.Round)
	}
	// the default rides as a FROZEN token (the retired table wire elided a
	// field sitting at its default); it stays part of the bytes to keep
	// existing ids stable
	if f.HasDefault {
		switch {
		case f.DefVariant != "":
			fmt.Fprintf(b, " default=variant:%s", f.DefVariant)
		case f.DefInt != nil:
			fmt.Fprintf(b, " default=%s", f.DefInt.String())
		case f.Type.Kind == TBool:
			fmt.Fprintf(b, " default=%t", f.DefBool)
		default:
			fmt.Fprintf(b, " default=%v", f.DefFloat)
		}
	}
	b.WriteString("\n")
}
