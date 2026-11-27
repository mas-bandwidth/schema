// C13: present flag byte != 0 — the C++ conformance guard for schema matrix
// cell cpp/C13.
//
// Law (docs/FIXED-FORM-ALGORITHM.md:453, §4.5 fix 2, 2026-11-03; docs/rulings.json
// strict-only-read): a `bool` or a PRESENT FLAG byte of 0 or 1 lands as false or
// true, and any other byte refuses the whole load `wire_damaged`; a present byte
// of 0 makes its payload DEAD, and a non-zero dead byte refuses wire_damaged.
// The row once asserted normalisation to true (the retired law). The same op (kTableFixedBool, op 8)
// handles both the scalar `bool` and the optional's `_present` companion —
// the cell cpp/C12 covers the scalar side, this row covers the present-flag
// side that sits in front of every optional payload.
//
// This test uses HB.schema's `Hostile` fixed-form record (test/tables/HB.schema:
// flag bool, link ?Pay, trail uint32 = 2), constructs a valid one-record file
// with `link_present` set, forges the PRESENT BYTE in the body to 0x02 (and
// 0xFF, and 0x00), and reads it back through the generated C++ fixed-form
// reader. The assertions: 1 and 0 (over a zero payload) land; 0x02, 0xFF and a 0
// over a non-zero payload refuse the whole load wire_damaged and land nothing.
//
// HOW THE PRESENT BYTE IS READ: as an INTEGER via memcpy, never as a bool
// load, so the fixture never relies on the C++ conversion to coerce a
// non-zero into one. That is how the case sits inside the sanitized target
// and still reports.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "HB.h"
#include "HBTable.h"

namespace {

int failures = 0;

void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
    else       { std::printf( "PASS: %s\n", what ); }
}

// The one byte of a member, read as an INTEGER. Never a load of the member.
uint8_t byte_of( const void * p )
{
    uint8_t v = 0;
    std::memcpy( &v, p, 1 );
    return v;
}

// Build a valid one-record fixed-form file for hb::Hostile with link_present
// SET, so the present byte is in the file under the reader's law, and return
// the offset into the file where the RECORD BODY begins, so the caller can
// forge exactly the present byte without touching the header or the layout.
std::vector<uint8_t> one_present( int32_t & body_at )
{
    hb::Hostile h;
    hb::HostileReset( h );
    h.flag = true;
    h.link_present = true;        // the present flag we will forge
    h.link.n = 42;
    h.trail = 7;

    const int64_t file_size = hb::HostileFixedMeasure( 1 );
    std::vector<uint8_t> out( (size_t) file_size );
    check( hb::HostileFixedSave( &h, 1, out.data(), file_size ) == file_size,
           "one_present: the lawful record saves" );

    // The record body offset: header (16) + layout-size u32 (4) + layout bytes + record hash (8).
    body_at = (int32_t) ( hb::kTableFixedHeaderBytes
                        + 4
                        + (size_t) hb::HostileFixedLayoutBytes
                        + 8u );
    return out;
}

// The link_present bool sits at body src offset 1 (see HostileFixedPlan entry
// `link present`: { 1u, 8u, 1u, kTableFixedBool, ... }), immediately after
// the scalar `flag` at body src offset 0.
constexpr size_t kPresentSrcOffset = 1;
// The link payload (Pay, an int32) sits at body src offset 2, behind the present byte.
constexpr size_t kPayloadSrcOffset = 2;

// a refused load lands nothing: the generated FixedLoad leaves the row at its
// declared defaults (R4), never the wire's values or the poison
bool at_defaults( const hb::Hostile & back )
{
    hb::Hostile fresh;
    hb::HostileReset( fresh );
    return byte_of( &back.flag ) == byte_of( &fresh.flag )
        && byte_of( &back.link_present ) == byte_of( &fresh.link_present )
        && back.trail == fresh.trail;
}

// strict-only-read: the forged cases once asserted "the record still reads, the
// present byte lands as 1, the neighbours land, the counters stay 0". The law
// moved (docs/FIXED-FORM-ALGORITHM.md:453, §4.5 fix 2): a present byte other
// than 0 or 1 refuses the whole load wire_damaged; and a present byte of 0
// makes the payload under it DEAD, where a non-zero byte refuses wire_damaged
// too. test/conformance/go/rows/C13_test.go holds the same law.
void refused_case( size_t at, uint8_t forged, const char * refuses, const char * nothing_lands )
{
    int32_t body = 0;
    std::vector<uint8_t> file = one_present( body );
    file[ body + at ] = forged;

    hb::Hostile back;
    hb::HostileReset( back );
    // Pre-poison so a load that left these standing would be caught.
    back.flag = false;
    back.trail = 0xA5A5A5A5u;
    back.link.n = 0;

    hb::TableReport r;
    std::vector<hb::TableFixedEntry> plan( 256 );
    const int64_t n = hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                            plan.data(), 256, NULL, &r );

    check( n < 0 && ( r.refused || r.damaged ) && r.refuse_reason == hb::wire_damaged, refuses );
    check( at_defaults( back ), nothing_lands );
}

// A FORGED PRESENT BYTE OF 2: the whole load refuses wire_damaged.
void present_byte_two_case()
{
    refused_case( kPresentSrcOffset, 2,
        "present_byte_two: a present byte of 2 refuses the whole load wire_damaged (C13)",
        "present_byte_two: a refused load lands nothing" );
}

// A PRESENT BYTE OF 1: the lawful present reads clean, every field from the wire.
void present_byte_one_case()
{
    int32_t body = 0;
    std::vector<uint8_t> file = one_present( body );

    hb::Hostile back;
    hb::HostileReset( back );
    back.flag = false;
    back.trail = 0xA5A5A5A5u;
    back.link.n = 0;

    hb::TableReport r;
    std::vector<hb::TableFixedEntry> plan( 256 );
    const int64_t n = hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                            plan.data(), 256, NULL, &r );

    check( n == 1 && !r.refused && !r.damaged && r.unknown == 0 && r.widened == 0,
           "present_byte_one: a present byte of 1 reads clean and counts nothing" );
    check( byte_of( &back.link_present ) == 1 && back.link_present == true,
           "present_byte_one: a present byte of 1 lands as true (1) (C13)" );
    check( byte_of( &back.flag ) == 1 && back.link.n == 42 && back.trail == 7,
           "present_byte_one: the flag, the payload and the trail land from the wire" );
}

// A PRESENT BYTE OF 0 OVER A ZERO PAYLOAD: lands as false (0), the lawful absent.
void present_byte_zero_case()
{
    int32_t body = 0;
    std::vector<uint8_t> file = one_present( body );

    // The lawful absent: the present byte 0 and its dead payload bytes zero, as
    // a writer lays them down.
    file[ body + kPresentSrcOffset ] = 0;
    std::memset( file.data() + body + kPayloadSrcOffset, 0, 4 );

    hb::Hostile back;
    hb::HostileReset( back );

    hb::TableReport r;
    std::vector<hb::TableFixedEntry> plan( 256 );
    const int64_t n = hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                            plan.data(), 256, NULL, &r );

    // strict-only-read: was "r.clamped == 0, an absent optional counts no
    // clamp"; the retired counter's zero is "not refused, not damaged".
    check( n == 1 && !r.refused && !r.damaged,
           "present_byte_zero: a present byte of 0 over a zero payload reads clean" );
    check( byte_of( &back.link_present ) == 0,
           "present_byte_zero: a present byte of 0 lands as false (0) (C13)" );
    check( back.link_present == false,
           "present_byte_zero: the C++ bool conversion reads 0 the same way (C13)" );
}

// A PRESENT BYTE OF 0 OVER THE OLD PAYLOAD: the dead bytes are non-zero, refused.
void present_byte_zero_dead_payload_case()
{
    refused_case( kPresentSrcOffset, 0,
        "present_byte_zero_dead: a present byte of 0 over a non-zero payload refuses wire_damaged (ALG §4.5:453)",
        "present_byte_zero_dead: a refused load lands nothing" );
}

// A PRESENT BYTE OF 0xFF: the maximal byte — refuses like 0x02.
void present_byte_ff_case()
{
    // strict-only-read: was "reads, lands 1, r.clamped == 0"; now a refusal.
    refused_case( kPresentSrcOffset, 0xFF,
        "present_byte_ff: a present byte of 0xFF refuses the whole load wire_damaged (C13)",
        "present_byte_ff: a refused load lands nothing" );
}

} // namespace

int main()
{
    present_byte_two_case();
    present_byte_one_case();
    present_byte_zero_case();
    present_byte_zero_dead_payload_case();
    present_byte_ff_case();

    if ( failures > 0 )
    {
        std::printf( "C13 cpp present flag byte not 0 or 1: %d FAILED\n", failures );
        return 1;
    }
    std::printf( "C13 cpp present flag byte not 0 or 1: refused wire_damaged; 0 and 1 land\n" );
    return 0;
}
