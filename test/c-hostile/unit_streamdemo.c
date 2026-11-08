/* the streamdemo unit (tables/stream). */
#include "StreamTable.h"
#define HC_PACKAGE streamdemo
#define HC_PACKAGE_UPPER STREAMDEMO
#include "hostile_c.h"

HC_VAR( feed )

static const hc_binding_t rows[] = {
    { HC_VAR_FIELDS( "tables/stream", "Feed", feed ) },
};

const hc_binding_t * hc_bindings_streamdemo( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
