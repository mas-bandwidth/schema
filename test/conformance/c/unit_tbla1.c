#include "A1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tbla1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLA1
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Root, root )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tbla1, Root )
};
const ConformanceCodec * conformance_codecs_tbla1( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
