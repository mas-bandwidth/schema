#include "TableWriteBoundsTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE writebounds
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER WRITEBOUNDS
#include "unit.h"

SCHEMA_CONFORMANCE_GRAPH_CODEC( TortureVarWref, torture_var_wref )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( writebounds, TortureVarWref )
};

const ConformanceCodec * conformance_codecs_writebounds( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}

int conformance_write_refusal_set_writebounds( const char * root, void * value, const char * path,
                                               const char * kind, const char * text )
{
    schema_conformance_holder_TortureVarWref * holder = (schema_conformance_holder_TortureVarWref *) value;
    writebounds_torture_var_wref_t * v = (writebounds_torture_var_wref_t *) (uintptr_t) holder->root;
    if ( strcmp( root, "TortureVarWref" ) != 0 || v == NULL ) { return 0; }
    if ( strcmp( path, "opt_arr" ) == 0 && strcmp( kind, "count" ) == 0 )
    {
        const int32_t count = conformance_write_refusal_count( text );
        const int32_t capacity = (int32_t) ( sizeof( v->opt_arr ) / sizeof( v->opt_arr[0] ) );
        int32_t i;
        for ( i = v->opt_arr_count; i < count && i < capacity; i++ ) { v->opt_arr[i] = v->opt_arr[0]; }
        v->opt_arr_count = count;
        v->opt_arr_present = 1;
        return 1;
    }
    if ( strcmp( path, "arm_str.a_str" ) == 0 && v->arm_str.type == WRITEBOUNDS_WREF_ARM_TYPE_A_STR )
    {
        const int32_t capacity = (int32_t) sizeof( v->arm_str.as.a_str.value ) - 1;
        return conformance_write_refusal_text( v->arm_str.as.a_str.value, capacity, &v->arm_str.as.a_str.value_length, kind, text );
    }
    if ( strcmp( kind, "int" ) != 0 ) { return 0; }
    if ( strcmp( path, "arm_bits.a_bits" ) == 0 && v->arm_bits.type == WRITEBOUNDS_WREF_ARM_TYPE_A_BITS )
    {
        CONFORMANCE_WREF_UNSIGNED( v->arm_bits.as.a_bits, text );
        return 1;
    }
    if ( strcmp( path, "arm_range.a_range" ) == 0 && v->arm_range.type == WRITEBOUNDS_WREF_ARM_TYPE_A_RANGE )
    {
        CONFORMANCE_WREF_SIGNED( v->arm_range.as.a_range, text );
        return 1;
    }
    if ( strcmp( path, "chan_lim[1]" ) == 0 )
    {
        CONFORMANCE_WREF_SIGNED( v->chan_lim[1], text );
        return 1;
    }
    return 0;
}
