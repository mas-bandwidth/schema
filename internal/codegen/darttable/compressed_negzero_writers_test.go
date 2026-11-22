package darttable

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// A COMPRESSED FLOAT'S -0 IS WRITTEN AS +0 BY EVERY DART WRITER
// (docs/SPEC-TABLES.md §3.3, the special values; docs/rulings.json
// fixed-compressed-negzero). The variable save and its retaining twin, the
// text writer, the cook, the fixed save and the message form's elision stored
// a compressed -0 as held. The plain float `p` is the control: its -0 is a
// value and rides bit-exact. The Go reference engine's corpus,
// testdata/conformance/feature-witnesses/compressed-negzero-go, is the
// worked example; its 15 rows run here too.
const dartNegZeroCorpus = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// TestDartCompressedFloatStoresAreGuarded is the fast half: it reads the
// generated source rather than running it. Every store of a float (the wire
// payload, the JSON text, the fixed image, the cook and the message form's
// elision cell) takes the +0 test for a compressed float and not for the
// plain one.
func TestDartCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile(filepath.Join(dartNegZeroCorpus, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(schema)))
	if err != nil {
		t.Fatal(err)
	}
	store := regexp.MustCompile(`_f32Write\(|_jsonFloat\(|\.float32Bits\(|_float32Cell\[0\] = \(?(v|value)\.`)
	define := regexp.MustCompile(`(int|String) (_f32Write|_jsonFloat|float32Bits)\(double`)
	plainRef := regexp.MustCompile(`\b(v|value)\.p\b`)
	guarded, plain := map[string]int{}, map[string]int{}
	for name, data := range files {
		for i, line := range strings.Split(string(data), "\n") {
			m := store.FindString(line)
			if m == "" || define.MatchString(line) {
				continue
			}
			isPlain := plainRef.MatchString(line)
			isGuarded := strings.Contains(line, " == 0 ? 0.0 : ")
			switch {
			case isPlain && isGuarded:
				t.Errorf("%s:%d: the plain float's -0 is normalized: %s", name, i+1, strings.TrimSpace(line))
			case isPlain:
				plain[m]++
			case !isGuarded:
				t.Errorf("%s:%d: a compressed float is stored as held: %s", name, i+1, strings.TrimSpace(line))
			default:
				guarded[m]++
			}
		}
	}
	for _, m := range []string{"_f32Write(", "_jsonFloat(", ".float32Bits(", "_float32Cell[0] = (value."} {
		if guarded[m] == 0 {
			t.Errorf("no guarded compressed store through %q", m)
		}
	}
	if len(plain) == 0 {
		t.Errorf("no plain float store found: the control is gone")
	}
}

// TestDartCompressedNegZeroWriters runs the corpus's 15 rows and every
// writer's -0/+0 twin under the Dart VM.
func TestDartCompressedNegZeroWriters(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart (it runs the generated library)")
	dart := dartBinary(t)
	schema, err := os.ReadFile(filepath.Join(dartNegZeroCorpus, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := filepath.Abs(dartNegZeroCorpus)
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	files["probe.dart"] = []byte(dartNegZeroProbe)
	dir := t.TempDir()
	writeFiles(t, dir, files)
	cmd := testproc.Command(t, dart, "--enable-asserts", "probe.dart", corpus)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "\nPASS rows=15 twins=84\n") {
		t.Fatalf("dart probe: %v\n%s", err, out)
	}
}

const dartNegZeroProbe = `
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'NzgoWire.dart' as w;
import 'NzgoFixed.dart' as f;
import 'ProbeFixed.dart' as pf;
import 'ProbeCook.dart' as c;

int bad = 0;
void fail(String s) {
  print('FAIL $s');
  bad++;
}

String hex(List<int> b) => b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();

Uint8List? cookOf(int Function() measure, bool Function(Uint8List) write) {
  final n = measure();
  if (n < 0) return null;
  final out = Uint8List(n);
  return write(out) ? out : null;
}

Uint8List? fixedOf(int Function(int) measure, int Function(Uint8List) save) {
  final out = Uint8List(measure(1));
  return save(out) == out.length ? out : null;
}

Uint8List? messageOf(f.Fix v) {
  final r = f.TableReport();
  final n = pf.fixMeasureMessages([v], r);
  if (n < 0) return null;
  final out = Uint8List(n);
  return pf.fixSaveMessages([v], out, r) == n ? out : null;
}

int slotOf(String index) => index == 'Low' ? 0 : index == 'High' ? 1 : int.parse(index);

// THE CORPUS: load <base>, set <path> to -0 in the native value, save <form>
// and owe <expect>.<form>.bin. A fixed row loads <base>.fixed.bin, which the
// Go reference engine pins as its encoding of <base>.json.
void setWire(dynamic v, String path) {
  final dot = path.indexOf('.');
  if (dot >= 0) {
    if (v.pick.armTag != 1) throw StateError('$path: the base does not set arm v');
    v.pick.v = -0.0;
    return;
  }
  final br = path.indexOf('[');
  if (br < 0) {
    switch (path) {
      case 'a': v.a = -0.0; return;
      case 'z': v.z = -0.0; return;
      case 'p': v.p = -0.0; return;
    }
    throw StateError('$path: no such leaf');
  }
  final name = path.substring(0, br);
  final slot = slotOf(path.substring(br + 1, path.length - 1));
  final List<double> list = name == 'xs' ? v.xs : name == 'ys' ? v.ys : v.ks;
  if (slot >= list.length) throw StateError('$path: slot $slot of ${list.length}');
  list[slot] = -0.0;
}

void setFixed(f.Fix v, String path) {
  final dot = path.indexOf('.');
  if (dot >= 0) {
    if (v.pick.type != 1) throw StateError('$path: the base does not set arm v');
    v.pick.v = -0.0;
    return;
  }
  final br = path.indexOf('[');
  if (br < 0) {
    switch (path) {
      case 'a': v.a = -0.0; return;
      case 'z': v.z = -0.0; return;
      case 'p': v.p = -0.0; return;
    }
    throw StateError('$path: no such leaf');
  }
  final name = path.substring(0, br);
  final slot = slotOf(path.substring(br + 1, path.length - 1));
  final Float32List list = name == 'xs' ? v.xs : name == 'ys' ? v.ys : v.ks;
  list[slot] = -0.0;
}

typedef Twin = (String, bool, void Function(dynamic, double), void Function(dynamic, double));

void main(List<String> args) {
  final dir = args[0];
  var rows = 0;
  for (final line in File('$dir/cases.tsv').readAsLinesSync()) {
    if (line.isEmpty || line.startsWith('#')) continue;
    final col = line.split('\t');
    final name = col[0], form = col[1], root = col[2], base = col[3], path = col[4], expect = col[5];
    final want = File('$dir/$expect.$form.bin').readAsBytesSync();
    Uint8List? got;
    try {
      if (form == 'variable') {
        final text = File('$dir/$base.json').readAsStringSync();
        final r = w.TableTextReport();
        final dynamic v = root == 'Var' ? w.varFromJson(text, r) : w.fixFromJson(text, r);
        if (r.refused) {
          fail('$name: $base.json does not read: ${r.reason}');
          continue;
        }
        setWire(v, path);
        got = root == 'Var' ? w.varWireSave(v as w.Var) : w.fixWireSave(v as w.Fix);
      } else {
        final data = File('$dir/$base.fixed.bin').readAsBytesSync();
        final back = List<f.Fix>.generate(8, (_) => f.Fix());
        final r = f.TableFixedReport();
        if (pf.fixFixedLoad(back, back.length, data, data.length, pf.fixFixedNewPlan(), r) != 1) {
          fail('$name: $base.fixed.bin does not load');
          continue;
        }
        setFixed(back[0], path);
        got = fixedOf(pf.fixFixedMeasure, (out) => pf.fixFixedSave([back[0]], 1, out));
      }
    } catch (e) {
      fail('$name: $e');
      continue;
    }
    rows++;
    if (got == null) {
      fail('$name: the $form save refused');
    } else if (hex(got) != hex(want)) {
      fail('$name: the $form save of -0 at $path is not $expect.$form.bin:\n have ${hex(got)}\n want ${hex(want)}');
    }
  }

  // THE TWINS: every writer saves a value holding -0 in one cell and owes the
  // bytes its +0 twin writes; the plain float p owes different bytes.
  final List<Twin> cases = [
    ('leaf_nonzero_default', false, (v, z) => v.a = z, (v, z) => v.a = z),
    ('leaf_zero_default', false, (v, z) => v.z = z, (v, z) => v.z = z),
    ('array_element', false, (v, z) { v.xs[0] = z; v.xs[1] = 0.05; }, (v, z) { v.xs[0] = z; v.xs[1] = 0.05; }),
    ('counted_element', false, (v, z) { v.ys = <double>[0.05, z]; }, (v, z) { v.ys[0] = 0.05; v.ys[1] = z; v.ysCount = 2; }),
    ('keyed_slot', false, (v, z) { v.ks[0] = z; v.ks[1] = 0.05; }, (v, z) { v.ks[0] = z; v.ks[1] = 0.05; }),
    ('union_arm', false, (v, z) { v.pick.armTag = 1; v.pick.v = z; }, (v, z) { v.pick.type = 1; v.pick.v = z; }),
    ('plain_control', true, (v, z) => v.p = z, (v, z) => v.p = z),
  ];
  final writers = <(String, String, bool, dynamic Function(), Uint8List? Function(dynamic))>[
    ('Var', 'save', false, () => w.Var(), (v) => w.varWireSave(v)),
    ('Var', 'save_retain', false, () => w.Var(), (v) => w.varWireSaveRetain(v, w.TableRetain(0, <int>[]), w.TableWireReport())),
    ('Var', 'json', false, () => w.Var(), (v) => utf8.encode(w.varToJson(v))),
    ('Var', 'cook_le', false, () => w.Var(), (v) => cookOf(() => c.VarCook.cookMeasure(v), (out) => c.VarCook.cook(v, out, Endian.little))),
    ('Var', 'cook_be', false, () => w.Var(), (v) => cookOf(() => c.VarCook.cookMeasure(v), (out) => c.VarCook.cook(v, out, Endian.big))),
    ('Var', 'fixed', true, () => f.Var(), (v) => fixedOf(pf.varFixedMeasure, (out) => pf.varFixedSave([v], 1, out))),
    ('Fix', 'save', false, () => w.Fix(), (v) => w.fixWireSave(v)),
    ('Fix', 'json', false, () => w.Fix(), (v) => utf8.encode(w.fixToJson(v))),
    ('Fix', 'cook_le', false, () => w.Fix(), (v) => cookOf(() => c.FixCookWire.cookMeasure(v), (out) => c.FixCookWire.cook(v, out, Endian.little))),
    ('Fix', 'cook_be', false, () => w.Fix(), (v) => cookOf(() => c.FixCookWire.cookMeasure(v), (out) => c.FixCookWire.cook(v, out, Endian.big))),
    ('Fix', 'fixed', true, () => f.Fix(), (v) => fixedOf(pf.fixFixedMeasure, (out) => pf.fixFixedSave([v], 1, out))),
    ('Fix', 'message', true, () => f.Fix(), (v) => messageOf(v)),
  ];
  var twins = 0;
  for (final (root, writer, fixedForm, make, write) in writers) {
    for (final (name, plain, setV, setF) in cases) {
      final set = fixedForm ? setF : setV;
      final plus = make(), minus = make();
      set(plus, 0.0);
      set(minus, -0.0);
      final tag = '$root/$writer/$name';
      Uint8List? p, m;
      try {
        p = write(plus);
        m = write(minus);
      } catch (e) {
        fail('$tag: $e');
        continue;
      }
      twins++;
      if (p == null || m == null) {
        fail('$tag: refused');
        continue;
      }
      final same = hex(p) == hex(m);
      if (plain && same) fail("$tag: the plain float's -0 did not ride");
      if (!plain && !same) fail("$tag: a compressed float's -0 was not written as +0:\n -0 ${hex(m)}\n +0 ${hex(p)}");
    }
  }
  print(bad == 0 ? '\nPASS rows=$rows twins=$twins' : '\nBAD $bad');
}
`
