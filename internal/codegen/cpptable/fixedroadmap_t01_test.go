package cpptable

// TestFixedRoadmapCppT01Framing proves the eleven tasks the roadmap node
// `fixed-tables` carries for the cpp leg's file-envelope, batch-capacity and
// plan-selection rows, in roadmap order (docs/roadmap.sexp:366-388, 767-846).
// Each task is one subtest named by its task id; EVERY subtest is
// self-contained — it generates the leg's own C++ fixed-form header from a
// schema pair, writes a file with the generated `<Root>FixedSave`, damages the
// bytes, and reads them back with the generated `<Root>FixedLoad` — so the
// assertions run wherever `go test` runs and do not depend on the reference's
// byte corpus (`make tables-fixedform-corpus` needs the sibling `../serialize`
// runtime, which this card names as not run and never fakes).
//
// THE PAGE SENTENCES EACH ASSERTION IMPLEMENTS (docs/FIXED-FORM-ALGORITHM.md
// §5.3's load order, reproduced at lines 840-915; docs/SPEC-TABLES.md §3.4):
//
//   - F4  line 3/§5.3 step 3: "L := LE(4, b+16); if 20 + L > bytes, REFUSE
//     layout_malformed." A file truncated inside its layout is that name, and
//     a named refusal is never also `malformed`.
//   - F7  §5.3 step 2: "fewer than 20 bytes: malformed" — the residue of a
//     truncation, never a refusal by name, reason untouched.
//   - F8  §5.3 step 6: "if record_bytes <= 8 or rest % record_bytes != 0,
//     malformed — bytes left over means the two ends of the file have met."
//   - F12 step 7 / line 852: "a known hash, different layout length or bytes
//     → REFUSE layout_malformed" — a second layout for a hash the reader holds.
//   - F9  §5.3 step 10: "n := rest / record_bytes; if n > capacity: REFUSE
//     batch_too_large", nothing decoded, no counter moved.
//   - F10 §5.3 step 11: "if LE(8, at) != h, REFUSE no_layout" — a record hash
//     naming no layout this reader holds.
//   - R7  line 858: "the selected entry being the reader's own is an INDEX
//     COMPARISON and never a recomputation ... the identity lane never
//     compiles a plan."
//   - R8  step 5: "if none: REFUSE layout_newer, reporting h AND NOTHING ELSE."
//   - R9  line 852 + §1.1: "the seven §1.1 malformations under a KNOWN hash all
//     come back as one name, layout_malformed"; a known layout with
//     non-default values reads back exactly.
//   - R12 step 11: "if LE(8, at) != h: REFUSE no_layout -- BEFORE any byte is
//     landed. PREFILL(out[rec], fills)". The check is before the prefill.
//   - R13 line 868: "REFUSE is total: no counter moves, nothing is decoded, and
//     not one destination byte is written -- the prefill included."

import (
	"path/filepath"
	"testing"
)

// The schema pair every subtest generates. The pair differs by exactly one
// appended field (`w`, default 77), so the compiled old-file plan carries a
// NONEMPTY fill list — the only thing that can tell a prefill that ran from one
// that did not (docs/FIXED-FORM-VERSIONING-TESTS.md §5.8 row 9).
const cppT01Schema = "VNEW_field_append.schema"
const cppT01Pkg = "vnew_field_append"

// cppT01Preface is the C++ every subtest body opens with: four records with
// non-default and boundary values (the int32 ends), written by the generated
// save, plus the framing constants the assertions read.
const cppT01Preface = `
    static uint8_t buffer[1 << 16];
    Lineage want[4];
    memset( want, 0, sizeof( want ) );
    for ( int k = 0; k < 4; ++k ) { LineageReset( want[k] ); }
    want[0].x = -2147483647 - 1; want[0].y = 2147483647; want[0].z = -1; want[0].w = 42;
    want[1].x = 1;  want[1].y = 2;  want[1].z = 3;  want[1].w = 4;
    want[2].x = 7;  want[2].y = 8;  want[2].z = 9;  want[2].w = 10;
    want[3].x = 11; want[3].y = 12; want[3].z = 13; want[3].w = 14;
    int64_t written = LineageFixedSave( want, 4, buffer, (int64_t) sizeof( buffer ) );
    if ( written != LineageFixedMeasure( 4 ) ) { printf( "t01: save wrote %lld for 4 records\n", (long long) written ); return 1; }
    const int64_t L = (int64_t) LineageFixedLayoutBytes;
    const int64_t recbytes = (int64_t) LineageFixedRecordBytes;
    const int64_t head = kTableFixedHeaderBytes + 4;
    const int64_t at0 = head + L;
    static TableFixedEntry plan[4096];
    static TableFixedEntry cacheStore[4096];
    TableReport r;
`

// ---- F4: layout_malformed, truncated ----------------------------------------
//
// Cutting the file anywhere inside its layout region leaves fewer bytes than
// the layout's own u32 length claims, which is the `20 + L > bytes` guard's
// name: layout_malformed, refused, malformed FALSE (a name is not residue), no
// counter moved.
const cppT01F4 = cppT01Preface + `
    for ( int64_t cut = head; cut < at0; ++cut )
    {
        Lineage got[4];
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( got, 4, buffer, cut, plan, 4096, NULL, &lr );
        if ( m != -1 || !lr.refused || lr.reason != layout_malformed || lr.malformed )
            { printf( "F4: cut=%lld m=%lld refused=%d reason=%d malformed=%d\n", (long long) cut, (long long) m, (int) lr.refused, (int) lr.reason, (int) lr.malformed ); return 1; }
        if ( lr.unknown != 0 || lr.kind_mismatch != 0 || lr.clamped != 0 || lr.widened != 0 || lr.duplicate != 0 )
            { printf( "F4: cut=%lld moved a counter\n", (long long) cut ); return 1; }
    }
`

// ---- F7: malformed, under 20 bytes -----------------------------------------
//
// Fewer than a whole fixed header is the residue of a truncation: malformed,
// no refusal by name, reason untouched, no counter moved.
const cppT01F7 = cppT01Preface + `
    for ( int64_t k = 0; k < head; ++k )
    {
        Lineage got[4];
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( got, 4, buffer, k, plan, 4096, NULL, &lr );
        if ( m != -1 || lr.malformed != 1 || lr.refused )
            { printf( "F7: k=%lld m=%lld malformed=%d refused=%d\n", (long long) k, (long long) m, (int) lr.malformed, (int) lr.refused ); return 1; }
        if ( lr.reason != 0 ) { printf( "F7: k=%lld reason moved to %d\n", (long long) k, (int) lr.reason ); return 1; }
        if ( lr.unknown != 0 || lr.kind_mismatch != 0 || lr.clamped != 0 || lr.widened != 0 || lr.duplicate != 0 )
            { printf( "F7: k=%lld moved a counter\n", (long long) k ); return 1; }
    }
`

// ---- F8: malformed, ragged tail --------------------------------------------
//
// Appending 1 .. record_bytes-1 bytes leaves a tail that is not a whole record:
// malformed. Appending a WHOLE record is not a ragged tail; it walks off the
// end into no_layout because the fifth record's hash names nothing.
const cppT01F8 = cppT01Preface + `
    for ( int64_t extra = 1; extra < recbytes; ++extra )
    {
        Lineage got[4];
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( got, 8, buffer, written + extra, plan, 4096, NULL, &lr );
        if ( m != -1 || lr.malformed != 1 || lr.refused )
            { printf( "F8: extra=%lld m=%lld malformed=%d refused=%d\n", (long long) extra, (long long) m, (int) lr.malformed, (int) lr.refused ); return 1; }
        if ( lr.reason != 0 ) { printf( "F8: extra=%lld reason moved to %d\n", (long long) extra, (int) lr.reason ); return 1; }
        if ( lr.unknown != 0 || lr.kind_mismatch != 0 || lr.clamped != 0 || lr.widened != 0 || lr.duplicate != 0 )
            { printf( "F8: extra=%lld moved a counter\n", (long long) extra ); return 1; }
    }
    {
        Lineage got[8];
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( got, 8, buffer, written + recbytes, plan, 4096, NULL, &lr );
        if ( m != -1 || !lr.refused || lr.reason != no_layout || lr.malformed )
            { printf( "F8: extra==record_bytes is one more whole record (no_layout), not a ragged tail: m=%lld refused=%d reason=%d malformed=%d\n", (long long) m, (int) lr.refused, (int) lr.reason, (int) lr.malformed ); return 1; }
    }
`

// ---- F12: second layout for a held hash ------------------------------------
//
// The header's hash is one this reader holds; its layout bytes are NOT. Both a
// changed byte and a changed length are `layout_malformed`.
const cppT01F12 = cppT01Preface + `
    {
        static uint8_t f[1 << 16];
        memcpy( f, buffer, (size_t) written );
        f[head + (L / 2)] ^= 0xFF; /* a second set of bytes for a held hash */
        Lineage got[4];
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( got, 4, f, written, plan, 4096, NULL, &lr );
        if ( m != -1 || !lr.refused || lr.reason != layout_malformed || lr.malformed )
            { printf( "F12: a changed byte: m=%lld refused=%d reason=%d malformed=%d\n", (long long) m, (int) lr.refused, (int) lr.reason, (int) lr.malformed ); return 1; }
    }
    {
        static uint8_t f[1 << 16];
        memcpy( f, buffer, (size_t) written );
        TableFixedPut32( f + kTableFixedHeaderBytes, (uint32_t) ( L - 1 ) ); /* a second LENGTH for a held hash */
        Lineage got[4];
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( got, 4, f, written, plan, 4096, NULL, &lr );
        if ( m != -1 || !lr.refused || lr.reason != layout_malformed || lr.malformed )
            { printf( "F12: a changed length: m=%lld refused=%d reason=%d malformed=%d\n", (long long) m, (int) lr.refused, (int) lr.reason, (int) lr.malformed ); return 1; }
    }
`

// ---- F9: batch_too_large ---------------------------------------------------
//
// Four records against a capacity of three is a refusal by name, found from the
// count before a body is decoded: REFUSE total, the destination poisoned 0x5A
// stays 0x5A.
const cppT01F9 = cppT01Preface + `
    {
        static uint8_t dest[1 << 16];
        memset( dest, 0x5A, sizeof( dest ) );
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( (Lineage *) (void *) dest, 3, buffer, written, plan, 4096, NULL, &lr );
        if ( m != -1 || !lr.refused || lr.reason != batch_too_large || lr.malformed )
            { printf( "F9: m=%lld refused=%d reason=%d malformed=%d\n", (long long) m, (int) lr.refused, (int) lr.reason, (int) lr.malformed ); return 1; }
        if ( lr.unknown != 0 || lr.kind_mismatch != 0 || lr.clamped != 0 || lr.widened != 0 || lr.duplicate != 0 )
            { printf( "F9: a counter moved\n" ); return 1; }
        const uint8_t * p = dest;
        for ( int64_t i = 0; i < (int64_t) sizeof( Lineage ) * 3; ++i )
            if ( p[i] != 0x5A ) { printf( "F9: byte %lld is 0x%02x, not the poison: batch_too_large wrote a destination byte\n", (long long) i, p[i] ); return 1; }
    }
`

// ---- F10: no_layout --------------------------------------------------------
//
// A record whose own hash is not the file's is a refusal by name, and nothing
// is decoded.
const cppT01F10 = cppT01Preface + `
    {
        static uint8_t f[1 << 16];
        memcpy( f, buffer, (size_t) written );
        TableFixedPut64( f + at0, 0xDEADBEEFCAFEF00DULL ); /* the record hash names no layout */
        Lineage got[4];
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( got, 4, f, written, plan, 4096, NULL, &lr );
        if ( m != -1 || !lr.refused || lr.reason != no_layout || lr.malformed )
            { printf( "F10: m=%lld refused=%d reason=%d malformed=%d\n", (long long) m, (int) lr.refused, (int) lr.reason, (int) lr.malformed ); return 1; }
        if ( lr.unknown != 0 || lr.kind_mismatch != 0 || lr.clamped != 0 || lr.widened != 0 || lr.duplicate != 0 )
            { printf( "F10: a counter moved\n" ); return 1; }
    }
`

// ---- R7: the identity lane is an index comparison --------------------------
//
// The reader's own hash is the LAST lineage entry, so the identity lane is an
// index lookup against a compile-time constant, never a hash recomputed from
// the layout bytes. The proof is behavioural: an identity read handed a plan
// cache compiles nothing (the elixir bug compiled on every read of its own
// files, §5.3/line 858).
const cppT01R7 = cppT01Preface + `
    {
        if ( LineageFixedLineage[LineageFixedLineageCount - 1] != LineageFixedHash )
            { printf( "R7: the reader's own hash is not the last lineage entry: the identity lane is not an index comparison\n" ); return 1; }
        TableFixedPlanCache cache; TableFixedPlanCacheInit( cache, cacheStore, 4096 );
        Lineage got[4];
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( got, 4, buffer, written, plan, 4096, &cache, &lr );
        if ( m != 4 || lr.malformed || lr.refused )
            { printf( "R7: the identity read did not land: m=%lld malformed=%d refused=%d\n", (long long) m, (int) lr.malformed, (int) lr.refused ); return 1; }
        if ( cache.compiles != 0 || cache.used != 0 )
            { printf( "R7: the identity lane compiled a plan: compiles=%d used=%d\n", cache.compiles, cache.used ); return 1; }
    }
`

// ---- R8: a hash in no lineage entry -> layout_newer ------------------------
//
// The header's hash names no entry: layout_newer, reporting the file's hash AND
// NOTHING ELSE, refused, no counter moved, not one destination byte written.
const cppT01R8 = cppT01Preface + `
    {
        static uint8_t f[1 << 16];
        memcpy( f, buffer, (size_t) written );
        const uint64_t forged = 0x0123456789ABCDEFULL;
        TableFixedPut64( f + kTableFixedHashAt, forged ); /* a hash in no lineage entry */
        static uint8_t dest[1 << 16];
        memset( dest, 0x5A, sizeof( dest ) );
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( (Lineage *) (void *) dest, 4, f, written, plan, 4096, NULL, &lr );
        if ( m != -1 || !lr.refused || lr.reason != layout_newer || lr.malformed )
            { printf( "R8: m=%lld refused=%d reason=%d malformed=%d\n", (long long) m, (int) lr.refused, (int) lr.reason, (int) lr.malformed ); return 1; }
        if ( lr.layout_hash != forged )
            { printf( "R8: reported hash 0x%llx, not the file's 0x%llx\n", (unsigned long long) lr.layout_hash, (unsigned long long) forged ); return 1; }
        if ( lr.unknown != 0 || lr.kind_mismatch != 0 || lr.clamped != 0 || lr.widened != 0 || lr.duplicate != 0 || lr.retained != 0 || lr.retain_lost != 0 )
            { printf( "R8: layout_newer reports the hash AND NOTHING ELSE; a counter moved\n" ); return 1; }
        const uint8_t * p = dest;
        for ( int64_t i = 0; i < (int64_t) sizeof( Lineage ) * 4; ++i )
            if ( p[i] != 0x5A ) { printf( "R8: byte %lld is 0x%02x: a refusal wrote a destination byte\n", (long long) i, p[i] ); return 1; }
    }
`

// ---- R9: select known layouts and refuse unsupported input ------------------
//
// The capability: a file written under the OLD layout (three int32s, same hash
// the new reader holds as lineage entry 0) is READ through the compiled plan —
// every old value lands exactly and the widened tail takes the reader's default
// 77 — and a known hash whose layout length or bytes differ is REFUSED with
// layout_malformed. The old file is built here from the compiler's OWN lineage
// constants (the hash, layout bytes and record size it laid down), never from a
// sibling runtime.
const cppT01R9 = cppT01Preface + `
    {
        const uint64_t H0 = LineageFixedLineage[0];
        const int64_t L0 = LineageFixedKnown[0].layout_bytes;
        const int64_t RB0 = LineageFixedKnown[0].record_bytes;
        if ( RB0 != 20 || L0 <= 0 || LineageFixedLineageCount < 2 )
            { printf( "R9: the old field_append entry is not the three-int32 layout: RB0=%lld L0=%lld count=%d\n", (long long) RB0, (long long) L0, (int) LineageFixedLineageCount ); return 1; }
        static uint8_t ob[1 << 16];
        ob[0] = kTableFixedForm;
        memset( ob + 1, 0, (size_t) ( kTableFixedHashAt - 1 ) );
        TableFixedPut64( ob + kTableFixedHashAt, H0 );
        TableFixedPut32( ob + kTableFixedHeaderBytes, (uint32_t) L0 );
        memcpy( ob + kTableFixedHeaderBytes + 4, LineageFixedKnown[0].layout, (size_t) L0 );
        const int64_t ore = kTableFixedHeaderBytes + 4 + L0;
        TableFixedPut64( ob + ore, H0 );
        TableFixedPut32( ob + ore + 8,  0x11111111u ); /* x */
        TableFixedPut32( ob + ore + 12, 0x22222222u ); /* y */
        TableFixedPut32( ob + ore + 16, 0x33333333u ); /* z */
        const int64_t olen = ore + RB0;
        Lineage got[2];
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( got, 2, ob, olen, plan, 4096, NULL, &lr );
        if ( m != 1 || lr.malformed || lr.refused )
            { printf( "R9: the old file did not read through the lineage: m=%lld malformed=%d refused=%d reason=%d\n", (long long) m, (int) lr.malformed, (int) lr.refused, (int) lr.reason ); return 1; }
        if ( got[0].x != 0x11111111 || got[0].y != 0x22222222 || got[0].z != 0x33333333 )
            { printf( "R9: the old values did not land: x=%d y=%d z=%d\n", got[0].x, got[0].y, got[0].z ); return 1; }
        if ( got[0].w != 77 )
            { printf( "R9: the reader's tail default did not land: w=%d, not 77\n", got[0].w ); return 1; }
        {
            static uint8_t f[1 << 16];
            memcpy( f, ob, (size_t) olen );
            TableFixedPut32( f + kTableFixedHeaderBytes, (uint32_t) ( L0 - 1 ) ); /* a known hash, a different LENGTH */
            TableReport mr; memset( &mr, 0, sizeof( mr ) );
            int64_t mm = LineageFixedLoad( got, 2, f, olen, plan, 4096, NULL, &mr );
            if ( mm != -1 || !mr.refused || mr.reason != layout_malformed || mr.malformed )
                { printf( "R9: a different layout length is not layout_malformed: mm=%lld refused=%d reason=%d\n", (long long) mm, (int) mr.refused, (int) mr.reason ); return 1; }
        }
        {
            static uint8_t f[1 << 16];
            memcpy( f, ob, (size_t) olen );
            f[kTableFixedHeaderBytes + 4] ^= 0xFF; /* a known hash, different BYTES */
            TableReport mr; memset( &mr, 0, sizeof( mr ) );
            int64_t mm = LineageFixedLoad( got, 2, f, olen, plan, 4096, NULL, &mr );
            if ( mm != -1 || !mr.refused || mr.reason != layout_malformed || mr.malformed )
                { printf( "R9: different layout bytes are not layout_malformed: mm=%lld refused=%d reason=%d\n", (long long) mm, (int) mr.refused, (int) mr.reason ); return 1; }
        }
    }
`

// ---- R12: the per-record hash check is before the prefill -------------------
//
// The same old file's header hash selects the OLD lineage entry, so a compiled
// plan with a NONEMPTY fill list (w = 77) is in hand when step 11 compares the
// record's own hash. Invert that record hash; the refusal is no_layout and the
// destination, poisoned 0x5A after Reset and before the load, is still 0x5A in
// every byte — not the prefill's 77, not a zero.
const cppT01R12 = cppT01Preface + `
    {
        const uint64_t H0 = LineageFixedLineage[0];
        const int64_t L0 = LineageFixedKnown[0].layout_bytes;
        const int64_t RB0 = LineageFixedKnown[0].record_bytes;
        static uint8_t ob[1 << 16];
        ob[0] = kTableFixedForm;
        memset( ob + 1, 0, (size_t) ( kTableFixedHashAt - 1 ) );
        TableFixedPut64( ob + kTableFixedHashAt, H0 );
        TableFixedPut32( ob + kTableFixedHeaderBytes, (uint32_t) L0 );
        memcpy( ob + kTableFixedHeaderBytes + 4, LineageFixedKnown[0].layout, (size_t) L0 );
        const int64_t ore = kTableFixedHeaderBytes + 4 + L0;
        TableFixedPut64( ob + ore, H0 );
        TableFixedPut32( ob + ore + 8,  0x11111111u );
        TableFixedPut32( ob + ore + 12, 0x22222222u );
        TableFixedPut32( ob + ore + 16, 0x33333333u );
        const int64_t olen = ore + RB0;
        static uint8_t f[1 << 16];
        memcpy( f, ob, (size_t) olen );
        for ( int i = 0; i < 8; ++i ) { f[ore + i] = (uint8_t) ~f[ore + i]; } /* the per-record hash, inverted */
        static uint8_t dest[1 << 16];
        memset( dest, 0x5A, sizeof( dest ) );
        TableReport r2; memset( &r2, 0, sizeof( r2 ) );
        int64_t m = LineageFixedLoad( (Lineage *) (void *) dest, 2, f, olen, plan, 4096, NULL, &r2 );
        if ( m != -1 || !r2.refused || r2.reason != no_layout || r2.malformed )
            { printf( "R12: m=%lld refused=%d reason=%d malformed=%d\n", (long long) m, (int) r2.refused, (int) r2.reason, (int) r2.malformed ); return 1; }
        if ( r2.clamped != 0 || r2.unknown != 0 || r2.kind_mismatch != 0 || r2.widened != 0 || r2.duplicate != 0 )
            { printf( "R12: a counter moved\n" ); return 1; }
        const uint8_t * p = dest;
        for ( int64_t i = 0; i < (int64_t) sizeof( Lineage ) * 2; ++i )
            if ( p[i] != 0x5A ) { printf( "R12: byte %lld is 0x%02x — the prefill ran before the record hash check\n", (long long) i, p[i] ); return 1; }
    }
`

// ---- R13: REFUSE is total --------------------------------------------------
//
// A refusal by name is never also `malformed`; every counter stays zero and not
// one destination byte is written. The layout_newer refusal is the witness.
const cppT01R13 = cppT01Preface + `
    {
        static uint8_t f[1 << 16];
        memcpy( f, buffer, (size_t) written );
        TableFixedPut64( f + kTableFixedHashAt, 0xFEEDFACECAFEBEEFULL );
        static uint8_t dest[1 << 16];
        memset( dest, 0xA5, sizeof( dest ) );
        TableReport lr; memset( &lr, 0, sizeof( lr ) );
        int64_t m = LineageFixedLoad( (Lineage *) (void *) dest, 4, f, written, plan, 4096, NULL, &lr );
        if ( m != -1 ) { printf( "R13: a refusal returns -1, not %lld\n", (long long) m ); return 1; }
        if ( !lr.refused || lr.malformed ) { printf( "R13: refused=%d malformed=%d — refused+reason and malformed are never both set\n", (int) lr.refused, (int) lr.malformed ); return 1; }
        if ( lr.reason != layout_newer ) { printf( "R13: reason=%d, not layout_newer\n", (int) lr.reason ); return 1; }
        if ( lr.unknown != 0 || lr.kind_mismatch != 0 || lr.clamped != 0 || lr.widened != 0 || lr.duplicate != 0 || lr.retained != 0 || lr.retain_lost != 0 )
            { printf( "R13: a counter moved on a refusal\n" ); return 1; }
        const uint8_t * p = dest;
        for ( int64_t i = 0; i < (int64_t) sizeof( Lineage ) * 4; ++i )
            if ( p[i] != 0xA5 ) { printf( "R13: byte %lld is 0x%02x — REFUSE wrote a destination byte\n", (long long) i, p[i] ); return 1; }
    }
`

// TestFixedRoadmapCppT01Framing is table-driven, one subtest per task id, and
// every subtest opens with t.Parallel().
func TestFixedRoadmapCppT01Framing(t *testing.T) {
	t.Parallel()
	schema := filepath.Join("..", "..", "..", "test", "tables", cppT01Schema)
	table := []struct {
		id   string
		body string
	}{
		{id: "cpp/F4", body: cppT01F4},
		{id: "cpp/F7", body: cppT01F7},
		{id: "cpp/F8", body: cppT01F8},
		{id: "cpp/F12", body: cppT01F12},
		{id: "cpp/F9", body: cppT01F9},
		{id: "cpp/F10", body: cppT01F10},
		{id: "cpp/R7", body: cppT01R7},
		{id: "cpp/R8", body: cppT01R8},
		{id: "cpp/R9/remaining-boundaries", body: cppT01R9},
		{id: "cpp/R12", body: cppT01R12},
		{id: "cpp/R13", body: cppT01R13},
	}
	for _, c := range table {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			out, err := cppRunCapability(t, schema, cppT01Pkg, c.body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", c.id, err, out)
			}
		})
	}
}
