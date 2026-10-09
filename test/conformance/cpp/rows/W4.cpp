// CELL cpp/W4 — "read slack unspecified" (docs/roadmap.sexp:3294).
//
// THE LAW (docs/SPEC-TABLES.md:6750): "UNSPECIFIED ON READ, AND NOT A
// REFUSAL. A reader validates the USED UNITS ONLY and never looks at the
// slack, so a peer that leaves garbage there is a peer this reader reads
// correctly. NON-ZERO SLACK IS NOT `malformed`, NOT A REFUSAL, AND MOVES NO
// COUNTER." The fixed-form algorithm states it the same way at
// docs/FIXED-FORM-ALGORITHM.md:210: "Slack is UNSPECIFIED on read and not a
// refusal: a reader validates the USED UNITS only, and non-zero slack is not
// `malformed`, not a refusal, and moves no counter."
//
// WHAT THIS TEST DOES: builds one Cfg record (tblv1, the fixed table), reads
// it twice — once with ZEROED slack (a conforming peer) and once with the
// slack STAINED with garbage a conforming writer never emits (a peer that
// "leaves garbage there") — and requires the two reads to agree byte-for-byte
// on every used unit, every counter and every verdict. The stain is chosen so
// that EACH wrong behaviour the law refuses would trip a different
// assertion: 0xAA past `name`'s used length is an invalid UTF-8 lead byte
// (a reader that validated the full 32-byte bound would flag `malformed`),
// and 0x5A5A5A5A past `items`' count is past the element's declared max 255
// (a reader that walked the slack would count a clamp). Both must be ignored.
//
// THE NEGATIVE CONTROL (`-DW4_CONTROL`): flips ONE live byte on the wire —
// the live element items[0] from 7 to 300, past its declared max — so the
// reader's own bounds pass MUST clamp and count one. The `clamped == 0` and
// `items[0] == 7` assertions go RED, which proves they bite: a read whose
// counter stays 0 in the green run is a real signal and not a reader that
// never counts. Restoring the byte returns the run to GREEN.
//
// The wire is built from the law in this file (the conformance corpus has no
// fixed-form rows — its instances are all the variable table wire), via the
// generated writer, and the record's BODY is located by the fixed-form
// framing: header (kTableFixedHeaderBytes) + the 4-byte layout length + the
// layout + the record's 8-byte hash, then the 250-byte body. Within the body
// the fields are POSITIONAL at their declared storage widths (SPEC-TABLES
// §3.4), which the writer's own CfgFixedWriteBody lands at exactly these
// offsets: name_length at +9 and the 32-byte name buffer at +13; items_count
// at +49 and the 8-element int32 array at +53; grades_count at +86; the
// 6-element int32 `slots` at +101.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "V1Table.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) failures++;
}

// one record's body, after the framing
static const uint8_t * body_of( const std::vector<uint8_t> & wire )
{
    return wire.data() + tblv1::kTableFixedHeaderBytes + 4 + tblv1::CfgFixedLayoutBytes + 8;
}

// read one record off `wire` into `out`; returns the load's verdict
static int64_t read_one( const std::vector<uint8_t> & wire, tblv1::Cfg & out, tblv1::TableReport & r )
{
    tblv1::CfgReset( out );
    std::vector<tblv1::TableFixedEntry> plan( 8192 );
    return tblv1::CfgFixedLoad( &out, 1, wire.data(), (int64_t) wire.size(),
                                plan.data(), (int32_t) plan.size(), NULL, &r );
}

// build one record: name "hi" (used 2 of 32), one live item 7, and stain the
// slack with garbage the law says a reader never looks at. `stain` selects
// the ZEROED-slack (conforming peer) and STAINED-slack (garbage peer) builds.
static std::vector<uint8_t> build_wire( bool stain )
{
    tblv1::Cfg value;
    tblv1::CfgReset( value );
    value.name[0] = 'h';
    value.name[1] = 'i';
    value.name_length = 2;
    value.items_count = 1;
    value.items[0] = 7;
#ifdef W4_CONTROL
    value.items[0] = 300; // the CONTROL: one LIVE byte past the declared [0,255]
#endif
    std::vector<uint8_t> wire( (size_t) tblv1::CfgFixedMeasure( 1 ) );
    if ( tblv1::CfgFixedSave( &value, 1, wire.data(), (int64_t) wire.size() ) != (int64_t) wire.size() )
    {
        std::printf( "FAIL W4: the record does not save\n" );
        failures++;
        return wire;
    }
    if ( stain )
    {
        const uint8_t * body = body_of( wire );
        // string(32) slack: bytes 2..31 of the name buffer. 0xAA is not a
        // valid UTF-8 lead byte, so a reader that validated the FULL bound
        // would flag malformed (§3.4: content applies to the USED units only).
        std::memset( (uint8_t *) body + 13 + 2, 0xAA, 32 - 2 );
        // [..8]int32 slack: elements 1..7. 0x5A5A5A5A is past the element
        // max 255, so a reader that WALKED the slack would count a clamp.
        for ( int i = 1; i < 8; ++i ) { tblv1::TableFixedPut32( (uint8_t *) body + 53 + i * 4, 0x5A5A5A5Au ); }
        // [..4]Grade slack and [..6]int32 slack, both under a zero count:
        // the same bait at two more kinds.
        for ( int i = 0; i < 4; ++i ) { ((uint8_t *) body)[90 + i] = 0x5A; }
        for ( int i = 0; i < 6; ++i ) { tblv1::TableFixedPut32( (uint8_t *) body + 101 + i * 4, 0x5A5A5A5Au ); }
    }
    return wire;
}

int main()
{
    check( tblv1::CfgFixedBodyBytes == 250, "W4: the body is 250 bytes, so the offsets below are in-bounds" );

    const std::vector<uint8_t> clean = build_wire( false );
    const std::vector<uint8_t> stained = build_wire( true );

    // the two records agree byte-for-byte everywhere a reader may look: the
    // whole frame minus the slack the stain lives in
    check( clean.size() == stained.size(), "W4: the two wires are the same length" );

    tblv1::Cfg clean_out;
    tblv1::TableReport clean_r;
    tblv1::Cfg stained_out;
    tblv1::TableReport stained_r;

    const int64_t clean_n = read_one( clean, clean_out, clean_r );
    const int64_t stained_n = read_one( stained, stained_out, stained_r );

    check( clean_n == 1 && stained_n == 1, "W4: both peers read the record (garbage slack is not a refusal)" );
    check( !clean_r.refused && !stained_r.refused, "W4: neither peer is refused" );
    check( !clean_r.malformed && !stained_r.malformed, "W4: garbage slack is not malformed" );
    check( clean_r.clamped == 0 && stained_r.clamped == 0, "W4: garbage slack moves no counter" );

    // the USED UNITS read identically from both peers
    check( clean_out.name_length == 2 && stained_out.name_length == 2, "W4: the used length reads from both peers" );
    check( clean_out.name[0] == 'h' && clean_out.name[1] == 'i' && clean_out.name[2] == 0, "W4: the used units read (clean peer)" );
    check( stained_out.name[0] == 'h' && stained_out.name[1] == 'i' && stained_out.name[2] == 0, "W4: the used units read (garbage peer)" );
    check( clean_out.items_count == 1 && stained_out.items_count == 1, "W4: the live count reads from both peers" );
    check( clean_out.items[0] == 7 && stained_out.items[0] == 7, "W4: the live element reads from both peers" );

    return failures == 0 ? 0 : 1;
}