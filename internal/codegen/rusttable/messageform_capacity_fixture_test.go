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
// (docs/SPEC-TABLES.md §3.3): (0, false), and (ruling R4) client_build back at
// its declared default. Storage for two owes (2, true) and 40. Storage for
// three, all at the sentinel, owes (0, false) and every slot at its declared
// default after a framing refusal (form byte changed) and after a body/close
// refusal (last byte dropped). A caller's capacity is a resource limit (§6.5),
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
    // RULING R4: the refusal leaves the value handed in at its declared default.
    let dflt = login_request_message_default().client_build;
    let refused = !ok && count == 0 && rep.refused && one[0].client_build == dflt;
    let mut two = vec![LoginRequestRow::default(); 2];
    let mut loaded = TableReport::default();
    let (got, ok2) = login_request_load_messages(&mut two, &vocab, &WIRE, &mut loaded);
    let delivered = ok2 && got == 2 && two[0].client_build == 40 && two[1].client_build == 40;
    // R4 over every value handed in: storage for THREE, all at the sentinel.
    // (a) the form byte changed, a framing refusal before any body; (b) the
    // last byte dropped, a body/close refusal. Slot 2 is past the batch's
    // count of 2, so a 0..count reset would leave it at 17.
    let mut framing = WIRE.to_vec();
    framing[0] = 9;
    let mut three = vec![LoginRequestRow::default(); 3];
    for v in three.iter_mut() { v.client_build = 17; }
    let mut frep = TableReport::default();
    let (fcount, fok) = login_request_load_messages(&mut three, &vocab, &framing, &mut frep);
    let framing_reset = !fok && fcount == 0 && three.iter().all(|v| v.client_build == dflt);
    let short = &WIRE[..WIRE.len() - 1];
    let mut three_b = vec![LoginRequestRow::default(); 3];
    for v in three_b.iter_mut() { v.client_build = 17; }
    let mut brep = TableReport::default();
    let (bcount, bok) = login_request_load_messages(&mut three_b, &vocab, short, &mut brep);
    let body_reset = !bok && bcount == 0 && three_b.iter().all(|v| v.client_build == dflt);
    say("capacity_fixture", refused && delivered && framing_reset && body_reset,
        format!("storage for one: count={count} ok={ok} client_build={} report={rep:?}; storage for two: count={got} ok={ok2}; framing: count={fcount} ok={fok} builds={:?}; body: count={bcount} ok={bok} builds={:?}",
            one[0].client_build,
            three.iter().map(|v| v.client_build).collect::<Vec<_>>(),
            three_b.iter().map(|v| v.client_build).collect::<Vec<_>>()));
`
