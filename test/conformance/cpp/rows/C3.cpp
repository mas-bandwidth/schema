// C3 — TEXT LENGTH BOUND (once the text length clamp), the cpp leg's assertion of it (guard).
//
// The law is docs/FIXED-FORM-ALGORITHM.md:449 and :1053, §4.5's `text` scatter case:
//
//   cap := size;
//   if v := SLE(4, record+src) < 0 or v > cap: REFUSE length_over_bound,
//   before any destination byte is written (docs/rulings.json strict-only-read:
//   no read clamps; the row once asserted a clamp to [0, cap]).
//   Copy the payload, and terminate at the used length where the language
//   stores a terminator — never for `bytes`.
//
// docs/SPEC-TABLES.md:6878 states the op, :9796-:9797 the storage: a
// `string(N)` is `char[N + 1]` plus an int32 used length.
//
// THE PRODUCTION PATH, not a helper: the generated fixed-form reader of the
// conformance unit whose plan carries a text entry —
//   tabledemo::ProfileConfigFixedLoad (name string(32): plan entry
//     kTableFixedText meta=Utf8, size 32, cap 32)
// whose TableFixedRun -> TableFixedApply kTableFixedText arm is the clamp,
// reached the same way the driver reaches these units (same headers, same
// -I paths, same linked *Table.cpp the Makefile gives build/conformance-cpp).
//
// THE VECTOR, derived from the law: the conformance corpus carries no
// fixed-form data for this cell — every wire under testdata/wire/tables is
// form 1, the tolerant wire, whose text clamp is a different one (leb128,
// at a code point boundary). So the smallest fixed-form byte vector is built
// here: a one-record form-3 file written by the unit's own generated
// FixedSave —
//   [ form 3 ][ 7 reserved zero bytes ][ layout hash, 8 ]
//   [ layout bytes, u32 length ][ record: 8-byte hash ][ 261/16-byte body ]
// — and the text entry's SLE(4) length word is poked. The word's offset is
// the plan entry's own src (0 for `name`), so it sits at
//   kTableFixedHeaderBytes + 4 + <T>FixedLayoutBytes + 8   (record 0's body)
// exactly the address the writer's own <T>FixedWriteBody puts it at. The
// forged values are one past the cap (33 against 32),
// the negative end (-1), and a `bytes` length one past its cap (20 against
// 16) for the never-terminates half of the sentence. The destination struct
// is poisoned before every read, so a byte that checks as 0 was WRITTEN by
// the read and not left over from initialisation.

#include <cstdio>
#include <cstdint>
#include <cstring>
#include <vector>

#include "TablesTable.h"  // tabledemo (tables/examples)

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "ok" : "FAIL", what );
    if ( !ok ) { failures++; }
}

// a refused load lands nothing: the generated FixedLoad leaves the row at its
// declared defaults (R4), so neither the wire's values nor the 0xAB poison stand
static bool untouched( const tabledemo::ProfileConfig & back )
{
    tabledemo::ProfileConfig fresh;
    tabledemo::ProfileConfigReset( fresh );
    return back.name_length == fresh.name_length && back.experience == fresh.experience
        && back.icon_length == fresh.icon_length;
}

// ---- the narrow half: tabledemo::ProfileConfig, `name` string(32) ----------

static void narrow_case()
{
    tabledemo::ProfileConfig v;
    tabledemo::ProfileConfigReset( v );
    std::memset( v.name, 'A', 32 ); // the used length AT the cap, clean ASCII
    v.name_length = 32;
    std::memset( v.icon, 0x42, 16 ); // a full bytes(16), clean
    v.icon_length = 16;
    v.experience = 777; // the neighbour that must stand through every clamp

    std::vector<uint8_t> wire( (size_t) tabledemo::ProfileConfigFixedMeasure( 1 ) );
    check( tabledemo::ProfileConfigFixedSave( &v, 1, wire.data(), (int64_t) wire.size() )
               == (int64_t) wire.size(),
           "narrow: the one-record form-3 file saves through the unit's own writer" );

    // record 0's body, then the text entry's SLE(4) length word at its src
    const size_t body = (size_t) tabledemo::kTableFixedHeaderBytes + 4
                      + (size_t) tabledemo::ProfileConfigFixedLayoutBytes + 8;
    uint8_t * const name_word = wire.data() + body + 0;
    uint8_t * const icon_word = wire.data() + body + 36;

    // CLEAN AT THE CAP — [0, cap] is inclusive: a used length of exactly cap
    // is in the bound, rides whole, and moves no counter
    {
        tabledemo::ProfileConfig back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        tabledemo::TableReport r;
        std::vector<tabledemo::TableFixedEntry> plan( 1024 );
        const int64_t n = tabledemo::ProfileConfigFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && !r.refused && !r.damaged,
               "narrow clean: a used length at the cap reads the whole record" );
        // strict-only-read: a kind mismatch, an out-of-range value and a repeat now refuse the
        // whole load (no counter), so the retired zero counts read as not refused, not damaged.
        check( !r.refused && !r.damaged && r.widened == 0 && r.unknown == 0,
               "narrow clean: the cap is IN the bound — no counter moves" );
        check( back.name_length == 32 && back.name[0] == 'A' && back.name[31] == 'A',
               "narrow clean: the used length lands whole at the cap" );
        check( back.name[32] == 0,
               "narrow clean: terminated at the used length, name[32] the read's own store" );
        check( back.experience == 777 && back.icon_length == 16 && back.icon[15] == 0x42,
               "narrow clean: the neighbours stand" );
    }

    // strict-only-read: the three cases below once asserted a CLAMP (n == 1, clamped == 1,
    // the length landed at the bound, the neighbours stood). No read clamps now: a text or
    // bytes length outside [0, cap] refuses the whole load length_over_bound before any
    // destination byte is written (docs/FIXED-FORM-ALGORITHM.md:449, :1053; plans/strict-only.md
    // decisions 1-2).

    // ONE PAST THE CAP — 33 refuses the whole load, nothing lands
    tabledemo::TableFixedPut32( name_word, 33 );
    {
        tabledemo::ProfileConfig back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        tabledemo::TableReport r;
        std::vector<tabledemo::TableFixedEntry> plan( 1024 );
        const int64_t n = tabledemo::ProfileConfigFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n < 0 && r.refused && !r.damaged && r.reason == tabledemo::length_over_bound,
               "narrow over: a length one past the cap refuses length_over_bound" );
        check( untouched( back ),
               "narrow over: a refused load lands nothing (declared defaults)" );
    }

    // THE NEGATIVE END — -1 refuses the whole load, nothing lands
    tabledemo::TableFixedPut32( name_word, 0xFFFFFFFFu );
    {
        tabledemo::ProfileConfig back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        tabledemo::TableReport r;
        std::vector<tabledemo::TableFixedEntry> plan( 1024 );
        const int64_t n = tabledemo::ProfileConfigFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n < 0 && r.refused && !r.damaged && r.reason == tabledemo::length_over_bound,
               "narrow negative: a negative length refuses length_over_bound" );
        check( untouched( back ),
               "narrow negative: a refused load lands nothing (declared defaults)" );
    }
    tabledemo::TableFixedPut32( name_word, 32 ); // restore the clean word

    // `bytes` — the same bound: a length past the cap refuses, and no store lands (a
    // terminating store at icon[16] would have landed on icon_length)
    tabledemo::TableFixedPut32( icon_word, 20 );
    {
        tabledemo::ProfileConfig back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        tabledemo::TableReport r;
        std::vector<tabledemo::TableFixedEntry> plan( 1024 );
        const int64_t n = tabledemo::ProfileConfigFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n < 0 && r.refused && !r.damaged && r.reason == tabledemo::length_over_bound && untouched( back ),
               "narrow bytes: a bytes(N) length past the cap refuses length_over_bound, nothing lands" );
    }
}

int main()
{
    narrow_case();
    if ( failures != 0 )
    {
        std::printf( "C3: %d assertion(s) failed\n", failures );
        return 1;
    }
    std::printf( "C3: text length bound holds on the cpp leg (refuses, never clamps)\n" );
    return 0;
}
