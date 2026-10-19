package darttable

// THE SCAN STOPS WHERE THE REFERENCE'S NODE-TABLE WALK STOPS
// (docs/SPEC-TABLES.md §3.1, §4): once open lets the value pass run on a root
// whose body cannot reach its terminator, scanNodeTable must stop at the FIRST
// unframed field — a reference above the entry count, a missing kind byte, an
// unskippable kind — exactly where the reference's nodeTableBytes returns
// (internal/tablewire/nodetable.go). These pins are the regressions the
// keep-going fuzz found when that stop was missed: the Dart value pass ran on
// where the oracle stopped, counted spurious events and saved spurious bytes.
// The first two are pinned corpus wires (blob_shared for blobdemo.Catalog,
// map_chunks for mapdemo.Chunks) whose FIRST field's kind byte is corrupted to
// an unskippable kind. The last four are r1's regressions-at-head cases, whose
// damaged reference is 1<<63 or one past the entry count. Both the generated
// reader and the oracle must stop at the damage and answer the same report,
// measure and saved bytes.

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
	"github.com/mas-bandwidth/schema/v2/ir"
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

// THE FOUR r1 REGRESSIONS, from dart-fix-mixed-kind-mismatch-count.r1.rowan.1/
// regressions-at-head.log: the stock keep-going fuzz's four scan-stop
// divergences, two on graphdemo.Scene and two on blobdemo.Catalog. The first
// pair writes a ten-byte reference of 1<<63 (a negative Dart int, an unsigned
// value above every entry); the second writes an ordinary reference above the
// entry count. In all four the oracle stops the node-table walk at the damaged
// reference, keeps only the fields before it, and saves the bytes below; the
// Dart scan must stop at the same reference rather than finding a node table
// past it.
const (
	sceneRefOverflowHex   = "01010c0474726565020402000000031102ffffffffffffffffff011104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000"
	catalogRefOverflowHex = "01010c03617274021102ffffffffffffffffff011106041107050c43060618010c05627269636b0708070000000811030911040a1105000b030102030c036361700608010c047461696c000b05deadbeef7f0c0b68656c6c6f20626c6f627300861b638ebaadbcc4030c9a5fcc128f0a03b2f40f72193b61dd7c58d1bafbf83bfffffffffffffffffb645ffecc6d4e8e43326721d7969cef054aa33067555b85a5024c86eb28aa5d28f025a0ba6c31e5e44f1c4f47c02e2f58fcaffad8e04b700c00000000000000"
	sceneRefUnnamedHex    = "01010c0474726565020402000000031102101104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000"
	catalogRefUnnamedHex  = "01010c036172740211020d1106041107050c43060618010c05627269636b0708070000000811030911040a1105000b030102030c036361700608010c047461696c000b05deadbeef7f0c0b68656c6c6f20626c6f627300861b638ebaadbcc4030c9a5fcc128f0a03b2f40f72193b61dd7c58d1bafbf83bfffffffffffffffffb645ffecc6d4e8e43326721d7969cef054aa33067555b85a5024c86eb28aa5d28f025a0ba6c31e5e44f1c4f47c02e2f58fcaffad8e04b700c00000000000000"
)

func runWireUnframedDart(t *testing.T, schema, root, wireFile, loadFn, measureFn, saveFn, mutantHex string) {
	t.Helper()
	t.Parallel()
	runWireUnframedCore(t, buildUnit(t, schema), root, wireFile, loadFn, measureFn, saveFn, mutantHex)
}

// runWireUnframedDirDart is runWireUnframedDart against a unit on disk: the
// four r1 regressions below are the pointer and blob corpora, whose tables
// span more than one schema file.
func runWireUnframedDirDart(t *testing.T, unitDir, root, loadFn, measureFn, saveFn, mutantHex string) {
	t.Helper()
	t.Parallel()
	u := loadDirUnit(t, unitDir)
	runWireUnframedCore(t, u, root, runtimeHome(u)+"Wire.dart", loadFn, measureFn, saveFn, mutantHex)
}

func runWireUnframedCore(t *testing.T, u *ir.Unit, root, wireFile, loadFn, measureFn, saveFn, mutantHex string) {
	t.Helper()

	mutant, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatalf("the embedded mutant is not hex: %v", err)
	}

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

func TestWireSceneRefOverflowUnframedRootDart(t *testing.T) {
	runWireUnframedDirDart(t, filepath.Join("..", "..", "..", "tables", "pointers"), "Scene",
		"sceneWireLoad", "sceneWireMeasure", "sceneWireSave", sceneRefOverflowHex)
}

func TestWireCatalogRefOverflowUnframedRootDart(t *testing.T) {
	runWireUnframedDirDart(t, filepath.Join("..", "..", "..", "tables", "blobs"), "Catalog",
		"catalogWireLoad", "catalogWireMeasure", "catalogWireSave", catalogRefOverflowHex)
}

func TestWireSceneRefUnnamedUnframedRootDart(t *testing.T) {
	runWireUnframedDirDart(t, filepath.Join("..", "..", "..", "tables", "pointers"), "Scene",
		"sceneWireLoad", "sceneWireMeasure", "sceneWireSave", sceneRefUnnamedHex)
}

func TestWireCatalogRefUnnamedUnframedRootDart(t *testing.T) {
	runWireUnframedDirDart(t, filepath.Join("..", "..", "..", "tables", "blobs"), "Catalog",
		"catalogWireLoad", "catalogWireMeasure", "catalogWireSave", catalogRefUnnamedHex)
}
