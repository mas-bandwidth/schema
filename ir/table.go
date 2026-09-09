// Table-wire field identity and the table closure (docs/SPEC-TABLES.md).
// Target-independent, so every backend and any generator outside this module
// derives one id for one field name.
package ir

import (
	"slices"
	"sort"
	"strings"
)

// FieldId is the stable TABLE-wire identity of a field name:
// fold16(fnv1a32(name)), rebounding 0 (the terminator) to 1.
func FieldId(name string) uint16 {
	h := uint32(0x811C9DC5)
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 0x01000193
	}
	id := uint16((h ^ (h >> 16)) & 0xFFFF)
	if id == 0 {
		id = 1
	}
	return id
}

// VariantId is the stable TABLE-wire identity of an enum variant or a union
// arm: the same fold a field name takes, over the variant's own name. An
// enum's implicit None and a union's empty arm ride as 0, which the fold's
// rebound keeps free of every declared name.
func VariantId(name string) uint16 { return FieldId(name) }

// TableTypeId is a node record's TYPE ID (docs/SPEC-TABLES.md §3.1): the target
// table's NAME under fnv1a64, with a result of 0 rebounding to 1.
//
// Sixty-four bits because a table name is the one vocabulary scoped to a WHOLE
// unit closure rather than to a single table or enum, so its collision
// population is the largest on the wire; two tables in one closure whose ids
// collide are still a compile error naming both (§11). It is the id a node
// RECORD carries on the wire and the id a region's node directory carries
// beside every offset (§6.3), so the wire and the cook name a node's type with
// one number.
func TableTypeId(name string) uint64 {
	h := uint64(0xCBF29CE484222325)
	for i := 0; i < len(name); i++ {
		h ^= uint64(name[i])
		h *= 0x00000100000001B3
	}
	if h == 0 {
		h = 1
	}
	return h
}

// BytesTypeId and StringTypeId are the two RESERVED node type ids a BYTE
// BUFFER's record rides under (docs/SPEC-TABLES.md §2.5, §3.1): the same fold
// a table's name takes, over the keywords `bytes` and `string`, which no table
// can be named — so the two sit in every closure's id population beside the
// tables' and separate a `*bytes` blob from a `*string` blob as `bytes(N)`
// and `string(N)` are separated on the wire.
var (
	BytesTypeId  = TableTypeId("bytes")
	StringTypeId = TableTypeId("string")
)

// BlobTypeId is the reserved type id a BYTE BUFFER field's node rides under:
// [BytesTypeId] for `*bytes`, [StringTypeId] for `*string`, and 0 for a field
// that is not a byte buffer.
func BlobTypeId(f *Field) uint64 {
	switch {
	case !f.Type.Blob():
		return 0
	case f.Type.Kind == TString:
		return StringTypeId
	default:
		return BytesTypeId
	}
}

// BlobFields lists the BYTE BUFFER fields of a unit's table closure as
// `Table.field`, sorted — the names a backend that does not carry the
// construct puts in its refusal (docs/SPEC-TABLES.md §11).
func BlobFields(u *Unit) []string {
	var out []string
	for name := range TableClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.Type.Blob() {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// NodeTableFieldId is the RESERVED field id the node table rides under
// (docs/SPEC-TABLES.md §3.1). §5's fold reaches it and ordinary names land there, so
// the compiler refuses a field name — or a `was` — whose id does (§11).
const NodeTableFieldId = uint16(0xFFFF)

// NodeIndexNull, NodeIndexRoot are the two node indices that name no record:
// `0` is null and `1` is the ROOT, the body that hosts the node table. Record
// `k` (1-based) is node index `k + 1` (docs/SPEC-TABLES.md §3.1).
const (
	NodeIndexNull = uint32(0)
	NodeIndexRoot = uint32(1)
)

// TableFieldId is a field's EFFECTIVE table-wire id: the hash of its
// `was = "old_name"` alias when one is declared — so wire identity survives
// the rename — and of its own name otherwise.
func TableFieldId(f *Field) uint16 {
	if f.WasName != "" {
		return FieldId(f.WasName)
	}
	return FieldId(f.Name)
}

// TableFieldJsonKey is a field's key in the text form (docs/SPEC-TABLES.md §16.3):
// the `json = "key"` attribute where one is declared, and the field's own name
// otherwise. Independent of the wire id — a `was` rename and a `json` key are
// two different vocabularies over one field.
func TableFieldJsonKey(f *Field) string {
	if f.JsonKey != "" {
		return f.JsonKey
	}
	return f.Name
}

// VariableTables reports the table MODE, and the mode is DECLARED
// (docs/SPEC-TABLES.md §2.2). `fixed table T` declares the FIXED class — a
// plain struct of known sizeof that gets none of the arena machinery — and a
// plain `table T` is the VARIABLE wire, whatever its fields happen to be.
// Nothing is inferred and nothing is guessed, which is the owner's ruling:
// "otherwise, it could be a bit of a guess whether the table is fixed or
// variable, couldn't it? we don't want to surprise the user."
//
// So this is the COMPLEMENT of the declared flag, and every emitter switches
// on it at once. The old least-fixed-point derivation survives as
// [FixedClosureBreaks], which is no longer a mode but a CHECK: it is what the
// compiler refuses a `fixed table` by, naming the field and the table it
// breaks, so a feature that stops a table being fixed is a compile error at
// the declaration rather than a silent change of wire.
//
// A closure member that is not a table — a `type` held by value — carries no
// declaration of its own and is never variable: a type body takes no pointer,
// no map and no unbounded array, so nothing in one can make it so.
func VariableTables(u *Unit) map[string]bool {
	variable := map[string]bool{}
	for name := range TableClosure(u) {
		// ONLY THE VARIABLE NAMES RIDE IN THE MAP, as they did when the mode
		// was derived: a caller asks `variable[name]` and reads false for
		// everything else, and `len(variable) == 0` is still "this unit is
		// fixed throughout", which is the question the zero-cost gate asks.
		if st := u.Tables[name]; st != nil && !st.Fixed {
			variable[name] = true
		}
	}
	return variable
}

// FixedBreak is one construct in a fixed table's BY-VALUE closure that makes
// a body variable size — the thing a `fixed table` is refused by
// (docs/SPEC-TABLES.md §2.2, §11).
type FixedBreak struct {
	Owner string // the closure member that declares the field
	Field string // the field's own name
	Why   string // what it is, and why a fixed body cannot hold it
	Refs  string // the spec references the refusal cites
}

// At is the break's field as a refusal spells it: `Owner.field`.
func (b FixedBreak) At() string { return b.Owner + "." + b.Field }

// FixedClosureBreaks walks a table's BY-VALUE closure and returns every
// construct that makes a body variable size (docs/SPEC-TABLES.md §2.2). It is
// the CHECK behind the `fixed table` refusal, and the only thing left of the
// old derivation: "Nests by value" reaches through every by-value edge there
// is — a plain nested table, an element of a bounded array, an element of an
// enum-keyed array, a member of a guarded (`if`) group, an optional's value
// (§2.3), and a UNION ARM that is a table (§2.6).
//
// A nested FIXED table stops the walk: its own closure is checked at its own
// declaration, so a break is reported once, where it is declared. A nested
// PLAIN table is itself a break, because the variable wire has no size a
// fixed body could hold.
//
// `member` resolves a closure name to its struct; the checker passes its own
// resolver, because the unit is not assembled yet when this runs.
func FixedClosureBreaks(member func(string) *Struct, name string) []FixedBreak {
	var out []FixedBreak
	seen := map[string]bool{}
	seenUnion := map[*Union]bool{}
	var walk func(owner string)
	var walkUnion func(un *Union)

	walkLine := func(owner string, f *Field, arm bool) {
		where, kinds := "", "docs/SPEC-TABLES.md §2.2"
		if arm {
			where, kinds = " arm", "docs/SPEC-TABLES.md §2.2, §2.6"
		}
		add := func(why, refs string) {
			out = append(out, FixedBreak{owner, f.Name, why, refs})
		}
		switch {
		case f.Guard != "":
			// THE LOOKBACK CONDITIONAL IS THERE TO MAKE A BODY VARIABLE SIZE
			// (SPEC §4.5): what rides depends on a value read earlier in the
			// same body, so two values of one table have two sizes.
			add("sits in an `if` branch, and the lookback conditional is there to make a body variable size — what rides depends on a value read earlier in the same body, so two values of the table have two sizes", "docs/SPEC-TABLES.md §2.2, SPEC §4.5")
		case f.IsMap():
			add("is a map"+where+", and a map's entries live in the arena on the authoring side and in the node's own extent in a region — the count is the data's, not the declaration's", kinds+", §2.8")
		case f.IsList():
			add("is an unbounded array"+where+", and its elements live in the arena on the authoring side and in the node's own extent in a region — the count is the data's, not the declaration's", kinds+", §2.9")
		case f.Type.Blob():
			add("is a byte buffer"+where+" at its used size, which is a pointer at a blob node — the bytes are the data's, not the declaration's; `string(N)` and `bytes(N)` are the bounded spellings and have a size the declaration states", kinds+", §2.5")
		case f.Type.Pointer:
			add("is a pointer"+where+", and a pointer is an arena allocation and a node record — the lifecycle the fixed class exists to not pay for", kinds+", §2.1")
		case f.Type.Kind != TNamed:
		default:
			switch ref := f.Type.Ref.(type) {
			case *Struct:
				switch {
				case ref.IsTable && !ref.Fixed:
					held := "holds the plain table " + ref.Name + " by value"
					if arm {
						held = "is an arm holding the plain table " + ref.Name
					}
					add(held+", and a plain `table` is the variable wire whatever its fields are — a fixed body has no size to hold one at", kinds)
				case ref.IsTable:
					// A NESTED FIXED TABLE STOPS THE WALK: its own closure is
					// checked at its own declaration, so a break is reported
					// once, where a reader can edit it.
				default:
					walk(ref.Name)
				}
			case *Union:
				walkUnion(ref)
			}
		}
	}

	// A UNION ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6), so the arms take
	// the field rules unchanged, reported under the UNION's own name — that is
	// the declaration a reader has to edit.
	walkUnion = func(un *Union) {
		if seenUnion[un] {
			return
		}
		seenUnion[un] = true
		for _, v := range un.Variants {
			if v.F != nil {
				walkLine(un.Name, v.F, true)
			}
		}
	}

	walk = func(owner string) {
		if seen[owner] {
			return
		}
		seen[owner] = true
		st := member(owner)
		if st == nil {
			return
		}
		for _, f := range st.Fields {
			walkLine(owner, f, false)
		}
	}

	walk(name)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Field < out[j].Field
	})
	return out
}

// PointerTargets is the set of tables some pointer field targets — the tables
// that need an arena allocation surface (Builder.Alloc<T>()) and a cooked
// accessor. A table can be a pointer target and a root at once.
func PointerTargets(u *Unit) map[string]bool {
	targets := map[string]bool{}
	for name := range TableClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.Type.Pointer && f.Type.Kind == TNamed {
				targets[f.Type.Name] = true // a byte buffer names no table (§2.5)
			}
			// a POINTER ARM targets a node exactly as a pointer field does
			// (§2.6): the arm is the edge, so its pointee needs the same
			// allocation surface and the same cooked accessor
			if un, ok := f.Type.Ref.(*Union); ok && f.Type.Kind == TNamed {
				unionPointerTargets(un, targets, map[*Union]bool{})
			}
		}
	}
	return targets
}

// reachableEdges walks a root's closure in the numbering walk's order
// (docs/SPEC-TABLES.md §3.1) and calls edge at every FIELD LINE that names a
// node: a `*T` field or element, a POINTER ARM (§2.6) and a byte buffer
// (§2.5). It descends every by-value edge there is, a nested table, an
// array's element, a map's entry (§2.8), a list's element (§2.9) and a union's
// arms, nested unions included, and it descends a pointee too, because the
// pointers inside a node the root names are the root's to name. Each
// declaration is entered once.
func reachableEdges(root *Struct, edge func(f *Field)) {
	visited := map[string]bool{}
	seen := map[*Union]bool{}
	var descend func(st *Struct)
	var line func(f *Field)
	line = func(f *Field) {
		if f.IsMap() {
			descend(f.MapEntry)
			return
		}
		if f.Type.Pointer {
			edge(f)
		}
		if f.Type.Kind != TNamed {
			return
		}
		switch ref := f.Type.Ref.(type) {
		case *Struct:
			descend(ref)
		case *Union:
			if seen[ref] {
				return
			}
			seen[ref] = true
			for _, v := range ref.Variants {
				if v.F != nil {
					line(v.F)
				}
			}
		}
	}
	descend = func(st *Struct) {
		if st == nil || visited[st.Name] {
			return
		}
		visited[st.Name] = true
		for _, f := range st.Fields {
			line(f)
		}
	}
	descend(root)
}

// PointerReachable is the set of tables A ROOT's numbering can place, in
// FIRST-VISIT order: the tables some pointer reachable FROM THAT ROOT targets,
// a pointer field's, an array's or a list's element's, or a POINTER ARM's,
// found by the walk the numbering takes (docs/SPEC-TABLES.md §3.1, §2.6, §2.8,
// §2.9).
//
// It is narrower than [PointerTargets], which is the unit's whole set, and the
// difference is what a READER owes: a node record whose type id no pointer
// below this root can name is a node this reader cannot place, so it commands
// no region storage and its body is skipped and counted `unknown` (§3.1,
// §6.5). A file never carries one, because a writer writes only the ids its
// own body used; the MESSAGE form can, because a connection's table announces
// every table's name id whether or not a pointer names it (§3.3).
func PointerReachable(root *Struct) []*Struct {
	named := map[string]bool{}
	var out []*Struct
	reachableEdges(root, func(f *Field) {
		if f.Type.Blob() {
			return
		}
		ref, ok := f.Type.Ref.(*Struct)
		if !ok || named[ref.Name] {
			return
		}
		named[ref.Name] = true
		out = append(out, ref)
	})
	return out
}

// PointerReachableBlobs is [PointerReachable]'s answer for the two RESERVED
// node type ids (docs/SPEC-TABLES.md §2.5): whether a `*bytes` edge and
// whether a `*string` edge sits anywhere below this root, over the same walk.
//
// A blob node is a pointer's pointee exactly as a table's node is, so the same
// rule decides it: a `*bytes` record under a root no `*bytes` pointer sits
// below is a node this reader cannot place, and it commands no region storage,
// its body is skipped and one `unknown` is counted (§3.1, §6.5). A file never
// carries one; the MESSAGE form can, because §3.3's unconditional tail
// announces both reserved ids whether or not the root names them.
func PointerReachableBlobs(root *Struct) (bytes bool, str bool) {
	reachableEdges(root, func(f *Field) {
		switch {
		case !f.Type.Blob():
		case f.Type.Kind == TString:
			str = true
		default:
			bytes = true
		}
	})
	return bytes, str
}

// unionPointerTargets adds every table a POINTER ARM of un targets, through
// nested union arms too (docs/SPEC-TABLES.md §2.6).
func unionPointerTargets(un *Union, targets map[string]bool, seen map[*Union]bool) {
	if seen[un] {
		return
	}
	seen[un] = true
	for _, v := range un.Variants {
		if v.F == nil || v.F.Type.Kind != TNamed {
			continue
		}
		if v.F.Type.Pointer {
			targets[v.F.Type.Name] = true // a byte buffer names no table (§2.5)
		}
		if inner, ok := v.F.Type.Ref.(*Union); ok {
			unionPointerTargets(inner, targets, seen)
		}
	}
}

// TableClosure is the set of structs that carry table codecs and reflection
// descriptors: every `table` declaration plus every struct reachable from one
// through fields (nested tables and types, array elements, union payloads),
// transitively. Plain types outside the closure stay packet-wire only.
func TableClosure(u *Unit) map[string]bool {
	closure := map[string]bool{}
	seenUnion := map[*Union]bool{}
	var walk func(name string)
	var walkUnion func(un *Union)
	walkUnion = func(un *Union) {
		if seenUnion[un] {
			return
		}
		seenUnion[un] = true
		// an ARM reaches a declaration exactly as a field does (§2.6): by
		// value, through a pointer, or as an array's element — and an arm
		// that is another union reaches through that union's own arms
		for _, v := range un.Variants {
			if v.F == nil || v.F.Type.Kind != TNamed {
				continue
			}
			switch ref := v.F.Type.Ref.(type) {
			case *Struct:
				walk(ref.Name)
			case *Union:
				walkUnion(ref)
			}
		}
	}
	walk = func(name string) {
		if closure[name] {
			return
		}
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			return
		}
		closure[name] = true
		for _, f := range st.Fields {
			if f.IsMap() {
				// the GENERATED ENTRY is a real table of the closure
				// (docs/SPEC-TABLES.md §2.8), and the value it carries is
				// reached through it like any nested table's fields
				walk(f.MapEntry.Name)
				continue
			}
			if f.Type.Kind != TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *Struct:
				walk(ref.Name)
			case *Union:
				walkUnion(ref)
			}
		}
	}
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		walk(name)
	}
	return closure
}

// DartMemberName is the one true mapping from a schema field name to its Dart
// member spelling: lower_snake_case -> lowerCamelCase, the first-letter-lowered
// form of [GoExportName]. The checker's claim over the Dart backend's table
// verbs and the Dart emitters must share it, or the check lies.
func DartMemberName(name string) string {
	s := GoExportName(name)
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// TableUnionArrays names every ARRAY OF UNIONS a table closure holds
// (docs/SPEC-TABLES.md §2.6), as `Member.field`, sorted: the fields a backend
// without the form refuses a unit over, by name.
func TableUnionArrays(u *Unit) []string {
	closure := TableClosure(u)
	var out []string
	for name := range closure {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.Array == ArrayNone || f.Type.Kind != TNamed {
				continue
			}
			if _, isUnion := f.Type.Ref.(*Union); isUnion {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// TableVoidArmUnions names every union in a unit's TABLE CLOSURE that carries
// a PAYLOAD-FREE ARM (SPEC §4.8), sorted. Such a union has a packet wire —
// the tag alone — so it is not a table-closure construct, and a port that
// carries table codecs still has to know the arm has no storage: the ports
// refuse a unit that puts one in a table closure, by name
// (docs/SPEC-TABLES.md §2.6, §11).
func TableVoidArmUnions(u *Unit) []string {
	seen := map[string]bool{}
	var out []string
	var note func(un *Union)
	note = func(un *Union) {
		if seen[un.Name] {
			return
		}
		seen[un.Name] = true
		for _, v := range un.Variants {
			if v.Void() {
				out = append(out, un.Name)
				break
			}
		}
		for _, v := range un.Variants {
			if v.F != nil && v.F.Type.Kind == TNamed {
				if inner, ok := v.F.Type.Ref.(*Union); ok {
					note(inner)
				}
			}
		}
	}
	for name := range TableClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.Type.Kind != TNamed {
				continue
			}
			if un, ok := f.Type.Ref.(*Union); ok {
				note(un)
			}
		}
	}
	sort.Strings(out)
	return out
}

// TableClosureVocabulary is the set of enum, flags and union declarations a
// TABLE CLOSURE reaches, keyed by name. §5's refusals are scoped to the
// closure throughout — the variant refusal reaches a vocabulary through a
// closure member's field, and through the KEY of a closure member's keyed
// array — so a declaration this set does not carry has variant ids nothing
// ever checked, and every id column the view gives it is the reserved id
// (docs/SPEC-TABLES.md §8.2).
func TableClosureVocabulary(u *Unit) map[string]bool {
	closure := TableClosure(u)
	out := map[string]bool{}
	seen := map[string]bool{}
	var reach func(fields []*Field)
	reachUnion := func(un *Union) {
		if seen[un.Name] {
			return
		}
		seen[un.Name] = true
		out[un.Name] = true
		for _, arm := range un.Variants {
			if arm.F != nil {
				reach([]*Field{arm.F})
			}
		}
	}
	reach = func(fields []*Field) {
		for _, f := range fields {
			// an enum-keyed array's KEY reaches the enum as surely as a field
			// of that type does (docs/SPEC-TABLES.md §2.4)
			if f.KeyEnumRef != nil {
				out[f.KeyEnumRef.Name] = true
			}
			if f.Type.Kind != TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *Enum:
				out[ref.Name] = true
			case *Flags:
				out[ref.Name] = true
			case *Union:
				reachUnion(ref)
			}
		}
	}
	for name := range closure {
		if st, ok := u.Structs[name]; ok {
			reach(st.Fields)
		}
		if st, ok := u.Tables[name]; ok {
			reach(st.Fields)
		}
	}
	return out
}

// ClosureHoldsArenaEdge reports whether a member's BY-VALUE closure declares a
// POINTER, a MAP (§2.8) or an UNBOUNDED ARRAY (§2.9) — the three constructs
// whose contents live in the arena on the authoring side and in the node's own
// extent in a region, and, for a pointer, can be SHARED between two slots.
//
// It is not the MODE. The mode is declared (§2.2) and says which wire a table
// is on; this says whether a TEXT needs labels for shared nodes, which is the
// question §16.7 and §17.2's directory rule actually turn on. The two used to
// be one answer because the mode was derived from these same three edges; a
// guarded table is now variable without holding any of them, so the text asks
// its own question rather than reading a mode that no longer means this.
func ClosureHoldsArenaEdge(member func(string) *Struct, name string) bool {
	seen := map[string]bool{}
	seenUnion := map[*Union]bool{}
	var walk func(string) bool
	var line func(*Field) bool
	var union func(*Union) bool
	line = func(f *Field) bool {
		if f.Type.Pointer || f.IsMap() || f.IsList() {
			return true
		}
		if f.Type.Kind != TNamed {
			return false
		}
		switch ref := f.Type.Ref.(type) {
		case *Struct:
			return walk(ref.Name)
		case *Union:
			return union(ref)
		}
		return false
	}
	union = func(un *Union) bool {
		if seenUnion[un] {
			return false
		}
		seenUnion[un] = true
		for _, v := range un.Variants {
			if v.F != nil && line(v.F) {
				return true
			}
		}
		return false
	}
	walk = func(n string) bool {
		if seen[n] {
			return false
		}
		seen[n] = true
		st := member(n)
		if st == nil {
			return false
		}
		return slices.ContainsFunc(st.Fields, line)
	}
	return walk(name)
}
