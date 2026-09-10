// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, in JavaScript: the
// LAYOUT, the vocabulary block and its hash.
//
// A fixed-table record is an eight-byte hash of the writer's vocabulary block
// and then the values in declared order, every field at its DECLARED STORAGE
// WIDTH, nothing padded between fields. The width is the DECLARATION's, which
// SPEC.md fixes identically in every port, so the layout in this file is the
// same arithmetic the C++ reference does and the block it builds is the same
// run of bytes — which is the point: two writers whose blocks agree byte for
// byte agree on every id, kind, size and position in the closure.
//
// THE WALK IS DUPLICATED FROM cpptable ON PURPOSE, exactly as this package's
// wire kinds are (jstable.go): a port that derived the block from the
// reference emitter's private helpers would break the day the two files
// disagree, and this way a disagreement shows up in the SHARED GOLDEN BYTES
// instead — held by TestFixedLayoutMatchesReference, which builds the block for
// the paired bench's own type and checks it against the reference's constants.
//
// Nothing here touches form 1. JavaScript carries no form 1 for tables at all
// (schema#516): this is the first table wire this backend has, and it reads
// and writes ONLY form 3.
package jstable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedKindOptional is the ONE kind §3.4's block adds to §3's closed set: the
// OPTIONAL WRAPPER, one child, whose size is one present byte plus the child's.
// It is a BLOCK kind and not a WIRE kind — nothing rides under it in a record.
const fixedKindOptional = 35

// The block's own framing, which §3.4 fixes forever: an entry is seventeen
// bytes and the block is a count and a run of them.
const (
	fixedEntryBytes   = 17
	fixedCountBytes   = int64(4) // a count and a length are the int32 their storage already is
	fixedPresentBytes = int64(1)
	fixedHashBytes    = int64(8)
	fixedFormByte     = 3
)

// ---------------------------------------------------------------------------
// THE LAYOUT: a constant size per field
// ---------------------------------------------------------------------------

// fixedStorageBytes is a LEAF's declared storage width — the width SPEC.md
// gives the declaration, identical in every port.
func fixedStorageBytes(t ir.FieldType) int64 {
	switch t.Kind {
	case ir.TBool:
		return 1
	case ir.TInt, ir.TFixed:
		return int64(t.Width / 8)
	case ir.TBits:
		if t.Width <= 32 {
			return 4
		}
		return 8
	case ir.TFloat32:
		return 4
	case ir.TFloat64:
		return 8
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			return int64(r.StorageBits / 8)
		case *ir.Flags:
			return 8 // the raw mask, as §3 carries it
		}
	}
	return 0
}

// fixedTypeBytes is C(T), the sum of its fields'. Every one is a compile-time
// constant, which is why a JavaScript Measure is a constant and not a walk.
func fixedTypeBytes(st *ir.Struct) int64 {
	var n int64
	for _, f := range st.Fields {
		n += fixedFieldBytes(f)
	}
	return n
}

// fixedFieldBytes is C(f) — §3.4's constant-size table in one function.
func fixedFieldBytes(f *ir.Field) int64 {
	var payload int64
	switch {
	case f.KeyEnum != "":
		payload = f.KeyEnumRef.Max * fixedElementBytes(f)
	case f.Array == ir.ArrayFixed:
		payload = f.ArrayBound * fixedElementBytes(f)
	case f.Array == ir.ArrayCounted:
		payload = fixedCountBytes + f.ArrayBound*fixedElementBytes(f)
	case f.Type.Kind == ir.TString:
		payload = fixedCountBytes + f.Type.Size
	case f.Type.Kind == ir.TWString:
		payload = fixedCountBytes + 2*f.Type.Size
	case f.Type.Kind == ir.TBytes:
		payload = fixedCountBytes + f.Type.Size
	default:
		payload = fixedElementBytes(f)
	}
	if f.Type.Optional {
		return fixedPresentBytes + payload
	}
	return payload
}

// fixedElementBytes is the constant of ONE element of a field.
func fixedElementBytes(f *ir.Field) int64 {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedTypeBytes(r)
		case *ir.Union:
			widest := int64(0)
			for _, v := range r.Variants {
				if n := fixedFieldBytes(v.F); n > widest {
					widest = n
				}
			}
			return int64(ir.StorageBitsFor(r.Max)/8) + widest
		}
	}
	return fixedStorageBytes(f.Type)
}

// fixedUnionTagBytes is the storage width of a union's tag ordinal.
func fixedUnionTagBytes(u *ir.Union) int64 { return int64(ir.StorageBitsFor(u.Max) / 8) }

// fixedSupported reports whether a type's whole closure is one this backend
// carries. A pointer, a map and an unbounded array make their holder VARIABLE
// and never reach here; a GUARDED BRANCH §3.4 refuses outright, for the
// owner's own reason — the lookback conditional exists to make a body variable,
// which disqualifies a fixed table.
//
// The union-arm restriction is the C++ reference's and this port keeps it, so
// that the two lay out the same closure or neither does.
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
// THE VOCABULARY BLOCK, and MY SIDE of it
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

// fixedDst is MY side of the walk, one row per block entry: the storage facts
// a block entry cannot carry, which is what a plan compiled from another
// writer's block lands values through.
//
// THE READER'S OWN STORAGE IN THIS LANGUAGE IS THE CANONICAL BODY IMAGE — this
// build's own declared order at this build's own widths, which is exactly the
// wire's layout. JavaScript has no struct layout to take an offsetof of
// (jstable.go's opening statement), so where the C++ reference's rows carry
// `offsetof( T, member )` these carry the field's own offset inside the body,
// and the identity plan's source and destination are the same number.
type fixedDst struct {
	dst     int64 // this entry's byte offset inside its parent's image
	stride  int64 // an array entry's element stride
	aux     int64 // a text field's buffer offset, a counted array's count, a present flag
	counted int   // an array that carries a live count
	arg     int   // a text field's flavour
}

// the text flavours, which say how a `text` entry lands its units.
const (
	fixedTextUtf8  = 1
	fixedTextWide  = 2
	fixedTextBytes = 3
)

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

// fixedWalkField pushes one FIELD of a table, at its byte offset inside the
// table's own image.
func fixedWalkField(w *fixedWalk, f *ir.Field, at int64) {
	id := ir.TableFieldWireId(f)
	size := fixedFieldBytes(f)
	if f.Type.Optional {
		// the OPTIONAL WRAPPER: the present flag rides at the field's own
		// offset and the payload WHOLE behind it
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
		// `bytes(N)` is an ARRAY of u8 on this wire, as it is in §3, so the row
		// is the COUNTED ARRAY's row and not the text row: on an array entry
		// `dst` is the ELEMENT BASE and `aux` is where the count lands, which
		// is the pair the plan compiler's array case reads (fixedruntime.go,
		// `case 14`). Spelling it the other way round — the text row's
		// order — landed the count in the buffer and the first four content
		// bytes in the used length, on every record a compiled plan read.
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
	w.push(fixedLayoutEntry{id: id, kind: ir.TableWireScalarKind(f), size: size, children: 0, note: note},
		fixedDst{dst: at})
}

// fixedWalkEnum pushes an ENUM and its VARIANTS. A variant's ORDINAL IS ITS
// POSITION IN THE BLOCK, from 1, and 0 is None — which is what lets a writer
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

// fixedLayoutBytes is the block exactly as it rides: a u32 entry count and a
// run of seventeen-byte entries, every number little-endian.
func fixedLayoutBytes(entries []fixedLayoutEntry) []byte {
	out := make([]byte, 0, 4+fixedEntryBytes*len(entries))
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

// fixedLayoutHash is fnv1a64 over the block's bytes exactly as written, and it
// is the eight bytes every record carries (§3.4). It is a WIRE IDENTITY and
// not a security claim.
func fixedLayoutHash(block []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range block {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

// fixedRoots is every table of a unit the fixed form is emitted for. Unlike
// the C++ reference there is no leaf cap here: JavaScript builds no plan at
// compile time, so there is no constexpr array for a type to overflow — the
// identity plan is a constant this emitter writes and any other plan is
// compiled at load time, which is the follow-on §3.4 names for the reference's
// own bound.
func fixedRoots(u *ir.Unit, tables []*ir.Struct) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range tables {
		// THE FILTER IS THE REFUSAL ITSELF, and it has to be: a form emitted
		// for a table the refusal names is a module that says in a comment it
		// carries nothing and then carries it anyway. That is how the optional
		// hole shipped — fixedRefusal named `?T` while this loop asked only
		// fixedSupported, so the writer emitted a body with no present byte in
		// it and the comment above said the form was not there.
		if !st.IsTable || st.IsMapEntry() || fixedRefusal(st) != "" {
			continue
		}
		_ = u
		out = append(out, st)
	}
	return out
}

// fixedCollectTypes orders a root's closure so that a nested type's helpers are
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
				if s, ok := v.F.Type.Ref.(*ir.Struct); ok && v.F.Type.Kind == ir.TNamed {
					fixedCollectTypes(s, seen, order)
				}
			}
		}
	}
	*order = append(*order, st)
}
