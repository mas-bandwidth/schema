// THE PLAN LANE OF THE PLAN-READ ARM (BENCH-STANDARD.md §1.10) — the Rust leg.
//
// Compiled ONLY into `table-rust-plan` (feature `lineage`), the binary
// `bench/paired -mode build` links against the lineage-aware generation of the
// paired unit: bench/corpus/Bench.schema + bench/corpus/FixedTable.schema,
// UNCHANGED, generated beside bench/tables/rust/lineage/schema.lock, whose
// lineage for FixedTable is the VOLD layout at index 0 and this build's own at
// index 1. So `benchpaired` here is the leg's ordinary generated codec with the
// lock's lineage laid down in it, and `benchvold` is the VOLD generation
// (bench/tables/rust/lineage/VOLD_bench_lineage.schema), which this file uses
// for one thing: WRITING the older-layout file, never reading anything timed.
//
// THIS FILE IS NOT SHAPE-BLIND, AND THE LEDGER SAYS SO
// (bench/tables/rust/SHAPE-GATE.allow). The OLD record has to be derived from
// the paired unit's own 64 logical records — the SAME records, decoded once,
// never a second value list — and the reader's answer has to be checked field
// class by field class. Both are a copy between two generated row types that
// differ by exactly the six narrowings of §1.10's table, and a copy names its
// fields. §1.10 puts this producer in the C++ dump (test/tables/
// fixedform_dump.cpp, outside bench/ where naming a field is not a benchmark);
// the card that wrote this file is scoped to the Rust leg's paths, so the
// producer lives here, exempted by count, and the move is named in its report.
// No TIMED loop names a field: the three loops in main.rs are shape-blind.
//
// THE GATE BEFORE THE CLOCK is the versioning law's own (§1.10, "The gate
// before the clock"), all 64 records, in this order:
//
//   1. the lineage the BUILD laid down is the lock's: two entries, the VOLD
//      layout's hash and bytes at index 0, this build's own at index 1, and
//      the committed lock names both hashes;
//   2. the committed `.layout` is the VOLD build's own block, byte for byte,
//      and the committed `.bin` carries it;
//   3. the committed `.bin` is RE-DERIVED here from the 64 records the
//      identity corpus decoded to, byte for byte — ordinary builds only
//      verify it (`--emit-lineage` is the one writer);
//   4. the VOLD codec reads its own file back clean (no counter) and
//      re-saves it byte for byte;
//   5. the NEW reader reads the OLD file: every OLD value landed exactly, the
//      appended field and the widened bound's tail slots hold their DECLARED
//      default, and the counters are §5.4's for these six classes —
//      `widened` once per widened entry per record, nothing else moves;
//   6. a destination POISONED with the identity corpus's values (whose tail
//      slots and appended field are not defaults) reads the OLD file to the
//      same answer: a reused destination leaks nothing, which is what lets
//      the timed loop reuse one.
//
// WHAT THESE GATES CANNOT SEE, measured rather than assumed: a reader that
// SKIPPED the prefill. The generated LOAD builds each record in a per-call
// image that starts zeroed and scatters the whole image into the destination,
// and the defaults this corpus's filled slots declare are all zero — so with
// the prefill's copy deleted from the generated reader (a negative control
// run on hetzner2 for the card that wrote this) every gate above still
// passes. That the prefill is ON the clock is a property of the generated
// code — the hole copy sits inside LOAD's per-record loop in
// fixedtable_fixed.rs, the same loop the identity lane runs with an empty
// hole list — and it is checked by reading that code, not by this gate.

use super::{Ctx, FixedCorpus, NUM_VARIANTS, PLAN_CAPACITY};
use benchpaired::{
    FIXED_TABLE_FIXED_DEFAULTS, FIXED_TABLE_FIXED_HASH, FIXED_TABLE_FIXED_LINEAGE,
    FIXED_TABLE_FIXED_OWN, FixedTableRow, TableFixedEntry, TableFixedReport,
    fixed_table_fixed_load, fixed_table_fixed_save, fixed_table_fixed_scatter,
};
use benchvold as old;

// The lineage corpus, loaded and proved: the file the plan lane reads, its
// bytes per record (layout included, as the identity lane counts its own), and
// the plan lane's own reused destination and storage.
pub(crate) struct LineageCorpus {
    pub(crate) file: Vec<u8>,
    pub(crate) bytes_per_op: f64,
    pub(crate) out: Vec<FixedTableRow>,
    pub(crate) plan: Vec<TableFixedEntry>,
    pub(crate) remap: Vec<u16>,
    pub(crate) report: TableFixedReport,
}

const NAME: &str = "bench_plan_read";

// THE ENUM APPEND, AND THE ONE VALUE §1.10 DID NOT FORESEE. The paired
// corpus's records use the three variants the NEW enum appended (§1.10 names
// only the appended field, the bound's tail and long text as what the OLD
// record cannot hold), and an OLD writer cannot have written an ordinal it
// never declared — the VOLD reader's bounds pass clamps every one (gate 4).
// So an ordinal past the OLD variant count is folded back onto the OLD
// variants, deterministically and never onto None: `((v - 1) mod COUNT) + 1`.
// The fold changes no width and no class; the reader's answer for that slot
// is the OLD value, and the gate's expectation (expected) carries the same
// fold. The gate prints how many ordinals it moved.
fn old_ordinal(v: u8) -> u8 {
    let count = old::MixedWeapon::COUNT.0;
    if v > count { (v - 1) % count + 1 } else { v }
}

// The OLD record of one logical record: every field the OLD generation
// declares, copied; the three it cannot hold (the appended field, the bound's
// tail, text past the OLD capacity) left behind. A value the OLD generation
// cannot represent at all is a refusal by name, never a silent substitute.
fn narrow(new: &FixedTableRow) -> Result<old::FixedTableRow, String> {
    let n = &new.value;
    let mut out = old::FixedTableRow::default();
    let o = &mut out.value;
    o.sequence = n.sequence;
    o.ack_sequence = n.ack_sequence;
    o.ack_bits = n.ack_bits;
    o.session_id = n.session_id;
    o.client_id = n.client_id;
    o.nonce = n.nonce;
    o.world_time = n.world_time;
    o.frame_tick = n.frame_tick;
    o.server_time = n.server_time;
    o.entities_count = n.entities_count;
    for (d, s) in o.entities.iter_mut().zip(n.entities.iter()) {
        d.entity_id = s.entity_id;
        d.pos_x = s.pos_x;
        d.pos_y = s.pos_y;
        d.pos_z = s.pos_z;
        d.yaw = s.yaw;
        d.pitch = s.pitch;
        d.vel_x = s.vel_x;
        d.vel_y = s.vel_y;
        d.vel_z = s.vel_z;
        d.health = s.health;
        d.weapon = old_ordinal(s.weapon);
        d.damage = s.damage;
        d.moving = s.moving;
        d.firing = s.firing;
    }
    // THE WIDENED BOUND: the count and the elements the OLD bound holds.
    let cap = o.stats.len();
    o.stats_count = n.stats_count.min(cap as i32);
    for (d, s) in o.stats.iter_mut().zip(n.stats.iter()) {
        d.stat_id = s.stat_id;
        // THE INTEGER WIDEN, read backward: the OLD storage is narrower, the
        // declared range is the same, so every in-range value fits.
        d.delta = i16::try_from(s.delta)
            .map_err(|_| "a stat delta does not fit the OLD integer width".to_string())?;
    }
    // THE NEW UNION ARM: the OLD union has no third arm.
    if let Some(hit) = n.game_event.hit() {
        o.game_event.set_hit(old::MixedHitEventRow {
            target_id: hit.target_id,
            damage: hit.damage,
            hit_kind: hit.hit_kind,
            crit: hit.crit,
        });
    } else if let Some(chat) = n.game_event.chat() {
        o.game_event.set_chat(old::MixedChatEventRow {
            channel: chat.channel,
            speaker: chat.speaker,
        });
    } else if n.game_event.tag() != 0 {
        return Err("a record selects the union arm the OLD generation does not have".into());
    }
    o.loadout = n.loadout;
    // THE WIDENED TEXT: the used bytes the OLD capacity holds (the buffer
    // carries one byte past the capacity, as every generated text slot does).
    let text_cap = o.player_name.len() - 1;
    let used = (n.player_name_length.max(0) as usize).min(text_cap);
    o.player_name[..used].copy_from_slice(&n.player_name[..used]);
    o.player_name_length = used as i32;
    o.payload = n.payload;
    o.payload_length = n.payload_length;
    o.aim_x = n.aim_x;
    o.aim_y = n.aim_y;
    o.aim_z = n.aim_z;
    o.recoil = n.recoil;
    o.drift = n.drift;
    o.wide_key = old::TableU128(n.wide_key.0);
    o.flux = old::TableI128(n.flux.0);
    o.ping = n.ping;
    o.crc_hint = n.crc_hint;
    o.has_extra = n.has_extra;
    o.extra = n.extra;
    // THE APPENDED FIELD is not carried: the OLD body ends at the field above.
    Ok(out)
}

// What the NEW reader must answer for one OLD record: the logical record as
// it stands, except the three things the OLD record did not carry, which take
// the DECLARED default — read from this build's own defaults image through its
// own scatter, never written down here.
fn expected(
    new: &FixedTableRow,
    defaults: &FixedTableRow,
    old_cap: usize,
    old_text: usize,
) -> FixedTableRow {
    let mut e = *new;
    let v = &mut e.value;
    for entity in v.entities.iter_mut() {
        entity.weapon = old_ordinal(entity.weapon);
    }
    v.stats_count = v.stats_count.min(old_cap as i32);
    v.stats[old_cap..].copy_from_slice(&defaults.value.stats[old_cap..]);
    let used = (v.player_name_length.max(0) as usize).min(old_text);
    v.player_name[used..].fill(0);
    v.player_name_length = used as i32;
    v.idle_ticks = defaults.value.idle_ticks;
    e
}

fn save_one(row: &FixedTableRow, buf: &mut Vec<u8>) -> Option<()> {
    buf.clear();
    buf.resize(benchpaired::fixed_table_fixed_measure(1), 0);
    fixed_table_fixed_save(std::slice::from_ref(row), buf).map(|_| ())
}

// The OLD file of the 64 logical records, written by the VOLD codec.
fn derive_file(values: &[FixedTableRow]) -> Result<Vec<u8>, String> {
    let mut rows = Vec::with_capacity(values.len());
    for v in values {
        rows.push(narrow(v)?);
    }
    let mut file = vec![0u8; old::fixed_table_fixed_measure(rows.len())];
    match old::fixed_table_fixed_save(&rows, &mut file) {
        Some(n) if n == file.len() => Ok(file),
        _ => Err("the VOLD codec refused to save the narrowed records".into()),
    }
}

// `--emit-lineage DIR`: the ONE writer of the committed pair. Run from the
// paired build's checkout; the files then land under version control.
pub(crate) fn emit(ctx: &mut Ctx, fixed: &FixedCorpus, dir: &str) {
    let file = match derive_file(&fixed.values) {
        Ok(f) => f,
        Err(e) => return ctx.fail(NAME, &e),
    };
    for (name, bytes) in [
        ("bench_lineage.bin", &file[..]),
        ("bench_lineage.layout", &old::FIXED_TABLE_FIXED_BLOCK[..]),
    ] {
        let path = format!("{dir}/{name}");
        if std::fs::write(&path, bytes).is_err() {
            return ctx.fail(NAME, &format!("could not write {path}"));
        }
        eprintln!("wrote {path} ({} bytes)", bytes.len());
    }
}

pub(crate) fn load(ctx: &mut Ctx, fixed: &FixedCorpus) -> Option<LineageCorpus> {
    let dir = ctx.args.lineage_dir.clone();
    let file = ctx.read_corpus(&dir, "bench_lineage.bin");
    let layout = ctx.read_corpus(&dir, "bench_lineage.layout");
    let lock = ctx.read_corpus(&dir, "schema.lock");
    let (Some(file), Some(layout), Some(lock)) = (file, layout, lock) else {
        ctx.failed = true;
        return None;
    };

    // gate 1: THE LINEAGE IS THE LOCK'S.
    let lock = String::from_utf8_lossy(&lock);
    let names = |h: u64| lock.contains(&format!("lineage wire=0x{h:016x} "));
    if FIXED_TABLE_FIXED_LINEAGE.len() != 2
        || FIXED_TABLE_FIXED_OWN != 1
        || FIXED_TABLE_FIXED_LINEAGE[1].hash != FIXED_TABLE_FIXED_HASH
        || FIXED_TABLE_FIXED_LINEAGE[0].hash != old::FIXED_TABLE_FIXED_HASH
        || FIXED_TABLE_FIXED_LINEAGE[0].layout != old::FIXED_TABLE_FIXED_BLOCK
        || FIXED_TABLE_FIXED_LINEAGE[0].record != old::FIXED_TABLE_FIXED_RECORD_BYTES
        || !names(old::FIXED_TABLE_FIXED_HASH)
        || !names(FIXED_TABLE_FIXED_HASH)
    {
        ctx.fail(
            NAME,
            "this build's lineage is not the lock's (VOLD at index 0, its own at index 1): regenerate the unit beside bench/tables/rust/lineage/schema.lock",
        );
        return None;
    }

    // gate 2: THE COMMITTED LAYOUT IS THE VOLD BLOCK, and the file carries it.
    let header = old::fixed_table_fixed_measure(0);
    if layout != old::FIXED_TABLE_FIXED_BLOCK
        || file.len() < header
        || !file[..header].ends_with(&layout)
    {
        ctx.fail(NAME, "bench_lineage.layout is not the VOLD build's block, or bench_lineage.bin does not carry it");
        return None;
    }

    // gate 3: THE FILE IS RE-DERIVED FROM THE 64 RECORDS, byte for byte.
    match derive_file(&fixed.values) {
        Ok(derived) if derived == file => {}
        Ok(_) => {
            ctx.fail(NAME, "bench_lineage.bin is not the narrowing of the paired corpus's 64 records (re-pin with --emit-lineage)");
            return None;
        }
        Err(e) => {
            ctx.fail(NAME, &e);
            return None;
        }
    }

    // gate 4: the VOLD codec reads its own file clean and writes it back.
    let mut olds = vec![old::FixedTableRow::default(); NUM_VARIANTS];
    let mut oplan = vec![old::TableFixedEntry::default(); PLAN_CAPACITY];
    let mut oremap = vec![0u16; PLAN_CAPACITY];
    let mut orep = old::TableFixedReport::default();
    let mut twin = vec![0u8; file.len()];
    let read = old::fixed_table_fixed_load(&mut olds, &file, &mut oplan, &mut oremap, &mut orep);
    let clean = read == Some(NUM_VARIANTS)
        && orep.clamped == 0
        && orep.widened == 0
        && orep.unknown == 0
        && orep.kind_mismatch == 0;
    if !clean || old::fixed_table_fixed_save(&olds, &mut twin) != Some(file.len()) || twin != file {
        ctx.fail(
            NAME,
            &format!(
                "the VOLD codec does not read its own file back clean (read {read:?}, clamped={}, widened={}, unknown={}, kind_mismatch={}; a clamp is a value the OLD generation cannot hold)",
                orep.clamped, orep.widened, orep.unknown, orep.kind_mismatch
            ),
        );
        return None;
    }

    // gate 5: NEW READS OLD — every value, every default, every counter.
    let mut defaults = FixedTableRow::default();
    let mut drep = TableFixedReport::default();
    fixed_table_fixed_scatter(&FIXED_TABLE_FIXED_DEFAULTS, &mut defaults, &mut drep);
    let old_cap = olds[0].value.stats.len();
    let old_text = olds[0].value.player_name.len() - 1;
    let new_cap = defaults.value.stats.len();
    let mut out = vec![FixedTableRow::default(); NUM_VARIANTS];
    let mut plan = vec![TableFixedEntry::default(); PLAN_CAPACITY];
    let mut remap = vec![0u16; PLAN_CAPACITY];
    let mut report = TableFixedReport::default();
    if fixed_table_fixed_load(&mut out, &file, &mut plan, &mut remap, &mut report)
        != Some(NUM_VARIANTS)
    {
        ctx.fail(NAME, "the NEW reader refused the OLD file");
        return None;
    }
    // §5.4: `widened` once per entry per record, and a widened run is never
    // folded, so the integer widen inside the bound is min(their_n, my_n)
    // entries per record. Every other class here is an append the reader
    // knows, and an append is not an event.
    let widened = (NUM_VARIANTS * old_cap.min(new_cap)) as u32;
    eprintln!(
        "{NAME} gate: counters widened={} (expected {widened} = {NUM_VARIANTS} records x min({old_cap}, {new_cap})), clamped={}, unknown={}, kind_mismatch={}",
        report.widened, report.clamped, report.unknown, report.kind_mismatch
    );
    if report.widened != widened
        || report.clamped != 0
        || report.unknown != 0
        || report.kind_mismatch != 0
        || report.malformed
        || report.refused
    {
        ctx.fail(
            NAME,
            "the NEW reader's counters on the OLD file are not §5.4's for these six classes",
        );
        return None;
    }
    let (mut want, mut got) = (Vec::new(), Vec::new());
    for (k, (read, logical)) in out.iter().zip(fixed.values.iter()).enumerate() {
        let e = expected(logical, &defaults, old_cap, old_text);
        if read.value.idle_ticks != defaults.value.idle_ticks
            || read.value.stats[old_cap..]
                .iter()
                .zip(defaults.value.stats[old_cap..].iter())
                .any(|(a, b)| a.stat_id != b.stat_id || a.delta != b.delta)
        {
            ctx.fail(NAME, &format!("record {k}: the reader's appended field or the bound's tail does not hold its declared default"));
            return None;
        }
        if save_one(&e, &mut want).is_none() || save_one(read, &mut got).is_none() || want != got {
            ctx.fail(
                NAME,
                &format!("record {k}: an OLD value did not land exactly"),
            );
            return None;
        }
    }

    // gate 6: a POISONED destination — the identity corpus's own values,
    // whose tail slots and appended field are not defaults — reads the OLD
    // file to the same answer, so the reused destination leaks nothing.
    let mut poisoned = fixed.values.clone();
    let mut prep = TableFixedReport::default();
    if fixed_table_fixed_load(&mut poisoned, &file, &mut plan, &mut remap, &mut prep)
        != Some(NUM_VARIANTS)
    {
        ctx.fail(NAME, "the reused-destination read of the OLD file refused");
        return None;
    }
    for k in 0..NUM_VARIANTS {
        if save_one(&poisoned[k], &mut got).is_none()
            || save_one(&out[k], &mut want).is_none()
            || want != got
        {
            ctx.fail(
                NAME,
                &format!("record {k}: a reused destination kept a previous record's bytes"),
            );
            return None;
        }
    }
    let folded: usize = fixed
        .values
        .iter()
        .map(|r| {
            r.value
                .entities
                .iter()
                .filter(|e| old_ordinal(e.weapon) != e.weapon)
                .count()
        })
        .sum();
    eprintln!(
        "{NAME} gate: {folded} enum ordinals past the OLD variant count folded onto OLD variants by the producer (old_ordinal)"
    );
    eprintln!(
        "{NAME} gate: OK — {NUM_VARIANTS} OLD records ({} B/record on the wire, lineage index 0 of 2) land exactly; NEW record {} B",
        old::FIXED_TABLE_FIXED_RECORD_BYTES,
        benchpaired::FIXED_TABLE_FIXED_RECORD_BYTES
    );
    let bytes_per_op = file.len() as f64 / NUM_VARIANTS as f64;
    Some(LineageCorpus {
        file,
        bytes_per_op,
        out,
        plan,
        remap,
        report,
    })
}
