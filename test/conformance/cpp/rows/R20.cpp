// R20 — §21.2's landing rules, the C++ leg (docs/SPEC-TABLES.md:16452).
//
// The law:
//
//   "A field the reader added: its declared default. A field the reader
//    deprecated: dropped, counted once under `unknown` per plan. A narrower
//    integer or float: widened exactly, `widened` counts. A shorter array or
//    string: landed, the reader's slack is template zeros. An older enum: its
//    ordinals are the reader's, the list being a prefix. Everything else:
//    copied."                              (docs/SPEC-TABLES.md:16452-16455)
//
// ONE CLAUSE IS GOVERNED ELSEWHERE, and this file asserts the governed law
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md:328-331, ruling 12.3 #3): a
// deprecated field "keeps its slot and is landed by every plan, identity
// included; nothing is dropped and `unknown` does not move for it." That
// supersedes §21.2's "dropped, counted once under unknown per plan" (the
// bill's §3 wording, corrected by its own ruling); §21.1's own table agrees
// (docs/SPEC-TABLES.md:16448, "read on every plan"). The assertion below is
// the reference's: `field_deprecate` lands a,b,c exact with `unknown == 0`.
//
// The production path this file exercises is the generated FIXED reader's
// plan compile + load: build/tables-generated/<schema>/*Table.h, emitted by
// internal/codegen/cpptable, driven exactly as the leg's own versioning gate
// drives it (test/tables/versioning_lists.cpp, test/tables/versioning_numbers.cpp,
// built by `make build/schema_test_fixedform`). Each row's OLD file is written
// by the OLD generation's writer and read by the NEW generation's
// `##FixedLoad` — the §21.2 "newer reader, older file" direction, one record.
//
// Run: c++ -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
//        -Ibuild/tables-generated/{vold,vnew}_field_append
//        -Ibuild/tables-generated/{vold,vnew}_field_deprecate
//        -Ibuild/tables-generated/{vold,vnew}_enum_append
//        -Ibuild/tables-generated/vnum
//        test/conformance/cpp/rows/R20.cpp -o build/rows-cpp-R20
//      && ./build/rows-cpp-R20
// Exit 0 green, exit 1 red; one printed line per assertion.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "VOLD_field_appendTable.h"
#include "VNEW_field_appendTable.h"
#include "VOLD_field_deprecateTable.h"
#include "VNEW_field_deprecateTable.h"
#include "VOLD_int_widenTable.h"
#include "VNEW_int_widenTable.h"
#include "VOLD_float_widenTable.h"
#include "VNEW_float_widenTable.h"
#include "VOLD_string_growTable.h"
#include "VNEW_string_growTable.h"
#include "VOLD_array_bounded_growTable.h"
#include "VNEW_array_bounded_growTable.h"
#include "VOLD_enum_appendTable.h"
#include "VNEW_enum_appendTable.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s: %s\n", ok ? "ok" : "FAIL", what );
    if ( !ok ) failures++;
}

// one record, saved by the WRITER generation whose name is on the call
template <typename T, typename Measure, typename Save>
static std::vector<uint8_t> one( const T & v, Measure measure, Save save )
{
    std::vector<uint8_t> out( (size_t) measure( 1 ) );
    check( save( &v, 1, out.data(), (int64_t) out.size() ) == (int64_t) out.size(), "the OLD file saves" );
    return out;
}

// ---------------------------------------------------------------------------
// clause 1 — A field the reader added: its declared default.
// field_append: vec {x,y,z} -> {x,y,z,w}; w declared = 77.

static void field_append_case()
{
    vold_field_append::Lineage old;
    vold_field_append::LineageReset( old );
    old.x = 11; old.y = 22; old.z = 33;
    std::vector<uint8_t> ob = one( old, vold_field_append::LineageFixedMeasure,
                                   vold_field_append::LineageFixedSave );

    vnew_field_append::Lineage back;
    vnew_field_append::LineageReset( back );
    vnew_field_append::TableReport r;
    std::vector<vnew_field_append::TableFixedEntry> plan( 1024 );
    check( vnew_field_append::LineageFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                plan.data(), 1024, NULL, &r ) == 1,
           "R20/added-field: the OLD file reads one record on the NEW reader" );
    check( back.x == 11 && back.y == 22 && back.z == 33,
           "R20/added-field: the writer's own fields land exactly" );
    check( back.w == 77,
           "R20/added-field: the field the reader added lands its DECLARED DEFAULT (w == 77)" );
    check( r.unknown == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
           "R20/added-field: no counter moved — an append is not a widen" );
}

// ---------------------------------------------------------------------------
// clause 2 — A field the reader deprecated: kept its slot, read on every plan,
// unknown does not move (the governing ruling, bill 12.3 #3).
// field_deprecate: {a,b,c} -> {a,b deprecated,c}.

static void field_deprecate_case()
{
    vold_field_deprecate::Lineage old;
    vold_field_deprecate::LineageReset( old );
    old.a = 1; old.b = 2; old.c = 3;
    std::vector<uint8_t> ob = one( old, vold_field_deprecate::LineageFixedMeasure,
                                   vold_field_deprecate::LineageFixedSave );

    vnew_field_deprecate::Lineage back;
    vnew_field_deprecate::LineageReset( back );
    vnew_field_deprecate::TableReport r;
    std::vector<vnew_field_deprecate::TableFixedEntry> plan( 1024 );
    check( vnew_field_deprecate::LineageFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                   plan.data(), 1024, NULL, &r ) == 1,
           "R20/deprecated-field: the OLD file reads one record on the NEW reader" );
    check( back.a == 1 && back.b == 2 && back.c == 3,
           "R20/deprecated-field: a,b,c exact — the deprecated field KEEPS its slot and lands on every plan" );
    check( r.unknown == 0,
           "R20/deprecated-field: `unknown` does not move for a deprecated field the reader has (bill 12.3 #3)" );
    check( r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
           "R20/deprecated-field: nothing else fired" );
}

// ---------------------------------------------------------------------------
// clause 3 — A narrower integer or float: widened exactly, `widened` counts.
// int_widen: int16 -> int32; the three values a widening gets wrong.
// float_widen: float32 -> float64; a signalling NaN's payload rides the 22 bits.

static void int_widen_case()
{
    const int16_t values[3] = { -1, INT16_MIN, INT16_MAX };
    const char * names[3] = { "R20/int-widen: -1 sign-extends to -1",
                              "R20/int-widen: INT16_MIN lands exactly",
                              "R20/int-widen: INT16_MAX lands exactly" };
    for ( int k = 0; k < 3; ++k )
    {
        vold_int_widen::IntWiden old;
        vold_int_widen::IntWidenReset( old );
        old.lead = 0xAAAAAAAAu;
        old.v = values[k];
        old.trail = 0xBBBBBBBBu;
        std::vector<uint8_t> ob = one( old, vold_int_widen::IntWidenFixedMeasure,
                                       vold_int_widen::IntWidenFixedSave );
        vnew_int_widen::IntWiden back;
        vnew_int_widen::IntWidenReset( back );
        vnew_int_widen::TableReport r;
        std::vector<vnew_int_widen::TableFixedEntry> plan( 1024 );
        check( vnew_int_widen::IntWidenFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                  plan.data(), 1024, NULL, &r ) == 1,
               "R20/int-widen: the OLD file reads one record on the NEW reader" );
        check( back.v == (int32_t) values[k], names[k] );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "R20/int-widen: lead and trail bracket the row exactly" );
        check( r.widened == 1, "R20/int-widen: `widened` counts exactly once per widened field" );
        check( r.unknown == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "R20/int-widen: nothing else fired" );
    }
}

static void float_widen_case()
{
    const uint32_t kSignalling = 0x7F8ABCDEu;
    float sf; std::memcpy( &sf, &kSignalling, 4 );

    vold_float_widen::FloatWiden old;
    vold_float_widen::FloatWidenReset( old );
    old.lead = 0xAAAAAAAAu;
    old.v = sf;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_float_widen::FloatWidenFixedMeasure,
                                   vold_float_widen::FloatWidenFixedSave );
    vnew_float_widen::FloatWiden back;
    vnew_float_widen::FloatWidenReset( back );
    vnew_float_widen::TableReport r;
    std::vector<vnew_float_widen::TableFixedEntry> plan( 1024 );
    check( vnew_float_widen::FloatWidenFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                  plan.data(), 1024, NULL, &r ) == 1,
           "R20/float-widen: the OLD file reads one record on the NEW reader" );
    uint64_t got = 0;
    std::memcpy( &got, &back.v, 8 );
    check( ( got >> 63 ) == ( kSignalling >> 31 ) && ( ( got >> 52 ) & 0x7FFull ) == 0x7FFull,
           "R20/float-widen: a NaN of the same sign stays a NaN" );
    const uint32_t payload22 = kSignalling & 0x003FFFFFu;
    check( (uint32_t) ( ( got >> 29 ) & 0x003FFFFFull ) == payload22,
           "R20/float-widen: the 22 payload bits BELOW the quiet bit ride exactly" );
    check( r.widened == 1, "R20/float-widen: `widened` counts exactly once" );
    check( r.unknown == 0 && r.clamped == 0 && !r.malformed && !r.refused,
           "R20/float-widen: nothing else fired" );
}

// ---------------------------------------------------------------------------
// clause 4 — A shorter array or string: landed, the reader's slack is template
// zeros. string_grow: string(8) -> string(16); the old capacity is FULL.
// array_bounded_grow: [..4]int32 -> [..8]int32; the writer's count is 4.

static void string_grow_case()
{
    vold_string_grow::StringGrow old;
    vold_string_grow::StringGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    std::strcpy( old.text, "abcdefgh" );
    old.text_length = 8;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_string_grow::StringGrowFixedMeasure,
                                   vold_string_grow::StringGrowFixedSave );
    vnew_string_grow::StringGrow back;
    vnew_string_grow::StringGrowReset( back );
    vnew_string_grow::TableReport r;
    std::vector<vnew_string_grow::TableFixedEntry> plan( 1024 );
    check( vnew_string_grow::StringGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                  plan.data(), 1024, NULL, &r ) == 1,
           "R20/string: the OLD file reads one record on the NEW reader" );
    check( back.text_length == 8 && std::strcmp( back.text, "abcdefgh" ) == 0,
           "R20/string: the writer's length and bytes land exactly" );
    bool slack = true;
    for ( int i = 8; i < 16; ++i ) slack = slack && back.text[i] == 0;
    check( slack, "R20/string: the reader's slack (slots 8..15) is template ZEROS" );
    check( r.unknown == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
           "R20/string: a capacity that grew moves no counter" );
}

static void array_bounded_grow_case()
{
    vold_array_bounded_grow::ArrayBoundedGrow old;
    vold_array_bounded_grow::ArrayBoundedGrowReset( old );
    old.lead = 0xAAAAAAAAu;
    old.vals_count = 4;
    for ( int i = 0; i < 4; ++i ) old.vals[i] = 1000 + i;
    old.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> ob = one( old, vold_array_bounded_grow::ArrayBoundedGrowFixedMeasure,
                                   vold_array_bounded_grow::ArrayBoundedGrowFixedSave );
    vnew_array_bounded_grow::ArrayBoundedGrow back;
    vnew_array_bounded_grow::ArrayBoundedGrowReset( back );
    vnew_array_bounded_grow::TableReport r;
    std::vector<vnew_array_bounded_grow::TableFixedEntry> plan( 1024 );
    check( vnew_array_bounded_grow::ArrayBoundedGrowFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                                               plan.data(), 1024, NULL, &r ) == 1,
           "R20/array: the OLD file reads one record on the NEW reader" );
    check( back.vals_count == 4, "R20/array: the WRITER's count lands, not the reader's bound" );
    bool exact = true;
    for ( int i = 0; i < 4; ++i ) exact = exact && back.vals[i] == 1000 + i;
    check( exact, "R20/array: the writer's four elements land exactly" );
    bool slack = true;
    for ( int i = 4; i < 8; ++i ) slack = slack && back.vals[i] == 0;
    check( slack, "R20/array: the reader's slack (slots 4..7) is template ZEROS" );
    check( r.unknown == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
           "R20/array: a bound that grew moves no counter" );
}

// ---------------------------------------------------------------------------
// clause 5 — An older enum: its ordinals are the reader's, the list being a
// prefix. enum_append: Tier {Bronze,Silver,Gold} -> +Platinum; a gold record.

static void enum_append_case()
{
    vold_enum_append::Lineage old;
    vold_enum_append::LineageReset( old );
    old.tier = vold_enum_append::Tier::Gold;
    old.seq = 9;
    std::vector<uint8_t> ob = one( old, vold_enum_append::LineageFixedMeasure,
                                   vold_enum_append::LineageFixedSave );
    vnew_enum_append::Lineage back;
    vnew_enum_append::LineageReset( back );
    vnew_enum_append::TableReport r;
    std::vector<vnew_enum_append::TableFixedEntry> plan( 1024 );
    check( vnew_enum_append::LineageFixedLoad( &back, 1, ob.data(), (int64_t) ob.size(),
                                               plan.data(), 1024, NULL, &r ) == 1,
           "R20/enum: the OLD file reads one record on the NEW reader" );
    check( (int) vold_enum_append::Tier::Gold == (int) vnew_enum_append::Tier::Gold,
           "R20/enum: the OLD list is a PREFIX — the shared ordinals are equal across the pair" );
    check( back.tier == vnew_enum_append::Tier::Gold,
           "R20/enum: an old Gold record reads Gold — the writer's ordinal IS the reader's" );
    check( back.seq == 9, "R20/enum: the scalar behind the enum lands exactly" );
    check( r.unknown == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
           "R20/enum: an appended variant moves no counter" );
}

int main()
{
    field_append_case();
    field_deprecate_case();
    int_widen_case();
    float_widen_case();
    string_grow_case();
    array_bounded_grow_case();
    enum_append_case();
    if ( failures == 0 ) { std::printf( "R20: §21.2's landing rules green on the C++ leg\n" ); }
    return failures == 0 ? 0 : 1;
}