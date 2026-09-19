package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// §5.8 row 4 (writer_bound_count): the reader's bound is WIDER than the
// writer's, and the forged file carries a count BETWEEN the two. The NEW build
// reads the forged OLD bytes through its lineage, so the plan is a COMPILED one
// and the count's bound is the WRITER's, carried by the plan — never the
// reader's 8, never the forged 7. The probe forges the count word in Rust over
// the bytes it just read: the lead 0xAAAAAAAA followed by the count 4 is the
// eight-byte locator, and 7 is written over the count.

func TestFixedVersioningWriterBoundCount(t *testing.T) {
	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// writer_bound_count, §5.8 row 4: the reader's bound is WIDER than the
/// writer's, and the forged file carries a count BETWEEN the two. The NEW build
/// reads the forged OLD bytes through its lineage, so the plan is a COMPILED one
/// and the count's bound is the WRITER's, carried by the plan — never the
/// reader's 8, never the forged 7. The forge writes 7 as a little-endian u32
/// over the count word the lead's needle locates.
#[test]
fn v_writer_bound_count_new_reads_old() {
    let mut data = std::fs::read(%q).expect("the reference's old_array_bounded_grow.bin");
    // THE FORGE'S LOCATOR: lead 0xAAAAAAAA immediately followed by the count 4,
    // both little-endian u32 — the eight-byte needle that must occur once.
    let needle: [u8; 8] = [0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00];
    let mut found: Option<usize> = None;
    for (i, w) in data.windows(needle.len()).enumerate() {
        if w == &needle[..] {
            assert!(found.is_none(), "the lead+count needle occurs more than once; it is not a locator");
            found = Some(i);
        }
    }
    let found = found.unwrap_or_else(|| panic!("the lead+count needle occurs zero times; it is not a locator"));
    data[found + 4..found + 8].copy_from_slice(&7u32.to_le_bytes()); // the forge
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    let n = n.unwrap_or_else(|| panic!(
        "a forged count inside the reader's bound is not a refusal: {} {:?}", report.reason.name(), report
    ));
    assert_eq!(n, 1, "the forged file reads exactly one record, not {n}");
    assert!(
        !report.refused && report.reason == TableFixedReason::None,
        "a forged count inside the writer's bound is not a refusal: {:?}", report
    );
    assert!(!report.malformed, "a count inside the writer's bound is not damage: {:?}", report);
    assert_eq!(
        values[0].vals_count, 4,
        "vals_count is the WRITER's bound carried by the plan, never the reader's 8 nor the forged 7"
    );
    for k in 0..4usize {
        assert_eq!(values[0].vals[k], 1000 + k as i32, "vals[{k}] did not land");
    }
    for k in 4..8usize {
        assert_eq!(values[0].vals[k], 0, "vals[{k}] past the writer's bound is not the reader's declared default");
    }
    assert!(
        values[0].lead == 0xAAAA_AAAA && values[0].trail == 0xBBBB_BBBB,
        "the row moved a neighbour: lead={:#x} trail={:#x}", values[0].lead, values[0].trail
    );
    assert_eq!(
        report.clamped, 1,
        "the bounds pass counts once per entry per record, so clamped is EXACTLY 1, never 2: {:?}", report
    );
    assert!(
        report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0,
        "counters moved on a clean clamped read: {:?}", report
    );
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"))

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
		name:   "probe_writer_bound_count",
		reader: "VNEW_array_bounded_grow",
		older:  []string{"VOLD_array_bounded_grow"},
		src:    versionProbeSource(tests),
	})

	cmd := exec.Command("cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_writer_bound_count")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("writer_bound_count: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_writer_bound_count_new_reads_old")
}
