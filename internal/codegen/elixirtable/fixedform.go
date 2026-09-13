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
// THE CONSTANT SIZES, THE WALK, THE LAYOUT AND THE HASH ARE ir's
// (ir/fixedform.go). A second copy of any of them in a second backend is a
// second answer waiting to happen, so this port renders rather than recomputes
// — the same call C and C++ make. What this file spells for itself is Elixir:
// how a destination is a byte offset into THIS BUILD's own record IMAGE (there
// is no offsetof here). Destination rows carry destinations and nothing about
// a range: the generated projection holds the bound after the loop.
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

// fixedDst is MY SIDE of one layout entry: the storage facts a layout entry
// cannot carry, which is what a plan compiled from another writer's layout
// lands values through.
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
//
// THE WALK IS ir.TableFixedWalkRoot. A `bytes(N)` destination row is an
// ARRAY's — Dst the buffer, Aux the live count — because the TEXT row is the
// other way round (Dst the length, Aux the buffer). The identity plan lands
// `bytes(N)` with the TEXT op and reads neither column; a plan compiled from
// this row is what `compile_array` walks, and a text-convention row handed it
// a count destination that was the buffer's first four bytes.
type fixedDst struct {
	dst     int64 // this entry's byte offset inside its parent's image
	stride  int64 // an array entry's element stride
	aux     int64 // a text field's buffer offset, a counted array's count, a present flag
	counted int   // an array that carries a live count
	arg     int   // a text field's flavour
}

// imageDstRows renders ir's destination spec into this port's image offsets,
// one row per layout entry, in the same order TableFixedWalkRoot produced.
func imageDstRows(u *ir.Unit, entries []ir.TableFixedLayoutEntry) []fixedDst {
	out := make([]fixedDst, len(entries))
	for i, e := range entries {
		out[i] = imageDstRow(u, e)
	}
	return out
}

func imageDstRow(u *ir.Unit, e ir.TableFixedLayoutEntry) fixedDst {
	d := e.Dst
	stride := int64(0)
	switch {
	case d.Stride1:
		stride = 1
	case d.Stride != nil:
		// THE IMAGE'S STRIDE, not C sizeof: this port's storage is the wire.
		stride = ir.TableFixedElementBytes(d.Stride)
	}
	dst := imageTerms(u, d.Dst)
	aux := imageTerms(u, d.Aux)
	if d.AuxExtendsDst {
		// a union's tag sits INSIDE the storage Dst named, and on this image
		// the tag leads that storage, so the two offsets are the same byte
		aux = dst + imageTerms(u, d.Aux)
	}
	return fixedDst{dst: dst, stride: stride, aux: aux, counted: d.Counted, arg: d.Meta}
}

func imageTerms(u *ir.Unit, terms []ir.TableFixedTerm) int64 {
	var n int64
	for _, t := range terms {
		n += imageTermOffset(u, t)
	}
	return n
}

func imageTermOffset(u *ir.Unit, t ir.TableFixedTerm) int64 {
	if un := fixedNamedUnion(u, t.Type); un != nil {
		if t.Member == "type" {
			return 0
		}
		// every arm overlays the same byte: the tag's width, no C padding
		return fixedUnionTagBytes(un)
	}
	if st := fixedNamedStruct(u, t.Type); st != nil {
		return imageMemberOffset(st, t.Member)
	}
	return 0
}

func fixedNamedStruct(u *ir.Unit, name string) *ir.Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

func fixedNamedUnion(u *ir.Unit, name string) *ir.Union {
	if un := u.Unions[name]; un != nil {
		return un
	}
	return u.TableUnions[name]
}

// imageMemberOffset is one member's byte offset inside its owner's IMAGE —
// declared order at declared widths, nothing padded between fields. It is not
// ir.TableFixedMemberOffset, which answers the C ABI and would hand this port
// a buffer that sits where the length lives.
func imageMemberOffset(st *ir.Struct, member string) int64 {
	var at int64
	for _, f := range st.Fields {
		if off, ok := imageFieldMember(f, at, member); ok {
			return off
		}
		at += ir.TableFixedFieldBytes(f)
	}
	return 0
}

func imageFieldMember(f *ir.Field, at int64, member string) (int64, bool) {
	payload := at
	if f.Type.Optional {
		if member == f.Name+"_present" {
			return at, true
		}
		payload = at + ir.TableFixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		if member == f.Name {
			return payload, true
		}
	case f.Array == ir.ArrayCounted:
		if member == f.Name+"_count" {
			return payload, true
		}
		if member == f.Name {
			return payload + ir.TableFixedCountBytes, true
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		if member == f.Name+"_length" {
			return payload, true
		}
		if member == f.Name {
			return payload + ir.TableFixedCountBytes, true
		}
	default:
		if member == f.Name {
			return payload, true
		}
	}
	return 0, false
}

// fixedRoots is every table of the unit the fixed form is emitted for, in
// declaration order.
//
// THERE IS NO LEAF CAP HERE, which is a bound on the C++ REFERENCE and not on
// the wire (§3.4's "held by test"): that port builds its identity plan in an
// array the compiler sizes, and Elixir builds no plan at compile time — the
// identity plan is a literal this emitter writes, and every OTHER plan is built
// ONCE at MODULE LOAD from the lock's own bytes (§5.9 #3), which is build time
// for docs/FIXED-FORM-ALGORITHM.md §5.2 and never a load-path compile. NOTHING
// ON THE LOAD PATH COMPILES: the sentence this comment used to carry — "any
// other plan is compiled at load time" — described the shape §5.6 retired. The
// RECORD CEILING is the wire's, so it is kept: ir.TableFixedFormRoots applies it
// and the compiler names every table it costs.
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
