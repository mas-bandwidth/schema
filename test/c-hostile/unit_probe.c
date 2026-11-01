/* the probe unit (schemas/probe.schema). */
#include "probeTable.h"
#define HC_PACKAGE probe
#define HC_PACKAGE_UPPER PROBE
#include "hostile_c.h"

HC_VAR( root )

static const hc_binding_t rows[] = {
    { HC_VAR_FIELDS( "schemas/probe.schema", "Root", root ) },
};

const hc_binding_t * hc_bindings_probe( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
