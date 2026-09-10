// THE FIRST BYTE, held by test (docs/SPEC-TABLES.md §3). The registry is the
// only place a form byte is assigned a meaning, so what is measured here is
// that it assigns exactly the six §3 names, that byte 0 and every unassigned
// value refuse BY NAME, and that the line a tool prints says which.
package ir

import (
	"strings"
	"testing"
)

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

// BYTE 0 AND ANY UNASSIGNED VALUE ARE REFUSED BY NAME. `6` is the byte the
// unknown-form negative control plants precisely because no form defines or
// reserves it, so this is the row that keeps that true.
func TestWireFormLineRefusesByName(t *testing.T) {
	for _, row := range []struct {
		data []byte
		want string
	}{
		{nil, "REFUSED: an empty file carries no form byte"},
		{[]byte{0}, "REFUSED: form 0 is never assigned"},
		{[]byte{6}, "REFUSED: form 6 is assigned by no form"},
		{[]byte{0xFF}, "REFUSED: form 255 is assigned by no form"},
	} {
		if got := WireFormLine(row.data); !strings.HasPrefix(got, row.want) {
			t.Errorf("WireFormLine(%v) = %q, want a line starting %q", row.data, got, row.want)
		}
	}
}

// AND THE THREE LIVE FORMS SAY WHICH THEY ARE, ON ONE LINE. A planned form
// says it is planned, because a tool that printed `FORM 4 the cook` over bytes
// nothing writes yet would be reporting a form that does not exist.
func TestWireFormLineNamesTheLiveForms(t *testing.T) {
	for _, row := range []struct {
		b    byte
		want string
	}{
		{1, "FORM 1 the variable form (docs/SPEC-TABLES.md §3)"},
		{2, "FORM 2 the message form (docs/SPEC-TABLES.md §3.3)"},
		{3, "FORM 3 the fixed form (docs/SPEC-TABLES.md §3.4)"},
		{4, "FORM 4 the cook — PLANNED, no build reads it yet (docs/SPEC-TABLES.md §7)"},
		{5, "FORM 5 the block form — PLANNED, no build reads it yet (docs/SPEC-TABLES.md §19)"},
	} {
		if got := WireFormLine([]byte{row.b, 0, 0}); got != row.want {
			t.Errorf("WireFormLine(%d) = %q, want %q", row.b, got, row.want)
		}
	}
}
