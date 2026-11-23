// THE 2026-11-22 SURFACES (test/conformance/README.md, "The 2026-11-22
// surfaces ask what no surface asked before"; docs/SPEC-CONFORMANCE.md §2).
//
// Every answer here is what a generated crate's own call did: a write that
// returned, a named Err or None, or a debug_assert! panic (this driver builds
// in the dev profile, so an assert leg's write contract fires: docs/SPEC.md
// §5.1). The driver sets the one value a row names and calls the runtime; it
// never decides a verdict the runtime did not give. A value this leg's storage
// cannot hold (a native [N]T holding N+1, an i8 holding 128, a Vec holding no
// list) is `unrepresentable`, never `refused` on the runtime's behalf.

use super::*;

// ---------------------------------------------------------------------------
// the new write-refusal kinds, set on a value or a Row
// ---------------------------------------------------------------------------

// `elems` on a Vec: more elements are copies of element 0, fewer drop the
// tail (testdata/conformance/tables/FORMAT.md).
fn wref_elems<T: Clone>(v: &mut Vec<T>, value: &str) -> Mutation {
    let n = wref_usize(value);
    if n > v.len() {
        let Some(first) = v.first().cloned() else { fail(&format!("write-refusal: elems {value} on an empty array")) };
        v.resize(n, first);
    } else {
        v.truncate(n);
    }
    Mutation::Done
}

// `elems` on a native [T; N]: it holds exactly N, so any other count is no
// value of the type.
fn wref_native_elems(len: usize, value: &str) -> Mutation {
    if wref_usize(value) == len { Mutation::Done } else { Mutation::Unrepresentable }
}

// `count` on a Vec: `-1` is a list field holding no list, which a Vec<T>
// field cannot be (SF-28); any other count is `elems`.
fn wref_vec_count<T: Clone>(v: &mut Vec<T>, value: &str) -> Mutation {
    let Ok(n) = value.parse::<i64>() else { fail(&format!("write-refusal: count {value}")) };
    if n < 0 {
        return Mutation::Unrepresentable;
    }
    wref_elems(v, value)
}

fn wref_arrays(v: &mut tblsurf::Arrays, path: &str, kind: &str, value: &str) -> Mutation {
    match (path, kind) {
        ("trio", "elems") => wref_elems(&mut v.trio, value),
        ("per", "elems") => wref_elems(&mut v.per, value),
        ("marks", "mask") => wref_int(value, &mut v.marks),
        _ => fail(&format!("write-refusal: Arrays names no {kind} path {path}")),
    }
}

fn wref_arrays_row(v: &mut tblsurf::ArraysRow, path: &str, kind: &str, value: &str) -> Mutation {
    match (path, kind) {
        ("trio", "elems") => wref_native_elems(v.trio.len(), value),
        ("per", "elems") => wref_native_elems(v.per.len(), value),
        ("marks", "mask") => wref_int(value, &mut v.marks),
        _ => fail(&format!("write-refusal: Arrays names no {kind} path {path}")),
    }
}

fn wref_counted(v: &mut tblsurf::Counted, path: &str, kind: &str, value: &str) -> Mutation {
    match (path, kind) {
        ("few", "count") => wref_vec_count(&mut v.few, value),
        _ => fail(&format!("write-refusal: Counted names no {kind} path {path}")),
    }
}

// `dupkey 1` appends a copy of the map's first entry, so its key is held
// twice; `dupkey 0` leaves the map as loaded (FL-22). This leg's map is a Vec
// of entries, which holds a key twice.
fn wref_fleet(v: &mut mapdemo::Fleet, path: &str, kind: &str, value: &str) -> Mutation {
    match (path, kind, value) {
        ("ships", "dupkey", "0") => Mutation::Done,
        ("ships", "dupkey", "1") => {
            let Some(first) = v.ships.first().cloned() else { fail("write-refusal: Fleet fixture holds no ships entry") };
            v.ships.push(first);
            Mutation::Done
        }
        _ => fail(&format!("write-refusal: Fleet names no {kind} {value} at path {path}")),
    }
}

fn wref_profile_config_row(v: &mut tabledemo::ProfileConfigRow, path: &str, kind: &str, value: &str) -> Mutation {
    match (path, kind) {
        // FL-04: the Row's storage is exactly the declared width
        ("tilt", "int") => wref_int(value, &mut v.tilt),
        ("badge", "int") => wref_int(value, &mut v.badge),
        ("experience", "int") => wref_int(value, &mut v.experience),
        _ => fail(&format!("write-refusal: ProfileConfig names no {kind} path {path}")),
    }
}

// `point` sets the pointer at `path` to the node the pointer at `value` points
// at (FL-28). The graph is Rc<ListNode> with no interior mutability: a node,
// once shared, cannot be changed, so a pointer inside a node cannot be made
// to point at a node that encloses it. A cycle is no value this storage holds.
fn wref_scene_point(v: &mut graphdemo::Scene, path: &str, value: &str) -> Mutation {
    let target = match value {
        "head" => v.head.clone(),
        "alias" => v.alias.clone(),
        _ => fail(&format!("write-refusal: Scene names no pointer {value}")),
    };
    match path {
        "head" => v.head = target,
        "alias" => v.alias = target,
        "head.next" | "alias.next" => return Mutation::Unrepresentable,
        _ => fail(&format!("write-refusal: Scene names no pointer path {path}")),
    }
    Mutation::Done
}

// ---------------------------------------------------------------------------
// the values the rows start from
// ---------------------------------------------------------------------------

fn arrays_row(case: &str, wire: &str) -> tblsurf::ArraysRow {
    let data = slurp(wire);
    wref_fixed_row!(tblsurf, arrays_fixed_load, ArraysRow, case, wire, &data)
}

// the Arrays VALUE, through the crate's own conversion from its fixed record
fn arrays_value(case: &str, wire: &str) -> tblsurf::Arrays {
    tblsurf::Arrays::try_from(&arrays_row(case, wire)).unwrap_or_else(|_| fail(&format!("{case}: the Arrays record has no value")))
}

fn counted_value(case: &str, wire: &str) -> tblsurf::Counted {
    let mut report = tblsurf::TableReport::default();
    tblsurf::counted_load(&slurp(wire), &mut report).unwrap_or_else(|| fail(&format!("{case}: load refused {wire}")))
}

fn fleet_value(case: &str, wire: &str) -> mapdemo::Fleet {
    let mut report = mapdemo::TableReport::default();
    mapdemo::fleet_load(&slurp(wire), &mut report).unwrap_or_else(|| fail(&format!("{case}: load refused {wire}")))
}

// A row's answer: the verdict the writer gave, or a per-case absence with
// its reason when this leg carries no writer for the row's root.
pub(crate) enum Answer {
    Verdict(&'static str),
    Absent(String),
}

fn verdict(answer: &'static str) -> Option<Answer> {
    Some(Answer::Verdict(answer))
}

// The value's own fixed record (the crate's From conversion, where the
// write contract's debug asserts sit) saved by FixedSave as a batch of one.
fn arrays_fixed_save(v: &tblsurf::Arrays) -> Result<Vec<u8>, String> {
    let row = tblsurf::ArraysRow::from(v);
    let mut buffer = vec![0u8; tblsurf::arrays_fixed_measure(1)];
    let written = tblsurf::arrays_fixed_save(&[row], &mut buffer).ok_or_else(String::new)?;
    buffer.truncate(written);
    Ok(buffer)
}

fn answer(m: Mutation, write: impl FnOnce() -> &'static str) -> Option<Answer> {
    match m {
        Mutation::Done => verdict(write()),
        Mutation::Unrepresentable => verdict("unrepresentable\n"),
    }
}

fn json_verdict<F: FnOnce() -> Option<String>>(write: F) -> &'static str {
    match wref_run(|| write().ok_or_else(String::new)) {
        Ok(_) => "wrote\n",
        Err(_) => "refused\n",
    }
}

// ---------------------------------------------------------------------------
// write-refusal[-fixed|-message|-json|-cook|-block]: the 2026-11-22 rows
// ---------------------------------------------------------------------------

// write_refusal answers a row whose root this module carries, and None for a
// row the surface's own function answers. `form` is the writer: "" the file
// save, "fixed", "message", "json", "cook" or "block".
pub(crate) fn write_refusal(manifest: &Manifest, form: &str, f: &[String]) -> Option<Answer> {
    if f.len() < 8 {
        fail(&format!("write-refusal line has {} fields", f.len()));
    }
    let (case, unit, root, wire, path, kind, value) = (f[1].as_str(), f[2].as_str(), f[3].as_str(), f[4].as_str(), f[5].as_str(), f[6].as_str(), f[7].as_str());
    if form == "message" {
        return write_refusal_message(manifest, f);
    }
    match (form, unit, root) {
        // a fixed root: the value, its own record, FixedSave (the file form of
        // a declared fixed root is form 3, and FixedSave is its writer)
        ("" | "fixed", "tblsurf", "Arrays") => {
            let mut v = arrays_value(case, wire);
            answer(wref_arrays(&mut v, path, kind, value), || wref_save(|| arrays_fixed_save(&v)))
        }
        ("", "tabledemo", "ProfileConfig") => {
            let data = slurp(wire);
            let mut row = wref_fixed_row!(tabledemo, profile_config_fixed_load, ProfileConfigRow, case, wire, &data);
            answer(wref_profile_config_row(&mut row, path, kind, value), || {
                wref_save(|| {
                    let mut buffer = vec![0u8; tabledemo::profile_config_fixed_measure(1)];
                    tabledemo::profile_config_fixed_save(&[row], &mut buffer).ok_or_else(String::new)
                })
            })
        }
        ("", "tblsurf", "Counted") => {
            let mut v = counted_value(case, wire);
            answer(wref_counted(&mut v, path, kind, value), || wref_save(|| tblsurf::counted_save_checked(&v)))
        }
        ("", "mapdemo", "Fleet") => {
            let mut v = fleet_value(case, wire);
            answer(wref_fleet(&mut v, path, kind, value), || wref_save(|| mapdemo::fleet_save_checked(&v)))
        }
        // the text writer
        ("json", "tblsurf", "Arrays") => {
            let mut v = arrays_value(case, wire);
            answer(wref_arrays(&mut v, path, kind, value), || json_verdict(|| tblsurf::arrays_to_json_checked(&v)))
        }
        ("json", "tblsurf", "Counted") => {
            let mut v = counted_value(case, wire);
            answer(wref_counted(&mut v, path, kind, value), || json_verdict(|| tblsurf::counted_to_json_checked(&v)))
        }
        ("json", "mapdemo", "Fleet") => {
            let mut v = fleet_value(case, wire);
            answer(wref_fleet(&mut v, path, kind, value), || json_verdict(|| mapdemo::fleet_to_json_checked(&v)))
        }
        // THE COOK WRITER (SPEC-TABLES §7.6): <root>_cook_write measures and
        // writes the whole cook in one call, little-endian (order 1)
        ("cook", "graphdemo", "Scene") => {
            if kind != "point" {
                fail(&format!("write-refusal-cook: Scene names no {kind} path {path}"));
            }
            let mut report = graphdemo::TableReport::default();
            let mut v = graphdemo::scene_load(&slurp(wire), &mut report).unwrap_or_else(|| fail(&format!("{case}: load refused {wire}")));
            answer(wref_scene_point(&mut v, path, value), || wref_save(|| graphdemo::scene_cook_write(&v, 1)))
        }
        ("cook", "tblsurf", "Counted") => {
            let mut v = counted_value(case, wire);
            answer(wref_counted(&mut v, path, kind, value), || wref_save(|| tblsurf::counted_cook_write(&v, 1)))
        }
        ("cook", "tblsurf", "Arrays") => {
            let mut v = arrays_value(case, wire);
            answer(wref_arrays(&mut v, path, kind, value), || wref_save(|| tblsurf::arrays_cook_write(&v, 1)))
        }
        ("cook", "tabledemo", "RootConfig") => {
            use tabledemo::{RootConfig, RootConfigRow, root_config_fixed_load};
            let data = slurp(wire);
            let Some(mut v) = fixed_storage_value!(tabledemo, RootConfigRow, RootConfig, root_config_fixed_load, &data) else {
                fail(&format!("{case}: fixed load refused {wire}"));
            };
            answer(wref_root_config(&mut v, path, kind, value), || wref_save(|| tabledemo::root_config_cook_write(&v, 1)))
        }
        // THE BLOCK BUILDER (SPEC-TABLES §19.1): <root>_block_begin is handed
        // the storage and the root's counts (these roots declare none) and
        // writes the prologue; the projection's fields are the value's, filled
        // into the block it built. `wrote` when the builder built the block.
        ("block", "backenddemo", "StorePurchase") => {
            let data = slurp(wire);
            let mut row = wref_fixed_row!(backenddemo, store_purchase_fixed_load, StorePurchaseRow, case, wire, &data);
            answer(wref_store_purchase_row(&mut row, path, kind, value), || {
                block_build::<backenddemo::StorePurchaseBlockProjection>(|buf, capacity| unsafe {
                    let used = backenddemo::store_purchase_block_begin(buf, capacity, &backenddemo::StorePurchaseCounts::default())?;
                    let p = &mut *(buf as *mut backenddemo::StorePurchaseBlockProjection);
                    p.player_id = row.player_id;
                    p.sku = row.sku;
                    p.sku_length = row.sku_length;
                    p.quantity = row.quantity;
                    p.price_minor = row.price_minor;
                    p.currency = row.currency;
                    Some(used)
                })
            })
        }
        ("block", "backenddemo", "MatchResult") => {
            let data = slurp(wire);
            let row = wref_fixed_row!(backenddemo, match_result_fixed_load, MatchResultRow, case, wire, &data);
            let m = match (path, kind) {
                ("players", "elems") => wref_native_elems(row.players.len(), value),
                _ => fail(&format!("write-refusal-block: MatchResult names no {kind} path {path}")),
            };
            answer(m, || {
                block_build::<backenddemo::MatchResultBlockProjection>(|buf, capacity| unsafe {
                    let used = backenddemo::match_result_block_begin(buf, capacity, &backenddemo::MatchResultCounts::default())?;
                    let p = &mut *(buf as *mut backenddemo::MatchResultBlockProjection);
                    p.match_id = row.match_id;
                    p.players = row.players;
                    Some(used)
                })
            })
        }
        ("cook" | "block", _, _) => fail(&format!("write-refusal-{form}: no root {unit} {root}")),
        _ => {
            if wire == "-" {
                fail(&format!("write-refusal{}: {case}: a fresh-value row on a root this leg does not carry ({unit} {root})", if form.is_empty() { String::new() } else { format!("-{form}") }));
            }
            None
        }
    }
}

// One block built into 64-byte-aligned storage (§19.1) with room for the
// projection; the builder's own None, or a debug_assert! panic, is the
// refusal.
fn block_build<P>(build: impl FnOnce(*mut u8, i64) -> Option<i64>) -> &'static str {
    let capacity = (std::mem::size_of::<P>() + 63) / 64 * 64 + 64;
    let layout = Layout::from_size_align(capacity, 64).unwrap_or_else(|_| fail("block: layout"));
    let buf = unsafe { alloc_zeroed(layout) };
    if buf.is_null() {
        fail("block: allocation failed");
    }
    let built = wref_run(|| build(buf, capacity as i64).ok_or_else(String::new));
    unsafe { dealloc(buf, layout) };
    match built {
        Ok(_) => "wrote\n",
        Err(_) => "refused\n",
    }
}

// write-refusal-message on surfaces_conn: the announcement read into the
// direction's vocabulary, the Row set, MeasureMessages then SaveMessages as a
// batch of one. A message Row is native storage ([N]T is [T; N]).
fn write_refusal_message(manifest: &Manifest, f: &[String]) -> Option<Answer> {
    let (case, conn, root, wire, path, kind, value) = (f[1].as_str(), f[2].as_str(), f[3].as_str(), f[4].as_str(), f[5].as_str(), f[6].as_str(), f[7].as_str());
    let Some(c) = manifest.of_kind("connection").find(|c| c[1] == conn) else {
        fail(&format!("write-refusal-message {case} names no connection {conn}"));
    };
    if c[2] != "tblsurf" {
        if wire == "-" {
            fail(&format!("write-refusal-message: {case}: a fresh-value row on a connection this leg does not carry ({conn})"));
        }
        return None;
    }
    let ann = slurp(&c[4]);
    let mut storage = vec![tblsurf::TableMessageEntry::default(); 4096];
    let mut vocabulary = tblsurf::TableVocabulary::new(&mut storage);
    let mut report = tblsurf::TableReport::default();
    if !tblsurf::announce_read(&mut vocabulary, &ann, &mut report) {
        fail(&format!("{case}: the announcement {} did not read", c[4]));
    }
    macro_rules! send_one {
        ($row:expr, $measure:ident, $save:ident) => {{
            let row = $row;
            wref_save(|| {
                let mut ignored = tblsurf::TableReport::default();
                let n = tblsurf::$measure(&[row], &mut ignored);
                if n < 0 {
                    return Err(String::new());
                }
                let mut buffer = vec![0u8; n as usize];
                if tblsurf::$save(&[row], &mut buffer, &mut ignored) != n {
                    return Err(String::new());
                }
                Ok(buffer)
            })
        }};
    }
    match root {
        "Narrow" | "Wide" => {
            let data = slurp(wire);
            if (path, kind) != ("hp", "int") {
                fail(&format!("write-refusal-message: {root} names no {kind} path {path}"));
            }
            if root == "Narrow" {
                let mut row = wref_fixed_row!(tblsurf, narrow_fixed_load, NarrowRow, case, wire, &data);
                answer(wref_int(value, &mut row.hp), || send_one!(row, narrow_measure_messages, narrow_save_messages))
            } else {
                let mut row = wref_fixed_row!(tblsurf, wide_fixed_load, WideRow, case, wire, &data);
                answer(wref_int(value, &mut row.hp), || send_one!(row, wide_measure_messages, wide_save_messages))
            }
        }
        "Arrays" => {
            let mut row = arrays_row(case, wire);
            answer(wref_arrays_row(&mut row, path, kind, value), || send_one!(row, arrays_measure_messages, arrays_save_messages))
        }
        // SF-05, SF-06: the generated crate emits no big_measure_messages or
        // big_save_messages for a fixed table past the fixed form's ceiling,
        // so there is no message writer to hand the value to
        "Big" => {
            Some(Answer::Absent(String::from("the Rust leg emits no message verbs (big_measure_messages, big_save_messages) for Big, a fixed table past the 65536-byte fixed-form ceiling")))
        }
        _ => fail(&format!("write-refusal-message: no root tblsurf {root}")),
    }
}

// the answer file, or `<case>.absent` with its reason
pub(crate) fn spill_answer(out: &str, case: &str, answer: &Answer) {
    match answer {
        Answer::Verdict(text) => spill(out, case, text.as_bytes()),
        Answer::Absent(why) => spill(out, &format!("{case}.absent"), why.as_bytes()),
    }
}

// write-refusal-cook and write-refusal-block: every row is this module's.
pub(crate) fn surface_write_refusal_form(manifest: &Manifest, out: &str, surface: &str, form: &str) {
    for f in manifest.of_kind(surface) {
        let Some(answer) = write_refusal(manifest, form, f) else {
            fail(&format!("{surface}: no root {} {}", f[2], f[3]));
        };
        spill_answer(out, &f[1], &answer);
    }
}

// ---------------------------------------------------------------------------
// announce <case> <unit> <root> <mode> <input>
// ---------------------------------------------------------------------------

macro_rules! announce_read_mode {
    ($k:ident, $mode:expr, $input:expr) => {{
        use $k as k;
        let data = slurp($input);
        let mut storage = vec![k::TableMessageEntry::default(); 4096];
        let mut vocabulary = k::TableVocabulary::new(&mut storage);
        // FL-36: STRICT MODE IS OPT-IN (§3.3): `lenient` leaves the flag at
        // the vocabulary's own default
        if $mode == "strict" {
            vocabulary.strict = true;
        }
        let mut report = k::TableReport::default();
        if k::announce_read(&mut vocabulary, &data, &mut report) { b"read\n".to_vec() } else { b"refused\n".to_vec() }
    }};
}

macro_rules! announce_write {
    ($k:ident) => {{
        use $k as k;
        let mut buffer = vec![0u8; k::announce_measure()];
        let n = k::announce(&mut buffer);
        if n < 0 {
            b"refused\n".to_vec()
        } else {
            buffer.truncate(n as usize);
            buffer
        }
    }};
}

// FL-18: SaveMessages as a batch of one, no announcement read or written
macro_rules! announce_send {
    ($row:expr, $measure:ident, $save:ident) => {{
        let row = $row;
        let mut report = tblsurf::TableReport::default();
        let n = tblsurf::$measure(&[row], &mut report);
        let mut buffer = vec![0u8; n.max(0) as usize];
        if n < 0 || tblsurf::$save(&[row], &mut buffer, &mut report) != n {
            b"refused\n".to_vec()
        } else {
            buffer
        }
    }};
}

pub(crate) fn surface_announce(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("announce") {
        if f.len() != 6 {
            fail(&format!("announce line has {} fields", f.len()));
        }
        let (case, unit, root, mode, input) = (f[1].as_str(), f[2].as_str(), f[3].as_str(), f[4].as_str(), f[5].as_str());
        let answer = match (mode, unit) {
            ("write", "tblann4096") => announce_write!(tblann4096),
            ("write", "tblann4097") => announce_write!(tblann4097),
            ("write", "tblsurf") => announce_write!(tblsurf),
            ("strict" | "lenient", "messagedemo") => announce_read_mode!(messagedemo, mode, input),
            ("strict" | "lenient", "tblsurf") => announce_read_mode!(tblsurf, mode, input),
            ("send", "tblsurf") => match root {
                "Narrow" => {
                    let row = if input == "-" {
                        tblsurf::NarrowRow::from(&tblsurf::Narrow::default())
                    } else {
                        let data = slurp(input);
                        wref_fixed_row!(tblsurf, narrow_fixed_load, NarrowRow, case, input, &data)
                    };
                    announce_send!(row, narrow_measure_messages, narrow_save_messages)
                }
                "Arrays" => {
                    let row = if input == "-" {
                        tblsurf::ArraysRow::from(&tblsurf::Arrays::default())
                    } else {
                        arrays_row(case, input)
                    };
                    announce_send!(row, arrays_measure_messages, arrays_save_messages)
                }
                _ => fail(&format!("announce {case}: no message root tblsurf {root}")),
            },
            _ => fail(&format!("announce {case}: no {mode} for unit {unit}")),
        };
        spill(out, case, &answer);
    }
}

// ---------------------------------------------------------------------------
// read-into <case> <unit> <root> <form> <lawful> <damaged> <count> <announcement>
// ---------------------------------------------------------------------------
//
// ONE report and <count> caller values through a lawful read, a refused read
// and the lawful read again (SPEC-TABLES §6.5). Each read's line is the
// call's own outcome: `read` when it returned success with no refusal on its
// report, `refused` when it returned its refusal, `threw` when it panicked
// (caught only to say so), and `read-with-refusal` when it returned success
// while the report it was handed still held a refusal (FL-02 broken).

fn read_line(outcome: Result<bool, String>, refused_on_report: bool) -> &'static str {
    match outcome {
        Err(_) => "threw\n",
        Ok(true) if refused_on_report => "read-with-refusal\n",
        Ok(true) => "read\n",
        Ok(false) => "refused\n",
    }
}

fn catch<T>(read: impl FnOnce() -> T) -> Result<T, String> {
    wref_run(|| Ok(read()))
}

macro_rules! read_into_fixed {
    ($k:ident, $row:ident, $value:ident, $load:ident, $to:ident, $case:expr, $lawful:expr, $damaged:expr, $count:expr) => {{
        use $k as k;
        let lawful = slurp($lawful);
        let damaged = slurp($damaged);
        // fresh values: the root's declared defaults, through the crate's own
        // conversion into its record
        let mut values = vec![k::$row::from(&k::$value::default()); $count];
        let mut plan = vec![k::TableFixedEntry::default(); FIXED_PLAN];
        let mut remap = vec![0u16; FIXED_PLAN];
        let mut report = k::TableFixedReport::default();
        let mut text = String::new();
        let mut read = |data: &[u8], values: &mut Vec<k::$row>, report: &mut k::TableFixedReport| -> &'static str {
            let got = catch(|| k::$load(values, data, &mut plan, &mut remap, report).is_some());
            read_line(got, report.refused || report.damaged)
        };
        text.push_str(read(&lawful, &mut values, &mut report));
        text.push_str(read(&damaged, &mut values, &mut report));
        for (i, row) in values.iter().enumerate() {
            let value = k::$value::try_from(row).unwrap_or_else(|_| fail(&format!("{}: value {i} has no text value", $case)));
            text.push_str(&k::$to(&value).unwrap_or_else(|| fail(&format!("{}: ToJson refused value {i}", $case))));
        }
        text.push_str(read(&lawful, &mut values, &mut report));
        text
    }};
}

macro_rules! read_into_message {
    ($k:ident, $row:ident, $value:ident, $load:ident, $to:ident, $case:expr, $lawful:expr, $damaged:expr, $count:expr, $ann:expr) => {{
        use $k as k;
        let lawful = slurp($lawful);
        let damaged = slurp($damaged);
        let ann = slurp($ann);
        let mut storage = vec![k::TableMessageEntry::default(); 4096];
        let mut vocabulary = k::TableVocabulary::new(&mut storage);
        let mut announced = k::TableReport::default();
        if !k::announce_read(&mut vocabulary, &ann, &mut announced) {
            fail(&format!("{}: the announcement {} did not read", $case, $ann));
        }
        let mut values = vec![k::$row::from(&k::$value::default()); $count];
        let mut report = k::TableReport::default();
        let mut text = String::new();
        let read = |data: &[u8], values: &mut Vec<k::$row>, report: &mut k::TableReport| -> &'static str {
            let got = catch(|| k::$load(values, &vocabulary, data, report).1);
            read_line(got, report.refused || report.damaged)
        };
        text.push_str(read(&lawful, &mut values, &mut report));
        text.push_str(read(&damaged, &mut values, &mut report));
        for (i, row) in values.iter().enumerate() {
            let value = k::$value::try_from(row).unwrap_or_else(|_| fail(&format!("{}: value {i} has no text value", $case)));
            text.push_str(&k::$to(&value).unwrap_or_else(|| fail(&format!("{}: ToJson refused value {i}", $case))));
        }
        text.push_str(read(&lawful, &mut values, &mut report));
        text
    }};
}

pub(crate) fn surface_read_into(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("read-into") {
        if f.len() != 9 {
            fail(&format!("read-into line has {} fields", f.len()));
        }
        let (case, unit, root, form, lawful, damaged, count, ann) = (f[1].as_str(), f[2].as_str(), f[3].as_str(), f[4].as_str(), f[5].as_str(), f[6].as_str(), f[7].as_str(), f[8].as_str());
        let count = || count.parse::<usize>().unwrap_or_else(|_| fail(&format!("read-into {case}: count {count}")));
        match (form, unit, root) {
            ("fixed", "tblsurf", "Narrow") => {
                let text = read_into_fixed!(tblsurf, NarrowRow, Narrow, narrow_fixed_load, narrow_to_json_checked, case, lawful, damaged, count());
                spill(out, case, text.as_bytes());
            }
            ("message", "backenddemo", "StorePurchase") => {
                let text = read_into_message!(backenddemo, StorePurchaseRow, StorePurchase, store_purchase_load_messages, store_purchase_to_json_checked, case, lawful, damaged, count(), ann);
                spill(out, case, text.as_bytes());
            }
            // FL-17: this leg's fixed load takes the caller's slice of records,
            // and the slice IS the capacity, so no call omits it
            ("fixed-nocap", _, _) => spill(out, case, b"unrepresentable\n"),
            // The variable Load and the text read RETURN a fresh value
            // (Option<T>); neither is handed a caller value to read into, so
            // there is no "values the call was handed" for this surface to
            // hold (SPEC-TABLES §6.5)
            ("variable" | "json", _, _) => spill(out, &format!("{case}.absent"), format!("the Rust {form} read returns a fresh value and takes no caller value to read into").as_bytes()),
            _ => fail(&format!("read-into {case}: no {form} read for {unit} {root}")),
        }
    }
}

// ---------------------------------------------------------------------------
// json-write-into <case> <unit> <root> <wire>
// ---------------------------------------------------------------------------
//
// Rust has no process locale of its own: its float formatting never consults
// one. The C library's locale is the process's, so it is set through
// setlocale(LC_ALL, …) to the comma-decimal locale the harness names, and
// the run fails if that is refused. The text writer returns a String and
// takes no caller buffer, so FL-15's tail has no bytes for it to touch; the
// answer is the text it returned.
unsafe extern "C" {
    fn setlocale(category: std::ffi::c_int, locale: *const std::ffi::c_char) -> *mut std::ffi::c_char;
}

#[cfg(target_os = "linux")]
const LC_ALL: std::ffi::c_int = 6;
#[cfg(not(target_os = "linux"))]
const LC_ALL: std::ffi::c_int = 0;

pub(crate) fn surface_json_write_into(manifest: &Manifest, out: &str) {
    let name = std::env::var("SCHEMA_COMMA_LOCALE").unwrap_or_else(|_| fail("json-write-into: SCHEMA_COMMA_LOCALE is not set"));
    let c_name = std::ffi::CString::new(name.clone()).unwrap_or_else(|_| fail("json-write-into: locale name"));
    if unsafe { setlocale(LC_ALL, c_name.as_ptr()) }.is_null() {
        fail(&format!("json-write-into: setlocale(LC_ALL, {name}) was refused"));
    }
    for f in manifest.of_kind("json-write-into") {
        if f.len() != 5 {
            fail(&format!("json-write-into line has {} fields", f.len()));
        }
        let (case, unit, root, wire) = (f[1].as_str(), f[2].as_str(), f[3].as_str(), f[4].as_str());
        let data = slurp(wire);
        let text = if is_fixed_form(&data) {
            match fixed_text_write(unit, root, &data) {
                Some(Some(text)) => text,
                Some(None) => fail(&format!("{case}: the text writer refused the value")),
                None => fail(&format!("{case}: no fixed root {unit} {root}")),
            }
        } else {
            let written = with_root!(unit, root, |load, _save, to_json, _from| {
                let mut report = Default::default();
                let Some(v) = load(&data, &mut report) else { fail(&format!("{case}: load refused {wire}")) };
                to_json(&v).unwrap_or_else(|| fail(&format!("{case}: the text writer refused the value")))
            });
            written.unwrap_or_else(|| fail(&format!("{case}: no root {unit} {root}")))
        };
        spill(out, case, text.as_bytes());
    }
}
