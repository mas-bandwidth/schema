/* the listdemo unit (tables/lists). */
#include "HoldersTable.h"
#define HC_PACKAGE listdemo
#define HC_PACKAGE_UPPER LISTDEMO
#include "hostile_c.h"

HC_VAR( army )

static const hc_binding_t rows[] = {
    { HC_VAR_FIELDS( "tables/lists", "Army", army ) },
};

const hc_binding_t * hc_bindings_listdemo( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
