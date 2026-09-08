package compiler

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cMessageSchema = `package probe
enum Choice { First, Second }
flags Features { One, Two, Three }
type Leaf { x int32 = -17 | min = -1000, max = 1000
choice Choice }
union Body { text string(16)
numbers [..3]int16
leaf Leaf
empty }
table Root {
 enabled bool
 amount int16 = -3 | min = -8, max = 47
 ratio float32 | min = -1, max = 1, resolution = 0.01
 mask Features
 name string(12)
 payload bytes(12)
 units wstring(12)
 leaf Leaf
 counters [2]uint32
 slots [Choice]Leaf
 samples [..3]int32 | min = 0, max = 0
 body Body
 history [..2]Body
 choices ?[2]Choice
}
`

func TestCTableMessageSave(t *testing.T) {
	u := unitFromSource(t, cMessageSchema)
	m := tabletext.NewModel(u)
	texts := []string{`{}`, `{"enabled":true,"amount":17,"ratio":0.42,"mask":["One","Three"],"name":"hi","payload":"AQID","units":"𝄞","leaf":{"x":9,"choice":"Second"},"counters":[4,5],"slots":{"First":{"x":5}},"samples":[0,0,0],"body":{"text":"hello"},"history":[{"numbers":[2,3]},{"empty":null}],"choices":["First","Second"]}`, `{"body":{"leaf":{"x":22}},"payload":"BAUGBw=="}`}
	var instances []*tabletext.Instance
	var setup strings.Builder
	for i, s := range texts {
		v := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if !m.Read(v, []byte(s), &report) || !report.Silent() {
			t.Fatalf("fixture %d: %+v", i, report)
		}
		instances = append(instances, v)
		fmt.Fprintf(&setup, "CHECK(root_from_json(values+%d,%q,%d,&report));\n", i, s, len(s))
	}
	expected, err := tablewire.EncodeMessages(m, instances)
	if err != nil {
		t.Fatal(err)
	}
	octets := func(b []byte) string {
		var s strings.Builder
		for _, v := range b {
			fmt.Fprintf(&s, "0x%02x,", v)
		}
		return s.String()
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do {if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;}} while(0)
static const uint8_t expected[]={%s};
static const uint8_t announcement[]={%s};
int main(void) {
 Root values[3],decoded[3]; TableReport report={0}; TableMessageEntry entries[kTableMessageEntriesHere];
 TableVocabulary vocabulary=table_vocabulary(entries,kTableMessageEntriesHere);uint8_t output[4096];int64_t n,count;
 %s
 CHECK(!report.malformed && !report.clamped && !report.unknown);
 CHECK(announce_measure()==sizeof(announcement));CHECK(announce(output,sizeof(output))==sizeof(announcement));CHECK(!memcmp(output,announcement,sizeof(announcement)));
 CHECK(announce_read(&vocabulary,announcement,sizeof(announcement),&report));CHECK(vocabulary.announced && vocabulary.count==kTableMessageEntriesHere);
 CHECK(!report.refused && !report.malformed);
 n=root_measure_messages(values,3,&report);CHECK(n==sizeof(expected));
 CHECK(root_save_messages(values,3,output,n,&report)==n);CHECK(!memcmp(output,expected,sizeof(expected)));
 count=3;CHECK(root_load_messages(decoded,&count,&vocabulary,expected,sizeof(expected),&report));CHECK(count==3);
 CHECK(!report.malformed&&!report.refused&&!report.unknown&&!report.clamped&&!report.widened&&!report.kind_mismatch);
 CHECK(root_save_messages(decoded,3,output,n,&report)==n);CHECK(!memcmp(output,expected,sizeof(expected)));
 output[n-1]=0xa5;CHECK(root_save_messages(values,3,output,n-1,&report)==-1);CHECK(output[n-1]==0xa5);
 CHECK(root_measure_messages(values,0,&report)==-1);CHECK(root_measure_messages(values,257,&report)==-1);
 CHECK(report.refused && report.reason==SCHEMA_TABLE_BATCH_TOO_LARGE && !report.malformed);
 return 0;
}`, octets(expected), octets(tablewire.Announce(u)), setup.String())
	runCTableWireProbe(t, u, source)
}

func TestCTableAnnouncementDistinctQuantizedShapes(t *testing.T) {
	u := unitFromSource(t, `package probe
table First { rate float32 | min = 0, max = 1, resolution = 0.1 }
table Second { rate float32 | min = 0, max = 1, resolution = 0.1001 }
`)
	var vocabulary tablewire.Vocabulary
	var report tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(u), &report); err != nil {
		t.Fatal(err)
	}
	runCTableWireProbe(t, u, `#include "ProbeTable.h"
#include <stdio.h>
int main(void) {
 TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);
 TableReport report={0};uint8_t bytes[kTableAnnounceBytes];
 if(sizeof(TableMessageEntry)!=96)return 2;
 if(announce(bytes,sizeof(bytes))!=sizeof(bytes))return 1;
 if(!announce_read(&v,bytes,sizeof(bytes),&report)){fprintf(stderr,"own announcement: malformed=%d refused=%d\n",report.malformed,report.refused);return 1;}
	return 0;
}`)
}

func TestCTableMessageGraph(t *testing.T) {
	u := unitFromSource(t, `package probe
table Node { n int32
next *Node
items []int16 }
table Item { samples []uint8 }
union Action { number uint32
blob *bytes
node *Node }
table Root { node *Node
aliases [..3]*Node
blob *bytes
title *string
numbers []uint8
zeroes []int32 | min = 0, max = 0
entries map[int32]Item
action Action }
`)
	text := `{"node":{"&node":1,"n":7,"items":[2,3]},"aliases":[{"&node":1},null],"blob":"AAEC","title":"hi","numbers":[1,2],"zeroes":[0,0],"entries":{"7":{"samples":[4,5]},"2":{"samples":[9]}},"action":{"node":{"n":11}}}`
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(text), &report) || !report.Silent() {
		t.Fatalf("fixture: %+v", report)
	}
	values := []*tabletext.Instance{v, m.New(m.Lookup("Root"))}
	expected, err := tablewire.EncodeMessages(m, values)
	if err != nil {
		t.Fatal(err)
	}
	var bytes strings.Builder
	for _, v := range expected {
		fmt.Fprintf(&bytes, "0x%02x,", v)
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do {if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;}} while(0)
static const uint8_t expected[]={%s};
int main(void){
 RootBuilder a,b;const Root * roots[2],*loaded[2];uint8_t buffer[4096],*region;int64_t n,storage,count,attribution;
 TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary vocabulary=table_vocabulary(entries,kTableMessageEntriesHere);TableReport report={0};TableRefuseReason reason=99;
 CHECK(root_builder_init(&a));CHECK(root_builder_init(&b));CHECK(root_from_json(&a,%q,%d,&report));CHECK(!report.malformed&&!report.clamped&&!report.unknown);
 CHECK(root_builder_root(&b)!=NULL);CHECK(root_builder_lock(&a));CHECK(root_builder_lock(&b));roots[0]=(const Root *)(const void *)a.region;roots[1]=(const Root *)(const void *)b.region;
 n=root_measure_messages(roots,2,&report);CHECK(n==sizeof(expected));CHECK(root_save_messages(roots,2,buffer,n,&report)==n);CHECK(!memcmp(buffer,expected,sizeof(expected)));
 CHECK(announce_read(&vocabulary,kTableAnnounce,kTableAnnounceBytes,&report));
 storage=root_load_measure_messages_ex(&vocabulary,expected,sizeof(expected),&attribution,&reason);CHECK(storage>attribution&&reason==99);
 region=(uint8_t *)malloc((size_t)storage+8);CHECK(region!=NULL);memset(region,0xa5,(size_t)storage+8);
 count=2;CHECK(root_load_messages(loaded,&count,region,storage,&vocabulary,expected,sizeof(expected),&report));CHECK(count==2&&!report.malformed&&!report.refused&&!report.unknown&&!report.clamped&&!report.kind_mismatch);
 CHECK(region[storage]==0xa5);CHECK(node_at(NULL,&loaded[0]->node)==node_at(NULL,&loaded[0]->aliases[0]));
 CHECK(root_save_messages(loaded,2,buffer,n,&report)==n);CHECK(!memcmp(buffer,expected,sizeof(expected)));
 count=1;CHECK(!root_load_messages(loaded,&count,region,storage,&vocabulary,expected,sizeof(expected),&report));CHECK(count==2&&report.reason==SCHEMA_TABLE_BATCH_TOO_LARGE);
 root_builder_shutdown(&a);root_builder_shutdown(&b);free(region);return 0;
}`, bytes.String(), text, len(text))
	runCTableWireProbe(t, u, source)
}

func TestCTableMessageEvolution(t *testing.T) {
	sender := unitFromSource(t, `package probe
type Item { value int8 }
union Payload { number uint8 }
table Root { x int8
y uint8
actual float32
name string(16)
values [..4]uint8
child Item
choice Payload
extra uint32
changed bool }
`)
	receiver := unitFromSource(t, `package probe
type Item { value int32 }
union Payload { number uint32 }
table Root { x int32 = 1000 | min=1000,max=2000
y uint64 | min=0,max=100
actual float64
name string(4)
values [..2]uint32 | min=0,max=100
child Item
choice Payload
changed string(4) }
`)
	sm, rm := tabletext.NewModel(sender), tabletext.NewModel(receiver)
	v := sm.New(sm.Lookup("Root"))
	var report tabletext.Report
	text := []byte(`{"x":-3,"y":200,"actual":1.25,"name":"ééé","values":[4,150,20],"child":{"value":-5},"choice":{"number":255},"extra":7,"changed":true}`)
	if !sm.Read(v, text, &report) || !report.Silent() {
		t.Fatalf("fixture: %+v", report)
	}
	wire, err := tablewire.EncodeMessage(sm, v)
	if err != nil {
		t.Fatal(err)
	}
	announcement := tablewire.Announce(sender)
	var vocabulary tablewire.Vocabulary
	if err := vocabulary.AnnounceRead(announcement, &report); err != nil {
		t.Fatal(err)
	}
	decoded := rm.New(rm.Lookup("Root"))
	if ok, err := tablewire.DecodeMessage(rm, decoded, wire, &vocabulary, &report); err != nil || !ok {
		t.Fatalf("oracle: %v %+v", err, report)
	}
	expected, err := tablewire.Encode(rm, decoded)
	if err != nil {
		t.Fatal(err)
	}
	octets := func(b []byte) string {
		var out strings.Builder
		for _, v := range b {
			fmt.Fprintf(&out, "0x%02x,", v)
		}
		return out.String()
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do{if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;}}while(0)
static const uint8_t announcement[]={%s},wire[]={%s},expected[]={%s};
int main(void){
 Root value;TableMessageEntry entries[128];TableVocabulary v=table_vocabulary(entries,128);TableReport report={0};int64_t count=1,n;uint8_t saved[4096];
 CHECK(announce_read(&v,announcement,sizeof(announcement),&report));CHECK(root_load_messages(&value,&count,&v,wire,sizeof(wire),&report));
 CHECK(!report.malformed&&!report.refused&&report.widened==%d&&report.clamped==%d&&report.kind_mismatch==%d&&report.unknown==%d);
 n=root_save(&value,saved,sizeof(saved));CHECK(n==sizeof(expected));CHECK(!memcmp(saved,expected,sizeof(expected)));return 0;
}`, octets(announcement), octets(wire), octets(expected), report.Widened, report.Clamped, report.KindMismatch, report.Unknown)
	runCTableWireProbe(t, receiver, source)
}

func TestCTableMessageZeroWidthListCapacity(t *testing.T) {
	u := unitFromSource(t, `package probe
 table Root { items []int32 | min=0,max=0 }
 `)
	slot := ir.TableVocabularySlots(u)[ir.TableFieldEntry(u.Tables["Root"].Fields[0]).Key()]
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
 int main(void){
 TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);TableReport report={0};TableRefuseReason reason=99;
 uint8_t wire[64];TableBitWriter w=table_bit_writer(wire,sizeof(wire));
 if(!announce_read(&v,kTableAnnounce,kTableAnnounceBytes,&report))return 1;
 table_bit_put(&w,2,8);table_bit_put(&w,0,8);table_bit_put(&w,%d,kTableMessageRefBitsHere);table_bit_put(&w,UINT32_MAX,32);table_bit_put(&w,0,kTableMessageRefBitsHere);table_bit_align_write(&w);
 if(root_load_measure_messages_ex(&v,wire,w.bits/8,NULL,&reason)!=-1||reason!=SCHEMA_TABLE_REFUSE_COUNT_OVER_LENGTH)return 2;
 if(report.malformed||report.refused)return 3;
 return 0;
 }`, slot))
}

func TestCTableMessagePartialRoot(t *testing.T) {
	u := unitFromSource(t, `package probe
table Node { x uint32 }
table Root { node *Node
value uint64 }
`)
	slot := ir.TableVocabularySlots(u)[ir.TableFieldEntry(u.Tables["Root"].Fields[1]).Key()]
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
 int main(void){
 TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);TableReport report={0};
 uint8_t wire[64],*region;TableBitWriter w=table_bit_writer(wire,sizeof(wire));int64_t need,count=1;const Root * root=NULL;
 if(!announce_read(&v,kTableAnnounce,kTableAnnounceBytes,&report))return 1;
 table_bit_put(&w,2,8);table_bit_put(&w,0,8);table_bit_put(&w,%d,kTableMessageRefBitsHere);table_bit_put(&w,99,32);table_bit_align_write(&w);
 need=root_load_measure_messages(&v,wire,w.bits/8);if(need<=0)return 2;region=(uint8_t *)malloc((size_t)need);if(!region)return 3;
 if(root_load_messages(&root,&count,region,need,&v,wire,w.bits/8,&report)||count!=0||root==NULL||!report.malformed||report.refused||root->value!=0){free(region);return 4;}
 free(region);return 0;
 }`, slot))
}

func TestCTableAnnouncementFailureClearsEntries(t *testing.T) {
	u := unitFromSource(t, "package probe\ntable Root { a int32\nb string(8)\nc bool }\n")
	runCTableWireProbe(t, u, `#include "ProbeTable.h"
 int main(void){uint8_t wire[2048];TableMessageEntry entries[1];TableVocabulary v;TableReport report={0};int64_t n=announce(wire,sizeof(wire));size_t i;
 if(n<=0){return 1;}memset(entries,0xa5,sizeof(entries));v=table_vocabulary(entries,1);
 if(announce_read(&v,wire,n,&report)||!report.refused||report.reason!=SCHEMA_TABLE_VOCABULARY_TOO_LARGE)return 2;
 for(i=0;i<sizeof(entries);i++)if(((uint8_t*)entries)[i])return 3;
 v=table_vocabulary(NULL,1);memset(&report,0,sizeof(report));if(announce_read(&v,wire,n,&report)||report.reason!=SCHEMA_TABLE_NO_VOCABULARY)return 4;
 {TableMessageEntry full[kTableMessageEntriesHere];int64_t at;size_t j;int failures=0;for(at=0;at<n;at++){memset(full,0,sizeof(full));v=table_vocabulary(full,kTableMessageEntriesHere);report=(TableReport){0};wire[at]^=0x80;
 if(!announce_read(&v,wire,n,&report)){failures++;for(j=0;j<sizeof(full);j++)if(((uint8_t *)full)[j])return 5;}wire[at]^=0x80;}if(!failures)return 6;}return 0;
 }`)
}

// A sizing scan follows announced framing even when a reserved transport id
// occurs in a root body. Typed decoding still rejects that body as damage.
// This is schema#749: C LoadMeasure used to stop at the reserved id (192)
// while C++ / the oracle sized the batch (960).
func TestCTableMessageMeasureReservedFraming(t *testing.T) {
	paths, err := filepath.Glob("../tables/pointers/*.schema")
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	runCTableWireProbe(t, u, `#include "GraphTable.h"
#include <stdio.h>
static const uint8_t wire[]={
 0x02,0x0c,0x01,0x00,0xa5,0x01,0x00,0x00,0x00,0xeb,0x04,0x00,0x00,0x00,0x28,0x01,
 0x61,0xcf,0x00,0xd6,0x11,0x00,0x00,0x00,0x10,0x66,0x70,0x00,0x74,0x6f,0x70,0x21,
 0x44,0x03,0x33,0x48,0x6c,0x65,0x66,0x74,0xc0,0x0c,0x16,0x72,0x69,0x67,0x68,0x74,
 0x00,0xcc,0xad,0x1b,0x6d,0x69,0x64,0x00,0x45,0x00,0x74,0x72,0x65,0x65,0x95,0xc0,
 0x90,0xc5,0xeb,0x00
};
int main(void){
 TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);
 TableReport report={0};const Scene * roots[256]={0};int64_t count=256,need;uint8_t * region;
 if(!announce_read(&v,kTableAnnounce,kTableAnnounceBytes,&report))return 1;
 need=scene_load_measure_messages(&v,wire,sizeof(wire));
 if(need!=960){fprintf(stderr,"reserved framing extent %lld want 960\n",(long long)need);return 2;}
 region=(uint8_t *)malloc((size_t)need);if(!region)return 3;
 if(scene_load_messages(roots,&count,region,need,&v,wire,sizeof(wire),&report)||!report.malformed){free(region);return 4;}
 free(region);return 0;
}
`)
}
