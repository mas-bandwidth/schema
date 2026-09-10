// fixedform_dump.cpp — THE FIXED FORM'S ORACLE BYTES (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-COVERAGE.md).
//
// WHY THIS EXISTS AT ALL. Every other wire in this project has pinned bytes —
// `testdata/wire/tables/` for §3, `cook-write/` for §7, `block/` for §19 — and
// the fixed form had NONE. What the versioning conformance set holds is that a
// reader of one generation makes the right VALUES out of a writer of another;
// what nothing held is that the writer produced the right BYTES. Those are two
// different claims, and only the second one is what a second port has to match.
// A leg whose writer and reader are wrong in the same direction passes every
// round-trip test there is and cannot exchange one record with anybody.
//
// So this program writes, for every fixture of `fixedform_fixtures.h`, the
// FILE the reference's writer produces: its header, its layout hash, its layout
// LENGTH, and the record's hash and body as hex. That file is pinned under
// `testdata/conformance/tables/fixedform/` and every leg byte-compares it, on
// the same terms as the cook's dump and the block's.
//
// WHAT IS PINNED AND WHAT IS NOT. The LAYOUT's own bytes are NOT dumped and its
// HASH is: the layout is hundreds of bytes of ids and sizes, the hash is
// fnv1a64 over exactly those bytes (§3.4), and a hash that matches is a layout
// that matches — so pinning the hash pins the layout and pinning both would be
// one golden with two homes. The layout's LENGTH rides beside it, because a
// length is the one layout fact a hash collision could not be made to hide and
// it is what a reader indexes the records off.
//
// THE RECORD BODY IS HEX AND NOT A VALUE DUMP, and that is the point. §3.4's
// whole claim is that a field "rides as its DECLARED STORAGE IMAGE,
// LITTLE-ENDIAN, at its DECLARED STORAGE WIDTH, and nothing is padded between
// fields" — a claim about BYTES. A dump that printed `tilt = 112` would pass on
// a port that put the byte in the wrong place, which is the one thing this
// file is for. The declared slack is in the hex too: §3.4 says a writer
// ZERO-FILLS it, and zeros nobody looked at are zeros nobody has.
//
// The output is DETERMINISTIC — every value is a constant of
// `fixedform_fixtures.h` and nothing here reads a clock, a pointer or an
// environment — so `make tables-fixedform-corpus` regenerating it twice
// produces the same bytes twice, and the gate says so.

#include <cstdio>
#include <cstdint>
#include <cstring>
#include <vector>

#include "fixedform_fixtures.h"

static int failures = 0;

// ---------------------------------------------------------------------------

// ONE CASE, WRITTEN THE WAY A CALLER WRITES ONE. The measure, the save, and the
// save's own return checked against the buffer — a short write that nobody
// checked would dump a tail of uninitialised bytes and pin them.
template <typename Value, typename Measure, typename Save>
static void dump_case( const char * name, const char * unit, const char * root,
                       const Value & v, Measure measure, Save save,
                       int64_t layout_bytes, uint64_t layout_hash, int64_t body_bytes )
{
    const int64_t want = measure( 1 );
    std::vector<uint8_t> f( (size_t) want );
    const int64_t wrote = save( &v, 1, f.data(), (int64_t) f.size() );
    if ( wrote != want )
    {
        std::printf( "# FAIL %s: save wrote %lld of %lld\n", name, (long long) wrote, (long long) want );
        failures++;
        return;
    }

    // THE HEADER IS §3'S AND THE OFFSETS ARE §3.4'S: form byte, six reserved
    // zeros, the LAYOUT HASH at 8, the layout LENGTH at 16, then the layout,
    // then the records back to back to the end of the file.
    const size_t layout_at = 16 + 4;
    const size_t record_at = layout_at + (size_t) layout_bytes;

    std::printf( "case %s\n", name );
    std::printf( "  unit %s root %s\n", unit, root );
    std::printf( "  form %u reserved", (unsigned) f[0] );
    for ( int i = 1; i < 8; ++i ) { std::printf( " %02X", f[i] ); }
    std::printf( "\n" );
    std::printf( "  layout %lld bytes hash %016llX\n",
                 (long long) layout_bytes, (unsigned long long) layout_hash );
    std::printf( "  layout length field %u\n", (unsigned) ( (uint32_t) f[16] | ( (uint32_t) f[17] << 8 ) |
                                                            ( (uint32_t) f[18] << 16 ) | ( (uint32_t) f[19] << 24 ) ) );
    std::printf( "  file %lld bytes, record %lld = hash 8 + body %lld\n",
                 (long long) f.size(), (long long) ( 8 + body_bytes ), (long long) body_bytes );

    // THE RECORD'S OWN HASH, which is not the header's question. The header's
    // eight bytes say WHICH LAYOUT IS IN THIS FILE and a record's say WHICH
    // LAYOUT STAMPED THIS RECORD (§3.4), so both are printed and a port that
    // wrote one and not the other is visible here rather than at a peer.
    std::printf( "  hash " );
    for ( int i = 0; i < 8; ++i ) { std::printf( "%02X", f[record_at + (size_t) i] ); }
    std::printf( "\n" );

    // THE BODY, SIXTEEN BYTES A LINE, offset-labelled. A diff that moves says
    // WHICH FIELD moved rather than that something did.
    for ( int64_t at = 0; at < body_bytes; at += 16 )
    {
        std::printf( "  %04llX ", (unsigned long long) at );
        for ( int64_t i = 0; i < 16 && at + i < body_bytes; ++i )
        {
            std::printf( " %02X", f[record_at + 8 + (size_t) ( at + i )] );
        }
        std::printf( "\n" );
    }
    std::printf( "\n" );
}

#define DUMP( name, ns, Root, value ) \
    dump_case( name, #ns, #Root, value, ns::Root##FixedMeasure, ns::Root##FixedSave, \
               ns::Root##FixedLayoutBytes, ns::Root##FixedHash, ns::Root##FixedBodyBytes )

int main()
{
    std::printf( "# THE FIXED FORM'S RECORD ORACLE (docs/SPEC-TABLES.md §3.4)\n" );
    std::printf( "#\n" );
    std::printf( "# Generated by test/tables/fixedform_dump.cpp from the fixtures of\n" );
    std::printf( "# test/tables/fixedform_fixtures.h, which is the same file\n" );
    std::printf( "# test/tables/fixedform_main.cpp reads. Regenerate with\n" );
    std::printf( "# `make tables-fixedform-corpus`; a byte that MOVES under an unchanged\n" );
    std::printf( "# schema is stop-the-line and never a quiet repin, exactly as a wire\n" );
    std::printf( "# golden is (testdata/conformance/tables/FORMAT.md).\n" );
    std::printf( "#\n" );
    std::printf( "# Every number is LITTLE-ENDIAN and the body's declared slack is zero,\n" );
    std::printf( "# which is §3.4's write rule and is pinned here as bytes rather than\n" );
    std::printf( "# asserted as a property.\n" );
    std::printf( "\n" );

    // ---- the scalar edits, both generations ------------------------------
    { tblfx1::FxRoot v; FillFx1( v ); DUMP( "fx1/root", tblfx1, FxRoot, v ); }
    { tblfx2::FxRoot v; FillFx2( v ); DUMP( "fx2/root", tblfx2, FxRoot, v ); }

    // ---- TEXT OF EACH FLAVOUR UNDER A UNION ARM --------------------------
    //
    // `fu1/wide` is where FLAVOUR 2 GETS ORACLE BYTES: a length in CODE UNITS
    // and two bytes per unit, one of which has a non-zero high byte.
    { tblfu1::MarkRoot v; FillFu1Wide( v );   DUMP( "fu1/wide",   tblfu1, MarkRoot, v ); }
    { tblfu1::MarkRoot v; FillFu1Narrow( v ); DUMP( "fu1/narrow", tblfu1, MarkRoot, v ); }
    { tblfu1::MarkRoot v; FillFu1Raw( v );    DUMP( "fu1/raw",    tblfu1, MarkRoot, v ); }
    { tblfu1::MarkRoot v; FillFu1List( v );   DUMP( "fu1/list",   tblfu1, MarkRoot, v ); }
    // TAG 0: the arm extent is declared slack and every byte of it is zero
    { tblfu1::MarkRoot v; FillFu1None( v );   DUMP( "fu1/none",   tblfu1, MarkRoot, v ); }
    { tblfu2::MarkRoot v; FillFu2Skip( v );   DUMP( "fu2/skip",   tblfu2, MarkRoot, v ); }
    { tblfu2::MarkRoot v; FillFu2List( v );   DUMP( "fu2/list",   tblfu2, MarkRoot, v ); }
    { tblfu2::MarkRoot v; FillFu2Narrow( v ); DUMP( "fu2/narrow", tblfu2, MarkRoot, v ); }

    // ---- the bool, the optional, the array of unions, the three-deep ------
    { tblfn1::FnRoot v; FillFn1( v );       DUMP( "fn1/full",   tblfn1, FnRoot, v ); }
    // THE ABSENT OPTIONAL: the present byte zero and the payload ZERO-FILLED,
    // which is the byte picture §3.4's slack rule states and the one the
    // residue case in fixedform_main.cpp then damages.
    { tblfn1::FnRoot v; FillFn1Absent( v ); DUMP( "fn1/absent", tblfn1, FnRoot, v ); }
    { tblfn2::FnRoot v; FillFn2( v );       DUMP( "fn2/full",   tblfn2, FnRoot, v ); }

    // ---- A TOP-LEVEL wstring(N) ------------------------------------------
    { wide::Stamp v; FillWideStamp( v ); DUMP( "wide/stamp", wide, Stamp, v ); }

    // ---- the widths, the 128-bit integers and the containers --------------
    { scalardemo::SimState v; FillScalars( v ); DUMP( "scalars/simstate", scalardemo, SimState, v ); }

    // ---- the IEEE-754 bit patterns ---------------------------------------
    { tblf1::Floats v; FillFloats( v ); DUMP( "f1/floats", tblf1, Floats, v ); }

    // ---- the enum, the counted enum array and the text -------------------
    { tblv1::Cfg v; FillV1( v ); DUMP( "v1/cfg", tblv1, Cfg, v ); }

    // ---- `flags`, the declared defaults, and a table renamed -------------
    { tblw1::Vessel v; FillW1( v ); DUMP( "w1/vessel", tblw1, Vessel, v ); }

    // ---- the `bits(N)` family at its DECLARED storage widths -------------
    { tabledemo::RangedWidths v; FillBits( v ); DUMP( "bits/widths", tabledemo, RangedWidths, v ); }

    // ---- the text-content proving ground, at three used extents ----------
    { tblp1::Chain v; FillP1( v, "", 0 );                 DUMP( "p1/empty", tblp1, Chain, v ); }
    { tblp1::Chain v; FillP1( v, "chain", 5 );            DUMP( "p1/short", tblp1, Chain, v ); }
    { tblp1::Chain v; FillP1( v, "0123456789abcdef", 16 ); DUMP( "p1/full",  tblp1, Chain, v ); }

    if ( failures != 0 ) { std::printf( "# %d failure(s)\n", failures ); return 1; }
    return 0;
}
