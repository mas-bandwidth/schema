package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// §5.8 row cfloat_range_widen: the OLD writer's aim range is [-1,1], the NEW
// reader's is [-2,2] — the range WIDENED. A compressed float is held to the
// WRITER's range the lineage plan carries, like every ranged kind
// (docs/rulings.json strict-only-read, docs/FIXED-FORM-ALGORITHM.md §4.6).
// old_ is the control (the writer's range is NARROWER than the reader's) and
// lands the writer's 0.5 exactly; hostile_ is forged to 1.5 (outside the OLD
// writer's bound, inside the NEW reader's) and refuses the whole batch
// out_of_range, which is what says the bound is the writer's; past_ is forged
// to 5.0 (outside both) and refuses whole too. Deleting the lineage entry's
// writer pass (fixed_clamp_known_N) turns hostile_ red while past_ stays green
// on the reader's own bound.

func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	corpus := versionCorpus(t)
	// THE ONE VALUE THAT LANDS COMES FROM THE CORPUS MANIFEST: `old_`'s
	// `values=r0.aim`, compared by its float32 bits.
	oldAim := versionManifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim")

	tests := fmt.Sprintf(`
/// §5.8 row cfloat_range_widen, NEW-READS-OLD across THREE corpus files. A
/// compressed float is held to the WRITER's range the lineage plan carries
/// (strict-only-read, FIXED-FORM-ALGORITHM §4.6). old_ lands the writer's 0.5
/// exactly: the OLD writer's [-1,1] is narrower than the reader's [-2,2].
/// hostile_ is the old bytes with aim forged to 1.5 — outside the OLD writer's
/// bound, inside the NEW reader's — and refuses whole as out_of_range. past_
/// is forged to 5.0, outside BOTH, and refuses whole as out_of_range.
#[test]
fn v_cfloat_range_widen_new_reads_old() {
    // old_cfloat_range_widen.bin: the OLD writer, aim = 0.5, on the old grid.
    let old_data = std::fs::read(%[1]q).expect("the reference's old_cfloat_range_widen.bin");
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
        assert!(!report.damaged, "old_: a clean backward read is not damage: {:?}", report);
        assert_eq!(values[0].lead, 1, "old_: the lead bracket moved: lead={}", values[0].lead);
        assert_eq!(values[0].trail, 2, "old_: the trail bracket moved: trail={}", values[0].trail);
        assert_eq!(values[0].aim.to_bits(), %[4]d, "old_: the reader lands the writer's own value BIT-EXACT, from the corpus manifest: aim={:#x}", values[0].aim.to_bits());
        assert!(
            report.unknown == 0 && report.widened == 0,
            "old_: counters moved on a clean backward read: {:?}", report
        );
    }

    // hostile_cfloat_range_widen.bin: the old bytes with aim FORGED to 1.5,
    // outside the OLD writer's [-1,1], inside the NEW reader's [-2,2].
    let hostile_data = std::fs::read(%[2]q).expect("the reference's hostile_cfloat_range_widen.bin");
    {
        let mut values = vec![ROWTYPE::default(); 8];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = LOADFN(&mut values, &hostile_data, &mut plan, &mut remap, &mut report);
        // STRICT-ONLY: a value past THE WRITER'S range refuses the whole batch
        // as out_of_range, though it lies inside this reader's own.
        assert!(n.is_none(), "hostile_: 1.5 past the writer's [-1,1] was read, not refused: {:?}", report);
        assert!(
            report.refused && report.reason == TableFixedReason::OutOfRange,
            "hostile_: 1.5 past the writer's [-1,1] is an out_of_range refusal: {:?}", report
        );
        assert!(!report.damaged, "hostile_: a refusal is not damage: {:?}", report);
    }

    // past_cfloat_range_widen.bin: the old bytes with aim FORGED to 5.0,
    // outside the NEW reader's [-2,2] too.
    let past_data = std::fs::read(%[3]q).expect("the reference's past_cfloat_range_widen.bin");
    {
        let mut values = vec![ROWTYPE::default(); 8];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = LOADFN(&mut values, &past_data, &mut plan, &mut remap, &mut report);
        // STRICT-ONLY: a value past both ranges refuses the whole batch as
        // out_of_range.
        assert!(n.is_none(), "past_: 5.0 past the reader's [-2,2] was read, not refused: {:?}", report);
        assert!(
            report.refused && report.reason == TableFixedReason::OutOfRange,
            "past_: 5.0 past the reader's [-2,2] is an out_of_range refusal: {:?}", report
        );
        assert!(!report.damaged, "past_: a refusal is not damage: {:?}", report);
    }
}
`, filepath.Join(corpus, "old_cfloat_range_widen.bin"),
		filepath.Join(corpus, "hostile_cfloat_range_widen.bin"),
		filepath.Join(corpus, "past_cfloat_range_widen.bin"), oldAim)

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
