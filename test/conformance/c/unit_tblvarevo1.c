#include "VarEvo1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblvarevo1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLVAREVO1
#include "unit.h"

SCHEMA_CONFORMANCE_GRAPH_CODEC( EvoDoc, evo_doc )
SCHEMA_CONFORMANCE_CODEC( EvoMsg, evo_msg )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( tblvarevo1, EvoDoc ),
    SCHEMA_CONFORMANCE_ROW( tblvarevo1, EvoMsg )
};
const ConformanceCodec * conformance_codecs_tblvarevo1( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}

int conformance_retain_varevo1(const uint8_t * wire,int64_t bytes,int short_buffer,int64_t id_capacity,int * counters,uint8_t ** output,int64_t * output_bytes,char ** text,int64_t * text_bytes)
{
    enum { roomy = 1 << 20 };
    tblvarevo1_table_retain_t retain;
    const tblvarevo1_evo_doc_t * root;
    tblvarevo1_table_report_t report = { 0 }, save = { 0 };
    uint8_t * region = NULL;
    int64_t need, size;
    int ok = 0;
    memset( &retain, 0, sizeof( retain ) );
    *output = NULL;
    *output_bytes = 0;
    *text = NULL;
    *text_bytes = 0;
    if ( id_capacity < 0 ) id_capacity = roomy / 8;
    if ( id_capacity > INT32_MAX ) return 0;
    need = tblvarevo1_evo_doc_load_measure( wire, bytes );
    if ( need < 0 ) return 0;
    region = (uint8_t *) malloc( (size_t) ( need > 0 ? need : 1 ) );
    if ( !region ) return 0;
    retain.bytes = (uint8_t *) malloc( roomy );
    retain.capacity = roomy;
    retain.ids = (tblvarevo1_table_retain_id_t *) calloc( (size_t) ( id_capacity ? id_capacity : 1 ), sizeof( tblvarevo1_table_retain_id_t ) );
    retain.id_capacity = (int32_t) id_capacity;
    if ( !retain.bytes || !retain.ids ) goto done;
    if ( short_buffer )
    {
        if ( !tblvarevo1_evo_doc_load_retain( region, need, wire, bytes, &retain, &report ) ) goto done;
        retain.capacity = retain.used - 1;
        memset( &report, 0, sizeof( report ) );
    }
    root = tblvarevo1_evo_doc_load_retain( region, need, wire, bytes, &retain, &report );
    if ( !root ) goto done;
    counters[0] = report.retained;
    counters[1] = report.retain_lost;
    counters[2] = report.unknown;
    size = tblvarevo1_evo_doc_measure_retain( root, &retain );
    if ( size < 0 ) goto done;
    *output = (uint8_t *) malloc( (size_t) ( size ? size : 1 ) );
    if ( !*output ) goto done;
    if ( tblvarevo1_evo_doc_save_retain( root, &retain, *output, size, &save ) != size ) goto done;
    counters[3] = save.retain_lost;
    *output_bytes = size;
    *text_bytes = tblvarevo1_evo_doc_to_json_measure( root );
    if ( *text_bytes < 0 ) goto done;
    *text = (char *) malloc( (size_t) *text_bytes + 1 );
    if ( !*text ) goto done;
    if ( tblvarevo1_evo_doc_to_json( root, *text, *text_bytes ) != *text_bytes ) goto done;
    ok = 1;
done:
    free( retain.bytes );
    free( retain.ids );
    free( region );
    if ( !ok ) { free( *output ); *output = NULL; free( *text ); *text = NULL; }
    return ok;
}
