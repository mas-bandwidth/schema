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

// CompressedFloatBits replicates serialize_compressed_float's width
// derivation exactly (float32 arithmetic, the clamp, the ceil).
func CompressedFloatBits(fmin, fmax, res float64) int64 {
	delta := float32(fmax) - float32(fmin)
	values := delta / float32(res)
	if !(values >= 1.0) { // the runtime's own form — it also catches NaN
		values = 1.0
	}
	if values > 4294967040.0 {
		values = 4294967040.0
	}
	maxIntegerValue := uint64(math.Ceil(float64(values)))
	return int64(bits.Len64(maxIntegerValue))
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
		return int64(f.Type.Width)
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
				for _, e := range []ast.Expr{fld.ArrayExpr, fld.Type.SizeExpr, fld.DefExpr, fld.QuantScaleExpr, fld.QuantMaxExpr} {
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
