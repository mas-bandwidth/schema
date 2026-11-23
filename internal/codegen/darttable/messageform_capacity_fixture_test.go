package darttable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestDartMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated Dart's loginRequestLoadMessages. Storage for one body
// whose clientBuild is the sentinel 17 owes "A refused batch returns a count of
// `0`" and "refuses before it decodes anything" (docs/SPEC-TABLES.md §3.3): 0,
// batch_too_large, and (R4) every value handed in at its declared default, not
// the sentinel. Storage for THREE sentinel values owes the same 0 and all three
// at their defaults after a framing refusal (the form byte changed), a body
// refusal (the last byte dropped) and a close refusal (a byte past the pad):
// slot 2 is past the batch's count, so only a reset of the whole list reaches
// it. Storage for two owes 2 and 40 in both. A caller's capacity is a resource limit (§6.5), so this row runs here,
// in the leg's own tests, never in the shared suites.
func TestDartMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	dartBinary(t)
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, "../../../tables/backend/Backend.schema")
	body := fmt.Sprintf(`
  final wire = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  check(h.announceRead(vocab, h.tableAnnouncement, h.TableReport()), 'announce');
  final one = <h.LoginRequest>[h.LoginRequest()..clientBuild = 17];
  final rep = h.TableReport();
  final count = r.loginRequestLoadMessages(one, wire, vocab, rep);
  check(count == 0 && rep.refused && rep.reason == 'batch_too_large' && one[0].clientBuild == h.LoginRequest().clientBuild,
      'storage for one: count=$count refused=${rep.refused} reason=${rep.reason} clientBuild=${one[0].clientBuild}');
  final defaultBuild = h.LoginRequest().clientBuild;
  check(defaultBuild != 17, 'the sentinel is the default');
  final damaged = <String, Uint8List>{
    'framing': Uint8List.fromList(wire)..[0] = 3,
    'body': Uint8List.sublistView(wire, 0, wire.length - 1),
    'close': Uint8List.fromList(<int>[...wire, 0]),
  };
  for (final name in damaged.keys) {
    final three = <h.LoginRequest>[
      h.LoginRequest()..clientBuild = 17,
      h.LoginRequest()..clientBuild = 17,
      h.LoginRequest()..clientBuild = 17,
    ];
    final rep3 = h.TableReport();
    final got3 = r.loginRequestLoadMessages(three, damaged[name]!, vocab, rep3);
    check(got3 == 0 && rep3.refused && three.every((x) => x.clientBuild == defaultBuild),
        '$name refusal: count=$got3 refused=${rep3.refused} clientBuild=${three.map((x) => x.clientBuild)}');
  }
  final two = <h.LoginRequest>[h.LoginRequest(), h.LoginRequest()];
  final loaded = h.TableReport();
  final got = r.loginRequestLoadMessages(two, wire, vocab, loaded);
  check(got == 2 && !loaded.refused && two[0].clientBuild == 40 && two[1].clientBuild == 40,
      'storage for two: count=$got reason=${loaded.reason}');
`, dartBytes(fixture))
	out, err := runBatchProbe(t, u, "LoginRequest", body)
	if err != nil {
		t.Fatalf("capacity fixture: %v\n%s", err, out)
	}
}
