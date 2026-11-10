// the hostilepacket unit (internal/ci/hostilecorpus/corpus/packet/hostile.schema):
// packet types only. Its generated hostile.h is reached through hostileWire.h,
// which includes it from its own directory; this runner's hostile.h is the one
// hostile.hpp includes.
#include "hostileWire.h"
#define HC_NS hostilepacket
#include "hostile.hpp"

HC_PACKET( ScalarRange )
HC_PACKET( BadEnum )
HC_PACKET( BadUnion )
HC_PACKET( BadCount )
HC_PACKET( BadConst )
HC_PACKET( BadReserved )
HC_PACKET( BadAlign )
HC_PACKET( Truncated )

#define HC_PACKET_ROW( TYPE ) \
    hc_binding_t row; \
    std::memset( &row, 0, sizeof( row ) ); \
    row.schema = "internal/ci/hostilecorpus/corpus/packet/hostile.schema"; \
    row.table = #TYPE; \
    row.packet = hc_packet_##TYPE

extern "C" const hc_binding_t * hc_bindings_hostilepacket( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_PACKET_ROW( ScalarRange ); rows.push_back( row ); }
        { HC_PACKET_ROW( BadEnum ); rows.push_back( row ); }
        { HC_PACKET_ROW( BadUnion ); rows.push_back( row ); }
        { HC_PACKET_ROW( BadCount ); rows.push_back( row ); }
        { HC_PACKET_ROW( BadConst ); rows.push_back( row ); }
        { HC_PACKET_ROW( BadReserved ); rows.push_back( row ); }
        { HC_PACKET_ROW( BadAlign ); rows.push_back( row ); }
        { HC_PACKET_ROW( Truncated ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
