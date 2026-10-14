// C3 — TEXT LENGTH CLAMP, the cpp leg's assertion of it (guard).
//
// The law is docs/FIXED-FORM-ALGORITHM.md:333, §4.5's `text` scatter case:
//
//   unit := (meta == wide) ? 2 : 1;  cap := size / unit;
//   v := SLE(4, record+src) clamped into [0, cap], COUNT clamped if it fired.
//   Copy the payload, and terminate at the used length where the language
//   stores a terminator — never for `bytes`.
//
// docs/SPEC-TABLES.md:6878 states the op, :9796-:9797 the storage: a
// `string(N)` is `char[N + 1]` plus an int32 used length; a `wstring(N)` is
// `char16_t[N + 1]` with the used length in CODE UNITS.
//
// THE PRODUCTION PATH, not a helper: the generated fixed-form readers of the
// two conformance units whose plans carry a text entry —
//   tabledemo::ProfileConfigFixedLoad (name string(32): plan entry
//     kTableFixedText meta=Utf8, size 32, cap 32)
//   wide::StampFixedLoad (label wstring(4): plan entry kTableFixedText
//     meta=Wide, size 8, cap 8/2 = 4 units)
// whose TableFixedRun -> TableFixedApply kTableFixedText arm is the clamp,
// reached the same way the driver reaches these units (same headers, same
// -I paths, same linked *Table.cpp the Makefile gives build/conformance-cpp).
//
// THE VECTOR, derived from the law: the conformance corpus carries no
// fixed-form data for this cell — every wire under testdata/wire/tables is
// form 1, the tolerant wire, whose text clamp is a different one (leb128,
// at a code point boundary). So the smallest fixed-form byte vector is built
// here: a one-record form-3 file written by each unit's own generated
// FixedSave —
//   [ form 3 ][ 7 reserved zero bytes ][ layout hash, 8 ]
//   [ layout bytes, u32 length ][ record: 8-byte hash ][ 261/16-byte body ]
// — and the text entry's SLE(4) length word is poked. The word's offset is
// the plan entry's own src (0 for both `name` and `label`), so it sits at
//   kTableFixedHeaderBytes + 4 + <T>FixedLayoutBytes + 8   (record 0's body)
// exactly the address the writer's own <T>FixedWriteBody puts it at. The
// forged values are one past the cap (33 against 32; 6 against 4 units),
// the negative end (-1), and a `bytes` length one past its cap (20 against
// 16) for the never-terminates half of the sentence. The destination struct
// is poisoned before every read, so a byte that checks as 0 was WRITTEN by
// the read and not left over from initialisation.

#include <cstdio>
#include <cstdint>
#include <cstring>
#include <vector>

#include "TablesTable.h"  // tabledemo (tables/examples)
#include "CaptionTable.h" // wide (examples-wide)

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "ok" : "FAIL", what );
    if ( !ok ) { failures++; }
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
        check( n == 1 && !r.refused && !r.malformed,
               "narrow clean: a used length at the cap reads the whole record" );
        check( r.clamped == 0 && r.widened == 0 && r.unknown == 0 && r.kind_mismatch == 0,
               "narrow clean: the cap is IN the bound — no counter moves" );
        check( back.name_length == 32 && back.name[0] == 'A' && back.name[31] == 'A',
               "narrow clean: the used length lands whole at the cap" );
        check( back.name[32] == 0,
               "narrow clean: terminated at the used length, name[32] the read's own store" );
        check( back.experience == 777 && back.icon_length == 16 && back.icon[15] == 0x42,
               "narrow clean: the neighbours stand" );
    }

    // ONE PAST THE CAP — 33 clamps to 32, counts ONCE, and the record reads on
    tabledemo::TableFixedPut32( name_word, 33 );
    {
        tabledemo::ProfileConfig back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        tabledemo::TableReport r;
        std::vector<tabledemo::TableFixedEntry> plan( 1024 );
        const int64_t n = tabledemo::ProfileConfigFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && !r.refused && !r.malformed && r.clamped == 1,
               "narrow over: a length one past the cap clamps to the cap, COUNT clamped once" );
        check( back.name_length == 32 && back.name[0] == 'A' && back.name[31] == 'A',
               "narrow over: the clamped length lands at the cap, payload intact" );
        check( back.name[32] == 0,
               "narrow over: terminated at the clamped used length" );
        check( back.experience == 777 && back.icon_length == 16,
               "narrow over: the clamp is one field's — the rest of the record stands" );
    }

    // THE NEGATIVE END — -1 clamps to 0, counts ONCE, and the record reads on
    tabledemo::TableFixedPut32( name_word, 0xFFFFFFFFu );
    {
        tabledemo::ProfileConfig back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        tabledemo::TableReport r;
        std::vector<tabledemo::TableFixedEntry> plan( 1024 );
        const int64_t n = tabledemo::ProfileConfigFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && !r.refused && !r.malformed && r.clamped == 1,
               "narrow negative: a negative length clamps to zero, COUNT clamped once" );
        check( back.name_length == 0 && back.name[0] == 0,
               "narrow negative: the used length is zero and the terminator lands at index zero" );
        check( back.experience == 777 && back.icon_length == 16,
               "narrow negative: and the record's other fields still stand" );
    }
    tabledemo::TableFixedPut32( name_word, 32 ); // restore the clean word

    // `bytes` — the same clamp, and NEVER a terminator: a terminating store at
    // icon[16] would land on icon_length's first byte and zero it
    tabledemo::TableFixedPut32( icon_word, 20 );
    {
        tabledemo::ProfileConfig back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        tabledemo::TableReport r;
        std::vector<tabledemo::TableFixedEntry> plan( 1024 );
        const int64_t n = tabledemo::ProfileConfigFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && !r.refused && !r.malformed && r.clamped == 1
                   && back.icon_length == 16 && back.icon[15] == 0x42 && back.name_length == 32,
               "narrow bytes: a bytes(N) length past the cap clamps and never terminates" );
    }
}

// ---- the wide half: wide::Stamp, `label` wstring(4) -------------------------

static void wide_case()
{
    wide::Stamp v;
    wide::StampReset( v );
    const char16_t units[4] = { u'a', u'b', u'c', u'd' };
    std::memcpy( v.label, units, sizeof( units ) ); // 4 units, AT the cap
    v.label_length = 4;
    v.seq = 0xC0FFEEu; // the neighbour that must stand through the clamp

    std::vector<uint8_t> wire( (size_t) wide::StampFixedMeasure( 1 ) );
    check( wide::StampFixedSave( &v, 1, wire.data(), (int64_t) wire.size() )
               == (int64_t) wire.size(),
           "wide: the one-record form-3 file saves through the unit's own writer" );

    const size_t body = (size_t) wide::kTableFixedHeaderBytes + 4
                      + (size_t) wide::StampFixedLayoutBytes + 8;
    uint8_t * const label_word = wire.data() + body + 0;

    // CLEAN AT THE CAP, IN CODE UNITS
    {
        wide::Stamp back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        wide::TableReport r;
        std::vector<wide::TableFixedEntry> plan( 1024 );
        const int64_t n = wide::StampFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && !r.refused && !r.malformed && r.clamped == 0 && back.label_length == 4,
               "wide clean: a used length at the cap reads whole, no counter moves" );
        check( back.label[0] == u'a' && back.label[3] == u'd' && back.label[4] == u'\0',
               "wide clean: the payload lands and the terminating zero unit is the read's own store" );
        check( back.seq == 0xC0FFEEu,
               "wide clean: the neighbour field stands" );
    }

    // PAST THE CAP — 6 clamps to 4 CODE UNITS (8 bytes / 2), not to bytes: a
    // reader whose cap was the byte size would keep 6 or land 8, both red here
    wide::TableFixedPut32( label_word, 6 );
    {
        wide::Stamp back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        wide::TableReport r;
        std::vector<wide::TableFixedEntry> plan( 1024 );
        const int64_t n = wide::StampFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && !r.refused && !r.malformed && r.clamped == 1 && back.label_length == 4,
               "wide over: a length past the cap clamps in CODE UNITS (8 bytes / 2), COUNT clamped once" );
        check( back.label[3] == u'd' && back.label[4] == u'\0',
               "wide over: the payload stands at the cap and the terminator is the fifth unit" );
        check( back.seq == 0xC0FFEEu,
               "wide over: the clamp is one field's — seq stands" );
    }
}

int main()
{
    narrow_case();
    wide_case();
    if ( failures != 0 )
    {
        std::printf( "C3: %d assertion(s) failed\n", failures );
        return 1;
    }
    std::printf( "C3: text length clamp holds on the cpp leg\n" );
    return 0;
}
