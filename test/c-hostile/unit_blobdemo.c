/* the blobdemo unit (tables/blobs). */
#include "AssetsTable.h"
#define HC_PACKAGE blobdemo
#define HC_PACKAGE_UPPER BLOBDEMO
#include "hostile_c.h"

HC_VAR( catalog )

static const hc_binding_t rows[] = {
    { HC_VAR_FIELDS( "tables/blobs", "Catalog", catalog ) },
};

const hc_binding_t * hc_bindings_blobdemo( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
