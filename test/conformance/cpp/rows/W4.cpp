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
//
// THE LAW MOVED (docs/FIXED-FORM-ALGORITHM.md:449 and §5.3 step 11; docs/
// rulings.json strict-only-read): the bytes past a text's used length and an
// array's unused slots are DEAD, and a non-zero dead byte refuses the whole
// load `wire_damaged` in the CHECK pass, every value handed in left at its
// declared defaults (R4). The row once asserted the garbage peer reads; it now
// asserts the refusal, as test/conformance/go/rows/W4_test.go does.
//
// WHAT THIS TEST DOES: builds one Cfg record (tblv1, the fixed table), reads
// it twice — once with ZEROED dead bytes (a conforming peer), which reads and
// lands its used units, and once with the dead bytes STAINED with garbage a
// conforming writer never emits, which refuses wire_damaged and lands
// nothing. The stain is 0xAA past `name`'s used length (an invalid UTF-8 lead
// byte) and 0x5A5A5A5A past `items`' count (past the element's declared max
// 255), so the refusal is the dead-byte rule and not a content one.
//
// THE NEGATIVE CONTROL (`-DW4_CONTROL`): forges ONE live byte on the wire —
// the live element items[0] from 7 to 300, past its declared max — so the
// clean peer's read refuses out_of_range and its assertions go RED.
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

// read one record off `wire` into `out`; returns the load's verdict. The
// destination is poisoned first, so a refusal that left it standing (rather
// than at its declared defaults, R4) is caught, and a clean read must land.
static int64_t read_one( const std::vector<uint8_t> & wire, tblv1::Cfg & out, tblv1::TableReport & r )
{
    tblv1::CfgReset( out );
    out.name_length = 5;
    out.items_count = 3;
    out.items[0] = 99;
    std::vector<tblv1::TableFixedEntry> plan( 8192 );
    return tblv1::CfgFixedLoad( &out, 1, wire.data(), (int64_t) wire.size(),
                                plan.data(), (int32_t) plan.size(), NULL, &r );
}

// build one record: name "hi" (used 2 of 32), one live item 7, and stain the
// dead bytes with garbage no conforming writer emits. `stain` selects the
// ZEROED (conforming peer) and STAINED (garbage peer) builds.
static std::vector<uint8_t> build_wire( bool stain )
{
    tblv1::Cfg value;
    tblv1::CfgReset( value );
    value.name[0] = 'h';
    value.name[1] = 'i';
    value.name_length = 2;
    value.items_count = 1;
    value.items[0] = 7;
    std::vector<uint8_t> wire( (size_t) tblv1::CfgFixedMeasure( 1 ) );
    if ( tblv1::CfgFixedSave( &value, 1, wire.data(), (int64_t) wire.size() ) != (int64_t) wire.size() )
    {
        std::printf( "FAIL W4: the record does not save\n" );
        failures++;
        return wire;
    }
#ifdef W4_CONTROL
    // the CONTROL: one LIVE element past the declared [0,255], forged on the
    // wire (the writer's bound is a debug assert)
    tblv1::TableFixedPut32( (uint8_t *) body_of( wire ) + 53, 300u );
#endif
    if ( stain )
    {
        const uint8_t * body = body_of( wire );
        // string(32) dead bytes: 2..31 of the name buffer, 0xAA (not a valid
        // UTF-8 lead byte, so the refusal is the dead-byte rule, not content).
        std::memset( (uint8_t *) body + 13 + 2, 0xAA, 32 - 2 );
        // [..8]int32 dead slots: elements 1..7, 0x5A5A5A5A (past the element
        // max 255, so the refusal is not a bound on a live value).
        for ( int i = 1; i < 8; ++i ) { tblv1::TableFixedPut32( (uint8_t *) body + 53 + i * 4, 0x5A5A5A5Au ); }
        // [..4]Grade and [..6]int32 dead slots, both under a zero count.
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

    check( clean.size() == stained.size(), "W4: the two wires are the same length" );

    tblv1::Cfg clean_out;
    tblv1::TableReport clean_r;
    tblv1::Cfg stained_out;
    tblv1::TableReport stained_r;

    const int64_t clean_n = read_one( clean, clean_out, clean_r );
    const int64_t stained_n = read_one( stained, stained_out, stained_r );

    // THE CONFORMING PEER reads and lands its used units.
    // strict-only-read: was "clamped == 0 on both"; the clean read is not
    // refused and not damaged.
    check( clean_n == 1 && !clean_r.refused && !clean_r.damaged, "W4: the zeroed peer reads clean" );
    check( clean_out.name_length == 2 && clean_out.name[0] == 'h' && clean_out.name[1] == 'i' && clean_out.name[2] == 0,
           "W4: the used units read (clean peer)" );
    check( clean_out.items_count == 1 && clean_out.items[0] == 7, "W4: the live element reads (clean peer)" );

    // THE GARBAGE PEER: was "reads, not refused, not malformed, no counter,
    // the used units land"; a non-zero dead byte now refuses the whole load
    // wire_damaged (FIXED-FORM-ALGORITHM.md:449, §5.3 step 11).
    check( stained_n == -1 && ( stained_r.refused || stained_r.damaged )
               && stained_r.refuse_reason == tblv1::wire_damaged,
           "W4: non-zero dead bytes refuse the whole load wire_damaged" );
    tblv1::Cfg fresh;
    tblv1::CfgReset( fresh );
    check( stained_out.name_length == fresh.name_length && stained_out.items_count == fresh.items_count
               && stained_out.items[0] == fresh.items[0],
           "W4: the refused load leaves the value at its declared defaults (R4), not the poison" );

    return failures == 0 ? 0 : 1;
}
