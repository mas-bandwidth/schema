// THE TWO WIRE BYTES A FIXED READER NORMALISES
// (docs/FIXED-FORM-ALGORITHM.md §4.5's last row; "Reference fixes pending" fix 2).
//
// A fixed record is a positional image and the read loop moves bytes. Two of
// those bytes are not integers: a `bool` and an OPTIONAL's PRESENT flag are
// `0` or `1` on the wire and nothing else, and §4.5 says a reader lands them
// as `byte != 0`, normalised to the language's own true — "0x02 is not a bool
// a reader stores verbatim".
//
// THE FIX IS IN. The C++ reference lands both through kTableFixedBool, so a
// forged 0x02 becomes a bool holding 1 (true) and never 2, on the identity plan
// and on a plan compiled from another writer's layout. The cases below are
// ordinary checks now; each was the RED witness that waited on fix 2, and each
// names the neighbouring bytes it pre-poisons so a normalise that spilled would
// be caught.
//
// HOW THIS FILE READS THE BYTE. It `memcpy`s the member's byte out and compares
// the INTEGER, so the fixture never LOADS a bool whose value would be UB. That
// is the whole of the trick, and it is how the case could sit inside the
// sanitized target and still report.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "HBTable.h"

namespace {

int failures = 0;

void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
}

// The one byte of a member, read as an INTEGER. Never a load of the member.
uint8_t byte_of( const void * p )
{
    uint8_t v = 0;
    std::memcpy( &v, p, 1 );
    return v;
}

// One lawful record, saved, with `at` pointing at the record BODY so a case can
// forge exactly one byte of it. The body offsets are the writer's, declared
// order: 0 flag, 1 link_present, 2..5 the payload, 6..9 trail.
std::vector<uint8_t> one_record( size_t & body_at )
{
    hb::Hostile v;
    hb::HostileReset( v );
    v.flag = true;
    v.link_present = true;
    v.link.n = 11;
    v.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> out( (size_t) hb::HostileFixedMeasure( 1 ) );
    check( hb::HostileFixedSave( &v, 1, out.data(), (int64_t) out.size() ) == (int64_t) out.size(),
           "hostile_bytes: the lawful record saves" );
    body_at = (size_t) hb::kTableFixedHeaderBytes + 4 + (size_t) hb::HostileFixedLayoutBytes + 8;
    return out;
}

// a bool byte of 2
void bool_byte_two_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_record( body );
    file[ body + 0 ] = 2;

    hb::Hostile back;
    hb::HostileReset( back );
    // PRE-POISON the neighbours: the bytes beside the normalised one must land
    // from the wire, neither keeping this stain nor taking the bool op's byte.
    back.link.n = 0x5A5A5A5A;
    back.trail = 0x5A5A5A5Au;
    hb::TableReport r;
    std::vector<hb::TableFixedEntry> plan( 256 );
    const int64_t n = hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                            plan.data(), 256, NULL, &r );
    check( n == 1 && !r.refused && !r.malformed,
           "bool_byte_two: a bool byte of 2 is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.flag ) == 1,
           "bool_byte_two: a bool byte of 2 lands as the language's own true (1), never verbatim (§4.5)" );
    check( back.link_present && back.link.n == 11 && back.trail == 0xBBBBBBBBu,
           "bool_byte_two: the fields beside the normalised byte land from the wire, untouched" );
}

// a present byte of 2
void present_byte_two_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_record( body );
    file[ body + 1 ] = 2;

    hb::Hostile back;
    hb::HostileReset( back );
    back.link.n = 0x5A5A5A5A;
    back.trail = 0x5A5A5A5Au;
    hb::TableReport r;
    std::vector<hb::TableFixedEntry> plan( 256 );
    const int64_t n = hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                            plan.data(), 256, NULL, &r );
    check( n == 1 && !r.refused && !r.malformed,
           "present_byte_two: a present byte of 2 is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.link_present ) == 1,
           "present_byte_two: an optional's present byte of 2 lands as the language's own true (1) (§4.5)" );
    check( back.trail == 0xBBBBBBBBu && back.link.n == 11,
           "present_byte_two: the payload and the trail beside it land from the wire, untouched" );
}

} // namespace

int main()
{
    bool_byte_two_case();
    present_byte_two_case();
    if ( failures > 0 )
    {
        std::printf( "fixed form hostile bytes (C++): %d FAILED\n", failures );
        return 1;
    }
    std::printf( "fixed form hostile bytes (C++): the bool and the present byte, both normalised\n" );
    return 0;
}
