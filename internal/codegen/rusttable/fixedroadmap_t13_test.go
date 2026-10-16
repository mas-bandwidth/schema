package rusttable

// RUST/F12 ON THE RUST LEG: A SECOND LAYOUT FOR A HELD HASH
// (docs/roadmap.sexp node `fixed-tables` rust/F12; docs/SPEC-TABLES.md §3.4;
// docs/FIXED-FORM-ALGORITHM.md §5.3 step 7 and rows 4 and 5).
//
// The fixed FILE carries the layout hash (header bytes 8..15), the layout length
// (bytes 16..19) and the layout bytes (from byte 20). A file is matched on the
// header's hash and on nothing else, and the hash is TAKEN AS GIVEN — the
// definitions digest is not on the wire — so the layout's bytes are held to the
// lock's: a known hash over different bytes is `layout_malformed`, ONE name
// (§5.3 step 7; "a known hash over different bytes is layout_malformed"). The
// owner ruled 2026-10-13 that this existing refusal closes F12 and that no new
// refusal name is added, here or anywhere.
//
// THIS TEST IS C's c/F12 TWIN (internal/codegen/ctable/fixedroadmap_t01_test.go's
// cT01SecondLayout), narrowed to the two vectors the card names. It builds a
// valid fixed file IN MEMORY for a known layout, then:
//
//	(a) a copy whose header hash is the held one and whose layout bytes differ by
//	    ONE byte — the first layout byte bent; and
//	(b) a copy whose header hash is the held one and whose layout LENGTH differs —
//	    shorter, then longer, with the stated length following the bytes (§5.3 row 3).
//
// The generated Rust reader runs over all three: the first READS, each copy is
// refused as `layout_malformed` BEFORE any record is parsed, and the poisoned
// destination is untouched (§5.3 row 5, §5.7).
//
// THE TEST COMPILES AND RUNS RUST, so it is a functional test: it SKIPS BY NAME
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and FAILS BY NAME when
// SCHEMA_REQUIRE_CORPUS=1 (the corpus gate fixedversioning_test.go keeps at
// versionCorpus: a suite that skips itself under a target that promised the
// toolchain reports green over §5 having never run). It uses no real time,
// sockets, network or outside services beyond the compiler it must run.
//
// THE REVERSED WITNESS: with the byte comparison removed from the emitted load
// (the `if block != known.layout` arm of fixedform.go's fixed_load emit, a
// planted edit restored and never committed) every refusal stops and the test
// goes red at
//
//	--- FAIL: TestFixedRoadmapRustT13LayoutHashCollision/bytes-differ (0.00s)
//	    fixedroadmap_t13_test.go:278: rust/F12 (bytes-differ): a held hash over
//	    layout bytes differing by one must refuse layout_malformed before any
//	    record: T13 bytes-differ BAD a held hash over different layout bytes was
//	    not layout_malformed before any record
//
// so each case pins the refusal itself and not a reader that refuses everything.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// t13Schema is one fixed table whose own layout is the ONE known layout the test
// builds a file for. The fields are plain scalars so the Row, the writer, the
// reader and the fixed form all exist for it.
const t13Schema = `package probe

fixed table Lineage
{
    lead  int32
    trail int32
}
`

// t13Probe is the Rust binary's crate root: the generated fixed modules beside
// it, one writer of a valid file, and the three reads F12 turns on. Every check
// prints its own `T13 <id> ok` or `T13 <id> BAD <why>` line, which the Go test
// reads out.
func t13Probe() string {
	return `mod build_version;
mod fixed_runtime;
mod probe_fixed;
mod probe_records;

pub use build_version::*;
pub use fixed_runtime::*;
pub use probe_fixed::*;
pub use probe_records::*;

fn say(id: &str, ok: bool, why: &str) {
    if ok {
        println!("T13 {id} ok");
    } else {
        println!("T13 {id} BAD {why}");
    }
}

/// The storage the caller hands a read: POISONED with 0x5A and never reset, so a
/// byte a refused read wrote is told from a zero default (docs/FIXED-FORM-ALGORITHM.md §5.7).
fn t13_poisoned(n: usize) -> Vec<LineageRow> {
    let mut values = vec![LineageRow::default(); n];
    unsafe {
        core::ptr::write_bytes(
            values.as_mut_ptr() as *mut u8,
            0x5A,
            core::mem::size_of::<LineageRow>() * values.len(),
        );
    }
    values
}

/// Not one destination byte was written: every byte still holds the poison.
fn t13_untouched(values: &[LineageRow]) -> bool {
    unsafe {
        let bytes = core::slice::from_raw_parts(
            values.as_ptr() as *const u8,
            core::mem::size_of::<LineageRow>() * values.len(),
        );
        bytes.iter().all(|&b| b == 0x5A)
    }
}

fn t13_read(data: &[u8]) -> (Option<usize>, TableFixedReport, Vec<LineageRow>) {
    let mut values = t13_poisoned(1);
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = lineage_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
    (n, report, values)
}

/// A copy under a held hash must be refused as layout_malformed before any
/// record, with no counter moved and the destination untouched (§5.3 row 5).
fn t13_refused_malformed(data: &[u8], why: &str) -> bool {
    let (n, report, values) = t13_read(data);
    if n.is_some() {
        println!("T13 detail {why}: read {} records instead of refusing", n.unwrap());
    }
    n.is_none()
        && report.refused
        && report.reason == TableFixedReason::LayoutMalformed
        && report.reason.name() == "layout_malformed"
        && !report.malformed
        && report.layout_hash == 0
        && t13_untouched(&values)
}

fn main() {
    // A VALID FILE, BUILT IN MEMORY for this build's own (known) layout: the
    // pinned header, the layout behind its u32 length, then one record.
    let need = lineage_fixed_measure(1);
    let mut good = vec![0u8; need];
    let mut v = LineageRow::default();
    v.lead = 0x11223344;
    v.trail = 0x55667788;
    let wrote = lineage_fixed_save(&[v], &mut good).expect("the writer fills the buffer");
    assert!(wrote == need, "measure and save disagree");
    let layout_at = TABLE_FIXED_HEADER_BYTES + 4;
    let layout_len = u32::from_le_bytes(
        good[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]
            .try_into()
            .expect("four bytes"),
    ) as usize;
    assert!(layout_len >= 4, "the layout carries at least its count word");

    // (0) THE KNOWN LAYOUT'S OWN FILE READS, so the refusals below are not a
    // reader that refuses everything.
    let (n, report, _) = t13_read(&good);
    say(
        "valid-reads",
        n == Some(1)
            && !report.refused
            && !report.malformed
            && report.reason == TableFixedReason::None,
        "the known layout's own file did not read clean",
    );

    // (a) THE HELD HASH OVER LAYOUT BYTES THAT DIFFER BY ONE BYTE
    // (docs/FIXED-FORM-ALGORITHM.md §5.3 step 7, row 5): the byte comparison
    // against the lock's layout is what holds the header and the layout together.
    let mut bent = good.clone();
    bent[layout_at] ^= 0x01;
    say(
        "bytes-differ",
        t13_refused_malformed(&bent, "bytes differ"),
        "a held hash over different layout bytes was not layout_malformed before any record",
    );

    // (b) THE HELD HASH OVER A DIFFERENT LAYOUT LENGTH (§5.3 rows 4 and 5): the
    // length word names a layout one byte SHORT of the lock's, so the compared
    // bytes come up short.
    let mut short = good.clone();
    short[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]
        .copy_from_slice(&((layout_len - 1) as u32).to_le_bytes());
    say(
        "length-shorter",
        t13_refused_malformed(&short, "length shorter"),
        "a held hash over a shorter layout length was not layout_malformed before any record",
    );

    // (b) AND ONE BYTE LONGER: the extra byte is the first record byte, so the
    // comparison sees a layout the lock does not hold.
    let mut long = good.clone();
    long[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]
        .copy_from_slice(&((layout_len + 1) as u32).to_le_bytes());
    say(
        "length-longer",
        t13_refused_malformed(&long, "length longer"),
        "a held hash over a longer layout length was not layout_malformed before any record",
    );
}
`
}

// t13Run generates the unit, drops the probe crate, and runs it once with rustc.
// rustc absent is a SKIP unless SCHEMA_REQUIRE_CORPUS promises the toolchain, in
// which case it is a FAILURE BY NAME.
func t13Run(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so rust/F12 cannot be run: %v", err)
		}
		t.Skip("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust")
	}
	files, err := GenerateLineage(unitFrom(t, t13Schema), nil)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	if _, ok := files["probe_fixed.rs"]; !ok {
		t.Fatalf("the schema's fixed root grew no fixed form; got %v", keysOf(files))
	}
	root := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(t13Probe()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "t13probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = root
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	return string(out)
}

// TestFixedRoadmapRustT13LayoutHashCollision holds rust/F12: a valid file reads,
// and each copy of it — a held hash over layout bytes that differ by one, and a
// held hash over a different layout length — is refused as `layout_malformed`
// before any record, the destination untouched (docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 7 and rows 4 and 5).
func TestFixedRoadmapRustT13LayoutHashCollision(t *testing.T) {
	t.Parallel()
	out := t13Run(t)
	cases := []struct {
		id  string
		why string
	}{
		{"valid-reads", "the known layout's own file must read"},
		{"bytes-differ", "a held hash over layout bytes differing by one must refuse layout_malformed before any record"},
		{"length-shorter", "a held hash over a shorter layout length must refuse layout_malformed before any record"},
		{"length-longer", "a held hash over a longer layout length must refuse layout_malformed before any record"},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			if line := t13Line(out, c.id); line != "T13 "+c.id+" ok" {
				t.Fatalf("rust/F12 (%s): %s: %s", c.id, c.why, line)
			}
		})
	}
}

// t13Line is the probe's one line for a check id.
func t13Line(out, id string) string {
	prefix := "T13 " + id + " "
	for l := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return fmt.Sprintf("[no T13 line for %s in the probe's output: %s]", id, strings.TrimSpace(out))
}
