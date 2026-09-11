// THE FIXED FORM'S CROSS-LANGUAGE BYTE ORACLE (docs/SPEC-TABLES.md §3.4).
//
// The C++ backend is the REFERENCE for this form, so the reference is what
// writes the bytes and every port matches them. This binary writes one form-3
// FILE per root, with values set by hand so nothing passes by accident, and a
// port's leg proves itself against those files two ways:
//
//   THE WRITE   read the file, save the values back, and the bytes must be
//               identical. That reaches every field: a byte a port encodes
//               differently is a byte that does not come back.
//   THE READ    read a file written under ANOTHER schema's block, which is the
//               plan path and the whole of what §3.4's versioning invariant is
//               worth.
//
// It writes and says nothing else, so a port's leg can diff the files without
// parsing a word of output.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "FX1Table.h"
#include "FX2Table.h"
#include "P1Table.h"
#include "P3Table.h"
#include "KeyedTable.h"
#include "PackTable.h"
#include "FXWTable.h"
#include "FU1Table.h"
#include "FU2Table.h"

static bool spill( const char * dir, const char * name, const std::vector<uint8_t> & data )
{
    char path[1024];
    std::snprintf( path, sizeof( path ), "%s/%s", dir, name );
    FILE * f = std::fopen( path, "wb" );
    if ( !f ) { std::fprintf( stderr, "cannot write %s\n", path ); return false; }
    const bool ok = std::fwrite( data.data(), 1, data.size(), f ) == data.size();
    return std::fclose( f ) == 0 && ok;
}

template <typename T, typename Measure, typename Save>
static bool emit( const char * dir, const char * name, const std::vector<T> & values, Measure measure, Save save )
{
    std::vector<uint8_t> out( (size_t) measure( (int64_t) values.size() ) );
    if ( save( values.data(), (int64_t) values.size(), out.data(), (int64_t) out.size() ) != (int64_t) out.size() )
    {
        std::fprintf( stderr, "%s: save refused\n", name );
        return false;
    }
    return spill( dir, name, out );
}


// A FLOAT RIDES AS ITS BIT PATTERN (docs/SPEC-TABLES.md §3, §4, schema#480), so
// a SIGNALLING NaN has to be put in place through its bits: a literal would be
// quieted by the compiler before the writer ever saw it.
static float float_from_bits( uint32_t bits ) { float f; std::memcpy( &f, &bits, 4 ); return f; }
static double double_from_bits( uint64_t bits ) { double d; std::memcpy( &d, &bits, 8 ); return d; }

// ---- FX1 / FX2: the versioning pair ---------------------------------------
//
// The five edits FX1.schema names, and the values are the ones a port's leg
// asserts on the other side of the plan.

static bool fx1_file( const char * dir )
{
    std::vector<tblfx1::FxRoot> v( 2 );
    tblfx1::FxRootReset( v[0] );
    v[0].keep = 4242u;
    v[0].narrow = 40000u; // a uint16 value FX2's WIDENED read must reproduce
    v[0].renamed = 321;
    v[0].gone = 654;
    v[0].nested.a = 111;
    v[0].nested.b = 222;
    // A SHORT STRING AND A PARTLY-USED ARRAY, which is where §3.4's "the slack
    // is zero" is worth pinning: the bytes past the used length and past the
    // live count are the template's zeros in every port or they are not the
    // same bytes.
    std::strcpy( v[0].label, "fx1" );
    v[0].label_length = 3;
    v[0].marks[0] = 101;
    v[0].marks[1] = 202;
    v[0].marks_count = 2;
    // a `bytes(N)` PARTLY USED: an array of u8 on this wire, so the slack past
    // the live length is the template's zeros here too
    v[0].blob[0] = 0xDE; v[0].blob[1] = 0xAD; v[0].blob[2] = 0xBE; v[0].blob[3] = 0xEF;
    v[0].blob_length = 4;
    tblfx1::FxRootReset( v[1] );
    v[1].keep = 1u;
    v[1].narrow = 2u;
    v[1].renamed = 3;
    v[1].gone = 4;
    v[1].nested.a = 5;
    v[1].nested.b = 6;
    v[1].label_length = 0; // nothing used at all: the WHOLE span is slack
    v[1].marks_count = 0;
    v[1].blob_length = 0;
    return emit( dir, "fx1.bin", v, tblfx1::FxRootFixedMeasure, tblfx1::FxRootFixedSave );
}

static bool fx2_file( const char * dir )
{
    std::vector<tblfx2::FxRoot> v( 1 );
    tblfx2::FxRootReset( v[0] );
    v[0].keep = 5150u;
    v[0].narrow = 70000u; // wider than FX1 holds: a kind that MOVED, not a widening
    v[0].renamed_to = 808;
    v[0].added = 909;
    v[0].nested.a = 33;
    v[0].nested.b = 44;
    v[0].extra.x = 55;
    v[0].extra.y = 66;
    std::strcpy( v[0].label, "fx2" );
    v[0].label_length = 3;
    v[0].marks[0] = 303;
    v[0].marks_count = 1;
    v[0].blob[0] = 0x01; v[0].blob[1] = 0x02; v[0].blob[2] = 0x03;
    v[0].blob_length = 3;
    return emit( dir, "fx2.bin", v, tblfx2::FxRootFixedMeasure, tblfx2::FxRootFixedSave );
}

// ---- P1 / P3: a value against an optional ----------------------------------

static bool p1_file( const char * dir )
{
    std::vector<tblp1::Chain> v( 1 );
    tblp1::ChainReset( v[0] );
    std::strcpy( v[0].name, "chain-one" );
    v[0].name_length = 9;
    v[0].link.value = 77;
    std::strcpy( v[0].link.tag, "tagged" );
    v[0].link.tag_length = 6;
    return emit( dir, "p1.bin", v, tblp1::ChainFixedMeasure, tblp1::ChainFixedSave );
}

static bool p3_file( const char * dir )
{
    std::vector<tblp3::Chain> v( 2 );
    tblp3::ChainReset( v[0] );
    std::strcpy( v[0].name, "present" );
    v[0].name_length = 7;
    v[0].link_present = true;
    v[0].link.value = 88;
    std::strcpy( v[0].link.tag, "here" );
    v[0].link.tag_length = 4;
    tblp3::ChainReset( v[1] );
    std::strcpy( v[1].name, "absent" );
    v[1].name_length = 6;
    v[1].link_present = false;
    // THE PAYLOAD RIDES WHOLE WHETHER OR NOT IT IS PRESENT (§3.4), and when the
    // flag is 0 what rides is ZERO. These stores are here to prove it: the
    // storage carries values, the flag says absent, and the file's bytes for
    // this payload are the template's zeros all the same.
    v[1].link.value = 99;
    std::strcpy( v[1].link.tag, "still" );
    v[1].link.tag_length = 5;
    return emit( dir, "p3.bin", v, tblp3::ChainFixedMeasure, tblp3::ChainFixedSave );
}

// ---- the rich shape: keyed arrays nesting keyed arrays, and an optional ----

static bool keyed_file( const char * dir )
{
    std::vector<tabledemo::KeyedConfig> v( 2 );
    for ( int k = 0; k < 2; ++k )
    {
        tabledemo::KeyedConfigReset( v[k] );
        for ( int t = 0; t < 3; ++t )
        {
            v[k].teams.slots[t].spawn_count = 4 + t + k * 10;
            const char * names[3] = { "red", "blue", "green" };
            std::strcpy( v[k].teams.slots[t].banner, names[t] );
            v[k].teams.slots[t].banner_length = (int32_t) std::strlen( names[t] );
            v[k].scores.per_team[t] = 1000 * ( t + 1 ) + k;
        }
        for ( int h = 0; h < 3; ++h )
        {
            v[k].hulls.slots[h].health = 100.0f + (float) h + (float) k;
            v[k].hulls.slots[h].mass = 1.5f * (float) ( h + 1 );
            for ( int w = 0; w < 3; ++w )
            {
                v[k].hulls.slots[h].turrets.slots[w].damage = 10.0f + (float) ( h * 3 + w );
                v[k].hulls.slots[h].turrets.slots[w].cooldown = 0.25f * (float) ( w + 1 );
                v[k].hulls.slots[h].turrets.slots[w].gunner_present = ( ( h + w ) % 2 ) == 0;
                v[k].hulls.slots[h].turrets.slots[w].gunner.reaction = 0.2f + 0.1f * (float) w;
                v[k].hulls.slots[h].turrets.slots[w].gunner.tracking = ( w % 2 ) == 1;
            }
        }
    }
    return emit( dir, "keyed.bin", v, tabledemo::KeyedConfigFixedMeasure, tabledemo::KeyedConfigFixedSave );
}

// ---- the packed corpus's root: counted arrays, an enum with a declared
// default, a fixed array of floats, and an optional section inside a record ---

static bool pack_file( const char * dir )
{
    std::vector<tabledemo::PackConfig> v( 2 );
    for ( int k = 0; k < 2; ++k )
    {
        tabledemo::PackConfigReset( v[k] );
        v[k].version = 7u + (uint32_t) k;
        v[k].global.tick_rate = 120u - (uint32_t) k;
        v[k].global.difficulty = k == 0 ? tabledemo::Difficulty::Hard : tabledemo::Difficulty::Easy;
        const char * note = k == 0 ? "first build" : "second build";
        std::strcpy( v[k].global.build_note, note );
        v[k].global.build_note_length = (int32_t) std::strlen( note );
        for ( int i = 0; i < 3; ++i ) { v[k].global.spawn_delays[i] = 0.5f * (float) ( i + 1 + k ); }
        for ( int s = 0; s < 3; ++s )
        {
            const char * names[3] = { "fighter", "bomber", "scout" };
            std::strcpy( v[k].ships.slots[s].display_name, names[s] );
            v[k].ships.slots[s].display_name_length = (int32_t) std::strlen( names[s] );
            v[k].ships.slots[s].health = 100.0f + (float) ( s * 10 + k );
            v[k].ships.slots[s].mass = 1.0f + 0.25f * (float) s;
            v[k].ships.slots[s].hardpoints_count = s + 1;
            for ( int h = 0; h < s + 1; ++h ) { v[k].ships.slots[s].hardpoints[h] = h + 1; }
            v[k].ships.slots[s].gunner_present = ( s % 2 ) == 0;
            v[k].ships.slots[s].gunner.reaction = 0.2f + 0.05f * (float) s;
            v[k].ships.slots[s].gunner.tracking = ( s % 2 ) == 1;
            const char * calls[3] = { "ace", "hammer", "ghost" };
            std::strcpy( v[k].ships.slots[s].gunner.callsign, calls[s] );
            v[k].ships.slots[s].gunner.callsign_length = (int32_t) std::strlen( calls[s] );
            v[k].thresholds.slots[s] = 100 * ( s + 1 ) + k;
        }
        v[k].reserves_count = 2;
        for ( int r = 0; r < 2; ++r )
        {
            const char * names[2] = { "spare-a", "spare-b" };
            std::strcpy( v[k].reserves[r].display_name, names[r] );
            v[k].reserves[r].display_name_length = (int32_t) std::strlen( names[r] );
            v[k].reserves[r].health = 50.0f + (float) r;
            v[k].reserves[r].mass = 2.0f;
            v[k].reserves[r].hardpoints_count = 1;
            v[k].reserves[r].hardpoints[0] = 8;
            v[k].reserves[r].gunner_present = false;
        }
    }
    return emit( dir, "pack.bin", v, tabledemo::PackConfigFixedMeasure, tabledemo::PackConfigFixedSave );
}

// ---- the WIDE TEXT unit (docs/SPEC-TABLES.md §3.4, kind 33) ----
//
// The wide flavour's ONLY oracle bytes. A length in CODE UNITS and 2N bytes
// behind it, at the body's head and again at a NESTED offset, with a narrow
// `string(8)` between them so a leg that counted bytes where it owed units
// comes out wrong here and nowhere else has to catch it.
static bool fxw_file( const char * dir )
{
    std::vector<tblfxw::FxWide> v( 2 );

    tblfxw::FxWideReset( v[0] );
    // seven BASIC-PLANE code units, one short of the bound
    const char16_t hello[7] = { u'h', u'e', u'l', u'l', u'o', u' ', u'!' };
    std::memcpy( v[0].caption, hello, sizeof( hello ) );
    v[0].caption_length = 7;
    std::strcpy( v[0].label, "narrow" );
    v[0].label_length = 6;
    const char16_t inner0[4] = { u'a', u'b', u'c', u'd' };
    std::memcpy( v[0].inner.text, inner0, sizeof( inner0 ) );
    v[0].inner.text_length = 4;
    v[0].seq = 41;

    tblfxw::FxWideReset( v[1] );
    // AN ASTRAL PAIR AND THE TWO BASIC-PLANE ENDS OF THE RANGE: a surrogate
    // pair is TWO code units and the length counts both, which is the number
    // a leg using bytes gets wrong by a factor of two.
    const char16_t astral[5] = { u'\uE000', 0xD83D, 0xDE00, u'\uFFFF', u'z' };
    std::memcpy( v[1].caption, astral, sizeof( astral ) );
    v[1].caption_length = 5;
    v[1].label_length = 0; // empty, and its eight bytes are slack
    v[1].inner.text_length = 0;
    v[1].seq = 1000; // the declared max, so the clamp has no false case here

    return emit( dir, "fxw.bin", v, tblfxw::FxWideFixedMeasure, tblfxw::FxWideFixedSave );
}

// ---- FU1 / FU2: TEXT UNDER A UNION ARM, AND THE TWO WIDENING RUNGS ---------
//
// The nested-union pair was the one fixture in this corpus with NO oracle bytes
// (schema#876, card 15): every leg pinned FU1 by writing it itself and reading
// it back, so a leg whose bytes differ from the reference's by one byte passed
// in silence. These two files are those bytes.
//
// The record set is set by hand and reaches, in one file:
//   BOTH ARMS            the second arm (a `string(8)` between two scalars) and
//                        the first (a bare scalar), so the arms' differing
//                        sizes make the second arm's offsets its own
//   THE SIGNED RUNG      `mark int16` at -1, at INT16_MIN and at INT16_MAX, so
//                        the SIGN-EXTENDED read into FU2's `int32` has a true
//                        case at both ends and a false one: a leg that zero-
//                        extends lands 65535 for -1 and cannot hide it
//   THE FLOAT RUNG       `heat float32` as a SIGNALLING NaN with the smallest
//                        payload, as a negative signalling NaN with a rich one,
//                        and as an ordinary 1.5 beside them — the rung where a
//                        leg that widens through a hardware conversion quiets
//                        the NaN and loses the payload
//   THE TWO FORGED BYTES `flag` and `note`'s presence, which §3.4 spells as one
//                        byte that is true when it is NOT ZERO; the writer puts
//                        1 here and the leg's own forging case does the rest

static bool fu1_file( const char * dir )
{
    std::vector<tblfu1::FuRoot> v( 3 );

    // the SECOND arm, with text between its two scalars
    tblfu1::FuRootReset( v[0] );
    v[0].flag = true;
    v[0].note = 44;
    v[0].note_present = true;
    v[0].pick.type = tblfu1::PickType::Labelled;
    v[0].pick.labelled.lead = 101;
    std::strcpy( v[0].pick.labelled.label, "hello" );
    v[0].pick.labelled.label_length = 5;
    v[0].pick.labelled.trail = 202;
    v[0].tail = 11;
    v[0].mark = -1;                                   // the rung's headline case
    v[0].heat = float_from_bits( 0x7F800001u );       // signalling, smallest payload

    // the FIRST arm, a bare scalar, and the optional ABSENT
    tblfu1::FuRootReset( v[1] );
    v[1].flag = false;
    v[1].note_present = false;
    v[1].note = 0;
    v[1].pick.type = tblfu1::PickType::Plain;
    v[1].pick.plain.n = 303;
    v[1].tail = 12;
    v[1].mark = -32768;                               // INT16_MIN: every sign bit set
    v[1].heat = float_from_bits( 0xFFA5A5A5u );       // negative, signalling, rich payload

    // the SECOND arm again with its text WHOLLY UNUSED, so the eight bytes of
    // the span are slack and a positive `mark` gives the sign rung a false case
    tblfu1::FuRootReset( v[2] );
    v[2].flag = true;
    v[2].note = -7;
    v[2].note_present = true;
    v[2].pick.type = tblfu1::PickType::Labelled;
    v[2].pick.labelled.lead = -5;
    v[2].pick.labelled.label_length = 0;
    v[2].pick.labelled.trail = 606;
    v[2].tail = 13;
    v[2].mark = 32767;                                // INT16_MAX: positive, nothing to extend
    v[2].heat = 1.5f;                                 // an ordinary float beside the NaNs

    return emit( dir, "fu1.bin", v, tblfu1::FuRootFixedMeasure, tblfu1::FuRootFixedSave );
}

// FU2's own bytes, for the OTHER direction: read under FU1's declaration
// `extra` is a field that reader cannot name and the two rungs run BACKWARDS,
// which is a KIND THAT MOVED and not a rung at all (§4: not decoded, the
// declared default stands, `kind_mismatch`).
static bool fu2_file( const char * dir )
{
    std::vector<tblfu2::FuRoot> v( 2 );

    tblfu2::FuRootReset( v[0] );
    v[0].flag = true;
    v[0].note = 55;
    v[0].note_present = true;
    v[0].pick.type = tblfu2::PickType::Labelled;
    v[0].pick.labelled.lead = 111;
    std::strcpy( v[0].pick.labelled.label, "wide" );
    v[0].pick.labelled.label_length = 4;
    v[0].pick.labelled.trail = 222;
    v[0].tail = 21;
    v[0].mark = -70000;                               // past everything an int16 holds
    v[0].heat = double_from_bits( 0x7FF00DEFACED0001ull ); // signalling at sixty-four bits
    v[0].extra = 909;

    tblfu2::FuRootReset( v[1] );
    v[1].flag = false;
    v[1].note_present = false;
    v[1].pick.type = tblfu2::PickType::Plain;
    v[1].pick.plain.n = 404;
    v[1].tail = 22;
    v[1].mark = 70000;
    v[1].heat = -2.25;
    v[1].extra = -11;

    return emit( dir, "fu2.bin", v, tblfu2::FuRootFixedMeasure, tblfu2::FuRootFixedSave );
}

int main( int argc, char ** argv )
{
    if ( argc != 2 ) { std::fprintf( stderr, "usage: %s <outdir>\n", argv[0] ); return 1; }
    const char * dir = argv[1];
    if ( !fx1_file( dir ) || !fx2_file( dir ) || !p1_file( dir ) || !p3_file( dir ) ||
         !keyed_file( dir ) || !pack_file( dir ) || !fxw_file( dir ) ||
         !fu1_file( dir ) || !fu2_file( dir ) ) { return 1; }
    return 0;
}
