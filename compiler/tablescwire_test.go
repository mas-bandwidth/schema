package compiler

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Exercise generated code across the first-use reference-width boundary. The
// expectation is produced by the independent compiler engine, not the emitter.
// Nested frames must measure with the vocabulary already accumulated by their
// parent, and repeated saves must not retain a previous call's vocabulary.
func TestCTableWireReferenceBoundary(t *testing.T) {
	var schema, fill strings.Builder
	schema.WriteString(`package probe

enum Choice {
Current
Second
}
type Leaf { choice Choice }
table Root {
`)
	for i := range 127 {
		fmt.Fprintf(&schema, "field%d uint8\n", i)
		fmt.Fprintf(&fill, "    value.field%d = 1;\n", i)
	}
	schema.WriteString("leaf Leaf | was = \"oldLeaf\"\nleaves [..2]Leaf\nslots [Choice]Leaf\n}\n")
	fill.WriteString("    value.leaf.choice = CHOICE_CURRENT;\n    value.leaves_count = 2;\n    value.leaves[0].choice = CHOICE_SECOND;\n    value.leaves[1].choice = CHOICE_CURRENT;\n    value.slots[0].choice = CHOICE_SECOND;\n")
	u := unitFromSource(t, schema.String())
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	for i := range 127 {
		value.Fields[i].Cell.U = 1
	}
	value.Fields[127].Cell.Tab.Fields[0].Cell.U = 1
	value.Fields[128].Count = 2
	value.Fields[128].Elems[0].Tab.Fields[0].Cell.U = 2
	value.Fields[128].Elems[1].Tab.Fields[0].Cell.U = 1
	value.Fields[129].Elems[0].Tab.Fields[0].Cell.U = 2
	expected, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	if count := binary.LittleEndian.Uint64(expected[len(expected)-8:]); count <= 128 {
		t.Fatalf("test did not cross the identity boundary: %d", count)
	}
	var octets strings.Builder
	for _, b := range expected {
		fmt.Fprintf(&octets, "0x%02x,", b)
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
static const uint8_t expected[] = { %s };
int main(void)
{
    Root value, decoded;
    TableReport report;
    uint8_t saved[sizeof(expected) + 1], damaged[sizeof(expected)];
    int64_t n;
    root_reset(&value);
%s
    n = root_measure(&value);
    CHECK(n == (int64_t)sizeof(expected));
    CHECK(root_save(&value, saved, n) == n);
    CHECK(memcmp(saved, expected, sizeof(expected)) == 0);
    CHECK(root_save(&value, saved, n) == n);
    CHECK(memcmp(saved, expected, sizeof(expected)) == 0);
    saved[n-1] = 0x7d;
    CHECK(root_save(&value, saved, n-1) == -1);
    CHECK(saved[n-1] == 0x7d);
    CHECK(root_save(&value, saved, -1) == -1);
    CHECK(root_save(&value, NULL, n) == -1);
    memset(&report, 0, sizeof(report));
    CHECK(root_load(&decoded, expected, n, &report));
    CHECK(!report.malformed && !report.refused && !report.unknown && !report.kind_mismatch && !report.widened && !report.clamped);
    CHECK(root_save(&decoded, saved, n) == n);
    CHECK(memcmp(saved, expected, sizeof(expected)) == 0);
    CHECK(root_load(&decoded, expected, n, NULL));
    memcpy(damaged, expected, sizeof(expected));
    damaged[0] = 2;
    memset(&report, 0, sizeof(report));
    CHECK(!root_load(&decoded, damaged, n, &report));
    CHECK(report.refused && report.reason == SCHEMA_TABLE_MESSAGE_FORM_AS_FILE && !report.malformed);
    CHECK(root_measure(&decoded) == 10);
    damaged[0] = 99;
    memset(&report, 0, sizeof(report));
    CHECK(!root_load(&decoded, damaged, n, &report));
    CHECK(report.refused && report.reason == SCHEMA_TABLE_NEWER_FORM && !report.malformed);
    memset(&report, 0, sizeof(report));
    CHECK(!root_load(&decoded, NULL, n, &report) && report.malformed && !report.refused);
    return 0;
}
`, octets.String(), fill.String())
	runCTableWireProbe(t, u, source)
}

func runCTableWireProbe(t *testing.T, u *ir.Unit, source string) {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("generated C execution requires cc")
	}
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
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cc, "-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2", "-I", dir, filepath.Join(dir, "main.c"), filepath.Join(dir, "ProbeTable.c"), "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	if output, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("execute: %v\n%s", err, output)
	}
}

// Clamps must happen after promoting the source-width integer. NaN widening
// must preserve its sign and payload without quieting a signaling NaN.
func TestCTableWireWidening(t *testing.T) {
	u := unitFromSource(t, `package probe
    table Root {
        low int32 = 1000 | min = 1000, max = 2000
        high int32 = -1000 | min = -2000, max = -1000
        positive uint64 = 1000 | min = 1000, max = 2000
        real float64
        samples [..2]uint32 | min = 0, max = 2000
    }
    `)
	wire := []byte{1, 1, 2, 255, 2, 2, 0, 3, 6, 255, 4, 10}
	wire = binary.LittleEndian.AppendUint32(wire, 0xffa5a5a5)
	wire = append(wire, 5, 14, 6, 7, 2, 0, 0, 255, 255, 0)
	for _, f := range u.Tables["Root"].Fields {
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f))
	}
	wire = binary.LittleEndian.AppendUint64(wire, 5)
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	var report tabletext.Report
	if ok, err := tablewire.Decode(model, value, wire, &report); err != nil || !ok {
		t.Fatalf("oracle decode: %v %v", ok, err)
	}
	if report.Widened != 5 || report.Clamped != 4 || report.Malformed {
		t.Fatalf("test did not exercise five widenings and four clamps: %+v", report)
	}
	if value.Fields[2].Cell.U != 1000 {
		t.Fatalf("oracle lost the widened clamp: %d", value.Fields[2].Cell.U)
	}
	saved, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	octets := func(data []byte) string {
		var s strings.Builder
		for _, b := range data {
			fmt.Fprintf(&s, "0x%02x,", b)
		}
		return s.String()
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
    #include <stdio.h>
    static const uint8_t wire[] = { %s }, expected[] = { %s };
    int main(void) {
        Root value; TableReport report; uint8_t saved[sizeof(expected)];
        memset(&report, 0, sizeof(report));
        if (!root_load(&value, wire, sizeof(wire), &report)) return 1;
        if (report.widened != 5 || report.clamped != 4 || report.malformed || report.refused) return 2;
        if (root_measure(&value) != (int64_t)sizeof(expected)) { fprintf(stderr, "measure %%lld expected %%zu: low %%d high %%d positive %%llu samples %%d [%%u, %%u]\n", (long long)root_measure(&value), sizeof(expected), value.low, value.high, (unsigned long long)value.positive, value.samples_count, value.samples[0], value.samples[1]); return 3; }
        if (root_save(&value, saved, sizeof(saved)) != (int64_t)sizeof(expected)) return 4;
        return memcmp(saved, expected, sizeof(expected)) != 0;
    }
    `, octets(wire), octets(saved)))
}

// A damaged later occurrence resets text to its declared default; an explicit
// empty value remains an edit and survives a save/load cycle.
func TestCTableWireDefaultRecovery(t *testing.T) {
	u := unitFromSource(t, `package probe
flags Caps { Jump, Crouch }
table Root {
 name string(8) = "new"
 data bytes(4) = "ab"
 caps Caps = { Jump }
}
`)
	wire := []byte{1, 1, 12, 2, 'o', 'k', 1, 12, 1, 0xff, 0}
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(u.Tables["Root"].Fields[0]))
	wire = binary.LittleEndian.AppendUint64(wire, 1)
	var octets strings.Builder
	for _, b := range wire {
		fmt.Fprintf(&octets, "0x%02x,", b)
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
static const uint8_t damaged[] = {%s};
int main(void) {
 Root value, copy; TableReport report; uint8_t out[256]; int64_t n;
 root_reset(&value);
 CHECK(value.name_length == 3 && !memcmp(value.name,"new",4));
 CHECK(value.data_length == 2 && !memcmp(value.data,"ab",2));
 CHECK(value.caps == CAPS_JUMP && root_measure(&value) == 10);
 memset(&report,0,sizeof(report));
 CHECK(root_load(&value,damaged,sizeof(damaged),&report));
 CHECK(report.malformed && value.name_length == 3 && !memcmp(value.name,"new",4));
 value.name_length = 0; value.name[0] = 0; value.data_length = 0; value.caps = 0;
 n=root_save(&value,out,sizeof(out)); CHECK(n > 10);
 memset(&report,0,sizeof(report));
 CHECK(root_load(&copy,out,n,&report));
 CHECK(!report.malformed && copy.name_length == 0 && copy.data_length == 0 && copy.caps == 0);
 return 0;
}
`, octets.String()))
}

// Integer JSON conversion keeps a magnitude unsigned until its domain is
// established. Large unsigned tokens and negative exponents must never pass
// through an out-of-range float-to-integer conversion or wrap to zero.
func TestCTableWireJsonIntegerDomain(t *testing.T) {
	u := unitFromSource(t, `package probe
table Root {
 small uint8
 bounded uint64 | min = 0, max = 18446744073709551615
 debt int64
}
`)
	runCTableWireProbe(t, u, `#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
int main(void) {
 Root value; TableReport report;
 const char * first="{\"small\":18446744073709551615,\"bounded\":18446744073709551615,\"debt\":-9223372036854775808}";
 const char * second="{\"small\":-1e30,\"bounded\":-1e30}";
 memset(&report,0,sizeof(report));
 CHECK(root_from_json(&value,first,(int64_t)strlen(first),&report));
 CHECK(value.small == 255 && value.bounded == UINT64_MAX && value.debt == INT64_MIN);
 CHECK(report.clamped == 1 && !report.kind_mismatch && !report.malformed);
 memset(&report,0,sizeof(report));
 CHECK(root_from_json(&value,second,(int64_t)strlen(second),&report));
 CHECK(value.small == 0 && value.bounded == 0 && report.clamped == 4);
 CHECK(!report.kind_mismatch && !report.malformed);
 return 0;
}
`)
}

// A deep by-value closure must not cause exponential frame remeasurement.
// Only the leaf rides, so checking each ancestor for elision should stop at
// that first field and measuring its frame must walk the payload once.
func TestCTableWireDeepFrames(t *testing.T) {
	var schema, member strings.Builder
	schema.WriteString("package probe\ntable Layer0 { value uint8 }\n")
	for i := 1; i <= 24; i++ {
		fmt.Fprintf(&schema, "table Layer%d { child Layer%d }\n", i, i-1)
		member.WriteString(".child")
	}
	schema.WriteString("table Root { child Layer24 }\n")
	u := unitFromSource(t, schema.String())
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
int main(void) {
 Root value, copy; uint8_t out[1024]; int64_t n; TableReport report;
 root_reset(&value); value.child%s.value=1;
 n=root_measure(&value);
 if(n<=10 || n>1024 || root_save(&value,out,n)!=n) return 1;
 memset(&report,0,sizeof(report));
 if(!root_load(&copy,out,n,&report) || report.malformed || copy.child%s.value!=1) return 2;
 return 0;
}
`, member.String(), member.String()))
}

func TestCTableWireGraphIdentity(t *testing.T) {
	u := unitFromSource(t, `package probe
table Node {
 value uint32
 next *Node
}
table Root {
 first *Node
 slots [..3]*Node
 peers [2]*Node
}
`)
	model := tabletext.NewModel(u)
	root := model.New(u.Tables["Root"])
	first, second := model.New(u.Tables["Node"]), model.New(u.Tables["Node"])
	first.Fields[0].Cell.U = 7
	second.Fields[0].Cell.U = 9
	first.Fields[1].Cell.Node = second
	root.Fields[0].Cell.Node = first
	root.Fields[1].Count = 3
	root.Fields[1].Elems[0].Node = second
	root.Fields[1].Elems[2].Node = first
	root.Fields[2].Elems[0].Node = second
	expected, err := tablewire.Encode(model, root)
	if err != nil {
		t.Fatal(err)
	}
	var octets strings.Builder
	for _, b := range expected {
		fmt.Fprintf(&octets, "0x%02x,", b)
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
static const uint8_t expected[]={%s};
int main(void) {
 RootBuilder builder,copy; Root * root; Node * first; Node * second;
 const Root * locked; const Root * loaded; TableSink sink; TableCtx ctx;
 char text[4096]; int64_t text_bytes;
 uint8_t saved[sizeof(expected)],*region; int64_t extent,attribution; int reason; TableReport report;
 CHECK(root_builder_init(&builder)); root=root_builder_root(&builder);
 ctx.arena=&builder.arena; sink.region=NULL; sink.worker=&builder.main;
 first=node_emplace(&sink,&root->first); CHECK(first!=NULL);first->value=7;
 second=node_emplace(&sink,&first->next); CHECK(second!=NULL);second->value=9;
 root->slots_count=3;root->slots[0]=first->next;root->slots[2]=root->first;root->peers[0]=first->next;
 CHECK(root_measure(&ctx,root)==(int64_t)sizeof(expected));
 CHECK(root_save(&ctx,root,saved,sizeof(saved))==(int64_t)sizeof(expected));
 CHECK(!memcmp(saved,expected,sizeof(saved)));
 second->next=root->first;
 CHECK(root_measure(&ctx,root)==-1 && !root_builder_lock(&builder));
 second->next.value=0;
 CHECK(root_builder_lock(&builder));locked=(const Root *)(const void *)builder.region;
 CHECK(node_at(NULL,&locked->slots[2])==node_at(NULL,&locked->first));
 CHECK(node_at(NULL,&locked->slots[0])==node_at(NULL,&locked->peers[0]));
 CHECK(root_save(NULL,locked,saved,sizeof(saved))==(int64_t)sizeof(expected));
 CHECK(!memcmp(saved,expected,sizeof(saved)));
 extent=root_load_measure_ex(expected,sizeof(expected),&attribution,&reason);
 CHECK(extent>0 && attribution==3*(int64_t)sizeof(TableNodeDirEntry) && reason==0);
 region=(uint8_t *)malloc((size_t)extent);CHECK(region!=NULL);memset(&report,0,sizeof(report));
 loaded=root_load(region,extent,expected,sizeof(expected),&report);
 CHECK(loaded!=NULL && !report.malformed && !report.unknown && !report.kind_mismatch);
 CHECK(node_at(NULL,&loaded->slots[2])==node_at(NULL,&loaded->first));
 CHECK(loaded->slots[1].value==0 && loaded->slots_count==3);
 CHECK(root_builder_init(&copy));CHECK(root_load_builder(&copy,expected,sizeof(expected),&report));
 CHECK(root_builder_lock(&copy));
 CHECK(root_save(NULL,(const Root *)(const void *)copy.region,saved,sizeof(saved))==(int64_t)sizeof(expected));
 CHECK(!memcmp(saved,expected,sizeof(saved)));
 root_builder_shutdown(&copy);
 text_bytes=root_to_json_measure(loaded);CHECK(text_bytes>0 && text_bytes<4096);
 CHECK(root_to_json(loaded,text,sizeof(text))==text_bytes);
 CHECK(root_builder_init(&copy));memset(&report,0,sizeof(report));
 CHECK(root_from_json(&copy,text,text_bytes,&report));CHECK(!report.malformed && !report.kind_mismatch && !report.unknown);
 CHECK(root_builder_lock(&copy));
 CHECK(root_save(NULL,(const Root *)(const void *)copy.region,saved,sizeof(saved))==(int64_t)sizeof(expected));
 CHECK(!memcmp(saved,expected,sizeof(saved)));
 root_builder_shutdown(&copy);root_builder_shutdown(&builder);free(region);return 0;
}
`, octets.String()))
}

func TestCTableWireBlobIdentity(t *testing.T) {
	u := unitFromSource(t, `package probe
table Root {
 data *bytes
 alias *bytes
 text *string
 empty *bytes
}`)
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	blob := &tabletext.Blob{Data: []byte{0, 255, 42}}
	value.Fields[0].Cell.Blob = blob
	value.Fields[1].Cell.Blob = blob
	value.Fields[2].Cell.Blob = &tabletext.Blob{Data: []byte("hello")}
	value.Fields[3].Cell.Blob = &tabletext.Blob{}
	expected, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	var octets strings.Builder
	for _, b := range expected {
		fmt.Fprintf(&octets, "0x%02x,", b)
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
static const uint8_t expected[]={%s};
int main(void) {
 RootBuilder builder,copy; Root * root; const Root * locked; TableCtx ctx;
 uint8_t saved[sizeof(expected)],*data,*region; int64_t extent; TableReport report={0};
 CHECK(root_builder_init(&builder));root=root_builder_root(&builder);ctx.arena=&builder.arena;
 data=table_bytes_emplace(&builder.main,&root->data,3);CHECK(data!=NULL);data[0]=0;data[1]=255;data[2]=42;
 root->alias=root->data;
 CHECK(table_string_emplace(&builder.main,&root->text,"hello",5)!=NULL);
 CHECK(table_bytes_emplace(&builder.main,&root->empty,0)!=NULL);
 CHECK(root_measure(&ctx,root)==(int64_t)sizeof(expected));
 CHECK(root_save(&ctx,root,saved,sizeof(saved))==(int64_t)sizeof(expected));CHECK(!memcmp(saved,expected,sizeof(saved)));
 CHECK(root_builder_lock(&builder));locked=(const Root *)(const void *)builder.region;
 CHECK(table_bytes_at(NULL,&locked->data).data==table_bytes_at(NULL,&locked->alias).data);
 CHECK(table_bytes_at(NULL,&locked->empty).data!=NULL && table_bytes_at(NULL,&locked->empty).length==0);
 CHECK(!strcmp(table_string_at(NULL,&locked->text).data,"hello"));
 CHECK(root_to_json_measure(locked)==-1);
 extent=root_load_measure(expected,sizeof(expected));CHECK(extent>0);region=(uint8_t *)malloc((size_t)extent);CHECK(region!=NULL);
 locked=root_load(region,extent,expected,sizeof(expected),&report);CHECK(locked!=NULL && !report.malformed && !report.unknown);
 CHECK(table_bytes_at(NULL,&locked->data).data==table_bytes_at(NULL,&locked->alias).data);
 CHECK(!strcmp(table_string_at(NULL,&locked->text).data,"hello"));
 CHECK(root_builder_init(&copy));CHECK(root_load_builder(&copy,expected,sizeof(expected),&report));
 CHECK(root_builder_lock(&copy));CHECK(root_save(NULL,(const Root *)(const void *)copy.region,saved,sizeof(saved))==(int64_t)sizeof(expected));
 CHECK(!memcmp(saved,expected,sizeof(saved)));root_builder_shutdown(&copy);root_builder_shutdown(&builder);free(region);
 CHECK(root_builder_init(&builder));root=root_builder_root(&builder);
 data=table_bytes_emplace(&builder.main,&root->data,(int64_t)kTableSegmentSize+17);CHECK(data!=NULL);
 data[0]=31;data[kTableSegmentSize+16]=73;
 CHECK(root_builder_lock(&builder));locked=(const Root *)(const void *)builder.region;
 CHECK(table_bytes_at(NULL,&locked->data).length==(int64_t)kTableSegmentSize+17);
 CHECK(table_bytes_at(NULL,&locked->data).data[0]==31 && table_bytes_at(NULL,&locked->data).data[kTableSegmentSize+16]==73);
 root_builder_shutdown(&builder);return 0;
}
`, octets.String()))
}

// Fail each allocation in a complete authoring round trip. Every path must
// return to its caller and release through the same pair that allocated it.
func TestCTableWireAllocatorFailures(t *testing.T) {
	u := unitFromSource(t, `package probe
table Node { value uint32 }
table Root {
 first *Node
 second *Node
}`)
	runCTableWireProbe(t, u, `#include "ProbeTable.h"
#include <stdio.h>
typedef struct Counts { int calls,fail,live; } Counts;
static void * allocate(void * context,int64_t bytes) {
 Counts * c=(Counts *)context; void * p;
 if(c->calls++==c->fail) return NULL;
 p=calloc(1,(size_t)bytes);if(p!=NULL)c->live++;return p;
}
static void release(void * context,void * p) {
 Counts * c=(Counts *)context;if(p!=NULL)c->live--;free(p);
}
static int exercise(Counts * counts) {
 const char * source="{\"first\":{\"&node\":1,\"value\":9},\"second\":{\"&node\":1}}";
 TableAllocator a={allocate,release,counts}; RootBuilder b,copy; TableReport report={0};
 TableCtx ctx; uint8_t wire[2048]; char text[1024]; int64_t n,m; int ok=0,have_copy=0;
 if(!root_builder_init_with_allocator(&b,a))goto done;
 if(!root_from_json(&b,source,(int64_t)strlen(source),&report))goto done;
 ctx.arena=&b.arena;
 n=root_measure(&ctx,root_builder_root(&b));if(n<=0 || n>2048)goto done;
 if(root_save(&ctx,root_builder_root(&b),wire,n)!=n)goto done;
 if(!root_builder_lock(&b))goto done;
 if(root_measure_with_allocator(NULL,(const Root *)(const void *)b.region,a)!=n)goto done;
 if(root_save_with_allocator(NULL,(const Root *)(const void *)b.region,wire,n,a)!=n)goto done;
 m=root_to_json_measure_with_allocator((const Root *)(const void *)b.region,a);if(m<=0 || m>1024)goto done;
 if(root_to_json_with_allocator((const Root *)(const void *)b.region,text,m,a)!=m)goto done;
 have_copy=1;if(!root_builder_init_with_allocator(&copy,a))goto done;
 if(!root_load_builder(&copy,wire,n,&report) || !root_builder_lock(&copy))goto done;
 if(node_at(NULL,&((const Root *)(const void *)copy.region)->first)!=node_at(NULL,&((const Root *)(const void *)copy.region)->second))goto done;
 ok=1;
 done:
 if(have_copy)root_builder_shutdown(&copy);
 root_builder_shutdown(&b);return ok;
}
int main(void) {
 int fail,complete=0;
 for(fail=0;fail<128;fail++) {
  Counts counts={0,fail,0}; int ok=exercise(&counts);
  if(counts.live!=0) { fprintf(stderr,"allocation %d leaked %d allocations\n",fail,counts.live);return 1; }
  if(counts.calls<=fail) { if(!ok)return 2;complete=1;break; }
  if(ok) { fprintf(stderr,"allocation %d was ignored\n",fail);return 3; }
 }
 return complete?0:4;
}
`)
}
