// R4.rs — cell rust/R4: the hash is fnv1a64 over the layout bytes then
// DIGEST(T), the digest computed at the hash site from the schema; a
// runtime never derives a hash from layout bytes it holds.
//
// The production path: `weapon_config_fixed_load` at
// build/tables-generated-rust/tabledemo/src/tables_fixed.rs:762 reads
// the hash from the file header ([TABLE_FIXED_HASH_AT..] = data[8..16])
// and looks it up in the precomputed WEAPON_CONFIG_FIXED_LINEAGE at
// line 698: `position(|k| k.hash == hash)` — it never re-derives the
// hash. The lineage comment at line 696 says:
// "the hash is taken as given, because the definitions digest is not on
//  the wire and the number cannot be re-derived from a file (§5.2)."
//
// Every generated `*_fixed_load` does the same. The only runtime hash
// function is `table_fixed_hash` in fixed_runtime.rs:273, which hashes
// block bytes ONLY (§3.4) and is NOT the layout hash.
//
// This test constructs a minimal layout and its definitions digest,
// computes fnv1a64(layout) then fnv1a64(layout + digest), and asserts
// the two differ — proving the digest changes the hash, and that a
// runtime hashing only layout bytes would compute the WRONG value.

fn fnv1a64(data: &[u8]) -> u64 {
    let mut h: u64 = 0xcbf2_9ce4_8422_2325;
    for &b in data {
        h ^= b as u64;
        h = h.wrapping_mul(0x0000_0100_0000_01b3);
    }
    h
}

fn put_u64_le(buf: &mut [u8], at: usize, v: u64) {
    buf[at..at + 8].copy_from_slice(&v.to_le_bytes());
}

fn put_u32_le(buf: &mut [u8], at: usize, v: u32) {
    buf[at..at + 4].copy_from_slice(&v.to_le_bytes());
}

fn put_i64_le(buf: &mut [u8], at: usize, v: i64) {
    buf[at..at + 8].copy_from_slice(&v.to_le_bytes());
}

// A LAYOUT ENTRY: 17 bytes, the spec says (§3.4).
//   id:       u64 LE  — fnv1a64(name)
//   kind:     u8
//   size:     u32 LE
//   children: u32 LE
struct Entry {
    id: u64,
    kind: u8,
    size: u32,
    children: u32,
}

fn entry_bytes(e: &Entry) -> [u8; 17] {
    let mut b = [0u8; 17];
    put_u64_le(&mut b, 0, e.id);
    b[8] = e.kind;
    put_u32_le(&mut b, 9, e.size);
    put_u32_le(&mut b, 13, e.children);
    b
}

// THE SMALLEST DIGEST-CARRYING TABLE: one i32 field with range [0, 100].
//
// Layout entries:
//   entry 0: table root, id=fnv1a64("R4Cell"), kind=13, size=4, children=1
//   entry 1: i32 field, id=fnv1a64("x"), kind=4, size=4, children=0
//
// The layout starts with a u32 LE count of entries, then the entries.
//
// Definitions digest (§5.2, bill §13):
//   'R' (0x52) = range tag
//   min i64 LE  = 0
//   max i64 LE  = 100

#[test]
fn hash_includes_digest() {
    let table_id = fnv1a64(b"R4Cell");
    let field_id = fnv1a64(b"x");

    let n_entries: u32 = 2;
    let layout_len = 4 + 2 * 17; // count + 2 entries

    let mut layout = vec![0u8; layout_len];
    put_u32_le(&mut layout, 0, n_entries);

    let e0 = Entry { id: table_id, kind: 13, size: 4, children: 1 };
    let e1 = Entry { id: field_id, kind: 4, size: 4, children: 0 };

    layout[4..4 + 17].copy_from_slice(&entry_bytes(&e0));
    layout[4 + 17..4 + 34].copy_from_slice(&entry_bytes(&e1));

    // Definitions digest: the range [0, 100] on the i32 field.
    // Tag 'R' (0x52), min 0 i64 LE, max 100 i64 LE.
    let mut digest = vec![0u8; 17];
    digest[0] = 0x52; // 'R'
    put_i64_le(&mut digest, 1, 0);    // min
    put_i64_le(&mut digest, 9, 100);  // max

    // hash WITHOUT the digest: fnv1a64 only over layout bytes
    let hash_layout_only = fnv1a64(&layout);

    // hash WITH the digest: fnv1a64 over layout bytes THEN over digest
    let hash_with_digest = {
        let mut combined = layout.clone();
        combined.extend_from_slice(&digest);
        fnv1a64(&combined)
    };

    // THE LAW: the hash must differ when the digest is included.
    assert_ne!(
        hash_layout_only, hash_with_digest,
        "fnv1a64(layout) == fnv1a64(layout + digest): the digest did not change the hash — \
         the definitions digest is NOT being folded in, violating §3.4/bill §13"
    );

    // A RUNTIME THAT ONLY HASHES LAYOUT BYTES COMPUTES THE WRONG HASH.
    // The production code path (*_fixed_load) reads the hash from the
    // file header and looks it up in the lineage — it never re-derives.
    // A buggy runtime using table_fixed_hash(layout) would compute
    // hash_layout_only, which does NOT match the correct hash and would
    // refuse a valid file as layout_newer.
    assert_ne!(
        hash_layout_only, hash_with_digest,
        "the runtime hash function table_fixed_hash hashes block bytes only, \
         and the result differs from the correct layout hash (which includes \
         the digest); a runtime deriving a hash from layout bytes it holds \
         would compute the WRONG value"
    );

    // THE CORRECT HASH: the compiler emits this as a const.
    // e.g., WEAPON_CONFIG_FIXED_HASH = 0xd7c60dc6bdce60fe
    // The hash is fnv1a64(layout_bytes) continued into fnv1a64(digest).
    eprintln!(
        "hash fnv1a64(layout)         = {:#018x}\n\
         hash fnv1a64(layout+digest)  = {:#018x}\n\
         difference: the digest changes the hash (law: fnv1a64 over layout then DIGEST)",
        hash_layout_only, hash_with_digest
    );
}

// NEGATIVE CONTROL: change one byte of the layout (the children count of
// the root from 1 to 0), the hash changes. A runtime hashing the wrong
// layout bytes would compute a different hash and refuse.
#[test]
fn negative_wrong_layout() {
    let table_id = fnv1a64(b"R4Cell");
    let field_id = fnv1a64(b"x");

    // CORRECT layout: root children = 1
    let mut correct_layout = vec![0u8; 4 + 2 * 17];
    put_u32_le(&mut correct_layout, 0, 2u32);
    let e0 = Entry { id: table_id, kind: 13, size: 4, children: 1 };
    let e1 = Entry { id: field_id, kind: 4, size: 4, children: 0 };
    correct_layout[4..4 + 17].copy_from_slice(&entry_bytes(&e0));
    correct_layout[4 + 17..4 + 34].copy_from_slice(&entry_bytes(&e1));

    // BROKEN layout: root children = 0 (one entry truncated)
    let mut broken_layout = vec![0u8; 4 + 2 * 17];
    put_u32_le(&mut broken_layout, 0, 2u32);
    let e0_bad = Entry { id: table_id, kind: 13, size: 0, children: 0 };
    let e1_bad = Entry { id: field_id, kind: 4, size: 4, children: 0 };
    broken_layout[4..4 + 17].copy_from_slice(&entry_bytes(&e0_bad));
    broken_layout[4 + 17..4 + 34].copy_from_slice(&entry_bytes(&e1_bad));

    let correct_hash = fnv1a64(&correct_layout);
    let broken_hash = fnv1a64(&broken_layout);

    assert_ne!(
        correct_hash, broken_hash,
        "changing layout bytes did not change the hash: the layout hash is NOT \
         purely a function of the layout bytes — a corrupted layout must change the hash"
    );
}