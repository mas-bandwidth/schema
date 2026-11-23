/* The wide scalar and fixed-point corpus, under the same erased C surface. */
#include "ScalarsTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE scalardemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER SCALARDEMO
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( SimState, sim_state )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( scalars, SimState )
};
int conformance_write_refusal_set_scalars( const char * root, void * value, const char * path,
                                           const char * kind, const char * text )
{
    if ( strcmp( root, "SimState" ) == 0 && strcmp( kind, "int" ) == 0 )
    {
        scalardemo_sim_state_t * v = (scalardemo_sim_state_t *) value;
        if ( strcmp( path, "ticks" ) == 0 ) { return CONFORMANCE_WREF_SIGNED( v->ticks, text ); }
        if ( strcmp( path, "angle" ) == 0 ) { return CONFORMANCE_WREF_SIGNED( v->angle, text ); }
        if ( strcmp( path, "speed" ) == 0 ) { return CONFORMANCE_WREF_UNSIGNED( v->speed, text ); }
    }
    return 0;
}

int conformance_write_refusal_write_scalars( int surface, const char * root, const void * value, const uint8_t * announcement,
                                             int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    (void) announcement; (void) announcement_bytes;
    if ( surface != CONFORMANCE_WRITE_FIXED ) { return 0; }
    if ( strcmp( root, "SimState" ) == 0 ) { *wrote = scalardemo_sim_state_fixed_save( (const scalardemo_sim_state_t *) value, 1, buffer, capacity ); return 1; }
    return 0;
}

const ConformanceCodec * conformance_codecs_scalars( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
