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
//   message ordinals         the MessageType tag is the message's index
//   object ordinals          the ObjectType tag likewise
//   contexts                 a context scopes [local] fields out of a view
//   every type's fields      order IS the wire order
//   field names              the TABLE wire's field identity is
//                            fold16(fnv1a32(name)) — a rename moves data
//   type kind/width/sign     the bit width and the encoding
//   declared bounds          the width follows from the range
//   array kind and bounds    the count field's width, and the element count
//   string/bytes capacity    the length field's width
//   float range + resolution the quantized step count
//   fixed I and F            the Q format and the raw bounds
//   quantize scale/bound     the shallow view's per-component width
//   specified defaults       the TABLE wire elides a field at its default
//   branch structure         a guard removes fields from the wire
//   const/reserved/align     literal bits, zero bits, and padding
//   enum max, storage bits   the tag's wire range
//   flags wire bits          the mask width
//
// WHAT IS EXCLUDED, and why each one does NOT move the wire:
//
//   comments, whitespace     no bytes
//   file names and layout    a type's wire does not depend on which file it
//                            is declared in, nor on declaration order
//   enum VARIANT names       ordinals are the wire; renaming Red to Crimson
//                            leaves every byte identical
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

	for _, c := range u.Contexts {
		fmt.Fprintf(&b, "context %s\n", c)
	}
	for i, m := range u.Messages {
		fmt.Fprintf(&b, "message-ordinal %d %s\n", i+1, m)
	}
	for i, o := range u.ObjNames {
		fmt.Fprintf(&b, "object-ordinal %d %s\n", i+1, o)
	}

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
		fmt.Fprintf(&b, "type %s table=%t message=%t\n", st.Name, st.IsTable, st.IsMessage)
		projectItems(&b, st.Items, "  ")
	}

	names = names[:0]
	for n := range u.Objects {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		o := u.Objects[n]
		fmt.Fprintf(&b, "object %s\n", o.Name)
		for _, f := range o.Fields {
			projectField(&b, f, "  ")
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
	// the NAME rides: the table wire's field identity is its name hash, so a
	// rename is a wire-breaking change even though the message wire is
	// unmoved
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
	if f.Round != "" {
		fmt.Fprintf(b, " round=%s", f.Round)
	}
	if f.HasQuantize {
		fmt.Fprintf(b, " quantize scale=%d bound=%d shallow=%t shift=%d",
			f.QuantScale, f.QuantBound, f.FixedShallow, f.QuantShift)
	}
	// view membership changes which fields appear in which generated view,
	// and a [local] field is off the deep wire entirely
	if f.Local {
		b.WriteString(" local")
	}
	if f.Interpolate {
		b.WriteString(" interpolate")
	}
	if f.Context != "" {
		fmt.Fprintf(b, " context=%s", f.Context)
	}
	// the TABLE wire elides a field sitting at its default, so the default is
	// part of the bytes
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
