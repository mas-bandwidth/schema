// THE ID-TABLE WIRE'S IDENTITY AND KIND VOCABULARY (docs/SPEC-TABLES.md §3,
// §5): one hash, at one width, for every vocabulary the wire has.
//
// It lives beside the wire-shape projection's own helpers because it is
// target-independent wire law: the C++ reference emits it into codecs and
// descriptors, the compiler's engine (internal/tablewire) writes and reads it,
// the build version renders it (§20.2) and the tables baseline records it
// (§18.1). Two copies of this mapping would be two wires.
package ir

import (
	"encoding/binary"
	"sort"
)

// TableWireId is the stable identity of a name on this wire: fnv1a64 of the
// name, with no fold and no rebound. A field's wire id, an enum variant's, a
// union arm's and a table's own name id are all this one function
// (docs/SPEC-TABLES.md §5). Width follows the largest population rather than
// each vocabulary's own, because one rule in place of three is worth more than
// the bytes three would have saved, and the bytes it costs are paid ONCE a
// file: an id rides in the id table and a body names it by reference (§3).
//
// A name whose hash is 0 is an ordinary id like any other. What names nothing
// is the REFERENCE 0, a position rather than a hash — the field terminator,
// the enum's None and the union's empty arm.
func TableWireId(name string) uint64 {
	if TableWireIdHook != nil {
		if planted, ok := TableWireIdHook(name); ok {
			return planted
		}
	}
	h := uint64(0xCBF29CE484222325)
	for i := 0; i < len(name); i++ {
		h ^= uint64(name[i])
		h *= 0x00000100000001B3
	}
	return h
}

// TableWireIdHook is the COMPILER TEST HOOK the reserved-id and table-id
// refusals are demonstrated through (docs/SPEC-TABLES.md §3, held by test). No
// declarable name hashes to the reserved node-table id, or to another table's
// id at sixty-four bits, so a control that could turn either refusal red has to
// plant the collision BELOW the hash: it returns the colliding value for one
// named spelling and nothing for every other. Nil in every build but a test's.
var TableWireIdHook func(name string) (planted uint64, ok bool)

// TableFieldWireId is a field's EFFECTIVE id on this wire: the hash of its
// `was = "old_name"` alias when one is declared — so wire identity survives
// the rename — and of its own name otherwise (docs/SPEC-TABLES.md §5).
func TableFieldWireId(f *Field) uint64 {
	if f.WasName != "" {
		return TableWireId(f.WasName)
	}
	return TableWireId(f.Name)
}

// TableNodeWireId is the RESERVED id the node table's field rides under
// (docs/SPEC-TABLES.md §3.1), and the one id the language holds back (§5). No
// name is expected to produce it, and a declared name that does — a `was`
// included — is refused by the checker naming the field (§11).
const TableNodeWireId = uint64(0xFFFFFFFFFFFFFFFF)

// MapKeyWireId and MapValueWireId are the two ids a MAP's generated entry
// carries (docs/SPEC-TABLES.md §2.8, §5). They are constants of the RULE and
// not beside it: the two names take the hash every field name takes, so the
// pair moves when the rule moves and never on its own — which is what lets a
// user's own `table Pair`, a `key K` beside a `value V`, under `[..N]Pair` be
// the map's bytes.
var (
	MapKeyWireId   = TableWireId("key")
	MapValueWireId = TableWireId("value")
)

// BytesWireTypeId and StringWireTypeId are the two RESERVED node type ids a
// BYTE BUFFER's record rides under (docs/SPEC-TABLES.md §2.5, §3.1): the same
// hash a table's name takes, over the keywords `bytes` and `string`, which no
// table can be named.
var (
	BytesWireTypeId  = TableWireId("bytes")
	StringWireTypeId = TableWireId("string")
)

// BlobWireTypeId is the reserved type id a BYTE BUFFER field's node rides
// under: [BytesWireTypeId] for `*bytes`, [StringWireTypeId] for `*string`,
// and 0 for a field that is not a byte buffer.
func BlobWireTypeId(f *Field) uint64 {
	switch {
	case !f.Type.Blob():
		return 0
	case f.Type.Kind == TString:
		return StringWireTypeId
	default:
		return BytesWireTypeId
	}
}

// TableWireForm is the FORM BYTE, and it is the whole header
// (docs/SPEC-TABLES.md §3). It versions the FRAMING §3 describes; a reader
// that meets a byte it does not know refuses the wire by name and never
// reports damage.
const TableWireForm = 1

// The kinds this form adds to §3's closed set. An ENUM rides under its own
// kind carrying the reference to its variant name's id, whatever the
// declaration-side storage width; the ESCAPE is how a later major adds a kind
// without the addition reading as damage; the PAYLOAD-FREE kind is what an
// arm that holds nothing rides under, because an arm header carries a kind.
const (
	TableKindEnum      = 30
	TableKindEscape    = 31
	TableKindNoPayload = 32
)

// TableWireScalarKind is [TableScalarKind] on this form: an ENUM answers its
// own kind 30 rather than the storage-width integer kind the earlier form
// gave it (docs/SPEC-TABLES.md §3). Everything else is unchanged, so the two
// differ in exactly the one place the kind was added for.
func TableWireScalarKind(f *Field) int {
	if f.Type.Kind == TNamed {
		if _, isEnum := f.Type.Ref.(*Enum); isEnum {
			return TableKindEnum
		}
	}
	return TableScalarKind(f)
}

// TableWireFieldKind is [TableFieldKind] on this form: an array is its array
// kind, and a non-array field is [TableWireScalarKind].
func TableWireFieldKind(f *Field) int {
	if k := TableFieldKind(f); k != TableScalarKind(f) {
		return k
	}
	return TableWireScalarKind(f)
}

// TableWireNodeIndex names the two indices that name no record: 0 is null and
// 1 is the ROOT, the body that hosts the node table. Record k (1-based) is
// node index k + 1 (docs/SPEC-TABLES.md §3.1). An index is the same canonical
// LEB128 every length and count on this wire is, so it has no ceiling below
// 2^64 − 1.
const (
	NodeWireIndexNull = uint64(0)
	NodeWireIndexRoot = uint64(1)
)

// TableWireElemKind is [TableElemKind] on this form: an array of enums opens
// its body with kind 30 and carries a variant id reference per element
// (docs/SPEC-TABLES.md §3).
func TableWireElemKind(f *Field) int {
	k := TableElemKind(f)
	if k == 0 {
		return 0
	}
	if f.Array != ArrayNone && f.Type.Kind == TNamed {
		if _, isEnum := f.Type.Ref.(*Enum); isEnum {
			return TableKindEnum
		}
	}
	return k
}

// ArmWireFixedWidth is the byte count an arm of this field type owes under its
// own `L`, and 0 for an arm whose payload is length-shaped or reference-shaped
// (docs/SPEC-TABLES.md §3's arm payload table). A POINTER arm and an ENUM arm
// are reference-shaped on this form: their `L` is the byte count of the
// canonical LEB128 they frame, which the reader checks against the reference
// it read rather than against a constant.
func ArmWireFixedWidth(f *Field) int {
	if f == nil || f.Array != ArrayNone || f.Type.Kind == TString || f.Type.Kind == TBytes {
		return 0
	}
	if f.Type.Pointer {
		return 0 // a node index: canonical LEB128, its own length
	}
	kind := TableWireScalarKind(f)
	switch kind {
	case TableKindTable, TableKindUnion, TableKindEnum:
		return 0
	}
	return TableKindWidth(kind)
}

// TableWireIdCapacity is the number of DISTINCT ids a unit's table closure can
// spell, which is the size a save's id table is declared at
// (docs/SPEC-TABLES.md §3): every field's effective id, every enum variant's
// and union arm's the closure reaches, every table's own name id, the two
// reserved blob ids, and the node table's reserved id. It is a compile-time
// fact, so a save allocates nothing for the table it writes.
func TableWireIdCapacity(u *Unit) int {
	ids := map[uint64]bool{
		TableNodeWireId:  true,
		BytesWireTypeId:  true,
		StringWireTypeId: true,
	}
	var noteUnion func(un *Union)
	noteField := func(f *Field) {
		ids[TableFieldWireId(f)] = true
		if f.IsMap() {
			ids[MapKeyWireId] = true
			ids[MapValueWireId] = true
		}
		if f.KeyEnumRef != nil {
			for _, v := range f.KeyEnumRef.Variants {
				ids[TableWireId(v)] = true
			}
		}
		if f.Type.Kind != TNamed {
			return
		}
		switch ref := f.Type.Ref.(type) {
		case *Enum:
			for _, v := range ref.Variants {
				ids[TableWireId(v)] = true
			}
		case *Union:
			noteUnion(ref)
		}
	}
	seen := map[*Union]bool{}
	noteUnion = func(un *Union) {
		if seen[un] {
			return
		}
		seen[un] = true
		for _, v := range un.Variants {
			ids[TableWireId(v.Name)] = true
			if v.F != nil {
				noteField(v.F)
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
		ids[TableWireId(st.WireName())] = true
		for _, f := range st.Fields {
			noteField(f)
		}
	}
	return len(ids)
}

// WstringWireTypeId is the third RESERVED node type id the message form's
// announcement carries (docs/SPEC-TABLES.md §3.3): the same hash a table's
// name takes, over the keyword `wstring`, which no table can be named. The
// three blob ids ride in the announcement whether or not the unit declares a
// blob, so this one is a constant of the rule rather than of a declaration.
var WstringWireTypeId = TableWireId("wstring")

// TableBuildVersionWireId is the RESERVED id the ANNOUNCEMENT's one required
// field rides under (docs/SPEC-TABLES.md §3.3), and the second id the language
// holds back (§5, §11), beside the node table's [TableNodeWireId]. A reserved
// id in any body but the one whose transport it is, is malformed (§3.1).
const TableBuildVersionWireId = uint64(0xFFFFFFFFFFFFFFFE)

// TableWireMessageForm is the MESSAGE FORM's form byte (docs/SPEC-TABLES.md
// §3.3). A form-2 wire is TWO PARTS, the form byte and the root body, and its
// id table is the CONNECTION's rather than the wire's.
const TableWireMessageForm = 2

// TableVocabularyMaxEntries is the CONFORMING DEFAULT bound on an
// announcement's entry count (docs/SPEC-TABLES.md §3.3): 32 KiB a direction,
// eight times the 500-id unit that is already a large one. A connection's
// table is bounded by nothing the wire carries, so a receiver declares a
// maximum and refuses an announcement above it by name — reading the count,
// comparing it, and touching no entry.
const TableVocabularyMaxEntries = 4096

// TableVocabulary is the whole id vocabulary a unit can put on a form-2 wire,
// in the order §3.3 settles: the reserved build-version id at slot 1, then the
// COOK PROJECTION's order (§20.2) — each record in the order the projection
// renders it and each record's fields in the order the projection renders
// them, then each enum's variants and each union's arms in that same order —
// then the TAIL the projection does not name: the reserved node-table id, the
// three blob type ids as `bytes`, `string`, `wstring`, and every table's own
// name id in the projection's sorted record order.
//
// An id already placed is never placed twice, so a field name three records
// share takes the slot its first appearance gave it. Slot k is entry k counted
// from 1, which is the returned slice's index k-1.
//
// The tail is UNCONDITIONAL: a unit with no pointer announces the node-table
// id and the three blob ids anyway, so that an ordinary edit — a first `*T`,
// or a `*T` at a new type — only ever grows the tail at its end and never
// reshuffles a slot a generated field header carries as a constant.
func TableVocabulary(u *Unit) []uint64 {
	var ids []uint64
	seen := map[uint64]bool{}
	place := func(id uint64) {
		if seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	place(TableBuildVersionWireId)

	closure := TableClosure(u)
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	// THE ORDER IS THE COOK PROJECTION'S, so the members sort by the name the
	// projection prints — which for a map's generated entry is its anonymous
	// key, exactly as [CookProjection] sorts them.
	sort.Slice(names, func(i, j int) bool {
		return ProjectionMemberName(u, names[i]) < ProjectionMemberName(u, names[j])
	})

	enums := map[string]*Enum{}
	flags := map[string]*Flags{}
	unions := map[string]*Union{}
	for _, name := range names {
		st := memberStruct(u, name)
		if st == nil {
			continue
		}
		for _, fl := range layoutRecord(u, st).Fields {
			place(TableFieldWireId(fl.Field))
			// the vocabularies a field REACHES are collected exactly as the
			// projection collects them, so the two orders are one order
			cookFieldLine(u, fl, enums, flags, unions)
		}
	}
	collectArmRefs(enums, flags, unions)
	for _, name := range sortedKeysOf(enums) {
		for _, v := range enums[name].Variants {
			place(TableWireId(v))
		}
	}
	// A `flags` DECLARATION NAMES NOTHING ON THIS WIRE: a mask rides raw, so
	// no variant of it is ever an id (§20.1). It is skipped here rather than
	// left unmentioned, because the projection renders a block for it between
	// the enums and the unions.
	for _, name := range sortedKeysOf(unions) {
		for _, v := range unions[name].Variants {
			place(TableWireId(v.Name))
		}
	}

	place(TableNodeWireId)
	place(BytesWireTypeId)
	place(StringWireTypeId)
	place(WstringWireTypeId)
	for _, name := range names {
		// A MAP'S GENERATED ENTRY IS NOT A TABLE OF THE DECLARATION'S, and
		// its name is generated: it is reached only through the map that
		// generates it, never through a pointer, so its name id is on no wire
		// and hashing it here would let a `was` rename of the holder move a
		// slot (§2.8, §20.2).
		if st := memberStruct(u, name); st == nil || st.MapEntryOf != "" {
			continue
		}
		if u.Tables[name] == nil {
			continue // a `type` in the closure is nested by value and never pointed at
		}
		place(TableWireId(u.Tables[name].WireName()))
	}
	return ids
}

// TableAnnouncement is the unit's ID TABLE MESSAGE, byte for byte
// (docs/SPEC-TABLES.md §3.3). It is an ordinary form-`1` FILE whose body
// carries one field, the BUILD VERSION under the reserved build-version id at
// kind `9`, and whose trailer is the whole connection table: slot `1` the
// reserved id, slots `2` and up the vocabulary.
//
// Every byte of it is settled by the compiler, which is why a backend may emit
// it as a constant byte array and its length rather than as a walk.
func TableAnnouncement(u *Unit) []byte {
	ids := TableVocabulary(u)
	out := make([]byte, 0, 1+11+8*len(ids)+8)
	out = append(out, TableWireForm)
	out = append(out, 1)                   // reference 1: the reserved build-version id, first-use
	out = append(out, uint8(TableKindU64)) // kind 9
	out = binary.LittleEndian.AppendUint64(out, BuildVersion(u))
	out = append(out, 0) // the zero reference that ends the body
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}
