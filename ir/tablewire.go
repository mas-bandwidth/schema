// THE ID-TABLE WIRE'S IDENTITY AND KIND VOCABULARY (docs/SPEC-TABLES.md §3,
// §5): one hash, at one width, for every vocabulary the wire has.
//
// It lives beside the wire-shape projection's own helpers because it is
// target-independent wire law: the C++ reference emits it into codecs and
// descriptors, the compiler's engine (internal/tablewire) writes and reads it,
// the build version renders it (§20.2) and the tables baseline records it
// (§18.1). Two copies of this mapping would be two wires.
package ir

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
	h := uint64(0xCBF29CE484222325)
	for i := 0; i < len(name); i++ {
		h ^= uint64(name[i])
		h *= 0x00000100000001B3
	}
	return h
}

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
		ids[TableWireId(name)] = true
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			noteField(f)
		}
	}
	return len(ids)
}
