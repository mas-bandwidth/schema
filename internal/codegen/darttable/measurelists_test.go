package darttable

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestWireMeasureCountsListExtentsDart pins that the generated Dart
// <root>WireMeasure counts every out-of-line list extent — scalar- and
// pointer-element lists included — so it answers exactly what the Go oracle
// tablewire.FileRegionMeasure does on the same bytes (internal/tablewire/
// extent.go, docs/SPEC-TABLES.md §6.2). The representative cause is
// listdemo.Save on list_scalar_widen (corpus seed list_scalar_widen, pass
// vector #0, run seed 24845619678): the Dart walk measured 128 where Go
// measured 168, because it skipped the []int32 scores and []*LogEntry log.
func TestWireMeasureCountsListExtentsDart(t *testing.T) {
	t.Parallel()

	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "lists", "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schema files under tables/lists: %v", err)
	}
	u := loadUnit(t, schemas...)
	root := u.Tables["Save"]
	if root == nil {
		t.Fatal("the listdemo unit has no Save root")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire, ok := files["ListdemoWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no ListdemoWire.dart")
	}

	oracle := filepath.Join("..", "..", "..", "testdata", "wire", "tables", "fuzz-vectors", "list_scalar_widen.bin")
	data, err := os.ReadFile(oracle)
	if err != nil {
		t.Fatalf("read %s: %v", oracle, err)
	}
	want, _, refused := tablewire.FileRegionMeasure(u, root, data)
	if refused {
		t.Fatal("Go FileRegionMeasure refused a clean corpus read")
	}
	if want != 168 {
		t.Fatalf("Go FileRegionMeasure = %d, want 168 (the 128-vs-168 list_scalar_widen case)", want)
	}

	got := measureProbe(t, "ListdemoWire.dart", wire, "Save", oracle, packetFiles(t, u))
	if int64(got) != want {
		t.Errorf("Dart saveWireMeasure = %d, tablewire.FileRegionMeasure = %d on list_scalar_widen (the 128-vs-168 case)", got, want)
	}

	// The vector #0 mutant is restored from the cold read's receipt with
	// `xxd -r -p`: it is the same 179 bytes, so measuring it must agree with
	// the Go oracle just as the corpus file does, and a clean Go read must
	// never come back as -1 from Dart.
	const mutantHex = "01010e380d030d020a0000803f0308030000000013020a00000040040a000000400308040000000013020a00004040040a0000804003080500000000050e051103020200060e0e02030a000000140000001e000000070c0a0108070908070000000000094b4d57aa3347d2071702864cf563af3a706e3e9343e59d541502864cf463af3154af1d19735012b20f40270b6b9801ffffffffffffffff5f8258ac3615785e8476bc2eef83761e0900000000000000"
	mutant, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatalf("decode the receipt mutant: %v", err)
	}
	mutantPath := filepath.Join(t.TempDir(), "list_scalar_widen_mutant.bin")
	if err := os.WriteFile(mutantPath, mutant, 0o600); err != nil {
		t.Fatal(err)
	}
	mutantWant, _, mutantRefused := tablewire.FileRegionMeasure(u, root, mutant)
	if mutantRefused {
		mutantWant = -1
	}
	got = measureProbe(t, "ListdemoWire.dart", wire, "Save", mutantPath, packetFiles(t, u))
	if int64(got) != mutantWant {
		t.Errorf("Dart saveWireMeasure = %d, tablewire.FileRegionMeasure = %d on the vector #0 mutant", got, mutantWant)
	}
}
