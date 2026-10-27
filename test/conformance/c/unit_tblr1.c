#include "R1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblr1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLR1
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Cfg, cfg )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblr1, Cfg )
};
const ConformanceCodec * conformance_codecs_tblr1( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
