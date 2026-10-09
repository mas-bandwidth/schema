// cell cpp/E6 — "Renaming uses the declared identity"
// (docs/roadmap.sexp:2385; audit schema#898, matrix schema#876).
//
// THE LAW (docs/SPEC-TABLES.md §5, "Renaming uses the declared identity"):
//   A field renamed through `was = "old"` keeps its wire identity: the id
//   is fnv1a64 of the WIRE name, and `was` keeps the OLD name, so the
//   layout bytes do not move and the rename is not a version at all.
//
// THE PAIR ON THIS CLASSPATH. V1 declares `name` (V1.schema:71); V2
// renames it `title` with `was = "name"` (V2.schema:65). Both spellings
// carry the SAME wire id fnv1a64("name") = 0xc4bcadba8e631b86, and
// fnv1a64("title") rides nowhere in V2's layout.
//
// THE DERIVATION OF THE WIRE ID (fnv1a64, from ir/tablewire.go TableWireId):
//   h = 0xcbf29ce484222325
//   for each byte: h ^= byte; h *= 0x100000001b3 (all uint64 wrap).
//   fnv1a64("name")  = 0xc4bcadba8e631b86
//   fnv1a64("title") = 0xf5e472e586e4d545
// These are computed below; the generated code must use the OLD name's hash.
//
// THE PRODUCTION PATH this test drives: V1CfgFixedLoad / V2CfgFixedLoad
// (build/tables-generated/v1/V1Table.h and v2/V2Table.h), which read
// records through a compiled plan, pairing fields by wire id. The writer
// (CfgFixedSave) and the round-trip through the same generation exercise
// the read's identity lane.
//
// Run: c++ -std=c++17 -Wall <include/flags from build/conformance-cpp>
//       test/conformance/cpp/rows/E6.cpp -o build/rows-cpp-E6
//       && ./build/rows-cpp-E6

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <vector>

#include "V1Table.h"
#include "V2Table.h"

// ---------------------------------------------------------------------------
// fnv1a64 — the wire identity of a name (ir/tablewire.go TableWireId;
// docs/SPEC-TABLES.md §5), "with no fold and no rebound".
// ---------------------------------------------------------------------------

static uint64_t fnv1a64( const char * name )
{
    uint64_t h = 0xcbf29ce484222325ull;
    while ( *name ) {
        h ^= (uint64_t) (unsigned char) *name++;
        h *= 0x100000001b3ull;
    }
    return h;
}

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { failures++; }
}

// ---------------------------------------------------------------------------
// layout helpers: a layout block is a u32 entry count followed by 17-byte
// entries — id u64 LE, kind u8, size u32 LE, children u32 LE
// (ir/fixedform.go TableFixedLayoutBytes; entry = 17 bytes).
// ---------------------------------------------------------------------------

struct LayoutEntry { uint64_t id; uint8_t kind; uint32_t size; uint32_t children; };

static LayoutEntry entry_at( const uint8_t * layout, int index )
{
    const uint8_t * e = layout + 4 + index * 17;
    LayoutEntry r;
    std::memcpy( &r.id, e, 8 );
    r.kind = e[8];
    std::memcpy( &r.size, e + 9, 4 );
    std::memcpy( &r.children, e + 13, 4 );
    return r;
}

static int entry_count( const uint8_t * layout )
{
    uint32_t c;
    std::memcpy( &c, layout, 4 );
    return (int) c;
}

// ---------------------------------------------------------------------------
// THE ASSERTIONS
// ---------------------------------------------------------------------------

int main()
{
    const uint64_t id_name  = fnv1a64( "name" );
    const uint64_t id_title = fnv1a64( "title" );

    // sanity: the two hashes differ
    check( id_name != id_title,
           "fnv1a64(\"name\") != fnv1a64(\"title\") — the two spellings hash to two ids" );

    // the known wire id from the generated code
    check( id_name == 0xc4bcadba8e631b86ull,
           "fnv1a64(\"name\") == 0xc4bcadba8e631b86 — matches the generated constant" );

    // ---- (1) V1's layout: `name` carries the wire id fnv1a64("name") ----
    {
        bool found = false;
        const int n = entry_count( tblv1::CfgFixedLayout );
        for ( int i = 0; i < n; i++ ) {
            const LayoutEntry e = entry_at( tblv1::CfgFixedLayout, i );
            if ( e.id == id_name ) { found = true; break; }
        }
        check( found, "v1: `name` carries the wire id fnv1a64(\"name\") in the layout" );
    }

    // ---- (2) V2's layout: `title` carries the DECLARED identity (old name) ----
    {
        bool found_old = false;
        bool found_new = false;
        const int n = entry_count( tblv2::CfgFixedLayout );
        for ( int i = 0; i < n; i++ ) {
            const LayoutEntry e = entry_at( tblv2::CfgFixedLayout, i );
            if ( e.id == id_name  ) { found_old = true; }
            if ( e.id == id_title ) { found_new = true; }
        }
        check( found_old,
               "v2: `title` (was=\"name\") carries the DECLARED identity fnv1a64(\"name\")" );
        check( !found_new,
               "v2: no field carries fnv1a64(\"title\") — a bare rename would have" );
    }

    // ---- (3) V1 round-trip: write `name`, read it back ----
    {
        tblv1::Cfg v;
        tblv1::CfgReset( v );
        v.name[0] = 'h'; v.name[1] = 'i';
        v.name_length = 2;

        std::vector<uint8_t> wire( (size_t) tblv1::CfgFixedMeasure( 1 ) );
        check( tblv1::CfgFixedSave( &v, 1, wire.data(), (int64_t) wire.size() )
                   == (int64_t) wire.size(),
               "v1: the writer emits a record with `name` set" );

        tblv1::Cfg back;
        tblv1::CfgReset( back );
        tblv1::TableReport r;
        std::vector<tblv1::TableFixedEntry> plan( 8192 );
        const int64_t n = tblv1::CfgFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(),
            plan.data(), (int32_t) plan.size(), NULL, &r );
        check( n == 1 && !r.refused && !r.malformed,
               "v1: the reader accepts its own wire" );
        check( back.name_length == 2 && back.name[0] == 'h' && back.name[1] == 'i',
               "v1: `name` reads back through the production load path" );
    }

    // ---- (4) V2 round-trip: write `title`, read it back ----
    {
        tblv2::Cfg v;
        tblv2::CfgReset( v );
        v.title[0] = 'h'; v.title[1] = 'i';
        v.title_length = 2;

        std::vector<uint8_t> wire( (size_t) tblv2::CfgFixedMeasure( 1 ) );
        check( tblv2::CfgFixedSave( &v, 1, wire.data(), (int64_t) wire.size() )
                   == (int64_t) wire.size(),
               "v2: the writer emits a record with `title` set" );

        tblv2::Cfg back;
        tblv2::CfgReset( back );
        tblv2::TableReport r;
        std::vector<tblv2::TableFixedEntry> plan( 8192 );
        const int64_t n = tblv2::CfgFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(),
            plan.data(), (int32_t) plan.size(), NULL, &r );
        check( n == 1 && !r.refused && !r.malformed,
               "v2: the reader accepts its own wire" );
        check( back.title_length == 2 && back.title[0] == 'h' && back.title[1] == 'i',
               "v2: `title` reads back through the production load path" );
    }

    // ---- (5) CROSS-GEN: V1 writes `name`, V2 reads it as `title` ----
    // This is the heart of the law: the wire identity is fnv1a64("name"),
    // and V2's reader, seeing that id in the layout (from `was = "name"`),
    // must land the bytes in `title`. V1->V2 is the lineage direction:
    // the wire matches V1's known layout, so V2 compiles a plan from
    // V1's layout. The counters will have `unknown` for fields V1 has
    // that V2 dropped (e.g. `b`) and `widened` for fields whose kind changed,
    // but the `name`->`title` rename moves no counter because it IS the same identity.
    {
        tblv1::Cfg v1;
        tblv1::CfgReset( v1 );
        v1.name[0] = 'h'; v1.name[1] = 'e'; v1.name[2] = 'y';
        v1.name_length = 3;

        std::vector<uint8_t> wire( (size_t) tblv1::CfgFixedMeasure( 1 ) );
        check( tblv1::CfgFixedSave( &v1, 1, wire.data(), (int64_t) wire.size() )
                   == (int64_t) wire.size(),
               "cross: V1 saves a record with `name` = \"hey\"" );

        tblv2::Cfg v2;
        tblv2::CfgReset( v2 );
        tblv2::TableReport r;
        std::vector<tblv2::TableFixedEntry> plan( 8192 );
        const int64_t n = tblv2::CfgFixedLoad(
            &v2, 1, wire.data(), (int64_t) wire.size(),
            plan.data(), (int32_t) plan.size(), NULL, &r );
        check( n == 1 && !r.refused && !r.malformed,
               "cross: V2 reads V1's wire without refusal" );
        check( v2.title_length == 3 && v2.title[0] == 'h' && v2.title[1] == 'e' && v2.title[2] == 'y',
               "cross: V1's `name` lands in V2's `title` — the declared identity matched" );
        // The V1->V2 read has unknown/widened counters for other fields
        // (e.g. `b` removed, `a` widened int32->float32), but the rename
        // field itself does not contribute to any counter — it pairs cleanly.
    }

    if ( failures != 0 ) {
        std::printf( "E6: %d assertion(s) failed\n", failures );
        return 1;
    }
    std::printf( "E6: renaming uses the declared identity on the cpp leg\n" );
    return 0;
}
