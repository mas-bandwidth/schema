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
//   enum variant names,      an enum value rides as its declaration ordinal,
//     in declaration order   so the ordered names ARE the ordinal-to-meaning
//                            map; nothing else can see a reorder
//   flags wire bits          the mask width
//   flags variant names,     bit i is variant i, the enum rule exactly
//     in declaration order
//   union variant order,     the tag is positional and the payload is the
//     count, payload types   wire (SPEC §4.8)
//   union ARM names,         two arms of ONE payload type reorder invisibly
//     in declaration order   without them — the enum case exactly, in the
//                            shape where the payload types cannot carry the
//                            order (#491)
//
// WHAT IS EXCLUDED, and why each one does NOT move the wire:
//
//   comments, whitespace     no bytes, DOC comments among them
//   file names and layout    a type's wire does not depend on which file it
//                            is declared in, nor on declaration order
//   const declarations       their values are already resolved into the
//                            bounds that appear above
//   type tags, native-type   generation-time only
//     attributes
//   an enum or a union no    the projection is the CLOSURE over the unit's
//     `type` REACHES         `type` declarations (see [projectionClosure]),
//                            so a declaration only `table` bodies reach
//                            produces no packet byte and moves no id
//
// The projection carries TWO version lines. A change to the RENDERING moves
// every id through [ProjectionVersion]; a compiler change that moves the BYTES
// under an unchanged rendering moves every id through [WireLaw]. Both are
// deliberate and visible rather than a silent break.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
)

// ProjectionVersion is the rendering's own version. BUMPING IT MOVES EVERY
// PROTOCOL ID, so it changes only when the projection must describe something
// it previously did not.
//
// 2: the ordered variant names of every enum and flags declaration, and the
// ordered arm names of every union. Version 1 could not see a reorder of any
// of the three.
//
// 3: the enums and unions are SCOPED BY REACHABILITY (SPEC §3.1). Version 2
// listed the unit; version 3 lists the closure over the unit's `type`
// declarations, so an enum or a union only `table` bodies reach contributes
// nothing to this id. `flags` is held out of the scoping and projects whether
// a `type` reaches it or not.
const ProjectionVersion = 3

// WireLaw is the CODEC LAW's version: the compiler's own rules for turning a
// value into bytes and bytes back into a value, which the rendering above
// cannot see. BUMPING IT MOVES EVERY PROTOCOL ID.
//
// It bumps on any compiler change that can alter, for the same schema and the
// same values, the encoded bytes, the inputs accepted, the reads rejected, the
// defaults materialized, or a numeric conversion. The 2026-08-15 fixed-point
// rounding amendment — ties toward +infinity to ties away from zero — is the
// worked example: the shape was untouched, the bytes on exact negative ties
// were not, and two builds either side of it held the same id.
//
// The invariant it exists to hold: no generated byte and no read decision may
// change for the same schema and input without the protocol id changing
// (SPEC §3.1).
const WireLaw = 1

// WireProjection renders the unit's wire-affecting facts as canonical text.
// Two units with the same projection produce the same bytes; that is the
// property the protocol id rests on, and internal/check tests it.
func WireProjection(u *Unit) string {
	var b strings.Builder

	reached := projectionClosure(u)

	fmt.Fprintf(&b, "schema-wire-projection %d\n", ProjectionVersion)
	fmt.Fprintf(&b, "schema-wire-law %d\n", WireLaw)
	fmt.Fprintf(&b, "package %s\n", u.Package)

	names := make([]string, 0, len(u.Enums))
	for n := range u.Enums {
		if reached.Enums[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		e := u.Enums[n]
		// the ordered variant NAMES are the wire fact: a value rides as its
		// declaration ordinal (implicit None = 0, variants dense from 1), so
		// the names are the only record of which ordinal means what. A
		// reorder is invisible without them — the spurious MATCH this
		// projection exists to refuse — and a rename therefore moves the id.
		fmt.Fprintf(&b, "enum %s max=%d storage=%d variants=%d\n", e.Name, e.Max, e.StorageBits, len(e.Variants))
		for i := range e.Variants {
			v := e.VariantWireName(i)
			fmt.Fprintf(&b, "  variant %d name=%s\n", i+1, v)
		}
	}

	// FLAGS PROJECT UNCONDITIONALLY, and it is the ONE exception to the
	// reachability rule (SPEC §3.1): a flags mask is the table wire's one
	// POSITIONAL vocabulary, a variant's identity is its bit, and no read
	// report can see a bit reassigned — so the protocol id is the only
	// runtime frame that refuses two peers holding different bit
	// assignments. The other positional vocabulary a table could have had is
	// gone rather than excepted: `[E.Max]T` is refused in a table body
	// (docs/SPEC-TABLES.md §2.4), so an enum a table reaches rides by name.
	names = names[:0]
	for n := range u.Flags {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := u.Flags[n]
		// bit i is variant i, so the ordered names carry the bit positions
		// for exactly the enum's reason
		fmt.Fprintf(&b, "flags %s wirebits=%d\n", f.Name, f.WireBits)
		for i, v := range f.Variants {
			fmt.Fprintf(&b, "  bit %d name=%s\n", i, v)
		}
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

	// unions: arm ORDER, count, arm NAMES and payload type references all
	// project. The tag is positional and the payload is the wire (SPEC §4.8),
	// and the names are what the payload types cannot carry alone: two arms of
	// ONE payload type reorder invisibly without them, which is the enum
	// case exactly (#491). A rename therefore moves the id, as it does for an
	// enum or flags variant.
	//
	// A union the closure REACHES projects, and no other: a union only
	// `table` bodies reach has no packet byte to describe (SPEC §3.1). An arm
	// is a FIELD LINE whose facts are wire facts (docs/SPEC-TABLES.md §2.6,
	// §20.8): an arm that names a declared type by value keeps the `payload=`
	// spelling it always had, any other arm renders the field projection, and
	// a PAYLOAD-FREE arm carries `kind=none`. A union with a TABLE ARM is
	// excluded whole, as the table itself is — a `type` body refuses such a
	// union by name, so the closure never reaches one.
	names = names[:0]
	for n := range u.Unions {
		if reached.Unions[n] {
			names = append(names, n)
		}
	}
	for n := range u.TableUnions {
		if reached.Unions[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		un := u.Unions[n]
		if un == nil {
			un = u.TableUnions[n]
		}
		fmt.Fprintf(&b, "union %s max=%d\n", un.Name, un.Max)
		for i, v := range un.Variants {
			switch {
			case v.Void():
				fmt.Fprintf(&b, "  variant %d name=%s kind=none\n", i+1, v.WireName())
			case v.Body():
				fmt.Fprintf(&b, "  variant %d name=%s payload=%s\n", i+1, v.WireName(), v.Type)
			default:
				fmt.Fprintf(&b, "  variant %d name=%s ", i+1, v.WireName())
				projectField(&b, v.F, "")
			}
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
	// existing id stable — so a rename moves the id even though the wire is
	// unmoved. Dropping it is a ProjectionVersion bump, taken deliberately
	// or not at all.
	// A `was` RENAME PROJECTS THE WIRE NAME (docs/SPEC-TABLES.md §5), so the
	// rename that keeps a table-wire identity keeps the protocol id too.
	fmt.Fprintf(b, "%sfield %s kind=%d", ind, TableFieldWireName(f), int(f.Type.Kind))

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
		case f.Type.Kind == TString || f.Type.Kind == TBytes:
			fmt.Fprintf(b, " default=bytes:%x", f.DefBytes)
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

// Reached is the set of declarations the unit's `type` declarations reach —
// the projection's scope (SPEC §3.1). `flags` is absent from it because
// `flags` projects unconditionally and needs no closure to say so.
type Reached struct {
	Enums  map[string]bool
	Unions map[string]bool
}

// projectionClosure walks SPEC §3.1's EIGHT EDGES from every `type` in the
// unit and returns what they reach.
//
// EVERY `type` IS A ROOT, because the language has no way to say which types
// go on a wire: a `type` is a struct and its codec, every one of them is
// emitted, and any of them may be handed to a writer. So an unused helper
// type projects like a used one, and only the enums and unions are scoped.
//
// The edges, in the page's order, and where each one lives below:
//
//  1. a field's named TYPE, in any spelling — `T`, `?T`, `*T`  (reachField)
//  2. an array's ELEMENT type, the same set one level in       (reachField)
//  3. an array's BOUND, where the bound names an enum          (reachExpr)
//  4. a keyed array's KEY enum, `[E]T`                         (reachField)
//  5. a CONSTANT's value expression, followed through          (reachExpr)
//  6. a union ARM's payload type and the arm's field facts     (reachUnion)
//  7. the members of BOTH SIDES of a branch                    (reachItems)
//  8. every item of a reached `type`, transitively             (reachStruct)
//
// A MISSED EDGE IS THE DANGEROUS DIRECTION — a declaration a packet byte
// reaches that the walk does not, which is two incompatible builds shaking
// hands — so internal/check's projection test holds one negative control per
// edge kind, each a declaration reachable only through that edge.
func projectionClosure(u *Unit) Reached {
	w := closureWalk{
		u:       u,
		reached: Reached{Enums: map[string]bool{}, Unions: map[string]bool{}},
		structs: map[string]bool{},
		consts:  map[string]bool{},
	}
	for name := range u.Structs {
		w.reachStruct(name)
	}
	return w.reached
}

type closureWalk struct {
	u       *Unit
	reached Reached
	structs map[string]bool // visited, so a cycle of pointers terminates
	consts  map[string]bool // visited, so a const chain terminates
}

// reachStruct descends into a `type` the walk has reached — EDGE 8, which is
// what makes the walk a closure rather than one step.
func (w *closureWalk) reachStruct(name string) {
	if w.structs[name] {
		return
	}
	w.structs[name] = true
	if st := w.u.Structs[name]; st != nil {
		w.reachItems(st.Items)
	}
}

// reachItems walks a body's items. A BRANCH reaches through BOTH SIDES —
// EDGE 7: an untaken branch's members are wire facts (SPEC §4.4), so an
// `else` body reaches what a `then` body does.
func (w *closureWalk) reachItems(items []Item) {
	for _, item := range items {
		switch n := item.(type) {
		case *FieldItem:
			w.reachField(n.F)
		case *Branch:
			w.reachItems(n.Then)
			w.reachItems(n.Else)
		}
	}
}

func (w *closureWalk) reachField(f *Field) {
	if f == nil {
		return
	}
	// EDGES 1 AND 2 are one lookup: a field's named type is its ELEMENT type
	// when the field is an array, in every spelling the language has.
	if f.Type.Kind == TNamed {
		w.reachDecl(f.Type.Name)
	}
	// EDGE 4: a keyed array's key enum. In a `type` body `[E]T` lowers to the
	// positional `[E.Max]T` (docs/SPEC-TABLES.md §2.4), so the key enum is an
	// extent by another name and edge 3's reason is its reason.
	if f.KeyEnum != "" {
		w.reachDecl(f.KeyEnum)
	}
	// EDGE 3, and EDGE 5 through it: the bound expression, followed to what
	// it names. `[E.Max]T` puts E's variant count in the extent AND makes
	// slot i variant i+1's, so an enum reached by nothing but a bound still
	// decides what every element MEANS — the edge a walk that followed only
	// types would miss.
	if f.Array != ArrayNone {
		w.reachExpr(f.ArrayExpr)
	}
	// a map's GENERATED ENTRY is a table of the closure (docs/SPEC-TABLES.md
	// §2.8) and a `type` body cannot spell a map, so this descent reaches
	// nothing today. It costs nothing and it is the safe direction: a walk
	// that met a map field and stepped over it would be a missed edge.
	if f.MapEntry != nil {
		w.reachItems(f.MapEntry.Items)
	}
}

// reachDecl reaches whatever a name denotes. A `flags` declaration is not
// recorded: it projects unconditionally, so the closure has nothing to say
// about it.
func (w *closureWalk) reachDecl(name string) {
	if w.u.Enums[name] != nil {
		w.reached.Enums[name] = true
		return
	}
	if un := w.u.Unions[name]; un != nil {
		w.reachUnion(name, un)
		return
	}
	if un := w.u.TableUnions[name]; un != nil {
		w.reachUnion(name, un)
		return
	}
	if w.u.Structs[name] != nil {
		w.reachStruct(name)
	}
}

// reachUnion records a union and descends through its arms — EDGE 6: an
// arm's payload type and the arm's own field facts, which re-enter the walk
// at edge 1.
//
// A UNION WITH A TABLE ARM IS EXCLUDED WHOLE, as the table itself is: it has
// no packet encoding to project (SPEC §3.1, docs/SPEC-TABLES.md §2.6). Such a
// union is a table-closure construct, which a `type` body refuses by name, so
// no closure walk reaches one and the exclusion needs no rule of its own.
func (w *closureWalk) reachUnion(name string, un *Union) {
	if w.reached.Unions[name] {
		return
	}
	w.reached.Unions[name] = true
	for _, v := range un.Variants {
		if v.Ref != nil {
			w.reachStruct(v.Ref.Name)
		}
		w.reachField(v.F)
	}
}

// reachExpr follows a bound expression to the declarations it names — EDGE 3
// where the bound spells the enum itself, EDGE 5 where a `const` stands
// between. A const declaration does not project, because its value is already
// resolved into the bound that does, so the walk reaches the enum BEHIND a
// `const N = E.Max` exactly as it reaches one spelled at the bound.
func (w *closureWalk) reachExpr(e Expr) {
	switch e := e.(type) {
	case *ast.IdentExpr:
		// a bare name at a bound is a CONSTANT: a bound that names a declared
		// enum is the keyed spelling `[E]T`, which the checker resolves into
		// KeyEnum, and edge 4 carries it from there.
		if c := w.u.Consts[e.Name]; c != nil && !w.consts[e.Name] {
			w.consts[e.Name] = true
			w.reachExpr(c.Expr)
		}
	case *ast.MaxExpr:
		// `E.Max` and `E.Count` name an enum or a flags declaration, and
		// `<Union>Type.Max` the generated tag set of a union (SPEC §4.2)
		w.reachDecl(e.Enum)
		if base, ok := strings.CutSuffix(e.Enum, "Type"); ok {
			if un := w.u.Unions[base]; un != nil {
				w.reachUnion(base, un)
			}
		}
	case *ast.UnaryExpr:
		w.reachExpr(e.X)
	case *ast.BinaryExpr:
		w.reachExpr(e.X)
		w.reachExpr(e.Y)
	case *ast.ParenExpr:
		w.reachExpr(e.X)
	}
}
