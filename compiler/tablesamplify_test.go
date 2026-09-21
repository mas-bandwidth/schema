package compiler

import (
	"os"
	"strings"
	"testing"
)

// The TABLE-read AMPLIFICATION BOUND (schema#466, docs/SPEC-TABLES.md §6.5,
// docs/SECURITY.md). Cap'n Proto forbids node sharing and defends a hostile
// read with a traversal limit; schema keeps sharing and reads tables as
// untrusted input, so the page must state what a hostile pointer graph can
// make LoadMeasure materialize, and the statement must match the C++
// reference.
//
// This is the RED test for the statement: before the page said TWO wire bytes
// (a record's type id reference and its declared length) the bound it printed
// was THREE, which the reference does not honor. The reference counts a
// record from the framing and decodes no body (TableNodeScanNext), so a
// hostile record is two wire bytes and the worst-case amplification is the
// schema's largest record per two wire bytes.
func TestTableAmplificationBoundIsStated(t *testing.T) {
	page, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	security, err := os.ReadFile("../docs/SECURITY.md")
	if err != nil {
		t.Fatal(err)
	}

	// 1. the page states the two-wire-byte record, not a three-byte one
	if strings.Contains(string(page), "THREE wire bytes") {
		t.Error("SPEC-TABLES §6.5 still states the smallest hostile record is THREE wire bytes; the framing proves TWO")
	}
	if !strings.Contains(string(page), "TWO wire bytes") {
		t.Error("SPEC-TABLES §6.5 does not state the two-wire-byte hostile record")
	}
	if !strings.Contains(string(page), "amplification") {
		t.Error("SPEC-TABLES §6.5 does not name the amplification bound")
	}

	// 2. the security page carries the same bound
	if !strings.Contains(string(security), "TWO wire bytes") {
		t.Error("docs/SECURITY.md does not state the two-wire-byte table-read amplification bound")
	}
	if !strings.Contains(string(security), "amplification") && !strings.Contains(string(security), "materialize") {
		t.Error("docs/SECURITY.md does not state the table-read amplification bound")
	}

	// 3. the C++ reference the page is certified against agrees: the record
	// scan advances by the declared length alone and reads no body, so a
	// record can be a type id reference and a zero length.
	reference, err := os.ReadFile("../testdata/golden/tables/pointers/GraphTable.h")
	if err != nil {
		t.Fatal(err)
	}
	if !referenceBoundHolds(string(reference)) {
		t.Error("the C++ reference no longer sizes a record from its declared length alone")
	}
}

// referenceBoundHolds is the two facts the golden C++ must keep for the
// two-wire-byte record to hold: the scan advances by the declared length
// alone (a zero length is a legal, bodyless record) and it counts the record
// from the framing.
func referenceBoundHolds(reference string) bool {
	return strings.Contains(reference, "s.payload_offset = rec.offset + length;") &&
		strings.Contains(reference, "s.records++;")
}

// TestTableAmplificationBoundNegativeControl is the red test's own control
// (schema#466): a check that can only pass is no check, so remove the
// record-length bound — the one line that lets a record be bodyless — from a
// COPY of the reference and require the bound to fall. The sabotage is
// in-memory; no tracked file moves.
func TestTableAmplificationBoundNegativeControl(t *testing.T) {
	reference, err := os.ReadFile("../testdata/golden/tables/pointers/GraphTable.h")
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := strings.Replace(string(reference), "s.payload_offset = rec.offset + length;", "s.payload_offset = rec.offset;", 1)
	if sabotaged == string(reference) {
		t.Fatal("the negative control patched nothing")
	}
	if referenceBoundHolds(sabotaged) {
		t.Fatal("the bound stayed green with the record's declared-length advance removed")
	}
}
