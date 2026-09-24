package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// §5.8 row cfloat_res_refine: the NEW reader (aim resolution 0.01) reads the OLD
// writer's lawful old_cfloat_res_refine.bin (aim resolution 0.1) through its
// lineage entry. This row takes NO hostile file: the refinement moves the
// DEFINITIONS digest ('Q', bill §13), so the hash moves and the bytes do not. A
// compressed float rides as the float32 ITSELF (SPEC §3.4), so `aim` must land
// the old writer's 0.3 EXACTLY (0x3E99999A) and `clamped` is asserted EXACTLY 0:
// a leg that requantizes the stored value lands a different float, and a merely
// in-range `aim` check would pass the read this row exists to catch.

func TestFixedVersioningCfloatResRefine(t *testing.T) {
	corpus := versionCorpus(t)
	// THE NUMBER COMES FROM THE CORPUS MANIFEST, not from a literal here
	// (schema#1164). This leg already compared BITS, which is the right
	// comparison; what it did not do is take the number from the REFERENCE.
	aim := versionManifestFloatBits(t, corpus, "old_cfloat_res_refine.bin", "values", "r0.aim")

	tests := fmt.Sprintf(`
/// §5.8 row cfloat_res_refine, NEW-READS-OLD: the NEW reader (aim resolution
/// 0.01) reads the OLD writer's lawful old_cfloat_res_refine.bin (aim resolution
/// 0.1) through the handed-in lineage entry. In the fixed form a compressed
/// float rides as the float32 ITSELF (SPEC §3.4), so the resolution is a
/// DEFINITION in the digest, not wire: the stored 0.3 needs no requantizing
/// because 0.1 is a whole multiple of this reader's 0.01, so aim lands EXACTLY
/// and clamped == 0.
#[test]
fn v_cfloat_res_refine_new_reads_old() {
    let data = std::fs::read(%q).expect("the reference's old_cfloat_res_refine.bin");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    let n = n.unwrap_or_else(|| panic!(
        "the newer reader REFUSED the older writer's file: {} {:?}", report.reason.name(), report
    ));
    assert_eq!(n, 1, "the cfloat_res_refine file carries one record, not {n}");
    assert!(
        !report.refused && report.reason == TableFixedReason::None,
        "a clean NEW-READS-OLD is not a refusal: {:?}", report
    );
    assert!(!report.malformed, "a clean backward read is not damage: {:?}", report);
    assert_eq!(values[0].lead, 1, "the lead bracket moved: lead={}", values[0].lead);
    assert_eq!(values[0].trail, 2, "the trail bracket moved: trail={}", values[0].trail);
    assert_eq!(
        values[0].aim.to_bits(), %[2]d,
        "the refined reader must land the old writer's float BIT-EXACT: aim={:#x}, the manifest says {:#x}",
        values[0].aim.to_bits(), %[2]d
    );
    assert_eq!(
        report.clamped, 0,
        "a value on the reader's own grid needs no requantize, so clamped == 0: {:?}", report
    );
    assert!(
        report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0,
        "counters moved on a clean backward read: {:?}", report
    );
}
`, filepath.Join(corpus, "old_cfloat_res_refine.bin"), aim)

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
		name:   "probe_cfloat_res_refine",
		reader: "VNEW_cfloat_res_refine",
		older:  []string{"VOLD_cfloat_res_refine"},
		src:    versionProbeSource(tests),
	})

	cmd := exec.Command("cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_cfloat_res_refine")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cfloat_res_refine: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_cfloat_res_refine_new_reads_old")
}
