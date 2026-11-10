package darttable

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The 232-byte Scene reference #42 mutant (corpus seed graph_tree, pass
// reference #42, run seed 24845619678): a body in it carries bytes after its
// terminator inside its record length, which the strict Go oracle refuses
// as wire_damaged (internal/tablewire's body walk), and the Dart leg must
// refuse the same load with the same reason. The mutant bytes are embedded
// as hex, so the test reproduces without the fuzz output.
// sha256 0329ec90564c4ddf1cbb96a12f01d45fa38ac11109a0d087740a5298ef5ac195.
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
// Scene reference #42 mutant exactly as the Go oracle does: the whole load
// refused, with the strict reason wire_damaged on both sides.
func TestWireReportSceneReferenceMalformedDart(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	t.Parallel()
	dartBin := dartBinary(t)
	mutant, err := hex.DecodeString(sceneRef42MutantHex)
	if err != nil {
		t.Fatalf("decode mutant hex: %v", err)
	}

	c := graphdemoUnit(t)

	// THE ORACLE: DecodeRegion refuses the mutant whole — bytes after a
	// body's terminator inside its record length are wire_damaged, and no
	// counter and no value is defined beside the verdict.
	model := tabletext.NewModel(c)
	def := model.Lookup("Scene")
	if def == nil {
		t.Fatal("graphdemo declares no Scene root")
	}
	inst := model.New(def)
	var rep tabletext.Report
	if ok, derr := tablewire.DecodeRegion(model, inst, mutant, &rep); ok || !tablewire.ReadRefusal(derr) {
		t.Fatalf("oracle decode: ok=%v err=%v, want a refusal", ok, derr)
	}
	if !rep.Refused {
		t.Fatalf("oracle does not flag the mutant refused: %+v", rep)
	}
	if !strings.HasPrefix(rep.Reason, "wire_damaged") {
		t.Fatalf("oracle refuses the mutant as %q, want wire_damaged", rep.Reason)
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
  t.sceneWireLoad(data, report);
  if (!report.refused) {
    stderr.writeln('FAIL: Dart read accepted, oracle refused');
    exitCode = 1;
  }
  if (report.reason != 'wire_damaged') {
    stderr.writeln('FAIL: Dart refused as ${report.reason}, oracle wire_damaged');
    exitCode = 1;
  }
  stdout.writeln('unknown=${report.unknown} '
      'widened=${report.widened} '
      'refused=${report.refused} reason=${report.reason}');
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
	if !strings.Contains(string(out), "refused=true reason=wire_damaged") {
		t.Fatalf("the Dart leg does not refuse the mutant as the oracle does: %s", out)
	}
}
