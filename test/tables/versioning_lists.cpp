// THE FIXED FORM READS BACKWARD, NEVER FORWARD — THE LIST ROWS, ON THE C++
// REFERENCE.
//
// The bill is docs/FIXED-FORM-BILL-READS-BACKWARD.md; the law is its §2; the
// procedures are docs/FIXED-FORM-ALGORITHM.md §5; the test design is
// docs/FIXED-FORM-VERSIONING-TESTS.md, and this file is that design's two READ
// columns for the rows that are LISTS — a field list, an enum's variants, a
// union's arms, a flags' bits, an enum-keyed array's slots, a nested table's
// fields:
//
//   NEW-READS-OLD     the widened reader reads the older writer's file: every
//                     old value lands EXACTLY, the reader's tail is its
//                     declared default, and the counters are what §5.2 says.
//   OLD-REFUSES-NEW   the older reader given the widened writer's file refuses
//                     `layout_newer` BEFORE ANY RECORD: nothing decoded, no
//                     counter moved, and the reason is this refusal's own.
//
// Glenn, 2026-09-10: "Newer versions of the fixed table should be able to read
// OLD versions. But old versions CANNOT read new versions, and complain loudly
// and refuse. Nothing else makes sense."
//
// RED FIRST IS THE POINT. `layout_newer` DOES NOT EXIST. This build's reader,
// handed a newer file, compiles a plan from the stranger's layout and READS it
// — which is the old contract, and is exactly what the bill retires. So every
// OLD-REFUSES-NEW column below is RED today, by name, on the known-red list,
// and the day the refusal lands they all go green together and the list has to
// be emptied. A listed case that starts PASSING fails this target, which is how
// the list is held from both ends (the convention is fixedform_properties.cpp's).
//
// THE PAIRS. One row is one definition change and nothing else. Each row has
// two schemas, test/tables/VOLD_<row>.schema and VNEW_<row>.schema, carrying
// the SAME table name `Lineage` — the two layouts are ONE LINEAGE — and
// differing only in their package, so both generations compile into this one
// binary. That is the FX1/FX2 precedent and it means no row needs `Old<Row>` /
// `New<Row>` names.
//
// THE BYTES. test/tables/fixedform_dump.cpp writes `old_<row>.bin` and
// `new_<row>.bin` into build/fixedform-corpus from the same two writers with
// the same hand-set values, which is the corpus every other leg reads. The
// cases below build those same bytes in memory rather than opening the files,
// so this binary needs no corpus on disk — the same choice fixedform_main.cpp
// makes, and each case names its corpus file so a leg can line the two up.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "VOLD_field_appendTable.h"
#include "VNEW_field_appendTable.h"
#include "VOLD_field_deprecateTable.h"
#include "VNEW_field_deprecateTable.h"
#include "VOLD_field_undeprecateTable.h"
#include "VNEW_field_undeprecateTable.h"
#include "VOLD_enum_appendTable.h"
#include "VNEW_enum_appendTable.h"
#include "VOLD_enum_widthTable.h"
#include "VNEW_enum_widthTable.h"
#include "VOLD_union_appendTable.h"
#include "VNEW_union_appendTable.h"
#include "VOLD_union_arm_payload_widenTable.h"
#include "VNEW_union_arm_payload_widenTable.h"
#include "VOLD_flags_appendTable.h"
#include "VNEW_flags_appendTable.h"
#include "VOLD_keyed_array_enum_appendTable.h"
#include "VNEW_keyed_array_enum_appendTable.h"
#include "VOLD_nested_appendTable.h"
#include "VNEW_nested_appendTable.h"
#include "VOLD_rename_without_wasTable.h"
#include "VNEW_rename_without_wasTable.h"

// ---------------------------------------------------------------------------
// the failure count of this translation unit, handed to fixedform_main.cpp's by
// the one registration call at the bottom

static int failures = 0;

static void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
}

// ---------------------------------------------------------------------------
// THE KNOWN-RED LIST, held from both ends (test/tables/fixedform_properties.cpp
// is where this convention lives). A listed case may fail and the target still
// ends green; a listed case that PASSES fails the target, so landing the fix
// means deleting its line here.

struct KnownRed
{
    const char * key;
    const char * waits_on;
    int reached;
    int failed;
};

// emptied as each named red lands. a listed case that PASSES fails the target.
static KnownRed known_red_store[1];
static const int known_red_n = 0;

static KnownRed * find_red( const char * key )
{
    for ( int i = 0; i < known_red_n; ++i )
    {
        if ( std::strcmp( known_red_store[i].key, key ) == 0 ) { return &known_red_store[i]; }
    }
    return NULL;
}

static void check_red( bool ok, const char * key, const char * what )
{
    KnownRed * r = find_red( key );
    if ( r == NULL ) { check( ok, what ); return; }
    r->reached++;
    if ( !ok )
    {
        r->failed++;
        std::printf( "KNOWN-RED [%s]: %s\n", r->key, what );
    }
}

static int known_red_report()
{
    int bad = 0;
    const int n = known_red_n;
    (void) known_red_store;
    std::printf( "versioning lists: KNOWN-RED, %d case(s), each waiting on a named fix:\n", n );
    for ( int i = 0; i < n; ++i )
    {
        const KnownRed & r = known_red_store[i];
        std::printf( "  %-44s  %s  <- %s\n", r.key,
                     r.failed > 0 ? "RED  " : ( r.reached > 0 ? "GREEN" : "UNRUN" ), r.waits_on );
        if ( r.reached == 0 )
        {
            std::printf( "FAIL: KNOWN-RED case %s was never reached: the case that carries it is gone\n", r.key );
            bad++;
        }
        else if ( r.failed == 0 )
        {
            std::printf( "FAIL: KNOWN-RED case %s PASSES now — delete it from known_red[] as part of landing %s\n",
                         r.key, r.waits_on );
            bad++;
        }
    }
    return bad;
}

// ---------------------------------------------------------------------------
// THE REFUSAL, SPELLED WITHOUT THE NAME IT DOES NOT HAVE YET.
//
// The bill names this refusal `layout_newer` and the runtime carries no such
// enumerator, so no case can write it down. What a case CAN write down is
// everything the name is worth, and all of it is observable today:
//
//   1. the read is refused and returns a negative count — before any record;
//   2. no counter moved and nothing is marked malformed (REFUSE is total);
//   3. the reason is NOT one this build already has — not a form byte, and not
//      one of §1.1's eight broken-layout reasons: a newer layout IS a layout,
//      and it is not this reader's to read;
//   4. the destination is untouched, which each case asserts on its own fields.
//
// The reference exposes no TEXT for a reason (TableReport carries the
// enumerator and nothing else), so "the refusal names the row's definition"
// has nowhere to land on this leg yet; the per-row definition is named in the
// case's own message instead, which is what a leg with text will assert.

#define VL_OWN_REASON( NS, r ) ( ( r ).reason == NS::layout_newer )

// the three assertions every OLD-REFUSES-NEW column shares. `definition` is the
// row's own definition, named so the line says what the refusal owes.
template <typename Report>
static void refuses_newer( const char * key, const char * definition, int64_t n, const Report & r,
                           bool own_reason, uint64_t want_hash )
{
    char what[320];
    std::snprintf( what, sizeof( what ),
                   "%s: refused before any record, naming %s", key, definition );
    check_red( n < 0 && r.refused, key, what );
    std::snprintf( what, sizeof( what ), "%s: REFUSE is total — no counter moved", key );
    check_red( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 &&
               !r.malformed, key, what );
    std::snprintf( what, sizeof( what ),
                   "%s: the reason is `layout_newer`'s own — not a form byte, not a broken layout", key );
    check_red( r.refused && own_reason, key, what );
    std::snprintf( what, sizeof( what ),
                   "%s: layout_newer carries the file's hash and nothing else (bill §12.4)", key );
    check_red( r.layout_hash == want_hash, key, what );
}

// the counters §5.2 says a clean NEW-READS-OLD moves, and the ones it does not
template <typename Report>
static void reads_clean( const char * row, const Report & r, int want_unknown, int want_widened )
{
    char what[320];
    std::snprintf( what, sizeof( what ), "%s: NEW-READS-OLD — unknown == %d per §5.2", row, want_unknown );
    check( r.unknown == want_unknown, what );
    std::snprintf( what, sizeof( what ), "%s: NEW-READS-OLD — widened == %d per §5.2", row, want_widened );
    check( r.widened == want_widened, what );
    std::snprintf( what, sizeof( what ),
                   "%s: NEW-READS-OLD — nothing clamped, no kind moved, no damage, no refusal", row );
    check( r.clamped == 0 && r.kind_mismatch == 0 && !r.malformed && !r.refused, what );
}

// ---------------------------------------------------------------------------
// field_append: vec {x,y,z} -> {x,y,z,w}   (old_field_append.bin / new_field_append.bin)

static void field_append_case()
{
    std::vector<uint8_t> oldf( (size_t) vold_field_append::LineageFixedMeasure( 1 ) );
    {
        vold_field_append::Lineage v;
        vold_field_append::LineageReset( v );
        v.x = 11; v.y = 22; v.z = 33;
        check( vold_field_append::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) ==
               (int64_t) oldf.size(), "field_append: the OLD file saves" );
    }
    std::vector<uint8_t> newf( (size_t) vnew_field_append::LineageFixedMeasure( 1 ) );
    {
        vnew_field_append::Lineage v;
        vnew_field_append::LineageReset( v );
        v.x = 44; v.y = 55; v.z = 66; v.w = 777;
        check( vnew_field_append::LineageFixedSave( &v, 1, newf.data(), (int64_t) newf.size() ) ==
               (int64_t) newf.size(), "field_append: the NEW file saves" );
    }

    // NEW-READS-OLD
    {
        vnew_field_append::Lineage back;
        vnew_field_append::LineageReset( back );
        vnew_field_append::TableReport r;
        std::vector<vnew_field_append::TableFixedEntry> plan( 1024 );
        const int64_t n = vnew_field_append::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                              plan.data(), 1024, NULL, &r );
        check( n == 1, "field_append: NEW-READS-OLD — one record" );
        check( back.x == 11 && back.y == 22 && back.z == 33,
               "field_append: NEW-READS-OLD — every old value lands exactly" );
        check( back.w == 77, "field_append: NEW-READS-OLD — the appended field `w` takes its declared default" );
        reads_clean( "field_append", r, 0, 0 );
    }

    // OLD-REFUSES-NEW
    {
        vold_field_append::Lineage back;
        vold_field_append::LineageReset( back );
        vold_field_append::TableReport r;
        std::vector<vold_field_append::TableFixedEntry> plan( 1024 );
        const int64_t n = vold_field_append::LineageFixedLoad( &back, 1, newf.data(), (int64_t) newf.size(),
                                                              plan.data(), 1024, NULL, &r );
        refuses_newer( "field_append/OLD-REFUSES-NEW", "the field `w`", n, r,
                       VL_OWN_REASON( vold_field_append, r ), vnew_field_append::LineageFixedHash );
        check_red( back.x == 0 && back.y == 0 && back.z == 0, "field_append/OLD-REFUSES-NEW",
                   "field_append/OLD-REFUSES-NEW: nothing was decoded — every field is still its default" );
    }
}

// ---------------------------------------------------------------------------
// field_deprecate: {a,b,c} -> {a, b deprecated, c}
// (old_field_deprecate.bin / new_field_deprecate.bin)
//
// The second column of this row is NOT a refusal: a deprecation is not newer
// (docs/FIXED-FORM-VERSIONING-TESTS.md names it so), and the OLD reader READS
// the new file with `b` landing as written, because the new writer still writes
// the slot. What the NEW reader owes is the other half: `b` dropped and counted
// once under `unknown`.

static void field_deprecate_case()
{
    std::vector<uint8_t> oldf( (size_t) vold_field_deprecate::LineageFixedMeasure( 1 ) );
    {
        vold_field_deprecate::Lineage v;
        vold_field_deprecate::LineageReset( v );
        v.a = 1; v.b = 2; v.c = 3;
        check( vold_field_deprecate::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) ==
               (int64_t) oldf.size(), "field_deprecate: the OLD file saves" );
    }
    std::vector<uint8_t> newf( (size_t) vnew_field_deprecate::LineageFixedMeasure( 1 ) );
    {
        vnew_field_deprecate::Lineage v;
        vnew_field_deprecate::LineageReset( v );
        v.a = 4; v.b = 5; v.c = 6;
        check( vnew_field_deprecate::LineageFixedSave( &v, 1, newf.data(), (int64_t) newf.size() ) ==
               (int64_t) newf.size(), "field_deprecate: the NEW file saves" );
    }

    // NEW-READS-OLD: a,b,c exact, no counter (bill §12.3). Deprecated is READ
    // on every plan, identity included; the application ignores it.
    {
        vnew_field_deprecate::Lineage back;
        vnew_field_deprecate::LineageReset( back );
        vnew_field_deprecate::TableReport r;
        std::vector<vnew_field_deprecate::TableFixedEntry> plan( 1024 );
        const int64_t n = vnew_field_deprecate::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                                 plan.data(), 1024, NULL, &r );
        check( n == 1, "field_deprecate: NEW-READS-OLD — one record" );
        check( back.a == 1 && back.b == 2 && back.c == 3,
               "field_deprecate: NEW-READS-OLD — a,b,c exact; deprecated `b` is READ, nothing dropped" );
        check( r.unknown == 0 && r.clamped == 0 && r.kind_mismatch == 0 && !r.malformed && !r.refused,
               "field_deprecate: NEW-READS-OLD — no counter (bill §12.3)" );
    }

    // OLD-READS-NEW: a deprecation is not newer, so this is a READ
    {
        vold_field_deprecate::Lineage back;
        vold_field_deprecate::LineageReset( back );
        vold_field_deprecate::TableReport r;
        std::vector<vold_field_deprecate::TableFixedEntry> plan( 1024 );
        const int64_t n = vold_field_deprecate::LineageFixedLoad( &back, 1, newf.data(), (int64_t) newf.size(),
                                                                 plan.data(), 1024, NULL, &r );
        check( n == 1, "field_deprecate: OLD-READS-NEW — a deprecation is not newer, so the old reader reads" );
        check( back.a == 4 && back.b == 5 && back.c == 6,
               "field_deprecate: OLD-READS-NEW — `b` lands as written, because the new writer still writes it" );
        check( !r.refused && !r.malformed, "field_deprecate: OLD-READS-NEW — no refusal and no damage" );
    }
}

// ---------------------------------------------------------------------------
// field_undeprecate: {a, b deprecated, c} -> {a,b,c}
// (old_field_undeprecate.bin / new_field_undeprecate.bin)
//
// Allowed in both directions, and the row's whole content is that BOTH reads
// land (docs/FIXED-FORM-VERSIONING-TESTS.md: "reads" in both read columns).

static void field_undeprecate_case()
{
    std::vector<uint8_t> oldf( (size_t) vold_field_undeprecate::LineageFixedMeasure( 1 ) );
    {
        vold_field_undeprecate::Lineage v;
        vold_field_undeprecate::LineageReset( v );
        v.a = 1; v.b = 2; v.c = 3;
        check( vold_field_undeprecate::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) ==
               (int64_t) oldf.size(), "field_undeprecate: the OLD file saves" );
    }
    std::vector<uint8_t> newf( (size_t) vnew_field_undeprecate::LineageFixedMeasure( 1 ) );
    {
        vnew_field_undeprecate::Lineage v;
        vnew_field_undeprecate::LineageReset( v );
        v.a = 4; v.b = 5; v.c = 6;
        check( vnew_field_undeprecate::LineageFixedSave( &v, 1, newf.data(), (int64_t) newf.size() ) ==
               (int64_t) newf.size(), "field_undeprecate: the NEW file saves" );
    }

    {
        vnew_field_undeprecate::Lineage back;
        vnew_field_undeprecate::LineageReset( back );
        vnew_field_undeprecate::TableReport r;
        std::vector<vnew_field_undeprecate::TableFixedEntry> plan( 1024 );
        const int64_t n = vnew_field_undeprecate::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                                   plan.data(), 1024, NULL, &r );
        check( n == 1, "field_undeprecate: NEW-READS-OLD — one record" );
        check( back.a == 1 && back.b == 2 && back.c == 3,
               "field_undeprecate: NEW-READS-OLD — a and b and c land exactly, the undeprecated field included" );
        check( !r.refused && !r.malformed, "field_undeprecate: NEW-READS-OLD — no refusal and no damage" );
    }
    {
        vold_field_undeprecate::Lineage back;
        vold_field_undeprecate::LineageReset( back );
        vold_field_undeprecate::TableReport r;
        std::vector<vold_field_undeprecate::TableFixedEntry> plan( 1024 );
        const int64_t n = vold_field_undeprecate::LineageFixedLoad( &back, 1, newf.data(), (int64_t) newf.size(),
                                                                   plan.data(), 1024, NULL, &r );
        check( n == 1, "field_undeprecate: OLD-READS-NEW — undeprecating is not newer, so the old reader reads" );
        check( back.a == 4 && back.c == 6, "field_undeprecate: OLD-READS-NEW — the live fields land exactly" );
        check( !r.refused && !r.malformed, "field_undeprecate: OLD-READS-NEW — no refusal and no damage" );
    }
}

// ---------------------------------------------------------------------------
// enum_append: Tier {Bronze,Silver,Gold} -> + Platinum
// (old_enum_append.bin / new_enum_append.bin)

static void enum_append_case()
{
    std::vector<uint8_t> oldf( (size_t) vold_enum_append::LineageFixedMeasure( 1 ) );
    {
        vold_enum_append::Lineage v;
        vold_enum_append::LineageReset( v );
        v.tier = vold_enum_append::Tier::Gold;
        v.seq = 9;
        check( vold_enum_append::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) ==
               (int64_t) oldf.size(), "enum_append: the OLD file saves" );
    }
    // TWO RECORDS, AND THE FIRST HOLDS A VALUE THE OLD READER COULD NAME. The
    // refusal is a property of the LAYOUT, never of a record's values: `Gold`
    // leading the file is what makes that the assertion.
    std::vector<uint8_t> newf( (size_t) vnew_enum_append::LineageFixedMeasure( 2 ) );
    {
        vnew_enum_append::Lineage v[2];
        vnew_enum_append::LineageReset( v[0] );
        v[0].tier = vnew_enum_append::Tier::Gold; v[0].seq = 10;
        vnew_enum_append::LineageReset( v[1] );
        v[1].tier = vnew_enum_append::Tier::Platinum; v[1].seq = 11;
        check( vnew_enum_append::LineageFixedSave( v, 2, newf.data(), (int64_t) newf.size() ) ==
               (int64_t) newf.size(), "enum_append: the NEW file saves" );
    }

    {
        vnew_enum_append::Lineage back;
        vnew_enum_append::LineageReset( back );
        vnew_enum_append::TableReport r;
        std::vector<vnew_enum_append::TableFixedEntry> plan( 1024 );
        const int64_t n = vnew_enum_append::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                             plan.data(), 1024, NULL, &r );
        check( n == 1, "enum_append: NEW-READS-OLD — one record" );
        check( back.tier == vnew_enum_append::Tier::Gold,
               "enum_append: NEW-READS-OLD — a gold record reads gold: append-only means the writer's "
               "ordinal IS the reader's" );
        check( (int) vold_enum_append::Tier::Gold == (int) vnew_enum_append::Tier::Gold,
               "enum_append: the ordinals are equal across the pair, which is what appending buys" );
        check( back.seq == 9, "enum_append: NEW-READS-OLD — the scalar after the enum lands exactly" );
        reads_clean( "enum_append", r, 0, 0 );
    }

    {
        vold_enum_append::Lineage back[2];
        vold_enum_append::LineageReset( back[0] );
        vold_enum_append::LineageReset( back[1] );
        vold_enum_append::TableReport r;
        std::vector<vold_enum_append::TableFixedEntry> plan( 1024 );
        const int64_t n = vold_enum_append::LineageFixedLoad( back, 2, newf.data(), (int64_t) newf.size(),
                                                             plan.data(), 1024, NULL, &r );
        refuses_newer( "enum_append/OLD-REFUSES-NEW", "the variant `Platinum`", n, r,
                       VL_OWN_REASON( vold_enum_append, r ), vnew_enum_append::LineageFixedHash );
        check_red( back[0].seq == 0 && back[1].seq == 0, "enum_append/OLD-REFUSES-NEW",
                   "enum_append/OLD-REFUSES-NEW: nothing was decoded, not even the record whose variant "
                   "this reader could have named" );
    }
}

// ---------------------------------------------------------------------------
// enum_width: 255 variants -> 256, so the ordinal's width goes 1 -> 2
// (old_enum_width.bin / new_enum_width.bin)
//
// The boundary is StorageBitsFor (ir/ir.go): with None = 0 implicit and the
// variants dense from 1, 255 variants have extent 255 and ride in one byte, and
// the 256th takes the extent to 256 and the ordinal to two. One appended
// variant moves every byte after it.

static void enum_width_case()
{
    check( (int) sizeof( vold_enum_width::Wide ) == 1,
           "enum_width: the OLD side's ordinal is ONE byte (255 variants)" );
    check( (int) sizeof( vnew_enum_width::Wide ) == 2,
           "enum_width: the NEW side's ordinal is TWO bytes (256 variants)" );

    std::vector<uint8_t> oldf( (size_t) vold_enum_width::LineageFixedMeasure( 1 ) );
    {
        vold_enum_width::Lineage v;
        vold_enum_width::LineageReset( v );
        v.tier = vold_enum_width::Wide::V200;
        v.seq = 12;
        check( vold_enum_width::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) ==
               (int64_t) oldf.size(), "enum_width: the OLD file saves" );
    }
    std::vector<uint8_t> newf( (size_t) vnew_enum_width::LineageFixedMeasure( 2 ) );
    {
        vnew_enum_width::Lineage v[2];
        vnew_enum_width::LineageReset( v[0] );
        v[0].tier = vnew_enum_width::Wide::V200; v[0].seq = 13;
        vnew_enum_width::LineageReset( v[1] );
        v[1].tier = vnew_enum_width::Wide::V256; v[1].seq = 14; // the ordinal a byte cannot hold
        check( vnew_enum_width::LineageFixedSave( v, 2, newf.data(), (int64_t) newf.size() ) ==
               (int64_t) newf.size(), "enum_width: the NEW file saves" );
    }

    {
        vnew_enum_width::Lineage back;
        vnew_enum_width::LineageReset( back );
        vnew_enum_width::TableReport r;
        std::vector<vnew_enum_width::TableFixedEntry> plan( 4096 );
        const int64_t n = vnew_enum_width::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                            plan.data(), 4096, NULL, &r );
        check( n == 1, "enum_width: NEW-READS-OLD — one record" );
        check( back.tier == vnew_enum_width::Wide::V200,
               "enum_width: NEW-READS-OLD — a width-1 ordinal widened into width 2, exactly" );
        check( back.seq == 12, "enum_width: NEW-READS-OLD — the scalar behind the widened ordinal lands exactly" );
        // §5.2: "wider in y: widen (COUNT widened)", and an enum's ordinal is
        // the row this is written for. The counter is the red half of this
        // column; the value above is the green half.
        check( r.widened == 1, "enum_width/NEW-READS-OLD: a widened ordinal COUNTS widened (§5.2)" );
        check( r.unknown == 0 && r.clamped == 0 && r.kind_mismatch == 0 && !r.malformed && !r.refused,
               "enum_width: NEW-READS-OLD — nothing else fired" );
    }

    {
        vold_enum_width::Lineage back[2];
        vold_enum_width::LineageReset( back[0] );
        vold_enum_width::LineageReset( back[1] );
        vold_enum_width::TableReport r;
        std::vector<vold_enum_width::TableFixedEntry> plan( 4096 );
        const int64_t n = vold_enum_width::LineageFixedLoad( back, 2, newf.data(), (int64_t) newf.size(),
                                                            plan.data(), 4096, NULL, &r );
        refuses_newer( "enum_width/OLD-REFUSES-NEW", "the layout's enum ordinal width (1 -> 2)", n, r,
                       VL_OWN_REASON( vold_enum_width, r ), vnew_enum_width::LineageFixedHash );
        check_red( back[0].seq == 0 && back[1].seq == 0, "enum_width/OLD-REFUSES-NEW",
                   "enum_width/OLD-REFUSES-NEW: nothing was decoded" );
    }
}

// ---------------------------------------------------------------------------
// union_append: Pick {alpha,beta} -> + gamma
// (old_union_append.bin / new_union_append.bin)

static void union_append_case()
{
    std::vector<uint8_t> oldf( (size_t) vold_union_append::LineageFixedMeasure( 1 ) );
    {
        vold_union_append::Lineage v;
        vold_union_append::LineageReset( v );
        v.pick.type = vold_union_append::PickType::Alpha;
        v.pick.alpha.m = 7;
        v.seq = 15;
        check( vold_union_append::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) ==
               (int64_t) oldf.size(), "union_append: the OLD file saves" );
    }
    std::vector<uint8_t> newf( (size_t) vnew_union_append::LineageFixedMeasure( 2 ) );
    {
        vnew_union_append::Lineage v[2];
        vnew_union_append::LineageReset( v[0] );
        v[0].pick.type = vnew_union_append::PickType::Alpha;
        v[0].pick.alpha.m = 8;
        v[0].seq = 16;
        vnew_union_append::LineageReset( v[1] );
        v[1].pick.type = vnew_union_append::PickType::Gamma;
        v[1].pick.gamma.p = 9;
        v[1].seq = 17;
        check( vnew_union_append::LineageFixedSave( v, 2, newf.data(), (int64_t) newf.size() ) ==
               (int64_t) newf.size(), "union_append: the NEW file saves" );
    }

    {
        vnew_union_append::Lineage back;
        vnew_union_append::LineageReset( back );
        vnew_union_append::TableReport r;
        std::vector<vnew_union_append::TableFixedEntry> plan( 1024 );
        const int64_t n = vnew_union_append::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                              plan.data(), 1024, NULL, &r );
        check( n == 1, "union_append: NEW-READS-OLD — one record" );
        check( back.pick.type == vnew_union_append::PickType::Alpha,
               "union_append: NEW-READS-OLD — an `alpha` record lands alpha" );
        check( back.pick.alpha.m == 7, "union_append: NEW-READS-OLD — the arm's payload lands exactly" );
        check( (int) vold_union_append::PickType::Beta == (int) vnew_union_append::PickType::Beta,
               "union_append: the arms' tags are equal across the pair — append-only keeps every ordinal" );
        check( back.seq == 15, "union_append: NEW-READS-OLD — the scalar behind the union lands exactly" );
        reads_clean( "union_append", r, 0, 0 );
    }

    {
        vold_union_append::Lineage back[2];
        vold_union_append::LineageReset( back[0] );
        vold_union_append::LineageReset( back[1] );
        vold_union_append::TableReport r;
        std::vector<vold_union_append::TableFixedEntry> plan( 1024 );
        const int64_t n = vold_union_append::LineageFixedLoad( back, 2, newf.data(), (int64_t) newf.size(),
                                                              plan.data(), 1024, NULL, &r );
        refuses_newer( "union_append/OLD-REFUSES-NEW", "the arm `gamma`", n, r,
                       VL_OWN_REASON( vold_union_append, r ), vnew_union_append::LineageFixedHash );
        check_red( back[0].seq == 0 && back[1].seq == 0 &&
                   back[0].pick.type == vold_union_append::PickType::None,
                   "union_append/OLD-REFUSES-NEW",
                   "union_append/OLD-REFUSES-NEW: nothing was decoded, tag included" );
    }
}

// ---------------------------------------------------------------------------
// union_arm_payload_widen: arm alpha {x} -> {x,y}
// (old_union_arm_payload_widen.bin / new_union_arm_payload_widen.bin)

static void union_arm_payload_widen_case()
{
    namespace vo = vold_union_arm_payload_widen;
    namespace vn = vnew_union_arm_payload_widen;

    std::vector<uint8_t> oldf( (size_t) vo::LineageFixedMeasure( 1 ) );
    {
        vo::Lineage v;
        vo::LineageReset( v );
        v.pick.type = vo::PickType::Alpha;
        v.pick.alpha.x = 21;
        v.seq = 18;
        check( vo::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) == (int64_t) oldf.size(),
               "union_arm_payload_widen: the OLD file saves" );
    }
    std::vector<uint8_t> newf( (size_t) vn::LineageFixedMeasure( 1 ) );
    {
        vn::Lineage v;
        vn::LineageReset( v );
        v.pick.type = vn::PickType::Alpha;
        v.pick.alpha.x = 22;
        v.pick.alpha.y = 23;
        v.seq = 19;
        check( vn::LineageFixedSave( &v, 1, newf.data(), (int64_t) newf.size() ) == (int64_t) newf.size(),
               "union_arm_payload_widen: the NEW file saves" );
    }

    {
        vn::Lineage back;
        vn::LineageReset( back );
        vn::TableReport r;
        std::vector<vn::TableFixedEntry> plan( 1024 );
        const int64_t n = vn::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                plan.data(), 1024, NULL, &r );
        check( n == 1, "union_arm_payload_widen: NEW-READS-OLD — one record" );
        check( back.pick.type == vn::PickType::Alpha,
               "union_arm_payload_widen: NEW-READS-OLD — the arm lands" );
        check( back.pick.alpha.x == 21, "union_arm_payload_widen: NEW-READS-OLD — `x` lands exactly" );
        char what[256];
        std::snprintf( what, sizeof( what ),
                       "union_arm_payload_widen/NEW-READS-OLD: the appended `y` under arm `alpha` takes "
                       "its declared default 55 — it landed %d", (int) back.pick.alpha.y );
        check( back.pick.alpha.y == 55, what );
        check( back.seq == 18, "union_arm_payload_widen: NEW-READS-OLD — the scalar behind the union lands" );
        reads_clean( "union_arm_payload_widen", r, 0, 0 );
    }

    {
        vo::Lineage back;
        vo::LineageReset( back );
        vo::TableReport r;
        std::vector<vo::TableFixedEntry> plan( 1024 );
        const int64_t n = vo::LineageFixedLoad( &back, 1, newf.data(), (int64_t) newf.size(),
                                                plan.data(), 1024, NULL, &r );
        refuses_newer( "union_arm_payload_widen/OLD-REFUSES-NEW", "the field `y` under arm `alpha`", n, r,
                       VL_OWN_REASON( vo, r ), vn::LineageFixedHash );
        check_red( back.seq == 0 && back.pick.type == vo::PickType::None,
                   "union_arm_payload_widen/OLD-REFUSES-NEW",
                   "union_arm_payload_widen/OLD-REFUSES-NEW: nothing was decoded" );
    }
}

// ---------------------------------------------------------------------------
// flags_append: Caps {Jump,Crouch} -> + Fly
// (old_flags_append.bin / new_flags_append.bin)
//
// THE ROW THAT SAYS THE LAYOUT IS MISSING SOMETHING. A flags field rides as an
// eight-byte mask and its layout entry carries children = 0 (ir/fixedform.go),
// so appending a flag moves NO LAYOUT BYTE: the two hashes are equal, the
// reader takes the identity plan, and no version check has anything to read.
// The case asserts the LAW anyway — that is what tells whoever lands
// `layout_newer` that the layout entry owes a flag count first.

static void flags_append_case()
{
    check( vold_flags_append::LineageFixedHash != vnew_flags_append::LineageFixedHash,
           "flags_append: the definitions digest covers a flags bit count, so the hash MOVES (§13)" );

    std::vector<uint8_t> oldf( (size_t) vold_flags_append::LineageFixedMeasure( 1 ) );
    {
        vold_flags_append::Lineage v;
        vold_flags_append::LineageReset( v );
        v.caps = vold_flags_append::Caps_Jump | vold_flags_append::Caps_Crouch;
        v.seq = 20;
        check( vold_flags_append::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) ==
               (int64_t) oldf.size(), "flags_append: the OLD file saves" );
    }
    std::vector<uint8_t> newf( (size_t) vnew_flags_append::LineageFixedMeasure( 1 ) );
    {
        vnew_flags_append::Lineage v;
        vnew_flags_append::LineageReset( v );
        v.caps = vnew_flags_append::Caps_Jump | vnew_flags_append::Caps_Fly;
        v.seq = 21;
        check( vnew_flags_append::LineageFixedSave( &v, 1, newf.data(), (int64_t) newf.size() ) ==
               (int64_t) newf.size(), "flags_append: the NEW file saves" );
    }

    {
        vnew_flags_append::Lineage back;
        vnew_flags_append::LineageReset( back );
        vnew_flags_append::TableReport r;
        std::vector<vnew_flags_append::TableFixedEntry> plan( 1024 );
        const int64_t n = vnew_flags_append::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                              plan.data(), 1024, NULL, &r );
        check( n == 1, "flags_append: NEW-READS-OLD — one record" );
        check( back.caps == ( vnew_flags_append::Caps_Jump | vnew_flags_append::Caps_Crouch ),
               "flags_append: NEW-READS-OLD — the mask lands equal, bit for bit" );
        check( back.seq == 20, "flags_append: NEW-READS-OLD — the scalar behind the mask lands exactly" );
        reads_clean( "flags_append", r, 0, 0 );
    }

    {
        vold_flags_append::Lineage back;
        vold_flags_append::LineageReset( back );
        vold_flags_append::TableReport r;
        std::vector<vold_flags_append::TableFixedEntry> plan( 1024 );
        const int64_t n = vold_flags_append::LineageFixedLoad( &back, 1, newf.data(), (int64_t) newf.size(),
                                                              plan.data(), 1024, NULL, &r );
        refuses_newer( "flags_append/OLD-REFUSES-NEW", "the flag `Fly`", n, r,
                       VL_OWN_REASON( vold_flags_append, r ), vnew_flags_append::LineageFixedHash );
        check_red( back.caps == 0 && back.seq == 0, "flags_append/OLD-REFUSES-NEW",
                   "flags_append/OLD-REFUSES-NEW: nothing was decoded" );
    }
}

// ---------------------------------------------------------------------------
// keyed_array_enum_append: [Tier]int32 with Tier gaining Platinum
// (old_keyed_array_enum_append.bin / new_keyed_array_enum_append.bin)

static void keyed_array_enum_append_case()
{
    namespace vo = vold_keyed_array_enum_append;
    namespace vn = vnew_keyed_array_enum_append;

    std::vector<uint8_t> oldf( (size_t) vo::LineageFixedMeasure( 1 ) );
    {
        vo::Lineage v;
        vo::LineageReset( v );
        v.slots[vo::Tier::Bronze] = 101;
        v.slots[vo::Tier::Silver] = 102;
        v.slots[vo::Tier::Gold] = 103;
        v.seq = 22;
        check( vo::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) == (int64_t) oldf.size(),
               "keyed_array_enum_append: the OLD file saves" );
    }
    std::vector<uint8_t> newf( (size_t) vn::LineageFixedMeasure( 1 ) );
    {
        vn::Lineage v;
        vn::LineageReset( v );
        v.slots[vn::Tier::Bronze] = 201;
        v.slots[vn::Tier::Silver] = 202;
        v.slots[vn::Tier::Gold] = 203;
        v.slots[vn::Tier::Platinum] = 204;
        v.seq = 23;
        check( vn::LineageFixedSave( &v, 1, newf.data(), (int64_t) newf.size() ) == (int64_t) newf.size(),
               "keyed_array_enum_append: the NEW file saves" );
    }

    {
        vn::Lineage back;
        vn::LineageReset( back );
        vn::TableReport r;
        std::vector<vn::TableFixedEntry> plan( 1024 );
        const int64_t n = vn::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                plan.data(), 1024, NULL, &r );
        check( n == 1, "keyed_array_enum_append: NEW-READS-OLD — one record" );
        check( back.slots[vn::Tier::Bronze] == 101 && back.slots[vn::Tier::Silver] == 102 &&
               back.slots[vn::Tier::Gold] == 103,
               "keyed_array_enum_append: NEW-READS-OLD — the three slots the writer had land exactly" );
        check( back.slots[vn::Tier::Platinum] == 0,
               "keyed_array_enum_append: NEW-READS-OLD — the slot the appended key opened takes the "
               "element's default" );
        check( back.seq == 22, "keyed_array_enum_append: NEW-READS-OLD — the scalar behind the array lands" );
        reads_clean( "keyed_array_enum_append", r, 0, 0 );
    }

    {
        vo::Lineage back;
        vo::LineageReset( back );
        vo::TableReport r;
        std::vector<vo::TableFixedEntry> plan( 1024 );
        const int64_t n = vo::LineageFixedLoad( &back, 1, newf.data(), (int64_t) newf.size(),
                                                plan.data(), 1024, NULL, &r );
        refuses_newer( "keyed_array_enum_append/OLD-REFUSES-NEW",
                       "the variant `Platinum` of the key enum `Tier`", n, r, VL_OWN_REASON( vo, r ),
                       vn::LineageFixedHash );
        check_red( back.slots[vo::Tier::Bronze] == 0 && back.seq == 0,
                   "keyed_array_enum_append/OLD-REFUSES-NEW",
                   "keyed_array_enum_append/OLD-REFUSES-NEW: nothing was decoded, no slot touched" );
    }
}

// ---------------------------------------------------------------------------
// nested_append: Vec {x,y,z} -> {x,y,z,w}, inside Lineage
// (old_nested_append.bin / new_nested_append.bin)

static void nested_append_case()
{
    std::vector<uint8_t> oldf( (size_t) vold_nested_append::LineageFixedMeasure( 1 ) );
    {
        vold_nested_append::Lineage v;
        vold_nested_append::LineageReset( v );
        v.v.x = 1; v.v.y = 2; v.v.z = 3; v.seq = 24;
        check( vold_nested_append::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) ==
               (int64_t) oldf.size(), "nested_append: the OLD file saves" );
    }
    std::vector<uint8_t> newf( (size_t) vnew_nested_append::LineageFixedMeasure( 1 ) );
    {
        vnew_nested_append::Lineage v;
        vnew_nested_append::LineageReset( v );
        v.v.x = 4; v.v.y = 5; v.v.z = 6; v.v.w = 7; v.seq = 25;
        check( vnew_nested_append::LineageFixedSave( &v, 1, newf.data(), (int64_t) newf.size() ) ==
               (int64_t) newf.size(), "nested_append: the NEW file saves" );
    }

    {
        vnew_nested_append::Lineage back;
        vnew_nested_append::LineageReset( back );
        vnew_nested_append::TableReport r;
        std::vector<vnew_nested_append::TableFixedEntry> plan( 1024 );
        const int64_t n = vnew_nested_append::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                               plan.data(), 1024, NULL, &r );
        check( n == 1, "nested_append: NEW-READS-OLD — one record" );
        check( back.v.x == 1 && back.v.y == 2 && back.v.z == 3,
               "nested_append: NEW-READS-OLD — the nested table's old fields land exactly" );
        check( back.v.w == 88,
               "nested_append: NEW-READS-OLD — `Vec.w`, appended inside the nested table, takes its default" );
        check( back.seq == 24,
               "nested_append: NEW-READS-OLD — the field AFTER the grown nested table did not slide" );
        reads_clean( "nested_append", r, 0, 0 );
    }

    {
        vold_nested_append::Lineage back;
        vold_nested_append::LineageReset( back );
        vold_nested_append::TableReport r;
        std::vector<vold_nested_append::TableFixedEntry> plan( 1024 );
        const int64_t n = vold_nested_append::LineageFixedLoad( &back, 1, newf.data(), (int64_t) newf.size(),
                                                               plan.data(), 1024, NULL, &r );
        refuses_newer( "nested_append/OLD-REFUSES-NEW", "the field `Vec.w`", n, r,
                       VL_OWN_REASON( vold_nested_append, r ), vnew_nested_append::LineageFixedHash );
        check_red( back.v.x == 0 && back.v.y == 0 && back.v.z == 0 && back.seq == 0,
                   "nested_append/OLD-REFUSES-NEW",
                   "nested_append/OLD-REFUSES-NEW: nothing was decoded, inside the nesting or outside it" );
    }
}

// ---------------------------------------------------------------------------
// rename_without_was: `a` -> `b was = "a"`
// (old_rename_without_was.bin / new_rename_without_was.bin)
//
// The row with NO refusal in it, and that is its content. Wire identity is the
// hash of the name and `was` keeps the old name's hash, so the rename moves no
// layout byte at all: the two layouts are byte-identical, both readers take the
// identity plan, and the pair reads in both directions. The compile-time half
// of this row — `b` ALONE, with no `was`, refused as "renamed without was" — is
// the lock's (internal/lockfile), not the reader's.

static void rename_without_was_case()
{
    check( vold_rename_without_was::LineageFixedHash == vnew_rename_without_was::LineageFixedHash,
           "rename_without_was: a rename THROUGH `was` moves no layout byte — the two hashes are equal, "
           "so the edit is not a version at all" );
    check( vold_rename_without_was::LineageFixedLayoutBytes ==
           vnew_rename_without_was::LineageFixedLayoutBytes &&
           std::memcmp( vold_rename_without_was::LineageFixedLayout,
                        vnew_rename_without_was::LineageFixedLayout,
                        (size_t) vold_rename_without_was::LineageFixedLayoutBytes ) == 0,
           "rename_without_was: and the layouts are byte-identical, not merely equal by hash" );

    std::vector<uint8_t> oldf( (size_t) vold_rename_without_was::LineageFixedMeasure( 1 ) );
    {
        vold_rename_without_was::Lineage v;
        vold_rename_without_was::LineageReset( v );
        v.a = 31; v.seq = 26;
        check( vold_rename_without_was::LineageFixedSave( &v, 1, oldf.data(), (int64_t) oldf.size() ) ==
               (int64_t) oldf.size(), "rename_without_was: the OLD file saves" );
    }
    std::vector<uint8_t> newf( (size_t) vnew_rename_without_was::LineageFixedMeasure( 1 ) );
    {
        vnew_rename_without_was::Lineage v;
        vnew_rename_without_was::LineageReset( v );
        v.b = 32; v.seq = 27;
        check( vnew_rename_without_was::LineageFixedSave( &v, 1, newf.data(), (int64_t) newf.size() ) ==
               (int64_t) newf.size(), "rename_without_was: the NEW file saves" );
    }

    {
        vnew_rename_without_was::Lineage back;
        vnew_rename_without_was::LineageReset( back );
        vnew_rename_without_was::TableReport r;
        std::vector<vnew_rename_without_was::TableFixedEntry> plan( 1024 );
        const int64_t n = vnew_rename_without_was::LineageFixedLoad( &back, 1, oldf.data(), (int64_t) oldf.size(),
                                                                    plan.data(), 1024, NULL, &r );
        check( n == 1, "rename_without_was: NEW-READS-OLD — one record" );
        check( back.b == 31, "rename_without_was: NEW-READS-OLD — `b` reads `a`'s bytes under `was`" );
        check( back.seq == 26, "rename_without_was: NEW-READS-OLD — the scalar beside it lands exactly" );
        reads_clean( "rename_without_was", r, 0, 0 );
    }
    {
        vold_rename_without_was::Lineage back;
        vold_rename_without_was::LineageReset( back );
        vold_rename_without_was::TableReport r;
        std::vector<vold_rename_without_was::TableFixedEntry> plan( 1024 );
        const int64_t n = vold_rename_without_was::LineageFixedLoad( &back, 1, newf.data(), (int64_t) newf.size(),
                                                                    plan.data(), 1024, NULL, &r );
        check( n == 1, "rename_without_was: OLD-READS-NEW — a rename through `was` is not newer" );
        check( back.a == 32, "rename_without_was: OLD-READS-NEW — `a` reads `b`'s bytes" );
        check( !r.refused && !r.malformed, "rename_without_was: OLD-READS-NEW — no refusal and no damage" );
    }
}

// ---------------------------------------------------------------------------
// THE ONE REGISTRATION CALL. fixedform_main.cpp calls this once and adds the
// count to its own, so this file owns its cases and its known-red list and the
// main file owns one line.

int versioning_lists_cases();

int versioning_lists_cases()
{
    field_append_case();
    field_deprecate_case();
    field_undeprecate_case();
    enum_append_case();
    enum_width_case();
    union_append_case();
    union_arm_payload_widen_case();
    flags_append_case();
    keyed_array_enum_append_case();
    nested_append_case();
    rename_without_was_case();
    failures += known_red_report();
    if ( failures == 0 ) { std::printf( "versioning lists: the LIST rows' read columns green\n" ); }
    return failures;
}
