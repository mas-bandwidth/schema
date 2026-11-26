package rusttable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// F8 (schema#876) is fixed_form_ragged_tail: a record region that is not a
// whole number of records is REFUSED, and its recommended name is `truncated`
// (docs/FIXED-FORM-ALGORITHM.md §5.3 step 9 and its condition table: "a ragged
// tail: rest mod record_bytes != 0 | truncated"). The verdict is compared
// across legs and the name is diagnostic, and THIS FORM'S REPORT HAS NO
// MALFORMED FLAG (§5.3, "There is no `malformed` flag on this form's report"),
// so damaged stays false. Until 2026-11-25 this test asserted the off-spec
// answer, malformed with reason untouched. The guard's first arm, record_bytes <= 8,
// is NOT reachable from a file at all: record_bytes is the SELECTED LINEAGE
// ENTRY's record size, compiled into the reader (LINEAGE_FIXED_RECORD_BYTES),
// and a reader whose own lineage declared a record under nine bytes is a
// different row against a different fixture. rest is the file's bytes after the
// header and the layout block (fixedform.go:1116), so this row forges ONLY the
// second arm, !rest.len().is_multiple_of(record_bytes), by appending 1 to
// record_bytes-1 bytes to a real corpus file. extra == record_bytes is one more
// whole record, not a ragged tail, so it owes batch_too_large (a refusal) and
// is measured separately. The destination is poisoned first, then every row
// must hold the declared defaults after the refusal (SPEC-TABLES §6.5).
// CONTROL: disable the ragged arm in fixedform.go and the row goes RED: with
// the arm gone the reader computes n := rest / record_bytes, reads one whole
// record off a file with a byte too many, and REPORTS SUCCESS.

func TestFixedFormRaggedTail(t *testing.T) {

	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// fixed_form_ragged_tail: a record region that is not a whole number of
/// records is REFUSED by the name truncated (FIXED-FORM-ALGORITHM §5.3 step
/// 9), and this form's report has no malformed flag. record_bytes is the SELECTED LINEAGE
/// ENTRY's record size, compiled into the reader (LINEAGE_FIXED_RECORD_BYTES),
/// so the guard's record_bytes <= 8 arm is NOT reachable from a file at all.
/// rest is the file's bytes after the header and the layout block, so this row
/// forges ONLY the second arm, rest.len() not a multiple of record_bytes, by
/// appending 1 to record_bytes-1 bytes to a REAL corpus file. extra ==
/// record_bytes is one more whole record, not a ragged tail, so it owes a
/// different answer and is measured separately. A clean read at the FULL length
/// first proves the fixture, then every ragged tail must land refused true,
/// reason Truncated, damaged false, the call None, the counters zero, and every
/// destination row reset to its declared defaults.
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
            !report.refused && report.reason == TableFixedReason::None,
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
        // Poison every caller row so unchanged storage cannot pass the reset
        // check. The nonzero declared default w=77 also rejects zero-filling.
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
        assert!(n.is_none(), "a {extra}-byte ragged tail is refused, never read: {:?} {:?}", n, report);
        assert!(report.refused, "a {extra}-byte ragged tail is a refusal (§5.3 step 9): {:?}", report);
        assert_eq!(
            report.reason, TableFixedReason::Truncated,
            "a ragged tail is refused by the name truncated: {:?}", report.reason
        );
        assert_eq!(report.layout_hash, 0, "truncated carries no hash: {:?}", report);
        assert!(
            report.widened == 0 && report.unknown == 0,
            "a ragged tail moves no counter: {:?}", report
        );
        // VNEW_field_append.schema declares x/y/z=0 and w=77.
        assert!(
            values.iter().all(|v| v.x == 0 && v.y == 0 && v.z == 0 && v.w == 77),
            "a {extra}-byte ragged read did not restore every declared default"
        );
    }
    // extra == record_bytes IS ONE MORE WHOLE RECORD, not a ragged tail: the
    // reader owes batch_too_large, a different name.
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

	cmd := testproc.Command(t, "cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_fixed_form_ragged_tail")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixed_form_ragged_tail: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_fixed_form_ragged_tail")
}
