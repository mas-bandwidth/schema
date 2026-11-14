package darttable

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestFunctionalDartLongPointerChain(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	started := time.Now()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadDirUnit(t, filepath.Join(root, "tables", "pointers"))
	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")
	oracles := map[string][]byte{}
	raw, err := os.ReadFile(filepath.Join(framing, "node_chain_200000.bin"))
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Scene"))
	var report tabletext.Report
	ok, err := tablewire.Decode(m, inst, raw, &report)
	if err != nil || !ok {
		t.Fatalf("Go oracle refused node_chain_200000: ok=%t err=%v report=%+v", ok, err, report)
	}
	canonical, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("Go oracle could not save node_chain_200000: %v", err)
	}
	oracles["node_chain_200000"] = canonical
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string][]byte{}
	for name, src := range packetFiles(t, u) {
		if !strings.Contains(name, "/") {
			set[name] = src
		}
	}
	for name, src := range files {
		if !strings.Contains(name, "/") {
			set[name] = src
		}
	}
	dir := t.TempDir()
	writeFiles(t, dir, set)
	if err := os.WriteFile(filepath.Join(dir, "long_chain_probe.dart"), []byte(longChainProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, data := range oracles {
		if err := os.WriteFile(filepath.Join(dir, name+"_canonical.bin"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	remaining := 120*time.Second - time.Since(started)
	if remaining <= 0 {
		t.Fatalf("preparing the Go-oracle wires exceeded 120 s")
	}
	ctx, cancel := context.WithTimeout(context.Background(), remaining)
	defer cancel()
	cmd := exec.CommandContext(ctx, dartBinary(t), "--enable-asserts", "long_chain_probe.dart", framing, dir)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("long pointer chain exceeded 120 s: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("long pointer chain probe failed: %v\n%s", err, out)
	}
}

const longChainProbe = `import 'dart:io';
import 'dart:typed_data';
import 'GraphdemoWire.dart' as g;

void check(bool ok, String what) {
  if (!ok) throw StateError(what);
}

Uint8List wire(String dir, String name) => File('$dir/$name.bin').readAsBytesSync();

bool sameBytes(List<int> a, List<int> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

void exercise(String framing, String oracles) {
  final control = wire(framing, 'node_chain_two');
  final controlReport = g.TableWireReport();
  final controlValue = g.listNodeWireLoad(control, controlReport);
  check(!controlReport.refused, 'node_chain_two: strict load refused');
  g.listNodeWireSave(controlValue);

  final bytes = wire(framing, 'node_chain_200000');
  final canonical = wire(oracles, 'node_chain_200000_canonical');
  stdout.writeln('loading node_chain_200000 (${bytes.length} bytes)');
  final report = g.TableWireReport();
  final value = g.sceneWireLoad(bytes, report);
  check(!report.refused, 'node_chain_200000: strict load refused: ${report.reason}');
  stdout.writeln('read unknown=${report.unknown}, widened=${report.widened}, missing=${report.missingFields}');
  stdout.writeln('loaded node_chain_200000');
  final saved = g.sceneWireSave(value);
  check(sameBytes(saved, canonical), 'node_chain_200000: save differs from Go-oracle canonical bytes');
  stdout.writeln('saved node_chain_200000 to Go-oracle canonical bytes');
  final canonicalReport = g.TableWireReport();
  final canonicalValue = g.sceneWireLoad(canonical, canonicalReport);
  check(!canonicalReport.refused, 'canonical node_chain_200000: strict load refused');
  check(sameBytes(g.sceneWireSave(canonicalValue), canonical), 'canonical node_chain_200000: save/load/save changed bytes');
  try {
    g.sceneToJson(value);
    throw StateError('node_chain_200000: JSON writer accepted a value past its depth cap');
  } on StateError catch (e) {
    if (!e.message.toString().contains('past the depth cap')) rethrow;
  }
  stdout.writeln('JSON writer refused at its depth cap');
}

void main(List<String> args) {
  exercise(args[0], args[1]);
  stdout.writeln('node_chain_200000 value released at end of scope');
}
`
