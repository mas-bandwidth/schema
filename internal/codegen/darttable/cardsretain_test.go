package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCardDescriptorsNameEveryField pins docs/SPEC-TABLES.md §8.1, §8.3: the
// generated descriptors of tables/cards name every field in declaration order
// with its kind, its shape (value / list / blob) and its declared bound,
// against a vocabulary written here in the probe and NOT read back from the
// descriptors. Adding a field to the declaration without updating the
// vocabulary reds the test, which is the gate the card's descriptor row stands
// on.
func TestCardDescriptorsNameEveryField(t *testing.T) {
	t.Parallel()

	u := cardsUnit(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Dart should accept card constructs: %v", err)
	}
	wire, ok := files["CardsWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no CardsWire.dart for the card closure")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CardsWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';

import 'CardsWire.dart';

void check(bool ok, String msg) {
  if (!ok) {
    stderr.writeln('FAIL: $msg');
    exit(1);
  }
}

bool eq(List<List<Object?>> a, List<List<Object?>> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i].length != b[i].length) return false;
    for (var j = 0; j < a[i].length; j++) {
      if (a[i][j] != b[i][j]) return false;
    }
  }
  return true;
}

// the pinned vocabulary: {table, field, kind, shape, bound, min, max} in
// declaration order, written here and never derived from the descriptors.
final vocab = <String, List<List<Object?>>>{
  'PathText': [
    ['s', 12, 'value', 192, null, null],
  ],
  'CardID': [
    ['s', 12, 'value', 64, null, null],
  ],
  'KeyValue': [
    ['key', 12, 'value', 32, null, null],
    ['value', 17, 'blob', null, null, null],
  ],
  'Consumer': [
    ['kind', 12, 'value', 16, null, null],
    ['who', 12, 'value', 32, null, null],
    ['route', 12, 'value', 48, null, null],
    ['model', 12, 'value', 64, null, null],
    ['started', 5, 'value', null, null, null],
    ['ended', 5, 'value', null, null, null],
    ['outcome', 12, 'value', 16, null, null],
    ['line', 12, 'value', 200, null, null],
    ['cost_micro_usd', 5, 'value', null, null, null],
  ],
  'Card': [
    ['id', 12, 'value', 64, null, null],
    ['sha', 12, 'value', 40, null, null],
    ['tier', 30, 'value', null, null, null],
    ['repo', 12, 'value', 96, null, null],
    ['_base', 12, 'value', 96, null, null],
    ['kind', 12, 'value', 24, null, null],
    ['priority', 30, 'value', null, null, null],
    ['stream', 12, 'value', 32, null, null],
    ['paths', 14, 'list', null, null, null],
    ['_new', 14, 'list', null, null, null],
    ['shared', 14, 'list', null, null, null],
    ['needs', 14, 'list', null, null, null],
    ['test', 12, 'value', 192, null, null],
    ['done', 17, 'blob', null, null, null],
    ['deadline_minutes', 4, 'value', null, 0, 100000],
    ['source', 17, 'blob', null, null, null],
    ['epoch', 4, 'value', null, null, null],
    ['column', 30, 'value', null, null, null],
    ['holder', 12, 'value', 32, null, null],
    ['attempt', 4, 'value', null, null, null],
    ['head', 12, 'value', 40, null, null],
    ['branch', 12, 'value', 128, null, null],
    ['body', 17, 'blob', null, null, null],
    ['extra', 14, 'list', null, null, null],
    ['consumers', 14, 'list', null, null, null],
  ],
};

void main() {
  final d = cardDescriptors();
  check(d.length == vocab.length, 'table count ${d.length} != ${vocab.length}');
  for (final entry in vocab.entries) {
    final info = d[entry.key];
    check(info != null, 'no descriptor for table ${entry.key}');
    final got = info!.fields
        .map((f) => <Object?>[f.name, f.kind, f.shape, f.bound, f.min, f.max])
        .toList();
    check(eq(got, entry.value),
        '${entry.key} descriptors differ from the pinned vocabulary: $got');
  }
  stdout.writeln('ok');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("card descriptors over the generated library: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not report ok: %s", out)
	}
}

// retainUnit builds the checked unit of one testdata/cards declaration of the
// retain pair (docs/SPEC-TABLES.md §6.6): older.schema is the reader, newer.schema
// the writer whose added fields the reader cannot name.
func retainUnit(t *testing.T, name string) *ir.Unit {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "cards", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return buildUnit(t, string(src))
}

// retainProbe generates one side's CardsWire.dart into a temp dir, writes the
// given probe beside it and runs it with the given args, returning the output.
func retainProbe(t *testing.T, schema string, probe string, args ...string) string {
	t.Helper()
	u := retainUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Dart should accept the retain pair: %v", err)
	}
	wire, ok := files["CardsWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatalf("Generate emitted no CardsWire.dart for %s", schema)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CardsWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Args = append(cmd.Args, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("retain probe over %s: %v\n%s", schema, err, out)
	}
	return string(out)
}

// TestCardConstructsRetainUnknownFields pins docs/SPEC-TABLES.md §6.6: the older
// declaration's retained load keeps the newer declaration's unknown scalar and
// string fields, re-emits them in a retained save byte-equal to the Go leg's
// committed evidence (retain-go.wire), and the newer declaration rereads every
// retained value equal. The unknown *string (kind 17, a node index) and the
// node record it rode under are the excluded classes, reported as loss and
// never retained silently.
func TestCardConstructsRetainUnknownFields(t *testing.T) {
	t.Parallel()

	// the producer: the NEWER declaration's plain save of the populated value,
	// byte-equal to the committed input retain-newer.wire.
	produce := `import 'dart:io';
import 'CardsWire.dart';
void main(List<String> args) {
  final c = Card();
  c.id = 'x';
  c.n = 7;
  c.extra = 'hello';
  c.futureI = 42;
  c.blob = 'world';
  File(args[0]).writeAsBytesSync(cardWireSave(c));
}
`
	dir := t.TempDir()
	u := retainUnit(t, "newer.schema")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CardsWire.dart"), files["CardsWire.dart"], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "producer.dart"), []byte(produce), 0o600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "input.wire")
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "producer.dart", input)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the newer declaration's save failed: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(input)
	want, err := os.ReadFile(filepath.Join("testdata", "cards", "retain-newer.wire"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("the producer's save differs from the committed input: %d bytes vs %d", len(got), len(want))
	}

	// the round trip: the OLDER declaration's retained load and save.
	roundTrip := `import 'dart:io';
import 'CardsWire.dart';
void main(List<String> args) {
  final wire = File(args[0]).readAsBytesSync();
  final retain = TableRetain(1048576, List<int>.filled(131072, 0));
  final report = TableWireReport();
  final card = cardWireLoadRetain(wire, retain, report);
  stdout.writeln('LOAD unknown=${report.unknown} retained=${report.retained} retainLost=${report.retainLost}');
  final size = cardWireMeasureRetain(card, retain);
  final out = cardWireSaveRetain(card, retain, report);
  stdout.writeln('SAVE size=$size retained=${report.retained} retainLost=${report.retainLost} len=${out.length}');
  File(args[1]).writeAsBytesSync(out);
}
`
	outPath := filepath.Join(dir, "retained.wire")
	out := retainProbe(t, "older.schema", roundTrip, input, outPath)
	if !strings.Contains(out, "LOAD unknown=4 retained=2 retainLost=2") {
		t.Errorf("the retained load miscounted the load events:\n%s", out)
	}
	if !strings.Contains(out, "retained=2 retainLost=2 len=66") {
		t.Errorf("the retained save miscounted the save events:\n%s", out)
	}

	// the retained save is byte-equal to the Go leg's committed evidence.
	gotRet, _ := os.ReadFile(outPath)
	wantRet, err := os.ReadFile(filepath.Join("testdata", "cards", "retain-go.wire"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotRet) != string(wantRet) {
		t.Errorf("the retained save differs from the Go leg's: got %d bytes, want %d bytes\n%x", len(gotRet), len(wantRet), gotRet)
	}

	// the verifier: the NEWER declaration rereads every retained value equal.
	verify := `import 'dart:io';
import 'CardsWire.dart';
void main(List<String> args) {
  final card = cardWireLoad(File(args[0]).readAsBytesSync());
  final ok = card.id == 'x' && card.n == 7 && card.extra == 'hello' && card.futureI == 42 && card.blob == null;
  if (!ok) {
    stderr.writeln('FAIL: id=${card.id} n=${card.n} extra=${card.extra} futureI=${card.futureI} blob=${card.blob}');
    exit(1);
  }
  stdout.writeln('ok');
}
`
	verOut := retainProbe(t, "newer.schema", verify, outPath)
	if !strings.Contains(verOut, "ok") {
		t.Errorf("the newer declaration did not reread the retained values equal:\n%s", verOut)
	}
}

// TestRetentionCapacityIsALoss pins docs/SPEC-TABLES.md §6.6: a retention buffer
// one byte short of the retained records, or an id list one entry short, drops
// the record whole and counts one retainLost, and the input bytes are never
// changed. An unknown enum variant in a known field is an excluded class and
// blocks a lossless claim.
func TestRetentionCapacityIsALoss(t *testing.T) {
	t.Parallel()

	roundTrip := func(cap, idCap string) string {
		probe := `import 'dart:io';
import 'CardsWire.dart';
void main(List<String> args) {
  final wire = File(args[0]).readAsBytesSync();
  final retain = TableRetain(` + cap + `, List<int>.filled(` + idCap + `, 0));
  final report = TableWireReport();
  final card = cardWireLoadRetain(wire, retain, report);
  stdout.writeln('LOAD unknown=${report.unknown} retained=${report.retained} retainLost=${report.retainLost}');
  final out = cardWireSaveRetain(card, retain, report);
  stdout.writeln('SAVE retained=${report.retained} retainLost=${report.retainLost} len=${out.length}');
}
`
		input, err := filepath.Abs(filepath.Join("testdata", "cards", "retain-newer.wire"))
		if err != nil {
			t.Fatal(err)
		}
		return retainProbe(t, "older.schema", probe, input)
	}

	// one byte short: no retained record fits, and none is re-emitted.
	short := roundTrip("3", "131072")
	if !strings.Contains(short, "LOAD unknown=4 retained=0 retainLost=4") {
		t.Errorf("a buffer one byte short mis-counted the load loss:\n%s", short)
	}
	if !strings.Contains(short, "SAVE retained=0 retainLost=4") {
		t.Errorf("a buffer one byte short mis-counted the save loss:\n%s", short)
	}

	// one id short: the retained ids cannot enter the caller's list, so the
	// save drops each retained record and counts it.
	noIds := roundTrip("1048576", "0")
	if !strings.Contains(noIds, "LOAD unknown=4 retained=2 retainLost=2") {
		t.Errorf("an empty id list changed the load counters:\n%s", noIds)
	}
	if !strings.Contains(noIds, "SAVE retained=2 retainLost=4") {
		t.Errorf("the id-list overflow was not reported as loss:\n%s", noIds)
	}

	// an unknown enum variant in a KNOWN field is excluded (§6.6) and blocks a
	// lossless claim: the field reads None, one unknown and one retainLost.
	enumOlder := `package cards
enum Tier { flash, pro }
table Card
{
    id   string(8)
    tier Tier
}
`
	enumNewer := `package cards
enum Tier { flash, pro, heavy }
table Card
{
    id   string(8)
    tier Tier
}
`
	produce := `import 'dart:io';
import 'CardsWire.dart';
void main(List<String> args) {
  final c = Card();
  c.id = 'x';
  c.tier = 3;
  File(args[0]).writeAsBytesSync(cardWireSave(c));
}
`
	roundTripEnum := `import 'dart:io';
import 'CardsWire.dart';
void main(List<String> args) {
  final wire = File(args[0]).readAsBytesSync();
  final retain = TableRetain(1048576, List<int>.filled(131072, 0));
  final report = TableWireReport();
  final card = cardWireLoadRetain(wire, retain, report);
  stdout.writeln('LOAD unknown=${report.unknown} retained=${report.retained} retainLost=${report.retainLost} tier=${card.tier}');
}
`
	u := buildUnit(t, enumNewer)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CardsWire.dart"), files["CardsWire.dart"], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "producer.dart"), []byte(produce), 0o600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "enum.wire")
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "producer.dart", input)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the newer enum save failed: %v\n%s", err, out)
	}
	u = buildUnit(t, enumOlder)
	files, err = Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "CardsWire.dart"), files["CardsWire.dart"], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "probe.dart"), []byte(roundTripEnum), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(dartBinary(t), "--enable-asserts", "probe.dart", input)
	cmd.Dir = dir2
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the older enum retain read failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "LOAD unknown=1 retained=0 retainLost=1 tier=0") {
		t.Errorf("the unknown enum variant was not reported as loss:\n%s", out)
	}
}
