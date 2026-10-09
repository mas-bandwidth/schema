// CELL cpp/ScalarDefaultsValidData — "Scalar and enum defaults: valid-data
// write/read acceptance" (docs/roadmap.sexp: scalar-defaults/cpp/valid-data).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md §3.4, §4.3, fix 15): the writer
// zero-fills the record template before storing values (§3.1), so bytes a
// value does not overwrite carry zero on the wire. The reader's prefill
// (§4.3) writes declared defaults into bytes the plan does not cover; for
// the identity plan that set is EMPTY, so the identity read pays no prefill
// (fix 15) and defaults come from the struct's own member initializers.
//
// WHAT THIS TEST DOES: resets a scalardemo::SimState to its declared defaults
// (energy = -250, scale = 1.0, pose.x = 0.5, all else zero), writes it as a
// one-record fixed-form file, reads it back into a freshly poisoned struct,
// and asserts every default value survives the round trip. The Pose's x
// default (0.5 = 32768 in Q48.16) is checked as the enum/default half of
// the card's title — Pose.x is the one nested-type default with a non-zero
// value, and the scalar defaults (energy, scale) carry their own non-zero
// images on the wire.
//
// THE NEGATIVE CONTROL (`-DSCALAR_DEFAULTS_CONTROL`): flips the LOW BYTE of
// the scale field in the written wire body. scale is 65536 (0x00010000 LE),
// so the wire at body+114 is 00 00 01 00; flipping byte 0 to 0x01 makes it
// 65792, which reads back as scale != 65536. The assertion goes RED.
// Restoring the byte returns the run to GREEN. This byte is on the WIRE for
// a declared default, so a reader that did not restore defaults from the
// struct would see the wrong value.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "ScalarsTable.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) failures++;
}

int main()
{
    // ---- build the default-valued record ----

    scalardemo::SimState src;
    scalardemo::SimStateReset( src );

    // verify the defaults we are about to write
    check( src.energy == (serialize::int128_t) -250,
           "default energy is -250 as int128" );
    check( src.scale == 65536, "default scale is 65536 (1.0 in Q16.16)" );
    check( src.pose.x == 32768ll, "default pose.x is 32768 (0.5 in Q48.16)" );
    check( src.pose.y == 0, "default pose.y is 0" );
    check( src.pose.heading == 0, "default pose.heading is 0" );
    check( src.tilt == 0, "default tilt is 0" );
    check( src.angle == 0, "default angle is 0" );
    check( src.spawn_present == false, "spawn is absent by default" );

    std::vector<uint8_t> wire( (size_t) scalardemo::SimStateFixedMeasure( 1 ) );
    check( scalardemo::SimStateFixedSave( &src, 1, wire.data(), (int64_t) wire.size() )
               == (int64_t) wire.size(),
           "SimStateFixedSave writes the default record" );

    // ---- the control: deliberately corrupt one wire byte ----
    // body offset: header (16) + layout-length word (4) + layout (616) + record hash (8)
    const size_t body = (size_t) scalardemo::kTableFixedHeaderBytes + 4
                      + (size_t) scalardemo::SimStateFixedLayoutBytes + 8;
#ifdef SCALAR_DEFAULTS_CONTROL
    // Flip byte 0 of the scale field (body+114) from 0x00 to 0x01.
    // scale = 65536 (0x00010000 LE); corrupting the low byte to 0x01
    // gives 65792, which the round trip will read back as != 65536.
    wire[body + 114] ^= 0x01;
#endif

    // ---- poison the destination so a byte that reads zero was WRITTEN zero ----

    scalardemo::SimState dst;
    std::memset( (void *) &dst, 0xBB, sizeof( dst ) );

    scalardemo::TableReport report;
    std::vector<scalardemo::TableFixedEntry> plan( 1024 );
    const int64_t n = scalardemo::SimStateFixedLoad( &dst, 1, wire.data(), (int64_t) wire.size(),
                                                      plan.data(), 1024, NULL, &report );
    check( n == 1 && !report.refused && !report.malformed,
           "SimStateFixedLoad reads one record without refusal" );
    check( report.clamped == 0 && report.widened == 0 && report.unknown == 0,
           "no counter moves on a clean default round trip" );

    // ---- verify the defaults survived ----

    check( dst.energy == (serialize::int128_t) -250,
           "energy survives the round trip at -250" );
    check( dst.scale == 65536, "scale survives the round trip at 65536 (1.0 Q16.16)" );
    check( dst.pose.x == 32768ll, "pose.x survives the round trip at 32768 (0.5 Q48.16)" );
    check( dst.pose.y == 0, "pose.y survives the round trip at 0" );
    check( dst.pose.heading == 0, "pose.heading survives the round trip at 0" );
    check( dst.tilt == 0, "tilt survives the round trip at 0" );
    check( dst.angle == 0, "angle survives the round trip at 0" );
    check( dst.spawn_present == false, "spawn_present survives the round trip as false" );

    // ---- the wire image of the defaults is the right bytes ----

    // scale is at body+114 (SimStateFixedWriteBody: TableFixedPut32(b+114,...)),
    // 65536 in LE is 00 00 01 00
    check( wire[body + 114] == 0x00 && wire[body + 115] == 0x00
           && wire[body + 116] == 0x01 && wire[body + 117] == 0x00,
           "scale's wire image is 65536 in little-endian (00 00 01 00)" );

    // energy is at body+82 (16 bytes, LE int128). -250 as two's-complement
    // LE int128: lo = 0xFFFFFFFFFFFFFF06, hi = 0xFFFFFFFFFFFFFFFF
    // LE byte order: lo first. 0x06 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF
    check( wire[body + 82] == 0x06 && wire[body + 83] == 0xFF,
           "energy's low wire bytes are 0x06 0xFF (the -250 signature)" );

    return failures == 0 ? 0 : 1;
}
