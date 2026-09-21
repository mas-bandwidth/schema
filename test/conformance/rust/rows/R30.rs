// R30: compressed float rides as the float: min/max/resolution are definitions,
// the step is in the digest under 'Q', finer widens, coarser refuses by name,
// dropping the triple widens, fp-contract off on every leg.
//
// Law: docs/FIXED-FORM-ALGORITHM.md:583
//   "a FLOAT range's RESOLUTION | `'Q'`, then the step as an f64's IEEE-754 bits,
//    u64 LE, immediately after that range's `'R'` and bounds. This row is present
//    for every float range, including an uncompressed range whose absent resolution
//    is `0.0` (eight zero bytes). It is here because a compressed float **rides as
//    the float32** in this form (SPEC §3.4), so the step is nowhere in the layout
//    bytes: without this row `resolution = 0.01` and `= 0.1` hash identically and a
//    finer reader cannot refuse a coarsened peer."
//
// Test: Two schemas with the same float32 range [min = -1, max = 1] but different
// resolutions (0.01 vs 0.1) must produce different BUILD_VERSIONs (i.e., different
// definition digests), proving that the resolution is included in the digest under 'Q'.
//
// Uses existing test schemas:
//   test/tables/VNEW_cfloat_res_refine.schema: aim float32 | min = -1, max = 1, resolution = 0.01
//   test/tables/VOLD_cfloat_res_refine.schema: aim float32 | min = -1, max = 1, resolution = 0.1
//
// To run: rustc --edition 2021 --test \
//   -L build/tables-generated-rust \
//   --extern vnew_cfloat_res_refine=build/tables-generated-rust/vnew_cfloat_res_refine \
//   --extern vold_cfloat_res_refine=build/tables-generated-rust/vold_cfloat_res_refine \
//   test/conformance/rust/rows/R30.rs -o build/rows-rust-R30 && ./build/rows-rust-R30

use vnew_cfloat_res_refine::BUILD_VERSION as FINE_VERSION;
use vold_cfloat_res_refine::BUILD_VERSION as COARSE_VERSION;

#[test]
fn test_resolution_moves_the_hash() {
    // Two schemas with the same float32 range [min = -1, max = 1] but different
    // resolutions (0.01 vs 0.1) must have different BUILD_VERSIONs, proving the
    // resolution is in the digest under 'Q'.
    assert_ne!(
        FINE_VERSION,
        COARSE_VERSION,
        "resolution = 0.01 and resolution = 0.1 must produce different digests (different BUILD_VERSIONs)"
    );
}

fn main() {
    // Run the test
    test_resolution_moves_the_hash();
    println!("PASS: resolution moves the hash");
}
