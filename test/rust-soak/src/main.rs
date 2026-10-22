// THE RUST TABLES SOAK (docs/PORTING.md I9, issue #416; docs/SPEC-TABLES.md,
// "What allocates, and what never does"). The twin of test/c-tables/soak_main.c.
//
// Load, the reads, Measure and Save on the root of every case below, in a loop,
// for as long as the caller asks, with this process's own allocator COUNTED.
//
// THE NUMBER THAT MATTERS IS THE CALL COUNT. The fixed-form codec's contract is
// that the caller owns every buffer — the values, the plan, the remap, the wire
// and the save buffer — and the generated load, measure and save allocate
// NOTHING. A live-byte sample cannot say that: a matched alloc/free pair inside
// the loop leaves it where it was before it is ever read. So a #[global_allocator]
// counts every call (alloc, alloc_zeroed, realloc, dealloc), the count is taken
// at EVERY SAMPLE, and the soak fails the moment it moves after the warmup pass.
// The live-heap byte count and the resident set are reported beside it as the
// weaker instruments: the first is gated (it sees retention exactly), the second
// is reported only, because a page the kernel maps in is not an allocation.
//
// THE CORPUS IS THE C SOAK'S UNITS, ON THIS PORT'S WIRE. The C soak reads the
// tolerant wire (form 1) under testdata/wire/tables; the Rust port's table
// surface for these units is the FIXED FORM (form byte 3, docs/SPEC-TABLES.md
// §3.4), and a fixed reader refuses form 1 by name (PreviousForm). So the files
// are the fixed form's: the C++ reference's own dump where it writes the root
// (build/fixedform-corpus: keyed.bin, pack.bin, p1.bin, p3.bin), and a file this
// binary writes from populated rows before the clock where it does not
// (RootConfig, ProfileConfig, LoadoutConfig, ArchiveConfig, both Cfg
// generations). WideBlob is not here: its record is 280012 bytes, past the
// fixed form's 65536-byte ceiling, so the compiler emits no fixed codec for it.
// The four EVOLUTION SEAMS the C soak crosses are here as what this form does
// with them: a fixed reader takes the header's hash as given and, with no lock
// lineage, refuses a stranger's layout BY NAME — that refusal is a read path
// too, and it must allocate nothing either.
//
// It is a FUNCTIONAL soak: it runs for real seconds, so it is a make target
// (`make tables-rust-soak SOAK_SECONDS=<n>`) and never a unit test.
//
//   rust-soak <seconds> <fixedform-corpus-dir>

use std::alloc::{GlobalAlloc, Layout, System};
use std::fmt::Debug;
use std::hint::black_box;
use std::process::exit;
use std::sync::atomic::{AtomicBool, AtomicI64, AtomicU64, Ordering::Relaxed};
use std::time::Instant;

// ---- THE ALLOCATOR CALL COUNTER ----
//
// Every call is forwarded to the system allocator. LIVE is the net byte count
// and is kept always (it is the drift instrument); CALLS moves only while
// COUNTING is set, which is the whole measured loop including the sampling code,
// so a clock read or a sample that allocated would be caught too.
struct Counting;

static COUNTING: AtomicBool = AtomicBool::new(false);
static CALLS: AtomicU64 = AtomicU64::new(0);
static LIVE: AtomicI64 = AtomicI64::new(0);

#[inline(always)]
fn count_call() {
    if COUNTING.load(Relaxed) {
        CALLS.fetch_add(1, Relaxed);
    }
}

unsafe impl GlobalAlloc for Counting {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        count_call();
        let p = unsafe { System.alloc(layout) };
        if !p.is_null() {
            LIVE.fetch_add(layout.size() as i64, Relaxed);
        }
        p
    }
    unsafe fn alloc_zeroed(&self, layout: Layout) -> *mut u8 {
        count_call();
        let p = unsafe { System.alloc_zeroed(layout) };
        if !p.is_null() {
            LIVE.fetch_add(layout.size() as i64, Relaxed);
        }
        p
    }
    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        count_call();
        unsafe { System.dealloc(ptr, layout) };
        LIVE.fetch_sub(layout.size() as i64, Relaxed);
    }
    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        count_call();
        let p = unsafe { System.realloc(ptr, layout, new_size) };
        if !p.is_null() {
            LIVE.fetch_add(new_size as i64 - layout.size() as i64, Relaxed);
        }
        p
    }
}

#[global_allocator]
static GLOBAL: Counting = Counting;

/// The resident set in kB, from /proc on Linux; None where there is no /proc.
/// Called only with COUNTING off: reading the file allocates.
fn resident_kb() -> Option<i64> {
    let status = std::fs::read_to_string("/proc/self/status").ok()?;
    let line = status.lines().find(|l| l.starts_with("VmRSS:"))?;
    line.split_whitespace().nth(1)?.parse().ok()
}

// ---- ONE CASE: a root, a file, and the caller-owned buffers ----

type LoadFn<R, E, Rep> = fn(&mut [R], &[u8], &mut [E], &mut [u16], &mut Rep) -> Option<usize>;
type SaveFn<R> = fn(&[R], &mut [u8]) -> Option<usize>;
type MeasureFn = fn(usize) -> usize;
type ReadFn<R> = fn(&[R]) -> u64;

/// What the setup pass saw, pinned: every pass of the loop must see it again.
enum Expect<Rep> {
    /// the file loads `records` records with this report, and saves back to
    /// `measure(records)` bytes — byte for byte the file when `exact`
    Loaded {
        records: usize,
        report: Rep,
        exact: bool,
    },
    /// the file is refused, with this report
    Refused(Rep),
}

/// The caller-owned storage the contract names, all allocated before the clock.
const RECORDS: usize = 8;
const PLAN: usize = 4096;

struct Fixed<R, E, Rep> {
    label: &'static str,
    wire: Vec<u8>,
    values: Vec<R>,
    plan: Vec<E>,
    remap: Vec<u16>,
    out: Vec<u8>,
    load: LoadFn<R, E, Rep>,
    save: SaveFn<R>,
    measure: MeasureFn,
    read: ReadFn<R>,
    expect: Expect<Rep>,
    checksum: u64,
}

trait Case {
    fn label(&self) -> &'static str;
    fn describe(&self) -> String;
    /// One iteration: Load, the reads, Measure, Save. An Err is a static
    /// string so the failure path does not allocate either.
    fn pass(&mut self) -> Result<(), &'static str>;
    fn checksum(&self) -> u64;
}

#[derive(Clone, Copy, PartialEq)]
enum Kind {
    /// the file must load clean and re-save to its own bytes
    Exact,
    /// a file written under ANOTHER layout: what the reader does is pinned
    Seam,
}

impl<R: Copy + Default, E: Copy + Default, Rep: Copy + Default + PartialEq + Debug>
    Fixed<R, E, Rep>
{
    #[allow(clippy::too_many_arguments)]
    fn case(
        label: &'static str,
        kind: Kind,
        wire: Vec<u8>,
        load: LoadFn<R, E, Rep>,
        save: SaveFn<R>,
        measure: MeasureFn,
        read: ReadFn<R>,
    ) -> Result<Box<dyn Case>, String>
    where
        R: 'static,
        E: 'static,
        Rep: 'static,
    {
        let mut c = Fixed {
            label,
            wire,
            values: vec![R::default(); RECORDS],
            plan: vec![E::default(); PLAN],
            remap: vec![0u16; PLAN],
            out: Vec::new(),
            load,
            save,
            measure,
            read,
            expect: Expect::Refused(Rep::default()),
            checksum: 0,
        };
        let mut report = Rep::default();
        let n = (c.load)(
            &mut c.values,
            &c.wire,
            &mut c.plan,
            &mut c.remap,
            &mut report,
        );
        match n {
            Some(records) => {
                c.out = vec![0u8; (c.measure)(records)];
                let wrote = (c.save)(&c.values[..records], &mut c.out);
                if wrote != Some(c.out.len()) {
                    return Err(format!("{label}: save did not fill what measure says"));
                }
                let exact = c.out == c.wire;
                if kind == Kind::Exact {
                    // THE GOLDEN GATE, before the clock: an exact case must load
                    // clean and come back BYTE FOR BYTE, or there is nothing
                    // honest to soak.
                    if report != Rep::default() {
                        return Err(format!("{label}: the load moved a counter: {report:?}"));
                    }
                    if !exact {
                        return Err(format!(
                            "{label}: does not re-save to its own bytes; refusing to soak a codec that does not reproduce its file"
                        ));
                    }
                }
                c.expect = Expect::Loaded {
                    records,
                    report,
                    exact,
                };
            }
            None => {
                if kind == Kind::Exact {
                    return Err(format!("{label}: the file does not load: {report:?}"));
                }
                c.expect = Expect::Refused(report);
            }
        }
        Ok(Box::new(c))
    }
}

impl<R: Copy, E: Copy, Rep: Copy + Default + PartialEq + Debug> Case for Fixed<R, E, Rep> {
    fn label(&self) -> &'static str {
        self.label
    }

    fn describe(&self) -> String {
        match &self.expect {
            Expect::Loaded {
                records,
                report,
                exact,
            } => format!(
                "{}: {} bytes, {} record(s), {}{}",
                self.label,
                self.wire.len(),
                records,
                if *exact {
                    "re-saves byte for byte"
                } else {
                    "re-saves in this reader's own layout"
                },
                if *report == Rep::default() {
                    String::new()
                } else {
                    format!(", report {report:?}")
                }
            ),
            Expect::Refused(report) => format!(
                "{}: {} bytes, refused by name: {report:?}",
                self.label,
                self.wire.len()
            ),
        }
    }

    fn pass(&mut self) -> Result<(), &'static str> {
        let mut report = Rep::default();
        // LOAD
        let n = (self.load)(
            &mut self.values,
            &self.wire,
            &mut self.plan,
            &mut self.remap,
            &mut report,
        );
        match &self.expect {
            Expect::Loaded {
                records,
                report: pinned,
                exact,
            } => {
                let records = *records;
                if n != Some(records) || report != *pinned {
                    return Err("a load answered differently from the setup pass");
                }
                // THE READS: the generated rows and their accessors
                self.checksum = self
                    .checksum
                    .wrapping_add((self.read)(black_box(&self.values[..records])));
                // MEASURE
                let need = (self.measure)(records);
                if need != self.out.len() {
                    return Err("measure answered differently from the setup pass");
                }
                // SAVE
                if (self.save)(&self.values[..records], &mut self.out) != Some(need) {
                    return Err("a save did not fill what measure says");
                }
                if *exact && self.out != self.wire {
                    return Err("an exact case re-saved to different bytes");
                }
            }
            Expect::Refused(pinned) => {
                if n.is_some() || report != *pinned {
                    return Err("a refusal answered differently from the setup pass");
                }
            }
        }
        Ok(())
    }

    fn checksum(&self) -> u64 {
        self.checksum
    }
}

// ---- THE FILES THIS BINARY WRITES: populated rows, saved before the clock ----

fn text<const N: usize>(s: &str) -> ([u8; N], i32) {
    let mut b = [0u8; N];
    b[..s.len()].copy_from_slice(s.as_bytes());
    (b, s.len() as i32)
}

fn written<R>(values: &[R], save: SaveFn<R>, measure: MeasureFn) -> Vec<u8> {
    let mut out = vec![0u8; measure(values.len())];
    if save(values, &mut out) != Some(out.len()) {
        eprintln!("soak: a populated row did not save");
        exit(1);
    }
    out
}

mod demo {
    use tabledemo::*;

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

    pub fn loadout(i: usize) -> LoadoutConfigRow {
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

    pub fn profile(i: usize) -> ProfileConfigRow {
        let (name, name_length) =
            super::text::<33>(["pilot-one", "pilot-two", "pilot-three"][i % 3]);
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
        let (version_note, version_note_length) = super::text::<17>("soak root");
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

    pub fn archive(i: usize) -> ArchiveConfigRow {
        ArchiveConfigRow {
            root: root(i),
            count: 7,
        }
    }

    fn weapon_read(w: &WeaponConfigRow) -> u64 {
        let effect = match (w.effect.buff(), w.effect.debuff()) {
            (Some(b), _) => b.multiplier.to_bits() as u64,
            (_, Some(d)) => d.amount as u64,
            _ => 0,
        };
        w.damage.to_bits() as u64
            ^ w.speed.to_bits() as u64
            ^ w.penetration as u64
            ^ w.channel as u64
            ^ w.homing as u64
            ^ w.effect.tag() as u64
            ^ effect
    }

    fn loadout_read(l: &LoadoutConfigRow) -> u64 {
        let mut s = l.grade as u64 ^ l.perks ^ weapon_read(&l.primary);
        for g in &l.grades[..l.grades_count.clamp(0, 4) as usize] {
            s = s.wrapping_mul(31).wrapping_add(*g as u64);
        }
        for w in &l.backups {
            s ^= weapon_read(w);
        }
        for a in &l.attachments[..l.attachments_count.clamp(0, 8) as usize] {
            s = s.wrapping_add(a.slot as u64 ^ a.power.to_bits() as u64);
        }
        s
    }

    fn profile_read(p: &ProfileConfigRow) -> u64 {
        let mut s = p.name_length as u64
            ^ p.icon_length as u64
            ^ p.experience as u64
            ^ p.timestamp as u64
            ^ p.epoch;
        s ^= p.precision.to_bits()
            ^ p.port as u64
            ^ p.heading as u64
            ^ p.tilt as u64
            ^ p.badge as u64;
        for r in &p.ratings {
            s = s.wrapping_add(r.to_bits() as u64);
        }
        if p.has_loadout {
            s ^= loadout_read(&p.loadout);
        }
        s
    }

    fn root_read(r: &RootConfigRow) -> u64 {
        let mut s = r.version_note_length as u64;
        for w in &r.weapons[..r.weapons_count.clamp(0, 8) as usize] {
            s = s.wrapping_add(weapon_read(w));
        }
        for p in &r.profiles[..r.profiles_count.clamp(0, 4) as usize] {
            s = s.wrapping_add(profile_read(p));
        }
        s
    }

    pub fn roots(v: &[RootConfigRow]) -> u64 {
        v.iter().map(root_read).fold(0, u64::wrapping_add)
    }
    pub fn profiles(v: &[ProfileConfigRow]) -> u64 {
        v.iter().map(profile_read).fold(0, u64::wrapping_add)
    }
    pub fn loadouts(v: &[LoadoutConfigRow]) -> u64 {
        v.iter().map(loadout_read).fold(0, u64::wrapping_add)
    }
    pub fn archives(v: &[ArchiveConfigRow]) -> u64 {
        v.iter()
            .map(|a| root_read(&a.root) ^ a.count as u64)
            .fold(0, u64::wrapping_add)
    }
    pub fn keyed(v: &[KeyedConfigRow]) -> u64 {
        let mut s = 0u64;
        for k in v {
            for t in &k.teams {
                s = s.wrapping_add(t.spawn_count as u64 ^ t.banner_length as u64);
            }
            for p in &k.scores.per_team {
                s = s.wrapping_add(*p as u64);
            }
            for h in &k.hulls {
                s ^= h.health.to_bits() as u64;
                for t in &h.turrets {
                    s = s.wrapping_add(
                        t.cooldown.to_bits() as u64
                            ^ t.gunner_present as u64
                            ^ t.gunner.reaction.to_bits() as u64,
                    );
                }
            }
        }
        s
    }
    pub fn packs(v: &[PackConfigRow]) -> u64 {
        v.iter()
            .map(|p| p.version as u64 ^ p.global.tick_rate as u64 ^ p.global.difficulty as u64)
            .fold(0, u64::wrapping_add)
    }
}

mod v1 {
    use tblv1::*;

    pub fn cfg(i: usize) -> CfgRow {
        let (name, name_length) = super::text::<33>("soak cfg v1");
        let mut c = CfgRow {
            a: 500 + i as i32,
            b: 2.25,
            mode: 1,
            name,
            name_length,
            inner: InnerRow { factor: 3.5 },
            items: [1, 2, 3, 255, 0, 0, 0, 0],
            items_count: 4,
            grade: 2,
            grades: [1, 2, 0, 0],
            grades_count: 2,
            podium: [1, 2, 1],
            slots: [6, 5, 4, 3, 0, 0],
            slots_count: 4,
            tally: [1, 2, 3],
            tokens: [10, 20, 30, 40],
            ranks: [1, 2, 1, 2],
            ledger: [7, 8, 9],
            extra: InnerRow { factor: 4.0 },
            extra_present: true,
            tier: 12,
            tier_present: true,
            mark: 2,
            mark_present: true,
            ..Default::default()
        };
        for (j, cell) in c.bank.iter_mut().enumerate() {
            let (label, label_length) = super::text::<9>("cell");
            *cell = CellRow {
                power: 100 * j as i32,
                label,
                label_length,
            };
        }
        c.effect.set_boost(BoostRow { power: 9 });
        c
    }

    pub fn cfgs(v: &[CfgRow]) -> u64 {
        let mut s = 0u64;
        for c in v {
            s = s.wrapping_add(
                c.a as u64
                    ^ c.b.to_bits() as u64
                    ^ c.mode as u64
                    ^ c.name_length as u64
                    ^ c.inner.factor.to_bits() as u64,
            );
            for x in &c.items[..c.items_count.clamp(0, 8) as usize] {
                s = s.wrapping_mul(31).wrapping_add(*x as u64);
            }
            for b in &c.bank {
                s ^= b.power as u64 ^ b.label_length as u64;
            }
            s ^= c.effect.tag() as u64 ^ c.effect.boost().map_or(0, |b| b.power as u64);
            if c.extra_present {
                s ^= c.extra.factor.to_bits() as u64;
            }
            if c.tier_present {
                s ^= c.tier as u64;
            }
        }
        s
    }
}

mod v2 {
    use tblv2::*;

    pub fn cfg(i: usize) -> CfgRow {
        let (title, title_length) = super::text::<33>("soak cfg v2");
        let mut c = CfgRow {
            a: 1.5 + i as f32,
            c: true,
            mode: 1,
            title,
            title_length,
            inner: InnerRow {
                factor: 3.5,
                gain: 0.5,
            },
            items: [1, 2, 3, 4, 5, 0, 0, 0],
            items_count: 5,
            grade: 3,
            grades: [1, 2, 3, 0],
            grades_count: 3,
            podium: [3, 2, 1],
            slots: [1, 2, 0],
            slots_count: 2,
            tally: [1, 2, 3, 4],
            tokens: [10, 20, 30, 40, 50],
            ranks: [1, 2, 3, 1, 2],
            ledger: [7, 8, 9],
            extra: InnerRow {
                factor: 4.0,
                gain: 1.0,
            },
            extra_present: true,
            tier: 12,
            tier_present: true,
            mark: 2,
            mark_present: true,
            ..Default::default()
        };
        for (j, cell) in c.bank.iter_mut().enumerate() {
            let (label, label_length) = super::text::<9>("cell");
            *cell = CellRow {
                power: 100 * j as i32,
                label,
                label_length,
            };
        }
        c.effect.set_hex(HexRow { level: 4 });
        c
    }

    pub fn cfgs(v: &[CfgRow]) -> u64 {
        let mut s = 0u64;
        for c in v {
            s = s.wrapping_add(
                c.a.to_bits() as u64
                    ^ c.c as u64
                    ^ c.mode as u64
                    ^ c.title_length as u64
                    ^ c.inner.gain.to_bits() as u64,
            );
            for x in &c.items[..c.items_count.clamp(0, 8) as usize] {
                s = s.wrapping_mul(31).wrapping_add(*x as u64);
            }
            for b in &c.bank {
                s ^= b.power as u64 ^ b.label_length as u64;
            }
            s ^= c.effect.tag() as u64 ^ c.effect.hex().map_or(0, |h| h.level as u64);
            if c.mark_present {
                s ^= c.mark as u64;
            }
        }
        s
    }
}

fn chains_p1(v: &[tblp1::ChainRow]) -> u64 {
    v.iter()
        .map(|c| c.name_length as u64 ^ c.link.value as u64 ^ c.link.tag_length as u64)
        .fold(0, u64::wrapping_add)
}

fn chains_p3(v: &[tblp3::ChainRow]) -> u64 {
    v.iter()
        .map(|c| {
            c.name_length as u64
                ^ if c.link_present {
                    c.link.value as u64 ^ c.link.tag_length as u64
                } else {
                    0
                }
        })
        .fold(0, u64::wrapping_add)
}

fn slurp(dir: &str, name: &str) -> Vec<u8> {
    match std::fs::read(format!("{dir}/{name}")) {
        Ok(b) => b,
        Err(e) => {
            eprintln!("soak: cannot read {dir}/{name}: {e}");
            exit(1);
        }
    }
}

fn cases(corpus: &str) -> Result<Vec<Box<dyn Case>>, String> {
    use tabledemo as d;
    // the files this binary writes, from rows populated across every field kind
    let root = written(
        &[demo::root(0), demo::root(3)],
        d::root_config_fixed_save,
        d::root_config_fixed_measure,
    );
    let profile = written(
        &[demo::profile(1), demo::profile(2)],
        d::profile_config_fixed_save,
        d::profile_config_fixed_measure,
    );
    let loadout = written(
        &[demo::loadout(4), demo::loadout(5)],
        d::loadout_config_fixed_save,
        d::loadout_config_fixed_measure,
    );
    let archive = written(
        &[demo::archive(6)],
        d::archive_config_fixed_save,
        d::archive_config_fixed_measure,
    );
    let cfg1 = written(
        &[v1::cfg(0), v1::cfg(1)],
        tblv1::cfg_fixed_save,
        tblv1::cfg_fixed_measure,
    );
    let cfg2 = written(
        &[v2::cfg(0), v2::cfg(1)],
        tblv2::cfg_fixed_save,
        tblv2::cfg_fixed_measure,
    );
    // the C++ reference's own files (test/tables/fixedform_dump.cpp)
    let keyed = slurp(corpus, "keyed.bin");
    let pack = slurp(corpus, "pack.bin");
    let p1 = slurp(corpus, "p1.bin");
    let p3 = slurp(corpus, "p3.bin");

    Ok(vec![
        Fixed::case(
            "tabledemo.RootConfig (written here)",
            Kind::Exact,
            root,
            d::root_config_fixed_load,
            d::root_config_fixed_save,
            d::root_config_fixed_measure,
            demo::roots,
        )?,
        Fixed::case(
            "tabledemo.ProfileConfig (written here)",
            Kind::Exact,
            profile,
            d::profile_config_fixed_load,
            d::profile_config_fixed_save,
            d::profile_config_fixed_measure,
            demo::profiles,
        )?,
        Fixed::case(
            "tabledemo.LoadoutConfig (written here)",
            Kind::Exact,
            loadout,
            d::loadout_config_fixed_load,
            d::loadout_config_fixed_save,
            d::loadout_config_fixed_measure,
            demo::loadouts,
        )?,
        Fixed::case(
            "tabledemo.ArchiveConfig (written here)",
            Kind::Exact,
            archive,
            d::archive_config_fixed_load,
            d::archive_config_fixed_save,
            d::archive_config_fixed_measure,
            demo::archives,
        )?,
        Fixed::case(
            "tabledemo.KeyedConfig (keyed.bin, C++ reference)",
            Kind::Exact,
            keyed,
            d::keyed_config_fixed_load,
            d::keyed_config_fixed_save,
            d::keyed_config_fixed_measure,
            demo::keyed,
        )?,
        Fixed::case(
            "tabledemo.PackConfig (pack.bin, C++ reference)",
            Kind::Exact,
            pack,
            d::pack_config_fixed_load,
            d::pack_config_fixed_save,
            d::pack_config_fixed_measure,
            demo::packs,
        )?,
        Fixed::case(
            "tblv1.Cfg (written here)",
            Kind::Exact,
            cfg1.clone(),
            tblv1::cfg_fixed_load,
            tblv1::cfg_fixed_save,
            tblv1::cfg_fixed_measure,
            v1::cfgs,
        )?,
        Fixed::case(
            "tblv2.Cfg (written here)",
            Kind::Exact,
            cfg2.clone(),
            tblv2::cfg_fixed_load,
            tblv2::cfg_fixed_save,
            tblv2::cfg_fixed_measure,
            v2::cfgs,
        )?,
        Fixed::case(
            "tblp1.Chain (p1.bin, C++ reference)",
            Kind::Exact,
            p1.clone(),
            tblp1::chain_fixed_load,
            tblp1::chain_fixed_save,
            tblp1::chain_fixed_measure,
            chains_p1,
        )?,
        Fixed::case(
            "tblp3.Chain (p3.bin, C++ reference)",
            Kind::Exact,
            p3.clone(),
            tblp3::chain_fixed_load,
            tblp3::chain_fixed_save,
            tblp3::chain_fixed_measure,
            chains_p3,
        )?,
        // THE EVOLUTION SEAMS: bytes read by a type that did not write them
        Fixed::case(
            "seam: tblv2.Cfg reads tblv1's file",
            Kind::Seam,
            cfg1,
            tblv2::cfg_fixed_load,
            tblv2::cfg_fixed_save,
            tblv2::cfg_fixed_measure,
            v2::cfgs,
        )?,
        Fixed::case(
            "seam: tblv1.Cfg reads tblv2's file",
            Kind::Seam,
            cfg2,
            tblv1::cfg_fixed_load,
            tblv1::cfg_fixed_save,
            tblv1::cfg_fixed_measure,
            v1::cfgs,
        )?,
        Fixed::case(
            "seam: tblp3.Chain reads p1.bin",
            Kind::Seam,
            p1,
            tblp3::chain_fixed_load,
            tblp3::chain_fixed_save,
            tblp3::chain_fixed_measure,
            chains_p3,
        )?,
        Fixed::case(
            "seam: tblp1.Chain reads p3.bin",
            Kind::Seam,
            p3,
            tblp1::chain_fixed_load,
            tblp1::chain_fixed_save,
            tblp1::chain_fixed_measure,
            chains_p1,
        )?,
    ])
}

/// THE PLANTED PAIR (the negative control's cfg flag, never generated code):
/// one allocation and its free, per case on every iteration, as the C control
/// plants its malloc/free pair. black_box on both sides is what
/// stops the optimiser deleting a dead allocation outright — a control the
/// compiler removed proves nothing.
#[inline(never)]
fn planted_pair() {
    #[cfg(feature = "negative-control")]
    {
        let b = black_box(Box::new(black_box(1u8)));
        drop(b);
    }
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let seconds: f64 = args.get(1).and_then(|s| s.parse().ok()).unwrap_or(20.0);
    let corpus = args
        .get(2)
        .map(String::as_str)
        .unwrap_or("build/fixedform-corpus");

    let mut cases = match cases(corpus) {
        Ok(c) => c,
        Err(e) => {
            eprintln!("soak: {e}");
            exit(1);
        }
    };
    if cfg!(feature = "negative-control") {
        println!(
            "rust tables soak: SABOTAGED build — one matched alloc/free pair planted per iteration (feature negative-control)"
        );
    }
    for c in &cases {
        println!("rust tables soak: case {}", c.describe());
    }
    println!(
        "rust tables soak: counting allocator calls over the measured loop, sampled every 1024 iterations"
    );

    let before = LIVE.load(Relaxed);
    let start = Instant::now();
    let mut iterations: u64 = 0;
    let mut settled_live: i64 = 0;
    let mut settled_calls: u64 = 0;
    let mut warmup_calls: u64 = 0;
    let mut settled_rss: Option<i64> = None;
    let mut samples: u64 = 0;
    let mut moved: Option<(u64, u64)> = None; // (iteration, count at that sample)

    COUNTING.store(true, Relaxed);
    loop {
        for c in cases.iter_mut() {
            planted_pair();
            if let Err(e) = c.pass() {
                COUNTING.store(false, Relaxed);
                eprintln!("SOAK FAILED: {}: {e}", c.label());
                exit(1);
            }
        }
        iterations += 1;
        if iterations == 1 {
            // THE WARMUP is one pass: whatever a first call initialises is
            // charged to it, and the count it ends on is the one every sample
            // after it must equal.
            COUNTING.store(false, Relaxed);
            warmup_calls = CALLS.load(Relaxed);
            settled_live = LIVE.load(Relaxed);
            settled_rss = resident_kb();
            settled_calls = CALLS.load(Relaxed);
            COUNTING.store(true, Relaxed);
            continue;
        }
        // THE SAMPLE, every 1024 passes: the clock read is the cost being
        // amortised, and the count is taken at every one of them. A zero-second
        // run stops at two passes, the fewest that compares a sample with the
        // warmup at all.
        if seconds <= 0.0 || iterations & 0x3ff == 0 {
            samples += 1;
            let now = CALLS.load(Relaxed);
            if now != settled_calls {
                moved = Some((iterations, now));
                break;
            }
            if seconds <= 0.0 || start.elapsed().as_secs_f64() >= seconds {
                break;
            }
        }
    }
    COUNTING.store(false, Relaxed);
    let calls = CALLS.load(Relaxed);
    let after = LIVE.load(Relaxed);
    let final_rss = resident_kb();

    let checksum = cases.iter().fold(0u64, |s, c| s ^ c.checksum());
    println!(
        "rust tables soak: {} iterations over {} cases in {:.0} s (read checksum {:016x})",
        iterations,
        cases.len(),
        start.elapsed().as_secs_f64(),
        checksum
    );
    println!(
        "rust tables soak: live allocation {before} bytes before, {settled_live} after the first iteration, {after} at the end"
    );
    match (settled_rss, final_rss) {
        (Some(a), Some(b)) => println!(
            "rust tables soak: resident memory {a} kB after the first iteration, {b} kB at the end, drift {:+} kB (the weaker instrument: reported, not gated)",
            b - a
        ),
        _ => println!(
            "rust tables soak: no /proc on this platform, so resident memory went unmeasured"
        ),
    }
    println!(
        "rust tables soak: {} allocator calls in the warmup pass, {} allocator calls after it over {} samples and {} iterations",
        warmup_calls,
        calls - settled_calls,
        samples,
        iterations - 1
    );
    if after != settled_live {
        eprintln!(
            "SOAK FAILED: live allocation moved from {settled_live} to {after} bytes over {iterations} iterations — the fixed-form load, measure and save allocate nothing, so this is a leak"
        );
        exit(1);
    }
    if let Some((at, now)) = moved {
        eprintln!(
            "SOAK FAILED: the allocator call count moved from {settled_calls} to {now} by iteration {at} — the fixed-form load, measure and save allocate NOTHING, and the live-byte sample cannot see a matched alloc/free pair because the number returns to where it was"
        );
        exit(1);
    }
    println!(
        "rust tables soak: ZERO allocator calls after the warmup and allocations flat — the fixed-form load, the reads, measure and save allocate nothing"
    );
}
