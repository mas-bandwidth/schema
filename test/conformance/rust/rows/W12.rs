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
// This case computes it, after the Go row (test/conformance/go/rows/W12_test.go).
// Two fixed tables of the blockdemo unit whose layouts carry NO range / bits /
// fixed / flags fact. THE LAW MOVED: this row once asserted their baked hash
// is fnv1a64 over the layout block ALONE (an empty legacy digest). Every v2
// fixed root's digest now BEGINS with the 26-byte identity prefix
// `\0schema-fixed-definitions\x02`, "even when there are no rows"
// (docs/FIXED-FORM-ALGORITHM.md §5.2, "PROSPECTIVE V2 DEFINITIONS DIGEST
// (WireLaw 3)", and :73 "prospective v2 roots always include their identity
// prefix"; reference ir/fixedform_digest_v2.go `tableFixedDigestV2Prefix`;
// vector testdata/conformance/fixed-digest/vector.txt, whose `digest` line
// opens with these 26 bytes). These two tables have no rows, so their digest
// is that prefix and nothing else. So:
//
//   * the block opens with its 4-byte entry count,
//   * the LEGACY layout-only hash stays a FROZEN constant check (the two
//     constants this row was cut with), so the count rule is still pinned
//     on the layout bytes alone,
//   * fnv1a64 over the block AS WRITTEN (count included) THEN the digest
//     equals the baked hash — the current key,
//   * dropping the 4-byte count moves both numbers — the count is in the input,
//   * the two tables' hashes differ, so the case is not one lucky constant.
//
// fnv1a64 is written out LONGHAND here and never borrowed from the code under
// test (whose `table_fixed_hash` would agree with whatever it happened to do),
// and the digest bytes are this row's own, transcribed from the spec: it is
// the oracle, not an echo.
//
// PaddedRow: 17 layout entries, legacy layout-only hash 0x9683695abec5c762.
// RenderCamera: 14 layout entries, legacy layout-only hash 0x74ade5c3866f68d1.

use blockdemo::{
    PADDED_ROW_FIXED_BLOCK, PADDED_ROW_FIXED_HASH, RENDER_CAMERA_FIXED_BLOCK, RENDER_CAMERA_FIXED_HASH,
};

/// THE DEFINITIONS DIGEST of a v2 fixed root with no rows: exactly the 26-byte
/// identity prefix (docs/FIXED-FORM-ALGORITHM.md §5.2, the bytes spelled out
/// there: 00 73 63 68 65 6d 61 2d 66 69 78 65 64 2d 64 65 66 69 6e 69 74 69 6f
/// 6e 73 02).
const V2_EMPTY_DIGEST: [u8; 26] = *b"\0schema-fixed-definitions\x02";

fn fnv1a64(parts: &[&[u8]]) -> u64 {
    let mut h: u64 = 0xcbf2_9ce4_8422_2325;
    for part in parts {
        for &v in *part {
            h ^= u64::from(v);
            h = h.wrapping_mul(0x0000_0100_0000_01b3);
        }
    }
    h
}

struct Case {
    name: &'static str,
    block: &'static [u8],
    hash: u64,
    legacy_hash: u64,
}

const CASES: [Case; 2] = [
    Case {
        name: "PaddedRow",
        block: &PADDED_ROW_FIXED_BLOCK,
        hash: PADDED_ROW_FIXED_HASH,
        legacy_hash: 0x9683_695a_bec5_c762,
    },
    Case {
        name: "RenderCamera",
        block: &RENDER_CAMERA_FIXED_BLOCK,
        hash: RENDER_CAMERA_FIXED_HASH,
        legacy_hash: 0x74ad_e5c3_866f_68d1,
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
fn the_digest_prefix_is_the_spec_bytes() {
    // the oracle's own bytes, checked against the spec's hex spelling
    let spelled: [u8; 26] = [
        0x00, 0x73, 0x63, 0x68, 0x65, 0x6d, 0x61, 0x2d, 0x66, 0x69, 0x78, 0x65, 0x64, 0x2d, 0x64,
        0x65, 0x66, 0x69, 0x6e, 0x69, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x02,
    ];
    assert_eq!(V2_EMPTY_DIGEST, spelled, "the v2 identity prefix is the 26 bytes §5.2 spells");
}

// THE FROZEN LEGACY KEY: fnv1a64 over the layout block alone, count included,
// is still the number this row was cut with — the layout bytes did not move,
// only the identity epoch did.
#[test]
fn legacy_layout_only_hash_is_frozen() {
    for c in CASES {
        assert_eq!(
            fnv1a64(&[c.block]),
            c.legacy_hash,
            "{}: the legacy layout-only hash over the {} bytes as written (count included) moved",
            c.name,
            c.block.len()
        );
    }
}

// THE CURRENT KEY: the layout as written, the 4-byte count included, then the
// definitions digest (the v2 prefix, these tables having no rows).
#[test]
fn hash_is_fnv1a64_over_the_block_count_included_then_the_digest() {
    for c in CASES {
        let want = fnv1a64(&[c.block, &V2_EMPTY_DIGEST]);
        assert_eq!(
            c.hash, want,
            "{}: the layout hash is not fnv1a64 over the {} bytes as written (the 4-byte count included) then the 26-byte v2 digest",
            c.name,
            c.block.len()
        );
        assert_ne!(
            c.hash, c.legacy_hash,
            "{}: the baked hash is still the legacy layout-only key; the v2 digest is not folded in",
            c.name
        );
    }
}

// THE NEGATIVE CONTROLS, both kept: dropping the 4-byte count moves the
// legacy number AND the current one.
#[test]
fn dropping_the_4byte_count_moves_the_hash() {
    for c in CASES {
        let dropped_legacy = fnv1a64(&[&c.block[4..]]);
        assert_ne!(
            dropped_legacy, c.legacy_hash,
            "{}: dropping the 4-byte count leaves the legacy hash unchanged, so the count is not in the input",
            c.name
        );
        let dropped = fnv1a64(&[&c.block[4..], &V2_EMPTY_DIGEST]);
        assert_ne!(
            dropped, c.hash,
            "{}: dropping the 4-byte count leaves the current hash unchanged, so the count is not in the input",
            c.name
        );
        assert_ne!(
            dropped,
            fnv1a64(&[c.block, &V2_EMPTY_DIGEST]),
            "{}: with and without the count must differ under the digest too",
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
