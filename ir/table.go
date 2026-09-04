// Table-wire field identity and the table closure (docs/SPEC-TABLES.md).
// Target-independent, so every backend and any generator outside this module
// derives one id for one field name.
package ir

import (
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

// VariableTables derives the table MODE — the compiler works it out; nobody
// declares it (the owner's ruling: "i wouldn't want to manually have to
// specify this… the compiler can work it out"). A closure member is
// VARIABLE-LENGTH when a pointer appears anywhere in its BY-VALUE closure:
// its own `*T` fields, or those of anything it nests by value. Everything
// else is FIXED-SIZE — a plain struct of known sizeof, exactly as every table
// was before pointers existed, and it gets none of the arena machinery.
//
// The derivation is a least-fixed-point over the by-value edges: pointer
// edges carry no size and never propagate the mode to the POINTING table's
// nester, they only mark the table that declares them.
func VariableTables(u *Unit) map[string]bool {
	closure := TableClosure(u)
	member := func(name string) *Struct {
		if st := u.Tables[name]; st != nil {
			return st
		}
		return u.Structs[name]
	}
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)

	variable := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, name := range names {
			if variable[name] {
				continue
			}
			st := member(name)
			if st == nil {
				continue
			}
			for _, f := range st.Fields {
				if f.Type.Pointer {
					variable[name] = true
					break
				}
				if f.IsMap() {
					// a MAP is a variable edge whatever its key and value are
					// (docs/SPEC-TABLES.md §2.8): its entries live in the
					// arena on the authoring side and in the node's own extent
					// in a region, so the table that declares one rides with
					// the pointers and has no block form
					variable[name] = true
					break
				}
				if f.Type.Kind != TNamed {
					continue
				}
				switch ref := f.Type.Ref.(type) {
				case *Struct:
					if variable[ref.Name] {
						variable[name] = true
					}
				case *Union:
					if unionIsVariable(ref, variable, map[*Union]bool{}) {
						variable[name] = true
					}
				}
				if variable[name] {
					break
				}
			}
			if variable[name] {
				changed = true
			}
		}
	}
	return variable
}

// unionIsVariable reports whether a union makes its holder VARIABLE-LENGTH
// (docs/SPEC-TABLES.md §2.2, §2.6): mode derivation runs through arms, so an
// arm that is a POINTER, that names a variable-length table, or that is a
// union which is itself variable, makes the holder variable.
func unionIsVariable(un *Union, variable map[string]bool, seen map[*Union]bool) bool {
	if seen[un] {
		return false
	}
	seen[un] = true
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		if v.F.Type.Pointer {
			return true
		}
		if v.Type != "" && variable[v.Type] {
			return true
		}
		if inner, ok := v.F.Type.Ref.(*Union); ok && unionIsVariable(inner, variable, seen) {
			return true
		}
	}
	return false
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

// PointerReachable is the set of tables A ROOT's numbering can place: the
// tables some pointer reachable FROM THAT ROOT targets, following by-value
// edges through nested tables, union arms and map entries exactly as the
// numbering walk does (docs/SPEC-TABLES.md §3.1, §2.6, §2.8).
//
// It is narrower than [PointerTargets], which is the unit's whole set, and the
// difference is what a READER owes: a node record whose type id no pointer
// below this root can name is a node this reader cannot place, so it commands
// no region storage and its body is skipped and counted `unknown` (§3.1,
// §6.5). A file never carries one, because a writer writes only the ids its
// own body used; the MESSAGE form can, because a connection's table announces
// every table's name id whether or not a pointer names it (§3.3).
func PointerReachable(u *Unit, root *Struct) map[string]bool {
	named := map[string]bool{}
	visited := map[string]bool{}
	var descend func(st *Struct)
	descend = func(st *Struct) {
		if st == nil || visited[st.Name] {
			return
		}
		visited[st.Name] = true
		for _, f := range st.Fields {
			if f.IsMap() {
				descend(f.MapEntry)
				continue
			}
			if f.Type.Kind != TNamed {
				continue
			}
			if un, isUnion := f.Type.Ref.(*Union); isUnion {
				for _, v := range un.Variants {
					if v.Ref != nil {
						descend(v.Ref)
					}
				}
				continue
			}
			ref, ok := f.Type.Ref.(*Struct)
			if !ok {
				continue
			}
			if f.Type.Pointer {
				named[ref.Name] = true
			}
			descend(ref)
		}
	}
	descend(root)
	return named
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
