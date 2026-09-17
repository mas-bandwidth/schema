package rusttable

import (
	"strings"
	"testing"
)

// The Rust table wire is the ID-TABLE form (docs/SPEC-TABLES.md §3,
// schema#518/#435): the form byte is read first, every length, count, index
// and reference is a canonical LEB128, identities are 64-bit first-use
// references, and the id table is the last thing in the file, located from the
// end by its fixed little-endian u64 entry count.
//
// This is the red-first gate: it failed while the port carried no wire at all
// and passes when the codecs are back in the id-table form.
func TestRustTableWireIsIdTableForm(t *testing.T) {
	out := generate(t, valueOnly)

	table, ok := out["probe_table.rs"]
	if !ok {
		t.Fatalf("no probe_table.rs: the port emits no table wire (docs/SPEC-TABLES.md §3)")
	}
	wire, ok := out["table_runtime.rs"]
	if !ok {
		t.Fatalf("no table_runtime.rs: the id-table framing runtime is missing")
	}
	text := string(table)
	runtime := string(wire)

	// THE FORM BYTE IS FIRST. Every save opens by putting form 1 before the
	// body, and every load reads it first.
	if !strings.Contains(text, "put8(1);") {
		t.Errorf("the emitted table wire never writes the form byte 1 first (docs/SPEC-TABLES.md §3)")
	}

	// EVERY NUMBER IS A CANONICAL LEB128. The length, count, index and id
	// reference all ride putleb; a non-minimal spelling is malformed.
	for _, fn := range []string{"pub fn putleb", "pub fn getleb", "pub fn putid", "pub fn getid"} {
		if !strings.Contains(runtime, fn) {
			t.Errorf("the emitted wire runtime has no %s — a number is not a canonical LEB128 (docs/SPEC-TABLES.md §3)", fn)
		}
	}

	// THE ID TABLE IS LAST, located from the END: every identity once, in
	// first-use order, then the entry count as a fixed little-endian u64.
	if !strings.Contains(runtime, "put64(self.ids[i])") || !strings.Contains(runtime, "put64(self.count as u64)") {
		t.Errorf("the emitted wire does not write the trailing id table as entries then a fixed little-endian u64 count")
	}

	// IDENTITY AT SIXTY-FOUR BITS, with the reserved node-table id named.
	if !strings.Contains(runtime, "u64::MAX") {
		t.Errorf("the emitted wire names no 64-bit reserved id (docs/SPEC-TABLES.md §3)")
	}
}
