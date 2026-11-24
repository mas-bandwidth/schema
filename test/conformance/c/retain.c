#include "driver.h"
#include "RT1Table.h"

/* The root owns its bounded byte store and retained-ID list. */
int conformance_retain(const uint8_t * wire,int64_t bytes,int short_buffer,int64_t id_capacity,int * counters,uint8_t ** output,int64_t * output_bytes,char ** text,int64_t * text_bytes)
{
 enum { roomy=1<<20 };
 tblrt1_table_retain_t retain;const tblrt1_node_t * root;tblrt1_table_report_t report={0},save={0};
 uint8_t * region=NULL;int64_t need,size;int ok=0;
 memset(&retain,0,sizeof(retain));*output=NULL;*output_bytes=0;*text=NULL;*text_bytes=0;
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
 counters[3]=save.retain_lost;*output_bytes=size;
 /* the known value the retaining read returned, as the §16 text */
 *text_bytes=tblrt1_node_to_json_measure(root);if(*text_bytes<0)goto done;*text=(char *)malloc((size_t)*text_bytes+1);if(!*text)goto done;
 if(tblrt1_node_to_json(root,*text,*text_bytes)!=*text_bytes)goto done;
 ok=1;
 done:free(retain.bytes);free(retain.ids);free(region);if(!ok){free(*output);*output=NULL;free(*text);*text=NULL;}return ok;
}

/* READ-INTO, THE `retain` FORM (test/conformance/README.md): ONE report, which
   the driver PRE-FILLS with every field set, through the lawful retaining
   load, the damaged one and the lawful one again. After each lawful load the
   report must equal, FIELD BY FIELD, the report the same load leaves in a
   fresh report (docs/SPEC-TABLES.md §6.5: every read zeroes a supplied report
   in full as its first act, the retain family included). Every field of the C
   report is a public member, so the pre-fill sets each one directly. */
static const tblrt1_node_t * retain_into_load(const uint8_t * wire,int64_t bytes,tblrt1_table_report_t * report,int * ran)
{
 enum { roomy=1<<20 };
 tblrt1_table_retain_t retain;const tblrt1_node_t * root;uint8_t * raw;uint8_t * region;int64_t need;
 *ran=0;memset(&retain,0,sizeof(retain));
 need=tblrt1_node_load_measure(wire,bytes);if(need<0)need=0;
 raw=(uint8_t *)malloc((size_t)need+64);if(!raw)return NULL;region=raw+((64-((uintptr_t)raw&63))&63);
 retain.bytes=(uint8_t *)malloc(roomy);retain.capacity=roomy;retain.ids=(tblrt1_table_retain_id_t *)calloc(roomy/8,sizeof(tblrt1_table_retain_id_t));retain.id_capacity=roomy/8;
 if(!retain.bytes||!retain.ids){free(retain.bytes);free(retain.ids);free(raw);return NULL;}
 root=tblrt1_node_load_retain(region,need,wire,bytes,&retain,report);*ran=1;
 free(retain.bytes);free(retain.ids);free(raw);
 return root; /* compared against NULL only: the region is gone */
}
static const char * retain_into_step(const uint8_t * wire,int64_t bytes,tblrt1_table_report_t * report,int * ran)
{
 const int ok=retain_into_load(wire,bytes,report,ran)!=NULL;
 const int refused=report->refused||report->refuse_reason!=0||report->damage_reason!=0;
 if(ok&&!refused)return "read\n";
 if(!ok&&refused)return "refused\n";
 return ok?"read-with-refused-report\n":"refused-with-clean-report\n";
}
static void retain_into_compare(ConformanceText * answer,const tblrt1_table_report_t * r,const tblrt1_table_report_t * fresh)
{
 char line[256];int same=1;
#define RETAIN_INTO_FIELD(f) if(r->f!=fresh->f){snprintf(line,sizeof(line),"kept " #f " %lld want %lld\n",(long long)r->f,(long long)fresh->f);conformance_text_add(answer,line);same=0;}
 RETAIN_INTO_FIELD(unknown) RETAIN_INTO_FIELD(missing_required) RETAIN_INTO_FIELD(widened) RETAIN_INTO_FIELD(blob_shared)
 RETAIN_INTO_FIELD(retained) RETAIN_INTO_FIELD(retain_lost) RETAIN_INTO_FIELD(refused) RETAIN_INTO_FIELD(reason)
 RETAIN_INTO_FIELD(refuse_reason) RETAIN_INTO_FIELD(damage_reason) RETAIN_INTO_FIELD(non_finite_floats)
 RETAIN_INTO_FIELD(build_version_remote) RETAIN_INTO_FIELD(build_version_own) RETAIN_INTO_FIELD(layout_hash)
#undef RETAIN_INTO_FIELD
 if(same)conformance_text_add(answer,"zeroed\n");
}
int conformance_read_into_retain(const uint8_t * lawful,int64_t lawful_bytes,const uint8_t * damaged,int64_t damaged_bytes,ConformanceText * answer)
{
 tblrt1_table_report_t fresh,report;int ran;
 memset(&fresh,0,sizeof(fresh));
 if(retain_into_load(lawful,lawful_bytes,&fresh,&ran)==NULL||fresh.refused){fprintf(stderr,"driver: read-into: the lawful retaining load refused into a fresh report\n");return 2;}
 memset(&report,0,sizeof(report));
 report.unknown=7;report.missing_required=7;report.widened=7;report.blob_shared=7;report.retained=7;report.retain_lost=7;
 report.refused=1;report.reason=7;report.refuse_reason=7;report.damage_reason=7;report.non_finite_floats=7;
 report.build_version_remote=7;report.build_version_own=7;report.layout_hash=7;
 conformance_text_add(answer,retain_into_step(lawful,lawful_bytes,&report,&ran));if(!ran)return 2;
 retain_into_compare(answer,&report,&fresh);
 conformance_text_add(answer,retain_into_step(damaged,damaged_bytes,&report,&ran));if(!ran)return 2;
 conformance_text_add(answer,retain_into_step(lawful,lawful_bytes,&report,&ran));if(!ran)return 2;
 retain_into_compare(answer,&report,&fresh);
 return 0;
}
