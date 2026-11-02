// the messagedemo unit (tables/messages).
#include "MessagesTable.h"
#define HC_NS messagedemo
#include "hostile.hpp"

HC_CPP_REASONS()
HC_FIXED( ToolMessage )
HC_MESSAGE( ToolMessage )

extern "C" const hc_binding_t * hc_bindings_messagedemo( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_FIXED_ROW( "tables/messages", "ToolMessage", ToolMessage ); row.message = hc_message_ToolMessage; rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
