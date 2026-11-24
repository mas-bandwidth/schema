package darttable

// A DEPRECATED VARIANT IS NEVER WRITTEN BY THE DART BLOCK BUILDER
// (docs/SPEC-TABLES.md §2.15 WRITE). <root>BlockBuild validates the value
// against the write contract before it writes a byte (fixedmodule.go,
// emitBlockValidateElem); Dart's writer checks are debug asserts (SPEC.md
// §5.1), so the probe runs under --enable-asserts and wants the builder to
// throw AssertionError on the deprecated variant `Old` (ordinal 2), on the
// scalar MoodField.mood and on the array element Moods.row[0], and to write
// the live variants `Fierce` and `Calm`. Until 2026-11-24 the block builder
// checked only the enum's range and wrote `Old`. No corpus row reaches this
// writer (the block rows are hand-dispatched per root in the driver), so this
// leg test is the gate: make tables-dart-deprecated-block-write.

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// the Mood, Moods and MoodField of tables/examples/Tables.schema.
const deprecatedBlockSchema = `package probe
enum Mood { Calm
 Old | deprecated
 Fierce }
fixed table Moods { row [..4]Mood }
fixed table MoodField { mood Mood = Fierce
 tail uint16 }
`

const deprecatedBlockProbe = `import 'dart:typed_data';

import './Probe.dart';
import './ProbeFixed.dart';

// the builder must throw AssertionError on the deprecated variant
void refuses(String label, int Function() build) {
  String got;
  try {
    got = 'wrote (${build()})';
  } on AssertionError {
    print('CHECK $label ok');
    return;
  } catch (e) {
    got = 'threw $e';
  }
  print('CHECK $label FAIL $got');
}

// and write the live one
void writes(String label, int Function() build) {
  try {
    final n = build();
    print('CHECK $label ${n > 0 ? 'ok' : 'FAIL refused ($n)'}');
  } catch (e) {
    print('CHECK $label FAIL threw $e');
  }
}

MoodField scalar(Mood m) => MoodField()..mood = m;

Moods element(Mood m) {
  final v = Moods()..rowCount = 2;
  v.row[0] = m;
  v.row[1] = Mood.calm;
  return v;
}

Uint8List block() => Uint8List(4096);

void main() {
  refuses('block:scalar', () => moodFieldBlockBuild(block(), 0, scalar(Mood.old)));
  writes('block:scalar-live', () => moodFieldBlockBuild(block(), 0, scalar(Mood.fierce)));
  writes('block:scalar-calm', () => moodFieldBlockBuild(block(), 0, scalar(Mood.calm)));
  refuses('block:element', () => moodsBlockBuild(block(), 0, element(Mood.old)));
  writes('block:element-live', () => moodsBlockBuild(block(), 0, element(Mood.fierce)));
  writes('block:element-calm', () => moodsBlockBuild(block(), 0, element(Mood.calm)));
}
`

// TestDartBlockBuildRefusesDeprecatedVariant generates the probe schema,
// runs the block builder in the leg's DEBUG build (asserts on) and wants
// every CHECK line ok: Old refused, element and scalar; Fierce and Calm
// written.
func TestDartBlockBuildRefusesDeprecatedVariant(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	dartBin := dartBinary(t)
	u := unitFrom(t, deprecatedBlockSchema)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate tables: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// NOT `probe.dart`: on a case-insensitive filesystem it is Probe.dart
	if err := os.WriteFile(filepath.Join(dir, "deprecated_block_probe.dart"),
		[]byte(deprecatedBlockProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, dartBin, "--enable-asserts", "deprecated_block_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the deprecated block probe did not run: %v\n%s", err, out)
	}
	want := []string{"block:scalar", "block:scalar-live", "block:scalar-calm",
		"block:element", "block:element-live", "block:element-calm"}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "CHECK "); ok {
			label, verdict, _ := strings.Cut(rest, " ")
			got[label] = verdict
		}
	}
	for _, label := range want {
		verdict, ok := got[label]
		if !ok {
			t.Errorf("the Dart block builder: %s printed no CHECK line\n%s", label, out)
			continue
		}
		if verdict != "ok" {
			t.Errorf("the Dart block builder (<root>BlockBuild): %s: %s", label, verdict)
		}
	}
	if len(got) != len(want) {
		t.Errorf("the probe printed %d CHECK lines, want %d:\n%s", len(got), len(want), out)
	}
}
