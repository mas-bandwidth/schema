/* the featurewitnesses unit (testdata/conformance/feature-witnesses/full). */
#include "FeaturesTable.h"
#define HC_PACKAGE featurewitnesses
#define HC_PACKAGE_UPPER FEATUREWITNESSES
#include "hostile_c.h"

HC_VAR( root )

static const hc_binding_t rows[] = {
    { HC_VAR_FIELDS( "testdata/conformance/feature-witnesses/full/Features.schema", "Root", root ) },
};

const hc_binding_t * hc_bindings_featurewitnesses( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
