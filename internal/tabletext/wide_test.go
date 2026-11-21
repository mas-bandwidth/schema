package tabletext

import (
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The two exact conversions of docs/SPEC-TABLES.md §16.2 at the wide kinds,
// and the saturation and exactness rules the hostile corpus holds both engines
// to (testdata/conformance/tables/json-hostile/fixed-* and int128-*).
func TestWideText(t *testing.T) {
	type row struct {
		token     string
		kind      int
		frac      int
		raw       string // "" = inexact (kind mismatch)
		saturated bool
	}
	rows := []row{
		{"1.0", ir.TableKindFixed32, 16, "65536", false},
		{"-0.5", ir.TableKindFixed32, 16, "-32768", false},
		{"0.1", ir.TableKindFixed32, 16, "", false},
		{"2", ir.TableKindFixed32, 16, "131072", false},
		{"15e-1", ir.TableKindFixed32, 16, "98304", false},
		{"1e3", ir.TableKindFixed32, 0, "1000", false},
		{"-0.0", ir.TableKindFixed32, 16, "0", false},
		{"-0", ir.TableKindUFixed32, 16, "0", false},
		{"-0.5", ir.TableKindUFixed32, 16, "0", true},
		{"1e999999999", ir.TableKindFixed32, 16, "170141183460469231731687303715884105727", true},
		{"-1e400", ir.TableKindUFixed32, 16, "0", true},
		{"1e-999999999", ir.TableKindFixed32, 16, "", false},
		{"0.0000152587890625", ir.TableKindFixed32, 16, "1", false},
		{"340282366920938463463374607431768211455", ir.TableKindU128, 0, "340282366920938463463374607431768211455", false},
		{"340282366920938463463374607431768211456", ir.TableKindU128, 0, "340282366920938463463374607431768211455", true},
		{"-1", ir.TableKindU128, 0, "0", true},
		{"1.5", ir.TableKindI128, 0, "", false},
		{"3e38", ir.TableKindU128, 0, "300000000000000000000000000000000000000", false},
		{"-170141183460469231731687303715884105729", ir.TableKindI128, 0, "-170141183460469231731687303715884105728", true},
		{"99.99999999976716935634613037109375", ir.TableKindFixed64, 32, "429496729599", false},
	}
	for _, r := range rows {
		raw, exact, saturated := ParseWide(r.token, r.kind, r.frac)
		if r.raw == "" {
			if exact {
				t.Errorf("%s: placed %v, the value is not representable", r.token, raw)
			}
			continue
		}
		if !exact || raw.String() != r.raw || saturated != r.saturated {
			t.Errorf("%s: got %v exact=%v saturated=%v, want %s saturated=%v", r.token, raw, exact, saturated, r.raw, r.saturated)
		}
	}
	format := []struct {
		raw  string
		kind int
		frac int
		want string
	}{
		{"65536", ir.TableKindFixed32, 16, "1.0"},
		{"-32768", ir.TableKindFixed32, 16, "-0.5"},
		{"1", ir.TableKindFixed32, 16, "0.0000152587890625"},
		{"0", ir.TableKindFixed32, 16, "0.0"},
		{"1000", ir.TableKindFixed32, 0, "1000.0"},
		{"429496729599", ir.TableKindFixed64, 32, "99.99999999976716935634613037109375"},
		{"-1267650600228229401496703205376", ir.TableKindI128, 0, "-1267650600228229401496703205376"},
		{"340282366920938463463374607431768211455", ir.TableKindU128, 0, "340282366920938463463374607431768211455"},
	}
	for _, r := range format {
		raw, _ := new(big.Int).SetString(r.raw, 10)
		if got := FormatWide(raw, r.kind, r.frac); got != r.want {
			t.Errorf("format %s F=%d: got %s, want %s", r.raw, r.frac, got, r.want)
		}
		// the bytes round-trip at the kind's width
		back := WideFromBytes(WideBytes(raw, r.kind), r.kind)
		if back.Cmp(raw) != 0 {
			t.Errorf("bytes %s: back as %v", r.raw, back)
		}
	}
}

// TABLETEXT-5. A token whose decimal fraction is longer than F can spell is
// inexact by counting alone: with trailing zeros stripped the value has
// exactly len(digits) - point fractional decimal digits, and a dyadic fraction
// of F bits has at most F of them. ParseWide answers it before any big.Int is
// built, so a hostile megabyte of fraction digits costs a linear scan and not
// a quadratic decimal conversion. The work is counted as allocations, never
// timed: the refusal allocates nothing, at any length.
func TestWideLongFractionRefusesWithoutArithmetic(t *testing.T) {
	for _, n := range []int{100, 100000} {
		token := "0." + strings.Repeat("1", n)
		for _, kind := range []int{ir.TableKindFixed32, ir.TableKindFixed64} {
			if _, exact, _ := ParseWide(token, kind, 16); exact {
				t.Fatalf("%d fraction digits at F=16: placed, want inexact", n)
			}
			allocs := testing.AllocsPerRun(3, func() { ParseWide(token, kind, 16) })
			if allocs != 0 {
				t.Errorf("%d fraction digits at F=16: %v allocations, want 0 (the arithmetic ran)", n, allocs)
			}
		}
	}
	// the boundary: 16 fractional digits at F=16 is exactly 2^-16 when the
	// digits spell it, and 17 never is
	if raw, exact, _ := ParseWide("0.0000152587890625", ir.TableKindFixed32, 16); !exact || raw.String() != "1" {
		t.Fatalf("2^-16: got %v exact=%v", raw, exact)
	}
	if _, exact, _ := ParseWide("0.00001525878906251", ir.TableKindFixed32, 16); exact {
		t.Fatal("17 fractional digits at F=16 placed")
	}
	// trailing zeros do not count, and an exponent moves the point
	if raw, exact, _ := ParseWide("0.5"+strings.Repeat("0", 100000), ir.TableKindFixed32, 16); !exact || raw.String() != "32768" {
		t.Fatalf("0.5 with trailing zeros: got %v exact=%v", raw, exact)
	}
	if raw, exact, _ := ParseWide("5"+strings.Repeat("1", 30)+"e-30", ir.TableKindFixed32, 16); exact {
		t.Fatalf("30 fractional digits through an exponent placed %v", raw)
	}
}
