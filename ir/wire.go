// Wire-width computation (SPEC §6.1 item 4): target-independent, so every
// backend derives the same MaxBits from one implementation.
package ir

import (
	"math"
	"math/big"
	"math/bits"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
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
	return alignUp(alignUp(bits, 8)/8, 8)
}

// CompressedFloatParams replicates serialize_compressed_float's parameter
// derivation exactly (float32 arithmetic, the clamp, the ceil): the step count
// the writer quantizes onto, and the wire width that carries it. One
// implementation, so the widths the backends advertise and the bytes the data
// compiler packs cannot drift apart.
func CompressedFloatParams(fmin, fmax, res float64) (maxIntegerValue uint64, wireBits int64) {
	values := compressedFloatValues(fmin, fmax, res)
	if !(values >= 1.0) { // the runtime's own form — it also catches NaN
		values = 1.0
	}
	if values > 4294967040.0 {
		values = 4294967040.0
	}
	maxIntegerValue = uint64(math.Ceil(float64(values)))
	return maxIntegerValue, int64(bits.Len64(maxIntegerValue))
}

// ValidCompressedFloatParams rejects a triple whose runtime derivation loses
// its range to overflow or to the upper clamp. A fractional count below one
// legitimately uses one step. The same float32 operations feed the codec.
func ValidCompressedFloatParams(fmin, fmax, res float64) bool {
	values := compressedFloatValues(fmin, fmax, res)
	return values > 0 && values <= 4294967040.0
}

func compressedFloatValues(fmin, fmax, res float64) float32 {
	delta := float32(fmax) - float32(fmin)
	return delta / float32(res)
}

// CompressedFloatBits is the wire width alone.
func CompressedFloatBits(fmin, fmax, res float64) int64 {
	_, wireBits := CompressedFloatParams(fmin, fmax, res)
	return wireBits
}

// PointerWireBits is what a pointer field costs on the wire: a `u32` index
// into the flat node table under kind 17 (docs/SPEC-TABLES.md §3.1). It is the
// width of the REFERENCE; the referent is a node of its own, counted once
// wherever it is reached from.
const PointerWireBits = 32

// MaxBitsField is one field's worst-case wire bits, alignment points counted
// at the worst 7.
func MaxBitsField(f *Field) int64 {
	elem := maxBitsScalar(f)
	switch f.Array {
	case ArrayFixed:
		return mulSize(f.ArrayBound, elem)
	case ArrayCounted:
		return addSize(BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)), mulSize(f.ArrayBound, elem))
	default:
		return elem
	}
}

func maxBitsScalar(f *Field) int64 {
	if f.Type.Pointer {
		// A POINTER IS ITS REFERENCE, NEVER ITS REFERENT
		// (docs/SPEC-TABLES.md §3.1): a `*T` and a `*bytes`/`*string` ride as
		// a u32 node index into the flat node table, and no pointer edge is a
		// nesting level, so a width computation stops here exactly as the
		// numbering walk and the storage layout do. Descending instead read a
		// legal `table Node { next *Node }` as an infinite by-value nesting
		// and overflowed the stack.
		return PointerWireBits
	}
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
		// (SPEC §4.6; bitlen(B−A) + F generalizes wrong
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
		return addSize(addSize(BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), 7), mulSize(f.Type.Size, 8))
	case TWString:
		// length prefix + one 32-BIT GROUP per code unit, and NO PADDING TERM:
		// the wide path aligns nowhere (SPEC §4.12)
		return addSize(BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), mulSize(f.Type.Size, 32))
	case TNamed:
		switch ref := f.Type.Ref.(type) {
		case *Enum:
			return BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
		case *Flags:
			return int64(ref.WireBits)
		case *Struct:
			return MaxBitsStruct(ref)
		case *Union:
			return MaxBitsUnion(ref)
		}
	}
	return 0
}

// MaxBitsUnion is the longest wire path through a union: the tag plus the
// largest payload (SPEC §4.8 — None costs the tag bits only).
func MaxBitsUnion(u *Union) int64 {
	bits := BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	var largest int64
	for _, v := range u.Variants {
		// AN ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6): a by-value
		// declared type is its struct's path, a PAYLOAD-FREE arm costs the tag
		// alone (SPEC §4.8), and any other arm is its field's own path.
		var b int64
		switch {
		case v.Void():
		case v.Body():
			b = MaxBitsStruct(v.Ref)
		default:
			b = MaxBitsField(v.F)
		}
		if b > largest {
			largest = b
		}
	}
	return addSize(bits, largest)
}

// MaxBitsStruct is the longest wire path through a struct: branches take the
// larger side. The walk descends BY-VALUE EDGES ONLY, so it terminates on
// every legal declaration: a by-value composition cycle is a compile error,
// and a pointer edge is a reference whose width is [PointerWireBits]
// (docs/SPEC-TABLES.md §3.1), so a pointer-recursive table is finite here.
func MaxBitsStruct(st *Struct) int64 {
	var walk func(items []Item) int64
	walk = func(items []Item) int64 {
		var total int64
		for _, item := range items {
			switch item := item.(type) {
			case *FieldItem:
				total = addSize(total, MaxBitsField(item.F))
			case *Branch:
				then, els := walk(item.Then), walk(item.Else)
				if then > els {
					total = addSize(total, then)
				} else {
					total = addSize(total, els)
				}
			case *ConstItem:
				total = addSize(total, item.Bits)
			case *ReservedItem:
				total = addSize(total, item.Bits)
			case *AlignItem:
				total = addSize(total, 7)
			}
		}
		return total
	}
	return walk(st.Items)
}

// FixedWireBits reports whether EVERY legal encoding of st occupies exactly
// the same number of wire bits, and returns that width. It is the condition
// under which [MaxBitsStruct] is EXACT rather than an upper bound.
//
// Not fixed: a branch (the two sides need not agree), an align (0..7 bits of
// pad), a counted array (the element count varies), a string/bytes/wstring (a
// declared length is a bound, not a width), a union (arms differ and
// [MaxBitsUnion] takes the largest), and a pointer (the reference is fixed but
// nothing here is entitled to speak for the referent). Everything else has one
// width and only one: ranged and bare ints, bits, bool, fixed-point, float32
// compressed or bare, float64, enums, flags, fixed arrays of those, and
// by-value nestings of structs that are themselves fixed.
//
// The walk descends BY-VALUE EDGES ONLY and stops at pointers, exactly as
// [MaxBitsStruct] does, so it terminates on every legal declaration.
//
// This is what lets a reader hoist its per-field past-end tests: a reader that
// has proved it holds MaxBitsStruct(st) bits cannot fail on any field of st,
// because every field is always read and the total is exact.
func FixedWireBits(st *Struct) (int64, bool) {
	if !fixedItems(st.Items) {
		return 0, false
	}
	return MaxBitsStruct(st), true
}

func fixedItems(items []Item) bool {
	for _, item := range items {
		switch item := item.(type) {
		case *FieldItem:
			if !fixedField(item.F) {
				return false
			}
		case *ConstItem, *ReservedItem:
			// a literal bit count, the same on every path
		default:
			// *Branch, *AlignItem, and any item kind this analysis has not
			// been taught. Unknown is NOT fixed: a wrong "not fixed" costs a
			// hoisted guard, a wrong "fixed" would change what a read refuses.
			_ = item
			return false
		}
	}
	return true
}

func fixedField(f *Field) bool {
	if f.Type.Pointer || f.Array == ArrayCounted {
		return false
	}
	switch f.Type.Kind {
	case TInt, TBits, TBool, TFloat32, TFloat64, TFixed:
		return true
	case TString, TBytes, TWString:
		return false
	case TNamed:
		switch ref := f.Type.Ref.(type) {
		case *Enum:
			return true
		case *Flags:
			return true
		case *Struct:
			_, ok := FixedWireBits(ref)
			return ok
		case *Union:
			return false
		default:
			_ = ref
			return false
		}
	}
	return false
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
// Counted arrays ([..N]uint8) are tracked for position but not marked —
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
	// a POINTER is never a nesting: it rides as its reference's fixed width
	// (docs/SPEC-TABLES.md §3.1), so the position advances by that and the
	// walk does not descend into the referent
	isStruct := false
	if f.Type.Kind == TNamed && !f.Type.Pointer {
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
		return pos.add(mulSize(f.ArrayBound, maxBitsScalar(f)))
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
	case f.Type.Kind == TWString:
		// length prefix, then one 32-bit group per code unit and no align
		// (§4.12). Every group is a whole number of BYTES, so the groups move
		// the position by a multiple of 8 whatever the length is: the exit is
		// the entry plus the length prefix, and it stays KNOWN when the entry
		// is. That is the sense in which wide text introduces no alignment
		// point — it neither creates one nor destroys the one it entered on.
		return pos.add(BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)))
	case isStruct:
		// nested structs are analyzed on their own; here only the exit
		// position matters
		return walkAligned(f.Type.Ref.(*Struct).Items, pos, nil)
	}
	// every remaining scalar kind is fixed-width, and maxBitsScalar is exact
	// for it (TInt/TBits/TBool/TFloat32±compressed/TFloat64/Enum/Flags)
	return pos.add(maxBitsScalar(f))
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
			case *Union:
				// payloads are held by value — a cross-file payload is an
				// include/use edge exactly like a named field type
				for _, v := range d.Variants {
					note(v.Type)
				}
			}
		}
		deps[f.Base] = set
	}
	return deps
}

// ProtocolIdHome picks the file whose output carries the unit-level protocol
// id constant (and, per target, its companions): the constants aspect file if
// the unit has one, else the first file. One rule for every backend, so the
// id lives in the same schema file's output across all targets.
func ProtocolIdHome(u *Unit) string {
	for _, f := range u.Files {
		if f.Base == "Constants" {
			return f.Base
		}
	}
	if len(u.Files) > 0 {
		return u.Files[0].Base
	}
	return ""
}
