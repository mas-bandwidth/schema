// A fixed default is declared in WHOLE UNITS and storage initializes to the
// raw scaled integer (SPEC §4.6). An integer default wider than 2^53 must
// survive to DefInt exactly — narrowing to float64 first and widening again
// loses the low bits, and the range check compares the rounded value too.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedDefaultKeepsExactWideInteger(t *testing.T) {
	t.Parallel()
	u := buildUnit(t, `package t
type T
{
    a ufixed(64, 0) = 9007199254740993 | min = 0, max = 9223372036854775807
    b fixed(64, 0)  = -9007199254740993 | min = -9223372036854775807, max = 9223372036854775807
    c ufixed(64, 0) = 9223372036854775807 | min = 0, max = 9223372036854775807
}
`)
	st := u.Structs["T"]
	if st == nil {
		t.Fatal("type T did not resolve")
	}
	fields := map[string]*ir.Field{}
	for _, f := range st.Fields {
		fields[f.Name] = f
	}
	for _, tc := range []struct{ name, want string }{
		{"a", "9007199254740993"},
		{"b", "-9007199254740993"},
		{"c", "9223372036854775807"},
	} {
		f := fields[tc.name]
		if f == nil || !f.HasDefault || f.DefInt == nil {
			t.Fatalf("field %s did not resolve with a default: %+v", tc.name, f)
		}
		if f.DefInt.String() != tc.want {
			t.Errorf("field %s: DefInt = %s, want the exact integer %s", tc.name, f.DefInt, tc.want)
		}
	}
	if got := refuse(t, "package t\ntype T { x ufixed(16, 16) = 0.1 | min = 0, max = 5 }\n"); !strings.Contains(got, "not exactly representable") {
		t.Errorf("an inexact fractional default must still be refused, got: %s", got)
	}
}
