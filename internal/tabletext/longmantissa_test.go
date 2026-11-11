package tabletext_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// longMantissaModel is one Root with a float64 and a uint32 field, built the
// same way longnumber_test.go builds its model.
func longMantissaModel(t *testing.T) *tabletext.Model {
	t.Helper()
	const src = "package probe\ntable Root\n{\n    f float64\n    u uint32\n}\n"
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

// docs/SPEC-TABLES.md §16.2 Numbers: a valid number is read by its value
// however many digits it spans, so a mantissa longer than a conversion's own
// working precision still lands the value its decimal spells. A float field
// reads an exponent token; an integer field does not, so the long-mantissa
// row is the float's alone.
func TestTextLongMantissaReadsByValue(t *testing.T) {
	t.Parallel()
	m := longMantissaModel(t)
	st := m.Lookup("Root")
	if st == nil {
		t.Fatal("no table Root")
	}
	// 1 followed by n zeros, times 10^-n, is exactly 1.
	one := func(n int) string {
		return "1" + strings.Repeat("0", n) + "e-" + strconv.Itoa(n)
	}
	for _, n := range []int{799, 800, 1000, 5000} {
		token := one(n)
		inst := m.New(st)
		var r tabletext.Report
		text := []byte(`{"f":` + token + `}`)
		if !m.Read(inst, text, &r) || !r.Silent() {
			t.Fatalf("%d zeros: the valid number was refused: %+v", n, r)
		}
		if got := field(t, inst, "f").Cell.F; got != 1.0 {
			t.Fatalf("%d zeros: float64 landed %v, want 1.0", n, got)
		}
	}
	// 123 followed by 900 zeros, times 10^-900, is exactly 123; the same
	// mantissa with 898 zeros and 10^-900 is exactly 1.23.
	for _, row := range []struct {
		zeros int
		want  float64
	}{
		{900, 123},
		{898, 1.23},
	} {
		token := "123" + strings.Repeat("0", row.zeros) + "e-900"
		inst := m.New(st)
		var r tabletext.Report
		if !m.Read(inst, []byte(`{"f":`+token+`}`), &r) || !r.Silent() {
			t.Fatalf("%d zeros after 123: the valid number was refused: %+v", row.zeros, r)
		}
		if got := field(t, inst, "f").Cell.F; got != row.want {
			t.Fatalf("%d zeros after 123: float64 landed %v, want %v", row.zeros, got, row.want)
		}
	}
}
