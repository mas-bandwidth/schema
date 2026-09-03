package tabletext

import (
	"math/big"
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
