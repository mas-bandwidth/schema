package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// rust/C1 and rust/C2 are the two ends of the count clamp in the array-bounds
// work set: "count clamp v<0" and "count clamp v>Max", quoted verbatim from
// docs/roadmap.sexp. Row 4, writer_bound_count, forges this same count word
// from 4 to 7 — neither negative nor past the reader's own max of 8 — so it
// touches neither end. On 2026-09-19 the negative arm was deleted from the
// emitted runtime and not one landed fixed-table test went red on five legs
// (c, cs, dart, elixir and js): it is guarded by nothing. clamped is asserted
// EXACTLY == 1, never >= 1, because a leg that counts in the count op and
// again in the bounds pass lands 2. C2's count of 4 is the WRITER's bound
// carried by the plan, never the reader's own 8.

func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// count_clamp_ends, §5.8 rows C1 and C2: the two ENDS of the count clamp.
/// Row 4 forges this same count word to 7, inside the writer's bound, so it
/// exercises neither end. C1 forges -1 (0xFFFFFFFF read as the wire's int32)
/// and must clamp to ZERO; C2 forges 9, past the writer's 4 AND the reader's
/// 8, and must clamp to the WRITER's 4 carried by the plan. The forge writes a
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
    let n = n.unwrap_or_else(|| panic!(
        "a forged count is not a refusal: {} {:?}", report.reason.name(), report
    ));
    assert_eq!(n, 1, "the forged file reads exactly one record, not {n}");
    assert!(
        !report.refused && report.reason == TableFixedReason::None,
        "a forged count is not a refusal: {:?}", report
    );
    assert!(!report.malformed, "a forged count is not damage: {:?}", report);
    assert!(
        values[0].lead == 0xAAAA_AAAA && values[0].trail == 0xBBBB_BBBB,
        "a forged count moved a neighbour: lead={:#x} trail={:#x}", values[0].lead, values[0].trail
    );
    assert_eq!(
        values[0].vals_count, 0,
        "the negative count clamps to ZERO, never -1, never the writer's 4, never the reader's 8"
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
    let n = n.unwrap_or_else(|| panic!(
        "a forged count is not a refusal: {} {:?}", report.reason.name(), report
    ));
    assert_eq!(n, 1, "the forged file reads exactly one record, not {n}");
    assert!(
        !report.refused && report.reason == TableFixedReason::None,
        "a forged count is not a refusal: {:?}", report
    );
    assert!(!report.malformed, "a forged count is not damage: {:?}", report);
    assert!(
        values[0].lead == 0xAAAA_AAAA && values[0].trail == 0xBBBB_BBBB,
        "a forged count moved a neighbour: lead={:#x} trail={:#x}", values[0].lead, values[0].trail
    );
    assert_eq!(
        values[0].vals_count, 4,
        "vals_count is the WRITER's bound carried by the plan, never the reader's 8, never the forged 9"
    );
    for k in 0..4usize {
        assert_eq!(values[0].vals[k], 1000 + k as i32, "vals[{k}] did not land");
    }
    for k in 4..8usize {
        assert_eq!(values[0].vals[k], 0, "vals[{k}] past the writer's bound is not the reader's declared default");
    }
    assert_eq!(
        report.clamped, 1,
        "the bounds pass counts once per entry per record, so clamped is EXACTLY 1, never 2: {:?}", report
    );
    assert!(
        report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0,
        "counters moved on a clean clamped read: {:?}", report
    );
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
