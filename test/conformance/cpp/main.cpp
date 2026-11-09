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

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <new>
#include <string>
#include <vector>

#include "TablesTable.h"
#include "WideTable.h"
#include "FeaturesTable.h"
#include "NestedTable.h"
#include "KeyedTable.h"
#include "PackTable.h"
#include "V1Table.h"
#include "V2Table.h"
#include "P1Table.h"
#include "P2Table.h"
#include "P3Table.h"
// the WIDE-SCALAR unit and its evolved twin (docs/SPEC-TABLES.md §3, §4)
#include "ScalarsTable.h"
#include "WidesTable.h"
#include "Scalars2Table.h"
// the MESSAGE FORM's units (docs/SPEC-TABLES.md §3.3): the three backend
// messages the ruling measured, and the WIDE-VOCABULARY unit whose slots fall
// on both sides of the one-byte boundary
#include "BackendTable.h"
#include "VocabTable.h"
#include "Vocab9Table.h"
// the RETAIN-UNKNOWN reader (docs/SPEC-TABLES.md §6.6): the older build, which
// cannot name what RT2 and RT3 wrote. Only the READER is here, the two newer
// builds are writers of the pinned vectors and this driver never runs one.
#include "RT1Table.h"
// the POINTERED unit (docs/SPEC-TABLES.md §6.2): a region and a root pointer,
// never a value, which is why it gets its own row shape below
#include "GraphTable.h"
#include "MarksTable.h"
#include "PartsTable.h"
#include "RenderBlock.h"
#include "PaddedBlock.h"
// the MESSAGE units (docs/SPEC-TABLES.md §2.6): a union whose arms are tables,
// fixed in messagedemo and with a variable arm in streamdemo, plus the
// message evolution pair
#include "MessagesTable.h"
#include "StreamTable.h"
#include "M1Table.h"
#include "M2Table.h"
#include "A1Table.h"
#include "A2Table.h"
#include "K1Table.h"
#include "K2Table.h"
#include "G1Table.h"
#include "W1Table.h"
#include "W2Table.h"
#include "R1Table.h"
#include "R2Table.h"
// the BYTE BUFFER unit (docs/SPEC-TABLES.md §2.5): a blob at its used size,
// pointed at — a variable root like any pointered one
#include "AssetsTable.h"
#include "CellsTable.h"
#include "ChunksTable.h"
#include "CrewsTable.h"
#include "DepthTable.h"
#include "DocsTable.h"
#include "FleetTable.h"
#include "PairsTable.h"
#include "RowsTable.h"
#include "RunsTable.h"
#include "SlotsTable.h"
#include "SpansTable.h"
#include "TextTable.h"
#include "TrailsTable.h"
#include "SaveTable.h"
#include "HoldersTable.h"
#include "SharedTable.h"
#include "MigrateTable.h"
#include "ReportTable.h"

// ---------------------------------------------------------------------------
// the manifest, read exactly as testdata/conformance/tables/FORMAT.md states it
// ---------------------------------------------------------------------------

struct Line
{
    std::vector<std::string> field;
};

static std::vector<Line> manifest_lines;

static bool read_manifest( const char * path )
{
    FILE * file = fopen( path, "r" );
    if ( file == NULL )
    {
        fprintf( stderr, "driver: cannot open %s\n", path );
        return false;
    }
    std::string text;
    int c;
    while ( ( c = fgetc( file ) ) != EOF )
    {
        if ( c == '\n' )
        {
            if ( !text.empty() && text[0] != '#' )
            {
                Line line;
                size_t i = 0;
                while ( i < text.size() )
                {
                    while ( i < text.size() && ( text[i] == ' ' || text[i] == '\t' || text[i] == '\r' ) ) i++;
                    size_t start = i;
                    while ( i < text.size() && text[i] != ' ' && text[i] != '\t' && text[i] != '\r' ) i++;
                    if ( i > start ) line.field.push_back( text.substr( start, i - start ) );
                }
                if ( !line.field.empty() && line.field[0][0] != '#' ) manifest_lines.push_back( line );
            }
            text.clear();
            continue;
        }
        text.push_back( (char) c );
    }
    fclose( file );
    return true;
}

// ---------------------------------------------------------------------------
// files
// ---------------------------------------------------------------------------

static bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * file = fopen( path, "rb" );
    if ( file == NULL ) return false;
    fseek( file, 0, SEEK_END );
    long size = ftell( file );
    fseek( file, 0, SEEK_SET );
    out.resize( size > 0 ? (size_t) size : 0 );
    bool ok = size == 0 || fread( out.data(), 1, (size_t) size, file ) == (size_t) size;
    fclose( file );
    return ok;
}

static bool spill( const std::string & dir, const std::string & name, const void * data, size_t bytes )
{
    std::string path = dir + "/" + name;
    FILE * file = fopen( path.c_str(), "wb" );
    if ( file == NULL )
    {
        fprintf( stderr, "driver: cannot write %s\n", path.c_str() );
        return false;
    }
    bool ok = bytes == 0 || fwrite( data, 1, bytes, file ) == bytes;
    fclose( file );
    return ok;
}

// ---------------------------------------------------------------------------
// the codec table: one row per (unit, root) the corpus names
// ---------------------------------------------------------------------------
//
// Every row is the SAME five function pointers, so a surface is one loop over
// this table and not a switch per root. A root with no text form carries NULLs
// in the last three columns and the driver reports the surface absent for it.

// Each unit declares its OWN TableReport — the generated surface is namespaced
// whole (§6.1) — so the driver carries one report shape of its own and each row
// copies into it. Every read has one outcome, the whole value or a refusal
// (docs/SPEC-TABLES.md §4, §6.5), so the verdict is the refusal flag and the two
// counters that survive the strict read ride beside it as diagnostics. The
// harness compares the verdict, never a counter (test/conformance/README.md).
struct Report
{
    int unknown, widened;
    bool refused;
    bool blob_shared; // the refusal's reason was a blob two slots name (§2.5)
};

struct Codec
{
    const char * unit;
    const char * root;
    // the erased shapes: the template below is what restores the type
    bool ( *load )( void *, const uint8_t *, int64_t, Report * );
    int64_t ( *measure )( const void * );
    int64_t ( *save )( const void *, uint8_t *, int64_t );
    bool ( *from_json )( void *, const char *, int64_t, Report * );
    int64_t ( *to_json_measure )( const void * );
    int64_t ( *to_json )( const void *, char *, int64_t );
    // the COOK's write side (§7.6). The byte order is an int here — 0 little,
    // 1 big — because each unit spells the enum in its own namespace, and the
    // order is a parameter of the WRITE rather than a fact of the host.
    int64_t ( *cook_measure )( const void * );
    bool ( *cook )( const void *, void *, uint64_t, int );
    void * ( *make )();
    void ( *reset )( void * );
    // THE FORM-1 READ OF THE SAME DECLARATION, for the framing battery alone:
    // its vectors are form-1 files and the rules they pin are the variable
    // form's (docs/LAYERS/1-framing.md), so a root declared `fixed table` is
    // read there as the oracle reads it, with its class variable
    // (test/conformance/harness/generate.go, framingVariableUnit).
    bool ( *load_form1 )( void *, const uint8_t *, int64_t, Report * );
};

// A read is refused when the plain name returned its false, -1 or NULL; the
// report says so too, and either is enough, so a row cannot print `read` for a
// read its report refused.
template <typename T>
static void copy_report( const T & from, Report * to )
{
    to->unknown = from.unknown;
    to->widened = from.widened;
    to->refused = from.refused || from.damaged || (int) from.refuse_reason != 0;
    to->blob_shared = (int) from.refuse_reason == 24; // blob_shared is reason 24 in the one vocabulary
}

template <typename T>
struct Erase
{
    static T & value()
    {
        static T storage;
        return storage;
    }
    static void * make() { return (void *) &value(); }
    static void reset( void * p ) { new ( p ) T(); }
};

// A FIXED root's file is FORM 3 (docs/SPEC-TABLES.md §3.4) and a reader that
// expects a fixed table refuses every other form, so the codec reads and writes
// it through the fixed-form names, one record: FixedLoad is the plain strict
// read of that form (-1 refuses the whole load), and FixedSave the writer whose
// bytes the wire goldens hold. The plan storage is the caller's (§3.4), roomy
// enough for any writer's layout the corpus carries; no cache is shared.
static const int32_t kFixedPlanCapacity = 4096;

#define CODEC( unit_key, ns, type )                                                            \
    {                                                                                          \
        unit_key, #type,                                                                       \
        []( void * v, const uint8_t * b, int64_t n, Report * r ) {                             \
            static std::vector<ns::TableFixedEntry> plan( (size_t) kFixedPlanCapacity );       \
            ns::TableReport inner;                                                             \
            const int64_t count = ns::type##FixedLoad( (ns::type *) v, 1, b, n, plan.data(),   \
                                                       kFixedPlanCapacity, NULL, &inner );     \
            copy_report( inner, r );                                                           \
            if ( count != 1 ) r->refused = true;                                               \
            return count == 1;                                                                 \
        },                                                                                     \
        []( const void * ) { return ns::type##FixedMeasure( 1 ); },                            \
        []( const void * v, uint8_t * b, int64_t n ) {                                         \
            return ns::type##FixedSave( (const ns::type *) v, 1, b, n );                       \
        },                                                                                     \
        []( void * v, const char * t, int64_t n, Report * r ) {                                \
            ns::TableReport inner;                                                             \
            bool ok = ns::type##FromJson( *(ns::type *) v, t, n, &inner );             \
            copy_report( inner, r );                                                           \
            return ok;                                                                         \
        },                                                                                     \
        []( const void * v ) { return ns::type##ToJsonMeasure( *(const ns::type *) v ); },     \
        []( const void * v, char * t, int64_t n ) {                                            \
            return ns::type##ToJson( *(const ns::type *) v, t, n );                            \
        },                                                                                     \
        []( const void * v ) { return ns::type##CookMeasure( *(const ns::type *) v ); },       \
        []( const void * v, void * o, uint64_t c, int big ) {                                  \
            return ns::type##Cook( *(const ns::type *) v, o, c,                                \
                                   big ? ns::TableByteOrder::Big                               \
                                       : ns::TableByteOrder::Little );                         \
        },                                                                                     \
        Erase<ns::type>::make, Erase<ns::type>::reset,                                         \
        []( void * v, const uint8_t * b, int64_t n, Report * r ) {                             \
            ns::TableReport inner;                                                             \
            bool ok = ns::type##Load( *(ns::type *) v, b, n, &inner );                         \
            copy_report( inner, r );                                                           \
            return ok;                                                                         \
        }                                                                                      \
    }

// A FIXED root past §3.4's 65536-byte record bound HAS NO FILE FORM: no form 3
// carries it, and a reader that expects a fixed table refuses every other form,
// so every file read of it refuses and nothing is saved. The text and the cook
// still reach it, which is the rest of the row.
#define NOFILE_CODEC( unit_key, ns, type )                                                     \
    {                                                                                          \
        unit_key, #type,                                                                       \
        []( void *, const uint8_t *, int64_t, Report * r ) {                                   \
            *r = Report();                                                                     \
            r->refused = true;                                                                 \
            return false;                                                                      \
        },                                                                                     \
        []( const void * ) { return (int64_t) -1; },                                           \
        []( const void *, uint8_t *, int64_t ) { return (int64_t) -1; },                       \
        []( void * v, const char * t, int64_t n, Report * r ) {                                \
            ns::TableReport inner;                                                             \
            bool ok = ns::type##FromJson( *(ns::type *) v, t, n, &inner );                     \
            copy_report( inner, r );                                                           \
            return ok;                                                                         \
        },                                                                                     \
        []( const void * v ) { return ns::type##ToJsonMeasure( *(const ns::type *) v ); },     \
        []( const void * v, char * t, int64_t n ) {                                            \
            return ns::type##ToJson( *(const ns::type *) v, t, n );                            \
        },                                                                                     \
        []( const void * v ) { return ns::type##CookMeasure( *(const ns::type *) v ); },       \
        []( const void * v, void * o, uint64_t c, int big ) {                                  \
            return ns::type##Cook( *(const ns::type *) v, o, c,                                \
                                   big ? ns::TableByteOrder::Big                               \
                                       : ns::TableByteOrder::Little );                         \
        },                                                                                     \
        Erase<ns::type>::make, Erase<ns::type>::reset,                                         \
        []( void * v, const uint8_t * b, int64_t n, Report * r ) {                             \
            ns::TableReport inner;                                                             \
            bool ok = ns::type##Load( *(ns::type *) v, b, n, &inner );                         \
            copy_report( inner, r );                                                           \
            return ok;                                                                         \
        }                                                                                      \
    }

static const Codec codecs[] = {
    CODEC( "tabledemo", tabledemo, RootConfig ),
    CODEC( "tabledemo", tabledemo, ProfileConfig ),
    CODEC( "tabledemo", tabledemo, LoadoutConfig ),
    NOFILE_CODEC( "tabledemo", tabledemo, WideBlob ),
    CODEC( "tabledemo", tabledemo, ArchiveConfig ),
    CODEC( "tabledemo", tabledemo, KeyedConfig ),
    CODEC( "tabledemo", tabledemo, PackConfig ),
    CODEC( "tblv1", tblv1, Cfg ),
    CODEC( "tblv2", tblv2, Cfg ),
    CODEC( "tblp1", tblp1, Chain ),
    CODEC( "tblp3", tblp3, Chain ),
    CODEC( "messagedemo", messagedemo, ToolMessage ),
    CODEC( "messagedemo", messagedemo, Edit ),
    CODEC( "tblm1", tblm1, Msg ),
    CODEC( "tblm2", tblm2, Msg ),
    CODEC( "tbla1", tbla1, Root ),
    CODEC( "tbla2", tbla2, Root ),
    CODEC( "tblk1", tblk1, Root ),
    CODEC( "tblk2", tblk2, Root ),
    CODEC( "tblr1", tblr1, Cfg ),
    CODEC( "listdemo", listdemo, Bounded ),
    CODEC( "tblr2", tblr2, Cfg ),
    CODEC( "scalars", scalardemo, SimState ),
    CODEC( "tblscalars2", scalardemo2, SimState ),
    // the MESSAGE FORM's units (docs/SPEC-TABLES.md §3.3): their FILE-form
    // vectors are ordinary instances and ride every surface an instance rides
    CODEC( "backenddemo", backenddemo, LoginRequest ),
    CODEC( "backenddemo", backenddemo, MatchResult ),
    CODEC( "backenddemo", backenddemo, StorePurchase ),
    CODEC( "vocabdemo", vocabdemo, Wide00 ),
    CODEC( "vocabdemo", vocabdemo, Wide09 ),
    CODEC( "vocab9demo", vocab9demo, Wide00 ),
    CODEC( "vocab9demo", vocab9demo, Wide19 ),
};

// ---------------------------------------------------------------------------
// the VARIABLE class's rows (docs/SPEC-TABLES.md §6.2)
// ---------------------------------------------------------------------------
//
// A pointered root is not held by value: it is read through a REGION and a root
// pointer, and the caller owns the region; its text is read into a BUILDER,
// whose arena is where every node comes from (§16.7). So it cannot share the
// table above — that row's columns take a `T &` that does not exist here — and
// it gets its own, with the same erasure and the same one-loop-per-surface
// shape. The two text columns carry the whole of a surface's step, because the
// builder lives inside the call: `json_read` reads the text into a fresh
// builder and answers the bytes it saves, `to_json` writes a region root.
struct VarCodec
{
    const char * unit;
    const char * root;
    const void * ( *load )( std::vector<uint8_t> &, const uint8_t *, int64_t, Report * );
    int64_t ( *measure )( const void * );
    int64_t ( *save )( const void *, uint8_t *, int64_t );
    // the COOK's write side over a REGION root (§7.6): the numbering it runs
    // allocates through the default pair, which is the hook pair
    int64_t ( *cook_measure )( const void * );
    bool ( *cook )( const void *, void *, uint64_t, int );
    bool ( *json_read )( const char *, int64_t, Report *, std::vector<uint8_t> & );
    bool ( *to_json )( const void *, std::vector<char> & );
};

#define VARCODEC( unit_key, ns, type )                                                         \
    {                                                                                          \
        unit_key, #type,                                                                       \
        []( std::vector<uint8_t> & region, const uint8_t * b, int64_t n, Report * r )           \
            -> const void * {                                                                  \
            int64_t need = ns::type##LoadMeasure( b, n );                                       \
            region.assign( (size_t) need, 0 );                                                  \
            ns::TableReport inner;                                                              \
            const ns::type * root = ns::type##Load( region.data(), need, b, n, &inner ); \
            copy_report( inner, r );                                                            \
            return (const void *) root;                                                         \
        },                                                                                     \
        []( const void * v ) { return ns::type##Measure( (const ns::type *) v ); },             \
        []( const void * v, uint8_t * b, int64_t n ) {                                          \
            return ns::type##Save( (const ns::type *) v, b, n );                                \
        },                                                                                     \
        []( const void * v ) { return ns::type##CookMeasure( (const ns::type *) v ); },         \
        []( const void * v, void * o, uint64_t c, int big ) {                                   \
            return ns::type##Cook( (const ns::type *) v, o, c,                                  \
                                   big ? ns::TableByteOrder::Big                               \
                                       : ns::TableByteOrder::Little );                         \
        },                                                                                     \
        []( const char * t, int64_t n, Report * r, std::vector<uint8_t> & bytes ) {             \
            ns::type##Builder builder;                                                          \
            ns::TableReport inner;                                                              \
            bool ok = ns::type##FromJson( builder, t, n, &inner );                      \
            copy_report( inner, r );                                                            \
            if ( !ok ) return false;                                                            \
            int64_t size = ns::type##Measure( builder );                                        \
            if ( size < 0 ) return false;                                                       \
            bytes.assign( (size_t) size, 0 );                                                   \
            return ns::type##Save( builder, bytes.data(), size ) == size;                        \
        },                                                                                     \
        []( const void * v, std::vector<char> & text ) {                                        \
            int64_t size = ns::type##ToJsonMeasure( (const ns::type *) v );                      \
            if ( size < 0 ) return false;                                                       \
            text.assign( (size_t) size + 1, 0 );                                                \
            text.resize( (size_t) size );                                                       \
            return ns::type##ToJson( (const ns::type *) v, text.data(), size ) == size;          \
        }                                                                                      \
    }

static const VarCodec var_codecs[] = {
    VARCODEC( "mapdemo", mapdemo, Cells ),
    VARCODEC( "mapdemo", mapdemo, Chunks ),
    VARCODEC( "mapdemo", mapdemo, Crews ),
    VARCODEC( "mapdemo", mapdemo, Depth ),
    VARCODEC( "mapdemo", mapdemo, Docs ),
    VARCODEC( "mapdemo", mapdemo, Fleet ),
    VARCODEC( "mapdemo", mapdemo, Pairs ),
    VARCODEC( "mapdemo", mapdemo, Slots ),
    VARCODEC( "mapdemo", mapdemo, Spans ),
    VARCODEC( "mapdemo", mapdemo, Text ),
    VARCODEC( "mapdemo", mapdemo, Trails ),
    VARCODEC( "mapdemo", mapdemo, Runs ),
    VARCODEC( "mapdemo", mapdemo, Row ),
    VARCODEC( "mapdemo", mapdemo, WideRow ),
    VARCODEC( "mapdemo", mapdemo, EdgeRow ),
    VARCODEC( "listdemo", listdemo, Save ),
    VARCODEC( "listdemo", listdemo, Mixed ),
    VARCODEC( "listdemo", listdemo, Album ),
    VARCODEC( "listdemo", listdemo, Sheet ),
    VARCODEC( "listdemo", listdemo, Army ),
    VARCODEC( "listdemo", listdemo, Unbounded ),
    VARCODEC( "listdemo", listdemo, Ints ),
    VARCODEC( "listdemo", listdemo, Squad ),
    VARCODEC( "widelist", widelistdemo, Wide ),

    VARCODEC( "graphdemo", graphdemo, Scene ),
    VARCODEC( "graphdemo", graphdemo, ListNode ),
    VARCODEC( "tblp2", tblp2, Chain ),
    VARCODEC( "streamdemo", streamdemo, Feed ),
    VARCODEC( "blobdemo", blobdemo, Catalog ),
    VARCODEC( "tblg1", tblg1, Guarded ),
    VARCODEC( "tblw1", tblw1, Fleet ),
    VARCODEC( "tblw2", tblw2, Fleet ),
    VARCODEC( "featurewitnesses", featurewitnesses, Root ),
    VARCODEC( "featurewitnesses", featurewitnesses, ArrayRoot ),
};

// ---------------------------------------------------------------------------
// the MESSAGE FORM's rows (docs/SPEC-TABLES.md §3.3)
// ---------------------------------------------------------------------------
//
// A message is one round trip against the CONNECTION's announced table: the
// announcement is read first, into this direction's table, and the message
// resolves against it. That is the whole surface, so one column carries it. The
// message form is fixed tables only (§3.3), so every root here is a fixed
// table.
struct MsgCodec
{
    const char * unit;
    const char * root;
    bool ( *round_trip )( const uint8_t * announcement, int64_t announcement_bytes,
                          const uint8_t * message, int64_t message_bytes,
                          std::vector<uint8_t> & out, Report * r );
};

#define MSGCODEC( unit_key, ns, type )                                                          \
    {                                                                                           \
        unit_key, #type,                                                                        \
        []( const uint8_t * a, int64_t an, const uint8_t * b, int64_t n,                        \
            std::vector<uint8_t> & out, Report * r ) {                                          \
            std::vector<ns::TableMessageEntry> entries( (size_t) ns::kTableMessageEntriesHere ); \
            ns::TableVocabulary vocabulary( entries.data(), ns::kTableMessageEntriesHere );     \
            ns::TableReport inner;                                                              \
            if ( !ns::AnnounceRead( vocabulary, a, an, &inner ) ) { copy_report( inner, r ); return false; } \
            ns::type value;                                                                     \
            ns::type##Reset( value );                                                           \
            int64_t count = 1;                                                                  \
            if ( !ns::type##LoadMessages( &value, &count, vocabulary, b, n, &inner ) ) { copy_report( inner, r ); return false; } \
            copy_report( inner, r );                                                            \
            int64_t size = ns::type##MeasureMessages( &value, count, &inner );                  \
            if ( size < 0 ) return false;                                                       \
            out.assign( (size_t) size, 0 );                                                     \
            return ns::type##SaveMessages( &value, count, out.data(), size, &inner ) == size;   \
        }                                                                                       \
    }


static const MsgCodec msg_codecs[] = {
    MSGCODEC( "backenddemo", backenddemo, LoginRequest ),
    MSGCODEC( "backenddemo", backenddemo, MatchResult ),
    MSGCODEC( "backenddemo", backenddemo, StorePurchase ),
    MSGCODEC( "vocabdemo", vocabdemo, Wide00 ),
    MSGCODEC( "vocabdemo", vocabdemo, Wide09 ),
    MSGCODEC( "vocab9demo", vocab9demo, Wide00 ),
    MSGCODEC( "vocab9demo", vocab9demo, Wide19 ),
};

// ---------------------------------------------------------------------------
// RETAIN-UNKNOWN's rows (docs/SPEC-TABLES.md §6.6)
// ---------------------------------------------------------------------------
//
// A retain row is a REGION ROUND TRIP and only that: LoadRetain into a region,
// then MeasureRetain and SaveRetain out of that same region. The row's two
// capacities are RULES rather than numbers, `short` is one byte under what
// this port's own full load used, because a record's byte cost is the port's
// own, so the codec below runs the load twice where the row asks for it.
struct RetainAnswer
{
    int32_t retained = 0;
    int32_t retain_lost = 0;
    int32_t unknown = 0;
    int32_t save_lost = 0;
    bool read = false;          // the retaining read accepted the whole value
    std::vector<char> text;     // the §16 text of the known value it read
    std::vector<uint8_t> saved; // the save's bytes
};

struct RetainCodec
{
    const char * unit;
    const char * root;
    bool ( *run )( const uint8_t * wire, int64_t wire_bytes,
                   bool short_buffer, int64_t id_capacity, RetainAnswer & answer );
};

// the roomy capacity a `full` row takes, the page's own C/8 bound beside it
static const int64_t kRetainRoomy = 1 << 20;

#define RETAINCODEC( unit_key, ns, type )                                                       \
    {                                                                                           \
        unit_key, #type,                                                                        \
        []( const uint8_t * wire, int64_t n,                                                    \
            bool short_buffer, int64_t id_capacity, RetainAnswer & answer ) {                   \
            std::vector<uint8_t> bytes( (size_t) kRetainRoomy, 0 );                             \
            std::vector<ns::TableRetain::Id> ids(                                               \
                (size_t) ( id_capacity < 0 ? kRetainRoomy / 8 : id_capacity ) );                \
            int64_t need = ns::type##LoadMeasure( wire, n );                                    \
            if ( need < 0 ) return true; /* measured as a refusal: no value to answer */        \
            int64_t capacity = kRetainRoomy;                                                    \
            if ( short_buffer )                                                                 \
            {                                                                                   \
                /* ONE BYTE SHORT OF THE LAST RECORD, which is this port's own  */              \
                /* full load less one: a record's byte cost is the port's (§6.6) */             \
                std::vector<uint8_t> probe_region( (size_t) need, 0 );                          \
                ns::TableRetain probe;                                                          \
                probe.bytes = bytes.data();                                                     \
                probe.capacity = kRetainRoomy;                                                  \
                probe.ids = ids.data();                                                         \
                probe.id_capacity = (int32_t) ids.size();                                       \
                ns::TableReport ignored;                                                        \
                if ( ns::type##LoadRetain( probe_region.data(), need, wire, n, &probe, &ignored ) == NULL ) return true; \
                capacity = probe.used - 1;                                                      \
            }                                                                                   \
            std::vector<uint8_t> region( (size_t) need, 0 );                                    \
            ns::TableRetain retain;                                                             \
            retain.bytes = bytes.data();                                                        \
            retain.capacity = capacity;                                                         \
            retain.ids = ids.data();                                                            \
            retain.id_capacity = (int32_t) ids.size();                                          \
            ns::TableReport load;                                                               \
            const ns::type * root = ns::type##LoadRetain( region.data(), need, wire, n, &retain, &load ); \
            if ( root == NULL ) return true; /* a refusal is an answer: `refused`, no value */   \
            answer.read = true;                                                                 \
            const int64_t text_size = ns::type##ToJsonMeasure( root );                           \
            if ( text_size < 0 ) return false;                                                  \
            answer.text.assign( (size_t) text_size + 1, 0 );                                    \
            if ( ns::type##ToJson( root, answer.text.data(), text_size ) != text_size ) return false; \
            answer.text.resize( (size_t) text_size );                                           \
            answer.retained = load.retained;                                                    \
            answer.retain_lost = load.retain_lost;                                              \
            answer.unknown = load.unknown;                                                      \
            ns::TableReport save;                                                               \
            int64_t size = ns::type##MeasureRetain( root, &retain );                            \
            if ( size < 0 ) return false;                                                       \
            answer.saved.assign( (size_t) size, 0 );                                            \
            if ( ns::type##SaveRetain( root, &retain, answer.saved.data(), size, &save ) != size ) return false; \
            answer.save_lost = save.retain_lost;                                                \
            return true;                                                                        \
        }                                                                                       \
    }


static const RetainCodec retain_codecs[] = {
    RETAINCODEC( "tblrt1", tblrt1, Node ),
};

static const RetainCodec * find_retain_codec( const std::string & unit, const std::string & root )
{
    for ( size_t i = 0; i < sizeof( retain_codecs ) / sizeof( retain_codecs[0] ); i++ )
    {
        if ( unit == retain_codecs[i].unit && root == retain_codecs[i].root )
        {
            return &retain_codecs[i];
        }
    }
    return NULL;
}

static const MsgCodec * find_msg_codec( const std::string & unit, const std::string & root )
{
    for ( size_t i = 0; i < sizeof( msg_codecs ) / sizeof( msg_codecs[0] ); i++ )
    {
        if ( unit == msg_codecs[i].unit && root == msg_codecs[i].root ) { return &msg_codecs[i]; }
    }
    return NULL;
}

static const VarCodec * find_var_codec( const std::string & unit, const std::string & root )
{
    for ( size_t i = 0; i < sizeof( var_codecs ) / sizeof( var_codecs[0] ); i++ )
    {
        if ( unit == var_codecs[i].unit && root == var_codecs[i].root ) { return &var_codecs[i]; }
    }
    return NULL;
}

static const Codec * find_codec( const std::string & unit, const std::string & root )
{
    for ( size_t i = 0; i < sizeof( codecs ) / sizeof( codecs[0] ); i++ )
    {
        if ( unit == codecs[i].unit && root == codecs[i].root ) return &codecs[i];
    }
    return NULL;
}

// ---------------------------------------------------------------------------
// blocks
// ---------------------------------------------------------------------------
//
// A block's base is 64-byte aligned by construction (§19.1), so the bytes are
// copied once into aligned storage — which is what a host engine's boundary
// looks like, and it keeps BlockOpen's alignment check a real one.

struct Aligned
{
    uint8_t * raw;
    uint8_t * base;
    int64_t bytes;

    // `extent` is the length the CALLER claims, which a forgery may set past
    // the bytes it carries: that is the fact row nine and row ten of the
    // battery are about, and a file alone cannot carry it. The allocation is
    // the claim, so a reader that walks past what it was given walks into a
    // sanitizer's redzone rather than into a neighbour.
    //
    // `lead` is the POINTER column: 0 an aligned base, 1..63 that many bytes
    // past one. An unaligned base is a pointer fact and not a file fact, so the
    // bytes are copied AT the base the column states and the base-alignment
    // clause is the one that must refuse them.
    bool create( const std::vector<uint8_t> & data, int64_t extent, int lead = 0 )
    {
        // the allocation IS the claim, and the claim may be shorter than the
        // file: a driver copies what fits and zeroes the rest, which is the one
        // rule both forgery batteries read the extent column by.
        bytes = extent < 0 ? (int64_t) data.size() : extent;
        raw = (uint8_t *) malloc( (size_t) ( bytes > 0 ? bytes : 1 ) + 64 + (size_t) lead );
        if ( raw == NULL ) return false;
        base = (uint8_t *) ( ( (uintptr_t) raw + 63 ) & ~(uintptr_t) 63 ) + lead;
        memset( base, 0, (size_t) bytes );
        const size_t copy = data.size() < (size_t) bytes ? data.size() : (size_t) bytes;
        memcpy( base, data.data(), copy );
        return true;
    }
    void destroy() { free( raw ); raw = NULL; }
};

// the block opened, and on a refusal the REASON BlockOpen names beside its
// false (docs/SPEC-TABLES.md §19.2): the caller's own initial value is what
// a match leaves standing
static bool open_block( const std::string & name, const std::vector<uint8_t> & data, int64_t extent,
                        blockdemo::TableRefuseReason * reason = NULL, int lead = 0 )
{
    Aligned storage = {};
    if ( !storage.create( data, extent, lead ) ) return false;
    bool opened = false;
    if ( name.rfind( "block_render", 0 ) == 0 )
    {
        blockdemo::RenderFrameBlock block;
        opened = blockdemo::RenderFrameBlockOpen( block, storage.base, storage.bytes, reason );
    }
    else if ( name.rfind( "block_padded", 0 ) == 0 )
    {
        blockdemo::PaddedFrameBlock block;
        opened = blockdemo::PaddedFrameBlockOpen( block, storage.base, storage.bytes, reason );
    }
    else
    {
        fprintf( stderr, "driver: no block named %s\n", name.c_str() );
        storage.destroy();
        exit( 1 );
    }
    storage.destroy();
    return opened;
}

// ---------------------------------------------------------------------------
// the BLOCK ROW DUMP (testdata/conformance/tables/FORMAT.md)
// ---------------------------------------------------------------------------
//
// The `block` surface says only that an image OPENS, which a port passes with
// a reader that checks the prologue and stops. This one reads every row VALUE
// FOR VALUE and writes the walk out as text, so two implementations' reads are
// byte-compared. It is written against §8's descriptors and NOTHING ELSE — no
// generated struct, no field named in this file — because that is the claim
// §19.2 makes for them, and a walk that reached for a struct would be proving
// something else.
//
// The conventions are the cook's node dump's (§7.5), with one addition the
// cook's corpus never needed: a FLOAT is written as its IEEE-754 BIT PATTERN.
// A block row is a byte-identical projection, so its bits are the fact and a
// decimal spelling would be a rounding rule two languages have to agree on for
// no gain — the cook's dump refuses a float rather than inventing one, and
// this is the same refusal to invent, answered by not spelling it as a number.

typedef blockdemo::TableBlockInfo BlockInfo;
typedef blockdemo::TableBlockFieldInfo BlockField;

static void dump_scalar( std::string & out, const uint8_t * at, uint8_t kind, uint32_t width )
{
    char text[64];
    switch ( kind )
    {
        case 1: // bool
            snprintf( text, sizeof( text ), "%s", *at != 0 ? "true" : "false" );
            break;
        case 10: // float32: the bit pattern, in this build's own order
        {
            uint32_t bits = 0;
            memcpy( &bits, at, 4 );
            snprintf( text, sizeof( text ), "0x%08x", bits );
            break;
        }
        case 11: // float64
        {
            uint64_t bits = 0;
            memcpy( &bits, at, 8 );
            snprintf( text, sizeof( text ), "0x%016llx", (unsigned long long) bits );
            break;
        }
        case 2: case 3: case 4: case 5: // the SIGNED integers
        {
            int64_t v = 0;
            if ( width == 1 ) { int8_t t; memcpy( &t, at, 1 ); v = t; }
            else if ( width == 2 ) { int16_t t; memcpy( &t, at, 2 ); v = t; }
            else if ( width == 4 ) { int32_t t; memcpy( &t, at, 4 ); v = t; }
            else { memcpy( &v, at, 8 ); }
            snprintf( text, sizeof( text ), "%lld", (long long) v );
            break;
        }
        default: // an enum's ordinal, a flags mask, and every unsigned integer
        {
            uint64_t v = 0;
            if ( width == 1 ) { v = *at; }
            else if ( width == 2 ) { uint16_t t; memcpy( &t, at, 2 ); v = t; }
            else if ( width == 4 ) { uint32_t t; memcpy( &t, at, 4 ); v = t; }
            else { memcpy( &v, at, 8 ); }
            snprintf( text, sizeof( text ), "%llu", (unsigned long long) v );
            break;
        }
    }
    out += text;
}

// a string's or a `bytes`' USED bytes, quoted, with everything outside
// printable ASCII escaped — the cook dump's spelling, exactly
static void dump_text( std::string & out, const uint8_t * at, int32_t used )
{
    if ( used < 0 ) used = 0;
    out += '"';
    for ( int32_t i = 0; i < used; i++ )
    {
        const uint8_t c = at[i];
        if ( c >= 0x20 && c < 0x7f && c != '"' && c != '\\' )
        {
            out += (char) c;
        }
        else
        {
            char escape[8];
            snprintf( escape, sizeof( escape ), "\\x%02x", (unsigned) c );
            out += escape;
        }
    }
    out += '"';
    char tail[32];
    snprintf( tail, sizeof( tail ), " len=%d", (int) used );
    out += tail;
}

static std::string dump_join( const std::string & prefix, const char * name )
{
    return prefix.empty() ? std::string( name ) : prefix + "." + name;
}

static std::string dump_slot( const std::string & path, int64_t slot )
{
    char index[32];
    snprintf( index, sizeof( index ), "[%lld]", (long long) slot );
    return path + index;
}

// One record's leaves, at two spaces, in descriptor order. `storage` is the
// record's own base. Out-of-line arrays are the caller's business: they are a
// section of their own, not a leaf.
static bool dump_record( std::string & out, const uint8_t * storage, const BlockInfo * info,
                         const std::string & path )
{
    if ( info == NULL ) { fprintf( stderr, "driver: a descriptor names no record\n" ); return false; }
    for ( int i = 0; i < info->num_fields; i++ )
    {
        const BlockField & f = info->fields[i];
        if ( f.out_of_line ) continue;
        const std::string name = dump_join( path, f.name );

        if ( f.counted )
        {
            // a string or a `bytes`: the used length lives at count_offset
            int32_t used = 0;
            memcpy( &used, storage + f.count_offset, sizeof( used ) );
            if ( used < 0 || used > f.array_bound )
            {
                fprintf( stderr, "driver: %s.%s carries a used length of %d, outside [ 0, %d ]\n",
                         info->name, f.name, used, f.array_bound );
                return false;
            }
            out += "  " + name + " = ";
            dump_text( out, storage + f.offset, used );
            out += "\n";
        }
        else
        {
            const int64_t slots = f.is_array ? (int64_t) f.array_bound : 1;
            for ( int64_t s = 0; s < slots; s++ )
            {
                const std::string at = f.is_array ? dump_slot( name, s ) : name;
                const uint8_t * value = storage + f.offset + s * (int64_t) f.elem_size;
                if ( f.element != NULL )
                {
                    if ( !dump_record( out, value, f.element(), at ) ) return false;
                }
                else
                {
                    out += "  " + at + " = ";
                    dump_scalar( out, value, f.kind, f.elem_size );
                    out += "\n";
                }
            }
        }

        if ( f.optional )
        {
            out += "  " + name + "#present = ";
            out += storage[f.present_offset] != 0 ? "true" : "false";
            out += "\n";
        }
    }
    return true;
}

// the whole dump of one opened block: the projection's own fields, then every
// out-of-line array in declaration order, row by row
static bool dump_block( std::string & out, const uint8_t * base, const BlockInfo * info )
{
    char header[256];
    snprintf( header, sizeof( header ), "projection %s @0\n", info->name );
    out += header;
    if ( !dump_record( out, base, info, "" ) ) return false;

    for ( int i = 0; i < info->num_fields; i++ )
    {
        const BlockField & f = info->fields[i];
        if ( !f.out_of_line ) continue;
        uint64_t offset_of = 0;
        uint32_t count = 0, stride = 0;
        memcpy( &offset_of, base + f.offset_of_offset, 8 );
        memcpy( &count, base + f.count_offset, 4 );
        memcpy( &stride, base + f.stride_offset, 4 );
        const BlockInfo * row = f.element();
        if ( row == NULL ) { fprintf( stderr, "driver: %s names no element\n", f.name ); return false; }
        snprintf( header, sizeof( header ), "array %s %s @%llu count=%u stride=%u\n",
                  f.name, row->name, (unsigned long long) offset_of, count, stride );
        out += header;
        for ( uint32_t r = 0; r < count; r++ )
        {
            const uint64_t at = offset_of + (uint64_t) r * stride;
            snprintf( header, sizeof( header ), "row %u @%llu\n", r, (unsigned long long) at );
            out += header;
            if ( !dump_record( out, base + at, row, "" ) ) return false;
        }
    }
    return true;
}

static bool block_dump( const std::string & name, const std::vector<uint8_t> & data, std::string & out )
{
    Aligned storage = {};
    if ( !storage.create( data, -1 ) ) return false;
    bool ok = false;
    if ( name.rfind( "block_render", 0 ) == 0 )
    {
        blockdemo::RenderFrameBlock block;
        ok = blockdemo::RenderFrameBlockOpen( block, storage.base, storage.bytes ) &&
             dump_block( out, block.base, blockdemo::RenderFrameBlock::Type() );
    }
    else if ( name.rfind( "block_padded", 0 ) == 0 )
    {
        blockdemo::PaddedFrameBlock block;
        ok = blockdemo::PaddedFrameBlockOpen( block, storage.base, storage.bytes ) &&
             dump_block( out, block.base, blockdemo::PaddedFrameBlock::Type() );
    }
    else
    {
        fprintf( stderr, "driver: no block named %s\n", name.c_str() );
    }
    storage.destroy();
    return ok;
}

// ---------------------------------------------------------------------------
// the surfaces
// ---------------------------------------------------------------------------

static std::vector<uint8_t> scratch;

// a wire the strict read refused has no bytes to save: the answer is
// `<case>.absent` naming the refusal, which a `wire-refused` row expects
static bool spill_refusal( const std::string & out, const std::string & name, const Report & report )
{
    const char * why = report.blob_shared ? "blob_shared" : "refused";
    return spill( out, name + ".absent", why, strlen( why ) );
}

// THE VERDICT LINE of a `report`, `framing` and `json-hostile` answer: the
// legacy seven fields the harness still reads, with the retired counters at
// zero and the refusal flag the only thing that selects the verdict
// (test/conformance/README.md, "Every read has one outcome").
static std::vector<char> verdict_line( const Report & report, bool read )
{
    char text[128];
    const int n = snprintf( text, sizeof( text ), "%d,0,%d,0,0,false,%s\n",
                            report.unknown, report.widened, read ? "read" : "refused" );
    return std::vector<char>( text, text + n );
}

static int surface_wire( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "instance" ) continue;
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            std::vector<uint8_t> region;
            Report report;
            const void * root = variable->load( region, wire.data(), (int64_t) wire.size(), &report );
            if ( root == NULL )
            {
                // A `wire-refused` instance (a blob two slots name, §2.5): the
                // strict read refuses the whole load, so the row's answer is
                // that refusal and no bytes, as the Go driver's is. A refusal
                // of any other instance is the same answer, and the harness
                // holds it against the row's flag.
                if ( !spill_refusal( out, f[1], report ) ) return 1;
                continue;
            }
            int64_t size = variable->measure( root );
            if ( size < 0 ) return 1;
            scratch.assign( (size_t) size, 0 );
            if ( variable->save( root, scratch.data(), size ) != size ) return 1;
            if ( !spill( out, f[1], scratch.data(), (size_t) size ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) )
        {
            if ( !spill_refusal( out, f[1], report ) ) return 1;
            continue;
        }
        int64_t size = codec->measure( value );
        if ( size < 0 ) return 1;
        scratch.assign( (size_t) size, 0 );
        if ( codec->save( value, scratch.data(), size ) != size ) return 1;
        if ( !spill( out, f[1], scratch.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

// THE MESSAGE SURFACE (docs/SPEC-TABLES.md §3.3): read the message against the
// connection's announced table, and write it back. The expectation is the
// message golden itself, exactly as the wire surface's is the wire golden.
static int surface_message( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "message" ) continue;
        // message <name> <connection> <root> <file-form wire> <message-form wire>
        std::string unit, announcement_path;
        for ( size_t j = 0; j < manifest_lines.size(); j++ )
        {
            const std::vector<std::string> & c = manifest_lines[j].field;
            if ( c[0] == "connection" && c[1] == f[2] ) { unit = c[2]; announcement_path = c[4]; break; }
        }
        if ( unit.empty() ) { fprintf( stderr, "driver: no connection %s\n", f[2].c_str() ); return 1; }
        const MsgCodec * codec = find_msg_codec( unit, f[3] );
        if ( f[5] == "-" )
        {
            // a variable table: the message form is fixed tables only (§3.3),
            // so no codec exists, and the row is answered by name
            if ( codec != NULL ) { fprintf( stderr, "driver: %s.%s is a refusal row and carries a message codec\n", unit.c_str(), f[3].c_str() ); return 1; }
            static const char reason[] = "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)";
            if ( !spill( out, f[1] + ".absent", (const uint8_t *) reason, sizeof( reason ) - 1 ) ) return 1;
            continue;
        }
        if ( codec == NULL ) { fprintf( stderr, "driver: no message codec for %s.%s\n", unit.c_str(), f[3].c_str() ); return 1; }
        std::vector<uint8_t> announcement, message;
        if ( !slurp( announcement_path.c_str(), announcement ) ) { fprintf( stderr, "driver: cannot read %s\n", announcement_path.c_str() ); return 1; }
        if ( !slurp( f[5].c_str(), message ) ) { fprintf( stderr, "driver: cannot read %s\n", f[5].c_str() ); return 1; }
        std::vector<uint8_t> answer;
        Report report;
        if ( !codec->round_trip( announcement.data(), (int64_t) announcement.size(),
                                 message.data(), (int64_t) message.size(), answer, &report ) ) { return 1; }
        if ( !spill( out, f[1], answer.data(), answer.size() ) ) return 1;
    }
    return 0;
}

// THE RETAIN SURFACES (docs/SPEC-TABLES.md §6.6): the COUNTERS on one and the
// BYTES on the other, so each is one shape. A row's `retain` line carries the
// wire and the two capacities; nothing about the answer reaches the driver.
static bool retain_row( const std::vector<std::string> & f, RetainAnswer & answer )
{
    // retain <case> <unit> <root> <wire> <capacity> <ids>
    const std::string unit = f[2], root = f[3], wire_path = f[4];
    const bool short_buffer = f[5] == "short";
    int64_t ids = -1;
    if ( f[6] != "full" ) { ids = (int64_t) strtoll( f[6].c_str(), NULL, 10 ); }

    std::vector<uint8_t> wire;
    if ( !slurp( wire_path.c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", wire_path.c_str() ); return false; }
    const RetainCodec * codec = find_retain_codec( unit, root );
    if ( codec == NULL ) { fprintf( stderr, "driver: no retain codec for %s.%s\n", unit.c_str(), root.c_str() ); return false; }
    return codec->run( wire.data(), (int64_t) wire.size(), short_buffer, ids, answer );
}

static int surface_retain( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "retain" ) continue;
        RetainAnswer answer;
        if ( !retain_row( f, answer ) ) return 1;
        // the whole KNOWN value or the refusal (test/conformance/README.md):
        // the retention counters are this port's diagnostics, never compared
        std::vector<char> text;
        static const char read[] = "read\n", refused[] = "refused\n";
        if ( answer.read ) text.insert( text.end(), read, read + sizeof( read ) - 1 );
        else text.insert( text.end(), refused, refused + sizeof( refused ) - 1 );
        text.insert( text.end(), answer.text.begin(), answer.text.end() );
        if ( !spill( out, f[1], text.data(), text.size() ) ) return 1;
    }
    return 0;
}

static int surface_retain_save( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "retain" ) continue;
        RetainAnswer answer;
        if ( !retain_row( f, answer ) ) return 1;
        if ( !spill( out, f[1], answer.saved.data(), answer.saved.size() ) ) return 1;
    }
    return 0;
}

// The §16 text of a by-value codec's value: the value half of a `report` and
// `json-hostile` answer (test/conformance/README.md).
static bool codec_text( const Codec * codec, void * value, std::vector<char> & text )
{
    int64_t size = codec->to_json_measure( value );
    if ( size < 0 ) return false;
    text.resize( (size_t) size + 1 );
    if ( codec->to_json( value, text.data(), size ) != size ) return false;
    text.resize( (size_t) size );
    return true;
}

// ---------------------------------------------------------------------------
// THE STRICT FORM-1 WALK (docs/LAYERS/1-framing.md; docs/SPEC-TABLES.md §6.5)
// ---------------------------------------------------------------------------
//
// The conditions decidable from the wire alone, applied to every byte of a
// form-1 file before any value is decoded: in fields and records this reader
// declares and in those it does not. Skipped is not unread: an unknown field
// is walked by the same rules as a known one, so damage inside it refuses the
// load. This is the C++ port of the engine's walk (internal/tablewire
// wirecheck.go, held beside it by test/conformance/harness/strictcheck.go):
// the framing (lengths, counts, terminators, headers inside their own L, the
// id table's references, canonical LEB128, a kind no reader can skip), a
// repeated field id in one body, ill-formed text, a non-finite float, the
// node table's framing, a reserved id out of place, and then the pointer
// graph: an index past the node table, a cycle, a record no pointer reaches
// and one blob record named from two slots. The generated readers decode
// without this walk, so the framing battery runs it here, in the driver,
// before the read it answers for: damage discards the whole load (D1), and a
// refused row owes the refusal alone.

struct FramingWalk
{
    const uint64_t * ids;
    size_t id_count;
    std::vector<uint64_t> edges; // the kind 17 payloads of the body being walked
    const uint8_t * table;       // the node table's payload, when the root carries it
    size_t table_bytes;
    bool table_present;
    bool non_finite;
    int depth;
};

static uint64_t framing_u64le( const uint8_t * p )
{
    uint64_t v = 0;
    for ( int i = 0; i < 8; i++ ) v |= (uint64_t) p[i] << ( 8 * i );
    return v;
}

static uint64_t framing_name_id( const char * name )
{
    uint64_t h = 0xCBF29CE484222325ull;
    for ( const char * p = name; *p != '\0'; p++ )
    {
        h ^= (uint64_t) (uint8_t) *p;
        h *= 0x00000100000001B3ull;
    }
    return h;
}

// a canonical LEB128: minimal spelling, at most ten bytes, the tenth byte
// carrying no bit above the 64th value bit (F10)
static bool framing_leb( const uint8_t * buf, size_t len, size_t off, uint64_t * value, size_t * next )
{
    uint64_t v = 0;
    unsigned shift = 0;
    for ( int i = 0; ; i++ )
    {
        if ( off >= len || i >= 10 ) return false;
        const uint8_t b = buf[off++];
        if ( i == 9 && b > 1 ) return false;
        v |= (uint64_t) ( b & 0x7F ) << shift;
        if ( ( b & 0x80 ) == 0 )
        {
            if ( i > 0 && b == 0 ) return false; // a non-minimal spelling is damage
            *value = v;
            *next = off;
            return true;
        }
        shift += 7;
    }
}

static bool framing_fixed_kind( int kind )
{
    switch ( kind )
    {
        case 1: case 2: case 6: case 3: case 7: case 4: case 8: case 10:
        case 5: case 9: case 11: case 18: case 19:
        case 20: case 21: case 22: case 23: case 24:
        case 25: case 26: case 27: case 28: case 29: case 36:
            return true;
        default:
            return false;
    }
}

static int framing_kind_width( int kind )
{
    switch ( kind )
    {
        case 1: case 2: case 6: case 20: case 25: return 1;
        case 3: case 7: case 21: case 26: return 2;
        case 4: case 8: case 10: case 22: case 27: return 4;
        case 5: case 9: case 11: case 23: case 28: case 36: return 8;
        case 18: case 19: case 24: case 29: return 16;
        default: return 0;
    }
}

static bool framing_scalar( FramingWalk * w, const uint8_t * b, int kind )
{
    if ( kind == 1 ) return b[0] <= 1; // a bool byte other than 0 or 1 is damage
    if ( kind == 10 )
    {
        uint32_t bits = 0;
        for ( int i = 0; i < 4; i++ ) bits |= (uint32_t) b[i] << ( 8 * i );
        if ( ( ( bits >> 23 ) & 0xFF ) == 0xFF ) w->non_finite = true;
    }
    if ( kind == 11 )
    {
        const uint64_t bits = framing_u64le( b );
        if ( ( ( bits >> 52 ) & 0x7FF ) == 0x7FF ) w->non_finite = true;
    }
    return true;
}

static bool framing_text_valid( const uint8_t * b, size_t len )
{
    size_t i = 0;
    while ( i < len )
    {
        const uint8_t c = b[i];
        if ( c == 0 ) return false;
        if ( c < 0x80 ) { i++; continue; }
        int extra;
        uint32_t cp;
        if ( ( c & 0xE0 ) == 0xC0 ) { extra = 1; cp = c & 0x1F; if ( cp < 2 ) return false; }
        else if ( ( c & 0xF0 ) == 0xE0 ) { extra = 2; cp = c & 0x0F; }
        else if ( ( c & 0xF8 ) == 0xF0 ) { extra = 3; cp = c & 0x07; }
        else return false;
        if ( i + (size_t) extra >= len ) return false;
        for ( int k = 1; k <= extra; k++ )
        {
            if ( ( b[i + (size_t) k] & 0xC0 ) != 0x80 ) return false;
            cp = ( cp << 6 ) | (uint32_t) ( b[i + (size_t) k] & 0x3F );
        }
        if ( extra == 2 && cp < 0x800 ) return false;
        if ( extra == 3 && cp < 0x10000 ) return false;
        if ( cp >= 0xD800 && cp <= 0xDFFF ) return false; // a surrogate is not a scalar
        if ( cp > 0x10FFFF ) return false;
        i += (size_t) extra + 1;
    }
    return true;
}

static bool framing_payload( FramingWalk * w, const uint8_t * buf, size_t len, size_t off, int kind, size_t * next );
static bool framing_content( FramingWalk * w, const uint8_t * b, size_t len, int kind );

static bool framing_array_header( const uint8_t * b, size_t len, int * elem_kind, uint64_t * count, size_t * off )
{
    // THE HEADER LIES INSIDE ITS OWN L (H2, H3): a body too short to carry
    // the element kind byte and the count is damage, inert at any value
    if ( len < 2 ) return false;
    uint64_t n = 0;
    size_t after = 0;
    if ( !framing_leb( b, len, 1, &n, &after ) ) return false;
    if ( n > 0x7FFFFFFFull ) return false; // a count past the storage cap
    *elem_kind = (int) b[0];
    *count = n;
    *off = after;
    return true;
}

static bool framing_array( FramingWalk * w, const uint8_t * b, size_t len )
{
    int elem_kind = 0;
    uint64_t count = 0;
    size_t off = 0;
    if ( !framing_array_header( b, len, &elem_kind, &count, &off ) ) return false;
    for ( uint64_t i = 0; i < count; i++ )
    {
        if ( !framing_payload( w, b, len, off, elem_kind, &off ) ) return false;
    }
    return off == len; // bytes left inside an array's L after its elements (L6)
}

static bool framing_keyed( FramingWalk * w, const uint8_t * b, size_t len )
{
    int elem_kind = 0;
    uint64_t count = 0;
    size_t off = 0;
    if ( !framing_array_header( b, len, &elem_kind, &count, &off ) ) return false;
    std::vector<uint64_t> seen;
    for ( uint64_t i = 0; i < count; i++ )
    {
        uint64_t key = 0;
        size_t after_key = 0;
        if ( !framing_leb( b, len, off, &key, &after_key ) ) return false;
        if ( key == 0 || key > (uint64_t) w->id_count ) return false; // H7
        for ( size_t k = 0; k < seen.size(); k++ ) if ( seen[k] == key ) return false;
        seen.push_back( key );
        uint64_t n = 0;
        size_t after_n = 0;
        if ( !framing_leb( b, len, after_key, &n, &after_n ) ) return false;
        if ( n > (uint64_t) ( len - after_n ) ) return false;
        off = after_n + (size_t) n;
        if ( !framing_content( w, b + after_n, (size_t) n, elem_kind ) ) return false;
    }
    return off == len;
}

static bool framing_union( FramingWalk * w, const uint8_t * buf, size_t len, size_t off, size_t * next )
{
    uint64_t ref = 0;
    size_t after_ref = 0;
    if ( !framing_leb( buf, len, off, &ref, &after_ref ) ) return false;
    if ( ref == 0 ) { *next = after_ref; return true; } // the empty arm carries nothing
    if ( ref > (uint64_t) w->id_count || after_ref >= len ) return false;
    const int kind = (int) buf[after_ref];
    uint64_t n = 0;
    size_t after_n = 0;
    if ( !framing_leb( buf, len, after_ref + 1, &n, &after_n ) ) return false;
    if ( n > (uint64_t) ( len - after_n ) ) return false;
    const size_t end = after_n + (size_t) n;
    const bool structural = kind == 13 || kind == 14 || kind == 16 || kind == 15;
    if ( structural )
    {
        if ( w->depth >= 64 ) return false;
        w->depth++;
    }
    const bool ok = framing_content( w, buf + after_n, (size_t) n, kind );
    if ( structural ) w->depth--;
    *next = end;
    return ok;
}

static bool framing_content( FramingWalk * w, const uint8_t * b, size_t len, int kind )
{
    if ( framing_fixed_kind( kind ) )
    {
        if ( len != (size_t) framing_kind_width( kind ) ) return false; // an L that is not its width (H9)
        return framing_scalar( w, b, kind );
    }
    switch ( kind )
    {
        case 12: return framing_text_valid( b, len );
        case 31: return true; // the escape: opaque by rule (§3)
        case 32: return len == 0; // kind 32 with a nonzero L is damage
        case 13: case 14: case 16:
        {
            if ( w->depth >= 64 ) return false;
            w->depth++;
            bool ok;
            if ( kind == 13 ) ok = framing_body( w, b, len, false );
            else if ( kind == 14 ) ok = framing_array( w, b, len );
            else ok = framing_keyed( w, b, len );
            w->depth--;
            return ok;
        }
        case 17: case 30: case 15:
        {
            size_t end = 0;
            return framing_payload( w, b, len, 0, kind, &end ) && end == len;
        }
        default:
            return false; // a kind no reader can skip (R3)
    }
}

static bool framing_payload( FramingWalk * w, const uint8_t * buf, size_t len, size_t off, int kind, size_t * next )
{
    if ( framing_fixed_kind( kind ) )
    {
        const int width = framing_kind_width( kind );
        if ( (size_t) width > len - off ) return false;
        *next = off + (size_t) width;
        return framing_scalar( w, buf + off, kind );
    }
    switch ( kind )
    {
        case 17: // a pointer: the index is bounds-checked, never followed (T7)
        {
            uint64_t index = 0;
            if ( !framing_leb( buf, len, off, &index, next ) ) return false;
            w->edges.push_back( index );
            return true;
        }
        case 30: // an enum: a variant reference into the id table
        {
            uint64_t ref = 0;
            if ( !framing_leb( buf, len, off, &ref, next ) ) return false;
            return ref <= (uint64_t) w->id_count;
        }
        case 15:
            return framing_union( w, buf, len, off, next );
        case 12: case 31: case 32: case 13: case 14: case 16:
        {
            uint64_t n = 0;
            size_t after_n = 0;
            if ( !framing_leb( buf, len, off, &n, &after_n ) ) return false;
            if ( n > (uint64_t) ( len - after_n ) ) return false; // L4
            *next = after_n + (size_t) n;
            return framing_content( w, buf + after_n, (size_t) n, kind );
        }
        default:
            return false; // a kind no reader can skip (R3)
    }
}

static bool framing_body( FramingWalk * w, const uint8_t * buf, size_t len, bool root )
{
    size_t off = 0;
    std::vector<uint64_t> seen;
    for ( ;; )
    {
        uint64_t ref = 0;
        size_t after_ref = 0;
        if ( !framing_leb( buf, len, off, &ref, &after_ref ) ) return false;
        off = after_ref;
        if ( ref == 0 ) return off == len; // bytes after the terminator inside its L (L3)
        if ( ref > (uint64_t) w->id_count ) return false; // F9
        const uint64_t id = w->ids[ref - 1];
        for ( size_t k = 0; k < seen.size(); k++ ) if ( seen[k] == id ) return false; // R5
        seen.push_back( id );
        if ( off >= len ) return false;
        const int kind = (int) buf[off++];
        if ( id == 0xFFFFFFFFFFFFFFFEull || id == 0xFFFFFFFFFFFFFFFDull ) return false; // R6
        if ( id == 0xFFFFFFFFFFFFFFFFull )
        {
            // the node table rides once, at the root, as kind 12 (T1, T5)
            if ( !root || kind != 12 ) return false;
            uint64_t n = 0;
            size_t after_n = 0;
            if ( !framing_leb( buf, len, off, &n, &after_n ) ) return false;
            if ( n > (uint64_t) ( len - after_n ) ) return false;
            w->table = buf + after_n;
            w->table_bytes = (size_t) n;
            w->table_present = true;
            off = after_n + (size_t) n;
            continue;
        }
        if ( !framing_payload( w, buf, len, off, kind, &off ) ) return false;
    }
}

struct FramingRecord
{
    uint64_t type_id;
    const uint8_t * body;
    size_t body_bytes;
};

static bool framing_scan_records( FramingWalk * w, const uint8_t * payload, size_t len, std::vector<FramingRecord> * records )
{
    // the record scan is authoritative, and a node_count that disagrees with
    // it is malformed (T3); nothing is sized from node_count before it (T2)
    uint64_t declared = 0;
    size_t off = 0;
    if ( !framing_leb( payload, len, 0, &declared, &off ) ) return false;
    while ( off < len )
    {
        uint64_t ref = 0;
        size_t after_ref = 0;
        if ( !framing_leb( payload, len, off, &ref, &after_ref ) ) return false;
        if ( ref == 0 || ref > (uint64_t) w->id_count ) return false; // T4
        uint64_t n = 0;
        size_t after_n = 0;
        if ( !framing_leb( payload, len, after_ref, &n, &after_n ) ) return false;
        if ( n > (uint64_t) ( len - after_n ) ) return false;
        FramingRecord record;
        record.type_id = w->ids[ref - 1];
        record.body = payload + after_n;
        record.body_bytes = (size_t) n;
        records->push_back( record );
        off = after_n + (size_t) n;
    }
    return off == len && declared == (uint64_t) records->size();
}

static bool framing_graph( const std::vector<std::vector<uint64_t> > & children, const std::vector<bool> & blobs )
{
    // node 0 is the root, node k is record k (wire index k + 1): an index past
    // the table, a cycle, a record no pointer reaches, one blob named twice
    const size_t n = children.size();
    std::vector<uint64_t> names( n, 0 );
    for ( size_t c = 0; c < n; c++ )
    {
        for ( size_t e = 0; e < children[c].size(); e++ )
        {
            const uint64_t index = children[c][e];
            if ( index == 0 ) continue;
            if ( index - 1 >= (uint64_t) n ) return false;
            names[(size_t) ( index - 1 )]++;
        }
    }
    for ( size_t k = 0; k < blobs.size(); k++ )
    {
        if ( blobs[k] && names[k + 1] > 1 ) return false; // one blob, two slots (§2.5)
    }
    std::vector<uint8_t> mark( n, 0 ); // 0 unvisited, 1 open, 2 closed (T8, linear)
    std::vector<size_t> stack_node;
    std::vector<size_t> stack_next;
    stack_node.push_back( 0 );
    stack_next.push_back( 0 );
    mark[0] = 1;
    while ( !stack_node.empty() )
    {
        const size_t node = stack_node.back();
        size_t & next = stack_next.back();
        const std::vector<uint64_t> & out = children[node];
        if ( next == out.size() )
        {
            mark[node] = 2;
            stack_node.pop_back();
            stack_next.pop_back();
            continue;
        }
        const uint64_t index = out[next++];
        if ( index == 0 ) continue;
        const size_t target = (size_t) ( index - 1 );
        if ( mark[target] == 1 ) return false; // a cycle, over every slot (T8)
        if ( mark[target] == 2 ) continue;     // a second pointer to a closed node is a share
        mark[target] = 1;
        stack_node.push_back( target );
        stack_next.push_back( 0 );
    }
    for ( size_t k = 1; k < n; k++ ) if ( mark[k] == 0 ) return false; // unreachable is damage
    return true;
}

// framing_strict_ok answers whether a form-1 file's whole wire is well-formed
// under the rules above. It takes no declaration: every leg is held to it, and
// a false answer is the strict read's refusal, with nothing else defined (D1).
static bool framing_strict_ok( const uint8_t * data, size_t len )
{
    if ( len < 9 || data[0] != 1 ) return false; // F1, F5: no form byte, or a short file
    const uint64_t count = framing_u64le( data + len - 8 );
    if ( count > (uint64_t) ( len / 8 ) || count > 4096 ) return false; // F5, F6
    const size_t span = (size_t) count * 8 + 8;
    if ( span + 1 > len ) return false;
    const size_t first = len - span; // the body is data[1:first] (F2, F4)
    std::vector<uint64_t> ids( (size_t) count );
    for ( size_t i = 0; i < (size_t) count; i++ )
    {
        ids[i] = framing_u64le( data + first + i * 8 );
        for ( size_t k = 0; k < i; k++ ) if ( ids[k] == ids[i] ) return false; // one id twice (F5)
    }
    FramingWalk walk;
    walk.ids = ids.data();
    walk.id_count = ids.size();
    walk.table = NULL;
    walk.table_bytes = 0;
    walk.table_present = false;
    walk.non_finite = false;
    walk.depth = 0;
    if ( !framing_body( &walk, data + 1, first - 1, true ) ) return false;
    std::vector<std::vector<uint64_t> > children;
    children.push_back( walk.edges );
    std::vector<bool> blobs;
    if ( walk.table_present )
    {
        std::vector<FramingRecord> records;
        if ( !framing_scan_records( &walk, walk.table, walk.table_bytes, &records ) ) return false;
        const uint64_t bytes_id = framing_name_id( "bytes" );
        const uint64_t string_id = framing_name_id( "string" );
        for ( size_t i = 0; i < records.size(); i++ )
        {
            walk.edges.clear();
            if ( records[i].type_id == bytes_id || records[i].type_id == string_id )
            {
                // a blob record is its bytes (§2.5, §3.1): no body to walk
                if ( records[i].type_id == string_id && !framing_text_valid( records[i].body, records[i].body_bytes ) ) return false;
                blobs.push_back( true );
            }
            else
            {
                if ( !framing_body( &walk, records[i].body, records[i].body_bytes, false ) ) return false;
                blobs.push_back( false );
            }
            children.push_back( walk.edges );
        }
    }
    return framing_graph( children, blobs ) && !walk.non_finite;
}

// THE ONE READ AND ITS WRITE-BACK (test/conformance/README.md): an accepted row
// owes the verdict line, the §16 text of the value, and the canonical wire the
// value saves to in `<case>.wire`; a refused row owes `refused` and nothing
// else. `with_value` is false for a framing row that says `no-value`, which
// still owes its wire. The value is ONE value in two forms, so both come from
// the same read. `form1` reads a by-value root through its form-1 read, which
// only the framing battery asks for.
static bool read_answer( const std::string & out, const std::string & name, const std::vector<uint8_t> & wire,
                         const VarCodec * variable, const Codec * codec, bool with_value, bool form1 )
{
    Report report = Report();
    std::vector<char> value_text;
    std::vector<uint8_t> saved;
    bool ok = false;
    // the framing battery's read is a form-1 read, so the strict walk above
    // judges the whole wire first: damage anywhere refuses the load before
    // any value is decoded, and nothing the decode produced is defined (D1)
    const bool framing_ok = !form1 || framing_strict_ok( wire.data(), wire.size() );
    if ( !framing_ok ) report.refused = true;
    if ( variable != NULL )
    {
        std::vector<uint8_t> region;
        const void * root = framing_ok ? variable->load( region, wire.data(), (int64_t) wire.size(), &report ) : NULL;
        ok = root != NULL && !report.refused;
        if ( ok )
        {
            if ( with_value && !variable->to_json( root, value_text ) ) return false;
            const int64_t size = variable->measure( root );
            if ( size < 0 ) return false;
            saved.assign( (size_t) size, 0 );
            if ( variable->save( root, saved.data(), size ) != size ) return false;
        }
    }
    else
    {
        void * value = codec->make();
        codec->reset( value );
        ok = framing_ok && ( form1 ? codec->load_form1 : codec->load )( value, wire.data(), (int64_t) wire.size(), &report ) && !report.refused;
        if ( ok )
        {
            if ( with_value && !codec_text( codec, value, value_text ) ) return false;
            // the write-back is the root's OWN file form whichever form was
            // read: the value is one value, and its canonical bytes are form 3
            const int64_t size = codec->measure( value );
            if ( size < 0 ) return false;
            saved.assign( (size_t) size, 0 );
            if ( codec->save( value, saved.data(), size ) != size ) return false;
        }
    }
    std::vector<char> answer = verdict_line( report, ok );
    if ( ok )
    {
        answer.insert( answer.end(), value_text.begin(), value_text.end() );
        if ( !spill( out, name + ".wire", saved.data(), saved.size() ) ) return false;
    }
    return spill( out, name, answer.data(), answer.size() );
}

static int surface_report( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "report" ) continue;
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        const Codec * codec = variable == NULL ? find_codec( f[2], f[3] ) : NULL;
        if ( variable == NULL && codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
        if ( !read_answer( out, f[1], wire, variable, codec, true, false ) ) return 1;
    }
    return 0;
}

// THE FRAMING VECTORS (docs/LAYERS/1-framing.md): the same answer as the
// `report` surface, the value left out where the row says `no-value`. A
// derived line is `framing <case> <unit> <root> <wire> [no-value]`.
static int surface_framing( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "framing" ) continue;
        const bool no_value = f.size() > 5 && f[5] == "no-value";
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        const Codec * codec = variable == NULL ? find_codec( f[2], f[3] ) : NULL;
        if ( variable == NULL && codec == NULL )
        {
            // a root this leg's table carries no codec for: absent per
            // case, which the matrix counts against the reference leg
            if ( !spill( out, f[1] + ".absent", "", 0 ) ) return 1;
            continue;
        }
        if ( !read_answer( out, f[1], wire, variable, codec, !no_value, true ) ) return 1;
    }
    return 0;
}

// an instance the corpus carries on the wire alone: past the text form's depth
// cap by the form's own rule (§16.7), so no leg is asked for its text
static bool no_text( const std::vector<std::string> & f )
{
    return f.size() > 5 && f[5] == "no-text";
}

static int surface_json_read( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "instance" ) continue;
        if ( no_text( f ) ) continue;
        std::vector<uint8_t> text;
        std::string path = "testdata/conformance/tables/json/" + f[1] + ".json";
        if ( !slurp( path.c_str(), text ) ) { fprintf( stderr, "driver: cannot read %s\n", path.c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            Report report;
            std::vector<uint8_t> bytes;
            if ( !variable->json_read( (const char *) text.data(), (int64_t) text.size(), &report, bytes ) ) return 1;
            if ( !spill( out, f[1], bytes.data(), bytes.size() ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) return 1;

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->from_json( value, (const char *) text.data(), (int64_t) text.size(), &report ) ) return 1;
        int64_t size = codec->measure( value );
        if ( size < 0 ) return 1;
        scratch.assign( (size_t) size, 0 );
        if ( codec->save( value, scratch.data(), size ) != size ) return 1;
        if ( !spill( out, f[1], scratch.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

static int surface_json_write( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "instance" ) continue;
        if ( no_text( f ) ) continue;
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            std::vector<uint8_t> region;
            Report report;
            const void * root = variable->load( region, wire.data(), (int64_t) wire.size(), &report );
            if ( root == NULL ) return 1;
            std::vector<char> text;
            if ( !variable->to_json( root, text ) ) return 1;
            if ( !spill( out, f[1] + ".json", text.data(), text.size() ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) return 1;

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) ) return 1;
        int64_t size = codec->to_json_measure( value );
        if ( size < 0 ) return 1;
        std::vector<char> text( (size_t) size + 1 );
        if ( codec->to_json( value, text.data(), size ) != size ) return 1;
        if ( !spill( out, f[1] + ".json", text.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

// A VALUE THE WRITER REFUSES (docs/SPEC-TABLES.md §16.14, W0): the line names
// a wire that reads, and the damage the driver puts into it afterwards, which no
// read can deliver: the bytes 61 FF 62 in a narrow text field, the units 0061
// D800 0062 in a wide one (testdata/conformance/tables/FORMAT.md,
// `json-write-refusal`), or `none` where the value is already past what a text
// may carry. The answer is `refused`, never a text.
static bool damage_text( const std::string & unit, const std::string & root, void * value, const std::string & damage )
{
    if ( damage == "none" ) return true;
    if ( damage == "narrow:name" && unit == "tabledemo" && root == "ProfileConfig" )
    {
        tabledemo::ProfileConfig & v = *(tabledemo::ProfileConfig *) value;
        v.name[0] = 0x61; v.name[1] = (char) 0xff; v.name[2] = 0x62;
        v.name_length = 3;
        return true;
    }
    fprintf( stderr, "driver: damage %s names no text field of %s.%s\n", damage.c_str(), unit.c_str(), root.c_str() );
    return false;
}

static int surface_json_write_refusal( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "json-write-refusal" ) continue;
        if ( f.size() < 6 ) { fprintf( stderr, "driver: a json-write-refusal line is <case> <unit> <root> <wire> <damage>\n" ); return 1; }
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        Report report = Report();
        std::vector<char> text;
        bool wrote = false;
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            if ( f[5] != "none" ) { fprintf( stderr, "driver: %s: a region root takes no damage\n", f[1].c_str() ); return 1; }
            std::vector<uint8_t> region;
            const void * root = variable->load( region, wire.data(), (int64_t) wire.size(), &report );
            if ( root == NULL ) { fprintf( stderr, "driver: %s: load refused %s\n", f[1].c_str(), f[4].c_str() ); return 1; }
            wrote = variable->to_json( root, text );
        }
        else
        {
            const Codec * codec = find_codec( f[2], f[3] );
            if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
            void * value = codec->make();
            codec->reset( value );
            if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) ) { fprintf( stderr, "driver: %s: load refused %s\n", f[1].c_str(), f[4].c_str() ); return 1; }
            if ( !damage_text( f[2], f[3], value, f[5] ) ) return 1;
            wrote = codec_text( codec, value, text );
        }
        static const char refused[] = "refused\n";
        if ( !wrote ) text.assign( refused, refused + sizeof( refused ) - 1 );
        if ( !spill( out, f[1] + ".json", text.data(), text.size() ) ) return 1;
    }
    return 0;
}

// json-text: the nightly's CROSS-READ of a port's text (test/conformance/
// README.md, "The nightly"; docs/SPEC-TABLES.md §16). The harness hands a
// substituted manifest whose lines name a PORT's JSON text,
//   json-text <case> <unit> <root> <text>
// and the reference reads it, writes it back, and the text goes out as
// <case>.json; the harness holds it to the port's bytes. It is not a matrix
// surface and `list` does not name it: no leg but the reference answers it.
static int surface_json_text( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "json-text" ) continue;
        if ( f.size() < 5 ) { fprintf( stderr, "driver: a json-text line is <case> <unit> <root> <text>\n" ); return 1; }
        std::vector<uint8_t> text;
        if ( !slurp( f[4].c_str(), text ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            Report report;
            std::vector<uint8_t> bytes;
            if ( !variable->json_read( (const char *) text.data(), (int64_t) text.size(), &report, bytes ) ) return 1;
            std::vector<uint8_t> region;
            const void * root = variable->load( region, bytes.data(), (int64_t) bytes.size(), &report );
            if ( root == NULL ) return 1;
            std::vector<char> written;
            if ( !variable->to_json( root, written ) ) return 1;
            if ( !spill( out, f[1] + ".json", written.data(), written.size() ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->from_json( value, (const char *) text.data(), (int64_t) text.size(), &report ) ) return 1;
        int64_t size = codec->to_json_measure( value );
        if ( size < 0 ) return 1;
        std::vector<char> written( (size_t) size + 1 );
        if ( codec->to_json( value, written.data(), size ) != size ) return 1;
        if ( !spill( out, f[1] + ".json", written.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

// cook-write: the runtime WRITES a cooked file and the bytes are the tool's
// (docs/SPEC-TABLES.md §7.6), in BOTH byte orders. The value is CONSTRUCTED
// (test/conformance/README.md, "Cook writes use explicit value construction"):
// the derived line is
//   cook-write <instance> <JSON input or -> <source field or -> <alias field or ->
// and `-` as the input means the `no-text` instance's wire. A source and an
// alias name two blobs that must share ONE in-memory object, which no wire
// may carry (§2.5) and a builder may: the cook is where that sharing is legal.
//
// The two answers are written as <instance> and <instance>-be. The big-endian
// half needs no big-endian host: the order is a parameter of the write, settled
// at cook time for the target build, so every leg answers both on any machine.
static bool cook_both( const std::string & out, const std::string & name, int64_t need,
                       bool ( *cook )( const void *, void *, uint64_t, int ), const void * value )
{
    if ( need <= 0 ) return false;
    std::vector<uint8_t> file( (size_t) need );
    for ( int big = 0; big < 2; big++ )
    {
        if ( !cook( value, file.data(), (uint64_t) need, big ) ) return false;
        if ( !spill( out, big ? name + "-be" : name, file.data(), (size_t) need ) ) return false;
    }
    return true;
}

// THE ONE SHARING RECIPE the corpus carries: blobdemo's Catalog with `alias`
// naming the very blob `thumb` names, set on the builder after the text read
static bool cook_shared( const std::string & out, const std::string & name, const std::vector<std::string> & inst,
                         const std::vector<uint8_t> & text, const std::string & source, const std::string & alias )
{
    if ( inst[2] != "blobdemo" || inst[3] != "Catalog" || source != "thumb" || alias != "alias" )
    {
        fprintf( stderr, "driver: no sharing recipe for %s.%s %s:%s\n", inst[2].c_str(), inst[3].c_str(), source.c_str(), alias.c_str() );
        return false;
    }
    blobdemo::CatalogBuilder builder;
    blobdemo::TableReport report;
    if ( !blobdemo::CatalogFromJson( builder, (const char *) text.data(), (int64_t) text.size(), &report ) ) return false;
    blobdemo::Catalog * root = builder.GetRoot();
    if ( root == NULL || root->thumb.value == 0 ) return false;
    root->alias = root->thumb;
    const int64_t need = blobdemo::CatalogCookMeasure( builder );
    if ( need <= 0 ) return false;
    std::vector<uint8_t> file( (size_t) need );
    for ( int big = 0; big < 2; big++ )
    {
        if ( !blobdemo::CatalogCook( builder, file.data(), (uint64_t) need,
                                     big ? blobdemo::TableByteOrder::Big : blobdemo::TableByteOrder::Little ) ) return false;
        if ( !spill( out, big ? name + "-be" : name, file.data(), (size_t) need ) ) return false;
    }
    return true;
}

static int surface_cook_write( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "cook-write" ) continue;
        if ( f.size() != 5 ) { fprintf( stderr, "driver: a cook-write line has five fields\n" ); return 1; }
        // the line names an INSTANCE; that line carries the unit and the root
        const std::vector<std::string> * inst = NULL;
        for ( size_t j = 0; j < manifest_lines.size(); j++ )
        {
            const std::vector<std::string> & g = manifest_lines[j].field;
            if ( g[0] == "instance" && g[1] == f[1] ) { inst = &manifest_lines[j].field; break; }
        }
        if ( inst == NULL ) { fprintf( stderr, "driver: no instance %s\n", f[1].c_str() ); return 1; }
        const bool from_text = f[2] != "-";
        std::vector<uint8_t> input;
        const std::string & input_path = from_text ? f[2] : ( *inst )[4];
        if ( !slurp( input_path.c_str(), input ) ) { fprintf( stderr, "driver: cannot read %s\n", input_path.c_str() ); return 1; }
        if ( f[3] != "-" || f[4] != "-" )
        {
            if ( !from_text || f[3] == "-" || f[4] == "-" ) { fprintf( stderr, "driver: %s: a sharing recipe names an input and both fields\n", f[1].c_str() ); return 1; }
            if ( !cook_shared( out, f[1], *inst, input, f[3], f[4] ) ) { fprintf( stderr, "driver: %s: the shared cook failed\n", f[1].c_str() ); return 1; }
            continue;
        }

        // a FIXED root is a value; a POINTERED root is a region and a root
        // pointer (§6.2), loaded exactly as the wire surface loads it
        const Codec * codec = find_codec( ( *inst )[2], ( *inst )[3] );
        const VarCodec * var = codec == NULL ? find_var_codec( ( *inst )[2], ( *inst )[3] ) : NULL;
        if ( codec == NULL && var == NULL ) return 2; // this backend writes no cook of this root
        Report report = Report();
        bool ok;
        if ( codec != NULL )
        {
            void * value = codec->make();
            codec->reset( value );
            ok = from_text ? codec->from_json( value, (const char *) input.data(), (int64_t) input.size(), &report )
                           : codec->load( value, input.data(), (int64_t) input.size(), &report );
            ok = ok && !report.refused && cook_both( out, f[1], codec->cook_measure( value ), codec->cook, value );
        }
        else
        {
            std::vector<uint8_t> wire;
            if ( from_text )
            {
                if ( !var->json_read( (const char *) input.data(), (int64_t) input.size(), &report, wire ) ) wire.clear();
            }
            else
            {
                wire = input;
            }
            std::vector<uint8_t> region;
            const void * value = wire.empty() ? NULL : var->load( region, wire.data(), (int64_t) wire.size(), &report );
            ok = value != NULL && !report.refused && cook_both( out, f[1], var->cook_measure( value ), var->cook, value );
        }
        if ( !ok ) { fprintf( stderr, "driver: %s: the cook input did not read or did not cook\n", f[1].c_str() ); return 1; }
    }
    return 0;
}

// foreign() reverses the MAGIC word — the eight bytes at offset 0 — which is
// what that word looks like to a reader of the OTHER byte order (§19.1, §7.1).
// It makes the file foreign to WHOEVER READS IT rather than to a particular
// host, so the refusal lands on the magic check every Open puts first and the
// expectation is `refuse` for every leg on every machine.
static std::vector<uint8_t> foreign( const std::vector<uint8_t> & data )
{
    std::vector<uint8_t> out = data;
    if ( out.size() >= 8 )
    {
        for ( int i = 0; i < 4; i++ )
        {
            const uint8_t t = out[i];
            out[i] = out[7 - i];
            out[7 - i] = t;
        }
    }
    return out;
}

static int surface_block( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "block" ) continue;
        std::vector<uint8_t> data;
        if ( !slurp( f[3].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[3].c_str() ); return 1; }
        const char * verdict = open_block( f[1], data, -1 ) ? "open\n" : "refuse\n";
        if ( !spill( out, f[1], verdict, strlen( verdict ) ) ) return 1;
    }
    return 0;
}

// json-hostile: one tree per rule the text form states (§16.2, §16.3, §17.5).
// The answer is the REPORT the read produces, then the §16 text of the value it
// left, or `refused` — the same verdict the engine's own gate holds, over the
// same data, and the value the oracle's read leaves.
static int surface_json_hostile( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "json-hostile" ) continue;
        std::vector<uint8_t> text;
        // the tree is what `schema pack` reads, so the text is <tree>/<root>.json (§17)
        const std::string path = f[4] + "/" + f[3] + ".json";
        if ( !slurp( path.c_str(), text ) ) { fprintf( stderr, "driver: cannot read %s\n", path.c_str() ); return 1; }

        Report report = Report();
        bool ok = false;
        std::vector<char> value_text;
        std::vector<uint8_t> saved;
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            // the builder's value reaches its text through its own saved bytes,
            // which are the write-back wire, and the region read takes them back
            ok = variable->json_read( (const char *) text.data(), (int64_t) text.size(), &report, saved ) && !report.refused;
            if ( ok )
            {
                std::vector<uint8_t> region;
                Report again;
                const void * root = variable->load( region, saved.data(), (int64_t) saved.size(), &again );
                if ( root == NULL || !variable->to_json( root, value_text ) ) return 1;
            }
        }
        else
        {
            const Codec * codec = find_codec( f[2], f[3] );
            if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
            void * value = codec->make();
            codec->reset( value );
            ok = codec->from_json( value, (const char *) text.data(), (int64_t) text.size(), &report ) && !report.refused;
            if ( ok )
            {
                if ( !codec_text( codec, value, value_text ) ) return 1;
                const int64_t size = codec->measure( value );
                if ( size < 0 ) return 1;
                saved.assign( (size_t) size, 0 );
                if ( codec->save( value, saved.data(), size ) != size ) return 1;
            }
        }
        std::vector<char> answer = verdict_line( report, ok );
        if ( ok )
        {
            answer.insert( answer.end(), value_text.begin(), value_text.end() );
            if ( !spill( out, f[1] + ".wire", saved.data(), saved.size() ) ) return 1;
        }
        if ( !spill( out, f[1], answer.data(), answer.size() ) ) return 1;
    }
    return 0;
}

static int surface_block_dump( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "block" ) continue;
        std::vector<uint8_t> data;
        if ( !slurp( f[3].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[3].c_str() ); return 1; }
        std::string text;
        if ( !block_dump( f[1], data, text ) ) return 1;
        if ( !spill( out, f[1], text.data(), text.size() ) ) return 1;
    }
    return 0;
}

// the cross-endian refusal over the block form: the same images with their
// magic reversed, which every leg must refuse
static int surface_block_foreign( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "block" ) continue;
        std::vector<uint8_t> data;
        if ( !slurp( f[3].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[3].c_str() ); return 1; }
        const std::vector<uint8_t> swapped = foreign( data );
        const char * verdict = open_block( f[1], swapped, -1 ) ? "open\n" : "refuse\n";
        if ( !spill( out, f[1], verdict, strlen( verdict ) ) ) return 1;
    }
    return 0;
}

static int surface_forgery( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "forgery" ) continue;
        if ( f[2] != "block" ) continue; // the cook's battery is its own binary's
        std::vector<uint8_t> data;
        if ( !slurp( f[4].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        // the POINTER column is the caller's buffer: 0 an aligned base, 1..63
        // that many bytes past one. The block battery carries one such row, so
        // the base-alignment clause is held here as it is in the cook battery.
        const int lead = f[6] == "null" ? 0 : (int) strtol( f[6].c_str(), NULL, 0 );
        const char * verdict = open_block( f[3], data, (int64_t) strtoll( f[5].c_str(), NULL, 0 ), NULL, lead ) ? "open\n" : "refuse\n";
        if ( !spill( out, f[1], verdict, strlen( verdict ) ) ) return 1;
    }
    return 0;
}

// block-reason: the REASON BlockOpen names beside its false, one word per
// forgery a `refusal` line asks for, spelled as the enum's own value name so
// the manifest pins the first failing clause (docs/SPEC-TABLES.md §7, §19.2)
static const char * reason_name( blockdemo::TableRefuseReason reason )
{
    switch ( reason )
    {
        case blockdemo::ok: return "ok";
        case blockdemo::not_a_cook: return "not_a_cook";
        case blockdemo::foreign_order: return "foreign_order";
        case blockdemo::wrong_build_version: return "wrong_build_version";
        case blockdemo::reserved_not_zero: return "reserved_not_zero";
        case blockdemo::bad_alignment: return "bad_alignment";
        case blockdemo::truncated: return "truncated";
        case blockdemo::unaligned_base: return "unaligned_base";
        case blockdemo::bad_layout: return "bad_layout";
        case blockdemo::unknown_form: return "unknown_form";
        case blockdemo::count_over_length: return "count_over_length";
        case blockdemo::count_over_extent_cap: return "count_over_extent_cap";
        case blockdemo::blob_over_size_cap: return "blob_over_size_cap";
        case blockdemo::data_cycle: return "data_cycle";
        case blockdemo::invalid_value: return "invalid_value";
        case blockdemo::allocation_failed: return "allocation_failed";
        case blockdemo::wire_damaged: return "wire_damaged";
        case blockdemo::malformed: return "malformed";
        case blockdemo::out_of_range: return "out_of_range";
        case blockdemo::kind_mismatch: return "kind_mismatch";
        case blockdemo::base64_damaged: return "base64_damaged";
        case blockdemo::base64_not_canonical: return "base64_not_canonical";
        case blockdemo::integrity_mismatch: return "integrity_mismatch";
        case blockdemo::non_finite_float: return "non_finite_float";
        case blockdemo::blob_shared: return "blob_shared";
        case blockdemo::previous_form: return "previous_form";
        case blockdemo::newer_form: return "newer_form";
        case blockdemo::message_form_as_file: return "message_form_as_file";
        case blockdemo::layout_unknown: return "layout_unknown";
        case blockdemo::layout_unsupported: return "layout_unsupported";
        case blockdemo::layout_newer: return "layout_newer";
        case blockdemo::layout_malformed: return "layout_malformed";
        case blockdemo::layout_count_mismatch: return "layout_count_mismatch";
        case blockdemo::layout_kind_unknown: return "layout_kind_unknown";
        case blockdemo::layout_kind_invalid: return "layout_kind_invalid";
        case blockdemo::layout_size_mismatch: return "layout_size_mismatch";
        case blockdemo::layout_tree_unclosed: return "layout_tree_unclosed";
        case blockdemo::layout_too_deep: return "layout_too_deep";
        case blockdemo::layout_record_too_large: return "layout_record_too_large";
        case blockdemo::plan_too_large: return "plan_too_large";
        case blockdemo::no_layout: return "no_layout";
        case blockdemo::batch_too_large: return "batch_too_large";
        case blockdemo::text_ill_formed: return "text_ill_formed";
        case blockdemo::count_over_bound: return "count_over_bound";
        case blockdemo::length_over_bound: return "length_over_bound";
        case blockdemo::missing_required: return "missing_required";
    }
    return "?";
}

static int surface_block_reason( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & r = manifest_lines[i].field;
        if ( r[0] != "refusal" || r.size() < 3 || r[2] != "block" ) continue;
        const std::vector<std::string> * forgery = NULL;
        for ( size_t j = 0; j < manifest_lines.size(); j++ )
        {
            const std::vector<std::string> & f = manifest_lines[j].field;
            if ( f[0] == "forgery" && f[1] == r[1] ) forgery = &f;
        }
        if ( forgery == NULL ) { fprintf( stderr, "driver: refusal %s names no forgery\n", r[1].c_str() ); return 1; }
        const std::vector<std::string> & f = *forgery;
        std::vector<uint8_t> data;
        if ( !slurp( f[4].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        blockdemo::TableRefuseReason reason = blockdemo::ok;
        const int lead = f[6] == "null" ? 0 : (int) strtol( f[6].c_str(), NULL, 0 );
        const bool opened = open_block( f[3], data, (int64_t) strtoll( f[5].c_str(), NULL, 0 ), &reason, lead );
        std::string answer = std::string( opened ? "ok" : reason_name( reason ) ) + "\n";
        if ( !spill( out, r[1], answer.data(), answer.size() ) ) return 1;
    }
    return 0;
}

// ---------------------------------------------------------------------------
// pinning the block forgeries as DATA (test/conformance/README.md)
// ---------------------------------------------------------------------------
//
// The battery in test/tables/block_main.cpp names the words it damages through
// the projection's own field names, which no other language can read out of a
// manifest. This mode resolves each of them to a BYTE OFFSET inside the block
// image and prints the manifest lines, so the eleven words — and the
// base-alignment row that damages no word — become data every backend runs, the
// C++ one included, through the same path.

struct Patch
{
    const char * name;
    uint64_t offset;
    int width;
    uint64_t value;
    const char * verdict;
    const char * label;
    int64_t claim; // the extent the caller passes; -1 means "the file's own"
};

static uint64_t swap64( uint64_t v )
{
    uint64_t r = 0;
    for ( int i = 0; i < 8; i++ ) r |= ( ( v >> ( i * 8 ) ) & 0xffull ) << ( 56 - i * 8 );
    return r;
}

static int emit_block_forgeries()
{
    std::vector<uint8_t> data;
    if ( !slurp( "testdata/wire/tables/block_render.bin", data ) )
    {
        fprintf( stderr, "driver: cannot read the block golden\n" );
        return 1;
    }
    Aligned storage = {};
    if ( !storage.create( data, -1 ) ) return 1;
    blockdemo::RenderFrameBlock block;
    if ( !blockdemo::RenderFrameBlockOpen( block, storage.base, storage.bytes ) )
    {
        fprintf( stderr, "driver: the block golden does not open\n" );
        return 1;
    }

    const uint8_t * base = storage.base;
    const uint64_t at_magic = (uint64_t) ( (const uint8_t *) &block.projection->magic - base );
    const uint64_t at_build = (uint64_t) ( (const uint8_t *) &block.projection->build_version - base );
    const uint64_t at_order = (uint64_t) ( (const uint8_t *) &block.projection->byte_order - base );
    const uint64_t at_ships_offset = (uint64_t) ( (const uint8_t *) &block.projection->ships.offset_of - base );
    const uint64_t at_ships_stride = (uint64_t) ( (const uint8_t *) &block.projection->ships.stride - base );
    const uint64_t at_ships_count = (uint64_t) ( (const uint8_t *) &block.projection->ships.count - base );
    const uint64_t at_cameras_offset = (uint64_t) ( (const uint8_t *) &block.projection->cameras.offset_of - base );

    const uint64_t magic = block.projection->magic;
    const uint64_t build = block.projection->build_version;
    const uint64_t order = block.projection->byte_order;
    const uint64_t ships_offset = block.projection->ships.offset_of;
    const uint32_t ships_stride = block.projection->ships.stride;

    const Patch patches[] = {
        { "block_magic_foreign", at_magic, 8, 0, "refuse", "a foreign magic", -1 },
        { "block_magic_swapped", at_magic, 8, swap64( magic ), "refuse", "a block of the other byte order", -1 },
        { "block_build_version", at_build, 8, build ^ 1ull, "refuse", "a build this one does not match", -1 },
        { "block_byte_order", at_order, 8, (uint64_t) ( order == 1 ? 2 : 1 ), "refuse", "the other byte order in the prologue's own word", -1 },
        { "block_array_unaligned", at_ships_offset, 8, ships_offset + 1, "refuse", "an array start not aligned for its element", -1 },
        { "block_array_in_prologue", at_ships_offset, 8, 0, "refuse", "an array start inside the projection", -1 },
        { "block_array_escapes", at_ships_offset, 8, (uint64_t) blockdemo::RenderFrameBlockMaxBytes, "refuse", "an array that leaves the block", -1 },
        { "block_pitch", at_ships_stride, 4, (uint64_t) ( ships_stride + 8 ), "refuse", "a pitch that is not this build's own", -1 },
        { "block_count_past_extent", at_ships_count, 4, 4000, "refuse", "a count whose rows leave the extent the caller passed", 200000 },
        { "block_count_past_maximum", at_ships_count, 4,
          (uint64_t) ( blockdemo::RenderFrameBlock::ShipsMax + 904 ), "refuse",
          "a count past the declared maximum, inside a roomy extent", (int64_t) blockdemo::RenderFrameBlockMaxBytes },
        { "block_offset_overflow", at_cameras_offset, 8, 0x7fffffffffffffc0ull, "refuse",
          "an offset 64-aligned and just under 2^63 — the forgery fuzzer's find", -1 },
    };

    printf( "# THE BLOCK FORGERY BATTERY as data (docs/SPEC-TABLES.md §19.2), pinned from\n" );
    printf( "# test/tables/block_main.cpp's eleven by test/conformance/cpp: each row is one\n" );
    printf( "# word of an otherwise valid block image, resolved to a byte offset, plus the\n" );
    printf( "# base-alignment row the cook battery has always carried and the block's had not.\n" );
    printf( "#\n" );
    printf( "#   forgery <name> block <subject> <base> <pointer> <offset> <width> <value> <extent> <verdict> <label>\n" );
    printf( "#\n" );
    printf( "# <base> names the block line the image comes from; <extent> is the length the\n# CALLER claims (-1: the image's own); <pointer> is the BUFFER the caller holds —\n# 0 an aligned base, 1..63 that many bytes past one, `null` no buffer at all —\n# because an unaligned base is a pointer fact and not a file fact.\n" );
    printf( "# The harness applies the patch and hands a driver a path.\n" );
    printf( "# Repin with: make conformance-pin.\n" );
    for ( size_t i = 0; i < sizeof( patches ) / sizeof( patches[0] ); i++ )
    {
        const Patch & p = patches[i];
        printf( "forgery %-26s block block_render block_render 0 0x%04llx %d 0x%-16llx %8lld %s %s\n",
                p.name, (unsigned long long) p.offset, p.width,
                (unsigned long long) p.value, (long long) p.claim, p.verdict, p.label );
    }
    // and the base-alignment row, which damages no word: the image is whole and
    // the CALLER's buffer sits one byte past an aligned base, so only the last
    // clause of the read can refuse it (docs/SPEC-TABLES.md §19.2).
    printf( "forgery %-26s block block_render block_render 1 -1 0 0 -1 refuse a base one byte past an aligned one\n",
            "block_base_unaligned" );
    storage.destroy();
    return 0;
}

int main( int argc, char ** argv )
{
    if ( argc >= 2 && strcmp( argv[1], "emit-block-forgeries" ) == 0 )
    {
        return emit_block_forgeries();
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
        printf( "wire\nmessage\nreport\nframing\nretain\nretain-save\njson-read\njson-write\njson-hostile\ncook-write\nblock\nblock-foreign\nblock-dump\nforgery\nblock-reason\n" );
        return 0;
    }
    if ( argc < 4 )
    {
        fprintf( stderr, "usage: %s <manifest> <surface> <outdir>\n", argv[0] );
        return 2;
    }
    const std::string out = argv[3];

    if ( surface == "wire" ) return surface_wire( out );
    if ( surface == "message" ) return surface_message( out );
    if ( surface == "report" ) return surface_report( out );
    if ( surface == "framing" ) return surface_framing( out );
    if ( surface == "retain" ) return surface_retain( out );
    if ( surface == "retain-save" ) return surface_retain_save( out );
    if ( surface == "json-read" ) return surface_json_read( out );
    if ( surface == "json-write" ) return surface_json_write( out ) || surface_json_write_refusal( out );
    if ( surface == "json-text" ) return surface_json_text( out );
    if ( surface == "json-hostile" ) return surface_json_hostile( out );
    if ( surface == "cook-write" ) return surface_cook_write( out );
    if ( surface == "block" ) return surface_block( out );
    if ( surface == "block-foreign" ) return surface_block_foreign( out );
    if ( surface == "block-dump" ) return surface_block_dump( out );
    if ( surface == "forgery" ) return surface_forgery( out );
    if ( surface == "block-reason" ) return surface_block_reason( out );
    return 2;
}
