package rusttable

// THE FINITE RULE ON THE RUST LEG (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27:
// "Tables go finite-only too, same rule as packets. NaN/inf refused on write
// and read, -0 and subnormals kept"). The Go leg's twin is
// gotable/floatspecial_test.go (TestGoPlainFloatSpecialValuesAgreeAcrossForms).
//
// ONE PROBE, EVERY FORM THIS LEG CARRIES, every NaN class and both infinities:
//
//   - the VARIABLE form: the writer refuses by name (<root>_save_checked), the
//     tolerant read refuses on the raw bits, at both widths, as an element, in
//     a nested table and across the float rung (kind 10 into a float64), and a
//     -0 under a +0 default rides because elision compares bits;
//   - a RETAINED UNKNOWN float (ruling 3): the retaining read refuses one, at
//     kind 10 and kind 11, named <Table>.0x<id>; the plain read, which keeps
//     nothing, skips it as the Go oracle's plain read does;
//   - the MESSAGE form: measure and save answer -1 for a plain, an element, a
//     nested, a union arm and a COMPRESSED float (refused before the quantizer,
//     ruling 1), and a batch holding one in any body delivers nothing;
//   - the FIXED form: the save refuses and writes no byte, the read's finite
//     pass refuses before the bounds pass (a compressed float included) and
//     resets every record of the batch, and a counted array's slack is not a
//     value;
//   - the COOK writer refuses and writes no byte.
//
// Ruling 2 (a decoding block or cook accessor latches the refusal) has no site
// on this leg: a block projection and a cooked record are the caller's #[repr(C)]
// structs read in place (block.go emitProjectionField, cook.go emitCookHandle),
// and no generated accessor decodes a float.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. It skips by name
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name
// when SCHEMA_REQUIRE_CORPUS=1, which is how make tables-rust-finite-float runs
// it; a plain go test skips it behind slowtest.Gate (SCHEMA_SLOW=1 runs it). The Go half reads only the generator's output: no clock, no socket.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const finiteFloatSchema = `package probe

type Inner
{
    w float64
}

union Pick
{
    f float32
    n int32
}

fixed table Probe
{
    a     float32
    b     float64
    arr   [3]float32
    live  [..3]float64
    inner Inner
    pick  Pick
    aim   float32 | min = -1, max = 1, resolution = 0.01
}

table Node
{
    v float32
}

table Loose
{
    x    float32
    y    float64
    list [..4]float32
    kids [..2]Node
}

fixed table Cooked
{
    f float32
    d float64
}
`

// finiteFloatProbe is the probe's body; the module lines are written in front
// of it by runFiniteFloatProbe.
const finiteFloatProbe = `
// THE FINITE RULE ON THE RUST LEG (docs/SPEC-TABLES.md §3.3): every read
// refuses a NaN or an infinity on its raw bits as non_finite_float, naming the
// field, every writer refuses one, and -0 and the subnormals ride bit for bit.
// One line per clause: "FINITE <id> ok" or "FINITE <id> BAD <why>".

fn say(id: &str, ok: bool, why: String) {
    if ok {
        println!("FINITE {id} ok");
    } else {
        println!("FINITE {id} BAD {why}");
    }
}

const BAD32: [u32; 5] = [0x7FC0_0000, 0x7F80_0001, 0xFFA5_A5A5, 0x7F80_0000, 0xFF80_0000];
const BAD64: [u64; 5] = [
    0x7FF8_0000_0000_0000,
    0x7FF0_0000_0000_0001,
    0xFFF5_A5A5_A5A5_A5A5,
    0x7FF0_0000_0000_0000,
    0xFFF0_0000_0000_0000,
];
const SUB32: u32 = 0x007F_FFFF;
const SUB64: u64 = 0x000F_FFFF_FFFF_FFFF;
const NEG0_32: u32 = 0x8000_0000;
const NEG0_64: u64 = 0x8000_0000_0000_0000;
// distinct finite markers a test patches to a non-finite pattern
const MARK32: u32 = 0x3F9E_0419;
const MARK64: u64 = 0x3FF3_C083_126E_978D;

fn patch(buf: &mut [u8], from: &[u8], to: &[u8]) -> bool {
    match buf.windows(from.len()).position(|w| w == from) {
        Some(at) => {
            buf[at..at + to.len()].copy_from_slice(to);
            true
        }
        None => false,
    }
}

// THE MESSAGE FORM IS A BIT STREAM, low bit first (§3.3): a value can sit at
// any bit offset, so the marker is found and replaced bit by bit.
fn bit(buf: &[u8], at: usize) -> u64 {
    ((buf[at / 8] >> (at % 8)) & 1) as u64
}

fn patch_bits(buf: &mut [u8], from: u64, to: u64, width: usize) -> bool {
    let total = buf.len() * 8;
    for start in 16..total.saturating_sub(width - 1) {
        let mut v = 0u64;
        for k in 0..width {
            v |= bit(buf, start + k) << k;
        }
        if v == from {
            for k in 0..width {
                let at = start + k;
                if (to >> k) & 1 == 1 {
                    buf[at / 8] |= 1 << (at % 8);
                } else {
                    buf[at / 8] &= !(1 << (at % 8));
                }
            }
            return true;
        }
    }
    false
}

fn names() {
    say("names/variable", TableReason::NonFiniteFloat.name() == "non_finite_float", TableReason::NonFiniteFloat.name().to_string());
    say("names/fixed", TableFixedReason::NonFiniteFloat.name() == "non_finite_float", TableFixedReason::NonFiniteFloat.name().to_string());
}

// ---- the variable form -------------------------------------------------------

fn refused(report: &TableReport, field: &str) -> Result<(), String> {
    if !report.refused || report.reason != TableReason::NonFiniteFloat || report.non_finite_float != field {
        return Err(format!("want the refusal non_finite_float naming {field}: {report:?}"));
    }
    if report.non_finite_float_reason().as_deref() != Some(&format!("non_finite_float: {field}")[..]) {
        return Err(format!("the reason is {:?}", report.non_finite_float_reason()));
    }
    Ok(())
}

fn variable() {
    // -0 UNDER A +0 DEFAULT RIDES, and so does every subnormal (§3.3)
    let v = Loose {
        x: f32::from_bits(NEG0_32),
        y: f64::from_bits(NEG0_64),
        list: vec![f32::from_bits(SUB32), f32::from_bits(NEG0_32)],
        kids: vec![Node { v: f32::from_bits(NEG0_32) }],
    };
    let wire = loose_save(&v);
    let mut report = TableReport::default();
    let back = loose_load(&wire, &mut report);
    let ok = back.as_ref().is_some_and(|b| {
        b.x.to_bits() == NEG0_32
            && b.y.to_bits() == NEG0_64
            && b.list.iter().map(|x| x.to_bits()).collect::<Vec<_>>() == [SUB32, NEG0_32]
            && b.kids.len() == 1
            && b.kids[0].v.to_bits() == NEG0_32
    });
    say("variable/minus-zero-and-subnormal-ride", ok && report == TableReport::default(), format!("{back:?} {report:?}"));
    let sub = Loose { x: f32::from_bits(SUB32), y: f64::from_bits(SUB64), ..Default::default() };
    let back = loose_load(&loose_save(&sub), &mut TableReport::default());
    say(
        "variable/subnormal-rides",
        back.as_ref().is_some_and(|b| b.x.to_bits() == SUB32 && b.y.to_bits() == SUB64),
        format!("{back:?}"),
    );

    // THE WRITER REFUSES, BY NAME, every NaN and both infinities
    for (i, &b32) in BAD32.iter().enumerate() {
        let b64 = BAD64[i];
        for (what, value, field) in [
            ("x", Loose { x: f32::from_bits(b32), ..Default::default() }, "Loose.x"),
            ("y", Loose { y: f64::from_bits(b64), ..Default::default() }, "Loose.y"),
            ("list", Loose { list: vec![1.0, f32::from_bits(b32)], ..Default::default() }, "Loose.list"),
            ("kids", Loose { kids: vec![Node { v: f32::from_bits(b32) }], ..Default::default() }, "Node.v"),
        ] {
            let got = loose_save_checked(&value);
            say(
                &format!("variable/writer-refuses/{what}/{i}"),
                got.as_ref().err().map(String::as_str) == Some(&format!("non_finite_float: {field}")[..]),
                format!("{got:?}"),
            );
        }
    }

    // EVERY READ REFUSES on the raw bits, the tolerant read included
    for (i, &b32) in BAD32.iter().enumerate() {
        let b64 = BAD64[i];
        let cases: [(&str, Loose, Vec<u8>, Vec<u8>, &str); 4] = [
            ("x", Loose { x: f32::from_bits(MARK32), ..Default::default() }, MARK32.to_le_bytes().to_vec(), b32.to_le_bytes().to_vec(), "Loose.x"),
            ("y", Loose { y: f64::from_bits(MARK64), ..Default::default() }, MARK64.to_le_bytes().to_vec(), b64.to_le_bytes().to_vec(), "Loose.y"),
            ("list", Loose { list: vec![1.0, f32::from_bits(MARK32)], ..Default::default() }, MARK32.to_le_bytes().to_vec(), b32.to_le_bytes().to_vec(), "Loose.list"),
            ("kids", Loose { kids: vec![Node { v: f32::from_bits(MARK32) }], ..Default::default() }, MARK32.to_le_bytes().to_vec(), b32.to_le_bytes().to_vec(), "Node.v"),
        ];
        for (what, value, from, to, field) in cases {
            let mut wire = loose_save(&value);
            if !patch(&mut wire, &from, &to) {
                say(&format!("variable/read-refuses/{what}/{i}"), false, "the marker is not in the wire".to_string());
                continue;
            }
            let mut report = TableReport::default();
            let got = loose_load(&wire, &mut report);
            let verdict = if got.is_some() { Err(format!("the read landed {got:?}")) } else { refused(&report, field) };
            say(&format!("variable/read-refuses/{what}/{i}"), verdict.is_ok(), verdict.err().unwrap_or_default());
        }
    }

    // THE FLOAT RUNG, kind 10 into a float64: the ordinary conversion for a
    // finite float32, and the refusal for a NaN or an infinity. The wire is
    // the one-field file loose_save writes for y, with y re-spelled at kind 10.
    let one = loose_save(&Loose { y: 1.0, ..Default::default() });
    let trailer = one[one.len() - 16..].to_vec();
    let rung = |bits: u32| -> Vec<u8> {
        let mut w = vec![1u8, 1, 10];
        w.extend_from_slice(&bits.to_le_bytes());
        w.push(0);
        w.extend_from_slice(&trailer);
        w
    };
    let mut report = TableReport::default();
    let got = loose_load(&rung(0x3FC0_0000), &mut report);
    say(
        "variable/float-rung-widens",
        got.as_ref().is_some_and(|b| b.y == 1.5) && report.widened == 1 && !report.refused,
        format!("{got:?} {report:?}"),
    );
    for (i, &b32) in BAD32.iter().enumerate() {
        let mut report = TableReport::default();
        let got = loose_load(&rung(b32), &mut report);
        let verdict = if got.is_some() { Err(format!("the read landed {got:?}")) } else { refused(&report, "Loose.y") };
        say(&format!("variable/float-rung-refuses/{i}"), verdict.is_ok(), verdict.err().unwrap_or_default());
    }

    // A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE (ruling 3): a field
    // id this reader cannot name, at kind 10 and kind 11.
    let unknown = |kind: u8, payload: &[u8]| -> Vec<u8> {
        let mut w = vec![1u8, 1, kind];
        w.extend_from_slice(payload);
        w.push(0);
        w.extend_from_slice(&0x0123_4567_89AB_CDEFu64.to_le_bytes());
        w.extend_from_slice(&1u64.to_le_bytes());
        w
    };
    let retain = |wire: &[u8]| -> (Option<Loose>, TableReport) {
        let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: Vec::with_capacity(512) };
        let mut report = TableReport::default();
        let got = loose_load_retain(wire, &mut store, &mut report);
        (got, report)
    };
    let (got, report) = retain(&unknown(10, &1.5f32.to_bits().to_le_bytes()));
    say(
        "retain/finite-unknown-is-kept",
        got.is_some() && report.unknown == 1 && report.retained == 1,
        format!("{got:?} {report:?}"),
    );
    for (i, &b32) in BAD32.iter().enumerate() {
        for (kind, payload) in [(10u8, b32.to_le_bytes().to_vec()), (11u8, BAD64[i].to_le_bytes().to_vec())] {
            let (got, report) = retain(&unknown(kind, &payload));
            let ok = got.is_none()
                && report.refused
                && report.reason == TableReason::NonFiniteFloat
                && report.non_finite_float == "Loose"
                && report.non_finite_float_id == Some(0x0123_4567_89AB_CDEF)
                && report.non_finite_float_reason().as_deref() == Some("non_finite_float: Loose.0x0123456789abcdef");
            say(&format!("retain/non-finite-unknown-refuses/{kind}/{i}"), ok, format!("{got:?} {report:?}"));
            // the plain read keeps nothing, so it skips the bytes and counts
            // the unknown, as the Go oracle's plain read does
            let mut plain = TableReport::default();
            let got = loose_load(&unknown(kind, &payload), &mut plain);
            say(
                &format!("retain/plain-read-skips/{kind}/{i}"),
                got.is_some() && plain.unknown == 1 && !plain.refused,
                format!("{got:?} {plain:?}"),
            );
        }
    }
}

// ---- the message form --------------------------------------------------------

fn message() {
    let mut entries = vec![TableMessageEntry::default(); 4096];
    let mut vocabulary = TableVocabulary::new(&mut entries);
    if !announce_read(&mut vocabulary, &TABLE_ANNOUNCEMENT, &mut TableReport::default()) {
        say("message/announce", false, "the unit's own announcement did not read".to_string());
        return;
    }
    let save = |values: &[ProbeRow]| -> Option<Vec<u8>> {
        let mut report = TableReport::default();
        let n = probe_measure_messages(values, &mut report);
        if n < 0 {
            return None;
        }
        let mut buf = vec![0u8; n as usize];
        (probe_save_messages(values, &mut buf, &mut report) == n).then_some(buf)
    };
    let load = |wire: &[u8], values: &mut [ProbeRow]| -> (usize, bool, TableReport) {
        let mut report = TableReport::default();
        let (n, ok) = probe_load_messages(values, &vocabulary, wire, &mut report);
        (n, ok, report)
    };

    let mut v = probe_message_default();
    v.a = f32::from_bits(NEG0_32);
    v.b = f64::from_bits(SUB64);
    v.arr = [f32::from_bits(SUB32), f32::from_bits(NEG0_32), 1.0];
    v.inner.w = f64::from_bits(NEG0_64);
    let mut back = vec![probe_message_default(); 1];
    let wire = save(&[v]);
    let (n, ok, report) = wire.as_ref().map_or((0, false, TableReport::default()), |w| load(w, &mut back));
    say(
        "message/minus-zero-and-subnormal-ride",
        n == 1
            && ok
            && back[0].a.to_bits() == NEG0_32
            && back[0].b.to_bits() == SUB64
            && back[0].arr.map(f32::to_bits) == [SUB32, NEG0_32, 0x3F80_0000]
            && back[0].inner.w.to_bits() == NEG0_64,
        format!("{n} {ok} {report:?}"),
    );

    for (i, &b32) in BAD32.iter().enumerate() {
        let b64 = BAD64[i];
        let mut cases: Vec<(&str, ProbeRow)> = Vec::new();
        let mut c = probe_message_default();
        c.a = f32::from_bits(b32);
        cases.push(("a", c));
        let mut c = probe_message_default();
        c.b = f64::from_bits(b64);
        cases.push(("b", c));
        let mut c = probe_message_default();
        c.arr[1] = f32::from_bits(b32);
        cases.push(("arr", c));
        let mut c = probe_message_default();
        c.live_count = 2;
        c.live[1] = f64::from_bits(b64);
        cases.push(("live", c));
        let mut c = probe_message_default();
        c.inner.w = f64::from_bits(b64);
        cases.push(("inner", c));
        let mut c = probe_message_default();
        c.pick.set_f(f32::from_bits(b32));
        cases.push(("pick", c));
        // a COMPRESSED float's NaN or infinity is refused, never quantized
        let mut c = probe_message_default();
        c.aim = f32::from_bits(b32);
        cases.push(("aim", c));
        for (what, value) in cases {
            let mut report = TableReport::default();
            let measure = probe_measure_messages(&[value], &mut report);
            let mut buf = vec![0u8; 4096];
            let saved = probe_save_messages(&[value], &mut buf, &mut report);
            say(&format!("message/writer-refuses/{what}/{i}"), measure == -1 && saved == -1, format!("measure {measure} save {saved}"));
        }
    }

    for (i, &b32) in BAD32.iter().enumerate() {
        let b64 = BAD64[i];
        let mut cases: Vec<(&str, ProbeRow, u64, u64, usize, &str)> = Vec::new();
        let mut c = probe_message_default();
        c.a = f32::from_bits(MARK32);
        cases.push(("a", c, MARK32 as u64, b32 as u64, 32, "Probe.a"));
        let mut c = probe_message_default();
        c.b = f64::from_bits(MARK64);
        cases.push(("b", c, MARK64, b64, 64, "Probe.b"));
        let mut c = probe_message_default();
        c.arr[2] = f32::from_bits(MARK32);
        cases.push(("arr", c, MARK32 as u64, b32 as u64, 32, "Probe.arr"));
        let mut c = probe_message_default();
        c.inner.w = f64::from_bits(MARK64);
        cases.push(("inner", c, MARK64, b64, 64, "Inner.w"));
        let mut c = probe_message_default();
        c.pick.set_f(f32::from_bits(MARK32));
        cases.push(("pick", c, MARK32 as u64, b32 as u64, 32, "Pick.f"));
        for (what, value, from, to, width, field) in cases {
            // the bad body is the SECOND of two: no body of a refused batch is
            // delivered, so the first goes back to its declared defaults too
            let mut first = probe_message_default();
            first.a = 2.5;
            let Some(mut wire) = save(&[first, value]) else {
                say(&format!("message/read-refuses/{what}/{i}"), false, "the finite batch did not save".to_string());
                continue;
            };
            if !patch_bits(&mut wire, from, to, width) {
                say(&format!("message/read-refuses/{what}/{i}"), false, "the marker is not in the batch".to_string());
                continue;
            }
            let mut values = vec![probe_message_default(); 2];
            let (n, ok, report) = load(&wire, &mut values);
            let verdict = if n != 0 || ok {
                Err(format!("the batch delivered {n} bodies, ok={ok}"))
            } else if values[0].a != 0.0 {
                Err(format!("body one kept a = {} from a refused batch", values[0].a))
            } else {
                refused(&report, field)
            };
            say(&format!("message/read-refuses/{what}/{i}"), verdict.is_ok(), verdict.err().unwrap_or_default());
        }
    }
}

// ---- the fixed form ----------------------------------------------------------

fn fixed_save(values: &[ProbeRow]) -> Option<Vec<u8>> {
    let mut buf = vec![0xEEu8; probe_fixed_measure(values.len())];
    let n = probe_fixed_save(values, &mut buf)?;
    (n == buf.len()).then_some(buf)
}

fn fixed_load(data: &[u8], values: &mut [ProbeRow]) -> (Option<usize>, TableFixedReport) {
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = probe_fixed_load(values, data, &mut plan, &mut remap, &mut report);
    (n, report)
}

fn fixed() {
    let mut v = ProbeRow::default();
    v.a = f32::from_bits(NEG0_32);
    v.b = f64::from_bits(SUB64);
    v.arr = [f32::from_bits(SUB32), f32::from_bits(NEG0_32), 1.0];
    v.live_count = 1;
    v.live[0] = f64::from_bits(NEG0_64);
    v.inner.w = f64::from_bits(SUB64);
    v.pick.set_f(f32::from_bits(NEG0_32));
    let file = fixed_save(&[v]);
    let mut back = vec![ProbeRow::default(); 1];
    let (n, report) = file.as_ref().map_or((None, TableFixedReport::default()), |f| fixed_load(f, &mut back));
    say(
        "fixed/minus-zero-and-subnormal-ride",
        n == Some(1)
            && report == TableFixedReport::default()
            && back[0].a.to_bits() == NEG0_32
            && back[0].b.to_bits() == SUB64
            && back[0].arr.map(f32::to_bits) == [SUB32, NEG0_32, 0x3F80_0000]
            && back[0].live[0].to_bits() == NEG0_64
            && back[0].inner.w.to_bits() == SUB64
            && back[0].pick.f().map(f32::to_bits) == Some(NEG0_32),
        format!("{n:?} {report:?}"),
    );

    for (i, &b32) in BAD32.iter().enumerate() {
        let b64 = BAD64[i];
        let mut cases: Vec<(&str, ProbeRow)> = Vec::new();
        let mut c = ProbeRow::default();
        c.a = f32::from_bits(b32);
        cases.push(("a", c));
        let mut c = ProbeRow::default();
        c.b = f64::from_bits(b64);
        cases.push(("b", c));
        let mut c = ProbeRow::default();
        c.arr[0] = f32::from_bits(b32);
        cases.push(("arr", c));
        let mut c = ProbeRow::default();
        c.live_count = 2;
        c.live[1] = f64::from_bits(b64);
        cases.push(("live", c));
        let mut c = ProbeRow::default();
        c.inner.w = f64::from_bits(b64);
        cases.push(("inner", c));
        let mut c = ProbeRow::default();
        c.pick.set_f(f32::from_bits(b32));
        cases.push(("pick", c));
        let mut c = ProbeRow::default();
        c.aim = f32::from_bits(b32);
        cases.push(("aim", c));
        for (what, value) in cases {
            // A REFUSAL WRITES NO BYTE
            let mut buf = vec![0xEEu8; probe_fixed_measure(2)];
            let got = probe_fixed_save(&[ProbeRow::default(), value], &mut buf);
            say(
                &format!("fixed/writer-refuses/{what}/{i}"),
                got.is_none() && buf.iter().all(|&b| b == 0xEE),
                format!("{got:?}"),
            );
        }
    }
    // the SLACK past a counted array's live count is not a value: it is never
    // written and never tested
    let mut c = ProbeRow::default();
    c.live_count = 1;
    c.live[2] = f64::from_bits(BAD64[0]);
    say("fixed/slack-is-not-a-value", fixed_save(&[c]).is_some(), String::new());

    for (i, &b32) in BAD32.iter().enumerate() {
        let b64 = BAD64[i];
        let mut cases: Vec<(&str, ProbeRow, Vec<u8>, Vec<u8>, &str)> = Vec::new();
        let mut c = ProbeRow::default();
        c.a = f32::from_bits(MARK32);
        cases.push(("a", c, MARK32.to_le_bytes().to_vec(), b32.to_le_bytes().to_vec(), "Probe.a"));
        let mut c = ProbeRow::default();
        c.b = f64::from_bits(MARK64);
        cases.push(("b", c, MARK64.to_le_bytes().to_vec(), b64.to_le_bytes().to_vec(), "Probe.b"));
        let mut c = ProbeRow::default();
        c.arr[1] = f32::from_bits(MARK32);
        cases.push(("arr", c, MARK32.to_le_bytes().to_vec(), b32.to_le_bytes().to_vec(), "Probe.arr"));
        let mut c = ProbeRow::default();
        c.live_count = 3;
        c.live[2] = f64::from_bits(MARK64);
        cases.push(("live", c, MARK64.to_le_bytes().to_vec(), b64.to_le_bytes().to_vec(), "Probe.live"));
        let mut c = ProbeRow::default();
        c.inner.w = f64::from_bits(MARK64);
        cases.push(("inner", c, MARK64.to_le_bytes().to_vec(), b64.to_le_bytes().to_vec(), "Inner.w"));
        let mut c = ProbeRow::default();
        c.pick.set_f(f32::from_bits(MARK32));
        cases.push(("pick", c, MARK32.to_le_bytes().to_vec(), b32.to_le_bytes().to_vec(), "Pick.f"));
        // a COMPRESSED float: the finite test runs BEFORE the bounds pass, so
        // a NaN is refused and never clamped (ruling 1)
        let mut c = ProbeRow::default();
        c.aim = f32::from_bits(0x3E80_0000);
        cases.push(("aim", c, 0x3E80_0000u32.to_le_bytes().to_vec(), b32.to_le_bytes().to_vec(), "Probe.aim"));
        for (what, value, from, to, field) in cases {
            let mut first = ProbeRow::default();
            first.a = 2.5;
            let Some(mut file) = fixed_save(&[first, value]) else {
                say(&format!("fixed/read-refuses/{what}/{i}"), false, "the finite file did not save".to_string());
                continue;
            };
            let header = 20 + PROBE_FIXED_BLOCK.len() + PROBE_FIXED_RECORD_BYTES;
            if !patch(&mut file[header..], &from, &to) {
                say(&format!("fixed/read-refuses/{what}/{i}"), false, "the marker is not in record two".to_string());
                continue;
            }
            let mut values = vec![ProbeRow::default(); 2];
            values[0].a = 9.0;
            let (n, report) = fixed_load(&file, &mut values);
            let verdict = if n.is_some() {
                Err(format!("the read landed {n:?}"))
            } else if !report.refused || report.reason != TableFixedReason::NonFiniteFloat || report.malformed || report.non_finite_float != field {
                Err(format!("want the refusal non_finite_float naming {field}: {report:?}"))
            } else if values[0].a != 0.0 {
                Err(format!("record one kept a = {} from a refused batch", values[0].a))
            } else {
                Ok(())
            };
            say(&format!("fixed/read-refuses/{what}/{i}"), verdict.is_ok(), verdict.err().unwrap_or_default());
        }
    }
}

// ---- the cook ----------------------------------------------------------------

fn cook() {
    let mut buf = vec![0xEEu8; 4096];
    let ok = cooked_cook_from(&CookedRow { f: f32::from_bits(NEG0_32), d: f64::from_bits(SUB64) }, &mut buf, 1);
    say("cook/minus-zero-and-subnormal-cook", ok, String::new());
    for (i, &b32) in BAD32.iter().enumerate() {
        for (what, row) in [
            ("f", CookedRow { f: f32::from_bits(b32), d: 0.0 }),
            ("d", CookedRow { f: 0.0, d: f64::from_bits(BAD64[i]) }),
        ] {
            let mut buf = vec![0xEEu8; 4096];
            let ok = cooked_cook_from(&row, &mut buf, 1);
            say(&format!("cook/writer-refuses/{what}/{i}"), !ok && buf.iter().all(|&b| b == 0xEE), String::new());
        }
    }
}

fn main() {
    names();
    variable();
    message();
    fixed();
    cook();
}
`

// finiteFloatFamilies is every family of clauses the probe reports, and the
// least count of each, so a probe that stopped reporting a family cannot pass.
var finiteFloatFamilies = map[string]int{
	"names/":                                 2,
	"variable/minus-zero-and-subnormal-ride": 1,
	"variable/subnormal-rides":               1,
	"variable/writer-refuses/":               20,
	"variable/read-refuses/":                 20,
	"variable/float-rung-widens":             1,
	"variable/float-rung-refuses/":           5,
	"retain/finite-unknown-is-kept":          1,
	"retain/non-finite-unknown-refuses/":     10,
	"retain/plain-read-skips/":               10,
	"message/minus-zero-and-subnormal-ride":  1,
	"message/writer-refuses/":                35,
	"message/read-refuses/":                  25,
	"fixed/minus-zero-and-subnormal-ride":    1,
	"fixed/writer-refuses/":                  35,
	"fixed/slack-is-not-a-value":             1,
	"fixed/read-refuses/":                    35,
	"cook/minus-zero-and-subnormal-cook":     1,
	"cook/writer-refuses/":                   10,
}

func TestRustFiniteFloat(t *testing.T) {
	t.Parallel()
	out, err := runFiniteFloatProbe(t, nil)
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	ok, bad := finiteFloatLines(out)
	for _, line := range bad {
		t.Errorf("%s", line)
	}
	for family, least := range finiteFloatFamilies {
		n := 0
		for _, id := range ok {
			if strings.HasPrefix(id, family) {
				n++
			}
		}
		if n < least {
			t.Errorf("the probe reported %d passing clauses of %s, want at least %d", n, family, least)
		}
	}
}

// TestRustFiniteFloatNegativeControl is the rule's REVERSED WITNESS: each half
// of it is taken out of the GENERATED Rust in turn, and the probe must go red
// on exactly that half's clauses and on nothing else. A gate nobody has watched
// go red is not a gate.
func TestRustFiniteFloatNegativeControl(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		sabotage [][2]string
		reds     []string
	}{
		{"variable-read", [][2]string{
			{"if table_finite_f32(raw as u32) {", "if true /* SABOTAGED */ {"},
			{"else if table_finite_f64(raw) {", "else if true /* SABOTAGED */ {"},
		}, []string{"variable/read-refuses/", "variable/float-rung-refuses/"}},
		{"variable-writer", [][2]string{
			{"ids.refused = Some(format!(\"non_finite_float: {at}\"));", "let _ = at; /* SABOTAGED */"},
		}, []string{"variable/writer-refuses/"}},
		{"retain", [][2]string{
			{"if !table_retained_float_finite(kind,", "if false && !table_retained_float_finite(kind,"},
		}, []string{"retain/non-finite-unknown-refuses/"}},
		{"message-read", [][2]string{
			{"|| table_finite_f32(lo as u32) {", "|| true /* SABOTAGED */ {"},
			{"else if table_finite_f64(lo) {", "else if true /* SABOTAGED */ {"},
		}, []string{"message/read-refuses/"}},
		{"message-writer", [][2]string{
			{".is_finite() { return false; }", ".is_finite() && false { return false; }"},
		}, []string{"message/writer-refuses/"}},
		{"fixed-read", [][2]string{
			{"        if !why.is_empty() {\n            for value in values.iter_mut()", "        if false && !why.is_empty() {\n            for value in values.iter_mut()"},
		}, []string{"fixed/read-refuses/"}},
		{"fixed-writer", [][2]string{
			{"if values.iter().any(|value| !", "if false && values.iter().any(|value| !"},
		}, []string{"fixed/writer-refuses/"}},
		{"cook-writer", [][2]string{
			{"if f.storage == TableCookStorage::Float\n", "if false && f.storage == TableCookStorage::Float\n"},
		}, []string{"cook/writer-refuses/"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := runFiniteFloatProbe(t, func(files map[string][]byte) {
				for _, s := range c.sabotage {
					hit := false
					for name, data := range files {
						if strings.Contains(string(data), s[0]) {
							files[name] = []byte(strings.ReplaceAll(string(data), s[0], s[1]))
							hit = true
						}
					}
					if !hit {
						t.Fatalf("NEGATIVE CONTROL: the sabotage %q did not apply", s[0])
					}
				}
			})
			if err != nil {
				t.Fatalf("the sabotaged probe did not run: %v\n%s", err, out)
			}
			_, bad := finiteFloatLines(out)
			if len(bad) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s half of the finite rule is gone and the probe stayed GREEN", c.name)
			}
			for _, line := range bad {
				id := strings.Fields(strings.TrimPrefix(line, "FINITE "))[0]
				inside := false
				for _, red := range c.reds {
					inside = inside || strings.HasPrefix(id, red)
				}
				if !inside {
					t.Errorf("NEGATIVE CONTROL: taking out the %s half reddened a clause outside it: %s", c.name, line)
				}
			}
			for _, red := range c.reds {
				if !strings.Contains(strings.Join(bad, "\n"), "FINITE "+red) {
					t.Errorf("NEGATIVE CONTROL FAILED: taking out the %s half left every %s clause green", c.name, red)
				}
			}
		})
	}
}

// finiteFloatLines splits the probe's output into the passing clause ids and
// the failing lines whole.
func finiteFloatLines(out string) (ok, bad []string) {
	for line := range strings.SplitSeq(out, "\n") {
		id, verdict, found := strings.Cut(strings.TrimPrefix(line, "FINITE "), " ")
		switch {
		case !strings.HasPrefix(line, "FINITE "):
		case found && verdict == "ok":
			ok = append(ok, id)
		default:
			bad = append(bad, line)
		}
	}
	return ok, bad
}

// runFiniteFloatProbe generates the probe unit, lets edit change the generated
// files (a negative control's sabotage), and builds every table module but the
// packet one beside the probe with rustc alone: the table runtimes are
// self-contained, so no cargo workspace is needed.
func runFiniteFloatProbe(t *testing.T, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the finite rule cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
	files, err := Generate(unitFrom(t, finiteFloatSchema))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		mod := strings.TrimSuffix(name, ".rs")
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".rs") || mod == "lib" || mod == "probe" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, mod)
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString(finiteFloatProbe)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "finite")
	build := exec.Command("rustc", "--edition", "2024", "-o", bin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	return string(out), err
}
