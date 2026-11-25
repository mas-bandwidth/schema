/* FL-19's map form (test/tables/moodmap), opted in by main.c's list. */
#include "MoodMapTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblmoodmap
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLMOODMAP
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(MoodMap,mood_map)
const ConformanceCodec * conformance_codecs_tblmoodmap(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(tblmoodmap,MoodMap)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
