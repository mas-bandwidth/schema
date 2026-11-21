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

#ifndef CONFORMANCE_NO_WREF_CASE
/* THE WRITE CONTRACT on a PLAIN VARIABLE root (MANIFEST.txt, review-1121 R1):
   Mixed's lists, each an element the load left, set in place. A loaded list's
   elements sit contiguous at the sequence's self-relative offset. Every row on
   this unit is a variable Save or a ToJson, so the unit has no writer of its
   own: the case runner calls the codec's. */
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
            int32_t * elems;
            if ( v->bounds.count < 2 || v->bounds.items.value == 0 ) { return 0; }
            elems = (int32_t *) (void *) ( (uint8_t *) (void *) &v->bounds.items + v->bounds.items.value );
            return CONFORMANCE_WREF_SIGNED( elems[1], text );
        }
        if ( strcmp( path, "grades[0]" ) == 0 && strcmp( kind, "ordinal" ) == 0 )
        {
            listdemo_grade_t * elems;
            if ( v->grades.count < 1 || v->grades.items.value == 0 ) { return 0; }
            elems = (listdemo_grade_t *) (void *) ( (uint8_t *) (void *) &v->grades.items + v->grades.items.value );
            return CONFORMANCE_WREF_UNSIGNED( elems[0], text );
        }
        if ( strcmp( path, "hits[0]" ) == 0 && strcmp( kind, "tag" ) == 0 )
        {
            listdemo_hit_t * elems;
            if ( v->hits.count < 1 || v->hits.items.value == 0 ) { return 0; }
            elems = (listdemo_hit_t *) (void *) ( (uint8_t *) (void *) &v->hits.items + v->hits.items.value );
            CONFORMANCE_WREF_UNSIGNED( elems[0].type, text );
            /* a lawful tag takes its arm at the arm's declared defaults */
            memset( &elems[0].as, 0, sizeof( elems[0].as ) );
            return 1;
        }
    }
    return 0;
}

int conformance_write_refusal_write_listdemo( int surface, const char * root, const void * value, const uint8_t * announcement,
                                              int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    (void) surface; (void) root; (void) value; (void) announcement; (void) announcement_bytes; (void) buffer; (void) capacity; (void) wrote;
    return 0;
}
#endif
