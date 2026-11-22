package rusttable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestRustMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of
// tables/backend, client_build 40, written by the Go engine and pinned to it
// by test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated Rust's login_request_load_messages. Storage for one
// body whose client_build is the sentinel 17 owes "A refused batch returns a
// count of `0`" and "refuses before it decodes anything"
// (docs/SPEC-TABLES.md §3.3): (0, false), the sentinel untouched. Storage for
// two owes (2, true) and 40. A caller's capacity is a resource limit (§6.5),
// so this row runs here, in the leg's own tests, never in the shared suites.
func TestRustMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := generateUnit(t, root, []string{"tables/backend"})
	out := runMessageBatchProbe(t, u, fmt.Sprintf(rustCapacityFixtureMain, len(fixture), rustBytes(fixture)))
	if line := messageBatchLine(out, "capacity_fixture"); line != "BATCH capacity_fixture ok" {
		t.Fatalf("capacity fixture: %s", line)
	}
}

const rustCapacityFixtureMain = `
    const WIRE: [u8; %d] = [%s];
    let mut storage = vec![TableMessageEntry::default(); 4096];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut arep = TableReport::default();
    assert!(announce_read(&mut vocab, &TABLE_ANNOUNCEMENT, &mut arep), "the unit's own announcement reads");
    let mut one = vec![LoginRequestRow::default(); 1];
    one[0].client_build = 17;
    let mut rep = TableReport::default();
    let (count, ok) = login_request_load_messages(&mut one, &vocab, &WIRE, &mut rep);
    let refused = !ok && count == 0 && rep.refused && one[0].client_build == 17;
    let mut two = vec![LoginRequestRow::default(); 2];
    let mut loaded = TableReport::default();
    let (got, ok2) = login_request_load_messages(&mut two, &vocab, &WIRE, &mut loaded);
    let delivered = ok2 && got == 2 && two[0].client_build == 40 && two[1].client_build == 40;
    say("capacity_fixture", refused && delivered,
        format!("storage for one: count={count} ok={ok} client_build={} report={rep:?}; storage for two: count={got} ok={ok2}", one[0].client_build));
`
