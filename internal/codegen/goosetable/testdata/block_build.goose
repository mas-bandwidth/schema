// block_build.goose — THE TWO-LANGUAGE BLOCK GOLDEN, Goose's half
// (docs/SPEC-TABLES.md §19.1, §19.5; accel_test.go). It builds the frames
// test/tables/block_main.cpp pins — every row value a function of its array
// and its index (vec3_for, quat_for, fill_*) — through the generated builder:
// Begin from the counts, then each row as its TYPED record through
// <Table>Block<Array>Put, and the projection's own fields through its
// setters. It prints each frame's used bytes as one hex line, which the test
// holds to testdata/wire/tables/block_render.bin and block_padded.bin.
import os;
import .RenderBlock;
import .PaddedBlock;

fn f(i: i64) -> f64 { return i as f64; }

fn vec3_for(salt: i64, i: i64) -> blockdemo::RenderVector3 {
    return blockdemo::RenderVector3 { X: 1.5 * f(i) + f(salt), Y: 2.25 * f(i) - f(salt), Z: 0.5 * f(i) + 2.0 * f(salt) };
}

fn quat_for(salt: i64, i: i64) -> blockdemo::RenderQuaternion {
    return blockdemo::RenderQuaternion { X: 0.25 * f(i) + f(salt), Y: 0.125 * f(i), Z: 0.0625 * f(i) - f(salt), W: 1.0 - 0.5 * f(i % 3) };
}

fn hex_line(b: const u8[:]) {
    let digits = "0123456789abcdef";
    var line: u8[>..] = [];
    for x in b {
        line.push(digits[(x >> 4) as! i64]);
        line.push(digits[(x & 15) as! i64]);
    }
    line.push('\n');
    write_stdout(line);
}

fn zeroed(n: i64) -> u8[>..] {
    var s: u8[>..] = [];
    var i: i64 = 0;
    while i < n { s.push(0); i += 1; }
    return s;
}

fn fail(why: const u8[:]) { write_stderr(str("block_build: ", why, "\n")); exit(1); }

fn render() {
    var storage = zeroed(blockdemo::RenderFrameBlockMaxBytes);
    let b = storage[0..storage.len];
    let counts = blockdemo::RenderFrameBlockCounts { Cameras: 1, Ships: 5, Turrets: 7, Missiles: 3, DynamicProps: 2, StaticProps: 4, CosmeticProps: 3, Lasers: 6, Explosions: 2 };
    var refusal = schema_rt::TableBlockRefusal { .. };
    let used = blockdemo::RenderFrameBlockBegin(b, 0, counts, refusal);
    if used < 0 { fail("Begin refused the golden counts"); }
    blockdemo::RenderFrameBlockSetVersion(b, blockdemo::RenderVersion as! u64);
    var i: i64 = 0;
    while i < counts.Cameras {
        let r = blockdemo::RenderCamera { Position: vec3_for(1, i), Rotation: quat_for(1, i), CameraId: (i * 7 + 1) as! u32, CameraType: (i % 4) as! u32,
            TargetObjectId: (i * 13 + 2) as! u32, Fov: (0.5 * f(i) + 60.0) as f32 };
        if !blockdemo::RenderFrameBlockCamerasPut(b, i, r) { fail("a camera row refused"); }
        i += 1;
    }
    i = 0;
    while i < counts.Ships {
        let r = blockdemo::RenderShip { Position: vec3_for(2, i), Rotation: quat_for(2, i), Flags: (i % 16) as! u64, ObjectId: (i * 3 + 11) as! u32,
            TargetObjectId: (i * 5 + 7) as! u32, Thrust: (0.25 * f(i)) as f32, ObjectSequence: (i % 251) as! u8, ShipType: (i % 4) as! u8,
            Team: (i % 5) as! u8, HasTargetLock: (i % 2) == 0, PredictedExplode: (i % 3) == 0 };
        if !blockdemo::RenderFrameBlockShipsPut(b, i, r) { fail("a ship row refused"); }
        i += 1;
    }
    i = 0;
    while i < counts.Turrets {
        let r = blockdemo::RenderTurret { Rotation: quat_for(3, i), Flags: (i * 17) as! u64, ObjectId: (i * 2 + 1) as! u32, ParentObjectId: (i / 3) as! u32,
            TurretIndex: (i % 8) as! u32, TargetObjectId: (i * 11) as! u32, ObjectSequence: (i % 253) as! u8, Team: (i % 5) as! u8, HasTargetLock: (i % 5) == 0 };
        if !blockdemo::RenderFrameBlockTurretsPut(b, i, r) { fail("a turret row refused"); }
        i += 1;
    }
    i = 0;
    while i < counts.Missiles {
        let r = blockdemo::RenderMissile { Position: vec3_for(4, i), Rotation: quat_for(4, i), Flags: (i * 19) as! u64, ObjectId: (i * 23) as! u32,
            ObjectSequence: (i % 249) as! u8, MissileType: (i % 3) as! u8, Team: (i % 5) as! u8 };
        if !blockdemo::RenderFrameBlockMissilesPut(b, i, r) { fail("a missile row refused"); }
        i += 1;
    }
    i = 0;
    while i < counts.DynamicProps {
        let r = blockdemo::RenderDynamicProp { Position: vec3_for(5, i), Rotation: quat_for(5, i), Flags: (i * 29) as! u64, ObjectId: (i * 31) as! u32,
            ObjectSequence: (i % 247) as! u8, PropType: (i % 4) as! u8, Team: (i % 5) as! u8 };
        if !blockdemo::RenderFrameBlockDynamicPropsPut(b, i, r) { fail("a dynamic prop row refused"); }
        i += 1;
    }
    i = 0;
    while i < counts.StaticProps {
        let r = blockdemo::RenderStaticProp { Position: vec3_for(6, i), Rotation: quat_for(6, i), Scale: 0.5 + 0.25 * f(i % 7), Flags: (i * 37) as! u64,
            StaticPropId: (i * 41) as! u32, PropType: (i % 4) as! u8, Team: (i % 5) as! u8 };
        if !blockdemo::RenderFrameBlockStaticPropsPut(b, i, r) { fail("a static prop row refused"); }
        i += 1;
    }
    i = 0;
    while i < counts.CosmeticProps {
        let r = blockdemo::RenderCosmeticProp { Position: vec3_for(7, i), Rotation: quat_for(7, i), Scale: 0.25 + 0.125 * f(i % 5), Flags: (i * 43) as! u64,
            CosmeticPropId: (i * 47) as! u32, PropSequence: (i % 241) as! u8, PropType: (i % 4) as! u8, Team: (i % 5) as! u8 };
        if !blockdemo::RenderFrameBlockCosmeticPropsPut(b, i, r) { fail("a cosmetic prop row refused"); }
        i += 1;
    }
    i = 0;
    while i < counts.Lasers {
        let r = blockdemo::RenderLaser { Start: vec3_for(8, i), Finish: vec3_for(9, i), T: 0.125 * f(i % 8), LaserId: (i * 53) as! u32,
            LaserType: (i % 3) as! u8, Team: (i % 5) as! u8 };
        if !blockdemo::RenderFrameBlockLasersPut(b, i, r) { fail("a laser row refused"); }
        i += 1;
    }
    i = 0;
    while i < counts.Explosions {
        let r = blockdemo::RenderExplosion { Position: vec3_for(10, i), Rotation: quat_for(10, i), T: 0.0625 * f(i % 16), ExplosionId: (i * 59) as! u32,
            ParentObjectId: (i * 61) as! u32, ExplosionType: (i % 3) as! u8, Team: (i % 5) as! u8 };
        if !blockdemo::RenderFrameBlockExplosionsPut(b, i, r) { fail("an explosion row refused"); }
        i += 1;
    }
    let bytes = blockdemo::RenderFrameBlockBytes(b);
    if bytes != used { fail("Bytes disagrees with the extent Begin answered"); }
    // a frame that does not open is still printed: the test names the bytes
    if !blockdemo::RenderFrameBlockOpen(b[0..bytes], 0) { write_stderr("block_build: the built frame does not open\n"); }
    hex_line(b[0..bytes]);
}

fn padded() {
    var storage = zeroed(blockdemo::PaddedFrameBlockMaxBytes);
    let b = storage[0..storage.len];
    let counts = blockdemo::PaddedFrameBlockCounts { Rows: 4 };
    var refusal = schema_rt::TableBlockRefusal { .. };
    let used = blockdemo::PaddedFrameBlockBegin(b, 0, counts, refusal);
    if used < 0 { fail("Begin refused the padded counts"); }
    blockdemo::PaddedFrameBlockSetMarker(b, 7);
    blockdemo::PaddedFrameBlockSetStamp(b, 0x0123456789abcdef);
    blockdemo::PaddedFrameBlockSetBlob(b, "twelve bytes");
    var i: i64 = 0;
    while i < counts.Rows {
        var r = blockdemo::PaddedRow { .. };
        r.Tag = (10 + i) as! u8;
        r.Value = 0.5 * f(i) + 100.0;
        r.Flag = (i % 2) == 0;
        r.Id = (i * 1000 + 3) as! u32;
        let label = str("row-", i);
        for c, k in label { r.Label[k] = c; }
        r.LabelLength = label.len as! i32;
        var s: i64 = 0;
        while s < 4 { r.Slots[s] = (i * 4 + s) as! u16; s += 1; }
        // by KEY: key t (1..4) is storage index t - 1 (docs/SPEC-TABLES.md §2.4)
        var t: i64 = 1;
        while t <= 4 { r.Teams[t - 1] = (i + t) as! u8; t += 1; }
        r.Counter = (i * 9) as! i32;
        r.CounterPresent = (i % 2) == 1;
        if !blockdemo::PaddedFrameBlockRowsPut(b, i, r) { fail("a padded row refused"); }
        i += 1;
    }
    let bytes = blockdemo::PaddedFrameBlockBytes(b);
    if bytes != used { fail("Bytes disagrees with the extent Begin answered"); }
    if !blockdemo::PaddedFrameBlockOpen(b[0..bytes], 0) { write_stderr("block_build: the built padded frame does not open\n"); }
    hex_line(b[0..bytes]);
}

fn main() {
    render();
    padded();
}
