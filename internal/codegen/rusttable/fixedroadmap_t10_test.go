package rusttable

// THE ROADMAP'S COMPOSITES AND OPTIONALS TASKS ON THE RUST LEG
// (docs/roadmap.sexp node `fixed-tables`; docs/SPEC-TABLES.md §3.4's record
// form, "every field at its bound", and docs/FIXED-FORM-ALGORITHM.md §3.1 write
// / §4.4 run). One subtest per task id, table-driven, t.Parallel() first.
//
// Each task is [capability]: "the leg writes and reads the construct with
// non-default and boundary values, asserted, in a target a gate runs". THE
// TARGET IS A RUST BINARY THIS TEST BUILDS AND RUNS, exactly as the leg's own
// fixed-form harness (fixedroadmap_t09_test.go) does: the fixed runtime, the
// record module and the codec module are self-contained, so the probe compiles
// with `rustc` alone and the subtests read the running binary's own lines.
//
// EVERY ROW IS ONE TASK: the probe writes ONE Lineage record whose construct
// carries non-default and boundary values, reads it back through the leg's own
// `lineage_fixed_save`/`lineage_fixed_load`, and says `T10 <id> ok` or
// `T10 <id> BAD <why>`. The declared defaults ride the emitted PREFILL image
// (`LINEAGE_FIXED_DEFAULTS`), so a defaults row is proved by writing the values
// the declaration names and holding the bytes to that image.
//
// `union-field-arms` is NOT among these subtests: its node asks for a DIRECT
// text or array arm payload (docs/roadmap.sexp, union-field-arms/rust/valid-data
// `:remaining` "Implement direct text/byte-buffer/array arm layout and
// emitters"), and this backend writes and reads neither. The shared gate
// ADMITS them — ir.TableFixedSupported (ir/fixedform.go:241) refuses only a
// pointer, map, unbounded array, keyed array or optional arm, so a direct
// string(N) or [..N]T arm falls through to true — but this backend's own
// unionRowable (internal/codegen/rusttable/cook.go:391) refuses those same
// arms, and the effect is a SILENT drop of the WHOLE fixed form: GenerateLineage
// returns only build_version.rs, no `<base>_fixed.rs`. The supported shape (a
// text or array nested inside a type arm) is the sibling task union-values; the
// direct scalar arm alone does not prove a title that also names text and
// array. That row is reported :unknown in docs/roadmap-evidence/fixed-rust.sexp.
//
// `payload-free-arms` is also NOT among these subtests: the shared eligibility
// rule admits a payload-free arm (ir/fixedform.go's TableFixedSupported skips a
// nil arm), but this backend still emits no fixed form for a closure that holds
// one — GenerateLineage returns only build_version.rs and no `<base>_fixed.rs`
// — so there is no writer or reader to exercise. That row is reported :unknown
// in docs/roadmap-evidence/fixed-rust.sexp.
//
// `utf16-values` is NOT among these subtests: the Rust target refuses a
// `wstring(N)` field in a table closure (compiler/target_rust.go:30 calls
// refuseWideText, and rust is absent from tableWideTextTargets at
// compiler/widetext.go:25), so a table fixture that declared one would only
// reach an emitter branch the registered target never enters — the bypass
// §5.9 #44 forbids folding into a green (internal/codegen/rusttable/
// fixedversioning_test.go:230 refuses the same shape by name). The fixture
// therefore declares no wstring field, and that row is reported :unknown in
// docs/roadmap-evidence/fixed-rust.sexp.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// t10Schema is one fixed table that declares every construct this card proves.
// The union's arms are named types (a scalar, a text and an array field), the
// optional scalar/enum and optional array carry presence bytes, and the
// scalar/enum/float, string, bytes and flags fields each declare a default
// (SPEC-TABLES §3.4, §2.6).
const t10Schema = `package probe

enum Grade { Bronze, Silver, Gold }

flags Perks { Shielded, Cloaked, Hasted }

type ScalarArm { n int32 }
type TextArm   { s string(8) }
type ArrayArm  { a [..3]int32 }

union Pick
{
    sa ScalarArm
    ta TextArm
    aa ArrayArm
}

union Scalar
{
    cnt  int32
    flag bool
}

fixed table Lineage
{
    lead   int32
    picks  [..4]Pick
    sc     Scalar
    a      ?int32
    g      ?Grade
    arr    ?[..8]int32
    n      int32 = 7
    gr     Grade = Gold
    f      float32 = 2.5
    s      string(8) = "hi"
    b      bytes(4) = "ab"
    p      Perks = { Cloaked }
}
`

// t10Probe is the Rust binary's crate root: the generated fixed modules beside
// it, one writer of each construct's non-default and boundary values, one
// reader through the leg's own load, and one line per task.
func t10Probe() string {
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
        println!("T10 {id} ok");
    } else {
        println!("T10 {id} BAD {why}");
    }
}

// round writes one record and reads it back, asserting a clean read.
fn round(v: LineageRow) -> LineageRow {
    let need = lineage_fixed_measure(1);
    let mut buf = vec![0u8; need];
    let wrote = lineage_fixed_save(&[v], &mut buf).expect("the writer fills the buffer");
    assert!(wrote == need, "measure and save disagree");
    let mut out = vec![LineageRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = lineage_fixed_load(&mut out, &buf, &mut plan, &mut remap, &mut report)
        .expect("the buffer this writer wrote reads");
    assert!(
        n == 1 && !report.refused && !report.malformed && report.clamped == 0,
        "clean read: {report:?}"
    );
    out[0]
}

// body_of writes one record and returns its body, the bytes the plan lands.
fn body_of(v: &LineageRow) -> Vec<u8> {
    let need = lineage_fixed_measure(1);
    let mut buf = vec![0u8; need];
    lineage_fixed_save(&[*v], &mut buf).expect("save");
    let start = need - LINEAGE_FIXED_RECORD_BYTES;
    buf[start + 8..start + LINEAGE_FIXED_RECORD_BYTES].to_vec()
}

// declared_defaults_row is a record holding exactly the values the declaration
// names: the scalar/enum/float, string, bytes and flags defaults.
fn declared_defaults_row() -> LineageRow {
    let mut v = LineageRow::default(); // carries the named text/bytes defaults
    v.n = 7;
    v.gr = 3; // Gold
    v.f = 2.5;
    v.p = 0b010; // { Cloaked }
    v
}

fn main() {
    // NON-DEFAULT AND BOUNDARY values, one per construct (§3.4).
    let mut v = LineageRow::default();
    v.lead = i32::MIN;
    v.picks_count = 2;
    v.picks[0].set_sa(ScalarArmRow { n: i32::MIN });
    v.picks[1].set_ta({
        let mut t = TextArmRow::default();
        t.s_length = 6;
        t.s[..6].copy_from_slice(b"abcdef");
        t
    });
    v.a_present = true;
    v.a = i32::MIN;
    v.g_present = true;
    v.g = 3; // Gold, the declared extent's top
    v.arr_present = true;
    v.arr_count = 3;
    v.arr[0] = i32::MIN;
    v.arr[1] = 0;
    v.arr[2] = i32::MAX;
    v.n = -9;
    v.gr = 1; // Bronze
    v.f = -2.25;
    v.s_length = 3;
    v.s[..3].copy_from_slice(b"xyz");
    v.b_length = 4;
    v.b = [0x00, 0xff, 0x5a, 0x01];
    v.p = 0b101;
    let o = round(v);

    // union-arrays: the count then the arm images.
    let union_ok = o.picks_count == 2
        && o.picks[0].sa().map(|a| a.n) == Some(i32::MIN)
        && o.picks[1].ta().map(|t| t.s_length) == Some(6);
    say(
        "union-arrays",
        union_ok,
        "an element of the array of unions did not land",
    );

    // optional-scalars: the present byte then the payload; and the absent end.
    let mut v5 = LineageRow::default();
    v5.a_present = false;
    v5.g_present = false;
    let o5 = round(v5);
    let opt_ok = o.a_present
        && o.a == i32::MIN
        && o.g_present
        && o.g == 3
        && !o5.a_present
        && !o5.g_present;
    say(
        "optional-scalars",
        opt_ok,
        "an optional scalar or enum did not land beside its present byte",
    );

    // optional-arrays: the present byte, the count and the elements.
    let optarr_ok = o.arr_present && o.arr_count == 3 && o.arr[2] == i32::MAX;
    say(
        "optional-arrays",
        optarr_ok,
        "the optional array's count or an element did not land",
    );

    // scalar-defaults: the declared image is the write of the declared values,
    // and a non-default value round-trips.
    let img = body_of(&declared_defaults_row());
    let scalar_defaults_ok = img.as_slice() == &LINEAGE_FIXED_DEFAULTS[..]
        && o.n == -9
        && o.gr == 1
        && o.f.to_bits() == (-2.25f32).to_bits();
    say(
        "scalar-defaults",
        scalar_defaults_ok,
        "the declared scalar/enum/float defaults are not the prefill image",
    );

    // text-bytes-flags-defaults: the named text/bytes defaults live on the
    // constructed row, the flags default rides the image, and the boundary
    // values round-trip.
    let d = LineageRow::default();
    let text_defaults_ok = d.s_length == 2
        && &d.s[..2] == b"hi"
        && d.b_length == 2
        && &d.b[..2] == b"ab";
    let tb_ok = text_defaults_ok
        && img.as_slice() == &LINEAGE_FIXED_DEFAULTS[..]
        && o.s_length == 3
        && &o.s[..3] == b"xyz"
        && o.b_length == 4
        && o.b == [0x00, 0xff, 0x5a, 0x01]
        && o.p == 0b101;
    say(
        "text-bytes-flags-defaults",
        tb_ok,
        "a declared text, bytes or flags default did not ride or round-trip",
    );
}
`
}

// t10Run generates the unit, drops the probe crate, and runs it once with rustc.
func t10Run(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Fatalf("rustc is not on PATH: %v", err)
	}
	files, err := GenerateLineage(unitFrom(t, t10Schema), nil)
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
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(t10Probe()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "t10probe")
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

// TestFixedRoadmapRustT10CompositesOptionals holds one subtest per task id this
// card proves. Each subtest reads the running probe's own line for its id.
func TestFixedRoadmapRustT10CompositesOptionals(t *testing.T) {
	t.Parallel()
	out := t10Run(t)
	tasks := []struct {
		id  string
		say string
	}{
		{"union-arrays/rust/valid-data", "union-arrays"},
		{"optional-scalars/rust/valid-data", "optional-scalars"},
		{"optional-arrays/rust/valid-data", "optional-arrays"},
		{"scalar-defaults/rust/valid-data", "scalar-defaults"},
		{"text-bytes-flags-defaults/rust/valid-data", "text-bytes-flags-defaults"},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			if line := t10Line(out, task.say); line != "T10 "+task.say+" ok" {
				t.Fatalf("the rust leg did not write and read %s with non-default and boundary values: %s", task.id, line)
			}
		})
	}
}

// t10Line is the probe's one line for a task id.
func t10Line(out, id string) string {
	prefix := "T10 " + id + " "
	for l := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return fmt.Sprintf("[no T10 line for %s in the probe's output: %s]", id, strings.TrimSpace(out))
}
