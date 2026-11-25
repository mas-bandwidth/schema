// THE C++ CONFORMANCE DRIVER (test/conformance/README.md).
//
// One process per surface. The harness hands it the derived manifest, the
// surface name and an output directory; the driver writes one file per case and
// says nothing else. Every expectation lives in the DATA — this file holds no
// literal instance, no expected byte and no expected count.
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>
//
// Exit 0 means the surface ran. Exit 2 means this backend does not implement
// it, which the matrix prints as ABSENT rather than as a failure: a backend
// that has no text form is missing a feature, not failing a test, and the
// difference is the whole reason the matrix exists.
//
// The cook's READ surfaces are not here and that is deliberate: the node dump
// and the 111-row forgery battery are already held by test/tables/cook_main.cpp,
// whose unit this one does not compile, and the shell wrapper delegates both to
// that binary rather than to a second copy of the same walk. The BLOCK surfaces
// are here, because this driver already opens the block unit — and so is
// `cook-write` (§7.6), because it is the CORPUS's instances a runtime cooks,
// and this driver is the one that already loads every one of them off the wire.
//
// THE UNITS. This file is the dispatch and the codec registry; the surfaces
// and the codec rows are compiled in units of their own, side by side
// (driver.h names each, the Makefile's CONFORMANCE_DRIVER_TUS lists them), so
// no one unit compiles every generated header.

#include <cstdio>
#include <cstring>
#include <string>

#include "driver.h"

// ---------------------------------------------------------------------------
// the registry: every family unit's rows, walked in this order
// ---------------------------------------------------------------------------
static const DriverTables * const families[] = {
    &driver_tables_examples,
    &driver_tables_evolution_a,
    &driver_tables_messages,
    &driver_tables_evolution_b,
    &driver_tables_lists,
    &driver_tables_varevo12,
    &driver_tables_varevo3,
    &driver_tables_backend,
    &driver_tables_cooktorture,
    &driver_tables_tblsurf,
    &driver_tables_maps,
    &driver_tables_pointers,
};

// the first row of one shape whose unit and root match, across every family
template <typename Row>
static const Row * find_row( Span<Row> DriverTables::*member, const std::string & unit, const std::string & root )
{
    for ( size_t f = 0; f < sizeof( families ) / sizeof( families[0] ); f++ )
    {
        const Span<Row> & rows = families[f]->*member;
        for ( size_t i = 0; i < rows.count; i++ )
        {
            if ( unit == rows.at[i].unit && root == rows.at[i].root ) return &rows.at[i];
        }
    }
    return NULL;
}

const Codec * find_codec( const std::string & unit, const std::string & root )
{
    return find_row( &DriverTables::codecs, unit, root );
}

const VarCodec * find_var_codec( const std::string & unit, const std::string & root )
{
    return find_row( &DriverTables::var_codecs, unit, root );
}

const MsgCodec * find_msg_codec( const std::string & unit, const std::string & root )
{
    return find_row( &DriverTables::msg_codecs, unit, root );
}

const RetainCodec * find_retain_codec( const std::string & unit, const std::string & root )
{
    return find_row( &DriverTables::retain_codecs, unit, root );
}

const RetainSequenceCodec * find_retain_sequence_codec( const std::string & unit, const std::string & root )
{
    return find_row( &DriverTables::retain_sequence_codecs, unit, root );
}

const ReadIntoCodec * find_read_into_codec( const std::string & unit, const std::string & root )
{
    return find_row( &DriverTables::read_into_codecs, unit, root );
}

// an announcement is the unit's, with no root
const AnnounceUnit * find_announce_unit( const std::string & unit )
{
    for ( size_t f = 0; f < sizeof( families ) / sizeof( families[0] ); f++ )
    {
        const Span<AnnounceUnit> & rows = families[f]->announce_units;
        for ( size_t i = 0; i < rows.count; i++ )
        {
            if ( unit == rows.at[i].unit ) return &rows.at[i];
        }
    }
    return NULL;
}

int main( int argc, char ** argv )
{
    if ( argc >= 2 && strcmp( argv[1], "emit-block-forgeries" ) == 0 )
    {
        return emit_block_forgeries_answer();
    }
    if ( argc < 3 )
    {
        fprintf( stderr, "usage: %s <manifest> list\n       %s <manifest> <surface> <outdir>\n", argv[0], argv[0] );
        return 2;
    }
    if ( !read_manifest( argv[1] ) ) return 1;

    const std::string surface = argv[2];
    if ( surface == "list" )
    {
        printf( "wire\nmessage\nmessage-prior\nreport\nframing\nretain\nretain-save\nretain-refusal\njson-read\njson-write\njson-hostile\ncook-write\nblock\nblock-foreign\nblock-dump\nforgery\nwrite-refusal\nwrite-refusal-fixed\nwrite-refusal-message\nwrite-refusal-json\n"
                "write-refusal-cook\nwrite-refusal-block\nwrite-refusal-shape\nannounce\nread-into\njson-write-into\n" );
        // the opt-in units this leg answers (MANIFEST.txt `opt-in`)
        printf( "unit shapes\nunit tblmoodmap\n" );
        return 0;
    }
    if ( argc < 4 )
    {
        fprintf( stderr, "usage: %s <manifest> <surface> <outdir>\n", argv[0] );
        return 2;
    }
    const std::string out = argv[3];

    if ( surface == "wire" ) return surface_wire( out );
    if ( surface == "message" ) return surface_message( out, false );
    if ( surface == "message-prior" ) return surface_message( out, true );
    if ( surface == "report" ) return surface_report( out );
    if ( surface == "framing" ) return surface_framing( out );
    if ( surface == "retain" ) return surface_retain( out );
    if ( surface == "retain-save" ) return surface_retain_save( out );
    if ( surface == "retain-refusal" ) return surface_retain_refusal( out );
    if ( surface == "json-read" ) return surface_json_read( out );
    if ( surface == "json-write" ) return surface_json_write( out ) || surface_json_write_refusal( out );
    if ( surface == "json-text" ) return surface_json_text( out );
    if ( surface == "json-hostile" ) return surface_json_hostile( out );
    if ( surface == "cook-write" ) return surface_cook_write( out );
    if ( surface == "block" ) return surface_block( out );
    if ( surface == "block-foreign" ) return surface_block_foreign( out );
    if ( surface == "block-dump" ) return surface_block_dump_answer( out );
    if ( surface == "forgery" ) return surface_forgery( out );
    if ( surface == "block-reason" ) return surface_block_reason( out );
#ifdef NDEBUG
    // C++ is an ASSERT LEG (docs/SPEC.md §5.1; the ruling `write-refusal`): its
    // write checks are schema_assert, and a release build carries none. A
    // driver built with NDEBUG answers no write-refusal row, so the harness
    // never credits a release build with a refusal.
    if ( surface.rfind( "write-refusal", 0 ) == 0 )
    {
        fprintf( stderr, "driver: %s: this C++ driver was built with NDEBUG; an assert leg answers the write contract from its debug build only\n", surface.c_str() );
        return 1;
    }
#endif
    if ( surface == "write-refusal" || surface == "write-refusal-fixed" || surface == "write-refusal-message" ||
         surface == "write-refusal-json" || surface == "write-refusal-cook" || surface == "write-refusal-block" ||
         surface == "write-refusal-shape" )
    {
        return surface_write_refusal( surface, out );
    }
    if ( surface == "announce" ) return surface_announce( out );
    if ( surface == "read-into" ) return surface_read_into( out );
    if ( surface == "json-write-into" ) return surface_json_write_into( out );
    return 2;
}
