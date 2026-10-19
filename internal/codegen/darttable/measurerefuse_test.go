package darttable

// THE WIRE MEASURE NEVER REFUSES A WIRE THE ORACLE READS CLEANLY
// (docs/SPEC-TABLES.md §6.2, internal/tablewire/extent.go). The Dart
// <name>WireMeasure and the Go reference FileRegionMeasure must agree byte for
// byte: a refusal in Go (-1, refused) stays -1 in Dart, and a clean Go read is
// never -1 in Dart. The wire fuzzer's cause D3 (dart-wire-fuzzer-gate-on-
// every-root) was emitMeasure refusing a wire whose node table the reference
// measures: the Dart emitter stopped at the FIRST node-table field and refused
// on its malformed content, while the reference's nodeTableBytes scans past
// every earlier field to the LAST one and measures that.

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wireMeasureRoot is one (unit, root) and one mutant of this cause: the wire
// fuzzer's pass that Dart refused and Go measured cleanly.
type wireMeasureRoot struct {
	unitDir string
	root    string
	mutant  string
}

func TestWireMeasureNoFalseRefusalDart(t *testing.T) {
	t.Parallel()
	dart := dartBinary(t)

	tests := []wireMeasureRoot{
		{
			unitDir: filepath.Join("..", "..", "..", "tables", "blobs"),
			root:    "Catalog",
			// blobdemo.Catalog, corpus seed blob_str16, random pass #10538,
			// run seed 24845619678. The first node-table field carries a type
			// reference above the entry count; the reference scans past it to
			// the second field and measures it cleanly.
			mutant: "01010c067465726d3136021102031103040c1a0270106162636465666768696a6bd36d6e6f700605a0a1a2a3a4040c1a0205106162636465666768696a6b6c6d6e6f700605a0a1a2a3a400861b638ebaadbcc4dd7c58d1bafbf83bb746a665bb209250ffffffffffffffff58fcaffad8e04b70e44f1c4f47c02e2f0600000000000000",
		},
		{
			unitDir: filepath.Join("..", "..", "..", "tables", "pointers"),
			root:    "Scene",
			// graphdemo.Scene, corpus seed graph_tree, pass reference #2, run
			// seed 24845619678. The root body's first reference is above the
			// trailer's entry count; the reference stops its node-table scan
			// there and measures the root alone, where the old Dart measure
			// refused at open().
			mutant: "01100c0474726565020402000000031102041104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.root, func(t *testing.T) {
			t.Parallel()
			u := wireMeasureUnit(t, tc.unitDir)
			data, err := hex.DecodeString(tc.mutant)
			if err != nil {
				t.Fatalf("mutant hex: %v", err)
			}

			got, whole, refused := tablewire.FileRegionMeasure(u, u.Tables[tc.root], data)
			if refused {
				t.Fatalf("oracle refuses the mutant; this card's mutant reads cleanly")
			}
			if !whole {
				t.Fatalf("oracle measure not exact; cannot pin a byte count")
			}

			files, err := generateWireFiles(u)
			if err != nil {
				t.Fatalf("generateWireFiles: %v", err)
			}
			home := runtimeHome(u)
			src, ok := files[home+"Wire.dart"]
			if !ok {
				t.Fatalf("no generated wire library %sWire.dart", home)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, home+"Wire.dart"), src, 0o600); err != nil {
				t.Fatal(err)
			}
			probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import '%sWire.dart';

void main(List<String> args) {
  final f = File(args[0]);
  final b = f.readAsBytesSync();
  print(%sWireMeasure(Uint8List.fromList(b)));
}
`, home, strings.ToLower(tc.root))
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
				t.Fatalf("dart measure probe: %v\n%s", err, out)
			}
			line := strings.TrimSpace(string(out))
			if line == "" {
				t.Fatalf("dart measure probe printed nothing: %s", out)
			}
			var dartMeasure int64
			if _, err := fmt.Sscanf(line, "%d", &dartMeasure); err != nil {
				t.Fatalf("dart measure probe output not an int: %q", line)
			}
			if dartMeasure != got {
				t.Fatalf("Dart WireMeasure=%d, Go FileRegionMeasure=%d: a clean Go read must not be refused and must measure the same bytes", dartMeasure, got)
			}
		})
	}
}

func wireMeasureUnit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var srcs []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ast, perrs := parser.Parse(e.Name(), src)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", e.Name(), perrs[0])
		}
		srcs = append(srcs, check.SourceFile{
			Path: path, Name: e.Name(), Base: strings.TrimSuffix(e.Name(), ".schema"), Bytes: src, AST: ast,
		})
	}
	u, cerrs := check.Unit(srcs)
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", dir, cerrs[0])
	}
	return u
}
