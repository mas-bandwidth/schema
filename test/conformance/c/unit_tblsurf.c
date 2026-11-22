/* the tblsurf unit's codecs (test/tables/surfaces): the 2026-11-22 surfaces'
   own unit. Narrow, Wide and Arrays are fixed roots with a file form; Big is a
   fixed root past the 65536-byte ceiling with none (its rows start from the
   fresh value); Chain, Holder and Counted are variable roots. */
#include <errno.h>
#include "SurfacesTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE surfaces
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER SURFACES
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( Narrow, narrow )
SCHEMA_CONFORMANCE_CODEC( Wide, wide )
SCHEMA_CONFORMANCE_CODEC( Arrays, arrays )
SCHEMA_CONFORMANCE_CODEC_NOFORM( Big, big )
SCHEMA_CONFORMANCE_GRAPH_CODEC( Chain, chain )
SCHEMA_CONFORMANCE_GRAPH_CODEC( Holder, holder )
SCHEMA_CONFORMANCE_GRAPH_CODEC( Counted, counted )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblsurf, Narrow ),
    SCHEMA_CONFORMANCE_ROW( tblsurf, Wide ),
    SCHEMA_CONFORMANCE_ROW( tblsurf, Arrays ),
    SCHEMA_CONFORMANCE_ROW( tblsurf, Big ),
    SCHEMA_CONFORMANCE_GRAPH_ROW( tblsurf, Chain ),
    SCHEMA_CONFORMANCE_GRAPH_ROW( tblsurf, Holder ),
    SCHEMA_CONFORMANCE_GRAPH_ROW( tblsurf, Counted )
};
const ConformanceCodec * conformance_codecs_tblsurf( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}

/* an int32_t field from an `int` row */
static int surf_set_int32( int32_t * field, const char * text )
{
    long long n = 0;
    const int set = conformance_write_refusal_int( text, INT32_MIN, INT32_MAX, &n );
    if ( set == 1 ) { *field = (int32_t) n; }
    return set;
}

/* Sets the one field a write-contract row names (driver.h). */
int conformance_write_refusal_set_tblsurf( const char * root, void * value, const char * path,
                                           const char * kind, const char * text )
{
    if ( strcmp( root, "Arrays" ) == 0 )
    {
        surfaces_arrays_t * v = (surfaces_arrays_t *) value;
        if ( strcmp( path, "trio" ) == 0 && strcmp( kind, "elems" ) == 0 )
        {
            return conformance_write_refusal_elems( text, (long long) ( sizeof( v->trio ) / sizeof( v->trio[0] ) ) );
        }
        if ( strcmp( path, "per" ) == 0 && strcmp( kind, "elems" ) == 0 )
        {
            return conformance_write_refusal_elems( text, (long long) ( sizeof( v->per ) / sizeof( v->per[0] ) ) );
        }
        if ( strcmp( path, "marks" ) == 0 && strcmp( kind, "mask" ) == 0 )
        {
            /* a flags field's raw bits, in decimal, into the unit's mask storage */
            char * end = NULL;
            unsigned long long n;
            errno = 0;
            n = strtoull( text, &end, 10 );
            if ( end == NULL || end == text || *end != 0 || errno != 0 ) { return 0; }
            if ( sizeof( v->marks ) < sizeof( n ) && ( n >> ( 8 * sizeof( v->marks ) ) ) != 0 ) { return CONFORMANCE_SET_UNREPRESENTABLE; }
            v->marks = (surfaces_marks_t) n;
            return 1;
        }
    }
    if ( strcmp( root, "Counted" ) == 0 && strcmp( path, "few" ) == 0 && strcmp( kind, "count" ) == 0 )
    {
        /* a counted array's count, element 0 copied into new elements; past
           storage only the count moves, and -1 is a negative count, which this
           leg's int32_t count holds */
        schema_conformance_holder_Counted * holder = (schema_conformance_holder_Counted *) value;
        surfaces_counted_t * v = (surfaces_counted_t *) (void *) holder->root;
        const int32_t storage = (int32_t) ( sizeof( v->few ) / sizeof( v->few[0] ) );
        long long n = 0;
        int32_t i;
        if ( conformance_write_refusal_int( text, INT32_MIN, INT32_MAX, &n ) != 1 ) { return 0; }
        for ( i = v->few_count; i < (int32_t) n && i < storage; i++ ) { v->few[i] = v->few[0]; }
        v->few_count = (int32_t) n;
        return 1;
    }
    if ( strcmp( root, "Narrow" ) == 0 && strcmp( path, "hp" ) == 0 && strcmp( kind, "int" ) == 0 ) { return surf_set_int32( &( (surfaces_narrow_t *) value )->hp, text ); }
    if ( strcmp( root, "Wide" ) == 0 && strcmp( path, "hp" ) == 0 && strcmp( kind, "int" ) == 0 ) { return surf_set_int32( &( (surfaces_wide_t *) value )->hp, text ); }
    if ( strcmp( root, "Big" ) == 0 )
    {
        surfaces_big_t * v = (surfaces_big_t *) value;
        if ( strcmp( path, "hp" ) == 0 && strcmp( kind, "int" ) == 0 ) { return surf_set_int32( &v->hp, text ); }
        if ( strcmp( path, "mask" ) == 0 && strcmp( kind, "int" ) == 0 )
        {
            long long n = 0;
            const int set = conformance_write_refusal_int( text, 0, (long long) UINT32_MAX, &n );
            if ( set == 1 ) { v->mask = (uint32_t) n; }
            return set;
        }
        if ( strcmp( path, "ratio" ) == 0 && strcmp( kind, "float" ) == 0 )
        {
            char * end = NULL;
            v->ratio = strtof( text, &end );
            return end != NULL && end != text && *end == 0;
        }
    }
    return 0;
}

/* The fixed form's FixedSave and the message form's SaveMessages, a batch of
   one, for the roots the rows name. The message rows read the connection's
   announcement first, as backenddemo's do. */
int conformance_write_refusal_write_tblsurf( int surface, const char * root, const void * value, const uint8_t * announcement,
                                             int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    surfaces_table_report_t report;
    memset( &report, 0, sizeof( report ) );
    if ( surface == CONFORMANCE_WRITE_FIXED )
    {
        if ( strcmp( root, "Arrays" ) == 0 ) { *wrote = surfaces_arrays_fixed_save( (const surfaces_arrays_t *) value, 1, buffer, capacity ); return 1; }
        if ( strcmp( root, "Narrow" ) == 0 ) { *wrote = surfaces_narrow_fixed_save( (const surfaces_narrow_t *) value, 1, buffer, capacity ); return 1; }
        if ( strcmp( root, "Wide" ) == 0 ) { *wrote = surfaces_wide_fixed_save( (const surfaces_wide_t *) value, 1, buffer, capacity ); return 1; }
        return 0;
    }
    if ( surface == CONFORMANCE_WRITE_MESSAGE )
    {
        static surfaces_table_message_entry_t entries[SURFACES_K_TABLE_MESSAGE_ENTRIES_HERE];
        surfaces_table_vocabulary_t vocabulary = surfaces_table_vocabulary( entries, SURFACES_K_TABLE_MESSAGE_ENTRIES_HERE );
        if ( !surfaces_announce_read( &vocabulary, announcement, announcement_bytes, &report ) ) { return 0; }
        if ( strcmp( root, "Arrays" ) == 0 ) { *wrote = surfaces_arrays_save_messages( (const surfaces_arrays_t *) value, 1, buffer, capacity, &report ); return 1; }
        if ( strcmp( root, "Narrow" ) == 0 ) { *wrote = surfaces_narrow_save_messages( (const surfaces_narrow_t *) value, 1, buffer, capacity, &report ); return 1; }
        if ( strcmp( root, "Wide" ) == 0 ) { *wrote = surfaces_wide_save_messages( (const surfaces_wide_t *) value, 1, buffer, capacity, &report ); return 1; }
        if ( strcmp( root, "Big" ) == 0 ) { *wrote = surfaces_big_save_messages( (const surfaces_big_t *) value, 1, buffer, capacity, &report ); return 1; }
        return 0;
    }
    return 0;
}

SCHEMA_CONFORMANCE_ANNOUNCE_1122

/* SaveMessages of one with NO announcement read or written on this
   connection: the writer's own vocabulary (README, `announce`, send) */
#define SURF_SEND( NAME, FN ) \
    if ( strcmp( root, NAME ) == 0 ) \
    { \
        const int64_t need = surfaces_##FN##_measure_messages( (const surfaces_##FN##_t *) value, 1, &report ); \
        if ( need < 0 ) { return 0; } \
        *out = (uint8_t *) malloc( (size_t) ( need > 0 ? need : 1 ) ); \
        if ( *out == NULL ) { fprintf( stderr, "driver: send: out of memory\n" ); exit( 1 ); } \
        *size = surfaces_##FN##_save_messages( (const surfaces_##FN##_t *) value, 1, *out, need, &report ); \
        return *size >= 0; \
    }
static int surf_send( const char * root, const void * value, uint8_t ** out, int64_t * size )
{
    surfaces_table_report_t report;
    memset( &report, 0, sizeof( report ) );
    SURF_SEND( "Narrow", narrow )
    SURF_SEND( "Wide", wide )
    SURF_SEND( "Arrays", arrays )
    SURF_SEND( "Big", big )
    return -1;
}

/* a read's line: `read` when it returned the value and the ONE report it was
   handed says nothing was refused */
static const char * surf_read_line( int ok, const surfaces_table_report_t * report )
{
    return ok && !report->refused && report->refuse_reason == SCHEMA_SURFACES_TABLE_REFUSE_OK && report->damage_reason == 0 ? "read\n" : "refused\n";
}

static int surf_text( ConformanceText * answer, int64_t size, int64_t wrote, char * text )
{
    if ( size < 0 || wrote != size ) { free( text ); return 0; }
    conformance_text_raw( answer, text, (size_t) size );
    free( text );
    return 1;
}

/* A VARIABLE root's caller value is its BUILDER: load_builder reads the
   variable form into it and from_json the text, each with the one report. */
static int surf_read_into_counted( const char * form, const uint8_t * lawful, int64_t lawful_bytes, const uint8_t * damaged,
                                   int64_t damaged_bytes, int64_t count, ConformanceText * answer )
{
    surfaces_counted_builder_t builder;
    surfaces_table_report_t report;
    const surfaces_counted_t * root;
    const int variable = strcmp( form, "variable" ) == 0;
    int64_t size;
    char * text;
    int ok, pass;
    if ( ( !variable && strcmp( form, "json" ) != 0 ) || count != 1 ) { return 1; }
    if ( !surfaces_counted_builder_init( &builder ) ) { fprintf( stderr, "driver: read-into: builder_init refused\n" ); return 2; }
    memset( &report, 0, sizeof( report ) );
    for ( pass = 0; pass < 3; pass++ )
    {
        const uint8_t * in = pass == 1 ? damaged : lawful;
        const int64_t bytes = pass == 1 ? damaged_bytes : lawful_bytes;
        if ( pass == 2 )
        {
            root = surfaces_counted_builder_root( &builder );
            if ( root == NULL ) { fprintf( stderr, "driver: read-into: the builder holds no root after the refused read\n" ); surfaces_counted_builder_shutdown( &builder ); return 2; }
            size = surfaces_counted_to_json_measure( root );
            text = (char *) malloc( (size_t) ( size > 0 ? size : 0 ) + 1 );
            if ( text == NULL || !surf_text( answer, size, size < 0 ? -1 : surfaces_counted_to_json( root, text, size + 1 ), text ) )
            {
                fprintf( stderr, "driver: read-into: to_json refused the value the read left\n" );
                surfaces_counted_builder_shutdown( &builder );
                return 2;
            }
        }
        ok = variable ? surfaces_counted_load_builder( &builder, in, bytes, &report )
                      : surfaces_counted_from_json( &builder, (const char *) in, bytes, &report );
        conformance_text_add( answer, surf_read_line( ok, &report ) );
    }
    surfaces_counted_builder_shutdown( &builder );
    return 0;
}

/* A FIXED root's caller values are an array of `count`: fixed_load with that
   capacity. */
static int surf_read_into_narrow( const char * form, const uint8_t * lawful, int64_t lawful_bytes, const uint8_t * damaged,
                                  int64_t damaged_bytes, int64_t count, ConformanceText * answer )
{
    static surfaces_table_fixed_entry_t plan[4096];
    surfaces_table_report_t report;
    surfaces_narrow_t * values;
    int64_t i;
    int pass;
    if ( strcmp( form, "fixed" ) != 0 ) { return 1; }
    values = (surfaces_narrow_t *) calloc( (size_t) count, sizeof( surfaces_narrow_t ) );
    if ( values == NULL ) { fprintf( stderr, "driver: read-into: out of memory\n" ); return 2; }
    for ( i = 0; i < count; i++ ) { surfaces_narrow_reset( values + i ); }
    memset( &report, 0, sizeof( report ) );
    for ( pass = 0; pass < 3; pass++ )
    {
        const uint8_t * in = pass == 1 ? damaged : lawful;
        const int64_t bytes = pass == 1 ? damaged_bytes : lawful_bytes;
        int64_t n;
        if ( pass == 2 )
        {
            for ( i = 0; i < count; i++ )
            {
                const int64_t size = surfaces_narrow_to_json_measure( values + i );
                char * text = (char *) malloc( (size_t) ( size > 0 ? size : 0 ) + 1 );
                if ( text == NULL || !surf_text( answer, size, size < 0 ? -1 : surfaces_narrow_to_json( values + i, text, size + 1 ), text ) )
                {
                    fprintf( stderr, "driver: read-into: to_json refused value %lld\n", (long long) i );
                    free( values );
                    return 2;
                }
            }
        }
        n = surfaces_narrow_fixed_load( values, count, in, bytes, plan, 4096, NULL, &report );
        conformance_text_add( answer, surf_read_line( n >= 0, &report ) );
    }
    free( values );
    return 0;
}

static int surf_read_into( const char * root, const char * form, const uint8_t * lawful, int64_t lawful_bytes,
                           const uint8_t * damaged, int64_t damaged_bytes, int64_t count,
                           const uint8_t * announcement, int64_t announcement_bytes, ConformanceText * answer )
{
    (void) announcement; (void) announcement_bytes;
    if ( strcmp( root, "Counted" ) == 0 ) { return surf_read_into_counted( form, lawful, lawful_bytes, damaged, damaged_bytes, count, answer ); }
    if ( strcmp( root, "Narrow" ) == 0 ) { return surf_read_into_narrow( form, lawful, lawful_bytes, damaged, damaged_bytes, count, answer ); }
    return 1;
}

static const ConformanceUnit1122 unit1122 = { "tblsurf", schema_conformance_announce_measure_, schema_conformance_announce_write_,
                                              schema_conformance_announce_read_, surf_send, surf_read_into };
const ConformanceUnit1122 * conformance_unit1122_tblsurf( void ) { return &unit1122; }
