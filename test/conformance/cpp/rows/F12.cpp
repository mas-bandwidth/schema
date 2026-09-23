// F12 — a second layout for a held hash (docs/SPEC-TABLES.md §5.3, the file
// and the framings of the fixed form).
//
// THE LAW (docs/SPEC-TABLES.md:7032): "a layout is NAMED BY ITS HASH and a
// second layout for a second hash amends nothing. A second layout for a hash
// already held is refused by name and changes nothing."
//
// THE PRODUCTION PATH: tblfx1::FxRootFixedLoad is the generated reader. It
// splits the file header (algorithm §5.3 steps 1-3), then selects the lineage
// entry by hash (step 4). The header's hash is TAKEN AS GIVEN — the definitions
// digest is not on the wire. Against a hash THIS BUILD HOLDS, the layout bytes
// the file carries are COMPARED to the bytes the lock recorded. Same bytes: the
// identity lane opens. Different bytes: `layout_malformed` refused before a
// record byte is touched, no counter moved.
//
// THE VECTOR, built from the law and the leg's own facts — no corpus file.
// The held hash and the lock's own layout bytes are this build's own; the
// header carries the hash plus the LENGTH, and the layout rides behind it.
// A record's body is just the declared byte count, because the refusal F12
// names happens in `select`, BEFORE any record is split off (algorithm §5.3
// step 4 vs step 7).

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "FX1Table.h"

namespace {

int failures = 0;

void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
    else       { std::printf( "PASS: %s\n", what ); }
}

// Build a one-record form-3 file for FxRoot and return the offset into the
// file where the layout begins, so the caller can bend a byte inside it.
std::vector<uint8_t> one_fxroot_record()
{
    tblfx1::FxRoot v;
    tblfx1::FxRootReset( v );
    v.keep = 42;
    v.narrow = 7;
    v.renamed = 99;
    v.gone = 13;
    v.nested.a = 1;
    v.nested.b = 2;
    std::strcpy( v.label, "fx" );
    v.label_length = 2;
    v.marks_count = 1;
    v.marks[0] = 100;
    v.blob_length = 2;
    v.blob[0] = 0xAB;
    v.blob[1] = 0xCD;

    const int64_t file_size = tblfx1::FxRootFixedMeasure( 1 );
    std::vector<uint8_t> out( (size_t) file_size );
    check( tblfx1::FxRootFixedSave( &v, 1, out.data(), file_size ) == file_size,
           "one_fxroot_record: the lawful record saves" );
    return out;
}

} // namespace

int main()
{
    const std::vector<uint8_t> clean = one_fxroot_record();
    const uint64_t hash = tblfx1::FxRootFixedHash;

    // CONTROL: the reader HOLDS this layout, and its bytes match, so the
    // identity lane opens it — the refusal below is about the DIFFERENCE, not
    // the file.
    {
        tblfx1::FxRoot back;
        tblfx1::FxRootReset( back );
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 256 );
        const int64_t n = tblfx1::FxRootFixedLoad(
            &back, 1, clean.data(), (int64_t) clean.size(),
            plan.data(), 256, NULL, &r );
        check( n == 1 && !r.refused && !r.malformed,
               "CONTROL: the un-bent layout, under its held hash, opens" );
        check( back.keep == 42 && back.narrow == 7 && back.renamed == 99,
               "CONTROL: the record lands the fields it wrote" );
    }

    // Bend ONE byte inside the layout, past the header, without touching the
    // header's own hash: the SAME held hash now sits over DIFFERENT layout
    // bytes. The layout starts at kTableFixedHeaderBytes + 4 (past the header
    // and the u32 length word). We pick an offset well inside the layout.
    {
        std::vector<uint8_t> bent = clean;
        const size_t layout_at = (size_t) tblfx1::kTableFixedHeaderBytes + 4;
        // bend one byte in the middle of the layout — past the 4-byte count
        bent[ layout_at + 5 ] ^= 0xFF;

        tblfx1::FxRoot back;
        tblfx1::FxRootReset( back );
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 256 );
        const int64_t n = tblfx1::FxRootFixedLoad(
            &back, 1, bent.data(), (int64_t) bent.size(),
            plan.data(), 256, NULL, &r );
        check( n < 0,
               "F12: the load of a held hash over other layout bytes returns -1" );
        check( r.refused,
               "F12: a second layout for a held hash is refused" );
        check( r.reason == tblfx1::layout_malformed,
               "F12: the refusal IS `layout_malformed`, its own name" );

        // "AND CHANGES NOTHING": a refusal by name decodes nothing and moves no
        // counter, and a named refusal is never `malformed`.
        check( !r.malformed,
               "F12: the refusal is not damage" );
        check( r.unknown == 0,
               "F12: nothing was decoded — no unknown counter" );
        check( r.kind_mismatch == 0,
               "F12: no kind-mismatch counter" );
        check( r.widened == 0,
               "F12: no widened counter" );
        check( r.clamped == 0,
               "F12: no clamp counter" );
    }

    // THE CONTRAST THAT MAKES F12 DISCRIMINATING: a hash in NO lineage entry
    // is a different answer — `layout_newer`, the file's hash reported — which
    // is why a held-hash-over-different-bytes must be its own name and not that
    // one.
    {
        std::vector<uint8_t> unknown_file = clean;
        const uint64_t unknown_hash = hash + 1;
        tblfx1::TableFixedPut64( unknown_file.data() + tblfx1::kTableFixedHashAt, unknown_hash );

        tblfx1::FxRoot back;
        tblfx1::FxRootReset( back );
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 256 );
        const int64_t n = tblfx1::FxRootFixedLoad(
            &back, 1, unknown_file.data(), (int64_t) unknown_file.size(),
            plan.data(), 256, NULL, &r );
        check( n < 0,
               "F12: an unknown hash also returns -1" );
        check( r.refused,
               "F12: an unknown hash is refused too" );
        check( r.reason == tblfx1::layout_newer,
               "F12: an unknown hash is `layout_newer`, a different name" );
        check( r.layout_hash == unknown_hash,
               "F12: and `layout_newer` reports the file's hash" );
    }

    if ( failures > 0 )
    {
        std::printf( "F12 cpp second layout for a held hash: %d FAILED\n", failures );
        return 1;
    }
    std::printf( "F12 cpp second layout for a held hash: the assertion that proves it, red first\n" );
    return 0;
}