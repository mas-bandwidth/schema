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
// modules (variable_runtime.rs, table_enums.rs, <file>_table.rs, <file>_variable.rs) and a
// mod.rs the make target writes (make/rust.mk, tables-rust-wire-fuzz-all). A
// unit's TableReport and TableRetain are its OWN types, so each root's arm
// names its unit's module and nothing is shared across units.
//
// THE RETENTION ARM (docs/SPEC-TABLES.md §6.6): a roster entry whose retain
// byte is set is driven through the root's retaining pair — <root>_load_retain
// and <root>_save_retain over the caller-owned stores the arm declares — and
// the two retention counters ride the reply beside the six.
//
// THE ROSTER THIS LEG CARRIES is ROOTS and MESSAGE_ROOTS below. A form-1
// root whose unit the Rust generator refuses whole answers 0. The retention
// arm is the variable form's; a form-2 message has no retaining codec (§6.6).

mod backenddemo;
mod blobdemo;
mod graphdemo;
mod listdemo;
mod mapdemo;
mod messagedemo;
mod scalars;
mod streamdemo;
mod tabledemo;
mod tbla2;
mod tblg1;
mod tblk1;
mod tblk2;
mod tblm1;
mod tblm2;
mod tblp1;
mod tblp2;
mod tblp3;
mod tblr1;
mod tblr2;
mod tblscalars2;
mod tblv1;
mod tblv2;
mod tblw1;
mod tblw2;
mod vocab9demo;
mod vocabdemo;
mod widedemo;

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
    ($unit:ident, $load:ident, $load_retain:ident, $save:ident, $save_checked:ident, $save_retain:ident, $measure:ident, $wire:expr, $retain:expr) => {{
        use $unit::*;
        let wire: &[u8] = $wire;
        let mut report = TableReport::default();
        let mut reply = Reply { measure: $measure(wire), ..Default::default() };
        if $retain {
            let mut store = TableRetain { bytes: Vec::with_capacity(RETAIN_BYTES), ids: Vec::with_capacity(RETAIN_IDS) };
            let value = $load_retain(wire, &mut store, &mut report);
            reply.loaded = value.is_some();
            // THE REPORT ACCUMULATES ACROSS THE PAIR (§6.6): one report is
            // zeroed before the load and read after the save
            // a cycle, or a blob two slots name (§2.5), is a save refusal:
            // the checked save names the writer's refusal without the panic
            if !report.cycle_on_save && $save_checked(value.as_ref().unwrap_or(&Default::default())).is_ok() {
                reply.saved = $save_retain(&value.unwrap_or_default(), &mut store, &mut report);
            } else {
                reply.save_fail = true;
            }
            reply.retained = report.retained;
            reply.retain_lost = report.retain_lost;
        } else {
            let value = $load(wire, &mut report);
            reply.loaded = value.is_some();
            // a cycle, or a blob two slots name (§2.5), is a save refusal
            if !report.cycle_on_save {
                match $save_checked(&value.unwrap_or_default()) {
                    Ok(saved) => reply.saved = saved,
                    Err(_) => reply.save_fail = true,
                }
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

// One fixed root's form-2 message batch (§3.3). The oracle decodes the batch
// but saves body one as a single-message batch, so the leg does the same.
macro_rules! message_arm {
    ($unit:ident, $default:ident, $load:ident, $measure:ident, $save:ident, $wire:expr) => {{
        use $unit::*;
        let mut report = TableReport::default();
        let mut reply = Reply { loaded: true, ..Default::default() };
        let mut entries = vec![TableMessageEntry::default(); 4096];
        let mut vocabulary = TableVocabulary::new(&mut entries);
        if announce_read(&mut vocabulary, &TABLE_ANNOUNCEMENT, &mut TableReport::default()) {
            let mut values = vec![$default(); TABLE_MESSAGE_BATCH_MAX];
            let _ = $load(&mut values, &vocabulary, $wire, &mut report);
            let mut saved_report = TableReport::default();
            let n = $measure(&values[..1], &mut saved_report);
            if n < 0 {
                reply.save_fail = true;
            } else {
                reply.saved = vec![0; n as usize];
                if $save(&values[..1], &mut reply.saved, &mut saved_report) != n {
                    reply.save_fail = true;
                    reply.saved.clear();
                }
            }
        } else {
            reply.save_fail = true;
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

const MESSAGE_ROOTS: &[(&str, &str)] = &[
    ("backenddemo", "LoginRequest"), ("backenddemo", "MatchResult"),
    ("backenddemo", "StorePurchase"), ("backenddemo", "Envelope"),
    ("vocabdemo", "Wide00"), ("vocabdemo", "Wide09"),
    ("vocab9demo", "Wide00"), ("vocab9demo", "Wide19"),
];

fn drive_message(unit: &str, root: &str, wire: &[u8]) -> Option<Reply> {
    match (unit, root) {
        ("backenddemo", "LoginRequest") => message_arm!(backenddemo, login_request_message_default, login_request_load_messages, login_request_measure_messages, login_request_save_messages, wire),
        ("backenddemo", "MatchResult") => message_arm!(backenddemo, match_result_message_default, match_result_load_messages, match_result_measure_messages, match_result_save_messages, wire),
        ("backenddemo", "StorePurchase") => message_arm!(backenddemo, store_purchase_message_default, store_purchase_load_messages, store_purchase_measure_messages, store_purchase_save_messages, wire),
        ("backenddemo", "Envelope") => message_arm!(backenddemo, envelope_message_default, envelope_load_messages, envelope_measure_messages, envelope_save_messages, wire),
        ("vocabdemo", "Wide00") => message_arm!(vocabdemo, wide00_message_default, wide00_load_messages, wide00_measure_messages, wide00_save_messages, wire),
        ("vocabdemo", "Wide09") => message_arm!(vocabdemo, wide09_message_default, wide09_load_messages, wide09_measure_messages, wide09_save_messages, wire),
        ("vocab9demo", "Wide00") => message_arm!(vocab9demo, wide00_message_default, wide00_load_messages, wide00_measure_messages, wide00_save_messages, wire),
        ("vocab9demo", "Wide19") => message_arm!(vocab9demo, wide19_message_default, wide19_load_messages, wide19_measure_messages, wide19_save_messages, wire),
        _ => None,
    }
}

/// ROOTS is every (unit, root) this driver answers, in the order `drive`
/// matches them.
const ROOTS: &[(&str, &str)] = &[
    ("backenddemo", "LoginRequest"),
    ("backenddemo", "MatchResult"),
    ("backenddemo", "StorePurchase"),
    ("blobdemo", "Catalog"),
    ("graphdemo", "Scene"),
    ("listdemo", "Album"),
    ("listdemo", "Army"),
    ("listdemo", "Mixed"),
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
    ("mapdemo", "Text"),
    ("mapdemo", "Trails"),
    ("messagedemo", "ToolMessage"),
    ("scalars", "SimState"),
    ("streamdemo", "Feed"),
    ("tabledemo", "ArchiveConfig"),
    ("tabledemo", "KeyedConfig"),
    ("tabledemo", "LoadoutConfig"),
    ("tabledemo", "ProfileConfig"),
    ("tabledemo", "RootConfig"),
    ("tabledemo", "WideBlob"),
    ("tbla2", "Root"),
    ("tblg1", "Guarded"),
    ("tblk1", "Root"),
    ("tblk2", "Root"),
    ("tblm1", "Msg"),
    ("tblm2", "Msg"),
    ("tblp1", "Chain"),
    ("tblp2", "Chain"),
    ("tblp3", "Chain"),
    ("tblr1", "Cfg"),
    ("tblr2", "Cfg"),
    ("tblscalars2", "SimState"),
    ("tblv1", "Cfg"),
    ("tblv2", "Cfg"),
    ("tblw1", "Fleet"),
    ("tblw2", "Fleet"),
    ("vocab9demo", "Wide00"),
    ("vocab9demo", "Wide19"),
    ("vocabdemo", "Wide00"),
    ("vocabdemo", "Wide09"),
    ("widedemo", "Caption"),
    ("widedemo", "Stamp"),
];

// A fixed-class root still has a form-1 file codec, but the harness holds
// its caller-owned region even when load refuses damaged framing (§3, §4.2).
const FIXED_ROOTS: &[(&str, &str)] = &[
    ("backenddemo", "LoginRequest"),
    ("backenddemo", "MatchResult"),
    ("backenddemo", "StorePurchase"),
    ("messagedemo", "ToolMessage"),
    ("scalars", "SimState"),
    ("tabledemo", "ArchiveConfig"),
    ("tabledemo", "KeyedConfig"),
    ("tabledemo", "LoadoutConfig"),
    ("tabledemo", "ProfileConfig"),
    ("tabledemo", "RootConfig"),
    ("tabledemo", "WideBlob"),
    ("tbla2", "Root"),
    ("tblk1", "Root"),
    ("tblk2", "Root"),
    ("tblm1", "Msg"),
    ("tblm2", "Msg"),
    ("tblp1", "Chain"),
    ("tblp3", "Chain"),
    ("tblr1", "Cfg"),
    ("tblr2", "Cfg"),
    ("tblscalars2", "SimState"),
    ("tblv1", "Cfg"),
    ("tblv2", "Cfg"),
    ("vocab9demo", "Wide00"),
    ("vocab9demo", "Wide19"),
    ("vocabdemo", "Wide00"),
    ("vocabdemo", "Wide09"),
    ("widedemo", "Caption"),
    ("widedemo", "Stamp"),
];

fn drive(unit: &str, root: &str, wire: &[u8], retain: bool) -> Option<Reply> {
    match (unit, root) {
        ("backenddemo", "LoginRequest") => arm!(backenddemo, login_request_load, login_request_load_retain, login_request_save, login_request_save_checked, login_request_save_retain, login_request_load_measure, wire, retain),
        ("backenddemo", "MatchResult") => arm!(backenddemo, match_result_load, match_result_load_retain, match_result_save, match_result_save_checked, match_result_save_retain, match_result_load_measure, wire, retain),
        ("backenddemo", "StorePurchase") => arm!(backenddemo, store_purchase_load, store_purchase_load_retain, store_purchase_save, store_purchase_save_checked, store_purchase_save_retain, store_purchase_load_measure, wire, retain),
        ("blobdemo", "Catalog") => arm!(blobdemo, catalog_load, catalog_load_retain, catalog_save, catalog_save_checked, catalog_save_retain, catalog_load_measure, wire, retain),
        ("graphdemo", "Scene") => arm!(graphdemo, scene_load, scene_load_retain, scene_save, scene_save_checked, scene_save_retain, scene_load_measure, wire, retain),
        ("listdemo", "Album") => arm!(listdemo, album_load, album_load_retain, album_save, album_save_checked, album_save_retain, album_load_measure, wire, retain),
        ("listdemo", "Army") => arm!(listdemo, army_load, army_load_retain, army_save, army_save_checked, army_save_retain, army_load_measure, wire, retain),
        ("listdemo", "Mixed") => arm!(listdemo, mixed_load, mixed_load_retain, mixed_save, mixed_save_checked, mixed_save_retain, mixed_load_measure, wire, retain),
        ("listdemo", "Save") => arm!(listdemo, save_load, save_load_retain, save_save, save_save_checked, save_save_retain, save_load_measure, wire, retain),
        ("listdemo", "Sheet") => arm!(listdemo, sheet_load, sheet_load_retain, sheet_save, sheet_save_checked, sheet_save_retain, sheet_load_measure, wire, retain),
        ("listdemo", "Unbounded") => arm!(listdemo, unbounded_load, unbounded_load_retain, unbounded_save, unbounded_save_checked, unbounded_save_retain, unbounded_load_measure, wire, retain),
        ("mapdemo", "Cells") => arm!(mapdemo, cells_load, cells_load_retain, cells_save, cells_save_checked, cells_save_retain, cells_load_measure, wire, retain),
        ("mapdemo", "Chunks") => arm!(mapdemo, chunks_load, chunks_load_retain, chunks_save, chunks_save_checked, chunks_save_retain, chunks_load_measure, wire, retain),
        ("mapdemo", "Crews") => arm!(mapdemo, crews_load, crews_load_retain, crews_save, crews_save_checked, crews_save_retain, crews_load_measure, wire, retain),
        ("mapdemo", "Depth") => arm!(mapdemo, depth_load, depth_load_retain, depth_save, depth_save_checked, depth_save_retain, depth_load_measure, wire, retain),
        ("mapdemo", "Docs") => arm!(mapdemo, docs_load, docs_load_retain, docs_save, docs_save_checked, docs_save_retain, docs_load_measure, wire, retain),
        ("mapdemo", "Fleet") => arm!(mapdemo, fleet_load, fleet_load_retain, fleet_save, fleet_save_checked, fleet_save_retain, fleet_load_measure, wire, retain),
        ("mapdemo", "Pairs") => arm!(mapdemo, pairs_load, pairs_load_retain, pairs_save, pairs_save_checked, pairs_save_retain, pairs_load_measure, wire, retain),
        ("mapdemo", "Runs") => arm!(mapdemo, runs_load, runs_load_retain, runs_save, runs_save_checked, runs_save_retain, runs_load_measure, wire, retain),
        ("mapdemo", "Slots") => arm!(mapdemo, slots_load, slots_load_retain, slots_save, slots_save_checked, slots_save_retain, slots_load_measure, wire, retain),
        ("mapdemo", "Spans") => arm!(mapdemo, spans_load, spans_load_retain, spans_save, spans_save_checked, spans_save_retain, spans_load_measure, wire, retain),
        ("mapdemo", "Text") => arm!(mapdemo, text_load, text_load_retain, text_save, text_save_checked, text_save_retain, text_load_measure, wire, retain),
        ("mapdemo", "Trails") => arm!(mapdemo, trails_load, trails_load_retain, trails_save, trails_save_checked, trails_save_retain, trails_load_measure, wire, retain),
        ("messagedemo", "ToolMessage") => arm!(messagedemo, tool_message_load, tool_message_load_retain, tool_message_save, tool_message_save_checked, tool_message_save_retain, tool_message_load_measure, wire, retain),
        ("scalars", "SimState") => arm!(scalars, sim_state_load, sim_state_load_retain, sim_state_save, sim_state_save_checked, sim_state_save_retain, sim_state_load_measure, wire, retain),
        ("streamdemo", "Feed") => arm!(streamdemo, feed_load, feed_load_retain, feed_save, feed_save_checked, feed_save_retain, feed_load_measure, wire, retain),
        ("tabledemo", "ArchiveConfig") => arm!(tabledemo, archive_config_load, archive_config_load_retain, archive_config_save, archive_config_save_checked, archive_config_save_retain, archive_config_load_measure, wire, retain),
        ("tabledemo", "KeyedConfig") => arm!(tabledemo, keyed_config_load, keyed_config_load_retain, keyed_config_save, keyed_config_save_checked, keyed_config_save_retain, keyed_config_load_measure, wire, retain),
        ("tabledemo", "LoadoutConfig") => arm!(tabledemo, loadout_config_load, loadout_config_load_retain, loadout_config_save, loadout_config_save_checked, loadout_config_save_retain, loadout_config_load_measure, wire, retain),
        ("tabledemo", "ProfileConfig") => arm!(tabledemo, profile_config_load, profile_config_load_retain, profile_config_save, profile_config_save_checked, profile_config_save_retain, profile_config_load_measure, wire, retain),
        ("tabledemo", "RootConfig") => arm!(tabledemo, root_config_load, root_config_load_retain, root_config_save, root_config_save_checked, root_config_save_retain, root_config_load_measure, wire, retain),
        ("tabledemo", "WideBlob") => arm!(tabledemo, wide_blob_load, wide_blob_load_retain, wide_blob_save, wide_blob_save_checked, wide_blob_save_retain, wide_blob_load_measure, wire, retain),
        ("tbla2", "Root") => arm!(tbla2, root_load, root_load_retain, root_save, root_save_checked, root_save_retain, root_load_measure, wire, retain),
        ("tblg1", "Guarded") => arm!(tblg1, guarded_load, guarded_load_retain, guarded_save, guarded_save_checked, guarded_save_retain, guarded_load_measure, wire, retain),
        ("tblk1", "Root") => arm!(tblk1, root_load, root_load_retain, root_save, root_save_checked, root_save_retain, root_load_measure, wire, retain),
        ("tblk2", "Root") => arm!(tblk2, root_load, root_load_retain, root_save, root_save_checked, root_save_retain, root_load_measure, wire, retain),
        ("tblm1", "Msg") => arm!(tblm1, msg_load, msg_load_retain, msg_save, msg_save_checked, msg_save_retain, msg_load_measure, wire, retain),
        ("tblm2", "Msg") => arm!(tblm2, msg_load, msg_load_retain, msg_save, msg_save_checked, msg_save_retain, msg_load_measure, wire, retain),
        ("tblp1", "Chain") => arm!(tblp1, chain_load, chain_load_retain, chain_save, chain_save_checked, chain_save_retain, chain_load_measure, wire, retain),
        ("tblp2", "Chain") => arm!(tblp2, chain_load, chain_load_retain, chain_save, chain_save_checked, chain_save_retain, chain_load_measure, wire, retain),
        ("tblp3", "Chain") => arm!(tblp3, chain_load, chain_load_retain, chain_save, chain_save_checked, chain_save_retain, chain_load_measure, wire, retain),
        ("tblr1", "Cfg") => arm!(tblr1, cfg_load, cfg_load_retain, cfg_save, cfg_save_checked, cfg_save_retain, cfg_load_measure, wire, retain),
        ("tblr2", "Cfg") => arm!(tblr2, cfg_load, cfg_load_retain, cfg_save, cfg_save_checked, cfg_save_retain, cfg_load_measure, wire, retain),
        ("tblscalars2", "SimState") => arm!(tblscalars2, sim_state_load, sim_state_load_retain, sim_state_save, sim_state_save_checked, sim_state_save_retain, sim_state_load_measure, wire, retain),
        ("tblv1", "Cfg") => arm!(tblv1, cfg_load, cfg_load_retain, cfg_save, cfg_save_checked, cfg_save_retain, cfg_load_measure, wire, retain),
        ("tblv2", "Cfg") => arm!(tblv2, cfg_load, cfg_load_retain, cfg_save, cfg_save_checked, cfg_save_retain, cfg_load_measure, wire, retain),
        ("tblw1", "Fleet") => arm!(tblw1, fleet_load, fleet_load_retain, fleet_save, fleet_save_checked, fleet_save_retain, fleet_load_measure, wire, retain),
        ("tblw2", "Fleet") => arm!(tblw2, fleet_load, fleet_load_retain, fleet_save, fleet_save_checked, fleet_save_retain, fleet_load_measure, wire, retain),
        ("vocab9demo", "Wide00") => arm!(vocab9demo, wide00_load, wide00_load_retain, wide00_save, wide00_save_checked, wide00_save_retain, wide00_load_measure, wire, retain),
        ("vocab9demo", "Wide19") => arm!(vocab9demo, wide19_load, wide19_load_retain, wide19_save, wide19_save_checked, wide19_save_retain, wide19_load_measure, wire, retain),
        ("vocabdemo", "Wide00") => arm!(vocabdemo, wide00_load, wide00_load_retain, wide00_save, wide00_save_checked, wide00_save_retain, wide00_load_measure, wire, retain),
        ("vocabdemo", "Wide09") => arm!(vocabdemo, wide09_load, wide09_load_retain, wide09_save, wide09_save_checked, wide09_save_retain, wide09_load_measure, wire, retain),
        ("widedemo", "Caption") => arm!(widedemo, caption_load, caption_load_retain, caption_save, caption_save_checked, caption_save_retain, caption_load_measure, wire, retain),
        ("widedemo", "Stamp") => arm!(widedemo, stamp_load, stamp_load_retain, stamp_save, stamp_save_checked, stamp_save_retain, stamp_load_measure, wire, retain),
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
    // (unit, root, available, retain, fixed class, message form)
    let mut roster: Vec<(String, String, bool, bool, bool, bool)> = Vec::with_capacity(count);
    for _ in 0..count {
        let unit = read_name().unwrap_or_default();
        let root = read_name().unwrap_or_default();
        let mut form = [0u8; 1];
        let mut retain = [0u8; 1];
        if io::stdin().read_exact(&mut form).is_err() || io::stdin().read_exact(&mut retain).is_err() {
            return;
        }
        let message = form[0] == 2;
        let available = (form[0] == 1 && ROOTS.iter().any(|(u, r)| *u == unit && *r == root))
            || (message && MESSAGE_ROOTS.iter().any(|(u, r)| *u == unit && *r == root));
        let fixed = FIXED_ROOTS.iter().any(|(u, r)| *u == unit && *r == root);
        let _ = out.write_all(&[available as u8]);
        roster.push((unit, root, available, retain[0] == 1, fixed, message));
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
        let (unit, root, _, retain, fixed, message) = &roster[index];
        let Some(mut reply) = (if *message { drive_message(unit, root, &wire) } else { drive(unit, root, &wire, *retain) }) else { return };
        if *fixed && !reply.loaded {
            // The fixed-class form-1 adapter keeps its region on a failed
            // tolerant read; the loader's Option only says it decoded no
            // value. The harness's loaded bit says the region still exists.
            reply.loaded = true;
            if reply.measure < 0 && !reply.refused { reply.malformed = true; }
        }
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
