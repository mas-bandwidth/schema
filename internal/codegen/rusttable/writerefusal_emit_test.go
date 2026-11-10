package rusttable

import (
	"strings"
	"testing"
)

// TestRustWriteRefusalEmitted pins the write contract's refusals in the
// variable-form save (docs/SPEC.md §5.1): a ranged integer off its min/max
// (row 1), a bits(N) value wider than its width (row 2), a counted array's
// count past Max (row 3, every build) and a bounded string(N)'s length past N
// or a zero byte (rows 4 and 8). Each is named by the field it guards, so a
// regressed check fails by name.
func TestRustWriteRefusalEmitted(t *testing.T) {
	t.Parallel()
	out := generate(t, `package probe

table Config
{
    amount  int32 = 0 | min = 0, max = 10
    channel bits(6)
    items   [..8]int32
    label   string(16)
}
`)
	var src strings.Builder
	for _, data := range out {
		src.Write(data)
	}
	whole := src.String()
	for _, want := range []string{
		`debug_assert!(v.amount >= 0 && v.amount <= 10, "amount out of range [0, 10]");`,
		`debug_assert!(v.channel <= 63, "channel exceeds bits(6) width");`,
		`if v.items.len() > 8 { ids.refused = Some(String::from("invalid_value: count past max")); }`,
		`debug_assert!(v.label.len() <= 16, "label length past 16");`,
		`debug_assert!(!v.label.as_bytes().contains(&0), "label holds a zero byte");`,
	} {
		if !strings.Contains(whole, want) {
			t.Errorf("the variable-form save does not emit %q", want)
		}
	}
}
