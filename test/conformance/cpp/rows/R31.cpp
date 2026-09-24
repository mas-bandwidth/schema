// R31 — full-width lanes: arg/arg2 full width in the emitted static plan with
// the compare at the tag's own width; a 64-bit ordinal temporary; the remap
// table as long as the WRITER's variant count, refusing by name past the
// length word (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.4, §5.8 rows 5/7/14).
//
// THE THREE CLAUSES:
//   1. arg/arg2 are uint64_t in TableFixedEntry; the guard compare reads at
//      the tag's own width via TableFixedTagAt(src, p.guard, p.argw).
//   2. The ordinal op reads through a 64-bit temporary (uint64_t raw), so a
//      width-8 ordinal is read whole and not truncated to 32 bits.
//   3. The remap table has n = te.children entries (the WRITER's variant
//      count); n > 65535 overflows the u16 length word and the compile refuses.
//
// WHY THIS FILE EXISTS: the roadmap's cpp/R31 stayed :unknown because no
// standalone row test asserts all three clauses together for the cpp leg.
//
// BUILD/RUN (from ./repo):
//   c++ -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
//     -Ibuild/tables-generated/fu1 -Ibuild/tables-generated/fu2
//     test/conformance/cpp/rows/R31.cpp -o build/rows-cpp-R31 &&
//     ./build/rows-cpp-R31
// Exit 0 green, exit 1 red; one printed line per assertion.

#include <cinttypes>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <vector>

#include "FU1Table.h"

static int failures = 0;
static int checks = 0;

static void check(bool ok, const char *name, const char *detail)
{
    ++checks;
    if (ok) {
        std::printf("PASS %s: %s\n", name, detail);
    } else {
        ++failures;
        std::printf("FAIL %s: %s\n", name, detail);
    }
}

// ---------------------------------------------------------------------------
// Clause 1: arg/arg2 are FULL WIDTH (uint64_t) in the emitted static plan
// ---------------------------------------------------------------------------
// The law (docs/FIXED-FORM-ALGORITHM.md §5.8 row 5): "arg and arg2 are
// uint64_t on both twins, the emitted static plan carries them full width,
// and the compare is at the tag's own width."

static void clause1_full_width_arg()
{
    using Entry = tblfu1::TableFixedEntry;

    // a) The struct layout: arg and arg2 are uint64_t
    check(sizeof(((Entry *)nullptr)->arg) == 8,
          "arg-full-width", "arg is 8 bytes (uint64_t)");
    check(sizeof(((Entry *)nullptr)->arg2) == 8,
          "arg2-full-width", "arg2 is 8 bytes (uint64_t)");

    // b) The runtime compare: TableFixedTagAt returns uint64_t and is
    // compared against arg (uint64_t). We prove this by running TWO guarded
    // entries: one with arg=257 that matches a two-byte tag of 0x0101, and
    // one that should be skipped because the tag doesn't match.
    //
    // guarded=1 means entry 0 is in the unguarded half (no check). We use
    // two entries: entry 0 is unguarded (copy), entry 1 is guarded (the
    // test entry). guarded=1 puts entry 0 in loop-1, entry 1 in loop-2.

    alignas(Entry) uint8_t blob[1024];
    std::memset(blob, 0, sizeof(blob));
    Entry *plan = reinterpret_cast<Entry *>(blob);

    // Entry 0: unguarded copy of a sentinel value we can check
    plan[0].op = tblfu1::kTableFixedCopy;
    plan[0].src = 8;
    plan[0].dst = 0;
    plan[0].size = 4;
    plan[0].guard = tblfu1::kTableFixedNoGuard;
    plan[0].argw = 1;
    plan[0].guard2 = tblfu1::kTableFixedNoGuard;

    // Entry 1: guarded copy — only runs when tag at offset 0 matches arg=257 at width 2
    plan[1].op = tblfu1::kTableFixedCopy;
    plan[1].src = 12;
    plan[1].dst = 4;
    plan[1].size = 4;
    plan[1].guard = 0;       // tag at record offset 0
    plan[1].arg = 257;       // 0x0101, needs both bytes
    plan[1].argw = 2;        // two-byte width
    plan[1].guard2 = tblfu1::kTableFixedNoGuard;

    uint8_t record[20];
    std::memset(record, 0, sizeof(record));
    uint32_t sentinel = 0xDEADBEEF;
    std::memcpy(record + 8, &sentinel, 4);
    uint32_t guarded_val = 0xCAFEBABE;
    std::memcpy(record + 12, &guarded_val, 4);

    // Test A: tag matches (0x0101 == 257)
    record[0] = 0x01;
    record[1] = 0x01;

    uint8_t dst[16];
    std::memset(dst, 0, sizeof(dst));

    tblfu1::TableReport r;
    tblfu1::TableFixedRun(plan, 2, 1, record, dst, &r);  // guarded=1

    uint32_t landed0 = 0, landed1 = 0;
    std::memcpy(&landed0, dst, 4);
    std::memcpy(&landed1, dst + 4, 4);

    check(landed0 == 0xDEADBEEF,
          "arg-full-width/unguarded",
          "unguarded entry ran: sentinel copied");
    check(landed1 == 0xCAFEBABE,
          "arg-compares-at-tag-width",
          "arg=257 matched tag=0x0101 at width=2 (full uint64_t compare)");

    // Test B: tag does NOT match (0x0001 != 257)
    record[0] = 0x01;
    record[1] = 0x00;
    std::memset(dst, 0, sizeof(dst));
    tblfu1::TableFixedRun(plan, 2, 1, record, dst, &r);

    std::memcpy(&landed0, dst, 4);
    std::memcpy(&landed1, dst + 4, 4);
    check(landed0 == 0xDEADBEEF,
          "arg-negative/unguarded",
          "unguarded entry still ran");
    check(landed1 == 0,
          "arg-compares-at-tag-width-negative",
          "arg=257 did NOT match tag=0x0001; a byte-only compare would have falsely matched");
}

// ---------------------------------------------------------------------------
// Clause 1b: arg2 full width — guard2 conjunction
// ---------------------------------------------------------------------------

static void clause1b_full_width_arg2()
{
    using Entry = tblfu1::TableFixedEntry;

    check(sizeof(((Entry *)nullptr)->arg2) == 8,
          "arg2-is-uint64", "arg2 is 8 bytes");
    check(sizeof(((Entry *)nullptr)->argw2) == 1,
          "argw2-is-uint8", "argw2 is 1 byte (the width specifier)");

    // Entry 1 has both guard AND guard2 set: runs when BOTH match
    alignas(Entry) uint8_t blob[1024];
    std::memset(blob, 0, sizeof(blob));
    Entry *plan = reinterpret_cast<Entry *>(blob);

    plan[0].op = tblfu1::kTableFixedCopy;
    plan[0].src = 8;
    plan[0].dst = 0;
    plan[0].size = 4;
    plan[0].guard = tblfu1::kTableFixedNoGuard;
    plan[0].guard2 = tblfu1::kTableFixedNoGuard;

    plan[1].op = tblfu1::kTableFixedCopy;
    plan[1].src = 12;
    plan[1].dst = 4;
    plan[1].size = 4;
    plan[1].guard = 0;
    plan[1].arg = 3;
    plan[1].argw = 1;
    plan[1].guard2 = 1;   // second guard at offset 1
    plan[1].arg2 = 7;     // must also match
    plan[1].argw2 = 1;

    uint8_t record[20];
    std::memset(record, 0, sizeof(record));
    uint32_t sentinel = 0xDEADBEEF;
    std::memcpy(record + 8, &sentinel, 4);
    uint32_t val = 0xCAFECAFE;
    std::memcpy(record + 12, &val, 4);

    // Both guards match
    record[0] = 3;
    record[1] = 7;

    uint8_t dst[16];
    std::memset(dst, 0xCC, sizeof(dst));

    tblfu1::TableReport r;
    tblfu1::TableFixedRun(plan, 2, 1, record, dst, &r);

    uint32_t landed = 0;
    std::memcpy(&landed, dst + 4, 4);
    check(landed == 0xCAFECAFE,
          "arg2-conjunction",
          "entry ran: guard=3==arg AND guard2=7==arg2 (both full-width)");

    // guard2 mismatches
    record[1] = 5;
    std::memset(dst, 0xCC, sizeof(dst));
    tblfu1::TableFixedRun(plan, 2, 1, record, dst, &r);
    std::memcpy(&landed, dst + 4, 4);
    check(landed == 0xCCCCCCCC,
          "arg2-conjunction-negative",
          "entry skipped: guard2=5 != arg2=7");
}

// ---------------------------------------------------------------------------
// Clause 2: a 64-bit ordinal temporary
// ---------------------------------------------------------------------------

static void clause2_64bit_ordinal_temporary()
{
    using Entry = tblfu1::TableFixedEntry;

    // Build an ordinal plan entry with size=8 (8-byte ordinal)
    alignas(Entry) uint8_t blob[512];
    std::memset(blob, 0, sizeof(blob));
    Entry *plan = reinterpret_cast<Entry *>(blob);

    // Remap table at offset 256: [length=1, remap[1]=0]
    const uint32_t map_at = 256;
    uint16_t *map = reinterpret_cast<uint16_t *>(blob + map_at);
    map[0] = 1;  // writer has 1 variant
    map[1] = 0;  // ordinal 1 -> None

    plan[0].op = tblfu1::kTableFixedOrdinal;
    plan[0].src = 0;
    plan[0].dst = 0;
    plan[0].size = 8;       // 8-byte ordinal width
    plan[0].dstsize = 8;    // store as uint64_t
    plan[0].aux = map_at;
    plan[0].guard = tblfu1::kTableFixedNoGuard;
    plan[0].argw = 1;

    // The source ordinal is 2^32 = 0x0000000100000000 LE
    uint8_t src[16];
    std::memset(src, 0, sizeof(src));
    src[4] = 1;  // byte 4 = 1, rest = 0 → 2^32 in LE

    uint8_t dst[16];
    std::memset(dst, 0xAB, sizeof(dst));

    tblfu1::TableReport r;
    tblfu1::TableFixedRun(plan, 1, 1, src, dst, &r);  // guarded=1: entry in first half

    uint64_t landed = 0;
    std::memcpy(&landed, dst, 8);

    // With a 64-bit temporary: raw = 2^32, which is > map[0] = 1, so v = 0,
    // clamped++. With a 32-bit temporary: raw = 0, which is == 0, so v = 0,
    // but clamped does NOT fire (raw == 0 means "none").
    check(r.clamped == 1,
          "64bit-ordinal-temporary",
          "width-8 ordinal of 2^32: clamped==1 (uint32_t temp would read 0, not count clamped)");
    check(landed == 0,
          "64bit-ordinal-lands-none",
          "the ordinal lands 0 (None) because 2^32 is past the writer's 1 variant");

    // Positive control: ordinal 1 (within the writer's range)
    std::memset(src, 0, sizeof(src));
    src[0] = 1;  // ordinal 1 in LE
    std::memset(dst, 0xAB, sizeof(dst));
    r.clamped = 0;
    tblfu1::TableFixedRun(plan, 1, 1, src, dst, &r);
    std::memcpy(&landed, dst, 8);
    check(landed == 0,
          "64bit-ordinal-in-range",
          "ordinal 1 maps to remap[1]=0 (the writer's sole variant)");
    check(r.clamped == 0,
          "64bit-ordinal-in-range-no-clamp",
          "ordinal 1 is within range, clamped==0");
}

// ---------------------------------------------------------------------------
// Clause 3: the remap table as long as the WRITER's variant count, refusing
// by name past the length word
// ---------------------------------------------------------------------------

static void clause3_remap_table_writer_length()
{
    using namespace tblfu1;

    // Test 1: remap table of length 3 (writer has 3 variants)
    {
        alignas(TableFixedEntry) uint8_t blob[512];
        std::memset(blob, 0, sizeof(blob));
        TableFixedEntry *plan = reinterpret_cast<TableFixedEntry *>(blob);
        const uint32_t map_at = 256;
        uint16_t *map = reinterpret_cast<uint16_t *>(blob + map_at);

        map[0] = 3;
        map[1] = 0;  // ordinal 1 -> None
        map[2] = 1;  // ordinal 2 -> variant 1
        map[3] = 2;  // ordinal 3 -> variant 2

        plan[0].op = kTableFixedOrdinal;
        plan[0].src = 0;
        plan[0].dst = 0;
        plan[0].size = 4;
        plan[0].dstsize = 8;
        plan[0].aux = map_at;
        plan[0].guard = kTableFixedNoGuard;
        plan[0].argw = 1;

        // Ordinal 4 is past the length word (3) → None + clamped
        uint8_t src[8];
        uint32_t ord = 4;
        std::memcpy(src, &ord, 4);
        std::memset(src + 4, 0, 4);

        uint8_t dst[16];
        std::memset(dst, 0xAB, sizeof(dst));

        TableReport r;
        TableFixedRun(plan, 1, 1, src, dst, &r);

        uint64_t landed = 0;
        std::memcpy(&landed, dst, 8);
        check(landed == 0,
              "remap-past-length-word",
              "ordinal 4 past length-word 3: lands 0 (None)");
        check(r.clamped == 1,
              "remap-past-length-clamped",
              "ordinal past the length word counts clamped");

        // Ordinal 2 is within range → remap[2] = 1
        std::memset(dst, 0xAB, sizeof(dst));
        ord = 2;
        std::memcpy(src, &ord, 4);
        r.clamped = 0;
        TableFixedRun(plan, 1, 1, src, dst, &r);
        std::memcpy(&landed, dst, 8);
        check(landed == 1,
              "remap-within-range",
              "ordinal 2 remaps to 1 via the length-3 table");
        check(r.clamped == 0,
              "remap-within-range-no-clamp",
              "ordinal within range: clamped==0");
    }

    // Test 2: The overflow check — n > 65535 refuses during compile
    {
        TableFixedCompiler c;
        c.plan = static_cast<TableFixedEntry *>(std::calloc(4096, sizeof(TableFixedEntry)));
        c.capacity = 4096;
        c.count = 0;
        c.overflow = false;
        c.hostile = false;
        c.pool = 0;

        // Attempt to lay a table of 65536 entries — past the u16 limit
        const uint32_t at = TableFixedLayTable(c, nullptr, 65536);
        (void)at;

        check(c.overflow,
              "remap-overflow-65536",
              "TableFixedLayTable with n=65536 sets overflow=true (past u16 length word)");

        // 65535 should still fit
        c.overflow = false;
        const uint32_t at2 = TableFixedLayTable(c, nullptr, 65535);
        (void)at2;
        check(!c.overflow,
              "remap-fits-65535",
              "TableFixedLayTable with n=65535 does NOT overflow (fits in u16)");

        std::free(c.plan);
    }
}

// ---------------------------------------------------------------------------
// Integration: full round-trip through FU1 exercising all three clauses
// ---------------------------------------------------------------------------

static void integration_full_roundtrip()
{
    tblfu1::FuRoot v;
    tblfu1::FuRootReset(v);
    v.tail = 42;
    v.pick.type = tblfu1::PickType::Plain;
    v.pick.plain.n = 123;

    std::vector<uint8_t> w((size_t)tblfu1::FuRootFixedMeasure(1));
    check(tblfu1::FuRootFixedSave(&v, 1, w.data(), (int64_t)w.size()) == (int64_t)w.size(),
          "integration-fu1-save", "FU1 record saves");

    tblfu1::FuRoot back;
    tblfu1::FuRootReset(back);
    tblfu1::TableReport r;
    std::vector<tblfu1::TableFixedEntry> plan(1024);

    const int64_t n = tblfu1::FuRootFixedLoad(&back, 1, w.data(), (int64_t)w.size(),
                                               plan.data(), 1024, nullptr, &r);
    check(n == 1,
          "integration-fu1-load",
          "FU1 loads its own record through the identity plan");
    check(back.tail == 42,
          "integration-tail-lands",
          "tail scalar lands exactly");
    check(back.pick.type == tblfu1::PickType::Plain,
          "integration-union-arm-lands",
          "union arm lands (arg full width in the plan)");
    check(back.pick.plain.n == 123,
          "integration-union-payload",
          "union payload lands exactly");
    check(r.clamped == 0 && r.widened == 0 && r.unknown == 0 && !r.refused && !r.malformed,
          "integration-no-counters",
          "clean read: no counters moved");
}

int main()
{
    clause1_full_width_arg();
    clause1b_full_width_arg2();
    clause2_64bit_ordinal_temporary();
    clause3_remap_table_writer_length();
    integration_full_roundtrip();

    if (failures == 0) {
        std::printf("R31: all %d assertions passed\n", checks);
        return 0;
    } else {
        std::printf("R31: %d of %d assertions failed\n", failures, checks);
        return 1;
    }
}
