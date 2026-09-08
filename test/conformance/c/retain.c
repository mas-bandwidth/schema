#include "driver.h"
#include "RT1Table.h"

/* Each body owns its bounded byte store and retained-ID list. */
int conformance_retain(int message,const uint8_t * announcement,int64_t announcement_bytes,const uint8_t * wire,int64_t bytes,int short_buffer,int64_t id_capacity,int * counters,uint8_t ** output,int64_t * output_bytes)
{
 enum { roomy=1<<20, batch_max=256 };
 TableMessageEntry entries[256];TableVocabulary vocabulary=table_vocabulary(entries,256);
 TableRetain retains[batch_max];const Node * roots[batch_max];TableReport report={0},save={0};
 uint8_t * region=NULL;int64_t need,count=1,i,total=0;int ok=0;
 memset(retains,0,sizeof(retains));*output=NULL;*output_bytes=0;
 if(id_capacity<0)id_capacity=roomy/8;
 if(id_capacity>INT32_MAX)return 0;
 if(message){if(!announce_read(&vocabulary,announcement,announcement_bytes,&report)||bytes<2)return 0;count=(int64_t)wire[1]+1;need=node_load_measure_messages(&vocabulary,wire,bytes);}
 else need=node_load_measure(wire,bytes);
 if(need<0){return 0;}region=(uint8_t *)malloc((size_t)need);if(!region)return 0;
 for(i=0;i<count;i++){retains[i].bytes=(uint8_t *)malloc(roomy);retains[i].capacity=roomy;retains[i].ids=(TableRetainId *)calloc((size_t)(id_capacity?id_capacity:1),sizeof(TableRetainId));retains[i].id_capacity=(int32_t)id_capacity;if(!retains[i].bytes||!retains[i].ids)goto done;}
 if(message){if(!node_load_retain_messages(roots,&count,region,need,&vocabulary,wire,bytes,retains,&report))goto done;}
 else {if(short_buffer){if(!node_load_retain(region,need,wire,bytes,retains,&report))goto done;retains[0].capacity=retains[0].used-1;report=(TableReport){0};}roots[0]=node_load_retain(region,need,wire,bytes,retains,&report);if(!roots[0])goto done;}
 counters[0]=report.retained;counters[1]=report.retain_lost;counters[2]=report.unknown;
 for(i=0;i<count;i++){int64_t size=node_measure_retain(roots[i],retains+i);uint8_t * grown;if(size<0||size>INT64_MAX-total)goto done;grown=(uint8_t *)realloc(*output,(size_t)(total+size));if(!grown)goto done;*output=grown;if(node_save_retain(roots[i],retains+i,grown+total,size,&save)!=size)goto done;total+=size;}
 counters[3]=save.retain_lost;*output_bytes=total;ok=1;
 done:for(i=0;i<batch_max;i++){free(retains[i].bytes);free(retains[i].ids);}free(region);if(!ok){free(*output);*output=NULL;}return ok;
}
