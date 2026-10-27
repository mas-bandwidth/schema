#include "M2Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblm2
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLM2
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Msg, msg )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblm2, Msg )
};
const ConformanceCodec * conformance_codecs_tblm2( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
