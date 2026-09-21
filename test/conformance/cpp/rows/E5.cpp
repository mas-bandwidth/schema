// cell cpp/E5 — ?T vs plain nesting (docs/SPEC-TABLES.md §2.3, §5).
// Law: "A field moved between `?T` and a plain nesting is not an evolution
// event at all — the bytes do not move" (docs/SPEC-TABLES.md:7194).
//
// P1 nests Link by value; P3 marks it ?Link. The two produce identical wire
// bytes. Test the identity and the evolution direction.

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <vector>

#include "P1Table.h"
#include "P3Table.h"

static int failed = 0;

static void check(bool ok, const char *msg)
{
    if (!ok) { fprintf(stderr, "FAIL: %s\n", msg); failed = 1; }
    else     { printf("PASS: %s\n", msg); }
}

static bool slurp(const char *path, std::vector<uint8_t> &out)
{
    FILE *f = fopen(path, "rb");
    if (!f) { fprintf(stderr, "cannot open %s\n", path); return false; }
    fseek(f, 0, SEEK_END); long sz = ftell(f); fseek(f, 0, SEEK_SET);
    out.resize((size_t)sz);
    bool ok = sz == 0 || fread(out.data(), 1, (size_t)sz, f) == (size_t)sz;
    fclose(f);
    return ok;
}

int main()
{
    // ---- load the two wire files ----
    std::vector<uint8_t> chain_value, chain_optional;
    if (!slurp("testdata/wire/tables/chain_value.bin", chain_value)) return 1;
    if (!slurp("testdata/wire/tables/chain_optional.bin", chain_optional)) return 1;

    // ---- (1) the two files are byte-for-byte identical ----
    check(chain_value.size() == chain_optional.size() &&
          memcmp(chain_value.data(), chain_optional.data(), chain_value.size()) == 0,
          "chain_value.bin and chain_optional.bin are byte-identical");

    // ---- (2) load chain_value.bin (written by P1, plain nesting) under P3 (?T) ----
    tblp3::TableReport r3;
    tblp3::Chain c3;
    bool ok = tblp3::ChainLoad(c3, chain_value.data(), (int64_t)chain_value.size(), &r3);
    check(ok, "P3 loads P1's chain_value.bin");
    check(r3.unknown == 0 && r3.kind_mismatch == 0 && r3.widened == 0 &&
          r3.clamped == 0 && r3.duplicate == 0 && !r3.malformed && !r3.refused,
          "P3 reads P1's wire with zero counters and no refusal");
    check(c3.link_present == true, "P3 reads link_present == true from P1's wire");

    // ---- (3) load chain_optional.bin (written by P3, present ?Link) under P1 (plain) ----
    tblp1::TableReport r1;
    tblp1::Chain c1;
    ok = tblp1::ChainLoad(c1, chain_optional.data(), (int64_t)chain_optional.size(), &r1);
    check(ok, "P1 loads P3's chain_optional.bin");
    check(r1.unknown == 0 && r1.kind_mismatch == 0 && r1.widened == 0 &&
          r1.clamped == 0 && r1.duplicate == 0 && !r1.malformed && !r1.refused,
          "P1 reads P3's wire with zero counters and no refusal");

    // ---- (4) save under each schema and compare ----
    // P3 saves what it read from P1's wire, P1 saves what it read from P3's.
    // Since the wires are identical, both saves must be identical AND must
    // equal the original wire.
    int64_t p3_size = tblp3::ChainMeasure(c3);
    std::vector<uint8_t> p3_saved((size_t)p3_size, 0);
    tblp3::ChainSave(c3, p3_saved.data(), p3_size);

    int64_t p1_size = tblp1::ChainMeasure(c1);
    std::vector<uint8_t> p1_saved((size_t)p1_size, 0);
    tblp1::ChainSave(c1, p1_saved.data(), p1_size);

    check(p3_size == p1_size, "P3 save size equals P1 save size");
    check(p3_size == (int64_t)chain_value.size(), "P3 save size equals original wire size");
    check(p1_size == (int64_t)chain_optional.size(), "P1 save size equals original wire size");
    check(memcmp(p3_saved.data(), p1_saved.data(), (size_t)p3_size) == 0,
          "P3 save of P1's value equals P1 save of P3's value byte-for-byte");

    // ---- (5) negative control: P3 loads with link_present forced false,
    // save differs from P1 save
    tblp3::Chain c3_absent = c3;
    c3_absent.link_present = false;
    int64_t p3_absent_size = tblp3::ChainMeasure(c3_absent);
    std::vector<uint8_t> p3_absent_saved((size_t)p3_absent_size, 0);
    tblp3::ChainSave(c3_absent, p3_absent_saved.data(), p3_absent_size);
    check(p3_absent_size < p3_size,
          "P3 save with link_present=false is smaller (link field elided)");

    // ---- (6) verify P3 correctly loads chain_optional.bin with link_present ----
    tblp3::Chain c3b;
    tblp3::TableReport r3b;
    ok = tblp3::ChainLoad(c3b, chain_optional.data(), (int64_t)chain_optional.size(), &r3b);
    check(ok, "P3 loads its own chain_optional.bin");
    check(c3b.link_present == true, "P3 reads link_present == true from own wire");
    check(r3b.unknown == 0 && r3b.kind_mismatch == 0 && !r3b.malformed,
          "P3 reads own wire with zero counters");

    // ----  end ----
    return failed;
}