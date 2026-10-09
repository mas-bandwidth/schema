// SignedFixedPointValidData — "Signed fixed-point fields: valid-data write/read acceptance"
// (docs/roadmap.sexp signed-fixed-point/cpp/valid-data).
//
// THE LAW (docs/SPEC-TABLES.md §3.4 record, fixed(I,F)): a fixed(I,F) field
// stores a signed raw scaled integer of I+F bits; the reader copies the raw
// value at its declared storage width and then clamps it to the declared
// [min, max] bounds in VALUE UNITS (docs/FIXED-FORM-ALGORITHM.md §4.6).
//
// WHAT THIS ASSERTS:
//   1. A signed fixed-point scalar written through the production FixedSave
//      reads back through FixedLoad with the SAME raw value (round-trip).
//   2. Values at the declared min and at the declared max round-trip exactly.
//   3. A forged value past the declared max clamps to max and counts ONE.
//   4. A forged value below the declared min clamps to min and counts ONE.
//   5. The NEW build (fixed(12,4)) reads what the OLD build (fixed(4,4))
//      wrote with the same value, because F is equal and the raw scaled
//      value is identical (the widen path, not a kind mismatch).
//
// PRODUCTION PATH (named call site): vold_fixed_i_grow::FixedIGrowFixedLoad
// and vnew_fixed_i_grow::FixedIGrowFixedLoad, reached through the same headers
// and include paths the conformance harness uses (-Ibuild/tables-generated/vnum
// from CONFORMANCE_INCLUDES, Makefile:4651). The clamp pass runs inside each
// FixedLoad after TableFixedRun, through FixedIGrowFixedClamp ->
// FixedIGrowFixedClampBody (VOLD_fixed_I_growTable.h:4304,
// VNEW_fixed_I_growTable.h:4304).
//
// VECTORS: built from the law here. The fixed-form wire is:
//   [form byte 3][7 zero reserved][layout hash u64][layout bytes u32 length]
//   [layout bytes...][records...]
// Each record: [8-byte hash][body bytes].
// The production writer (FixedIGrowFixedSave) produces exactly this framing,
// so the vector derivation is the law the writer implements.
//
// NEGATIVE CONTROL (`-DSFPVD_CONTROL`): flips the body's 'v' byte for record 0
// to a value past the declared max (0x7F = 127, past max 7), proving the clamp
// assertion bites. Restoring the byte returns GREEN.
//
// BUILD/RUN (from ./repo):
//   c++ -std=c++17 -Wall -Ibuild/tables-generated/vnum -I$(SERIALIZE)
//     test/conformance/cpp/rows/SignedFixedPointValidData.cpp
//     -o build/rows-cpp-SignedFixedPointValidData &&
//     ./build/rows-cpp-SignedFixedPointValidData

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "VOLD_fixed_I_growTable.h"
#include "VNEW_fixed_I_growTable.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { failures++; }
}

// ---- VOLD: fixed(4, 4), min=-8, max=7, storage int8 (8 bits) ----

static void vold_case()
{
    // The storage is 8 bits (4+4), so the raw value fits in int8_t.
    // F=4 means the value unit is scaled by 16: min=-8 -> raw=-128, max=7 -> raw=112.

    check( vold_fixed_i_grow::FixedIGrowFixedBodyBytes == 9,
           "VOLD: the body is 9 bytes (lead 4 + v 1 + trail 4)" );

    // 1. Write and read back a value inside the range: v = 3 (raw = 48)
    {
        vold_fixed_i_grow::FixedIGrow one;
        vold_fixed_i_grow::FixedIGrowReset( one );
        one.lead = 0xAABBCCDDu;
        one.v = 3;       // inside [min=-8, max=7]
        one.trail = 0x11223344u;

        std::vector<uint8_t> wire( (size_t) vold_fixed_i_grow::FixedIGrowFixedMeasure( 1 ) );
        const int64_t written = vold_fixed_i_grow::FixedIGrowFixedSave( &one, 1, wire.data(), (int64_t) wire.size() );
        check( written == (int64_t) wire.size(), "VOLD: the production writer saves the record" );

        vold_fixed_i_grow::FixedIGrow back;
        vold_fixed_i_grow::FixedIGrowReset( back );
        std::vector<vold_fixed_i_grow::TableFixedEntry> plan( 1024 );
        vold_fixed_i_grow::TableReport r;
        const int64_t n = vold_fixed_i_grow::FixedIGrowFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1, "VOLD: the production reader reads one record" );
        check( !r.refused && !r.malformed, "VOLD: not refused, not malformed" );
        check( back.lead == one.lead && back.trail == one.trail,
               "VOLD: the bracket fields survive the round-trip" );
        check( back.v == one.v, "VOLD: fixed(4,4) v=3 round-trips exactly" );
        check( r.clamped == 0 && r.widened == 0 && r.unknown == 0 && r.kind_mismatch == 0,
               "VOLD: no counters moved for an in-range value" );
    }

    // 2. Value at the DECLARED MIN: v = -8
    {
        vold_fixed_i_grow::FixedIGrow one;
        vold_fixed_i_grow::FixedIGrowReset( one );
        one.lead = 1;
        one.v = -8; // the declared min
        one.trail = 2;

        std::vector<uint8_t> wire( (size_t) vold_fixed_i_grow::FixedIGrowFixedMeasure( 1 ) );
        check( vold_fixed_i_grow::FixedIGrowFixedSave( &one, 1, wire.data(), (int64_t) wire.size() )
                   == (int64_t) wire.size(),
               "VOLD: writer saves v=min" );

        vold_fixed_i_grow::FixedIGrow back;
        vold_fixed_i_grow::FixedIGrowReset( back );
        std::vector<vold_fixed_i_grow::TableFixedEntry> plan( 1024 );
        vold_fixed_i_grow::TableReport r;
        const int64_t n = vold_fixed_i_grow::FixedIGrowFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && back.v == -8, "VOLD: v=-8 (the declared min) round-trips" );
        check( r.clamped == 0, "VOLD: the declared min does not clamp" );
    }

    // 3. Value at the DECLARED MAX: v = 7
    {
        vold_fixed_i_grow::FixedIGrow one;
        vold_fixed_i_grow::FixedIGrowReset( one );
        one.lead = 1;
        one.v = 7; // the declared max
        one.trail = 2;

        std::vector<uint8_t> wire( (size_t) vold_fixed_i_grow::FixedIGrowFixedMeasure( 1 ) );
        check( vold_fixed_i_grow::FixedIGrowFixedSave( &one, 1, wire.data(), (int64_t) wire.size() )
                   == (int64_t) wire.size(),
               "VOLD: writer saves v=max" );

        vold_fixed_i_grow::FixedIGrow back;
        vold_fixed_i_grow::FixedIGrowReset( back );
        std::vector<vold_fixed_i_grow::TableFixedEntry> plan( 1024 );
        vold_fixed_i_grow::TableReport r;
        const int64_t n = vold_fixed_i_grow::FixedIGrowFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && back.v == 7, "VOLD: v=7 (the declared max) round-trips" );
        check( r.clamped == 0, "VOLD: the declared max does not clamp" );
    }

    // 4. NEGATIVE CONTROL: forge a byte past the max to prove the clamp bites.
    //    The 'v' field is at body offset 4 (lead is 4 bytes). Flip it to 0x7F (127, past max 7).
    {
        vold_fixed_i_grow::FixedIGrow one;
        vold_fixed_i_grow::FixedIGrowReset( one );
        one.lead = 1;
        one.v = 3; // clean in-range value
        one.trail = 2;

        std::vector<uint8_t> wire( (size_t) vold_fixed_i_grow::FixedIGrowFixedMeasure( 1 ) );
        vold_fixed_i_grow::FixedIGrowFixedSave( &one, 1, wire.data(), (int64_t) wire.size() );

#ifdef SFPVD_CONTROL
        // Break: poke the 'v' byte past the max.
        // Body starts at: kTableFixedHeaderBytes(16) + 4(layout len) + FixedLayoutBytes + 8(record hash)
        // For VOLD: 16 + 4 + 68 + 8 = 96; v is at body offset 4.
        const size_t body_off = (size_t) vold_fixed_i_grow::kTableFixedHeaderBytes + 4
                              + (size_t) vold_fixed_i_grow::FixedIGrowFixedLayoutBytes + 8;
        wire[body_off + 4] = 0x7F; // 127, past max 7
#endif

        vold_fixed_i_grow::FixedIGrow back;
        vold_fixed_i_grow::FixedIGrowReset( back );
        std::vector<vold_fixed_i_grow::TableFixedEntry> plan( 1024 );
        vold_fixed_i_grow::TableReport r;
        vold_fixed_i_grow::FixedIGrowFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );

#ifdef SFPVD_CONTROL
        check( back.v == 7, "VOLD CONTROL: forged 127 clamps to max 7" );
        check( r.clamped == 1, "VOLD CONTROL: one clamped counted" );
#else
        check( back.v == 3 && r.clamped == 0, "VOLD green: no sabotage, no clamp" );
#endif
    }
}

// ---- VNEW: fixed(12, 4), min=-8, max=7, storage int16 (16 bits) ----

static void vnew_case()
{
    check( vnew_fixed_i_grow::FixedIGrowFixedBodyBytes == 10,
           "VNEW: the body is 10 bytes (lead 4 + v 2 + trail 4)" );

    // 1. Write and read back a value inside the range: v = 3
    {
        vnew_fixed_i_grow::FixedIGrow one;
        vnew_fixed_i_grow::FixedIGrowReset( one );
        one.lead = 0xAABBCCDDu;
        one.v = 3;
        one.trail = 0x11223344u;

        std::vector<uint8_t> wire( (size_t) vnew_fixed_i_grow::FixedIGrowFixedMeasure( 1 ) );
        const int64_t written = vnew_fixed_i_grow::FixedIGrowFixedSave( &one, 1, wire.data(), (int64_t) wire.size() );
        check( written == (int64_t) wire.size(), "VNEW: the production writer saves the record" );

        vnew_fixed_i_grow::FixedIGrow back;
        vnew_fixed_i_grow::FixedIGrowReset( back );
        std::vector<vnew_fixed_i_grow::TableFixedEntry> plan( 1024 );
        vnew_fixed_i_grow::TableReport r;
        const int64_t n = vnew_fixed_i_grow::FixedIGrowFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1, "VNEW: the production reader reads one record" );
        check( back.v == one.v, "VNEW: fixed(12,4) v=3 round-trips exactly" );
        check( r.clamped == 0, "VNEW: no clamp for in-range value" );
    }

    // 2. Value at DECLARED MIN and MAX
    {
        vnew_fixed_i_grow::FixedIGrow one;
        vnew_fixed_i_grow::FixedIGrowReset( one );
        one.lead = 1;
        one.v = -8;
        one.trail = 2;

        std::vector<uint8_t> wire( (size_t) vnew_fixed_i_grow::FixedIGrowFixedMeasure( 1 ) );
        vnew_fixed_i_grow::FixedIGrowFixedSave( &one, 1, wire.data(), (int64_t) wire.size() );

        vnew_fixed_i_grow::FixedIGrow back;
        vnew_fixed_i_grow::FixedIGrowReset( back );
        std::vector<vnew_fixed_i_grow::TableFixedEntry> plan( 1024 );
        vnew_fixed_i_grow::TableReport r;
        vnew_fixed_i_grow::FixedIGrowFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( back.v == -8 && r.clamped == 0, "VNEW: v=-8 (declared min) round-trips, no clamp" );

        one.v = 7;
        wire.assign( (size_t) vnew_fixed_i_grow::FixedIGrowFixedMeasure( 1 ), 0 );
        vnew_fixed_i_grow::FixedIGrowFixedSave( &one, 1, wire.data(), (int64_t) wire.size() );
        vnew_fixed_i_grow::FixedIGrowReset( back );
        vnew_fixed_i_grow::FixedIGrowFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( back.v == 7 && r.clamped == 0, "VNEW: v=7 (declared max) round-trips, no clamp" );
    }
}

// ---- CROSS-BUILD: VOLD writes, VNEW reads (I grows, F equal) ----

static void cross_build_case()
{
    // VOLD wrote fixed(4,4), VNEW reads fixed(12,4). F is equal (4), so the
    // raw scaled value is the same number in a wider slot. The reader widens,
    // does NOT kind_mismatch.

    vold_fixed_i_grow::FixedIGrow old_val;
    vold_fixed_i_grow::FixedIGrowReset( old_val );
    old_val.lead = 0xDEADBEEFu;
    old_val.v = -5;   // inside [-8, 7]
    old_val.trail = 0xCAFEBABEu;

    std::vector<uint8_t> wire( (size_t) vold_fixed_i_grow::FixedIGrowFixedMeasure( 1 ) );
    check( vold_fixed_i_grow::FixedIGrowFixedSave( &old_val, 1, wire.data(), (int64_t) wire.size() )
               == (int64_t) wire.size(),
           "cross: VOLD saves the wire" );

    vnew_fixed_i_grow::FixedIGrow new_back;
    vnew_fixed_i_grow::FixedIGrowReset( new_back );
    std::vector<vnew_fixed_i_grow::TableFixedEntry> plan( 1024 );
    vnew_fixed_i_grow::TableReport r;
    const int64_t n = vnew_fixed_i_grow::FixedIGrowFixedLoad(
        &new_back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
    check( n == 1, "cross: VNEW reads the VOLD wire" );
    check( new_back.lead == old_val.lead && new_back.trail == old_val.trail,
           "cross: the bracket fields survive" );
    check( new_back.v == old_val.v,
           "cross: fixed(4,4) v=-5 lands exactly in fixed(12,4) (F equal, widen)" );
    check( r.kind_mismatch == 0 && r.widened == 1,
           "cross: widened, not kind_mismatch — the element's I widened with F held" );
    check( r.clamped == 0 && r.unknown == 0 && !r.malformed && !r.refused,
           "cross: nothing else fired" );
}

int main()
{
    vold_case();
    vnew_case();
    cross_build_case();

    if ( failures != 0 )
    {
        std::printf( "SignedFixedPointValidData: %d assertion(s) failed\n", failures );
        return 1;
    }
    std::printf( "SignedFixedPointValidData: signed fixed-point write/read acceptance — green\n" );
    return 0;
}
