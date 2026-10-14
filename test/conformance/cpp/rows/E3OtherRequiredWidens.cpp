// E3OtherRequiredWidens: the unsigned widen-ladder case — uint16 → uint32 zero-extends
// (docs/FIXED-FORM-ALGORITHM.md:742-745, the unsigned ladder 6..9).
//
// LAW (docs/FIXED-FORM-ALGORITHM.md:742-744):
// "Across the LADDER — te.kind != me.kind and TableFixedWidens holds — the source
// SIGN-extends when the WRITER's kind is signed, i8..i64 or a signed fixed(I,F),
// and zero-extends otherwise (the reference: fixedruntime.go:993, over 92-95)."
// "Take it as stated: ladder widen, sign by the WRITER's kind; same-kind widen
// and enum widen, ZERO."
//
// What this test asserts that R16 and R20 do NOT:
//   R16: int_widen (signed ladder, sign-extends), array_elem_widen, enum_width
//   R20: int_widen (signed ladder), float_widen (float rung)
//   THIS:  uint_widen (unsigned ladder, ZERO-extends) — the ladder widen when
//          the writer's kind is unsigned, proving zero-extension and not sign-extension.
//
// PRODUCTION PATH (named call site): the schema compiler emits
//   vnew_uint_widen::UintWidenFixedLoad (build/tables-generated/vnew_uint_widen/VNEW_uint_widenTable.h:4418),
//   which runs the plan's widen op (algorithm §4.1, op "widen") with sign=0
//   because the writer's kind is unsigned (kind 7 = u16 → kind 8 = u32).
//   The production caller is the generated reader's load function, reached
//   exactly as test/tables/versioning_numbers.cpp:uint_widen_case() reaches it
//   (versioning_numbers.cpp:604), and as the conformance driver reaches every
//   FixedLoad (test/conformance/cpp/main.cpp:981 via the VarCodec::load column).
//
// NEGATIVE CONTROL: forge the writer's uint16 value's HIGH byte from 0xFF to 0x80
//   (0xFFFF → 0x80FF) so that a sign-extension would produce 0xFFFFFFFF instead
//   of the correct zero-extended 0x000080FF. The assertion on the zero-extended
//   value goes RED while the test still compiles; restore the byte and it is GREEN.
//
// THE VECTOR IS THE GENERATED WRITER'S OWN OUTPUT, written by
//   vold_uint_widen::UintWidenFixedSave, and read by
//   vnew_uint_widen::UintWidenFixedLoad through the COMPILED plan.

#include <cstdint>
#include <cstdio>
#include <cstring>

#include "VOLD_uint_widenTable.h"
#include "VNEW_uint_widenTable.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s: %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { failures++; }
}

int main()
{
    // ---- 1. uint16 → uint32: the top of the old width, 0xFFFF, zero-extends ----
    // If a port sign-extends, it reads 0xFFFFFFFF. The law says zero-extend: 0x0000FFFF.
    {
        vold_uint_widen::UintWiden old;
        vold_uint_widen::UintWidenReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v    = 0xFFFFu; // the maximum uint16: sign-extension would make this -1
        old.trail = 0xBBBBBBBBu;

        uint8_t buf[ 256 ];
        int64_t len = vold_uint_widen::UintWidenFixedSave( &old, 1, buf, (int64_t) sizeof( buf ) );
        check( len > 0, "uint_widen: the OLD writer (uint16) saved one record" );

        vnew_uint_widen::UintWiden back;
        vnew_uint_widen::UintWidenReset( back );
        vnew_uint_widen::TableReport r{};
        vnew_uint_widen::TableFixedEntry plan[ 4096 ];
        int64_t n = vnew_uint_widen::UintWidenFixedLoad(
            &back, 1, buf, len, plan, 4096, NULL, &r );

        check( n == 1, "uint_widen: the NEW reader (uint32) returned one record" );
        check( back.v == 0x0000FFFFu,
               "uint_widen: 0xFFFF ZERO-extends to 0x0000FFFF (sign-extension would be 0xFFFFFFFF)" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "uint_widen: the lead and trail bracket fields landed exactly" );
        check( r.widened == 1, "uint_widen: widened == 1 (once per entry per record, unsigned ladder)" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0,
               "uint_widen: no other counter moved" );
        check( !r.refused && !r.malformed, "uint_widen: the read is not a refusal" );
    }

    // ---- 2. uint16 → uint32: 0x8000 (MSB set, NOT a sign bit in unsigned) ----
    // 0x8000 in uint16 is 32768. A sign-extension of the bit-pattern would give
    // 0xFFFFFFFF8000 (truncated to 0x8000 in uint32). Zero-extension gives 0x00008000.
    // Both happen to be the same in uint32, so this checks the value itself.
    {
        vold_uint_widen::UintWiden old;
        vold_uint_widen::UintWidenReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v    = 0x8000u; // 32768: in unsigned, this is a large positive value
        old.trail = 0xBBBBBBBBu;

        uint8_t buf[ 256 ];
        int64_t len = vold_uint_widen::UintWidenFixedSave( &old, 1, buf, (int64_t) sizeof( buf ) );
        check( len > 0, "uint_widen/0x8000: the OLD writer saved one record" );

        vnew_uint_widen::UintWiden back;
        vnew_uint_widen::UintWidenReset( back );
        vnew_uint_widen::TableReport r{};
        vnew_uint_widen::TableFixedEntry plan[ 4096 ];
        int64_t n = vnew_uint_widen::UintWidenFixedLoad(
            &back, 1, buf, len, plan, 4096, NULL, &r );

        check( n == 1, "uint_widen/0x8000: the NEW reader returned one record" );
        check( back.v == 0x00008000u,
               "uint_widen/0x8000: 32768 lands as 0x00008000 (not sign-extended)" );
        check( r.widened == 1, "uint_widen/0x8000: widened == 1" );
    }

    // ---- 3. uint16 → uint32: 0x0001 (small value, both extensions agree) ----
    {
        vold_uint_widen::UintWiden old;
        vold_uint_widen::UintWidenReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v    = 0x0001u;
        old.trail = 0xBBBBBBBBu;

        uint8_t buf[ 256 ];
        int64_t len = vold_uint_widen::UintWidenFixedSave( &old, 1, buf, (int64_t) sizeof( buf ) );
        check( len > 0, "uint_widen/0x0001: the OLD writer saved one record" );

        vnew_uint_widen::UintWiden back;
        vnew_uint_widen::UintWidenReset( back );
        vnew_uint_widen::TableReport r{};
        vnew_uint_widen::TableFixedEntry plan[ 4096 ];
        int64_t n = vnew_uint_widen::UintWidenFixedLoad(
            &back, 1, buf, len, plan, 4096, NULL, &r );

        check( n == 1, "uint_widen/0x0001: the NEW reader returned one record" );
        check( back.v == 0x00000001u,
               "uint_widen/0x0001: the small value lands exactly" );
        check( r.widened == 1, "uint_widen/0x0001: widened == 1" );
    }

    // ---- 4. Behavioural negative control: forge the uint16 high byte ----
    // Find the uint16 field in the wire (0xFFFF = {0xFF, 0xFF} little-endian).
    // Change it to {0xFF, 0x80} (0x80FF). A sign-extension of this pattern
    // as int16 would give 0xFFFFFFFF80FF → 0x80FFFFFF in uint32 (the upper
    // bits set because 0x80FF as int16 is -32513). Zero-extension gives 0x000080FF.
    // The assertion on 0x000080FF goes RED if the implementation sign-extends.
    {
        vold_uint_widen::UintWiden old;
        vold_uint_widen::UintWidenReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v    = 0xFFFFu;
        old.trail = 0xBBBBBBBBu;

        uint8_t buf[ 256 ];
        int64_t len = vold_uint_widen::UintWidenFixedSave( &old, 1, buf, (int64_t) sizeof( buf ) );
        check( len > 0, "uint_widen/control: the OLD writer saved one record" );

        // Find the uint16 value in the wire. The body layout is:
        //   lead (uint32, 4 bytes) at offset 0
        //   v (uint16, 2 bytes) at offset 4
        //   trail (uint32, 4 bytes) at offset 6
        // The body starts after: header(16) + layout_length(4) + layout + hash(8).
        // Rather than compute offsets, scan for the 0xAA 0xAA 0xAA 0xAA marker.
        bool forged = false;
        for ( int64_t i = 0; i + 6 <= len; ++i )
        {
            if ( buf[i] == 0xAA && buf[i+1] == 0xAA && buf[i+2] == 0xAA && buf[i+3] == 0xAA )
            {
                // Found lead. The uint16 value is at i+4 (little-endian: 0xFF, 0xFF).
                // Change the HIGH byte from 0xFF to 0x80 → value becomes 0x80FF.
                buf[i + 5] = 0x80;
                forged = true;
                break;
            }
        }
        check( forged, "uint_widen/control: found and forged the uint16 field in the wire" );

        if ( forged )
        {
            vnew_uint_widen::UintWiden back;
            vnew_uint_widen::UintWidenReset( back );
            vnew_uint_widen::TableReport r{};
            vnew_uint_widen::TableFixedEntry plan[ 4096 ];
            int64_t n = vnew_uint_widen::UintWidenFixedLoad(
                &back, 1, buf, len, plan, 4096, NULL, &r );

            check( n == 1, "uint_widen/control: the NEW reader returned one record" );
            check( back.v == 0x000080FFu,
                   "uint_widen/control: 0x80FF zero-extends to 0x000080FF "
                   "(sign-extension would give 0x80FFFFFF — a port that extends by "
                   "signedness reads the wrong value here)" );
            check( r.widened == 1, "uint_widen/control: widened == 1 on the forged value" );
        }
    }

    if ( failures != 0 ) { std::printf( "E3OtherRequiredWidens: %d assertion(s) failed\n", failures ); return 1; }
    std::printf( "E3OtherRequiredWidens: unsigned widen-ladder (uint16 → uint32 zero-extends) — green\n" );
    return 0;
}
