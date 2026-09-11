// THE FIXED FORM'S VERSIONING CONFORMANCE (docs/SPEC-TABLES.md §3.4, "held by
// test"). The invariant §3.4 states before anything else is that a fixed
// record is positional BY PLAN and the positions are the WRITER's LAYOUT, never
// the reader's own declaration — "we must not ever break versioning in fixed
// tables", the project owner. This file is what that sentence is worth.
//
// Every case below reads a record through the ONE plan-driven path, and the
// only thing that differs between them is which plan the loop was handed.
//
//   1. SAME SCHEMA           the identity plan, a static constant
//   2. AN OLDER WRITER       a field this reader has and that record does not:
//                            the prefill's declared default stands
//   3. A NEWER WRITER        a field this reader cannot name, stepped over by
//                            the size its entry states; and a whole nested
//                            TYPE this reader cannot name, stepped over by its
//                            layout size
//   4. A RENAMED FIELD       arriving under `was =`, silently
//   5. A WIDENED FIELD       uint16 into uint32, decoded exactly, counted
//   6. AN ENUM AND A UNION   a variant and an arm inserted IN THE MIDDLE,
//                            remapped by name and never by position (V1/V2)
//   7. AN OPTIONAL           P1's value against P3's `?T` (§2.3's rule, and
//                            what this form does to it)
//   8. THE NEGATIVE CONTROL  a reader given the WRONG PLAN for a record, which
//                            must come out wrong. A test that never watched
//                            the wrong plan fail never checked the right one.
//   9. THE LAYOUT'S OWN      one corrupted-layout case per NAMED RULE §3.4
//      VALIDATION            holds an untrusted peer's layout to, each
//                            breaking exactly one thing in a layout this
//                            reader accepts and each refused under its own
//                            name with nothing decoded
//
// THE LAYOUT is what form 1 called the vocabulary block. It is neither §7's
// cooked block nor §19's block form.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "FX1Table.h"
#include "FX2Table.h"
#include "V1Table.h"
#include "V2Table.h"
#include "P1Table.h"
#include "P3Table.h"
#include "UT1Table.h"
#include "UT2Table.h"
#include "FU1Table.h"
#include "FU2Table.h"

static int failures = 0;

// ==== BEGIN rowan/cpp-versioning-lists ====
// the LIST rows' read columns, in their own translation unit; it returns its
// own failure count and prints its own known-red report
int versioning_lists_cases();
// ==== END rowan/cpp-versioning-lists ====

static void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
}

// ---------------------------------------------------------------------------

static void fx_case()
{
    // an FX1 record, every field off its default so nothing passes by accident
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.keep = 4242u;
    one.narrow = 40000u;      // a uint16 value the widened read must reproduce
    one.renamed = 321;
    one.gone = 654;
    one.nested.a = 111;
    one.nested.b = 222;
    // A `bytes(N)` IS AN ARRAY OF u8 ON THIS WIRE (§3.4), so its destination row
    // is an ARRAY's — the buffer, and the live length beside it — and not a
    // text field's, which is the other way round. Only a COMPILED plan reads
    // those columns, so only FX2's read of this record can tell.
    one.blob[0] = 0xDE; one.blob[1] = 0xAD; one.blob[2] = 0xBE; one.blob[3] = 0xEF;
    one.blob_length = 4;

    std::vector<uint8_t> w1( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w1.data(), (int64_t) w1.size() ) == (int64_t) w1.size(), "FX1 save" );

    // 1. SAME SCHEMA — the identity plan
    {
        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfx1::FxRootFixedLoad( &back, 1, w1.data(), (int64_t) w1.size(), plan.data(), 1024, NULL, &r );
        check( n == 1, "same schema: one record" );
        check( back.keep == 4242u && back.narrow == 40000u && back.renamed == 321 && back.gone == 654, "same schema: the scalars" );
        check( back.nested.a == 111 && back.nested.b == 222, "same schema: the nesting" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "same schema: a clean read moves no counter" );
    }

    // 2, 4, 5. FX2 READS FX1 — a plan compiled from FX1's layout
    {
        tblfx2::FxRoot back;
        tblfx2::TableReport r;
        std::vector<tblfx2::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfx2::FxRootFixedLoad( &back, 1, w1.data(), (int64_t) w1.size(), plan.data(), 1024, NULL, &r );
        check( n == 1, "older writer: one record" );
        check( back.keep == 4242u, "older writer: an unmoved field" );
        check( back.narrow == 40000u, "WIDENED: uint16 into uint32, exactly" );
        check( r.widened == 1, "WIDENED: one widened counts" );
        check( back.renamed_to == 321, "RENAMED: `was =` keeps the wire id" );
        check( back.added == 11, "MISSING: a field the writer does not carry takes its declared default" );
        check( back.extra.x == 0 && back.extra.y == 0, "MISSING: a whole nested type takes its defaults" );
        check( back.nested.a == 111 && back.nested.b == 222, "older writer: the nesting" );
        check( back.blob_length == 4 && back.blob[0] == 0xDE && back.blob[1] == 0xAD &&
               back.blob[2] == 0xBE && back.blob[3] == 0xEF,
               "BYTES(N): the compiled plan lands the buffer in the buffer and the length in the length" );
        check( back.blob[4] == 0 && back.blob[5] == 0, "BYTES(N): the slack past the live length is zero" );
        check( r.unknown == 1, "older writer: `gone` is the one field this reader cannot name" );
        check( r.kind_mismatch == 0 && !r.malformed && !r.refused, "older writer: nothing else fired" );
    }

    // 3. FX1 READS FX2 — an unknown field and an unknown NESTED TYPE
    {
        tblfx2::FxRoot two;
        tblfx2::FxRootReset( two );
        two.keep = 5150u;
        two.narrow = 70000u;
        two.renamed_to = 808;
        two.added = 909;
        two.nested.a = 33;
        two.nested.b = 44;
        two.extra.x = 55;
        two.extra.y = 66;
        std::vector<uint8_t> w2( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
        check( tblfx2::FxRootFixedSave( &two, 1, w2.data(), (int64_t) w2.size() ) == (int64_t) w2.size(), "FX2 save" );

        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfx1::FxRootFixedLoad( &back, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, NULL, &r );
        check( n == 1, "newer writer: one record" );
        check( back.keep == 5150u, "newer writer: an unmoved field lands past the unknowns" );
        check( back.renamed == 808, "newer writer: `was =` reads the other way too" );
        check( back.gone == 9, "newer writer: a field the writer dropped takes its declared default" );
        check( back.nested.a == 33 && back.nested.b == 44, "newer writer: the nesting lands past the unknown type" );
        // `added` is an unknown FIELD; `extra` is an unknown nested TYPE, and
        // stepping over it by its layout size is what puts `nested` in the
        // right place above
        check( r.unknown == 2, "newer writer: two names this reader does not have" );
        check( r.kind_mismatch == 1, "newer writer: uint32 into uint16 is a kind that moved, not a widening" );
        check( back.narrow == 3, "newer writer: a narrowing leaves the declared default" );
        check( !r.malformed && !r.refused, "newer writer: no damage and no refusal" );
    }
}

// ---------------------------------------------------------------------------

static void v_case()
{
    tblv1::Cfg one;
    tblv1::CfgReset( one );
    one.a = 42;
    std::strcpy( one.name, "hello" );
    one.name_length = 5;
    one.grade = tblv1::Grade::Gold;         // V2 inserts Silver BEFORE Gold
    one.effect.type = tblv1::EffectType::Ward;
    one.effect.ward.charge = 0.75f;         // V2 inserts hex BEFORE ward
    one.tokens[tblv1::Slot::Alpha] = 21;
    one.tokens[tblv1::Slot::Delta] = 24;    // V2 slides Beta and keeps Delta
    one.tier_present = true;
    one.tier = 77;

    std::vector<uint8_t> w( (size_t) tblv1::CfgFixedMeasure( 1 ) );
    check( tblv1::CfgFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "V1 save" );

    tblv2::Cfg back;
    tblv2::TableReport r;
    std::vector<tblv2::TableFixedEntry> plan( 8192 );
    const int64_t n = tblv2::CfgFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 8192, NULL, &r );
    check( n == 1, "V2 reads V1: one record" );
    check( back.grade == tblv2::Grade::Gold, "ENUM: a variant inserted in the middle is remapped by NAME" );
    check( back.effect.type == tblv2::EffectType::Ward, "UNION: an arm inserted in the middle is remapped by NAME" );
    check( back.effect.ward.charge == 0.75f, "UNION: the arm's payload lands" );
    check( std::strcmp( back.title, "hello" ) == 0 && back.title_length == 5, "RENAMED: title reads name's bytes" );
    check( back.tokens[tblv2::Slot::Alpha] == 21, "KEYED: a slot whose key did not move" );
    check( back.tokens[tblv2::Slot::Delta] == 24, "KEYED: a slot whose key SLID keeps its value" );
    check( back.tokens[tblv2::Slot::Sigma] == 0, "KEYED: a key the writer has no name for takes its default" );
    check( back.tier_present && back.tier == 77, "OPTIONAL: the present flag and the payload" );
    check( back.c, "MISSING: V2's own `c` takes its declared default" );
    check( back.a == 5.0f, "KIND MOVED: int32 -> float32 leaves the declared default" );
    check( !r.malformed && !r.refused, "V2 reads V1: no damage and no refusal" );
}

// ---------------------------------------------------------------------------

static void p_case()
{
    // P1 nests Link BY VALUE and P3 marks the same field `?Link`. On FORM 1
    // those two are wire-identical (§2.3). ON THIS FORM THEY ARE NOT: an
    // optional carries a present byte in front of a payload that rides whole,
    // so the layout says kind 35 on one side and kind 13 on the other, and the
    // edit is REPORTED rather than silent. That is the departure from §2.3
    // this form makes, and it is here so that it is a pinned fact and not a
    // surprise.
    tblp1::Chain one;
    tblp1::ChainReset( one );
    std::strcpy( one.name, "chain" );
    one.name_length = 5;
    one.link.value = 500;

    std::vector<uint8_t> w( (size_t) tblp1::ChainFixedMeasure( 1 ) );
    check( tblp1::ChainFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "P1 save" );

    tblp3::Chain back;
    tblp3::TableReport r;
    std::vector<tblp3::TableFixedEntry> plan( 1024 );
    const int64_t n = tblp3::ChainFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r );
    check( n == 1, "P3 reads P1: one record" );
    check( std::strcmp( back.name, "chain" ) == 0, "P3 reads P1: the plain field lands" );
    check( r.kind_mismatch == 1, "OPTIONAL vs VALUE is a reported kind on this form, never a silent reread" );
}

// ---------------------------------------------------------------------------

static void negative_control()
{
    // THE WRONG PLAN. FX1's identity plan is correct for an FX1 record and
    // wrong for an FX2 one — FX2 inserts `added` between `renamed_to` and
    // `nested`, so every offset past it has moved. Running FX1's plan over
    // FX2's body is exactly the mistake the hash exists to prevent, and it
    // must come out WRONG. If this ever comes out right, the hash is not
    // carrying anything and the test above proves nothing.
    tblfx2::FxRoot two;
    tblfx2::FxRootReset( two );
    two.keep = 5150u;
    two.narrow = 70000u;
    two.renamed_to = 808;
    two.added = 909;
    two.nested.a = 33;
    two.nested.b = 44;
    std::vector<uint8_t> w2( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
    tblfx2::FxRootFixedSave( &two, 1, w2.data(), (int64_t) w2.size() );
    const uint8_t * body = w2.data() + tblfx2::kTableFixedHeaderBytes + 4 + tblfx2::FxRootFixedLayoutBytes + 8;

    tblfx1::FxRoot wrong;
    tblfx1::FxRootReset( wrong );
    tblfx1::TableReport r;
    tblfx1::TableFixedRun( tblfx1::FxRootFixedPlan, tblfx1::FxRootFixedPlanCount, tblfx1::FxRootFixedPlanGuarded, body, (uint8_t *) &wrong, &r );
    const bool intact = wrong.nested.a == 33 && wrong.nested.b == 44 && wrong.renamed == 808;
    check( !intact, "NEGATIVE CONTROL: the wrong plan must NOT reproduce the record" );

    // and the loader never takes that path: the hash is what selects the plan
    tblfx1::FxRoot right;
    tblfx1::TableReport r2;
    std::vector<tblfx1::TableFixedEntry> plan( 1024 );
    const int64_t n = tblfx1::FxRootFixedLoad( &right, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, NULL, &r2 );
    check( n == 1 && right.nested.a == 33 && right.nested.b == 44,
           "NEGATIVE CONTROL: the loader compiles a plan from the layout and gets it right" );

    // A LAYOUT THAT IS NOT A LAYOUT IS REFUSED BY NAME, whole, and never damage.
    // Every rule has its own case in layout_validation() below; this one is
    // here because it is also the check that a refusal MOVES NO COUNTER.
    {
        std::vector<uint8_t> broken = w2;
        // THE ENTRY COUNT, which is the layout's own first four bytes and not
        // the u32 LENGTH in front of them: a length that no longer matches the
        // file is a different rule with a different name.
        broken[tblfx1::kTableFixedHeaderBytes + 4] ^= 0xFFu;
        tblfx1::FxRoot v;
        tblfx1::TableReport r3;
        const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, broken.data(), (int64_t) broken.size(), plan.data(), 1024, NULL, &r3 );
        check( bad < 0 && r3.refused && r3.reason == tblfx1::layout_count_mismatch, "REFUSED BY NAME: layout_count_mismatch" );
        check( r3.unknown == 0 && r3.kind_mismatch == 0 && !r3.malformed, "REFUSED BY NAME: a refusal moves no counter" );
    }

    // A FORM BYTE THIS READER DOES NOT CARRY IS A REFUSAL AND NEVER DAMAGE, AND
    // THE NAME SAYS WHICH DIRECTION (docs/SPEC-TABLES.md §3, §3.4). The registry
    // is ordered, so a fixed reader handed form `1` has been handed the
    // VARIABLE form, which is OLDER: calling that `newer_form` would send a
    // caller looking for a build that does not exist. Form `6` is the byte no
    // form defines or reserves, which is the only kind of byte `newer_form` is
    // the honest answer for.
    {
        struct { uint8_t form; tblfx1::TableMessageReason want; const char * what; } rows[3] = {
            { 1, tblfx1::previous_form,      "REFUSED BY NAME: previous_form for the VARIABLE form" },
            { 2, tblfx1::message_form_as_file, "REFUSED BY NAME: message_form_as_file for a batch" },
            { 6, tblfx1::newer_form,         "REFUSED BY NAME: newer_form for a byte no form defines" },
        };
        for ( int i = 0; i < 3; ++i )
        {
            std::vector<uint8_t> other = w2;
            other[0] = rows[i].form;
            tblfx1::FxRoot v;
            tblfx1::TableReport r4;
            const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, other.data(), (int64_t) other.size(), plan.data(), 1024, NULL, &r4 );
            check( bad < 0 && r4.refused && r4.reason == rows[i].want, rows[i].what );
            check( !r4.malformed, "REFUSED BY NAME: never damage" );
            check( r4.unknown == 0 && r4.kind_mismatch == 0 && r4.widened == 0 && r4.clamped == 0,
                   "REFUSED BY NAME: a form-byte refusal moves no counter" );
        }
    }

    // THE HEADER NAMES THE LAYOUT ONCE (docs/SPEC-TABLES.md §3): the eight bytes
    // at offset 8 are the LAYOUT's hash, and a header that claims a layout it
    // does not carry is refused. This is the case that proves the header's hash
    // is READ and not merely written.
    {
        std::vector<uint8_t> lying = w2;
        lying[tblfx1::kTableFixedHashAt] ^= 0xFFu;
        tblfx1::FxRoot v;
        tblfx1::TableReport r6;
        const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, lying.data(), (int64_t) lying.size(), plan.data(), 1024, NULL, &r6 );
        check( bad < 0 && r6.refused && r6.reason == tblfx1::layout_malformed,
               "REFUSED BY NAME: a header hash that is not the layout's" );
        check( !r6.malformed, "REFUSED BY NAME: never damage" );
    }

    // A PLAN THAT DOES NOT FIT THE CALLER'S STORAGE IS A REFUSAL BY NAME, and
    // the codec allocates nothing to get around it.
    {
        tblfx1::FxRoot v;
        tblfx1::TableReport r5;
        tblfx1::TableFixedEntry tiny[1];
        const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, w2.data(), (int64_t) w2.size(), tiny, 1, NULL, &r5 );
        check( bad < 0 && r5.refused && r5.reason == tblfx1::plan_too_large, "REFUSED BY NAME: plan_too_large" );
    }
}

// ---------------------------------------------------------------------------

// THE LAYOUT ARRIVES FROM AN UNTRUSTED PEER (docs/SPEC-TABLES.md §3.4). It is
// the one structure a reader must parse before it knows anything at all, so
// every rule it is held to refuses under ITS OWN NAME, before a single record
// byte is touched. A VALIDATION NOBODY WATCHED FAIL IS A VALIDATION NOBODY
// HAS, so there is one case per named rule, each taking a layout this reader
// accepts and breaking EXACTLY ONE THING in it.
//
// The file is the HEADER (docs/SPEC-TABLES.md §3: the form byte, seven reserved
// zero bytes, the layout hash at 8, the body at 16), then `u32 layout length,
// layout, records` — so the layout starts at byte 20, the entry count is the
// four bytes there, and entry k is the seventeen bytes at 20 + 4 + 17k: id
// (u64), kind (u8), size (u32), children (u32), every number little-endian.

static const size_t kLayoutAt = (size_t) tblfx1::kTableFixedHeaderBytes + 4;
static const size_t kEntry0 = kLayoutAt + 4;

static uint8_t * entry_at( std::vector<uint8_t> & f, size_t k ) { return f.data() + kEntry0 + k * 17; }

// an entry appended to a hand-built layout, for the two rules no single break
// of a real layout reaches
static void put_entry( std::vector<uint8_t> & layout, uint64_t id, uint8_t kind, uint32_t size, uint32_t children )
{
    const size_t at = layout.size();
    layout.resize( at + 17 );
    tblfx1::TableFixedPut64( layout.data() + at, id );
    layout[at + 8] = kind;
    tblfx1::TableFixedPut32( layout.data() + at + 9, size );
    tblfx1::TableFixedPut32( layout.data() + at + 13, children );
}

// a FILE around a hand-built layout: the form byte, the layout's length, the
// layout, and no records — every rule below refuses before a record is reached
static std::vector<uint8_t> file_of( const std::vector<uint8_t> & layout )
{
    std::vector<uint8_t> f( (size_t) tblfx1::kTableFixedHeaderBytes + 4, 0 );
    f[0] = 3; // the fixed form's byte
    tblfx1::TableFixedPut64( f.data() + tblfx1::kTableFixedHashAt,
                             tblfx1::TableFixedHashOf( layout.data(), (uint32_t) layout.size() ) );
    tblfx1::TableFixedPut32( f.data() + tblfx1::kTableFixedHeaderBytes, (uint32_t) layout.size() );
    f.insert( f.end(), layout.begin(), layout.end() );
    return f;
}

static void refuses( std::vector<uint8_t> & broken, tblfx1::TableMessageReason want, const char * what )
{
    tblfx1::FxRoot v;
    tblfx1::FxRootReset( v );
    tblfx1::TableReport r;
    std::vector<tblfx1::TableFixedEntry> plan( 1024 );
    const int64_t n = tblfx1::FxRootFixedLoad( &v, 1, broken.data(), (int64_t) broken.size(), plan.data(), 1024, NULL, &r );
    check( n < 0 && r.refused && r.reason == want, what );
    // NOTHING WAS DECODED AND NOTHING WAS COUNTED. A refusal that half-read a
    // record would be the damage the refusal exists to prevent.
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed,
           "a layout refusal sets nothing and counts nothing" );
}

static void layout_validation()
{
    // the layout this reader ACCEPTS, which every case below breaks once
    tblfx2::FxRoot two;
    tblfx2::FxRootReset( two );
    std::vector<uint8_t> good( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
    check( tblfx2::FxRootFixedSave( &two, 1, good.data(), (int64_t) good.size() ) == (int64_t) good.size(),
           "layout validation: the unbroken file saves" );
    {
        // and it READS, so every refusal below is the ONE break and not the file
        tblfx1::FxRoot v;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &v, 1, good.data(), (int64_t) good.size(), plan.data(), 1024, NULL, &r ) == 1 && !r.refused,
               "layout validation: the unbroken file reads, so the breaks below are the breaks" );
    }

    // 1. THE ENTRY COUNT FITS THE LAYOUT'S LENGTH EXACTLY
    {
        std::vector<uint8_t> f = good;
        const uint32_t count = tblfx1::TableFixedGet32( f.data() + kLayoutAt );
        tblfx1::TableFixedPut32( f.data() + kLayoutAt, count + 1u );
        refuses( f, tblfx1::layout_count_mismatch, "RULE: the entry count fits the layout length exactly" );
    }
    {
        // a count of ZERO is not a layout either: there is no root to walk
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( f.data() + kLayoutAt, 0u );
        refuses( f, tblfx1::layout_count_mismatch, "RULE: an entry count of zero is not a layout" );
    }

    // 2. EVERY KIND IS IN THE CLOSED SET. A fixed form's kind set is CLOSED, so
    //    a kind outside it means a NEWER FORM BYTE — a different form — and not
    //    a newer layout of this one. It is refused, never stepped over.
    {
        std::vector<uint8_t> f = good;
        entry_at( f, 1 )[8] = 200; // a kind no form byte this build carries defines
        refuses( f, tblfx1::layout_kind_unknown, "RULE: a kind outside the closed set is REFUSED, not skipped" );
    }

    // 3. A KIND IS USED AS ITS DEFINITION ALLOWS — here, the ROOT is a table
    {
        std::vector<uint8_t> f = good;
        entry_at( f, 0 )[8] = 14; // an array as the root of a record
        refuses( f, tblfx1::layout_kind_invalid, "RULE: the root entry is a TABLE" );
    }

    // 4. A CONSTANT SIZE MATCHES ITS KIND
    {
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( entry_at( f, 1 ) + 9, 5u ); // a uint32 leaf in five bytes
        refuses( f, tblfx1::layout_size_mismatch, "RULE: a constant size its kind does not admit" );
    }
    {
        // and a TABLE's size is the SUM of its children's, not a number of its own
        std::vector<uint8_t> f = good;
        const uint32_t body = tblfx1::TableFixedGet32( entry_at( f, 0 ) + 9 );
        tblfx1::TableFixedPut32( entry_at( f, 0 ) + 9, body + 4u );
        refuses( f, tblfx1::layout_size_mismatch, "RULE: a table's size is the sum of its fields'" );
    }

    // 5. THE PRE-ORDER CHILD WALK CONSUMES EXACTLY THE ENTRIES
    {
        std::vector<uint8_t> f = good;
        const uint32_t kids = tblfx1::TableFixedGet32( entry_at( f, 0 ) + 13 );
        tblfx1::TableFixedPut32( entry_at( f, 0 ) + 13, kids + 1u );
        refuses( f, tblfx1::layout_tree_unclosed, "RULE: the tree runs out of layout" );
    }
    {
        // THE OTHER DIRECTION: a tree that closes EARLY leaves entries no walk
        // reaches. It takes a hand-built layout to reach, and that is itself
        // worth stating: dropping a child of a TABLE is caught one rule sooner,
        // by the size that no longer sums, so the only subtree whose loss the
        // size rule cannot see is one that contributes NO size — an enum's
        // variants, at kind 32 and size 0.
        std::vector<uint8_t> layout;
        layout.resize( 4 );
        tblfx1::TableFixedPut32( layout.data(), 4u );
        put_entry( layout, 1u, 13u, 4u, 1u ); // a table of one field
        put_entry( layout, 2u, 30u, 4u, 0u ); // an enum, its TWO variants unreached
        put_entry( layout, 3u, 32u, 0u, 0u );
        put_entry( layout, 4u, 32u, 0u, 0u );
        std::vector<uint8_t> f = file_of( layout );
        refuses( f, tblfx1::layout_tree_unclosed, "RULE: the layout outlasts the tree" );
    }

    // 6. THE TOTAL RECORD SIZE IS WITHIN 65536 AND DOES NOT OVERFLOW
    {
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( entry_at( f, 0 ) + 9, 65537u );
        refuses( f, tblfx1::layout_record_too_large, "RULE: a record size past 65536" );
    }
    {
        // A SIZE THAT WOULD WRAP. The children's sizes are summed in 64 bits
        // precisely so a u32 that overflows is CAUGHT rather than wrapped into
        // a small number that then agrees with a parent.
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( entry_at( f, 1 ) + 9, 0xFFFFFFFFu );
        refuses( f, tblfx1::layout_record_too_large, "RULE: a size that would overflow the sum" );
    }

    // 7. NOTHING NESTED PAST THE READER'S WALK BOUND. A BOUND ON THE WALK AND
    //    NOT ON THE WIRE: the validation recurses, so a layout of a thousand
    //    entries each claiming one child would spend a reader's stack before
    //    any other rule could fire. Nothing in §3.4 fixes the number.
    {
        const uint32_t depth = 4096u; // far past any reader's own bound
        std::vector<uint8_t> layout;
        layout.resize( 4 );
        tblfx1::TableFixedPut32( layout.data(), depth + 1u );
        for ( uint32_t i = 0; i < depth; ++i )
        {
            // a table, then optional wrappers all the way down
            put_entry( layout, 1u, ( i == 0 ) ? 13u : 35u, depth - i, 1u );
        }
        put_entry( layout, 2u, 1u, 1u, 0u ); // a bool at the bottom
        std::vector<uint8_t> f = file_of( layout );
        refuses( f, tblfx1::layout_too_deep, "RULE: a nesting depth past the walk's own bound" );
    }

    // AND THE RESIDUE: bytes that are not a layout at all, which is the one
    // case the seven named rules never reach.
    {
        std::vector<uint8_t> f = file_of( std::vector<uint8_t>( 2, 0 ) );
        refuses( f, tblfx1::layout_malformed, "RULE: fewer bytes than a header is layout_malformed" );
    }
}

// ---------------------------------------------------------------------------

static void fuzz_case()
{
    // A LAYOUT IS A STRANGER'S BYTES, AND THAT IS THIS FORM'S WHOLE RISK. The
    // record carries no lengths and no terminators, so every offset the reader
    // uses is arithmetic over sizes the WRITER wrote down. A layout whose child
    // sizes do not sum to its parent's, or whose tree is a chain ten thousand
    // deep, is not a hypothetical — it is one flipped byte away.
    //
    // So: every byte of a form-3 file, flipped one bit at a time, handed to a
    // reader of the other generation. The reader must answer one of three ways
    // and never a fourth — a refusal by name, a malformed read, or a read that
    // lands values. UNDER ASAN "never a fourth" includes never touching a byte
    // outside the buffer, which is what this is really for. The named rules
    // above say which refusal a given break earns; this says there is no break
    // that earns none of them.
    tblfx2::FxRoot two;
    tblfx2::FxRootReset( two );
    two.keep = 5150u;
    two.narrow = 70000u;
    two.renamed_to = 808;
    two.added = 909;
    two.nested.a = 33;
    two.nested.b = 44;
    two.extra.x = 55;
    two.extra.y = 66;
    std::vector<uint8_t> clean( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
    tblfx2::FxRootFixedSave( &two, 1, clean.data(), (int64_t) clean.size() );

    std::vector<tblfx1::TableFixedEntry> plan( 1024 );
    int64_t refused = 0, damaged = 0, read = 0;
    for ( size_t at = 0; at < clean.size(); ++at )
    {
        for ( int bit = 0; bit < 8; ++bit )
        {
            std::vector<uint8_t> hit = clean;
            hit[at] = (uint8_t) ( hit[at] ^ ( 1u << bit ) );
            tblfx1::FxRoot v;
            tblfx1::TableReport r;
            const int64_t n = tblfx1::FxRootFixedLoad( &v, 1, hit.data(), (int64_t) hit.size(),
                                                       plan.data(), (int32_t) plan.size(), NULL, &r );
            if ( n < 0 )
            {
                if ( r.refused ) { refused++; check( !r.malformed, "fuzz: a refusal is never damage" ); }
                else { damaged++; check( r.malformed, "fuzz: a negative read that is not a refusal is damage" ); }
                continue;
            }
            read += n;
            check( !r.refused, "fuzz: a read that returned records was not refused" );
        }
    }
    std::printf( "fuzz: %zu bytes x 8 bits — %lld refused by name, %lld malformed, %lld records read, 0 divergences\n",
                 clean.size(), (long long) refused, (long long) damaged, (long long) read );
}

// ---------------------------------------------------------------------------

// THE SLACK IS ZERO (docs/SPEC-TABLES.md §3.4). A `string(N)` shorter than N
// and a `[..N]T` with unused slots are DECLARED bytes carrying no value, and
// what rides in them is the TEMPLATE'S ZEROS — never whatever the writer's own
// storage happened to hold past the used length or the live count.
//
// Copying the whole span instead is not a cosmetic difference: for an array of
// a type with declared defaults, the unused slots put the ELEMENT'S DEFAULT
// IMAGE on the wire, so a value nobody wrote rides as if somebody had; and for
// text, a field written twice with two different values does not compare equal
// on the second write. The Elixir port found both.
//
// THE CONTROL IS THE STAIN. The storage past the used length and the live count
// is filled with a byte that appears nowhere else in a clean record; the test
// first proves the stain IS there in the storage (or it would be checking
// nothing), then proves the WIRE carries none of it, and then proves that a
// whole-span copy of the same storage WOULD have carried it — which is the
// wrong behaviour, watched failing.
static void slack_case()
{
    tblfx1::FxRoot v;
    tblfx1::FxRootReset( v );
    v.keep = 11u;
    v.narrow = 22u;
    v.renamed = 33;
    v.gone = 44;
    v.nested.a = 55;
    v.nested.b = 66;
    std::memset( v.label, 0xAA, sizeof( v.label ) );
    v.label[0] = 'h';
    v.label[1] = 'i';
    v.label_length = 2;
    for ( int k = 0; k < 4; ++k ) { v.marks[k] = 0x5A5A5A5A; }
    v.marks[0] = 7;
    v.marks_count = 1;

    // the stain is really in the storage, so nothing below is vacuous
    check( (uint8_t) v.label[2] == 0xAAu, "CONTROL: the text slack really is stained in storage" );
    check( v.marks[1] == 0x5A5A5A5A, "CONTROL: the array slack really is stained in storage" );

    std::vector<uint8_t> file( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &v, 1, file.data(), (int64_t) file.size() ) == (int64_t) file.size(),
           "slack: the record saves" );
    const uint8_t * body = file.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
    const size_t body_bytes = (size_t) tblfx1::FxRootFixedBodyBytes;
    check( std::memchr( body, 0xAA, body_bytes ) == NULL,
           "SLACK IS ZERO: not one stained TEXT byte reached the wire" );
    check( std::memchr( body, 0x5A, body_bytes ) == NULL,
           "SLACK IS ZERO: not one stained ARRAY byte reached the wire" );

    // NEGATIVE CONTROL: the same storage copied WHOLE — which is what the
    // writer did before this fix — carries the stain, so the two checks above
    // discriminate and are not passing for some other reason.
    {
        uint8_t whole[sizeof( v.label )];
        std::memcpy( whole, v.label, sizeof( v.label ) );
        check( std::memchr( whole, 0xAA, sizeof( whole ) ) != NULL,
               "NEGATIVE CONTROL: a whole-span copy WOULD have carried the text stain" );
        int32_t marks[4];
        std::memcpy( marks, v.marks, sizeof( marks ) );
        check( marks[3] == 0x5A5A5A5A,
               "NEGATIVE CONTROL: a whole-span copy WOULD have carried the array stain" );
    }

    // and the record still reads back as itself: the used length and the live
    // count are what the reader validates, and both are inside the bound, so
    // nothing clamps.
    {
        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, file.data(), (int64_t) file.size(), plan.data(), 1024, NULL, &r ) == 1,
               "slack: the record reads" );
        check( back.label_length == 2 && back.label[0] == 'h' && back.label[1] == 'i' && back.label[2] == 0,
               "slack: the used length reads, and the buffer terminates at it" );
        check( back.marks_count == 1 && back.marks[0] == 7, "slack: the live count reads" );
        check( back.marks[1] == 0 && back.marks[2] == 0 && back.marks[3] == 0,
               "slack: an unused slot lands as the wire's zero and not as some writer's leftover" );
        check( r.clamped == 0 && !r.malformed && !r.refused, "slack: a clean read moves no counter" );
    }
}

// ---------------------------------------------------------------------------

// A `bytes(N)`'s DESTINATION ROW IS AN ARRAY'S (docs/SPEC-TABLES.md §3.4).
// `bytes(N)` rides as an array of u8, so the plan compiler lands it through the
// ARRAY case: the count goes to the row's aux and the elements to the row's
// dst. A text field's row is the other way round — dst the length, aux the
// buffer — and under that convention a `bytes(N)` hands the compiler a count
// destination that is the BUFFER'S FIRST FOUR BYTES and an element destination
// that is the LENGTH FIELD.
//
// THE NEGATIVE CONTROL IS THE OLD ROW, and it is built by swapping the two
// columns back on a copy of this build's own rows and compiling the same plan
// from the same layout. The row is found by its shape and not by its index —
// stride one and a live count is a `bytes(N)` and nothing else — so the control
// does not quietly stop pointing at it the day a field moves.
static void bytes_row_case()
{
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.blob[0] = 0xDE; one.blob[1] = 0xAD; one.blob[2] = 0xBE; one.blob[3] = 0xEF;
    one.blob_length = 4;
    std::vector<uint8_t> w( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "bytes row: FX1 save" );

    const size_t rows = sizeof( tblfx2::FxRootFixedDst ) / sizeof( tblfx2::FxRootFixedDst[0] );
    std::vector<tblfx2::TableFixedDst> swapped( tblfx2::FxRootFixedDst, tblfx2::FxRootFixedDst + rows );
    size_t found = rows;
    for ( size_t i = 0; i < rows; ++i )
    {
        if ( swapped[i].stride == 1u && swapped[i].counted != 0u ) { found = i; break; }
    }
    check( found != rows, "bytes row: the `bytes(N)` row is the one with stride one and a live count" );
    const uint32_t d = swapped[found].dst;
    swapped[found].dst = swapped[found].aux; // the TEXT convention, as it was
    swapped[found].aux = d;

    tblfx2::TableFixedLayoutView theirs;
    tblfx2::TableMessageReason why = tblfx2::layout_malformed;
    check( tblfx2::TableFixedParseLayout( tblfx1::FxRootFixedLayout, tblfx1::FxRootFixedLayoutBytes, theirs, why ),
           "bytes row: FX1's layout parses" );

    const uint8_t * body = w.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
    for ( int pass = 0; pass < 2; ++pass )
    {
        const tblfx2::TableFixedDst * rowset = pass == 0 ? tblfx2::FxRootFixedDst : swapped.data();
        std::vector<tblfx2::TableFixedEntry> plan( 2048 );
        int32_t guarded = 0;
        tblfx2::TableReport compile_report;
        uint32_t fill_at = 0;
        int32_t fill_count = 0;
        const int32_t made = tblfx2::TableFixedCompile( theirs, tblfx2::FxRootFixedLayout, (int32_t) tblfx2::FxRootFixedLayoutBytes,
                                                        rowset, tblfx2::FxRootFixedCover, tblfx2::FxRootFixedCoverCount,
                                                        plan.data(), 2048, &guarded, &fill_at, &fill_count, &compile_report );
        check( made > 0, "bytes row: the plan compiles either way — the rows are not what refuses" );
        // THE DESTINATION IS OVERSIZED ON PURPOSE. The wrong rows put an
        // element destination where the LENGTH FIELD is, and six bytes of
        // elements past a four-byte field is a step outside the storage — so
        // the control gives it room to be wrong, and what reports the bug is
        // the value that comes back rather than the sanitizer.
        std::vector<uint64_t> storage( sizeof( tblfx2::FxRoot ) / 8 + 16, 0 );
        tblfx2::FxRoot * back = (tblfx2::FxRoot *) (void *) storage.data();
        tblfx2::FxRootReset( *back );
        tblfx2::TableReport r;
        tblfx2::TableFixedRun( plan.data(), made, guarded, body, (uint8_t *) back, &r );
        const bool right = back->blob_length == 4 && back->blob[0] == 0xDE && back->blob[1] == 0xAD &&
                           back->blob[2] == 0xBE && back->blob[3] == 0xEF;
        if ( pass == 0 )
        {
            check( right, "BYTES(N): the array row lands the buffer in the buffer and the length in the length" );
        }
        else
        {
            check( !right, "NEGATIVE CONTROL: the text row really does write the count into the buffer" );
        }
    }
}

// ---------------------------------------------------------------------------

// TWO LANES, BECAUSE THEY ARE TWO FACTS (docs/SPEC-TABLES.md §3.4). A plan
// entry carries the ordinal the GUARD byte must hold for the entry to run, and
// the argument the entry's OWN OP takes — for a text entry, its flavour. They
// had one lane between them, and a `string(N)` under a union's arm could be
// guarded correctly or read with the right flavour and could not be both.
//
// UT1's string sits under the SECOND arm on purpose: a `string(N)`'s flavour is
// 1 and the second arm's ordinal is 2, so the collision does not merely lose a
// fact — it turns a byte string into a WIDE one, halving the bound and
// terminating two bytes at a time.
static void union_text_case()
{
    tblut1::UtRoot one;
    tblut1::UtRootReset( one );
    one.head = 42;
    one.tail = 99;
    one.pick.type = tblut1::PickType::B;   // the SECOND arm
    std::strcpy( one.pick.b.label, "seven77" ); // seven characters, so the slack is real
    one.pick.b.label_length = 7;
    one.pick.b.m = 1234;

    std::vector<uint8_t> w( (size_t) tblut1::UtRootFixedMeasure( 1 ) );
    check( tblut1::UtRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "UT1 save" );

    // THE IDENTITY PLAN: the guard holds the arm ordinal, the flavour is the
    // text op's own, and the two are read out of different lanes.
    {
        check( tblut1::UtRootFixedPlan[3].op == tblut1::kTableFixedText, "two lanes: the plan's fourth entry is the text" );
        check( tblut1::UtRootFixedPlan[3].arg == 2, "two lanes: arg is the SECOND arm's ordinal" );
        check( tblut1::UtRootFixedPlan[3].meta == tblut1::kTableFixedTextUtf8, "two lanes: meta is the utf8 flavour" );

        tblut1::UtRoot back;
        tblut1::TableReport r;
        std::vector<tblut1::TableFixedEntry> plan( 1024 );
        check( tblut1::UtRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "two lanes: the record reads" );
        check( back.pick.type == tblut1::PickType::B, "two lanes: the arm" );
        check( back.pick.b.label_length == 7 && std::strcmp( back.pick.b.label, "seven77" ) == 0,
               "two lanes: the arm's string(8), whole" );
        check( back.pick.b.m == 1234 && back.head == 42 && back.tail == 99, "two lanes: the rest of the record" );
        check( r.clamped == 0 && !r.malformed && !r.refused, "two lanes: a clean read moves no counter" );
    }

    // THE NEGATIVE CONTROL — the bug itself, watched failing. The flavour is
    // put back into the guard's lane, which is exactly what the one shared lane
    // did, and the SAME record read by the SAME loop comes out wrong: the arm's
    // ordinal 2 reads as kTableFixedTextWide, so the length is halved and the
    // terminator lands two bytes early.
    {
        std::vector<tblut1::TableFixedEntry> shared( tblut1::UtRootFixedPlan,
                                                     tblut1::UtRootFixedPlan + tblut1::UtRootFixedPlanCount );
        shared[3].meta = shared[3].arg; // ONE LANE, as it was
        tblut1::UtRoot wrong;
        tblut1::UtRootReset( wrong );
        tblut1::TableReport r;
        const uint8_t * body = w.data() + tblut1::kTableFixedHeaderBytes + 4 + tblut1::UtRootFixedLayoutBytes + 8;
        tblut1::TableFixedRun( shared.data(), tblut1::UtRootFixedPlanCount, tblut1::UtRootFixedPlanGuarded,
                               body, (uint8_t *) &wrong, &r );
        check( wrong.pick.b.label_length != 7 || std::strcmp( wrong.pick.b.label, "seven77" ) != 0,
               "NEGATIVE CONTROL: one shared lane really does read the arm's string wrong" );
    }

    // A COMPILED PLAN: UT2 inserted an arm IN THE MIDDLE, so the same string is
    // arm 3 here and arm 2 in the record. The guard must hold THEIR ordinal
    // while the tag this reader stores is MY ordinal, and the flavour is
    // neither number.
    {
        tblut2::UtRoot back;
        tblut2::TableReport r;
        std::vector<tblut2::TableFixedEntry> plan( 1024 );
        check( tblut2::UtRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "two lanes, compiled: the record reads" );
        check( back.pick.type == tblut2::PickType::B, "two lanes, compiled: the arm is remapped by NAME, not by ordinal" );
        check( back.pick.b.label_length == 7 && std::strcmp( back.pick.b.label, "seven77" ) == 0,
               "two lanes, compiled: the arm's string(8), whole" );
        check( back.pick.b.m == 1234 && back.head == 42 && back.tail == 99, "two lanes, compiled: the rest of the record" );
        check( r.clamped == 0 && !r.malformed && !r.refused, "two lanes, compiled: a clean read moves no counter" );
    }

    // and the other direction: UT1 reads a UT2 record whose arm is `b`, which
    // in that record is ordinal 3.
    {
        tblut2::UtRoot two;
        tblut2::UtRootReset( two );
        two.head = 7;
        two.tail = 8;
        two.pick.type = tblut2::PickType::B;
        std::strcpy( two.pick.b.label, "third" );
        two.pick.b.label_length = 5;
        two.pick.b.m = 555;
        std::vector<uint8_t> w2( (size_t) tblut2::UtRootFixedMeasure( 1 ) );
        check( tblut2::UtRootFixedSave( &two, 1, w2.data(), (int64_t) w2.size() ) == (int64_t) w2.size(), "UT2 save" );

        tblut1::UtRoot back;
        tblut1::TableReport r;
        std::vector<tblut1::TableFixedEntry> plan( 1024 );
        check( tblut1::UtRootFixedLoad( &back, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, NULL, &r ) == 1,
               "two lanes, back: the record reads" );
        check( back.pick.type == tblut1::PickType::B, "two lanes, back: arm 3 lands as arm 2, by name" );
        check( back.pick.b.label_length == 5 && std::strcmp( back.pick.b.label, "third" ) == 0,
               "two lanes, back: the arm's string(8), whole" );
        check( back.pick.b.m == 555, "two lanes, back: the arm's other field" );
    }
}

// ---------------------------------------------------------------------------

// TEXT UNDER AN ARM (docs/SPEC-TABLES.md §3.4, §15). UT1/UT2 is two-lanes /
// a slid ordinal. FU1/FU2 is the same hole on a TRAILING FIELD: FU1's second
// arm carries a string(8), FU2 appends `extra` so a read of FU1's bytes is a
// COMPILED plan rather than the identity one. The compiled read has to see
// "hello". Hash chooses the plan and nothing else; both reads go through
// FuRootFixedLoad.
static void text_under_arm_case()
{
    check( tblfu1::FuRootFixedHash != tblfu2::FuRootFixedHash,
           "text under an arm: FU2 extra changed the layout hash" );

    tblfu1::FuRoot v[2];
    tblfu1::FuRootReset( v[0] );
    v[0].flag = true;
    v[0].note = 44;
    v[0].note_present = true;
    v[0].pick.type = tblfu1::PickType::Labelled; // the SECOND arm
    v[0].pick.labelled.lead = 101;
    std::strcpy( v[0].pick.labelled.label, "hello" );
    v[0].pick.labelled.label_length = 5;
    v[0].pick.labelled.trail = 202;
    v[0].tail = 11;

    tblfu1::FuRootReset( v[1] );
    v[1].pick.type = tblfu1::PickType::Plain;
    v[1].pick.plain.n = 303;
    v[1].tail = 12;

    std::vector<uint8_t> w( (size_t) tblfu1::FuRootFixedMeasure( 2 ) );
    check( tblfu1::FuRootFixedSave( v, 2, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "FU1 save" );

    {
        tblfu1::FuRoot back[2];
        tblfu1::TableReport r;
        std::vector<tblfu1::TableFixedEntry> plan( 1024 );
        check( tblfu1::FuRootFixedLoad( back, 2, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 2,
               "text under an arm: the identity read takes both records" );
        check( back[0].pick.type == tblfu1::PickType::Labelled, "text under an arm: identity, the SECOND arm" );
        check( back[0].pick.labelled.lead == 101, "text under an arm: identity, the scalar BEFORE the text" );
        check( back[0].pick.labelled.label_length == 5 && std::strcmp( back[0].pick.labelled.label, "hello" ) == 0,
               "text under an arm: the IDENTITY read lands the text" );
        check( back[0].pick.labelled.trail == 202, "text under an arm: identity, the scalar AFTER the text" );
        check( back[0].flag && back[0].note_present && back[0].note == 44 && back[0].tail == 11,
               "text under an arm: identity, the rest of the labelled record" );
        check( back[1].pick.type == tblfu1::PickType::Plain && back[1].pick.plain.n == 303 && back[1].tail == 12,
               "text under an arm: identity, the FIRST arm as well" );
        check( r.clamped == 0 && !r.malformed && !r.refused,
               "text under an arm: identity, a clean read moves no counter" );
    }

    {
        tblfu2::FuRoot back[2];
        tblfu2::TableReport r;
        std::vector<tblfu2::TableFixedEntry> plan( 1024 );
        check( tblfu2::FuRootFixedLoad( back, 2, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 2,
               "text under an arm: the compiled read takes both records" );
        check( back[0].pick.type == tblfu2::PickType::Labelled, "text under an arm: compiled, the SECOND arm" );
        check( back[0].pick.labelled.lead == 101, "text under an arm: compiled, the scalar BEFORE the text" );
        check( back[0].pick.labelled.label_length == 5 && std::strcmp( back[0].pick.labelled.label, "hello" ) == 0,
               "text under an arm: the COMPILED read still sees the text" );
        check( back[0].pick.labelled.trail == 202, "text under an arm: compiled, the scalar AFTER the text" );
        check( back[0].flag && back[0].note_present && back[0].note == 44 && back[0].tail == 11,
               "text under an arm: compiled, the rest of the labelled record" );
        check( back[0].extra == 11,
               "text under an arm: the field FU1 does not carry took its declared default" );
        check( back[1].pick.type == tblfu2::PickType::Plain && back[1].pick.plain.n == 303 && back[1].tail == 12,
               "text under an arm: compiled, the FIRST arm as well" );
        check( back[1].extra == 11,
               "text under an arm: compiled, `extra` defaults on the FIRST arm too" );
        check( r.clamped == 0 && !r.malformed && !r.refused,
               "text under an arm: compiled, a clean read moves no counter" );
    }
}

// ---------------------------------------------------------------------------

// THE BOUNDS THE READ LOOP DOES NOT HOLD (docs/SPEC-TABLES.md §3.4). A fixed
// record is a positional image and the one read loop moves bytes: it asks
// nothing about what they mean. Two things a declaration bounds are therefore
// not the loop's at all — a RANGED SCALAR's declared min and max, and an
// ORDINAL's set: a union tag past the arm count, an enum ordinal past the
// enum's top value.
//
// They are held by STRAIGHT-LINE CODE in the generated decode, after the copy,
// and never by plan entries: an entry per bounded field is a test on every read
// of every record, which is the cost the identity plan exists to avoid. The
// pass runs over STORAGE, so ONE pass covers the identity plan and a plan
// compiled from a stranger's layout alike.
//
// THE POISON IS WRITTEN THROUGH THE WRITER, not poked into the bytes: the write
// side's own bounds are debug-only by rule and a range is not one of them, so a
// caller CAN put an out-of-range value on the wire and a reader is what has to
// answer for it.
static void bounds_case()
{
    // 1. A RANGED SCALAR, both ends, on the identity path.
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.keep = 1u;
    one.renamed = 5000;  // declared | min = 0, max = 1000
    one.gone = -7;       // and the low end of the same declaration
    one.nested.a = 111;
    one.nested.b = 222;
    std::vector<uint8_t> w( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "bounds: FX1 save" );
    {
        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "bounds: the record reads" );
        check( back.renamed == 1000, "RANGE: a value past max lands at max" );
        check( back.gone == 0, "RANGE: a value under min lands at min" );
        check( r.clamped == 2, "RANGE: two clamps, counted" );
        check( back.nested.a == 111 && back.nested.b == 222, "RANGE: an in-range neighbour is untouched" );
    }

    // THE NEGATIVE CONTROL: the read loop ALONE, with no straight-line pass
    // after it. The same record, the same plan, and the out-of-range value
    // survives — which is what says the bound is held by the pass and not by
    // something else that would have caught it anyway.
    {
        tblfx1::FxRoot loose;
        tblfx1::FxRootReset( loose );
        tblfx1::TableReport r;
        const uint8_t * body = w.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
        tblfx1::TableFixedRun( tblfx1::FxRootFixedPlan, tblfx1::FxRootFixedPlanCount, tblfx1::FxRootFixedPlanGuarded,
                               body, (uint8_t *) &loose, &r );
        check( loose.renamed == 5000 && loose.gone == -7,
               "NEGATIVE CONTROL: the loop alone really does leave an out-of-range value standing" );
        check( r.clamped == 0, "NEGATIVE CONTROL: and counts nothing" );
    }

    // 1b. THE SAME CONTROL ON A COMPILED PLAN. A plan compiled from this
    // build's OWN layout is the compiled path with nothing else moving, and it
    // must behave the same way in kind: the loop alone leaves the out-of-range
    // value standing, because NO PLAN OP CLAMPS — the pass after the loop is
    // the clamp, and it is the same pass for either plan.
    {
        tblfx1::TableFixedLayoutView parsed;
        tblfx1::TableMessageReason why = tblfx1::layout_malformed;
        check( tblfx1::TableFixedParseLayout( tblfx1::FxRootFixedLayout, tblfx1::FxRootFixedLayoutBytes, parsed, why ),
               "bounds, compiled-own: this build's layout parses" );
        std::vector<tblfx1::TableFixedEntry> compiled( 1024 );
        int32_t guarded = 0;
        tblfx1::TableReport cr;
        uint32_t fill_at = 0;
        int32_t fill_count = 0;
        const int32_t made = tblfx1::TableFixedCompile( parsed, tblfx1::FxRootFixedLayout, (int32_t) tblfx1::FxRootFixedLayoutBytes,
                                                        tblfx1::FxRootFixedDst, tblfx1::FxRootFixedCover, tblfx1::FxRootFixedCoverCount,
                                                        compiled.data(), 1024, &guarded, &fill_at, &fill_count, &cr );
        check( made > 0, "bounds, compiled-own: the plan compiles" );
        int32_t past_the_set = 0;
        for ( int32_t i = 0; i < made; ++i )
        {
            if ( compiled[(size_t) i].op > tblfx1::kTableFixedWidenF ) { past_the_set++; }
        }
        check( past_the_set == 0, "bounds, compiled-own: the ops are the whole set and none of them clamps" );

        tblfx1::FxRoot held;
        tblfx1::FxRootReset( held );
        tblfx1::TableReport r;
        const uint8_t * body = w.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
        tblfx1::TableFixedRun( compiled.data(), made, guarded, body, (uint8_t *) &held, &r );
        check( held.renamed == 5000 && held.gone == -7,
               "COMPILED, NEGATIVE CONTROL: the loop alone leaves an out-of-range value standing here too" );
        check( r.clamped == 0, "COMPILED, NEGATIVE CONTROL: and counts nothing" );

        tblfx1::FxRootFixedClamp( held, &r );
        check( held.renamed == 1000 && held.gone == 0,
               "COMPILED: THE PASS IS THE CLAMP — the same pass, after the compiled plan" );
        check( r.clamped == 2, "COMPILED: and it counts the same two the identity path counted" );
        check( held.nested.a == 111 && held.nested.b == 222, "COMPILED: an in-range neighbour is untouched" );
    }

    // 2. THE SAME NUMBERS THROUGH A COMPILED PLAN. FX2 reads the same record
    // through a plan compiled from FX1's layout, and the bound is the same one.
    {
        tblfx2::FxRoot back;
        tblfx2::TableReport r;
        std::vector<tblfx2::TableFixedEntry> plan( 2048 );
        check( tblfx2::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 2048, NULL, &r ) == 1,
               "bounds, compiled: the record reads" );
        check( back.renamed_to == 1000, "RANGE, compiled: the same clamp through a compiled plan" );
        check( r.clamped == 1, "RANGE, compiled: one clamp — `gone` is a field FX2 cannot name" );
    }

    // 3. A UNION TAG PAST THE ARM COUNT lands None and counts.
    {
        tblut1::UtRoot v;
        tblut1::UtRootReset( v );
        v.head = 1;
        v.tail = 2;
        v.pick.type = (tblut1::PickType) 7; // UT1 declares two arms
        std::vector<uint8_t> uw( (size_t) tblut1::UtRootFixedMeasure( 1 ) );
        check( tblut1::UtRootFixedSave( &v, 1, uw.data(), (int64_t) uw.size() ) == (int64_t) uw.size(), "bounds: UT1 save" );

        tblut1::UtRoot back;
        tblut1::TableReport r;
        std::vector<tblut1::TableFixedEntry> plan( 1024 );
        check( tblut1::UtRootFixedLoad( &back, 1, uw.data(), (int64_t) uw.size(), plan.data(), 1024, NULL, &r ) == 1,
               "bounds: the union record reads" );
        check( back.pick.type == tblut1::PickType::None, "ORDINAL: a tag past the arm count lands None" );
        check( r.clamped == 1, "ORDINAL: and counts as a clamp" );
        check( back.head == 1 && back.tail == 2, "ORDINAL: the rest of the record stands" );
    }

    // 4. AN ENUM ORDINAL PAST THE ENUM'S TOP VALUE lands None and counts.
    {
        tblv1::Cfg v;
        tblv1::CfgReset( v );
        v.grade = (tblv1::Grade) 9; // V1's Grade tops out at Gold
        std::vector<uint8_t> vw( (size_t) tblv1::CfgFixedMeasure( 1 ) );
        check( tblv1::CfgFixedSave( &v, 1, vw.data(), (int64_t) vw.size() ) == (int64_t) vw.size(), "bounds: V1 save" );

        tblv1::Cfg back;
        tblv1::TableReport r;
        std::vector<tblv1::TableFixedEntry> plan( 8192 );
        check( tblv1::CfgFixedLoad( &back, 1, vw.data(), (int64_t) vw.size(), plan.data(), 8192, NULL, &r ) == 1,
               "bounds: the enum record reads" );
        check( back.grade == tblv1::Grade::None, "ORDINAL: an ordinal past the enum's top value lands None" );
        check( r.clamped == 1, "ORDINAL: and counts as a clamp" );
    }

    // 5. LIVE COUNT, NEVER SLACK, ON BOTH PATHS. A counted array of ranged
    // integers, read twice: once through the identity plan and once through a
    // plan compiled from this build's own layout. Neither plan clamps, so the
    // loop alone leaves both live values standing on both paths, and the ONE
    // pass over storage holds both and counts two — the same two, because the
    // pass walks the LIVE count and never the slack behind it.
    {
        tblv1::Cfg v;
        tblv1::CfgReset( v );
        v.a = 5000;           // | min = 0, max = 1000
        v.items_count = 1;
        v.items[0] = 300;     // | min = 0, max = 255
        std::vector<uint8_t> vw( (size_t) tblv1::CfgFixedMeasure( 1 ) );
        check( tblv1::CfgFixedSave( &v, 1, vw.data(), (int64_t) vw.size() ) == (int64_t) vw.size(), "live-count: V1 save" );

        tblv1::Cfg identity;
        tblv1::TableReport ir;
        std::vector<tblv1::TableFixedEntry> iplan( 8192 );
        check( tblv1::CfgFixedLoad( &identity, 1, vw.data(), (int64_t) vw.size(), iplan.data(), 8192, NULL, &ir ) == 1,
               "live-count: identity reads" );
        check( identity.a == 1000 && identity.items[0] == 255, "live-count, identity: both live values clamp" );
        check( ir.clamped == 2, "live-count, identity: two clamps, slack never" );

        tblv1::TableFixedLayoutView parsed;
        tblv1::TableMessageReason why = tblv1::layout_malformed;
        check( tblv1::TableFixedParseLayout( tblv1::CfgFixedLayout, tblv1::CfgFixedLayoutBytes, parsed, why ),
               "live-count: this build's layout parses" );
        std::vector<tblv1::TableFixedEntry> compiled( 8192 );
        int32_t guarded = 0;
        tblv1::TableReport cr;
        uint32_t fill_at = 0;
        int32_t fill_count = 0;
        const int32_t made = tblv1::TableFixedCompile( parsed, tblv1::CfgFixedLayout, (int32_t) tblv1::CfgFixedLayoutBytes,
                                                       tblv1::CfgFixedDst, tblv1::CfgFixedCover, tblv1::CfgFixedCoverCount,
                                                       compiled.data(), 8192, &guarded, &fill_at, &fill_count, &cr );
        check( made > 0, "live-count: the plan compiles" );
        int32_t past_the_set = 0;
        for ( int32_t i = 0; i < made; ++i )
        {
            if ( compiled[(size_t) i].op > tblv1::kTableFixedWidenF ) { past_the_set++; }
        }
        check( past_the_set == 0, "live-count: a compiled plan carries no clamp op either" );

        tblv1::Cfg held;
        tblv1::CfgReset( held );
        tblv1::TableReport r;
        const uint8_t * body = vw.data() + tblv1::kTableFixedHeaderBytes + 4 + tblv1::CfgFixedLayoutBytes + 8;
        tblv1::TableFixedRun( compiled.data(), made, guarded, body, (uint8_t *) &held, &r );
        check( held.a == 5000 && held.items[0] == 300,
               "live-count, compiled loop: nothing in the plan held either value" );
        check( r.clamped == 0, "live-count, compiled loop: and it counted nothing" );

        tblv1::CfgFixedClamp( held, &r );
        check( held.a == 1000 && held.items[0] == 255, "live-count, compiled pass: both live values clamp" );
        check( r.clamped == 2, "live-count: identity and compiled count the same two" );
    }
}

// ---------------------------------------------------------------------------

// AN ABSENT OPTIONAL'S PAYLOAD IS THE TEMPLATE'S ZEROS (docs/SPEC-TABLES.md
// §3.4): the payload rides WHOLE whether or not it is present, and when the
// flag is 0 what rides is ZERO. It is one `if` in the writer, and what it buys
// is that a caller's untouched payload storage never reaches the wire — an
// absent optional is a hole in the record and not a window into the writer's
// memory.
//
// THE CONTROL IS THE STAIN, as it is for every other kind of slack: the payload
// storage is filled with a byte a clean record carries nowhere, the test proves
// the stain IS there, then proves the WIRE carries none of it, and then proves
// the SAME payload PRESENT does put those bytes on the wire — so the check is
// discriminating and not passing because the writer never wrote a payload at
// all.
static void absent_optional_case()
{
    tblp3::Chain v;
    tblp3::ChainReset( v );
    std::strcpy( v.name, "absent" );
    v.name_length = 6;
    v.link_present = false;
    v.link.value = 0x5A5A5A;
    std::memset( v.link.tag, 0x5A, sizeof( v.link.tag ) );
    v.link.tag_length = 5;
    check( (uint8_t) v.link.tag[0] == 0x5Au, "CONTROL: the absent payload really is stained in storage" );

    std::vector<uint8_t> w( (size_t) tblp3::ChainFixedMeasure( 1 ) );
    check( tblp3::ChainFixedSave( &v, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "absent optional: the record saves" );
    const uint8_t * body = w.data() + tblp3::kTableFixedHeaderBytes + 4 + tblp3::ChainFixedLayoutBytes + 8;
    const size_t body_bytes = (size_t) tblp3::ChainFixedBodyBytes;
    check( std::memchr( body, 0x5A, body_bytes ) == NULL,
           "ABSENT OPTIONAL: not one byte of the absent payload reached the wire" );

    // and the reader reads what the flag says, with the payload at its defaults
    {
        tblp3::Chain back;
        tblp3::TableReport r;
        std::vector<tblp3::TableFixedEntry> plan( 1024 );
        check( tblp3::ChainFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "absent optional: the record reads" );
        check( !back.link_present, "absent optional: the flag" );
        check( back.link.value == 0 && back.link.tag_length == 0, "absent optional: the payload reads as the wire's zeros" );
        check( r.clamped == 0 && !r.malformed && !r.refused, "absent optional: a clean read moves no counter" );
    }

    // THE DISCRIMINATING HALF: the same payload, PRESENT. Those bytes do reach
    // the wire, so the check above is about the flag and not about the writer
    // never having written a payload.
    {
        tblp3::Chain present = v;
        present.link_present = true;
        present.link.tag_length = 4;
        std::vector<uint8_t> pw( (size_t) tblp3::ChainFixedMeasure( 1 ) );
        check( tblp3::ChainFixedSave( &present, 1, pw.data(), (int64_t) pw.size() ) == (int64_t) pw.size(),
               "absent optional: the present twin saves" );
        const uint8_t * pbody = pw.data() + tblp3::kTableFixedHeaderBytes + 4 + tblp3::ChainFixedLayoutBytes + 8;
        check( std::memchr( pbody, 0x5A, (size_t) tblp3::ChainFixedBodyBytes ) != NULL,
               "NEGATIVE CONTROL: the SAME payload PRESENT really does reach the wire" );
    }
}

// ---------------------------------------------------------------------------

// THE ONE CONTENT RULE THE WIRE HAS (docs/SPEC-TABLES.md §3, §4), on this
// form's terms. A `string(N)`'s used bytes are well-formed UTF-8 with no zero
// among them — the same rule SPEC.md §4.7 puts on the packet wire, where the
// whole read refuses. HERE THE RECORD IS POSITIONAL, so the damage is one
// field's and the reader does not lose the others to it: THE FIELD READS ITS
// DECLARED DEFAULT, one `malformed` counts, and the rest of the record stands.
//
// The check runs over the USED LENGTH and over nothing else. The slack carries
// no meaning, and reading a whole declared bound to check bytes that mean
// nothing is the cost this form exists to avoid.
//
// THE POISON IS WRITTEN THROUGH THE WRITER, because it can be: the write side's
// bounds are the used length and the live count, both debug-only, and neither
// is a content rule. A caller CAN put a byte on this wire that is not text, and
// the reader is what has to answer for it.
static void text_content_case()
{
    struct Case { const char * what; const char * bytes; int32_t length; };
    const Case cases[] = {
        { "a lone 0xFF is no lead byte",            "\xFF",     1 },
        { "a truncated two-byte sequence",          "\xC3",     1 },
        { "a bare continuation byte",               "\x80",     1 },
        { "an overlong encoding of NUL",            "\xC0\x80", 2 },
        { "an INTERIOR NULL among the used bytes",  "a\0b",     3 },
    };
    for ( size_t k = 0; k < sizeof( cases ) / sizeof( cases[0] ); ++k )
    {
        tblfx1::FxRoot v;
        tblfx1::FxRootReset( v );
        v.keep = 1234u;
        v.nested.a = 7;
        std::memcpy( v.label, cases[k].bytes, (size_t) cases[k].length );
        v.label_length = cases[k].length;

        std::vector<uint8_t> w( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
        check( tblfx1::FxRootFixedSave( &v, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(),
               "text content: the record saves" );

        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "text content: the record reads" );
        check( r.malformed, cases[k].what );
        check( back.label_length == 2 && std::strcmp( back.label, "fx" ) == 0,
               "TEXT CONTENT: the damaged field reads its DECLARED DEFAULT" );
        check( back.keep == 1234u && back.nested.a == 7,
               "TEXT CONTENT: and the rest of the record stands — the damage is one field's" );
        check( !r.refused, "TEXT CONTENT: damage is not a refusal" );

        // THE NEGATIVE CONTROL: the read loop alone, with no content pass after
        // it. The same record, the same plan, and the bytes that are not text
        // stand in the storage with nothing said.
        if ( k == 0 )
        {
            tblfx1::FxRoot loose;
            tblfx1::FxRootReset( loose );
            tblfx1::TableReport r2;
            const uint8_t * body = w.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
            tblfx1::TableFixedRun( tblfx1::FxRootFixedPlan, tblfx1::FxRootFixedPlanCount, tblfx1::FxRootFixedPlanGuarded,
                                   body, (uint8_t *) &loose, &r2 );
            check( loose.label_length == 1 && (uint8_t) loose.label[0] == 0xFFu,
                   "NEGATIVE CONTROL: the loop alone really does leave a byte that is not text standing" );
            check( !r2.malformed, "NEGATIVE CONTROL: and says nothing about it" );
        }
    }

    // AND WELL-FORMED TEXT IS UNTOUCHED, which is what makes the cases above
    // discriminating: multi-byte UTF-8 inside the bound reads back whole.
    {
        tblfx1::FxRoot v;
        tblfx1::FxRootReset( v );
        std::memcpy( v.label, "\xC3\xA9t\xC3\xA9", 5 ); // "été"
        v.label_length = 5;
        std::vector<uint8_t> w( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
        tblfx1::FxRootFixedSave( &v, 1, w.data(), (int64_t) w.size() );

        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "text content: the well-formed record reads" );
        check( back.label_length == 5 && std::memcmp( back.label, "\xC3\xA9t\xC3\xA9", 5 ) == 0,
               "TEXT CONTENT: well-formed multi-byte UTF-8 rides whole" );
        check( !r.malformed && r.clamped == 0, "TEXT CONTENT: and moves no counter" );
    }
}

// THE GUARD IS COMPARED AT THE TAG'S WIDTH (docs/SPEC-TABLES.md §3.4). A
// union tag can be two bytes, and comparing only the first of them fires
// arm 1 on a foreign tag of 0x0101 — an ordinal no arm names. This case is
// a hand-built plan so the one-byte compare and the whole-width compare
// meet the same record; no schema has to declare two hundred and fifty six
// arms for the width to exist.
static void guard_width_case()
{
    uint8_t src[4] = { 0x01, 0x01, 0xAA, 0x00 };
    uint8_t dst[4];
    tblfx1::TableFixedEntry plan[2] = {};
    tblfx1::TableReport r = {};

    plan[0].src = 0; plan[0].dst = 0; plan[0].size = 2; plan[0].aux = 0;
    plan[0].guard = tblfx1::kTableFixedNoGuard;
    plan[0].op = tblfx1::kTableFixedCopy;
    plan[0].arg = 0; plan[0].meta = 0; plan[0].dstsize = 0; plan[0].sign = 0;
    plan[0].argw = 1;

    plan[1].src = 2; plan[1].dst = 2; plan[1].size = 1; plan[1].aux = 0;
    plan[1].guard = 0;
    plan[1].op = tblfx1::kTableFixedCopy;
    plan[1].arg = 1; plan[1].meta = 0; plan[1].dstsize = 0; plan[1].sign = 0;
    plan[1].argw = 2;

    std::memset( dst, 0, sizeof( dst ) );
    std::memset( &r, 0, sizeof( r ) );
    tblfx1::TableFixedRun( plan, 2, 1, src, dst, &r );
    check( dst[2] == 0, "GUARD WIDTH: tag 0x0101 at width 2 does not take arm 1" );

    src[1] = 0x00;
    std::memset( dst, 0, sizeof( dst ) );
    std::memset( &r, 0, sizeof( r ) );
    tblfx1::TableFixedRun( plan, 2, 1, src, dst, &r );
    check( dst[2] == 0xAA, "GUARD WIDTH: tag 0x0001 at width 2 takes arm 1" );

    // NEGATIVE CONTROL — the bug itself, watched failing. argw planted at 1
    // is the old one-byte compare, and the SAME 0x0101 record then fires arm 1.
    src[1] = 0x01;
    plan[1].argw = 1;
    std::memset( dst, 0, sizeof( dst ) );
    std::memset( &r, 0, sizeof( r ) );
    tblfx1::TableFixedRun( plan, 2, 1, src, dst, &r );
    check( dst[2] == 0xAA, "NEGATIVE CONTROL: a one-byte compare really does fire arm 1 on 0x0101" );
}

// THE COMPILE IS PAID ONCE PER PEER, NOT ONCE PER RECORD (§3.4). Two loads of
// the same foreign layout compile once; a third load with a different hash
// compiles once more; identity never increments the counter.
static void cache_case()
{
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.keep = 4242u;
    one.narrow = 40000u;
    one.renamed = 321;
    one.gone = 654;
    one.nested.a = 111;
    one.nested.b = 222;
    std::vector<uint8_t> w1( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w1.data(), (int64_t) w1.size() ) == (int64_t) w1.size(), "cache: FX1 save" );

    std::vector<tblfx2::TableFixedEntry> plan( 1024 );
    std::vector<tblfx2::TableFixedEntry> slab( (size_t) tblfx2::kTableFixedPlanCacheCapacity * 1024 );
    tblfx2::TableFixedPlanCache cache;
    tblfx2::TableFixedPlanCacheInit( cache, slab.data(), 1024 );

    tblfx2::FxRoot back;
    tblfx2::TableReport r;
    check( tblfx2::FxRootFixedLoad( &back, 1, w1.data(), (int64_t) w1.size(), plan.data(), 1024, &cache, &r ) == 1,
           "cache: first foreign load" );
    check( cache.compiles == 1, "cache: first foreign load compiled once" );
    check( cache.used == 1 && cache.slots[0].made == 1, "cache: the slot is marked made" );

    tblfx2::TableReport r2;
    check( tblfx2::FxRootFixedLoad( &back, 1, w1.data(), (int64_t) w1.size(), plan.data(), 1024, &cache, &r2 ) == 1,
           "cache: second foreign load" );
    check( cache.compiles == 1, "cache: two loads of the same layout compiled once" );

    std::vector<uint8_t> wB = w1;
    const uint32_t layout_bytes = tblfx2::TableFixedGet32( wB.data() + tblfx2::kTableFixedHeaderBytes );
    // the layout is a u32 count then the entries; flip a byte of entry 0's id
    wB[(size_t) tblfx2::kTableFixedHeaderBytes + 4 + 4] ^= 0x01;
    const uint64_t hashB = tblfx2::TableFixedHashOf( wB.data() + tblfx2::kTableFixedHeaderBytes + 4, layout_bytes );
    tblfx2::TableFixedPut64( wB.data() + tblfx2::kTableFixedHashAt, hashB );
    tblfx2::TableFixedPut64( wB.data() + tblfx2::kTableFixedHeaderBytes + 4 + layout_bytes, hashB );
    tblfx2::TableReport r3;
    check( tblfx2::FxRootFixedLoad( &back, 1, wB.data(), (int64_t) wB.size(), plan.data(), 1024, &cache, &r3 ) == 1,
           "cache: a different hash compiles once more" );
    check( cache.compiles == 2, "cache: the third load compiled once more" );
    check( cache.used == 2 && cache.slots[1].made == 1, "cache: the second slot is marked made" );

    tblfx2::FxRoot two;
    tblfx2::FxRootReset( two );
    two.keep = 1;
    std::vector<uint8_t> w2( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
    check( tblfx2::FxRootFixedSave( &two, 1, w2.data(), (int64_t) w2.size() ) == (int64_t) w2.size(), "cache: FX2 save" );
    tblfx2::TableReport r4;
    check( tblfx2::FxRootFixedLoad( &back, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, &cache, &r4 ) == 1,
           "cache: identity load" );
    check( cache.compiles == 2, "cache: identity never increments the compile counter" );
}

int main()
{
    std::printf( "FX1 FxRoot: body %lld, layout %lld, identity plan %d entries\n",
                 (long long) tblfx1::FxRootFixedBodyBytes, (long long) tblfx1::FxRootFixedLayoutBytes,
                 (int) tblfx1::FxRootFixedPlanCount );
    fx_case();
    v_case();
    p_case();
    negative_control();
    slack_case();
    union_text_case();
    text_under_arm_case();
    bytes_row_case();
    bounds_case();
    absent_optional_case();
    text_content_case();
    guard_width_case();
    cache_case();
    layout_validation();
    fuzz_case();
    // ==== BEGIN rowan/cpp-versioning-lists ====
    // THE LIST ROWS OF THE FIXED FORM'S VERSIONING LAW, in their own
    // translation unit (test/tables/versioning_lists.cpp) with their own
    // known-red list: NEW-READS-OLD and OLD-REFUSES-NEW, one lineage pair per
    // row (docs/FIXED-FORM-VERSIONING-TESTS.md).
    failures += versioning_lists_cases();
    // ==== END rowan/cpp-versioning-lists ====
    if ( failures != 0 ) { std::printf( "%d failure(s)\n", failures ); return 1; }
    std::printf( "fixed form: versioning conformance green\n" );
    return 0;
}
