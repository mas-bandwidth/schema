#include "lists/HoldersTable.h"
#include "lists/MigrateTable.h"
#include "lists/ReportTable.h"
#include "lists/SaveTable.h"
#include "lists/SharedTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE listdemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER LISTDEMO
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Save,save)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Mixed,mixed)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Album,album)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Sheet,sheet)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Army,army)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Unbounded,unbounded)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Ints,ints)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Squad,squad)
SCHEMA_CONFORMANCE_CODEC(Bounded,bounded)
const ConformanceCodec * conformance_codecs_listdemo(int * count) {
 static const ConformanceCodec codecs[]={
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Save),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Mixed),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Album),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Sheet),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Army),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Unbounded),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Ints),
 SCHEMA_CONFORMANCE_GRAPH_ROW(listdemo,Squad),
 SCHEMA_CONFORMANCE_ROW(listdemo,Bounded),
 }; *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
