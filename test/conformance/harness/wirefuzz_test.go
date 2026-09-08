package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// The C++ CatalogLoadMeasure returns 400 bytes (112 attribution) for this
// mutant. Four blob records command 72 data bytes: retaining only their type
// identities in the oracle used to omit all four and incorrectly demand 328.
func TestFileFuzzMeasureIncludesBlobLengths(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "blobdemo", "Catalog", false, false)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := hex.DecodeString("010c0c03617274021102031106041107050c43060618010c05627269636b0708070000000811030911040a1105000b030102030c036361700608010c047461696c000b05deadbeef7f0c0b68656c6c6f20626c6f627300861b638ebaadbcc4030c9a5fcc128f0a03b2f40f72193b61dd7c58d1bafbf83bfffffffffffffffffb645ffecc6d4e8e43326721d7969cef054aa33067555b85a5024c86eb28aa5d28f025a0ba6c31e5e44f1c4f47c02e2f58fcaffad8e04b700c00000000000000")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !answer.exact || answer.bytes != 400 || answer.report != (Counts{Unknown: 1}) {
		t.Fatalf("file framing: %+v", answer)
	}
}

func TestFileFuzzCountCapIsFramingRefusal(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "listdemo", "Save", false, false)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join("testdata", "wire", "tables", "fuzz-vectors", "list_count_cap.bin"))
	if err != nil {
		t.Fatal(err)
	}
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !answer.measureRefused || answer.bytes != -1 || answer.report.Malformed {
		t.Fatalf("count cap: %+v", answer)
	}
	if verdict := wireVerdict(root, legReply{measure: -1}, answer); verdict != "" {
		t.Fatal(verdict)
	}
	if verdict := wireVerdict(root, legReply{loaded: true, measure: 1024}, answer); verdict == "" {
		t.Fatal("accepted loaded count above the cap")
	}
}

// A mutable reader has no region preflight. Its int32 count-cap refusal
// discards the partial value without the region's recovery event.
func TestBuilderFuzzChecksCountRefusalWithoutRegionPreflight(t *testing.T) {
	_, _, units := corpus(t)
	for _, tc := range []struct{ name, table string }{
		{"list_count_cap", "Save"}, {"list_count_cross_length", "Album"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := newWireRoot(units, "listdemo", tc.table, false, false)
			if err != nil {
				t.Fatal(err)
			}
			root.builder = true
			wire, err := os.ReadFile(filepath.Join("testdata", "wire", "tables", "fuzz-vectors", tc.name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			answer, err := root.oracle(wire)
			if err != nil {
				t.Fatal(err)
			}
			if answer.measureRefused || answer.report.Malformed || !answer.encFail || !answer.builderStopped {
				t.Fatalf("builder count cap: %+v", answer)
			}
			reply := legReply{loaded: false, measure: -1, report: answer.report, saveFail: true}
			if verdict := wireVerdict(root, reply, answer); verdict != "" {
				t.Fatal(verdict)
			}
			reply.report.Malformed = true
			if verdict := wireVerdict(root, reply, answer); verdict == "" {
				t.Fatal("builder gate ignored an invented region recovery event")
			}
		})
	}
}
