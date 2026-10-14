// test/conformance/cpp/rows/R16.cpp — the cpp leg's assertion of the cpp/R16
// cell: §5.4's counters exactly (docs/FIXED-FORM-ALGORITHM.md:939-947).
//
//   | where | counter | when |
//   |---|---|---|
//   | MATCH, at COMPILE | unknown | once per writer field no reader field names,
//   |                   |         | ONCE PER PEER AND NEVER PER RECORD |
//   | widen, widenf | widened | once per entry per record; a folded element run
//   |                |         | is ONE and an unfolded one is min(their_n, my_n),
//   |                |         | and a widened run is never folded (§5.9 #33) |
//   | count, text | clamped | once per entry per record, when the length or count
//   |             |         | was out of range |
//   | the bounds pass | clamped | a FORGED ordinal remapped to None, on the
//   |                 |         | COMPILED plan exactly as on the identity one |
//   | copy, const, present, ordinal | none | the op lands its value and moves
//   |                               | nothing; the remap is the BOUNDS PASS's |
//
// THE ROW IS THE ONLY FILE THIS CARD TOUCHES. It is standalone and is not wired
// into the leg's runner (wiring rows/ into a leg's runner is its own card), so
// the RUN command names the include paths the units need:
//
//   c++ -std=c++17 -Wall -Ibuild/tables-generated/vnum
//       -Ibuild/tables-generated/vold_enum_width -Ibuild/tables-generated/vnew_enum_width
//       -Ibuild/tables-generated/vold_enum_append -Ibuild/tables-generated/vnew_enum_append
//       -Ibuild/tables-generated/vold_field_append -Ibuild/tables-generated/vnew_field_append
//       -Ibuild/tables-generated/vold_flags_append -Ibuild/tables-generated/vnew_flags_append
//       -Ibuild/tables-generated/vold_union_append -Ibuild/tables-generated/vnew_union_append
//       test/conformance/cpp/rows/R16.cpp -o build/rows-cpp-R16 && ./build/rows-cpp-R16
//
// THE VECTOR IS THE GENERATED WRITER'S OWN OUTPUT, which is the production path
// and not a hand-derivation: <Old>FixedSave lays down the OLD layout's form-3
// file, <New>FixedLoad reads it through the COMPILED plan (the file's header
// hash is the OLD layout's, so LOAD finds it in the lineage), and <Old>FixedLoad
// reads its own file through the IDENTITY plan. A single byte is then forged
// where a clause is about a value out of range. Nothing here reads
// build/fixedform-corpus; the file is self-contained.
//
// WHY THIS FILE EXISTS AT ALL: the roadmap's cpp/R16 stayed :unknown because
// the merged row PRs (cpptable's unknown_census, forged_ordinal_both_plans and
// count_clamp_ends) prove parts of §5.4, but the reference fixture
// test/tables/versioning_numbers.cpp asserts `r.widened > 0` for the element
// run and `r.clamped >= 1` for a clamp, and no landed test asserts the EXACT
// min(their_n, my_n) the fold rule owes, the text lane of clamped, or the
// census over MORE THAN ONE RECORD. Each assertion below is the exact number.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <new>

#include "VOLD_unknown_censusTable.h"
#include "VNEW_unknown_censusTable.h"
#include "VOLD_int_widenTable.h"
#include "VNEW_int_widenTable.h"
#include "VOLD_array_elem_widenTable.h"
#include "VNEW_array_elem_widenTable.h"
#include "VOLD_enum_widthTable.h"
#include "VNEW_enum_widthTable.h"
#include "VOLD_array_bounded_growTable.h"
#include "VNEW_array_bounded_growTable.h"
#include "VOLD_string_growTable.h"
#include "VNEW_string_growTable.h"
#include "VOLD_enum_appendTable.h"
#include "VNEW_enum_appendTable.h"
#include "VOLD_field_appendTable.h"
#include "VNEW_field_appendTable.h"
#include "VOLD_flags_appendTable.h"
#include "VNEW_flags_appendTable.h"
#include "VOLD_union_appendTable.h"
#include "VNEW_union_appendTable.h"
#include "VOLD_optional_addTable.h"
#include "VNEW_optional_addTable.h"

// ---- the assertion printer, one line per assertion --------------------------

static int g_failures = 0;

static void check( bool ok, const char * what )
{
    if ( ok ) { std::printf( "ok   %s\n", what ); }
    else      { std::printf( "FAIL %s\n", what ); ++g_failures; }
}

// the byte lanes the forge writes, independent of any namespace's spelling
static void put32( uint8_t * at, uint32_t v )
{
    at[0] = (uint8_t) v; at[1] = (uint8_t) ( v >> 8 );
    at[2] = (uint8_t) ( v >> 16 ); at[3] = (uint8_t) ( v >> 24 );
}

// EVERY needle this file forges is one field's lawful writer bytes; a needle
// that occurs twice where one record is written is a broken locator and says so.
static int64_t find_once( const uint8_t * data, int64_t len, const uint8_t * needle, int64_t n )
{
    int64_t found = -1;
    int hits = 0;
    for ( int64_t i = 0; i + n <= len; ++i )
    {
        if ( std::memcmp( data + i, needle, (size_t) n ) == 0 ) { found = i; ++hits; }
    }
    if ( hits != 1 ) { return -1; }
    return found;
}

// ---- unknown: once per PEER at COMPILE, never per record --------------------

static void unknown_census_once_per_peer( void )
{
    static uint8_t buf[1 << 16];
    // THREE records, each FOUR Items, each carrying the dropped `drop`: the
    // per-record divergence would read 3 and the per-element one 12.
    vold_unknown_census::Census old[3];
    for ( int rec = 0; rec < 3; ++rec )
    {
        old[rec].lead = 1; old[rec].trail = 2;
        for ( int k = 0; k < 4; ++k )
        {
            old[rec].items[k].a = 10 * rec + k;
            old[rec].items[k].drop = 100 + k;
        }
    }
    int64_t len = vold_unknown_census::CensusFixedSave( old, 3, buf, (int64_t) sizeof( buf ) );
    check( len > 0, "unknown_census: the OLD writer wrote three records" );

    vnew_unknown_census::Census back[3];
    vnew_unknown_census::TableReport r{};
    static vnew_unknown_census::TableFixedEntry plan[4096];
    int64_t n = vnew_unknown_census::CensusFixedLoad( back, 3, buf, len, plan, 4096, NULL, &r );

    check( n == 3, "unknown_census: the NEW reader returned all three records" );
    check( r.unknown == 1,
           "unknown is once per PEER at COMPILE (== 1, never 3 per record, never 12 per element)" );
    check( r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0,
           "unknown_census: no other counter moved on the counted peer" );
    check( !r.refused && !r.malformed, "unknown_census: a counted read is not a refusal" );

    bool landed = true;
    for ( int rec = 0; rec < 3 && landed; ++rec )
    {
        if ( back[rec].lead != 1 || back[rec].trail != 2 ) { landed = false; break; }
        for ( int k = 0; k < 4; ++k )
        {
            if ( back[rec].items[k].a != 10 * rec + k ) { landed = false; break; }
        }
    }
    check( landed, "unknown_census: every named field landed exactly" );
}

// ---- widened: once per entry per record, and never folded across a widen ----

static void widened_scalar_per_record( void )
{
    static uint8_t buf[1 << 16];
    vold_int_widen::IntWiden old[3];
    old[0].v = -1; old[1].v = -32768; old[2].v = 32767;
    int64_t len = vold_int_widen::IntWidenFixedSave( old, 3, buf, (int64_t) sizeof( buf ) );
    check( len > 0, "int_widen: the OLD writer wrote three records" );

    vnew_int_widen::IntWiden back[3];
    vnew_int_widen::TableReport r{};
    static vnew_int_widen::TableFixedEntry plan[4096];
    int64_t n = vnew_int_widen::IntWidenFixedLoad( back, 3, buf, len, plan, 4096, NULL, &r );

    check( n == 3, "int_widen: the NEW reader returned all three records" );
    check( r.widened == 3,
           "widened is once per ENTRY per RECORD (== 3 for three widened scalars, not 1)" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0,
           "int_widen: only widened moved" );
    check( back[0].v == -1 && back[1].v == -32768 && back[2].v == 32767,
           "int_widen: the sign-extended values landed exactly" );
}

static void widened_element_run_is_min( void )
{
    static uint8_t buf[1 << 16];
    // [..4]int16 into [..4]int32: FOUR elements sent, so a FOLDED read would
    // land widened == 1 and this clause exists to catch it.
    vold_array_elem_widen::ArrayElemWiden old[1];
    old[0].lead = 0xAAAAAAAAu; old[0].vals_count = 4;
    old[0].vals[0] = -1; old[0].vals[1] = -32768; old[0].vals[2] = 32767; old[0].vals[3] = 0;
    old[0].trail = 0xBBBBBBBBu;
    int64_t len = vold_array_elem_widen::ArrayElemWidenFixedSave( old, 1, buf, (int64_t) sizeof( buf ) );
    check( len > 0, "array_elem_widen: the OLD writer wrote its record" );

    vnew_array_elem_widen::ArrayElemWiden back[1];
    vnew_array_elem_widen::TableReport r{};
    static vnew_array_elem_widen::TableFixedEntry plan[4096];
    int64_t n = vnew_array_elem_widen::ArrayElemWidenFixedLoad( back, 1, buf, len, plan, 4096, NULL, &r );

    check( n == 1, "array_elem_widen: the NEW reader returned the record" );
    check( r.widened == 4,
           "a widened element run is UNFOLDED: widened == min(their_n, my_n) == 4 (a fold would read 1)" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0,
           "array_elem_widen: only widened moved" );
    check( back[0].lead == 0xAAAAAAAAu && back[0].trail == 0xBBBBBBBBu && back[0].vals_count == 4,
           "array_elem_widen: the count and the brackets did not move" );
    check( back[0].vals[0] == -1 && back[0].vals[1] == -32768 && back[0].vals[2] == 32767,
           "array_elem_widen: each element widened element by element, sign and all" );
}

static void widened_enum_ordinal_width( void )
{
    static uint8_t buf[1 << 16];
    // 255 variants (ordinal width 1) into 256 (width 2): a grown enum ordinal.
    vold_enum_width::Lineage old[1];
    old[0].tier = vold_enum_width::Wide::V255; old[0].seq = 12;
    int64_t len = vold_enum_width::LineageFixedSave( old, 1, buf, (int64_t) sizeof( buf ) );
    check( len > 0, "enum_width: the OLD writer wrote its record" );

    vnew_enum_width::Lineage back[1];
    vnew_enum_width::TableReport r{};
    static vnew_enum_width::TableFixedEntry plan[4096];
    int64_t n = vnew_enum_width::LineageFixedLoad( back, 1, buf, len, plan, 4096, NULL, &r );

    check( n == 1, "enum_width: the NEW reader returned the record" );
    check( r.widened == 1, "a grown enum ordinal width counts widened once (== 1)" );
    check( (int) back[0].tier == 255 && back[0].seq == 12,
           "enum_width: the widened ordinal and the scalar behind it landed exactly" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0,
           "enum_width: only widened moved" );
}

// ---- clamped: once per entry per record, for count and for text -------------

static void clamped_count_per_record( void )
{
    static uint8_t buf[1 << 16];
    vold_array_bounded_grow::ArrayBoundedGrow old[2];
    for ( int rec = 0; rec < 2; ++rec )
    {
        old[rec].lead = 0xAAAAAAAAu; old[rec].vals_count = 4;
        for ( int k = 0; k < 4; ++k ) { old[rec].vals[k] = 1000 + k; }
        old[rec].trail = 0xBBBBBBBBu;
    }
    int64_t len = vold_array_bounded_grow::ArrayBoundedGrowFixedSave( old, 2, buf, (int64_t) sizeof( buf ) );
    check( len > 0, "array_bounded_grow: the OLD writer wrote two records" );

    // FORGE: the count word of EACH record from the writer's 4 to 7 — BETWEEN
    // the writer's [..4] and the reader's [..8], which is §5.8 row 4's bound.
    static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00 };
    int hits = 0;
    for ( int64_t i = 0; i + 8 <= len; ++i )
    {
        if ( std::memcmp( buf + i, needle, 8 ) == 0 ) { put32( buf + i + 4, 7 ); ++hits; }
    }
    check( hits == 2, "array_bounded_grow: the locator found exactly the two records' count words" );

    vnew_array_bounded_grow::ArrayBoundedGrow back[2];
    vnew_array_bounded_grow::TableReport r{};
    static vnew_array_bounded_grow::TableFixedEntry plan[4096];
    int64_t n = vnew_array_bounded_grow::ArrayBoundedGrowFixedLoad( back, 2, buf, len, plan, 4096, NULL, &r );

    check( n == 2, "array_bounded_grow: the NEW reader returned both records" );
    check( back[0].vals_count == 4 && back[1].vals_count == 4,
           "array_bounded_grow: the count clamps to the WRITER's 4, never the reader's 8 nor the forged 7" );
    check( r.clamped == 2, "clamped is once per ENTRY per RECORD (== 2 for two records, not 1)" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0,
           "array_bounded_grow: only clamped moved" );
    check( back[0].lead == 0xAAAAAAAAu && back[0].trail == 0xBBBBBBBBu,
           "array_bounded_grow: the brackets did not move" );
}

static void clamped_text_length( void )
{
    static uint8_t buf[1 << 16];
    vold_string_grow::StringGrow old[1];
    old[0].lead = 0xAAAAAAAAu;
    std::strcpy( old[0].text, "abcdefgh" ); // the OLD capacity, FULL
    old[0].text_length = 8;
    old[0].trail = 0xBBBBBBBBu;
    int64_t len = vold_string_grow::StringGrowFixedSave( old, 1, buf, (int64_t) sizeof( buf ) );
    check( len > 0, "string_grow: the OLD writer wrote its record" );

    // FORGE: the length word from the writer's 8 to 12 — past the WRITER's
    // string(8) but inside the READER's string(16), which is the lane only the
    // writer's bound catches.
    static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x08, 0x00, 0x00, 0x00 };
    int64_t found = find_once( buf, len, needle, 8 );
    check( found >= 0, "string_grow: the length-word locator matched exactly once" );
    if ( found >= 0 ) { put32( buf + found + 4, 12 ); }

    vnew_string_grow::StringGrow back[1];
    vnew_string_grow::TableReport r{};
    static vnew_string_grow::TableFixedEntry plan[4096];
    int64_t n = vnew_string_grow::StringGrowFixedLoad( back, 1, buf, len, plan, 4096, NULL, &r );

    check( n == 1, "string_grow: the NEW reader returned the record" );
    check( back[0].text_length == 8 && std::strcmp( back[0].text, "abcdefgh" ) == 0,
           "string_grow: the text length clamps to the WRITER's 8 and the bytes land" );
    check( r.clamped == 1, "text clamps once per entry per record (== 1, not >= 1)" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0,
           "string_grow: only clamped moved" );
}

// ---- the forged ordinal: the bounds pass, on BOTH plans ---------------------

static void forged_ordinal_both_plans( void )
{
    static uint8_t buf[1 << 16];
    vold_enum_append::Lineage old[1];
    old[0].tier = vold_enum_append::Tier::Gold; // ordinal 3, the OLD writer's top
    old[0].seq = 9;
    int64_t len = vold_enum_append::LineageFixedSave( old, 1, buf, (int64_t) sizeof( buf ) );
    check( len > 0, "enum_append: the OLD writer wrote its record" );

    // FORGE: tier from Gold (3) to 4 — one past the WRITER's own variant count.
    static const uint8_t needle[5] = { 0x03, 0x09, 0x00, 0x00, 0x00 };
    int64_t found = find_once( buf, len, needle, 5 );
    check( found >= 0, "enum_append: the (tier=Gold, seq=9) locator matched exactly once" );
    if ( found >= 0 ) { buf[found] = 4; }

    // THE COMPILED PLAN: the NEW build reads the forged OLD file through the
    // lineage, so the plan is compiled from the OLD (three-variant) layout.
    {
        vnew_enum_append::Lineage back[1];
        vnew_enum_append::TableReport r{};
        static vnew_enum_append::TableFixedEntry plan[4096];
        int64_t n = vnew_enum_append::LineageFixedLoad( back, 1, buf, len, plan, 4096, NULL, &r );
        check( n == 1, "forged ordinal, compiled plan: the record read" );
        check( back[0].tier == vnew_enum_append::Tier::None,
               "forged ordinal, compiled plan: one past the WRITER's top lands None" );
        check( back[0].seq == 9, "forged ordinal, compiled plan: the scalar behind it landed" );
        check( r.clamped == 1, "forged ordinal, compiled plan: clamped == 1 (the bounds pass, not the op too)" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.duplicate == 0,
               "forged ordinal, compiled plan: only clamped moved" );
    }

    // THE IDENTITY PLAN: the OLD build reads its own forged file on its own
    // hash, so the plan is the baked identity one.
    {
        vold_enum_append::Lineage back[1];
        vold_enum_append::TableReport r{};
        static vold_enum_append::TableFixedEntry plan[4096];
        int64_t n = vold_enum_append::LineageFixedLoad( back, 1, buf, len, plan, 4096, NULL, &r );
        check( n == 1, "forged ordinal, identity plan: the record read" );
        check( back[0].tier == vold_enum_append::Tier::None,
               "forged ordinal, identity plan: one past the WRITER's top lands None" );
        check( back[0].seq == 9, "forged ordinal, identity plan: the scalar behind it landed" );
        check( r.clamped == 1, "forged ordinal, identity plan: clamped == 1 on the SAME forged bytes" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.duplicate == 0,
               "forged ordinal, identity plan: only clamped moved" );
    }
}

// ---- copy, const, present, ordinal move nothing on a clean append -----------

static void clean_append_moves_nothing( void )
{
    static uint8_t buf[1 << 16];

    // COPY: one field appended (field_append).
    {
        vold_field_append::Lineage old[1]; old[0].x = 11; old[0].y = 22; old[0].z = 33;
        int64_t len = vold_field_append::LineageFixedSave( old, 1, buf, (int64_t) sizeof( buf ) );
        check( len > 0, "field_append: the OLD writer wrote its record" );
        vnew_field_append::Lineage back[1];
        vnew_field_append::TableReport r{};
        static vnew_field_append::TableFixedEntry plan[4096];
        int64_t n = vnew_field_append::LineageFixedLoad( back, 1, buf, len, plan, 4096, NULL, &r );
        check( n == 1 && back[0].x == 11 && back[0].y == 22 && back[0].z == 33,
               "field_append: an appended field is not an event — the old values landed" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0,
               "field_append: the COPY op moved every counter not at all" );
    }

    // ORDINAL: one variant appended (enum_append), a lawful ordinal remap.
    {
        vold_enum_append::Lineage old[1];
        old[0].tier = vold_enum_append::Tier::Gold; old[0].seq = 9;
        int64_t len = vold_enum_append::LineageFixedSave( old, 1, buf, (int64_t) sizeof( buf ) );
        check( len > 0, "enum_append clean: the OLD writer wrote its record" );
        vnew_enum_append::Lineage back[1];
        vnew_enum_append::TableReport r{};
        static vnew_enum_append::TableFixedEntry plan[4096];
        int64_t n = vnew_enum_append::LineageFixedLoad( back, 1, buf, len, plan, 4096, NULL, &r );
        check( n == 1 && back[0].tier == vnew_enum_append::Tier::Gold && back[0].seq == 9,
               "enum_append clean: the appended variant is not an event — Gold landed" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0,
               "enum_append clean: the ORDINAL op moved every counter not at all" );
    }

    // CONST: one arm appended (union_append), a lawful tag remap.
    {
        vold_union_append::Lineage old[1];
        new ( (void *) &old[0].pick.alpha ) vold_union_append::Alpha{};
        old[0].pick.type = vold_union_append::PickType::Alpha;
        old[0].pick.alpha.m = 7;
        old[0].seq = 5;
        int64_t len = vold_union_append::LineageFixedSave( old, 1, buf, (int64_t) sizeof( buf ) );
        check( len > 0, "union_append: the OLD writer wrote its record" );
        vnew_union_append::Lineage back[1];
        vnew_union_append::TableReport r{};
        static vnew_union_append::TableFixedEntry plan[4096];
        int64_t n = vnew_union_append::LineageFixedLoad( back, 1, buf, len, plan, 4096, NULL, &r );
        check( n == 1 && back[0].pick.type == vnew_union_append::PickType::Alpha &&
               back[0].pick.alpha.m == 7 && back[0].seq == 5,
               "union_append: an appended arm is not an event — the tag and payload landed" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0,
               "union_append: the CONST op moved every counter not at all" );
    }

    // PRESENT: T into ?T (optional_add), every old value lands present.
    {
        vold_optional_add::OptionalAdd old[1];
        old[0].lead = 1; old[0].link.value = 42; old[0].trail = 2;
        int64_t len = vold_optional_add::OptionalAddFixedSave( old, 1, buf, (int64_t) sizeof( buf ) );
        check( len > 0, "optional_add: the OLD writer wrote its record" );
        vnew_optional_add::OptionalAdd back[1];
        vnew_optional_add::TableReport r{};
        static vnew_optional_add::TableFixedEntry plan[4096];
        int64_t n = vnew_optional_add::OptionalAddFixedLoad( back, 1, buf, len, plan, 4096, NULL, &r );
        check( n == 1 && back[0].link_present && back[0].link.value == 42,
               "optional_add: T into ?T lands present, and the value with it" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0,
               "optional_add: the PRESENT op moved every counter not at all" );
    }

    // FLAG: one flag appended (flags_append) — the same all-zero law.
    {
        vold_flags_append::Lineage old[1];
        old[0].caps = vold_flags_append::Caps_Jump; old[0].seq = 5;
        int64_t len = vold_flags_append::LineageFixedSave( old, 1, buf, (int64_t) sizeof( buf ) );
        check( len > 0, "flags_append: the OLD writer wrote its record" );
        vnew_flags_append::Lineage back[1];
        vnew_flags_append::TableReport r{};
        static vnew_flags_append::TableFixedEntry plan[4096];
        int64_t n = vnew_flags_append::LineageFixedLoad( back, 1, buf, len, plan, 4096, NULL, &r );
        check( n == 1, "flags_append: the appended flag is not an event — the record read" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0,
               "flags_append: every counter stayed at zero" );
    }
}

int main( void )
{
    unknown_census_once_per_peer();
    widened_scalar_per_record();
    widened_element_run_is_min();
    widened_enum_ordinal_width();
    clamped_count_per_record();
    clamped_text_length();
    forged_ordinal_both_plans();
    clean_append_moves_nothing();

    if ( g_failures != 0 )
    {
        std::printf( "R16: %d assertion(s) failed\n", g_failures );
        return 1;
    }
    std::printf( "R16: all assertions passed\n" );
    return 0;
}
