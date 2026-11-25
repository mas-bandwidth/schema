package rusttable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// F7 (schema#876) is fixed_form_under_20_bytes: a file shorter than the pinned
// header plus its own length word is REFUSED with nothing landed, and its
// recommended name is `truncated` (docs/FIXED-FORM-ALGORITHM.md §5.3 steps 1
// and 2, and the condition table: "no bytes at all" and "shorter than 20
// bytes" are both `truncated`). The verdict is compared across legs and the
// name is diagnostic, and THIS FORM'S REPORT HAS NO MALFORMED FLAG, so damaged
// stays false. Until 2026-11-25 this test asserted the off-spec answer,
// malformed with reason untouched. k == 0 is guarded on this leg by the
// SEPARATE empty guard emitted above the form-byte read, and it owes the same
// answer as k = 1..19; the test measures that instead of trimming the loop.
// The destination is poisoned first, then every caller row must hold the
// schema's declared defaults after the refusal (SPEC-TABLES §6.5). The nonzero
// w=77 default distinguishes a correct reset from zero-filling. CONTROL:
// delete the short-file guard in fixedform.go and, on safe Rust, the reader
// PANICS on a slice index.

func TestFixedFormUnder20Bytes(t *testing.T) {

	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// fixed_form_under_20_bytes: a file shorter than the pinned header plus its own
/// length word is REFUSED by the name truncated (§5.3 steps 1 and 2). The
/// doc's twenty is TABLE_FIXED_HEADER_BYTES (16) plus the layout's own u32
/// length (the next 4). A clean read at the FULL length first proves the
/// fixture, then every prefix from 0 up to that bound is handed to the reader
/// and must land every part of the refusal: refused true, reason Truncated,
/// damaged false, the call None, the counters zero, and every destination row
/// reset to its declared defaults.
#[test]
fn v_fixed_form_under_20_bytes() {
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
            !report.refused && report.reason == TableFixedReason::None && !report.damaged,
            "the full file is neither a refusal nor damage: {:?}", report
        );
        assert!(values[0].w == 777, "the full file lost its value: w={}", values[0].w);
    }
    // EVERY PREFIX UNDER THE TWENTY. The empty guard and the short-file guard
    // are SEPARATE, so k == 0 owes the same answer as k = 1..19.
    for k in 0..TABLE_FIXED_HEADER_BYTES + 4 {
        let short = &data[..k];
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
        let n = LOADFN(&mut values, short, &mut plan, &mut remap, &mut report);
        assert!(n.is_none(), "a {k}-byte file is refused, never read: {:?} {:?}", n, report);
        assert!(report.refused, "a {k}-byte file is a refusal (§5.3 steps 1, 2): {:?}", report);
        assert_eq!(
            report.reason, TableFixedReason::Truncated,
            "a short file is refused by the name truncated: {:?}", report.reason
        );
        assert!(!report.damaged, "this form's report has no malformed flag: {:?}", report);
        assert_eq!(report.layout_hash, 0, "truncated carries no hash: {:?}", report);
        assert!(
            report.widened == 0 && report.unknown == 0,
            "a short file moves no counter: {:?}", report
        );
        // VNEW_field_append.schema declares x/y/z=0 and w=77.
        assert!(
            values.iter().all(|v| v.x == 0 && v.y == 0 && v.z == 0 && v.w == 77),
            "a {k}-byte read did not restore every declared default"
        );
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
		name:   "probe_fixed_form_under_20_bytes",
		reader: "VNEW_field_append",
		older:  []string{"VOLD_field_append"},
		src:    versionProbeSource(tests),
	})

	cmd := testproc.Command(t, "cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_fixed_form_under_20_bytes")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixed_form_under_20_bytes: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_fixed_form_under_20_bytes")
}
