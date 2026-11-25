// the tblmoodmap unit (test/tables/moodmap): FL-19's map form.
#include "MoodMapTable.h"
#define HC_NS tblmoodmap
#include "hostile.hpp"

HC_CPP_REASONS()
HC_VAR( MoodMap )

extern "C" const hc_binding_t * hc_bindings_tblmoodmap( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_VAR_ROW( "test/tables/moodmap", "MoodMap", MoodMap ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
