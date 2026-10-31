package cpptable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestWireSkipCarriesTheTimestampKind pins kind 36 in the emitted
// TableReader::skip. The timestamp's payload is the i64's eight bytes
// (docs/SPEC-TABLES.md §2.11, §3: "Kinds 1–11, 18–29 and 36 skip their fixed
// width"), so a field whose id this reader cannot name, or a kind-mismatched
// field, under kind 36 steps over eight bytes and the read goes on. Without it
// the C++ reader called the field framing damage where the Go oracle counted
// it and read on: the wire fuzzer's red at mutant 688 of seed 24845619678
// (ci-full run 39774840059).
func TestWireSkipCarriesTheTimestampKind(t *testing.T) {
	t.Parallel()
	u := &ir.Unit{Package: "bench"}
	src := tablePrimitives("bench", false, false, false, false, widenCensus{}, 64, u)
	want := "case 5: case 9: case 11: case 23: case 28: case 36: return has( 8 ) ? ( offset += 8, true ) : false;"
	if !strings.Contains(src, want) {
		t.Errorf("the emitted TableReader::skip does not step over a kind 36 timestamp by its eight bytes: want %q", want)
	}
}

// TestWireBoolByteAboveOneIsMalformed pins the tolerant read's bool rule in
// the emitted reader: a bool byte that is neither 0 nor 1 reads true and is
// MALFORMED (docs/SPEC-TABLES.md §6.5), which is what the Go oracle
// (internal/tablewire scalarAt) reports. The C++ reader read it true and said
// nothing: the wire fuzzer's red on tblm1.Msg (Save.force under a union arm).
func TestWireBoolByteAboveOneIsMalformed(t *testing.T) {
	t.Parallel()
	u, cerrs := t03Check(t, `package boolprobe

table Probe
{
    on bool
}
`)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, body := range files {
		all.Write(body)
	}
	src := all.String()
	if strings.Contains(src, ".get8() != 0;") {
		t.Errorf("a bool still reads as `get8() != 0` with no malformed check")
	}
	want := "if ( bool_byte > 1 ) { r.report->malformed = true; }"
	if !strings.Contains(src, want) {
		t.Errorf("the emitted bool read does not flag a byte above 1 as malformed: want %q", want)
	}
}
