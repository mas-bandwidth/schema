// W2.cpp: absent optional skips store (docs/FIXED-FORM-ALGORITHM.md:204-206, cell cpp/W2).
//
// Law: "An absent optional writes flag 0 and skips the payload store: a
// declared default under a clear flag is meaning too (fix 13)."
//
// Reference implementation: test/tables/fixedform_main.cpp:1401 (absent_optional_case).
//
// The table tblp3::Chain carries an optional ?Link field. When link_present is
// false, ChainFixedSave writes flag 0 and skips storing the payload, leaving
// the body buffer prefilled with zeros. The test stains caller storage for
// link with 0x5A to prove that no stained bytes reach the wire.
//
// Discriminating half (negative control): When link_present is true with the same
// 0x5A stained payload, ChainFixedSave DOES write the payload to the wire,
// proving the check discriminates between absent and present optionals.

#include "P3Table.h"
#include <cstdio>
#include <cstring>
#include <vector>

static int failures = 0;

static void check( bool ok, const char * msg )
{
    std::printf( "%s: %s\n", ok ? "PASS" : "FAIL", msg );
    if ( !ok ) { failures++; }
}

int main()
{
    // --- 1. Absent optional case: link_present = false ---
    tblp3::Chain v;
    tblp3::ChainReset( v );
    std::strcpy( v.name, "absent" );
    v.name_length = 6;
    v.link_present = false;
    v.link.value = 0x5A5A5A;
    std::memset( v.link.tag, 0x5A, sizeof( v.link.tag ) );
    v.link.tag_length = 5;

    check( (uint8_t) v.link.tag[0] == 0x5Au,
           "CONTROL: the absent payload really is stained in storage" );

    std::vector<uint8_t> w( (size_t) tblp3::ChainFixedMeasure( 1 ) );
    check( tblp3::ChainFixedSave( &v, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(),
           "absent optional: the record saves" );

    const uint8_t * body = w.data() + tblp3::kTableFixedHeaderBytes + 4 + tblp3::ChainFixedLayoutBytes + 8;
    const size_t body_bytes = (size_t) tblp3::ChainFixedBodyBytes;

    check( std::memchr( body, 0x5A, body_bytes ) == NULL,
           "ABSENT OPTIONAL: not one byte of the absent payload reached the wire" );

    // And the reader reads what the flag says, with the payload at its defaults
    {
        tblp3::Chain back;
        tblp3::TableReport r;
        std::vector<tblp3::TableFixedEntry> plan( 1024 );
        check( tblp3::ChainFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "absent optional: the record reads" );
        check( !back.link_present, "absent optional: the flag" );
        check( back.link.value == 0 && back.link.tag_length == 0,
               "absent optional: the payload reads as the wire's zeros" );
        check( r.clamped == 0 && !r.malformed && !r.refused,
               "absent optional: a clean read moves no counter" );
    }

    // --- 2. The discriminating half: the same payload, PRESENT ---
    {
        tblp3::Chain present = v;
        present.link_present = true;
        present.link.tag_length = 4;
        std::vector<uint8_t> pw( (size_t) tblp3::ChainFixedMeasure( 1 ) );
        check( tblp3::ChainFixedSave( &present, 1, pw.data(), (int64_t) pw.size() ) == (int64_t) pw.size(),
               "absent optional: the present twin saves" );
        const uint8_t * pbody = pw.data() + tblp3::kTableFixedHeaderBytes + 4 + tblp3::ChainFixedLayoutBytes + 8;
        check( std::memchr( pbody, 0x5A, (size_t) tblp3::ChainFixedBodyBytes ) != NULL,
               "NEGATIVE CONTROL: the SAME payload PRESENT really does reach the wire" );
    }

    if ( failures > 0 ) {
        std::fprintf( stderr, "RED: %d failure(s)\n", failures );
        return 1;
    }
    std::printf( "PASS: absent optional skips store\n" );
    return 0;
}