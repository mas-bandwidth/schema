// THE C++ CODEC ROSTER: one row per (unit, root) the conformance corpus names.
//
// This is the ONE list of the generated roots the C++ reference reads, and the
// generated headers that declare them. Two drivers dispatch on it and neither
// keeps a list of its own:
//
//   test/conformance/cpp/main.cpp  the conformance driver, every surface
//   test/tables/hostile_main.cpp   the hostile-value gate (make tables-hostile-values)
//
// A row of testdata/conformance/tables/MANIFEST.txt that names a (unit, root)
// missing here is refused by name in both: the conformance driver exits on "no
// codec", the hostile gate prints FAIL for the row. The hostile gate kept a
// hand-copied root list until 2026-11-24, and three landings added json-hostile
// rows on roots it did not list (Depth, then Bounds and FixedBounds, then eight
// more): 40 of 316 rows red, seen by no per-PR tier. internal/ci
// TestCppCodecRosterCarriesEveryJsonHostileRoot holds this file to the
// manifest on every pull request, without a compiler.
//
// Every row is the SAME columns, so a surface is one loop over a table and not
// a switch per root.

#ifndef SCHEMA_TEST_CONFORMANCE_CPP_CODECS_H
#define SCHEMA_TEST_CONFORMANCE_CPP_CODECS_H

#include <cstddef>
#include <cstdint>
#include <cstring>
#include <new>
#include <string>
#include <vector>

#ifndef CONFORMANCE_WRITEBOUNDS_ONLY
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
#include "FixedEvoTable.h"
#include "VarEvo1Table.h"
#include "VarEvo2Table.h"
#include "VarEvo3Table.h"
#include "PriorNarrowTable.h"
#include "CookTortureTable.h"
#include "CookTortureBlock.h"
#include "CookTortureWideTable.h"
// the SHAPES unit (test/tables/shapes): bounds a float32 cannot hold exactly,
// -0, counted arrays with a Min, a degenerate fixed field, scalar union arms
#include "ShapesTable.h"
// an ARRAY OF PACKET UNIONS in a variable and a fixed table (docs/SPEC-TABLES.md §2.6)
#include "PacketUnionsTable.h"
#endif
#include "TableWriteBoundsTable.h"
#ifndef CONFORMANCE_WRITEBOUNDS_ONLY
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
// the VALUE-DEFAULTS unit (docs/SPEC.md §4.2): string, bytes and flags
// defaults in every carried field shape, a variable root and a fixed root
#include "ValueDefaultsTable.h"
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
// THE 2026-11-22 SURFACES (test/conformance/README.md): their own unit, the
// two announcement-cap units (both Announce.schema, so each by its directory)
// and the backend unit's BLOCK BUILDER, which write-refusal-block drives
#include <cerrno>
#include <clocale>
#include "SurfacesTable.h"
#include "announce4096/AnnounceTable.h"
#include "announce4097/AnnounceTable.h"
#include "BackendBlock.h"
#endif

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
[[maybe_unused]] static const int32_t kFixedPlanCapacity = 4096; // unused by the writebounds-only driver

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

#ifndef CONFORMANCE_WRITEBOUNDS_ONLY
static const Codec codecs[] = {
    CODEC( "tabledemo", tabledemo, RootConfig ),
    CODEC( "tabledemo", tabledemo, ProfileConfig ),
    CODEC( "tabledemo", tabledemo, LoadoutConfig ),
    NOFILE_CODEC( "tabledemo", tabledemo, WideBlob ),
    CODEC( "tabledemo", tabledemo, ArchiveConfig ),
    CODEC( "tabledemo", tabledemo, KeyedConfig ),
    CODEC( "tabledemo", tabledemo, PackConfig ),
    CODEC( "tabledemo", tabledemo, CountedMin ),
    CODEC( "tabledemo", tabledemo, FloatBound ),
    CODEC( "tabledemo", tabledemo, Moods ),
    CODEC( "tabledemo", tabledemo, MoodField ),
    CODEC( "tabledemo", tabledemo, MinArmFix ),
    CODEC( "tabledemo", tabledemo, MinPlainFix ),
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
    CODEC( "tblfixedevo", tblfixedevo, EvoRoot ),
    // the MESSAGE FORM's units (docs/SPEC-TABLES.md §3.3): their FILE-form
    // vectors are ordinary instances and ride every surface an instance rides
    CODEC( "backenddemo", backenddemo, LoginRequest ),
    CODEC( "backenddemo", backenddemo, MatchResult ),
    CODEC( "backenddemo", backenddemo, StorePurchase ),
    CODEC( "vocabdemo", vocabdemo, Wide00 ),
    CODEC( "vocabdemo", vocabdemo, Wide09 ),
    CODEC( "vocab9demo", vocab9demo, Wide00 ),
    CODEC( "vocab9demo", vocab9demo, Wide19 ),
    CODEC( "tblvarevo1", tblvarevo1, EvoMsg ),
    CODEC( "tblvarevo3", tblvarevo3, EvoMsg ),
    CODEC( "cooktorture", cooktorture, TortureFixedWref ),
    // the SHAPES unit (test/tables/shapes), opted in by `list` below
    CODEC( "shapes", shapes, FixedBounds ),
    // THE 2026-11-22 SURFACES' unit: Big is past the fixed form's ceiling
    CODEC( "tblsurf", surfaces, Narrow ),
    CODEC( "tblsurf", surfaces, Wide ),
    CODEC( "tblsurf", surfaces, Arrays ),
    NOFILE_CODEC( "tblsurf", surfaces, Big ),
    CODEC( "tblvaluedefaults", tblvaluedefaults, FixedDefaults ),
    CODEC( "packetunions", tblpacketunions, ShapeMsg ),
};
#endif

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
            /* the strict walk's refusal carries its reason out (§6.5): a blob */            \
            /* two slots name is refused here by name, blob_shared (§2.5) */                 \
            ns::TableRefuseReason why = ns::ok;                                                 \
            int64_t need = ns::type##LoadMeasure( b, n, NULL, &why );                           \
            if ( need < 0 )                                                                     \
            {                                                                                   \
                *r = Report();                                                                  \
                r->refused = true;                                                              \
                r->blob_shared = why == ns::blob_shared;                                        \
                return NULL;                                                                    \
            }                                                                                   \
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
#ifndef CONFORMANCE_WRITEBOUNDS_ONLY
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
    VARCODEC( "tblvarevo1", tblvarevo1, EvoDoc ),
    VARCODEC( "tblvarevo2", tblvarevo2, EvoDoc ),
    VARCODEC( "tblvarevo3", tblvarevo3, EvoDoc ),
    VARCODEC( "cooktorture", cooktorture, TortureCook ),
    VARCODEC( "cooktorturewide", cooktorturewide, TortureWideCook ),
    VARCODEC( "shapes", shapes, Bounds ),
    VARCODEC( "packetunions", tblpacketunions, ShapeDoc ),
#endif
    VARCODEC( "writebounds", writebounds, TortureVarWref ),
#ifndef CONFORMANCE_WRITEBOUNDS_ONLY
    VARCODEC( "graphdemo", graphdemo, Scene ),
    VARCODEC( "graphdemo", graphdemo, ListNode ),
    VARCODEC( "tblp2", tblp2, Chain ),
    VARCODEC( "streamdemo", streamdemo, Feed ),
    VARCODEC( "blobdemo", blobdemo, Catalog ),
    VARCODEC( "tblg1", tblg1, Guarded ),
    VARCODEC( "tblw1", tblw1, Fleet ),
    VARCODEC( "tblw2", tblw2, Fleet ),
    VARCODEC( "tblvaluedefaults", tblvaluedefaults, VarDefaults ),
    VARCODEC( "messagedemo", messagedemo, ToolEnvelope ),
    VARCODEC( "featurewitnesses", featurewitnesses, Root ),
    VARCODEC( "featurewitnesses", featurewitnesses, ArrayRoot ),
    VARCODEC( "tblsurf", surfaces, Chain ),
    VARCODEC( "tblsurf", surfaces, Holder ),
    VARCODEC( "tblsurf", surfaces, Counted ),
#endif
};
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
#ifdef CONFORMANCE_WRITEBOUNDS_ONLY
    (void) unit;
    (void) root;
    return NULL;
#else
    for ( size_t i = 0; i < sizeof( codecs ) / sizeof( codecs[0] ); i++ )
    {
        if ( unit == codecs[i].unit && root == codecs[i].root ) return &codecs[i];
    }
    return NULL;
#endif
}

#endif // SCHEMA_TEST_CONFORMANCE_CPP_CODECS_H
