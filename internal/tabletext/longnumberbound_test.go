package tabletext_test

import (
	"strings"
	"testing"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// docs/SPEC-TABLES.md §16.2 Numbers: the engine's token fold and the generated
// Go walk's must read one token to the same value. The written exponent is
// kept whole so a long mantissa's zeros can cancel it; only the folded scale
// is bounded, where every float format has already overflowed or underflowed.
func TestLongNumberExponentBoundAgrees(t *testing.T) {
	t.Parallel()
	m := longMantissaModel(t)
	st := m.Lookup("Root")
	if st == nil {
		t.Fatal("no table Root")
	}
	for _, tc := range []struct {
		name   string
		token  string
		want   float64
		finite bool
	}{
		{"exponent past the format", "1e10000005", 0, false},
		{"mantissa cancels a long exponent", "0." + strings.Repeat("0", 9999999) + "1e10000005", 1e5, true},
		{"mantissa and exponent past the format", "0." + strings.Repeat("0", 100199) + "1e10000005", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := m.New(st)
			var r tabletext.Report
			engineOK := m.Read(inst, []byte(`{"f":`+tc.token+`}`), &r)
			engine := field(t, inst, "f").Cell.F

			var v tabledemo.ProfileConfig
			var gr tabledemo.TableReport
			goOK := tabledemo.ProfileConfigFromJSON(&v, []byte(`{"precision":`+tc.token+`}`), &gr)

			if engineOK != goOK {
				t.Fatalf("verdicts differ: engine ok=%v (%+v), Go walk ok=%v (%v)", engineOK, r, goOK, gr.Err())
			}
			if !tc.finite {
				if engineOK {
					t.Fatalf("accepted a value past the format: engine %v, Go walk %v", engine, v.Precision)
				}
				return
			}
			if !engineOK {
				t.Fatalf("refused a valid value: engine %+v, Go walk %v", r, gr.Err())
			}
			if engine != tc.want || v.Precision != tc.want {
				t.Fatalf("engine %v, Go walk %v, want %v", engine, v.Precision, tc.want)
			}
		})
	}
}
