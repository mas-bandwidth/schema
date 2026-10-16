// THE WIRE-FUZZ DRIVER OVER EVERY ROOT (test/conformance/harness/wirefuzz.go):
// the Rust leg of the independent table-wire differential, over every
// variable-class FILE root of the harness's default roster that this leg
// carries (docs/SPEC-TABLES.md §3, §3.1, §4.2), not the Card root alone
// (wire_fuzz.rs keeps that one). The harness owns the mutations and the
// oracle; this process binds each roster entry to its unit's generated codec
// and returns the report, the region measure and the saved value on a pipe.
//
// It is a STANDALONE binary built with rustc, the way wire_fuzz.rs is: each
// unit is a module directory holding that unit's generated variable-wire
// modules (variable_runtime.rs, <file>_table.rs, <file>_variable.rs) and a
// mod.rs the make target writes (make/rust.mk, tables-rust-wire-fuzz-all). A
// unit's TableReport and TableRetain are its OWN types, so each root's arm
// names its unit's module and nothing is shared across units.
//
// THE RETENTION ARM (docs/SPEC-TABLES.md §6.6): a roster entry whose retain
// byte is set is driven through the root's retaining pair — <root>_load_retain
// and <root>_save_retain over the caller-owned stores the arm declares — and
// the two retention counters ride the reply beside the six.
//
// THE ROSTER THIS LEG CARRIES is ROOTS below, and nothing else answers
// available: a fixed-class root's form-1 file, a form-2 message entry, and a
// unit the Rust generator refuses whole answer 0 and the harness counts them
// absent. TestRetainsAndWidensOnEveryRootRust holds ROOTS equal, both ways, to
// the roster roots whose unit the Rust generator gives a variable codec.

mod blobdemo;
mod graphdemo;
mod listdemo;
mod mapdemo;
mod tblp2;

use std::io::{self, Read, Write};

/// One mutant's answer: what wirefuzz.go's legReply reads.
#[derive(Default)]
struct Reply {
    loaded: bool,
    unknown: u64,
    kind_mismatch: u64,
    widened: u64,
    clamped: u64,
    duplicate: u64,
    malformed: bool,
    refused: bool,
    measure: i64,
    retained: u64,
    retain_lost: u64,
    save_fail: bool,
    saved: Vec<u8>,
}

/// THE RETENTION ARM'S TWO STORES (docs/SPEC-TABLES.md §6.6), large on purpose
/// (wirefuzz.go's retainFuzzCapacity, retainFuzzIdCapacity): the arm compares
/// the feature and not the record layout.
const RETAIN_BYTES: usize = 1 << 20;
const RETAIN_IDS: usize = RETAIN_BYTES / 8;

// One root's arm: its unit's module and its four generated verbs. The report
// is the unit's own type, so the counters are copied out here.
macro_rules! arm {
    ($unit:ident, $load:ident, $load_retain:ident, $save:ident, $save_retain:ident, $measure:ident, $wire:expr, $retain:expr) => {{
        use $unit::*;
        let wire: &[u8] = $wire;
        let mut report = TableReport::default();
        let mut reply = Reply { measure: $measure(wire), ..Default::default() };
        if $retain {
            let mut store = TableRetain { bytes: Vec::with_capacity(RETAIN_BYTES), ids: Vec::with_capacity(RETAIN_IDS), nodes: Default::default() };
            let value = $load_retain(wire, &mut store, &mut report);
            reply.loaded = value.is_some();
            // THE REPORT ACCUMULATES ACROSS THE PAIR (§6.6): one report is
            // zeroed before the load and read after the save
            if !report.cycle_on_save {
                reply.saved = $save_retain(&value.unwrap_or_default(), &mut store, &mut report);
            } else {
                reply.save_fail = true;
            }
            reply.retained = report.retained;
            reply.retain_lost = report.retain_lost;
        } else {
            let value = $load(wire, &mut report);
            reply.loaded = value.is_some();
            if !report.cycle_on_save {
                reply.saved = $save(&value.unwrap_or_default());
            } else {
                reply.save_fail = true;
            }
        }
        reply.unknown = report.unknown;
        reply.kind_mismatch = report.kind_mismatch;
        reply.widened = report.widened;
        reply.clamped = report.clamped;
        reply.duplicate = report.duplicate;
        reply.malformed = report.malformed;
        reply.refused = report.refused;
        Some(reply)
    }};
}

/// ROOTS is every (unit, root) this driver answers, in the order `drive`
/// matches them.
const ROOTS: &[(&str, &str)] = &[
    ("blobdemo", "Catalog"),
    ("graphdemo", "Scene"),
    ("listdemo", "Album"),
    ("listdemo", "Army"),
    ("listdemo", "Save"),
    ("listdemo", "Sheet"),
    ("listdemo", "Unbounded"),
    ("mapdemo", "Cells"),
    ("mapdemo", "Chunks"),
    ("mapdemo", "Crews"),
    ("mapdemo", "Depth"),
    ("mapdemo", "Docs"),
    ("mapdemo", "Fleet"),
    ("mapdemo", "Pairs"),
    ("mapdemo", "Runs"),
    ("mapdemo", "Slots"),
    ("mapdemo", "Spans"),
    ("mapdemo", "Trails"),
    ("tblp2", "Chain"),
];

fn drive(unit: &str, root: &str, wire: &[u8], retain: bool) -> Option<Reply> {
    match (unit, root) {
        ("blobdemo", "Catalog") => arm!(blobdemo, catalog_load, catalog_load_retain, catalog_save, catalog_save_retain, catalog_load_measure, wire, retain),
        ("graphdemo", "Scene") => arm!(graphdemo, scene_load, scene_load_retain, scene_save, scene_save_retain, scene_load_measure, wire, retain),
        ("listdemo", "Album") => arm!(listdemo, album_load, album_load_retain, album_save, album_save_retain, album_load_measure, wire, retain),
        ("listdemo", "Army") => arm!(listdemo, army_load, army_load_retain, army_save, army_save_retain, army_load_measure, wire, retain),
        ("listdemo", "Save") => arm!(listdemo, save_load, save_load_retain, save_save, save_save_retain, save_load_measure, wire, retain),
        ("listdemo", "Sheet") => arm!(listdemo, sheet_load, sheet_load_retain, sheet_save, sheet_save_retain, sheet_load_measure, wire, retain),
        ("listdemo", "Unbounded") => arm!(listdemo, unbounded_load, unbounded_load_retain, unbounded_save, unbounded_save_retain, unbounded_load_measure, wire, retain),
        ("mapdemo", "Cells") => arm!(mapdemo, cells_load, cells_load_retain, cells_save, cells_save_retain, cells_load_measure, wire, retain),
        ("mapdemo", "Chunks") => arm!(mapdemo, chunks_load, chunks_load_retain, chunks_save, chunks_save_retain, chunks_load_measure, wire, retain),
        ("mapdemo", "Crews") => arm!(mapdemo, crews_load, crews_load_retain, crews_save, crews_save_retain, crews_load_measure, wire, retain),
        ("mapdemo", "Depth") => arm!(mapdemo, depth_load, depth_load_retain, depth_save, depth_save_retain, depth_load_measure, wire, retain),
        ("mapdemo", "Docs") => arm!(mapdemo, docs_load, docs_load_retain, docs_save, docs_save_retain, docs_load_measure, wire, retain),
        ("mapdemo", "Fleet") => arm!(mapdemo, fleet_load, fleet_load_retain, fleet_save, fleet_save_retain, fleet_load_measure, wire, retain),
        ("mapdemo", "Pairs") => arm!(mapdemo, pairs_load, pairs_load_retain, pairs_save, pairs_save_retain, pairs_load_measure, wire, retain),
        ("mapdemo", "Runs") => arm!(mapdemo, runs_load, runs_load_retain, runs_save, runs_save_retain, runs_load_measure, wire, retain),
        ("mapdemo", "Slots") => arm!(mapdemo, slots_load, slots_load_retain, slots_save, slots_save_retain, slots_load_measure, wire, retain),
        ("mapdemo", "Spans") => arm!(mapdemo, spans_load, spans_load_retain, spans_save, spans_save_retain, spans_load_measure, wire, retain),
        ("mapdemo", "Trails") => arm!(mapdemo, trails_load, trails_load_retain, trails_save, trails_save_retain, trails_load_measure, wire, retain),
        ("tblp2", "Chain") => arm!(tblp2, chain_load, chain_load_retain, chain_save, chain_save_retain, chain_load_measure, wire, retain),
        _ => None,
    }
}

fn read_u32() -> io::Result<u32> {
    let mut b = [0u8; 4];
    io::stdin().read_exact(&mut b)?;
    Ok(u32::from_le_bytes(b))
}

fn read_name() -> io::Result<String> {
    let mut n = [0u8; 2];
    io::stdin().read_exact(&mut n)?;
    let mut b = vec![0u8; u16::from_le_bytes(n) as usize];
    io::stdin().read_exact(&mut b)?;
    Ok(String::from_utf8_lossy(&b).into_owned())
}

fn main() {
    // `roots` lists ROOTS one per line, the answer TestRetainsAndWidensOnEveryRootRust
    // holds against the roster
    if std::env::args().nth(1).as_deref() == Some("roots") {
        for (unit, root) in ROOTS {
            println!("{unit} {root}");
        }
        return;
    }
    let stdout = io::stdout();
    let mut out = stdout.lock();
    let count = match read_u32() {
        Ok(c) => c as usize,
        Err(_) => return,
    };
    // (unit, root, available, retain)
    let mut roster: Vec<(String, String, bool, bool)> = Vec::with_capacity(count);
    for _ in 0..count {
        let unit = read_name().unwrap_or_default();
        let root = read_name().unwrap_or_default();
        let mut form = [0u8; 1];
        let mut retain = [0u8; 1];
        if io::stdin().read_exact(&mut form).is_err() || io::stdin().read_exact(&mut retain).is_err() {
            return;
        }
        // THE VARIABLE FORM, form byte 1, and only a root ROOTS names
        // (docs/SPEC-TABLES.md §3): a message entry (form 2) is not this
        // leg's, so it answers 0 the way a root it cannot name does
        let available = form[0] == 1 && ROOTS.iter().any(|(u, r)| *u == unit && *r == root);
        let _ = out.write_all(&[available as u8]);
        roster.push((unit, root, available, retain[0] == 1));
    }
    let _ = out.flush();
    loop {
        let index = match read_u32() {
            Ok(i) => i as usize,
            Err(_) => return, // EOF ends the stream
        };
        let size = match read_u32() {
            Ok(s) => s as usize,
            Err(_) => return,
        };
        if index >= roster.len() || !roster[index].2 {
            return;
        }
        let mut wire = vec![0u8; size];
        if io::stdin().read_exact(&mut wire).is_err() {
            return;
        }
        let (unit, root, _, retain) = &roster[index];
        let Some(reply) = drive(unit, root, &wire, *retain) else { return };
        // loaded, unknown, kind_mismatch, widened, clamped, duplicate, malformed,
        // refused, measure (i64), retained, retain_lost, saved length
        let mut head = [0u8; 47];
        head[0] = reply.loaded as u8;
        head[1..5].copy_from_slice(&(reply.unknown as u32).to_le_bytes());
        head[5..9].copy_from_slice(&(reply.kind_mismatch as u32).to_le_bytes());
        head[9..13].copy_from_slice(&(reply.widened as u32).to_le_bytes());
        head[13..17].copy_from_slice(&(reply.clamped as u32).to_le_bytes());
        head[17..21].copy_from_slice(&(reply.duplicate as u32).to_le_bytes());
        head[21] = reply.malformed as u8;
        head[22] = reply.refused as u8;
        head[23..31].copy_from_slice(&reply.measure.to_le_bytes());
        head[31..35].copy_from_slice(&(reply.retained as u32).to_le_bytes());
        head[35..39].copy_from_slice(&(reply.retain_lost as u32).to_le_bytes());
        head[39..47].copy_from_slice(&(if reply.save_fail { -1i64 } else { reply.saved.len() as i64 }).to_le_bytes());
        let _ = out.write_all(&head);
        let _ = out.write_all(&reply.saved);
        let _ = out.flush();
    }
}
