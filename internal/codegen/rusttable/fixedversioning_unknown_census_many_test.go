package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// §5.4: the compile census lands ONCE PER PEER AND NEVER PER RECORD. The landed
// one-record row reads old_unknown_census.bin, which holds ONE record, where
// once-per-peer and once-per-record are both 1 and the second half of the
// clause is never gated. many_unknown_census.bin holds THREE records of the
// same Census root: a per-record Unknown++ would land 3 there, so the unknown
// counter is asserted == 1 exactly, and never >= 1.

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// §5.4, the MANY half: the compile census lands once per peer, never per
/// record. The landed row reads old_unknown_census.bin (ONE record), where the
/// two counts are both 1. This probe reads many_unknown_census.bin (THREE
/// records of the same Census root): Item.drop is ONE field of ONE peer (Item),
/// so the census counts it ONCE across the whole file — unknown == 1 — while a
/// reader that counts once per RECORD lands 3. All three records, each with its
/// own distinct a values, must come back intact.
#[test]
fn v_unknown_census_many_new_reads_old() {
    let data = std::fs::read(%q).expect("the reference's many_unknown_census.bin");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    let n = n.unwrap_or_else(|| panic!(
        "the newer reader REFUSED the older writer's file: {} {:?}", report.reason.name(), report
    ));
    assert_eq!(n, 3, "the many_unknown_census file carries three records, not {n}");
    assert!(
        !report.refused && report.reason == TableFixedReason::None,
        "a clean NEW-READS-OLD is not a refusal: {:?}", report
    );
    assert!(!report.malformed, "a clean backward read is not damage: {:?}", report);
    assert_eq!(
        report.unknown, 1,
        "unknown == {}, want 1: Item.drop is ONE field of ONE peer, not one per record", report.unknown
    );
    assert!(
        report.kind_mismatch == 0 && report.widened == 0 && report.clamped == 0,
        "counters moved on a clean backward read: {:?}", report
    );
    assert_eq!(values[0].lead, 1, "record 0's lead moved: lead={}", values[0].lead);
    assert_eq!(values[0].trail, 2, "record 0's trail moved: trail={}", values[0].trail);
    assert_eq!(values[1].lead, 1, "record 1's lead moved: lead={}", values[1].lead);
    assert_eq!(values[1].trail, 2, "record 1's trail moved: trail={}", values[1].trail);
    assert_eq!(values[2].lead, 1, "record 2's lead moved: lead={}", values[2].lead);
    assert_eq!(values[2].trail, 2, "record 2's trail moved: trail={}", values[2].trail);
    assert!(
        values[0].items[0].a == 10 && values[0].items[1].a == 11
            && values[0].items[2].a == 12 && values[0].items[3].a == 13,
        "record 0's four a values did not land: {} {} {} {}",
        values[0].items[0].a, values[0].items[1].a, values[0].items[2].a, values[0].items[3].a
    );
    assert!(
        values[1].items[0].a == 20 && values[1].items[1].a == 21
            && values[1].items[2].a == 22 && values[1].items[3].a == 23,
        "record 1's four a values did not land: {} {} {} {}",
        values[1].items[0].a, values[1].items[1].a, values[1].items[2].a, values[1].items[3].a
    );
    assert!(
        values[2].items[0].a == 30 && values[2].items[1].a == 31
            && values[2].items[2].a == 32 && values[2].items[3].a == 33,
        "record 2's four a values did not land: {} {} {} {}",
        values[2].items[0].a, values[2].items[1].a, values[2].items[2].a, values[2].items[3].a
    );
}
`, filepath.Join(corpus, "many_unknown_census.bin"))

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs(filepath.Join(repo, "..", "serialize.rs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(serialize); err != nil {
		t.Fatalf("the Rust runtime is not beside this tree at %s: %v", serialize, err)
	}

	root := t.TempDir()
	versionWriteCrate(t, root, serialize, versionProbe{
		name:   "probe_unknown_census_many",
		reader: "VNEW_unknown_census",
		older:  []string{"VOLD_unknown_census"},
		src:    versionProbeSource(tests),
	})

	cmd := exec.Command("cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_unknown_census_many")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_unknown_census_many_new_reads_old")
}
