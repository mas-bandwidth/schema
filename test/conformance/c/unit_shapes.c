#include "ShapesTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE shapes
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER SHAPES
#include "unit.h"
#include <math.h>

SCHEMA_CONFORMANCE_GRAPH_CODEC( Bounds, bounds )
SCHEMA_CONFORMANCE_CODEC( FixedBounds, fixed_bounds )
SCHEMA_CONFORMANCE_GRAPH_CODEC( VarMoods, var_moods ) /* variable only: no fixed form, so the deprecated-variant rows reach the variable save */
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( shapes, Bounds ),
    SCHEMA_CONFORMANCE_ROW( shapes, FixedBounds ),
    SCHEMA_CONFORMANCE_GRAPH_ROW( shapes, VarMoods )
};

static int parse_shapes_f32( const char * text, float * out )
{
    char * end = NULL;
    if ( strcmp( text, "nan" ) == 0 ) { *out = NAN; return 1; }
    if ( strcmp( text, "inf" ) == 0 ) { *out = INFINITY; return 1; }
    if ( strcmp( text, "-inf" ) == 0 ) { *out = -INFINITY; return 1; }
    *out = strtof( text, &end );
    return end != NULL && *end == 0;
}

int conformance_write_refusal_set_shapes( const char * root, void * value, const char * path,
                                          const char * kind, const char * text )
{
    const int is_real = strcmp( kind, "float" ) == 0 || strcmp( kind, "real" ) == 0;
    if ( strcmp( root, "FixedBounds" ) == 0 )
    {
        shapes_fixed_bounds_t * v = (shapes_fixed_bounds_t *) value;
        if ( strcmp( path, "tenth" ) == 0 && is_real ) { return parse_shapes_f32( text, &v->tenth ); }
        if ( strcmp( path, "seventh" ) == 0 && is_real ) { return parse_shapes_f32( text, &v->seventh ); }
        if ( strcmp( path, "ranged" ) == 0 && is_real ) { return parse_shapes_f32( text, &v->ranged ); }
        if ( strcmp( path, "zero" ) == 0 && is_real ) { return parse_shapes_f32( text, &v->zero ); }
        if ( strcmp( path, "plain" ) == 0 && is_real ) { return parse_shapes_f32( text, &v->plain ); }
        if ( strcmp( path, "octets" ) == 0 && strcmp( kind, "count" ) == 0 )
        {
            const int32_t cap = (int32_t) ( sizeof( v->octets ) / sizeof( v->octets[0] ) );
            const int32_t cnt = conformance_write_refusal_count( text );
            int32_t idx;
            for ( idx = v->octets_count; idx < cnt && idx < cap; idx++ ) { v->octets[idx] = v->octets[0]; }
            v->octets_count = cnt;
            return 1;
        }
        if ( strcmp( path, "recs" ) == 0 && strcmp( kind, "count" ) == 0 )
        {
            const int32_t cap = (int32_t) ( sizeof( v->recs ) / sizeof( v->recs[0] ) );
            const int32_t cnt = conformance_write_refusal_count( text );
            int32_t idx;
            for ( idx = v->recs_count; idx < cnt && idx < cap; idx++ ) { v->recs[idx] = v->recs[0]; }
            v->recs_count = cnt;
            return 1;
        }
        if ( strcmp( path, "tones[0]" ) == 0 && strcmp( kind, "ordinal" ) == 0 )
        {
            CONFORMANCE_WREF_UNSIGNED( v->tones[0], text );
            if ( v->tones_count < 1 ) { v->tones_count = 1; }
            return 1;
        }
        return 0;
    }
    if ( strcmp( root, "Bounds" ) == 0 )
    {
        schema_conformance_holder_Bounds * holder = (schema_conformance_holder_Bounds *) value;
        shapes_bounds_t * v = (shapes_bounds_t *) (uintptr_t) holder->root;
        if ( v == NULL ) { return 0; }
        if ( strcmp( path, "tenth" ) == 0 && is_real ) { return parse_shapes_f32( text, &v->tenth ); }
        if ( strcmp( path, "seventh" ) == 0 && is_real ) { return parse_shapes_f32( text, &v->seventh ); }
        if ( strcmp( path, "ranged" ) == 0 && is_real ) { return parse_shapes_f32( text, &v->ranged ); }
        if ( strcmp( path, "zero" ) == 0 && is_real ) { return parse_shapes_f32( text, &v->zero ); }
        if ( strcmp( path, "octets" ) == 0 && strcmp( kind, "count" ) == 0 )
        {
            const int32_t cap = (int32_t) ( sizeof( v->octets ) / sizeof( v->octets[0] ) );
            const int32_t cnt = conformance_write_refusal_count( text );
            int32_t idx;
            for ( idx = v->octets_count; idx < cnt && idx < cap; idx++ ) { v->octets[idx] = v->octets[0]; }
            v->octets_count = cnt;
            return 1;
        }
        if ( strcmp( path, "recs" ) == 0 && strcmp( kind, "count" ) == 0 )
        {
            const int32_t cap = (int32_t) ( sizeof( v->recs ) / sizeof( v->recs[0] ) );
            const int32_t cnt = conformance_write_refusal_count( text );
            int32_t idx;
            for ( idx = v->recs_count; idx < cnt && idx < cap; idx++ ) { v->recs[idx] = v->recs[0]; }
            v->recs_count = cnt;
            return 1;
        }
        if ( strcmp( path, "tones[0]" ) == 0 && strcmp( kind, "ordinal" ) == 0 )
        {
            shapes_tone_t * elem = (shapes_tone_t *) (uintptr_t) shapes_bounds_tones_at( &v->tones, 0 );
            if ( elem == NULL ) { return 0; }
            CONFORMANCE_WREF_UNSIGNED( *elem, text );
            return 1;
        }
        if ( strcmp( path, "pick" ) == 0 && strcmp( kind, "tag" ) == 0 )
        {
            memset( &v->pick, 0, sizeof( v->pick ) );
            CONFORMANCE_WREF_UNSIGNED( v->pick.type, text );
            return 1;
        }
        return 0;
    }
    if ( strcmp( root, "VarMoods" ) == 0 )
    {
        schema_conformance_holder_VarMoods * holder = (schema_conformance_holder_VarMoods *) value;
        shapes_var_moods_t * v = (shapes_var_moods_t *) (uintptr_t) holder->root;
        if ( v == NULL ) { return 0; }
        if ( strcmp( path, "row[0]" ) == 0 && strcmp( kind, "ordinal" ) == 0 )
        {
            shapes_mood_t * elem = (shapes_mood_t *) (uintptr_t) shapes_var_moods_row_at( &v->row, 0 );
            if ( elem == NULL ) { return 0; }
            return CONFORMANCE_WREF_UNSIGNED( *elem, text );
        }
        if ( strcmp( path, "mood" ) == 0 && strcmp( kind, "ordinal" ) == 0 ) { return CONFORMANCE_WREF_UNSIGNED( v->mood, text ); }
        return 0;
    }
    return 0;
}

int conformance_write_refusal_write_shapes( int surface, const char * root, const void * value, const uint8_t * announcement,
                                            int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    (void) announcement; (void) announcement_bytes;
    if ( surface != CONFORMANCE_WRITE_FIXED ) { return 0; }
    if ( strcmp( root, "FixedBounds" ) == 0 )
    {
        *wrote = shapes_fixed_bounds_fixed_save( (const shapes_fixed_bounds_t *) value, 1, buffer, capacity );
        return 1;
    }
    return 0;
}

const ConformanceCodec * conformance_codecs_shapes( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
