package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
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

/// F10 (§5.3 step 7): "per record: if LE(8, at) != h, REFUSE no_layout" — this
/// leg's spelling is no_block (§5.9 #25). A forged hash in ANY record refuses by
/// name, total, and is never malformed. What a refusal at record k leaves in
/// out[0..k-1] is the page's OPEN QUESTION and is not asserted.
#[test]
fn v_t01_f10() {
    for k in 0..3usize {
        let mut data = t01_records(3);
        let at = TABLE_FIXED_HEADER_BYTES + 4 + t01_layout_len(&data) + k * LINEAGE_FIXED_RECORD_BYTES;
        data[at] ^= 0x01;
        let (n, report, values) = t01_read(&data, 3, 4096);
        assert!(n.is_none(), "record {k} with a forged hash must refuse: {:?}", report);
        assert_eq!(report.reason.name(), "no_block", "record {k}: {:?}", report.reason);
        assert!(report.refused && !report.malformed, "record {k}: refused and malformed are never both set: {:?}", report);
        assert!(t01_total(&report), "record {k}: no counter moves: {:?}", report);
        assert_eq!(report.layout_hash, 0, "record {k}: no_block carries no hash: {:?}", report);
        if k == 0 {
            assert!(t01_untouched(&values), "a refusal at record 0 wrote a destination byte");
        }
    }
}

/// R8 (§5.3 step 4): "a hash in no entry is layout_newer", reporting THE FILE'S
/// HASH AND NOTHING ELSE: the whole report is exactly refused, the reason and
/// the hash.
#[test]
fn v_t01_r8() {
    for forged in [0xDEAD_BEEF_CAFE_F00Du64, 0, u64::MAX] {
        let mut data = t01_records(2);
        data[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8].copy_from_slice(&forged.to_le_bytes());
        let (n, report, values) = t01_read(&data, 2, 4096);
        assert!(n.is_none(), "a hash in no entry must refuse: {:?}", report);
        let want = TableFixedReport {
            refused: true,
            reason: TableFixedReason::LayoutNewer,
            layout_hash: forged,
            ..TableFixedReport::default()
        };
        assert_eq!(report, want, "layout_newer reports the file's hash {forged:#018x} and NOTHING ELSE");
        assert!(t01_untouched(&values), "layout_newer wrote a destination byte");
    }
}

/// R9 (§5.3 step 5 and step 3's tail): "a known hash with a different layout
/// length or bytes → layout_malformed; the seven §1.1 malformations under a
/// known hash all come back as this one name". Every edit below leaves the
/// header's hash the reader's own.
#[test]
fn v_t01_r9() {
    let own = t01_records(1);
    let l = t01_layout_len(&own);
    let b = TABLE_FIXED_HEADER_BYTES + 4;
    let count = u32::from_le_bytes(own[b..b + 4].try_into().expect("four bytes")) as usize;
    assert_eq!(l, 4 + 17 * count, "the fixture's layout is count entries of 17 bytes");
    let entry = |i: usize| b + 4 + 17 * i;
    let mut cases: Vec<(String, Vec<u8>)> = Vec::new();
    // EVERY BYTE OF THE LAYOUT, flipped: a different layout of the same length.
    for i in 0..l {
        let mut d = own.clone();
        d[b + i] ^= 0xFF;
        cases.push((format!("byte {i} flipped"), d));
    }
    // A DIFFERENT LENGTH: shorter and longer, the stated length following the bytes.
    {
        let mut d = own.clone();
        d.remove(b + l - 1);
        d[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4].copy_from_slice(&((l - 1) as u32).to_le_bytes());
        cases.push(("one byte shorter".to_string(), d));
        let mut d = own.clone();
        d.insert(b + l, 0);
        d[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4].copy_from_slice(&((l + 1) as u32).to_le_bytes());
        cases.push(("one byte longer".to_string(), d));
        let mut d = own.clone();
        d[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4].copy_from_slice(&0u32.to_le_bytes());
        cases.push(("an empty layout".to_string(), d));
        let mut d = own.clone();
        d[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4].copy_from_slice(&u32::MAX.to_le_bytes());
        cases.push(("a stated length past the file".to_string(), d));
    }
    // §1.1's SEVEN, each forged in place under the known hash.
    let seven: [(&str, Vec<u8>); 7] = {
        let edit = |at: usize, v: &[u8]| { let mut d = own.clone(); d[at..at + v.len()].copy_from_slice(v); d };
        [
            ("1 count mismatch", edit(b, &((count + 1) as u32).to_le_bytes())),
            ("2 kind unknown", edit(entry(1) + 8, &[31])),
            ("3 size mismatch", edit(entry(1) + 9, &0x7FFF_FFFFu32.to_le_bytes())),
            ("4 kind invalid", edit(entry(0) + 8, &[14])),
            ("5 tree unclosed", edit(entry(0) + 13, &(count as u32).to_le_bytes())),
            ("6 record too large", edit(entry(0) + 9, &0x7FFF_FFFFu32.to_le_bytes())),
            ("7 too deep", {
                // a 70-entry chain of single-child tables, under the same hash
                let n = 70usize;
                let mut layout = (n as u32).to_le_bytes().to_vec();
                for i in 0..n {
                    layout.extend_from_slice(&[0u8; 8]);
                    layout.push(13);
                    layout.extend_from_slice(&1u32.to_le_bytes());
                    layout.extend_from_slice(&(if i + 1 < n { 1u32 } else { 0u32 }).to_le_bytes());
                }
                let mut d = own[..TABLE_FIXED_HEADER_BYTES].to_vec();
                d.extend_from_slice(&(layout.len() as u32).to_le_bytes());
                d.extend_from_slice(&layout);
                d
            }),
        ]
    };
    for (name, d) in seven { cases.push((name.to_string(), d)); }
    for (name, d) in cases {
        let (n, report, values) = t01_read(&d, 2, 4096);
        assert!(n.is_none(), "{name}: a lie about a known version is refused: {:?}", report);
        assert_eq!(report.reason.name(), "layout_malformed", "{name}: ONE name, not {:?}", report.reason);
        assert!(report.refused && !report.malformed, "{name}: refused and malformed are never both set: {:?}", report);
        assert!(t01_total(&report) && report.layout_hash == 0, "{name}: nothing else is reported: {:?}", report);
        assert!(t01_untouched(&values), "{name}: a destination byte was written");
    }
}

/// R13 (§5.3, §5.8 row 9): "REFUSE is total: refused+reason and malformed are
/// never both set, every counter stays zero, and not one destination byte is
/// written". Every refusal the load answers before a record lands is held to it
/// across ITS OWN name, and every malformed answer is held to the other side.
#[test]
fn v_t01_r13() {
    let own = t01_records(3);
    let at = TABLE_FIXED_HEADER_BYTES + 4 + t01_layout_len(&own);
    let edit = |f: &dyn Fn(&mut Vec<u8>)| { let mut d = own.clone(); f(&mut d); d };
    let refusals: Vec<(&str, Vec<u8>, usize, usize, &str)> = vec![
        ("previous_form", edit(&|d| d[0] = 1), 3, 4096, "previous_form"),
        ("message_form_as_file", edit(&|d| d[0] = 2), 3, 4096, "message_form_as_file"),
        ("newer_form", edit(&|d| d[0] = 9), 3, 4096, "newer_form"),
        ("layout_newer", edit(&|d| d[8] ^= 0xFF), 3, 4096, "layout_newer"),
        ("layout_malformed", edit(&|d| d[TABLE_FIXED_HEADER_BYTES + 4] ^= 0xFF), 3, 4096, "layout_malformed"),
        ("layout_past_the_file", edit(&|d| d[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4].copy_from_slice(&u32::MAX.to_le_bytes())), 3, 4096, "layout_malformed"),
        ("batch_too_large", own.clone(), 2, 4096, "batch_too_large"),
        ("no_block", edit(&|d| d[at] ^= 0x01), 3, 4096, "no_block"),
        ("plan_too_large", own.clone(), 3, 0, "plan_too_large"),
    ];
    for (label, d, capacity, plan_len, name) in refusals {
        let (n, report, values) = t01_read(&d, capacity, plan_len);
        assert!(n.is_none(), "{label}: a refusal reads nothing: {:?}", report);
        assert!(report.refused, "{label}: refused is set: {:?}", report);
        assert_eq!(report.reason.name(), name, "{label}");
        assert!(!report.malformed, "{label}: refused and malformed are never both set: {:?}", report);
        assert!(t01_total(&report), "{label}: every counter stays zero: {:?}", report);
        assert!(t01_untouched(&values), "{label}: a destination byte was written");
    }
    // THE OTHER ANSWER: malformed is never a refusal and never carries a name.
    let malformed: Vec<(&str, Vec<u8>)> = vec![
        ("no first byte", Vec::new()),
        ("under 20 bytes", own[..19].to_vec()),
        ("a nonzero reserved byte", edit(&|d| d[3] = 1)),
        ("a ragged tail", edit(&|d| d.push(0))),
    ];
    for (label, d) in malformed {
        let (n, report, values) = t01_read(&d, 3, 4096);
        assert!(n.is_none(), "{label}: malformed reads nothing: {:?}", report);
        assert!(report.malformed, "{label}: malformed is set: {:?}", report);
        assert!(!report.refused && report.reason == TableFixedReason::None, "{label}: never both set: {:?}", report);
        assert!(t01_total(&report), "{label}: every counter stays zero: {:?}", report);
        assert!(t01_untouched(&values), "{label}: a destination byte was written");
    }
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

// t01IdentityShape is rust/R7 (§5.3 step 8, §5.9 #20): "the identity lane is an
// index comparison, never a recomputed hash". The emitted load selects by
// `position(|k| k.hash == hash)` over the lock's constants, tests `found ==
// <OWN>` for identity, and never calls the hash function a runtime holds.
func t01IdentityShape(t *testing.T) {
	older := versionUnit(t, versionSchema(t, "VOLD_field_append"))
	u := versionUnit(t, versionSchema(t, "VNEW_field_append"))
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(older) {
		e, ok := FixedLineageOf(older, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	for label, l := range map[string]map[string][]FixedLineageEntry{"a lineage of two": lineage, "a lineage of one": nil} {
		tables, err := GenerateLineage(u, l)
		if err != nil {
			t.Fatalf("%s: GenerateLineage: %v", label, err)
		}
		seen := false
		for name, data := range tables {
			text := string(data)
			from := strings.Index(text, "_fixed_load(")
			if from < 0 {
				continue
			}
			seen = true
			load := text[from:]
			if end := strings.Index(load, "\n}\n"); end > 0 {
				load = load[:end]
			}
			if strings.Contains(load, "table_fixed_hash(") {
				t.Errorf("%s %s: the load recomputes a hash; the identity lane is an index comparison (§5.3 step 8)", label, name)
			}
			if !strings.Contains(load, ".position(|k| k.hash == hash)") {
				t.Errorf("%s %s: the plan is not selected by the header's hash against the lock's constants (§5.3 step 4)", label, name)
			}
			if l != nil && !strings.Contains(load, "let identity = found == ") {
				t.Errorf("%s %s: identity is not an index comparison (§5.3 step 8)", label, name)
			}
		}
		if !seen {
			t.Fatalf("%s: no load function was emitted", label)
		}
	}
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
		{id: "rust/F10", test: "v_t01_f10"},
		{id: "rust/R7", check: t01IdentityShape},
		{id: "rust/R8", test: "v_t01_r8"},
		{id: "rust/R9", test: "v_t01_r9"},
		{id: "rust/R13", test: "v_t01_r13"},
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
