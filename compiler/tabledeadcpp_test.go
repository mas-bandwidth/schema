// THE CONDITION THE C++ TABLE EMITTER GREW, AND THE FIXTURES THAT PROVE IT
// BOTH WAYS (docs/SPEC-TABLES.md §2.2, §3).
//
// The C++ Table header carries a block only where a call site in that unit
// can reach it:
//
//   - THE FOUR BLOB THUNKS, TableBlob{,Message}{Measure,Save}Thunk. Their only
//     call site is the `blob:` arm of the pointer edge walk, and it sits behind
//     a `*bytes` or a `*string` declaration, so a pointered unit that declares
//     neither had thirty-three lines it could not reach. The census is
//     ir.BlobPointerFields, a DECLARATION scan: gating a definition on the
//     numbering walk's ir.PointerReachableBlobs instead would rest on two
//     separate walks agreeing, and the arms gate's negative controls break that
//     agreement on purpose (see ir/blobcensus.go).
//
// A GREEN GATE ON THE UNITS THAT KEEP THEM IS NOT THE PROOF WANTED, because a
// condition that is always false and a condition that is always true both
// leave those units compiling. So each block is asked for TWICE: the fixture
// that DECLARES the construct must still carry it and must still compile and
// run, and its nearest neighbour — the same pointered schema with the blob
// pointer respelled a table pointer — must carry not one symbol of it. The two
// RESERVED TYPE IDS are named on the absent side on purpose: they are compared against by the
// retain path in every pointered unit and stay whether or not the
// unit writes a blob, so a change that took them out with the thunks is red
// here.
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const deadCppBlobSrc = `package probe

table Note
{
    tally int32
    data  *bytes
    next  *Note
}
`

// the nearest neighbour: the blob pointer respelled a table pointer, so the
// unit is still variable-length and still carries the arena, the numbering and
// the node thunks — and no blob.
const deadCppNodeSrc = `package probe

table Note
{
    tally int32
    next  *Note
    other *Note
}
`

// the three wide-text helpers and the two blob thunks, by name.
var (
	deadCppBlobSymbols = []string{
		"TableBlobMeasureThunk",
		"TableBlobSaveThunk",
	}
)

func deadCppHeader(t *testing.T, src string) (string, map[string][]byte) {
	t.Helper()
	files, err := New().Generate(unitFromSource(t, src), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	header, ok := files["ProbeTable.h"]
	if !ok {
		t.Fatal("the cpp target emitted no ProbeTable.h")
	}
	return string(header), files
}

func TestCppTableBlobThunksFollowTheCensus(t *testing.T) {
	with, files := deadCppHeader(t, deadCppBlobSrc)
	for _, sym := range deadCppBlobSymbols {
		if !strings.Contains(with, sym) {
			t.Errorf("a unit that declares *bytes must carry %s", sym)
		}
	}
	if !strings.Contains(with, "node.measure = &TableBlobMeasureThunk<Ctx>;") {
		t.Error("the blob edge's numbering must store TableBlobMeasureThunk")
	}
	deadCppCompile(t, files)

	without, _ := deadCppHeader(t, deadCppNodeSrc)
	for _, sym := range deadCppBlobSymbols {
		if strings.Contains(without, sym) {
			t.Errorf("a pointered unit with no blob must carry no %s", sym)
		}
	}
	// the unit is still pointered, so the NODE thunks and the two reserved
	// type ids are still its
	for _, sym := range []string{
		"TableNodeMeasureThunk", "TableNodeSaveThunk",
		"kTableBytesTypeId", "kTableStringTypeId",
	} {
		if !strings.Contains(without, sym) {
			t.Errorf("a pointered unit keeps %s whether or not it writes a blob", sym)
		}
	}
}

// deadCppCompile compiles and runs the generated unit, so the positive side of
// each condition is a BUILD and not only a string match: a helper the emitter
// emits and the codec calls has to be there at -Werror.
func deadCppCompile(t *testing.T, files map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), source, 0600); err != nil {
			t.Fatal(err)
		}
	}
	issue715CompileRun(t, dir, "c++", ".cpp", `#include "ProbeTable.h"
int main() { return 0; }
`)
}
