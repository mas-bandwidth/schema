package darttable

// THE PREFILL, as a constant of the type (docs/SPEC-TABLES.md §3.4).
//
// "A READ IS A PREFILL AND A LOOP, AND NOTHING ELSE." The prefill is what
// answers ABSENT FIELD: a field this record does not carry has NO PLAN ENTRY
// at all, so it keeps the default the prefill put there and the loop never
// learns it existed.
//
// In C++ the prefill is `<T>Reset( value )`, a generated walk over a struct.
// Here the reader's own storage is the canonical body image, so the prefill is
// what the WRITE side's template already is: A CONSTANT RUN OF BYTES, settled
// by the compiler. The load copies that image into EXACTLY THE RANGES THE
// PLAN DOES NOT LAND — identity's list is empty, so that read writes no byte
// twice. Every declared default is folded into the constant at generation
// time, so the read path neither walks nor branches to place one.

import (
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedPrefillBytes is the body image a read starts from: zero everywhere SPEC
// §5's zero initialization holds, and the declared default laid in wherever
// one overrides it.
func fixedPrefillBytes(st *ir.Struct) []byte {
	out := make([]byte, fixedTypeBytes(st))
	fixedPrefillInto(out, 0, st)
	return out
}

func fixedPrefillInto(out []byte, base int64, st *ir.Struct) {
	at := base
	for _, f := range st.Fields {
		fixedPrefillField(out, at, f)
		at += fixedFieldBytes(f)
	}
}

func fixedPrefillField(out []byte, at int64, f *ir.Field) {
	if f.Type.Optional {
		// an OPTIONAL field the writer did not send reads as ABSENT, with the
		// value at its declared defaults: the present flag stays zero
		at += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		// a slot this reader has but the writer never sent keeps its declared
		// default, exactly as an absent field does
		fixedPrefillElements(out, at, f, f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		fixedPrefillElements(out, at, f, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		// a counted array is born at its declared MINIMUM, which is where the
		// storage class's own constructor puts its count
		fixedPutInt(out, at, 4, big.NewInt(f.ArrayMin))
		fixedPrefillElements(out, at+fixedCountBytes, f, f.ArrayBound)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		// a string(N) or bytes(N) field's declared default is that default
		// (SPEC §4.2): the length and the bytes both ride in the prefill
		if f.HasDefault && len(f.DefBytes) > 0 {
			n := min(int64(len(f.DefBytes)), f.Type.Size)
			fixedPutInt(out, at, 4, big.NewInt(n))
			copy(out[at+fixedCountBytes:at+fixedCountBytes+n], f.DefBytes[:n])
		}
	case f.Type.Kind == ir.TWString:
		// no wstring default rides here: the IR carries a wstring default as
		// BYTES and this form counts CODE UNITS, so laying one down would be
		// this backend guessing an encoding. A wstring field prefills empty.
	default:
		fixedPrefillElement(out, at, f)
	}
}

func fixedPrefillElements(out []byte, at int64, f *ir.Field, count int64) {
	elem := fixedElementBytes(f)
	for i := range count {
		fixedPrefillElement(out, at+i*elem, f)
	}
}

func fixedPrefillElement(out []byte, at int64, f *ir.Field) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fixedPrefillInto(out, at, r)
			return
		case *ir.Union:
			// A UNION IS RESET ARM BY ARM, AT EACH ARM'S OWN OVERLAY STORAGE,
			// and only then the tag to None (§5.2's prefill, §5.9 #38). An arm
			// is not the member it looks like: a whole-value assignment over an
			// overlay resets one arm's worth of bytes and calls the union
			// reset, which leaves an APPENDED field inside another arm holding
			// whatever the image held last — and the image is reused record to
			// record. The arms run in DECLARED ORDER over the one overlay, so a
			// later arm's defaults stand where they reach; the tag is zero,
			// which is None.
			tag := fixedUnionTagBytes(r)
			for _, v := range r.Variants {
				if v.F == nil {
					continue // a payload-free arm has no bytes to reset
				}
				fixedPrefillElement(out, at+tag, v.F)
			}
			for i := range tag {
				out[at+i] = 0
			}
			return
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				if ord := fixedVariantOrdinal(r, f.DefVariant); ord > 0 {
					fixedPutInt(out, at, int64(r.StorageBits/8), big.NewInt(ord))
				}
			}
			return
		case *ir.Flags:
			if f.HasDefault && f.DefInt != nil {
				fixedPutInt(out, at, 8, f.DefInt)
			}
			return
		}
	}
	if !f.HasDefault {
		return
	}
	w := fixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		if f.DefBool {
			out[at] = 1
		}
	case ir.TFloat32:
		fixedPutInt(out, at, 4, new(big.Int).SetUint64(uint64(math.Float32bits(float32(f.DefFloat)))))
	case ir.TFloat64:
		fixedPutInt(out, at, 8, new(big.Int).SetUint64(math.Float64bits(f.DefFloat)))
	default:
		if f.DefInt != nil {
			fixedPutInt(out, at, w, f.DefInt)
		}
	}
}

// fixedVariantOrdinal is a variant's POSITION IN THE LAYOUT, from 1.
func fixedVariantOrdinal(e *ir.Enum, name string) int64 {
	for i, v := range e.Variants {
		if v == name {
			return int64(i + 1)
		}
	}
	return 0
}

// fixedPutInt lays an integer down little-endian at a declared storage width,
// two's complement for a negative one, which is the storage image this form
// rides at every width including the 128-bit pair (the low half, then the
// high).
func fixedPutInt(out []byte, at, width int64, v *big.Int) {
	if width <= 0 || v == nil {
		return
	}
	n := new(big.Int).Set(v)
	if n.Sign() < 0 {
		n.Add(n, new(big.Int).Lsh(big.NewInt(1), uint(width*8)))
	}
	mask := big.NewInt(0xff)
	tmp := new(big.Int)
	for i := range width {
		tmp.Rsh(n, uint(8*i))
		tmp.And(tmp, mask)
		out[at+i] = byte(tmp.Uint64())
	}
}
