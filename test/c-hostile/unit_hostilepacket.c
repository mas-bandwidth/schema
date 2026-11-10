/* the hostilepacket unit (internal/ci/hostilecorpus/corpus/packet/hostile.schema):
   packet types only. Its generated hostile.h is reached through hostileWire.h,
   which includes it from its own directory; this runner's hostile.h is the one
   hostile_c.h includes. */
#include "hostileWire.h"
#define HC_PACKAGE hostilepacket
#define HC_PACKAGE_UPPER HOSTILEPACKET
#include "hostile_c.h"

HC_PACKET( scalar_range )
HC_PACKET( bad_enum )
HC_PACKET( bad_union )
HC_PACKET( bad_count )
HC_PACKET( bad_const )
HC_PACKET( bad_reserved )
HC_PACKET( bad_align )
HC_PACKET( truncated )

#define HC_PACKET_ROW( TABLE, FN ) \
    { .schema = "internal/ci/hostilecorpus/corpus/packet/hostile.schema", .table = TABLE, .packet = hc_packet_##FN }

static const hc_binding_t rows[] = {
    HC_PACKET_ROW( "ScalarRange", scalar_range ),
    HC_PACKET_ROW( "BadEnum", bad_enum ),
    HC_PACKET_ROW( "BadUnion", bad_union ),
    HC_PACKET_ROW( "BadCount", bad_count ),
    HC_PACKET_ROW( "BadConst", bad_const ),
    HC_PACKET_ROW( "BadReserved", bad_reserved ),
    HC_PACKET_ROW( "BadAlign", bad_align ),
    HC_PACKET_ROW( "Truncated", truncated ),
};

const hc_binding_t * hc_bindings_hostilepacket( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
