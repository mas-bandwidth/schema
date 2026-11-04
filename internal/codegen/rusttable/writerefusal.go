package rusttable

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITE REFUSAL (docs/SPEC-TABLES.md §3): the variable-form save refuses a
// value its field cannot hold rather than writing it. In Rust the refusal is a
// debug_assert!, the idiom serialize.rs and the packet writer use: it panics
// with the field's name when debug assertions are on and compiles out when
// they are off. A required field's range (the numbering walk), a wstring(N)
// over N, a counted array over its Max, an arm's bytes(N) over N and a NaN or
// an infinity in a float refuse in every build instead;
// an enum ordinal or a union tag outside its set cannot be built, because both
// are native Rust enums.

// writeIntBounds is the range one integer value must sit in to be written: a
// ranged integer's declared min/max (on the raw scale for fixed), narrowed by
// a bits(N) field's width. ok is false when no value of the storage type can
// fall outside it.
func writeIntBounds(f *ir.Field) (lo, hi *big.Int, t string, ok bool) {
	switch f.Type.Kind {
	case ir.TInt, ir.TFixed, ir.TBits:
	default:
		return nil, nil, "", false
	}
	t = scalarRust(f)
	width, err := strconv.Atoi(t[1:])
	if err != nil {
		return nil, nil, "", false
	}
	storeLo := new(big.Int)
	storeHi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width)), big.NewInt(1))
	if t[0] == 'i' {
		storeLo.Neg(new(big.Int).Lsh(big.NewInt(1), uint(width-1)))
		storeHi.Sub(new(big.Int).Lsh(big.NewInt(1), uint(width-1)), big.NewInt(1))
	}
	lo, hi = new(big.Int).Set(storeLo), new(big.Int).Set(storeHi)
	if rlo, rhi, ranged := ir.TableRawRange(f); ranged {
		if rlo.Cmp(lo) > 0 {
			lo.Set(rlo)
		}
		if rhi.Cmp(hi) < 0 {
			hi.Set(rhi)
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < width {
		if max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1)); max.Cmp(hi) < 0 {
			hi = max
		}
	}
	if lo.Cmp(storeLo) == 0 && hi.Cmp(storeHi) == 0 {
		return nil, nil, "", false
	}
	return lo, hi, t, true
}

// intWriteAssert is the debug_assert! that refuses an integer outside its
// declared range or its bits(N) width, followed by a space; "" when the field
// is required (refused in every build by the numbering walk) or when no stored
// value can fall outside.
func intWriteAssert(f *ir.Field, x, owner string) string {
	if f.Required {
		return ""
	}
	lo, hi, t, ok := writeIntBounds(f)
	if !ok {
		return ""
	}
	// a dereferenced element `(*e)` is already the reference contains takes
	// (clippy::borrow_deref_ref refuses `&(*e)`)
	ref := "&" + x
	if strings.HasPrefix(x, "(*") && strings.HasSuffix(x, ")") {
		ref = x[2 : len(x)-1]
	}
	return fmt.Sprintf("debug_assert!((%s%s..=%s%s).contains(%s), %q); ", lo, t, hi, t, ref,
		fmt.Sprintf("%s: value outside %s..=%s, refused on write (docs/SPEC-TABLES.md section 3)", owner, lo, hi))
}

// lenWriteAssert is the debug_assert! that refuses a bounded string(N) or
// bytes(N) longer than N bytes; "" for an unbounded text.
func lenWriteAssert(f *ir.Field, x, owner, ind string) string {
	var bound int64
	switch f.Type.Kind {
	case ir.TString:
		if f.Type.TextUnbounded() {
			return ""
		}
		bound = f.Type.Size
	case ir.TBytes:
		bound = f.Type.Size
	default:
		return ""
	}
	return fmt.Sprintf("%sdebug_assert!(%s.len() <= %d, %q);\n", ind, x, bound,
		fmt.Sprintf("%s: longer than its bound of %d, refused on write (docs/SPEC-TABLES.md section 3)", owner, bound))
}

// countWriteRefuse refuses a bounded array holding more than its Max
// elements, in every build: the writer bounds the count it writes with that
// compare anyway, so the refusal costs nothing the clamp did not (docs/SPEC.md
// §5.1). The refusal is the save's named one (table_bound_refuse).
func countWriteRefuse(f *ir.Field, x, owner, ind string) string {
	if f.Array != ir.ArrayCounted {
		return ""
	}
	return fmt.Sprintf("%sif %s.len() > %d { table_bound_refuse(ids, %q); }\n", ind, x, f.ArrayBound, owner)
}

// bytesWriteRefuse refuses a bytes(N) arm payload longer than N bytes, in
// every build, for the same reason as countWriteRefuse: the arm's writer
// bounds the length it writes with that compare anyway.
func bytesWriteRefuse(f *ir.Field, x, owner, ind string) string {
	return fmt.Sprintf("%sif %s.len() > %d { table_bound_refuse(ids, %q); }\n", ind, x, f.Type.Size, owner)
}
