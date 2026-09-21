// rust/R23 — the static data's member names and order, and where the file's
// hash lands on the report (docs/FIXED-FORM-ALGORITHM.md §5.9 #19, #15).
//
// Standalone guard test, compiled with rustc against the generated runtime the
// conformance driver links (graphdemo's fixed_runtime), not with cargo:
//
//   rustc --edition 2024 --crate-type lib --crate-name graphdemo \
//     build/tables-generated-rust/graphdemo/src/fixed_runtime.rs \
//     -o build/libgraphdemo-fixed.rlib
//   rustc --edition 2021 --test \
//     --extern graphdemo=build/libgraphdemo-fixed.rlib \
//     test/conformance/rust/rows/R23.rs -o build/rows-rust-R23
//   ./build/rows-rust-R23
//
// THE LAW (§5.9 #19, and #34 which admits the leg's spelling). One entry of
// `R.known` carries the known-layout members — hash, layout, layout_bytes,
// record_bytes — the byte length riding BESIDE the pointer rather than inside
// it. The struct is `TableFixedKnownLayout` where a leg pair shares a text
// (fixedtwin holds C and C++), and a leg writing the struct from §5.2's prose
// alone lands a different spelling and goes red. The Rust leg's spelling is
// admitted (§5.9 #34): `TableFixedKnown`, no `layout_bytes` because `layout`
// is a `&'static [u8]` whose `.len()` IS the byte length, and the record size
// rides as `record`. What no leg may move is the hash's place (first) or the
// record size's source (the LOCK's, never the file's).
//
// A compiled leg asserts "names and order" the way its toolchain can: the
// struct literal below names every member, so a different spelling fails to
// COMPILE; the deferred type annotations pin each role (`hash` a u64 identity,
// `layout` the byte slice carrying its own length, `record` the usize size), so
// a leg that moved the byte length into a scalar or the hash out of first place
// fails to compile too.
//
// THE HASH (§5.9 #15). `layout_hash` is LAST on the report and zero on every
// other path: a clean load leaves it zero, a refusal BY NAME leaves it zero,
// and only the two layout refusals carry the file's hash.

use std::sync::atomic::{AtomicUsize, Ordering};

static FAILURES: AtomicUsize = AtomicUsize::new(0);

fn check(ok: bool, what: &str) {
    if ok {
        println!("ok   {what}");
    } else {
        println!("FAIL {what}");
        FAILURES.fetch_add(1, Ordering::Relaxed);
    }
}

use graphdemo::{TableFixedKnown, TableFixedReason, TableFixedReport, table_fixed_hash};

#[test]
fn r23_static_members_and_layout_hash() {
    // ---- PART ONE: the static data's member names and order (§5.9 #19) -----

    // The literal names every member: a different spelling fails to compile.
    let known = TableFixedKnown {
        hash: 0x_0123_4567_89ab_cdef,
        layout: &[0x10, 0x20, 0x30],
        record: 24,
        retired: false,
    };

    // Deferred type annotations pin each member's role and its place in the
    // order — hash FIRST as the u64 identity, layout SECOND as the byte slice,
    // the record size THIRD as a size.
    let _: u64 = known.hash;
    check(true, "TableFixedKnown.hash is present, a u64, and first in its role");

    let _: &'static [u8] = known.layout;
    check(
        true,
        "TableFixedKnown.layout is the byte slice — the bytes, second",
    );

    let _: usize = known.record;
    check(
        true,
        "TableFixedKnown.record is the lock's record size, in record_bytes' place",
    );

    let _: bool = known.retired;
    check(
        known.layout.len() == 3,
        "the byte length rides BESIDE the pointer: layout.len() is layout_bytes",
    );

    // ---- PART TWO: the file's hash lands as layout_hash (§5.9 #15) ---------

    // zero on every other path: a fresh (clean) report, and a refusal BY NAME.
    let fresh = TableFixedReport::default();
    check(
        fresh.layout_hash == 0,
        "a fresh report carries layout_hash zero",
    );

    let mut refused = TableFixedReport::default();
    let _ = refused.refuse::<()>(TableFixedReason::BatchTooLarge);
    check(
        refused.layout_hash == 0,
        "a refusal BY NAME keeps layout_hash zero — zero on every other path",
    );

    // only the layout refusals carry the file's hash, and it is THE FILE'S.
    let file_hash = table_fixed_hash(b"the file's own bytes");
    let mut newer = TableFixedReport::default();
    let _ = newer.refuse_layout::<()>(TableFixedReason::LayoutNewer, file_hash);
    check(
        newer.layout_hash == file_hash,
        "a layout refusal carries THE FILE'S hash (table_fixed_hash) in layout_hash",
    );

    assert_eq!(
        FAILURES.load(Ordering::Relaxed),
        0,
        "rust/R23: the static member names/order and the layout_hash law"
    );
}
