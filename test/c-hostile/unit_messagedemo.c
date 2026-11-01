/* the messagedemo unit (tables/messages). */
#include "MessagesTable.h"
#define HC_PACKAGE messagedemo
#define HC_PACKAGE_UPPER MESSAGEDEMO
#include "hostile_c.h"

HC_FIXED( tool_message )
HC_MESSAGE( tool_message )

static const hc_binding_t rows[] = {
    { HC_FIXED_FIELDS( "tables/messages", "ToolMessage", tool_message ), .message = hc_message_tool_message },
};

const hc_binding_t * hc_bindings_messagedemo( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
