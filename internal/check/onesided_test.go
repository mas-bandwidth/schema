// A one-sided integer range on a table field (docs/SPEC-TABLES.md §4, SPEC
// §4.6): `| min = A` alone means `| min = A, max = <the type's maximum>` and
// `| max = B` alone means `| min = <the type's minimum>, max = B` — the
// one-sided spelling IS the two-sided declaration. Still refused by name on a
// float, fixed point, int128, uint128, timestamp or named type, beside
// resolution, and on a `type` body or packet field.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestOneSidedIntegerRange(t *testing.T) {
	t.Parallel()
	t.Run("clean one-sided table", func(t *testing.T) {
		u := checkUnit(t, `package t
table OneSided
{
    a int64 | min = 0
    b uint16 | max = 9
    c int8 | min = -3
}
`)
		st := u.Tables["OneSided"]
		if st == nil {
			t.Fatal("table OneSided missing")
		}
		want := []struct {
			name string
			min  string
			max  string
		}{
			{"a", "0", "9223372036854775807"},
			{"b", "0", "9"},
			{"c", "-3", "127"},
		}
		if len(st.Fields) != len(want) {
			t.Fatalf("want %d fields, got %d", len(want), len(st.Fields))
		}
		for i, w := range want {
			f := st.Fields[i]
			if !f.RangeOneSided {
				t.Errorf("%s: want RangeOneSided=true", f.Name)
			}
			if !f.HasIntRange {
				t.Errorf("%s: want HasIntRange=true", f.Name)
			}
			if f.IntMin.String() != w.min {
				t.Errorf("%s: want IntMin=%s, got %s", f.Name, w.min, f.IntMin)
			}
			if f.IntMax.String() != w.max {
				t.Errorf("%s: want IntMax=%s, got %s", f.Name, w.max, f.IntMax)
			}
		}
	})
	t.Run("generated go byte-identical to the two-sided spelling", func(t *testing.T) {
		one := checkUnit(t, `package t
table OneSided
{
    a int64 | min = 0
    b uint16 | max = 9
    c int8 | min = -3
}
`)
		two := checkUnit(t, `package t
table OneSided
{
    a int64 | min = 0, max = 9223372036854775807
    b uint16 | min = 0, max = 9
    c int8 | min = -3, max = 127
}
`)
		oneFiles, err := gotable.Generate(one)
		if err != nil {
			t.Fatalf("gotable.Generate(one-sided): %v", err)
		}
		twoFiles, err := gotable.Generate(two)
		if err != nil {
			t.Fatalf("gotable.Generate(two-sided): %v", err)
		}
		if len(oneFiles) != len(twoFiles) {
			t.Fatalf("file counts differ: one-sided %d, two-sided %d", len(oneFiles), len(twoFiles))
		}
		for name, one := range oneFiles {
			two, ok := twoFiles[name]
			if !ok {
				t.Errorf("file %s present one-sided, absent two-sided", name)
				continue
			}
			if string(one) != string(two) {
				t.Errorf("file %s is not byte-identical between the spellings", name)
			}
		}
	})
	t.Run("refused by name", func(t *testing.T) {
		cases := []struct {
			name string
			src  string
			want string
		}{
			{"on a float", "package t\ntable T\n{\n    a float32 | min = 0\n}\n", "a one-sided float range is refused"},
			{"on fixed point", "package t\ntable T\n{\n    a fixed(24, 8) | min = 0\n}\n", "not valid on fixed point"},
			{"on int128", "package t\ntable T\n{\n    a int128 | min = 0\n}\n", "not valid on int128"},
			{"on uint128", "package t\ntable T\n{\n    a uint128 | min = 0\n}\n", "not valid on uint128"},
			{"on a timestamp", "package t\ntable T\n{\n    at timestamp | min = 0\n}\n", "not valid on timestamp"},
			{"on a named type", "package t\ntype Name\n{\n    x int32\n}\ntable T\n{\n    n Name | min = 0\n}\n", "not valid on Name"},
			{"beside resolution", "package t\ntable T\n{\n    a float32 | min = 0, resolution = 0.5\n}\n", "a one-sided float range is refused"},
			{"on a type body field", "package t\ntype P\n{\n    x int32 | min = 0\n}\n", "min without max (or vice versa) is a compile error"},
		}
		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				errs := runUnit(t, map[string]string{"T.schema": tc.src})
				if len(errs) == 0 {
					t.Fatal("compiled clean — one-sided range was accepted")
				}
				var got bool
				for _, e := range errs {
					if strings.Contains(e.Error(), tc.want) {
						got = true
					}
				}
				if !got {
					t.Fatalf("no diagnostic contains %q; got %v", tc.want, errs)
				}
			})
		}
	})
	t.Run("ir flag is not a projection token", func(t *testing.T) {
		// the filled range is the two-sided declaration: the projection must be
		// identical to spelling both sides
		one := checkUnit(t, `package t
table OneSided
{
    a int64 | min = 0
}
`)
		two := checkUnit(t, `package t
table OneSided
{
    a int64 | min = 0, max = 9223372036854775807
}
`)
		if ir.WireProjection(one) != ir.WireProjection(two) {
			t.Errorf("projections differ: one-sided %s, two-sided %s", ir.WireProjection(one), ir.WireProjection(two))
		}
	})
}

var _ = ir.TInt
