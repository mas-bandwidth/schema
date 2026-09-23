/* c/F12 — second layout for a held hash (docs/roadmap.sexp, matrix schema#876,
   audit schema#898, task c/F12, row group "file-envelope").

   THE LAW (docs/FIXED-FORM-ALGORITHM.md §2 the load's steps, step 7 at line 148,
   cross-referenced docs/SPEC-TABLES.md §5.3, §7032 the same law in the form's
   own language, bill §4 row 7):

     a layout is NAMED BY ITS HASH, and the header's hash is taken AS GIVEN —
     nothing recomputes it from the wire because the definitions digest is not
     on it. A second layout for a hash ALREADY HELD is refused by name:
     "a known hash whose bytes differ is LAYOUT_MALFORMED, a lie about a known
     version, one name — and §1.1's seven rules do not run at read time at all:
     no stranger's layout is ever walked". And §5.8's row 5 keeps a refusal
     TOTAL: no counter moves and no destination byte is written before the
     refusal fires.

   THE PRODUCTION PATH THIS TEST DRIVES: the entry point the c leg's own
   conformance driver reaches through K1Table.h's call table — here
   `root_fixed_load` (build/tables-generated-c/k1/K1Table.h:3569). That body
   reads the header's hash (step 4), selects a lineage entry by hash (step 5),
   and then at step 7 compares the file's layout bytes to the lock's recorded
   bytes for that hash (line 3629-3631). A byte that differs sets refused=1,
   reason=SCHEMA_TABLE_LAYOUT_MALFORMED, and returns -1 with no destination
   byte touched — and on the CONTRAST branch a hash no entry of the lineage
   holds is SCHEMA_TABLE_LAYOUT_NEWER, with `layout_hash` carrying the file's
   hash (§5.9 #7).

   THE VECTOR IS BUILT FROM THE LAW AND THE BUILD'S OWN FACTS — no corpus file.
   The held hash and the lock's own layout bytes are this build's own
   (root_fixed_hash, root_fixed_layout); the header carries the hash and the
   u32 length, then the layout, then one record of `root_fixed_record_bytes`
   bytes. The forge bends ONE byte INSIDE THE LAYOUT, past the header, without
   touching the header's hash or any reserved byte — the SAME held hash now
   sits over DIFFERENT layout bytes, which is the F12 lie and not any other
   name's. The CONTRAST carries an unknown hash (root_fixed_hash XOR 1) over
   the lock's correct layout bytes — a hash no lineage entry names, which is
   why the held-hash-over-different-bytes must be its own name and not that
   one.

   SELF-CONTAINED: includes only K1Table.h, links K1Table.c, depends on no
   other rows/ file and edits no shared file. */

#include <stdio.h>
#include <stdint.h>
#include <string.h>

#include "K1Table.h"

static int failures;

static void check( int ok, const char * what )
{
    printf( "%s: %s\n", ok ? "ok" : "FAIL", what );
    if ( !ok ) { failures = 1; }
}

int main( void )
{
    const uint64_t unknown_hash = root_fixed_hash ^ 1ull;
    const int64_t lock_layout_bytes = root_fixed_layout_bytes;
    const int64_t record_bytes = root_fixed_record_bytes;
    const int64_t total_bytes = (int64_t) kTableFixedHeaderBytes + 4 + lock_layout_bytes + record_bytes;
    static uint8_t clean[4096];
    static uint8_t bent[4096];
    static uint8_t other[4096];
    Root back;
    TableReport report;
    int64_t saved_bytes;
    int64_t n;

    /* THE CLEAN FILE — written by this build's own fixed-form writer. The
       header is the held hash over the lock's own layout bytes, with one
       record of declared width: the same wire the conformance driver's
       `wire` surface reads. */
    root_reset( &back );
    back.grade = GRADE_BRONZE;
    back.raw = 7;
    saved_bytes = root_fixed_save( &back, 1, clean, (int64_t) sizeof( clean ) );
    check( saved_bytes == total_bytes, "the one-record save writes the declared bytes" );

    /* THE CONTROL: the un-bent file, held hash, lock's own layout bytes — the
       reader HOLDS this layout and the bytes match, so the identity lane
       opens it. The refusal below is about the DIFFERENCE, not the file. */
    memset( &back, 0x5A, sizeof( back ) );
    memset( &report, 0, sizeof( report ) );
    n = root_fixed_load( &back, 1, clean, saved_bytes, NULL, 0, NULL, &report );
    check( n == 1, "CONTROL: the un-bent layout, under its held hash, opens" );
    check( back.grade == GRADE_BRONZE && back.raw == 7, "CONTROL: the un-bent read lands every field" );
    check( !report.refused && !report.malformed, "CONTROL: the un-bent read is a read, not a refusal" );
    check( report.unknown == 0 && report.clamped == 0 && report.kind_mismatch == 0 && report.widened == 0 && report.duplicate == 0,
           "CONTROL: the un-bent read moves no counter" );

    /* THE FORGE: copy the whole file, bend ONE byte INSIDE THE LAYOUT, past
       the header (16) and the u32 length (4). The header's hash and the seven
       reserved bytes are untouched, the entry count stays the same, the
       layout length stays the same — only the layout's content is different,
       and only §5.3 step 7 fires. We pick an offset past the entry-count
       u32 (offset 16+4+4=24) so the entry-count check would still pass and
       step 7 is the only check that can refuse. */
    memcpy( bent, clean, (size_t) saved_bytes );
    bent[(int64_t) kTableFixedHeaderBytes + 4 + 4 + 2] ^= 0x99;

    /* POISON: a refused read touches no destination byte (§5.8 row 5, §5.3
       step 7's comment), so back must come back with the bytes it had on
       entry, byte for byte. */
    memset( &back, 0x5A, sizeof( back ) );
    memset( &report, 0, sizeof( report ) );
    n = root_fixed_load( &back, 1, bent, total_bytes, NULL, 0, NULL, &report );
    check( n == -1, "F12: a second layout for a held hash refuses by name" );
    check( report.refused && !report.malformed,
           "F12: the refusal is a refusal by name, not framing damage" );
    check( report.reason == SCHEMA_TABLE_LAYOUT_MALFORMED,
           "F12: the name is layout_malformed" );
    check( report.unknown == 0 && report.clamped == 0 && report.kind_mismatch == 0 && report.widened == 0 && report.duplicate == 0,
           "F12: no counter moved on the refused read" );
    {
        int64_t i;
        int poisoned = 0;
        for ( i = 0; i < (int64_t) sizeof( back ); ++i )
        {
            if ( ( (const uint8_t *) &back )[i] != 0x5A ) { poisoned = 1; break; }
        }
        check( !poisoned, "F12: no destination byte was written by the refused read" );
    }

    /* THE CONTRAST — a hash no lineage entry names, over the lock's OWN
       layout bytes. §5.3 step 5 refuses this with SCHEMA_TABLE_LAYOUT_NEWER
       and `layout_hash` carries the FILE'S hash (§5.9 #7). The held-hash
       lie above is layout_malformed for a DIFFERENCE in the bytes; this is
       layout_newer for an UNKNOWN hash — the operator's two distinct
       answers, and the contrast is what makes F12 discriminating. */
    memcpy( other, clean, (size_t) total_bytes );
    table_fixed_put64( other + kTableFixedHashAt, unknown_hash );
    table_fixed_put64( other + kTableFixedHeaderBytes + 4 + lock_layout_bytes, unknown_hash );

    memset( &back, 0x5A, sizeof( back ) );
    memset( &report, 0, sizeof( report ) );
    n = root_fixed_load( &back, 1, other, total_bytes, NULL, 0, NULL, &report );
    check( n == -1, "F12 CONTRAST: an unknown hash refuses" );
    check( report.refused && !report.malformed,
           "F12 CONTRAST: the refusal is a refusal by name, not damage" );
    check( report.reason == SCHEMA_TABLE_LAYOUT_NEWER,
           "F12 CONTRAST: an unknown hash names a different refusal" );
    check( report.layout_hash == unknown_hash,
           "F12 CONTRAST: layout_newer reports the file's hash" );

    if ( failures )
    {
        printf( "c/F12: RED\n" );
        return 1;
    }
    printf( "c/F12: GREEN — a second layout for a held hash refuses layout_malformed by name; an unknown hash refuses layout_newer\n" );
    return 0;
}
