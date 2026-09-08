// The unchanged C++ reference codecs on the C driver's differential protocol.
#include "driver.h"
#include "maps/CellsTable.h"
#include "maps/ChunksTable.h"
#include "maps/CrewsTable.h"
#include "maps/DepthTable.h"
#include "maps/DocsTable.h"
#include "maps/FleetTable.h"
#include "maps/PairsTable.h"
#include "maps/RowsTable.h"
#include "maps/RunsTable.h"
#include "maps/SlotsTable.h"
#include "maps/SpansTable.h"
#include "maps/TextTable.h"
#include "maps/TrailsTable.h"
#include "lists/HoldersTable.h"
#include "lists/MigrateTable.h"
#include "lists/ReportTable.h"
#include "lists/SaveTable.h"
#include "lists/SharedTable.h"

#include "arms/CarryTable.h"
#include "arms/GateTable.h"
#include "arms/NestTable.h"
#include "arms/RingTable.h"

#define REFERENCE_CODEC(NS,TYPE) \
static const NS::TYPE * reference_##NS##_##TYPE; \
static void * reference_storage_##NS##_##TYPE() { free((void *)reference_##NS##_##TYPE); reference_##NS##_##TYPE=NULL; return &reference_##NS##_##TYPE; } \
static int reference_load_##NS##_##TYPE(void * value,const uint8_t * wire,int64_t bytes,ConformanceReport * out) { \
 NS::TableReport report; int64_t need=NS::TYPE##LoadMeasure(wire,bytes); \
 uint8_t * region=(uint8_t *)malloc(need>0 ? (size_t)need : 1); \
 const NS::TYPE * root=NS::TYPE##Load(region,need,wire,bytes,&report); \
 *(const NS::TYPE **)value=root; if(root==NULL) { free(region); } \
 out->unknown=report.unknown;out->kind_mismatch=report.kind_mismatch;out->widened=report.widened;out->clamped=report.clamped; \
 out->duplicate=report.duplicate;out->malformed=report.malformed;out->refused=report.refused;return root!=NULL; } \
static int64_t reference_measure_##NS##_##TYPE(const void * value) { const NS::TYPE * root=*(const NS::TYPE * const *)value;return root ? NS::TYPE##Measure(root) : -1; } \
static int64_t reference_save_##NS##_##TYPE(const void * value,uint8_t * buffer,int64_t capacity) { return NS::TYPE##Save(*(const NS::TYPE * const *)value,buffer,capacity); } \
static int64_t reference_load_measure_##NS##_##TYPE(const uint8_t * wire,int64_t bytes) { return NS::TYPE##LoadMeasure(wire,bytes); } \
static int64_t reference_cook_measure_##NS##_##TYPE(const void * value) { return NS::TYPE##CookMeasure(*(const NS::TYPE * const *)value); } \
static int reference_cook_##NS##_##TYPE(const void * value,void * buffer,uint64_t capacity,int big) { return NS::TYPE##Cook(*(const NS::TYPE * const *)value,buffer,capacity,big ? NS::TableByteOrder::Big : NS::TableByteOrder::Little); }
#define REFERENCE_ROW(NS,TYPE) {#NS,#TYPE,NULL,NULL,reference_load_##NS##_##TYPE,reference_measure_##NS##_##TYPE,reference_save_##NS##_##TYPE,NULL,NULL,reference_storage_##NS##_##TYPE,reference_load_measure_##NS##_##TYPE,reference_cook_measure_##NS##_##TYPE,reference_cook_##NS##_##TYPE,NULL}
REFERENCE_CODEC(mapdemo,Depth)
REFERENCE_CODEC(mapdemo,Text)
REFERENCE_CODEC(mapdemo,Cells)
REFERENCE_CODEC(mapdemo,Runs)
REFERENCE_CODEC(mapdemo,Slots)
REFERENCE_CODEC(mapdemo,Spans)
REFERENCE_CODEC(mapdemo,Docs)
REFERENCE_CODEC(mapdemo,Chunks)
REFERENCE_CODEC(mapdemo,Pairs)
REFERENCE_CODEC(mapdemo,Crews)
REFERENCE_CODEC(mapdemo,Trails)
REFERENCE_CODEC(mapdemo,Fleet)
const ConformanceCodec * conformance_codecs_mapdemo(int * count) {
 static const ConformanceCodec codecs[]={
 REFERENCE_ROW(mapdemo,Depth),
 REFERENCE_ROW(mapdemo,Text),
 REFERENCE_ROW(mapdemo,Cells),
 REFERENCE_ROW(mapdemo,Runs),
 REFERENCE_ROW(mapdemo,Slots),
 REFERENCE_ROW(mapdemo,Spans),
 REFERENCE_ROW(mapdemo,Docs),
 REFERENCE_ROW(mapdemo,Chunks),
 REFERENCE_ROW(mapdemo,Pairs),
 REFERENCE_ROW(mapdemo,Crews),
 REFERENCE_ROW(mapdemo,Trails),
 REFERENCE_ROW(mapdemo,Fleet),
 }; *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
REFERENCE_CODEC(listdemo,Save)
REFERENCE_CODEC(listdemo,Mixed)
REFERENCE_CODEC(listdemo,Album)
REFERENCE_CODEC(listdemo,Sheet)
REFERENCE_CODEC(listdemo,Army)
REFERENCE_CODEC(listdemo,Unbounded)
const ConformanceCodec * conformance_codecs_listdemo(int * count) {
 static const ConformanceCodec codecs[]={
 REFERENCE_ROW(listdemo,Save),
 REFERENCE_ROW(listdemo,Mixed),
 REFERENCE_ROW(listdemo,Album),
 REFERENCE_ROW(listdemo,Sheet),
 REFERENCE_ROW(listdemo,Army),
 REFERENCE_ROW(listdemo,Unbounded),
 }; *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}

REFERENCE_CODEC(armdemo,Holder)
REFERENCE_CODEC(armdemo,Hand)
REFERENCE_CODEC(armdemo,Chain)
REFERENCE_CODEC(armdemo,Gate)
REFERENCE_CODEC(armdemo,Nest)
REFERENCE_CODEC(armdemo,Ring)
REFERENCE_CODEC(armdemo,Rack)
REFERENCE_CODEC(armdemo,Tray)
const ConformanceCodec * conformance_codecs_armdemo(int * count) {
 static const ConformanceCodec codecs[]={
 REFERENCE_ROW(armdemo,Holder),
 REFERENCE_ROW(armdemo,Hand),
 REFERENCE_ROW(armdemo,Chain),
 REFERENCE_ROW(armdemo,Gate),
 REFERENCE_ROW(armdemo,Nest),
 REFERENCE_ROW(armdemo,Ring),
 REFERENCE_ROW(armdemo,Rack),
 REFERENCE_ROW(armdemo,Tray),
 };*count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}

#define SCHEMA_C_COLLECTIONS_FUZZ
#include "wire_fuzz_main.c"
