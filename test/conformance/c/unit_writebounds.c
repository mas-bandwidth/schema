#include "TableWriteBoundsTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE writebounds
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER WRITEBOUNDS
#include "unit.h"

/* ACCEPTED VARIABLE-TABLE WRITE BOUNDS (docs/SPEC.md §5.1): TortureVarWref is
 * the variable class, its seed lawful, and each write-refusal row moves one
 * field or union arm to its declared boundary before the variable save. */
SCHEMA_CONFORMANCE_GRAPH_CODEC( TortureVarWref, torture_var_wref )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( writebounds, TortureVarWref )
};

/* the arm a path names is the one the union carries: select it when it is not */
static void writebounds_select_arm( writebounds_wref_arm_t * arm, writebounds_wref_arm_type_t type )
{
    if ( arm->type != type )
    {
        memset( arm, 0, sizeof( *arm ) );
        arm->type = type;
    }
}

int conformance_write_refusal_set_writebounds( const char * root, void * value, const char * path,
                                               const char * kind, const char * text )
{
    schema_conformance_holder_TortureVarWref * holder;
    writebounds_torture_var_wref_t * v;
    if ( strcmp( root, "TortureVarWref" ) != 0 ) { return 0; }
    holder = (schema_conformance_holder_TortureVarWref *) value;
    v = (writebounds_torture_var_wref_t *) (uintptr_t) holder->root;
    if ( v == NULL ) { return 0; }
    if ( strcmp( path, "opt_arr" ) == 0 && strcmp( kind, "count" ) == 0 )
    {
        const int32_t cap = (int32_t) ( sizeof( v->opt_arr ) / sizeof( v->opt_arr[0] ) );
        const int32_t count = conformance_write_refusal_count( text );
        int32_t i;
        for ( i = v->opt_arr_count; i < count && i < cap; i++ ) { v->opt_arr[i] = v->opt_arr_count > 0 ? v->opt_arr[0] : 0; }
        v->opt_arr_count = count;
        v->opt_arr_present = 1;
        return 1;
    }
    if ( strcmp( path, "arm_bits.a_bits" ) == 0 && strcmp( kind, "int" ) == 0 )
    {
        writebounds_select_arm( &v->arm_bits, WRITEBOUNDS_WREF_ARM_TYPE_A_BITS );
        return CONFORMANCE_WREF_UNSIGNED( v->arm_bits.as.a_bits, text );
    }
    if ( strcmp( path, "arm_range.a_range" ) == 0 && strcmp( kind, "int" ) == 0 )
    {
        writebounds_select_arm( &v->arm_range, WRITEBOUNDS_WREF_ARM_TYPE_A_RANGE );
        return CONFORMANCE_WREF_SIGNED( v->arm_range.as.a_range, text );
    }
    if ( strcmp( path, "arm_str.a_str" ) == 0 )
    {
        writebounds_select_arm( &v->arm_str, WRITEBOUNDS_WREF_ARM_TYPE_A_STR );
        return conformance_write_refusal_text( v->arm_str.as.a_str.value, 8, &v->arm_str.as.a_str.value_length, kind, text );
    }
    if ( strcmp( path, "chan_lim[1]" ) == 0 && strcmp( kind, "int" ) == 0 )
    {
        return CONFORMANCE_WREF_SIGNED( v->chan_lim[1], text );
    }
    return 0;
}

const ConformanceCodec * conformance_codecs_writebounds( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
