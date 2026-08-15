// Wire-width computation (SPEC §6.1 item 4): target-independent, so every
// backend derives the same MaxBits from one implementation.
package ir

import (
	"math"
	"math/big"
	"math/bits"
	"sort"

	"github.com/mas-bandwidth/schema/internal/ast"
)

// BitsRequired mirrors the runtimes' bits_required(min, max): the bit length
// of (max - min). Streams require min < max, so the zero case cannot arise.
func BitsRequired(min, max *big.Int) int64 {
	diff := new(big.Int).Sub(max, min)
	return int64(diff.BitLen())
}

// MaxBytes converts a worst-case bit count into the buffer size that holds
// it, rounded UP to the 8-byte write-buffer granularity every serialize
// runtime requires — a constant advertised for sizing write buffers must be
// directly usable as one, and conservative is correct for a buffer bound
// (SPEC §6.1 item 4).
func MaxBytes(bits int64) int64 {
	bytes := (bits + 7) / 8
	return (bytes + 7) / 8 * 8
}

// CompressedFloatParams replicates serialize_compressed_float's parameter
// derivation exactly (float32 arithmetic, the clamp, the ceil): the step count
// the writer quantizes onto, and the wire width that carries it. One
// implementation, so the widths the backends advertise and the bytes the data
// compiler packs cannot drift apart.
func CompressedFloatParams(fmin, fmax, res float64) (maxIntegerValue uint64, wireBits int64) {
	delta := float32(fmax) - float32(fmin)
	values := delta / float32(res)
	if !(values >= 1.0) { // the runtime's own form — it also catches NaN
		values = 1.0
	}
	if values > 4294967040.0 {
		values = 4294967040.0
	}
	maxIntegerValue = uint64(math.Ceil(float64(values)))
	return maxIntegerValue, int64(bits.Len64(maxIntegerValue))
}

// CompressedFloatBits is the wire width alone.
func CompressedFloatBits(fmin, fmax, res float64) int64 {
	_, wireBits := CompressedFloatParams(fmin, fmax, res)
	return wireBits
}

// MaxBitsField is one field's worst-case wire bits, alignment points counted
// at the worst 7.
func MaxBitsField(f *Field) int64 {
	elem := maxBitsScalar(f)
	switch f.Array {
	case ArrayFixed:
		return f.ArrayBound * elem
	case ArrayCounted:
		return BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)) + f.ArrayBound*elem
	default:
		return elem
	}
}

func maxBitsScalar(f *Field) int64 {
	switch f.Type.Kind {
	case TInt:
		if f.HasIntRange {
			return BitsRequired(f.IntMin, f.IntMax)
		}
		return int64(f.Type.Width) // bare: raw storage-width bits (uint128 = 128)
	case TFixed:
		// the raw range is the whole-unit range shifted by F, and shifting a
		// range left by F adds exactly F to its bit length (STANDARD.md,
		// fixed) — EXCEPT the degenerate range, which costs zero bits, not F
		// (SPEC §4.6, decided 2026-08-15; bitlen(B−A) + F generalizes wrong
		// at exactly B == A, which is how two ports came to emit
		// fraction_bits on the wide path)
		if f.IntMin.Cmp(f.IntMax) == 0 {
			return 0
		}
		return BitsRequired(f.IntMin, f.IntMax) + int64(f.Type.FracBits)
	case TBits:
		return int64(f.Type.Width)
	case TBool:
		return 1
	case TFloat32:
		if f.HasFloatRange {
			return CompressedFloatBits(f.FMin, f.FMax, f.Resolution)
		}
		return 32
	case TFloat64:
		return 64
	case TString, TBytes:
		// length prefix + worst-case align pad + the bytes
		return BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)) + 7 + f.Type.Size*8
	case TNamed:
		switch ref := f.Type.Ref.(type) {
		case *Enum:
			return BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
		case *Flags:
			return int64(ref.WireBits)
		case *Struct:
			return MaxBitsStruct(ref)
		}
	}
	return 0
}

// MaxBitsStruct is the longest wire path through a struct: branches take the
// larger side (composition cycles are compile errors, so recursion ends).
func MaxBitsStruct(st *Struct) int64 {
	var walk func(items []Item) int64
	walk = func(items []Item) int64 {
		var total int64
		for _, item := range items {
			switch item := item.(type) {
			case *FieldItem:
				total += MaxBitsField(item.F)
			case *Branch:
				then, els := walk(item.Then), walk(item.Else)
				if then > els {
					total += then
				} else {
					total += els
				}
			case *ConstItem:
				total += item.Bits
			case *ReservedItem:
				total += item.Bits
			case *AlignItem:
				total += 7
			}
		}
		return total
	}
	return walk(st.Items)
}

// ---- static byte-alignment analysis ----
//
// wirePos tracks the wire bit position modulo 8 through a struct's items.
// known=false means the position cannot be determined statically; the
// analysis is conservative — unknown never enables an optimization, so a
// wrong "unknown" costs speed, never wire bytes.
type wirePos struct {
	mod   int64
	known bool
}

func (p wirePos) add(bits int64) wirePos {
	if !p.known {
		return p
	}
	return wirePos{mod: ((p.mod+bits)%8 + 8) % 8, known: true}
}

var wireUnknown = wirePos{known: false}
var wireAligned = wirePos{mod: 0, known: true}

// AlignedFixedByteArrays returns the fixed [N]uint8 array fields of st whose
// element bytes are statically guaranteed to begin on a byte boundary —
// after an `align` item, after a string/bytes field (whose wire ends on a
// byte boundary by §4.7), or any position provably ≡ 0 (mod 8) from there.
//
// At such a site, N consecutive unranged 8-bit writes produce exactly the N
// array bytes in stream order (§4.3: bit i of the stream lives in byte i/8),
// which is byte-identical to serialize's align-then-memcpy bulk path — the
// align contributes zero bits when already aligned. A backend may therefore
// serialize these fields with SerializeBytes instead of a per-byte loop
// WITHOUT changing the wire. Fields not in the map must keep the per-byte
// loop: bulk-copying them would insert padding the wire does not have.
//
// Entry position is unknown (a struct may be embedded at any bit offset),
// so nothing before the first alignment-forcing item is ever marked.
// Counted arrays ([<= N]uint8) are tracked for position but not marked —
// extending the bulk path to them is future work.
func AlignedFixedByteArrays(st *Struct) map[*Field]bool {
	out := map[*Field]bool{}
	walkAligned(st.Items, wireUnknown, out)
	return out
}

// walkAligned advances the position across items, marking qualifying fields
// into out (nil out = position tracking only, used for nested structs).
func walkAligned(items []Item, pos wirePos, out map[*Field]bool) wirePos {
	for _, item := range items {
		switch item := item.(type) {
		case *AlignItem:
			pos = wireAligned
		case *ConstItem:
			pos = pos.add(item.Bits)
		case *ReservedItem:
			pos = pos.add(item.Bits)
		case *Branch:
			then := walkAligned(item.Then, pos, out)
			els := pos
			if item.Else != nil {
				els = walkAligned(item.Else, pos, out)
			}
			// the untaken side writes nothing, so the position after the
			// branch is known only when both sides agree
			if then.known && els.known && then.mod == els.mod {
				pos = then
			} else {
				pos = wireUnknown
			}
		case *FieldItem:
			f := item.F
			if out != nil && isFixedByteArray(f) && pos.known && pos.mod == 0 {
				out[f] = true
			}
			pos = afterField(f, pos)
		}
	}
	return pos
}

// isFixedByteArray: a fixed [N]uint8 array with the full 8-bit wire — the
// shape whose per-element wire is exactly one byte with no transformation.
// (int8 and bits(8) would also be byte-exact on the wire, but their C++
// storage or wire casts differ; they can join when a profile convicts them.)
func isFixedByteArray(f *Field) bool {
	return f.Array == ArrayFixed && f.Type.Kind == TInt &&
		f.Type.Width == 8 && !f.Type.Signed && !f.HasIntRange
}

// afterField advances the position across one field's wire.
func afterField(f *Field, pos wirePos) wirePos {
	isStruct := false
	if f.Type.Kind == TNamed {
		_, isStruct = f.Type.Ref.(*Struct)
	}
	switch f.Array {
	case ArrayFixed:
		switch {
		case f.Type.Kind == TString || f.Type.Kind == TBytes:
			// each element ends aligned (§4.7); bounds are ≥ 1 (§4.4)
			return wireAligned
		case isStruct:
			// exit-from-unknown is entry-independent: if it is known, every
			// element exits there (bounds are ≥ 1); otherwise give up
			if e := walkAligned(f.Type.Ref.(*Struct).Items, wireUnknown, nil); e.known {
				return e
			}
			return wireUnknown
		}
		return pos.add(f.ArrayBound * maxBitsScalar(f))
	case ArrayCounted:
		// count prefix, then count elements: the exit is static only when
		// each element is a whole number of bytes
		if f.Type.Kind == TString || f.Type.Kind == TBytes || isStruct || maxBitsScalar(f)%8 != 0 {
			return wireUnknown
		}
		return pos.add(BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)))
	}
	switch {
	case f.Type.Kind == TString || f.Type.Kind == TBytes:
		// length prefix, align, then whole bytes — ends aligned (§4.7)
		return wireAligned
	case isStruct:
		// nested structs are analyzed on their own; here only the exit
		// position matters
		return walkAligned(f.Type.Ref.(*Struct).Items, pos, nil)
	}
	// every remaining scalar kind is fixed-width, and maxBitsScalar is exact
	// for it (TInt/TBits/TBool/TFloat32±compressed/TFloat64/Enum/Flags)
	return pos.add(maxBitsScalar(f))
}

// View selects an object view's wire (SPEC §4.8).
type View int

const (
	ViewDeep View = iota
	ViewShallow
)

// MaxBitsView is the worst-case wire bits of an object view's field list.
func MaxBitsView(fields []*Field, v View) int64 {
	var total int64
	for _, f := range fields {
		switch {
		case v == ViewShallow && f.HasQuantize && f.FixedShallow:
			// narrowed fixed composite (SPEC §4.8 rule 2b): each component's
			// wire range is its own whole-unit bounds scaled by QuantScale
			st := f.Type.Ref.(*Struct)
			for _, cf := range st.Fields {
				lo, hi := FixedShallowBounds(f, cf)
				total += BitsRequired(lo, hi)
			}
		case v == ViewShallow && f.HasQuantize:
			st := f.Type.Ref.(*Struct)
			per := BitsRequired(big.NewInt(-f.QuantBound), big.NewInt(f.QuantBound))
			total += per * int64(len(st.Fields))
		case v == ViewShallow && f.HasFloatRange:
			total += BitsRequired(big.NewInt(0), big.NewInt(f.Steps))
		case v == ViewDeep && f.HasFloatRange && f.Interpolate:
			// bare storage encoding on the deep wire (SPEC §4.8)
			if f.Type.Kind == TFloat64 {
				total += 64
			} else {
				total += 32
			}
		default:
			total += MaxBitsField(f)
		}
	}
	return total
}

// FileDeps collects, per file, the other files its declarations reference —
// named types by value, and constants named in emitted expressions. The C++
// backend derives its #include graph from this; owner selection for the
// unit-level dispatch surfaces uses its topo order in every target, so the
// surface lands in the same file across languages.
func FileDeps(u *Unit) map[string]map[string]bool {
	deps := map[string]map[string]bool{}
	for _, f := range u.Files {
		set := map[string]bool{}
		note := func(name string) {
			if base, ok := u.DeclFile[name]; ok && base != f.Base {
				set[base] = true
			}
		}
		var noteExpr func(e ast.Expr)
		noteExpr = func(e ast.Expr) {
			switch e := e.(type) {
			case *ast.IdentExpr:
				note(e.Name)
			case *ast.MaxExpr:
				// folds to a literal — no reference needed
			case *ast.UnaryExpr:
				noteExpr(e.X)
			case *ast.BinaryExpr:
				noteExpr(e.X)
				noteExpr(e.Y)
			case *ast.ParenExpr:
				noteExpr(e.X)
			}
		}
		noteFields := func(fields []*Field) {
			for _, fld := range fields {
				if fld.Type.Kind == TNamed {
					note(fld.Type.Name)
				}
				// EVERY expression a backend can render symbolically is a
				// dependency edge. IntMinExpr/IntMaxExpr were missing here
				// while every backend rendered them symbolically, so a
				// range bound naming a constant in another file was an
				// UNTRACKED edge: the topo order this graph feeds then chose
				// a dispatch owner that could not include cleanly (mutual
				// #include in C++, missing `use crate::*` in Rust), and the
				// include-cycle guard — reading the same wrong graph — saw
				// nothing. Adding a field with a symbolically-rendered
				// expression means adding it to this list.
				for _, e := range []ast.Expr{
					fld.ArrayExpr,
					fld.Type.SizeExpr,
					fld.DefExpr,
					fld.IntMinExpr,
					fld.IntMaxExpr,
					fld.QuantScaleExpr,
					fld.QuantMaxExpr,
				} {
					if e != nil {
						noteExpr(e)
					}
				}
			}
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *Const:
				if d.Expr != nil {
					noteExpr(d.Expr)
				}
			case *Struct:
				noteFields(d.Fields)
			case *Object:
				noteFields(d.Fields)
			}
		}
		deps[f.Base] = set
	}
	return deps
}

// MessageOwner and ObjectOwner name the file that carries a unit-level
// dispatch surface (the MessageType/ObjectType enums, the tag pairs, the
// dispatch functions): the LAST file in the unit's dependency topo order
// containing that kind of declaration. Emitting the surface once fixes the
// duplicate-symbol break when messages or objects span files (legal — the
// aspect layout is never compiler-enforced, SPEC §2); choosing the
// topologically last file means the C++ owner can include every other
// carrying file without ever creating an include cycle.
func MessageOwner(u *Unit) string {
	return dispatchOwner(u, func(f *File) bool {
		for _, d := range f.Decls {
			if st, ok := d.(*Struct); ok && st.IsMessage {
				return true
			}
		}
		return false
	})
}

func ObjectOwner(u *Unit) string {
	return dispatchOwner(u, func(f *File) bool {
		for _, d := range f.Decls {
			if _, ok := d.(*Object); ok {
				return true
			}
		}
		return false
	})
}

func dispatchOwner(u *Unit, has func(*File) bool) string {
	deps := FileDeps(u)
	// Kahn's algorithm over sorted bases — the same deterministic order the
	// C++ emitter's includes resolve in
	bases := make([]string, 0, len(u.Files))
	byBase := map[string]*File{}
	for _, f := range u.Files {
		bases = append(bases, f.Base)
		byBase[f.Base] = f
	}
	sort.Strings(bases)
	indeg := map[string]int{}
	for _, b := range bases {
		indeg[b] = len(deps[b])
	}
	done := map[string]bool{}
	owner := ""
	for range bases {
		pick := ""
		for _, b := range bases {
			if !done[b] && indeg[b] == 0 {
				pick = b
				break
			}
		}
		if pick == "" {
			// a file cycle — the C++ backend refuses it separately; fall back
			// to any remaining base so owner selection still terminates
			for _, b := range bases {
				if !done[b] {
					pick = b
					break
				}
			}
		}
		done[pick] = true
		if has(byBase[pick]) {
			owner = pick
		}
		for _, b := range bases {
			if !done[b] && deps[b][pick] {
				indeg[b]--
			}
		}
	}
	return owner
}
