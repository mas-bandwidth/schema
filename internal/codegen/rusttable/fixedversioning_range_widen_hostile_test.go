package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vector every leg reads. hostile_range_widen.bin is the
// OLD writer's | 0..100 file with `v` forged to 150, inside the NEW reader's
// | 0..200; the NEW reader clamps it to the WRITER's max and counts `clamped`
// once. The answer is the manifest's `expect=`, which is what the C++
// reference's own NEW reader landed from the same bytes.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "cargo (it runs the generated NEW reader over the forged file)")
	corpus := versionCorpus(t)
	want, err := fixedvector.Read(corpus, fixedvector.RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	v, err := want.Int("r0.v")
	if err != nil {
		t.Fatal(err)
	}
	lead, err := want.Int("r0.lead")
	if err != nil {
		t.Fatal(err)
	}
	trail, err := want.Int("r0.trail")
	if err != nil {
		t.Fatal(err)
	}
	tests := fmt.Sprintf(`
#[test]
fn v_range_widen_hostile() {
    let data = std::fs::read(%[1]q).expect("the reference's hostile_range_widen.bin");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    println!("range_widen_hostile: landed v={} clamped={}", values[0].v, report.clamped);
    let n = n.unwrap_or_else(|| panic!("the NEW reader REFUSED the forged file: {} {:?}", report.reason.name(), report));
    assert_eq!(n, 1, "the forged file carries one record, not {n}");
    assert!(!report.refused && !report.malformed, "a clamped read is not a refusal: {:?}", report);
    assert_eq!((values[0].v, report.clamped), (%[2]d, %[3]d), "the forged 150 clamps to the WRITER's max and counts once");
    assert_eq!((values[0].lead, values[0].trail), (%[4]d, %[5]d), "a bracket moved");
    assert!(
        report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0,
        "a counter that must not move did: {:?}", report
    );
}
`, filepath.Join(corpus, fixedvector.RangeWidenHostile), v, want.Clamped, lead, trail)

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
		name:   "probe_range_widen_hostile",
		reader: "VNEW_range_widen",
		older:  []string{"VOLD_range_widen"},
		src:    versionProbeSource(tests),
	})
	cmd := exec.Command("cargo", "test", "--", "--nocapture")
	cmd.Dir = filepath.Join(root, "probe_range_widen_hostile")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("range_widen_hostile: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_range_widen_hostile")
	t.Logf("%s", out)
}
