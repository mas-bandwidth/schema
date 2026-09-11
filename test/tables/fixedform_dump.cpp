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

// ---- rowan/cpp-versioning-numbers: BEGIN ----------------------------------
// THE VERSIONING LAW'S CORPUS (docs/FIXED-FORM-VERSIONING-TESTS.md): `old_<row>.bin`
// and `new_<row>.bin` per row, the two read columns' shared corpus, so every leg
// reads the SAME bytes the C++ reference wrote and the card on a port's job names
// one file. The values are the ones test/tables/versioning_numbers.cpp asserts.
#include "serialize.h"
#include "VOLD_array_bounded_growTable.h"
#include "VNEW_array_bounded_growTable.h"
#include "VOLD_array_fixed_growTable.h"
#include "VNEW_array_fixed_growTable.h"
#include "VOLD_array_elem_widenTable.h"
#include "VNEW_array_elem_widenTable.h"
#include "VOLD_constant_growTable.h"
#include "VNEW_constant_growTable.h"
#include "VOLD_string_growTable.h"
#include "VNEW_string_growTable.h"
#include "VOLD_wstring_growTable.h"
#include "VNEW_wstring_growTable.h"
#include "VOLD_bytes_growTable.h"
#include "VNEW_bytes_growTable.h"
#include "VOLD_int_widenTable.h"
#include "VNEW_int_widenTable.h"
#include "VOLD_uint_widenTable.h"
#include "VNEW_uint_widenTable.h"
#include "VOLD_float_widenTable.h"
#include "VNEW_float_widenTable.h"
#include "VOLD_range_widenTable.h"
#include "VNEW_range_widenTable.h"
#include "VOLD_bits_growTable.h"
#include "VNEW_bits_growTable.h"
#include "VOLD_fixed_I_growTable.h"
#include "VNEW_fixed_I_growTable.h"
#include "VOLD_optional_addTable.h"
#include "VNEW_optional_addTable.h"
#include "VOLD_floorTable.h"
#include "VMID_floorTable.h"
#include "VNEW_floorTable.h"
#include "VOLD_lineage_mergeTable.h"
#include "VBRA_lineage_mergeTable.h"
#include "VBRB_lineage_mergeTable.h"
#include "VNEW_lineage_mergeTable.h"
// ---- rowan/cpp-versioning-numbers: END ------------------------------------

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

// ---- rowan/cpp-versioning-numbers: BEGIN ----------------------------------
// ONE FILE PER SIDE PER ROW. `emit` above takes a vector and the unit's measure
// and save, so every row below is one statement and its values are visible.
#define VROW( NS, TBL, NAME, SETUP )                                                        \
    do {                                                                                     \
        std::vector<NS::TBL> v( 1 );                                                          \
        NS::TBL##Reset( v[0] );                                                               \
        SETUP                                                                                 \
        if ( !emit( dir, NAME, v, NS::TBL##FixedMeasure, NS::TBL##FixedSave ) ) { return false; } \
    } while ( 0 )

static bool versioning_numbers_files( const char * dir )
{
    VROW( vold_array_bounded_grow, ArrayBoundedGrow, "old_array_bounded_grow.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].vals_count = 4;
          for ( int i = 0; i < 4; ++i ) { v[0].vals[i] = 1000 + i; } v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_array_bounded_grow, ArrayBoundedGrow, "new_array_bounded_grow.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].vals_count = 8;
          for ( int i = 0; i < 8; ++i ) { v[0].vals[i] = 2000 + i; } v[0].trail = 0xBBBBBBBBu; );

    VROW( vold_array_fixed_grow, ArrayFixedGrow, "old_array_fixed_grow.bin",
          v[0].lead = 0xAAAAAAAAu; for ( int i = 0; i < 4; ++i ) { v[0].vals[i] = -500 - i; } v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_array_fixed_grow, ArrayFixedGrow, "new_array_fixed_grow.bin",
          v[0].lead = 0xAAAAAAAAu; for ( int i = 0; i < 8; ++i ) { v[0].vals[i] = 3000 + i; } v[0].trail = 0xBBBBBBBBu; );

    VROW( vold_array_elem_widen, ArrayElemWiden, "old_array_elem_widen.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].vals_count = 4; v[0].vals[0] = -1; v[0].vals[1] = INT16_MIN;
          v[0].vals[2] = INT16_MAX; v[0].vals[3] = 0; v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_array_elem_widen, ArrayElemWiden, "new_array_elem_widen.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].vals_count = 4;
          for ( int i = 0; i < 4; ++i ) { v[0].vals[i] = 100000 + i; } v[0].trail = 0xBBBBBBBBu; );

    VROW( vold_constant_grow, ConstantGrow, "old_constant_grow.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].vals_count = 4;
          for ( int i = 0; i < 4; ++i ) { v[0].vals[i] = 7000 + i; } v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_constant_grow, ConstantGrow, "new_constant_grow.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].vals_count = 8;
          for ( int i = 0; i < 8; ++i ) { v[0].vals[i] = 8000 + i; } v[0].trail = 0xBBBBBBBBu; );

    VROW( vold_string_grow, StringGrow, "old_string_grow.bin",
          v[0].lead = 0xAAAAAAAAu; std::strcpy( v[0].text, "abcdefgh" ); v[0].text_length = 8; v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_string_grow, StringGrow, "new_string_grow.bin",
          v[0].lead = 0xAAAAAAAAu; std::strcpy( v[0].text, "0123456789abcdef" ); v[0].text_length = 16; v[0].trail = 0xBBBBBBBBu; );

    const char16_t wsrc[8] = { u'h', u'e', u'l', u'l', u'o', 0xD83D, 0xDE00, u'￿' };
    VROW( vold_wstring_grow, WstringGrow, "old_wstring_grow.bin",
          v[0].lead = 0xAAAAAAAAu; std::memcpy( v[0].text, wsrc, sizeof( wsrc ) ); v[0].text_length = 8; v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_wstring_grow, WstringGrow, "new_wstring_grow.bin",
          v[0].lead = 0xAAAAAAAAu; for ( int i = 0; i < 16; ++i ) { v[0].text[i] = (char16_t) ( u'a' + i ); }
          v[0].text_length = 16; v[0].trail = 0xBBBBBBBBu; );

    VROW( vold_bytes_grow, BytesGrow, "old_bytes_grow.bin",
          v[0].lead = 0xAAAAAAAAu; for ( int i = 0; i < 8; ++i ) { v[0].blob[i] = (uint8_t) ( 0xF0 + i ); }
          v[0].blob_length = 8; v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_bytes_grow, BytesGrow, "new_bytes_grow.bin",
          v[0].lead = 0xAAAAAAAAu; for ( int i = 0; i < 16; ++i ) { v[0].blob[i] = (uint8_t) i; }
          v[0].blob_length = 16; v[0].trail = 0xBBBBBBBBu; );

    // THE THREE VALUES A WIDENING GETS WRONG, all three in ONE file as three records
    {
        std::vector<vold_int_widen::IntWiden> v( 3 );
        const int16_t values[3] = { -1, INT16_MIN, INT16_MAX };
        for ( int k = 0; k < 3; ++k )
        {
            vold_int_widen::IntWidenReset( v[(size_t) k] );
            v[(size_t) k].lead = 0xAAAAAAAAu;
            v[(size_t) k].v = values[k];
            v[(size_t) k].trail = 0xBBBBBBBBu;
        }
        if ( !emit( dir, "old_int_widen.bin", v, vold_int_widen::IntWidenFixedMeasure,
                    vold_int_widen::IntWidenFixedSave ) ) { return false; }
    }
    VROW( vnew_int_widen, IntWiden, "new_int_widen.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].v = 100000; v[0].trail = 0xBBBBBBBBu; );

    VROW( vold_uint_widen, UintWiden, "old_uint_widen.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].v = 0xFFFFu; v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_uint_widen, UintWiden, "new_uint_widen.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].v = 0xDEADBEEFu; v[0].trail = 0xBBBBBBBBu; );

    // A SIGNALLING NaN WITH A PAYLOAD: the bits are the value (the FU ruling)
    {
        const uint32_t kSignalling = 0x7F8ABCDEu;
        float f; std::memcpy( &f, &kSignalling, 4 );
        VROW( vold_float_widen, FloatWiden, "old_float_widen.bin",
              v[0].lead = 0xAAAAAAAAu; v[0].v = f; v[0].trail = 0xBBBBBBBBu; );
    }
    VROW( vnew_float_widen, FloatWiden, "new_float_widen.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].v = 1.0e300; v[0].trail = 0xBBBBBBBBu; );

    VROW( vold_range_widen, RangeWiden, "old_range_widen.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].v = 100; v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_range_widen, RangeWiden, "new_range_widen.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].v = 150; v[0].trail = 0xBBBBBBBBu; );

    VROW( vold_bits_grow, BitsGrow, "old_bits_grow.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].v = 0xFFu; v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_bits_grow, BitsGrow, "new_bits_grow.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].v = 0xFFFu; v[0].trail = 0xBBBBBBBBu; );

    VROW( vold_fixed_i_grow, FixedIGrow, "old_fixed_I_grow.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].v = -1; v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_fixed_i_grow, FixedIGrow, "new_fixed_I_grow.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].v = 1000; v[0].trail = 0xBBBBBBBBu; );

    VROW( vold_optional_add, OptionalAdd, "old_optional_add.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].link.value = 777; v[0].trail = 0xBBBBBBBBu; );
    VROW( vnew_optional_add, OptionalAdd, "new_optional_add.bin",
          v[0].lead = 0xAAAAAAAAu; v[0].link_present = false; v[0].link.value = 0; v[0].trail = 0xBBBBBBBBu; );

    // THE FLOOR'S LINEAGE: three layouts, oldest first. `old_floor.bin` is the
    // file below a floor of 1; `mid_floor.bin` the file AT it; `new_floor.bin`
    // the reader's own.
    VROW( vold_floor, Floored, "old_floor.bin", v[0].a = 11; );
    VROW( vmid_floor, Floored, "mid_floor.bin", v[0].a = 11; v[0].b = 22; );
    VROW( vnew_floor, Floored, "new_floor.bin", v[0].a = 11; v[0].b = 22; v[0].c = 33; );

    // THE BRANCH CASE: both pre-merge writers, and the merged build's own
    VROW( vold_lineage_merge, Merged, "old_lineage_merge.bin", v[0].anchor = 50; );
    VROW( vbra_lineage_merge, Merged, "a_lineage_merge.bin", v[0].anchor = 100; v[0].from_a = 111; );
    VROW( vbrb_lineage_merge, Merged, "b_lineage_merge.bin", v[0].anchor = 200; v[0].from_b = 222; );
    VROW( vnew_lineage_merge, Merged, "new_lineage_merge.bin", v[0].anchor = 300; v[0].from_a = 1; v[0].from_b = 2; );
    return true;
}
// ---- rowan/cpp-versioning-numbers: END ------------------------------------

int main( int argc, char ** argv )
{
    if ( argc != 2 ) { std::fprintf( stderr, "usage: %s <outdir>\n", argv[0] ); return 1; }
    const char * dir = argv[1];
    if ( !fx1_file( dir ) || !fx2_file( dir ) || !p1_file( dir ) || !p3_file( dir ) ||
         !keyed_file( dir ) || !pack_file( dir ) || !fxw_file( dir ) ) { return 1; }
    // ---- rowan/cpp-versioning-numbers: BEGIN ----
    if ( !versioning_numbers_files( dir ) ) { return 1; }
    // ---- rowan/cpp-versioning-numbers: END ----
    return 0;
}
