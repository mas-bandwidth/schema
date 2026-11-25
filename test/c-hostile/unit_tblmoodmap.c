/* the tblmoodmap unit (test/tables/moodmap): FL-19's map form. */
#include "MoodMapTable.h"
#define HC_PACKAGE tblmoodmap
#define HC_PACKAGE_UPPER TBLMOODMAP
#include "hostile_c.h"

HC_VAR( mood_map )

static const hc_binding_t rows[] = {
    { HC_VAR_FIELDS( "test/tables/moodmap", "MoodMap", mood_map ) },
};

const hc_binding_t * hc_bindings_tblmoodmap( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
