package rusttable

// THE DIRECT AND PAYLOAD-FREE UNION ARM ROWS ON THE RUST FIXED-FORM LEG
// (docs/roadmap.sexp node `fixed-tables`: union-field-arms/rust/valid-data and
// payload-free-arms/rust/valid-data).
//
// Law: docs/SPEC-TABLES.md §2.6 (union arms: payload-free arms and direct
// scalar, text, byte-buffer and bounded-array arms), §3.4 (the fixed record
// form: every field and arm at its declared bound, slack zeroed by the
// template), and docs/FIXED-FORM-ALGORITHM.md §3.1 (write), §4.5 (scatter and
// count/length/tag clamping), §4.6 (read-side bounds and text content rules).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const t18DirectSchema = `package probe

union Body
{
    s string(8)
    b bytes(4)
    a [3]int32
    c [..3]int32
}

fixed table Lineage
{
    lead  int32
    body  Body
    trail int32
}
`

const t18PayloadFreeSchema = `package probe

union Pick
{
    alpha int32
    quiet
    signal
}

fixed table Lineage
{
    lead  int32
    pick  Pick
    trail int32
}
`

// TestFixedRustT18ArmLines is the unit-tier check: it generates the Rust fixed
// form for both schemas in memory (no rustc, no clock, no socket, no network)
// and pins the emitted Row twin and layout assertions for direct companion
// arms, direct fixed-array arms, and payload-free arms.
func TestFixedRustT18ArmLines(t *testing.T) {
	t.Parallel()

	direct := generate(t, t18DirectSchema)
	if _, ok := direct["probe_fixed.rs"]; !ok {
		t.Fatalf("DIRECT schema emitted no probe_fixed.rs; got %v", keysOf(direct))
	}
	drec := string(direct["probe_records.rs"])
	for _, want := range []string{
		"#[repr(C)]\n#[derive(Clone, Copy)]\npub struct BodyRowArmS {\n    pub value: [u8; 9],",
		"    pub value_length: i32,\n}",
		"#[repr(C)]\n#[derive(Clone, Copy)]\npub struct BodyRowArmB {\n    pub value: [u8; 4],",
		"#[repr(C)]\n#[derive(Clone, Copy)]\npub struct BodyRowArmC {\n    pub value: [i32; 3],",
		"    pub value_count: i32,\n}",
		"pub union BodyRowArms {",
		"    pub(crate) s: BodyRowArmS,",
		"    pub(crate) b: BodyRowArmB,",
		"    pub(crate) a: [i32; 3],",
		"    pub(crate) c: BodyRowArmC,",
	} {
		if !strings.Contains(drec, want) {
			t.Errorf("DIRECT probe_records.rs is missing %q", want)
		}
	}

	pfree := generate(t, t18PayloadFreeSchema)
	if _, ok := pfree["probe_fixed.rs"]; !ok {
		t.Fatalf("PAYLOADFREE schema emitted no probe_fixed.rs; got %v", keysOf(pfree))
	}
	prec := string(pfree["probe_records.rs"])
	for _, want := range []string{
		"pub union PickRowArms {\n    pub(crate) alpha: i32,",
		"pub fn is_quiet(&self) -> bool {",
		"pub fn set_quiet(&mut self) {",
		"pub fn is_signal(&self) -> bool {",
		"pub fn set_signal(&mut self) {",
		"const _: () = assert!(core::mem::offset_of!(PickRowArms, alpha) == 0,",
	} {
		if !strings.Contains(prec, want) {
			t.Errorf("PAYLOADFREE probe_records.rs is missing %q", want)
		}
	}
	for _, forbid := range []string{
		"pub(crate) quiet:",
		"pub(crate) signal:",
		"offset_of!(PickRowArms, quiet)",
		"offset_of!(PickRowArms, signal)",
	} {
		if strings.Contains(prec, forbid) {
			t.Errorf("PAYLOADFREE probe_records.rs emitted forbidden %q", forbid)
		}
	}
}

func t18DirectProbe() string {
	return `mod build_version;
mod fixed_runtime;
mod probe_fixed;
mod probe_records;

pub use build_version::*;
pub use fixed_runtime::*;
pub use probe_fixed::*;
pub use probe_records::*;

fn say(arm: &str, ok: bool, why: &str) {
    if ok {
        println!("T18 union-field-arms/rust/valid-data/{arm} ok");
    } else {
        println!("T18 union-field-arms/rust/valid-data/{arm} BAD {why}");
    }
}

fn save_and_load(v: LineageRow) -> (Vec<u8>, LineageRow, TableFixedReport) {
    let need = lineage_fixed_measure(1);
    let mut buf = vec![0u8; need];
    let wrote = lineage_fixed_save(&[v], &mut buf).expect("save");
    assert_eq!(wrote, need, "measure and save disagree");
    let mut out = vec![LineageRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = lineage_fixed_load(&mut out, &buf, &mut plan, &mut remap, &mut report)
        .expect("load");
    assert_eq!(n, 1);
    let start = need - LINEAGE_FIXED_RECORD_BYTES;
    let body = buf[start + 8..start + LINEAGE_FIXED_RECORD_BYTES].to_vec();
    (body, out[0], report)
}

fn clean(r: &TableFixedReport) -> bool {
    !r.refused && !r.malformed && r.clamped == 0 && r.unknown == 0 && r.kind_mismatch == 0
}

fn main() {
    // Arm 1: s string(8) — boundary (full 8 bytes) and edge (empty + partial with dirty slack).
    {
        let mut full = LineageRow::default();
        full.lead = i32::MIN;
        full.trail = i32::MAX;
        full.body.set_s(BodyRowArmS {
            value: *b"abcdefgh\0",
            value_length: 8,
        });
        let (fbody, fout, frep) = save_and_load(full);
        let full_ok = clean(&frep)
            && fout.lead == i32::MIN
            && fout.trail == i32::MAX
            && fout.body.tag() == 1
            && fout.body.s().map(|a| (a.value_length, a.value)) == Some((8, *b"abcdefgh\0"))
            && &fbody[4 + 1 + 4..4 + 1 + 4 + 8] == b"abcdefgh"
            && fbody[4 + 1 + 4 + 8..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        let mut edge = LineageRow::default();
        edge.lead = 11;
        edge.trail = -11;
        let mut dirty = BodyRowArmS {
            value: [0xAA; 9],
            value_length: 0,
        };
        edge.body.set_s(dirty);
        let (ebody, eout, erep) = save_and_load(edge);
        let empty_ok = clean(&erep)
            && eout.lead == 11
            && eout.trail == -11
            && eout.body.tag() == 1
            && eout.body.s().map(|a| (a.value_length, a.value)) == Some((0, [0u8; 9]))
            && ebody[4 + 1..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        dirty.value[..2].copy_from_slice(b"hi");
        dirty.value_length = 2;
        edge.body.set_s(dirty);
        let (pbody, pout, prep) = save_and_load(edge);
        let part_ok = clean(&prep)
            && pout.body.s().map(|a| (a.value_length, &a.value[..2] == b"hi", a.value[2])) == Some((2, true, 0))
            && &pbody[4 + 1 + 4..4 + 1 + 4 + 2] == b"hi"
            && pbody[4 + 1 + 4 + 2..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        say("s", full_ok && empty_ok && part_ok, "string(8) arm boundary, edge or slack failed");
    }

    // Arm 2: b bytes(4) — boundary (full 4 bytes) and edge (empty + partial with dirty slack).
    {
        let mut full = LineageRow::default();
        full.lead = i32::MIN;
        full.trail = i32::MAX;
        full.body.set_b(BodyRowArmB {
            value: [0x00, 0xff, 0x5a, 0x80],
            value_length: 4,
        });
        let (fbody, fout, frep) = save_and_load(full);
        let full_ok = clean(&frep)
            && fout.lead == i32::MIN
            && fout.trail == i32::MAX
            && fout.body.tag() == 2
            && fout.body.b().map(|a| (a.value_length, a.value))
                == Some((4, [0x00, 0xff, 0x5a, 0x80]))
            && &fbody[4 + 1 + 4..4 + 1 + 4 + 4] == &[0x00, 0xff, 0x5a, 0x80]
            && fbody[4 + 1 + 4 + 4..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        let mut edge = LineageRow::default();
        edge.lead = 33;
        edge.trail = -33;
        let mut dirty = BodyRowArmB {
            value: [0xBB; 4],
            value_length: 0,
        };
        edge.body.set_b(dirty);
        let (ebody, eout, erep) = save_and_load(edge);
        let empty_ok = clean(&erep)
            && eout.lead == 33
            && eout.trail == -33
            && eout.body.tag() == 2
            && eout.body.b().map(|a| (a.value_length, a.value)) == Some((0, [0u8; 4]))
            && ebody[4 + 1..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        dirty.value[0] = 0xDE;
        dirty.value[1] = 0xAD;
        dirty.value_length = 2;
        edge.body.set_b(dirty);
        let (pbody, pout, prep) = save_and_load(edge);
        let part_ok = clean(&prep)
            && pout.body.b().map(|a| (a.value_length, a.value)) == Some((2, [0xDE, 0xAD, 0, 0]))
            && pbody[4 + 1 + 4 + 2..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        say("b", full_ok && empty_ok && part_ok, "bytes(4) arm boundary, edge or slack failed");
    }

    // Arm 3: a [3]int32 — boundary ([i32::MIN, 0, i32::MAX]) and edge ([0, 0, 0]).
    {
        let mut full = LineageRow::default();
        full.lead = i32::MIN;
        full.trail = i32::MAX;
        full.body.set_a([i32::MIN, 0, i32::MAX]);
        let (fbody, fout, frep) = save_and_load(full);
        let full_ok = clean(&frep)
            && fout.lead == i32::MIN
            && fout.trail == i32::MAX
            && fout.body.tag() == 3
            && fout.body.a() == Some([i32::MIN, 0, i32::MAX])
            && fbody[4 + 1 + 12..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        let mut edge = LineageRow::default();
        edge.lead = 44;
        edge.trail = -44;
        edge.body.set_a([0, 0, 0]);
        let (ebody, eout, erep) = save_and_load(edge);
        let empty_ok = clean(&erep)
            && eout.lead == 44
            && eout.trail == -44
            && eout.body.tag() == 3
            && eout.body.a() == Some([0, 0, 0])
            && ebody[4 + 1..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        say("a", full_ok && empty_ok, "[3]int32 arm boundary, edge or slack failed");
    }

    // Arm 4: c [..3]int32 — boundary (full 3 elements at i32::MIN/MAX) and edge (count 0 with dirty slack).
    {
        let mut full = LineageRow::default();
        full.lead = i32::MIN;
        full.trail = i32::MAX;
        full.body.set_c(BodyRowArmC {
            value: [i32::MIN, 42, i32::MAX],
            value_count: 3,
        });
        let (fbody, fout, frep) = save_and_load(full);
        let full_ok = clean(&frep)
            && fout.lead == i32::MIN
            && fout.trail == i32::MAX
            && fout.body.tag() == 4
            && fout.body.c().map(|a| (a.value_count, a.value))
                == Some((3, [i32::MIN, 42, i32::MAX]))
            && fbody[4 + 1 + 4 + 12..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        let mut edge = LineageRow::default();
        edge.lead = 55;
        edge.trail = -55;
        let mut dirty = BodyRowArmC {
            value: [99, -99, 77],
            value_count: 0,
        };
        edge.body.set_c(dirty);
        let (ebody, eout, erep) = save_and_load(edge);
        let empty_ok = clean(&erep)
            && eout.lead == 55
            && eout.trail == -55
            && eout.body.tag() == 4
            && eout.body.c().map(|a| (a.value_count, a.value)) == Some((0, [0, 0, 0]))
            && ebody[4 + 1..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        dirty.value[0] = i32::MIN;
        dirty.value_count = 1;
        edge.body.set_c(dirty);
        let (pbody, pout, prep) = save_and_load(edge);
        let part_ok = clean(&prep)
            && pout.body.c().map(|a| (a.value_count, a.value)) == Some((1, [i32::MIN, 0, 0]))
            && pbody[4 + 1 + 4 + 4..LINEAGE_FIXED_BODY_BYTES - 4].iter().all(|&b| b == 0);

        say("c", full_ok && empty_ok && part_ok, "[..3]int32 arm boundary, edge or slack failed");
    }
}
`
}

func t18PayloadFreeProbe() string {
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
        println!("T18 {id} ok");
    } else {
        println!("T18 {id} BAD {why}");
    }
}

fn clean(r: &TableFixedReport) -> bool {
    !r.refused && !r.malformed && r.clamped == 0 && r.unknown == 0 && r.kind_mismatch == 0
}

fn arm_bytes_of_row(p: &PickRow) -> [u8; 4] {
    let raw: &[u8] = unsafe {
        core::slice::from_raw_parts((p as *const PickRow as *const u8).add(4), 4)
    };
    [raw[0], raw[1], raw[2], raw[3]]
}

fn main() {
    // Three records: alpha at i32::MIN, quiet, signal.
    let mut r0 = LineageRow::default();
    r0.lead = -100;
    r0.pick.set_alpha(i32::MIN);
    r0.trail = 100;

    let mut r1 = LineageRow::default();
    r1.lead = -200;
    r1.pick.set_alpha(-1); // dirty the overlay first so set_quiet must zero it
    r1.pick.set_quiet();
    r1.trail = 200;

    let mut r2 = LineageRow::default();
    r2.lead = -300;
    r2.pick.set_alpha(-1); // dirty the overlay first so set_signal must zero it
    r2.pick.set_signal();
    r2.trail = 300;

    let need = lineage_fixed_measure(3);
    let mut buf = vec![0u8; need];
    let wrote = lineage_fixed_save(&[r0, r1, r2], &mut buf).expect("save 3");
    assert_eq!(wrote, need);

    let mut out = vec![LineageRow::default(); 3];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = lineage_fixed_load(&mut out, &buf, &mut plan, &mut remap, &mut report)
        .expect("load 3");
    assert_eq!(n, 3);

    let base = need - 3 * LINEAGE_FIXED_RECORD_BYTES;
    let body0 = &buf[base + 8..base + LINEAGE_FIXED_RECORD_BYTES];
    let body1 = &buf[base + LINEAGE_FIXED_RECORD_BYTES + 8..base + 2 * LINEAGE_FIXED_RECORD_BYTES];
    let body2 = &buf[base + 2 * LINEAGE_FIXED_RECORD_BYTES + 8..base + 3 * LINEAGE_FIXED_RECORD_BYTES];

    let alpha_ok = clean(&report)
        && out[0].lead == -100
        && out[0].trail == 100
        && out[0].pick.tag() == 1
        && out[0].pick.alpha() == Some(i32::MIN)
        && !out[0].pick.is_quiet()
        && !out[0].pick.is_signal()
        && body0[4] == 1;
    say("payload-free-arms/rust/valid-data/alpha", alpha_ok, "alpha arm failed");

    let quiet_ok = clean(&report)
        && out[1].lead == -200
        && out[1].trail == 200
        && out[1].pick.tag() == 2
        && out[1].pick.alpha().is_none()
        && out[1].pick.is_quiet()
        && !out[1].pick.is_signal()
        && body1[4] == 2
        && body1[5..9] == [0, 0, 0, 0]
        && arm_bytes_of_row(&r1.pick) == [0, 0, 0, 0]
        && arm_bytes_of_row(&out[1].pick) == [0, 0, 0, 0];
    say("payload-free-arms/rust/valid-data/quiet", quiet_ok, "quiet payload-free arm failed");

    let signal_ok = clean(&report)
        && out[2].lead == -300
        && out[2].trail == 300
        && out[2].pick.tag() == 3
        && out[2].pick.alpha().is_none()
        && !out[2].pick.is_quiet()
        && out[2].pick.is_signal()
        && body2[4] == 3
        && body2[5..9] == [0, 0, 0, 0]
        && arm_bytes_of_row(&r2.pick) == [0, 0, 0, 0]
        && arm_bytes_of_row(&out[2].pick) == [0, 0, 0, 0];
    say("payload-free-arms/rust/valid-data/signal", signal_ok, "signal payload-free arm failed");

    // tag-past-the-arm-set: forge tag byte > 3 and non-zero arm payload bytes;
    // load clamps tag to None (0), increments report.clamped once, and never
    // reads the forged payload into the overlay.
    let mut forged = vec![0u8; lineage_fixed_measure(1)];
    lineage_fixed_save(&[r0], &mut forged).expect("save 1");
    let rec_body = forged.len() - LINEAGE_FIXED_RECORD_BYTES + 8;
    forged[rec_body + 4] = 99; // past Pick's 3 arms
    forged[rec_body + 5..rec_body + 9].copy_from_slice(&0x7f7f7f7fu32.to_le_bytes());

    let mut fout = vec![LineageRow::default(); 1];
    fout[0].pick.set_alpha(12345);
    let mut freport = TableFixedReport::default();
    let fn_read = lineage_fixed_load(&mut fout, &forged, &mut plan, &mut remap, &mut freport)
        .expect("forged tag loads with clamp");
    let clamp_ok = fn_read == 1
        && !freport.refused
        && !freport.malformed
        && freport.clamped == 1
        && fout[0].lead == -100
        && fout[0].trail == 100
        && fout[0].pick.tag() == 0
        && fout[0].pick.alpha().is_none()
        && !fout[0].pick.is_quiet()
        && !fout[0].pick.is_signal()
        && arm_bytes_of_row(&fout[0].pick) == [0, 0, 0, 0];
    say("tag-past-the-arm-set/pick", clamp_ok, "tag past the arm set did not clamp cleanly");
}
`
}

func t18RunProbe(t *testing.T, schema, mainSrc, binName string) string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Fatalf("rustc is not on PATH: %v", err)
	}
	files, err := GenerateLineage(unitFrom(t, schema), nil)
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
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(mainSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, binName)
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

func TestFixedRoadmapRustT18UnionArms(t *testing.T) {
	t.Parallel()
	directOut := t18RunProbe(t, t18DirectSchema, t18DirectProbe(), "t18direct")
	pfreeOut := t18RunProbe(t, t18PayloadFreeSchema, t18PayloadFreeProbe(), "t18pfree")
	combined := directOut + pfreeOut
	t.Logf("t18 probe lines:\n%s", strings.TrimSpace(combined))

	t.Run("union-field-arms/rust/valid-data", func(t *testing.T) {
		t.Parallel()
		for _, arm := range []string{"s", "b", "a", "c"} {
			id := "union-field-arms/rust/valid-data/" + arm
			want := "T18 " + id + " ok"
			if got := t18Line(directOut, id); got != want {
				t.Fatalf("direct union arm %s failed: got %q, want %q", arm, got, want)
			}
		}
	})

	t.Run("payload-free-arms/rust/valid-data", func(t *testing.T) {
		t.Parallel()
		for _, arm := range []string{"alpha", "quiet", "signal"} {
			id := "payload-free-arms/rust/valid-data/" + arm
			want := "T18 " + id + " ok"
			if got := t18Line(pfreeOut, id); got != want {
				t.Fatalf("payload-free union arm %s failed: got %q, want %q", arm, got, want)
			}
		}
	})

	t.Run("tag-past-the-arm-set", func(t *testing.T) {
		t.Parallel()
		id := "tag-past-the-arm-set/pick"
		want := "T18 " + id + " ok"
		if got := t18Line(pfreeOut, id); got != want {
			t.Fatalf("tag-past-the-arm-set failed: got %q, want %q", got, want)
		}
	})
}

func t18Line(out, id string) string {
	prefix := "T18 " + id + " "
	for l := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return fmt.Sprintf("[no T18 line for %s in the probe's output: %s]", id, strings.TrimSpace(out))
}
