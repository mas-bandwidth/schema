// THE FORGERY FUZZER, RUST SIDE (docs/SPEC-TABLES.md §7, §19.2, §19.5) — the
// twin of test/tables/block_fuzz_main.cpp and test/cs-block's Fuzz.cs, over the
// same seed blocks, plus a COOK half over test/cookgen's fixtures.
//
// The C++ leg writes a valid block per count vector into build/block-fuzz/ with
// its generated builder, because this side has only the READ half of the form:
// a consumer never lays a block out, so the seeds must come from a producer.
// This leg mutates those bytes with the same mutators and holds Open to the
// same oracle:
//
//   REFUSE, or OPEN and be WHOLE. A mutant either makes Open return None and
//   point at nothing, or it opens — and then every row of every array is
//   addressable inside the extent the caller passed, every pitch is this
//   build's own, every count is inside its declared maximum, and the walk reads
//   every byte of every row.
//
// The oracle re-derives its bounds FROM THE DESCRIPTORS and from the triples in
// the instance, never from Open's own arithmetic — which is what lets it
// disagree with Open, and what makes a negative control able to go red at all.
//
// WHERE THE SANITIZER WOULD BE. Every mutant's region is allocated at EXACTLY
// the bytes the caller claims, so a read one byte past the extent is a read
// past a real allocation. Under `cargo miri` that is a hard error with a
// backtrace, which is this leg's address sanitizer; under the ordinary build
// the oracle's own bounds checks are what stand, and both legs run
// (`make tables-rust-fuzz`). Miri is perhaps two orders of magnitude slower, so
// it runs a reduced budget — the ENUMERATED passes, which are what cover the
// boundaries, and a token random budget on top.
//
// SEED, N and ONLY come from the environment, so a failing case re-runs alone.

use std::alloc::{alloc, dealloc, Layout};
use std::env;
use std::fs;
use std::process::exit;

// ---------------------------------------------------------------------------
// the verdict
// ---------------------------------------------------------------------------

struct Site {
    unit: String,
    vector: i64,
    pass: String,
    index: i64,
    description: String,
}

static mut SITE: Option<Site> = None;
static mut RUN_SEED: u64 = 0;
static mut SINK: u64 = 0;

fn site() -> &'static mut Site {
    unsafe {
        let slot = &mut *&raw mut SITE;
        if slot.is_none() {
            *slot = Some(Site {
                unit: String::new(),
                vector: 0,
                pass: String::new(),
                index: 0,
                description: String::new(),
            });
        }
        slot.as_mut().unwrap()
    }
}

// A defect the ORACLE saw: report and STOP. A fuzzer that keeps going after the
// first find reports the same class N times and minimises none of them.
fn defect(what: &str) -> ! {
    let s = site();
    let seed = unsafe { RUN_SEED };
    eprintln!("\nFAILED: {what}");
    eprintln!("  unit      {}", s.unit);
    eprintln!("  vector    {}", s.vector);
    eprintln!("  pass      {}", s.pass);
    eprintln!("  index     {}", s.index);
    eprintln!("  mutation  {}", s.description);
    eprintln!(
        "  re-run    SEED={seed} ONLY={}:{}:{}:{} cargo run --manifest-path test/rust-fuzz/Cargo.toml\n",
        s.unit, s.vector, s.pass, s.index
    );
    exit(1)
}

// ---------------------------------------------------------------------------
// the seeded generator: splitmix64, the C++ leg's own
// ---------------------------------------------------------------------------

#[derive(Clone, Copy)]
struct Rng {
    state: u64,
}

impl Rng {
    fn next(&mut self) -> u64 {
        self.state = self.state.wrapping_add(0x9e37_79b9_7f4a_7c15);
        let mut z = self.state;
        z = (z ^ (z >> 30)).wrapping_mul(0xbf58_476d_1ce4_e5b9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94d0_49bb_1331_11eb);
        z ^ (z >> 31)
    }

    fn below(&mut self, n: u64) -> u64 {
        if n == 0 { 0 } else { self.next() % n }
    }
}

fn mix(a: u64, b: u64) -> u64 {
    let mut z = a.wrapping_add(0x9e37_79b9_7f4a_7c15u64.wrapping_mul(b.wrapping_add(1)));
    z = (z ^ (z >> 30)).wrapping_mul(0xbf58_476d_1ce4_e5b9);
    z = (z ^ (z >> 27)).wrapping_mul(0x94d0_49bb_1331_11eb);
    z ^ (z >> 31)
}

// ---------------------------------------------------------------------------
// the units, and the ONE shape the oracle walks
// ---------------------------------------------------------------------------
//
// Every unit emits its own TableBlockInfo in its own crate, so blockdemo's and
// blockhome's are structurally identical and distinct types. Each row below
// converts its own into the shape here, which is what lets one oracle walk
// every unit.

#[derive(Clone)]
struct Array {
    name: &'static str,
    offset_of_offset: u32,
    count_offset: u32,
    stride_offset: u32,
    stride: u32,
    element_size: u32,
    element_align: u32,
    maximum: i64,
}

struct Unit {
    name: &'static str,
    vectors: i64,
    projection_size: i64,
    max_bytes: i64,
    arrays: Vec<Array>,
    // Open, erased: whether it opened, the used extent it reported, and the
    // base it pointed at.
    open: fn(*const u8, i64) -> Option<(*const u8, i64)>,
    // The TYPED walk: the generated row accessors, over an opened block. It is
    // the surface a consumer actually uses, so a mutant that satisfied the
    // oracle and then tripped the accessors would be a find.
    typed_walk: fn(*const u8, i64),
}

macro_rules! unit {
    ($name:literal, $vectors:expr, $krate:ident, $block:ty, $max_bytes:expr,
     [$(($array:literal, $maximum:expr, $rows:ident)),* $(,)?]) => {{
        let info = <$block>::type_info();
        let mut arrays = Vec::new();
        for field in info.fields.iter() {
            if !field.out_of_line {
                continue;
            }
            let element = match field.element {
                Some(e) => e(),
                None => defect("an out-of-line array's descriptor names no element record"),
            };
            let maximum = match field.name {
                $($array => $maximum,)*
                other => panic!("no maximum for {other}"),
            };
            arrays.push(Array {
                name: field.name,
                offset_of_offset: field.offset_of_offset,
                count_offset: field.count_offset,
                stride_offset: field.stride_offset,
                stride: field.stride,
                element_size: element.size,
                element_align: element.align,
                maximum,
            });
        }
        Unit {
            name: $name,
            vectors: $vectors,
            projection_size: info.size as i64,
            max_bytes: $max_bytes,
            arrays,
            open: |base, bytes| unsafe {
                <$block>::open(base, bytes).map(|b| (b.base(), b.bytes()))
            },
            typed_walk: |base, bytes| unsafe {
                if let Some(block) = <$block>::open(base, bytes) {
                    let mut accumulator = 0u64;
                    $(
                        for row in block.$rows() {
                            accumulator = accumulator.wrapping_add(row as *const _ as u64);
                        }
                    )*
                    SINK = SINK.wrapping_add(accumulator);
                }
            },
        }
    }};
}

fn units() -> Vec<Unit> {
    use blockdemo::{PaddedFrameBlock, RenderFrameBlock};
    use blockhome::PartFrameBlock;
    vec![
        unit!("render", 5, blockdemo, RenderFrameBlock, RenderFrameBlock::MAX_BYTES, [
            ("cameras", RenderFrameBlock::CAMERAS_MAX, cameras),
            ("ships", RenderFrameBlock::SHIPS_MAX, ships),
            ("turrets", RenderFrameBlock::TURRETS_MAX, turrets),
            ("missiles", RenderFrameBlock::MISSILES_MAX, missiles),
            ("dynamic_props", RenderFrameBlock::DYNAMIC_PROPS_MAX, dynamic_props),
            ("static_props", RenderFrameBlock::STATIC_PROPS_MAX, static_props),
            ("cosmetic_props", RenderFrameBlock::COSMETIC_PROPS_MAX, cosmetic_props),
            ("lasers", RenderFrameBlock::LASERS_MAX, lasers),
            ("explosions", RenderFrameBlock::EXPLOSIONS_MAX, explosions),
        ]),
        unit!("padded", 4, blockdemo, PaddedFrameBlock, PaddedFrameBlock::MAX_BYTES, [
            ("rows", PaddedFrameBlock::ROWS_MAX, rows),
        ]),
        unit!("part", 4, blockhome, PartFrameBlock, PartFrameBlock::MAX_BYTES, [
            ("parts", PartFrameBlock::PARTS_MAX, parts),
        ]),
    ]
}

// ---------------------------------------------------------------------------
// aligned storage, at EXACTLY the bytes the caller claims
// ---------------------------------------------------------------------------

struct Region {
    allocation: *mut u8,
    layout: Layout,
    base: *mut u8,
}

impl Region {
    fn new(claim: i64, lead: usize) -> Region {
        let want = (claim as usize + lead).max(1);
        let layout = Layout::from_size_align(want, 64).unwrap();
        let allocation = unsafe { alloc(layout) };
        if allocation.is_null() {
            defect("out of memory");
        }
        Region {
            allocation,
            layout,
            base: unsafe { allocation.add(lead) },
        }
    }
}

impl Drop for Region {
    fn drop(&mut self) {
        unsafe { dealloc(self.allocation, self.layout) };
    }
}

// ---------------------------------------------------------------------------
// the oracle
// ---------------------------------------------------------------------------

fn walk_opened(unit: &Unit, base: *const u8, bytes: i64, reported: i64) {
    if reported < unit.projection_size || reported > bytes {
        defect("an opened block reports a used extent outside [ the projection, the bytes the caller passed ]");
    }
    for array in unit.arrays.iter() {
        let offset_of = unsafe { read_u64(base.add(array.offset_of_offset as usize)) };
        let count = unsafe { read_u32(base.add(array.count_offset as usize)) } as u64;
        let stride = unsafe { read_u32(base.add(array.stride_offset as usize)) } as u64;

        if stride != array.stride as u64 {
            defect("an opened block carries a pitch that is not this build's own (docs/SPEC-TABLES.md §19.3)");
        }
        if array.element_size as u64 != stride {
            defect("an opened block's row descriptor disagrees with the pitch it opened at");
        }
        if count > array.maximum as u64 {
            defect("an opened block carries a count past its DECLARED MAXIMUM");
        }
        if offset_of < unit.projection_size as u64 {
            defect("an opened block's array starts inside the projection");
        }
        let start_alignment = 64u64.max(array.element_align as u64);
        if offset_of % start_alignment != 0 {
            defect("an opened block's array does not start aligned for its element (docs/SPEC-TABLES.md §19.1)");
        }
        if stride != 0 && count > (u64::MAX - offset_of) / stride {
            defect("an opened block's array extent does not fit in 64 bits");
        }
        let end = offset_of + count * stride;
        if end > bytes as u64 {
            defect("an opened block's rows leave the extent the caller passed");
        }
        if end > reported as u64 {
            defect("an opened block's rows leave the used extent it reported");
        }

        // the whole walk: every byte of every row, CHECKED above and READ here.
        // Under Miri the read is the guarantee; under the ordinary build the
        // check is, and the read is what makes the check about something.
        let span = (count * stride) as usize;
        let mut accumulator = 0u64;
        for i in 0..span {
            accumulator = accumulator.wrapping_add(unsafe { *base.add(offset_of as usize + i) } as u64);
        }
        unsafe { SINK = SINK.wrapping_add(accumulator) };
    }
}

unsafe fn read_u64(p: *const u8) -> u64 {
    unsafe { std::ptr::read_unaligned(p as *const u64) }
}

unsafe fn read_u32(p: *const u8) -> u32 {
    unsafe { std::ptr::read_unaligned(p as *const u32) }
}

// ---------------------------------------------------------------------------
// the mutators: the C++ leg's, value for value
// ---------------------------------------------------------------------------

const BOUNDARIES: [u64; 17] = [
    0,
    1,
    2,
    0x7fff_ffff,
    0x8000_0000,
    0xffff_ffff,
    0x1_0000_0000,
    0x7fff_ffff_ffff_ffff,
    0x8000_0000_0000_0000,
    0xffff_ffff_ffff_ffff,
    0xffff_ffff_ffff_fffe,
    0xffff_fffe_0000_0000,
    // and the same extremes rounded DOWN to a block's 64-byte start alignment,
    // because an offset_of that is not 64-aligned is refused before it reaches
    // the arithmetic and never exercises it
    0x4000_0000_0000_0000,
    0x7fff_ffff_ffff_ffc0,
    0x7fff_ffff_ffff_ff80,
    0x8000_0000_0000_0040,
    0xffff_ffff_ffff_ffc0,
];

const WIDTHS: [usize; 4] = [1, 2, 4, 8];

#[derive(Clone, Copy)]
struct Slot {
    name: &'static str,
    offset: u32,
    maximum: i64, // -1 when the slot has none
}

fn slots_of(unit: &Unit) -> Vec<Slot> {
    let mut slots = vec![
        Slot { name: "magic", offset: 0, maximum: -1 },
        Slot { name: "build_version", offset: 8, maximum: -1 },
        Slot { name: "byte_order", offset: 16, maximum: 2 },
    ];
    for array in unit.arrays.iter() {
        slots.push(Slot { name: array.name, offset: array.offset_of_offset, maximum: unit.max_bytes });
        slots.push(Slot { name: array.name, offset: array.count_offset, maximum: array.maximum });
        slots.push(Slot { name: array.name, offset: array.stride_offset, maximum: array.stride as i64 });
    }
    slots
}

fn num_values() -> usize {
    BOUNDARIES.len() + 3
}

fn boundary_value(slot: &Slot, v: usize) -> u64 {
    if v < BOUNDARIES.len() {
        return BOUNDARIES[v];
    }
    if slot.maximum < 0 {
        return BOUNDARIES[(v - BOUNDARIES.len()) % BOUNDARIES.len()];
    }
    match v - BOUNDARIES.len() {
        0 => slot.maximum as u64 - 1,
        1 => slot.maximum as u64,
        _ => slot.maximum as u64 + 1,
    }
}

fn write_word(buffer: *mut u8, buffer_bytes: i64, offset: u64, width: usize, value: u64) {
    if offset as i64 + width as i64 > buffer_bytes {
        return;
    }
    for i in 0..width {
        // little-endian, the order this build writes
        unsafe { *buffer.add(offset as usize + i) = (value >> (8 * i)) as u8 };
    }
}

fn mutate_random(rng: &mut Rng, unit: &Unit, slots: &[Slot], buffer: *mut u8, copied: i64) {
    let projection = unit.projection_size;
    match rng.below(7) {
        0 => site().description = "no mutation: the valid block itself".to_string(),
        1 => {
            let flips = 1 + rng.below(8);
            let mut at = String::from("byte flips at");
            for _ in 0..flips {
                let limit = if rng.below(4) != 0 && copied > projection { projection } else { copied };
                if limit <= 0 {
                    break;
                }
                let where_ = rng.below(limit as u64) as usize;
                unsafe { *buffer.add(where_) ^= 1u8 << rng.below(8) };
                at.push_str(&format!(" {where_}"));
            }
            site().description = at;
        }
        2 => {
            let which = rng.below(slots.len() as u64 + 4) as usize;
            let (name, offset, mut width, value);
            if which < slots.len() {
                let slot = slots[which];
                name = slot.name.to_string();
                offset = slot.offset as u64;
                width = WIDTHS[rng.below(4) as usize];
                if offset + width as u64 > projection as u64 {
                    width = 4;
                }
                value = boundary_value(&slot, rng.below(num_values() as u64) as usize);
            } else {
                name = "anywhere in the projection".to_string();
                let mut at = rng.below(projection as u64);
                width = WIDTHS[rng.below(4) as usize];
                if at + width as u64 > projection as u64 {
                    at = projection as u64 - width as u64;
                }
                offset = at;
                value = BOUNDARIES[rng.below(BOUNDARIES.len() as u64) as usize];
            }
            write_word(buffer, copied, offset, width, value);
            site().description = format!("{}-bit overwrite of {name} at {offset} with {value:#x}", width * 8);
        }
        3 => {
            let arrays = unit.arrays.len();
            if arrays < 2 || copied < projection {
                site().description = "no mutation: this unit has fewer than two triples to swap".to_string();
                return;
            }
            let a = rng.below(arrays as u64) as usize;
            let mut b = rng.below(arrays as u64 - 1) as usize;
            if b >= a {
                b += 1;
            }
            let offset_a = unit.arrays[a].offset_of_offset as usize;
            let offset_b = unit.arrays[b].offset_of_offset as usize;
            for i in 0..16 {
                unsafe {
                    let swap = *buffer.add(offset_a + i);
                    *buffer.add(offset_a + i) = *buffer.add(offset_b + i);
                    *buffer.add(offset_b + i) = swap;
                }
            }
            site().description = format!("the triples of arrays {a} and {b} swapped whole");
        }
        4 => {
            let arrays = unit.arrays.len();
            if arrays < 2 || copied < projection {
                site().description = "no mutation: this unit has fewer than two arrays to overlap".to_string();
                return;
            }
            let a = rng.below(arrays as u64) as usize;
            let b = rng.below(arrays as u64) as usize;
            let offset_of = unsafe { read_u64(buffer.add(unit.arrays[b].offset_of_offset as usize)) };
            write_word(buffer, copied, unit.arrays[a].offset_of_offset as u64, 8, offset_of);
            site().description = format!("array {a} pointed at array {b}'s rows");
        }
        5 => {
            let arrays = unit.arrays.len();
            if arrays == 0 || copied < projection {
                site().description = "no mutation: this unit has no array to grow".to_string();
                return;
            }
            let a = rng.below(arrays as u64) as usize;
            let count = unit.arrays[a].maximum as u64 + 1 + rng.below(64);
            write_word(buffer, copied, unit.arrays[a].count_offset as u64, 4, count);
            site().description = format!("array {a}'s count grown past its declared maximum, to {count}");
        }
        _ => {
            // the whole projection replaced by garbage
            let limit = copied.min(projection);
            for i in 0..limit {
                unsafe { *buffer.add(i as usize) = rng.next() as u8 };
            }
            site().description = "the whole projection replaced by garbage".to_string();
        }
    }
}

// ---------------------------------------------------------------------------
// one mutant
// ---------------------------------------------------------------------------

struct Options {
    seed: u64,
    random_mutants: i64,
    only: Option<(String, i64, String, i64)>,
    // The largest seed block this run will forge. It exists for the MIRI leg
    // and for nothing else: Miri interprets, so the oracle's per-byte row walk
    // over the 7.5 MiB count vector would cost hours and cover no check the
    // small vectors do not. What Miri is there to prove is that a REFUSED
    // mutant read nothing outside the extent on the way to refusing, and that
    // is a property of Open and of the projection, which every vector carries.
    max_seed_bytes: i64,
}

static mut MUTANTS_RUN: i64 = 0;
static mut MUTANTS_OPENED: i64 = 0;

fn selected(options: &Options, unit: &str, vector: i64, pass: &str, index: i64) -> bool {
    match &options.only {
        None => true,
        Some((u, v, p, i)) => u == unit && *v == vector && p == pass && *i == index,
    }
}

#[allow(clippy::too_many_arguments)]
fn run_one(
    options: &Options,
    unit: &Unit,
    slots: &[Slot],
    vector: i64,
    pass: &str,
    index: i64,
    seed_block: &[u8],
    claim: i64,
    lead: usize,
    random_mutation: bool,
    rng_seed: u64,
    fixed_description: &str,
) {
    if !selected(options, unit.name, vector, pass, index) {
        return;
    }
    {
        let s = site();
        s.unit = unit.name.to_string();
        s.vector = vector;
        s.pass = pass.to_string();
        s.index = index;
        s.description = fixed_description.to_string();
    }
    let extent = seed_block.len() as i64;
    let region = Region::new(claim, lead);
    let base = region.base;
    let copied = claim.min(extent);
    if copied > 0 {
        unsafe { std::ptr::copy_nonoverlapping(seed_block.as_ptr(), base, copied as usize) };
    }
    if claim > copied {
        // extension with GARBAGE: the bytes past the seed block are not zeros
        let mut garbage = Rng { state: mix(rng_seed, 0xda7a) };
        for i in copied..claim {
            unsafe { *base.add(i as usize) = garbage.next() as u8 };
        }
    }
    if random_mutation {
        let mut rng = Rng { state: rng_seed };
        mutate_random(&mut rng, unit, slots, base, copied);
    }
    if claim != extent || lead != 0 {
        let s = site();
        if !s.description.is_empty() {
            s.description.push_str("; ");
        }
        s.description
            .push_str(&format!("[ {claim} bytes claimed of a {extent}-byte block, base + {lead} ]"));
    }

    unsafe { MUTANTS_RUN += 1 };
    match (unit.open)(base, claim) {
        None => {}
        Some((opened_base, reported)) => {
            if opened_base != base {
                defect("an opened block points somewhere other than at the base the caller passed");
            }
            walk_opened(unit, base, claim, reported);
            (unit.typed_walk)(base, claim);
            unsafe { MUTANTS_OPENED += 1 };
        }
    }
}

fn run_unit(options: &Options, unit: &Unit, unit_index: usize, directory: &str) {
    let slots = slots_of(unit);
    for vector in 0..unit.vectors {
        let path = format!("{directory}/{}_v{vector}.bin", unit.name);
        let seed_block = match fs::read(&path) {
            Ok(b) => b,
            Err(_) => {
                eprintln!("FAILED: missing block fuzz seed {path} (the C++ leg writes it with --dump)");
                exit(1);
            }
        };
        let extent = seed_block.len() as i64;
        if extent > options.max_seed_bytes {
            continue;
        }

        // the unmutated block opens, so a green run is not a fuzzer that
        // refuses everything
        let before = unsafe { MUTANTS_OPENED };
        run_one(options, unit, &slots, vector, "valid", 0, &seed_block, extent, 0, false, 0,
                "the valid block, unmutated");
        if options.only.is_none() && unsafe { MUTANTS_OPENED } == before {
            site().pass = "valid".to_string();
            defect("the VALID block the C++ builder wrote did not open on this side");
        }

        // every length in [ 0, extent + 64 ], exhaustive where the sum of the
        // copies stays sane and sampled beyond
        const EXHAUSTIVE_LIMIT: i64 = 8192;
        if extent <= EXHAUSTIVE_LIMIT {
            for claim in 0..=extent + 64 {
                run_one(options, unit, &slots, vector, "trunc", claim, &seed_block, claim, 0, false, 0,
                        "truncated or extended, otherwise untouched");
            }
        } else {
            let interesting = [
                0, 1, 8, unit.projection_size - 1, unit.projection_size, unit.projection_size + 1,
                extent - 65, extent - 64, extent - 63, extent - 1, extent, extent + 1, extent + 63, extent + 64,
            ];
            let mut index = 0i64;
            for claim in interesting {
                if claim >= 0 && claim <= extent + 64 {
                    run_one(options, unit, &slots, vector, "trunc", index, &seed_block, claim, 0, false, 0,
                            "truncated or extended, otherwise untouched");
                }
                index += 1;
            }
            let samples: i64 = if extent > (1 << 20) { 64 } else { 256 };
            for k in 0..samples {
                let mut rng = Rng { state: mix(options.seed, mix(vector as u64, k as u64)) };
                let claim = rng.below(extent as u64 + 65) as i64;
                run_one(options, unit, &slots, vector, "trunc", index, &seed_block, claim, 0, false, 0,
                        "truncated or extended, otherwise untouched");
                index += 1;
            }
        }

        // the caller's buffer at base + 1 .. base + 63
        for lead in 1..64usize {
            run_one(options, unit, &slots, vector, "lead", lead as i64, &seed_block, extent, lead, false, 0,
                    "an unaligned base");
        }

        // every named slot x every width x every boundary value. The PROJECTION
        // is restored between mutants rather than the whole block: a slot
        // overwrite touches nothing else, and re-copying a 7.5 MiB block per
        // mutant would spend the gate's whole budget on memcpy without covering
        // one case more.
        {
            let region = Region::new(extent, 0);
            unsafe { std::ptr::copy_nonoverlapping(seed_block.as_ptr(), region.base, extent as usize) };
            let mut index = 0i64;
            for slot in slots.iter() {
                for width in WIDTHS {
                    if slot.offset as i64 + width as i64 > unit.projection_size {
                        continue;
                    }
                    for v in 0..num_values() {
                        if !selected(options, unit.name, vector, "slot", index) {
                            index += 1;
                            continue;
                        }
                        {
                            let s = site();
                            s.unit = unit.name.to_string();
                            s.vector = vector;
                            s.pass = "slot".to_string();
                            s.index = index;
                        }
                        let value = boundary_value(slot, v);
                        site().description = format!(
                            "{}-bit overwrite of {} at {} with {value:#x}",
                            width * 8, slot.name, slot.offset
                        );
                        unsafe {
                            std::ptr::copy_nonoverlapping(
                                seed_block.as_ptr(),
                                region.base,
                                unit.projection_size as usize,
                            )
                        };
                        write_word(region.base, extent, slot.offset as u64, width, value);
                        unsafe { MUTANTS_RUN += 1 };
                        if let Some((opened_base, reported)) = (unit.open)(region.base, extent) {
                            if opened_base != region.base {
                                defect("an opened block points somewhere other than at the base the caller passed");
                            }
                            walk_opened(unit, region.base, extent, reported);
                            (unit.typed_walk)(region.base, extent);
                            unsafe { MUTANTS_OPENED += 1 };
                        }
                        index += 1;
                    }
                }
            }
        }

        // the seeded mutators, over lengths and leads too
        let mut budget = options.random_mutants / unit.vectors;
        if extent > (1 << 20) {
            budget /= 8;
        }
        for k in 0..budget {
            let rng_seed = mix(mix(options.seed, mix(unit_index as u64, vector as u64)), k as u64);
            let mut axes = Rng { state: mix(rng_seed, 0x5eed) };
            let mut claim = extent;
            if axes.below(4) == 0 {
                claim = axes.below(extent as u64 + 65) as i64;
            }
            let mut lead = 0usize;
            if axes.below(8) == 0 {
                lead = 1 + axes.below(63) as usize;
            }
            run_one(options, unit, &slots, vector, "random", k, &seed_block, claim, lead, true, rng_seed, "");
        }
    }
}

// ---------------------------------------------------------------------------
// the COOK half (docs/SPEC-TABLES.md §7)
// ---------------------------------------------------------------------------
//
// A cook's Open is a HEADER MATCH, so its forgery surface is the header: the
// magic, the order word, the build version, the two reserved words, the
// alignment, the two part lengths — every one of them, at every width, at every
// boundary value — plus every truncation and every unaligned base. The oracle
// is the same shape as the block's: REFUSE, or OPEN and be WHOLE, where whole
// means the region lies inside the file and holds the root at its base.

struct Cook {
    root: &'static str,
    fixture: String,
    root_size: u64,
    root_align: u64,
    open: fn(*const u8, u64) -> Option<(*const u8, u64)>,
}

fn cooks(directory: &str) -> Vec<Cook> {
    macro_rules! cook {
        ($root:literal, $ty:ty) => {
            Cook {
                root: $root,
                fixture: format!("{directory}/{}.cook", $root),
                root_size: <$ty>::ROOT_SIZE as u64,
                root_align: <$ty>::ROOT_ALIGN as u64,
                open: |bytes, length| unsafe {
                    <$ty>::open(bytes, length).map(|c| (c.region(), c.region_length()))
                },
            }
        };
    }
    vec![
        cook!("Scene", graphdemo::SceneCook),
        cook!("Depot", graphdemo::DepotCook),
        cook!("Album", graphdemo::AlbumCook),
        cook!("TreeNode", graphdemo::TreeNodeCook),
        cook!("ListNode", graphdemo::ListNodeCook),
    ]
}

// §7.1's header is eight u64 words: magic, build_version, byte_order,
// data_length, attribution_length, alignment, and two reserved.
const COOK_HEADER_WORDS: [(&str, u64); 8] = [
    ("magic", 0),
    ("build_version", 8),
    ("byte_order", 16),
    ("data_length", 24),
    ("attribution_length", 32),
    ("alignment", 40),
    ("reserved_0", 48),
    ("reserved_1", 56),
];

fn run_cook(options: &Options, cook: &Cook) {
    let file = match fs::read(&cook.fixture) {
        Ok(b) => b,
        Err(_) => {
            eprintln!(
                "FAILED: missing cook fixture {} (test/cookgen writes it; make tables-rust-fuzz)",
                cook.fixture
            );
            exit(1);
        }
    };
    let length = file.len() as i64;

    let check = |base: *const u8, claim: i64| {
        if let Some((region, region_length)) = (cook.open)(base, claim as u64) {
            let offset = region as usize - base as usize;
            if offset as i64 > claim {
                defect("an opened cook's region begins past the length the caller passed");
            }
            if region_length > (claim - offset as i64) as u64 {
                defect("an opened cook's region leaves the length the caller passed");
            }
            if region_length < cook.root_size {
                defect("an opened cook's region does not hold its own root");
            }
            if (region as usize as u64) % cook.root_align != 0 {
                defect("an opened cook's root is not aligned for its own record");
            }
            // the whole walk: every byte of the region, which is the extent
            // Open said this build may read
            let mut accumulator = 0u64;
            for i in 0..region_length as usize {
                accumulator = accumulator.wrapping_add(unsafe { *region.add(i) } as u64);
            }
            unsafe { SINK = SINK.wrapping_add(accumulator) };
            return true;
        }
        false
    };

    let run = |pass: &str, index: i64, claim: i64, lead: usize, patch: Option<(u64, usize, u64)>, what: String| {
        if !selected(options, cook.root, 0, pass, index) {
            return;
        }
        {
            let s = site();
            s.unit = cook.root.to_string();
            s.vector = 0;
            s.pass = pass.to_string();
            s.index = index;
            s.description = what;
        }
        let region = Region::new(claim, lead);
        let copied = claim.min(length);
        if copied > 0 {
            unsafe { std::ptr::copy_nonoverlapping(file.as_ptr(), region.base, copied as usize) };
        }
        if claim > copied {
            for i in copied..claim {
                unsafe { *region.base.add(i as usize) = 0 };
            }
        }
        if let Some((offset, width, value)) = patch {
            write_word(region.base, claim, offset, width, value);
        }
        unsafe { MUTANTS_RUN += 1 };
        if check(region.base, claim) {
            unsafe { MUTANTS_OPENED += 1 };
        }
    };

    // the unmutated fixture opens
    let before = unsafe { MUTANTS_OPENED };
    run("valid", 0, length, 0, None, "the valid cook, unmutated".to_string());
    if options.only.is_none() && unsafe { MUTANTS_OPENED } == before {
        site().pass = "valid".to_string();
        defect("the VALID cook test/cookgen wrote did not open on this side");
    }

    // every header word x every width x every boundary value
    let mut index = 0i64;
    for (name, offset) in COOK_HEADER_WORDS {
        for width in WIDTHS {
            for value in BOUNDARIES {
                run("header", index, length, 0, Some((offset, width, value)),
                    format!("{}-bit overwrite of the header's {name} at {offset} with {value:#x}", width * 8));
                index += 1;
            }
        }
    }
    // the byte-swapped magic, which is a cook of the other byte order
    run("header", index, length, 0,
        Some((0, 8, graphdemo::TABLE_COOK_MAGIC.swap_bytes())),
        "a cook of the other byte order".to_string());

    // every truncation and extension
    for claim in 0..=length + 64 {
        run("trunc", claim, claim, 0, None, "truncated or extended, otherwise untouched".to_string());
    }

    // the caller's buffer at base + 1 .. base + 63
    for lead in 1..64usize {
        run("lead", lead as i64, length, lead, None, "an unaligned base".to_string());
    }

    // random single-word forgeries over the header
    for k in 0..options.random_mutants / 8 {
        let mut rng = Rng { state: mix(options.seed, k as u64) };
        let (name, offset) = COOK_HEADER_WORDS[rng.below(8) as usize];
        let width = WIDTHS[rng.below(4) as usize];
        let value = if rng.below(2) == 0 {
            BOUNDARIES[rng.below(BOUNDARIES.len() as u64) as usize]
        } else {
            rng.next()
        };
        run("random", k, length, 0, Some((offset, width, value)),
            format!("{}-bit overwrite of the header's {name} with {value:#x}", width * 8));
    }
}

// ---------------------------------------------------------------------------

fn env_u64(name: &str, fallback: u64) -> u64 {
    match env::var(name) {
        Ok(text) if !text.is_empty() => {
            let text = text.trim();
            let parsed = if let Some(hex) = text.strip_prefix("0x") {
                u64::from_str_radix(hex, 16)
            } else {
                text.parse::<u64>()
            };
            parsed.unwrap_or(fallback)
        }
        _ => fallback,
    }
}

fn main() {
    let seed = env_u64("SEED", 24845619678);
    let random_mutants = env_u64("N", 100000) as i64;
    let only = env::var("ONLY").ok().and_then(|text| {
        let parts: Vec<&str> = text.split(':').collect();
        if parts.len() != 4 {
            return None;
        }
        Some((
            parts[0].to_string(),
            parts[1].parse::<i64>().ok()?,
            parts[2].to_string(),
            parts[3].parse::<i64>().ok()?,
        ))
    });
    let max_seed_bytes = env_u64("MAX_SEED_BYTES", i64::MAX as u64) as i64;
    unsafe { RUN_SEED = seed };
    let options = Options { seed, random_mutants, only, max_seed_bytes };

    let block_directory =
        env::var("BLOCK_SEEDS").unwrap_or_else(|_| "build/block-fuzz".to_string());
    let cook_directory = env::var("COOK_FIXTURES").unwrap_or_else(|_| "build/cook-fuzz".to_string());

    for (i, unit) in units().iter().enumerate() {
        run_unit(&options, unit, i, &block_directory);
    }
    for cook in cooks(&cook_directory).iter() {
        run_cook(&options, cook);
    }

    println!(
        "rust forgery fuzzer: {} mutants, {} opened, none read outside the extent the caller passed",
        unsafe { MUTANTS_RUN },
        unsafe { MUTANTS_OPENED }
    );
}
