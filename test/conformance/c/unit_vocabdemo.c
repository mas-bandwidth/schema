#include "VocabTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE vocabdemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER VOCABDEMO
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Wide00, wide00 )
SCHEMA_CONFORMANCE_CODEC( Wide09, wide09 )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( vocabdemo, Wide00 ),
    SCHEMA_CONFORMANCE_ROW( vocabdemo, Wide09 )
};
const ConformanceCodec * conformance_codecs_vocabdemo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( Wide00, wide00 )
SCHEMA_CONFORMANCE_STRICT( Wide09, wide09 )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( vocabdemo, Wide00 ),
    SCHEMA_CONFORMANCE_STRICT_ROW( vocabdemo, Wide09 )
};

const ConformanceStrict * conformance_stricts_vocabdemo( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
