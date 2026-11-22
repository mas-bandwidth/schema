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

// TestDartReportsAndWriteRefusalsMatchGo holds the Dart port to Go where it
// had drifted (each row red on the old generator):
//
//   - FromJson and WireLoad zero the whole report at the start of every read,
//     so unknown, widened and retained never add up across calls.
//   - FromJson answers a refusal with a reason, never a FormatException, for
//     bad grammar, a repeated key, depth past 128, U+0000, a lone surrogate and
//     a reserved '&' key (docs/SPEC-TABLES.md §16.2, §16.3).
//   - ToJson and WireSave refuse a ranged value off its range with a DEBUG
//     ASSERT, an AssertionError, and not a StateError in every build (docs/SPEC.md §5.1, the write table);
//     ToJson holds a PRESENT optional to its range as well.
//   - SaveMessages measures before it writes: a buffer that is too short
//     answers -1 with every byte untouched (docs/SPEC-TABLES.md §3.3).
func TestDartReportsAndWriteRefusalsMatchGo(t *testing.T) {
	slowtest.Gate(t, "dart")
	const schema = `package probe

table Wired {
    a int32
    s string(8)
}

table Ranged {
    plain int32 | max = 10
    req int32 | max = 10, required
    opt ?int32 | max = 10
}

table Needs {
    a int32 | required
}

table Lists {
    names [..2]string(4)
    open []string
    by map[string]int32
}

fixed table Msg {
    a int32
    b int32
}
`
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	const probe = `import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeFixed.dart' as f;

void check(bool b, String s) { if (!b) throw StateError(s); }

void main() {
  // 1. the text report is zeroed in full by every read
  {
    final r = w.TableTextReport();
    w.wiredFromJson('{"a":1,"zz":2}', r);
    check(r.unknown == 1, 'first unknown: ${r.unknown}');
    w.wiredFromJson('{"a":1,"zz":2}', r);
    check(r.unknown == 1, 'unknown added up: ${r.unknown}');
    r.refuse('stale');
    w.wiredFromJson('{"a":1}', r);
    check(!r.refused && r.reason == 'ok' && r.unknown == 0, 'stale refusal survived: ${r.reason}');
  }

  // 2. the wire report is zeroed in full by every read
  {
    final v = w.Wired()..a = 3;
    final wire = w.wiredWireSave(v);
    final r = w.TableWireReport()..unknown = 5..widened = 3..retained = 2..retainLost = 1;
    final back = w.wiredWireLoad(wire, r);
    check(back.a == 3 && !r.refused, 'wire load');
    check(r.unknown == 0 && r.widened == 0 && r.retained == 0 && r.retainLost == 0,
        'wire counts added up: ${r.unknown} ${r.widened} ${r.retained} ${r.retainLost}');
  }

  // 3. a bad text is a refusal with a reason, not an exception
  {
    final deep = '{"zz":' + ('[' * 200) + (']' * 200) + '}';
    final cases = <String, String>{
      'grammar': '{"a":1 "s":"x"}',
      'repeated key': '{"a":1,"a":2}',
      'depth': deep,
      'U+0000': '{"s":"\\u0000"}',
      'lone surrogate': '{"s":"\\ud800"}',
      'reserved key': '{"&x":1}',
    };
    final reasons = <String, String>{
      'grammar': 'text_damaged',
      'repeated key': 'text_damaged',
      'depth': 'text_damaged',
      'U+0000': 'text_holds_zero',
      'lone surrogate': 'string_lone_surrogate',
      'reserved key': 'text_damaged',
    };
    cases.forEach((name, text) {
      final r = w.TableTextReport();
      try {
        w.wiredFromJson(text, r);
      } on FormatException catch (e) {
        throw StateError('$name threw FormatException: ${e.message}');
      }
      check(r.refused, '$name did not refuse');
      check(r.reason == reasons[name], '$name reason: ${r.reason}');
    });
  }

  // 4. a ranged value off its range is a debug assert, an AssertionError
  {
    var asserted = false;
    try { w.rangedToJson(w.Ranged()..plain = 11); } on AssertionError { asserted = true; }
    check(asserted, 'plain ToJson off range must assert');
    asserted = false;
    try { w.rangedWireSave(w.Ranged()..plain = 11); } on AssertionError { asserted = true; }
    check(asserted, 'plain WireSave off range must assert');
    asserted = false;
    try { w.rangedWireSave(w.Ranged()..req = 11); } on AssertionError { asserted = true; }
    check(asserted, 'required WireSave off range must assert');
    asserted = false;
    try { w.rangedToJson(w.Ranged()..req = 11); } on AssertionError { asserted = true; }
    check(asserted, 'required ToJson off range must assert');
    // a PRESENT optional is held to its range; an absent one is not
    asserted = false;
    try { w.rangedToJson(w.Ranged()..hasOpt = true..opt = 11); } on AssertionError { asserted = true; }
    check(asserted, 'present optional ToJson off range must assert');
    w.rangedToJson(w.Ranged()..hasOpt = false..opt = 11);
    w.rangedToJson(w.Ranged()..hasOpt = true..opt = 10);
  }

  // 5. SaveMessages measures first: a short buffer is refused untouched
  {
    final values = [f.Msg()..a = 1..b = 2, f.Msg()..a = 3..b = 4];
    final report = f.TableReport();
    final need = f.msgMeasureMessages(values, report);
    check(need > 3, 'measure: $need');
    final short = Uint8List(need - 1)..fillRange(0, need - 1, 0xa5);
    check(f.msgSaveMessages(values, short, report) == -1, 'short buffer must answer -1');
    check(short.every((x) => x == 0xa5), 'short buffer was written');
    final full = Uint8List(need);
    check(f.msgSaveMessages(values, full, report) == need, 'full buffer');
  }
  // 6. a missing required field is a refusal with a reason, never a throw
  {
    final r = w.TableTextReport();
    try {
      w.needsFromJson('{}', r);
    } on FormatException catch (e) {
      throw StateError('missing_required threw: ${e.message}');
    }
    check(r.refused && r.reason == 'missing_required: a', 'missing_required reason: ${r.reason}');
    final ok = w.TableTextReport();
    w.needsFromJson('{"a":1}', ok);
    check(!ok.refused, 'present required refused');
  }

  // 7. text-list and unbounded-text write checks are debug asserts
  {
    var asserted = false;
    try { w.listsWireSave(w.Lists()..names = [w.ListsNamesText()..value = 'toolong']); } on AssertionError { asserted = true; }
    check(asserted, 'text list WireSave must assert');
    asserted = false;
    try { w.listsWireSave(w.Lists()..by = {'a\u0000b': 1}); } on AssertionError { asserted = true; }
    check(asserted, 'unbounded text WireSave must assert');
  }
  print('OK');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "OK") {
		t.Fatalf("dart probe: %v\n%s", err, out)
	}
}
