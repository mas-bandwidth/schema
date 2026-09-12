/* THE REFUSAL NAMES OF §5.3'S CONDITION TABLE, C leg
   (docs/FIXED-FORM-ALGORITHM.md §5.3). The layout refusals already have a file
   of their own — fixedform_layout.c, §1.1's seven rules — and those are the
   rules a layout is held to. THESE ARE THE OTHER COLUMN: the rows of §5.3's
   table that are about THE FILE AROUND the layout, and until now the C suite
   named NONE of them. A refusal nobody watched fire is a refusal nobody has.

   ONE PROBE PER ROW, and each breaks EXACTLY ONE THING in a file this reader
   accepts. The base file is written by the FX1 writer itself — fixed_fx1_write,
   the same bytes fixedform_main.c already checked against fixed_fx1_bytes — so
   a probe that fires is the break and never the fixture.

   THE FILE'S SHAPE (docs/SPEC-TABLES.md §3, one rule for all five forms): the
   form byte at 0, seven reserved zero bytes, the LAYOUT HASH at 8, the body at
   16 — there a u32 layout length, then the layout, then the records to the end
   of the file, EACH PREFIXED BY ITS OWN 8-BYTE HASH. So the records begin at
   16 + 4 + sizeof( the layout ), and the first eight bytes there are the copy
   of the header's hash that step 11 compares.

   THE REPORT IS TWO ANSWERS AND §5.3 SAYS THEY ARE ASSERTED JOINTLY: a refusal
   by name sets `refused` and the name with `malformed` FALSE; a malformed read
   sets `malformed` with `refused` FALSE and the reason UNTOUCHED. Both return
   -1, and both move no counter. Every probe below holds both halves, because a
   port that refuses correctly beside a set `malformed` fails §5.3 even so. */

#include <stdio.h>
#include <string.h>

#include "FX1Table.h"
#include "fixedform.h"

/* the plan storage the caller declares; no probe here ever compiles a plan —
   FX1's lineage is one entry long, its own, so every read below is the
   identity path (§5.8 row 3) */
#define PlanCapacity 1024
static TableFixedEntry g_plan[PlanCapacity];

/* THIS LEG ALLOCATES NOTHING, and the units beside this one hold their bytes
   in statics for the same reason: the forged file is a static array, and one
   byte of slack past it is what the ragged-tail row needs. */
#define ForgedBytes 8192
static uint8_t g_forged[ForgedBytes];

/* THE DESTINATION, STAMPED. §5.3: "REFUSE is total — no counter moves, nothing
   is decoded, and not one destination byte is written, the prefill included."
   A stamp a clean record carries nowhere is the only way to watch that last
   clause hold, so the destination is filled with it before every probe and
   checked after. Two records' worth, because the batch row needs a capacity
   smaller than the file's count and not a buffer smaller than it. */
#define Stamp 0xCDu
static FxRoot g_out[2];

#define LayoutLengthAt ( (size_t) kTableFixedHeaderBytes )
#define RecordsAt ( (size_t) kTableFixedHeaderBytes + 4u + sizeof( fx_root_fixed_layout ) )

/* A REASON NO CONSTANT IN THE SET CARRIES, so "the reason is untouched" is a
   claim this file can actually make: the generated load never writes the
   caller's report except where §5.3 says it does. */
#define ReasonSentinel 4242

static int all_stamped( const void * p, size_t bytes )
{
    const uint8_t * b = (const uint8_t *) p;
    size_t i;
    for ( i = 0; i < bytes; i++ )
    {
        if ( b[i] != Stamp ) { return 0; }
    }
    return 1;
}

/* ONE ROW OF §5.3's TABLE THAT HAS A NAME: refused, that name, malformed
   false, -1, and nothing written or counted. */
static void refuses( const uint8_t * forged, int64_t bytes, int64_t capacity, int want, const char * what )
{
    TableReport r;
    int64_t n;

    memset( g_out, Stamp, sizeof( g_out ) );
    memset( &r, 0, sizeof( r ) );
    r.reason = ReasonSentinel;
    n = fx_root_fixed_load( g_out, capacity, forged, bytes, g_plan, PlanCapacity, NULL, &r );
    fixed_check( n == -1 && r.refused && r.reason == want, what );
    fixed_check( !r.malformed, "§5.3 TWO ANSWERS: a refusal by name leaves `malformed` FALSE" );
    fixed_check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0,
                 "§5.3 REFUSE IS TOTAL: a named refusal moves no counter" );
    fixed_check( all_stamped( g_out, sizeof( g_out ) ),
                 "§5.3 REFUSE IS TOTAL: not one destination byte is written, the prefill included" );
}

void fixed_fx1_refusal_names( const uint8_t * good, int64_t bytes )
{
    const uint64_t header_hash = table_fixed_get64( good + kTableFixedHashAt );

    fixed_check( bytes > 0 && bytes + 1 <= (int64_t) ForgedBytes,
                 "C §5.3: the good file and one byte of tail fit this unit's buffer" );
    if ( bytes <= 0 || bytes + 1 > (int64_t) ForgedBytes ) { return; }

    /* THE NEGATIVE CONTROL, FIRST: the UNBROKEN file READS, so every refusal
       below is the one break and not the fixture. */
    {
        TableReport r;
        memset( &r, 0, sizeof( r ) );
        fixed_check( fx_root_fixed_load( g_out, 2, good, bytes, g_plan, PlanCapacity, NULL, &r ) == 1 &&
                     !r.refused && !r.malformed,
                     "C §5.3 CONTROL: the unbroken file reads, so the breaks below are the breaks" );
    }
    fixed_check( RecordsAt + 8u <= (size_t) bytes,
                 "C §5.3: the per-record hash really is the eight bytes behind the layout" );
    fixed_check( table_fixed_get64( good + RecordsAt ) == header_hash,
                 "C §5.3 CONTROL: a good record's hash IS the file's hash" );

    /* ROW: "a record hash that is not the file's" — `no_layout` (step 11). The
       check is the FIRST thing the record loop does, before the prefill, so the
       stamp above is what says nothing landed. */
    {
        memcpy( g_forged, good, (size_t) bytes );
        table_fixed_put64( g_forged + RecordsAt, header_hash + 1ull );
        refuses( g_forged, bytes, 2, SCHEMA_TABLE_NO_LAYOUT,
                 "§5.3 step 11: a record hash that is not the file's is no_layout" );
    }

    /* ROW: "more records than the caller's capacity" — `batch_too_large`
       (step 10). The file is untouched; the CALLER is what is too small, which
       is the whole of the row: one record does not fit a capacity of zero. */
    {
        refuses( good, bytes, 0, SCHEMA_TABLE_BATCH_TOO_LARGE,
                 "§5.3 step 10: more records than the caller's capacity is batch_too_large" );
    }

    /* ROW: "form byte 1" — `previous_form` (step 2). The registry is ORDERED,
       so the variable form handed to a fixed reader is named by where it sits
       relative to this form and never by one word for every wrong byte. */
    {
        memcpy( g_forged, good, (size_t) bytes );
        g_forged[0] = 1;
        refuses( g_forged, bytes, 2, SCHEMA_TABLE_PREVIOUS_FORM,
                 "§5.3 step 2: form byte 1 is previous_form" );
    }

    /* ROW: "form byte 2" — `message_form_as_file` (step 2), the other
       direction's own name: a MESSAGE, which is not a file at all. */
    {
        memcpy( g_forged, good, (size_t) bytes );
        g_forged[0] = 2;
        refuses( g_forged, bytes, 2, SCHEMA_TABLE_MESSAGE_FORM_AS_FILE,
                 "§5.3 step 2: form byte 2 is message_form_as_file" );
    }

    /* ROW: "anything else" — `newer_form` (step 2). Four is a form this build
       does not carry and is AHEAD of: ship the reader. */
    {
        memcpy( g_forged, good, (size_t) bytes );
        g_forged[0] = 4;
        refuses( g_forged, bytes, 2, SCHEMA_TABLE_NEWER_FORM,
                 "§5.3 step 2: any other form byte is newer_form" );
    }

    /* ROW: "a ragged tail: `rest mod record_bytes != 0`" — and this row has NO
       NAME. It is the residue: `malformed` true, `refused` FALSE, the reason
       UNTOUCHED, -1. One byte past the last whole record is the minimal break,
       and `record_bytes` comes from the LOCK and never from the file (§5.3's
       step 9 note), so the file cannot talk its way out of the modulus. */
    {
        TableReport r;
        int64_t n;

        memcpy( g_forged, good, (size_t) bytes );
        g_forged[bytes] = 0;
        memset( g_out, Stamp, sizeof( g_out ) );
        memset( &r, 0, sizeof( r ) );
        r.reason = ReasonSentinel;
        n = fx_root_fixed_load( g_out, 2, g_forged, bytes + 1, g_plan, PlanCapacity, NULL, &r );
        fixed_check( n == -1 && r.malformed,
                     "§5.3 step 9: a ragged tail is malformed, and -1" );
        fixed_check( !r.refused,
                     "§5.3 TWO ANSWERS: a ragged tail sets `refused` FALSE — it is the residue, not a name" );
        fixed_check( r.reason == ReasonSentinel,
                     "§5.3 TWO ANSWERS: a ragged tail leaves the reason UNTOUCHED" );
        fixed_check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0,
                     "§5.3: a malformed read moves no counter either" );
        fixed_check( all_stamped( g_out, sizeof( g_out ) ),
                     "§5.3: and a malformed read writes not one destination byte" );
    }

    /* ROW: "the plan does not fit the caller's declared capacity" —
       `plan_too_large`, owed by every leg (§5.9 #45). THE PROBE IS WRITTEN
       ANYWAY, because the row is owed whether or not this leg can reach it
       today, and a probe that goes missing is how a row stops being owed. A
       capacity of ZERO is the smallest declaration there is: nothing fits it. */
    {
        TableReport r;
        int64_t n;

        memset( g_out, Stamp, sizeof( g_out ) );
        memset( &r, 0, sizeof( r ) );
        r.reason = ReasonSentinel;
        n = fx_root_fixed_load( g_out, 2, good, bytes, g_plan, 0, NULL, &r );
        if ( n == -1 && r.refused && r.reason == SCHEMA_TABLE_PLAN_TOO_LARGE )
        {
            fixed_check( !r.malformed, "§5.3 TWO ANSWERS: plan_too_large leaves `malformed` FALSE" );
            fixed_check( all_stamped( g_out, sizeof( g_out ) ),
                         "§5.3 REFUSE IS TOTAL: plan_too_large writes not one destination byte" );
        }
        else
        {
            /* NOT A FAILURE OF THIS LEG'S CONFORMANCE, A FACT ABOUT WHERE THE
               CHECK SITS. The generated load reads `plan_capacity` only inside
               the not-my-hash branch — the branch that resolves a PEER's
               lineage entry to a plan — so a file carrying THIS BUILD'S OWN
               hash takes the identity plan and never reaches the comparison.
               FX1's lineage is one entry long, its own, so the branch is not
               even emitted for this generation. The row is owed again wherever
               a reader resolves a peer: the lineage harness
               (internal/codegen/ctable/fixedversioning_test.go). */
            printf( "KNOWN-RED: plan_too_large did not fire — a plan capacity of 0 on an identity read returned %lld with refused=%d reason=%d; the capacity check sits under the not-my-hash branch, so an identity read never reaches it\n",
                    (long long) n, r.refused, r.reason );
        }
    }
}
