package rusttable

// THE WIDE-TEXT ROW ON THE RUST LEG (docs/roadmap.sexp node `fixed-tables`,
// utf16-values/rust/valid-data and rust/C5). Law: docs/SPEC-TABLES.md §3.4's
// fixed record — a `wstring(N)` is a used length in CODE UNITS and then 2N
// bytes — and docs/SPEC.md §4.12, whose content rule makes a used run that is
// not paired UTF-16 with no zero unit a REFUSAL BY NAME rather than data.
//
// ONE SUBTEST PER TASK. utf16-values writes and reads the construct at the
// reference values (empty, a BMP unit, a BMP pair and the full declared
// capacity) and asserts each unit lands. rust/C5 asserts the length is counted
// in UTF-16 CODE UNITS — the astral pair U+1F600 is TWO units, not one code
// point — and that a used run which is not the text the kind says it is is
// refused under its OWN NAME (`text_ill_formed`, §4.12's interior-null and
// surrogate-pairing checks).
//
// THE TARGET IS A RUST BINARY THIS TEST BUILDS AND RUNS, exactly as
// fixedroadmap_t09_test.go does: the fixed runtime, the record module and the
// codec module are self-contained, so the probe compiles with `rustc` alone
// and the Go subtests read the running binary's own `T19 <id> ok` lines.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// t19Schema is the one fixed table this card proves: a single `wstring(10)`
// field, ten code units of storage with a used length beside it (SPEC §4.12).
const t19RustSchema = `package probe

fixed table Root
{
    wide wstring(10)
}
`

// t19Probe is the Rust binary's crate root: the generated fixed modules beside
// it, the reference writes and reads, and one line per task.
func t19Probe() string {
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
        println!("T19 {id} ok");
    } else {
        println!("T19 {id} BAD {why}");
    }
}

// round writes one record and reads it back, asserting a clean read; it is the
// one write/read the row's verdict compares against.
fn round(v: RootRow) -> RootRow {
    let need = root_fixed_measure(1);
    let mut buf = vec![0u8; need];
    let wrote = root_fixed_save(&[v], &mut buf).expect("the writer fills the buffer");
    assert!(wrote == need, "measure and save disagree");
    let mut out = vec![RootRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = root_fixed_load(&mut out, &buf, &mut plan, &mut remap, &mut report)
        .expect("the buffer this writer wrote reads");
    assert!(
        n == 1 && !report.refused && !report.malformed,
        "clean read: {report:?}"
    );
    out[0]
}

// The reference values (utf16-values/go/valid-data): empty, a BMP unit, a BMP
// pair and the full declared capacity.
fn reference_values() -> Vec<Vec<u16>> {
    vec![
        vec![],
        vec![0x0041],
        vec![0x00E9, 0x4E2D],
        (1u16..=10).collect(),
    ]
}

fn main() {
    // utf16-values: every reference value round-trips, unit for unit.
    let mut utf16_ok = true;
    for want in reference_values() {
        let mut v = RootRow::default();
        v.wide[..want.len()].copy_from_slice(&want);
        v.wide_length = want.len() as i32;
        let o = round(v);
        if o.wide_length != want.len() as i32 {
            utf16_ok = false;
            break;
        }
        for (i, unit) in want.iter().enumerate() {
            if o.wide[i] != *unit {
                utf16_ok = false;
            }
        }
    }
    say(
        "utf16-values",
        utf16_ok,
        "a reference wstring value did not land unit for unit",
    );

    // rust/C5: THE LENGTH IS CODE UNITS. U+1F600 is one code point and TWO
    // UTF-16 code units (a surrogate pair), and the used length must be 2.
    let pair: [u16; 2] = [0xD83D, 0xDE00];
    let mut pv = RootRow::default();
    pv.wide[..2].copy_from_slice(&pair);
    pv.wide_length = 2;
    let po = round(pv);
    let units_ok = po.wide_length == 2 && po.wide[0] == 0xD83D && po.wide[1] == 0xDE00;

    // AND EACH REFUSAL IS NAMED. A used run with an interior null and a used
    // run whose high surrogate is final are both refused under §4.12's own
    // name text_ill_formed, never read as data.
    let mut null_refused = false;
    {
        let mut v = RootRow::default();
        v.wide[..3].copy_from_slice(&[0x0041, 0x0000, 0x0042]);
        v.wide_length = 3;
        let need = root_fixed_measure(1);
        let mut buf = vec![0u8; need];
        root_fixed_save(&[v], &mut buf).expect("save");
        let mut out = vec![RootRow::default(); 1];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let got = root_fixed_load(&mut out, &buf, &mut plan, &mut remap, &mut report);
        null_refused =
            got.is_none() && report.refused && report.reason.name() == "text_ill_formed";
    }
    let mut lone_refused = false;
    {
        let mut v = RootRow::default();
        v.wide[0] = 0xD83D; // a high surrogate with no low half after it
        v.wide_length = 1;
        let need = root_fixed_measure(1);
        let mut buf = vec![0u8; need];
        root_fixed_save(&[v], &mut buf).expect("save");
        let mut out = vec![RootRow::default(); 1];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let got = root_fixed_load(&mut out, &buf, &mut plan, &mut remap, &mut report);
        lone_refused =
            got.is_none() && report.refused && report.reason.name() == "text_ill_formed";
    }
    say(
        "C5",
        units_ok && null_refused && lone_refused,
        "the wide length is not code units or a named refusal did not fire",
    );
}
`
}

// t19Run generates the unit, drops the probe crate, and runs it once with rustc.
func t19Run(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skipf("rustc is not on PATH: %v", err)
	}
	files, err := GenerateLineage(unitFrom(t, t19RustSchema), nil)
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
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(t19Probe()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "t19probe")
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

// TestFixedRoadmapRustT19WideText holds one subtest per task id this card
// proves. Each subtest reads the running probe's own line for its id.
func TestFixedRoadmapRustT19WideText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Rust toolchain (it compiles and runs the generated unit)")
	out := t19Run(t)
	tasks := []struct {
		id, say string
	}{
		{"utf16-values/rust/valid-data", "utf16-values"},
		{"rust/C5", "C5"},
	}
	for _, task := range tasks {
		task := task
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			if line := t19Line(out, task.say); line != "T19 "+task.say+" ok" {
				t.Fatalf("the rust leg did not carry %s: %s", task.id, line)
			}
		})
	}
}

// t19Line is the probe's one line for a task id.
func t19Line(out, id string) string {
	prefix := "T19 " + id + " "
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return fmt.Sprintf("[no T19 line for %s in the probe's output: %s]", id, strings.TrimSpace(out))
}
