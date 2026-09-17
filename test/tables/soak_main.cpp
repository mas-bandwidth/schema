// THE C++ TABLES SOAK (docs/SPEC-TABLES.md, "What allocates, and what never
// does"; docs/PORTING.md I9, schema#416).
//
// The read, measure, save and text paths are read over the wire corpus in a
// loop for as long as the caller asks, and what is watched is the ALLOCATION
// COUNT: every global `operator new` is counted, and the count over the
// measured loop must be zero. A live-byte sample is a LEAK instrument and
// nothing more — a path that allocates and frees the same bytes every
// iteration leaves the heap flat forever — so the count is the gate and the
// live-byte number, where the platform exposes one, is printed beside it.
//
//   ./build/schema_test_cpp_soak <seconds>
//
// The negative control compiles this file with -DSOAK_SABOTAGE, which plants a
// matched `new`/`delete` pair per iteration and requires the count gate to go
// red while the loop keeps producing the right bytes.

#include "TablesTable.h"
#include "WideTable.h"
#include "KeyedTable.h"
#include "NestedTable.h"
#include "V1Table.h"
#include "V2Table.h"
#include "P1Table.h"
#include "P3Table.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <functional>
#include <string>
#include <vector>

// ---- the counting allocator: every global operator new is one call ----

static unsigned long long schema_soak_allocs = 0;

void * operator new( size_t bytes )
{
    schema_soak_allocs++;
    void * p = malloc( bytes != 0 ? bytes : 1 );
    if ( p == NULL ) { abort(); }
    return p;
}

void * operator new[]( size_t bytes ) { return operator new( bytes ); }
void operator delete( void * p ) noexcept { free( p ); }
void operator delete[]( void * p ) noexcept { free( p ); }
void operator delete( void * p, size_t ) noexcept { free( p ); }
void operator delete[]( void * p, size_t ) noexcept { free( p ); }

// ---- one corpus row: the golden bytes, the two caller buffers, the loop body ----

struct Row
{
    const char * name;
    std::vector<uint8_t> wire;
    std::vector<uint8_t> scratch;
    std::vector<char> text;
    std::function<void()> body;
};

static std::vector<uint8_t> read_file( const char * path )
{
    std::vector<uint8_t> out;
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { fprintf( stderr, "soak: cannot open %s\n", path ); exit( 1 ); }
    fseek( f, 0, SEEK_END );
    long size = ftell( f );
    fseek( f, 0, SEEK_SET );
    if ( size > 0 )
    {
        out.resize( (size_t) size );
        if ( fread( out.data(), 1, (size_t) size, f ) != (size_t) size ) { fprintf( stderr, "soak: cannot read %s\n", path ); exit( 1 ); }
    }
    fclose( f );
    return out;
}

// one row, for one generated root type. The value and the text buffer are the
// CALLER's, allocated once here; the loop body only loads, measures, saves and
// reads the text back into them.
template <typename Value, typename Report>
static void add_row( std::vector<Row> & rows, const char * name, const char * wire_path,
                     bool ( *load )( Value &, const uint8_t *, int64_t, Report * ),
                     int64_t ( *measure )( const Value & ),
                     int64_t ( *save )( const Value &, uint8_t *, int64_t ),
                     int64_t ( *to_json_measure )( const Value & ),
                     int64_t ( *to_json )( const Value &, char *, int64_t ),
                     bool ( *from_json )( Value &, const char *, int64_t, Report * ) )
{
    Row row;
    row.name = name;
    row.wire = read_file( wire_path );
    Value * value = new Value();
    Report report{};
    load( *value, row.wire.data(), (int64_t) row.wire.size(), &report );
    const int64_t scratch_bytes = measure( *value );
    const int64_t text_bytes = to_json_measure( *value );
    if ( scratch_bytes < 0 || text_bytes < 0 ) { fprintf( stderr, "soak: %s does not measure\n", name ); exit( 1 ); }
    row.scratch.resize( (size_t) scratch_bytes + 1 );
    row.text.resize( (size_t) text_bytes + 1 );
    // THE GOLDEN GATE, before the clock: an exact case must re-save to its own
    // bytes, so the loop is exercising a codec and not an accident.
    if ( save( *value, row.scratch.data(), (int64_t) row.scratch.size() ) != (int64_t) row.wire.size() ||
         memcmp( row.scratch.data(), row.wire.data(), row.wire.size() ) != 0 )
    {
        fprintf( stderr, "soak: %s does not re-save to its own bytes — refusing to soak a codec that does not "
                 "reproduce the corpus\n", name );
        exit( 1 );
    }
    uint8_t * wire = row.wire.data();
    const int64_t wire_bytes = (int64_t) row.wire.size();
    uint8_t * scratch = row.scratch.data();
    char * text = row.text.data();
    row.body = [value, report, wire, wire_bytes, scratch, text, load, measure, save, to_json_measure, to_json, from_json]() mutable
    {
        Report local{};
        if ( !load( *value, wire, wire_bytes, &local ) ) { fprintf( stderr, "soak: the golden stopped loading\n" ); abort(); }
        const int64_t bytes = measure( *value );
        if ( save( *value, scratch, bytes ) != bytes ) { fprintf( stderr, "soak: save did not write measure's answer\n" ); abort(); }
        const int64_t t = to_json_measure( *value );
        if ( to_json( *value, text, t ) != t ) { fprintf( stderr, "soak: ToJson did not write its own measure\n" ); abort(); }
        Report back{};
        if ( !from_json( *value, text, t, &back ) ) { fprintf( stderr, "soak: the text did not read back\n" ); abort(); }
#ifdef SOAK_SABOTAGE
        // NEGATIVE CONTROL: a MATCHED pair per iteration — the live bytes
        // return to where they were, so only the COUNT can see it.
        char * planted = new char[1];
        delete[] planted;
#endif
    };
    rows.push_back( std::move( row ) );
}

int main( int argc, char ** argv )
{
    const double seconds = argc > 1 ? strtod( argv[1], NULL ) : 20.0;
    std::vector<Row> rows;

    add_row( rows, "root_full", "testdata/wire/tables/root_full.bin",
             tabledemo::RootConfigLoad, tabledemo::RootConfigMeasure, tabledemo::RootConfigSave,
             tabledemo::RootConfigToJsonMeasure, tabledemo::RootConfigToJson, tabledemo::RootConfigFromJson );
    add_row( rows, "root_default", "testdata/wire/tables/root_default.bin",
             tabledemo::RootConfigLoad, tabledemo::RootConfigMeasure, tabledemo::RootConfigSave,
             tabledemo::RootConfigToJsonMeasure, tabledemo::RootConfigToJson, tabledemo::RootConfigFromJson );
    add_row( rows, "profile_elide", "testdata/wire/tables/profile_elide.bin",
             tabledemo::ProfileConfigLoad, tabledemo::ProfileConfigMeasure, tabledemo::ProfileConfigSave,
             tabledemo::ProfileConfigToJsonMeasure, tabledemo::ProfileConfigToJson, tabledemo::ProfileConfigFromJson );
    add_row( rows, "loadout_full", "testdata/wire/tables/loadout_full.bin",
             tabledemo::LoadoutConfigLoad, tabledemo::LoadoutConfigMeasure, tabledemo::LoadoutConfigSave,
             tabledemo::LoadoutConfigToJsonMeasure, tabledemo::LoadoutConfigToJson, tabledemo::LoadoutConfigFromJson );
    add_row( rows, "wide_blob", "testdata/wire/tables/wide_blob.bin",
             tabledemo::WideBlobLoad, tabledemo::WideBlobMeasure, tabledemo::WideBlobSave,
             tabledemo::WideBlobToJsonMeasure, tabledemo::WideBlobToJson, tabledemo::WideBlobFromJson );
    add_row( rows, "archive", "testdata/wire/tables/archive.bin",
             tabledemo::ArchiveConfigLoad, tabledemo::ArchiveConfigMeasure, tabledemo::ArchiveConfigSave,
             tabledemo::ArchiveConfigToJsonMeasure, tabledemo::ArchiveConfigToJson, tabledemo::ArchiveConfigFromJson );
    add_row( rows, "keyed_config", "testdata/wire/tables/keyed_config.bin",
             tabledemo::KeyedConfigLoad, tabledemo::KeyedConfigMeasure, tabledemo::KeyedConfigSave,
             tabledemo::KeyedConfigToJsonMeasure, tabledemo::KeyedConfigToJson, tabledemo::KeyedConfigFromJson );
    add_row( rows, "keyed_default", "testdata/wire/tables/keyed_default.bin",
             tabledemo::KeyedConfigLoad, tabledemo::KeyedConfigMeasure, tabledemo::KeyedConfigSave,
             tabledemo::KeyedConfigToJsonMeasure, tabledemo::KeyedConfigToJson, tabledemo::KeyedConfigFromJson );
    add_row( rows, "v1_cfg", "testdata/wire/tables/v1_cfg.bin",
             tblv1::CfgLoad, tblv1::CfgMeasure, tblv1::CfgSave,
             tblv1::CfgToJsonMeasure, tblv1::CfgToJson, tblv1::CfgFromJson );
    add_row( rows, "v2_cfg", "testdata/wire/tables/v2_cfg.bin",
             tblv2::CfgLoad, tblv2::CfgMeasure, tblv2::CfgSave,
             tblv2::CfgToJsonMeasure, tblv2::CfgToJson, tblv2::CfgFromJson );
    add_row( rows, "chain_value", "testdata/wire/tables/chain_value.bin",
             tblp1::ChainLoad, tblp1::ChainMeasure, tblp1::ChainSave,
             tblp1::ChainToJsonMeasure, tblp1::ChainToJson, tblp1::ChainFromJson );
    add_row( rows, "chain_optional", "testdata/wire/tables/chain_optional.bin",
             tblp3::ChainLoad, tblp3::ChainMeasure, tblp3::ChainSave,
             tblp3::ChainToJsonMeasure, tblp3::ChainToJson, tblp3::ChainFromJson );

    // one untimed pass, so a lazily built descriptor or a first-touch page is
    // charged to the setup rather than to the measured loop
    for ( Row & row : rows ) { row.body(); }

    schema_soak_allocs = 0;
    const time_t start = time( NULL );
    unsigned long long iterations = 0;
    for ( ;; )
    {
        for ( Row & row : rows ) { row.body(); }
        iterations++;
        if ( ( iterations & 0x3ff ) == 0 && difftime( time( NULL ), start ) >= seconds ) { break; }
    }

    printf( "cpp tables soak: %llu iterations over %u cases in %.0f s\n",
            iterations, (unsigned) rows.size(), difftime( time( NULL ), start ) );
    printf( "cpp tables soak: %llu allocator call(s) over %llu iterations\n",
            schema_soak_allocs, iterations );
    if ( schema_soak_allocs != 0 )
    {
        fprintf( stderr, "SOAK FAILED: the read, measure, save and text paths made %llu allocator call(s) over "
                 "%llu iterations — they allocate NOTHING, and a live-byte sample cannot see a matched "
                 "new/delete pair because the number returns to where it was\n",
                 schema_soak_allocs, iterations );
        return 1;
    }
    printf( "cpp tables soak: ZERO allocator calls — the read, measure, save and text paths allocate nothing\n" );
    return 0;
}
