package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// F7 (schema#876, matrix row GGGGGGGGG) is fixed_form_under_20_bytes: a file
// shorter than the pinned header plus its own length word is MALFORMED, the
// RESIDUE answer, not a refusal by name. It was a gap on all nine legs; five
// (go, c, dart, js, cpp) have landed and rust is one of four that remain. The
// doc (docs/FIXED-FORM-ALGORITHM.md) draws the two answers as exclusive:
// refused plus reason is one answer, malformed is the other, and they are
// NEVER both set. So reason stays UNTOUCHED (TableFixedReason::None), and
// asserting layout_malformed here would be wrong: layout_malformed is a
// REFUSAL by name, the other answer. k == 0 is guarded on this leg by the
// SEPARATE empty guard emitted above the form-byte read (fixedform.go:1090),
// and it owes the same five-part answer as k = 1..19; the test measures that
// instead of trimming the loop. The destination is poisoned first, then every
// caller row must hold the schema's declared defaults after malformed damage.
// The nonzero w=77 default distinguishes a correct reset from zero-filling.
// CONTROL 2 deletes the short-file guard (fixedform.go:1104) and, on safe
// Rust, the reader PANICS on a slice index instead of producing a swapped
// report.

func TestFixedFormUnder20Bytes(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	corpus := versionCorpus(t)

	tests := fmt.Sprintf(`
/// fixed_form_under_20_bytes: a file shorter than the pinned header plus its own
/// length word is MALFORMED, the RESIDUE answer, never a refusal by name. The
/// doc's twenty is TABLE_FIXED_HEADER_BYTES (16) plus the layout's own u32
/// length (the next 4). A clean read at the FULL length first proves the
/// fixture, then every prefix from 0 up to that bound is handed to the reader
/// and must land every part of the malformed row: malformed true, refused
/// false, reason UNTOUCHED (TableFixedReason::None), the call None, all four
/// counters zero, and every destination row reset to its declared defaults.
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
    // are SEPARATE, so k == 0 owes the same five-part answer as k = 1..19.
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
        assert!(n.is_none(), "a {k}-byte file is malformed, never read: {:?} {:?}", n, report);
        assert!(report.damaged, "a {k}-byte file sets damaged: {:?}", report);
        assert!(!report.refused, "a short file is the RESIDUE, never a refusal by name: {:?}", report);
        assert_eq!(
            report.reason, TableFixedReason::None,
            "a short file leaves reason UNTOUCHED, never layout_damaged: {:?}", report.reason
        );
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

	cmd := exec.Command("cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_fixed_form_under_20_bytes")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixed_form_under_20_bytes: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "v_fixed_form_under_20_bytes")
}
