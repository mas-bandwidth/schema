package tablewire_test

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A count past the int32 storage cap refuses count_over_extent_cap on every
// read (docs/SPEC-TABLES.md §2.9, §6.5), and a refusal carries no counter. The wires are the cap rows of
// internal/codegen/goosetable/testdata/load_refusal_reasons.tsv, over that
// table's measure unit.
func TestCountCapRefusalMovesNoCounter(t *testing.T) {
	t.Parallel()
	m := listModel(t, "package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n")
	root := m.Lookup("Root")
	for _, row := range []struct{ name, wire string }{
		{"values_cap31", "01010e0604808080800800fb674d1acf7b27210100000000000000"},
		{"entries_cap31", "01010e060d80808080080053a245082ca7b2c50100000000000000"},
		{"values_widened_over_cap", "01010e0602808080800800fb674d1acf7b27210100000000000000"},
	} {
		wire, err := hex.DecodeString(row.wire)
		if err != nil {
			t.Fatal(err)
		}
		report := tabletext.Report{Unknown: 3}
		ok, err := tablewire.Decode(m, m.New(root), wire, &report)
		var capRefusal *tablewire.CountRefusal
		if ok || !errors.As(err, &capRefusal) || !report.Refused || report.Unknown != 0 {
			t.Errorf("%s: builder %v %v %+v, want the count-cap refusal with no counter moved", row.name, ok, err, report)
		}
	}
	// the control: a widening element kind whose count lands counts one
	// widened, and the region read refuses the over-cap count as the value
	// read does
	wire, err := hex.DecodeString("01010e0302010900fb674d1acf7b27210100000000000000")
	if err != nil {
		t.Fatal(err)
	}
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(root), wire, &report); !ok || err != nil || report != (tabletext.Report{Widened: 1}) {
		t.Errorf("a widened list under the cap: %v %v %+v, want one widened", ok, err, report)
	}
	wire, err = hex.DecodeString("01010e0602808080800800fb674d1acf7b27210100000000000000")
	if err != nil {
		t.Fatal(err)
	}
	report = tabletext.Report{}
	var capRefusal *tablewire.CountRefusal
	if ok, err := tablewire.DecodeRegion(m, m.New(root), wire, &report); ok || !errors.As(err, &capRefusal) || !report.Refused {
		t.Errorf("a widened list over the cap into a region: %v %v %+v, want the count-cap refusal", ok, err, report)
	}
}
