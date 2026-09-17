/* FX1's half of the fixed form's versioning conformance (docs/SPEC-TABLES.md
   §3.4). This translation unit is the ONLY one that names tblfx1's types; see
   fixedform.h for why there is more than one. */

#include <stdio.h>
#include <string.h>

#include "FX1Table.h"
#include "fixedform.h"

/* The plan storage the caller owns. The identity path never touches it; a
   plan compiled from another writer's layout lands in it, and a layout whose
   plan does not fit is a refusal by name (§3.4: this codec never allocates). */
#define PlanCapacity 8192
static TableFixedEntry g_plan[PlanCapacity];

static void fill( FxRoot * value )
{
    fx_root_reset( value );
    value->keep = 4242u;
    value->narrow = 40000u;   /* a uint16 value FX2's widened read must reproduce */
    value->renamed = 321;
    value->gone = 654;
    value->nested.a = 111;
    value->nested.b = 222;
    value->label[0] = 'h';
    value->label[1] = 'i';
    value->label_length = 2;
    value->marks[0] = 7;
    value->marks_count = 1;
    /* A `bytes(N)` IS AN ARRAY OF u8 ON THIS WIRE (§3.4), so its destination
       row is an ARRAY's — the buffer, and the live length beside it — and not
       a text field's, which is the other way round. Only a COMPILED plan reads
       those columns, so only FX2's read of this record can tell. */
    value->blob[0] = 0xDE; value->blob[1] = 0xAD; value->blob[2] = 0xBE; value->blob[3] = 0xEF;
    value->blob_length = 4;
}

/* THE SLACK IS ZERO (docs/SPEC-TABLES.md §3.4), and this is the C twin of the
   reference's slack_case. A `string(N)` shorter than N and a `[..N]T` with
   unused slots are DECLARED bytes carrying no value: what rides in them is the
   TEMPLATE'S ZEROS, never whatever this writer's storage held past the used
   length or the live count.

   THE CONTROL IS THE STAIN: the storage past the used length and the live
   count is filled with a byte a clean record carries nowhere, the test proves
   the stain IS in the storage, then proves the WIRE carries none of it, then
   proves a whole-span copy of the same storage WOULD have carried it. */
void fixed_fx1_slack( void )
{
    static uint8_t file[8192];
    static TableFixedEntry plan[PlanCapacity];
    FxRoot v, back;
    TableReport r;
    const uint8_t * body;
    size_t body_bytes;
    uint8_t whole[sizeof( v.label )];
    int32_t marks[4];
    int k;
    int64_t need, n;

    fx_root_reset( &v );
    v.keep = 11u;
    v.narrow = 22u;
    v.renamed = 33;
    v.gone = 44;
    v.nested.a = 55;
    v.nested.b = 66;
    memset( v.label, 0xAA, sizeof( v.label ) );
    v.label[0] = 'h';
    v.label[1] = 'i';
    v.label_length = 2;
    for ( k = 0; k < 4; k++ ) { v.marks[k] = 0x5A5A5A5A; }
    v.marks[0] = 7;
    v.marks_count = 1;

    fixed_check( (uint8_t) v.label[2] == 0xAAu, "C CONTROL: the text slack really is stained in storage" );
    fixed_check( v.marks[1] == 0x5A5A5A5A, "C CONTROL: the array slack really is stained in storage" );

    need = fx_root_fixed_measure( 1 );
    fixed_check( need <= (int64_t) sizeof( file ), "C slack: the record fits the buffer" );
    fixed_check( fx_root_fixed_save( &v, 1, file, (int64_t) sizeof( file ) ) == need, "C slack: the record saves" );
    body = file + kTableFixedHeaderBytes + 4 + (int64_t) sizeof( fx_root_fixed_layout ) + 8;
    body_bytes = (size_t) fx_root_fixed_body_bytes;
    fixed_check( memchr( body, 0xAA, body_bytes ) == NULL,
                 "C SLACK IS ZERO: not one stained TEXT byte reached the wire" );
    fixed_check( memchr( body, 0x5A, body_bytes ) == NULL,
                 "C SLACK IS ZERO: not one stained ARRAY byte reached the wire" );

    memcpy( whole, v.label, sizeof( v.label ) );
    fixed_check( memchr( whole, 0xAA, sizeof( whole ) ) != NULL,
                 "C NEGATIVE CONTROL: a whole-span copy WOULD have carried the text stain" );
    memcpy( marks, v.marks, sizeof( marks ) );
    fixed_check( marks[3] == 0x5A5A5A5A,
                 "C NEGATIVE CONTROL: a whole-span copy WOULD have carried the array stain" );

    memset( &r, 0, sizeof( r ) );
    n = fx_root_fixed_load( &back, 1, file, need, plan, PlanCapacity, NULL, &r );
    fixed_check( n == 1, "C slack: the record reads" );
    fixed_check( back.label_length == 2 && back.label[0] == 'h' && back.label[1] == 'i' && back.label[2] == 0,
                 "C slack: the used length reads, and the buffer terminates at it" );
    fixed_check( back.marks_count == 1 && back.marks[0] == 7, "C slack: the live count reads" );
    fixed_check( back.marks[1] == 0 && back.marks[2] == 0 && back.marks[3] == 0,
                 "C slack: an unused slot lands as the wire's zero" );
    fixed_check( r.clamped == 0 && !r.malformed && !r.refused, "C slack: a clean read moves no counter" );
}

int64_t fixed_fx1_bytes( void ) { return fx_root_fixed_measure( 1 ); }

int64_t fixed_fx1_write( uint8_t * buffer, int64_t capacity )
{
    FxRoot one;
    fill( &one );
    return fx_root_fixed_save( &one, 1, buffer, capacity );
}

/* CASE 1: THE SAME SCHEMA — the identity plan, a static constant this build
   laid down, and the loop that runs it is the loop every other case runs. */
void fixed_fx1_read_own( const uint8_t * data, int64_t bytes )
{
    FxRoot back;
    TableReport r;
    int64_t n;
    memset( &r, 0, sizeof( r ) );
    n = fx_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, NULL, &r );
    fixed_check( n == 1, "same schema: one record" );
    fixed_check( back.keep == 4242u && back.narrow == 40000u && back.renamed == 321 && back.gone == 654,
                 "same schema: the scalars" );
    fixed_check( back.nested.a == 111 && back.nested.b == 222, "same schema: the nesting" );
    fixed_check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
                 "same schema: a silent report" );
}

/* ONE FORM-HEADER PROBE: the destination is POISONED before the load and
   compared unchanged after it, so a refusal that wrote one byte is caught, and
   the report has to name the reason and claim nothing else. `want_refused` is
   1 for a named form-byte refusal and 0 for the residue, `malformed`. */
static void form_case( const char * name, const uint8_t * file, int64_t bytes,
                       int want_refused, int want_reason, int want_malformed )
{
    FxRoot before, after;
    TableReport r;
    char why[160];
    int64_t got;

    memset( &before, 0xAB, sizeof( before ) );
    memset( &after, 0xAB, sizeof( after ) );
    memset( &r, 0, sizeof( r ) );

    got = fx_root_fixed_load( &after, 1, file, bytes, g_plan, PlanCapacity, NULL, &r );

    snprintf( why, sizeof( why ), "C FORM HEADER: %s returns a refusal, never a record", name );
    fixed_check( got < 0, why );

    snprintf( why, sizeof( why ), "C FORM HEADER: %s names the reason and nothing else", name );
    fixed_check( r.refused == want_refused && r.malformed == want_malformed &&
                 ( want_refused ? r.reason == want_reason : 1 ), why );

    snprintf( why, sizeof( why ), "C FORM HEADER: %s claims no success", name );
    fixed_check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 &&
                 r.duplicate == 0 && r.layout_hash == 0, why );

    snprintf( why, sizeof( why ), "C FORM HEADER: %s writes no destination byte", name );
    fixed_check( memcmp( &before, &after, sizeof( before ) ) == 0, why );
}

/* THE FIVE FORM-HEADER PROBES (docs/FIXED-FORM-ALGORITHM.md §5.3 step 1 and 2,
   docs/SPEC-TABLES.md §3.4). THE FORM BYTE IS READ BEFORE THE FILE'S LENGTH:
   a committed form-1 file is TEN bytes and a form-2 batch THREE, and a reader
   that measured first would answer malformed for every real file of the two
   forms it is supposed to name. Cases 2 and 3 take exactly those lengths and
   never a full-length buffer. */
void fixed_fx1_form_headers( const uint8_t * data, int64_t bytes )
{
    static const uint8_t form1[10] = { 1, 0, 0, 0, 0, 0, 0, 0, 0, 0 };
    static const uint8_t form2[3] = { 2, 1, 0 };
    static const uint8_t none[1] = { 0 };
    static uint8_t form0[8192];
    static uint8_t reserved[8192];

    /* ZERO BYTES IS THE RESIDUE: malformed, with no byte to earn a name. */
    form_case( "zero bytes", none, 0, 0, SCHEMA_TABLE_NO_REFUSAL, 1 );

    /* THE TEN- AND THREE-BYTE FILES of the two forms this reader NAMES: the
       form byte earns its name before the twenty-byte minimum is measured. */
    form_case( "a ten-byte form-1 file", form1, 10, 1, SCHEMA_TABLE_PREVIOUS_FORM, 0 );
    form_case( "a three-byte form-2 batch", form2, 3, 1, SCHEMA_TABLE_MESSAGE_FORM_AS_FILE, 0 );

    /* FORM BYTE 0 IS NOT A FORM AND IS STILL NOT MALFORMED: the registry is
       ORDERED, so the otherwise valid fixture with its form byte zeroed is
       named by where its byte sits — newer_form. */
    memcpy( form0, data, (size_t) bytes );
    form0[0] = 0;
    form_case( "form byte 0 on a valid tail", form0, bytes, 1, SCHEMA_TABLE_NEWER_FORM, 0 );

    /* THE SEVEN RESERVED BYTES ARE REFUSED, NOT IGNORED (§3.4, §5.3 step 2):
       the valid form-3 fixture with reserved byte 3 set nonzero is malformed. */
    memcpy( reserved, data, (size_t) bytes );
    reserved[3] = 1;
    form_case( "reserved byte 3 nonzero", reserved, bytes, 0, SCHEMA_TABLE_NO_REFUSAL, 1 );
}

/* CASE 3: A NEWER WRITER — a field this reader cannot name, stepped over by
   the size its layout entry states; a whole nested TYPE it cannot name, stepped
   over by its layout size; a field the writer dropped, which keeps this
   reader's declared default; and a kind that MOVED, which is reported and
   never reinterpreted. */
void fixed_fx1_read_fx2( const uint8_t * data, int64_t bytes )
{
    FxRoot back;
    TableReport r;
    int64_t n;
    memset( &r, 0, sizeof( r ) );
    n = fx_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, NULL, &r );
    fixed_check( n == 1, "newer writer: one record" );
    fixed_check( back.keep == 5150u, "newer writer: an unmoved field lands past the unknowns" );
    fixed_check( back.renamed == 808, "newer writer: `was =` reads the other way too" );
    fixed_check( back.gone == 9, "newer writer: a field the writer dropped takes its declared default" );
    fixed_check( back.nested.a == 33 && back.nested.b == 44, "newer writer: the nesting lands past the unknown type" );
    fixed_check( r.unknown == 2, "newer writer: two names this reader does not have" );
    fixed_check( r.kind_mismatch == 1, "newer writer: uint32 into uint16 is a kind that moved, not a widening" );
    fixed_check( back.narrow == 3, "newer writer: a narrowing leaves the declared default" );
    fixed_check( !r.malformed && !r.refused, "newer writer: no damage and no refusal" );
}

/* THE BOUNDS THE READ LOOP DOES NOT HOLD (docs/SPEC-TABLES.md §3.4). A fixed
   record is a positional image and the one read loop moves bytes: it asks
   nothing about what they mean. A RANGED SCALAR's declared min and max are held
   by STRAIGHT-LINE CODE in the generated decode, after the copy, and never by
   plan entries — an entry per bounded field is a test on every read of every
   record, which is the cost the identity plan exists to avoid.

   THE POISON IS WRITTEN THROUGH THE WRITER and not poked into the bytes: the
   write side's own bounds are debug-only by rule and a range is not one of
   them, so a caller CAN put an out-of-range value on the wire and the reader is
   what has to answer for it. */
int64_t fixed_fx1_write_out_of_range( uint8_t * buffer, int64_t capacity )
{
    FxRoot v;
    fx_root_reset( &v );
    v.keep = 1u;
    v.renamed = 5000;  /* declared | min = 0, max = 1000 */
    v.gone = -7;       /* and the low end of the same declaration */
    v.nested.a = 111;
    v.nested.b = 222;
    return fx_root_fixed_save( &v, 1, buffer, capacity );
}

void fixed_fx1_bounds( const uint8_t * data, int64_t bytes )
{
    FxRoot back, loose;
    TableReport r;
    const uint8_t * body;

    memset( &r, 0, sizeof( r ) );
    fixed_check( fx_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, NULL, &r ) == 1,
                 "C bounds: the record reads" );
    fixed_check( back.renamed == 1000, "C RANGE: a value past max lands at max" );
    fixed_check( back.gone == 0, "C RANGE: a value under min lands at min" );
    fixed_check( r.clamped == 2, "C RANGE: two clamps, counted" );
    fixed_check( back.nested.a == 111 && back.nested.b == 222, "C RANGE: an in-range neighbour is untouched" );

    /* THE NEGATIVE CONTROL: the read loop ALONE, with no straight-line pass
       after it. The same record, the same plan, and the out-of-range value
       survives — which is what says the bound is held by the pass and not by
       something else that would have caught it anyway. */
    fx_root_reset( &loose );
    memset( &r, 0, sizeof( r ) );
    body = data + kTableFixedHeaderBytes + 4 + (int64_t) sizeof( fx_root_fixed_layout ) + 8;
    table_fixed_run( fx_root_fixed_plan, fx_root_fixed_plan_count, fx_root_fixed_plan_guarded,
                     body, (uint8_t *) &loose, &r );
    fixed_check( loose.renamed == 5000 && loose.gone == -7,
                 "C NEGATIVE CONTROL: the loop alone really does leave an out-of-range value standing" );
    fixed_check( r.clamped == 0, "C NEGATIVE CONTROL: and counts nothing" );

    /* THE SAME CONTROL ON A COMPILED PLAN. A plan compiled from this build's
       OWN layout is the compiled path with nothing else moving, and it behaves
       the same way in kind: the loop alone leaves the out-of-range value
       standing, because NO PLAN OP CLAMPS. The pass after the loop is the
       clamp, and it is the same pass for either plan. */
    {
        TableFixedLayoutView parsed;
        static TableFixedEntry compiled[1024];
        TableReport cr;
        FxRoot held;
        int why = SCHEMA_TABLE_LAYOUT_MALFORMED;
        int32_t guarded = 0;
        int32_t made;
        int32_t i, past_the_set = 0;

        fixed_check( table_fixed_parse_layout( fx_root_fixed_layout, (int64_t) sizeof( fx_root_fixed_layout ), &parsed, &why ) != 0,
                     "C bounds, compiled-own: this build's layout parses" );
        memset( &cr, 0, sizeof( cr ) );
        uint32_t fill_at = 0;
        int32_t fill_count = 0;
        made = table_fixed_compile( &parsed, fx_root_fixed_layout, (int32_t) sizeof( fx_root_fixed_layout ),
                                    fx_root_fixed_dst, fx_root_fixed_cover, fx_root_fixed_cover_count,
                                    compiled, 1024, &guarded, &fill_at, &fill_count, &cr );
        fixed_check( made > 0, "C bounds, compiled-own: the plan compiles" );
        for ( i = 0; i < made; ++i )
        {
            if ( compiled[i].op > (uint8_t) kTableFixedBool ) { past_the_set++; }
        }
        fixed_check( past_the_set == 0, "C bounds, compiled-own: the ops are the whole set and none of them clamps" );

        fx_root_reset( &held );
        memset( &r, 0, sizeof( r ) );
        table_fixed_run( compiled, made, guarded, body, (uint8_t *) &held, &r );
        fixed_check( held.renamed == 5000 && held.gone == -7,
                     "C COMPILED, NEGATIVE CONTROL: the loop alone leaves an out-of-range value standing here too" );
        fixed_check( r.clamped == 0, "C COMPILED, NEGATIVE CONTROL: and counts nothing" );

        schema_tblfx1_fx_root_fixed_clamp_( &held, &r );
        fixed_check( held.renamed == 1000 && held.gone == 0,
                     "C COMPILED: THE PASS IS THE CLAMP — the same pass, after the compiled plan" );
        fixed_check( r.clamped == 2, "C COMPILED: and it counts the same two the identity path counted" );
        fixed_check( held.nested.a == 111 && held.nested.b == 222, "C COMPILED: an in-range neighbour is untouched" );
    }
}

/* THE ONE CONTENT RULE THE WIRE HAS (docs/SPEC-TABLES.md §3, §4), the C twin of
   the reference's. A `string(N)`'s used bytes are well-formed UTF-8 with no
   zero among them — the same rule SPEC.md §4.7 puts on the packet wire, where
   the whole read refuses. HERE THE RECORD IS POSITIONAL, so the damage is one
   field's: the field reads its DECLARED DEFAULT, one `malformed` counts, and
   the rest of the record stands.

   The poison is written THROUGH THE WRITER, because it can be: the write side's
   bounds are the used length and the live count, both debug-only, and neither
   is a content rule. */
void fixed_fx1_text_content( void )
{
    static const struct { const char * what; const char * bytes; int32_t length; } cases[] = {
        { "C TEXT: a lone 0xFF is no lead byte",           "\xFF",     1 },
        { "C TEXT: a truncated two-byte sequence",         "\xC3",     1 },
        { "C TEXT: a bare continuation byte",              "\x80",     1 },
        { "C TEXT: an overlong encoding of NUL",           "\xC0\x80", 2 },
        { "C TEXT: an INTERIOR NULL among the used bytes", "a\0b",     3 }
    };
    static uint8_t file[8192];
    size_t k;
    int64_t n;

    for ( k = 0; k < sizeof( cases ) / sizeof( cases[0] ); k++ )
    {
        FxRoot v, back;
        TableReport r;

        fx_root_reset( &v );
        v.keep = 1234u;
        v.nested.a = 7;
        memcpy( v.label, cases[k].bytes, (size_t) cases[k].length );
        v.label_length = cases[k].length;

        n = fx_root_fixed_save( &v, 1, file, (int64_t) sizeof( file ) );
        fixed_check( n == fx_root_fixed_measure( 1 ), "C text content: the record saves" );

        memset( &r, 0, sizeof( r ) );
        fixed_check( fx_root_fixed_load( &back, 1, file, n, g_plan, PlanCapacity, NULL, &r ) == 1,
                     "C text content: the record reads" );
        fixed_check( r.malformed, cases[k].what );
        fixed_check( back.label_length == 2 && strcmp( back.label, "fx" ) == 0,
                     "C TEXT CONTENT: the damaged field reads its DECLARED DEFAULT" );
        fixed_check( back.keep == 1234u && back.nested.a == 7,
                     "C TEXT CONTENT: and the rest of the record stands" );
        fixed_check( !r.refused, "C TEXT CONTENT: damage is not a refusal" );

        if ( k == 0 )
        {
            /* THE NEGATIVE CONTROL: the read loop alone, with no content pass
               after it — the bytes that are not text stand with nothing said. */
            FxRoot loose;
            TableReport r2;
            const uint8_t * body;
            fx_root_reset( &loose );
            memset( &r2, 0, sizeof( r2 ) );
            body = file + kTableFixedHeaderBytes + 4 + (int64_t) sizeof( fx_root_fixed_layout ) + 8;
            table_fixed_run( fx_root_fixed_plan, fx_root_fixed_plan_count, fx_root_fixed_plan_guarded,
                             body, (uint8_t *) &loose, &r2 );
            fixed_check( loose.label_length == 1 && (uint8_t) loose.label[0] == 0xFFu,
                         "C NEGATIVE CONTROL: the loop alone really does leave a byte that is not text standing" );
            fixed_check( !r2.malformed, "C NEGATIVE CONTROL: and says nothing about it" );
        }
    }

    /* AND WELL-FORMED TEXT IS UNTOUCHED, which is what makes the cases above
       discriminating. */
    {
        FxRoot v, back;
        TableReport r;
        fx_root_reset( &v );
        memcpy( v.label, "\xC3\xA9t\xC3\xA9", 5 ); /* "ete" with acutes */
        v.label_length = 5;
        n = fx_root_fixed_save( &v, 1, file, (int64_t) sizeof( file ) );
        memset( &r, 0, sizeof( r ) );
        fixed_check( fx_root_fixed_load( &back, 1, file, n, g_plan, PlanCapacity, NULL, &r ) == 1,
                     "C text content: the well-formed record reads" );
        fixed_check( back.label_length == 5 && memcmp( back.label, "\xC3\xA9t\xC3\xA9", 5 ) == 0,
                     "C TEXT CONTENT: well-formed multi-byte UTF-8 rides whole" );
        fixed_check( !r.malformed && r.clamped == 0, "C TEXT CONTENT: and moves no counter" );
    }
}

/* THE GUARD IS COMPARED AT THE TAG'S WIDTH (docs/SPEC-TABLES.md §3.4). A
   union tag can be two bytes, and comparing only the first of them fires
   arm 1 on a foreign tag of 0x0101. Hand-built so the one-byte compare and
   the whole-width compare meet the same record. */
void fixed_guard_width( void )
{
    uint8_t src[4];
    uint8_t dst[4];
    TableFixedEntry plan[2];
    TableReport r;

    src[0] = 0x01; src[1] = 0x01; src[2] = 0xAA; src[3] = 0x00;

    plan[0] = table_fixed_entry_zero();
    plan[0].src = 0; plan[0].dst = 0; plan[0].size = 2;
    plan[0].op = kTableFixedCopy;

    plan[1] = table_fixed_entry_zero();
    plan[1].src = 2; plan[1].dst = 2; plan[1].size = 1;
    plan[1].guard = 0;
    plan[1].op = kTableFixedCopy;
    plan[1].arg = 1;
    plan[1].argw = 2;

    memset( dst, 0, sizeof( dst ) );
    memset( &r, 0, sizeof( r ) );
    table_fixed_run( plan, 2, 1, src, dst, &r );
    fixed_check( dst[2] == 0, "C GUARD WIDTH: tag 0x0101 at width 2 does not take arm 1" );

    src[1] = 0x00;
    memset( dst, 0, sizeof( dst ) );
    memset( &r, 0, sizeof( r ) );
    table_fixed_run( plan, 2, 1, src, dst, &r );
    fixed_check( dst[2] == 0xAA, "C GUARD WIDTH: tag 0x0001 at width 2 takes arm 1" );

    src[1] = 0x01;
    plan[1].argw = 1;
    memset( dst, 0, sizeof( dst ) );
    memset( &r, 0, sizeof( r ) );
    table_fixed_run( plan, 2, 1, src, dst, &r );
    fixed_check( dst[2] == 0xAA, "C NEGATIVE CONTROL: a one-byte compare really does fire arm 1 on 0x0101" );
}

/* W6: FX1's identity plan has no union, so it has no guarded half at all —
   and `split` being a count of UNGUARDED entries rather than of guarded ones
   is what makes that come out right: the split is the END of the plan, not
   zero (docs/FIXED-FORM-ALGORITHM.md §4.1, §4.4, fix 6). */
void fixed_fx1_partition( void )
{
    fixed_partition_is_held( fx_root_fixed_plan, fx_root_fixed_plan_count, fx_root_fixed_plan_guarded, "FX1 identity plan" );
    fixed_check( fx_root_fixed_plan_guarded == fx_root_fixed_plan_count,
                 "W6: a plan with no guarded entry has its split at the END, not at zero" );
}

/* C1/C2: THE COUNT CLAMP (docs/FIXED-FORM-ALGORITHM.md §4.5). A counted
   array's count is four bytes a STRANGER wrote: `v := SLE(4, record+src)`;
   below zero it is zero and past the READER's own bound, IN ELEMENTS, it is
   the bound, and either way `COUNT clamped` once for the field.

   THE FORGERY IS IN THE BYTES and not through the writer: §3.1's write-side
   bound checks are DEBUG ONLY by rule and clamp nothing. The identity plan is
   what carries `count` (§4.1), so this reads its own file. */
void fixed_fx1_count_clamp( void )
{
    FxRoot one;
    static uint8_t file[8192];
    int64_t need, body_off;
    uint32_t count_at = 0xFFFFFFFFu;
    int32_t bound = -1;
    int32_t count_rows = 0;
    int32_t i;

    fx_root_reset( &one );
    one.marks[0] = 7; one.marks[1] = 8;
    one.marks_count = 2;
    need = fx_root_fixed_measure( 1 );
    fixed_check( need > 0 && need <= (int64_t) sizeof( file ), "count clamp: the record fits the buffer" );
    fixed_check( fx_root_fixed_save( &one, 1, file, need ) == need, "count clamp: the record saves" );
    body_off = kTableFixedHeaderBytes + 4 + (int64_t) sizeof( fx_root_fixed_layout ) + 8;

    /* THE COUNT'S OFFSET IS FOUND BY THE PLAN'S OWN ROW, by shape and not by a
       number in this file: the identity plan carries exactly ONE `count` op and
       it is `marks`', because `blob`'s count rides in its own `text` row's `aux`
       and never as a `count` entry (§4.1, fix 10). A `count` op's `size` IS the
       reader's own bound IN ELEMENTS, which for `marks [..4]int32` is four. */
    for ( i = 0; i < fx_root_fixed_plan_count; ++i )
    {
        if ( fx_root_fixed_plan[i].op != kTableFixedCount ) { continue; }
        count_rows++;
        count_at = fx_root_fixed_plan[i].src;
        bound = (int32_t) fx_root_fixed_plan[i].size;
    }
    fixed_check( count_rows == 1, "count clamp: the identity plan carries exactly ONE count op, so the row below is `marks`'" );
    fixed_check( count_at != 0xFFFFFFFFu && bound == 4, "count clamp: and its bound is FOUR ELEMENTS — `marks [..4]int32`'s own Max" );
    fixed_check( table_fixed_get32( file + body_off + count_at ) == 2,
                 "count clamp: and the offset it names is where the live count really is" );

    /* C1: A COUNT BELOW ZERO. Not a large number — zero, and one clamp. */
    {
        FxRoot back;
        TableReport r;
        table_fixed_put32( file + body_off + count_at, 0xFFFFFFFFu ); /* -1, as the wire spells it */
        memset( &r, 0, sizeof( r ) );
        fixed_check( fx_root_fixed_load( &back, 1, file, need, g_plan, PlanCapacity, NULL, &r ) == 1,
                     "C1: a forged count of -1 still READS — a clamp is not a refusal" );
        fixed_check( back.marks_count == 0, "C1: a count below zero clamps to ZERO" );
        fixed_check( r.clamped == 1, "C1: and counts exactly one clamp" );
        fixed_check( !r.malformed && !r.refused, "C1: a clamp is neither malformed nor a refusal" );
    }

    /* C2: A COUNT PAST THE READER'S OWN BOUND, IN ELEMENTS. */
    {
        FxRoot back;
        TableReport r;
        table_fixed_put32( file + body_off + count_at, (uint32_t) ( bound + 1 ) );
        memset( &r, 0, sizeof( r ) );
        fixed_check( fx_root_fixed_load( &back, 1, file, need, g_plan, PlanCapacity, NULL, &r ) == 1,
                     "C2: a forged count of Max+1 still READS" );
        fixed_check( back.marks_count == bound, "C2: a count past Max clamps to MAX, in elements" );
        fixed_check( r.clamped == 1, "C2: and counts exactly one clamp" );
        fixed_check( back.marks[0] == 7 && back.marks[1] == 8, "C2: the live elements are still the record's" );
    }

    /* AND THE CONTROL: the bound itself is not a clamp. */
    {
        FxRoot back;
        TableReport r;
        table_fixed_put32( file + body_off + count_at, (uint32_t) bound );
        memset( &r, 0, sizeof( r ) );
        fixed_check( fx_root_fixed_load( &back, 1, file, need, g_plan, PlanCapacity, NULL, &r ) == 1,
                     "count clamp: a count of exactly Max reads" );
        fixed_check( back.marks_count == bound && r.clamped == 0,
                     "CONTROL: a count of exactly Max is in range and counts NOTHING" );
    }
}
