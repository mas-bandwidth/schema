// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, in Elixir: THE
// LAYOUT — the once-per-carrier description a record's eight-byte hash names —
// and MY SIDE of it.
//
// A fixed-table record is an eight-byte hash of the writer's LAYOUT and then
// the values in declared order, every field at its DECLARED STORAGE WIDTH,
// nothing padded between fields. The width is the DECLARATION's, which SPEC.md
// fixes identically in every port, so the arithmetic below is the C++
// reference's arithmetic and the layout it builds is the same run of bytes —
// which is the point: two writers whose layouts agree byte for byte agree on
// every id, kind, size and position in the closure.
//
// THE CONSTANT SIZES ARE ir's AND NOT THIS BACKEND'S (ir/tablefixed.go). A
// record's constant size is WIRE LAW, the compiler warns and refuses out of the
// same functions the C++ reference emits from, and a port that re-derived them
// privately would be a port whose disagreement showed up as a wrong byte on a
// customer's wire rather than as a red test here.
//
// THE WALK ITSELF is duplicated from cpptable/fixedform.go on purpose, as the
// JavaScript port's is: the reference's walk carries C++ storage facts —
// `offsetof`, `sizeof` — in the same pass, and a port that reached into those
// private helpers would break the day the two files disagree. This way a
// disagreement shows up in the SHARED LAYOUT BYTES instead, held by
// TestFixedLayoutMatchesTheCppReference.
//
// Nothing here touches form 1. The Elixir backend carries no form-1 table wire
// at all (schema#515): this is the first table WIRE this backend has, and it
// reads and writes ONLY form 3.
package elixirtable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The form byte, and the layout's own framing, which §3.4 fixes forever: an
// entry is SEVENTEEN BYTES and the layout is a count and a run of them. There
// is no version byte inside the layout — the FORM BYTE versions everything
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
// OPTIONAL WRAPPER, one child, whose size is one present byte plus the child's.
// It is a LAYOUT kind and not a WIRE kind — nothing rides under it in a record.
const fixedKindOptional = ir.TableKindOptional

// the flavours a `text` plan entry lands its units under.
const (
	fixedTextUtf8  = 1
	fixedTextWide  = 2
	fixedTextBytes = 3
)

func fixedTypeBytes(st *ir.Struct) int64  { return ir.TableFixedTypeBytes(st) }
func fixedFieldBytes(f *ir.Field) int64   { return ir.TableFixedFieldBytes(f) }
func fixedElementBytes(f *ir.Field) int64 { return ir.TableFixedElementBytes(f) }

// fixedUnionTagBytes is the storage width of a union's tag ordinal.
func fixedUnionTagBytes(u *ir.Union) int64 { return int64(ir.StorageBitsFor(u.Max) / 8) }

// fixedSupported reports whether a type's whole closure is one this port lays
// out. A pointer, a map and an unbounded array make their holder VARIABLE
// (§2.2) and never reach here; a GUARDED BRANCH §3.4 refuses outright, for the
// owner's own reason — the lookback conditional exists to make a body vary in
// size, which is what disqualifies a fixed table.
//
// The union-arm restriction is the C++ REFERENCE's and this port keeps it, so
// that the two lay out the same closure or neither does. Elixir could carry a
// union arm holding text or an array — a value here is a term and not a
// committed storage layout — but a port that laid out a closure the reference
// refuses would be a port writing bytes the reference cannot read back, which
// is the one thing the byte oracle exists to prevent.
func fixedSupported(st *ir.Struct, depth int) bool {
	if depth > 16 {
		return false
	}
	for _, f := range st.Fields {
		if f.Guard != "" || f.Type.Pointer || f.IsMap() || f.IsList() || f.Type.Blob() {
			return false
		}
		if f.Type.Kind == ir.TNamed {
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
	}
	return true
}

// ---------------------------------------------------------------------------
// THE LAYOUT, and MY SIDE of it
// ---------------------------------------------------------------------------

// fixedLayoutEntry is one seventeen-byte entry: an id, a kind, a constant size
// and a child count, in the writer's declared order (§3.4).
type fixedLayoutEntry struct {
	id       uint64
	kind     int
	size     int64
	children int
	note     string // a comment on the emitted binary; never a wire byte
}

// fixedDst is MY SIDE of the walk, one row per layout entry: the storage facts
// a layout entry cannot carry, which is what a plan compiled from another
// writer's layout lands values through.
//
// THE READER'S OWN STORAGE IN THIS LANGUAGE IS THE RECORD IMAGE. Elixir has no
// struct layout to take an offsetof of — a %Struct{} is a map and its fields
// are terms, not bytes — so where the C++ reference's rows carry
// `offsetof( T, member )` these carry the field's own byte offset inside THIS
// BUILD's own record image, which is this build's declared order at this
// build's own widths. That image IS the wire's layout, so the identity plan's
// source and destination are the same number and it coalesces to a single run;
// a generated straight-line decode then projects the image into the language's
// own terms in ONE binary pattern match, which is the step C++ gets free
// because there a struct IS its bytes.
type fixedDst struct {
	dst     int64 // this entry's byte offset inside its parent's image
	stride  int64 // an array entry's element stride
	aux     int64 // a text field's buffer offset, a counted array's count, a present flag
	counted int   // an array that carries a live count
	arg     int   // a text field's flavour

	// THE READER'S OWN RANGE, as Elixir literals, and "" when the field
	// declares none. THE BOUNDS DO NOT RIDE on this form, so a plan's `clamp`
	// op holds a value to the READER's own and to nothing else (§3.4).
	lo string
	hi string
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
// order: entry 0 is the ROOT at kind 13, carrying the root's constant size and
// its field count, and every entry is followed immediately by its `child count`
// children.
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

// fixedWalkField pushes one FIELD of a table, at its byte offset inside the
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
		// EVERY SLOT IS WRITTEN, in the key enum's declared order, and no key
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
		// `bytes(N)` is an ARRAY of u8 on this wire, as it is in §3: THE LENGTH,
		// THEN THE UNITS, so its row is a counted array's row — the count at the
		// field's own offset (`aux`) and the units behind it (`dst`). A row that
		// put the units first landed a stranger's blob under its own length.
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			fixedDst{dst: at + fixedCountBytes, stride: 1, aux: at, counted: 1, arg: fixedTextBytes})
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
	lo, hi := fixedRangeOf(f)
	if lo == "nil" {
		lo, hi = "", ""
	}
	w.push(fixedLayoutEntry{id: id, kind: ir.TableWireScalarKind(f), size: size, children: 0, note: note},
		fixedDst{dst: at, lo: lo, hi: hi})
}

// fixedWalkEnum pushes an ENUM and its VARIANTS. A variant's ORDINAL IS ITS
// POSITION IN THE LAYOUT, from 1, and 0 is None — which is what lets a writer
// whose enum gained a variant in the middle be REMAPPED rather than
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
	for i := range 8 {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

// fixedLayoutHash is fnv1a64 over the layout's bytes exactly as written, and it
// is the eight bytes every record carries (§3.4). It is a WIRE IDENTITY and not
// a security claim (§5's note on the same function).
func fixedLayoutHash(layout []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range layout {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

// fixedRoots is every table of the unit the fixed form is emitted for, in
// declaration order.
//
// THERE IS NO LEAF CAP HERE, which is a bound on the C++ REFERENCE and not on
// the wire (§3.4's "held by test"): that port builds its identity plan in an
// array the compiler sizes, and Elixir builds no plan at compile time — the
// identity plan is a literal this emitter writes and any other plan is compiled
// at load time, which is the follow-on §3.4 names for the reference's own
// bound. The RECORD CEILING is the wire's, so it is kept: ir.TableFixedFormRoots
// applies it and the compiler names every table it costs.
func fixedRoots(u *ir.Unit) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range ir.TableFixedFormRoots(u) {
		if !fixedSupported(st, 0) {
			continue
		}
		out = append(out, st)
	}
	return out
}

// fixedClosure is every TYPE the unit's fixed roots reach, with a nested type
// ordered before the type that holds it.
func fixedClosure(roots []*ir.Struct) []*ir.Struct {
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	return order
}

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
