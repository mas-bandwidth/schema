package darttable

// THE VERSIONING FIXTURES ON THE DART LEG (docs/FIXED-FORM-VERSIONING-TESTS.md,
// the procedures in docs/FIXED-FORM-ALGORITHM.md §5). Every row of that page is
// ONE definition change and owes TWO read columns:
//
//	NEW-READS-OLD    the widened reader reads the older writer's file: every
//	                 old value lands exactly, the reader's tail is its declared
//	                 default, and the counters are §5.4's — for an APPEND, all
//	                 of them zero, because an append the reader knows is not an
//	                 event.
//	OLD-REFUSES-NEW  the older reader given the widened writer's file refuses
//	                 `layout_newer` BEFORE any record, reporting THE FILE'S
//	                 HASH and nothing else, with no counter moved and
//	                 `malformed` false (§5.3's joint answer).
//
// The bytes are the C++ reference's: `make tables-fixedform-corpus` writes
// `build/fixedform-corpus/old_<row>.bin` and `new_<row>.bin`, plus the rows that
// are not a pair — `old_/mid_/new_floor.bin` and `old_/a_/b_/new_lineage_merge.bin`
// (§5.9 #28). The two schemas of a row are `test/tables/VOLD_<row>.schema` and
// `VNEW_<row>.schema`: ONE table name in two packages, because the two layouts
// are ONE LINEAGE.
//
// THE PROBE UNITS LIVE BESIDE THE LEG'S GENERATOR (§5.9 #18), one generated
// probe per row per COLUMN, compiled and run with THIS leg's own toolchain —
// the Dart SDK the Makefile pins. Two generations of one table name have no
// spelling inside one Dart library, so the column is the unit.
//
// THE LINEAGE IS DATA THE BUILD HANDS THE BACKEND (§5.2, COMPILE(lock, T)):
// here the test plays the lock, handing the newer unit the older unit's locked
// entry — the wire hash, the layout bytes verbatim and the record size. A
// reader NEVER parses the layout a file carries; it matches the header's hash
// against the lineage and compares the bytes it already holds.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the rows ---------------------------------------------------------------

type versionRow struct {
	row string
	// sameHash is a row whose edit moves no layout byte and no digest byte
	// (`rename_without_was`, `field_deprecate`, `field_undeprecate`): the two
	// hashes are equal, both sides take the identity plan, and the file READS
	// in BOTH directions — which is the test that the hash, and only the hash,
	// is the version (§5.7).
	sameHash bool
	// widens is a row that grows a WIDTH, so `widened` moves at least once;
	// every other row is an APPEND and owes every counter at zero (§5.9 #10).
	widens bool
	// poison fills the reader's record image with 0x5A before the load, so a
	// prefill that never ran cannot pass as a zero default (§5.7, §5.9 #17).
	poison bool
	// check is Dart source asserting the landed values, over `values`.
	check string
}

var versionRows = []versionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widens: true},
	// THE POISON ROWS (§5.7): the image the prefill lands in is filled with
	// 0x5A first, because with a zero default over a zeroed image the row passes
	// whether the prefill ran or not — and these two are the rows whose whole
	// subject is that it ran. The appended slots owe THE ELEMENT'S OWN DEFAULTS.
	{row: "array_fixed_grow", poison: true, check: `
  check(values[0].lead == 0xAAAAAAAA && values[0].trail == 0xBBBBBBBB,
      'array_fixed_grow moved a neighbour: ${values[0].lead} ${values[0].trail}');
  for (var i = 4; i < 8; i++) {
    check(values[0].vals[i].x == 7 && values[0].vals[i].y == 9,
        'slot $i past the old bound is not the ELEMENT default: '
        '${values[0].vals[i].x} ${values[0].vals[i].y}');
  }`},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append", check: `
  check(
    values[0].x == 11 && values[0].y == 22 && values[0].z == 33,
    'the old writer values did not land: ${values[0].x} ${values[0].y} ${values[0].z}',
  );
  check(values[0].w == 77, 'the appended field is not its declared default: ${values[0].w}');`},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widens: true},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true, check: `
  check(n == 3, 'the int_widen file carries three records, not $n');
  const want = <int>[-1, -32768, 32767];
  for (var k = 0; k < 3; k++) {
    check(values[k].v == want[k], 'record $k widened wrong: ${values[k].v}');
    check(
      values[k].lead == 0xAAAAAAAA && values[k].trail == 0xBBBBBBBB,
      'record $k moved a neighbour: ${values[k].lead} ${values[k].trail}',
    );
  }`},
	{row: "keyed_array_enum_append", poison: true, check: `
  check(values[0].slots[3].n == 7,
      'the slot the appended key opened is not the element default: '
      '${values[0].slots[3].n}');`},
	{row: "nested_append"},
	{row: "optional_add"},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	// `union_arm_payload_widen` IS NOT A WIDEN: it appends a field INSIDE an
	// arm, so it owes `widened == 0` like every other append (§5.7).
	{row: "union_arm_payload_widen"},
	{row: "wstring_grow"},
}

// ---- NEW-READS-OLD ----------------------------------------------------------

func TestFixedVersioningNewReadsOld(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	for _, r := range versionRows {
		r := r
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			counters := `check(report.widened == 0, 'widened ${report.widened}, and an append is not an event');`
			if r.widens {
				counters = `check(report.widened != 0, 'a widening row moved no widened counter');`
			}
			poison := ""
			if r.poison {
				poison = `  plan.image.fillRange(0, plan.image.length, 0x5A);`
			}
			body := fmt.Sprintf(`  final data = File(FILE).readAsBytesSync();
%s
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n >= 1, 'the newer reader refused the older writer file: n=$n ${why(report)}');
  check(report.refused == 0, 'a clean NEW-READS-OLD is not a refusal: ${why(report)}');
  check(!report.malformed, 'a clean NEW-READS-OLD is not malformed');
  check(
    report.unknown == 0 && report.kindMismatch == 0 && report.clamped == 0 && report.duplicate == 0,
    'counters moved on a clean backward read: ${why(report)}',
  );
  %s
%s`, poison, counters, r.check)
			out, err := runVersionProbe(t, dartBin, "VNEW_"+r.row, []string{"VOLD_" + r.row}, 0,
				filepath.Join(corpus, "old_"+r.row+".bin"), body)
			if err != nil {
				t.Fatalf("NEW-READS-OLD %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- OLD-REFUSES-NEW --------------------------------------------------------

func TestFixedVersioningOldRefusesNew(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	for _, r := range versionRows {
		r := r
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			var body string
			if r.sameHash {
				// A row whose edit moves no layout byte and no digest byte has
				// NO second column: the hashes are equal and the file reads in
				// both directions (§5.7).
				body = `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n >= 1, 'a row that moved no layout byte must READ both ways: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed, 'an equal hash is not a version: ${why(report)}');`
			} else {
				body = `  final data = File(FILE).readAsBytesSync();
  final want = ByteData.sublistView(data).getUint64(8, Endian.little);
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == -1, 'the older reader did not refuse the newer writer file: n=$n');
  check(
    name(report.refused) == 'layout_newer',
    'a hash in no lineage entry owes layout_newer, not ${name(report.refused)}',
  );
  check(
    report.layoutHash == want,
    'layout_newer reports THE FILE hash: got ${report.layoutHash}, want $want',
  );
  check(!report.malformed, 'a refusal by name never sets malformed too (the joint answer)');
  check(
    report.widened == 0 && report.unknown == 0 && report.kindMismatch == 0 &&
        report.clamped == 0 && report.duplicate == 0,
    'REFUSE is total: no counter moves: ${why(report)}',
  );
  // THE DESTINATION IS STILL EVERY FIELD OF A FRESH VALUE, compared as VALUES
  // and never as a struct's slack (§5.9 #17): the poison is laid through the
  // record image, which is the byte view this leg has.
  check(
    fresh(values[0]),
    'REFUSE wrote destination values',
  );`
			}
			out, err := runVersionProbe(t, dartBin, "VOLD_"+r.row, nil, 0,
				filepath.Join(corpus, "new_"+r.row+".bin"), body)
			if err != nil {
				t.Fatalf("OLD-REFUSES-NEW %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- the floor --------------------------------------------------------------
//
// The floor is ONE NUMBER and the lineage is ONE ARRAY, so "retired" is an
// index cut and the operator's two answers stay distinct: below the floor is
// `layout_unsupported` (upgrade the client), outside the lineage is
// `layout_newer` (ship the reader). §5.2, §5.7's three floor rows.

func TestFixedVersioningFloor(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	oldFile := filepath.Join(corpus, "old_floor.bin")
	midFile := filepath.Join(corpus, "mid_floor.bin")

	run := func(t *testing.T, retire int, body string) {
		t.Helper()
		out, err := runVersionProbe(t, dartBin, "VNEW_floor", []string{"VOLD_floor", "VMID_floor"}, retire,
			oldFile, body)
		if err != nil {
			t.Fatalf("floor: %v\n%s", err, out)
		}
	}

	t.Run("floor_at", func(t *testing.T) {
		t.Parallel()
		// Nothing retired: the OLDEST file is at the floor and it reads.
		run(t, 0, `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n >= 1 && report.refused == 0 && !report.malformed,
      'a file AT the floor must read: n=$n ${why(report)}');`)
	})
	t.Run("floor_below", func(t *testing.T) {
		t.Parallel()
		// Entry 0 retired: the floor is 1, and the file one below it refuses
		// layout_unsupported — nothing decoded, no counter moved. Both layout
		// refusals report the file's hash (§5.9 #7).
		run(t, 1, fmt.Sprintf(`  final data = File(FILE).readAsBytesSync();
  final want = ByteData.sublistView(data).getUint64(8, Endian.little);
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == -1 && name(report.refused) == 'layout_unsupported',
      'a file below the floor owes layout_unsupported: n=$n ${name(report.refused)}');
  check(report.layoutHash == want, 'layout_unsupported reports the file hash too');
  check(!report.malformed && report.widened == 0 && report.unknown == 0 && report.clamped == 0,
      'REFUSE is total: ${why(report)}');
  final mid = File(%q).readAsBytesSync();
  final m = LOAD(values, values.length, mid, mid.length, plan, report);
  check(m >= 1, 'the file AT the raised floor must still read: $m ${why(report)}');`, midFile))
	})
	t.Run("floor_raise_live", func(t *testing.T) {
		t.Parallel()
		// The floor raised by one: the file that read yesterday refuses today.
		run(t, 2, fmt.Sprintf(`  final mid = File(%q).readAsBytesSync();
  final n = LOAD(values, values.length, mid, mid.length, plan, report);
  check(n == -1 && name(report.refused) == 'layout_unsupported',
      'the floor raised by one: yesterday file must refuse: n=$n ${name(report.refused)}');`, midFile))
	})
}

// ---- the hash ---------------------------------------------------------------

func TestFixedVersioningHash(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	old := filepath.Join(corpus, "old_field_append.bin")
	fresh := filepath.Join(corpus, "new_field_append.bin")

	probe := func(t *testing.T, body string) {
		t.Helper()
		out, err := runVersionProbe(t, dartBin, "VNEW_field_append", []string{"VOLD_field_append"}, 0, old, body)
		if err != nil {
			t.Fatalf("hash case: %v\n%s", err, out)
		}
	}

	// hash_unknown: a hash in no lineage refuses layout_newer, reporting the
	// FILE'S hash and nothing else.
	t.Run("hash_unknown", func(t *testing.T) {
		t.Parallel()
		probe(t, `  final data = File(FILE).readAsBytesSync();
  ByteData.sublistView(data).setUint64(8, 0x0EADBEEFCAFEF00D, Endian.little);
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == -1 && name(report.refused) == 'layout_newer',
      'hash_unknown: n=$n ${name(report.refused)}');
  check(report.layoutHash == 0x0EADBEEFCAFEF00D, 'hash_unknown reports the file hash');
  check(!report.malformed, 'hash_unknown is a refusal by name, not malformed');`)
	})

	// hash_known_bytes_differ: a KNOWN hash whose layout bytes differ from the
	// lock's is ONE name, layout_malformed — "a lie about a known version". The
	// seven §1.1 malformations under a known hash all land here, never in a
	// runtime walk (§5.3).
	t.Run("hash_known_bytes_differ", func(t *testing.T) {
		t.Parallel()
		probe(t, `  final data = File(FILE).readAsBytesSync();
  data[20 + 4] ^= 0xFF;
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == -1 && name(report.refused) == 'layout_malformed',
      'hash_known_bytes_differ: n=$n ${name(report.refused)}');
  check(!report.malformed, 'a refusal by name never sets malformed too');`)
	})

	// hash_identity: the reader's own hash selects the identity plan.
	t.Run("hash_identity", func(t *testing.T) {
		t.Parallel()
		probe(t, fmt.Sprintf(`  final data = File(%q).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n >= 1 && report.refused == 0 && !report.malformed,
      'hash_identity: n=$n ${why(report)}');
  check(values[0].w == 777, 'the identity plan lost a value: ${values[0].w}');`, fresh))
	})
}

// ---- lineage_merge ----------------------------------------------------------
//
// Two branches append different fields; after the merge BOTH pre-merge files
// read on the merged build (the name-subset rule, bill §8a.1).

func TestFixedVersioningLineageMerge(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	body := fmt.Sprintf(`  for (final path in <String>[%q, %q]) {
    final data = File(path).readAsBytesSync();
    final n = LOAD(values, values.length, data, data.length, plan, report);
    check(n >= 1 && report.refused == 0 && !report.malformed,
        '$path: both pre-merge files read on the merged build: n=$n ${why(report)}');
  }`, filepath.Join(corpus, "a_lineage_merge.bin"), filepath.Join(corpus, "b_lineage_merge.bin"))
	out, err := runVersionProbe(t, dartBin, "VNEW_lineage_merge",
		[]string{"VOLD_lineage_merge", "VBRA_lineage_merge", "VBRB_lineage_merge"}, 0,
		filepath.Join(corpus, "old_lineage_merge.bin"), body)
	if err != nil {
		t.Fatalf("lineage_merge: %v\n%s", err, out)
	}
}

// ---- the harness ------------------------------------------------------------

func fixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old_field_append.bin")); err != nil {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS. A bare
		// `go test ./...` on a tree that never built the oracle has nothing to
		// read and says so; but `make tables-dart-versioning` builds the corpus
		// first and sets SCHEMA_REQUIRE_CORPUS=1, so under that target a
		// missing file means the build did not do what the target says it did.
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return dir
}

// dartBinary is THE LEG'S OWN TOOLCHAIN (§5.9 #18): the SDK make/dart.mk pins,
// overridable the way the Makefile overrides it.
func dartBinary(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("DART"); env != "" {
		return env
	}
	dart, err := filepath.Abs("../../../dist/dart-sdk-3.13.2/bin/dart")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dart); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the Dart SDK is not at %s: %v", dart, err)
		}
		t.Skip("the Dart SDK is not unpacked in dist/: see make/dart.mk")
	}
	return dart
}

func versionSchema(t *testing.T, base string) *ir.Unit {
	t.Helper()
	path := filepath.Join("..", "..", "..", "test", "tables", base+".schema")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ast, perrs := parser.Parse(base+".schema", src)
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", base, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: path, Name: base + ".schema", Base: base, Bytes: src, AST: ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", base, cerrs[0])
	}
	return u
}

// versionRoot is the fixed table a corpus file was written FROM: the OUTER
// table, the one no other fixed table of the unit names by value (§5.9 #9).
func versionRoot(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) == 0 {
		t.Fatal("a versioning row declares a fixed root")
	}
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st
		}
	}
	return roots[len(roots)-1]
}

// runVersionProbe generates the READER's Dart with the OLDER units' locked
// entries handed to it as its lineage — oldest first, the reader's own layout
// last — marks the first `retire` entries retired (which is what moves the
// floor), writes ONE GENERATED PROBE beside it, and runs it with the leg's own
// toolchain. A probe is generated, never hand-written, so a row and its
// negative control cost the same (§5.9 #18).
func runVersionProbe(t *testing.T, dartBin, reader string, older []string, retire int, file, body string) (string, error) {
	t.Helper()
	u := versionSchema(t, reader)
	lineage := map[string][]FixedLineageEntry{}
	for i, base := range older {
		o := versionSchema(t, base)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			e.Retired = i < retire
			if e.Retired {
				e.Reason = "retired by the test's lock"
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	// THE PACKET LIBRARY TOO: a row's unions, enums and flags are declared
	// there, and the fixed library imports them the way a consumer does.
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("generate: %v", err)
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
	st := versionRoot(t, u)
	lower := lowerFirst(st.Name)
	src := strings.NewReplacer(
		"FILE", fmt.Sprintf("%q", file),
		"LOAD", "t."+lower+"FixedLoad",
	).Replace(body)
	probe := fmt.Sprintf(`// GENERATED BY internal/codegen/darttable/fixedversioning_test.go — one probe per
// row per COLUMN (docs/FIXED-FORM-ALGORITHM.md §5.9 #18).
import 'dart:io';
import 'dart:typed_data';

import './%sFixed.dart' as t;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    exitCode = 1;
  }
}

String name(int refused) => t.TableFixedRefusal.name(refused);

String why(t.TableFixedReport r) =>
    'refused=${name(r.refused)} malformed=${r.malformed} '
    'unknown=${r.unknown} kind=${r.kindMismatch} clamped=${r.clamped} '
    'widened=${r.widened} duplicate=${r.duplicate} layoutHash=${r.layoutHash}';

// A FRESH VALUE's every field, compared as VALUES (§5.9 #17).
bool fresh(t.%[2]s value) {
  final want = t.%[2]s();
  t.init%[2]s(want);
  return '$value' == '$want' || _same(value, want);
}

bool _same(t.%[2]s a, t.%[2]s b) {
  final bytes = Uint8List(t.%[3]sFixedBodyBytes);
  final other = Uint8List(t.%[3]sFixedBodyBytes);
  t.%[3]sFixedWriteBody(bytes, ByteData.sublistView(bytes), 0, a);
  t.%[3]sFixedWriteBody(other, ByteData.sublistView(other), 0, b);
  for (var i = 0; i < bytes.length; i++) {
    if (bytes[i] != other[i]) {
      return false;
    }
  }
  return true;
}

void main() {
  final values = <t.%[2]s>[for (var i = 0; i < 8; i++) t.%[2]s()];
  for (final v in values) {
    t.init%[2]s(v);
  }
  final plan = t.%[3]sFixedNewPlan();
  final report = t.TableFixedReport();
%[4]s
  if (exitCode == 0) {
    stdout.writeln('ok');
  }
}
`, reader, st.Name, lower, src)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBin, "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
