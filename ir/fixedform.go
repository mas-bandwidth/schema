// THE FIXED FORM'S WIRE LAW (docs/SPEC-TABLES.md §3.4), in one place because
// it produces BYTES and every port has to produce the same ones.
//
// A fixed-table record is an eight-byte hash of the writer's layout
// and then the values in declared order, every field at its DECLARED STORAGE
// WIDTH, nothing padded between fields. Three things decide those bytes: the
// constant size of every field, the pre-order walk that becomes the vocabulary
// block, and the hash over the block. A second copy of any of them in a second
// backend is a second answer waiting to happen, so they live here and the
// backends render rather than recompute.
//
// What is NOT here is everything a target spells for itself: the storage type
// of an element, how an offset into a record is written down, how a plan is
// laid out in the generated source. Those differ per language by design, and
// [FixedDstSpec] is the target-neutral description each one renders.

package ir

import "strconv"

// FixedKindOptional is the ONE kind §3.4's block adds to §3's closed set: the
// OPTIONAL WRAPPER, one child, whose size is one present byte plus the child's.
// Nothing rides under it in a record — it is a block kind and not a wire kind.
const FixedKindOptional = 35

// FixedLeafCap bounds a compile-time plan array. Past it the walk is a
// build-time cost nobody asked for; see the backends' root selection.
const FixedLeafCap = 4096

const (
	// FixedCountBytes and FixedPresentBytes: a count and a length are the
	// int32 their storage already is, and a presence flag is one byte.
	FixedCountBytes   = int64(4)
	FixedPresentBytes = int64(1)
)

// ---------------------------------------------------------------------------
// THE LAYOUT: a constant size per field
// ---------------------------------------------------------------------------

// FixedStorageBytes is a LEAF's declared storage width — the width SPEC.md
// gives the declaration, identical in every port, which is what makes the
// identity plan a copy rather than a conversion (§3.4).
func FixedStorageBytes(t FieldType) int64 {
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

// FixedTypeBytes is C(T). Every one is a compile-time constant, which is why
// MeasureBody is a constant and not a function that reads the value.
func FixedTypeBytes(st *Struct) int64 {
	var n int64
	for _, f := range st.Fields {
		n += FixedFieldBytes(f)
	}
	return n
}

// FixedFieldBytes is C(f) — §3.4's constant-size table in one function.
func FixedFieldBytes(f *Field) int64 {
	var payload int64
	switch {
	case f.KeyEnum != "":
		payload = f.KeyEnumRef.Max * FixedElementBytes(f)
	case f.Array == ArrayFixed:
		payload = f.ArrayBound * FixedElementBytes(f)
	case f.Array == ArrayCounted:
		payload = FixedCountBytes + f.ArrayBound*FixedElementBytes(f)
	case f.Type.Kind == TString:
		payload = FixedCountBytes + f.Type.Size
	case f.Type.Kind == TWString:
		payload = FixedCountBytes + 2*f.Type.Size
	case f.Type.Kind == TBytes:
		payload = FixedCountBytes + f.Type.Size
	default:
		payload = FixedElementBytes(f)
	}
	if f.Type.Optional {
		return FixedPresentBytes + payload
	}
	return payload
}

// FixedElementBytes is the constant of ONE element of a field.
func FixedElementBytes(f *Field) int64 {
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			return FixedTypeBytes(r)
		case *Union:
			// the tag, then the WIDEST ARM: the slack behind a narrower one is
			// zero on write and ignored on read (§3.4).
			//
			// AN ARM CAN CARRY NO PAYLOAD AT ALL, and it contributes nothing.
			// The guard is not decoration: these functions answer the COMPILER's
			// size bounds over every non-variable table of a unit, not only the
			// ones a backend will lay out, so they have to be TOTAL over
			// anything the parser accepts.
			widest := int64(0)
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				if n := FixedFieldBytes(v.F); n > widest {
					widest = n
				}
			}
			return int64(StorageBitsFor(r.Max)/8) + widest
		}
	}
	return FixedStorageBytes(f.Type)
}

// FixedSupported reports whether a type's whole closure is one this form
// carries. A pointer, a map and an unbounded array make their holder VARIABLE
// and never reach here; what this adds is the two shapes the reference does
// not yet lay out — a union arm carrying text or an array, and a guarded
// branch, which §3.4 refuses outright.
func FixedSupported(st *Struct, depth int) bool {
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
				if !FixedSupported(r, depth+1) {
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
					if s, ok := a.Type.Ref.(*Struct); ok && a.Type.Kind == TNamed && !FixedSupported(s, depth+1) {
						return false
					}
				}
			}
		}
	}
	return true
}

// FixedFlatType reports a type whose STORAGE IMAGE IS ITS WIRE IMAGE: the
// declared order is the storage order, there is no padding anywhere in it, and
// no field of it reorders against the wire. An array of such a type is ONE run
// however many elements it has, which is what keeps a plan small and what
// makes the whole of a big array one move.
func FixedFlatType(u *Unit, st *Struct) bool {
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
		if !FixedFlatElem(u, f) {
			return false
		}
	}
	// and the C ABI agrees: no padding anywhere in the record
	return RecordLayout(u, st).Size == FixedTypeBytes(st)
}

// FixedFlatElem is the same question about ONE element of a field.
func FixedFlatElem(u *Unit, f *Field) bool {
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			return FixedFlatType(u, r)
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

// FixedKeyedSlots is the raw slot storage of a keyed array: a TABLE's keyed
// field is a keyed wrapper whose slots sit behind `.slots`, and a closure
// type's is the packet backend's plain array (docs/SPEC-TABLES.md §2.4).
func FixedKeyedSlots(owner *Struct, f *Field) string {
	if f.KeyEnum != "" && owner.IsTable {
		return f.Name + ".slots"
	}
	return f.Name
}

// FixedLeafCount bounds the entries a type's leaf walk writes, which sizes a
// plan array. It counts leaves BEFORE coalescing.
func FixedLeafCount(u *Unit, st *Struct) int {
	n := 0
	for _, f := range st.Fields {
		n += FixedFieldLeafCount(u, f)
	}
	return n
}

// FixedFieldLeafCount is one field's share of the count above.
func FixedFieldLeafCount(u *Unit, f *Field) int {
	per := FixedElementLeafCount(u, f)
	flat := FixedFlatElem(u, f)
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

// FixedElementLeafCount is the same question about ONE element.
func FixedElementLeafCount(u *Unit, f *Field) int {
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			return FixedLeafCount(u, r)
		case *Union:
			n := 1 // the tag
			for _, v := range r.Variants {
				n += FixedFieldLeafCount(u, v.F)
			}
			return n
		}
	}
	return 1
}

// ---------------------------------------------------------------------------
// THE VOCABULARY BLOCK, and MY side of it
// ---------------------------------------------------------------------------

// FixedTerm is ONE offsetof: a generated type and a member of it. A
// destination is a SUM of these — at most two, and the second only for a
// union's tag, which sits inside the union storage the first term named.
type FixedTerm struct {
	Type   string
	Member string
}

// FixedDstSpec is MY side of one block entry: the storage facts a block entry
// cannot carry, in terms every target can render for itself. A plan compiled
// from another writer's block lands values through these.
type FixedDstSpec struct {
	Dst    []FixedTerm // this entry's storage offset inside its parent; empty is 0
	Stride *Field      // an array element's storage stride: sizeof of THIS field's element
	Aux    []FixedTerm // a text field's buffer, an optional's present byte, a union's tag
	// Stride1 marks a stride that is a literal one byte, which is what a
	// `bytes(N)` field's element is: a target renders 1 rather than a sizeof.
	Stride1 bool
	// AuxExtendsDst says the aux sits INSIDE the storage Dst named — a union's
	// tag, which is at an offset within the union's own storage — so a target
	// renders it as the destination and then the aux's offset, not as an
	// offset of its own.
	AuxExtendsDst bool
	Counted       int
	Arg           int
}

// FixedLayoutEntry is one seventeen-byte entry: an id, a kind, a constant size
// and a child count, in the writer's declared order (§3.4) — and beside it the
// destination row the same walk produced, which never rides the wire.
type FixedLayoutEntry struct {
	ID       uint64
	Kind     int
	Size     int64
	Children int
	Note     string // a comment on the emitted array; never a wire byte
	Dst      FixedDstSpec
}

type fixedWalk struct {
	entries []FixedLayoutEntry
}

func (w *fixedWalk) push(e FixedLayoutEntry, dst FixedDstSpec) {
	e.Dst = dst
	w.entries = append(w.entries, e)
}

func term(typeName, member string) []FixedTerm {
	return []FixedTerm{{Type: typeName, Member: member}}
}

// FixedWalkRoot is THE walk: the block's entries in pre-order, each carrying
// the destination row for the same position. One walk produces both, so the
// two can never fall out of step.
func FixedWalkRoot(st *Struct) []FixedLayoutEntry {
	w := &fixedWalk{}
	w.push(FixedLayoutEntry{ID: TableWireId(st.WireName()), Kind: TableKindTable, Size: FixedTypeBytes(st), Children: len(st.Fields), Note: st.Name}, FixedDstSpec{})
	for _, f := range st.Fields {
		fixedWalkField(w, st.Name, f)
	}
	return w.entries
}

func fixedWalkField(w *fixedWalk, owner string, f *Field) {
	id := TableFieldWireId(f)
	size := FixedFieldBytes(f)
	if f.Type.Optional {
		w.push(FixedLayoutEntry{ID: id, Kind: FixedKindOptional, Size: size, Children: 1, Note: f.Name + " ?"},
			FixedDstSpec{Aux: term(owner, f.Name+"_present")})
		fixedWalkPayload(w, owner, f, id, size-FixedPresentBytes)
		return
	}
	fixedWalkPayload(w, owner, f, id, size)
}

func fixedWalkPayload(w *fixedWalk, owner string, f *Field, id uint64, size int64) {
	switch {
	case f.KeyEnum != "":
		// a keyed array's slots are the storage's first member, so the field's
		// own offset is the slot base (§2.4's keyed wrapper)
		w.push(FixedLayoutEntry{ID: id, Kind: TableKindKeyed, Size: size, Children: 2, Note: f.Name},
			FixedDstSpec{Dst: term(owner, f.Name), Stride: f})
		fixedWalkEnum(w, f.KeyEnumRef, TableWireId(f.KeyEnum), f.KeyEnum, nil)
		fixedWalkElement(w, f, 0, "element", nil)
	case f.Array != ArrayNone:
		spec := FixedDstSpec{Dst: term(owner, f.Name), Stride: f}
		if f.Array == ArrayCounted {
			spec.Counted = 1
			spec.Aux = term(owner, f.Name+"_count")
		}
		w.push(FixedLayoutEntry{ID: id, Kind: TableKindArray, Size: size, Children: 1, Note: f.Name}, spec)
		fixedWalkElement(w, f, 0, "element", nil)
	case f.Type.Kind == TBytes:
		// `bytes(N)` is an ARRAY of u8 on this wire, as it is in §3, and the
		// text flavour on the row is what makes the reader land it in one move
		w.push(FixedLayoutEntry{ID: id, Kind: TableKindArray, Size: size, Children: 1, Note: f.Name},
			FixedDstSpec{Dst: term(owner, f.Name+"_length"), Stride1: true, Aux: term(owner, f.Name), Counted: 1, Arg: 3})
		w.push(FixedLayoutEntry{ID: 0, Kind: TableKindU8, Size: 1, Children: 0, Note: "u8"}, FixedDstSpec{})
	case f.Type.Kind == TString:
		w.push(FixedLayoutEntry{ID: id, Kind: TableKindString, Size: size, Children: 0, Note: f.Name},
			FixedDstSpec{Dst: term(owner, f.Name+"_length"), Aux: term(owner, f.Name), Arg: 1})
	case f.Type.Kind == TWString:
		w.push(FixedLayoutEntry{ID: id, Kind: TableKindWstring, Size: size, Children: 0, Note: f.Name},
			FixedDstSpec{Dst: term(owner, f.Name+"_length"), Aux: term(owner, f.Name), Arg: 2})
	default:
		fixedWalkElement(w, f, id, f.Name, term(owner, f.Name))
	}
}

func fixedWalkElement(w *fixedWalk, f *Field, id uint64, note string, dst []FixedTerm) {
	size := FixedElementBytes(f)
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			w.push(FixedLayoutEntry{ID: id, Kind: TableKindTable, Size: size, Children: len(r.Fields), Note: note}, FixedDstSpec{Dst: dst})
			for _, sub := range r.Fields {
				fixedWalkField(w, r.Name, sub)
			}
			return
		case *Union:
			w.push(FixedLayoutEntry{ID: id, Kind: TableKindUnion, Size: size, Children: len(r.Variants), Note: note},
				FixedDstSpec{Dst: dst, Aux: term(f.Type.Name, "type"), AuxExtendsDst: true})
			for _, v := range r.Variants {
				armID := TableWireId(v.WireName())
				fixedWalkElement(w, v.F, armID, v.Name, term(f.Type.Name, v.Name))
			}
			return
		case *Enum:
			fixedWalkEnum(w, r, id, note, dst)
			return
		}
	}
	w.push(FixedLayoutEntry{ID: id, Kind: TableWireScalarKind(f), Size: size, Children: 0, Note: note}, FixedDstSpec{Dst: dst})
}

func fixedWalkEnum(w *fixedWalk, e *Enum, id uint64, note string, dst []FixedTerm) {
	w.push(FixedLayoutEntry{ID: id, Kind: TableKindEnum, Size: int64(e.StorageBits / 8), Children: len(e.Variants), Note: note}, FixedDstSpec{Dst: dst})
	for i := range e.Variants {
		w.push(FixedLayoutEntry{ID: TableWireId(e.VariantWireName(i)), Kind: TableKindNoPayload, Size: 0, Children: 0, Note: e.Variants[i]}, FixedDstSpec{})
	}
}

// FixedLayoutBytes is the block exactly as it rides: a u32 entry count and a
// run of seventeen-byte entries, every number little-endian.
func FixedLayoutBytes(entries []FixedLayoutEntry) []byte {
	out := make([]byte, 0, 4+17*len(entries))
	out = appendFixedU32(out, uint32(len(entries)))
	for _, e := range entries {
		out = appendFixedU64(out, e.ID)
		out = append(out, byte(e.Kind))
		out = appendFixedU32(out, uint32(e.Size))
		out = appendFixedU32(out, uint32(e.Children))
	}
	return out
}

func appendFixedU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendFixedU64(b []byte, v uint64) []byte {
	for i := range 8 {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

// FixedLayoutHash is fnv1a64 over the block's bytes exactly as written, and it
// is the eight bytes every record carries (§3.4).
func FixedLayoutHash(block []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range block {
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
type FixedMember struct {
	Owner  string
	Member string
	Value  int64
}

// fixedMembers lists a record's members in declared order with the offset ir's
// C ABI walk gives each, and the names this backend spells them by.
func FixedMembers(u *Unit, st *Struct) []FixedMember {
	ml := RecordLayout(u, st)
	out := []FixedMember{{Owner: st.Name, Value: ml.Size}}
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
			out = append(out, FixedMember{Owner: st.Name, Member: names[k], Value: p.Offset})
		}
	}
	return out
}

// fixedMemberOffset answers one member's byte offset inside its record.
func FixedMemberOffset(u *Unit, st *Struct, member string) int64 {
	for _, m := range FixedMembers(u, st) {
		if m.Member == member {
			return m.Value
		}
	}
	return 0
}

// fixedUnionArmOffset is where every arm of a union sits — the `as` member,
// which is one offset for all of them because they are overlaid.
func FixedUnionArmOffset(u *Unit, un *Union) int64 {
	_, _, _, armOffset := UnionLayout(u, un)
	return armOffset
}

// fixedStorageBytes is sizeof of ONE element's C storage — what the loop over
// an array strides by, and what a keyed array's slot stride is.
func FixedStrideBytes(u *Unit, f *Field) int64 {
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

// FixedNoGuard is the guard of an entry that belongs to no union arm.
const FixedNoGuard = int64(-1)

// The three ops an IDENTITY plan can carry. The other four are a compiled
// plan's, and a plan compiled from another writer's block is built at run time
// by the runtime each backend carries.
const (
	FixedOpCopy  = 0
	FixedOpCount = 1
	FixedOpText  = 2
)

// FixedLeaf is one entry of a plan: byte offsets into the record's body and
// into the reader's own storage, and the op that moves between them.
type FixedLeaf struct {
	Src, Dst, Size, Aux int64
	Guard               int64 // FixedNoGuard when the entry belongs to no arm
	Op, Arg             int
	Note                string // a comment on the emitted array; never a wire byte
}

// fixedLeaves writes one type's leaves at a base the caller gives — the C twin
// of <Name>FixedLeaves.
func fixedLeaves(u *Unit, st *Struct, src, dst int64, out *[]FixedLeaf) {
	off := int64(0)
	for _, f := range st.Fields {
		fixedFieldLeaves(u, st, f, src+off, dst, out)
		off += FixedFieldBytes(f)
	}
}

func fixedFieldLeaves(u *Unit, st *Struct, f *Field, src, dst int64, out *[]FixedLeaf) {
	base := src
	if f.Type.Optional {
		*out = append(*out, FixedLeaf{Src: base, Dst: dst + FixedMemberOffset(u, st, f.Name+"_present"),
			Size: 1, Guard: FixedNoGuard, Op: FixedOpCopy, Note: f.Name + " present"})
		base += FixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		fixedElementLoop(u, st, f, base, dst, f.KeyEnumRef.Max, out)
	case f.Array == ArrayFixed:
		fixedElementLoop(u, st, f, base, dst, f.ArrayBound, out)
	case f.Array == ArrayCounted:
		*out = append(*out, FixedLeaf{Src: base, Dst: dst + FixedMemberOffset(u, st, f.Name+"_count"),
			Size: f.ArrayBound, Guard: FixedNoGuard, Op: FixedOpCount, Note: f.Name + " count"})
		fixedElementLoop(u, st, f, base+FixedCountBytes, dst, f.ArrayBound, out)
	case f.Type.Kind == TString:
		fixedTextLeaf(u, st, f, base, dst, f.Type.Size, 1, out)
	case f.Type.Kind == TWString:
		fixedTextLeaf(u, st, f, base, dst, 2*f.Type.Size, 2, out)
	case f.Type.Kind == TBytes:
		fixedTextLeaf(u, st, f, base, dst, f.Type.Size, 3, out)
	default:
		fixedElementLeavesAt(u, f, base, dst+FixedMemberOffset(u, st, f.Name), f.Name, out)
	}
}

func fixedTextLeaf(u *Unit, st *Struct, f *Field, src, dst, units int64, flavour int, out *[]FixedLeaf) {
	*out = append(*out, FixedLeaf{Src: src, Dst: dst + FixedMemberOffset(u, st, f.Name+"_length"),
		Size: units, Aux: dst + FixedMemberOffset(u, st, f.Name), Guard: FixedNoGuard,
		Op: FixedOpText, Arg: flavour, Note: f.Name})
}

func fixedElementLoop(u *Unit, st *Struct, f *Field, src, dst, count int64, out *[]FixedLeaf) {
	elem := FixedElementBytes(f)
	at := dst + FixedMemberOffset(u, st, f.Name)
	if FixedFlatElem(u, f) {
		// THE WHOLE ARRAY IS ONE RUN: its storage image is its wire image, so
		// the elements need no walk at all and the plan carries one entry
		// however many of them there are.
		*out = append(*out, FixedLeaf{Src: src, Dst: at, Size: count * elem,
			Guard: FixedNoGuard, Op: FixedOpCopy, Note: f.Name + ", whole"})
		return
	}
	stride := FixedStrideBytes(u, f)
	for i := int64(0); i < count; i++ {
		fixedElementLeavesAt(u, f, src+i*elem, at+i*stride, f.Name+"["+strconv.FormatInt(i, 10)+"]", out)
	}
}

// fixedElementLeavesAt is ONE element's leaves at the bases given.
func fixedElementLeavesAt(u *Unit, f *Field, src, dst int64, note string, out *[]FixedLeaf) {
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			fixedLeaves(u, r, src, dst, out)
			return
		case *Union:
			tag := int64(StorageBitsFor(r.Max) / 8)
			arms := FixedUnionArmOffset(u, r)
			*out = append(*out, FixedLeaf{Src: src, Dst: dst, Size: tag,
				Guard: FixedNoGuard, Op: FixedOpCopy, Note: note + " tag"})
			for i, v := range r.Variants {
				// THE GUARD IS STAMPED ON AFTERWARDS, over everything the arm
				// wrote, exactly as the reference does: an arm nested inside an
				// arm answers to the OUTER tag, which is the one that decides
				// whether any of it is there at all.
				at := len(*out)
				fixedElementLeavesAt(u, v.F, src+tag, dst+arms, note+"."+v.Name, out)
				for q := at; q < len(*out); q++ {
					(*out)[q].Guard = src
					(*out)[q].Arg = i + 1
				}
			}
			return
		}
	}
	*out = append(*out, FixedLeaf{Src: src, Dst: dst, Size: FixedElementBytes(f),
		Guard: FixedNoGuard, Op: FixedOpCopy, Note: note})
}

// FixedBuildPlan is the leaf walk and the coalescer together: one type's
// identity plan, exactly as every backend must lay it down. THE PLAN IS
// PARTITIONED — every unguarded entry first, then every guarded one — and
// adjacent COPY entries whose source and destination both advance together are
// one entry. Neighbours are never merged across the split.
func FixedBuildPlan(u *Unit, st *Struct) (plan []FixedLeaf, guarded int) {
	var raw []FixedLeaf
	fixedLeaves(u, st, 0, 0, &raw)
	return fixedCoalesce(raw)
}

func fixedCoalesce(raw []FixedLeaf) (plan []FixedLeaf, guarded int) {
	for pass := 0; pass < 2; pass++ {
		for _, e := range raw {
			if (e.Guard != FixedNoGuard) != (pass == 1) {
				continue
			}
			if len(plan) > guarded {
				last := &plan[len(plan)-1]
				if last.Op == FixedOpCopy && e.Op == FixedOpCopy &&
					last.Guard == e.Guard && last.Arg == e.Arg &&
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
