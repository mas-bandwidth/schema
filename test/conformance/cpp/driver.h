// THE C++ CONFORMANCE DRIVER'S SHARED DECLARATIONS (test/conformance/README.md).
//
// The driver is one binary, build/conformance-cpp, built from several
// translation units (Makefile, CONFORMANCE_DRIVER_TUS) that make compiles side
// by side: main.cpp holds the dispatch and the codec registry, and every other
// unit one part of the driver:
//
//   driver_codec.cpp   every surface answered through the codec rows, which
//                      reads no generated header at all
//   driver_blocks.cpp  the block surfaces (block_walk.h)
//   driver_wref.cpp    the write contract's batteries
//   driver_<family>    one family of generated units: the header of each unit
//                      in it, and its codec rows
//
// One unit holding all of it compiled for 71 s at -O0, serially, inside the
// build the CI's produce job has two minutes for; each family now carries the
// generated code of its own units only. A unit's compile seconds are recorded
// beside its object and held to a budget (Makefile,
// CONFORMANCE_DRIVER_TU_BUDGET), so a family that grows back past it goes red
// by name.
//
// This header is what every unit shares: the erased row shapes and the macros
// that fill them, the registry of rows each family hands main.cpp, and the
// functions one unit answers for another.
#pragma once

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <limits>
#include <new>
#include <string>
#include <type_traits>
#include <vector>

// the manifest reader and the file helpers (manifest.h)
#include "manifest.h"

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
    // One `message-batch` row (docs/SPEC-TABLES.md §3.3): load the batch with
    // room for every body and answer the count the loader left in its
    // out-parameter.
    bool ( *batch )( const uint8_t * announcement, int64_t announcement_bytes,
                     const uint8_t * message, int64_t message_bytes, int64_t & reported );
    // An `announce send` row (test/conformance/README.md): SaveMessages of one
    // body with NO announcement read or written, so under the writer's own
    // vocabulary (FL-18). False when the writer refuses.
    bool ( *send )( const void * value, std::vector<uint8_t> & out );
};

#define MSGCODEC( unit_key, ns, type )                                                          \
    {                                                                                           \
        unit_key, #type,                                                                        \
        []( const uint8_t * a, int64_t an, const uint8_t * b, int64_t n,                        \
            std::vector<uint8_t> & out, Report * r ) {                                          \
            const int64_t vocab_cap = ns::kTableMessageEntriesHere > 4096 ? ns::kTableMessageEntriesHere : 4096; \
            std::vector<ns::TableMessageEntry> entries( (size_t) vocab_cap );                   \
            ns::TableVocabulary vocabulary( entries.data(), vocab_cap );     \
            ns::TableReport inner;                                                              \
            if ( !ns::AnnounceRead( vocabulary, a, an, &inner ) ) { copy_report( inner, r ); return false; } \
            std::vector<ns::type> values( 256 );                                                \
            for ( size_t i = 0; i < values.size(); i++ ) { ns::type##Reset( values[i] ); }      \
            int64_t count = (int64_t) values.size();                                            \
            if ( !ns::type##LoadMessages( values.data(), &count, vocabulary, b, n, &inner ) ) { copy_report( inner, r ); return false; } \
            copy_report( inner, r );                                                            \
            int64_t size = ns::type##MeasureMessages( values.data(), count, &inner );           \
            if ( size < 0 ) return false;                                                       \
            out.assign( (size_t) size, 0 );                                                     \
            return ns::type##SaveMessages( values.data(), count, out.data(), size, &inner ) == size;   \
        },                                                                                      \
        []( const uint8_t * a, int64_t an, const uint8_t * b, int64_t n, int64_t & reported ) { \
            const int64_t vocab_cap = ns::kTableMessageEntriesHere > 4096 ? ns::kTableMessageEntriesHere : 4096; \
            std::vector<ns::TableMessageEntry> entries( (size_t) vocab_cap );                   \
            ns::TableVocabulary vocabulary( entries.data(), vocab_cap );                        \
            ns::TableReport inner;                                                              \
            if ( !ns::AnnounceRead( vocabulary, a, an, &inner ) ) { return false; }             \
            std::vector<ns::type> values( 256 );                                                \
            for ( size_t i = 0; i < values.size(); i++ ) { ns::type##Reset( values[i] ); }      \
            ns::TableReport loaded;                                                             \
            reported = (int64_t) values.size();                                                 \
            (void) ns::type##LoadMessages( values.data(), &reported, vocabulary, b, n, &loaded ); \
            return true;                                                                        \
        },                                                                                      \
        []( const void * v, std::vector<uint8_t> & out ) {                                      \
            ns::TableReport inner;                                                              \
            const int64_t size = ns::type##MeasureMessages( (const ns::type *) v, 1, &inner );  \
            if ( size < 0 ) return false;                                                       \
            out.assign( (size_t) size, 0 );                                                     \
            return ns::type##SaveMessages( (const ns::type *) v, 1, out.data(), size, &inner ) == size; \
        }                                                                                       \
    }


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


// THE RETAIN-REFUSAL SURFACE (test/conformance/README.md): ONE retention store
// through LoadRetain of the first wire, SaveRetain, LoadRetain of the refused
// wire into the same store (a region of its own, so the first value stays
// whole; the first's size where LoadMeasure refuses the wire), and SaveRetain
// of the first value again. The verdicts and what the second save did go in
// <case>, the first save in <case>.before, the second in <case>.wire.
struct RetainSequence
{
    bool first_read = false;
    bool second_refused = false;
    bool second_saved = false;
    std::vector<uint8_t> before;
    std::vector<uint8_t> after;
};

struct RetainSequenceCodec
{
    const char * unit;
    const char * root;
    bool ( *run )( const std::vector<uint8_t> & first, const std::vector<uint8_t> & second, RetainSequence & answer );
};

#define RETAINSEQUENCE( unit_key, ns, type )                                                    \
    {                                                                                           \
        unit_key, #type,                                                                        \
        []( const std::vector<uint8_t> & first, const std::vector<uint8_t> & second,            \
            RetainSequence & answer ) {                                                         \
            std::vector<uint8_t> bytes( (size_t) kRetainRoomy, 0 );                             \
            std::vector<ns::TableRetain::Id> ids( (size_t) ( kRetainRoomy / 8 ) );              \
            ns::TableRetain retain;                                                             \
            retain.bytes = bytes.data();                                                        \
            retain.capacity = kRetainRoomy;                                                     \
            retain.ids = ids.data();                                                            \
            retain.id_capacity = (int32_t) ids.size();                                          \
            int64_t need = ns::type##LoadMeasure( first.data(), (int64_t) first.size() );       \
            if ( need < 0 ) return true;                                                        \
            std::vector<uint8_t> region( (size_t) ( need ? need : 1 ), 0 );                    \
            ns::TableReport load;                                                               \
            const ns::type * root = ns::type##LoadRetain( region.data(), need, first.data(), (int64_t) first.size(), &retain, &load ); \
            if ( root == NULL ) return true;                                                    \
            answer.first_read = true;                                                           \
            int64_t size = ns::type##MeasureRetain( root, &retain );                            \
            if ( size < 0 ) return false;                                                       \
            answer.before.assign( (size_t) size, 0 );                                           \
            ns::TableReport save;                                                               \
            if ( ns::type##SaveRetain( root, &retain, answer.before.data(), size, &save ) != size ) return false; \
            int64_t other_need = ns::type##LoadMeasure( second.data(), (int64_t) second.size() ); \
            if ( other_need < 0 ) other_need = need;                                            \
            std::vector<uint8_t> other( (size_t) ( other_need ? other_need : 1 ), 0 );          \
            ns::TableReport refused;                                                            \
            answer.second_refused = ns::type##LoadRetain( other.data(), other_need, second.data(), (int64_t) second.size(), &retain, &refused ) == NULL || refused.refused || refused.damaged; \
            size = ns::type##MeasureRetain( root, &retain );                                    \
            if ( size < 0 ) return true;                                                        \
            answer.after.assign( (size_t) size, 0 );                                            \
            ns::TableReport again;                                                              \
            answer.second_saved = ns::type##SaveRetain( root, &retain, answer.after.data(), size, &again ) == size; \
            return true;                                                                        \
        }                                                                                       \
    }

// ---------------------------------------------------------------------------
// THE ANNOUNCEMENT (docs/SPEC-TABLES.md §3.3; test/conformance/README.md,
// `announce`): a unit's announcement writer, and its reader with the strict
// flag set or left at its default
// ---------------------------------------------------------------------------
struct AnnounceUnit
{
    const char * unit;
    int64_t ( *measure )();
    int64_t ( *write )( uint8_t *, int64_t );
    bool ( *read )( const uint8_t *, int64_t, bool );
};

// A fresh vocabulary with room for the widest announcement the corpus sends,
// as the `message` surface makes it; `strict` is the flag the first
// AnnounceRead reads.
#define ANNOUNCEUNIT( unit_key, ns )                                                                    \
    {                                                                                                   \
        unit_key, []() { return ns::AnnounceMeasure(); },                                               \
        []( uint8_t * b, int64_t n ) { return ns::Announce( b, n ); },                                  \
        []( const uint8_t * a, int64_t an, bool strict ) {                                              \
            const int64_t cap = ns::kTableMessageEntriesHere > 4096 ? ns::kTableMessageEntriesHere : 4096; \
            std::vector<ns::TableMessageEntry> entries( (size_t) cap );                                 \
            ns::TableVocabulary vocabulary( entries.data(), cap );                                      \
            vocabulary.strict = strict;                                                                 \
            ns::TableReport report;                                                                     \
            return ns::AnnounceRead( vocabulary, a, an, &report );                                      \
        }                                                                                               \
    }

// ---------------------------------------------------------------------------
// READ INTO THE CALLER'S VALUES (docs/SPEC-TABLES.md §6.5, §3.4; README,
// `read-into`): ONE report and ONE set of values through a lawful read, a
// refused read and the lawful read again, the text of every value written
// after the refused one
// ---------------------------------------------------------------------------
template <typename R>
static bool read_into_refused( const R & r )
{
    return r.refused || r.damaged || (int) r.refuse_reason != 0;
}

inline const char * read_into_line( bool ok, bool refused )
{
    return ok && !refused ? "read\n" : "refused\n";
}

struct ReadIntoCodec
{
    const char * unit;
    const char * root;
    // 0 answered, 1 no such form here, 2 the runtime gave no value to answer
    // with (named on stderr)
    int ( *run )( const std::string & form, const std::vector<uint8_t> & lawful, const std::vector<uint8_t> & damaged,
                  int64_t count, const std::vector<uint8_t> & announcement, std::string & answer );
};

// A VARIABLE root's caller value is its BUILDER: LoadBuilder reads the
// variable form into it and FromJson the text, each with the one report.
#define READINTO_VAR( unit_key, ns, type )                                                                          \
    {                                                                                                               \
        unit_key, #type,                                                                                            \
        []( const std::string & form, const std::vector<uint8_t> & lawful, const std::vector<uint8_t> & damaged,    \
            int64_t count, const std::vector<uint8_t> &, std::string & answer ) -> int {                            \
            if ( ( form != "variable" && form != "json" ) || count != 1 ) return 1;                                 \
            ns::type##Builder builder;                                                                              \
            ns::TableReport report;                                                                                 \
            auto read = [&]( const std::vector<uint8_t> & in ) {                                                    \
                try                                                                                                 \
                {                                                                                                   \
                    const bool ok = form == "variable"                                                              \
                                        ? ns::type##LoadBuilder( builder, in.data(), (int64_t) in.size(), &report ) \
                                        : ns::type##FromJson( builder, (const char *) in.data(), (int64_t) in.size(), &report ); \
                    answer += read_into_line( ok, read_into_refused( report ) );                                    \
                }                                                                                                   \
                catch ( ... )                                                                                       \
                {                                                                                                   \
                    answer += "threw\n";                                                                            \
                }                                                                                                   \
            };                                                                                                      \
            read( lawful );                                                                                         \
            read( damaged );                                                                                        \
            const ns::type * root = builder.GetRoot();                                                              \
            if ( root == NULL ) { fprintf( stderr, "driver: read-into: the builder holds no root after the refused read\n" ); return 2; } \
            const int64_t size = ns::type##ToJsonMeasure( root );                                                   \
            if ( size < 0 ) { fprintf( stderr, "driver: read-into: ToJsonMeasure refused the value the read left\n" ); return 2; } \
            std::vector<char> text( (size_t) size + 1 );                                                            \
            if ( ns::type##ToJson( root, text.data(), size ) != size ) { fprintf( stderr, "driver: read-into: ToJson refused the value the read left\n" ); return 2; } \
            answer.append( text.data(), (size_t) size );                                                            \
            read( lawful );                                                                                         \
            return 0;                                                                                               \
        }                                                                                                           \
    }

// A FIXED root's caller values are an array of `count`: FixedLoad with that
// capacity, or LoadMessages after the connection's announcement is read.
#define READINTO_FIXED( unit_key, ns, type )                                                                        \
    {                                                                                                               \
        unit_key, #type,                                                                                            \
        []( const std::string & form, const std::vector<uint8_t> & lawful, const std::vector<uint8_t> & damaged,    \
            int64_t count, const std::vector<uint8_t> & announcement, std::string & answer ) -> int {               \
            if ( form != "fixed" && form != "message" ) return 1;                                                   \
            std::vector<ns::type> values( (size_t) count );                                                         \
            for ( size_t i = 0; i < values.size(); i++ ) ns::type##Reset( values[i] );                             \
            ns::TableReport report;                                                                                 \
            std::vector<ns::TableFixedEntry> plan( (size_t) kFixedPlanCapacity );                                   \
            const int64_t cap = ns::kTableMessageEntriesHere > 4096 ? ns::kTableMessageEntriesHere : 4096;          \
            std::vector<ns::TableMessageEntry> entries( (size_t) cap );                                             \
            ns::TableVocabulary vocabulary( entries.data(), cap );                                                  \
            if ( form == "message" )                                                                                \
            {                                                                                                       \
                ns::TableReport announced;                                                                          \
                if ( !ns::AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), &announced ) ) \
                {                                                                                                   \
                    fprintf( stderr, "driver: read-into: the connection's announcement did not read\n" );           \
                    return 2;                                                                                       \
                }                                                                                                   \
            }                                                                                                       \
            auto read = [&]( const std::vector<uint8_t> & in ) {                                                    \
                try                                                                                                 \
                {                                                                                                   \
                    bool ok = false;                                                                                \
                    if ( form == "fixed" )                                                                          \
                        ok = ns::type##FixedLoad( values.data(), count, in.data(), (int64_t) in.size(), plan.data(), \
                                                  kFixedPlanCapacity, NULL, &report ) >= 0;                         \
                    else                                                                                            \
                    {                                                                                               \
                        int64_t loaded = count;                                                                     \
                        ok = ns::type##LoadMessages( values.data(), &loaded, vocabulary, in.data(), (int64_t) in.size(), &report ); \
                    }                                                                                               \
                    answer += read_into_line( ok, read_into_refused( report ) );                                    \
                }                                                                                                   \
                catch ( ... )                                                                                       \
                {                                                                                                   \
                    answer += "threw\n";                                                                            \
                }                                                                                                   \
            };                                                                                                      \
            read( lawful );                                                                                         \
            read( damaged );                                                                                        \
            for ( size_t i = 0; i < values.size(); i++ )                                                            \
            {                                                                                                       \
                const int64_t size = ns::type##ToJsonMeasure( values[i] );                                          \
                if ( size < 0 ) { fprintf( stderr, "driver: read-into: ToJsonMeasure refused value %zu\n", i ); return 2; } \
                std::vector<char> text( (size_t) size + 1 );                                                        \
                if ( ns::type##ToJson( values[i], text.data(), size ) != size ) { fprintf( stderr, "driver: read-into: ToJson refused value %zu\n", i ); return 2; } \
                answer.append( text.data(), (size_t) size );                                                        \
            }                                                                                                       \
            read( lawful );                                                                                         \
            return 0;                                                                                               \
        }                                                                                                           \
    }


// ---------------------------------------------------------------------------
// THE REGISTRY: each family's rows, handed to main.cpp
// ---------------------------------------------------------------------------
//
// A family unit defines its rows in tables of its own and names them in one
// DriverTables, in the order of the members below; a family with no row of a
// shape leaves that member empty ({}). Every member is a Span of a different
// row type, so a table in the wrong place does not compile. main.cpp lists
// every family once, and the find_* lookups walk them in that order. Every
// (unit, root) is registered once across all the families, so the order
// answers nothing: a lookup finds the one row or none.
template <typename T>
struct Span
{
    const T * at;
    size_t count;
};

template <typename T, size_t N>
constexpr Span<T> span_of( const T ( &rows )[N] )
{
    return Span<T>{ rows, N };
}

struct DriverTables
{
    Span<Codec> codecs;
    Span<VarCodec> var_codecs;
    Span<MsgCodec> msg_codecs;
    Span<RetainCodec> retain_codecs;
    Span<RetainSequenceCodec> retain_sequence_codecs;
    Span<AnnounceUnit> announce_units;
    Span<ReadIntoCodec> read_into_codecs;
};

// one per family unit (driver_<family>.cpp)
extern const DriverTables driver_tables_examples;
extern const DriverTables driver_tables_maps;
extern const DriverTables driver_tables_lists;
extern const DriverTables driver_tables_cooktorture;
extern const DriverTables driver_tables_messages;
extern const DriverTables driver_tables_varevo12;
extern const DriverTables driver_tables_varevo3;
extern const DriverTables driver_tables_tblsurf;
extern const DriverTables driver_tables_pointers;
extern const DriverTables driver_tables_backend;
extern const DriverTables driver_tables_evolution_a;
extern const DriverTables driver_tables_evolution_b;

// the lookups (main.cpp): the row for (unit, root), or NULL
const Codec * find_codec( const std::string & unit, const std::string & root );
const VarCodec * find_var_codec( const std::string & unit, const std::string & root );
const MsgCodec * find_msg_codec( const std::string & unit, const std::string & root );
const RetainCodec * find_retain_codec( const std::string & unit, const std::string & root );
const RetainSequenceCodec * find_retain_sequence_codec( const std::string & unit, const std::string & root );
const AnnounceUnit * find_announce_unit( const std::string & unit );
const ReadIntoCodec * find_read_into_codec( const std::string & unit, const std::string & root );

// ---------------------------------------------------------------------------
// what one unit answers for another
// ---------------------------------------------------------------------------

// the surfaces answered through the codec rows (driver_codec.cpp)
int surface_wire( const std::string & out );
int surface_message( const std::string & out, bool prior );
int surface_report( const std::string & out );
int surface_framing( const std::string & out );
int surface_retain( const std::string & out );
int surface_retain_save( const std::string & out );
int surface_retain_refusal( const std::string & out );
int surface_json_read( const std::string & out );
int surface_json_write( const std::string & out );
int surface_json_write_refusal( const std::string & out );
int surface_json_text( const std::string & out );
int surface_json_hostile( const std::string & out );
int surface_cook_write( const std::string & out );
int surface_announce( const std::string & out );
int surface_read_into( const std::string & out );
int surface_json_write_into( const std::string & out );

// the block surfaces (driver_blocks.cpp, over block_walk.h)
int surface_block( const std::string & out );
int surface_block_foreign( const std::string & out );
int surface_block_dump_answer( const std::string & out );
int surface_forgery( const std::string & out );
int surface_block_reason( const std::string & out );
int emit_block_forgeries_answer();

// the write contract's batteries (driver_wref.cpp)
int surface_write_refusal( const std::string & surface, const std::string & out );

// the json-write-refusal damage, which names a field of tabledemo's
// ProfileConfig (driver_examples.cpp)
bool damage_text( const std::string & unit, const std::string & root, void * value, const std::string & damage );

// the one cook-write sharing recipe, blobdemo's Catalog (driver_pointers.cpp)
bool cook_shared( const std::string & out, const std::string & name, const std::vector<std::string> & inst,
                  const std::vector<uint8_t> & text, const std::string & source, const std::string & alias );

// the `region` read-into form, tblsurf's Counted (driver_tblsurf.cpp)
int read_into_region( const std::vector<uint8_t> & lawful, const std::vector<uint8_t> & damaged, int64_t count, std::string & answer );
