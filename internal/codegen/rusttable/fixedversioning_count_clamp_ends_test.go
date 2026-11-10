package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// rust/C1 and rust/C2 are the two ends of the count bound in the array-bounds
// work set: "count clamp v<0" and "count clamp v>Max", quoted verbatim from
// docs/roadmap.sexp. Row 4, writer_bound_count, forges this same count word
// from 4 to 7 — neither negative nor past the reader's own max of 8 — so it
// touches neither end. Under strict-only (docs/plans/strict-only.md) both ends
// refuse the whole batch as out_of_range: nothing lands, no counter moves.

func TestFixedVersioningCountClampEnds(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// count_clamp_ends, §5.8 rows C1 and C2: the two ENDS of the count bound.
/// Row 4 forges this same count word to 7, inside the writer's bound, so it
/// exercises neither end. C1 forges -1 (0xFFFFFFFF read as the wire's int32)
/// and C2 forges 9, past the writer's 4 AND the reader's 8: each refuses as
/// out_of_range. The forge writes a
/// little-endian u32 over the count word the lead's needle locates.
#[test]
fn v_count_clamp_negative() {
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
    data[found + 4..found + 8].copy_from_slice(&0xFFFF_FFFFu32.to_le_bytes()); // the forge: -1
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    // STRICT-ONLY: a count past its bound refuses the whole batch as
    // out_of_range; nothing of the record lands.
    assert!(n.is_none(), "a forged count was read, not refused: {:?}", report);
    assert!(
        report.refused && report.reason == TableFixedReason::OutOfRange,
        "a forged count is an out_of_range refusal: {:?}", report
    );
    assert!(!report.damaged, "a forged count is not damage: {:?}", report);
    assert!(
        values[0].lead != 0xAAAA_AAAA && values[0].trail != 0xBBBB_BBBB,
        "a refused batch landed the record: lead={:#x} trail={:#x}", values[0].lead, values[0].trail
    );
    assert_eq!(values[0].vals_count, 0, "a refused batch landed a count");
    assert!(report.unknown == 0 && report.widened == 0, "a refusal moved a counter: {:?}", report);
}

/// C2 — the count forged past the READER'S OWN bound, and past the writer's.
#[test]
fn v_count_clamp_past_bound() {
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
    data[found + 4..found + 8].copy_from_slice(&9u32.to_le_bytes()); // the forge: 9
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    // STRICT-ONLY: a count past its bound refuses the whole batch as
    // out_of_range; nothing of the record lands.
    assert!(n.is_none(), "a forged count was read, not refused: {:?}", report);
    assert!(
        report.refused && report.reason == TableFixedReason::OutOfRange,
        "a forged count is an out_of_range refusal: {:?}", report
    );
    assert!(!report.damaged, "a forged count is not damage: {:?}", report);
    assert!(
        values[0].lead != 0xAAAA_AAAA && values[0].trail != 0xBBBB_BBBB,
        "a refused batch landed the record: lead={:#x} trail={:#x}", values[0].lead, values[0].trail
    );
    assert_eq!(values[0].vals_count, 0, "a refused batch landed a count");
    assert!(report.unknown == 0 && report.widened == 0, "a refusal moved a counter: {:?}", report);
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), filepath.Join(corpus, "old_array_bounded_grow.bin"))

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
		name:   "probe_count_clamp_ends",
		reader: "VNEW_array_bounded_grow",
		older:  []string{"VOLD_array_bounded_grow"},
		src:    versionProbeSource(tests),
	})

	cmd := exec.Command("cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_count_clamp_ends")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("count_clamp_ends: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_count_clamp_negative")
	versionAssert(t, string(out), "v_count_clamp_past_bound")
}
