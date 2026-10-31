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

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestCTableRetainFile(t *testing.T) {
	const old = `package probe
enum Key { A
B }
fixed table Leaf { x uint32 }
fixed table Many { items [..2]Leaf }
union Choice { leaf Leaf
many Many }
table Node { leaf Leaf
next *Node }
table Root { vertex *Node
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
	fixture := `{"vertex":{"leaf":{"future":31}},"leaf":{"future":32},"list":[{"future":33},{"future":34}],"array":[{"future":35},{"future":36}],"slots":{"A":{"future":37},"B":{"future":38}},"choice":{"many":{"items":[{"future":39},{"future":40}]}},"entries":{"2":{"future":41},"7":{"future":42}},"extra":"kept"}`
	if !m.Read(value, []byte(fixture), &report) || !report.Silent() {
		t.Fatalf("fixture: %+v", report)
	}
	wire, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatal(err)
	}
	var octets strings.Builder
	for _, b := range wire {
		fmt.Fprintf(&octets, "0x%02x,", b)
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;} } while(0)
static const uint8_t wire[]={%s};
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
 return 0;
}
`, octets.String())
	runCTableWireProbe(t, unitFromSource(t, old), source)
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
		})
	}
}

func TestCTableRetainRefusedByName(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("requires C compiler")
	}
	u := unitFromSource(t, "package probe\nfixed table Fixed { x uint32 }\ntable Variable { next *Variable }\n")
	files, err := generateCUnqualified(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, call := range []string{"fixed_load_retain()", "fixed_measure_retain()", "fixed_save_retain()", "fixed_load_retain_messages()", "fixed_save_retain_messages()"} {
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
