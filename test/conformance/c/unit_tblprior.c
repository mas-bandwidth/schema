#include "PriorNarrowTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblprior
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLPRIOR
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( Prior, prior )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblprior, Prior )
};

const ConformanceCodec * conformance_codecs_tblprior( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
