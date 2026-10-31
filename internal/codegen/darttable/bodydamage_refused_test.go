package darttable

// ROOT-BODY FRAMING DAMAGE IS REFUSED BY THE DART LEG (docs/SPEC-TABLES.md
// §3.3 and §4): DAMAGE DISCARDS THE WHOLE LOAD, so a body whose framing walk
// cannot reach a terminator, including at a reference whose ten-byte encoding
// sets bit 63 (which a Dart int holds as negative), is a refusal. Wire fuzzer
// cause D7.
//
// Each mutant is measured and loaded by the generated Dart, and the answer is
// compared with the oracle on what the fuzz harness judges on damaged input:
// the measure, and that the load says malformed. No counter and no saved byte
// is compared.

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type bodyDamageCase struct {
	name    string
	unitDir string
	root    string
	mutant  string
}

func TestWireLoadRefusesPastBodyDamageDart(t *testing.T) {
	t.Parallel()
	dart := dartBinary(t)

	cases := []bodyDamageCase{
		{
			name:    "SceneRefOverflow",
			unitDir: filepath.Join("..", "..", "..", "tables", "pointers"),
			root:    "Scene",
			// graphdemo.Scene, corpus seed graph_tree, run seed 24845619678,
			// the card's representative (leg measure=192 saved=111, oracle
			// encoded=39); pass reference #27 in this card's scratch fuzz,
			// mutant sha256
			// 4a3abb36bdb394168c4c0a241ead110a97bc4870d63c859fa6c4e5b0b4f4a7e4.
			// The fourth field's reference is the ten-byte maximum, 1<<63,
			// which a Dart int reads as negative: the node-table scan stepped
			// over it and found the node table past the damage, so head, tree
			// and the rest were kept where the oracle keeps name and version.
			mutant: "01010c0474726565020402000000031102ffffffffffffffffff011104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000",
		},
		{
			name:    "CatalogRefOverflow",
			unitDir: filepath.Join("..", "..", "..", "tables", "blobs"),
			root:    "Catalog",
			// blobdemo.Catalog, corpus seed blob_small, run seed 24845619678,
			// the card's representative (leg measure=88 saved=149, oracle 24);
			// pass reference #20 in this card's scratch fuzz, mutant sha256
			// fb51536c88fd7a663c789e89411ab1efa6db05796ef4e0e8332b9bf558003d0a.
			// The third field's reference is 1<<63: the oracle keeps name and
			// stops, and head resolves against no node table.
			mutant: "01010c03617274021102ffffffffffffffffff011106041107050c43060618010c05627269636b0708070000000811030911040a1105000b030102030c036361700608010c047461696c000b05deadbeef7f0c0b68656c6c6f20626c6f627300861b638ebaadbcc4030c9a5fcc128f0a03b2f40f72193b61dd7c58d1bafbf83bfffffffffffffffffb645ffecc6d4e8e43326721d7969cef054aa33067555b85a5024c86eb28aa5d28f025a0ba6c31e5e44f1c4f47c02e2f58fcaffad8e04b700c00000000000000",
		},
		{
			name:    "SceneRefUnnamed",
			unitDir: filepath.Join("..", "..", "..", "tables", "pointers"),
			root:    "Scene",
			// graphdemo.Scene, corpus seed graph_tree, pass reference #23, run
			// seed 24845619678 (the open() half: Dart saved 10 bytes, an
			// empty value, where the oracle saves 39), mutant sha256
			// 61b0494107dcea1186c4f93271a07a3b960d0fb86bd522e17da8af633af4e98b.
			// The fourth root-body field's reference (16) is above the
			// trailer's entry count (15): the oracle keeps name and version,
			// reads head's index against an empty node table, and stops.
			mutant: "01010c0474726565020402000000031102101104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000",
		},
		{
			name:    "CatalogRefUnnamed",
			unitDir: filepath.Join("..", "..", "..", "tables", "blobs"),
			root:    "Catalog",
			// blobdemo.Catalog, corpus seed blob_small, pass reference #16,
			// run seed 24845619678 (the open() half: Dart saved 10 bytes
			// where the oracle saves 24), mutant sha256
			// 975623f8b40aa2ec533839f89385acfaa232c4d19e60e89fcf571f67f239f98d.
			// The third root-body field's reference (13) is above the entry
			// count (12): the oracle keeps name and head's index, and stops.
			mutant: "01010c036172740211020d1106041107050c43060618010c05627269636b0708070000000811030911040a1105000b030102030c036361700608010c047461696c000b05deadbeef7f0c0b68656c6c6f20626c6f627300861b638ebaadbcc4030c9a5fcc128f0a03b2f40f72193b61dd7c58d1bafbf83bfffffffffffffffffb645ffecc6d4e8e43326721d7969cef054aa33067555b85a5024c86eb28aa5d28f025a0ba6c31e5e44f1c4f47c02e2f58fcaffad8e04b700c00000000000000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := loadDirUnit(t, tc.unitDir)
			def := u.Tables[tc.root]
			if def == nil {
				t.Fatalf("no table %s in %s", tc.root, tc.unitDir)
			}
			data, err := hex.DecodeString(tc.mutant)
			if err != nil {
				t.Fatalf("mutant hex: %v", err)
			}

			// THE ORACLE, exactly as the wire fuzz harness asks it
			// (test/conformance/harness/wirefuzz.go, wireRoot.oracle): measure,
			// region decode, encode.
			wantMeasure, whole, measureRefused := tablewire.FileRegionMeasure(u, def, data)
			if measureRefused || !whole {
				t.Fatalf("oracle measure refused=%t whole=%t; this cause's mutants measure exactly", measureRefused, whole)
			}
			model := tabletext.NewModel(u)
			var rep tabletext.Report
			if _, derr := tablewire.DecodeRegion(model, model.New(def), data, &rep); derr != nil || !rep.Malformed {
				t.Fatalf("oracle does not read the mutant as damaged: %v %+v", derr, rep)
			}

			files, err := generateWireFiles(u)
			if err != nil {
				t.Fatalf("generateWireFiles: %v", err)
			}
			home := runtimeHome(u)
			src, found := files[home+"Wire.dart"]
			if !found {
				t.Fatalf("no generated wire library %sWire.dart", home)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, home+"Wire.dart"), src, 0o600); err != nil {
				t.Fatal(err)
			}
			lower := strings.ToLower(tc.root[:1]) + tc.root[1:]
			probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import '%[1]sWire.dart';

String _hex(List<int> b) => b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();

void main(List<String> args) {
  final wire = Uint8List.fromList(File(args[0]).readAsBytesSync());
  print(%[2]sWireMeasure(wire));
  final r = TableWireReport();
  final v = %[2]sWireLoad(wire, r);
  print('${r.unknown},${r.kindMismatch},${r.widened},${r.clamped},0,${r.malformed},${r.refused ? 'refused' : 'read'}');
  print(_hex(%[2]sWireSave(v)));
}
`, home, lower)
			if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
				t.Fatal(err)
			}
			mutantPath := filepath.Join(dir, "mutant.bin")
			if err := os.WriteFile(mutantPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(dart, "--enable-asserts", "probe.dart", mutantPath)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("dart probe: %v\n%s", err, out)
			}
			lines := strings.Fields(strings.TrimSpace(string(out)))
			if len(lines) != 3 {
				t.Fatalf("dart probe printed %d lines, want measure, report and saved: %s", len(lines), out)
			}
			var gotMeasure int64
			if _, err := fmt.Sscanf(lines[0], "%d", &gotMeasure); err != nil {
				t.Fatalf("dart measure not an int: %q", lines[0])
			}
			if gotMeasure != wantMeasure {
				t.Errorf("measure: Dart %d, oracle %d", gotMeasure, wantMeasure)
			}
			if fields := strings.Split(lines[1], ","); len(fields) != 7 || fields[5] != "true" {
				t.Errorf("the Dart load did not refuse a damaged body: report %s", lines[1])
			}
		})
	}
}
