package darttable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3),
// on the Dart leg, run on the cross-port corpus
// testdata/conformance/feature-witnesses/compressed-f32-ends: every row of its
// cases.tsv runs through the generated Dart under the Dart VM with
// --enable-asserts, so the debug-only write checks are on. internal/tablewire's
// TestCompressedF32EndsCorpus is the reference runner; the row logic is its.
//
// The fixed root's JSON is read through the wire library's fFromJson and
// carried into the fixed library's F field by field, so the fixed save is the
// fixed writer's.
const dartF32EndsDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

const dartF32EndsDriver = `import 'dart:io';
import 'dart:typed_data';
import 'F32endsWire.dart' as w;
import 'F32endsFixed.dart' as p;
import 'ProbeFixed.dart' as f;

late String dir;
var reason = '';

Uint8List file(String name) => File('$dir/$name').readAsBytesSync();

String hex(List<int>? b) => b == null ? 'null' : b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();

String same(List<int> got, String want) {
  final wb = file(want);
  if (got.length == wb.length) {
    var eq = true;
    for (var i = 0; i < got.length; i++) {
      if (got[i] != wb[i]) { eq = false; break; }
    }
    if (eq) return '';
  }
  return 'saved ${hex(got)}, want $want ${hex(wb)}';
}

Uint8List? saveFixed(p.F v) {
  final out = Uint8List(f.fFixedMeasure(1));
  final n = f.fFixedSave([v], 1, out);
  return n < 0 ? null : Uint8List.sublistView(out, 0, n);
}

p.F carry(w.F v) {
  final out = p.F();
  out.r = v.r;
  for (var i = 0; i < out.xs.length && i < v.xs.length; i++) { out.xs[i] = v.xs[i]; }
  return out;
}

w.V? jsonV(String stem) {
  final r = w.TableTextReport();
  final v = w.vFromJson(String.fromCharCodes(file('$stem.json')), r);
  reason = r.refusal;
  return r.refused ? null : v;
}

p.F? jsonF(String stem) {
  final r = w.TableTextReport();
  final v = w.fFromJson(String.fromCharCodes(file('$stem.json')), r);
  reason = r.refusal;
  return r.refused ? null : carry(v);
}

w.V? readVariable(String stem) {
  final r = w.TableWireReport();
  final v = w.vWireLoad(file('$stem.variable.bin'), r);
  reason = r.refusal;
  return r.refused ? null : v;
}

p.F? readFixed(String stem) {
  final data = file('$stem.fixed.bin');
  final v = p.F();
  final r = p.TableFixedReport();
  final n = f.fFixedLoad([v], 1, data, data.length, f.fFixedNewPlan(), r);
  reason = r.refused == p.TableFixedRefusal.outOfRange ? 'out_of_range' : 'fixed refusal ${r.refused}';
  return n == 1 && r.refused == p.TableFixedRefusal.none ? v : null;
}

String read(String form, String root, String input, String expect) {
  List<int>? saved;
  bool got;
  if (root == 'V') {
    final v = form == 'json' ? jsonV(input) : readVariable(input);
    got = v != null;
    if (got && expect != 'refused') saved = w.vWireSave(v);
  } else {
    final v = form == 'json' ? jsonF(input) : readFixed(input);
    got = v != null;
    if (got && expect != 'refused') saved = saveFixed(v);
  }
  if (expect == 'refused') {
    if (got) return 'the $form read was not refused';
    return reason == 'out_of_range' ? '' : 'refused, but not out_of_range: $reason';
  }
  if (!got) return 'the $form read refused the value at the float32 end';
  if (saved == null) return 'saving again what was just read was refused';
  return same(saved, '$expect.${root == 'V' ? 'variable' : 'fixed'}.bin');
}

int index(String path) => int.parse(path.substring(path.indexOf('[') + 1, path.indexOf(']')));

String write(String form, String root, String input, String path, String bits, String expect) {
  final x = (ByteData(4)..setUint32(0, int.parse(bits, radix: 16))).getFloat32(0);
  List<int>? saved;
  try {
    if (form == 'variable') {
      final v = jsonV(input);
      if (v == null) return '$input.json is refused';
      if (path == 'r') { v.r = x; } else { v.xs[index(path)] = x; }
      saved = w.vWireSave(v);
    } else {
      final v = jsonF(input);
      if (v == null) return '$input.json is refused';
      if (path == 'r') { v.r = x; } else { v.xs[index(path)] = x; }
      saved = saveFixed(v);
    }
  } on AssertionError {
    saved = null;
  } on StateError {
    saved = null;
  }
  if (expect == 'refused') return saved == null ? '' : 'the $form save was not refused: ${hex(saved)}';
  if (saved == null) return 'the $form save was refused';
  return same(saved, '$expect.$form.bin');
}

void main(List<String> args) {
  dir = args[0];
  for (final line in File('$dir/cases.tsv').readAsLinesSync()) {
    if (line.isEmpty || line.startsWith('#')) continue;
    final c = line.split('\t');
    String why;
    try {
      why = c[1] == 'read' ? read(c[2], c[3], c[4], c[7]) : write(c[2], c[3], c[4], c[5], c[6], c[7]);
    } catch (e) {
      why = 'threw $e';
    }
    print('CHECK ${c[0]}${why.isEmpty ? ' ok' : ' FAIL $why'}');
  }
}
`

// TestDartCompressedF32EndsCorpus runs every row of the cross-port corpus
// under the Dart VM (gated).
func TestDartCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart (it runs the generated Dart)")
	dart := dartBinary(t)
	u := loadDirUnit(t, dartF32EndsDir)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	for _, name := range []string{"F32endsWire.dart", "F32endsFixed.dart", "ProbeFixed.dart"} {
		if files[name] == nil {
			t.Fatalf("the generator wrote no %s", name)
		}
	}
	corpus, err := filepath.Abs(dartF32EndsDir)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "f32ends_driver.dart"), []byte(dartF32EndsDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, dart, "--enable-asserts", "f32ends_driver.dart", corpus)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dart: %v\n%s", err, out)
	}
	text, err := os.ReadFile(filepath.Join(dartF32EndsDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "CHECK ")
		if !ok {
			continue
		}
		label, verdict, _ := strings.Cut(rest, " ")
		seen[label] = true
		if verdict != "ok" {
			t.Errorf("%s: %s", label, strings.TrimPrefix(verdict, "FAIL "))
		}
	}
	for line := range strings.SplitSeq(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if name, _, _ := strings.Cut(line, "\t"); !seen[name] {
			t.Errorf("the driver made no %s check\n%s", name, out)
		}
	}
}
