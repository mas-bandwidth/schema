package tabletext_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// Integer range bounds are exact schema integers. Rounding either side through
// float64 would erase the refusals near 2^53 and the signed/unsigned 64-bit limits.
func TestIntegerRangeBoundsStayExact(t *testing.T) {
	const src = `package probe

table Root {
    upper uint64 | max = 18446744073709551610
    lower uint64 = 9007199254740993 | min = 9007199254740993
    negative int64 = -9007199254740993 | max = -9007199254740993
    floor int64 | min = -9223372036854775807
    packed bits(63)
}
`
	f, errs := parser.Parse("Probe.schema", []byte(src))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f}})
	if len(cerrs) > 0 {
		t.Fatal(cerrs)
	}
	m := tabletext.NewModel(u)
	for _, tc := range []struct {
		field, input string
		want         uint64
		over         int // nonzero: the input is past the bound and refuses
	}{
		{"upper", "18446744073709551609", 18446744073709551609, 0},
		{"upper", "18446744073709551610", 18446744073709551610, 0},
		{"upper", "18446744073709551611", 18446744073709551610, 1},
		{"upper", "18446744073709551615", 18446744073709551610, 1},
		{"lower", "9007199254740992", 9007199254740993, 1},
		{"lower", "9007199254740993", 9007199254740993, 0},
		{"lower", "9007199254740994", 9007199254740994, 0},
		{"negative", "-9007199254740992", uint64(18437736874454810623), 1},
		{"negative", "-9007199254740993", uint64(18437736874454810623), 0},
		{"negative", "-9007199254740994", uint64(18437736874454810622), 0},
		{"floor", "-9223372036854775808", uint64(9223372036854775809), 1},
		{"floor", "-9223372036854775807", uint64(9223372036854775809), 0},
		{"packed", "9223372036854775807", 9223372036854775807, 0},
		{"packed", "9223372036854775808", 9223372036854775807, 1},
	} {
		t.Run(tc.field+"/"+tc.input, func(t *testing.T) {
			value := m.New(u.Tables["Root"])
			var rep tabletext.Report
			text := fmt.Sprintf(`{"%s":%s}`, tc.field, tc.input)
			read := m.Read(value, []byte(text), &rep)
			if tc.over > 0 {
				if read || !rep.Refused || !strings.HasPrefix(rep.Reason, "out_of_range") {
					t.Fatalf("%s: want an out_of_range refusal, got %+v", text, rep)
				}
				return
			}
			got, ok := value.FieldByKey(tc.field)
			if !read || !ok || got.Cell.U != tc.want || !rep.Silent() {
				t.Fatalf("%s: got value=%d report=%+v, want value=%d", text, got.Cell.U, rep, tc.want)
			}
		})
	}
}
