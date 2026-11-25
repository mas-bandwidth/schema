package main

import (
	"strings"
	"testing"
)

// TestTheCReasonRestatementIsDroppedWithLayoutUnknown holds the twin map to
// the C runtime as the emitter writes it since 73b36bfb3d: the form-3 reason
// enum the C runtime restates now ends at SCHEMA_TABLE_LAYOUT_UNKNOWN = 22
// (FIXED-FORM-ALGORITHM §5.3 step 5), and the whole restatement is still
// dropped (bench/paired/TWIN.md). An enum the map does not recognise is left
// in, so a new tail the map was not taught stays a leftover line.
func TestTheCReasonRestatementIsDroppedWithLayoutUnknown(t *testing.T) {
	enum := `enum {
    SCHEMA_TABLE_NO_LAYOUT = 7,
    SCHEMA_TABLE_LAYOUT_MALFORMED = 8,
    SCHEMA_TABLE_TEXT_ILL_FORMED = 20,
    SCHEMA_TABLE_LAYOUT_UNKNOWN = 22
};
`
	for _, line := range canonicalize(enum) {
		if strings.TrimSpace(line) != "" {
			t.Fatalf("the C reason restatement ending at layout_unknown left %q", line)
		}
	}
	// The control: an enumerator the map does not know after it is not erased.
	planted := strings.Replace(enum, "SCHEMA_TABLE_LAYOUT_UNKNOWN = 22", "SCHEMA_TABLE_LAYOUT_UNKNOWN = 22,\n    SCHEMA_TABLE_SOMETHING_NEW = 23", 1)
	if !strings.Contains(strings.Join(canonicalize(planted), "\n"), "SOMETHING_NEW") {
		t.Fatal("an enumerator the twin map was not taught was erased with the restatement")
	}
}
