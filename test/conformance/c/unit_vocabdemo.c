#include "VocabTable.h"
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
