#include "CookTortureTable.h"
#include "CookTortureBlock.h"
#define SCHEMA_CONFORMANCE_PACKAGE cooktorture
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER COOKTORTURE
#include "unit.h"
#include <math.h>

SCHEMA_CONFORMANCE_GRAPH_CODEC( TortureCook, torture_cook )
SCHEMA_CONFORMANCE_CODEC( TortureFixedWref, torture_fixed_wref )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( cooktorture, TortureCook ),
    SCHEMA_CONFORMANCE_ROW( cooktorture, TortureFixedWref )
};

static int parse_cooktorture_f32( const char * text, float * out )
{
    char * end = NULL;
    if ( strcmp( text, "nan" ) == 0 ) { *out = NAN; return 1; }
    if ( strcmp( text, "inf" ) == 0 ) { *out = INFINITY; return 1; }
    if ( strcmp( text, "-inf" ) == 0 ) { *out = -INFINITY; return 1; }
    *out = strtof( text, &end );
    return end != NULL && *end == 0;
}

static int parse_cooktorture_f64( const char * text, double * out )
{
    char * end = NULL;
    if ( strcmp( text, "nan" ) == 0 ) { *out = NAN; return 1; }
    if ( strcmp( text, "inf" ) == 0 ) { *out = INFINITY; return 1; }
    if ( strcmp( text, "-inf" ) == 0 ) { *out = -INFINITY; return 1; }
    *out = strtod( text, &end );
    return end != NULL && *end == 0;
}

int conformance_write_refusal_set_cooktorture( const char * root, void * value, const char * path,
                                               const char * kind, const char * text )
{
    const long long n = strtoll( text, NULL, 10 );
    if ( strcmp( root, "TortureFixedWref" ) == 0 )
    {
        cooktorture_torture_fixed_wref_t * v = (cooktorture_torture_fixed_wref_t *) value;
        if ( strcmp( path, "counted_f64[0]" ) == 0 && strcmp( kind, "float" ) == 0 ) { return v->counted_f64_count >= 1 && parse_cooktorture_f64( text, &v->counted_f64[0] ); }
        if ( strcmp( path, "counted_f64" ) == 0 && strcmp( kind, "count" ) == 0 )
        {
            const int32_t cap = (int32_t) ( sizeof( v->counted_f64 ) / sizeof( v->counted_f64[0] ) );
            int32_t i;
            for ( i = v->counted_f64_count; i < (int32_t) n && i < cap; i++ ) { v->counted_f64[i] = v->counted_f64[0]; }
            v->counted_f64_count = (int32_t) n;
            return 1;
        }
        if ( strcmp( path, "fixed_f32[1]" ) == 0 && strcmp( kind, "float" ) == 0 ) { return parse_cooktorture_f32( text, &v->fixed_f32[1] ); }
        if ( strcmp( path, "chan_f32[1]" ) == 0 && strcmp( kind, "float" ) == 0 ) { return parse_cooktorture_f32( text, &v->chan_f32[1] ); }
        if ( strcmp( path, "opt_f32" ) == 0 && strcmp( kind, "float" ) == 0 ) { v->opt_f32_present = 1; return parse_cooktorture_f32( text, &v->opt_f32 ); }
        if ( strcmp( path, "u_val.u_f32" ) == 0 && strcmp( kind, "float" ) == 0 ) { return parse_cooktorture_f32( text, &v->u_val.as.u_f32 ); }
        if ( strcmp( path, "u_val.u_str" ) == 0 ) { return conformance_write_refusal_text( v->u_val.as.u_str.value, 16, &v->u_val.as.u_str.value_length, kind, text ); }
        return 0;
    }
    if ( strcmp( root, "TortureCook" ) == 0 )
    {
        schema_conformance_holder_TortureCook * holder = (schema_conformance_holder_TortureCook *) value;
        cooktorture_torture_cook_t * v = (cooktorture_torture_cook_t *) (uintptr_t) holder->root;
        if ( v == NULL ) { return 0; }
        if ( strcmp( path, "slice_f32[0]" ) == 0 && strcmp( kind, "float" ) == 0 )
        {
            float * elems;
            if ( v->slice_f32.count < 1 || v->slice_f32.items.value == 0 ) { return 0; }
            elems = (float *) (void *) ( (uint8_t *) (void *) &v->slice_f32.items + v->slice_f32.items.value );
            return parse_cooktorture_f32( text, &elems[0] );
        }
        if ( strcmp( path, "map_f64[0].value" ) == 0 && strcmp( kind, "float" ) == 0 )
        {
            cooktorture_torture_cook_map_f64_entry_t * entries;
            if ( v->map_f64.count < 1 || v->map_f64.items.value == 0 ) { return 0; }
            entries = (cooktorture_torture_cook_map_f64_entry_t *) (void *) ( (uint8_t *) (void *) &v->map_f64.items + v->map_f64.items.value );
            return parse_cooktorture_f64( text, &entries[0].value );
        }
        if ( strcmp( path, "map_f64[0].key" ) == 0 )
        {
            cooktorture_torture_cook_map_f64_entry_t * entries;
            if ( v->map_f64.count < 1 || v->map_f64.items.value == 0 ) { return 0; }
            entries = (cooktorture_torture_cook_map_f64_entry_t *) (void *) ( (uint8_t *) (void *) &v->map_f64.items + v->map_f64.items.value );
            return conformance_write_refusal_text( entries[0].key, 16, &entries[0].key_length, kind, text );
        }
        if ( strcmp( path, "str_arr[0]" ) == 0 )
        {
            return v->str_arr_count >= 1 && conformance_write_refusal_text( v->str_arr[0].value, 16, &v->str_arr[0].value_length, kind, text );
        }
        /* SILENT-FAILURE ROWS: the variable Save's bits(N) width on every carrier */
        if ( strcmp( kind, "int" ) == 0 )
        {
            if ( strcmp( path, "b_bits13" ) == 0 ) { v->b_bits13 = (uint32_t) n; return 1; }
            if ( strcmp( path, "opt_bits" ) == 0 ) { v->opt_bits = (uint64_t) n; v->opt_bits_present = 1; return 1; }
            if ( strcmp( path, "arr_bits[0].v" ) == 0 )
            {
                cooktorture_bits_cell_t * cells;
                if ( v->arr_bits.count < 1 || v->arr_bits.items.value == 0 ) { return 0; }
                cells = (cooktorture_bits_cell_t *) (void *) ( (uint8_t *) (void *) &v->arr_bits.items + v->arr_bits.items.value );
                cells[0].v = (uint64_t) n;
                return 1;
            }
        }
        return 0;
    }
    return 0;
}

int conformance_write_refusal_write_cooktorture( int surface, const char * root, const void * value, const uint8_t * announcement,
                                                 int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    (void) announcement; (void) announcement_bytes;
    if ( surface != CONFORMANCE_WRITE_FIXED ) { return 0; }
    if ( strcmp( root, "TortureFixedWref" ) == 0 ) { *wrote = cooktorture_torture_fixed_wref_fixed_save( (const cooktorture_torture_fixed_wref_t *) value, 1, buffer, capacity ); return 1; }
    return 0;
}
const ConformanceCodec * conformance_codecs_cooktorture( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}

int conformance_torture_block_open_ex( void * data, int64_t bytes, int * reason, const uint8_t ** out_base, const void ** out_type )
{
    cooktorture_torture_frame_block_t block;
    if ( !cooktorture_torture_frame_block_open_ex( &block, data, bytes, reason ) ) return 0;
    if ( out_base != NULL ) *out_base = block.base;
    if ( out_type != NULL ) *out_type = cooktorture_torture_frame_block_type();
    return 1;
}
