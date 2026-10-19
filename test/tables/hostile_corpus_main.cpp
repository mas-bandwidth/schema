// THE C++ PORT RUNS THE WHOLE SHARED HOSTILE CORPUS
// (schema-cpp-runs-hostile-corpus; docs/HOSTILE-CORPUS.md).
//
// Reads every manifest case, decodes the raw input with the carrier the
// manifest names through the generated C++ reader, and prints one line per
// case:
//
//     CASE<TAB>name<TAB>verdict<TAB>reason<TAB>base64(canonical text or '')
//
// The comparison against the manifest lives in
// internal/ci/schema_cpp_runs_hostile_corpus_test.go, which runs this binary.
// It reads files only: no network, no real time.
//
// The planted-reader-bug control does not live here: the make target builds a
// second runner from a scratch copy of the generated readers with one bug
// planted in them (make/schema-cpp-runs-hostile-corpus.mk).

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <string>
#include <vector>

#include "PackTable.h"
#include "TablesTable.h"
#include "GraphTable.h"
#include "MessagesTable.h"
#include "AssetsTable.h"
#include "ScalarsTable.h"
#include "StreamTable.h"
#include "probeTable.h"

static std::string slurp( const std::string & path, bool * found = NULL )
{
    std::string out;
    FILE * file = fopen( path.c_str(), "rb" );
    if ( found != NULL ) { *found = file != NULL; }
    if ( file == NULL ) { return out; }
    char buffer[ 65536 ];
    size_t n = 0;
    while ( ( n = fread( buffer, 1, sizeof( buffer ), file ) ) > 0 ) { out.append( buffer, n ); }
    fclose( file );
    return out;
}

static std::string b64( const std::string & in )
{
    static const char * table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    std::string out;
    size_t i = 0;
    while ( i + 2 < in.size() )
    {
        const uint32_t v = ( uint32_t( uint8_t( in[ i ] ) ) << 16 ) | ( uint32_t( uint8_t( in[ i + 1 ] ) ) << 8 ) | uint8_t( in[ i + 2 ] );
        out += table[ ( v >> 18 ) & 63 ];
        out += table[ ( v >> 12 ) & 63 ];
        out += table[ ( v >> 6 ) & 63 ];
        out += table[ v & 63 ];
        i += 3;
    }
    if ( i + 1 == in.size() )
    {
        const uint32_t v = uint32_t( uint8_t( in[ i ] ) ) << 16;
        out += table[ ( v >> 18 ) & 63 ];
        out += table[ ( v >> 12 ) & 63 ];
        out += "==";
    }
    else if ( i + 2 == in.size() )
    {
        const uint32_t v = ( uint32_t( uint8_t( in[ i ] ) ) << 16 ) | ( uint32_t( uint8_t( in[ i + 1 ] ) ) << 8 );
        out += table[ ( v >> 18 ) & 63 ];
        out += table[ ( v >> 12 ) & 63 ];
        out += table[ ( v >> 6 ) & 63 ];
        out += "=";
    }
    return out;
}

static const char * reason_name( int reason )
{
    switch ( reason )
    {
        case tabledemo::newer_form: return "newer_form";
        case tabledemo::previous_form: return "previous_form";
        case tabledemo::no_vocabulary: return "no_vocabulary";
        case tabledemo::second_announcement: return "second_announcement";
        case tabledemo::vocabulary_too_large: return "vocabulary_too_large";
        case tabledemo::message_form_as_file: return "message_form_as_file";
        case tabledemo::batch_too_large: return "batch_too_large";
        case tabledemo::no_layout: return "no_layout";
        case tabledemo::layout_malformed: return "layout_malformed";
        case tabledemo::plan_too_large: return "plan_too_large";
        case tabledemo::layout_count_mismatch: return "layout_count_mismatch";
        case tabledemo::layout_kind_unknown: return "layout_kind_unknown";
        case tabledemo::layout_kind_invalid: return "layout_kind_invalid";
        case tabledemo::layout_size_mismatch: return "layout_size_mismatch";
        case tabledemo::layout_tree_unclosed: return "layout_tree_unclosed";
        case tabledemo::layout_too_deep: return "layout_too_deep";
        case tabledemo::layout_record_too_large: return "layout_record_too_large";
        case tabledemo::layout_newer: return "layout_newer";
        case tabledemo::layout_unsupported: return "layout_unsupported";
        case tabledemo::text_ill_formed: return "text_ill_formed";
        default: return "unnamed";
    }
}

static std::string refuse_reason( int reason )
{
    return std::string( "refuse\t" ) + reason_name( reason ) + "\t";
}

template <typename T, typename R>
static std::string text_fixed( const uint8_t * data, int64_t bytes,
                               bool ( *from )( T &, const char *, int64_t, R * ),
                               int64_t ( *measure )( const T & ),
                               int64_t ( *to )( const T &, char *, int64_t ) )
{
    T value;
    R report;
    if ( !from( value, (const char *) data, bytes, &report ) || report.malformed ) { return "refuse\tdamaged\t"; }
    const int64_t n = measure( value );
    if ( n < 0 ) { return "refuse\tdamaged\t"; }
    std::string out( (size_t) n, '\0' );
    const int64_t wrote = to( value, out.empty() ? NULL : &out[ 0 ], n );
    if ( wrote < 0 ) { return "refuse\tdamaged\t"; }
    out.resize( (size_t) wrote );
    return "accept\t-\t" + b64( out );
}

template <typename B, typename T, typename R, typename A>
static std::string text_variable( const uint8_t * data, int64_t bytes,
                                  bool ( *from )( B &, const char *, int64_t, R * ),
                                  int64_t ( *measure )( const T *, A ),
                                  int64_t ( *to )( const T *, char *, int64_t, A ),
                                  A allocator )
{
    B builder;
    R report;
    if ( !from( builder, (const char *) data, bytes, &report ) || report.malformed ) { return "refuse\tdamaged\t"; }
    builder.Lock();
    const T * root = builder.AsConst();
    const int64_t n = measure( root, allocator );
    if ( n < 0 ) { return "refuse\tdamaged\t"; }
    std::string out( (size_t) n, '\0' );
    const int64_t wrote = to( root, out.empty() ? NULL : &out[ 0 ], n, allocator );
    if ( wrote < 0 ) { return "refuse\tdamaged\t"; }
    out.resize( (size_t) wrote );
    return "accept\t-\t" + b64( out );
}

static std::string text_examples( const std::string & table, const uint8_t * data, int64_t bytes )
{
    if ( table == "RootConfig" )
        return text_fixed<tabledemo::RootConfig, tabledemo::TableReport>(
            data, bytes, tabledemo::RootConfigFromJson, tabledemo::RootConfigToJsonMeasure, tabledemo::RootConfigToJson );
    if ( table == "GlobalSettings" )
        return text_fixed<tabledemo::GlobalSettings, tabledemo::TableReport>(
            data, bytes, tabledemo::GlobalSettingsFromJson, tabledemo::GlobalSettingsToJsonMeasure, tabledemo::GlobalSettingsToJson );
    if ( table == "PackConfig" )
        return text_fixed<tabledemo::PackConfig, tabledemo::TableReport>(
            data, bytes, tabledemo::PackConfigFromJson, tabledemo::PackConfigToJsonMeasure, tabledemo::PackConfigToJson );
    if ( table == "ShipEntry" )
        return text_fixed<tabledemo::ShipEntry, tabledemo::TableReport>(
            data, bytes, tabledemo::ShipEntryFromJson, tabledemo::ShipEntryToJsonMeasure, tabledemo::ShipEntryToJson );
    return "refuse\tunnamed\t";
}

static std::string text( const std::string & schema, const std::string & table, const uint8_t * data, int64_t bytes )
{
    if ( schema == "tables/examples" ) { return text_examples( table, data, bytes ); }
    if ( schema == "tables/pointers" && table == "Scene" )
        return text_variable<graphdemo::SceneBuilder, graphdemo::Scene, graphdemo::TableReport, graphdemo::TableAllocator>(
            data, bytes, graphdemo::SceneFromJson, graphdemo::SceneToJsonMeasure, graphdemo::SceneToJson, graphdemo::TableDefaultAllocator() );
    if ( schema == "tables/messages" && table == "ToolMessage" )
        return text_fixed<messagedemo::ToolMessage, messagedemo::TableReport>(
            data, bytes, messagedemo::ToolMessageFromJson, messagedemo::ToolMessageToJsonMeasure, messagedemo::ToolMessageToJson );
    if ( schema == "tables/blobs" && table == "Catalog" )
        return text_variable<blobdemo::CatalogBuilder, blobdemo::Catalog, blobdemo::TableReport, blobdemo::TableAllocator>(
            data, bytes, blobdemo::CatalogFromJson, blobdemo::CatalogToJsonMeasure, blobdemo::CatalogToJson, blobdemo::TableDefaultAllocator() );
    if ( schema == "tables/scalars" && table == "SimState" )
        return text_fixed<scalardemo::SimState, scalardemo::TableReport>(
            data, bytes, scalardemo::SimStateFromJson, scalardemo::SimStateToJsonMeasure, scalardemo::SimStateToJson );
    if ( schema == "tables/stream" && table == "Feed" )
        return text_variable<streamdemo::FeedBuilder, streamdemo::Feed, streamdemo::TableReport, streamdemo::TableAllocator>(
            data, bytes, streamdemo::FeedFromJson, streamdemo::FeedToJsonMeasure, streamdemo::FeedToJson, streamdemo::TableDefaultAllocator() );
    return "refuse\tunnamed\t";
}

template <typename T, typename R>
static std::string wire_fixed( const uint8_t * data, int64_t bytes,
                               bool ( *load )( T &, const uint8_t *, int64_t, R * ),
                               int64_t ( *measure )( const T & ),
                               int64_t ( *to )( const T &, char *, int64_t ) )
{
    T value;
    R report;
    const bool ok = load( value, data, bytes, &report );
    if ( report.refused ) { return refuse_reason( (int) report.reason ); }
    if ( !ok || report.malformed ) { return "refuse\tdamaged\t"; }
    const int64_t n = measure( value );
    if ( n < 0 ) { return "refuse\tdamaged\t"; }
    std::string out( (size_t) n, '\0' );
    const int64_t wrote = to( value, out.empty() ? NULL : &out[ 0 ], n );
    if ( wrote < 0 ) { return "refuse\tdamaged\t"; }
    out.resize( (size_t) wrote );
    return "accept\t-\t" + b64( out );
}

// The variable reader's own refusal names (TableRefuseReason), printed as the
// C++ reader spells them: a name the engine spells differently is a finding.
static const char * refuse_name( probe::TableRefuseReason reason )
{
    switch ( reason )
    {
        case probe::ok: return "ok";
        case probe::not_a_cook: return "not_a_cook";
        case probe::foreign_order: return "foreign_order";
        case probe::wrong_build_version: return "wrong_build_version";
        case probe::reserved_not_zero: return "reserved_not_zero";
        case probe::bad_alignment: return "bad_alignment";
        case probe::truncated: return "truncated";
        case probe::unaligned_base: return "unaligned_base";
        case probe::bad_layout: return "bad_layout";
        case probe::unknown_form: return "unknown_form";
        case probe::count_over_length: return "count_over_length";
        case probe::count_over_extent_cap: return "count_over_extent_cap";
        case probe::blob_over_size_cap: return "blob_over_size_cap";
        case probe::data_cycle: return "data_cycle";
        case probe::invalid_value: return "invalid_value";
        case probe::allocation_failed: return "allocation_failed";
        case probe::wire_damaged: return "wire_damaged";
        case probe::malformed: return "malformed";
        case probe::clamped: return "clamped";
        case probe::kind_mismatch: return "kind_mismatch";
        case probe::base64_damaged: return "base64_damaged";
        case probe::base64_not_canonical: return "base64_not_canonical";
        case probe::integrity_mismatch: return "integrity_mismatch";
        case probe::non_finite: return "non_finite";
        case probe::blob_shared: return "blob_shared";
        default: return "unnamed";
    }
}

static std::string wire( const std::string & schema, const std::string & table, const uint8_t * data, int64_t bytes )
{
    if ( schema == "tables/examples" && table == "ShipEntry" )
        return wire_fixed<tabledemo::ShipEntry, tabledemo::TableReport>(
            data, bytes, tabledemo::ShipEntryLoad, tabledemo::ShipEntryToJsonMeasure, tabledemo::ShipEntryToJson );
    if ( schema == "schemas/probe.schema" && table == "Root" )
    {
        probe::TableReport report;
        probe::TableRefuseReason why = probe::ok;
        const int64_t region_bytes = probe::RootLoadMeasure( data, bytes, NULL, &why );
        if ( region_bytes < 0 )
        {
            if ( why != probe::ok && why != probe::wire_damaged ) { return std::string( "refuse\t" ) + refuse_name( why ) + "\t"; }
            return "refuse\tdamaged\t";
        }
        std::vector<uint8_t> region( (size_t) ( region_bytes > 0 ? region_bytes : 1 ) );
        const probe::Root * root = probe::RootLoad( region.data(), region_bytes, data, bytes, &report );
        if ( report.refused ) { return refuse_reason( (int) report.reason ); }
        if ( root == NULL || report.malformed ) { return "refuse\tdamaged\t"; }
        const int64_t n = probe::RootToJsonMeasure( root );
        if ( n < 0 ) { return "refuse\tdamaged\t"; }
        std::string out( (size_t) n, '\0' );
        const int64_t wrote = probe::RootToJson( root, out.empty() ? NULL : &out[ 0 ], n );
        if ( wrote < 0 ) { return "refuse\tdamaged\t"; }
        out.resize( (size_t) wrote );
        return "accept\t-\t" + b64( out );
    }
    return "refuse\tunnamed\t";
}

static std::string announce( const std::string & carrier, const uint8_t * data, int64_t bytes )
{
    std::vector<tabledemo::TableMessageEntry> storage( 8192 );
    tabledemo::TableVocabulary vocabulary( storage.data(), carrier == "announce_entries" ? 1 : (int64_t) storage.size() );
    if ( carrier == "announce_bytes" ) { vocabulary.max_bytes = 8; }
    tabledemo::TableReport report;
    if ( carrier == "announce_second" )
    {
        if ( !tabledemo::AnnounceRead( vocabulary, data, bytes, &report ) )
        {
            if ( report.refused ) { return refuse_reason( (int) report.reason ); }
            return "refuse\tdamaged\t";
        }
        tabledemo::TableReport second;
        if ( !tabledemo::AnnounceRead( vocabulary, data, bytes, &second ) ) { return refuse_reason( (int) second.reason ); }
        return "accept\t-\t";
    }
    if ( !tabledemo::AnnounceRead( vocabulary, data, bytes, &report ) )
    {
        if ( report.refused ) { return refuse_reason( (int) report.reason ); }
        return "refuse\tdamaged\t";
    }
    return "accept\t-\t";
}

static std::string message( const std::string & carrier, const uint8_t * data, int64_t bytes )
{
    std::vector<messagedemo::TableMessageEntry> storage( 8192 );
    messagedemo::TableVocabulary vocabulary( storage.data(), (int64_t) storage.size() );
    messagedemo::TableReport report;
    if ( carrier == "message" )
    {
        messagedemo::TableReport announced;
        if ( !messagedemo::AnnounceRead( vocabulary, messagedemo::kTableAnnounce, messagedemo::AnnounceMeasure(), &announced ) )
        {
            if ( announced.refused ) { return refuse_reason( (int) announced.reason ); }
            return "refuse\tdamaged\t";
        }
    }
    messagedemo::ToolMessage values[ 1 ];
    int64_t count = 1;
    const bool ok = messagedemo::ToolMessageLoadMessages( values, &count, vocabulary, data, bytes, &report );
    if ( report.refused ) { return refuse_reason( (int) report.reason ); }
    if ( !ok ) { return report.malformed ? "refuse\tdamaged\t" : "refuse\tunnamed\t"; }
    if ( count != 1 ) { return "refuse\tdamaged\t"; }
    const int64_t n = messagedemo::ToolMessageToJsonMeasure( values[ 0 ] );
    if ( n < 0 ) { return "refuse\tdamaged\t"; }
    std::string out( (size_t) n, '\0' );
    const int64_t wrote = messagedemo::ToolMessageToJson( values[ 0 ], out.empty() ? NULL : &out[ 0 ], n );
    if ( wrote < 0 ) { return "refuse\tdamaged\t"; }
    out.resize( (size_t) wrote );
    return "accept\t-\t" + b64( out );
}

int main( int argc, char ** argv )
{
    if ( argc < 2 ) { fprintf( stderr, "usage: %s <corpus-dir>\n", argv[ 0 ] ); return 2; }
    const std::string dir = argv[ 1 ];
    const std::string manifest = slurp( dir + "/manifest.tsv" );
    size_t at = 0;
    while ( at <= manifest.size() )
    {
        const size_t end = manifest.find( '\n', at );
        const std::string line = manifest.substr( at, end == std::string::npos ? std::string::npos : end - at );
        at = end == std::string::npos ? manifest.size() + 1 : end + 1;
        if ( line.empty() || line[ 0 ] == '#' ) { continue; }
        std::vector<std::string> f;
        size_t start = 0;
        while ( true )
        {
            const size_t tab = line.find( '\t', start );
            f.push_back( line.substr( start, tab == std::string::npos ? std::string::npos : tab - start ) );
            if ( tab == std::string::npos ) { break; }
            start = tab + 1;
        }
        if ( f.size() != 9 ) { continue; }
        bool found = false;
        const std::string payload = slurp( dir + "/" + f[ 4 ], &found );
        if ( !found )
        {
            // a missing input is never decoded as zero bytes: it answers a
            // reason no manifest case carries, so the comparison is red
            printf( "CASE\t%s\trefuse\tmissing_input\t\n", f[ 0 ].c_str() );
            continue;
        }
        const uint8_t * data = (const uint8_t *) payload.data();
        const int64_t bytes = (int64_t) payload.size();
        std::string answer;
        if ( f[ 1 ] == "text" ) { answer = text( f[ 2 ], f[ 3 ], data, bytes ); }
        else if ( f[ 1 ] == "wire" ) { answer = wire( f[ 2 ], f[ 3 ], data, bytes ); }
        else if ( f[ 1 ] == "message" || f[ 1 ] == "message_novocab" ) { answer = message( f[ 1 ], data, bytes ); }
        else if ( f[ 1 ] == "announce" || f[ 1 ] == "announce_second" || f[ 1 ] == "announce_bytes" || f[ 1 ] == "announce_entries" )
        {
            answer = announce( f[ 1 ], data, bytes );
        }
        else { answer = "refuse\tunnamed\t"; }
        printf( "CASE\t%s\t%s\n", f[ 0 ].c_str(), answer.c_str() );
    }
    return 0;
}
