package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// §5.8 row 11, unknown_census: the NEW reader (Item.drop REMOVED) reads the OLD
// writer's lawful old_unknown_census.bin through the handed-in lineage entry.
// Four elements each carry a and the removed drop, but drop is ONE field of ONE
// peer (Item), so the compile census counts it ONCE: unknown == 1, and a leg
// that counts once per ELEMENT lands 4 and passes a >= 1 read. The check
// asserts == 1 exactly, never >= 1.

func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// §5.8 row 11, unknown_census: the NEW reader (Item.drop removed) reads the OLD
/// writer's lawful old_unknown_census.bin through the handed-in lineage entry.
/// Four elements each carry a and the removed drop, but drop is ONE field of ONE
/// peer (Item), so the compile census counts it ONCE: unknown == 1, and a leg
/// that counts once per ELEMENT would land 4 and pass a >= 1 read.
#[test]
fn v_unknown_census_new_reads_old() {
    let data = std::fs::read(%q).expect("the reference's old_unknown_census.bin");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    let n = n.unwrap_or_else(|| panic!(
        "the newer reader REFUSED the older writer's file: {} {:?}", report.reason.name(), report
    ));
    assert_eq!(n, 1, "the unknown_census file carries one record, not {n}");
    assert!(
        !report.refused && report.reason == TableFixedReason::None,
        "a clean NEW-READS-OLD is not a refusal: {:?}", report
    );
    assert!(!report.malformed, "a clean backward read is not damage: {:?}", report);
    assert_eq!(
        report.unknown, 1,
        "unknown == {}, want 1: Item.drop is ONE field of ONE peer, not one per element", report.unknown
    );
    assert!(
        report.kind_mismatch == 0 && report.widened == 0 && report.clamped == 0,
        "counters moved on a clean backward read: {:?}", report
    );
    assert_eq!(values[0].lead, 1, "the lead bracket moved: lead={}", values[0].lead);
    assert_eq!(values[0].trail, 2, "the trail bracket moved: trail={}", values[0].trail);
    assert!(
        values[0].items[0].a == 10 && values[0].items[1].a == 11
            && values[0].items[2].a == 12 && values[0].items[3].a == 13,
        "the four a values did not land: {} {} {} {}",
        values[0].items[0].a, values[0].items[1].a, values[0].items[2].a, values[0].items[3].a
    );
}
`, filepath.Join(corpus, "old_unknown_census.bin"))

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
		name:   "probe_unknown_census",
		reader: "VNEW_unknown_census",
		older:  []string{"VOLD_unknown_census"},
		src:    versionProbeSource(tests),
	})

	cmd := exec.Command("cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_unknown_census")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_unknown_census_new_reads_old")
}
