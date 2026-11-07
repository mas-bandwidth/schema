package darttable

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The 232-byte Scene reference #42 mutant (corpus seed graph_tree, pass
// reference #42, run seed 24845619678): Dart read it malformed where the Go
// oracle reads it clean — a reference check stricter than the oracle's. The
// mutant bytes are embedded as hex, so the test reproduces without the fuzz
// output. sha256 0329ec90564c4ddf1cbb96a12f01d45fa38ac11109a0d087740a5298ef5ac195.
const sceneRef42MutantHex = "01010c0474726565020402000000031102041104051107060c4d06070e000401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000"

// graphdemoUnit loads the graphdemo unit the same way the conformance harness
// does (tables/pointers), so the oracle and the generated Dart see the same
// schema.
func graphdemoUnit(t *testing.T) *ir.Unit {
	t.Helper()
	srcs, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "pointers", "*.schema"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(srcs) == 0 {
		t.Fatal("no graphdemo schemas")
	}
	files := make([]check.SourceFile, 0, len(srcs))
	for _, p := range srcs {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		base := strings.TrimSuffix(filepath.Base(p), ".schema")
		ast, perrs := parser.Parse(base+".schema", b)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", p, perrs[0])
		}
		files = append(files, check.SourceFile{Path: p, Name: base + ".schema", Base: base, Bytes: b, AST: ast})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check graphdemo: %v", cerrs[0])
	}
	return u
}

// TestWireReportSceneReferenceMalformedDart pins that the Dart leg reads the
// Scene reference #42 mutant exactly as the Go oracle does: malformed false,
// and the re-save byte-for-byte the oracle's 194 bytes.
func TestWireReportSceneReferenceMalformedDart(t *testing.T) {
	t.Parallel()
	dartBin := dartBinary(t)
	mutant, err := hex.DecodeString(sceneRef42MutantHex)
	if err != nil {
		t.Fatalf("decode mutant hex: %v", err)
	}

	c := graphdemoUnit(t)

	// THE ORACLE: DecodeRegion then Encode. The oracle reads the mutant clean
	// (malformed false) and re-encodes 194 bytes.
	model := tabletext.NewModel(c)
	def := model.Lookup("Scene")
	if def == nil {
		t.Fatal("graphdemo declares no Scene root")
	}
	inst := model.New(def)
	var rep tabletext.Report
	if ok, derr := tablewire.DecodeRegion(model, inst, mutant, &rep); derr != nil || !ok {
		t.Fatalf("oracle decode: ok=%v err=%v", ok, derr)
	}
	if rep.Malformed {
		t.Fatalf("oracle flags the mutant malformed: %+v", rep)
	}
	encoded, eerr := tablewire.Encode(model, inst)
	if eerr != nil {
		t.Fatalf("oracle encode: %v", eerr)
	}

	// Generate the Dart wire library and drive it with the leg's own toolchain.
	files, err := Generate(c)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var wireLib string
	for name := range files {
		if strings.HasSuffix(name, "Wire.dart") {
			wireLib = name
			break
		}
	}
	if wireLib == "" {
		t.Fatal("no Wire.dart generated")
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "mutant.bin"), mutant, 0o600); err != nil {
		t.Fatal(err)
	}

	probe := `import 'dart:io';
import './` + wireLib + `' as t;

void main() {
  final data = File('mutant.bin').readAsBytesSync();
  final report = t.TableWireReport();
  final v = t.sceneWireLoad(data, report);
  if (report.malformed) {
    stderr.writeln('FAIL: Dart read malformed, oracle clean');
    exitCode = 1;
  }
  final out = t.sceneWireSave(v);
  File('saved.bin').writeAsBytesSync(out);
  stdout.writeln('unknown=${report.unknown} kind=${report.kindMismatch} '
      'widened=${report.widened} clamped=${report.clamped} '
      'malformed=${report.malformed} refused=${report.refused}');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBin, "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dart probe failed: %v\n%s", err, out)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "saved.bin"))
	if err != nil {
		t.Fatalf("read saved: %v (%s)", err, out)
	}
	if !bytes.Equal(saved, encoded) {
		t.Fatalf("the Dart save diverges from the oracle: Dart %d bytes, oracle %d bytes, first difference at byte %d\n%s",
			len(saved), len(encoded), bytesFirstDiff(saved, encoded), out)
	}
}

func bytesFirstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}
