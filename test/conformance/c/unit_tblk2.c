#include "K2Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblk2
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLK2
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Root, root )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblk2, Root )
};
const ConformanceCodec * conformance_codecs_tblk2( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
