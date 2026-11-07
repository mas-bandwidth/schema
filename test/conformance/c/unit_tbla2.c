#include "A2Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tbla2
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLA2
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Root, root )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tbla2, Root )
};
const ConformanceCodec * conformance_codecs_tbla2( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
