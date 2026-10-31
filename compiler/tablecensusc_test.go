// THE CENSUS-GATED RUNTIMES OF THE C TABLE EMITTER, AND THE FIXTURES THAT
// PROVE EACH BOTH WAYS (docs/SPEC-TABLES.md §2.2, §3, §4). Card C-6's own
// dead-output condition, check_default, is next door in tabledeadc_test.go.
//
// The C Table header used to carry a block in units that no call site in
// those units could reach:
//
//   - THE BYTE BUFFER'S OWN SURFACE: the two views, the two reserved type ids,
//     the three read accessors and the blob node type. All of it is reached
//     from a `*bytes` or a `*string` DECLARATION and from nowhere else, so a
//     pointered unit that declares neither had thirty-two lines it could not
//     reach.
//
// EACH CENSUS IS A DECLARATION SCAN, never a reachability walk. Gating a
// DEFINITION on the numbering walk's ir.PointerReachableBlobs would rest on two
// separate walks agreeing, and the arms gate's negative controls break that
// agreement on purpose (see ir/blobcensus.go); the same reasoning makes the
// kind census ir.TableDeclaredKinds a superset of every shape test the widening
// call sites are emitted behind (see ir/tablekindcensus.go).
//
// A GREEN GATE ON THE UNITS THAT KEEP THEM IS NOT THE PROOF WANTED, because a
// condition that is always false and a condition that is always true both leave
// those units compiling. So each block is asked for TWICE: the fixture that
// DECLARES the construct must carry it, must CALL it, and must compile and run
// at -Werror with the sanitizers on; its NEAREST NEIGHBOUR — float64 respelled
// float32, the blob pointers respelled table pointers
// — must carry not one symbol of it AND must still compile, which is the half
// that catches a gate cut too deep.
//
// THE BYTE BUFFER'S FLOOR is named on the absent side on purpose. TableBlob,
// table_blob_storage and the emplace family are read by code that is in every
// pointered unit whether or not it declares a buffer — table_node_storage,
// table_cook_layout, the message form's record carve, and the JSON graph walk
// in every pointered <Base>Table.c — so a change that took them out with the
// rest is red here.
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const deadCF64Src = `package probe

fixed table Note
{
    ratio float64
    tally int32
}
`

// the nearest neighbour: the one declaration respelled at its ladder's bottom,
// so nothing on the float rung is declared and nothing widens into it.
const deadCF32Src = `package probe

fixed table Note
{
    ratio float32
    tally int32
}
`

// A POINTERED unit that declares both byte buffers: a field of each kind, an
// arm, and a map's value, which is every shape §2.5 allows one in.
const deadCBlobSrc = `package probe

table Leaf { v int32 }

union Reach
{
    text  *string
    plain int32
}

table Note
{
    tally int32
    data  *bytes
    docs  map[uint32]*bytes
    reach Reach
    leaf  *Leaf
    next  *Note
}
`

// The nearest neighbour: every blob pointer respelled a table pointer. The unit
// is still variable-length and still carries the arena, the numbering, the map,
// the graph and the message form — and no byte buffer.
const deadCNodeSrc = `package probe

table Leaf { v int32 }

union Reach
{
    only  *Leaf
    plain int32
}

table Note
{
    tally int32
    data  *Leaf
    docs  map[uint32]*Leaf
    reach Reach
    leaf  *Leaf
    next  *Note
}
`

var (
	// the byte buffer's own surface: the views, the reserved ids, the read
	// accessors and the blob node type.
	deadCBlobSymbols = []string{
		"TableBytesView", "TableStringView",
		"kTableBytesTypeId", "kTableStringTypeId",
		"table_blob_at", "table_bytes_at", "table_string_at",
		"table_blob_save", "table_blob_edges",
		"table_bytes_node_type", "table_string_node_type",
	}
	// and the floor every pointered unit keeps, buffer or no buffer: the
	// generic walks name each of these at a `type->blob` branch they take at
	// RUN time, and the JSON graph walk emplaces a buffer for text it reads.
	deadCBlobFloor = []string{
		"TableBlob", "table_blob_storage",
		"table_blob_emplace", "table_bytes_emplace", "table_string_emplace",
	}
)

func deadCHeader(t *testing.T, src string) (string, map[string][]byte) {
	t.Helper()
	files, err := generateCUnqualified(unitFromSource(t, src))
	if err != nil {
		t.Fatal(err)
	}
	header, ok := files["ProbeTable.h"]
	if !ok {
		t.Fatal("the c target emitted no ProbeTable.h")
	}
	return string(header), files
}

// TestCTableFloatRungIsTheFiniteTestAndTheConversion holds the float rung,
// kind 10 into a declared kind 11, to the finite rule (docs/SPEC-TABLES.md
// §3.3, §4). There was a census-gated table_wire_widen_f32 here, which carried
// a NaN's payload across by hand; a NaN or an infinity is now refused on its
// raw bits before the rung, and every finite float32 is exactly a float64, so
// the rung is the ordinary conversion and no unit carries a helper for it.
func TestCTableFloatRungIsTheFiniteTestAndTheConversion(t *testing.T) {
	with, files := deadCHeader(t, deadCF64Src)
	if strings.Contains(with, "table_wire_widen_f32") {
		t.Error("the NaN-payload surgery table_wire_widen_f32 is still emitted")
	}
	// the file wire's widened scalar and the message form's f32-shaped entry
	// read into the f64 field: the finite test, then the conversion
	for _, want := range []string{
		"if ( !table_bits_finite32( widened_bits ) ) { table_non_finite_float( r->report ); break; }",
		"double decoded_wide = (double) table_bits_to_float( widened_bits );",
		"decoded=(double)narrow;",
	} {
		if !strings.Contains(with, want) {
			t.Errorf("the f64 field's float rung must read %q", want)
		}
	}
	deadCCompile(t, files)

	without, files := deadCHeader(t, deadCF32Src)
	if strings.Contains(without, "decoded_wide = (double)") {
		t.Error("a unit that declares no float64 has no float rung to emit")
	}
	// the ladder PREDICATE stays in every unit: a kind comparison is what
	// every reader does, and it is named on the absent side on purpose
	if !strings.Contains(without, "static SCHEMA_UNUSED int table_kind_widens") {
		t.Error("table_kind_widens is every unit's and the f32 gate took it")
	}
	deadCCompile(t, files)
}

func TestCTableBlobRuntimeFollowsTheCensus(t *testing.T) {
	with, files := deadCHeader(t, deadCBlobSrc)
	for _, sym := range append(append([]string{}, deadCBlobSymbols...), deadCBlobFloor...) {
		if !strings.Contains(with, sym) {
			t.Errorf("a unit that declares *bytes must carry %s", sym)
		}
	}
	// the node-type switch is the one site that names the two records, and it
	// is what makes the definitions REACHED rather than merely present
	if !strings.Contains(with, "case kTableBytesTypeId: return &table_bytes_node_type;") {
		t.Error("the graph's node-type switch must name table_bytes_node_type")
	}
	if !strings.Contains(with, "case kTableStringTypeId: return &table_string_node_type;") {
		t.Error("the graph's node-type switch must name table_string_node_type")
	}
	deadCCompile(t, files)

	without, files := deadCHeader(t, deadCNodeSrc)
	for _, sym := range deadCBlobSymbols {
		if strings.Contains(without, sym) {
			t.Errorf("a pointered unit with no byte buffer must carry no %s", sym)
		}
	}
	for _, sym := range deadCBlobFloor {
		if !strings.Contains(without, sym) {
			t.Errorf("a pointered unit keeps %s whether or not it declares a byte buffer", sym)
		}
	}
	// the unit is still pointered, so the numbering and the node walk are its
	for _, sym := range []string{"table_node_storage", "table_number_build"} {
		if !strings.Contains(without, sym) {
			t.Errorf("a pointered unit keeps %s", sym)
		}
	}
	// AND IT STILL COMPILES, which is the half that catches a gate cut too
	// deep: every name the generic walks read at a `type->blob` branch has to
	// be declared even where no node type of the unit sets the column.
	deadCCompile(t, files)
}

// deadCCompile compiles and runs the generated unit, so each side of each
// condition is a BUILD and not only a string match: a helper the emitter emits
// and the codec calls has to be there at -Werror, and a helper the emitter
// withheld must be one nothing left behind still names.
func deadCCompile(t *testing.T, files map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), source, 0600); err != nil {
			t.Fatal(err)
		}
	}
	issue715CompileRun(t, dir, "clang", ".c", `#include "ProbeTable.h"
int main( void ) { return 0; }
`)
}
