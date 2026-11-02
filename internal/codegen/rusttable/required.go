package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"math/big"
	"strconv"
	"strings"
)

func hasRequired(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.Required {
			return true
		}
	}
	return false
}

// One body's final guard values decide which unseen names are required.
// The successful path allocates nothing, even for bodies larger than 512 fields.
func emitRequiredNames(b *strings.Builder, st *ir.Struct) {
	if !hasRequired(st) {
		return
	}
	fmt.Fprintln(b, "#[allow(unused_variables)]")
	fmt.Fprintf(b, "pub fn %s(v: &%s, seen: &[bool]) -> String {\n", fn(st.Name, "required_missing"), st.Name)
	fmt.Fprintln(b, "    let mut missing = String::new();")
	conditions := guardConds(st, "v")
	for i, f := range st.Fields {
		if !f.Required {
			continue
		}
		cond := fmt.Sprintf("!seen[%d]", i)
		if guard := conditions[f.Name]; guard != "" {
			cond += " && (" + guard + ")"
		}
		fmt.Fprintf(b, "    if %s { if !missing.is_empty() { missing.push_str(\", \"); } missing.push_str(%q); }\n", cond, f.Name)
	}
	fmt.Fprint(b, "    missing\n}\n\n")
}

func emitRequiredJSONEnd(b *strings.Builder, st *ir.Struct, ind string) {
	if !hasRequired(st) {
		return
	}
	fmt.Fprintf(b, "%slet missing = %s(v, &seen);\n", ind, fn(st.Name, "required_missing"))
	fmt.Fprintf(b, "%sif !missing.is_empty() { jr.missing_required += 1; jr.missing_required_fields = missing; jr.reason = TableReason::MissingRequired; }\n", ind)
}

// A wire occurrence counts only if its field and collection header are
// compatible. A skipped repeat leaves any previous compatible occurrence seen.
func requiredKindSkip(f *ir.Field) string {
	if f.Required {
		return "field_skipped = true; " + kindSkip
	}
	return kindSkip
}

func requiredCollectionSkip(f *ir.Field) string {
	if f.Required {
		return "field_skipped = true; "
	}
	return ""
}

func emitRequiredWireSeen(b *strings.Builder, f *ir.Field, ordinal int, ind string) {
	if f.Required {
		fmt.Fprintf(b, "%sif !field_skipped { seen[%d] = true; }\n", ind, ordinal)
	}
}

// Validate only active authored Required fields. The caller applies guards and
// uses the same walk for wire numbering and JSON traversal; cooked storage
// retains inactive bytes without treating them as active values.
func emitRequiredRangeCheck(b *strings.Builder, f *ir.Field, x, ind, refuse string) {
	if !f.Required || f.Type.Pointer || f.IsMap() {
		return
	}
	invalid := func(value string) string {
		if lo, hi, ok := ir.TableRawRange(f); ok {
			t := scalarRust(f)
			// Omit comparisons that the Rust storage type makes impossible.
			width, _ := strconv.Atoi(t[1:])
			low := new(big.Int)
			high := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width)), big.NewInt(1))
			if t[0] == 'i' {
				low.Neg(new(big.Int).Lsh(big.NewInt(1), uint(width-1)))
				high.Sub(new(big.Int).Lsh(big.NewInt(1), uint(width-1)), big.NewInt(1))
			}
			var terms []string
			if lo.Cmp(low) > 0 {
				terms = append(terms, fmt.Sprintf("%s < %s%s", value, lo, t))
			}
			if hi.Cmp(high) < 0 {
				terms = append(terms, fmt.Sprintf("%s > %s%s", value, hi, t))
			}
			return strings.Join(terms, " || ")
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < 64 && f.Type.Width != 8 && f.Type.Width != 16 && f.Type.Width != 32 {
			return fmt.Sprintf("%s > %d%s", value, uint64(1)<<uint(f.Type.Width)-1, scalarRust(f))
		}
		if f.HasFloatRange {
			return fmt.Sprintf("(%s as f64) < %s || (%s as f64) > %s", value, fixedFloatLit(f.FMin), value, fixedFloatLit(f.FMax))
		}
		return ""
	}
	if invalid(x) == "" {
		return
	}
	if f.Array != ir.ArrayNone || f.KeyEnum != "" {
		if f.Array == ir.ArrayFixed || f.KeyEnum != "" {
			n := f.ArrayBound
			if f.KeyEnumRef != nil {
				n = int64(len(f.KeyEnumRef.Variants))
			}
			fmt.Fprintf(b, "%sfor i in 0..%d { let e=%s.get(i).copied().unwrap_or(%s); if %s { %s } }\n", ind, n, x, scalarDefault(f), invalid("e"), refuse)
		} else {
			fmt.Fprintf(b, "%sfor &e in %s.iter() { if %s { %s } }\n", ind, x, invalid("e"), refuse)
		}
	} else {
		fmt.Fprintf(b, "%sif %s { %s }\n", ind, invalid(x), refuse)
	}
}
