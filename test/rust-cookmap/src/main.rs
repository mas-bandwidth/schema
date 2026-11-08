// THE RUST COOK OF MAPS AND LISTS (docs/PORTING.md M19, docs/SPEC-TABLES.md
// §2.8, §2.9, §7.6).
//
// Two halves, each held to something this leg did not write:
//
//   WRITE  every map and list instance of the corpus is loaded from its wire
//          and cooked by the generated writer in both byte orders, and the
//          bytes are `schema cook`'s (testdata/conformance/tables/cook-write),
//          which the C++ reference's own cook also lands on;
//   READ   map_full's cook is opened and every map of its root is searched:
//          the entries lie ascending, every key Find answers is the entry that
//          holds it, and a key the map does not hold answers None.
//
// Run from the repository root.

use std::process::exit;
use std::sync::atomic::{AtomicU32, Ordering};

static FAILURES: AtomicU32 = AtomicU32::new(0);

fn fail(why: String) {
    println!("FAIL {why}");
    FAILURES.fetch_add(1, Ordering::Relaxed);
}

fn slurp(path: &str) -> Vec<u8> {
    std::fs::read(path).unwrap_or_else(|e| {
        println!("FAIL cannot read {path}: {e}");
        exit(1)
    })
}

macro_rules! cook_case {
    ($name:expr, $krate:ident, $load:ident, $cook:ident) => {{
        let wire = slurp(&format!("testdata/wire/tables/{}.bin", $name));
        let mut report = Default::default();
        match $krate::$load(&wire, &mut report) {
            None => fail(format!("{}: the wire did not load", $name)),
            Some(v) => {
                for (order, suffix) in [(1u64, ""), (2u64, "-be")] {
                    let want = slurp(&format!("testdata/conformance/tables/cook-write/{}{}.cook", $name, suffix));
                    match $krate::$cook(&v, order) {
                        Err(why) => fail(format!("{}{}: the cook refused: {why}", $name, suffix)),
                        Ok(got) if got != want => {
                            let at = got.iter().zip(want.iter()).take_while(|(a, b)| a == b).count();
                            fail(format!("{}{}: {} bytes against the tool's {}, first difference at byte {at}", $name, suffix, got.len(), want.len()));
                        }
                        Ok(_) => {}
                    }
                }
            }
        }
    }};
}

fn text(buffer: &[u8], length: i32) -> &[u8] {
    &buffer[..length.max(0) as usize]
}

fn read_half() {
    let wire = slurp("testdata/wire/tables/map_full.bin");
    let mut report = Default::default();
    let Some(v) = mapdemo::fleet_load(&wire, &mut report) else {
        fail(String::from("map_full: the wire did not load"));
        return;
    };
    let cooked = mapdemo::fleet_cook_write(&v, 1).unwrap_or_else(|why| {
        println!("FAIL map_full: the cook refused: {why}");
        exit(1)
    });
    let Some(cook) = (unsafe { mapdemo::FleetCook::open(cooked.as_ptr(), cooked.len() as u64) }) else {
        fail(String::from("map_full: the cook did not open"));
        return;
    };
    let root = unsafe { &*cook.root() };
    let mut searched = 0;

    // a STRING key: bytes, unsigned, a prefix first
    let ships = unsafe { root.ships.as_slice() };
    for pair in ships.windows(2) {
        if text(&pair[0].key, pair[0].key_length) >= text(&pair[1].key, pair[1].key_length) {
            fail(String::from("Fleet.ships: the entries do not ascend"));
        }
    }
    for e in ships {
        let key = text(&e.key, e.key_length);
        match unsafe { root.ships_find(key) } {
            Some(found) if std::ptr::eq(found, e) => searched += 1,
            _ => fail(format!("Fleet.ships: Find did not answer the entry holding {:?}", String::from_utf8_lossy(key))),
        }
    }
    if unsafe { root.ships_find(b"no such ship") }.is_some() {
        fail(String::from("Fleet.ships: Find answered a key the map does not hold"));
    }

    // a SIGNED key: -3 before 2
    let tiers = unsafe { root.tiers.as_slice() };
    for pair in tiers.windows(2) {
        if pair[0].key >= pair[1].key {
            fail(String::from("Fleet.tiers: the entries do not ascend"));
        }
    }
    for e in tiers {
        match unsafe { root.tiers_find(e.key) } {
            Some(found) if std::ptr::eq(found, e) => searched += 1,
            _ => fail(format!("Fleet.tiers: Find did not answer the entry holding {}", e.key)),
        }
    }

    // an UNSIGNED key over pointer values
    let by_id = unsafe { root.by_id.as_slice() };
    for e in by_id {
        match unsafe { root.by_id_find(e.key) } {
            Some(found) if std::ptr::eq(found, e) => searched += 1,
            _ => fail(format!("Fleet.by_id: Find did not answer the entry holding {}", e.key)),
        }
    }

    // and a map INSIDE an entry: its own array in the same extent
    for e in unsafe { root.loadouts.as_slice() } {
        let inner = unsafe { e.value.as_slice() };
        for x in inner {
            match unsafe { e.value_find(x.key) } {
                Some(found) if std::ptr::eq(found, x) => searched += 1,
                _ => fail(format!("Fleet.loadouts: the inner Find did not answer the entry holding {}", x.key)),
            }
        }
    }
    if searched == 0 {
        fail(String::from("map_full: no entry was searched"));
    }
    println!("rust cook map find: {searched} entries found in place by binary search");
}

fn main() {
    cook_case!("map_cells", mapdemo, cells_load, cells_cook_write);
    cook_case!("map_chunks", mapdemo, chunks_load, chunks_cook_write);
    cook_case!("map_crews", mapdemo, crews_load, crews_cook_write);
    cook_case!("map_depth", mapdemo, depth_load, depth_cook_write);
    cook_case!("map_docs", mapdemo, docs_load, docs_cook_write);
    cook_case!("map_empty", mapdemo, fleet_load, fleet_cook_write);
    cook_case!("map_full", mapdemo, fleet_load, fleet_cook_write);
    cook_case!("map_pairs", mapdemo, pairs_load, pairs_cook_write);
    cook_case!("map_slots", mapdemo, slots_load, slots_cook_write);
    cook_case!("map_spans", mapdemo, spans_load, spans_cook_write);
    cook_case!("map_text", mapdemo, text_load, text_cook_write);
    cook_case!("map_trails", mapdemo, trails_load, trails_cook_write);
    cook_case!("map_runs", mapdemo, runs_load, runs_cook_write);
    cook_case!("list_tables", listdemo, save_load, save_cook_write);
    cook_case!("list_scalars", listdemo, save_load, save_cook_write);
    cook_case!("list_empty", listdemo, save_load, save_cook_write);
    cook_case!("list_erased", listdemo, save_load, save_cook_write);
    cook_case!("list_mixed", listdemo, mixed_load, mixed_cook_write);
    cook_case!("list_shared", listdemo, album_load, album_cook_write);
    cook_case!("list_before_pointer", listdemo, album_load, album_cook_write);
    cook_case!("list_nested", listdemo, sheet_load, sheet_cook_write);
    cook_case!("list_of_maps", listdemo, army_load, army_cook_write);
    cook_case!("list_migrates", listdemo, unbounded_load, unbounded_cook_write);
    // a 128-bit element at sizeof = 8 mod 16: the node aligns to sixteen
    cook_case!("widelist_wide", widelistdemo, wide_load, wide_cook_write);
    read_half();
    let failures = FAILURES.load(Ordering::Relaxed);
    if failures > 0 {
        println!("rust cook map: {failures} failure(s)");
        exit(1);
    }
    println!("rust cook map: 24 map and list instances land on the tool's cook in both byte orders");
}
