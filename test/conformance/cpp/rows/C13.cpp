// C13: present flag byte != 0 — the C++ conformance guard for schema matrix
// cell cpp/C13.
//
// Law (docs/FIXED-FORM-ALGORITHM.md:337): "a `bool` or a PRESENT FLAG lands as
// `byte != 0`, normalised to the language's own true. `0x02` is not a bool a
// reader stores verbatim (fix 2)." The same op (kTableFixedBool, op 8)
// handles both the scalar `bool` and the optional's `_present` companion —
// the cell cpp/C12 covers the scalar side, this row covers the present-flag
// side that sits in front of every optional payload.
//
// This test uses HB.schema's `Hostile` fixed-form record (test/tables/HB.schema:
// flag bool, link ?Pay, trail uint32 = 2), constructs a valid one-record file
// with `link_present` set, forges the PRESENT BYTE in the body to 0x02 (and
// 0xFF, and 0x00), and reads it back through the generated C++ fixed-form
// reader. The assertion is that the present byte lands as byte != 0 (the
// language's own true / 1) and as 0 (false), without moving any of the six
// §4 counters — a forged present byte is a CONTENT fact, not a read error.
//
// Negative control: change the forged byte back to 1 (the lawful value) and
// the assertion still passes; change the kTableFixedBool op to a plain
// memcpy (which lands 0x02 in the storage byte) and the assertion FAILS:
// the byte reads back as 2, not 1 (fix 2).
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

// A FORGED PRESENT BYTE OF 2: the record reads, and the present land is 1.
void present_byte_two_case()
{
    int32_t body = 0;
    std::vector<uint8_t> file = one_present( body );

    // Forge the present byte to 0x02 — a content fact, not a damage event.
    file[ body + kPresentSrcOffset ] = 2;

    hb::Hostile back;
    hb::HostileReset( back );

    // Pre-poison the neighbours so a normalisation that mislaid storage would be caught.
    back.flag = false;
    back.trail = 0xA5A5A5A5u;
    back.link.n = 0;

    hb::TableReport r;
    std::vector<hb::TableFixedEntry> plan( 256 );
    const int64_t n = hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                            plan.data(), 256, NULL, &r );

    check( n == 1 && !r.refused && !r.malformed,
           "present_byte_two: a present byte of 2 is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.link_present ) == 1,
           "present_byte_two: a present byte of 2 lands as the language's own true (1), never verbatim (C13)" );
    check( back.link_present == true,
           "present_byte_two: the C++ bool conversion reads 1 the same way (C13)" );
    check( byte_of( &back.flag ) == 1,
           "present_byte_two: the scalar `flag` beside the forged present byte lands from the wire, untouched" );
    check( back.link.n == 42,
           "present_byte_two: the payload behind the normalised present byte lands from the wire, untouched" );
    check( back.trail == 7,
           "present_byte_two: the int after the optional lands from the wire, untouched" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0,
           "present_byte_two: the five §4 counters stay exactly zero — a forged present byte is not damage (C13)" );
}

// A PRESENT BYTE OF 0: lands as false (0), the other side of the normalisation.
void present_byte_zero_case()
{
    int32_t body = 0;
    std::vector<uint8_t> file = one_present( body );

    // Forge the present byte to 0x00 — should land as false; the payload bytes
    // are still on the wire but no present op is held by them.
    file[ body + kPresentSrcOffset ] = 0;

    hb::Hostile back;
    hb::HostileReset( back );

    hb::TableReport r;
    std::vector<hb::TableFixedEntry> plan( 256 );
    const int64_t n = hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                            plan.data(), 256, NULL, &r );

    check( n == 1 && !r.refused && !r.malformed,
           "present_byte_zero: a present byte of 0 is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.link_present ) == 0,
           "present_byte_zero: a present byte of 0 lands as false (0) (C13)" );
    check( back.link_present == false,
           "present_byte_zero: the C++ bool conversion reads 0 the same way (C13)" );
    check( r.clamped == 0,
           "present_byte_zero: an absent optional counts no clamp — the payload is loose by rule (C13)" );
}

// A PRESENT BYTE OF 0xFF: the maximal non-zero byte — must still normalise to 1.
void present_byte_ff_case()
{
    int32_t body = 0;
    std::vector<uint8_t> file = one_present( body );

    // Forge the present byte to 0xFF — should still land as true (1).
    file[ body + kPresentSrcOffset ] = 0xFF;

    hb::Hostile back;
    hb::HostileReset( back );

    hb::TableReport r;
    std::vector<hb::TableFixedEntry> plan( 256 );
    const int64_t n = hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                            plan.data(), 256, NULL, &r );

    check( n == 1 && !r.refused && !r.malformed,
           "present_byte_ff: a present byte of 0xFF is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.link_present ) == 1,
           "present_byte_ff: a present byte of 0xFF lands as the language's own true (1), never 0xFF (C13)" );
    check( r.clamped == 0,
           "present_byte_ff: a forged present byte is not a clamp (C13)" );
}

} // namespace

int main()
{
    present_byte_two_case();
    present_byte_zero_case();
    present_byte_ff_case();

    if ( failures > 0 )
    {
        std::printf( "C13 cpp present flag byte != 0: %d FAILED\n", failures );
        return 1;
    }
    std::printf( "C13 cpp present flag byte != 0: the present byte normalised to byte != 0 on the identity plan\n" );
    return 0;
}
