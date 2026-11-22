package darttable

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The 2026-11-21 review's value rules on the Dart leg's file, fixed and JSON
// forms (review-owed/dart/land-dart; the cross-leg rules float32-range,
// negative-zero and counted-array-min):
//
//   - A compressed float32's range is held at float32 on every read: the
//     top step a writer stores for `max = 0.1` reads back, and a value past
//     float32(max) is refused out_of_range, the JSON read included (SPEC.md
//     §4.6; SPEC-TABLES §3.3).
//   - A compressed float holding -0 lands as +0 on every read and is stored
//     or printed as +0 by every writer (SPEC-TABLES §3.3; ruling
//     fixed-compressed-negzero).
//   - A counted [Min..N]T below its Min is refused on the variable, JSON and
//     fixed reads, and a fresh value is born at Min, which the variable save
//     elides as the reference's encodeArray does (SPEC.md §5.1 row 3, §4.2).
//
// The pure half reads the emitted Dart; the gated half runs it, and every
// verdict it asserts is first taken from the Go reference
// (internal/tablewire, internal/tabletext).

const review1121ReadsSchema = `package probe

table Inner { n int32 }

union Pick
{
	a int32
	b float32 | min = -1.0, max = 0.1, resolution = 0.001
}

table Root
{
	arr  [2..4]uint8
	tarr [1..3]Inner
	g    float32 | min = 0.0, max = 0.1, resolution = 0.001
	gs   []float32 | min = 0.0, max = 0.1, resolution = 0.001
	pick Pick
}

fixed table Frow
{
	arr [2..4]uint8
	g   float32 | min = 0.0, max = 0.1, resolution = 0.001
}
`

// The writer under another build: the same names and kinds, but no minimum
// on the arrays and a plain float32, so it can put on the wire a count below
// the reader's Min, a -0 and a value past the reader's range.
const review1121ReadsWriterSchema = `package probe

table Inner { n int32 }

union Pick
{
	a int32
	b float32
}

table Root
{
	arr  [..4]uint8
	tarr [..3]Inner
	g    float32
	gs   []float32
	pick Pick
}
`

func review1121ReadsFiles(t *testing.T) map[string][]byte {
	t.Helper()
	u := unitFrom(t, review1121ReadsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return files
}

func TestDartReview1121ReadsEmitted(t *testing.T) {
	t.Parallel()
	files := review1121ReadsFiles(t)
	wire := string(files["ProbeWire.dart"])
	fixed := string(files["ProbeFixed.dart"])
	for _, c := range []struct{ src, frag, what string }{
		{wire, "if (keep < 2 || keep > 4) { r.report.refuse('count_over_bound'); return; }", "the variable read must refuse a count below Min"},
		{wire, "if (keep < 1 || keep > 3) { r.report.refuse('count_over_bound'); return; }", "the variable read must refuse a table array below Min"},
		{wire, "if (v.arr.length < 2) { r.refuse('count_over_bound'); }", "the JSON read must refuse a count below Min"},
		{wire, "if (v.tarr.length < 1) { r.refuse('count_over_bound'); }", "the JSON read must refuse a table array below Min"},
		{wire, "List<int> arr = List<int>.generate(2, (_) => 0);", "a fresh [2..4]uint8 must be born at Min"},
		{wire, "List<Inner> tarr = List<Inner>.generate(1, (_) => Inner());", "a fresh [1..3]Inner must be born at Min"},
		{wire, "var birthArr = v.arr.length == 2;", "the save must test the birth value"},
		{wire, "if (v.arr.isNotEmpty && !birthArr) {", "the save must elide the birth value"},
		{wire, "if (v.tarr.isNotEmpty && !birthTarr) {", "the save must elide a table array's birth value"},
		{wire, "v.g = _jsonRangedFloat(_jsonFloatField(val, r, true, v.g), 0.0, 0.10000000149011612, r);", "the JSON read must hold a compressed float at float32"},
		{wire, "v.gs.add(_jsonRangedFloat(_jsonFloatField(item, r, true), 0.0, 0.10000000149011612, r));", "the JSON list read must hold each compressed float at float32"},
		{wire, "_jsonRangedFloat(_jsonFloatField(item, r, true, v.b), -1.0, 0.10000000149011612, r)", "the JSON arm read must hold a compressed float at float32"},
		{wire, "out.raw(_jsonFloat((v.g == 0 ? 0.0 : v.g), true));", "the JSON write must print a compressed -0 as +0"},
		{wire, "if (d > 0.10000000149011612) { r.report.refuse('out_of_range'); return; }", "the variable read must hold a float32 range at float32"},
		{wire, "if (cf < 0.0 || cf > 0.10000000149011612) r.report.refuse('out_of_range');", "the variable list read must hold each element's range"},
		{wire, "w.putU32(_f32Write((v.g == 0 ? 0.0 : v.g), 'Root.g'));", "the variable save must store a compressed -0 as +0"},
		{fixed, "if (value.arrCount < 2) {\n    report.refused = TableFixedRefusal.countOverBound;", "the fixed read must refuse a count below Min"},
		{fixed, "TableFixedLayout.float32Bits((value.g == 0 ? 0.0 : value.g))", "the fixed save must store a compressed -0 as +0"},
	} {
		if !strings.Contains(c.src, c.frag) {
			t.Errorf("%s: missing %q", c.what, c.frag)
		}
	}
	if strings.Contains(wire, "List<int> arr = <int>[];") {
		t.Error("a fresh [2..4]uint8 is still born empty")
	}
}

// review1121JSONWire is the reference's file-form bytes for a JSON text read
// under unit u.
func review1121JSONWire(t *testing.T, u *ir.Unit, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused {
		t.Fatalf("the reference did not read %s: %+v", text, rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the reference did not encode %s: %v", text, err)
	}
	return wire
}

func TestDartReview1121Reads(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart (it runs the generated variable, JSON and fixed reads)")
	reader := unitFrom(t, review1121ReadsSchema)
	writer := unitFrom(t, review1121ReadsWriterSchema)
	m := tabletext.NewModel(reader)

	// The writer's wires, each checked against the reference reader first.
	wires := map[string]struct {
		text   string
		refuse bool
	}{
		"arr_below_min": {`{"arr":[1],"tarr":[{"n":1}]}`, true},
		"tarr_empty":    {`{"arr":[1,2],"tarr":[]}`, false}, // an empty array elides: the field is absent and the birth value lands
		"arr_at_min":    {`{"arr":[1,2],"tarr":[{"n":1}]}`, false},
		"g_neg_zero":    {`{"g":-0.0}`, false},
		"g_top_step":    {`{"g":0.1}`, false},
		"g_past_max":    {`{"g":0.2}`, true},
		"gs_neg_zero":   {`{"gs":[-0.0,0.05]}`, false},
		"gs_past_max":   {`{"gs":[0.05,0.2]}`, true},
		"pick_past_max": {`{"pick":{"b":0.2}}`, true},
		"pick_neg_zero": {`{"pick":{"b":-0.0}}`, false},
	}
	var cases strings.Builder
	for _, name := range []string{"arr_below_min", "tarr_empty", "arr_at_min", "g_neg_zero", "g_top_step", "g_past_max", "gs_neg_zero", "gs_past_max", "pick_past_max", "pick_neg_zero"} {
		w := wires[name]
		wire := review1121JSONWire(t, writer, w.text)
		var rep tabletext.Report
		ok, _ := tablewire.Decode(m, m.New(reader.Tables["Root"]), wire, &rep)
		if refused := !ok || rep.Refused; refused != w.refuse {
			t.Fatalf("the reference reader's verdict on %s is refused=%v, the test says %v", name, refused, w.refuse)
		}
		fmt.Fprintf(&cases, "  wireCase('%s', %s, %t);\n", name, dartBytes(wire), w.refuse)
	}
	// The JSON texts, each checked against the reference text read.
	texts := []struct {
		name, text string
		refuse     bool
	}{
		{"json_arr_below_min", `{"arr":[1]}`, true},
		{"json_tarr_below_min", `{"tarr":[]}`, true},
		{"json_arr_at_min", `{"arr":[1,2]}`, false},
		{"json_g_top_step", `{"g":0.1}`, false},
		{"json_g_past_max", `{"g":0.2}`, true},
		{"json_g_below_min", `{"g":-0.001}`, true},
		{"json_g_neg_zero", `{"g":-0.0}`, false},
		{"json_gs_past_max", `{"gs":[0.2]}`, true},
		{"json_gs_neg_zero", `{"gs":[-0.0]}`, false},
		{"json_pick_past_max", `{"pick":{"b":0.2}}`, true},
	}
	for _, c := range texts {
		var rep tabletext.Report
		ok := m.Read(m.New(m.Lookup("Root")), []byte(c.text), &rep)
		if refused := !ok || rep.Refused; refused != c.refuse {
			t.Fatalf("the reference text read's verdict on %s is refused=%v, the test says %v", c.name, refused, c.refuse)
		}
		fmt.Fprintf(&cases, "  jsonCase('%s', %q, %t);\n", c.name, c.text, c.refuse)
	}
	// The reference's bytes for a fresh value: the birth value elides.
	fresh, err := tablewire.Encode(m, m.New(m.Lookup("Root")))
	if err != nil {
		t.Fatal(err)
	}
	out, err := runDartReview1121Reads(t, reader, fmt.Sprintf(review1121ReadsProbe, cases.String(), dartBytes(fresh)))
	if err != nil || !strings.Contains(out, "REVIEW1121_READS_OK") {
		t.Fatalf("the Dart reads did not hold: %v\n%s", err, out)
	}
}

func runDartReview1121Reads(t *testing.T, u *ir.Unit, probe string) (string, error) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, map[string][]byte{"review1121_reads.dart": []byte(probe)})
	dir := t.TempDir()
	writeFiles(t, dir, files)
	cmd := testproc.Command(t, dartBinary(t), "--enable-asserts", "review1121_reads.dart")
	cmd.Dir = dir
	got, err := cmd.CombinedOutput()
	return string(got), err
}

const review1121ReadsProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart';
import 'ProbeFixed.dart' as f;

final fails = <String>[];
void check(bool ok, String what) { if (!ok) fails.add('FAIL: ' + what); }
int bits32(double v) => (ByteData(4)..setFloat32(0, v, Endian.little)).getUint32(0, Endian.little);
bool sameBytes(Uint8List a, Uint8List b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) { if (a[i] != b[i]) return false; }
  return true;
}

void wireCase(String name, List<int> bytes, bool refuse) {
  final report = TableWireReport();
  final v = rootWireLoad(Uint8List.fromList(bytes), report);
  check(report.refused == refuse, '$name: variable read refused=${report.refused} want $refuse (${report.reason})');
  if (refuse) return;
  // a compressed float's -0 lands as +0 (§3.3)
  check(bits32(v.g) != 0x80000000, '$name: -0 landed as -0');
  for (final e in v.gs) { check(bits32(e) != 0x80000000, '$name: a -0 element landed as -0'); }
  if (v.pick.armTag == 2) check(bits32(v.pick.b) != 0x80000000, '$name: a -0 arm landed as -0');
  check(v.arr.length >= 2 && v.tarr.isNotEmpty, '$name: a counted array below its Min landed');
}

void jsonCase(String name, String text, bool refuse) {
  final report = TableTextReport();
  final v = rootFromJson(text, report);
  check(report.refused == refuse, '$name: JSON read refused=${report.refused} want $refuse (${report.reason})');
  if (refuse) return;
  check(bits32(v.g) != 0x80000000, '$name: -0 landed as -0');
  for (final e in v.gs) { check(bits32(e) != 0x80000000, '$name: a -0 element landed as -0'); }
}

void guard(String what, void Function() body) {
  try {
    body();
  } catch (e) {
    fails.add('FAIL: $what threw $e');
  }
}

void main() {
%s
  guard('birth', () {
  // A FRESH VALUE IS BORN AT MIN and saves the reference's bytes: the birth
  // value elides, and reads back at its birth.
  final fresh = Root();
  check(fresh.arr.length == 2 && fresh.arr.every((e) => e == 0), 'fresh arr is not born at Min: ${fresh.arr}');
  check(fresh.tarr.length == 1 && fresh.tarr[0].n == 0, 'fresh tarr is not born at Min');
  final freshWire = rootWireSave(fresh);
  check(sameBytes(freshWire, Uint8List.fromList(%s)), 'a fresh value does not save the reference bytes: $freshWire');
  final back = rootWireLoad(freshWire, TableWireReport());
  check(back.arr.length == 2 && back.tarr.length == 1, 'the birth value did not read back at its birth');
  // a value that is not the birth value rides and reads back
  final moved = Root()..arr = [0, 5];
  final movedBack = rootWireLoad(rootWireSave(moved), TableWireReport());
  check(movedBack.arr.length == 2 && movedBack.arr[1] == 5, 'a non-birth [0, 5] did not ride');
  final grown = Root()..tarr = [Inner(), Inner()];
  check(rootWireLoad(rootWireSave(grown), TableWireReport()).tarr.length == 2, 'a two-element tarr did not ride');
  final named = Root()..tarr = [Inner()..n = 3];
  check(rootWireLoad(rootWireSave(named), TableWireReport()).tarr[0].n == 3, 'a one-element tarr off its default did not ride');
  });

  guard('negative zero writers', () {
  // EVERY WRITER STORES A COMPRESSED -0 AS +0
  final negative = Root()..g = -0.0..gs = [-0.0];
  final positive = Root()..g = 0.0..gs = [0.0];
  check(sameBytes(rootWireSave(negative), rootWireSave(positive)), 'the variable save stored a compressed -0');
  check(!rootToJson(negative).contains('-0'), 'the JSON write printed a compressed -0: ${rootToJson(negative)}');
  final fneg = [f.Frow()..g = -0.0];
  final fpos = [f.Frow()..g = 0.0];
  final nb = Uint8List(f.frowFixedMeasure(1));
  final pb = Uint8List(f.frowFixedMeasure(1));
  f.frowFixedSave(fneg, 1, nb);
  f.frowFixedSave(fpos, 1, pb);
  check(sameBytes(nb, pb), 'the fixed save stored a compressed -0');
  });

  guard('fixed count below Min', () {
  // THE FIXED READ REFUSES A COUNT BELOW MIN: a lawful file at count 2,
  // its count then set to 1 (the dead slot is already zero).
  final rows = [f.Frow()..arr[0] = 7];
  final buf = Uint8List(f.frowFixedMeasure(1));
  f.frowFixedSave(rows, 1, buf);
  var at = -1;
  for (var i = 0; i + 6 <= buf.length; i++) {
    if (buf[i] == 2 && buf[i + 1] == 0 && buf[i + 2] == 0 && buf[i + 3] == 0 && buf[i + 4] == 7 && buf[i + 5] == 0) {
      check(at < 0, 'the count pattern is not unique');
      at = i;
    }
  }
  check(at >= 0, 'the count pattern is not in the fixed file');
  final lawful = [f.Frow()];
  final lr = f.TableFixedReport();
  check(f.frowFixedLoad(lawful, 1, buf, buf.length, f.frowFixedNewPlan(), lr) == 1 && lawful[0].arrCount == 2, 'the lawful fixed file did not read');
  if (at >= 0) {
    buf[at] = 1;
    final out = [f.Frow()];
    final fr = f.TableFixedReport();
    final n = f.frowFixedLoad(out, 1, buf, buf.length, f.frowFixedNewPlan(), fr);
    check(n == -1 && fr.refused == f.TableFixedRefusal.countOverBound, 'the fixed read kept a count below Min: n=$n refused=${fr.refused}');
  }
  });

  if (fails.isNotEmpty) { stderr.writeln(fails.join('\n')); exit(1); }
  stdout.writeln('REVIEW1121_READS_OK');
}
`
