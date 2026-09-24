package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// F8 (schema#876, matrix row G~GGPPPP~) is fixed_form_ragged_tail: a record
// region that is not a whole number of records is MALFORMED, the RESIDUE answer,
// not a refusal by name. It is a gap on go, cpp and cs, only partial on c and
// elixir. It is F7's direct sibling — the very next guard in the same emitted
// function — and go closed it in #1291. The doc draws the two answers as
// exclusive: refused plus reason is one answer, malformed is the other, and
// they are NEVER both set. So reason stays UNTOUCHED (TableFixedReason::None),
// and asserting layout_malformed here would be wrong: layout_malformed is a
// REFUSAL by name, the other answer. The guard's first arm, record_bytes <= 8,
// is NOT reachable from a file at all: record_bytes is the SELECTED LINEAGE
// ENTRY's record size, compiled into the reader (LINEAGE_FIXED_RECORD_BYTES),
// and a reader whose own lineage declared a record under nine bytes is a
// different row against a different fixture. rest is the file's bytes after the
// header and the layout block (fixedform.go:1116), so this row forges ONLY the
// second arm, !rest.len().is_multiple_of(record_bytes), by appending 1 to
// record_bytes-1 bytes to a real corpus file. extra == record_bytes is one more
// whole record, not a ragged tail, so it owes batch_too_large (a refusal) and
// is measured separately. The destination is proved untouched by poisoning
// every byte with 0x5A and sweeping as_bytes over the first row. CONTROL 2
// disables the ragged arm (fixedform.go:1187) and the row must go RED: with the
// arm gone the reader computes n := rest / record_bytes, reads one whole record
// off a file with a byte too many, and REPORTS SUCCESS (malformed false).

func TestFixedFormRaggedTail(t *testing.T) {
	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// fixed_form_ragged_tail: a record region that is not a whole number of
/// records is MALFORMED, the RESIDUE answer, never a refusal by name. The
/// reader's two answers are exclusive: refused plus reason is one, malformed is
/// the other, and they are NEVER both set. record_bytes is the SELECTED LINEAGE
/// ENTRY's record size, compiled into the reader (LINEAGE_FIXED_RECORD_BYTES),
/// so the guard's record_bytes <= 8 arm is NOT reachable from a file at all.
/// rest is the file's bytes after the header and the layout block, so this row
/// forges ONLY the second arm, rest.len() not a multiple of record_bytes, by
/// appending 1 to record_bytes-1 bytes to a REAL corpus file. extra ==
/// record_bytes is one more whole record, not a ragged tail, so it owes a
/// different answer and is measured separately. A clean read at the FULL length
/// first proves the fixture, then every ragged tail must land malformed true,
/// refused false, reason UNTOUCHED (TableFixedReason::None), the call None, all
/// four counters zero, and not one destination byte written.
#[test]
fn v_fixed_form_ragged_tail() {
    let data = std::fs::read(%q).expect("the reference's new_field_append.bin");
    // FULL LENGTH FIRST: a broken fixture cannot pass this row by accident.
    {
        let mut values = vec![ROWTYPE::default(); 8];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
        let n = n.unwrap_or_else(|| panic!(
            "the full file must read cleanly: {} {:?}", report.reason.name(), report
        ));
        assert!(n >= 1, "the full file lands at least one record, not {n}");
        assert!(
            !report.refused && report.reason == TableFixedReason::None && !report.malformed,
            "the full file is neither a refusal nor damage: {:?}", report
        );
        assert!(values[0].w == 777, "the full file lost its value: w={}", values[0].w);
    }
    // EVERY RAGGED TAIL. record_bytes is read from the emitted constant BY NAME
    // (LINEAGE_FIXED_RECORD_BYTES), never re-typed as a number: a hardcoded
    // number is a second copy of somebody else's decision and it rots.
    let record_bytes = LINEAGE_FIXED_RECORD_BYTES;
    for extra in 1..record_bytes {
        let mut ragged = data.clone();
        ragged.extend_from_slice(&vec![0u8; extra]);
        let mut values = vec![ROWTYPE::default(); 8];
        // THE DESTINATION IS POISONED, NOT RESET: a zero fill looks exactly
        // like a zero default, so only the nonzero 0x5A can prove no byte was
        // written by a read that should refuse everything.
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
        let n = LOADFN(&mut values, &ragged, &mut plan, &mut remap, &mut report);
        assert!(n.is_none(), "a {extra}-byte ragged tail is malformed, never read: {:?} {:?}", n, report);
        assert!(report.malformed, "a {extra}-byte ragged tail sets malformed: {:?}", report);
        assert!(!report.refused, "a ragged tail is the RESIDUE, never a refusal by name: {:?}", report);
        assert_eq!(
            report.reason, TableFixedReason::None,
            "a ragged tail leaves reason UNTOUCHED, never layout_malformed: {:?}", report.reason
        );
        assert!(
            report.widened == 0 && report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0,
            "a ragged tail moves no counter: {:?}", report
        );
        for (i, &b) in as_bytes(&values[0]).iter().enumerate() {
            assert!(b == 0x5A, "a {extra}-byte ragged read wrote a destination byte {i} = {b:#04x}");
        }
    }
    // extra == record_bytes IS ONE MORE WHOLE RECORD, not a ragged tail: the
    // reader owes batch_too_large, a REFUSAL by name, and the damage flag
    // stays false.
    {
        let mut whole = data.clone();
        whole.extend_from_slice(&vec![0u8; record_bytes]);
        let mut values = vec![ROWTYPE::default(); 1];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = LOADFN(&mut values, &whole, &mut plan, &mut remap, &mut report);
        assert!(n.is_none(), "a whole extra record is refused, never read: {:?} {:?}", n, report);
        assert_eq!(
            report.reason.name(), "batch_too_large",
            "extra == record_bytes is one more whole record, owed batch_too_large, not {:?}", report.reason
        );
        assert!(report.refused, "extra == record_bytes sets refused: {:?}", report);
        assert!(!report.malformed, "a refusal by name never sets malformed too: {:?}", report);
    }
}
`, filepath.Join(corpus, "new_field_append.bin"))

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
		name:   "probe_fixed_form_ragged_tail",
		reader: "VNEW_field_append",
		older:  []string{"VOLD_field_append"},
		src:    versionProbeSource(tests),
	})

	cmd := exec.Command("cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_fixed_form_ragged_tail")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixed_form_ragged_tail: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_fixed_form_ragged_tail")
}
