// THE RUST ALLOCATION GATE (docs/PORTING.md M1 and I1; docs/SPEC-TABLES.md,
// "What allocates, and what never does"). The twin of Go's
// TestLoadAllocatesNothing (test/go-tables/alloc_test.go) and of C++'s counting
// `operator new` under tables-cpp-alloc.
//
// THE CLAIM IS MEASURED, NOT READ OFF THE SOURCE. A grep over the generated
// modules for `Vec`/`Box`/`String` proves nothing: a `collect`, a `to_vec`, a
// `format!` or a by-value `String` in a struct the path builds all allocate
// without naming an allocator. So this binary installs a counting
// #[global_allocator] — an instrument the emitter cannot influence — and every
// ROW below is one read path of the generated code, called ITERATIONS times
// with the counter on. A GATED row's ceiling is ZERO allocator calls (alloc,
// alloc_zeroed and realloc; a free is not an allocation), and one call over the
// whole run turns it red. A REPORTED row has no ceiling: it allocates BY DESIGN,
// and its reason is printed beside its count, at the row, so the sentence is
// re-read when the emitter moves (docs/PORTING.md, "A measured row with a null
// ceiling cites why").
//
// THE ROWS, by surface:
//
//   - the FIXED FORM (form byte 3): load into caller-owned rows, plan and
//     remap; measure; save into a caller-owned buffer. RootConfig is the
//     corpus's richest fixed root (nested tables, counted arrays of tables,
//     strings, bytes, a union, a flags mask, every scalar width).
//   - the BLOCK form: open, and the row walk over two out-of-line arrays.
//   - the COOK form: the write into a caller buffer, open plus the root read,
//     and the self-relative DEREF walk down a cooked pointer chain
//     (build/cook-fuzz/ListNode.cook, test/cookgen's fixture).
//   - the VARIABLE wire (form byte 1): the load of a SCALAR-ONLY record, whose
//     value lands in the caller's slot with no heap, and its load_measure. The
//     records that carry a String or a Vec, and the save that answers a Vec, are
//     REPORTED: their storage is heap by the struct's own declaration.
//
// AN INSTRUMENT CHECK RUNS FIRST: one Box per call must count exactly one per
// call. A counter that reads zero there is watching nothing, and the gate
// refuses to report on it.
//
// It reads no clock and opens no socket: ITERATIONS is a count. It is a
// functional gate, a make target (`make tables-rust-alloc`), never a unit test.
//
//   rust-alloc <cook-fuzz-dir>

use std::alloc::{GlobalAlloc, Layout, System};
use std::hint::black_box;
use std::process::exit;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering::Relaxed};

// ---- THE ALLOCATOR CALL COUNTER ----

struct Counting;

static COUNTING: AtomicBool = AtomicBool::new(false);
static CALLS: AtomicU64 = AtomicU64::new(0);

#[inline(always)]
fn count_call() {
    if COUNTING.load(Relaxed) {
        CALLS.fetch_add(1, Relaxed);
    }
}

unsafe impl GlobalAlloc for Counting {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        count_call();
        unsafe { System.alloc(layout) }
    }
    unsafe fn alloc_zeroed(&self, layout: Layout) -> *mut u8 {
        count_call();
        unsafe { System.alloc_zeroed(layout) }
    }
    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        unsafe { System.dealloc(ptr, layout) }
    }
    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        count_call();
        unsafe { System.realloc(ptr, layout, new_size) }
    }
}

#[global_allocator]
static GLOBAL: Counting = Counting;

const ITERATIONS: u64 = 1000;

/// A row's ceiling: ZERO, or no ceiling with the reason it allocates.
#[derive(Clone, Copy)]
enum Ceiling {
    Zero,
    Reported(&'static str),
}

struct Gate {
    red: u32,
    rows: u32,
}

impl Gate {
    /// One row: a warm call outside the window, then ITERATIONS calls inside
    /// it. The body's answer goes to black_box so no call is optimised away.
    fn row(&mut self, name: &str, ceiling: Ceiling, mut body: impl FnMut() -> u64) {
        black_box(body());
        CALLS.store(0, Relaxed);
        COUNTING.store(true, Relaxed);
        for _ in 0..ITERATIONS {
            black_box(body());
        }
        COUNTING.store(false, Relaxed);
        let calls = CALLS.load(Relaxed);
        self.rows += 1;
        let per = calls / ITERATIONS;
        match ceiling {
            Ceiling::Zero if calls == 0 => {
                println!("  ok   {name}: 0 allocation calls over {ITERATIONS} calls")
            }
            Ceiling::Zero => {
                self.red += 1;
                println!(
                    "ALLOC FAILED: {name}: {calls} allocation calls over {ITERATIONS} calls ({per} per call; ceiling 0)"
                );
            }
            Ceiling::Reported(why) => println!(
                "  report {name}: {calls} allocation calls over {ITERATIONS} calls ({per} per call; no ceiling: {why})"
            ),
        }
    }
}

fn die(what: &str) -> ! {
    eprintln!("rust-alloc: {what}");
    exit(1)
}

/// 64-byte aligned caller storage: the block form opens only a 64-aligned base
/// and the cook form only a region aligned to its greatest alignof.
#[derive(Clone, Copy)]
#[repr(C, align(64))]
struct Line([u8; 64]);

fn aligned(bytes: usize) -> Vec<Line> {
    vec![Line([0; 64]); bytes.div_ceil(64)]
}

fn bytes_of(v: &mut [Line]) -> &mut [u8] {
    let n = v.len() * 64;
    unsafe { std::slice::from_raw_parts_mut(v.as_mut_ptr() as *mut u8, n) }
}

// ---- POPULATED FIXED ROWS (every field kind of the tables/examples root) ----

mod demo {
    use tabledemo::*;

    fn text<const N: usize>(s: &str) -> ([u8; N], i32) {
        let mut b = [0u8; N];
        b[..s.len()].copy_from_slice(s.as_bytes());
        (b, s.len() as i32)
    }

    pub fn weapon(i: usize) -> WeaponConfigRow {
        let mut w = WeaponConfigRow {
            damage: 21.0 + i as f32,
            speed: 500.0 - i as f32,
            penetration: (i % 10) as i32,
            channel: (i * 7 % 64) as u32,
            homing: i.is_multiple_of(2),
            ..Default::default()
        };
        match i % 3 {
            0 => w.effect.set_buff(BuffRow { multiplier: 1.5 }),
            1 => w.effect.set_debuff(DebuffRow { amount: 40 }),
            _ => w.effect.set_none(),
        }
        w
    }

    fn loadout(i: usize) -> LoadoutConfigRow {
        let mut l = LoadoutConfigRow {
            grade: 2,
            grades: [1, 2, 3, 0],
            grades_count: 3,
            podium: [3, 2, 1],
            perks: 0b101,
            primary: weapon(i),
            backups: [weapon(i + 1), weapon(i + 2)],
            attachments_count: 3,
            ..Default::default()
        };
        for (j, a) in l.attachments[..3].iter_mut().enumerate() {
            a.slot = j as i32;
            a.power = 1.0 + j as f32;
        }
        l
    }

    fn profile(i: usize) -> ProfileConfigRow {
        let (name, name_length) = text::<33>(["pilot-one", "pilot-two", "pilot-three"][i % 3]);
        ProfileConfigRow {
            name,
            name_length,
            icon: [0xa5; 16],
            icon_length: 16,
            experience: 1000 + i as u32,
            tilt: -3,
            heading: -1200,
            timestamp: 1_790_000_000 + i as i64,
            badge: 7,
            port: 40000,
            epoch: 1 << 40,
            precision: 0.125,
            ratings: [1.0, 2.0, 3.0, 4.0],
            has_loadout: true,
            loadout: loadout(i),
        }
    }

    pub fn root(i: usize) -> RootConfigRow {
        let (version_note, version_note_length) = text::<17>("alloc root");
        let mut r = RootConfigRow {
            version_note,
            version_note_length,
            weapons_count: 5,
            profiles_count: 3,
            ..Default::default()
        };
        for (j, w) in r.weapons[..5].iter_mut().enumerate() {
            *w = weapon(i + j);
        }
        for (j, p) in r.profiles[..3].iter_mut().enumerate() {
            *p = profile(i + j);
        }
        r
    }

    pub fn keyed() -> KeyedConfigRow {
        let mut k = KeyedConfigRow::default();
        for (i, t) in k.teams.iter_mut().enumerate() {
            let (banner, banner_length) = text::<17>(["red", "blue", "green"][i]);
            t.spawn_count = 4 + i as i32;
            t.banner = banner;
            t.banner_length = banner_length;
        }
        for (i, h) in k.hulls.iter_mut().enumerate() {
            h.health = 100.0 + i as f32;
            h.mass = 1.0 + i as f32;
            for (j, t) in h.turrets.iter_mut().enumerate() {
                t.damage = 10.0 + j as f32;
                t.cooldown = 0.5;
                t.gunner_present = j == 1;
                t.gunner.reaction = 0.25;
                t.gunner.tracking = true;
            }
        }
        k.scores.per_team = [10, 20, 30];
        k
    }

    pub fn root_read(r: &RootConfigRow) -> u64 {
        let mut s = r.version_note_length as u64;
        for w in &r.weapons[..r.weapons_count.clamp(0, 8) as usize] {
            s = s.wrapping_add(w.damage.to_bits() as u64 ^ w.effect.tag() as u64);
        }
        for p in &r.profiles[..r.profiles_count.clamp(0, 4) as usize] {
            s = s.wrapping_add(p.experience as u64 ^ p.loadout.primary.channel as u64);
        }
        s
    }

    pub fn keyed_read(k: &KeyedConfigRow) -> u64 {
        let mut s = 0u64;
        for t in &k.teams {
            s = s.wrapping_add(t.spawn_count as u64 ^ t.banner_length as u64);
        }
        for h in &k.hulls {
            s ^= h.health.to_bits() as u64;
            for t in &h.turrets {
                s = s.wrapping_add(t.damage.to_bits() as u64 ^ t.gunner_present as u64);
            }
        }
        s ^ k.scores.per_team.iter().map(|p| *p as u64).sum::<u64>()
    }
}

const RECORDS: usize = 2;
const PLAN: usize = 4096;

fn fixed_rows(gate: &mut Gate) {
    use tabledemo as d;

    // RootConfig: two populated records, written before the window
    let roots = [demo::root(0), demo::root(3)];
    let mut wire = vec![0u8; d::root_config_fixed_measure(RECORDS)];
    if d::root_config_fixed_save(&roots, &mut wire) != Some(wire.len()) {
        die("RootConfig did not save");
    }
    let mut values = vec![d::RootConfigRow::default(); RECORDS];
    let mut plan = vec![d::TableFixedEntry::default(); PLAN];
    let mut remap = vec![0u16; PLAN];
    let mut out = vec![0u8; wire.len()];

    gate.row(
        "fixed.RootConfig load (caller rows, plan, remap)",
        Ceiling::Zero,
        || {
            let mut report = d::TableFixedReport::default();
            let n =
                d::root_config_fixed_load(&mut values, &wire, &mut plan, &mut remap, &mut report);
            if n != Some(RECORDS) || report != d::TableFixedReport::default() {
                die("RootConfig did not load clean");
            }
            values
                .iter()
                .map(demo::root_read)
                .fold(0, u64::wrapping_add)
        },
    );
    gate.row("fixed.RootConfig measure", Ceiling::Zero, || {
        d::root_config_fixed_measure(black_box(RECORDS)) as u64
    });
    gate.row(
        "fixed.RootConfig save (caller buffer)",
        Ceiling::Zero,
        || {
            if d::root_config_fixed_save(&values, &mut out) != Some(out.len()) {
                die("RootConfig did not re-save");
            }
            out[out.len() - 1] as u64
        },
    );
    if out != wire {
        die("RootConfig did not re-save to its own bytes");
    }

    // KeyedConfig: keyed arrays of tables nested by value, optional sections
    let keyed = [demo::keyed()];
    let mut kwire = vec![0u8; d::keyed_config_fixed_measure(1)];
    if d::keyed_config_fixed_save(&keyed, &mut kwire) != Some(kwire.len()) {
        die("KeyedConfig did not save");
    }
    let mut kvalues = vec![d::KeyedConfigRow::default(); 1];
    gate.row(
        "fixed.KeyedConfig load (caller rows, plan, remap)",
        Ceiling::Zero,
        || {
            let mut report = d::TableFixedReport::default();
            if d::keyed_config_fixed_load(&mut kvalues, &kwire, &mut plan, &mut remap, &mut report)
                != Some(1)
            {
                die("KeyedConfig did not load");
            }
            demo::keyed_read(&kvalues[0])
        },
    );
}

fn block_rows(gate: &mut Gate) {
    use blockdemo as b;

    let counts = b::RenderFrameCounts {
        cameras: 1,
        ships: 64,
        lasers: 128,
        ..Default::default()
    };
    // the extent begin needs: generous, and caller-owned
    let mut storage = aligned(64 * 1024);
    let buf = bytes_of(&mut storage);
    let capacity = buf.len() as i64;
    let used = unsafe { b::render_frame_block_begin(buf.as_mut_ptr(), capacity, &counts) }
        .unwrap_or_else(|| die("RenderFrame block begin refused"));
    // fill the rows through the caller's own pointer, at the offsets the
    // instance gives
    {
        let block = unsafe { b::RenderFrameBlock::open(buf.as_ptr(), used) }
            .unwrap_or_else(|| die("RenderFrame block does not open"));
        let ships_at = block.projection().ships.offset_of as usize;
        let lasers_at = block.projection().lasers.offset_of as usize;
        let (ships, lasers) = (block.ships().len(), block.lasers().len());
        if ships != 64 || lasers != 128 {
            die("RenderFrame block answers the wrong counts");
        }
        for i in 0..ships {
            unsafe {
                let row = (buf.as_mut_ptr().add(ships_at) as *mut b::RenderShipRow).add(i);
                (*row).object_id = i as u32;
                (*row).thrust = i as f32;
            }
        }
        for i in 0..lasers {
            unsafe {
                let row = (buf.as_mut_ptr().add(lasers_at) as *mut b::RenderLaserRow).add(i);
                (*row).laser_id = 1000 + i as u32;
            }
        }
    }
    let base = buf.as_ptr();

    gate.row("block.RenderFrame open", Ceiling::Zero, || {
        let block = unsafe { b::RenderFrameBlock::open(black_box(base), used) }
            .unwrap_or_else(|| die("RenderFrame block does not open"));
        block.projection().ships.count as u64
    });
    let block = unsafe { b::RenderFrameBlock::open(base, used) }
        .unwrap_or_else(|| die("RenderFrame block does not open"));
    // THE ROW THE NEGATIVE CONTROL PLANTS INTO: one Box per row walked, 192
    // rows a call, so the planted count is exactly 192 per call.
    gate.row(
        "block.RenderFrame row walk (ships, lasers)",
        Ceiling::Zero,
        || {
            let block = black_box(&block);
            let mut s = 0u64;
            for row in block.ships() {
                #[cfg(feature = "negative-control")]
                black_box(Box::new(row.object_id));
                s = s.wrapping_add(row.object_id as u64 ^ row.thrust.to_bits() as u64);
            }
            for row in block.lasers() {
                #[cfg(feature = "negative-control")]
                black_box(Box::new(row.laser_id));
                s = s.wrapping_add(row.laser_id as u64);
            }
            s
        },
    );
}

fn cook_rows(gate: &mut Gate, cook_dir: &str) {
    use tabledemo as d;

    // KeyedConfig's cook: written into a caller buffer, then opened in place
    let keyed = demo::keyed();
    let need = d::keyed_config_cook_measure(&keyed) as usize;
    let mut storage = aligned(need);
    let buf = &mut bytes_of(&mut storage)[..need];
    gate.row(
        "cook.KeyedConfig write (caller buffer)",
        Ceiling::Zero,
        || {
            if !d::keyed_config_cook_from(&keyed, buf, d::TABLE_COOK_BYTE_ORDER) {
                die("KeyedConfig did not cook");
            }
            buf[need - 1] as u64
        },
    );
    let (base, length) = (buf.as_ptr(), need as u64);
    gate.row("cook.KeyedConfig open and root read", Ceiling::Zero, || {
        let cook = unsafe { d::KeyedConfigCook::open(black_box(base), length) }
            .unwrap_or_else(|| die("KeyedConfig cook does not open"));
        demo::keyed_read(unsafe { &*cook.root() })
    });

    // a cooked POINTER CHAIN (test/cookgen): every hop is a self-relative deref
    let path = format!("{cook_dir}/ListNode.cook");
    let file = std::fs::read(&path).unwrap_or_else(|e| {
        die(&format!(
            "cannot read {path}: {e} (make build/cook-fuzz/.stamp)"
        ))
    });
    let mut chain = aligned(file.len());
    bytes_of(&mut chain)[..file.len()].copy_from_slice(&file);
    let (cbase, clength) = (bytes_of(&mut chain).as_ptr(), file.len() as u64);
    let walk = |base: *const u8| -> u64 {
        let cook = unsafe { graphdemo::ListNodeCook::open(base, clength) }
            .unwrap_or_else(|| die("ListNode cook does not open"));
        let mut node = cook.root();
        let mut s = 0u64;
        let mut hops = 0u64;
        while !node.is_null() && hops < 1 << 20 {
            unsafe {
                s = s.wrapping_add((*node).value as u64);
                node = graphdemo::list_node_at(&(*node).next as *const i64);
            }
            hops += 1;
        }
        (s << 20) | hops
    };
    let hops = walk(cbase) & ((1 << 20) - 1);
    if hops < 2 {
        die("ListNode.cook is not a chain: the deref row would walk nothing");
    }
    println!("  (ListNode.cook: {hops} nodes a walk)");
    gate.row(
        "cook.ListNode open and deref chain walk",
        Ceiling::Zero,
        || walk(black_box(cbase)),
    );
}

fn variable_rows(gate: &mut Gate) {
    use tabledemo::tables as d; // the table layer's types, where a name is also a packet type

    // SCALAR-ONLY records: the value lands in the caller's slot, no heap
    let gunner = d::GunnerConfig {
        reaction: 0.75,
        tracking: true,
    };
    let gwire = d::gunner_config_save(&gunner);
    let mut slot = d::GunnerConfig::default();
    gate.row(
        "variable.GunnerConfig load (scalar-only)",
        Ceiling::Zero,
        || {
            let mut report = d::TableReport::default();
            slot = d::gunner_config_load(&gwire, &mut report)
                .unwrap_or_else(|| die("GunnerConfig did not load"));
            if report != d::TableReport::default() {
                die("GunnerConfig loaded with events");
            }
            slot.reaction.to_bits() as u64 ^ slot.tracking as u64
        },
    );
    gate.row("variable.GunnerConfig load_measure", Ceiling::Zero, || {
        d::gunner_config_load_measure(black_box(&gwire)) as u64
    });

    let weapon = d::WeaponConfig {
        damage: 21.0,
        speed: 500.0,
        penetration: 3,
        channel: 9,
        homing: true,
        effect: d::Effect::Buff(d::Buff { multiplier: 1.5 }),
    };
    let wwire = d::weapon_config_save(&weapon);
    let mut wslot = d::WeaponConfig::default();
    gate.row(
        "variable.WeaponConfig load (scalar-only, a union arm)",
        Ceiling::Zero,
        || {
            let mut report = d::TableReport::default();
            wslot = d::weapon_config_load(&wwire, &mut report)
                .unwrap_or_else(|| die("WeaponConfig did not load"));
            wslot.damage.to_bits() as u64 ^ matches!(wslot.effect, d::Effect::Buff(_)) as u64
        },
    );

    // REPORTED: the storage struct declares heap, so the load fills it
    let team = d::TeamConfig {
        spawn_count: 4,
        banner: String::from("red"),
    };
    let twire = d::team_config_save(&team);
    gate.row(
        "variable.TeamConfig load",
        Ceiling::Reported(
            "its `banner: String` is heap storage the load fills (keyed_table.rs TeamConfig)",
        ),
        || {
            let mut report = d::TableReport::default();
            let v = d::team_config_load(&twire, &mut report)
                .unwrap_or_else(|| die("TeamConfig did not load"));
            v.banner.len() as u64
        },
    );
    let root = d::RootConfig {
        version_note: String::from("alloc root"),
        weapons: vec![weapon.clone(), weapon.clone(), weapon],
        profiles: Vec::new(),
    };
    let rwire = d::root_config_save(&root);
    gate.row(
        "variable.RootConfig load",
        Ceiling::Reported("its `version_note: String` and `weapons: Vec<WeaponConfig>` are heap storage the load fills"),
        || {
            let mut report = d::TableReport::default();
            let v = d::root_config_load(&rwire, &mut report).unwrap_or_else(|| die("RootConfig did not load"));
            v.weapons.len() as u64
        },
    );
    gate.row(
        "variable.GunnerConfig save",
        Ceiling::Reported("the variable-wire save answers a fresh `Vec<u8>` by signature, grown from empty as it writes, and keeps its id table in `IdTable.entries: Vec<u64>`"),
        || d::gunner_config_save(black_box(&gunner)).len() as u64,
    );
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 2 {
        die("usage: rust-alloc <cook-fuzz-dir>");
    }
    if cfg!(feature = "negative-control") {
        println!("SABOTAGED build: one Box planted per row of the block row walk");
    }
    println!("rust alloc gate: {ITERATIONS} calls a row, counting #[global_allocator]");

    // THE INSTRUMENT CHECK: a counter that cannot see one Box per call is
    // watching nothing, and nothing below could go red.
    let mut check = Gate { red: 0, rows: 0 };
    check.row(
        "instrument check: one Box per call",
        Ceiling::Reported("planted"),
        || *black_box(Box::new(7u64)),
    );
    if CALLS.load(Relaxed) != ITERATIONS {
        die(&format!(
            "the counting allocator saw {} calls for {ITERATIONS} planted Boxes: the gate is watching nothing",
            CALLS.load(Relaxed)
        ));
    }

    let mut gate = Gate { red: 0, rows: 0 };
    fixed_rows(&mut gate);
    block_rows(&mut gate);
    cook_rows(&mut gate, &args[1]);
    variable_rows(&mut gate);

    if gate.red != 0 {
        println!(
            "rust alloc gate FAILED: {} of {} rows allocated past their ceiling",
            gate.red, gate.rows
        );
        exit(1);
    }
    println!(
        "rust alloc gate passed: {} rows, every gated row 0 allocation calls",
        gate.rows
    );
}
