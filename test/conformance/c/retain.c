#include "driver.h"
#include "RT1Table.h"

/* The root owns its bounded byte store and retained-ID list. */
int conformance_retain(const uint8_t * wire,int64_t bytes,int short_buffer,int64_t id_capacity,int * counters,uint8_t ** output,int64_t * output_bytes)
{
 enum { roomy=1<<20 };
 tblrt1_table_retain_t retain;const tblrt1_node_t * root;tblrt1_table_report_t report={0},save={0};
 uint8_t * region=NULL;int64_t need,size;int ok=0;
 memset(&retain,0,sizeof(retain));*output=NULL;*output_bytes=0;
 if(id_capacity<0)id_capacity=roomy/8;
 if(id_capacity>INT32_MAX)return 0;
 need=tblrt1_node_load_measure(wire,bytes);
 if(need<0){return 0;}region=(uint8_t *)malloc((size_t)need);if(!region)return 0;
 retain.bytes=(uint8_t *)malloc(roomy);retain.capacity=roomy;retain.ids=(tblrt1_table_retain_id_t *)calloc((size_t)(id_capacity?id_capacity:1),sizeof(tblrt1_table_retain_id_t));retain.id_capacity=(int32_t)id_capacity;if(!retain.bytes||!retain.ids)goto done;
 if(short_buffer){if(!tblrt1_node_load_retain(region,need,wire,bytes,&retain,&report))goto done;retain.capacity=retain.used-1;report=(tblrt1_table_report_t){0};}
 root=tblrt1_node_load_retain(region,need,wire,bytes,&retain,&report);if(!root)goto done;
 counters[0]=report.retained;counters[1]=report.retain_lost;counters[2]=report.unknown;
 size=tblrt1_node_measure_retain(root,&retain);if(size<0)goto done;*output=(uint8_t *)malloc((size_t)(size?size:1));if(!*output)goto done;
 if(tblrt1_node_save_retain(root,&retain,*output,size,&save)!=size)goto done;
 counters[3]=save.retain_lost;*output_bytes=size;ok=1;
 done:free(retain.bytes);free(retain.ids);free(region);if(!ok){free(*output);*output=NULL;}return ok;
}
