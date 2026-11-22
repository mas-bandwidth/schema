// THE FIRST BYTE, held by test (docs/SPEC-TABLES.md §3). The registry is the
// only place a form byte is assigned a meaning, so what is measured here is
// that it assigns exactly the six §3 names. That byte 0 and every unassigned
// value refuse BY NAME, and the line a tool prints, are fileformat_test.go's.
package ir

import "testing"

// THE REGISTRY IS APPEND-ONLY AND ITS BYTES ARE ITS INDICES. A form that has
// shipped keeps its number forever, so a row that moved is a wire break and
// this is the test that sees it.
func TestWireFormRegistryIsTheSixRowsInOrder(t *testing.T) {
	want := []struct {
		name    string
		planned bool
	}{
		{"never assigned", false},
		{"the variable form", false},
		{"the message form", false},
		{"the fixed form", false},
		{"the cook", true},
		{"the block form", true},
	}
	if len(WireForms) != len(want) {
		t.Fatalf("the registry is %d rows, want %d — a new form is a spec change (§3)", len(WireForms), len(want))
	}
	for i, w := range want {
		got := WireForms[i]
		if got.Byte != uint8(i) {
			t.Errorf("row %d carries byte %d: the registry is append-only and a form's number is forever", i, got.Byte)
		}
		if got.Name != w.name {
			t.Errorf("form %d is %q, want %q", i, got.Name, w.name)
		}
		if got.Planned != w.planned {
			t.Errorf("form %d planned=%v, want %v", i, got.Planned, w.planned)
		}
	}
}
