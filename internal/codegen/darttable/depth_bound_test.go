package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestDartWireDepthBound128 holds docs/SPEC-TABLES.md §4, ONE NESTING BOUND,
// on the Dart leg: the root body is at depth 0, each structural payload inside
// it is one level deeper (a table body, an array, a keyed or map body, and a
// union whose arm holds one of those or another union), and depth 128 passes
// while 129 refuses in every walk of the binary forms, unknown fields included
// (§6.5). Each held depth_* framing vector runs through graphdemo's Scene
// strict load and its retaining load; the verdict alone is compared, never a
// refusal's name, and no load may throw. A vector that reads must also keep
// its unknown field through the retaining load and save it back unchanged.
func TestDartWireDepthBound128(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadDirUnit(t, filepath.Join(root, "tables", "pointers"))
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
	if !strings.Contains(string(set["GraphdemoWire.dart"]), "sceneWireLoadRetain(") {
		t.Fatal("GraphdemoWire.dart carries no sceneWireLoadRetain: the retaining entry point moved")
	}
	dir := t.TempDir()
	writeFiles(t, dir, set)
	if err := os.WriteFile(filepath.Join(dir, "depth_probe.dart"), []byte(depthBoundProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, dartBinary(t), "--enable-asserts", "depth_probe.dart", filepath.Join(root, "testdata", "wire", "tables", "framing"))
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the depth probe failed: %v\n%s", err, out)
	}
}

const depthBoundProbe = `import 'dart:io';
import 'dart:typed_data';
import 'GraphdemoWire.dart' as g;

var failed = false;
void check(bool ok, String what) {
  if (!ok) {
    stdout.writeln('FAIL: $what');
    failed = true;
  }
}

void main(List<String> args) {
  Uint8List wire(String name) => File('${args[0]}/$name.bin').readAsBytesSync();
  for (final (row, read) in [
    ('depth_tables_128', true),
    ('depth_arrays_128', true),
    ('depth_union_tables_64', true),
    ('depth_keyed_128', true),
    ('depth_tables_129', false),
    ('depth_arrays_129', false),
    ('depth_union_tables_65', false),
    ('depth_keyed_129', false),
    ('depth_tables_5000', false),
  ]) {
    final want = read ? 'read' : 'refused';
    try {
      final r = g.TableWireReport();
      g.sceneWireLoad(wire(row), r);
      check(r.refused != read, '$row: the strict load did not answer $want');
    } catch (e) {
      check(false, '$row: the strict load threw ${e.runtimeType}');
    }
    try {
      final r = g.TableWireReport();
      final retain = g.TableRetain(1 << 20, List<int>.filled(1 << 12, 0));
      final v = g.sceneWireLoadRetain(wire(row), retain, r);
      check(r.refused != read, '$row: the retaining load did not answer $want');
      if (read && !r.refused) {
        // The retaining walk holds the same bound: a sound wire keeps its
        // unknown field whole and saves it back out unchanged (§6.6).
        check(r.retainLost == 0, '$row: the retaining load lost ${r.retainLost} records');
        final s = g.TableWireReport();
        final saved = g.sceneWireSaveRetain(v, retain, s);
        check(!s.refused && s.retainLost == 0, '$row: the retaining save refused or lost a record');
        final data = wire(row);
        var same = saved.length == data.length;
        for (var i = 0; same && i < data.length; i++) {
          same = saved[i] == data[i];
        }
        check(same, '$row: the retaining save is ${saved.length} bytes and the file is ${data.length}, and they differ');
      }
    } catch (e) {
      check(false, '$row: the retaining load threw ${e.runtimeType}');
    }
  }
  if (failed) exit(1);
  stdout.writeln('ok: every held depth vector answered its verdict through both loads');
}
`
