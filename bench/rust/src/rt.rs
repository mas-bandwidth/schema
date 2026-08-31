// schema bench — families rt and bits for the Rust runner.
//
// Family rt (BENCH-STANDARD.md §1.3, §1.5): the serialize.rs runtime API
// called BY HAND — the four Bench.schema shapes as hand-written packets over
// the Stream trait, the way a game would write them. The §1.5 oracle gate
// byte-compares the hand-written wire against the goldens the GENERATED code
// pinned (testdata/wire/bench_*.bin) and round-trips before any number. No
// `pub` on the packet types or ops per §3.1. Per §3.2 every benched op
// (each monomorphized Serialize) has EXACTLY two call sites: its untimed
// once-helper and its timed loop (#[inline(never)], so the §4.1 verdict has
// a loop body to count).
//
// Family bits (§1.4): the raw BitWriter/BitReader with the 16-width table
// (227 bits/group) over a 65536-byte buffer. Values vary per pass through
// the LCG (widths are the structure and stay fixed; bytes/pass asserted
// constant); reads rotate 64 pre-written variant buffers, each verified to
// read back exactly what was written before any number is produced.

use std::hint::black_box;
use std::time::Instant;

use serialize::{BitReader, BitWriter, ReadStream, Stream, WriteStream};

use crate::{BUFFER_SIZE, Ctx, MAX_NUM_RUNS, NUM_VARIANTS, VARIANT_STRIDE, bench_rng, run_stats};

// ---- the four shapes, hand-written (Bench.schema §1.3) ----

#[derive(Copy, Clone, Default)]
struct RtBenchPacket {
    a: i32,
    b: i32,
    c: i32,
    bits7: u32,
    bits13: u32,
    bits23: u32,
    flag: bool,
    x: f32,
    y: f32,
    z: f32,
    big: u64,
    blob: [u8; 17],
}

// serialize.rs 2.0.0: the canonical generic signature returns Result<(), S::Error>,
// so the write/measure instantiations are statically infallible and every `?`
// in them compiles to nothing (the read instantiation stays fully checked).
fn serialize_rt_packet<S: Stream>(
    s: &mut S,
    p: &mut RtBenchPacket,
) -> serialize::Result<(), S::Error> {
    s.serialize_int(&mut p.a, -100, 100)?;
    s.serialize_int(&mut p.b, 0, 65535)?;
    s.serialize_int(&mut p.c, -1000000, 1000000)?;
    s.serialize_bits(&mut p.bits7, 7)?;
    s.serialize_bits(&mut p.bits13, 13)?;
    s.serialize_bits(&mut p.bits23, 23)?;
    s.serialize_bool(&mut p.flag)?;
    s.serialize_f32(&mut p.x)?;
    s.serialize_f32(&mut p.y)?;
    s.serialize_f32(&mut p.z)?;
    s.serialize_u64(&mut p.big)?;
    s.serialize_bytes(&mut p.blob)?; // aligns internally — the schema says `align` out loud
    Ok(())
}

#[derive(Copy, Clone, Default)]
struct RtBenchInts {
    f0: i32,
    f1: i32,
    f2: i32,
    f3: i32,
    f4: i32,
    f5: i32,
    f6: i32,
    f7: i32,
    f8: i32,
    f9: i32,
}

fn serialize_rt_ints<S: Stream>(s: &mut S, f: &mut RtBenchInts) -> serialize::Result<(), S::Error> {
    s.serialize_int(&mut f.f0, -100, 100)?;
    s.serialize_int(&mut f.f1, 0, 65535)?;
    s.serialize_int(&mut f.f2, -1000000, 1000000)?;
    s.serialize_int(&mut f.f3, 0, 3)?;
    s.serialize_int(&mut f.f4, -15, 15)?;
    s.serialize_int(&mut f.f5, 0, 1000)?;
    s.serialize_int(&mut f.f6, -2048, 2047)?;
    s.serialize_int(&mut f.f7, 0, 255)?;
    s.serialize_int(&mut f.f8, -600000, 600000)?;
    s.serialize_int(&mut f.f9, 0, 100)?;
    Ok(())
}

#[derive(Copy, Clone, Default)]
struct RtBenchBits {
    b7: u32,
    b13: u32,
    b23: u32,
    b3: u32,
    b32: u32,
    b11: u32,
    b19: u32,
    b48: u64,
}

fn serialize_rt_bits<S: Stream>(s: &mut S, f: &mut RtBenchBits) -> serialize::Result<(), S::Error> {
    s.serialize_bits(&mut f.b7, 7)?;
    s.serialize_bits(&mut f.b13, 13)?;
    s.serialize_bits(&mut f.b23, 23)?;
    s.serialize_bits(&mut f.b3, 3)?;
    s.serialize_bits(&mut f.b32, 32)?;
    s.serialize_bits(&mut f.b11, 11)?;
    s.serialize_bits(&mut f.b19, 19)?;
    s.serialize_bits64(&mut f.b48, 48)?;
    Ok(())
}

// BenchMixed by hand (issue #184): every serialize runtime operation the
// schema language expresses, in the order the generated code emits them. The
// §1.5 oracle gate byte-compares this against the generated code's golden.
#[derive(Copy, Clone, Default)]
struct RtMixedEntity {
    entity_id: u32,
    pos_x: i32,
    pos_y: i32,
    pos_z: i32,
    yaw: u32,
    pitch: u32,
    vel_x: i32,
    vel_y: i32,
    vel_z: i32,
    health: i32,
    weapon: i32, // the enum wire
    damage: u32, // the flags wire, 8 bits
    moving: bool,
    firing: bool,
}

#[derive(Copy, Clone, Default)]
struct RtMixedStat {
    stat_id: u32,
    delta: i32,
}

#[derive(Copy, Clone, Default)]
struct RtMixedHitEvent {
    target_id: u32,
    damage: i32,
    hit_kind: i32,
    crit: bool,
}

#[derive(Copy, Clone, Default)]
struct RtMixedChatEvent {
    channel: i32,
    speaker: u32,
}

#[derive(Copy, Clone, Default)]
struct RtMixedPickupEvent {
    item_id: u32,
    amount: i32,
}

#[derive(Copy, Clone)]
struct RtBenchMixed {
    magic: u32,
    sequence: u32,
    ack_sequence: i32,
    ack_bits: u32,
    session_id: u64,
    client_id: u32,
    nonce: u64,
    world_time: i64,
    frame_tick: u64,
    server_time: i32, // raw Q24.8
    entities_count: i32,
    entities: [RtMixedEntity; 8],
    stats_count: i32,
    stats: [RtMixedStat; 80],
    event_type: i32, // the union tag: 0 = None
    hit: RtMixedHitEvent,
    chat: RtMixedChatEvent,
    pickup: RtMixedPickupEvent,
    loadout: [u8; 4],
    player_name_length: i32,
    player_name: [u8; 15],
    payload_length: i32,
    payload: [u8; 16],
    aim_x: f32,
    aim_y: f32,
    aim_z: f32,
    recoil: f32,
    drift: f64,
    wide_key: u128,
    flux: i128,
    ping: u16, // raw UQ8.8
    reserved_bits: u32,
    crc_hint: u32,
    has_extra: bool,
    extra: i32,
    idle_ticks: i32,
}

// std has no blanket Default for [T; 80], so the zero form is written out
impl Default for RtBenchMixed {
    fn default() -> Self {
        Self {
            magic: 0,
            sequence: 0,
            ack_sequence: 0,
            ack_bits: 0,
            session_id: 0,
            client_id: 0,
            nonce: 0,
            world_time: 0,
            frame_tick: 0,
            server_time: 0,
            entities_count: 0,
            entities: [RtMixedEntity::default(); 8],
            stats_count: 0,
            stats: [RtMixedStat::default(); 80],
            event_type: 0,
            hit: RtMixedHitEvent::default(),
            chat: RtMixedChatEvent::default(),
            pickup: RtMixedPickupEvent::default(),
            loadout: [0; 4],
            player_name_length: 0,
            player_name: [0; 15],
            payload_length: 0,
            payload: [0; 16],
            aim_x: 0.0,
            aim_y: 0.0,
            aim_z: 0.0,
            recoil: 0.0,
            drift: 0.0,
            wide_key: 0,
            flux: 0,
            ping: 0,
            reserved_bits: 0,
            crc_hint: 0,
            has_extra: false,
            extra: 0,
            idle_ticks: 0,
        }
    }
}

// the ±2^100 band flux rides in
const RT_FLUX_MIN: i128 = -(1i128 << 100);
const RT_FLUX_MAX: i128 = 1i128 << 100;

fn serialize_rt_mixed<S: Stream>(
    s: &mut S,
    f: &mut RtBenchMixed,
) -> serialize::Result<(), S::Error> {
    s.serialize_bits(&mut f.magic, 16)?;
    s.serialize_bits(&mut f.sequence, 16)?;
    s.serialize_int(&mut f.ack_sequence, 0, 65535)?;
    s.serialize_bits(&mut f.ack_bits, 32)?;
    s.serialize_u64(&mut f.session_id)?;
    s.serialize_u32(&mut f.client_id)?;
    s.serialize_bits64(&mut f.nonce, 64)?; // the full-unsigned ranged path is width-computed bits
    s.serialize_int64(&mut f.world_time, -1000000000000, 1000000000000)?;
    s.serialize_bits64(&mut f.frame_tick, 48)?;
    s.serialize_fixed(&mut f.server_time, 24, 8, 0, 65535)?;

    s.serialize_int(&mut f.entities_count, 1, 8)?;
    for i in 0..f.entities_count as usize {
        let e = &mut f.entities[i];
        s.serialize_bits(&mut e.entity_id, 12)?;
        s.serialize_int(&mut e.pos_x, -16383, 16383)?;
        s.serialize_int(&mut e.pos_y, -16383, 16383)?;
        s.serialize_int(&mut e.pos_z, -16383, 16383)?;
        s.serialize_bits(&mut e.yaw, 9)?;
        s.serialize_bits(&mut e.pitch, 9)?;
        s.serialize_int(&mut e.vel_x, -2048, 2047)?;
        s.serialize_int(&mut e.vel_y, -2048, 2047)?;
        s.serialize_int(&mut e.vel_z, -2048, 2047)?;
        s.serialize_int(&mut e.health, 0, 1000)?;
        s.serialize_int(&mut e.weapon, 0, 15)?;
        s.serialize_bits(&mut e.damage, 8)?;
        s.serialize_bool(&mut e.moving)?;
        s.serialize_bool(&mut e.firing)?;
    }

    s.serialize_int(&mut f.stats_count, 0, 80)?;
    for i in 0..f.stats_count as usize {
        s.serialize_bits(&mut f.stats[i].stat_id, 8)?;
        s.serialize_int(&mut f.stats[i].delta, -512, 511)?;
    }

    s.serialize_int(&mut f.event_type, 0, 3)?;
    match f.event_type {
        1 => {
            s.serialize_bits(&mut f.hit.target_id, 12)?;
            s.serialize_int(&mut f.hit.damage, 0, 4095)?;
            s.serialize_int(&mut f.hit.hit_kind, 0, 7)?;
            s.serialize_bool(&mut f.hit.crit)?;
        }
        2 => {
            s.serialize_int(&mut f.chat.channel, 0, 3)?;
            s.serialize_bits(&mut f.chat.speaker, 12)?;
        }
        3 => {
            s.serialize_bits(&mut f.pickup.item_id, 10)?;
            s.serialize_int(&mut f.pickup.amount, 0, 255)?;
        }
        _ => {}
    }

    for i in 0..4 {
        s.serialize_u8(&mut f.loadout[i])?;
    }

    // string(15) and bytes(16) ride as their §4.3 decomposition in every rt
    // leg — see bench/cpp/bench_main.cpp for the reasoning
    s.serialize_int(&mut f.player_name_length, 0, 15)?;
    let name_len = f.player_name_length as usize;
    s.serialize_bytes(&mut f.player_name[..name_len])?;
    s.serialize_int(&mut f.payload_length, 0, 16)?;
    let payload_len = f.payload_length as usize;
    s.serialize_bytes(&mut f.payload[..payload_len])?;

    s.serialize_compressed_float(&mut f.aim_x, -1.0, 1.0, 0.01)?;
    s.serialize_compressed_float(&mut f.aim_y, -1.0, 1.0, 0.01)?;
    s.serialize_compressed_float(&mut f.aim_z, -1.0, 1.0, 0.01)?;
    s.serialize_f32(&mut f.recoil)?;
    s.serialize_f64(&mut f.drift)?;
    s.serialize_u128(&mut f.wide_key)?;
    s.serialize_int128(&mut f.flux, RT_FLUX_MIN, RT_FLUX_MAX)?;
    s.serialize_fixed(&mut f.ping, 8, 8, 0, 250)?;

    s.serialize_bits(&mut f.reserved_bits, 4)?;
    s.serialize_align()?;
    s.serialize_bits(&mut f.crc_hint, 24)?;
    s.serialize_bool(&mut f.has_extra)?;
    if f.has_extra {
        s.serialize_int(&mut f.extra, 0, 255)?;
    } else {
        s.serialize_int(&mut f.idle_ticks, 0, 15)?;
    }
    Ok(())
}

// const(V, N) and reserved(N) are the two constructs the runtime API cannot
// refuse for you, and a generic serializer cannot construct the stream's own
// error type (WriteStream::Error is Infallible). So the verdict rides beside
// the decode and every read site checks it — the same bytes the generated
// reader refuses, refused here.
trait RtContract {
    fn contract_ok(&self) -> bool;
}

impl RtContract for RtBenchPacket {
    fn contract_ok(&self) -> bool {
        true
    }
}

impl RtContract for RtBenchInts {
    fn contract_ok(&self) -> bool {
        true
    }
}

impl RtContract for RtBenchBits {
    fn contract_ok(&self) -> bool {
        true
    }
}

impl RtContract for RtBenchMixed {
    fn contract_ok(&self) -> bool {
        self.magic == 0xC0DE && self.reserved_bits == 0
    }
}

// ---- pinned instances: test/bench/main.cpp (the golden producer), verbatim ----

fn pin_rt_packet() -> RtBenchPacket {
    let mut p = RtBenchPacket {
        a: -37,
        b: 12345,
        c: 987654,
        bits7: 97,
        bits13: 5000,
        bits23: 1234567,
        flag: true,
        x: 1.5,
        y: -3.25,
        z: 100.125,
        big: 0x123456789ABCDEF0,
        blob: [0; 17],
    };
    for (i, byte) in p.blob.iter_mut().enumerate() {
        *byte = (i * 31) as u8;
    }
    p
}

fn pin_rt_ints() -> RtBenchInts {
    RtBenchInts {
        f0: -37,
        f1: 12345,
        f2: 987654,
        f3: 2,
        f4: -15,
        f5: 777,
        f6: -2048,
        f7: 200,
        f8: -543210,
        f9: 99,
    }
}

fn pin_rt_bits() -> RtBenchBits {
    RtBenchBits {
        b7: 97,
        b13: 5000,
        b23: 1234567,
        b3: 5,
        b32: 0xDEADBEEF,
        b11: 1024,
        b19: 333333,
        b48: 0xFEDCBA987654,
    }
}

fn pin_rt_mixed() -> RtBenchMixed {
    let mut m = RtBenchMixed {
        magic: 0xC0DE,
        sequence: 52428,
        ack_sequence: 12345,
        ack_bits: 0xA5A5A5A5,
        session_id: 0x123456789ABCDEF0,
        client_id: 0xDEADBEEF,
        nonce: 0xFEDCBA9876543210,
        world_time: -987654321000,
        frame_tick: 0x123456789ABC,
        server_time: 12345678,
        entities_count: 8,
        stats_count: 80,
        event_type: 1, // Hit
        loadout: [0x11, 0x22, 0x33, 0x44],
        player_name_length: 8,
        payload_length: 8,
        aim_x: 0.5,
        aim_y: -0.25,
        aim_z: 0.75,
        recoil: 1.5,
        drift: -3.25,
        wide_key: (0x0123456789ABCDEFu128 << 64) | 0xFEDCBA9876543210u128,
        flux: (1i128 << 99) + 7,
        ping: 12345,
        crc_hint: 0xABCDEF,
        has_extra: true,
        extra: 200,
        ..Default::default()
    };
    for i in 0..8usize {
        let e = &mut m.entities[i];
        e.entity_id = (2049 + i * 17) as u32;
        e.pos_x = -16383 + (i as i32) * 4096;
        e.pos_y = 16383 - (i as i32) * 4096;
        e.pos_z = -1 + (i as i32) * 2048;
        e.yaw = (511 - i * 64) as u32;
        e.pitch = (i * 73) as u32;
        e.vel_x = -2048 + (i as i32) * 512;
        e.vel_y = 2047 - (i as i32) * 512;
        e.vel_z = -1024 + (i as i32) * 256;
        e.health = 1000 - (i as i32) * 100;
        e.weapon = 1 + i as i32;
        e.damage = (0x5A + i) as u32;
        e.moving = i % 2 == 0;
        e.firing = i % 3 == 0;
    }
    for i in 0..80usize {
        m.stats[i].stat_id = ((i * 3) % 256) as u32;
        m.stats[i].delta = -512 + ((i * 13) % 1024) as i32;
    }
    m.hit.target_id = 4095;
    m.hit.damage = 4095;
    m.hit.hit_kind = 7;
    m.hit.crit = true;
    m.player_name[..8].copy_from_slice(b"Rowan_01");
    m.payload[..8].copy_from_slice(&[0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04]);
    m
}

// ---- vary functions: bench/cpp/bench_main.cpp's rt mappings exactly ----

fn vary_rt_packet(p: &mut RtBenchPacket, rng: u64) {
    p.a = (((rng >> 8) & 63) as i32) - 32;
    p.b = ((rng >> 16) as u32 & 65535) as i32;
    p.c = (((rng >> 24) & 0xFFFFF) as i32) - 500000;
    p.bits7 = rng as u32 & 127;
    p.bits13 = (rng >> 3) as u32 & 8191;
    p.bits23 = (rng >> 5) as u32 & 8388607;
    p.flag = (rng & 1) != 0;
    p.x = (rng as u32 & 0xFFFF) as f32;
    p.big = rng;
    p.blob[0] = (rng >> 32) as u8;
}

fn vary_rt_ints(f: &mut RtBenchInts, rng: u64) {
    f.f0 = (((rng >> 8) & 63) as i32) - 32;
    f.f1 = ((rng >> 16) as u32 & 65535) as i32;
    f.f2 = (((rng >> 24) & 0xFFFFF) as i32) - 500000;
    f.f3 = ((rng >> 2) as u32 & 3) as i32;
    f.f4 = (((rng >> 11) & 15) as i32) - 8;
    f.f5 = ((rng >> 22) as u32 & 511) as i32;
    f.f6 = (((rng >> 33) & 2047) as i32) - 1024;
    f.f7 = ((rng >> 40) as u32 & 255) as i32;
    f.f8 = (((rng >> 30) & 0xFFFFF) as i32) - 500000;
    f.f9 = ((rng >> 57) as u32 & 63) as i32;
}

fn vary_rt_bits(f: &mut RtBenchBits, rng: u64) {
    f.b7 = rng as u32 & 127;
    f.b13 = (rng >> 3) as u32 & 8191;
    f.b23 = (rng >> 5) as u32 & 8388607;
    f.b3 = (rng >> 29) as u32 & 7;
    f.b32 = (rng >> 16) as u32;
    f.b11 = (rng >> 37) as u32 & 2047;
    f.b19 = (rng >> 44) as u32 & 524287;
    f.b48 = rng & 0xFFFFFFFFFFFF;
}

fn vary_rt_mixed(f: &mut RtBenchMixed, rng: u64) {
    f.sequence = (rng >> 8) as u32 & 65535;
    f.ack_sequence = ((rng >> 24) as u32 & 65535) as i32;
    f.ack_bits = (rng >> 16) as u32;
    f.session_id = rng;
    f.client_id = (rng >> 32) as u32;
    f.nonce = rng ^ 0xA5A5A5A5A5A5A5A5;
    f.world_time = (((rng >> 12) & 0xFFFFFFFFF) as i64) - 34359738368;
    f.frame_tick = rng & 0xFFFFFFFFFFFF;
    f.server_time = ((rng >> 20) & 0x7FFFFF) as i32;
    for i in 0..8usize {
        let e = &mut f.entities[i];
        e.entity_id = ((rng >> i) & 4095) as u32;
        e.pos_x = (((rng >> (i + 4)) & 16383) as i32) - 8192;
        e.pos_y = (((rng >> (i + 12)) & 16383) as i32) - 8192;
        e.health = ((rng >> (i + 20)) & 511) as i32;
        e.weapon = ((rng >> (i + 40)) & 15) as i32;
        e.damage = ((rng >> (i + 28)) & 255) as u32;
        e.moving = (rng >> i) & 1 != 0;
    }
    for i in 0..80usize {
        f.stats[i].delta = (((rng >> (i & 31)) & 1023) as i32) - 512;
    }
    f.hit.target_id = ((rng >> 6) & 4095) as u32;
    f.hit.damage = ((rng >> 18) & 4095) as i32;
    f.hit.hit_kind = ((rng >> 30) & 7) as i32;
    f.hit.crit = rng & 4 != 0;
    f.loadout[0] = (rng >> 56) as u8;
    f.player_name[7] = (65 + ((rng >> 50) & 15)) as u8;
    f.payload[0] = (rng >> 48) as u8;
    f.aim_x = ((rng >> 2) as u32 & 255) as f32 * (1.0 / 256.0) - 0.5;
    f.aim_y = ((rng >> 10) as u32 & 255) as f32 * (1.0 / 256.0) - 0.5;
    f.aim_z = ((rng >> 18) as u32 & 255) as f32 * (1.0 / 256.0) - 0.5;
    f.recoil = (rng as u32 & 0xFFFF) as f32;
    f.drift = (((rng >> 8) & 0xFFFFFF) as i64) as f64 * 0.5;
    f.wide_key = ((rng >> 1) as u128) << 64 | (rng as u128);
    f.flux = (rng >> 16) as i128;
    f.ping = ((rng >> 40) & 0x7FFF) as u16;
    f.crc_hint = ((rng >> 24) & 0xFFFFFF) as u32;
    f.extra = ((rng >> 52) & 255) as i32;
}

// ---- per-shape once-helpers (the single untimed call site per op, §3.2)
// and timed loops (#[inline(never)], one symbol per shape+path) ----

macro_rules! rt_family {
    ($shape:ty, $serialize:ident, $vary:ident,
     $once_write:ident, $once_read:ident, $write_loop:ident, $read_loop:ident) => {
        fn $once_write(msg: &mut $shape, buffer: &mut [u8]) -> i64 {
            let mut ws = WriteStream::new(buffer);
            if $serialize(&mut ws, msg).is_err() {
                return -1;
            }
            ws.flush();
            ws.bytes_processed() as i64
        }

        fn $once_read(out: &mut $shape, buffer: &[u8], bytes: usize) -> bool {
            let mut rs = ReadStream::new(buffer, bytes);
            $serialize(&mut rs, out).is_ok() && out.contract_ok()
        }

        #[inline(never)]
        fn $write_loop(
            ctx: &Ctx,
            base: &mut $shape,
            iters: i64,
            rng: &mut u64,
            buffer: &mut [u8],
        ) -> bool {
            for _ in 0..iters {
                *rng = bench_rng(*rng);
                $vary(base, *rng);
                let n;
                {
                    let mut ws = WriteStream::new(buffer);
                    if $serialize(&mut ws, base).is_err() {
                        return false;
                    }
                    ws.flush();
                    n = ws.bytes_processed();
                }
                black_box(&*buffer);
                ctx.sink.set(ctx.sink.get().wrapping_add(n));
            }
            true
        }

        #[inline(never)]
        fn $read_loop(
            ctx: &Ctx,
            out: &mut $shape,
            iters: i64,
            bytes_per_op: usize,
            variants: &[[u8; VARIANT_STRIDE]],
        ) -> bool {
            for i in 0..iters {
                let mut rs =
                    ReadStream::new(&variants[(i as usize) & (NUM_VARIANTS - 1)], bytes_per_op);
                if $serialize(&mut rs, out).is_err() || !out.contract_ok() {
                    return false;
                }
                black_box(&*out);
                ctx.sink.set(ctx.sink.get().wrapping_add(1));
            }
            true
        }
    };
}

rt_family!(
    RtBenchPacket,
    serialize_rt_packet,
    vary_rt_packet,
    rt_once_write_packet,
    rt_once_read_packet,
    rt_bench_packet_write_loop,
    rt_bench_packet_read_loop
);
rt_family!(
    RtBenchInts,
    serialize_rt_ints,
    vary_rt_ints,
    rt_once_write_ints,
    rt_once_read_ints,
    rt_bench_ints_write_loop,
    rt_bench_ints_read_loop
);
rt_family!(
    RtBenchBits,
    serialize_rt_bits,
    vary_rt_bits,
    rt_once_write_bits,
    rt_once_read_bits,
    rt_bench_bits_write_loop,
    rt_bench_bits_read_loop
);
rt_family!(
    RtBenchMixed,
    serialize_rt_mixed,
    vary_rt_mixed,
    rt_once_write_mixed,
    rt_once_read_mixed,
    rt_bench_mixed_write_loop,
    rt_bench_mixed_read_loop
);

// ---- the family rt driver: §1.5 oracle gate, then the timed loops ----

#[allow(clippy::too_many_arguments)]
fn bench_rt<T: Copy + Default + RtContract>(
    ctx: &Ctx,
    name: &str,
    iters: i64,
    pinned: T,
    once_write: fn(&mut T, &mut [u8]) -> i64,
    once_read: fn(&mut T, &[u8], usize) -> bool,
    write_loop: fn(&Ctx, &mut T, i64, &mut u64, &mut [u8]) -> bool,
    read_loop: fn(&Ctx, &mut T, i64, usize, &[[u8; VARIANT_STRIDE]]) -> bool,
    vary: fn(&mut T, u64),
) {
    let mut buffer = vec![0u8; BUFFER_SIZE];
    let mut twin = vec![0u8; BUFFER_SIZE];
    let mut variants = vec![[0u8; VARIANT_STRIDE]; NUM_VARIANTS];

    // oracle 1: the hand-written wire must equal the generated-code golden
    let mut base = pinned;
    let wrote = once_write(&mut base, &mut buffer);
    if wrote < 0 {
        ctx.fail(name, "write of pinned instance failed");
        return;
    }
    let bytes_per_op = wrote as usize;
    if !ctx.check_golden(name, &buffer[..bytes_per_op]) {
        ctx.failed.set(true);
        return;
    }

    // oracle 2: round-trip write -> read -> re-write -> identical bytes
    let mut out = T::default();
    if !once_read(&mut out, &buffer, bytes_per_op) {
        ctx.fail(name, "read of pinned instance failed");
        return;
    }
    if once_write(&mut out, &mut twin) != bytes_per_op as i64
        || buffer[..bytes_per_op] != twin[..bytes_per_op]
    {
        ctx.fail(name, "round-trip bytes differ");
        return;
    }

    // variant buffers (and proof that variation keeps bytes/op constant)
    let mut rng: u64 = 1;
    for k in 0..NUM_VARIANTS {
        rng = bench_rng(rng);
        vary(&mut base, rng);
        if once_write(&mut base, &mut variants[k][..BUFFER_SIZE]) != bytes_per_op as i64 {
            ctx.fail(
                name,
                "variation changed bytes/op — vary must keep structure fields fixed",
            );
            return;
        }
    }

    let mut write_rates = [0.0f64; MAX_NUM_RUNS];
    let mut read_rates = [0.0f64; MAX_NUM_RUNS];

    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        if !write_loop(ctx, &mut base, iters, &mut rng, &mut buffer) {
            ctx.fail(name, "write failed in loop");
            return;
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            write_rates[run - 1] = iters as f64 / time;
        }
    }

    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        if !read_loop(ctx, &mut out, iters, bytes_per_op, &variants) {
            ctx.fail(name, "read failed in loop");
            return;
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            read_rates[run - 1] = iters as f64 / time;
        }
    }

    ctx.report(
        name,
        "write",
        iters,
        bytes_per_op as i64,
        &run_stats(&mut write_rates[..ctx.num_runs]),
        "rt",
    );
    ctx.report(
        name,
        "read",
        iters,
        bytes_per_op as i64,
        &run_stats(&mut read_rates[..ctx.num_runs]),
        "rt",
    );
}

// iteration counts fixed and identical across all five runners (§2.1; sized
// in the C++ reference)
pub fn bench_rt_all(ctx: &Ctx) {
    bench_rt(
        ctx,
        "bench_packet",
        32000000,
        pin_rt_packet(),
        rt_once_write_packet,
        rt_once_read_packet,
        rt_bench_packet_write_loop,
        rt_bench_packet_read_loop,
        vary_rt_packet,
    );
    bench_rt(
        ctx,
        "bench_ints",
        40000000,
        pin_rt_ints(),
        rt_once_write_ints,
        rt_once_read_ints,
        rt_bench_ints_write_loop,
        rt_bench_ints_read_loop,
        vary_rt_ints,
    );
    bench_rt(
        ctx,
        "bench_bits",
        48000000,
        pin_rt_bits(),
        rt_once_write_bits,
        rt_once_read_bits,
        rt_bench_bits_write_loop,
        rt_bench_bits_read_loop,
        vary_rt_bits,
    );
    bench_rt_mixed(ctx);
}

// the --quick leg: bench_mixed alone (golden-gated by bench_rt like every leg)
pub fn bench_rt_mixed(ctx: &Ctx) {
    bench_rt(
        ctx,
        "bench_mixed",
        40000000,
        pin_rt_mixed(),
        rt_once_write_mixed,
        rt_once_read_mixed,
        rt_bench_mixed_write_loop,
        rt_bench_mixed_read_loop,
        vary_rt_mixed,
    );
}

// ------------------------------------------------------------------------------------------
// family bits (§1.4)
// ------------------------------------------------------------------------------------------

const BITS_NUM_WIDTHS: usize = 16;
const BITS_WIDTHS: [u32; BITS_NUM_WIDTHS] =
    [1, 32, 7, 13, 3, 25, 8, 19, 4, 28, 11, 16, 2, 30, 6, 22]; // 227 bits/group
const BITS_BUFFER_SIZE: usize = 65536;

fn bits_mask(width: u32) -> u32 {
    if width == 32 {
        0xFFFFFFFF
    } else {
        (1u32 << width) - 1
    }
}

// the per-pass value variation: one LCG step per pass, values from its bits
fn vary_bits_values(values: &mut [u32; BITS_NUM_WIDTHS], rng: u64) {
    for i in 0..BITS_NUM_WIDTHS {
        values[i] = (rng >> i) as u32 & bits_mask(BITS_WIDTHS[i]);
    }
}

// the single untimed write_bits call site (§3.2)
fn bits_write_pass(buffer: &mut [u8], values: &[u32; BITS_NUM_WIDTHS]) -> i64 {
    let mut w = BitWriter::new(buffer);
    while w.bits_available() >= 256 {
        for i in 0..BITS_NUM_WIDTHS {
            w.write_bits(values[i], BITS_WIDTHS[i]);
        }
    }
    w.flush_bits();
    w.bytes_written() as i64
}

// the single untimed read_bits call site (§3.2): the buffer must read back
// exactly the values written — the bits family's refusal gate
fn bits_read_verify(buffer: &[u8], values: &[u32; BITS_NUM_WIDTHS]) -> bool {
    let mut r = BitReader::new(buffer, BITS_BUFFER_SIZE);
    while r.bits_remaining() >= 256 {
        for i in 0..BITS_NUM_WIDTHS {
            if r.read_bits(BITS_WIDTHS[i]) != values[i] {
                return false;
            }
        }
    }
    true
}

#[inline(never)]
fn bitpacker_write_loop(
    ctx: &Ctx,
    passes: i64,
    bytes_per_pass: i64,
    rng: &mut u64,
    values: &mut [u32; BITS_NUM_WIDTHS],
    buffer: &mut [u8],
) -> bool {
    for _ in 0..passes {
        *rng = bench_rng(*rng);
        vary_bits_values(values, *rng);
        let wrote;
        {
            let mut w = BitWriter::new(buffer);
            while w.bits_available() >= 256 {
                for i in 0..BITS_NUM_WIDTHS {
                    w.write_bits(values[i], BITS_WIDTHS[i]);
                }
            }
            w.flush_bits();
            wrote = w.bytes_written() as i64;
        }
        if wrote != bytes_per_pass {
            return false; // the bytes_per_op assertion (§2.7)
        }
        black_box(&*buffer);
        ctx.sink.set(ctx.sink.get().wrapping_add(wrote as u64));
    }
    true
}

#[inline(never)]
fn bitpacker_read_loop(ctx: &Ctx, passes: i64, variants: &[Vec<u8>]) -> bool {
    for pass in 0..passes {
        let mut r = BitReader::new(
            &variants[(pass as usize) & (NUM_VARIANTS - 1)],
            BITS_BUFFER_SIZE,
        );
        let mut sum: u64 = 0;
        while r.bits_remaining() >= 256 {
            for i in 0..BITS_NUM_WIDTHS {
                sum += r.read_bits(BITS_WIDTHS[i]) as u64;
            }
        }
        ctx.sink.set(ctx.sink.get().wrapping_add(sum));
    }
    true
}

pub fn bench_bitpacker(ctx: &Ctx, passes: i64) {
    let mut values = [0u32; BITS_NUM_WIDTHS];
    let mut buffer = vec![0u8; BITS_BUFFER_SIZE];
    let mut variants: Vec<Vec<u8>> = (0..NUM_VARIANTS)
        .map(|_| vec![0u8; BITS_BUFFER_SIZE])
        .collect();

    let mut rng: u64 = 1;
    let mut bytes_per_pass: i64 = -1;
    for k in 0..NUM_VARIANTS {
        rng = bench_rng(rng);
        vary_bits_values(&mut values, rng);
        let wrote = bits_write_pass(&mut variants[k], &values);
        if bytes_per_pass < 0 {
            bytes_per_pass = wrote;
        }
        if wrote != bytes_per_pass {
            ctx.fail(
                "bitpacker",
                "variation changed bytes/pass — widths are the structure and must stay fixed",
            );
            return;
        }
        if !bits_read_verify(&variants[k], &values) {
            ctx.fail(
                "bitpacker",
                "read-back disagrees with written values — refusing to bench",
            );
            return;
        }
    }

    let mut write_rates = [0.0f64; MAX_NUM_RUNS];
    let mut read_rates = [0.0f64; MAX_NUM_RUNS];

    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        if !bitpacker_write_loop(
            ctx,
            passes,
            bytes_per_pass,
            &mut rng,
            &mut values,
            &mut buffer,
        ) {
            ctx.fail(
                "bitpacker",
                "bytes/pass changed in the timed loop (§2.7 assertion)",
            );
            return;
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            write_rates[run - 1] = passes as f64 / time;
        }
    }

    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        if !bitpacker_read_loop(ctx, passes, &variants) {
            ctx.fail("bitpacker", "read loop failed");
            return;
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            read_rates[run - 1] = passes as f64 / time;
        }
    }

    ctx.report(
        "bitpacker",
        "write",
        passes,
        bytes_per_pass,
        &run_stats(&mut write_rates[..ctx.num_runs]),
        "bits",
    );
    ctx.report(
        "bitpacker",
        "read",
        passes,
        bytes_per_pass,
        &run_stats(&mut read_rates[..ctx.num_runs]),
        "bits",
    );
}
