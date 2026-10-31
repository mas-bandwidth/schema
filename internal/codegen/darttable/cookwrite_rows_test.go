package darttable

// THE COOK-WRITE ROWS ON THE DART LEG (docs/SPEC-TABLES.md §7.6): every
// `cook-write` row of testdata/conformance/tables/MANIFEST.txt, cooked by this
// leg's runtime writer through the conformance driver and compared byte for
// byte with `schema cook`'s two committed goldens.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cookWriteAbsentDart is every unit whose cook-write rows the Dart leg answers
// ABSENT (test/conformance/README.md: absent is not failure, and it is said per
// case), and why. It is a RATCHET in both directions: a row of a unit NOT named
// here that comes back absent fails by name, and a row of a unit named here
// that comes back ANSWERED fails too, so the entry is deleted the day the leg
// carries it rather than going stale.
// Every unit is carried today: the form-1 cook writer took the array of
// unions (ToolMessage.history, §2.6) and the wide kinds (SimState, §3, §15).
var cookWriteAbsentDart = map[string]string{}

// TestCookWriteAnswersEveryCookWriteRowDart pins §7.6's runtime writer to
// `schema cook`'s committed bytes in BOTH byte orders, one manifest row at a
// time, through the conformance driver's own cook-write surface
// (test/conformance/dart/main.dart): the driver loads each row's instance wire,
// measures it, is refused one byte short without writing a byte, and cooks it
// in each order. A row is a passing cook or a named absence; never a skip.
//
// It runs the driver over the generated corpus in build/tables-generated-dart,
// which make's tables-dart-block-build builds first; a bare `go test` without
// the corpus skips by name, and SCHEMA_REQUIRE_CORPUS=1 makes that a failure.
func TestCookWriteAnswersEveryCookWriteRowDart(t *testing.T) {
	t.Parallel()
	dart := dartBinary(t)
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range []string{"pointers/GraphCook.dart", "blobs/AssetsCook.dart", "cards/CardsWire.dart"} {
		if _, err := os.Stat(filepath.Join(repo, "build", "tables-generated-dart", need)); err != nil {
			if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
				t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the generated Dart corpus lacks %s: run make tables-dart-block-build", need)
			}
			t.Skip("the generated Dart corpus is not built: make tables-dart-block-build")
		}
	}
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ name, unit, little, big string }
	units := map[string]string{}
	var rows []row
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "instance" {
			units[fields[1]] = fields[2]
		}
		if len(fields) == 0 || fields[0] != "cook-write" {
			continue
		}
		if len(fields) != 4 || seen[fields[1]] {
			t.Fatalf("bad or duplicate cook-write row: %q", line)
		}
		seen[fields[1]] = true
		rows = append(rows, row{name: fields[1], little: fields[2], big: fields[3]})
	}
	if len(rows) != 46 {
		t.Fatalf("cook-write manifest has %d rows, want 46", len(rows))
	}
	outDir := t.TempDir()
	cmd := exec.Command(dart, "--enable-asserts", "test/conformance/dart/main.dart", manifest, "cook-write", outDir)
	cmd.Dir = repo
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Errorf("the driver's cook-write surface failed: %v\n%s", runErr, output)
	}
	for _, r := range rows {
		r.unit = units[r.name]
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if r.unit == "" {
				t.Fatalf("cook-write %s names no instance", r.name)
			}
			absent, listed := cookWriteAbsentDart[r.unit]
			for _, order := range []struct{ suffix, golden string }{{"", r.little}, {"-be", r.big}} {
				out := filepath.Join(outDir, r.name+order.suffix)
				if why, err := os.ReadFile(out + ".absent"); err == nil {
					if !listed {
						t.Fatalf("%s%s: the Dart leg answered ABSENT for unit %s, which it carries: %s", r.name, order.suffix, r.unit, why)
					}
					if !strings.Contains(string(why), absent) {
						t.Fatalf("%s%s: absent for %q, the ratchet names %q", r.name, order.suffix, why, absent)
					}
					continue
				}
				got, err := os.ReadFile(out)
				if err != nil {
					t.Fatalf("%s%s: runtime cook output missing (driver: %v): %v\n%s", r.name, order.suffix, runErr, err, output)
				}
				if listed {
					t.Fatalf("%s%s: unit %s is answered now; delete it from cookWriteAbsentDart", r.name, order.suffix, r.unit)
				}
				want, err := os.ReadFile(filepath.Join(repo, order.golden))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					at := 0
					for at < len(got) && at < len(want) && got[at] == want[at] {
						at++
					}
					t.Fatalf("%s%s: the runtime cook differs from schema cook at byte %d: got %d bytes, want %d", r.name, order.suffix, at, len(got), len(want))
				}
			}
		})
	}
	// THE REFUSALS BY THE REFERENCE LEG'S NAMES (§7.6, the TableRefuse* words
	// of internal/codegen/gotable/refusal.go): a buffer one byte short of the
	// measured extent is TableRefuseInvalidValue and writes nothing, and a
	// cyclic graph is TableRefuseDataCycle with no extent at all.
	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		probe := strings.ReplaceAll(cookWriteRefusalProbe, "@CORPUS@", filepath.Join(repo, "build", "tables-generated-dart"))
		if err := os.WriteFile(filepath.Join(dir, "cook_refusal_probe.dart"), []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dart, "--enable-asserts", "cook_refusal_probe.dart")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "ok") {
			t.Fatalf("the variable cook refusal probe: %v\n%s", err, out)
		}
	})
}

const cookWriteRefusalProbe = `import 'dart:typed_data';
import '@CORPUS@/pointers/GraphdemoWire.dart' as wire;
import '@CORPUS@/pointers/GraphCook.dart' as cook;
import '@CORPUS@/pointers/GraphdemoCook.dart' show TableCookWriteReport;
void check(bool ok, String what) { if (!ok) throw StateError(what); }
void main() {
  final shared = wire.ListNode()..value = 7;
  final scene = wire.Scene()..head = shared..alias = shared;
  final report = TableCookWriteReport();
  final size = cook.SceneCook.cookMeasure(scene, report: report);
  check(size > 0 && report.reason == 'ok', 'measure: $size ${report.reason}');
  for (final order in [Endian.little, Endian.big]) {
    final bytes = Uint8List(size)..fillRange(0, size, 0xa5);
    check(!cook.SceneCook.cook(scene, Uint8List.sublistView(bytes, 0, size - 1), order, report: report), 'short write accepted');
    check(report.reason == 'invalid_value', 'short write named ${report.reason}, not TableRefuseInvalidValue');
    check(bytes.every((b) => b == 0xa5), 'a refused write changed the output');
    check(cook.SceneCook.cook(scene, bytes, order, report: report) && report.reason == 'ok', 'valid write refused');
  }
  final loop = wire.ListNode();
  loop.next = loop;
  final cyclic = wire.Scene()..head = loop;
  check(cook.SceneCook.cookMeasure(cyclic, report: report) < 0, 'a cycle measured');
  check(report.reason == 'data_cycle', 'a cycle named ${report.reason}, not TableRefuseDataCycle');
  final bytes = Uint8List(4096)..fillRange(0, 4096, 0xa5);
  check(!cook.SceneCook.cook(cyclic, bytes, Endian.little, report: report), 'a cycle written');
  check(bytes.every((b) => b == 0xa5), 'a refused cycle changed the output');
  print('ok');
}
`
