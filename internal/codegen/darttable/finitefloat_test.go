package darttable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE FINITE RULE ON THE DART LEG (docs/SPEC-TABLES.md §3.3; Glenn,
// 2026-10-27: "Tables go finite-only too, same rule as packets. NaN/inf
// refused on write and read, -0 and subnormals kept"). One probe holds every
// form this leg emits to it, one named vector a line:
//
//   - READ: the variable form (a scalar of each width, a list element, a [N]
//     element, a union arm and a RETAINED unknown float, ruling 3), the
//     message form (both widths), the fixed form (both widths and a Float32List
//     element) and the block/cook accessors (both widths and an element,
//     ruling 2: never returned, latched on the view, the field named). Each
//     refuses non_finite_float and names the field, and nothing lands.
//   - WRITE: the variable save, the message save, the fixed save and the cook
//     writer (measure and cook alike) each refuse a NaN or an infinity.
//   - -0: rides the variable and message forms under a +0 default (elision
//     compares bits) and is carried whole by the fixed form.
//
// TestDartFiniteFloatNegativeControl removes every read-side bit test from the
// generated source and requires exactly the READ vectors to go red.

const finiteFloatSchema = `package probe

union Shot
{
    f float32
    g float64
    n int32
}

table V
{
    a  int32
    x  float32
    y  float64 = 2
    xs []float32
    ys [3]float64
    s  Shot
}

fixed table F
{
    x  float32
    y  float64 = 1
    zs [2]float32
}
`

// the retaining reader's V names `a` alone, so every float of the writer's V
// is an unknown it retains.
const finiteFloatReaderSchema = `package rprobe

table V
{
    a int32
}
`

// finiteFloatReadVectors are the vectors the negative control must turn red,
// and nothing else.
var finiteFloatReadVectors = []string{
	"var-read-f32", "var-read-f64", "var-read-list", "var-read-array", "var-read-arm", "var-read-retained",
	"msg-read-f32", "msg-read-f64",
	"fixed-read-f32", "fixed-read-f64", "fixed-read-element",
	"row-read-f32", "row-read-f64", "row-read-element",
}

// finiteFloatReadTests are the read side's raw-bits tests as the generator
// spells them, and the sabotage that takes each away.
var finiteFloatReadTests = [][2]string{
	{"if ((b & 0x7f800000) == 0x7f800000) {", "if (false && (b & 0x7f800000) == 0x7f800000) {"},
	{"if ((b & 0x7ff0000000000000) == 0x7ff0000000000000) {", "if (false && (b & 0x7ff0000000000000) == 0x7ff0000000000000) {"},
	{"if ((kind == 10 && (readU32(r.data, start) & 0x7f800000) == 0x7f800000) ||", "if (false && (kind == 10 && (readU32(r.data, start) & 0x7f800000) == 0x7f800000) ||"},
	{"if ((b & 0x7f800000) == 0x7f800000) return", "if (false && (b & 0x7f800000) == 0x7f800000) return"},
	{"if ((b & 0x7ff0000000000000) == 0x7ff0000000000000) return", "if (false && (b & 0x7ff0000000000000) == 0x7ff0000000000000) return"},
	{"if ((bits & 0x7f800000) == 0x7f800000) {", "if (false && (bits & 0x7f800000) == 0x7f800000) {"},
	{"if ((bits & 0x7ff0000000000000) == 0x7ff0000000000000) {", "if (false && (bits & 0x7ff0000000000000) == 0x7ff0000000000000) {"},
	{"if ((bits & 0x7f800000) != 0x7f800000) {", "if (true || (bits & 0x7f800000) != 0x7f800000) {"},
	{"if ((bits & 0x7ff0000000000000) != 0x7ff0000000000000) {", "if (true || (bits & 0x7ff0000000000000) != 0x7ff0000000000000) {"},
}

const finiteFloatProbe = `import 'dart:typed_data';
import 'w/WIRE' as w;
import 'r/RWIRE' as rw;
import 'w/FIXED' as m;
import 'w/COOK' as c;

var failed = 0;
void vec(String name, bool ok, String detail) {
  if (ok) {
    print('VEC $name ok');
  } else {
    failed++;
    print('VEC $name FAIL $detail');
  }
}

// a finite MARKER the probe writes, then replaces in the bytes with a NaN or
// an infinity, so no writer ever handles the non-finite value
Uint8List plant(Uint8List wire, List<int> marker, List<int> bad) {
  final out = Uint8List.fromList(wire);
  for (var i = 0; i + marker.length <= out.length; i++) {
    var hit = true;
    for (var j = 0; j < marker.length; j++) {
      if (out[i + j] != marker[j]) { hit = false; break; }
    }
    if (hit) {
      out.setRange(i, i + bad.length, bad);
      return out;
    }
  }
  throw StateError('marker $marker is not in the wire');
}

List<int> le32(int v) => (ByteData(4)..setUint32(0, v, Endian.little)).buffer.asUint8List();
List<int> le64(int v) => (ByteData(8)..setUint64(0, v, Endian.little)).buffer.asUint8List();
int f32bits(double v) => (ByteData(4)..setFloat32(0, v, Endian.little)).getUint32(0, Endian.little);

final nan32 = le32(0x7fc00001);
final inf64 = le64(0x7ff0000000000000);

void varRead(String name, w.V value, List<int> marker, List<int> bad, String field) {
  final wire = plant(w.vWireSave(value), marker, bad);
  final report = w.TableWireReport();
  final back = w.vWireLoad(wire, report);
  final fresh = back.x == 0.0 && back.y == 2.0 && back.xs.isEmpty && back.ys.every((e) => e == 0.0) && back.s.armTag == 0;
  vec(name, report.refused && report.reason == 'non_finite_float: $field' && fresh,
      'refused=${report.refused} reason=${report.reason} landed-defaults=$fresh');
}

bool throwsNonFinite(void Function() save, String field) {
  try {
    save();
  } on StateError catch (e) {
    return e.message.startsWith('non_finite_float: $field');
  }
  return false;
}

Uint8List messageBody(int slot, int bits, int width) {
  final wire = Uint8List(32);
  final writer = m.TableBitWriter(wire);
  writer.put(slot, m.tableMessageRefBits);
  if (width == 64) {
    writer.put(bits, 64);
  } else {
    writer.put(bits, 32);
  }
  writer.put(0, m.tableMessageRefBits);
  writer.align();
  return Uint8List.sublistView(wire, 0, writer.bits >> 3);
}

void main() {
  // ---- the variable form ----
  varRead('var-read-f32', w.V()..x = 1.5, le32(0x3fc00000), nan32, 'V.x');
  varRead('var-read-f64', w.V()..y = 1.5, le64(0x3ff8000000000000), inf64, 'V.y');
  varRead('var-read-list', w.V()..xs = <double>[1.25], le32(0x3fa00000), nan32, 'V.xs');
  varRead('var-read-array', w.V()..ys = <double>[0.0, 0.0, 1.75], le64(0x3ffc000000000000), inf64, 'V.ys');
  varRead('var-read-arm', w.V()..s = (w.Shot()..armTag = 1..f = 1.125), le32(0x3f900000), nan32, 'Shot.f');
  {
    final good = w.vWireSave(w.V()..x = 1.5..a = 7);
    final retain = rw.TableRetain(1 << 20, List<int>.filled(1 << 17, 0));
    final kept = rw.TableWireReport();
    final a = rw.vWireLoadRetain(good, retain, kept);
    final retainedOk = a.a == 7 && kept.retained >= 1 && kept.reason == 'ok';
    final bad = plant(good, le32(0x3fc00000), nan32);
    final report = rw.TableWireReport();
    final b = rw.vWireLoadRetain(bad, retain, report);
    vec('var-read-retained', retainedOk && report.refused && report.reason.startsWith('non_finite_float: V.0x') && b.a == 0 && retain.records.isEmpty,
        'finite retained=${kept.retained} reason=${kept.reason}; planted reason=${report.reason} a=${b.a}');
  }
  vec('var-write', throwsNonFinite(() => w.vWireSave(w.V()..x = double.nan), 'V.x') &&
      throwsNonFinite(() => w.vWireSave(w.V()..y = double.infinity), 'V.y') &&
      throwsNonFinite(() => w.vWireSave(w.V()..xs = <double>[double.negativeInfinity]), 'V.xs') &&
      throwsNonFinite(() => w.vWireSave(w.V()..s = (w.Shot()..armTag = 2..g = double.nan)), 'Shot.g'), 'a save wrote a NaN or an infinity');
  {
    final plus = w.vWireSave(w.V());
    final minus = w.vWireSave(w.V()..x = -0.0);
    final back = w.vWireLoad(minus);
    vec('var-minus-zero', minus.length > plus.length && back.x == 0.0 && back.x.isNegative, 'a -0 under the +0 default did not ride');
  }
  {
    final value = w.V()..xs = <double>[1.5];
    vec('cook-write-variable', c.VCook.cookMeasure(value) > 0 && c.VCook.cookMeasure(w.V()..xs = <double>[double.nan]) == -1 &&
        !c.VCook.cook(w.V()..xs = <double>[double.nan], Uint8List(4096), Endian.little), 'the variable cook carried a NaN');
  }

  // ---- the message form ----
  {
    final r = m.TableMessageReader()..bits = m.TableBitReader(messageBody(SLOTX, 0x7fc00001, 32));
    vec('msg-read-f32', !m.fLoadMessageBody(r, m.F()) && r.report.reason == 'non_finite_float: F.x', 'reason=${r.report.reason}');
    final r64 = m.TableMessageReader()..bits = m.TableBitReader(messageBody(SLOTY, 0x7ff0000000000000, 64));
    vec('msg-read-f64', !m.fLoadMessageBody(r64, m.F()) && r64.report.reason == 'non_finite_float: F.y', 'reason=${r64.report.reason}');
    final out = Uint8List(64);
    vec('msg-write', !m.fSaveMessageBody(m.TableBitWriter(out), m.F()..x = double.nan) &&
        !m.fSaveMessageBody(m.TableBitWriter(out), m.F()..y = double.negativeInfinity), 'a message save wrote a NaN or an infinity');
    final minus = Uint8List(64);
    final mw = m.TableBitWriter(minus);
    m.fSaveMessageBody(mw, m.F()..x = -0.0);
    final mr = m.TableBitReader(minus);
    vec('msg-minus-zero', mr.get(m.tableMessageRefBits) == SLOTX && mr.get(32) == 0x80000000, 'a -0 under the +0 default did not ride');
  }

  // ---- the fixed form ----
  {
    final good = Uint8List(m.fFixedMeasure(1));
    final value = m.F()..x = 1.5..y = 1.5;
    value.zs[1] = 1.25;
    m.fFixedSave(<m.F>[value], 1, good);
    void fixedRead(String name, List<int> marker, List<int> bad, String field) {
      final planted = plant(good, marker, bad);
      final back = <m.F>[m.F()..x = 9.0];
      final report = m.TableFixedReport();
      final n = m.fFixedLoad(back, 1, planted, planted.length, m.fFixedNewPlan(), report);
      vec(name, n == -1 && report.refused == m.TableFixedRefusal.nonFiniteFloat && report.nonFiniteFloat == field &&
          back[0].x == 0.0 && back[0].y == 1.0,
          'n=$n refused=${m.TableFixedRefusal.name(report.refused)} field=${report.nonFiniteFloat}');
    }
    fixedRead('fixed-read-f32', le32(0x3fc00000), nan32, 'F.x');
    fixedRead('fixed-read-f64', le64(0x3ff8000000000000), inf64, 'F.y');
    fixedRead('fixed-read-element', le32(0x3fa00000), nan32, 'F.zs');
    final bad = m.F()..zs[0] = double.infinity;
    final out = Uint8List(m.fFixedMeasure(1));
    vec('fixed-write', m.fFixedSave(<m.F>[bad], 1, out) == -1 && out.every((b) => b == 0) &&
        m.fFixedSave(<m.F>[m.F()..y = double.nan], 1, out) == -1, 'a fixed save wrote a NaN or an infinity');
    final minus = Uint8List(m.fFixedMeasure(1));
    m.fFixedSave(<m.F>[m.F()..x = -0.0], 1, minus);
    final back = <m.F>[m.F()];
    final n = m.fFixedLoad(back, 1, minus, minus.length, m.fFixedNewPlan(), m.TableFixedReport());
    vec('fixed-minus-zero', n == 1 && back[0].x == 0.0 && back[0].x.isNegative, 'a -0 was not carried whole');
    vec('cook-write-fixed', c.FCook.cookMeasure(m.F()) > 0 && c.FCook.cookMeasure(m.F()..x = double.nan) == -1 &&
        !c.FCook.cook(m.F()..zs[1] = double.infinity, Uint8List(4096), Endian.little), 'the fixed cook carried a NaN or an infinity');
  }

  // ---- the block/cook accessors (ruling 2) ----
  {
    void rowRead(String name, int at, List<int> bad, double Function(c.FRow) read, String field) {
      final data = ByteData(c.FRow.rowSize);
      for (var i = 0; i < bad.length; i++) {
        data.setUint8(at + i, bad[i]);
      }
      final row = c.FRow(data, 0);
      final v = read(row);
      vec(name, v == 0.0 && row.nonFiniteFloat == field, 'answered $v latched "${row.nonFiniteFloat}"');
    }
    rowRead('row-read-f32', OFFX, nan32, (r) => r.x, 'F.x');
    rowRead('row-read-f64', OFFY, inf64, (r) => r.y, 'F.y');
    rowRead('row-read-element', OFFZS + 4, nan32, (r) => r.zsAt(1), 'F.zs');
    final data = ByteData(c.FRow.rowSize)..setUint32(OFFX, 0x00000001, Endian.little);
    final row = c.FRow(data, 0);
    vec('row-subnormal', f32bits(row.x) == 1 && row.nonFiniteFloat.isEmpty, 'a subnormal did not read whole');
  }
  print('finite probe: $failed failed');
}
`

// finiteFloatFiles generates the two units the probe reads, each in its own
// directory (both are one Probe.schema), with the probe beside them.
func finiteFloatFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	library := map[string]string{}
	for dir, src := range map[string]string{"w": finiteFloatSchema, "r": finiteFloatReaderSchema} {
		u := unitFrom(t, src)
		packet, err := dart.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(packet, tables)
		for name, data := range packet {
			if strings.Contains(name, "/") {
				continue
			}
			files[dir+"/"+name] = data
			for _, suffix := range []string{"Wire.dart", "Fixed.dart", "Cook.dart"} {
				if strings.HasSuffix(name, suffix) {
					library[dir+suffix] = name
				}
			}
		}
	}
	u := unitFrom(t, finiteFloatSchema)
	slot := func(f *ir.Field) int {
		entry := ir.TableFieldEntry(f)
		for i, v := range ir.TableVocabulary(u) {
			if entry.Key() == v.Key() {
				return i + 1
			}
		}
		t.Fatalf("%s has no vocabulary slot", f.Name)
		return 0
	}
	st := u.Tables["F"]
	ml := ir.RecordLayout(u, st)
	probe := strings.NewReplacer(
		"w/WIRE", "w/"+library["wWire.dart"],
		"r/RWIRE", "r/"+library["rWire.dart"],
		"w/FIXED", "w/"+library["wFixed.dart"],
		"w/COOK", "w/"+library["wCook.dart"],
		"SLOTX", fmt.Sprint(slot(st.Fields[0])),
		"SLOTY", fmt.Sprint(slot(st.Fields[1])),
		"OFFX", fmt.Sprint(ml.FieldByName("x").Offset),
		"OFFY", fmt.Sprint(ml.FieldByName("y").Offset),
		"OFFZS", fmt.Sprint(ml.FieldByName("zs").Offset),
	).Replace(finiteFloatProbe)
	files["finite_probe.dart"] = []byte(probe)
	return files
}

// runFiniteFloatProbe writes the files and runs the probe, answering each
// vector's verdict.
func runFiniteFloatProbe(t *testing.T, files map[string][]byte) (map[string]bool, string) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "finite_probe.dart")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	verdicts := map[string]bool{}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "VEC" {
			verdicts[fields[1]] = fields[2] == "ok"
		}
	}
	if !strings.Contains(string(out), "finite probe: ") {
		t.Fatalf("the finite probe did not run to its end:\n%s", out)
	}
	return verdicts, string(out)
}

func TestDartFiniteFloatsEveryForm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	verdicts, out := runFiniteFloatProbe(t, finiteFloatFiles(t))
	if len(verdicts) < len(finiteFloatReadVectors)+8 {
		t.Fatalf("the probe answered %d vectors:\n%s", len(verdicts), out)
	}
	for _, name := range slices.Sorted(maps.Keys(verdicts)) {
		if !verdicts[name] {
			t.Errorf("vector %s is red", name)
		}
	}
	if t.Failed() {
		t.Logf("%s", out)
	}
}

// TestDartFiniteFloatNegativeControl is the reversed witness: every read-side
// raw-bits test the generator emits is taken out of the generated source, and
// EXACTLY the read vectors must go red — the writers' refusals and the -0
// vectors stay green, so the probe is red for the reason it names.
func TestDartFiniteFloatNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	files := finiteFloatFiles(t)
	patched := 0
	for name, data := range files {
		if !strings.HasSuffix(name, "Wire.dart") && !strings.HasSuffix(name, "Fixed.dart") &&
			!strings.HasSuffix(name, "Cook.dart") && !strings.HasSuffix(name, "Block.dart") {
			continue
		}
		src := string(data)
		for _, s := range finiteFloatReadTests {
			patched += strings.Count(src, s[0])
			src = strings.ReplaceAll(src, s[0], s[1])
		}
		files[name] = []byte(src)
	}
	if patched == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: the sabotage matched nothing — the read tests moved")
	}
	verdicts, out := runFiniteFloatProbe(t, files)
	var red []string
	for _, name := range slices.Sorted(maps.Keys(verdicts)) {
		if !verdicts[name] {
			red = append(red, name)
		}
	}
	want := slices.Sorted(slices.Values(finiteFloatReadVectors))
	if !slices.Equal(red, want) {
		t.Fatalf("NEGATIVE CONTROL FAILED: with the read tests removed the red vectors are %v, want exactly %v\n%s", red, want, out)
	}
}
