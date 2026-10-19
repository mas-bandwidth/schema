package darttable

// THE SCAN STOPS WHERE THE REFERENCE'S NODE-TABLE WALK STOPS
// (docs/SPEC-TABLES.md §3.1, §4): once open lets the value pass run on a root
// whose body cannot reach its terminator, scanNodeTable must stop at the FIRST
// unframed field — a reference above the entry count, a missing kind byte, an
// unskippable kind — exactly where the reference's nodeTableBytes returns
// (internal/tablewire/nodetable.go). These two pins are the regressions the
// keep-going fuzz found when that stop was missed: the Dart value pass ran on
// where the oracle stopped, counted spurious events and saved spurious bytes.
// Each mutant is a pinned corpus wire (blob_shared for blobdemo.Catalog,
// map_chunks for mapdemo.Chunks) whose FIRST field's kind byte is corrupted to
// an unskippable kind; both the generated reader and the oracle must stop at
// it and answer the same report, measure and saved bytes.

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const blobCatalogSchema = `package blobdemo

table Asset
{
    name    string(32)
    kind    uint32
    data    *bytes
    caption *string
    next    *Asset
}

table Catalog
{
    name  string(32)
    head  *Asset
    thumb *bytes
    note  *string
    alias *bytes
}
`

const mapChunksSchema = `package mapdemo

table Chunks
{
    blobs map[int32]*bytes
    after int32
}
`

// blob_shared.bin, random mutant #35 of the keep-going-style run: a byte in
// the first field's payload is overwritten (kind 17 at the second field sees a
// length that runs past the node table), so the reference's nodeTableBytes
// stops before the node table and the value pass reads where the oracle stops.
// Before the scan stopped with it, the generated reader parsed a bogus node
// table and saved 62 bytes where the oracle saves 25.
const blobCatalogUnframedHex = "01010c047477696e0211022f1102040c070105041052304000861b638ebaadbcc403b2f40f72193b61b746a665bb209250ffffffffffffffffe44f1c4f47c02e2f0500000000000000"

// map_chunks.bin with byte 2 (the first field's kind) set from 14 to 0.
const mapChunksUnframedHex = "010100180d020a0204fdffffff031102000a02040200000003110300040408000000050c0d020604deadbeef0604000180ff005b92de9cabeae214ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffffe44f1c4f47c02e2f0600000000000000"

func runWireUnframedDart(t *testing.T, schema, root, wireFile, loadFn, measureFn, saveFn, mutantHex string) {
	t.Helper()
	t.Parallel()

	mutant, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatalf("the embedded mutant is not hex: %v", err)
	}

	u := buildUnit(t, schema)
	rootDef := u.Tables[root]
	if rootDef == nil {
		t.Fatalf("the unit has no %s root", root)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire, ok := files[wireFile]
	if !ok || len(wire) == 0 {
		t.Fatalf("Generate emitted no %s", wireFile)
	}

	// THE GO ORACLE: the wire fuzzer's own read and measure (internal/tablewire).
	model := tabletext.NewModel(u)
	def := model.Lookup(root)
	if def == nil {
		t.Fatalf("the unit declares no %s", root)
	}
	measured, whole, refused := tablewire.FileRegionMeasure(u, def, mutant)
	if refused || !whole {
		t.Fatalf("the oracle's measure is not exact: refused=%v whole=%v", refused, whole)
	}
	inst := model.New(def)
	var report tabletext.Report
	if _, err := tablewire.DecodeRegion(model, inst, mutant, &report); err != nil {
		t.Fatalf("the oracle refused the replay mutant: %v", err)
	}
	encoded, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatalf("the oracle could not save its read: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, wireFile), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mutant.bin"), mutant, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`import 'dart:io';
import '%s';

String hex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

void main(List<String> args) {
  final data = File(args[0]).readAsBytesSync();
  final report = TableWireReport();
  final measure = %s(data);
  final v = %s(data, report);
  final saved = %s(v);
  stdout.writeln('${report.unknown},${report.kindMismatch},'
      '${report.widened},${report.clamped},${report.malformed},'
      '${report.refused},$measure,${saved.length},${hex(saved)}');
}
`, wireFile, measureFn, loadFn, saveFn)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart", "mutant.bin")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart read of the replay mutant: %v\n%s", err, out)
	}
	fields := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(fields) != 9 {
		t.Fatalf("the probe did not answer nine fields: %q", out)
	}
	num := func(i int) int {
		n, err := strconv.Atoi(fields[i])
		if err != nil {
			t.Fatalf("probe field %d is not a number: %q", i, fields[i])
		}
		return n
	}
	if num(0) != report.Unknown {
		t.Fatalf("unknown: dart %d, oracle %d", num(0), report.Unknown)
	}
	if num(1) != report.KindMismatch {
		t.Fatalf("kindMismatch: dart %d, oracle %d", num(1), report.KindMismatch)
	}
	if num(2) != report.Widened {
		t.Fatalf("widened: dart %d, oracle %d", num(2), report.Widened)
	}
	if num(3) != report.Clamped {
		t.Fatalf("clamped: dart %d, oracle %d", num(3), report.Clamped)
	}
	if (fields[4] == "true") != report.Malformed {
		t.Fatalf("malformed: dart %v, oracle %v", fields[4], report.Malformed)
	}
	if (fields[5] == "true") != report.Refused {
		t.Fatalf("refused: dart %v, oracle %v", fields[5], report.Refused)
	}
	if num(6) != int(measured) {
		t.Fatalf("measure: dart %d, oracle %d", num(6), measured)
	}
	if num(7) != len(encoded) {
		t.Fatalf("saved length: dart %d, oracle %d", num(7), len(encoded))
	}
	if fields[8] != hex.EncodeToString(encoded) {
		t.Fatalf("saved bytes differ:\n dart   %s\n oracle %s", fields[8], hex.EncodeToString(encoded))
	}
}

func TestWireBlobCatalogUnframedRootDart(t *testing.T) {
	runWireUnframedDart(t, blobCatalogSchema, "Catalog", "BlobdemoWire.dart",
		"catalogWireLoad", "catalogWireMeasure", "catalogWireSave", blobCatalogUnframedHex)
}

func TestWireMapChunksUnframedRootDart(t *testing.T) {
	runWireUnframedDart(t, mapChunksSchema, "Chunks", "MapdemoWire.dart",
		"chunksWireLoad", "chunksWireMeasure", "chunksWireSave", mapChunksUnframedHex)
}
