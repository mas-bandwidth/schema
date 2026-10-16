package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFixedClassOnTableWireDart is the fixed-class card's red-first anchor
// (docs/SPEC-TABLES.md §3, §3.1, §4, §5, §16; SPEC §4.2). Its use case is a
// sprint card written in one language, read and upgraded in another, keeping
// every field across the hop. It pins, byte for byte against the Go oracle
// (internal/tablewire, internal/tabletext, and the committed oracle bytes the
// conformance manifest names):
//
//   - THE FIXED CLASS ON FORM 1: a `fixed table` a variable root reaches by
//     pointer, by value and as a counted pointer list rides the variable wire,
//     its `type` member included (test/tables/W1.schema, tblw1).
//   - DEFAULTS: string(N), bytes(N) and flags defaults are the storage's
//     initial value, the value the save elides against and the value an absent
//     field reads as; a non-default empty value rides (SPEC §4.2).
//   - RENAMES: a table renamed under `was` (W1 -> W2, a node record's type
//     id), an enum variant and a `type`'s field renamed under `was` read
//     across the hop in silence and re-save to the oracle's bytes (§5).
func TestFixedClassOnTableWireDart(t *testing.T) {
	t.Parallel()

	t.Run("fixed table in a variable closure (tblw1, tblw2)", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join("..", "..", "..")
		dir := t.TempDir()
		for _, gen := range []struct{ schema, wire string }{
			{"W1.schema", "Tblw1Wire.dart"},
			{"W2.schema", "Tblw2Wire.dart"},
		} {
			u := loadUnit(t, filepath.Join(root, "test", "tables", gen.schema))
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("%s: Dart must carry a fixed table on the variable wire, never refuse it: %v", gen.schema, err)
			}
			src, ok := files[gen.wire]
			if !ok || len(src) == 0 {
				t.Fatalf("%s: Generate emitted no %s — the fixed class gets no form-1 codec", gen.schema, gen.wire)
			}
			writeFile(t, filepath.Join(dir, gen.wire), src)
		}
		oracle := func(rel ...string) string {
			p, err := filepath.Abs(filepath.Join(append([]string{root}, rel...)...))
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
		runProbe(t, dir, fleetProbe,
			oracle("testdata", "wire", "tables", "w1_fleet.bin"),
			oracle("testdata", "wire", "tables", "w1_fleet_default.bin"),
			oracle("testdata", "wire", "tables", "w2_fleet.bin"),
			oracle("testdata", "conformance", "tables", "json", "w1_fleet.json"),
			oracle("testdata", "conformance", "tables", "json", "w2_fleet.json"),
		)
	})

	t.Run("string, bytes and flags defaults", func(t *testing.T) {
		t.Parallel()
		u := buildUnit(t, defaultsDecl)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("string, bytes and flags defaults must be carried on form 1: %v", err)
		}
		src, ok := files["DefsWire.dart"]
		if !ok || len(src) == 0 {
			t.Fatal("Generate emitted no DefsWire.dart — the defaults' table gets no form-1 codec")
		}
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "DefsWire.dart"), src)
		m := tabletext.NewModel(u)
		// the all-default record: every defaulted field is elided (§3)
		fresh := oracleEncode(t, m, u.Tables["Defs"], `{}`)
		// a non-default EMPTY value is not the default and must ride
		empty := oracleEncode(t, m, u.Tables["Defs"], defaultsEmptyJson)
		// every field away from its default
		set := oracleEncode(t, m, u.Tables["Defs"], defaultsSetJson)
		writeFile(t, filepath.Join(dir, "fresh.bin"), fresh)
		writeFile(t, filepath.Join(dir, "empty.bin"), empty)
		writeFile(t, filepath.Join(dir, "set.bin"), set)
		writeFile(t, filepath.Join(dir, "empty.json"), []byte(defaultsEmptyJson))
		writeFile(t, filepath.Join(dir, "set.json"), []byte(defaultsSetJson))
		runProbe(t, dir, defaultsProbe, "fresh.bin", "empty.bin", "set.bin", "empty.json", "set.json")
	})

	t.Run("renames: variant and type field", func(t *testing.T) {
		t.Parallel()
		oldU := buildUnit(t, renameOldDecl)
		newU := buildUnit(t, renameNewDecl)
		files, err := Generate(newU)
		if err != nil {
			t.Fatalf("a variant and a type field renamed under was must be carried on form 1: %v", err)
		}
		src, ok := files["RnewWire.dart"]
		if !ok || len(src) == 0 {
			t.Fatal("Generate emitted no RnewWire.dart")
		}
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "RnewWire.dart"), src)
		oldM := tabletext.NewModel(oldU)
		written := oracleEncode(t, oldM, oldU.Tables["Cfg"], renameOldJson)
		// the oracle's own read of the old bytes under the new names, re-saved:
		// silent, and the bytes a renamed reader owes back (§5)
		newM := tabletext.NewModel(newU)
		inst := newM.New(newU.Tables["Cfg"])
		var rep tabletext.Report
		if ok, err := tablewire.Decode(newM, inst, written, &rep); !ok || err != nil {
			t.Fatalf("oracle decode across the rename: ok=%v err=%v", ok, err)
		}
		if !rep.Silent() {
			t.Fatalf("the oracle reads the renamed generation with a report %+v; the fixture is wrong", rep)
		}
		resaved, err := tablewire.Encode(newM, inst)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "old.bin"), written)
		writeFile(t, filepath.Join(dir, "resaved.bin"), resaved)
		runProbe(t, dir, renameProbe, "old.bin", "resaved.bin")
	})
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// oracleEncode is the Go oracle's answer for one JSON text: read it into an
// instance of root (internal/tabletext, §16) and write the form-1 bytes
// (internal/tablewire, §3). A text the oracle reads with any event is a fixture
// error, not a finding.
func oracleEncode(t *testing.T, m *tabletext.Model, root *ir.Struct, text string) []byte {
	t.Helper()
	inst := m.New(root)
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || !rep.Silent() {
		t.Fatalf("oracle read of %s: report %+v", text, rep)
	}
	b, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// runProbe writes probe.dart beside the generated libraries and runs it under
// the leg's own SDK (dartBinary) with the given arguments.
func runProbe(t *testing.T, dir, probe string, args ...string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "probe.dart"), []byte(probe))
	cmd := exec.Command(dartBinary(t), append([]string{"--enable-asserts", "probe.dart"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not report ok: %s", out)
	}
}

const probeCommon = `
void check(bool ok, String msg) {
  if (!ok) {
    stderr.writeln('FAIL: $msg');
    exit(1);
  }
}

void same(List<int> got, List<int> want, String what) {
  if (got.length != want.length) {
    check(false, '$what: length ${got.length} != ${want.length}');
  }
  for (var i = 0; i < want.length; i++) {
    if (got[i] != want[i]) check(false, '$what: differs from the oracle at byte $i');
  }
}
`

// fleetProbe drives the two generations of the W pair against the committed
// oracle bytes and texts (testdata/wire/tables, testdata/conformance/tables).
const fleetProbe = `import 'dart:io';
import 'Tblw1Wire.dart' as w1;
import 'Tblw2Wire.dart' as w2;
` + probeCommon + `
void silent(int unknown, int kind, int widened, int clamped, bool malformed, String what) {
  check(unknown == 0 && kind == 0 && widened == 0 && clamped == 0 && !malformed,
      '$what: report $unknown,$kind,$widened,$clamped,$malformed is not silent');
}

void main(List<String> args) {
  final fleet = File(args[0]).readAsBytesSync();
  final dflt = File(args[1]).readAsBytesSync();
  final fleet2 = File(args[2]).readAsBytesSync();
  final text1 = File(args[3]).readAsStringSync();
  final text2 = File(args[4]).readAsStringSync();

  // the fixed table Vessel by pointer, as a counted pointer list and by value
  final r1 = w1.TableWireReport();
  final v = w1.fleetWireLoad(fleet, r1);
  silent(r1.unknown, r1.kindMismatch, r1.widened, r1.clamped, r1.malformed, 'w1 reads w1_fleet');
  same(w1.fleetWireSave(v), fleet, 'w1 re-save of w1_fleet');
  check(v.flagship != null && v.flagship!.name == 'Aurora' && v.flagship!.hull == 250, 'flagship fields');
  check(v.flagship!.caps == 2 && v.flagship!.tag == 'zzq' && v.flagship!.badge.label == 'vip', 'flagship flags, bytes and type member');
  check(v.escorts.length == 2 && identical(v.escorts[1], v.flagship), 'the shared pointer is one node');

  // DEFAULTS (SPEC §4.2): absent fields read as the declared default, a fresh
  // record saves as the oracle's all-default bytes
  final d = w1.fleetWireLoad(dflt);
  check(d.title == 'fleet', 'string default title: ${d.title}');
  check(d.home.name == 'untitled' && d.home.tag == 'ab' && d.home.caps == 5 && d.home.hull == 100,
      'the fixed table member reads its string, bytes, flags and int defaults');
  check(d.home.badge.label == 'new', 'a type member reads its string default');
  check(d.flagship == null && d.escorts.isEmpty, 'pointer and list read empty');
  same(w1.fleetWireSave(d), dflt, 'w1 re-save of w1_fleet_default');
  same(w1.fleetWireSave(w1.Fleet()), dflt, 'a fresh Fleet saves as the all-default oracle');

  // the text form, both ways
  same(w1.fleetWireSave(w1.fleetFromJson(text1)), fleet, 'w1 json-read of w1_fleet');
  check(w1.fleetToJson(v) == text1, 'w1 json-write of w1_fleet differs from the oracle text');

  // THE TABLE RENAME (§5): W2's Ship was Vessel; a W1 fleet reads in silence
  final r2 = w2.TableWireReport();
  final u = w2.fleetWireLoad(fleet, r2);
  silent(r2.unknown, r2.kindMismatch, r2.widened, r2.clamped, r2.malformed, 'w2 reads w1_fleet');
  same(w2.fleetWireSave(u), fleet2, 'w2 re-save of w1_fleet is the w2 oracle');
  same(w2.fleetWireSave(w2.fleetFromJson(text2)), fleet2, 'w2 json-read of w2_fleet');
  check(w2.fleetToJson(w2.fleetWireLoad(fleet2)) == text2, 'w2 json-write of w2_fleet differs from the oracle text');
  final r3 = w1.TableWireReport();
  w1.fleetWireLoad(fleet2, r3);
  silent(r3.unknown, r3.kindMismatch, r3.widened, r3.clamped, r3.malformed, 'w1 reads w2_fleet');
  stdout.writeln('ok');
}
`

// defaultsDecl spells each default the Dart literal has to escape: the quote,
// the backslash-free interpolation sigil and a non-ASCII rune.
const defaultsDecl = `package defs

flags Perm { Read, Write, Own }

type Tag
{
    label string(16) = "it's $5 é"
}

table Defs
{
    name  string(32) = "untitled"
    tag   bytes(4) = "ab"
    perm  Perm = { Read, Own }
    inner Tag
    count int32 = 7
}
`

const defaultsEmptyJson = `{"name": "", "tag": "", "perm": [], "inner": {"label": ""}, "count": 0}`

const defaultsSetJson = `{"name": "named", "tag": "eHl6", "perm": ["Write"], "inner": {"label": "x"}, "count": 9}`

const defaultsProbe = `import 'dart:io';
import 'DefsWire.dart';
` + probeCommon + `
void main(List<String> args) {
  final fresh = File(args[0]).readAsBytesSync();
  final empty = File(args[1]).readAsBytesSync();
  final set = File(args[2]).readAsBytesSync();
  final emptyText = File(args[3]).readAsStringSync();
  final setText = File(args[4]).readAsStringSync();

  final v = Defs();
  check(v.name == 'untitled' && v.tag == 'ab' && v.perm == 5 && v.count == 7, 'storage starts at the defaults');
  check(v.inner.label == "it's \$5 é", 'the escaped string default survives the literal: ${v.inner.label}');
  same(defsWireSave(v), fresh, 'a fresh record elides every default');
  final d = defsWireLoad(fresh);
  check(d.name == 'untitled' && d.tag == 'ab' && d.perm == 5 && d.inner.label == "it's \$5 é",
      'absent fields read as the defaults');

  final e = defsFromJson(emptyText);
  check(e.name == '' && e.tag == '' && e.perm == 0 && e.inner.label == '', 'the text reads non-default empties');
  same(defsWireSave(e), empty, 'a non-default empty value rides');
  final e2 = defsWireLoad(empty);
  check(e2.name == '' && e2.tag == '' && e2.perm == 0 && e2.inner.label == '' && e2.count == 0,
      'non-default empties read back');

  final s = defsFromJson(setText);
  same(defsWireSave(s), set, 'every field away from its default');
  final s2 = defsWireLoad(set);
  check(s2.name == 'named' && s2.tag == 'xyz' && s2.perm == 2 && s2.inner.label == 'x' && s2.count == 9,
      'set values read back');
  stdout.writeln('ok');
}
`

const renameOldDecl = `package rold

enum Grade { Bronze, Silver, Gold }

type Buff
{
    multiplier float32 = 1.0
}

table Cfg
{
    grade  Grade
    buff   Buff
    grades [..4]Grade
}
`

// renameNewDecl renames the variant and the type's field under `was`, so each
// id is the old name's hash (§5): an old record reads in silence.
const renameNewDecl = `package rnew

enum Grade
{
    Bronze,
    Argent | was = "Silver"
    Gold
}

type Buff
{
    mult float32 = 1.0 | was = "multiplier"
}

table Cfg
{
    grade  Grade
    buff   Buff
    grades [..4]Grade
}
`

const renameOldJson = `{"grade": "Silver", "buff": {"multiplier": 3.5}, "grades": ["Gold", "Silver"]}`

const renameProbe = `import 'dart:io';
import 'RnewWire.dart';
` + probeCommon + `
void main(List<String> args) {
  final old = File(args[0]).readAsBytesSync();
  final want = File(args[1]).readAsBytesSync();
  final r = TableWireReport();
  final v = cfgWireLoad(old, r);
  check(r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed,
      'the renamed generation reads the old record with a report');
  check(v.grade == 2, 'Silver reads as Argent (ordinal 2), got ${v.grade}');
  check(v.buff.mult == 3.5, 'multiplier reads as mult, got ${v.buff.mult}');
  check(v.grades.length == 2 && v.grades[0] == 3 && v.grades[1] == 2, 'the enum list reads across the rename');
  same(cfgWireSave(v), want, 're-save across the rename');
  stdout.writeln('ok');
}
`
