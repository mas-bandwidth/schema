/* R5.c — THE SCHEMA MATRIX CELL c/R5 (docs/roadmap.sexp:1484-1488):
   "the digest carries every range, every resolution (tag 'Q') and every
    reader limit (tag 'L'), and a flags type deduped by name, once".

   THE LAW (docs/FIXED-FORM-ALGORITHM.md:54-56, 579-589, §5.2):

     THE DEFINITIONS DIGEST (bill §13) is every fact of §5.1 that is not
     wire shape. Without it `int32 | 0..100` and `| 0..200` hash
     identically and an old reader cannot refuse the widening. ... byte
     layout, per field, declared order, depth first; each named type
     once: 'R' (min/max), 'Q' (resolution), 'B' (bits(N)), 'X'
     (fixed(I,F)), 'F' (flags), 'L' (reader limit). Pinned by tests in
     ir/fixedform_hash_test.go.

   AND row 10 of §5.8 (line 1130):

     | 10 | the digest carries only INTEGER ranges (`HasIntRange`) — a
     float range, a compressed float's resolution and a reader-side
     limit are still not in it, and a FLAGS type is re-emitted once per
     naming field because `seen` covers structs only | every range,
     every resolution (tag 'Q') and every limit, tag 'L' (bill §13), or
     the widening cannot be refused — **and a flags type deduped by NAME,
     once, as a struct is** (§5.2). An INTEROP BREAK, not a missed
     refusal: it moves the hash, so the reference owes it BEFORE any
     leg ports.


   TODAY'S DIGEST IS THE V2 DEFINITIONS DIGEST (WireLaw 3,
   docs/FIXED-FORM-ALGORITHM.md §5.2 "PROSPECTIVE V2 DEFINITIONS DIGEST",
   line 812, and "THE DIGEST, PINNED FOR A PORT"; docs/SPEC-TABLES.md
   line 8368). It supersedes the 49-byte digest this row first pinned
   (ir.TableFixedDefinitionsDigestLegacy, still in the IR as legacy
   evidence): every fixed root's digest BEGINS with the 26-byte identity
   prefix `\0schema-fixed-definitions\x02`; a row is `tag:u8,
   path_bytes:u32 LE, path, payload_bytes:u32 LE, payload`; a path
   segment is `role:u8, kind:u32 LE, wire_id:u64 LE`; the row tags are
   exactly C, R, D, Q, B, X, F. Two of the old laws moved with it, and
   each old assertion maps to its current one below:
     - "a flags type deduped by NAME, once" is RETIRED by WireLaw 3
       (§5.2 line 852: "Emit every reachable occurrence of a named
       struct, union or flags declaration at its wire path; do not
       deduplicate by name"). The old "exactly one 'F'" now reads:
       exactly one F row, at the path of the ONE field that names Caps
       (Vessel.caps), carrying 3 bits, 3 variants and the fnv1a64 of
       each variant name in declared order.
     - 'L' (reader limit) is not a v2 row tag (the tag list is C, R, D,
       Q, B, X, F). The old "no 'L' row" now reads: every row's tag is
       one of the seven, and Vessel (no float range, no bits(N), no
       fixed(I,F), no counted array) carries no C, D, Q, B or X row.
     - "every range": exactly one R row, at Vessel.hull, kind 4
       (int32), signed, 0 fractional bits, min 0 and max 1000 in the
       canonical sign + length-prefixed big-endian form.
   The central law is unchanged: the C leg's `*_fixed_hash` equals
   fnv1a64 over the layout THEN over the digest, with one running state
   (the Go leg's W12 row binds the same fold).

   The 149-byte Vessel digest below is the reference's own bytes,
   ir.TableFixedDefinitionsDigest(W1.Vessel), printed from
   test/tables/W1.schema on 2026-11-24. The row walk below does not trust
   the pin alone: it re-derives every id it checks (the root's
   fnv1a64("Vessel"), the fields' fnv1a64("caps") and fnv1a64("hull"),
   each variant's fnv1a64 of its name) and parses every row's framing.

   The negative control: edit one byte of `vessel_fixed_hash` in
   build/tables-generated-c/w1/W1Table.h (e.g. flip the top byte).
   The static constant then disagrees with fnv1a64(layout ++ digest),
   and the equality below goes RED. Restore — green.

   It reaches the generated C tables code the way the driver does
   (test/conformance/c/main.c, make/c.mk's C_CONFORMANCE_INCLUDES): one
   translation unit per unit of its own, here build/tables-generated-c/w1,
   with W1Table.h and W1Table.c (the latter for the non-inline helpers
   the header references: vessel_save wires its hash to the in-memory
   body, schema_tblw1_vessel_fixed_write_body_, and others). It depends
   on no other rows/ file and edits no shared file. */

#include <stdio.h>
#include <stdint.h>
#include <string.h>

#include "W1Table.h"

static int failed;

static void check( int cond, const char * what )
{
    if ( cond ) { printf( "ok: %s\n", what ); }
    else { printf( "FAILED: %s\n", what ); failed = 1; }
}

/* fnv1a64 over an arbitrary byte buffer. Same body as the C leg's
   `table_fixed_hash_of`. */
static uint64_t fnv1a64( const uint8_t * bytes, int64_t n )
{
    uint64_t h = 0xcbf29ce484222325ull;
    int64_t i;
    for ( i = 0; i < n; ++i )
    {
        h ^= (uint64_t) bytes[i];
        h *= 0x100000001b3ull;
    }
    return h;
}

static uint64_t fnv1a64_text( const char * s )
{
    return fnv1a64( (const uint8_t *) s, (int64_t) strlen( s ) );
}

static uint32_t le32( const uint8_t * b )
{
    return (uint32_t) b[0] | ( (uint32_t) b[1] << 8 ) | ( (uint32_t) b[2] << 16 ) | ( (uint32_t) b[3] << 24 );
}

static uint64_t le64( const uint8_t * b )
{
    return (uint64_t) le32( b ) | ( (uint64_t) le32( b + 4 ) << 32 );
}

/* THE V2 IDENTITY PREFIX (§5.2, WireLaw 3): every non-nil fixed root's
   digest begins with these 26 bytes. */
static const uint8_t identity_prefix[26] = {
    0x00, 's', 'c', 'h', 'e', 'm', 'a', '-', 'f', 'i', 'x', 'e', 'd', '-',
    'd', 'e', 'f', 'i', 'n', 'i', 't', 'i', 'o', 'n', 's', 0x02
};

/* THE EXPECTED DIGEST for W1's Vessel table, byte for byte (149 bytes):
   ir.TableFixedDefinitionsDigest(W1.Vessel).
     26  identity prefix
     'F' path 26 = t(13, fnv1a64 "Vessel") f(9, fnv1a64 "caps")
         payload 32 = bits 3, variants 3, fnv1a64 "Jump" "Crouch" "Fly"
     'R' path 26 = t(13, fnv1a64 "Vessel") f(4, fnv1a64 "hull")
         payload 21 = kind 4, signed 1, frac 0, min +0 (len 0),
                      max +0x03e8 (len 2) */
static const uint8_t vessel_expected_digest[149] = {
    0x00, 0x73, 0x63, 0x68, 0x65, 0x6d, 0x61, 0x2d, 0x66, 0x69, 0x78, 0x65, 0x64, 0x2d,
    0x64, 0x65, 0x66, 0x69, 0x6e, 0x69, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x02,
    /* 'F' row */
    0x46, 0x1a, 0x00, 0x00, 0x00,
    0x74, 0x0d, 0x00, 0x00, 0x00, 0x7b, 0x7b, 0x5d, 0xc2, 0x8e, 0x0c, 0x0d, 0x11,
    0x66, 0x09, 0x00, 0x00, 0x00, 0xf8, 0x57, 0x75, 0xe8, 0x90, 0x6b, 0x5a, 0xb5,
    0x20, 0x00, 0x00, 0x00,
    0x03, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00,
    0xad, 0x65, 0xa1, 0xf9, 0xe9, 0x77, 0xa4, 0x15,
    0xeb, 0x79, 0x94, 0x96, 0xf6, 0x43, 0x77, 0xcd,
    0x62, 0x1e, 0x8f, 0x9c, 0x19, 0xab, 0xb7, 0xf2,
    /* 'R' row */
    0x52, 0x1a, 0x00, 0x00, 0x00,
    0x74, 0x0d, 0x00, 0x00, 0x00, 0x7b, 0x7b, 0x5d, 0xc2, 0x8e, 0x0c, 0x0d, 0x11,
    0x66, 0x04, 0x00, 0x00, 0x00, 0xf6, 0xad, 0xda, 0x11, 0xcc, 0x8c, 0xda, 0x80,
    0x15, 0x00, 0x00, 0x00,
    0x04, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x02, 0x00, 0x00, 0x00, 0x03, 0xe8
};

/* A path of exactly two segments: the root t(13, root_id) then one field
   f(kind, field_id). */
static int path_is_root_field( const uint8_t * path, uint32_t path_n, uint64_t root_id,
                               uint32_t kind, uint64_t field_id )
{
    return path_n == 26u &&
           path[0] == 't' && le32( path + 1 ) == 13u && le64( path + 5 ) == root_id &&
           path[13] == 'f' && le32( path + 14 ) == kind && le64( path + 18 ) == field_id;
}

int main( void )
{
    const int64_t layout_n = (int64_t) sizeof( tblw1_vessel_fixed_layout );
    const int64_t digest_n = (int64_t) sizeof( vessel_expected_digest );

    /* THE PRINCIPAL DRIVER. fnv1a64 over a CONTIGUOUS byte vector
       (layout ++ digest) is what §5.2 specifies; we build the vector
       on the stack, fold it, and compare the result to the C leg's
       static `vessel_fixed_hash`. A C leg that folded only the layout
       would hash to the LAYOUT-ONLY number, and this assertion would
       FAIL — that is the bite of the control. */
    {
        uint8_t vector[ sizeof( tblw1_vessel_fixed_layout ) + sizeof( vessel_expected_digest ) ];
        memcpy( vector, tblw1_vessel_fixed_layout, (size_t) layout_n );
        memcpy( vector + layout_n, vessel_expected_digest, (size_t) digest_n );
        uint64_t folded = fnv1a64( vector, layout_n + digest_n );
        check( folded == tblw1_vessel_fixed_hash,
               "vessel_fixed_hash equals fnv1a64(layout ++ digest) (the §5.2 fold)" );
        printf( "  folded = 0x%016llx, vessel_fixed_hash = 0x%016llx\n",
                (unsigned long long) folded, (unsigned long long) tblw1_vessel_fixed_hash );

        /* THE COMPLEMENT: a hash of the LAYOUT ONLY must NOT match —
           the digest bytes are not in that fold. */
        uint64_t layout_only = tblw1_table_fixed_hash_of( tblw1_vessel_fixed_layout, layout_n );
        check( layout_only != tblw1_vessel_fixed_hash,
               "fnv1a64(layout only) != vessel_fixed_hash — the digest is folded in" );
        printf( "  layout_only = 0x%016llx\n", (unsigned long long) layout_only );
    }

    /* THE DIGEST'S ROWS, parsed by their framing (never by scanning for
       a tag byte that a hash could also hold). */
    {
        const uint64_t vessel_id = fnv1a64_text( "Vessel" );
        const uint8_t * d = vessel_expected_digest;
        int64_t at = (int64_t) sizeof( identity_prefix );
        int f_count = 0, r_count = 0, other_count = 0, bad_tag = 0, framing_ok = 1;
        int f_ok = 0, r_ok = 0;

        check( memcmp( d, identity_prefix, sizeof( identity_prefix ) ) == 0,
               "the digest begins with the 26-byte v2 identity prefix (WireLaw 3, §5.2)" );
        while ( at < digest_n )
        {
            uint8_t tag;
            uint32_t path_n, payload_n;
            const uint8_t * path;
            const uint8_t * payload;
            if ( at + 5 > digest_n ) { framing_ok = 0; break; }
            tag = d[at];
            path_n = le32( d + at + 1 );
            if ( at + 5 + (int64_t) path_n + 4 > digest_n ) { framing_ok = 0; break; }
            path = d + at + 5;
            payload_n = le32( path + path_n );
            payload = path + path_n + 4;
            if ( payload + payload_n > d + digest_n ) { framing_ok = 0; break; }
            at = (int64_t) ( payload + payload_n - d );
            switch ( tag )
            {
            case 'F':
                f_count++;
                f_ok = path_is_root_field( path, path_n, vessel_id, 9u, fnv1a64_text( "caps" ) ) &&
                       payload_n == 32u && le32( payload ) == 3u && le32( payload + 4 ) == 3u &&
                       le64( payload + 8 ) == fnv1a64_text( "Jump" ) &&
                       le64( payload + 16 ) == fnv1a64_text( "Crouch" ) &&
                       le64( payload + 24 ) == fnv1a64_text( "Fly" );
                break;
            case 'R':
            {
                static const uint8_t bounds[12] = { 0x00, 0x00, 0x00, 0x00, 0x00,
                                                    0x00, 0x02, 0x00, 0x00, 0x00, 0x03, 0xe8 };
                r_count++;
                r_ok = path_is_root_field( path, path_n, vessel_id, 4u, fnv1a64_text( "hull" ) ) &&
                       payload_n == 21u && le32( payload ) == 4u && payload[4] == 1u &&
                       le32( payload + 5 ) == 0u && memcmp( payload + 9, bounds, sizeof( bounds ) ) == 0;
                break;
            }
            case 'C': case 'D': case 'Q': case 'B': case 'X':
                other_count++;
                break;
            default:
                bad_tag++;
                break;
            }
        }
        check( framing_ok && at == digest_n,
               "every digest row frames exactly: tag, u32 path length, path, u32 payload length, payload" );
        check( bad_tag == 0,
               "every row tag is one of C R D Q B X F — no 'L' or other row (WireLaw 3 tag list)" );
        check( f_count == 1 && f_ok,
               "exactly one 'F' row, at Vessel.caps (kind 9): 3 bits, 3 variants, fnv1a64 Jump/Crouch/Fly in order" );
        check( r_count == 1 && r_ok,
               "exactly one 'R' row, at Vessel.hull (kind 4): signed, frac 0, min 0, max 1000 canonical" );
        check( other_count == 0,
               "Vessel has no counted array, float range, resolution, bits(N) or fixed(I,F): no C/D/Q/B/X row" );
    }

    /* THE WRITER STAMPS IT. `_fixed_save` writes the static
       `vessel_fixed_hash` into the file header at offset 8. Reading
       it back gives back the same number — the writer does not
       re-derive the hash. A breach here would be the same byte-level
       class as the static constant itself, and the assertion catches
       either of them. */
    {
        uint8_t buffer[2048];
        tblw1_vessel_t v;
        tblw1_table_report_t report;
        tblw1_table_fixed_entry_t plan[4096];
        memset( &v, 0, sizeof( v ) );
        memset( &report, 0, sizeof( report ) );
        int64_t written = tblw1_vessel_fixed_save( &v, 1, buffer,
                                             (int64_t) sizeof( buffer ) );
        check( written > 0, "vessel_fixed_save wrote the file" );
        uint64_t stamped;
        memcpy( &stamped, buffer + 8, sizeof( stamped ) );
        check( stamped == tblw1_vessel_fixed_hash,
               "the file header's 8-byte hash field equals vessel_fixed_hash" );
        printf( "  stamped = 0x%016llx\n", (unsigned long long) stamped );
        int64_t loaded = tblw1_vessel_fixed_load( &v, 1, buffer, written,
                                            plan, (int32_t) ( sizeof( plan ) / sizeof( plan[0] ) ),
                                            NULL, &report );
        check( loaded == 1 && report.refused == 0,
               "the writer-stamped record loads back, report clean" );
    }

    /* CHANGING A NAME MOVES THE HASH. One bit of the third flag's
       variant-name hash (Fly) flipped: the fold no longer matches the
       C leg's constant — the hash binds the names. */
    {
        uint8_t alt_digest[ sizeof( vessel_expected_digest ) ];
        uint8_t vector[ sizeof( tblw1_vessel_fixed_layout ) + sizeof( vessel_expected_digest ) ];
        memcpy( alt_digest, vessel_expected_digest, sizeof( alt_digest ) );
        alt_digest[26 + 5 + 26 + 4 + 8 + 16] ^= 0x01; /* first byte of fnv1a64("Fly") */
        memcpy( vector, tblw1_vessel_fixed_layout, (size_t) layout_n );
        memcpy( vector + layout_n, alt_digest, (size_t) digest_n );
        uint64_t folded = fnv1a64( vector, layout_n + digest_n );
        check( folded != tblw1_vessel_fixed_hash,
               "a single-byte change in the digest moves the hash — the digest binds the schema" );
        printf( "  folded_perturbed = 0x%016llx, vessel_fixed_hash = 0x%016llx\n",
                (unsigned long long) folded, (unsigned long long) tblw1_vessel_fixed_hash );
    }

    if ( failed ) { printf( "R5: RED\n" ); return 1; }
    printf( "R5: GREEN — the v2 digest carries the identity prefix, every range (R) "
            "and every flags (F) at its wire path; the C leg's vessel_fixed_hash "
            "equals fnv1a64(layout ++ digest)\n" );
    return 0;
}
