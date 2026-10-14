package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// THE ROADMAP'S ORDINARY-CAPABILITY VALUE TASKS ON THE RUST LEG (docs/roadmap.sexp
// node `fixed-tables`, the `*/rust/valid-data` children of signed-integers,
// unsigned-integers, signed-128, unsigned-128, bits-values, float64-values,
// compressed-floats, signed-fixed-point, unsigned-fixed-point, flags-values and
// enum-values; docs/SPEC-TABLES.md §3.4's record: "every field at its declared
// storage width"). A capability is proved by WRITING and READING the construct
// with NON-DEFAULT and BOUNDARY values and asserting what lands.
//
// THE TARGET IS A RUST BINARY THIS TEST BUILDS AND RUNS. The fixed form's shared
// runtime (fixed_runtime.rs) and the unit's record and codec modules are
// self-contained: a unit whose tables are all FIXED grows no packet module and
// therefore depends on no `serialize` crate. So the probe compiles with `rustc`
// alone — no sibling runtime checkout, no cargo workspace — and the test holds
// the running binary's own verdict lines.
//
// ONE ROW IS ONE TASK: the writer fills the boundary value of a task's widths,
// the reader lands them, and the check compares them at the field's own width.
// A task's line is `T09 <id> ok` or `T09 <id> BAD <why>`; the Go subtests read
// their own line out of the one run, exactly as fixedversioning_test.go reads a
// row out of its one cargo run.

// t09Schema is one fixed table that declares every kind this card proves, each
// at its declared storage width (§3.4): the four signed and four unsigned
// integer widths, a raw uint128 and a ranged int128, a bits(3) mask, float64,
// a compressed float (float32 with a range), signed and unsigned fixed-point,
// a flags mask, an enum, and a nested type.
const t09Schema = `package probe

enum Grade { Bronze, Silver, Gold }

flags Caps { Jump, Crouch, Fly }

type Inner
{
    factor float32 = 2.5
}

fixed table Config
{
    i8v    int8
    i16v   int16
    i32v   int32
    i64v   int64
    u8v    uint8
    u16v   uint16
    u32v   uint32
    u64v   uint64
    i128v  int128 | min = -5000000000, max = 5000000000
    u128v  uint128
    bv     bits(3)
    f64v   float64
    cf     float32 | min = -1, max = 1, resolution = 0.01
    sfix   fixed(16, 16)  | min = -8, max = 8
    ufix   ufixed(16, 16) | min = 0, max = 16
    caps   Caps
    grade  Grade = Silver
    inner  Inner
}
`

// t09Probe is the Rust binary's crate root: the generated fixed-form modules
// beside it, one writer of the boundary values, one reader of the same buffer,
// and one line per task whose field set rounded exactly.
//
// THE SIGNED 128-BIT ROW IS EXERCISED AT BOTH DECLARED RANGE EDGES, because the
// node (docs/roadmap.sexp, signed-128/rust/valid-data) requires "nonzero high
// 64-bit limbs and declared range edges on the fixed wire": the MINIMUM edge's
// two's-complement high 64 bits are all ones (a writer that dropped the high
// limb would be caught) and the MAXIMUM edge pins the other end. THE ENUM ROW IS
// EXERCISED AT A NON-DEFAULT ORDINAL AND AT NONE, because the node's title is
// "Enums and None" and its remaining asks for "enum-values values/boundaries not
// covered by cited assertions": `Gold` is the declared-extent boundary (the
// scatter's `raw > extent` clamp sits just past it) and `None = 0` is the other
// boundary the wire admits, while `Silver` (the declared default) would prove
// nothing a default construction does not already hold.
func t09Probe() string {
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
        println!("T09 {id} ok");
    } else {
        println!("T09 {id} BAD {why}");
    }
}

// round writes one record and reads it back, asserting a clean read; it is the
// one write/read the row's verdict compares against.
fn round(v: ConfigRow) -> ConfigRow {
    let need = config_fixed_measure(1);
    let mut buf = vec![0u8; need];
    let wrote = config_fixed_save(&[v], &mut buf).expect("the writer fills the buffer");
    assert!(wrote == need, "measure and save disagree");
    let mut out = vec![ConfigRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = config_fixed_load(&mut out, &buf, &mut plan, &mut remap, &mut report)
        .expect("the buffer this writer wrote reads");
    assert!(n == 1 && !report.refused && !report.malformed, "clean read: {report:?}");
    out[0]
}

fn main() {
    let mut v = ConfigRow::default();
    // NON-DEFAULT AND BOUNDARY values, one per width (§3.4's "declared storage
    // width"): the extreme of every integer, the largest small mask, a range
    // end on each bounded kind, and a value whose low bytes carry no zeroes.
    v.i8v = i8::MIN;
    v.i16v = i16::MAX;
    v.i32v = i32::MIN;
    v.i64v = i64::MAX;
    v.u8v = u8::MAX;
    v.u16v = u16::MAX;
    v.u32v = u32::MAX;
    v.u64v = u64::MAX;
    // the declared MIN edge: its two's-complement high 64 bits are all ones
    v.i128v = TableI128(-5_000_000_000i128);
    v.u128v = TableU128(u128::MAX);
    v.bv = 7;
    v.f64v = 3.141592653589793f64;
    v.cf = 0.5f32;
    v.sfix = 524_288i32;
    v.ufix = 1_048_576u32;
    v.caps = 0b101u64;
    // a NON-DEFAULT ordinal: Gold is the declared extent's top (its boundary)
    v.grade = 3u8;
    v.inner.factor = -2.5f32;

    let o = round(v);

    // THE OTHER DECLARED int128 EDGE: the writer must land +max as itself too.
    let mut vmax = v;
    vmax.i128v = TableI128(5_000_000_000i128);
    let omax = round(vmax);

    // THE ENUM'S NONE BOUNDARY: ordinal 0 is admitted beside the variants.
    let mut vnone = v;
    vnone.grade = 0u8;
    let onone = round(vnone);

    say(
        "signed-integers",
        o.i8v == v.i8v && o.i16v == v.i16v && o.i32v == v.i32v && o.i64v == v.i64v,
        "an int8/int16/int32/int64 boundary did not land at its width",
    );
    say(
        "unsigned-integers",
        o.u8v == v.u8v && o.u16v == v.u16v && o.u32v == v.u32v && o.u64v == v.u64v,
        "a uint8/uint16/uint32/uint64 boundary did not land at its width",
    );
    say(
        "signed-128",
        o.i128v.0 == v.i128v.0 && v.i128v.0 == -5_000_000_000i128
            && omax.i128v.0 == vmax.i128v.0 && vmax.i128v.0 == 5_000_000_000i128,
        "a declared int128 range edge did not land at its 128-bit width",
    );
    say(
        "unsigned-128",
        o.u128v.0 == v.u128v.0,
        "a raw uint128 above 2^64 landed as a different value",
    );
    say(
        "bits-values",
        o.bv == v.bv && o.bv == 7,
        "bits(3)'s largest value did not land as itself",
    );
    say(
        "float64-values",
        o.f64v.to_bits() == v.f64v.to_bits(),
        "a float64 did not land bit-exactly",
    );
    say(
        "compressed-floats",
        o.cf.to_bits() == v.cf.to_bits() && o.cf == 0.5,
        "a compressed float did not land as its float32",
    );
    say(
        "signed-fixed-point",
        o.sfix == v.sfix && o.sfix == 524_288,
        "a signed fixed-point raw value did not land",
    );
    say(
        "unsigned-fixed-point",
        o.ufix == v.ufix && o.ufix == 1_048_576,
        "an unsigned fixed-point raw value did not land",
    );
    say(
        "flags-values",
        o.caps == v.caps && o.caps == 0b101,
        "a flags mask did not land as its bits",
    );
    say(
        "enum-values",
        o.grade == v.grade && o.grade == 3
            && onone.grade == vnone.grade && onone.grade == 0,
        "an enum ordinal or the None boundary did not land on the wire",
    );
}
`
}

// t09Run generates the unit, drops the probe crate, and runs it once with rustc.
func t09Run(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Fatalf("rustc is not on PATH: %v", err)
	}
	files, err := GenerateLineage(unitFrom(t, t09Schema), nil)
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
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(t09Probe()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "t09probe")
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

// TestFixedRoadmapRustT09IntegersFloats holds one subtest per task id this card
// proves. Each subtest reads the running probe's own line for its id.
func TestFixedRoadmapRustT09IntegersFloats(t *testing.T) {
	t.Parallel()
	out := t09Run(t)
	tasks := []string{
		"signed-integers",
		"unsigned-integers",
		"signed-128",
		"unsigned-128",
		"bits-values",
		"float64-values",
		"compressed-floats",
		"signed-fixed-point",
		"unsigned-fixed-point",
		"flags-values",
		"enum-values",
	}
	for _, id := range tasks {
		id := id
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			if line := t09Line(out, id); line != "T09 "+id+" ok" {
				t.Fatalf("the rust leg did not write and read %s with boundary values: %s", id, line)
			}
		})
	}
}

// t09Line is the probe's one line for a task id.
func t09Line(out, id string) string {
	prefix := "T09 " + id + " "
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return fmt.Sprintf("[no T09 line for %s in the probe's output: %s]", id, strings.TrimSpace(out))
}
