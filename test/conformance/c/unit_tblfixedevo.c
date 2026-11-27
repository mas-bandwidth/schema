/* the tblfixedevo unit's codecs (test/tables/fixedevo). */
#include "FixedEvoTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblfixedevo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLFIXEDEVO
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( EvoRoot, evo_root )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblfixedevo, EvoRoot )
};
const ConformanceCodec * conformance_codecs_tblfixedevo( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}

/* read-into ri_fixed_lineage (docs/SPEC-TABLES.md §3.4): the lawful v3 record,
   then a LAWFUL v1 record read through the lineage into the SAME values, whose
   text the row owes; a field v1 does not carry holds its declared default. */
static int evo_read_into( const char * root, const char * form, const uint8_t * lawful, int64_t lawful_bytes,
                          const uint8_t * second, int64_t second_bytes, int64_t count,
                          const uint8_t * announcement, int64_t announcement_bytes, ConformanceText * answer )
{
    static tblfixedevo_table_fixed_entry_t plan[4096];
    tblfixedevo_table_report_t report;
    tblfixedevo_evo_root_t * values;
    int64_t i;
    int pass;
    (void) announcement; (void) announcement_bytes;
    if ( strcmp( root, "EvoRoot" ) != 0 || strcmp( form, "fixed" ) != 0 ) { return 1; }
    values = (tblfixedevo_evo_root_t *) calloc( (size_t) count, sizeof( tblfixedevo_evo_root_t ) );
    if ( values == NULL ) { fprintf( stderr, "driver: read-into: out of memory\n" ); return 2; }
    for ( i = 0; i < count; i++ ) { tblfixedevo_evo_root_reset( values + i ); }
    memset( &report, 0, sizeof( report ) );
    for ( pass = 0; pass < 3; pass++ )
    {
        const uint8_t * in = pass == 1 ? second : lawful;
        const int64_t bytes = pass == 1 ? second_bytes : lawful_bytes;
        int64_t n;
        if ( pass == 2 )
        {
            for ( i = 0; i < count; i++ )
            {
                const int64_t size = tblfixedevo_evo_root_to_json_measure( values + i );
                char * text = (char *) malloc( (size_t) ( size > 0 ? size : 0 ) + 1 );
                const int64_t wrote = text == NULL || size < 0 ? -1 : tblfixedevo_evo_root_to_json( values + i, text, size + 1 );
                if ( text == NULL || size < 0 || wrote != size )
                {
                    free( text );
                    fprintf( stderr, "driver: read-into: to_json refused value %lld\n", (long long) i );
                    free( values );
                    return 2;
                }
                conformance_text_raw( answer, text, (size_t) size );
                free( text );
            }
        }
        n = tblfixedevo_evo_root_fixed_load( values, count, in, bytes, plan, 4096, NULL, &report );
        conformance_text_add( answer, n >= 0 && !report.refused && report.refuse_reason == SCHEMA_TBLFIXEDEVO_TABLE_REFUSE_OK &&
                                              report.damage_reason == 0 ? "read\n" : "refused\n" );
    }
    free( values );
    return 0;
}

static const ConformanceUnit1122 unit1122 = { "tblfixedevo", NULL, NULL, NULL, NULL, evo_read_into };
const ConformanceUnit1122 * conformance_unit1122_tblfixedevo( void ) { return &unit1122; }
