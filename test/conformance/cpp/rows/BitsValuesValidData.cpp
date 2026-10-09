// CELL cpp/BitsValuesValidData — bits(N) fields: valid-data write/read acceptance.
//
// THE LAW (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md:173): a
// `bits(N)` field rides at its DECLARED STORAGE WIDTH — 4 bytes for N <= 32
// and 8 bytes for N > 32 — and its implied range is [0, 2^N - 1]. A value
// past the top clamps and counts; a value at the cap is valid and moves
// nothing.
//
// WHAT THIS TEST DOES: builds one RangedWidths record (form 3, the fixed
// wire) with every bits(N) field set to its CAP (2^N - 1), writes it through
// the generated RangedWidthsFixedSave, reads it back through
// RangedWidthsFixedLoad, and asserts that every field survives the round trip
// with no counter moved. The same record is then written with a mid-range
// value (not 0, not max) to prove the path is not a constant fold.
//
// THE NEGATIVE CONTROL (`-DBITS_CONTROL`): flips ONE byte of the b12 field
// on the wire after the write, which lifts the decoded value past the cap
// (4095). The reader's bounds pass must clamp it back, so `clamped == 1` and
// `b12 == 4095`. A reader that ignores the implied range would pass the
// corrupted value through and go RED here.
//
// BUILD/RUN (from ./repo):
//   c++ -std=c++17 -Wall <conformance -I flags> build/tables-generated/examples/RangesTable.cpp
//     test/conformance/cpp/rows/BitsValuesValidData.cpp -o build/rows-cpp-BitsValuesValidData &&
//     ./build/rows-cpp-BitsValuesValidData
// Exit 0 green / exit 1 red, one printed line per assertion.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "RangesTable.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) failures++;
}

int main()
{
    // ---- proof: the body size matches the sum of storage widths ----
    // b8=4, b16=4, b32=4, b64=8, b12=4, b48=8 → 32 bytes
    check( tabledemo::RangedWidthsFixedBodyBytes == 32,
           "BitsValuesValidData: the body is 32 bytes" );

    // ---- CASE 1: every bits(N) field AT its cap (2^N - 1) ----
    {
        tabledemo::RangedWidths v;
        tabledemo::RangedWidthsReset( v );
        v.b8  = 0xFFu;              // 2^8  - 1
        v.b16 = 0xFFFFu;            // 2^16 - 1
        v.b32 = 0xFFFFFFFFu;        // 2^32 - 1
        v.b64 = 0xFFFFFFFFFFFFFFFFull; // 2^64 - 1
        v.b12 = 0xFFFu;             // 2^12 - 1 = 4095
        v.b48 = 0xFFFFFFFFFFFFull;  // 2^48 - 1

        std::vector<uint8_t> wire( (size_t) tabledemo::RangedWidthsFixedMeasure( 1 ) );
        const int64_t wrote = tabledemo::RangedWidthsFixedSave( &v, 1, wire.data(), (int64_t) wire.size() );
        check( wrote == (int64_t) wire.size(),
               "BitsValuesValidData cap: the record saves whole" );

        tabledemo::RangedWidths back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        tabledemo::TableReport r;
        const int64_t n = tabledemo::RangedWidthsFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(),
            NULL, 0, NULL, &r );

        check( n == 1 && !r.refused && !r.malformed,
               "BitsValuesValidData cap: the record reads" );
        check( r.clamped == 0 && r.unknown == 0 && r.widened == 0,
               "BitsValuesValidData cap: at the cap moves no counter" );

        check( back.b8  == 0xFFu,              "BitsValuesValidData cap: b8  round-trips" );
        check( back.b16 == 0xFFFFu,            "BitsValuesValidData cap: b16 round-trips" );
        check( back.b32 == 0xFFFFFFFFu,        "BitsValuesValidData cap: b32 round-trips" );
        check( back.b64 == 0xFFFFFFFFFFFFFFFFull,
                                               "BitsValuesValidData cap: b64 round-trips" );
        check( back.b12 == 0xFFFu,             "BitsValuesValidData cap: b12 round-trips" );
        check( back.b48 == 0xFFFFFFFFFFFFull,  "BitsValuesValidData cap: b48 round-trips" );
    }

    // ---- CASE 2: mid-range values (not 0, not max) ----
    {
        tabledemo::RangedWidths v;
        tabledemo::RangedWidthsReset( v );
        v.b8  = 0xAAu;
        v.b16 = 0xBBBBu;
        v.b32 = 0xCCCCCCCCu;
        v.b64 = 0xDDDDDDDDDDDDDDDDull;
        v.b12 = 0xABCu;
        v.b48 = 0xEEEEEEEEEEEEull;

        std::vector<uint8_t> wire( (size_t) tabledemo::RangedWidthsFixedMeasure( 1 ) );
        const int64_t wrote = tabledemo::RangedWidthsFixedSave( &v, 1, wire.data(), (int64_t) wire.size() );
        check( wrote == (int64_t) wire.size(),
               "BitsValuesValidData mid: the record saves whole" );

        tabledemo::RangedWidths back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        tabledemo::TableReport r;
        const int64_t n = tabledemo::RangedWidthsFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(),
            NULL, 0, NULL, &r );

        check( n == 1 && !r.refused && !r.malformed,
               "BitsValuesValidData mid: the record reads" );
        check( r.clamped == 0 && r.unknown == 0,
               "BitsValuesValidData mid: mid-range moves no counter" );

        check( back.b8  == 0xAAu,              "BitsValuesValidData mid: b8  round-trips" );
        check( back.b16 == 0xBBBBu,            "BitsValuesValidData mid: b16 round-trips" );
        check( back.b32 == 0xCCCCCCCCu,        "BitsValuesValidData mid: b32 round-trips" );
        check( back.b64 == 0xDDDDDDDDDDDDDDDDull,
                                               "BitsValuesValidData mid: b64 round-trips" );
        check( back.b12 == 0xABCu,             "BitsValuesValidData mid: b12 round-trips" );
        check( back.b48 == 0xEEEEEEEEEEEEull,  "BitsValuesValidData mid: b48 round-trips" );
    }

    // ---- CASE 3: the negative control — one forged byte lifts b12 past its cap ----
    {
        tabledemo::RangedWidths v;
        tabledemo::RangedWidthsReset( v );
        v.b8  = 0x42u;
        v.b16 = 0x4242u;
        v.b32 = 0x42424242u;
        v.b64 = 0x4242424242424242ull;
        v.b12 = 0x100u;             // a value inside the uint16 storage but past bits(12) cap
        v.b48 = 0x424242424242ull;

        std::vector<uint8_t> wire( (size_t) tabledemo::RangedWidthsFixedMeasure( 1 ) );
        const int64_t wrote = tabledemo::RangedWidthsFixedSave( &v, 1, wire.data(), (int64_t) wire.size() );
        check( wrote == (int64_t) wire.size(),
               "BitsValuesValidData over-cap: the record saves whole (the write already clamps)" );

        // The fixed writer clamps on the write side too, so the wire already
        // carries the clamped value. For the negative control, forge a byte
        // on the wire after the save to push b12 past 4095.
        // The layout: form(1) + hash(8) + layout-len(4) + layout + record-hash(8) + body(32).
        // b12 sits at offset 20 in the body (b8@0=4b, b16@4=4b, b32@8=4b, b12@20=4b).
        const size_t body_off = (size_t) tabledemo::kTableFixedHeaderBytes + 4
                              + (size_t) tabledemo::RangedWidthsFixedLayoutBytes + 8;
        // b12 is a uint32 at body+20; poke the MSB to make the value > 4095
        // but within the uint32 range the writer stored.
        // The writer stored 0x00000100 (clamped from 0x100 → 0x100 is 256,
        // which is within 0..4095 so it wasn't clamped by the writer).
        // Let's set it to 0x00002000 (8192, well past the 4095 cap).
        // On little-endian: byte 22 is the high byte of the upper 16 bits
        // of the 32-bit word. The value 0x00002000 stored LE is
        // [00 20 00 00]. We want to set byte body_off+22 to 0x20.
        wire[ body_off + 22 ] = 0x20; // was 0x00; now the wire carries 0x00002000

        tabledemo::RangedWidths back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        tabledemo::TableReport r;
        const int64_t n = tabledemo::RangedWidthsFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(),
            NULL, 0, NULL, &r );

        check( n == 1 && !r.refused && !r.malformed,
               "BitsValuesValidData forged: the record reads (forged value is not a refusal)" );
        check( r.clamped == 1,
               "BitsValuesValidData forged: the b12 field clamps once" );
        check( back.b12 == 0xFFFu,
               "BitsValuesValidData forged: b12 clamps to 4095 (2^12 - 1)" );

        // neighbours stand
        check( back.b8 == 0x42u && back.b16 == 0x4242u && back.b32 == 0x42424242u,
               "BitsValuesValidData forged: the neighbour fields stand" );
    }

    return failures == 0 ? 0 : 1;
}
