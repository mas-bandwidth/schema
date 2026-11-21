#include "VarEvo3Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblvarevo3
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLVAREVO3
#include "unit.h"

SCHEMA_CONFORMANCE_GRAPH_CODEC( EvoDoc, evo_doc )
SCHEMA_CONFORMANCE_CODEC( EvoMsg, evo_msg )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( tblvarevo3, EvoDoc ),
    SCHEMA_CONFORMANCE_ROW( tblvarevo3, EvoMsg )
};
int conformance_write_refusal_set_tblvarevo3( const char * root, void * value, const char * path,
                                              const char * kind, const char * text )
{
    if ( strcmp( root, "EvoMsg" ) == 0 )
    {
        tblvarevo3_evo_msg_t * v = (tblvarevo3_evo_msg_t *) value;
        if ( strcmp( path, "payload.notice" ) == 0 )
        {
            return conformance_write_refusal_text( v->payload.as.notice.value, 24, &v->payload.as.notice.value_length, kind, text );
        }
        if ( strcmp( path, "payload" ) == 0 && strcmp( kind, "tag" ) == 0 )
        {
            return CONFORMANCE_WREF_UNSIGNED( v->payload.type, text );
        }
    }
    return 0;
}

int conformance_write_refusal_write_tblvarevo3( int surface, const char * root, const void * value, const uint8_t * announcement,
                                                int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    if ( strcmp( root, "EvoMsg" ) != 0 ) { return 0; }
    if ( surface == CONFORMANCE_WRITE_FIXED )
    {
        *wrote = tblvarevo3_evo_msg_fixed_save( (const tblvarevo3_evo_msg_t *) value, 1, buffer, capacity );
        return 1;
    }
    if ( surface == CONFORMANCE_WRITE_MESSAGE )
    {
        static tblvarevo3_table_message_entry_t entries[TBLVAREVO3_K_TABLE_MESSAGE_ENTRIES_HERE];
        tblvarevo3_table_vocabulary_t vocabulary = tblvarevo3_table_vocabulary( entries, TBLVAREVO3_K_TABLE_MESSAGE_ENTRIES_HERE );
        tblvarevo3_table_report_t report;
        memset( &report, 0, sizeof( report ) );
        if ( !tblvarevo3_announce_read( &vocabulary, announcement, announcement_bytes, &report ) ) { return 0; }
        *wrote = tblvarevo3_evo_msg_save_messages( (const tblvarevo3_evo_msg_t *) value, 1, buffer, capacity, &report );
        return 1;
    }
    return 0;
}

const ConformanceCodec * conformance_codecs_tblvarevo3( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
