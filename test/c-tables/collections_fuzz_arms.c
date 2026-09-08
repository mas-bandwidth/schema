#include "arms/CarryTable.h"
#include "arms/GateTable.h"
#include "arms/NestTable.h"
#include "arms/RingTable.h"
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Holder,holder)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Hand,hand)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Chain,chain)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Gate,gate)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Nest,nest)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Ring,ring)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Rack,rack)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Tray,tray)
const ConformanceCodec * conformance_codecs_armdemo(int * count) {
 static const ConformanceCodec codecs[]={
 SCHEMA_CONFORMANCE_GRAPH_ROW(armdemo,Holder),
 SCHEMA_CONFORMANCE_GRAPH_ROW(armdemo,Hand),
 SCHEMA_CONFORMANCE_GRAPH_ROW(armdemo,Chain),
 SCHEMA_CONFORMANCE_GRAPH_ROW(armdemo,Gate),
 SCHEMA_CONFORMANCE_GRAPH_ROW(armdemo,Nest),
 SCHEMA_CONFORMANCE_GRAPH_ROW(armdemo,Ring),
 SCHEMA_CONFORMANCE_GRAPH_ROW(armdemo,Rack),
 SCHEMA_CONFORMANCE_GRAPH_ROW(armdemo,Tray),
 };*count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
