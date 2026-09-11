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
#include "FH1Table.h"
#include "FH2Table.h"
#include "FG1Table.h"
#include "FE1Table.h"
#include "FE2Table.h"

// ---- rowan/cpp-versioning-numbers: BEGIN ----------------------------------
// THE VERSIONING LAW'S NUMBERS ROW (test/tables/versioning_numbers.cpp,
// docs/FIXED-FORM-VERSIONING-TESTS.md). One registration call, below in main,
// and it answers with its own failure count: its RED cases are printed by name
// and counted there, and do not fail this target.
int versioning_numbers_cases();
// ---- rowan/cpp-versioning-numbers: END ------------------------------------

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

    // 3. FX1 READS FX2 — OLD-REFUSES-NEW. The file's hash is not in FX1's known
    // set, so LOAD refuses layout_newer before any record (bill §4, §12.4).
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
        tblfx1::FxRootReset( back );
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfx1::FxRootFixedLoad( &back, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, NULL, &r );
        check( n < 0 && r.refused && r.reason == tblfx1::layout_newer,
               "newer writer: layout_newer before any record" );
        check( r.layout_hash == tblfx2::FxRootFixedHash,
               "newer writer: layout_newer carries the file's hash and nothing else" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && !r.malformed,
               "newer writer: REFUSE is total" );
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
    check( back.link_present && back.link.value == 500 && r.kind_mismatch == 0,
           "P3 reads P1: T into ?T lands present, payload exact (§12.8)" );
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
    tblfx1::TableFixedRun( tblfx1::FxRootFixedPlan, tblfx1::FxRootFixedPlanCount, tblfx1::FxRootFixedPlanGuarded, (const uint8_t *) tblfx1::FxRootFixedGuards, body, (uint8_t *) &wrong, &r );
    const bool intact = wrong.nested.a == 33 && wrong.nested.b == 44 && wrong.renamed == 808;
    check( !intact, "NEGATIVE CONTROL: the wrong plan must NOT reproduce the record" );

    // and the loader never takes that path: the hash is what selects the plan
    tblfx1::FxRoot right;
    tblfx1::TableReport r2;
    std::vector<tblfx1::TableFixedEntry> plan( 1024 );
    const int64_t n = tblfx1::FxRootFixedLoad( &right, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, NULL, &r2 );
    check( n < 0 && r2.refused && r2.reason == tblfx1::layout_newer,
           "NEGATIVE CONTROL: the loader refuses a newer hash, it does not compile a stranger's layout" );

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
        check( bad < 0 && r3.refused && r3.reason == tblfx1::layout_newer, "REFUSED BY NAME: a newer hash is layout_newer, not a walk name" );
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
        check( bad < 0 && r6.refused && r6.reason == tblfx1::layout_newer,
               "REFUSED BY NAME: a header hash this reader has never locked is layout_newer" );
        check( !r6.malformed, "REFUSED BY NAME: never damage" );
    }

    // A PLAN THAT DOES NOT FIT THE CALLER'S STORAGE IS A REFUSAL BY NAME, and
    // the codec allocates nothing to get around it.
    {
        tblfx1::FxRoot v;
        tblfx1::TableReport r5;
        tblfx1::TableFixedEntry tiny[1];
        const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, w2.data(), (int64_t) w2.size(), tiny, 1, NULL, &r5 );
        check( bad < 0 && r5.refused && r5.reason == tblfx1::layout_newer, "REFUSED BY NAME: a newer hash is refused before a plan is compiled" );
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
    // A KNOWN HASH whose layout BYTES differ is layout_malformed, one name for
    // all seven §1.1 breaks (bill §12.4). The unbroken file is this reader's OWN.
    tblfx1::FxRoot own;
    tblfx1::FxRootReset( own );
    std::vector<uint8_t> good( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &own, 1, good.data(), (int64_t) good.size() ) == (int64_t) good.size(),
           "layout validation: the unbroken file saves" );
    {
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
        refuses( f, tblfx1::layout_malformed, "RULE: the entry count fits the layout length exactly — known hash, layout_malformed" );
    }
    {
        // a count of ZERO is not a layout either: there is no root to walk
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( f.data() + kLayoutAt, 0u );
        refuses( f, tblfx1::layout_malformed, "RULE: an entry count of zero is not a layout — known hash, layout_malformed" );
    }

    // 2. EVERY KIND IS IN THE CLOSED SET. A fixed form's kind set is CLOSED, so
    //    a kind outside it means a NEWER FORM BYTE — a different form — and not
    //    a newer layout of this one. It is refused, never stepped over.
    {
        std::vector<uint8_t> f = good;
        entry_at( f, 1 )[8] = 200; // a kind no form byte this build carries defines
        refuses( f, tblfx1::layout_malformed, "RULE: a kind outside the closed set — known hash, layout_malformed" );
    }

    // 3. A KIND IS USED AS ITS DEFINITION ALLOWS — here, the ROOT is a table
    {
        std::vector<uint8_t> f = good;
        entry_at( f, 0 )[8] = 14; // an array as the root of a record
        refuses( f, tblfx1::layout_malformed, "RULE: the root entry is a TABLE — known hash, layout_malformed" );
    }

    // 4. A CONSTANT SIZE MATCHES ITS KIND
    {
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( entry_at( f, 1 ) + 9, 5u ); // a uint32 leaf in five bytes
        refuses( f, tblfx1::layout_malformed, "RULE: a constant size its kind does not admit — known hash, layout_malformed" );
    }
    {
        // and a TABLE's size is the SUM of its children's, not a number of its own
        std::vector<uint8_t> f = good;
        const uint32_t body = tblfx1::TableFixedGet32( entry_at( f, 0 ) + 9 );
        tblfx1::TableFixedPut32( entry_at( f, 0 ) + 9, body + 4u );
        refuses( f, tblfx1::layout_malformed, "RULE: a table's size is the sum of its fields' — known hash, layout_malformed" );
    }

    // 5. THE PRE-ORDER CHILD WALK CONSUMES EXACTLY THE ENTRIES
    {
        std::vector<uint8_t> f = good;
        const uint32_t kids = tblfx1::TableFixedGet32( entry_at( f, 0 ) + 13 );
        tblfx1::TableFixedPut32( entry_at( f, 0 ) + 13, kids + 1u );
        refuses( f, tblfx1::layout_malformed, "RULE: the tree runs out of layout — known hash, layout_malformed" );
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
        tblfx1::TableFixedPut64( f.data() + tblfx1::kTableFixedHashAt, tblfx1::FxRootFixedHash );
        refuses( f, tblfx1::layout_malformed, "RULE: the layout outlasts the tree — known hash, layout_malformed" );
    }

    // 6. THE TOTAL RECORD SIZE IS WITHIN 65536 AND DOES NOT OVERFLOW
    {
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( entry_at( f, 0 ) + 9, 65537u );
        refuses( f, tblfx1::layout_malformed, "RULE: a record size past 65536 — known hash, layout_malformed" );
    }
    {
        // A SIZE THAT WOULD WRAP. The children's sizes are summed in 64 bits
        // precisely so a u32 that overflows is CAUGHT rather than wrapped into
        // a small number that then agrees with a parent.
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( entry_at( f, 1 ) + 9, 0xFFFFFFFFu );
        refuses( f, tblfx1::layout_malformed, "RULE: a size that would overflow the sum — known hash, layout_malformed" );
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
        tblfx1::TableFixedPut64( f.data() + tblfx1::kTableFixedHashAt, tblfx1::FxRootFixedHash );
        refuses( f, tblfx1::layout_malformed, "RULE: a nesting depth past the walk's own bound — known hash, layout_malformed" );
    }

    // AND THE RESIDUE: bytes that are not a layout at all, which is the one
    // case the seven named rules never reach.
    {
        // 20 + L overruns the file: a length that is not a layout (algorithm §5.3 / §2).
        std::vector<uint8_t> f( (size_t) tblfx1::kTableFixedHeaderBytes + 4, 0 );
        f[0] = 3;
        tblfx1::TableFixedPut32( f.data() + tblfx1::kTableFixedHeaderBytes, 100u );
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
        tblfx2::TableFixedRun( plan.data(), made, guarded, (const uint8_t *) plan.data(), body, (uint8_t *) back, &r );
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
        check( tblut1::UtRootFixedPlan[3].gcount == 1, "a chain: the text entry is under exactly one union" );
        check( tblut1::TableFixedGuardArg( tblut1::UtRootFixedGuards[tblut1::UtRootFixedPlan[3].guards / sizeof( tblut1::TableFixedGuard )] ) == 2,
               "a chain: the link holds the SECOND arm's ordinal" );
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
        shared[3].meta = (uint8_t) tblut1::TableFixedGuardArg( tblut1::UtRootFixedGuards[shared[3].guards / sizeof( tblut1::TableFixedGuard )] ); // ONE LANE, as it was
        tblut1::UtRoot wrong;
        tblut1::UtRootReset( wrong );
        tblut1::TableReport r;
        const uint8_t * body = w.data() + tblut1::kTableFixedHeaderBytes + 4 + tblut1::UtRootFixedLayoutBytes + 8;
        tblut1::TableFixedRun( shared.data(), tblut1::UtRootFixedPlanCount, tblut1::UtRootFixedPlanGuarded,
                               (const uint8_t *) tblut1::UtRootFixedGuards, body, (uint8_t *) &wrong, &r );
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
        check( tblut1::UtRootFixedLoad( &back, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, NULL, &r ) < 0 &&
               r.refused && r.reason == tblut1::layout_newer,
               "two lanes, back: OLD-REFUSES-NEW — layout_newer, the hash and nothing else" );
        check( r.layout_hash == tblut2::UtRootFixedHash, "two lanes, back: the file's hash" );
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

// OWED 7: an arm inside an arm answers to the OUTER tag (#876 card 13).
// Inner is an arm of Outer. A compiled read of FH1 through FH2 must land the
// inner arm, and a foreign outer arm must not be decoded as this inner union.
static void owed7_nested_union_outer_tag_case()
{
    check( tblfh1::NestRootFixedHash != tblfh2::NestRootFixedHash,
           "owed 7: FH2 extra changed the layout hash" );

    tblfh1::NestRoot v[2];
    tblfh1::NestRootReset( v[0] );
    v[0].pick.type = tblfh1::OuterType::Wrap;
    v[0].pick.wrap.inner.type = tblfh1::InnerType::Leaf;
    v[0].pick.wrap.inner.leaf.n = 42;
    v[0].tail = 7;

    tblfh1::NestRootReset( v[1] );
    v[1].pick.type = tblfh1::OuterType::Other;
    v[1].pick.other.m = 99;
    v[1].tail = 8;

    std::vector<uint8_t> w( (size_t) tblfh1::NestRootFixedMeasure( 2 ) );
    check( tblfh1::NestRootFixedSave( v, 2, w.data(), (int64_t) w.size() ) == (int64_t) w.size(),
           "owed 7: FH1 save" );

    {
        tblfh2::NestRoot back[2];
        tblfh2::TableReport r;
        std::vector<tblfh2::TableFixedEntry> plan( 1024 );
        check( tblfh2::NestRootFixedLoad( back, 2, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 2,
               "owed 7: the compiled read takes both records" );
        check( back[0].pick.type == tblfh2::OuterType::Wrap,
               "owed 7: compiled, the WRAP arm of the outer union" );
        {
            char what[256];
            std::snprintf( what, sizeof( what ),
                           "owed 7: compiled, the inner union answers to the OUTER tag (inner.type=%d, want Leaf=%d)",
                           (int) back[0].pick.wrap.inner.type, (int) tblfh2::InnerType::Leaf );
            check( back[0].pick.wrap.inner.type == tblfh2::InnerType::Leaf, what );
        }
        check( back[0].pick.wrap.inner.leaf.n == 42, "owed 7: compiled, the inner leaf lands" );
        check( back[0].tail == 7 && back[0].extra == 11, "owed 7: compiled, tail and extra" );
        check( back[1].pick.type == tblfh2::OuterType::Other,
               "owed 7: a foreign outer arm is not decoded as this inner union" );
        check( back[1].pick.other.m == 99 && back[1].tail == 8 && back[1].extra == 11,
               "owed 7: compiled, the other arm and tail land" );
        check( r.clamped == 0 && !r.malformed && !r.refused,
               "owed 7: a clean read moves no counter" );
    }
}

// OWED 8: a width-8 ordinal is read whole through a 64-bit temporary. A
// uint32_t temp truncated a 64-bit ordinal whose low four bytes were zero.
static void owed8_width8_ordinal_read_whole_case()
{
    alignas( tblfu1::TableFixedEntry ) uint8_t blob[512];
    std::memset( blob, 0, sizeof( blob ) );
    tblfu1::TableFixedEntry * plan = reinterpret_cast<tblfu1::TableFixedEntry *>( blob );
    const uint32_t map_at = 256;
    uint16_t * map = reinterpret_cast<uint16_t *>( blob + map_at );
    map[0] = 1;
    map[1] = 1;
    plan[0].op = tblfu1::kTableFixedOrdinal;
    plan[0].src = 0;
    plan[0].dst = 0;
    plan[0].size = 8;
    plan[0].dstsize = 8;
    plan[0].aux = map_at;
    plan[0].gcount = 0; // no chain: this entry belongs to no arm

    uint8_t src[8] = { 0, 0, 0, 0, 1, 0, 0, 0 }; // 2^32, needs all eight bytes
    uint8_t dst[8];
    std::memset( dst, 0xAB, sizeof( dst ) );
    tblfu1::TableReport r;
    tblfu1::TableFixedRun( plan, 1, 1, blob, src, dst, &r );
    uint64_t landed = 0;
    std::memcpy( &landed, dst, 8 );
    check( landed == 0, "owed 8: a width-8 ordinal of 2^32 is past the writer's one variant" );
    check( r.clamped >= 1, "owed 8: COUNT clamped — a uint32_t temp would have read 0 and counted nothing" );
}

// OWED 10: a record whose hash names nothing writes nothing to the caller's
// storage. Poison it first; REFUSE is total.
static void owed10_no_layout_writes_nothing_case()
{
    tblfu1::FuRoot v;
    tblfu1::FuRootReset( v );
    v.tail = 11;
    v.pick.type = tblfu1::PickType::Plain;
    v.pick.plain.n = 303;
    std::vector<uint8_t> w( (size_t) tblfu1::FuRootFixedMeasure( 1 ) );
    check( tblfu1::FuRootFixedSave( &v, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(),
           "owed 10: FU1 save" );
    const size_t rec = (size_t) tblfu1::kTableFixedHeaderBytes + 4
        + (size_t) tblfu1::FuRootFixedLayoutBytes;
    tblfu1::TableFixedPut64( w.data() + rec, 0xDEADBEEFCAFEBABEull );

    tblfu2::FuRoot back;
    std::memset( &back, 0xAB, sizeof( back ) );
    const tblfu2::FuRoot poison = back;
    tblfu2::TableReport r;
    std::vector<tblfu2::TableFixedEntry> plan( 1024 );
    const int64_t n = tblfu2::FuRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(),
                                               plan.data(), 1024, NULL, &r );
    check( n < 0 && r.refused && r.reason == tblfu2::no_layout,
           "owed 10: a record hash naming nothing is no_layout" );
    check( std::memcmp( &back, &poison, sizeof( back ) ) == 0,
           "owed 10: REFUSE writes nothing; the caller's storage is still poison" );
}

// OWED 13: a forged ordinal past the writer's variant count COUNT clamped on
// the COMPILED plan. FE2 reads FE1; Grade's top on the writer is Gold = 3.
static void owed13_compiled_ordinal_counts_clamped_case()
{
    tblfe1::FeRoot v;
    tblfe1::FeRootReset( v );
    v.grade = tblfe1::Grade::Gold;
    v.tail = 7;
    std::vector<uint8_t> w( (size_t) tblfe1::FeRootFixedMeasure( 1 ) );
    check( tblfe1::FeRootFixedSave( &v, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(),
           "owed 13: FE1 save" );
    const size_t rec = (size_t) tblfe1::kTableFixedHeaderBytes + 4
        + (size_t) tblfe1::FeRootFixedLayoutBytes;
    w[rec + 8] = 4; // past Gold, which FE1 has no name for
    {
        tblfe2::FeRoot back;
        tblfe2::FeRootReset( back );
        tblfe2::TableReport r;
        std::vector<tblfe2::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfe2::FeRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(),
                                                   plan.data(), 1024, NULL, &r );
        check( n == 1, "owed 13: the forged file reads on the compiled plan" );
        check( back.grade == tblfe2::Grade::None,
               "owed 13: ordinal 4 lands None against the WRITER's 3 variants" );
        check( r.clamped >= 1, "owed 13: COUNT clamped on the compiled plan" );
        check( back.tail == 7, "owed 13: the scalar after the enum stands" );
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
                               (const uint8_t *) tblfx1::FxRootFixedGuards,
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
        tblfx1::TableFixedRun( compiled.data(), made, guarded, (const uint8_t *) compiled.data(), body, (uint8_t *) &held, &r );
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
        tblv1::TableFixedRun( compiled.data(), made, guarded, (const uint8_t *) compiled.data(), body, (uint8_t *) &held, &r );
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
                                   (const uint8_t *) tblfx1::FxRootFixedGuards,
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
    tblfx1::TableFixedGuard pool[4] = {};
    tblfx1::TableReport r = {};

    // ONE LINK, the tag at offset 0, arm 1, compared at TWO bytes.
    pool[0].guard = 0; pool[0].arg_lo = 1; pool[0].arg_hi = 0; pool[0].argw = 2;

    plan[0].src = 0; plan[0].dst = 0; plan[0].size = 2; plan[0].aux = 0;
    plan[0].op = tblfx1::kTableFixedCopy;
    plan[0].meta = 0; plan[0].dstsize = 0; plan[0].sign = 0;
    plan[0].gcount = 0;

    plan[1].src = 2; plan[1].dst = 2; plan[1].size = 1; plan[1].aux = 0;
    plan[1].op = tblfx1::kTableFixedCopy;
    plan[1].meta = 0; plan[1].dstsize = 0; plan[1].sign = 0;
    plan[1].guards = 0; plan[1].gcount = 1;

    std::memset( dst, 0, sizeof( dst ) );
    std::memset( &r, 0, sizeof( r ) );
    tblfx1::TableFixedRun( plan, 2, 1, (const uint8_t *) pool, src, dst, &r );
    check( dst[2] == 0, "GUARD WIDTH: tag 0x0101 at width 2 does not take arm 1" );

    src[1] = 0x00;
    std::memset( dst, 0, sizeof( dst ) );
    std::memset( &r, 0, sizeof( r ) );
    tblfx1::TableFixedRun( plan, 2, 1, (const uint8_t *) pool, src, dst, &r );
    check( dst[2] == 0xAA, "GUARD WIDTH: tag 0x0001 at width 2 takes arm 1" );

    // NEGATIVE CONTROL — the bug itself, watched failing. argw planted at 1
    // is the old one-byte compare, and the SAME 0x0101 record then fires arm 1.
    src[1] = 0x01;
    pool[0].argw = 1;
    std::memset( dst, 0, sizeof( dst ) );
    std::memset( &r, 0, sizeof( r ) );
    tblfx1::TableFixedRun( plan, 2, 1, (const uint8_t *) pool, src, dst, &r );
    check( dst[2] == 0xAA, "NEGATIVE CONTROL: a one-byte compare really does fire arm 1 on 0x0101" );

    // 6: the ordinal IS FULL WIDTH (bill §12.7). A two-byte tag of 256 is arm
    // 256, and a byte lane would wrap it to 0, so the entry would never run.
    src[0] = 0x00; src[1] = 0x01; src[2] = 0xAA; src[3] = 0x00;
    pool[0].arg_lo = 256; pool[0].argw = 2;
    std::memset( dst, 0, sizeof( dst ) );
    r = {};
    tblfx1::TableFixedRun( plan, 2, 1, (const uint8_t *) pool, src, dst, &r );
    check( dst[2] == 0xAA, "ARG LANE: tag 256 at width 2 takes arm 256" );
    src[1] = 0x00;
    std::memset( dst, 0, sizeof( dst ) );
    r = {};
    tblfx1::TableFixedRun( plan, 2, 1, (const uint8_t *) pool, src, dst, &r );
    check( dst[2] == 0, "ARG LANE: tag 0 does not take arm 256" );
}

// THE CHAIN HAS NO LENGTH BOUND, AND A FORGED INNER TAG UNDER AN UNSELECTED
// OUTER ARM LANDS NOTHING AT EVERY DEPTH (§5.9). Hand-built so the depth is the
// variable and nothing else is: three tags, three links, and the entry rides
// only when all three hold. Two lanes could hold two of these conditions and
// had to refuse the third by name; a chain holds ten as readily as three.
static void guard_chain_depth_case()
{
    uint8_t src[8] = {};
    uint8_t dst[4];
    tblfx1::TableFixedEntry plan[1] = {};
    tblfx1::TableFixedGuard pool[3] = {};
    tblfx1::TableReport r = {};

    // OUTERMOST FIRST: the tag at 0 must be 1, the tag at 1 must be 2, the tag
    // at 2 must be 3 — three nested unions, each one byte wide.
    pool[0].guard = 0; pool[0].arg_lo = 1; pool[0].argw = 1;
    pool[1].guard = 1; pool[1].arg_lo = 2; pool[1].argw = 1;
    pool[2].guard = 2; pool[2].arg_lo = 3; pool[2].argw = 1;

    plan[0].src = 3; plan[0].dst = 0; plan[0].size = 1; plan[0].op = tblfx1::kTableFixedCopy;
    plan[0].guards = 0; plan[0].gcount = 3;

    src[0] = 1; src[1] = 2; src[2] = 3; src[3] = 0xAA;
    std::memset( dst, 0, sizeof( dst ) );
    r = {};
    tblfx1::TableFixedRun( plan, 1, 0, (const uint8_t *) pool, src, dst, &r );
    check( dst[0] == 0xAA, "A CHAIN OF THREE: every link holds, so the entry rides" );

    // THE FORGED INNER TAG: the two inner tags still name this arm, and the
    // OUTER one does not. Nothing lands — the chain is tested in order and the
    // first mismatch is the end of it.
    src[0] = 2;
    std::memset( dst, 0, sizeof( dst ) );
    r = {};
    tblfx1::TableFixedRun( plan, 1, 0, (const uint8_t *) pool, src, dst, &r );
    check( dst[0] == 0, "A CHAIN OF THREE: a forged inner tag under an unselected outer arm lands nothing" );

    // AND THE MIDDLE ONE, which is the condition a two-lane shape dropped: the
    // outermost and the innermost hold, the one between them does not.
    src[0] = 1; src[1] = 9;
    std::memset( dst, 0, sizeof( dst ) );
    r = {};
    tblfx1::TableFixedRun( plan, 1, 0, (const uint8_t *) pool, src, dst, &r );
    check( dst[0] == 0, "A CHAIN OF THREE: the middle tag is a condition too" );

    // A FOURTH LINK COSTS ONE MORE POOL LINK AND NOTHING ELSE.
    tblfx1::TableFixedGuard four[4] = {};
    four[0].guard = 0; four[0].arg_lo = 1; four[0].argw = 1;
    four[1].guard = 1; four[1].arg_lo = 2; four[1].argw = 1;
    four[2].guard = 2; four[2].arg_lo = 3; four[2].argw = 1;
    four[3].guard = 3; four[3].arg_lo = 4; four[3].argw = 1;
    plan[0].src = 4; plan[0].gcount = 4;
    src[0] = 1; src[1] = 2; src[2] = 3; src[3] = 4; src[4] = 0xBB;
    std::memset( dst, 0, sizeof( dst ) );
    r = {};
    tblfx1::TableFixedRun( plan, 1, 0, (const uint8_t *) four, src, dst, &r );
    check( dst[0] == 0xBB, "A CHAIN OF FOUR: four nested unions, four links, no bound" );
    src[3] = 9;
    std::memset( dst, 0, sizeof( dst ) );
    r = {};
    tblfx1::TableFixedRun( plan, 1, 0, (const uint8_t *) four, src, dst, &r );
    check( dst[0] == 0, "A CHAIN OF FOUR: the fourth tag is a condition too" );
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
    const int64_t nB = tblfx2::FxRootFixedLoad( &back, 1, wB.data(), (int64_t) wB.size(), plan.data(), 1024, &cache, &r3 );
    check( nB < 0 && r3.refused && r3.reason == tblfx2::layout_newer,
           "cache: a hash in no lineage is layout_newer, not a second compile" );
    check( cache.compiles == 1, "cache: a stranger's hash does not compile" );

    tblfx2::FxRoot two;
    tblfx2::FxRootReset( two );
    two.keep = 1;
    std::vector<uint8_t> w2( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
    check( tblfx2::FxRootFixedSave( &two, 1, w2.data(), (int64_t) w2.size() ) == (int64_t) w2.size(), "cache: FX2 save" );
    tblfx2::TableReport r4;
    check( tblfx2::FxRootFixedLoad( &back, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, &cache, &r4 ) == 1,
           "cache: identity load" );
    check( cache.compiles == 1, "cache: identity never increments the compile counter" );
}

// ---------------------------------------------------------------------------

// W10: EVERY COMPILED ENTRY IS BOUNDED BY THE WRITER'S OWN DECLARED RECORD SIZE
// (docs/FIXED-FORM-ALGORITHM.md §4.2, fix 3). The plan's source offsets are
// arithmetic over sizes a WRITER wrote down, so a layout that passes every
// rule of §1.1 and still names a byte past `root.size` is refused WHOLE and
// never partly compiled, under `layout_record_too_large`.
//
// THE LAYOUT HERE IS INTERNALLY VALID: hand-built out of this build's own
// field ids, passing all seven rules. After COMPILE-from-lock (#916), LOAD
// never parses a stranger — unknown hash is `layout_newer` — so the bound
// is asserted on `TableFixedCompile` directly, which is the path a known
// older writer still takes. A Load of the same bytes still refuses, and
// sets nothing.
//
// WHERE THE OVERRUN COMES FROM, precisely: an ARRAY entry's admitted size is
// `size % elem == 0` OR `4 + n*elem`, so a size of ZERO is a valid bare array
// of no elements — and the compiler takes the head from the READER's own row
// (`d.counted`, internal/codegen/cpptable/fixedruntime.go:995), so a reader
// whose field carries a live count subtracts four from zero. The elements then
// land at `their_at + 4`, past a record that ends at four.
static void record_bound_case()
{
    // the ids are READ OUT of this build's own layout rather than spelled as
    // hashes, so the case does not quietly stop naming the fields it means
    const tblfx1::TableFixedLayoutView mine = { tblfx1::FxRootFixedLayout, (int32_t) ( ( tblfx1::FxRootFixedLayoutBytes - 4 ) / 17 ) };
    const uint64_t root_id = tblfx1::TableFixedEntryAt( mine, 0 ).id;
    uint64_t keep_id = 0, blob_id = 0, blob_elem_id = 0, label_id = 0;
    for ( int32_t i = 1; i < mine.count; ++i )
    {
        const tblfx1::TableFixedLayoutEntry e = tblfx1::TableFixedEntryAt( mine, i );
        if ( e.kind == 8 && e.size == 4 && keep_id == 0 ) { keep_id = e.id; }          // `keep`, a uint32
        if ( e.kind == 12 && label_id == 0 ) { label_id = e.id; }                      // `label`, a string(8)
        if ( e.kind == 14 && i + 1 < mine.count )                                      // `blob`, bytes(6): an array of u8
        {
            const tblfx1::TableFixedLayoutEntry el = tblfx1::TableFixedEntryAt( mine, i + 1 );
            if ( el.kind == 6 && el.size == 1 ) { blob_id = e.id; blob_elem_id = el.id; }
        }
    }
    check( root_id != 0 && keep_id != 0 && blob_id != 0 && label_id != 0,
           "W10: the ids this case names are the ids this build's own layout carries" );

    // 1. AN ENTRY WHOSE `src + size` REACHES PAST `root.size`
    {
        std::vector<uint8_t> layout;
        layout.resize( 4 );
        tblfx1::TableFixedPut32( layout.data(), 4u );
        put_entry( layout, root_id, 13u, 4u, 2u );      // a record body of FOUR bytes
        put_entry( layout, keep_id, 8u, 4u, 0u );       // the four bytes, at 0
        put_entry( layout, blob_id, 14u, 0u, 1u );      // an array of NO bytes, at 4
        put_entry( layout, blob_elem_id, 6u, 1u, 0u );
        tblfx1::TableFixedLayoutView parsed;
        tblfx1::TableMessageReason why = tblfx1::layout_malformed;
        check( tblfx1::TableFixedParseLayout( layout.data(), (int64_t) layout.size(), parsed, why ),
               "W10: the layout passes all seven rules of §1.1, so the refusal below is the BOUND" );
        tblfx1::TableReport cr;
        std::vector<tblfx1::TableFixedEntry> cplan( 1024 );
        int32_t guarded = 0;
        uint32_t fill_at = 0;
        int32_t fill_count = 0;
        const int32_t made = tblfx1::TableFixedCompile( parsed, tblfx1::FxRootFixedLayout, (int32_t) tblfx1::FxRootFixedLayoutBytes,
                                                       tblfx1::FxRootFixedDst, tblfx1::FxRootFixedCover, tblfx1::FxRootFixedCoverCount,
                                                       cplan.data(), 1024, &guarded, &fill_at, &fill_count, &cr );
        check( made == -2, "W10: COMPILE answers -2 (hostile) for an entry past the writer's record size" );
        check( cr.reason == tblfx1::layout_record_too_large,
               "W10: COMPILE names layout_record_too_large for an entry past the writer's record size" );
        std::vector<uint8_t> f = file_of( layout );
        tblfx1::FxRoot v;
        tblfx1::FxRootReset( v );
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfx1::FxRootFixedLoad( &v, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r );
        check( n < 0 && r.refused, "W10: LOAD of the same bytes is REFUSED (hash selects; a stranger is layout_newer)" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed,
               "W10: the refusal sets nothing and counts nothing" );
        check( v.keep == 7u && v.blob_length == 0, "W10: NOT PARTLY COMPILED — no entry of the plan ran" );
    }

    // 2. THE BOUNDARY ITSELF STANDS. The same shape one byte short of the
    //    overrun — a text entry whose length word and payload END EXACTLY at
    //    `root.size` — is a layout with nothing wrong with it, and a bound that
    //    refused it would be a bound that refuses the wire.
    //
    //    AND WHAT A PORTER MUST KNOW: on this reference the OTHER TWO halves of
    //    the bound — a text entry's `+4` and a `guard` offset — are UNREACHABLE
    //    from a valid layout, because §1.1 rule 3 makes a table's size the sum
    //    of its children's and every per-kind rule bounds a child's span inside
    //    its parent's. A text entry's whole span IS its entry size (`>= 4`), and
    //    a union's guard is its own offset with a tag width of `size - widest`.
    //    So they are a defence against a HOLE IN RULE 3, and a port whose
    //    validation is weaker than this one's needs all three.
    {
        std::vector<uint8_t> layout;
        layout.resize( 4 );
        tblfx1::TableFixedPut32( layout.data(), 3u );
        put_entry( layout, root_id, 13u, 16u, 2u );     // four bytes, then a string(8)'s twelve
        put_entry( layout, keep_id, 8u, 4u, 0u );
        put_entry( layout, label_id, 12u, 12u, 0u );    // src 4, +4 the length word, 12 bytes to 16
        tblfx1::TableFixedLayoutView parsed;
        tblfx1::TableMessageReason why = tblfx1::layout_malformed;
        check( tblfx1::TableFixedParseLayout( layout.data(), (int64_t) layout.size(), parsed, why ),
               "W10: the boundary layout passes all seven rules of §1.1" );
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        int32_t guarded = 0;
        uint32_t fill_at = 0;
        int32_t fill_count = 0;
        const int32_t made = tblfx1::TableFixedCompile( parsed, tblfx1::FxRootFixedLayout, (int32_t) tblfx1::FxRootFixedLayoutBytes,
                                                       tblfx1::FxRootFixedDst, tblfx1::FxRootFixedCover, tblfx1::FxRootFixedCoverCount,
                                                       plan.data(), 1024, &guarded, &fill_at, &fill_count, &r );
        check( made >= 0 && !r.refused && !r.malformed, "W10: a text entry ending EXACTLY at root.size is not a refusal" );
        uint8_t body[16];
        std::memset( body, 0, sizeof( body ) );
        tblfx1::TableFixedPut32( body, 4242u );          // keep
        tblfx1::TableFixedPut32( body + 4, 2u );          // the label's length
        body[8] = 'h'; body[9] = 'i';
        tblfx1::FxRoot v;
        tblfx1::FxRootReset( v );
        tblfx1::TableFixedRun( plan.data(), made, guarded, (const uint8_t *) plan.data(), body, (uint8_t *) (void *) &v, &r );
        check( v.keep == 4242u && v.label_length == 2 && v.label[0] == 'h' && v.label[1] == 'i',
               "W10: and it reads — the bound admits the last byte of the record" );
    }
}

// ---------------------------------------------------------------------------

// C1/C2: THE COUNT CLAMP (docs/FIXED-FORM-ALGORITHM.md §4.5). A counted array's
// count is four bytes a STRANGER wrote: `v := SLE(4, record+src)`; below zero it
// is zero and past the READER's own bound, IN ELEMENTS, it is the bound, and
// either way `COUNT clamped` once for the field.
//
// THE FORGERY IS IN THE BYTES and not through the writer: §3.1's write-side
// bound checks are DEBUG ONLY by rule and clamp nothing, so a count of -1 is a
// thing only the wire can say. `-1` is spelled as the wire spells it, four
// 0xFF bytes, which is also the number a length field holds when a peer wrote a
// signed -1 and a u32 reader read it as four billion.
//
// THIS CASE LIVES HERE AND NOT IN THE PROPERTIES GATE because it cannot be
// reached from there: `decode_identity` (fixedform_properties.cpp:347) returns
// before the clamp pass when `inspect` sets `unsafe`, and a forged count is
// exactly what `inspect` calls unsafe — so P1 and P3 can never reach the clamp
// the defence is. The identity plan is what carries `count` (§4.1), so this
// reads its own file.
static void count_clamp_case()
{
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.marks[0] = 7; one.marks[1] = 8;
    one.marks_count = 2;
    std::vector<uint8_t> file( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, file.data(), (int64_t) file.size() ) == (int64_t) file.size(),
           "count clamp: the record saves" );
    uint8_t * body = file.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;

    // THE COUNT'S OFFSET IS FOUND BY THE PLAN'S OWN ROW, by shape and not by a
    // number in this file: the identity plan carries exactly ONE `count` op and
    // it is `marks`', because `blob`'s count rides in its own `text` row's `aux`
    // and never as a `count` entry (§4.1, fix 10). So the row is found by its OP
    // ALONE, and the bound it carries is then a REAL assertion: a `count` op's
    // `size` IS the reader's own bound IN ELEMENTS (§4.1), which for
    // `marks [..4]int32` is four — not four BYTES, which is what the same number
    // would mean in a `copy` row.
    uint32_t count_at = 0xFFFFFFFFu;
    int32_t bound = -1;
    int32_t count_rows = 0;
    for ( int32_t i = 0; i < tblfx1::FxRootFixedPlanCount; ++i )
    {
        const tblfx1::TableFixedEntry e = tblfx1::FxRootFixedPlan[i];
        if ( e.op != tblfx1::kTableFixedCount ) { continue; }
        ++count_rows;
        count_at = e.src;
        bound = (int32_t) e.size;
    }
    check( count_rows == 1, "count clamp: the identity plan carries exactly ONE count op, so the row below is `marks`'" );
    check( count_at != 0xFFFFFFFFu && bound == 4, "count clamp: and its bound is FOUR ELEMENTS — `marks [..4]int32`'s own Max" );
    check( (int32_t) tblfx1::TableFixedGet32( body + count_at ) == 2,
           "count clamp: and the offset it names is where the live count really is" );

    // C1: A COUNT BELOW ZERO. Not a large number — zero, and one clamp.
    {
        tblfx1::TableFixedPut32( body + count_at, 0xFFFFFFFFu ); // -1, as the wire spells it
        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, file.data(), (int64_t) file.size(), plan.data(), 1024, NULL, &r ) == 1,
               "C1: a forged count of -1 still READS — a clamp is not a refusal" );
        check( back.marks_count == 0, "C1: a count below zero clamps to ZERO" );
        check( r.clamped == 1, "C1: and counts exactly one clamp" );
        check( !r.malformed && !r.refused, "C1: a clamp is neither malformed nor a refusal" );
    }

    // C2: A COUNT PAST THE READER'S OWN BOUND, IN ELEMENTS.
    {
        tblfx1::TableFixedPut32( body + count_at, (uint32_t) ( bound + 1 ) );
        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, file.data(), (int64_t) file.size(), plan.data(), 1024, NULL, &r ) == 1,
               "C2: a forged count of Max+1 still READS" );
        check( back.marks_count == bound, "C2: a count past Max clamps to MAX, in elements" );
        check( r.clamped == 1, "C2: and counts exactly one clamp" );
        check( back.marks[0] == 7 && back.marks[1] == 8, "C2: the live elements are still the record's" );
    }

    // AND THE CONTROL: the bound itself is not a clamp. A count EQUAL to Max is
    // in range, and a clamp counted there would be a clamp on a clean read.
    {
        tblfx1::TableFixedPut32( body + count_at, (uint32_t) bound );
        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, file.data(), (int64_t) file.size(), plan.data(), 1024, NULL, &r ) == 1,
               "count clamp: a count of exactly Max reads" );
        check( back.marks_count == bound && r.clamped == 0, "CONTROL: a count of exactly Max is in range and counts NOTHING" );
    }
}

// ---------------------------------------------------------------------------

// W6: THE PLAN IS PARTITIONED (docs/FIXED-FORM-ALGORITHM.md §4.1, §4.4, fix 6).
// Every UNGUARDED entry first, then every guarded one, and the plan states
// where the second half starts — `split` is the NUMBER OF UNGUARDED ENTRIES and
// not a count of guarded ones. The entries that are nearly all of a plan then
// never test a guard, which is the measurement the requirement was bought with
// (63.5 ns against 55).
//
// AND NO COALESCED RUN CROSSES THE SPLIT: merging is inside each half only, or
// an entry that must run under a tag would ride inside one that always runs.
// The pair at the boundary is the whole of what that forbids, so it is the pair
// this case tests.
static void partition_is_held( const tblfx1::TableFixedEntry * plan, int32_t count, int32_t split, const char * who )
{
    char what[256];
    std::snprintf( what, sizeof( what ), "W6: %s — split is in range", who );
    check( split >= 0 && split <= count, what );
    for ( int32_t i = 0; i < count; ++i )
    {
        const bool guarded = plan[i].gcount != 0;
        if ( i < split && guarded )
        {
            std::snprintf( what, sizeof( what ), "W6: %s — entry %d is below the split and carries a GUARD", who, (int) i );
            check( false, what );
        }
        if ( i >= split && !guarded )
        {
            std::snprintf( what, sizeof( what ), "W6: %s — entry %d is above the split and carries NO guard", who, (int) i );
            check( false, what );
        }
    }
    // THE BOUNDARY PAIR: had the coalescer merged across the split, the entry
    // below it and the entry at it would be one entry. They are two, and this
    // says why they have to be.
    if ( split > 0 && split < count )
    {
        const tblfx1::TableFixedEntry & a = plan[split - 1];
        const tblfx1::TableFixedEntry & b = plan[split];
        const bool mergeable = a.op == tblfx1::kTableFixedCopy && b.op == tblfx1::kTableFixedCopy &&
                               a.guards == b.guards && a.gcount == b.gcount &&
                               a.src + a.size == b.src && a.dst + a.size == b.dst;
        std::snprintf( what, sizeof( what ), "W6: %s — the pair at the split was not coalesced across it", who );
        check( !mergeable, what );
    }
}

static void plan_partition_case()
{
    // A PLAN WITH BOTH HALVES: UT1 carries a union, so its arms' entries are
    // guarded and the scalars around them are not.
    partition_is_held( (const tblfx1::TableFixedEntry *) (const void *) tblut1::UtRootFixedPlan,
                       tblut1::UtRootFixedPlanCount, tblut1::UtRootFixedPlanGuarded, "UT1 identity plan" );
    check( tblut1::UtRootFixedPlanGuarded < tblut1::UtRootFixedPlanCount,
           "W6: UT1's identity plan really has a guarded half, so the case is not vacuous" );
    check( tblut1::UtRootFixedPlanGuarded > 0, "W6: and an unguarded one" );
    partition_is_held( (const tblfx1::TableFixedEntry *) (const void *) tblut2::UtRootFixedPlan,
                       tblut2::UtRootFixedPlanCount, tblut2::UtRootFixedPlanGuarded, "UT2 identity plan" );
    partition_is_held( (const tblfx1::TableFixedEntry *) (const void *) tblv1::CfgFixedPlan,
                       tblv1::CfgFixedPlanCount, tblv1::CfgFixedPlanGuarded, "V1 identity plan" );

    // A PLAN WITH NO GUARDED HALF AT ALL: the split is then the whole plan, and
    // `split` being a count of UNGUARDED entries rather than of guarded ones is
    // what makes that come out right.
    partition_is_held( tblfx1::FxRootFixedPlan, tblfx1::FxRootFixedPlanCount, tblfx1::FxRootFixedPlanGuarded,
                       "FX1 identity plan" );
    check( tblfx1::FxRootFixedPlanGuarded == tblfx1::FxRootFixedPlanCount,
           "W6: a plan with no guarded entry has its split at the END, not at zero" );

    // AND A COMPILED PLAN, which is the half an identity plan cannot speak for:
    // the walk runs TWICE, unguarded first, and each pass keeps its own half.
    // The caller never sees the split, so the property is read off the plan
    // itself — once a guarded entry has been seen, no unguarded entry follows.
    {
        tblut1::UtRoot one;
        tblut1::UtRootReset( one );
        one.head = 42; one.tail = 99;
        one.pick.type = tblut1::PickType::B;
        std::strcpy( one.pick.b.label, "seven77" );
        one.pick.b.label_length = 7;
        one.pick.b.m = 1234;
        std::vector<uint8_t> w( (size_t) tblut1::UtRootFixedMeasure( 1 ) );
        check( tblut1::UtRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "W6: UT1 saves" );

        tblut2::UtRoot back;
        tblut2::TableReport r;
        std::vector<tblut2::TableFixedEntry> plan( 1024 );
        check( tblut2::UtRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "W6: UT2 reads a UT1 record through a COMPILED plan" );
        int32_t seen_guarded = -1;
        int32_t guarded_count = 0;
        for ( int32_t i = 0; i < (int32_t) plan.size(); ++i )
        {
            if ( plan[i].op == 0 && plan[i].size == 0 && plan[i].src == 0 && plan[i].dst == 0 ) { break; }
            if ( plan[i].gcount != 0 )
            {
                if ( seen_guarded < 0 ) { seen_guarded = i; }
                guarded_count++;
            }
            else
            {
                check( seen_guarded < 0, "W6: a COMPILED plan puts every unguarded entry in front of every guarded one" );
            }
        }
        check( guarded_count > 0, "W6: the compiled plan really has guarded entries, so the case is not vacuous" );
    }
}

// ---------------------------------------------------------------------------

// W16: AN ARM INSIDE AN ARM ANSWERS TO THE OUTER TAG
// (docs/FIXED-FORM-ALGORITHM.md §4.1). A union's guard is stamped over
// EVERYTHING its arm produced, and a nested union's own arm selection must
// SURVIVE that stamp — so the two tags are CONJOINED (§5.8 row 6): an inner
// arm's entries answer to the inner tag on the first lane and to the OUTER tag
// on the second, and they run only when BOTH hold their ordinal. Neither half
// stands alone: under the outer tag alone every inner arm fires the moment the
// outer one rides, and under the inner tag alone a record whose outer tag
// selects the other arm runs the inner arm's entries over storage that arm does
// not own. The #864 read found JS's
// inner union overwriting the outer guard; FU1/FU2 are the C leg's pair and are
// not this shape. FH1/FH2 on the tip are the compiled pair (owed 7); FG1 is the
// identity-plan fixture this PR carries.
//
// FG1's body: `tier` at 0, `head` at 1, and the outer union at 5 — so 5 is the
// OUTER tag's byte and 6 is the INNER's, and the two numbers being one apart is
// what makes the assertion sharp.
static void nested_union_case()
{
    // 1. THE PLAN SAYS IT. Every guarded entry's chain BEGINS at the OUTER tag
    //    — outermost link first — and not one answers to the inner tag alone.
    //    An entry inside the inner union carries TWO links, and one inside a
    //    third union would carry three: the chain is as long as the nesting.
    uint32_t outer_tag_at = 0xFFFFFFFFu;
    for ( int32_t i = 0; i < tblfg1::FhRootFixedPlanCount; ++i )
    {
        const tblfg1::TableFixedEntry e = tblfg1::FhRootFixedPlan[i];
        if ( e.gcount == 0 ) { continue; }
        const uint32_t outer = tblfg1::FhRootFixedGuards[e.guards / sizeof( tblfg1::TableFixedGuard )].guard;
        if ( outer_tag_at == 0xFFFFFFFFu ) { outer_tag_at = outer; }
        check( outer == outer_tag_at, "W16: every guarded entry of a nested union answers to ONE OUTER tag" );
    }
    check( outer_tag_at != 0xFFFFFFFFu, "W16: the plan really has guarded entries" );
    // the inner union's OWN tag rides as a guarded entry, whose SOURCE is the
    // inner tag's byte and whose ONE LINK is the outer's: the tag a union writes
    // answers to the unions OUTSIDE it and never to itself.
    bool saw_inner_tag = false;
    uint32_t inner_tag_dst = 0xFFFFFFFFu;
    uint8_t inner_arm_arg = 0;
    for ( int32_t i = 0; i < tblfg1::FhRootFixedPlanCount; ++i )
    {
        const tblfg1::TableFixedEntry e = tblfg1::FhRootFixedPlan[i];
        if ( e.gcount == 0 ) { continue; }
        const tblfg1::TableFixedGuard & first = tblfg1::FhRootFixedGuards[e.guards / sizeof( tblfg1::TableFixedGuard )];
        if ( e.src == outer_tag_at + 1u && e.size == 1u ) { saw_inner_tag = true; inner_tag_dst = e.dst; inner_arm_arg = (uint8_t) tblfg1::TableFixedGuardArg( first ); }
        check( first.guard == outer_tag_at,
               "W16: an inner entry answers to the inner tag AND the outer one, never the inner alone" );
    }
    check( saw_inner_tag, "W16: the inner union's own tag byte is one of the entries the OUTER tag guards" );

    // AND THE TWO ARMS SHARE STORAGE, which is what makes the record half below
    // an assertion and not a hope: `Outer` is a real C++ union, so the OTHER
    // arm's entry lands at the same destination the inner tag does. An inner
    // entry that ran under the wrong tag would be READABLE IN THE OTHER ARM'S
    // OWN FIELD, and that field reading back whole is the measurement.
    uint32_t other_arm_dst = 0xFFFFFFFFu;
    for ( int32_t i = 0; i < tblfg1::FhRootFixedPlanCount; ++i )
    {
        const tblfg1::TableFixedEntry e = tblfg1::FhRootFixedPlan[i];
        if ( e.gcount != 1 ) { continue; }
        const tblfg1::TableFixedGuard & only = tblfg1::FhRootFixedGuards[e.guards / sizeof( tblfg1::TableFixedGuard )];
        if ( only.guard != outer_tag_at || tblfg1::TableFixedGuardArg( only ) == inner_arm_arg ) { continue; }
        other_arm_dst = e.dst;
    }
    check( other_arm_dst != 0xFFFFFFFFu && other_arm_dst == inner_tag_dst,
           "W16: the OTHER arm's field and the inner tag land at ONE destination — so that field is where a stray inner entry would show" );

    // 2. AND THE RECORD SAYS IT. The outer tag selects B, so not one inner entry
    //    may run — and the measurement of that is THE B ARM'S OWN FIELD, which
    //    shares its destination with the inner tag (part 1): `m` reading back as
    //    555 is the statement that no inner entry's byte landed in it.
    //
    //    WHAT THIS DOES NOT SAY, and why. A destination stained where the A arm
    //    would keep the inner union's payload comes back STILL STAINED, not at
    //    the default: for the identity plan the unwritten-range list is EMPTY
    //    (§4.3), so an identity read prefills nothing and every storage byte no
    //    entry lands keeps what the CALLER put there. "The inner arm returns to
    //    its default" would be a claim about a prefill this form deliberately
    //    does not pay; the reference's real guarantee is narrower and stronger —
    //    the read writes the selected arm and NOTHING ELSE. So the stain is
    //    asserted to SURVIVE, which is the same fact from the other side: a byte
    //    the plan does not own is a byte the read does not touch.
    {
        tblfg1::FhRoot one;
        tblfg1::FhRootReset( one );
        one.tier = tblfg1::Tier::Silver;
        one.head = 77;
        one.pick.type = tblfg1::OuterType::B;
        one.pick.b.m = 555;
        std::vector<uint8_t> w( (size_t) tblfg1::FhRootFixedMeasure( 1 ) );
        check( tblfg1::FhRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "W16: the B record saves" );

        tblfg1::FhRoot back;
        tblfg1::FhRootReset( back );
        back.pick.type = tblfg1::OuterType::A;
        back.pick.a.inner.type = tblfg1::InnerType::Other;
        back.pick.a.inner.other.k = 0x5A5A5A5A; // the stain, so nothing below is vacuous
        check( back.pick.a.inner.other.k == 0x5A5A5A5A, "CONTROL: the inner arm's storage really is stained" );
        // the stain is read back AS BYTES, through the address taken while that
        // arm was the live one: after the load the live arm is B, and the
        // question here is about the BYTES at that address and not about a
        // member of a union that is no longer selected.
        const uint8_t * const stain_at = (const uint8_t *) (const void *) &back.pick.a.inner.other.k;
        tblfg1::TableReport r;
        std::vector<tblfg1::TableFixedEntry> plan( 1024 );
        check( tblfg1::FhRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "W16: the B record reads" );
        check( back.pick.type == tblfg1::OuterType::B, "W16: the outer tag" );
        check( back.pick.b.m == 555,
               "W16: THE B ARM'S OWN FIELD IS WHOLE — no inner entry's bytes landed in the storage it shares with the inner tag" );
        check( back.tier == tblfg1::Tier::Silver && back.head == 77, "W16: the rest of the record" );
        uint32_t stain_after = 0;
        std::memcpy( &stain_after, stain_at, sizeof( stain_after ) );
        check( stain_after == 0x5A5A5A5Au,
               "W16: and the storage no entry of this read owns is UNTOUCHED — the identity plan prefills nothing (§4.3)" );
        check( r.clamped == 0 && !r.malformed && !r.refused, "W16: a clean read moves no counter" );
    }

    // 3. THE OTHER WAY: the outer tag selects A, and the inner union's arm lands
    //    whole — the stamp does not cost the inner selection.
    //
    //    THIS HALF WAS BLUNT WHILE THE STAMP REPLACED THE INNER GUARD: both
    //    inner arms ran whenever the outer one rode, and because each copied the
    //    same record offsets to the same storage offsets the overlapping bytes
    //    were IDENTICAL and the selected arm read back whole either way. The
    //    conjunction (§5.8 row 6) ends that: part 1 above is now the assertion
    //    that the inner tag is a condition of its own, and what the case below
    //    holds is that the inner selection ARRIVES through both conditions.
    {
        tblfg1::FhRoot one;
        tblfg1::FhRootReset( one );
        one.tier = tblfg1::Tier::Bronze;
        one.head = 11;
        one.pick.type = tblfg1::OuterType::A;
        one.pick.a.edge = 22;
        one.pick.a.inner.type = tblfg1::InnerType::Other;
        one.pick.a.inner.other.k = 33;
        std::vector<uint8_t> w( (size_t) tblfg1::FhRootFixedMeasure( 1 ) );
        check( tblfg1::FhRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "W16: the A record saves" );

        tblfg1::FhRoot back;
        tblfg1::FhRootReset( back );
        tblfg1::TableReport r;
        std::vector<tblfg1::TableFixedEntry> plan( 1024 );
        check( tblfg1::FhRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "W16: the A record reads" );
        check( back.pick.type == tblfg1::OuterType::A, "W16: the outer arm" );
        check( back.pick.a.inner.type == tblfg1::InnerType::Other && back.pick.a.inner.other.k == 33,
               "W16: THE INNER UNION'S OWN ARM SELECTION SURVIVES THE OUTER STAMP" );
        check( back.pick.a.edge == 22, "W16: and the outer arm's other field" );
        check( r.clamped == 0 && !r.malformed && !r.refused, "W16: a clean read moves no counter" );
    }
}


// ---- rowan/corpus-manifest: BEGIN -----------------------------------------
// THE MANIFEST'S OWN CHECK (docs/FIXED-FORM-ALGORITHM.md §5.7 step 1, ruling
// #32). A port asserts the corpus's values from `manifest.txt` and never from
// test/tables/fixedform_dump.cpp, so the manifest has to be COMPLETE and its
// roots have to be real: every `.bin` the dump wrote needs a line, and every
// `root=` has to name a table the generator actually emitted — a renamed table
// whose manifest line kept the old name is the one way this could lie quietly.
#if !defined( _WIN32 )
#include <dirent.h>
#include <set>
#include <string>

static std::string slurp_text( const std::string & path )
{
    FILE * f = std::fopen( path.c_str(), "rb" );
    if ( !f ) { return std::string(); }
    std::string out;
    char buf[4096];
    size_t n;
    while ( ( n = std::fread( buf, 1, sizeof( buf ), f ) ) > 0 ) { out.append( buf, n ); }
    std::fclose( f );
    return out;
}

static std::string field_of( const std::string & line, const char * key )
{
    const std::string k( key );
    const size_t at = line.find( k );
    if ( at == std::string::npos ) { return std::string(); }
    const size_t end = line.find( ' ', at + k.size() );
    return line.substr( at + k.size(), end == std::string::npos ? std::string::npos : end - at - k.size() );
}

// every `<Name>FixedSave` the generator emitted, under the generated trees: the
// dump calls exactly these, so a root is a table the dump generated or it is not
static void fixed_save_names( const std::string & dir, std::set<std::string> & into, int depth )
{
    if ( depth > 4 ) { return; }
    DIR * d = opendir( dir.c_str() );
    if ( !d ) { return; }
    while ( const struct dirent * e = readdir( d ) )
    {
        const std::string name( e->d_name );
        if ( name == "." || name == ".." ) { continue; }
        const std::string path = dir + "/" + name;
        if ( name.size() > 2 && name.compare( name.size() - 2, 2, ".h" ) == 0 )
        {
            const std::string text = slurp_text( path );
            const std::string needle( "FixedSave" );
            for ( size_t at = text.find( needle ); at != std::string::npos; at = text.find( needle, at + 1 ) )
            {
                size_t b = at;
                while ( b > 0 )
                {
                    const char c = text[b - 1];
                    const bool word = ( c >= 'a' && c <= 'z' ) || ( c >= 'A' && c <= 'Z' ) ||
                                      ( c >= '0' && c <= '9' ) || c == '_';
                    if ( !word ) { break; }
                    b--;
                }
                if ( b < at ) { into.insert( text.substr( b, at - b ) ); }
            }
        }
        else
        {
            fixed_save_names( path, into, depth + 1 );
        }
    }
    closedir( d );
}

static void manifest_case( const char * corpus_dir )
{
    const std::string dir( corpus_dir );
    const std::string manifest = slurp_text( dir + "/manifest.txt" );
    DIR * corpus = opendir( dir.c_str() );
    if ( !corpus )
    {
        // the corpus is another target's output; when it is not there this case
        // has nothing to hold and says so rather than passing quietly
        std::printf( "NOTE: build/fixedform-corpus is absent, manifest check skipped"
                     " (run `make tables-fixedform-corpus`)\n" );
        return;
    }
    check( !manifest.empty(), "manifest: build/fixedform-corpus/manifest.txt exists and is not empty" );

    std::set<std::string> roots;
    int lines = 0;
    for ( size_t at = 0; at < manifest.size(); )
    {
        const size_t nl = manifest.find( '\n', at );
        const std::string line = manifest.substr( at, nl == std::string::npos ? std::string::npos : nl - at );
        at = nl == std::string::npos ? manifest.size() : nl + 1;
        if ( line.empty() ) { continue; }
        lines++;
        const std::string file = field_of( line, "file=" );
        const std::string row = field_of( line, "row=" );
        const std::string side = field_of( line, "side=" );
        const std::string root = field_of( line, "root=" );
        const std::string records = field_of( line, "records=" );
        const bool shaped = !file.empty() && !row.empty() && !side.empty() && !root.empty() &&
                            !records.empty() && line.find( " values=" ) != std::string::npos;
        if ( !shaped ) { std::printf( "  manifest line: %s\n", line.c_str() ); }
        check( shaped, "manifest: every line carries file, row, side, root, records and values" );
        if ( !root.empty() ) { roots.insert( root ); }
    }

    int files = 0;
    while ( const struct dirent * e = readdir( corpus ) )
    {
        const std::string name( e->d_name );
        if ( name.size() < 5 || name.compare( name.size() - 4, 4, ".bin" ) != 0 ) { continue; }
        files++;
        const bool listed = manifest.find( "file=" + name + " " ) != std::string::npos;
        if ( !listed ) { std::printf( "  no manifest line for %s\n", name.c_str() ); }
        check( listed, "manifest: every corpus file has a manifest line" );
    }
    closedir( corpus );
    check( files > 0 && lines == files, "manifest: one line per corpus file, and no line without a file" );

    std::set<std::string> generated;
    fixed_save_names( "build/tables-generated", generated, 0 );
    fixed_save_names( "build/tables-generated-fxw", generated, 0 );
    check( !generated.empty(), "manifest: the generated trees name at least one fixed table" );
    bool all_generated = true;
    for ( std::set<std::string>::const_iterator i = roots.begin(); i != roots.end(); ++i )
    {
        const bool known = generated.find( *i ) != generated.end();
        if ( !known ) { std::printf( "  manifest root %s names no generated table\n", i->c_str() ); }
        check( known, "manifest: every root names a table the dump generated" );
        if ( !known ) { all_generated = false; }
    }
    // the summary says "all generated" only when the check above actually
    // passed: a green-sounding last line over a red case is how a failure reads
    // as noise. `root=` is NOT unique across rows — several rows share a root
    // (FU1's and FU2's is `FuRoot`) — so this counts DISTINCT roots; `row=` is
    // the per-file name.
    std::printf( "manifest: %d lines, %d corpus files, %d distinct roots, %s\n",
                 lines, files, (int) roots.size(),
                 all_generated ? "all generated" : "SOME ROOT NAMES NO GENERATED TABLE" );
}
#else
static void manifest_case( const char * ) {}
#endif
// ---- rowan/corpus-manifest: END -------------------------------------------

// ---------------------------------------------------------------------------

// THE REFERENCE READS ITS OWN ORACLE BYTES (schema#876, cards 15 and 16).
//
// The case above writes FU1 in this very process and reads it back, which is
// what every port's FU pin did too — and it is the one thing a self-write pin
// cannot see: whether THE FILE every leg diffs against says what this test
// claims. `make tables-fixedform-corpus` writes build/fixedform-corpus/fu1.bin
// and fu2.bin from the reference's writer; this case reads those files, on the
// IDENTITY path and through FU2's COMPILED plan (NEW-READS-OLD, the lineage
// COMPILE laid down), and states the values beside the bytes. FU1 given fu2.bin
// is OLD-REFUSES-NEW: a hash this reader has never locked is layout_newer
// before any record (algorithm §5.3). A leg now has bytes to match AND a list
// of values to match them against; before this, a leg-vs-reference divergence
// on the nested-union shape was invisible on every leg at once.
//
// AND IT IS WHERE THE TWO WIDENING RUNGS ARE ASSERTED AFTER THE COMPILED READ:
//
//   THE SIGNED RUNG  `mark int16` read into FU2's `int32`. The source is read
//                    SIGN-EXTENDED at the writer's width, so -1 lands -1 and
//                    not 65535, and INT16_MIN lands -32768 and not 32768. A
//                    zero-extending port is a port that passes every self-write
//                    pin in the tree and fails here.
//   THE FLOAT RUNG   `heat float32` read into FU2's `float64`, ON THE BITS: the
//                    23 payload bits ride in the top of the double's 52, sign
//                    and all, and a SIGNALLING NaN stays signalling. Every
//                    hardware `(double) f` quiets it, which is the defect.
//
// Both rungs are counted: two widened per record and no other counter moved.

static std::vector<uint8_t> slurp( const char * dir, const char * name, bool & ok )
{
    char path[1024];
    std::snprintf( path, sizeof( path ), "%s/%s", dir, name );
    std::vector<uint8_t> out;
    FILE * f = std::fopen( path, "rb" );
    if ( f == NULL )
    {
        std::printf( "FAIL: %s is not there (run: make tables-fixedform-corpus)\n", path );
        failures++;
        ok = false;
        return out;
    }
    uint8_t chunk[4096];
    size_t n;
    while ( ( n = std::fread( chunk, 1, sizeof( chunk ), f ) ) > 0 ) { out.insert( out.end(), chunk, chunk + n ); }
    std::fclose( f );
    ok = true;
    return out;
}

static uint32_t bits32( float f ) { uint32_t b; std::memcpy( &b, &f, 4 ); return b; }
static uint64_t bits64( double d ) { uint64_t b; std::memcpy( &b, &d, 8 ); return b; }

// WHAT THIS FORM'S FLOAT RUNG ACTUALLY DOES TO A NaN, stated as arithmetic so
// the claim is readable rather than inherited from the machine: the sign rides,
// the 23 payload bits ride at the top of the double's 52 — AND THE QUIET BIT
// COMES BACK SET, because `kTableFixedWidenF` is a plain `(double) f`
// (internal/codegen/cpptable/fixedruntime.go:323) and every hardware convert
// quiets.
//
// THAT IS NOT WHAT §4's OWN HELPER DOES. `TableWidenF32`, the rung on every
// other projection, carries the comment "since the hardware conversion would
// set the quiet bit" and does the bit surgery to keep a signalling NaN
// signalling; test/tables/floatnan_main.cpp pins THAT answer over F1/F2. So the
// same rung has two answers depending on the form, and FU1.schema's own note
// ("which the reference's `(double) f` QUIETS") predicted this one. The oracle
// pins what the reference does TODAY and names the divergence; which of the two
// is the ruling is schema#876's to settle, and the day it settles this constant
// is the one line that moves.
static uint64_t widened_f32_nan( uint32_t narrow )
{
    return ( (uint64_t) ( narrow >> 31 ) << 63 ) | 0x7FF8000000000000ull
         | ( (uint64_t) ( narrow & 0x007FFFFFu ) << 29 );
}

static void fu_oracle_case( const char * dir )
{
    // the three records fu1_file() in test/tables/fixedform_dump.cpp writes, as
    // values: a leg that reads the file gets this same table
    const uint32_t heat_bits[3] = { 0x7F800001u, 0xFFA5A5A5u, 0x3FC00000u /* 1.5f */ };
    const int32_t mark_wide[3] = { -1, -32768, 32767 };

    bool have = false;
    const std::vector<uint8_t> fu1 = slurp( dir, "fu1.bin", have );
    if ( have )
    {
        // 1. THE IDENTITY PATH over the reference's own file
        tblfu1::FuRoot back[3];
        tblfu1::TableReport r;
        std::vector<tblfu1::TableFixedEntry> plan( 1024 );
        check( tblfu1::FuRootFixedLoad( back, 3, fu1.data(), (int64_t) fu1.size(), plan.data(), 1024, NULL, &r ) == 3,
               "oracle fu1: the identity read takes all three records" );
        check( back[0].flag && back[0].note_present && back[0].note == 44 && back[0].tail == 11,
               "oracle fu1: identity, record 0's head" );
        check( back[0].pick.type == tblfu1::PickType::Labelled && back[0].pick.labelled.lead == 101 &&
               back[0].pick.labelled.label_length == 5 && std::strcmp( back[0].pick.labelled.label, "hello" ) == 0 &&
               back[0].pick.labelled.trail == 202,
               "oracle fu1: identity, the SECOND arm's text between its two scalars" );
        check( !back[1].flag && !back[1].note_present && back[1].tail == 12 &&
               back[1].pick.type == tblfu1::PickType::Plain && back[1].pick.plain.n == 303,
               "oracle fu1: identity, the FIRST arm and the ABSENT optional" );
        check( back[2].pick.type == tblfu1::PickType::Labelled && back[2].pick.labelled.lead == -5 &&
               back[2].pick.labelled.label_length == 0 && back[2].pick.labelled.trail == 606 &&
               back[2].note == -7 && back[2].tail == 13,
               "oracle fu1: identity, the second arm with its text WHOLLY UNUSED" );
        for ( int i = 0; i < 3; ++i )
        {
            check( back[i].mark == (int16_t) mark_wide[i], "oracle fu1: identity, `mark` at its own width" );
            check( bits32( back[i].heat ) == heat_bits[i], "oracle fu1: identity, `heat`'s BITS, NaN payload and all" );
        }
        check( ( bits32( back[0].heat ) & 0x00400000u ) == 0,
               "oracle fu1: identity, the signalling NaN is STILL SIGNALLING" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "oracle fu1: identity, a clean read moves no counter" );

        // 2. THE COMPILED PATH — FU2 reads FU1's file, and BOTH RUNGS WIDEN
        tblfu2::FuRoot wide[3];
        tblfu2::TableReport r2;
        std::vector<tblfu2::TableFixedEntry> plan2( 1024 );
        check( tblfu2::FuRootFixedLoad( wide, 3, fu1.data(), (int64_t) fu1.size(), plan2.data(), 1024, NULL, &r2 ) == 3,
               "oracle fu1: the compiled read takes all three records" );
        check( wide[0].pick.type == tblfu2::PickType::Labelled &&
               wide[0].pick.labelled.label_length == 5 && std::strcmp( wide[0].pick.labelled.label, "hello" ) == 0,
               "oracle fu1: the COMPILED read still lands the arm's text" );
        check( wide[1].pick.type == tblfu2::PickType::Plain && wide[1].pick.plain.n == 303,
               "oracle fu1: compiled, the FIRST arm as well" );
        for ( int i = 0; i < 3; ++i )
        {
            check( wide[i].mark == mark_wide[i],
                   "SIGNED RUNG: int16 into int32, SIGN-EXTENDED — -1 is -1 and never 65535" );
            check( wide[i].extra == 11, "oracle fu1: compiled, `extra` takes its declared default" );
        }
        // THE FLOAT RUNG on the two NaNs: the payload and the sign ride, the
        // quiet bit comes back set (see widened_f32_nan above)
        check( bits64( wide[0].heat ) == widened_f32_nan( heat_bits[0] ),
               "FLOAT RUNG: f32 into f64, the smallest NaN payload rides in the top of the 52" );
        check( bits64( wide[1].heat ) == widened_f32_nan( heat_bits[1] ),
               "FLOAT RUNG: f32 into f64, a NEGATIVE NaN's sign and rich payload ride" );
        // THE 22 PAYLOAD BITS BELOW THE QUIET BIT SURVIVE EXACTLY, at the top of
        // the double's 52 — that much both answers agree on. The 23rd, the f32
        // QUIET BIT, lands on the double's quiet bit and comes back SET, so it
        // is the ONE bit of the payload this form does not carry across: a
        // signalling NaN arrives quiet and nothing in the report says so.
        check( ( ( bits64( wide[0].heat ) >> 29 ) & 0x003FFFFFull ) == ( heat_bits[0] & 0x003FFFFFu ) &&
               ( ( bits64( wide[1].heat ) >> 29 ) & 0x003FFFFFull ) == ( heat_bits[1] & 0x003FFFFFu ),
               "FLOAT RUNG: the 22 payload bits below the quiet bit are where the narrow width had them" );
        check( ( heat_bits[0] & 0x00400000u ) == 0 && ( heat_bits[1] & 0x00400000u ) == 0,
               "FLOAT RUNG: both written NaNs are SIGNALLING at the narrow width" );
        check( ( bits64( wide[1].heat ) >> 63 ) == 1, "FLOAT RUNG: a negative NaN keeps its sign" );
        // THE DIVERGENCE, PINNED BY NAME: this form quiets and §4's own
        // TableWidenF32 does not (test/tables/floatnan_main.cpp). Pinned so the
        // day it is ruled on, a test says so rather than a port discovering it.
        check( ( bits64( wide[0].heat ) & 0x0008000000000000ull ) != 0,
               "FLOAT RUNG, THE FIXED FORM'S ANSWER: `(double) f` QUIETS the signalling NaN — "
               "§4's TableWidenF32 keeps it signalling, so the rung has two answers (schema#876)" );
        // AND AN ORDINARY FLOAT, where the conversion is the whole of the rung
        check( wide[2].heat == 1.5, "FLOAT RUNG: an ordinary float widens to the same number" );
        check( bits64( wide[2].heat ) == 0x3FF8000000000000ull, "FLOAT RUNG: 1.5 widens to 1.5's bits" );
        // TWO WIDENED PER RECORD AND NOTHING ELSE: `extra` is a field the writer
        // does not carry, which is the prefill's business and no counter's, and
        // FU1 carries no field FU2 cannot name
        check( r2.widened == 6, "BOTH RUNGS COUNTED: two widened fields times three records — `widened` is the RECORD's counter" );
        check( r2.unknown == 0 && r2.kind_mismatch == 0 && r2.clamped == 0 && !r2.malformed && !r2.refused,
               "BOTH RUNGS COUNTED: and no other counter moved" );
    }

    bool have2 = false;
    const std::vector<uint8_t> fu2 = slurp( dir, "fu2.bin", have2 );
    if ( have2 )
    {
        // 3. FU2's OWN BYTES on the identity path
        tblfu2::FuRoot back[2];
        tblfu2::TableReport r;
        std::vector<tblfu2::TableFixedEntry> plan( 1024 );
        check( tblfu2::FuRootFixedLoad( back, 2, fu2.data(), (int64_t) fu2.size(), plan.data(), 1024, NULL, &r ) == 2,
               "oracle fu2: the identity read takes both records" );
        check( back[0].flag && back[0].note == 55 && back[0].note_present && back[0].tail == 21 &&
               back[0].extra == 909 && back[0].mark == -70000,
               "oracle fu2: identity, the head, the wide `mark` and `extra`" );
        check( back[0].pick.type == tblfu2::PickType::Labelled && back[0].pick.labelled.lead == 111 &&
               back[0].pick.labelled.label_length == 4 && std::strcmp( back[0].pick.labelled.label, "wide" ) == 0 &&
               back[0].pick.labelled.trail == 222,
               "oracle fu2: identity, the SECOND arm's text" );
        check( bits64( back[0].heat ) == 0x7FF00DEFACED0001ull,
               "oracle fu2: identity, a signalling NaN at sixty-four bits rides as its bits" );
        check( back[1].pick.type == tblfu2::PickType::Plain && back[1].pick.plain.n == 404 &&
               back[1].mark == 70000 && back[1].heat == -2.25 && back[1].extra == -11 && back[1].tail == 22,
               "oracle fu2: identity, the FIRST arm" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "oracle fu2: identity, a clean read moves no counter" );

        // 4. THE OTHER DIRECTION — FU1 given FU2's file. COMPILE from the lock
        // (algorithm §5.3): hash selects, a stranger's layout is never parsed.
        // FU1's lineage is itself alone, so FU2's hash is layout_newer BEFORE
        // ANY RECORD — not a compiled backwards read, not kind_mismatch on the
        // two rungs. The rungs run FORWARD only (fu1 into FU2, above).
        tblfu1::FuRoot narrow[2];
        tblfu1::FuRootReset( narrow[0] );
        tblfu1::FuRootReset( narrow[1] );
        tblfu1::TableReport r1;
        std::vector<tblfu1::TableFixedEntry> plan1( 1024 );
        const int64_t got = tblfu1::FuRootFixedLoad( narrow, 2, fu2.data(), (int64_t) fu2.size(), plan1.data(), 1024, NULL, &r1 );
        check( got < 0 && r1.refused && r1.reason == tblfu1::layout_newer,
               "OLD-REFUSES-NEW: FU1 reading fu2.bin is layout_newer, nothing parsed" );
        check( r1.layout_hash == tblfu2::FuRootFixedHash,
               "OLD-REFUSES-NEW: the refusal carries the file's hash and nothing else (bill §12.4)" );
        check( r1.unknown == 0 && r1.kind_mismatch == 0 && r1.widened == 0 && r1.clamped == 0 && !r1.malformed,
               "OLD-REFUSES-NEW: REFUSE is total — no counter moved" );
        check( narrow[0].tail == 3 && narrow[1].tail == 3 &&
               narrow[0].mark == -1 && narrow[1].mark == -1 &&
               narrow[0].heat == 0.0f && narrow[1].heat == 0.0f,
               "OLD-REFUSES-NEW: nothing decoded — every field is still its declared default" );
    }
}

int main( int argc, char ** argv )
{
    // the reference's own oracle bytes, written by `make tables-fixedform-corpus`
    const char * corpus = argc > 1 ? argv[1] : "build/fixedform-corpus";
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
    fu_oracle_case( corpus );
    owed7_nested_union_outer_tag_case();
    owed8_width8_ordinal_read_whole_case();
    owed10_no_layout_writes_nothing_case();
    owed13_compiled_ordinal_counts_clamped_case();
    bytes_row_case();
    bounds_case();
    absent_optional_case();
    text_content_case();
    guard_width_case();
    guard_chain_depth_case();
    cache_case();
    layout_validation();
    record_bound_case();
    count_clamp_case();
    plan_partition_case();
    nested_union_case();
    fuzz_case();
    // ---- rowan/corpus-manifest ----
    manifest_case( corpus );
    // ---- rowan/cpp-versioning-numbers: BEGIN ----
    failures += versioning_numbers_cases();
    // ---- rowan/cpp-versioning-numbers: END ----
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
