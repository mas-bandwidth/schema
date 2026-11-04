package darttable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITE CONTRACT OF THE VARIABLE FORM (docs/SPEC.md §5.1): a save refuses
// a value its schema cannot hold. The every-build refusals are StateErrors in
// the writer (a required ranged integer, a text list past its bounds, an
// unbounded text past its cap, a union tag that names no arm, a NaN or an
// infinity). Every other contract is a Dart assert, live under
// --enable-asserts (and in the JIT's debug runs) and gone from a release AOT
// build: a ranged integer inside its min/max (scalars and every element of an
// array of them), a bits(N) value inside N bits, a bounded or fixed array's
// count inside its bounds, a string(N), bytes(N) or wstring(N) inside N, an
// enum ordinal inside its set (0, None, through the last variant), and narrow
// or wide text with no unpaired surrogate. Each field's asserts run before a
// byte of it is written.

// writeCheckHolds is the Dart condition ONE value of field f (an element's
// field for an array) must meet to be written, over expr; "" when no debug
// contract binds it. A required field's range is the every-build refusal's
// (requiredRangeCheck).
func writeCheckHolds(f *ir.Field, expr string) string {
	if isWide(f) || f.Type.Pointer || f.Type.Blob() {
		return ""
	}
	switch f.Type.Kind {
	case ir.TInt, ir.TBits:
		if f.HasIntRange {
			if f.Required {
				return ""
			}
			lo, hi, _ := ir.TableRawRange(f)
			if f.Type.Kind == ir.TInt && !f.Type.Signed && f.Type.Width == 64 {
				value := "BigInt.from(" + expr + ").toUnsigned(64)"
				return fmt.Sprintf("%s >= %s && %s <= %s", value, bigLiteral(lo), value, bigLiteral(hi))
			}
			return fmt.Sprintf("%s >= %s && %s <= %s", expr, dartBoundLiteral(lo), expr, dartBoundLiteral(hi))
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
			return fmt.Sprintf("%s >= 0 && %s <= %d", expr, expr, (uint64(1)<<uint(f.Type.Width))-1)
		}
	case ir.TString:
		// a Dart String is UTF-16: an unpaired surrogate in it has no UTF-8
		// spelling, which is how ill-formed narrow text reaches this writer
		// (an unbounded text is refused in every build by tableTextValueValid)
		if f.Type.Size > 0 && !f.Type.TextUnbounded() {
			return fmt.Sprintf("tableTextWellFormed(%s) && utf8.encode(%s).length <= %d", expr, expr, f.Type.Size)
		}
	case ir.TWString:
		// wide text counts UTF-16 code units, and every surrogate is paired
		if f.Type.Size > 0 {
			return fmt.Sprintf("tableTextWellFormed(%s) && %s.length <= %d", expr, expr, f.Type.Size)
		}
		return fmt.Sprintf("tableTextWellFormed(%s)", expr)
	case ir.TBytes:
		// bytes ride one a code unit
		if f.Type.Size > 0 {
			return fmt.Sprintf("%s.length <= %d", expr, f.Type.Size)
		}
	case ir.TNamed:
		if e := enumRef(f); e != nil {
			return fmt.Sprintf("%s >= 0 && %s <= %d", expr, expr, len(e.Variants))
		}
	}
	return ""
}

// writeCheckAsserts is the field's write contract as Dart asserts over expr,
// each line at ind; "" when none binds. present is the optional field's
// presence test ("" for every other field).
func writeCheckAsserts(f *ir.Field, expr, present, ind string) string {
	var holds []string
	if f.Array != ir.ArrayNone && !f.IsTextList() && !f.IsMap() {
		// a TEXT LIST's count and lengths are refused in every build
		switch {
		case f.Array == ir.ArrayCounted && f.ArrayMin > 0:
			holds = append(holds, fmt.Sprintf("%s.length >= %d && %s.length <= %d", expr, f.ArrayMin, expr, f.ArrayBound))
		case f.ArrayBound > 0:
			holds = append(holds, fmt.Sprintf("%s.length <= %d", expr, f.ArrayBound))
		}
		if unionRef(f) == nil && tableRef(f) == nil {
			if cond := writeCheckHolds(singleOf(f), "e"); cond != "" {
				holds = append(holds, fmt.Sprintf("%s.every((e) => %s)", expr, cond))
			}
		}
	} else if f.Array == ir.ArrayNone && !f.IsMap() {
		if cond := writeCheckHolds(f, expr); cond != "" {
			holds = append(holds, cond)
		}
	}
	var b strings.Builder
	for _, cond := range holds {
		if present != "" {
			cond = "!" + present + " || (" + cond + ")"
		}
		fmt.Fprintf(&b, "%sassert(%s, 'write contract: %s');\n", ind, cond, f.Name)
	}
	return b.String()
}
