// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, in Dart: the LAYOUT,
// its hash, and MY side of it.
//
// A fixed-table record is an eight-byte hash of the writer's LAYOUT and then
// the values in declared order, every field at its DECLARED STORAGE WIDTH,
// nothing padded between fields. The width is the DECLARATION's, which SPEC.md
// fixes identically in every port, so the arithmetic below is not this
// backend's: it is [ir.TableFixedFieldBytes] and its neighbours, the same
// functions the C++ reference emits its layout and its template from and the
// same ones the compiler measures §3.4's two size bounds with. A backend that
// re-derived them would drift from the reference the day one of them moved.
//
// THE WALK is duplicated from cpptable's on purpose — the same rule this
// package's wire kinds already follow — because a port that reached into the
// reference emitter's unexported helpers would break the day the two files
// disagree, and this way a disagreement shows up in the SHARED ORACLE BYTES
// (`make tables-fixedform-corpus`) instead.
//
// Nothing here touches form 1. Dart carries no form-1 table wire at all
// (schema#514): this is the first table wire this backend has, and it reads and
// writes ONLY form 3.
package darttable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The form byte and the layout's own framing, which §3.4 fixes forever: an
// entry is seventeen bytes and the layout is a count and a run of them. THERE
// IS NO VERSION BYTE INSIDE THE LAYOUT — the form byte versions everything
// behind it, the layout's own format included.
const (
	fixedFormByte     = 3
	fixedEntryBytes   = ir.TableFixedEntryBytes
	fixedHeaderBytes  = ir.TableFixedLayoutHeaderBytes
	fixedCountBytes   = ir.TableFixedCountBytes
	fixedPresentBytes = ir.TableFixedPresentBytes
	fixedHashBytes    = int64(8)
)

// fixedKindOptional is the ONE kind §3.4's layout adds to §3's closed set: the
// OPTIONAL WRAPPER, one child, whose size is one present byte plus its
// child's. It is a LAYOUT kind and not a WIRE kind — nothing rides under it in
// a record — and it is ir's, beside the rest of the kind vocabulary, because
// it is wire law and not this backend's.
const fixedKindOptional = ir.TableKindOptional

// the text flavours, which say how a `text` plan entry lands its units.
const (
	fixedTextUtf8  = 1
	fixedTextWide  = 2
	fixedTextBytes = 3
)

// ---------------------------------------------------------------------------
// THE CONSTANT SIZE, KIND BY KIND, IS ir's
// ---------------------------------------------------------------------------

func fixedStorageBytes(t ir.FieldType) int64 { return ir.TableFixedStorageBytes(t) }
func fixedTypeBytes(st *ir.Struct) int64     { return ir.TableFixedTypeBytes(st) }
func fixedFieldBytes(f *ir.Field) int64      { return ir.TableFixedFieldBytes(f) }
func fixedElementBytes(f *ir.Field) int64    { return ir.TableFixedElementBytes(f) }

// fixedUnionTagBytes is the storage width of a union's tag ordinal.
func fixedUnionTagBytes(u *ir.Union) int64 { return int64(ir.StorageBitsFor(u.Max) / 8) }

// ---------------------------------------------------------------------------
// WHAT THIS BACKEND LAYS OUT, AND WHAT IT REFUSES BY NAME
// ---------------------------------------------------------------------------

// fixedSupported reports whether a type's whole closure is one this backend
// lays out. A pointer, a map and an unbounded array make their holder VARIABLE
// (§2.2) and never reach here; what this adds is the two shapes the C++
// reference does not lay out either — a union arm carrying text or an array,
// and a guarded branch, which §3.4 refuses outright for the owner's own reason.
//
// The union-arm restriction is the reference's and this port keeps it, so that
// the two lay out the same closure or neither does.
func fixedSupported(st *ir.Struct, depth int) bool {
	if depth > 16 {
		return false
	}
	for _, f := range st.Fields {
		if f.Guard != "" || f.Type.Pointer || f.IsMap() || f.IsList() || f.Type.Blob() {
			return false
		}
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if !fixedSupported(r, depth+1) {
				return false
			}
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F == nil {
					return false // a void arm has no storage this walk can name
				}
				a := v.F
				if a.Type.Pointer || a.IsMap() || a.IsList() || a.Array != ir.ArrayNone || a.KeyEnum != "" ||
					a.Type.Kind == ir.TString || a.Type.Kind == ir.TWString || a.Type.Kind == ir.TBytes || a.Type.Optional {
					return false
				}
				if s, ok := a.Type.Ref.(*ir.Struct); ok && a.Type.Kind == ir.TNamed && !fixedSupported(s, depth+1) {
					return false
				}
			}
		}
	}
	return true
}

// fixedRefusal answers why a table cannot carry the fixed form in Dart, or ""
// when it can. NAMED, NEVER SILENT is the property: a table this backend will
// not emit the form for gets a line in the generated library saying which
// table and why, so a consumer reaching for Save gets a missing name from the
// analyzer beside a file that says the reason.
func fixedRefusal(st *ir.Struct) string {
	if !fixedSupported(st, 0) {
		return "its closure carries a construct the fixed class refuses — a pointer, a map, an unbounded array, a guarded branch, or a union arm holding text, an array or an optional (docs/SPEC-TABLES.md §3.4)"
	}
	if n := fixedOptionalInPacketType(st, 0); n != "" {
		// §3.4's ONE departure from §2.3: on this form `?T` and a plain `T`
		// nesting are ONE BYTE APART, because the present flag rides. A TABLE's
		// storage class is this emitter's own, so the present flag lands in a
		// member this file writes; a `type`'s class belongs to
		// internal/codegen/dart, which emits no presence member because the
		// packet wire has no optional at all (SPEC §4.2). Inventing one here
		// would be this backend adding storage to a class another emitter owns.
		return "field " + n + " is OPTIONAL inside a `type`, whose Dart class internal/codegen/dart owns and which carries no presence member for the present flag (docs/SPEC-TABLES.md §3.4, §2.3)"
	}
	if fixedTypeBytes(st) > ir.TableFixedRecordMaxBytes {
		return "its record body is past §3.4's 65536-byte ceiling, so no conforming reader would decode it"
	}
	return ""
}

// fixedOptionalInPacketType names the first optional field declared in a
// non-table `type` of the closure, which is the one shape this backend cannot
// place a present flag in.
func fixedOptionalInPacketType(st *ir.Struct, depth int) string {
	if depth > 16 {
		return ""
	}
	for _, f := range st.Fields {
		if f.Type.Optional && !st.IsTable {
			return st.Name + "." + f.Name
		}
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if n := fixedOptionalInPacketType(r, depth+1); n != "" {
				return n
			}
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				if s, ok := v.F.Type.Ref.(*ir.Struct); ok && v.F.Type.Kind == ir.TNamed {
					if n := fixedOptionalInPacketType(s, depth+1); n != "" {
						return n
					}
				}
			}
		}
	}
	return ""
}

// fixedRoots is every table of a file the fixed form is emitted for.
//
// THERE IS NO LEAF CAP HERE, unlike the C++ reference. Dart builds no plan at
// compile time — the identity plan is a constant this emitter writes and any
// other plan is compiled at load time — so there is no constexpr array for a
// type to overflow, which is the follow-on §3.4 names for the reference's own
// bound reached ahead of the reference.
func fixedRoots(tables []*ir.Struct) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range tables {
		if !st.IsTable || st.IsMapEntry() || fixedRefusal(st) != "" {
			continue
		}
		out = append(out, st)
	}
	return out
}

// fixedCollectTypes orders a root's closure so a nested type's helpers are
// emitted before the type that uses them.
func fixedCollectTypes(st *ir.Struct, seen map[string]bool, order *[]*ir.Struct) {
	if seen[st.Name] {
		return
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fixedCollectTypes(r, seen, order)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				if s, ok := v.F.Type.Ref.(*ir.Struct); ok && v.F.Type.Kind == ir.TNamed {
					fixedCollectTypes(s, seen, order)
				}
			}
		}
	}
	*order = append(*order, st)
}

// ---------------------------------------------------------------------------
// THE LAYOUT, AND MY SIDE OF IT
// ---------------------------------------------------------------------------

// fixedLayoutEntry is one seventeen-byte entry: an id, a kind, a constant size
// and a child count, in the writer's declared order (§3.4).
type fixedLayoutEntry struct {
	id       uint64
	kind     int
	size     int64
	children int
	note     string // a comment on the emitted array; never a wire byte
}

// fixedDst is MY side of the walk, one row per layout entry: the storage facts
// a layout entry cannot carry, which is what a plan compiled from another
// writer's layout lands values through.
//
// THE READER'S OWN STORAGE IN THIS LANGUAGE IS THE CANONICAL BODY IMAGE — this
// build's declared order at this build's own widths, which is exactly the
// wire's layout. Dart has no struct layout to take an offsetof of, so where
// the C++ reference's rows carry `offsetof( T, member )` these carry the
// field's own offset inside the body, and the identity plan's source and
// destination are the same number.
type fixedDst struct {
	dst     int64 // this entry's byte offset inside its parent's image
	stride  int64 // an array entry's element stride
	aux     int64 // a text field's buffer offset, a counted array's count, a present flag
	counted int   // an array that carries a live count
	arg     int   // a text field's flavour
}

type fixedWalk struct {
	entries []fixedLayoutEntry
	dst     []fixedDst
}

func (w *fixedWalk) push(e fixedLayoutEntry, d fixedDst) {
	w.entries = append(w.entries, e)
	w.dst = append(w.dst, d)
}

// fixedWalkRoot walks the root's closure PRE-ORDER in the writer's declared
// order: entry 0 is the ROOT at kind 13, and every entry is followed
// immediately by its child count children.
func fixedWalkRoot(st *ir.Struct) *fixedWalk {
	w := &fixedWalk{}
	w.push(fixedLayoutEntry{
		id:       ir.TableWireId(st.WireName()),
		kind:     ir.TableKindTable,
		size:     fixedTypeBytes(st),
		children: len(st.Fields),
		note:     st.Name,
	}, fixedDst{})
	var at int64
	for _, f := range st.Fields {
		fixedWalkField(w, f, at)
		at += fixedFieldBytes(f)
	}
	return w
}

// fixedWalkField pushes one FIELD of a table at its byte offset inside the
// table's own image.
func fixedWalkField(w *fixedWalk, f *ir.Field, at int64) {
	id := ir.TableFieldWireId(f)
	size := fixedFieldBytes(f)
	if f.Type.Optional {
		// the OPTIONAL WRAPPER: the present flag rides at the field's own
		// offset and the payload WHOLE behind it, present or not
		w.push(fixedLayoutEntry{id: id, kind: fixedKindOptional, size: size, children: 1, note: f.Name + " ?"},
			fixedDst{aux: at})
		fixedWalkPayload(w, f, id, size-fixedPresentBytes, at+fixedPresentBytes)
		return
	}
	fixedWalkPayload(w, f, id, size, at)
}

func fixedWalkPayload(w *fixedWalk, f *ir.Field, id uint64, size, at int64) {
	switch {
	case f.KeyEnum != "":
		// every slot is written, in the key enum's declared order, and no key
		// rides: the field's own offset is the slot base
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindKeyed, size: size, children: 2, note: f.Name},
			fixedDst{dst: at, stride: fixedElementBytes(f)})
		fixedWalkEnum(w, f.KeyEnumRef, ir.TableWireId(f.KeyEnum), f.KeyEnum, 0)
		fixedWalkElement(w, f, 0, "element", 0)
	case f.Array != ir.ArrayNone:
		counted, aux, base := 0, int64(0), at
		if f.Array == ir.ArrayCounted {
			// the count, then MAX elements, the slack zero-filled
			counted, aux, base = 1, at, at+fixedCountBytes
		}
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			fixedDst{dst: base, stride: fixedElementBytes(f), aux: aux, counted: counted})
		fixedWalkElement(w, f, 0, "element", 0)
	case f.Type.Kind == ir.TBytes:
		// `bytes(N)` is an ARRAY of u8 on this wire, as it is in §3, and the
		// text flavour on the row is what lands it in one move
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			fixedDst{dst: at, stride: 1, aux: at + fixedCountBytes, counted: 1, arg: fixedTextBytes})
		w.push(fixedLayoutEntry{id: 0, kind: ir.TableKindU8, size: 1, children: 0, note: "u8"}, fixedDst{})
	case f.Type.Kind == ir.TString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindString, size: size, children: 0, note: f.Name},
			fixedDst{dst: at, aux: at + fixedCountBytes, arg: fixedTextUtf8})
	case f.Type.Kind == ir.TWString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindWstring, size: size, children: 0, note: f.Name},
			fixedDst{dst: at, aux: at + fixedCountBytes, arg: fixedTextWide})
	default:
		fixedWalkElement(w, f, id, f.Name, at)
	}
}

func fixedWalkElement(w *fixedWalk, f *ir.Field, id uint64, note string, at int64) {
	size := fixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindTable, size: size, children: len(r.Fields), note: note},
				fixedDst{dst: at})
			var sub int64
			for _, sf := range r.Fields {
				fixedWalkField(w, sf, sub)
				sub += fixedFieldBytes(sf)
			}
			return
		case *ir.Union:
			// the tag ordinal at its tag type's storage width, then the WIDEST
			// arm, the slack behind a narrower arm zero-filled
			tag := fixedUnionTagBytes(r)
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindUnion, size: size, children: len(r.Variants), note: note},
				fixedDst{dst: at, aux: at})
			for _, v := range r.Variants {
				fixedWalkElement(w, v.F, ir.TableWireId(v.WireName()), v.Name, tag)
			}
			return
		case *ir.Enum:
			fixedWalkEnum(w, r, id, note, at)
			return
		}
	}
	w.push(fixedLayoutEntry{id: id, kind: ir.TableWireScalarKind(f), size: size, children: 0, note: note},
		fixedDst{dst: at})
}

// fixedWalkEnum pushes an ENUM and its VARIANTS. A variant's ORDINAL IS ITS
// POSITION IN THE LAYOUT, from 1, and 0 is None — which is what lets a writer
// whose enum gained a variant in the middle be remapped rather than
// reinterpreted.
func fixedWalkEnum(w *fixedWalk, e *ir.Enum, id uint64, note string, at int64) {
	w.push(fixedLayoutEntry{id: id, kind: ir.TableKindEnum, size: int64(e.StorageBits / 8), children: len(e.Variants), note: note},
		fixedDst{dst: at})
	for i := range e.Variants {
		w.push(fixedLayoutEntry{id: ir.TableWireId(e.VariantWireName(i)), kind: ir.TableKindNoPayload, size: 0, children: 0, note: e.Variants[i]},
			fixedDst{})
	}
}

// fixedLayoutBytes is the LAYOUT exactly as it rides: a u32 entry count and a
// run of seventeen-byte entries, every number little-endian.
func fixedLayoutBytes(entries []fixedLayoutEntry) []byte {
	out := make([]byte, 0, fixedHeaderBytes+fixedEntryBytes*len(entries))
	out = appendFixedU32(out, uint32(len(entries)))
	for _, e := range entries {
		out = appendFixedU64(out, e.id)
		out = append(out, byte(e.kind))
		out = appendFixedU32(out, uint32(e.size))
		out = appendFixedU32(out, uint32(e.children))
	}
	return out
}

func appendFixedU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendFixedU64(b []byte, v uint64) []byte {
	for i := 0; i < 8; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

// fixedLayoutHash is fnv1a64 over the layout's bytes exactly as written, and
// it is the eight bytes every record carries (§3.4). It is a WIRE IDENTITY and
// not a security claim.
func fixedLayoutHash(layout []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range layout {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}
