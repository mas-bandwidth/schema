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
    tblfx1::FxRootReset( v[1] );
    v[1].keep = 1u;
    v[1].narrow = 2u;
    v[1].renamed = 3;
    v[1].gone = 4;
    v[1].nested.a = 5;
    v[1].nested.b = 6;
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
    // the payload rides WHOLE whether or not it is present (§3.4)
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

int main( int argc, char ** argv )
{
    if ( argc != 2 ) { std::fprintf( stderr, "usage: %s <outdir>\n", argv[0] ); return 1; }
    const char * dir = argv[1];
    if ( !fx1_file( dir ) || !fx2_file( dir ) || !p1_file( dir ) || !p3_file( dir ) ||
         !keyed_file( dir ) || !pack_file( dir ) ) { return 1; }
    return 0;
}
