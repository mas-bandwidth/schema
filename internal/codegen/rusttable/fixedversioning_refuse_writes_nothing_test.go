package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// §5.8 row 9, `refuse_writes_nothing`. The NEW reader reads `old_nested_append.bin`
// through the OLD lineage entry, so a COMPILED plan with a NONEMPTY fill list is
// in hand (Vec.w = 88) when step 11 compares record 0's per-record hash — its
// first eight bytes, bitwise INVERTED; the header's hash at file+8 is untouched.
// Every counter on this leg's report (unknown, kind_mismatch, widened, clamped) is
// asserted EXACTLY zero: a prefill run before the hash check moves no counter and
// changes no name, so a `w >= 1` check would pass the read this row exists to catch.

func TestFixedVersioningRefuseWritesNothing(t *testing.T) {
	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// §5.8 row 9, refuse_writes_nothing: the NEW reader reads the OLD writer's
/// file through its lineage, so a COMPILED plan with a NONEMPTY fill list is in
/// hand (the appended nested Vec.w = 88 is the one nonzero prefill) when record
/// 0's per-record hash — its first eight bytes, bitwise INVERTED — fails the
/// hash check; the header's hash at file+8 stays untouched. The refusal is
/// TOTAL: no_block (this leg's spelling of no_layout, §5.9 #25), malformed
/// false, every counter zero, every poisoned byte still 0x5A.
#[test]
fn v_refuse_writes_nothing() {
    let mut data = std::fs::read(%q).expect("the reference's old_nested_append.bin");
    // record 0 begins after the pinned header and the layout block
    let block = u32::from_le_bytes(data[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4].try_into().expect("four bytes")) as usize;
    let at = TABLE_FIXED_HEADER_BYTES + 4 + block;
    assert!(at + 8 <= data.len(), "record 0 is out of the file: at={at} len={}", data.len());
    for i in 0..8 { data[at + i] = !data[at + i]; } // the forge
    let mut values = vec![ROWTYPE::default(); 8];
    // THE CALLER'S STORAGE IS POISONED, NOT RESET (§5.7): a zero fill looks
    // exactly like a zero default, so only a nonzero poison can tell a prefill
    // that RAN from one that never did. Vec.w's prefill is 88, so the byte
    // sweep below must find no 88 and no 0 — only 0x5A.
    unsafe {
        core::ptr::write_bytes(
            values.as_mut_ptr() as *mut u8,
            0x5A,
            core::mem::size_of::<ROWTYPE>() * values.len(),
        );
    }
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(n.is_none(), "a record whose hash names no layout must refuse: {:?} {:?}", n, report);
    assert_eq!(report.reason.name(), "no_block", "the refusal is no_block, not {:?}", report.reason);
    assert!(report.refused, "a refusal by name sets refused: {:?}", report);
    assert!(!report.malformed, "a refusal by name never sets malformed too: {:?}", report);
    assert!(
        report.widened == 0 && report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0,
        "REFUSE is TOTAL: no counter moves: {:?}", report
    );
    // THE BYTE SWEEP: the prefill's 88 is nowhere and neither is a zero — every
    // byte is still the 0x5A poison, because the hash check ran BEFORE the prefill.
    for (i, &b) in as_bytes(&values[0]).iter().enumerate() {
        assert!(b == 0x5A, "REFUSE wrote a destination byte {i} = {b:#04x}");
    }
}
`, filepath.Join(corpus, "old_nested_append.bin"))

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
		name:   "probe_refuse_writes_nothing",
		reader: "VNEW_nested_append",
		older:  []string{"VOLD_nested_append"},
		src:    versionProbeSource(tests),
	})

	cmd := exec.Command("cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_refuse_writes_nothing")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("refuse_writes_nothing: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_refuse_writes_nothing")
}
