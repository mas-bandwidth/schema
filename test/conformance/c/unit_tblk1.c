#include "K1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblk1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLK1
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Root, root )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblk1, Root )
};
const ConformanceCodec * conformance_codecs_tblk1( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
