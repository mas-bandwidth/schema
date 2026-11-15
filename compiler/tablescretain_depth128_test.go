package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestCTableRetainKeepsDepth128 pins docs/SPEC-TABLES.md §6.6 on the C table
// leg: the resolving walk permits §4's 128 framed levels, so a retaining load
// keeps an unknown field nested as deep as the plain read accepts, and the
// retaining save writes the file back byte for byte. A depth the binary bound
// refuses is refused by the retaining load too. The Go engine's verdict on
// each vector is the oracle.
func TestCTableRetainKeepsDepth128(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("..", "tables", "pointers", "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	scene := model.Lookup("Scene")
	framingDir := filepath.Join("..", "testdata", "wire", "tables", "framing")
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(framingDir, name+".bin"))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return data
	}

	reads := []string{"depth_tables_128", "depth_arrays_128", "depth_union_tables_64", "depth_keyed_128"}
	refused := []string{"depth_tables_129", "depth_arrays_129", "depth_union_tables_65", "depth_keyed_129", "depth_tables_5000"}
	for _, name := range reads {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(scene), read(name), &report)
		if !ok || report.Unknown != 1 {
			t.Fatalf("engine %s: ok=%v err=%v unknown=%d, want ok=true unknown=1", name, ok, err, report.Unknown)
		}
	}
	for _, name := range refused {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(scene), read(name), &report)
		if ok {
			t.Fatalf("engine %s: accepted a depth the binary bound refuses (err=%v)", name, err)
		}
	}

	var wires, checks strings.Builder
	for i, name := range reads {
		fmt.Fprintf(&wires, "static const uint8_t read_%d[]={%s};\n", i, cOctets(read(name)))
		fmt.Fprintf(&checks, " CHECK(keeps(read_%d,sizeof(read_%d),%q));\n", i, i, name)
	}
	for i, name := range refused {
		fmt.Fprintf(&wires, "static const uint8_t refused_%d[]={%s};\n", i, cOctets(read(name)))
		fmt.Fprintf(&checks, " CHECK(refuses(refused_%d,sizeof(refused_%d),%q));\n", i, i, name)
	}
	files, err := generateCUnqualified(u)
	if err != nil {
		t.Fatal(err)
	}
	header := ""
	for name, data := range files {
		if strings.HasSuffix(name, ".h") && strings.Contains(string(data), "scene_load_retain(") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no generated header declares scene_load_retain")
	}
	source := fmt.Sprintf(`#include %q
#include <stdio.h>
#include <stdlib.h>
#define CHECK(x) do { if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);failed=1;} } while(0)
#define STORE (1<<20)
#define IDS 1024
%s
static int keeps(const uint8_t * wire,int64_t bytes,const char * name)
{
 TableRetain keep;TableRetainId * ids;TableReport report;const Scene * root;uint8_t * region,* store,* out;int64_t need,saved;int ok=1;
 need=scene_load_measure(wire,bytes);if(need<=0){fprintf(stderr,"%%s: measure refused a readable depth\n",name);return 0;}
 region=(uint8_t *)malloc((size_t)need);store=(uint8_t *)malloc(STORE);out=(uint8_t *)malloc((size_t)bytes+1);ids=(TableRetainId *)malloc(sizeof(TableRetainId)*IDS);
 if(!region||!store||!out||!ids){fprintf(stderr,"%%s: out of memory\n",name);free(region);free(store);free(out);free(ids);return 0;}
 memset(&report,0,sizeof(report));root=scene_load(region,need,wire,bytes,&report);
 if(!root||report.refused){fprintf(stderr,"%%s: plain load refused a readable depth\n",name);ok=0;}
 memset(&keep,0,sizeof(keep));keep.bytes=store;keep.capacity=STORE;keep.ids=ids;keep.id_capacity=IDS;
 memset(&report,0,sizeof(report));root=scene_load_retain(region,need,wire,bytes,&keep,&report);
 if(!root||report.refused){fprintf(stderr,"%%s: retaining load refused a readable depth\n",name);ok=0;}
 else {
  if(report.retain_lost!=0||report.retained!=1){fprintf(stderr,"%%s: retaining load kept %%d and lost %%d unknown fields, want 1 and 0\n",name,(int)report.retained,(int)report.retain_lost);ok=0;}
  saved=scene_measure_retain(root,&keep);
  if(saved!=bytes){fprintf(stderr,"%%s: retaining measure is %%lld bytes, the file is %%lld\n",name,(long long)saved,(long long)bytes);ok=0;}
  else if(scene_save_retain(root,&keep,out,bytes,&report)!=bytes||memcmp(out,wire,(size_t)bytes)!=0){fprintf(stderr,"%%s: retaining save changed the file bytes\n",name);ok=0;}
  if(report.retain_lost!=0){fprintf(stderr,"%%s: retaining save lost an unknown field\n",name);ok=0;}
 }
 free(region);free(store);free(out);free(ids);return ok;
}
static int refuses(const uint8_t * wire,int64_t bytes,const char * name)
{
 TableRetain keep;TableRetainId ids[64];TableReport report;const Scene * root;uint8_t * region,* store;int64_t need;
 need=scene_load_measure(wire,bytes);if(need<=0)return 1;
 region=(uint8_t *)malloc((size_t)need);store=(uint8_t *)malloc(STORE);if(!region||!store){fprintf(stderr,"%%s: out of memory\n",name);free(region);free(store);return 0;}
 memset(&keep,0,sizeof(keep));keep.bytes=store;keep.capacity=STORE;keep.ids=ids;keep.id_capacity=64;
 memset(&report,0,sizeof(report));root=scene_load_retain(region,need,wire,bytes,&keep,&report);free(region);free(store);
 if(root&&!report.refused){fprintf(stderr,"%%s: retaining load accepted a refused depth\n",name);return 0;}
 return 1;
}
int main(void){
 int failed=0;
%s return failed;
}
`, header, wires.String(), checks.String())
	runCTableWireProbe(t, u, source)
}
