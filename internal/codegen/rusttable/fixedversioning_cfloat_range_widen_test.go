package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// §5.8 row cfloat_range_widen: the OLD writer's aim range is [-1,1], the NEW
// reader's is [-2,2] — the range WIDENED. A compressed float rides as the
// float32 ITSELF (SPEC §3.4), so the old bound is nowhere on the wire and the
// bounds pass on a READ is THE READER's. old_ lands the writer's 0.5 whole;
// hostile_ is forged to 1.5 (outside the OLD bound, inside the NEW) and lands
// 1.5 WHOLE, which says the pass is the reader's and not the writer's; past_ is
// forged to 5.0 (outside both) and clamps to the READER's own 2.0 with clamped
// counted EXACTLY 1 — the assertion that proves there is a pass at all, since
// deleting the emitter pass keeps hostile_ green and only past_ goes red.
// clamped is exact, never >= 1, to catch a reader that counts twice.

func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// §5.8 row cfloat_range_widen, NEW-READS-OLD across THREE corpus files. In the
/// fixed form a compressed float rides as the float32 ITSELF (SPEC §3.4), so the
/// OLD writer's [-1,1] bound is nowhere on the wire and the bounds pass on a
/// READ is THE READER'S: one pass over the reader's own [-2,2]. old_ lands the
/// writer's 0.5 whole, clamped == 0. hostile_ is the old bytes with aim forged
/// to 1.5 — outside the OLD bound, inside the NEW — and lands 1.5 WHOLE, clamped
/// == 0, which is what says the pass is the READER's and not the writer's. past_
/// is forged to 5.0, outside BOTH, and clamps to the reader's own 2.0 with
/// clamped == 1 EXACTLY, which is what says there is a pass at all: delete the
/// emitter's pass and hostile_ stays green while only past_ goes red.
#[test]
fn v_cfloat_range_widen_new_reads_old() {
    // old_cfloat_range_widen.bin: the OLD writer, aim = 0.5, on the old grid.
    let old_data = std::fs::read(%q).expect("the reference's old_cfloat_range_widen.bin");
    {
        let mut values = vec![ROWTYPE::default(); 8];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = LOADFN(&mut values, &old_data, &mut plan, &mut remap, &mut report);
        let n = n.unwrap_or_else(|| panic!(
            "old_: the newer reader REFUSED the older writer's file: {} {:?}", report.reason.name(), report
        ));
        assert_eq!(n, 1, "old_ carries one record, not {n}");
        assert!(
            !report.refused && report.reason == TableFixedReason::None,
            "old_: a clean NEW-READS-OLD is not a refusal: {:?}", report
        );
        assert!(!report.malformed, "old_: a clean backward read is not damage: {:?}", report);
        assert_eq!(values[0].lead, 1, "old_: the lead bracket moved: lead={}", values[0].lead);
        assert_eq!(values[0].trail, 2, "old_: the trail bracket moved: trail={}", values[0].trail);
        assert_eq!(values[0].aim.to_bits(), 0x3F000000, "old_: the reader lands the writer's 0.5 EXACTLY: aim={:#x}", values[0].aim.to_bits());
        assert_eq!(report.clamped, 0, "old_: 0.5 sits on the reader's grid, so clamped == 0: {:?}", report);
        assert!(
            report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0,
            "old_: counters moved on a clean backward read: {:?}", report
        );
    }

    // hostile_cfloat_range_widen.bin: the old bytes with aim FORGED to 1.5,
    // outside the OLD writer's [-1,1], inside the NEW reader's [-2,2].
    let hostile_data = std::fs::read(%q).expect("the reference's hostile_cfloat_range_widen.bin");
    {
        let mut values = vec![ROWTYPE::default(); 8];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = LOADFN(&mut values, &hostile_data, &mut plan, &mut remap, &mut report);
        let n = n.unwrap_or_else(|| panic!(
            "hostile_: the newer reader REFUSED the forged file: {} {:?}", report.reason.name(), report
        ));
        assert_eq!(n, 1, "hostile_ carries one record, not {n}");
        assert!(
            !report.refused && report.reason == TableFixedReason::None,
            "hostile_: a clean NEW-READS-OLD is not a refusal: {:?}", report
        );
        assert!(!report.malformed, "hostile_: a clean backward read is not damage: {:?}", report);
        assert_eq!(values[0].lead, 1, "hostile_: the lead bracket moved: lead={}", values[0].lead);
        assert_eq!(values[0].trail, 2, "hostile_: the trail bracket moved: trail={}", values[0].trail);
        assert_eq!(values[0].aim.to_bits(), 0x3FC00000, "hostile_: a forged 1.5 inside the reader's range lands WHOLE: aim={:#x}", values[0].aim.to_bits());
        assert_eq!(report.clamped, 0, "hostile_: 1.5 is inside the reader's [-2,2], so clamped == 0: {:?}", report);
        assert!(
            report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0,
            "hostile_: counters moved on a clean backward read: {:?}", report
        );
    }

    // past_cfloat_range_widen.bin: the old bytes with aim FORGED to 5.0,
    // outside the NEW reader's [-2,2] too.
    let past_data = std::fs::read(%q).expect("the reference's past_cfloat_range_widen.bin");
    {
        let mut values = vec![ROWTYPE::default(); 8];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = LOADFN(&mut values, &past_data, &mut plan, &mut remap, &mut report);
        let n = n.unwrap_or_else(|| panic!(
            "past_: the newer reader REFUSED the forged file: {} {:?}", report.reason.name(), report
        ));
        assert_eq!(n, 1, "past_ carries one record, not {n}");
        assert!(
            !report.refused && report.reason == TableFixedReason::None,
            "past_: a clean NEW-READS-OLD is not a refusal: {:?}", report
        );
        assert!(!report.malformed, "past_: a clean backward read is not damage: {:?}", report);
        assert_eq!(values[0].lead, 1, "past_: the lead bracket moved: lead={}", values[0].lead);
        assert_eq!(values[0].trail, 2, "past_: the trail bracket moved: trail={}", values[0].trail);
        assert_eq!(values[0].aim.to_bits(), 0x40000000, "past_: a forged 5.0 past the reader's own max clamps to the reader's 2.0: aim={:#x}", values[0].aim.to_bits());
        assert_eq!(report.clamped, 1, "past_: 5.0 is past the reader's [-2,2], so it clamps to 2.0 and clamped == 1 EXACTLY: {:?}", report);
        assert!(
            report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0,
            "past_: counters moved on a clean backward read: {:?}", report
        );
    }
}
`, filepath.Join(corpus, "old_cfloat_range_widen.bin"),
		filepath.Join(corpus, "hostile_cfloat_range_widen.bin"),
		filepath.Join(corpus, "past_cfloat_range_widen.bin"))

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
		name:   "probe_cfloat_range_widen",
		reader: "VNEW_cfloat_range_widen",
		older:  []string{"VOLD_cfloat_range_widen"},
		src:    versionProbeSource(tests),
	})

	cmd := exec.Command("cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_cfloat_range_widen")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cfloat_range_widen: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_cfloat_range_widen_new_reads_old")
}
