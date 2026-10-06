package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// THE ROADMAP'S FRAMING TASKS ON THE RUST LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.3's read table and §5.8's
// refusals; SPEC-TABLES §3.4). One subtest per task id. rust/F8 and rust/R7's
// behaviour are held by TestFixedFormRaggedTail and the other files of this
// package; the subtests below carry only the clauses those do not assert.
//
// rust/F12 (a second layout for a held hash) has no subtest: the rust leg has
// no announcement carrier to refuse it on, and the page names no rust surface
// for one, so the card reports it not-done and never decides the surface here.
//
// THE NAME OF THE PER-RECORD REFUSAL (§5.9 #25): the page's name is
// `no_layout`; a leg carrying form 1's words keeps `no_block` until the whole-leg
// rename and says so. This leg is such a leg, so rust/F10 asserts `no_block` and
// the divergence is reported, never hidden.
//
// ONE WORKSPACE, ONE `cargo test` (the two-minute rule of fixedversioning_test.go):
// the parent runs the single probe crate and every subtest reads its own rows
// out of that output.

// t01Probes is the rust source of the card's probes. Every test reads the
// reference's byte oracle and forges ONE thing in it.
func t01Probes(corpus string) string {
	return fmt.Sprintf(`
/// the storage the caller hands a read: POISONED with 0x5A and never reset, so a
/// byte a refused read wrote is told from a zero default (§5.7).
fn t01_poisoned(n: usize) -> Vec<ROWTYPE> {
    let mut values = vec![ROWTYPE::default(); n];
    unsafe {
        core::ptr::write_bytes(
            values.as_mut_ptr() as *mut u8,
            0x5A,
            core::mem::size_of::<ROWTYPE>() * values.len(),
        );
    }
    values
}

fn t01_untouched(values: &[ROWTYPE]) -> bool {
    values.iter().all(|v| as_bytes(v).iter().all(|&b| b == 0x5A))
}

fn t01_read(data: &[u8], capacity: usize, plan_len: usize) -> (Option<usize>, TableFixedReport, Vec<ROWTYPE>) {
    let mut values = t01_poisoned(capacity);
    let mut plan = vec![TableFixedEntry::default(); plan_len];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, data, &mut plan, &mut remap, &mut report);
    (n, report, values)
}

fn t01_own() -> Vec<u8> {
    std::fs::read(%[1]q).expect("the reference's new_field_append.bin")
}

/// where record 0 begins, and the layout's own length
fn t01_layout_len(data: &[u8]) -> usize {
    u32::from_le_bytes(data[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4].try_into().expect("four bytes")) as usize
}

/// the file's own header and layout, then k copies of its first record
fn t01_records(k: usize) -> Vec<u8> {
    let data = t01_own();
    let at = TABLE_FIXED_HEADER_BYTES + 4 + t01_layout_len(&data);
    let rb = LINEAGE_FIXED_RECORD_BYTES;
    assert!(data.len() >= at + rb, "the fixture holds a record");
    let mut out = data[..at].to_vec();
    for _ in 0..k {
        out.extend_from_slice(&data[at..at + rb]);
    }
    out
}

fn t01_total(report: &TableFixedReport) -> bool {
    report.widened == 0 && report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0
}

/// F9 (§5.3 step 6): "If rest / record_bytes passes the caller's capacity,
/// REFUSE batch_too_large". The count AT capacity reads; one past it refuses by
/// name BEFORE any record's hash is looked at (step 6 precedes step 7), as a
/// TOTAL refusal.
#[test]
fn v_t01_f9() {
    for k in 1..=4usize {
        let data = t01_records(k);
        let (n, report, _) = t01_read(&data, k, 4096);
        assert_eq!(n, Some(k), "{k} records into capacity {k} read: {:?}", report);
        assert!(!report.refused && !report.malformed, "a batch at capacity is clean: {:?}", report);
        if k == 1 {
            continue;
        }
        let (n, report, values) = t01_read(&data, k - 1, 4096);
        assert!(n.is_none(), "{k} records past capacity {} must refuse: {:?}", k - 1, report);
        assert_eq!(report.reason.name(), "batch_too_large", "{k} records past capacity: {:?}", report.reason);
        assert!(report.refused && !report.malformed, "a refusal by name never sets malformed: {:?}", report);
        assert!(t01_total(&report), "REFUSE is TOTAL, no counter moves: {:?}", report);
        assert!(t01_untouched(&values), "batch_too_large wrote a destination byte");
    }
    // THE COUNT IS CHECKED BEFORE THE RECORDS: a forged hash in the LAST record
    // of an over-capacity batch still answers batch_too_large, not no_block.
    let mut data = t01_records(3);
    let last = data.len() - LINEAGE_FIXED_RECORD_BYTES;
    for i in 0..8 { data[last + i] = !data[last + i]; }
    let (n, report, values) = t01_read(&data, 2, 4096);
    assert!(n.is_none() && report.reason.name() == "batch_too_large", "step 6 precedes step 7: {:?}", report);
    assert!(t01_untouched(&values), "the over-capacity refusal wrote a destination byte");
}

`, filepath.Join(corpus, "new_field_append.bin"))
}

// t01Run builds the one probe crate and runs it once.
func t01Run(t *testing.T) string {
	t.Helper()
	corpus := versionCorpus(t)
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
		name:   "probe_t01_framing",
		reader: "VNEW_field_append",
		older:  []string{"VOLD_field_append"},
		src:    versionProbeSource(t01Probes(corpus)),
	})
	cmd := exec.Command("cargo", "test")
	cmd.Dir = filepath.Join(root, "probe_t01_framing")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(out), "test result: FAILED") {
		t.Fatalf("t01 framing probe did not build: %v\n%s", err, versionTail(string(out)))
	}
	return string(out)
}

func TestFixedRoadmapT01Framing(t *testing.T) {
	t.Parallel()
	out := t01Run(t)
	tasks := []struct {
		id    string
		test  string // the rust probe whose ok line proves the task; "" runs check
		check func(t *testing.T)
	}{
		{id: "rust/F9", test: "v_t01_f9"},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			if task.check != nil {
				task.check(t)
				return
			}
			versionAssert(t, out, task.test)
		})
	}
}
