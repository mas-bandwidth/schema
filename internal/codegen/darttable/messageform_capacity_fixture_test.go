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
// batch_too_large, the sentinel untouched. Storage for two owes 2 and 40 in
// both. A caller's capacity is a resource limit (§6.5), so this row runs here,
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
  check(count == 0 && rep.refused && rep.reason == 'batch_too_large' && one[0].clientBuild == 17,
      'storage for one: count=$count refused=${rep.refused} reason=${rep.reason} clientBuild=${one[0].clientBuild}');
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
