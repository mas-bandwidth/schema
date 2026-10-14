// TextBytesFlagsDefaultsValidData.cpp — "String, byte-buffer and flags
// defaults: valid-data write/read acceptance" (cpp leg).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:1724, fix 1): the writer writes
// `length` units and `count` elements onto the zeroed template and stops.
// docs/SPEC-TABLES.md:733 — "string, bytes and flags defaults are part of
// that wire's contract exactly." docs/FIXED-FORM-ALGORITHM.md §3: the prefill
// fills unwritten bytes with declared defaults; for the identity plan the fill
// list is EMPTY (fix 15), so the identity read pays no prefill.
//
// WHAT THIS TEST DOES: builds one Vessel record (tblw1, W1.schema's fixed
// table) with NON-DEFAULT values for all three default-carrying field kinds:
//   - name string(32) = "untitled"  -> "hello"
//   - tag  bytes(4)   = "ab"        -> { 0x01, 0x02, 0x03 }
//   - caps Caps       = { Jump, Fly } -> Crouch only
// saves it through the production writer (VesselFixedSave), reads it back
// through the production reader (VesselFixedLoad), and asserts every value
// round-trips with no refusal, no malformation, and no counters moved.
//
// NEGATIVE CONTROL (-DCONTROL_BREAK_CAPS): flips the caps byte on the wire
// from Crouch (0x02) to Jump|Crouch|Fly (0x07), so the assertion
// `back.caps == Caps_Crouch` goes RED, proving the assertion bites. Restoring
// the byte returns GREEN.
//
// PRODUCTION PATH: tblw1::VesselFixedSave / tblw1::VesselFixedLoad, emitted
// by internal/codegen/cpptable from W1.schema's fixed table Vessel. The
// save/load path is the same one the conformance driver reaches through
// build/conformance-cpp.
//
// Run from ./repo:
//   c++ -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
//     -Ibuild/tables-generated/w1
//     test/conformance/cpp/rows/TextBytesFlagsDefaultsValidData.cpp
//     -o build/rows-cpp-TextBytesFlagsDefaultsValidData
//     && ./build/rows-cpp-TextBytesFlagsDefaultsValidData
// Exit 0 green, exit 1 red; one printed line per assertion.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "W1Table.h"

namespace {

int failures = 0;

void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
    else       { std::printf( "PASS: %s\n", what ); }
}

} // namespace

int main()
{
    // ---- build a Vessel with NON-DEFAULT values for all three default kinds ----
    tblw1::Vessel v;
    tblw1::VesselReset( v ); // start from declared defaults, then overwrite

    // string(32) default is "untitled" (8 chars); write "hello" (5 chars)
    std::memset( v.name, 0, sizeof( v.name ) );
    std::memcpy( v.name, "hello", 5 );
    v.name_length = 5;

    // bytes(4) default is "ab" (0x61,0x62); write {0x01,0x02,0x03} (3 bytes)
    std::memset( v.tag, 0, sizeof( v.tag ) );
    v.tag[0] = 0x01;
    v.tag[1] = 0x02;
    v.tag[2] = 0x03;
    v.tag_length = 3;

    // flags Caps default is Jump|Fly (0x05); write Crouch only (0x02)
    v.caps = tblw1::Caps_Crouch;

    // hull default is 100; write 42
    v.hull = 42;

    // ---- save ----
    const int64_t file_size = tblw1::VesselFixedMeasure( 1 );
    check( file_size > 0, "the measure returns a positive file size" );

    std::vector<uint8_t> wire( (size_t) file_size );
    const int64_t saved = tblw1::VesselFixedSave( &v, 1, wire.data(), (int64_t) wire.size() );
    check( saved == file_size, "the one-record form-3 file saves through the production writer" );

#ifdef CONTROL_BREAK_CAPS
    // THE NEGATIVE CONTROL: flip the caps byte from Crouch (0x02) to
    // Jump|Crouch|Fly (0x07). The caps field is a uint64 at body offset 44.
    // Body starts at kTableFixedHeaderBytes + 4 + VesselFixedLayoutBytes + 8.
    {
        const size_t body_at = (size_t) tblw1::kTableFixedHeaderBytes + 4
                             + (size_t) tblw1::VesselFixedLayoutBytes + 8;
        wire[body_at + 44] = 0x07; // Caps_Jump | Caps_Crouch | Caps_Fly
    }
#endif

    // ---- load (identity plan) ----
    {
        tblw1::Vessel back;
        tblw1::VesselReset( back ); // poison the destination with declared defaults
        tblw1::TableReport r;
        std::vector<tblw1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblw1::VesselFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );

        check( n == 1 && !r.refused && !r.malformed,
               "the identity read accepts the record (no refusal, no malformed)" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0,
               "no counters moved on a clean read" );

        // string: the written value lands exactly
        check( back.name_length == 5 && std::memcmp( back.name, "hello", 5 ) == 0 && back.name[5] == 0,
               "string(32): non-default 'hello' reads back, terminated at used length" );

        // bytes: the written value lands exactly
        check( back.tag_length == 3 && back.tag[0] == 0x01 && back.tag[1] == 0x02 && back.tag[2] == 0x03,
               "bytes(4): non-default {01,02,03} reads back at the written length" );

        // flags: the written mask lands exactly
        check( back.caps == tblw1::Caps_Crouch,
               "flags Caps: non-default Crouch-only reads back (not the declared Jump|Fly)" );

        // scalar: the written value lands
        check( back.hull == 42,
               "int32 hull: non-default 42 reads back" );

        // badge: the nested type's default (label = "new") lands from the zeroed template
        check( back.badge.label_length == 3 && std::memcmp( back.badge.label, "new", 3 ) == 0,
               "nested Badge: its declared default 'new' lands from the zeroed template" );
    }

    // ---- the title assertion ----
    if ( failures == 0 )
    {
        std::printf( "String, byte-buffer and flags defaults: valid-data write/read acceptance\n" );
    }
    else
    {
        std::printf( "TextBytesFlagsDefaultsValidData: %d assertion(s) failed\n", failures );
    }
    return failures == 0 ? 0 : 1;
}
