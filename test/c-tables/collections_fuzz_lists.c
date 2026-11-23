#include "lists/HoldersTable.h"
#include "lists/MigrateTable.h"
#include "lists/ReportTable.h"
#include "lists/SaveTable.h"
#include "lists/SharedTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE listdemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER LISTDEMO
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Save,save)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Mixed,mixed)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Album,album)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Sheet,sheet)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Army,army)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Unbounded,unbounded)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Ints,ints)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Squad,squad)
SCHEMA_CONFORMANCE_CODEC(Bounded,bounded)
const ConformanceCodec * conformance_codecs_listdemo(int * count) {
 static const ConformanceCodec codecs[]={
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Save),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Mixed),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Album),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Sheet),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Army),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Unbounded),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Ints),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Squad),
 SCHEMA_CONFORMANCE_ROW(listdemo,Bounded),
 }; *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}

int conformance_write_refusal_set_listdemo( const char * root, void * value, const char * path,
                                            const char * kind, const char * text )
{
    if ( strcmp( root, "Mixed" ) == 0 )
    {
        schema_conformance_holder_Mixed * holder = (schema_conformance_holder_Mixed *) value;
        listdemo_mixed_t * v = (listdemo_mixed_t *) (uintptr_t) holder->root;
        if ( v == NULL ) { return 0; }
        if ( strcmp( path, "bounds[1]" ) == 0 && strcmp( kind, "int" ) == 0 )
        {
            int32_t * elem = (int32_t *) (uintptr_t) listdemo_mixed_bounds_at( &v->bounds, 1 );
            if ( elem == NULL ) { return 0; }
            CONFORMANCE_WREF_SIGNED( *elem, text );
            return 1;
        }
        if ( strcmp( path, "grades[0]" ) == 0 && strcmp( kind, "ordinal" ) == 0 )
        {
            listdemo_grade_t * elem = (listdemo_grade_t *) (uintptr_t) listdemo_mixed_grades_at( &v->grades, 0 );
            if ( elem == NULL ) { return 0; }
            CONFORMANCE_WREF_UNSIGNED( *elem, text );
            return 1;
        }
        if ( strcmp( path, "hits[0]" ) == 0 && strcmp( kind, "tag" ) == 0 )
        {
            listdemo_hit_t * elem = (listdemo_hit_t *) (uintptr_t) listdemo_mixed_hits_at( &v->hits, 0 );
            if ( elem == NULL ) { return 0; }
            memset( elem, 0, sizeof( *elem ) );
            CONFORMANCE_WREF_UNSIGNED( elem->type, text );
            return 1;
        }
        return 0;
    }
    return 0;
}

int conformance_write_refusal_write_listdemo( int surface, const char * root, const void * value, const uint8_t * announcement,
                                              int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    (void) surface; (void) root; (void) value; (void) announcement; (void) announcement_bytes;
    (void) buffer; (void) capacity; (void) wrote;
    return 0;
}
