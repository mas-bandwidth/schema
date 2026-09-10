// THE FIXED FORM'S WIRE LAW (docs/SPEC-TABLES.md §3.4), in one place because
// it produces BYTES and every port has to produce the same ones.
//
// A fixed-table record is an eight-byte hash of the writer's LAYOUT and then
// the values in declared order, every field at its DECLARED STORAGE WIDTH,
// nothing padded between fields. Three things decide those bytes: the constant
// size of every field, the pre-order walk that becomes the LAYOUT, and the hash
// over the layout. A second copy of any of them in a second backend is a second
// answer waiting to happen, so they live here and the backends render rather
// than recompute.
//
// THE LAYOUT is what form 1 calls the vocabulary block, and §3.4 calls it the
// layout throughout: a fixed record's block is not a vocabulary — it is the
// SHAPE the record's bytes are laid out in, and every port's symbols say so.
//
// THE SIZE BOUNDS AND THE ROOT SELECTION ARE HERE TOO, at the bottom of the
// file, because they are answers over the SAME constant sizes: the compiler
// warns and refuses out of the very functions the reference emits its layout
// from, so the reference's bytes and the compiler's verdict cannot drift.
//
// What is NOT here is everything a target spells for itself: the storage type
// of an element, how an offset into a record is written down, how a plan is
// laid out in the generated source. Those differ per language by design, and
// [TableFixedDstSpec] is the target-neutral description each one renders.

package ir

import (
	"fmt"
	"strconv"
)

// TableKindOptional is the ONE kind §3.4's LAYOUT adds to §3's closed set: the
// OPTIONAL WRAPPER, one child, whose constant size is one present byte plus its
// child's. It is a LAYOUT kind and not a WIRE kind — nothing rides under it in
// a record — and it exists because on this form `?T` and a plain `T` are ONE
// BYTE APART where §2.3's form-1 rule makes them wire-identical.
const TableKindOptional = 35

// THE LAYOUT'S SHAPE, in bytes: a u32 entry count, then a run of seventeen-byte
// entries. THERE IS NO VERSION BYTE INSIDE THE LAYOUT (docs/SPEC-TABLES.md
// §3.4): the FORM BYTE versions everything behind it, the layout's own format
// included, so a layout format change is a NEW FORM BYTE and never a wider
// entry or a byte in front of the count.
const (
	TableFixedLayoutHeaderBytes = 4 // the entry count, and nothing else
	TableFixedEntryBytes        = 17
)

// THE PINNED FILE HEADER, ONE RULE FOR ALL FIVE FORMS (docs/SPEC-TABLES.md §3,
// "THE FIRST BYTE"): the FORM BYTE at offset 0, seven RESERVED ZERO bytes, the
// form's own EIGHT-BYTE HASH at offset 8, and the body at 16 — the alignment a
// memory-mapped body needs. The fixed form does not need that alignment today;
// it pads anyway, so the bytes do not move again the day the cook and the block
// form join the registry under the same header.
//
// The hash in the header is the LAYOUT's. Each record still carries its own
// eight-byte hash, which §3.4 has always said and which the header does not
// replace: the header names the layout ONCE for the file, and a record names
// the layout it was stamped by.
const (
	TableFixedHeaderBytes = int64(16)
	TableFixedHashAt      = int64(8)
)

// THE TWO SIZE BOUNDS, and the owner's reason for them: *"effectively, fixed
// tables should only be used for small things."* A fixed record is a value
// copied whole at every bound it declares, so a big one is a big copy on every
// read and write and a big zero-fill behind every unused byte — which is the
// cost this form trades bytes for speed to avoid.
//
// The compiler WARNS above the first, and above the second it stops emitting
// the fixed form for that table altogether — see [TableFixedRecordBounds] for
// why those are different kinds of thing. A reader holds an UNTRUSTED PEER's
// layout to the same 65536, because a record whose size the layout states is
// past it is one this build will not decode, so the two sides agree by
// construction.
const (
	TableFixedRecordWarnBytes = 4096
	TableFixedRecordMaxBytes  = 65536
)

// TableFixedLeafCap bounds a compile-time plan array. Past it the walk is a
// build-time cost nobody asked for; see the backends' root selection.
const TableFixedLeafCap = 4096

const (
	// TableFixedCountBytes and TableFixedPresentBytes: a count and a length are the
	// int32 their storage already is, and a presence flag is one byte.
	TableFixedCountBytes   = int64(4)
	TableFixedPresentBytes = int64(1)
)

// ---------------------------------------------------------------------------
// THE LAYOUT: a constant size per field
// ---------------------------------------------------------------------------

// TableFixedStorageBytes is a LEAF's declared storage width — the width SPEC.md
// gives the declaration, identical in every port, which is what makes the
// identity plan a copy rather than a conversion (§3.4).
func TableFixedStorageBytes(t FieldType) int64 {
	switch t.Kind {
	case TBool:
		return 1
	case TInt, TFixed:
		return int64(t.Width / 8)
	case TBits:
		if t.Width <= 32 {
			return 4
		}
		return 8
	case TFloat32:
		return 4
	case TFloat64:
		return 8
	case TNamed:
		switch r := t.Ref.(type) {
		case *Enum:
			return int64(r.StorageBits / 8)
		case *Flags:
			return 8 // the raw mask, as §3 carries it
		}
	}
	return 0
}

// TableFixedTypeBytes is C(T). Every one is a compile-time constant, which is why
// MeasureBody is a constant and not a function that reads the value.
func TableFixedTypeBytes(st *Struct) int64 {
	var n int64
	for _, f := range st.Fields {
		n += TableFixedFieldBytes(f)
	}
	return n
}

// TableFixedFieldBytes is C(f) — §3.4's constant-size table in one function.
func TableFixedFieldBytes(f *Field) int64 {
	var payload int64
	switch {
	case f.KeyEnum != "":
		payload = f.KeyEnumRef.Max * TableFixedElementBytes(f)
	case f.Array == ArrayFixed:
		payload = f.ArrayBound * TableFixedElementBytes(f)
	case f.Array == ArrayCounted:
		payload = TableFixedCountBytes + f.ArrayBound*TableFixedElementBytes(f)
	case f.Type.Kind == TString:
		payload = TableFixedCountBytes + f.Type.Size
	case f.Type.Kind == TWString:
		payload = TableFixedCountBytes + 2*f.Type.Size
	case f.Type.Kind == TBytes:
		payload = TableFixedCountBytes + f.Type.Size
	default:
		payload = TableFixedElementBytes(f)
	}
	if f.Type.Optional {
		return TableFixedPresentBytes + payload
	}
	return payload
}

// TableFixedElementBytes is the constant of ONE element of a field.
func TableFixedElementBytes(f *Field) int64 {
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			return TableFixedTypeBytes(r)
		case *Union:
			// the tag, then the WIDEST ARM: the slack behind a narrower one is
			// zero on write and ignored on read (§3.4)
			widest := int64(0)
			for _, v := range r.Variants {
				if v.F == nil {
					continue // a void arm has no storage; TableFixedSupported refuses one anyway
				}
				if n := TableFixedFieldBytes(v.F); n > widest {
					widest = n
				}
			}
			return int64(StorageBitsFor(r.Max)/8) + widest
		}
	}
	return TableFixedStorageBytes(f.Type)
}

// TableFixedSupported reports whether a type's whole closure is one this form
// carries. A pointer, a map and an unbounded array make their holder VARIABLE
// and never reach here; what this adds is the two shapes the reference does
// not yet lay out — a union arm carrying text or an array, and a guarded
// branch, which §3.4 refuses outright.
func TableFixedSupported(st *Struct, depth int) bool {
	if depth > 16 {
		return false
	}
	for _, f := range st.Fields {
		if f.Guard != "" || f.Type.Pointer || f.IsMap() || f.IsList() || f.Type.Blob() {
			return false
		}
		if f.Type.Kind == TNamed {
			switch r := f.Type.Ref.(type) {
			case *Struct:
				if !TableFixedSupported(r, depth+1) {
					return false
				}
			case *Union:
				for _, v := range r.Variants {
					if v.F == nil {
						return false // a void arm has no storage this walk can name
					}
					a := v.F
					if a.Type.Pointer || a.IsMap() || a.IsList() || a.Array != ArrayNone || a.KeyEnum != "" ||
						a.Type.Kind == TString || a.Type.Kind == TWString || a.Type.Kind == TBytes || a.Type.Optional {
						return false
					}
					if s, ok := a.Type.Ref.(*Struct); ok && a.Type.Kind == TNamed && !TableFixedSupported(s, depth+1) {
						return false
					}
				}
			}
		}
	}
	return true
}

// TableFixedFlatType reports a type whose STORAGE IMAGE IS ITS WIRE IMAGE: the
// declared order is the storage order, there is no padding anywhere in it, and
// no field of it reorders against the wire. An array of such a type is ONE run
// however many elements it has, which is what keeps a plan small and what
// makes the whole of a big array one move.
func TableFixedFlatType(u *Unit, st *Struct) bool {
	for _, f := range st.Fields {
		if f.Type.Optional || f.KeyEnum != "" || f.Array == ArrayCounted {
			return false
		}
		switch f.Type.Kind {
		case TString, TWString, TBytes, TMap:
			return false
		}
		if _, isUnion := f.Type.Ref.(*Union); isUnion {
			return false
		}
		if !TableFixedFlatElem(u, f) {
			return false
		}
	}
	// and the C ABI agrees: no padding anywhere in the record
	return RecordLayout(u, st).Size == TableFixedTypeBytes(st)
}

// TableFixedFlatElem is the same question about ONE element of a field.
func TableFixedFlatElem(u *Unit, f *Field) bool {
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			return TableFixedFlatType(u, r)
		case *Union:
			return false
		}
	}
	switch f.Type.Kind {
	case TString, TWString, TBytes, TMap:
		return false
	}
	return true
}

// TableFixedKeyedSlots is the raw slot storage of a keyed array: a TABLE's keyed
// field is a keyed wrapper whose slots sit behind `.slots`, and a closure
// type's is the packet backend's plain array (docs/SPEC-TABLES.md §2.4).
func TableFixedKeyedSlots(owner *Struct, f *Field) string {
	if f.KeyEnum != "" && owner.IsTable {
		return f.Name + ".slots"
	}
	return f.Name
}

// TableFixedLeafCount bounds the entries a type's leaf walk writes, which sizes a
// plan array. It counts leaves BEFORE coalescing.
func TableFixedLeafCount(u *Unit, st *Struct) int {
	n := 0
	for _, f := range st.Fields {
		n += TableFixedFieldLeafCount(u, f)
	}
	return n
}

// TableFixedFieldLeafCount is one field's share of the count above.
func TableFixedFieldLeafCount(u *Unit, f *Field) int {
	per := TableFixedElementLeafCount(u, f)
	flat := TableFixedFlatElem(u, f)
	n := per
	switch {
	case f.KeyEnum != "":
		n = int(f.KeyEnumRef.Max) * per
		if flat {
			n = 1
		}
	case f.Array == ArrayFixed:
		n = int(f.ArrayBound) * per
		if flat {
			n = 1
		}
	case f.Array == ArrayCounted:
		n = 1 + int(f.ArrayBound)*per
		if flat {
			n = 2
		}
	case f.Type.Kind == TString, f.Type.Kind == TWString, f.Type.Kind == TBytes:
		n = 1
	}
	if f.Type.Optional {
		n++
	}
	return n
}

// TableFixedElementLeafCount is the same question about ONE element.
func TableFixedElementLeafCount(u *Unit, f *Field) int {
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			return TableFixedLeafCount(u, r)
		case *Union:
			n := 1 // the tag
			for _, v := range r.Variants {
				n += TableFixedFieldLeafCount(u, v.F)
			}
			return n
		}
	}
	return 1
}

// ---------------------------------------------------------------------------
// THE LAYOUT, and MY side of it
// ---------------------------------------------------------------------------

// TableFixedTerm is ONE offsetof: a generated type and a member of it. A
// destination is a SUM of these — at most two, and the second only for a
// union's tag, which sits inside the union storage the first tableFixedTerm1 named.
type TableFixedTerm struct {
	Type   string
	Member string
}

// TableFixedDstSpec is MY side of one layout entry: the storage facts a layout
// entry cannot carry, in terms every target can render for itself. A plan
// compiled from another writer's layout lands values through these.
type TableFixedDstSpec struct {
	Dst    []TableFixedTerm // this entry's storage offset inside its parent; empty is 0
	Stride *Field           // an array element's storage stride: sizeof of THIS field's element
	Aux    []TableFixedTerm // a text field's buffer, an optional's present byte, a union's tag
	// Stride1 marks a stride that is a literal one byte, which is what a
	// `bytes(N)` field's element is: a target renders 1 rather than a sizeof.
	Stride1 bool
	// AuxExtendsDst says the aux sits INSIDE the storage Dst named — a union's
	// tag, which is at an offset within the union's own storage — so a target
	// renders it as the destination and then the aux's offset, not as an
	// offset of its own.
	AuxExtendsDst bool
	Counted       int
	// Meta is the ROW's own argument for the op that lands this entry: a text
	// field's flavour. It is named for the plan entry's meta lane, which is
	// where it ends up, and it is not the guard's tag — that lane is arg's.
	Meta int
}

// TableFixedLayoutEntry is one seventeen-byte entry: an id, a kind, a constant
// size and a child count, in the writer's declared order (§3.4) — and beside it
// the destination row the same walk produced, which never rides the wire.
type TableFixedLayoutEntry struct {
	ID       uint64
	Kind     int
	Size     int64
	Children int
	Note     string // a comment on the emitted array; never a wire byte
	Dst      TableFixedDstSpec
}

type tableFixedWalk struct {
	entries []TableFixedLayoutEntry
}

func (w *tableFixedWalk) push(e TableFixedLayoutEntry, dst TableFixedDstSpec) {
	e.Dst = dst
	w.entries = append(w.entries, e)
}

func tableFixedTerm1(typeName, member string) []TableFixedTerm {
	return []TableFixedTerm{{Type: typeName, Member: member}}
}

// TableFixedWalkRoot is THE walk: the layout's entries in pre-order, each carrying
// the destination row for the same position. One walk produces both, so the
// two can never fall out of step.
func TableFixedWalkRoot(st *Struct) []TableFixedLayoutEntry {
	w := &tableFixedWalk{}
	w.push(TableFixedLayoutEntry{ID: TableWireId(st.WireName()), Kind: TableKindTable, Size: TableFixedTypeBytes(st), Children: len(st.Fields), Note: st.Name}, TableFixedDstSpec{})
	for _, f := range st.Fields {
		tableFixedWalkField(w, st.Name, f)
	}
	return w.entries
}

func tableFixedWalkField(w *tableFixedWalk, owner string, f *Field) {
	id := TableFieldWireId(f)
	size := TableFixedFieldBytes(f)
	if f.Type.Optional {
		w.push(TableFixedLayoutEntry{ID: id, Kind: TableKindOptional, Size: size, Children: 1, Note: f.Name + " ?"},
			TableFixedDstSpec{Aux: tableFixedTerm1(owner, f.Name+"_present")})
		tableFixedWalkPayload(w, owner, f, id, size-TableFixedPresentBytes)
		return
	}
	tableFixedWalkPayload(w, owner, f, id, size)
}

func tableFixedWalkPayload(w *tableFixedWalk, owner string, f *Field, id uint64, size int64) {
	switch {
	case f.KeyEnum != "":
		// a keyed array's slots are the storage's first member, so the field's
		// own offset is the slot base (§2.4's keyed wrapper)
		w.push(TableFixedLayoutEntry{ID: id, Kind: TableKindKeyed, Size: size, Children: 2, Note: f.Name},
			TableFixedDstSpec{Dst: tableFixedTerm1(owner, f.Name), Stride: f})
		tableFixedWalkEnum(w, f.KeyEnumRef, TableWireId(f.KeyEnum), f.KeyEnum, nil)
		tableFixedWalkElement(w, f, 0, "element", nil)
	case f.Array != ArrayNone:
		spec := TableFixedDstSpec{Dst: tableFixedTerm1(owner, f.Name), Stride: f}
		if f.Array == ArrayCounted {
			spec.Counted = 1
			spec.Aux = tableFixedTerm1(owner, f.Name+"_count")
		}
		w.push(TableFixedLayoutEntry{ID: id, Kind: TableKindArray, Size: size, Children: 1, Note: f.Name}, spec)
		tableFixedWalkElement(w, f, 0, "element", nil)
	case f.Type.Kind == TBytes:
		// `bytes(N)` is an ARRAY of u8 on this wire, as it is in §3, SO ITS
		// DESTINATION ROW IS AN ARRAY'S: Dst is the buffer the elements land in
		// and Aux is the live count beside it. It is stated here because the
		// TEXT row above is the other way round — Dst the length, Aux the
		// buffer — and a `bytes(N)` written under the text convention hands a
		// plan compiled from a stranger's layout a count destination that is
		// the buffer's first four bytes and an element destination that is the
		// length field. The identity plan lands this field with the TEXT op and
		// reads neither column, so only the compiled path saw it.
		//
		// Meta still carries the text flavour: the identity walk below spends
		// it, and a port that lands `bytes(N)` in one move on the compiled path
		// reads it from here rather than deriving it a second time.
		w.push(TableFixedLayoutEntry{ID: id, Kind: TableKindArray, Size: size, Children: 1, Note: f.Name},
			TableFixedDstSpec{Dst: tableFixedTerm1(owner, f.Name), Stride1: true, Aux: tableFixedTerm1(owner, f.Name+"_length"), Counted: 1, Meta: 3})
		w.push(TableFixedLayoutEntry{ID: 0, Kind: TableKindU8, Size: 1, Children: 0, Note: "u8"}, TableFixedDstSpec{})
	case f.Type.Kind == TString:
		w.push(TableFixedLayoutEntry{ID: id, Kind: TableKindString, Size: size, Children: 0, Note: f.Name},
			TableFixedDstSpec{Dst: tableFixedTerm1(owner, f.Name+"_length"), Aux: tableFixedTerm1(owner, f.Name), Meta: 1})
	case f.Type.Kind == TWString:
		w.push(TableFixedLayoutEntry{ID: id, Kind: TableKindWstring, Size: size, Children: 0, Note: f.Name},
			TableFixedDstSpec{Dst: tableFixedTerm1(owner, f.Name+"_length"), Aux: tableFixedTerm1(owner, f.Name), Meta: 2})
	default:
		tableFixedWalkElement(w, f, id, f.Name, tableFixedTerm1(owner, f.Name))
	}
}

func tableFixedWalkElement(w *tableFixedWalk, f *Field, id uint64, note string, dst []TableFixedTerm) {
	size := TableFixedElementBytes(f)
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			w.push(TableFixedLayoutEntry{ID: id, Kind: TableKindTable, Size: size, Children: len(r.Fields), Note: note}, TableFixedDstSpec{Dst: dst})
			for _, sub := range r.Fields {
				tableFixedWalkField(w, r.Name, sub)
			}
			return
		case *Union:
			w.push(TableFixedLayoutEntry{ID: id, Kind: TableKindUnion, Size: size, Children: len(r.Variants), Note: note},
				TableFixedDstSpec{Dst: dst, Aux: tableFixedTerm1(f.Type.Name, "type"), AuxExtendsDst: true})
			for _, v := range r.Variants {
				armID := TableWireId(v.WireName())
				tableFixedWalkElement(w, v.F, armID, v.Name, tableFixedTerm1(f.Type.Name, v.Name))
			}
			return
		case *Enum:
			tableFixedWalkEnum(w, r, id, note, dst)
			return
		}
	}
	w.push(TableFixedLayoutEntry{ID: id, Kind: TableWireScalarKind(f), Size: size, Children: 0, Note: note}, TableFixedDstSpec{Dst: dst})
}

func tableFixedWalkEnum(w *tableFixedWalk, e *Enum, id uint64, note string, dst []TableFixedTerm) {
	w.push(TableFixedLayoutEntry{ID: id, Kind: TableKindEnum, Size: int64(e.StorageBits / 8), Children: len(e.Variants), Note: note}, TableFixedDstSpec{Dst: dst})
	for i := range e.Variants {
		w.push(TableFixedLayoutEntry{ID: TableWireId(e.VariantWireName(i)), Kind: TableKindNoPayload, Size: 0, Children: 0, Note: e.Variants[i]}, TableFixedDstSpec{})
	}
}

// TableFixedLayoutBytes is the LAYOUT exactly as it rides: a u32 entry count and
// a run of seventeen-byte entries, every number little-endian. THERE IS NO
// VERSION BYTE IN FRONT OF THE COUNT — the FORM BYTE versions the layout's own
// format (§3.4).
func TableFixedLayoutBytes(entries []TableFixedLayoutEntry) []byte {
	out := make([]byte, 0, TableFixedLayoutHeaderBytes+TableFixedEntryBytes*len(entries))
	out = appendTableFixedU32(out, uint32(len(entries)))
	for _, e := range entries {
		out = appendTableFixedU64(out, e.ID)
		out = append(out, byte(e.Kind))
		out = appendTableFixedU32(out, uint32(e.Size))
		out = appendTableFixedU32(out, uint32(e.Children))
	}
	return out
}

func appendTableFixedU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendTableFixedU64(b []byte, v uint64) []byte {
	for i := range 8 {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

// TableFixedLayoutHash is fnv1a64 over the layout's bytes exactly as written,
// and it is the eight bytes every record carries and the eight the file header
// carries once (§3, §3.4).
func TableFixedLayoutHash(layout []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range layout {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

// ---------------------------------------------------------------------------
// THE PLAN, IN NUMBERS
//
// A PLAN IS A FLAT ARRAY OF (source, destination, size, op) ENTRIES and a read
// is one loop over it (docs/SPEC-TABLES.md §3.4). The IDENTITY plan — the one a
// record written by this very build is read through — is the same array every
// time, so THE SCHEMA COMPILER BUILDS IT, once, here, and every backend lays
// the answer down as static data. No port needs a compile-time walk of its own,
// and no two ports can disagree about what the coalescer did.
//
// The destinations are byte offsets into the caller's own storage, computed from
// ir's C ABI model. Every backend emits the model's answers back as assertions
// against its own compiler's offsetof and sizeof, so a layout this walk ever got
// wrong is a build error rather than a misplaced value at run time.
// ---------------------------------------------------------------------------

// fixedMember is one asserted fact about a generated record: a member's offset,
// or the record's own size when Member is empty.
type TableFixedMember struct {
	Owner  string
	Member string
	Value  int64
}

// fixedMembers lists a record's members in declared order with the offset ir's
// C ABI walk gives each, and the names this backend spells them by.
func TableFixedMembers(u *Unit, st *Struct) []TableFixedMember {
	ml := RecordLayout(u, st)
	out := []TableFixedMember{{Owner: st.Name, Value: ml.Size}}
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		f := fl.Field
		pieces := FieldPieces(u, f, fl.Offset)
		names := []string{f.Name}
		switch {
		case f.Type.Kind == TString, f.Type.Kind == TWString, f.Type.Kind == TBytes:
			names = append(names, f.Name+"_length")
		case f.Array == ArrayCounted:
			names = append(names, f.Name+"_count")
		}
		if f.Type.Optional {
			names = append(names, f.Name+"_present")
		}
		for k, p := range pieces {
			if k >= len(names) {
				break
			}
			out = append(out, TableFixedMember{Owner: st.Name, Member: names[k], Value: p.Offset})
		}
	}
	return out
}

// fixedMemberOffset answers one member's byte offset inside its record.
func TableFixedMemberOffset(u *Unit, st *Struct, member string) int64 {
	for _, m := range TableFixedMembers(u, st) {
		if m.Member == member {
			return m.Value
		}
	}
	return 0
}

// fixedUnionArmOffset is where every arm of a union sits — the `as` member,
// which is one offset for all of them because they are overlaid.
func TableFixedUnionArmOffset(u *Unit, un *Union) int64 {
	_, _, _, armOffset := UnionLayout(u, un)
	return armOffset
}

// fixedStorageBytes is sizeof of ONE element's C storage — what the loop over
// an array strides by, and what a keyed array's slot stride is.
func TableFixedStrideBytes(u *Unit, f *Field) int64 {
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			return RecordLayout(u, r).Size
		case *Union:
			size, _, _, _ := UnionLayout(u, r)
			return size
		case *Enum:
			return int64(StorageBitsFor(r.Max)) / 8
		case *Flags:
			return 8
		}
	}
	switch f.Type.Kind {
	case TBool:
		return 1
	case TFloat32:
		return 4
	case TFloat64:
		return 8
	case TBits:
		if f.Type.Width <= 32 {
			return 4
		}
		return 8
	}
	return int64(f.Type.Width) / 8
}

// ---------------------------------------------------------------------------
// THE LEAF WALK AND THE COALESCER
//
// This is TableFixedBuildPlan and the constexpr leaf walk beside it, in Go,
// producing exactly what the C++ compiler produces from the same schema.
// ---------------------------------------------------------------------------

// TableFixedNoGuard is the guard of an entry that belongs to no union arm.
const TableFixedNoGuard = int64(-1)

// The three ops an IDENTITY plan can carry. The other four are a compiled
// plan's, and a plan compiled from another writer's block is built at run time
// by the runtime each backend carries.
const (
	TableFixedOpCopy  = 0
	TableFixedOpCount = 1
	TableFixedOpText  = 2
)

// TableFixedLeaf is one entry of a plan: byte offsets into the record's body and
// into the reader's own storage, and the op that moves between them.
type TableFixedLeaf struct {
	Src, Dst, Size, Aux int64
	Guard               int64 // TableFixedNoGuard when the entry belongs to no arm
	Op                  int
	// Arg is THE GUARD'S TAG and nothing else: the ordinal the byte at Guard
	// must hold for this entry to run. It is meaningless on an unguarded entry
	// and it belongs to no op.
	Arg int
	// Meta is THE OP'S OWN ARGUMENT, on the ops that have one: a text entry's
	// flavour today. It has its own lane because Arg's is the guard's, and the
	// two used to share one — which put a text field under a union arm on a
	// collision course, the flavour overwriting the tag or the tag the flavour
	// depending on which was stamped last.
	Meta int
	Note string // a comment on the emitted array; never a wire byte
}

// tableFixedLeaves writes one type's leaves at a base the caller gives — the C twin
// of <Name>FixedLeaves.
func tableFixedLeaves(u *Unit, st *Struct, src, dst int64, out *[]TableFixedLeaf) {
	off := int64(0)
	for _, f := range st.Fields {
		tableFixedFieldLeaves(u, st, f, src+off, dst, out)
		off += TableFixedFieldBytes(f)
	}
}

func tableFixedFieldLeaves(u *Unit, st *Struct, f *Field, src, dst int64, out *[]TableFixedLeaf) {
	base := src
	if f.Type.Optional {
		*out = append(*out, TableFixedLeaf{Src: base, Dst: dst + TableFixedMemberOffset(u, st, f.Name+"_present"),
			Size: 1, Guard: TableFixedNoGuard, Op: TableFixedOpCopy, Note: f.Name + " present"})
		base += TableFixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		tableFixedElementLoop(u, st, f, base, dst, f.KeyEnumRef.Max, out)
	case f.Array == ArrayFixed:
		tableFixedElementLoop(u, st, f, base, dst, f.ArrayBound, out)
	case f.Array == ArrayCounted:
		*out = append(*out, TableFixedLeaf{Src: base, Dst: dst + TableFixedMemberOffset(u, st, f.Name+"_count"),
			Size: f.ArrayBound, Guard: TableFixedNoGuard, Op: TableFixedOpCount, Note: f.Name + " count"})
		tableFixedElementLoop(u, st, f, base+TableFixedCountBytes, dst, f.ArrayBound, out)
	case f.Type.Kind == TString:
		tableFixedTextLeaf(u, st, f, base, dst, f.Type.Size, 1, out)
	case f.Type.Kind == TWString:
		tableFixedTextLeaf(u, st, f, base, dst, 2*f.Type.Size, 2, out)
	case f.Type.Kind == TBytes:
		tableFixedTextLeaf(u, st, f, base, dst, f.Type.Size, 3, out)
	default:
		tableFixedElementLeavesAt(u, f, base, dst+TableFixedMemberOffset(u, st, f.Name), f.Name, out)
	}
}

func tableFixedTextLeaf(u *Unit, st *Struct, f *Field, src, dst, units int64, flavour int, out *[]TableFixedLeaf) {
	*out = append(*out, TableFixedLeaf{Src: src, Dst: dst + TableFixedMemberOffset(u, st, f.Name+"_length"),
		Size: units, Aux: dst + TableFixedMemberOffset(u, st, f.Name), Guard: TableFixedNoGuard,
		Op: TableFixedOpText, Meta: flavour, Note: f.Name})
}

func tableFixedElementLoop(u *Unit, st *Struct, f *Field, src, dst, count int64, out *[]TableFixedLeaf) {
	elem := TableFixedElementBytes(f)
	at := dst + TableFixedMemberOffset(u, st, f.Name)
	if TableFixedFlatElem(u, f) {
		// THE WHOLE ARRAY IS ONE RUN: its storage image is its wire image, so
		// the elements need no walk at all and the plan carries one entry
		// however many of them there are.
		*out = append(*out, TableFixedLeaf{Src: src, Dst: at, Size: count * elem,
			Guard: TableFixedNoGuard, Op: TableFixedOpCopy, Note: f.Name + ", whole"})
		return
	}
	stride := TableFixedStrideBytes(u, f)
	for i := range count {
		tableFixedElementLeavesAt(u, f, src+i*elem, at+i*stride, f.Name+"["+strconv.FormatInt(i, 10)+"]", out)
	}
}

// tableFixedElementLeavesAt is ONE element's leaves at the bases given.
func tableFixedElementLeavesAt(u *Unit, f *Field, src, dst int64, note string, out *[]TableFixedLeaf) {
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			tableFixedLeaves(u, r, src, dst, out)
			return
		case *Union:
			tag := int64(StorageBitsFor(r.Max) / 8)
			arms := TableFixedUnionArmOffset(u, r)
			*out = append(*out, TableFixedLeaf{Src: src, Dst: dst, Size: tag,
				Guard: TableFixedNoGuard, Op: TableFixedOpCopy, Note: note + " tag"})
			for i, v := range r.Variants {
				// THE GUARD IS STAMPED ON AFTERWARDS, over everything the arm
				// wrote, exactly as the reference does: an arm nested inside an
				// arm answers to the OUTER tag, which is the one that decides
				// whether any of it is there at all.
				at := len(*out)
				tableFixedElementLeavesAt(u, v.F, src+tag, dst+arms, note+"."+v.Name, out)
				for q := at; q < len(*out); q++ {
					(*out)[q].Guard = src
					(*out)[q].Arg = i + 1
				}
			}
			return
		}
	}
	*out = append(*out, TableFixedLeaf{Src: src, Dst: dst, Size: TableFixedElementBytes(f),
		Guard: TableFixedNoGuard, Op: TableFixedOpCopy, Note: note})
}

// TableFixedBuildPlan is the leaf walk and the coalescer together: one type's
// identity plan, exactly as every backend must lay it down. THE PLAN IS
// PARTITIONED — every unguarded entry first, then every guarded one — and
// adjacent COPY entries whose source and destination both advance together are
// one entry. Neighbours are never merged across the split.
func TableFixedBuildPlan(u *Unit, st *Struct) (plan []TableFixedLeaf, guarded int) {
	var raw []TableFixedLeaf
	tableFixedLeaves(u, st, 0, 0, &raw)
	return tableFixedCoalesce(raw)
}

func tableFixedCoalesce(raw []TableFixedLeaf) (plan []TableFixedLeaf, guarded int) {
	for pass := range 2 {
		for _, e := range raw {
			if (e.Guard != TableFixedNoGuard) != (pass == 1) {
				continue
			}
			if len(plan) > guarded {
				last := &plan[len(plan)-1]
				if last.Op == TableFixedOpCopy && e.Op == TableFixedOpCopy &&
					last.Guard == e.Guard && last.Arg == e.Arg && last.Meta == e.Meta &&
					last.Src+last.Size == e.Src && last.Dst+last.Size == e.Dst {
					last.Size += e.Size
					continue
				}
			}
			plan = append(plan, e)
		}
		if pass == 0 {
			guarded = len(plan)
		}
	}
	return plan, guarded
}

// ---------------------------------------------------------------------------
// THE ROOTS AND THE BOUNDS
//
// Which tables the form applies to, and the two sizes a fixed record is held
// to. They are answers over the SAME constant sizes the walk above is built
// from — see the file header — so the reference's bytes and the compiler's
// verdict cannot drift apart.
// ---------------------------------------------------------------------------

// TableFixedRoots is every table of the unit the fixed form applies to, in
// declaration order: a table whose mode is FIXED (§2.2), which is every table
// that is not in [VariableTables] and is not a map's synthesised entry.
//
// THIS IS THE SELECTION POINT FOR FORM 3, AND #823'S KEYWORD REPLACES IT. §3.4
// selects the form BY THE KEYWORD: a `fixed table` encodes as form 3 always, a
// `table` encodes as form 1, and there is no path between them. The keyword is
// on branch `fixed-table-keyword` and is not merged, so until it lands the
// selection is the DERIVED mode below — the only thing in this tree that marks
// a table fixed. When it lands, this function reads [Struct.FixedDeclared].
//
// It is the compiler's own answer and not a backend's: a backend may carry the
// form for fewer types than this — the C++ reference's compile-time plan bound
// (§3.4's "held by test") and the record ceiling [TableFixedFormRoots] applies
// are both such narrowings — but the SIZE BOUNDS are reported over ALL of them,
// because a table nobody warned about is a table nobody fixed.
func TableFixedRoots(u *Unit) []*Struct {
	variable := VariableTables(u)
	var out []*Struct
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.IsTable || st.IsMapEntry() || variable[st.Name] {
				continue
			}
			out = append(out, st)
		}
	}
	return out
}

// TableFixedFormRoots is every table of the unit the FIXED FORM is emitted for:
// [TableFixedRoots] minus the ones whose record body is past
// [TableFixedRecordMaxBytes].
//
// THE 65536 IS THE WIRE'S CEILING AND NOT A PREFERENCE. A reader holds an
// untrusted peer's layout to it (§3.4's `layout_record_too_large`), so a record
// larger than it is one no conforming reader will decode — which makes emitting
// a writer for it a way to produce bytes nobody can read.
//
// A table that was merely DERIVED into the form (§2.2) and is past the ceiling
// keeps form 1, which §3.4 guarantees it never lost, and the compiler says so
// by name rather than dropping the form in silence. A table whose author
// DECLARED it fixed never reaches here at all: [TableFixedRecordBounds]
// refuses the compile, because the whole of #823's keyword is that a declared
// fixed table encodes as form 3 and a silent demotion to form 1 would be the
// surprise the keyword exists to prevent.
func TableFixedFormRoots(u *Unit) []*Struct {
	var out []*Struct
	for _, st := range TableFixedRoots(u) {
		if TableFixedTypeBytes(st) > TableFixedRecordMaxBytes {
			continue // and if it was DECLARED fixed, TableFixedRecordBounds already refused the compile
		}
		out = append(out, st)
	}
	return out
}

// TableFixedRecordBounds holds every fixed table of the unit to §3.4's two size
// bounds and answers the compiler's two channels: warnings, which name the
// table and the size and change no exit code, and refusals, which fail the
// compile.
//
// THE TWO BOUNDS ARE DIFFERENT KINDS OF THING, and conflating them would lose
// the difference:
//
//   - 4096 is ADVISORY and always on. Past it the table still carries the fixed
//     form and the compiler warns, naming the table and the size, because
//     *"effectively, fixed tables should only be used for small things"* is a
//     design rule and a rule nobody is told about is not a rule.
//   - 65536 is the WIRE's CEILING. Past it the table DOES NOT CARRY THE FIXED
//     FORM at all — no conforming reader would decode such a record (§3.4) — and
//     the compiler says so by name. It keeps form 1, which it never lost. This
//     is not a refusal of the unit: §12.1's render frame and §2.8's wide text
//     are legitimate fixed tables of megabytes that were never form-3 tables,
//     and refusing the unit over a form it does not use would be refusing the
//     wrong thing.
//
// `limit` is the HARD refusal bound in bytes and it is OFF by default (zero): a
// project that wants the advisory enforced as a gate sets `--fixed-record-limit
// 4096` and a fixed table past it does not compile. It is a project's policy
// knob and never a wire fact, and it only ever LOWERS: it cannot raise the
// 65536, because 65536 is the number a PEER's reader holds this build's records
// to (`layout_record_too_large`) and a peer's build has never heard of this
// project's flag.
//
// THE 65536 REFUSES A DECLARED FIXED TABLE AND DEMOTES A DERIVED ONE, and the
// difference is #823's keyword:
//
//   - A DECLARED fixed table ([Struct.FixedDeclared]) encodes as form 3 always.
//     Past the ceiling it cannot, and there is no form 1 for it to fall back
//     to without the author being surprised — *"if we add any feature that
//     stops it from being fixed, it is a compile error … we don't want to
//     surprise the user"* — so it is a REFUSAL BY NAME and the compile fails.
//   - A table merely DERIVED into the fixed mode (§2.2) asked for nothing. Past
//     the ceiling it keeps form 1, which it never lost, and the compiler warns.
//     §12.1's render frame and §2.8's wide text are exactly this: legitimate
//     fixed-MODE tables of megabytes that were never form-3 tables. WHEN #823
//     LANDS THIS BRANCH GOES AWAY — an undeclared `table` will not select form
//     3 at all — and it is here because the keyword is not merged.
func TableFixedRecordBounds(u *Unit, limit int64) (warnings []string, errs []error) {
	for _, st := range TableFixedRoots(u) {
		n := TableFixedTypeBytes(st)
		if limit > 0 && n > limit {
			errs = append(errs, fmt.Errorf(
				"table %s: a fixed table's record body is %d bytes, past the %d-byte --fixed-record-limit (docs/SPEC-TABLES.md §3.4) — a fixed record is copied WHOLE at every bound it declares, and *\"effectively, fixed tables should only be used for small things\"*; shrink the bounds it declares, make it a variable table (§2.2) with an unbounded array, a map or a pointer, or raise the limit",
				st.Name, n, limit))
			continue
		}
		switch {
		case n > TableFixedRecordMaxBytes && st.FixedDeclared:
			errs = append(errs, fmt.Errorf(
				"table %s: a DECLARED fixed table's record body is %d bytes, past the %d-byte fixed-form ceiling (docs/SPEC-TABLES.md §3.4) — no conforming reader decodes a record that size, so this table cannot carry form 3, and a fixed table never silently falls back to form 1; shrink the bounds it declares, or drop the `fixed` keyword and let it be a variable table (§2.2)",
				st.Name, n, TableFixedRecordMaxBytes))
		case n > TableFixedRecordMaxBytes:
			warnings = append(warnings, fmt.Sprintf(
				"table %s: a fixed table's record body is %d bytes, past the %d-byte fixed-form ceiling (docs/SPEC-TABLES.md §3.4) — THE FIXED FORM IS NOT EMITTED FOR IT, because no conforming reader decodes a record that size; it keeps form 1",
				st.Name, n, TableFixedRecordMaxBytes))
		case n > TableFixedRecordWarnBytes:
			warnings = append(warnings, fmt.Sprintf(
				"table %s: a fixed table's record body is %d bytes, past the %d-byte advisory bound (docs/SPEC-TABLES.md §3.4) — every read and write copies all of it and zero-fills the slack, and fixed tables are for small things; the fixed form stops being emitted at %d",
				st.Name, n, TableFixedRecordWarnBytes, TableFixedRecordMaxBytes))
		}
	}
	return warnings, errs
}

// TableFixedEmitted answers whether a BACKEND emits the fixed form for one
// type, and it is the ONE place that answer is computed: a port that asked the
// question for itself would be a port that could disagree about which types
// carry form 3 at all.
//
// Three things narrow [TableFixedRoots] here:
//
//   - THE CLOSURE has to be one this form lays out ([TableFixedSupported]).
//   - THE RECORD CEILING is the WIRE's: past [TableFixedRecordMaxBytes] no
//     conforming reader decodes the record, so emitting a writer for it is a
//     way to produce bytes nobody can read.
//   - THE PLAN'S LEAF CAP is the REFERENCE's: a plan is laid down as static
//     data in an array the compiler sizes, and past [TableFixedLeafCap] that
//     array is a build-time cost nobody asked for.
//
// Nothing here is silent. [TableFixedRecordBounds] names every table the two
// size bounds touch, and a table the leaf cap turns away is named by
// [TableFixedLeafCapRefusals].
func TableFixedEmitted(u *Unit, st *Struct) bool {
	if !st.IsTable || st.IsMapEntry() || !TableFixedSupported(st, 0) {
		return false
	}
	if VariableTables(u)[st.Name] {
		return false
	}
	if TableFixedTypeBytes(st) > TableFixedRecordMaxBytes {
		return false
	}
	return TableFixedLeafCount(u, st) <= TableFixedLeafCap
}

// TableFixedHasWriteBound answers whether any fixed table of the unit has a
// number the WRITE side bounds: a text field's used length, or a counted
// array's live count. Those are the only two write-side checks this form has,
// and they are DEBUG-ONLY (schema_assert), so a unit of plain scalars declares
// no bound and pays for no assert hook at all — the zero-cost gate (§2.2).
func TableFixedHasWriteBound(u *Unit) bool {
	seen := map[string]bool{}
	var has func(st *Struct) bool
	has = func(st *Struct) bool {
		if seen[st.Name] {
			return false
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			switch {
			case f.Array == ArrayCounted:
				return true
			case f.Type.Kind == TString, f.Type.Kind == TWString, f.Type.Kind == TBytes:
				return true
			}
			if f.Type.Kind != TNamed {
				continue
			}
			switch r := f.Type.Ref.(type) {
			case *Struct:
				if has(r) {
					return true
				}
			case *Union:
				for _, v := range r.Variants {
					if v.F == nil {
						continue
					}
					if s2, ok := v.F.Type.Ref.(*Struct); ok && v.F.Type.Kind == TNamed && has(s2) {
						return true
					}
				}
			}
		}
		return false
	}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if TableFixedEmitted(u, st) && has(st) {
				return true
			}
		}
	}
	return false
}
