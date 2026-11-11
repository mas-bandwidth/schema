#include "W1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblw1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLW1
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Fleet,fleet)
const ConformanceCodec * conformance_codecs_tblw1(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(tblw1,Fleet)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}

/* Sets the one field a write-contract row names (driver.h): the variable root
   the `write-refusal` surface saves through the variable-form Save. The loaded
   root lives in the holder's own region, so the setter writes it in place. */
int conformance_write_refusal_set_tblw1( const char * root, void * value, const char * path,
                                         const char * kind, const char * text )
{
    if ( strcmp( root, "Fleet" ) == 0 && strcmp( path, "home.hull" ) == 0 && strcmp( kind, "int" ) == 0 )
    {
        tblw1_fleet_t * v = (tblw1_fleet_t *) (uintptr_t) ( (schema_conformance_holder_Fleet *) value )->root;
        v->home.hull = (int32_t) strtoll( text, NULL, 10 );
        return 1;
    }
    return 0;
}

/* tblw1 rides only the variable surface, which saves through the codec. */
int conformance_write_refusal_write_tblw1( int surface, const char * root, const void * value, const uint8_t * announcement,
                                           int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    (void) surface; (void) root; (void) value; (void) announcement; (void) announcement_bytes; (void) buffer; (void) capacity; (void) wrote;
    return 0;
}
