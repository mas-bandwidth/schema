package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
)

// TestDartHostileCorpusReaderFixes pins the Dart reader fixes uncovered by
// schema-dart-runs-hostile-corpus:
//  1. tableMessageShapeEnd accepts a 64-bit ranged base whose top bit is set
//     (0xffffffffffffffff) so a second announcement on the same connection is
//     refused as second_announcement rather than malformed.
//  2. messageRides and emitLoadMessageBody honour f.Type.Optional on fixed
//     arrays (?[N]T) so an absent optional fixed array elides on the message
//     wire.
//  3. Wire map and unbounded list readers refuse a count past 0x7fffffff as
//     refused rather than damaged.
//  4. Text reader leaves a scalar field at its default or prior duplicate-key
//     value when a value is rejected as kindMismatch, and leaves union armTag
//     at None when the arm payload has the wrong JSON shape.
//  5. Text float32 reader and writer round at single precision across the
//     subnormal midpoint (2^-150) and the upper band below 2^128 - 2^103.
func TestDartHostileCorpusReaderFixes(t *testing.T) {
	t.Parallel()
	const schema = `package probe
enum Mode { Fast, Slow }
union Choice {
    flag
    score uint16
    nested Inner
}
table Inner {
    x int32 = 7
}
fixed table Msg {
    marks ?[2]Mode
}
table Root {
    version uint16 = 5
    ratio float32
    choice Choice
    by_mode [Mode]uint16
    entries map[int32]int32
}
`
	u := unitFrom(t, schema)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range tables {
		files[k] = v
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const probe = `import 'dart:typed_data';
import 'ProbeFixed.dart' as fix;
import 'ProbeWire.dart' as w;

void check(bool ok, String why) {
  if (!ok) throw StateError(why);
}

void main() {
  // 1. Optional fixed array elides when absent in message form.
  final vocab = fix.TableVocabulary(
      entries: List<fix.TableMessageEntry>.generate(64, (_) => fix.TableMessageEntry(0, 0)));
  final ar = fix.TableReport();
  check(fix.announceRead(vocab, fix.tableAnnouncement, ar), 'announceRead');
  final ar2 = fix.TableReport();
  check(!fix.announceRead(vocab, fix.tableAnnouncement, ar2) && ar2.reason == 'second_announcement',
      'second_announcement: ${ar2.reason}');

  final mIn = [fix.Msg()];
  final mBytes = Uint8List(fix.msgMeasureMessages(mIn, fix.TableReport()));
  final mWrote = fix.msgSaveMessages(mIn, mBytes, fix.TableReport());
  check(mWrote == 3 && mBytes[0] == 2 && mBytes[1] == 0 && mBytes[2] == 0,
      'absent ?[2]Mode must elide to 02 00 00, got $mWrote bytes');

  // 2. Wire map count past 0x7fffffff refuses with refused=true.
  final wr = w.TableWireReport();
  w.rootWireLoad(Uint8List.fromList([
    0x01, 0x01, 0x0e, 0x08, 0x0d, 0x80, 0x80, 0x80, 0x80, 0x08, 0x01, 0x00, 0x00,
    0x53, 0xa2, 0x45, 0x08, 0x2c, 0xa7, 0xb2, 0xc5, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00
  ]), wr);
  check(wr.refused && !wr.malformed, 'count refusal: refused=${wr.refused} malformed=${wr.malformed}');

  // 3. Null on scalar and rejected duplicate keys preserve defaults / earlier valid values.
  final tr1 = w.TableTextReport();
  final r1 = w.rootFromJson('{"version": null}', tr1);
  check(r1.version == 5 && tr1.kindMismatch == 1, 'null scalar kept default: ${r1.version}');

  final tr2 = w.TableTextReport();
  final r2 = w.rootFromJson('{"version": 42, "version": "bad", "by_mode": {"Fast": 9, "Fast": 1.5}}', tr2);
  check(r2.version == 42 && r2.byMode[0] == 9 && tr2.duplicate == 2 && tr2.kindMismatch == 2,
      'dup key rejected: version=${r2.version} byMode=${r2.byMode[0]}');

  // 4. Union arm with wrong shape leaves armTag == 0 (None).
  final tr3 = w.TableTextReport();
  final r3 = w.rootFromJson('{"choice": {"score": "bad"}}', tr3);
  check(r3.choice.armTag == 0 && tr3.kindMismatch == 1, 'union wrong shape armTag=${r3.choice.armTag}');
  final r4 = w.rootFromJson('{"choice": {"flag": 1}}', w.TableTextReport());
  check(r4.choice.armTag == 0, 'void arm non-null armTag=${r4.choice.armTag}');
  final r5 = w.rootFromJson('{"choice": {"nested": 1}}', w.TableTextReport());
  check(r5.choice.armTag == 0, 'table arm non-map armTag=${r5.choice.armTag}');

  // 5. Float32 subnormal midpoint and upper band round at single precision.
  final rSub = w.rootFromJson('{"ratio": 7.0064923216240853547e-46}');
  check(w.rootToJson(rSub).contains('"ratio": 1.4013e-45'), 'subnormal: ${w.rootToJson(rSub)}');
  final rUpper = w.rootFromJson('{"ratio": 340282356779733661637539395458142568447}');
  check(w.rootToJson(rUpper).contains('"ratio": 3.4028235e+38'), 'upper: ${w.rootToJson(rUpper)}');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(announceDartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
}
