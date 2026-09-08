package compiler

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCTableRetainFile(t *testing.T) {
	const old = `package probe
enum Key { A
B }
table Leaf { x uint32 }
table Many { items [..2]Leaf }
union Choice { leaf Leaf
many Many }
table Node { leaf Leaf
next *Node }
table Root { node *Node
leaf Leaf
list []Leaf
array [..3]Leaf
slots [Key]Leaf
choice Choice
entries map[int32]Leaf }
`
	future := strings.Replace(old, "x uint32", "x uint32\nfuture uint64", 1)
	future = strings.Replace(future, "entries map[int32]Leaf", "entries map[int32]Leaf\nextra string(32)", 1)
	newer := unitFromSource(t, future)
	m := tabletext.NewModel(newer)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	fixture := `{"node":{"leaf":{"future":31}},"leaf":{"future":32},"list":[{"future":33},{"future":34}],"array":[{"future":35},{"future":36}],"slots":{"A":{"future":37},"B":{"future":38}},"choice":{"many":{"items":[{"future":39},{"future":40}]}},"entries":{"2":{"future":41},"7":{"future":42}},"extra":"kept"}`
	if !m.Read(value, []byte(fixture), &report) || !report.Silent() {
		t.Fatalf("fixture: %+v", report)
	}
	wire, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatal(err)
	}
	message, err := tablewire.EncodeMessages(m, []*tabletext.Instance{value, value})
	if err != nil {
		t.Fatal(err)
	}
	var announcement, batch strings.Builder
	for _, b := range tablewire.Announce(newer) {
		fmt.Fprintf(&announcement, "0x%02x,", b)
	}
	for _, b := range message {
		fmt.Fprintf(&batch, "0x%02x,", b)
	}
	var octets strings.Builder
	for _, b := range wire {
		fmt.Fprintf(&octets, "0x%02x,", b)
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;} } while(0)
static const uint8_t wire[]={%s},announcement[]={%s},batch[]={%s};
int main(void){
 uint8_t storage[8192],out[8192];TableRetainId ids[128];TableRetain keep={0};TableReport report={0};const Root * root;int64_t bytes,need,cap;uint8_t * region;
 keep.bytes=storage;keep.capacity=sizeof(storage);keep.ids=ids;keep.id_capacity=128;
 need=root_load_measure(wire,sizeof(wire));CHECK(need>0);region=(uint8_t *)malloc((size_t)need);CHECK(region);
 root=root_load_retain(region,need,wire,sizeof(wire),&keep,&report);CHECK(root);CHECK(!report.malformed&&!report.refused&&report.unknown==13&&report.retained==13&&!report.retain_lost);CHECK(keep.count==13&&keep.id_used==0);
 bytes=root_measure_retain(root,&keep);CHECK(bytes==(int64_t)sizeof(wire));CHECK(root_save_retain(root,&keep,out,bytes,&report)==bytes);CHECK(!report.retain_lost);CHECK(!memcmp(wire,out,(size_t)bytes));
 CHECK(root_save_retain(root,&keep,out,bytes,NULL)==-1);
 for(cap=0;cap<bytes;cap++){memset(out,0xa5,sizeof(out));CHECK(root_save_retain(root,&keep,out,cap,&report)==-1);CHECK(out[cap]==0xa5);}
 keep.id_capacity=0;report=(TableReport){0};bytes=root_measure_retain(root,&keep);CHECK(bytes>0);CHECK(root_save_retain(root,&keep,out,bytes,&report)==bytes);CHECK(report.retain_lost==13&&keep.id_used==0);
 keep.id_capacity=128;keep.capacity=0;report=(TableReport){0};root=root_load_retain(region,need,wire,sizeof(wire),&keep,&report);CHECK(root&&report.retained==0&&report.retain_lost==13&&keep.count==0);
 keep.capacity=sizeof(storage);report=(TableReport){0};root=root_load_retain(region,need,wire,sizeof(wire),&keep,&report);CHECK(root&&report.retained==13);CHECK(root_save(NULL,root,out,sizeof(out))>0);
 free(region);
 {TableMessageEntry entries[128];TableVocabulary vocabulary=table_vocabulary(entries,128);TableRetain retains[2]={0};TableRetainId names[2][128];uint8_t stores[2][8192];const Root * roots[2];int64_t count=2;int i;
 report=(TableReport){0};CHECK(announce_read(&vocabulary,announcement,sizeof(announcement),&report));need=root_load_measure_messages(&vocabulary,batch,sizeof(batch));CHECK(need>0);region=(uint8_t *)malloc((size_t)need);CHECK(region);
 for(i=0;i<2;i++){retains[i].bytes=stores[i];retains[i].capacity=sizeof(stores[i]);retains[i].ids=names[i];retains[i].id_capacity=128;}
 CHECK(root_load_retain_messages(roots,&count,region,need,&vocabulary,batch,sizeof(batch),retains,&report));CHECK(count==2);CHECK(!report.malformed&&!report.refused&&report.retained==26&&!report.retain_lost);
 for(i=0;i<2;i++){bytes=root_measure_retain(roots[i],retains+i);CHECK(bytes==sizeof(wire));CHECK(root_save_retain(roots[i],retains+i,out,bytes,&report)==bytes);CHECK(!report.retain_lost&&!memcmp(wire,out,(size_t)bytes));}
 free(region);}
 return 0;
}
`, octets.String(), announcement.String(), batch.String())
	runCTableWireProbe(t, unitFromSource(t, old), source)
}

func TestCTableRetainCapacityBoundsZeroBitMessage(t *testing.T) {
	old := unitFromSource(t, "package probe\ntable Root { next *Root }\n")
	future := unitFromSource(t, "package probe\ntable Root { next *Root\nzeroes []int32 | min = 0, max = 0 }\n")
	var a strings.Builder
	for _, b := range tablewire.Announce(future) {
		fmt.Fprintf(&a, "0x%02x,", b)
	}
	runCTableWireProbe(t, old, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do {if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;}} while(0)
static const uint8_t announcement[]={%s};
int main(void){TableMessageEntry entries[64];TableVocabulary vocabulary=table_vocabulary(entries,64);TableReport report={0};TableBitWriter w;TableRetain retain={0};TableRetainId ids[16];uint8_t wire[64],store[4097];const Root * roots[1];uint8_t * region;int64_t count=1,need;int i;uint64_t ref=0;
 CHECK(announce_read(&vocabulary,announcement,sizeof(announcement),&report));for(i=0;i<vocabulary.count;i++)if(entries[i].kind==14)ref=(uint64_t)i+1;CHECK(ref);
 w=table_bit_writer(wire,sizeof(wire));table_bit_put(&w,2,8);table_bit_put(&w,0,8);table_bit_put(&w,ref,vocabulary.ref_bits);table_bit_put(&w,UINT32_MAX,32);table_bit_put(&w,0,vocabulary.ref_bits);table_bit_align_write(&w);CHECK(!w.overflow);
 need=root_load_measure_messages(&vocabulary,wire,w.bits/8);CHECK(need>0);region=(uint8_t *)malloc((size_t)need);CHECK(region);memset(store,0xa5,sizeof(store));retain.bytes=store;retain.capacity=sizeof(store)-1;retain.ids=ids;retain.id_capacity=16;
 CHECK(root_load_retain_messages(roots,&count,region,need,&vocabulary,wire,w.bits/8,&retain,&report));CHECK(count==1&&!report.malformed&&!report.refused&&report.unknown==1&&report.retain_lost==1&&report.retained==0);CHECK(retain.used==0&&retain.count==0&&store[sizeof(store)-1]==0xa5);free(region);return 0;
}`, a.String()))
}

func TestCTableRetainFramedDepth(t *testing.T) {
	old := unitFromSource(t, "package probe\ntable Root { next *Root }\n")
	for _, depth := range []int{64, 65} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			var schema, fixture strings.Builder
			schema.WriteString("package probe\ntable Root { next *Root\nextra T0 }\n")
			fixture.WriteString(`{"extra":`)
			for i := range depth {
				if i+1 < depth {
					fmt.Fprintf(&schema, "table T%d { nested T%d }\n", i, i+1)
					fixture.WriteString(`{"nested":`)
				} else {
					fmt.Fprintf(&schema, "table T%d { x uint8 }\n", i)
					fixture.WriteString(`{"x":7}`)
				}
			}
			fixture.WriteString(strings.Repeat("}", depth))
			future := unitFromSource(t, schema.String())
			m := tabletext.NewModel(future)
			value := m.New(m.Lookup("Root"))
			var report tabletext.Report
			if !m.Read(value, []byte(fixture.String()), &report) || !report.Silent() {
				t.Fatalf("fixture: %+v", report)
			}
			wire, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
			batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{value})
			if err != nil {
				t.Fatal(err)
			}
			announcement := tablewire.Announce(future)
			var octets strings.Builder
			for _, b := range wire {
				fmt.Fprintf(&octets, "0x%02x,", b)
			}
			kept := 0
			if depth == 64 {
				kept = 1
			}
			runCTableWireProbe(t, old, fmt.Sprintf(`#include "ProbeTable.h"
static const uint8_t wire[]={%s};
int main(void){TableRetain retain={0};TableRetainId ids[256];TableReport report={0};uint8_t store[8192],out[8192];const Root * root;int64_t need=root_load_measure(wire,sizeof(wire)),bytes;uint8_t * region;if(need<0)return 1;region=(uint8_t *)malloc((size_t)need);if(!region)return 2;retain.bytes=store;retain.capacity=sizeof(store);retain.ids=ids;retain.id_capacity=256;root=root_load_retain(region,need,wire,sizeof(wire),&retain,&report);if(!root||report.malformed||report.refused||report.unknown!=1||report.retained!=%d||report.retain_lost!=%d)return 3;bytes=root_measure_retain(root,&retain);if(bytes<0||root_save_retain(root,&retain,out,sizeof(out),&report)!=bytes)return 4;free(region);return 0;}
`, octets.String(), kept, 1-kept))
			runCTableWireProbe(t, old, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do{if(!(x)){fprintf(stderr,"message depth line %%d: %%s\n",__LINE__,#x);return 1;}}while(0)
static const uint8_t batch[]={%s},announcement[]={%s},wire[]={%s};
int main(void){TableMessageEntry entries[256];TableVocabulary vocabulary=table_vocabulary(entries,256);TableReport report={0};TableRetain retain={0};TableRetainId ids[256];uint8_t store[8192],out[8192];const Root * roots[1];int64_t need,count=1,bytes;uint8_t * region;
CHECK(announce_read(&vocabulary,announcement,sizeof(announcement),&report));need=root_load_measure_messages(&vocabulary,batch,sizeof(batch));CHECK(need>0);region=(uint8_t*)malloc((size_t)need);CHECK(region);retain.bytes=store;retain.capacity=sizeof(store);retain.ids=ids;retain.id_capacity=256;
CHECK(root_load_retain_messages(roots,&count,region,need,&vocabulary,batch,sizeof(batch),&retain,&report));CHECK(count==1&&!report.malformed&&!report.refused&&report.unknown==1&&report.retained==%d&&report.retain_lost==%d);bytes=root_measure_retain(roots[0],&retain);CHECK(bytes>0);CHECK(root_save_retain(roots[0],&retain,out,sizeof(out),&report)==bytes);if(report.retained){CHECK(bytes==sizeof(wire));CHECK(memcmp(wire,out,sizeof(wire))==0);}free(region);return 0;}
`, cppWire(batch), cppWire(announcement), cppWire(wire), kept, 1-kept))

		})
	}
}

func TestCTableRetainRefusedByName(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("requires C compiler")
	}
	u := unitFromSource(t, "package probe\ntable Fixed { x uint32 }\ntable Variable { next *Variable }\n")
	files, err := New().Generate(u, "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, call := range []string{"fixed_load_retain()", "fixed_measure_retain()", "fixed_save_retain()", "fixed_load_retain_messages()", "variable_save_retain_messages()"} {
		t.Run(call, func(t *testing.T) {
			path := filepath.Join(dir, "refusal.c")
			if err := os.WriteFile(path, []byte("#include \"ProbeTable.h\"\nint main(void){"+call+";return 0;}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(cc, "-std=c99", "-fsyntax-only", "-I", dir, path).CombinedOutput()
			if err == nil || !strings.Contains(string(out), "retention_") {
				t.Fatalf("expected named refusal: %v\n%s", err, out)
			}
		})
	}
}
