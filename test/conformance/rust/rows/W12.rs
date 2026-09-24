// W12 — "hash includes the 4-byte count" (docs/FIXED-FORM-ALGORITHM.md:56).
//
// The layout hash is fnv1a64 over the layout's bytes as written, the 4-byte
// entry count included, and then over the DEFINITIONS DIGEST. The Rust leg
// holds its hash as a baked constant (`*_FIXED_HASH`) and the runtime is right
// never to recompute it from the bytes it seals (§5.9 #47), so nothing on this
// leg computes the hash from the block at load time — a leg whose hash skipped
// the count word would still pass every fixture it has, because it never
// computes the hash at all. That is the `~` this cell reconciles: the behaviour
// is exercised, the rule was asserted by nothing.
//
// This case computes it. Two fixed tables of the blockdemo unit whose layouts
// carry NO range / bits / fixed / flags fact — their definitions digest is
// empty, so their baked hash is fnv1a64 over the layout block alone. The
// emitted `*_FIXED_BLOCK` IS the layout as written, and its own first four
// bytes ARE its entry count, so:
//
//   * the block opens with its 4-byte entry count,
//   * fnv1a64 over the block AS WRITTEN (count included) equals the baked hash,
//   * dropping the 4-byte count moves the number — the count is in the input,
//   * the two tables' hashes differ, so the case is not one lucky constant.
//
// fnv1a64 is written out LONGHAND here and never borrowed from the code under
// test (whose `table_fixed_hash` would agree with whatever it happened to do):
// it is the oracle, not an echo.
//
// PaddedRow: 17 layout entries, no digest, hash 0x9683695abec5c762.
// RenderCamera: 14 layout entries, no digest, hash 0x74ade5c3866f68d1.

use blockdemo::{
    PADDED_ROW_FIXED_BLOCK, PADDED_ROW_FIXED_HASH, RENDER_CAMERA_FIXED_BLOCK, RENDER_CAMERA_FIXED_HASH,
};

fn fnv1a64(b: &[u8]) -> u64 {
    let mut h: u64 = 0xcbf2_9ce4_8422_2325;
    for &v in b {
        h ^= u64::from(v);
        h = h.wrapping_mul(0x0000_0100_0000_01b3);
    }
    h
}

struct Case {
    name: &'static str,
    block: &'static [u8],
    hash: u64,
}

const CASES: [Case; 2] = [
    Case {
        name: "PaddedRow",
        block: &PADDED_ROW_FIXED_BLOCK,
        hash: PADDED_ROW_FIXED_HASH,
    },
    Case {
        name: "RenderCamera",
        block: &RENDER_CAMERA_FIXED_BLOCK,
        hash: RENDER_CAMERA_FIXED_HASH,
    },
];

#[test]
fn block_opens_with_its_4byte_entry_count() {
    for c in CASES {
        assert!(
            c.block.len() >= 4 && (c.block.len() - 4) % 17 == 0,
            "{}: the layout is not a 4-byte count plus whole 17-byte entries",
            c.name
        );
        let count = u32::from_le_bytes(c.block[0..4].try_into().unwrap()) as usize;
        let entries = (c.block.len() - 4) / 17;
        assert_eq!(
            count, entries,
            "{}: the layout as written does not open with its 4-byte entry count",
            c.name
        );
    }
}

#[test]
fn hash_is_fnv1a64_over_the_block_count_included() {
    for c in CASES {
        let want = fnv1a64(c.block);
        assert_eq!(
            c.hash, want,
            "{}: the layout hash is not fnv1a64 over the {} bytes as written, the 4-byte count included",
            c.name,
            c.block.len()
        );
    }
}

#[test]
fn dropping_the_4byte_count_moves_the_hash() {
    for c in CASES {
        let dropped = fnv1a64(&c.block[4..]);
        assert_ne!(
            dropped, c.hash,
            "{}: dropping the 4-byte count leaves the hash unchanged, so the count is not in the input",
            c.name
        );
    }
}

#[test]
fn two_layouts_do_not_share_a_hash() {
    assert_ne!(
        CASES[0].hash, CASES[1].hash,
        "two distinct layouts share a hash; the case is one lucky constant"
    );
}
