package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The compiler's canonical writer is independent of the C generator. These
// bytes include compact, differently aligned graph nodes and nested extents.
func TestCTableCookCanonical(t *testing.T) {
	u := unitFromSource(t, `package probe
table Tiny { value uint8 }
table Bucket { numbers []int16 }
table Root {
 flag uint8
 first *Tiny
 second *Tiny
 text string(8)
 values [..2]uint8
 buckets [..2]Bucket
 hidden [..2]*Tiny
}
`)
	m := tabletext.NewModel(u)
	value := m.New(u.Tables["Root"])
	report := tabletext.Report{}
	if !m.Read(value, []byte(`{"flag":7,"first":{"value":3},"second":{"value":5},"text":"hi","values":[8,9],"buckets":[{"numbers":[1,-2,3]}]}`), &report) || !report.Silent() {
		t.Fatalf("input: %+v", report)
	}
	wire, err := tablewire.Encode(m, value)
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
	var arrays strings.Builder
	fmt.Fprintf(&arrays, "static const uint8_t wire[]={%s};\n", octets(wire))
	for _, big := range []bool{false, true} {
		cooked, _, _, err := New().Cook(u, "Root", wire, CookOptions{Big: big})
		if err != nil {
			t.Fatal(err)
		}
		name := "little"
		if big {
			name = "big"
		}
		fmt.Fprintf(&arrays, "static const uint8_t %s[]={%s};\n", name, octets(cooked))
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
	source := `#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
` + arrays.String() + `
int main(void) {
 RootBuilder b; Root * root; TableCtx ctx; TableSink sink; TableReport report={0};
 uint8_t output[sizeof(little)+1],packed[4096]; int64_t need; int i; int16_t * number;
 const Root * opened; const Tiny * first;
 { TableRefuseReason reason=99; uint8_t unknown=2;
   CHECK(root_load_measure_ex(wire,sizeof(wire),NULL,&reason)>0 && reason==99);
   CHECK(root_load_measure_ex(&unknown,1,NULL,&reason)==-1 && reason==SCHEMA_TABLE_REFUSE_UNKNOWN_FORM);
   reason=99;CHECK(root_load_measure_ex(NULL,0,NULL,&reason)==-1 && reason==99);
 }
 CHECK(root_builder_init(&b));CHECK(root_load_builder(&b,wire,sizeof(wire),&report));
 root=root_builder_root(&b);ctx.arena=&b.arena;sink.worker=&b.main;sink.region=NULL;
 /* Padding and unused string tails must not enter a content-addressed cook. */
 ((uint8_t *)(void *)root)[1]=0xa5;
 for(i=root->text_length+1;i<(int)sizeof(root->text);i++) {root->text[i]=(char)0xa5;}
 need=root_cook_measure(&ctx,root);CHECK(need==(int64_t)sizeof(little));
 memset(output,0xa5,sizeof(output));CHECK(!root_cook(&ctx,root,output,need-1,TableByteOrder_Little));
 for(i=0;i<(int)sizeof(output);i++) {CHECK(output[i]==0xa5);}
 CHECK(root_cook(&ctx,root,output,need,TableByteOrder_Little));CHECK(!memcmp(output,little,sizeof(little)));CHECK(output[need]==0xa5);
 CHECK(root_cook(&ctx,root,output,need,TableByteOrder_Big));CHECK(!memcmp(output,big,sizeof(big)));
 CHECK(root_cook(&ctx,root,output,need,table_cook_byte_order==1 ? TableByteOrder_Little : TableByteOrder_Big));
 opened=root_open(output,need);CHECK(opened!=NULL);first=tiny_at(NULL,&opened->first);CHECK(first!=NULL && first->value==3);
 CHECK(root_save(NULL,opened,packed,sizeof(packed))==(int64_t)sizeof(wire));CHECK(!memcmp(packed,wire,sizeof(wire)));
 /* An unreached list must refuse before touching the output. */
 number=bucket_numbers_add(&b.main,&root->buckets[1].numbers);CHECK(number!=NULL);*number=42;
 CHECK(root_cook_measure(&ctx,root)==-1);memset(output,0xa5,sizeof(output));
 CHECK(!root_cook(&ctx,root,output,sizeof(output),TableByteOrder_Little));CHECK(output[0]==0xa5);CHECK(!root_builder_lock(&b));
 CHECK(bucket_numbers_erase(&b.arena,&root->buckets[1].numbers,number));
 CHECK(root_cook_measure(&ctx,root)==need);
 /* A hidden pointer is patched only if its node was independently reached. */
 CHECK(tiny_emplace(&sink,&root->hidden[1])!=NULL);
 CHECK(root_cook_measure(&ctx,root)==need);CHECK(!root_cook(&ctx,root,output,sizeof(output),TableByteOrder_Little));
 CHECK(root_open(output,need)==NULL);CHECK(!root_builder_lock(&b));
 root->hidden[1]=root->first;
 CHECK(root_cook(&ctx,root,output,sizeof(output),table_cook_byte_order==1 ? TableByteOrder_Little : TableByteOrder_Big));
 opened=root_open(output,need);CHECK(opened!=NULL && tiny_at(NULL,&opened->hidden[1])==tiny_at(NULL,&opened->first));
 CHECK(root_builder_lock(&b));opened=(const Root *)(const void *)b.region;
 CHECK(tiny_at(NULL,&opened->hidden[1])==tiny_at(NULL,&opened->first));
 root_builder_shutdown(&b);return 0;
}
`
	issue710CompileRun(t, dir, "cc", ".c", source)
}

// Refusals name the first failed clause. No counter or success path mutates
// the caller's reason, and a const block never exposes writable rows.
func TestCTableCookAndBlockRefusals(t *testing.T) {
	u := unitFromSource(t, `package probe
enum Key { One, Two }
type Row { value int32 }
table Root {
 rows [..2]Row
 keys [Key]int32
}
`)
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
	source := `#include <stdint.h>
#include <stdlib.h>
#include <setjmp.h>
static int allocations,releases;
static jmp_buf fatal;
static void * allocate_hook(int64_t n) { allocations++; return calloc(1,(size_t)n); }
static void release_hook(void * p) { if(p!=NULL) {releases++;} free(p); }
#define schema_allocate(n) allocate_hook(n)
#define schema_release(p) release_hook(p)
#define schema_assert(x) ((void)(x))
#define schema_fatal() longjmp(fatal,1)
#include "ProbeBlock.c"
#include <stdio.h>
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
int main(void) {
 uint8_t space[2048], skewed[2048];
 uint8_t * output=(uint8_t *)(((uintptr_t)space+63)&~(uintptr_t)63);
 uint8_t * skew=(uint8_t *)(((uintptr_t)skewed+63)&~(uintptr_t)63)+1;
 Root root; TableRefuseReason reason=99; int64_t need; int i;
 RootBlockStorage storage; RootBlock block; RootBlockConst read_only; RootCounts counts={0}; TableBlockAllocator pair=table_block_default_allocator();
 TableByteOrder order=table_cook_byte_order==1 ? TableByteOrder_Little : TableByteOrder_Big;
 static const int offsets[]={0,8,16,24,32,40,48,56};
 static const int reasons[]={SCHEMA_TABLE_REFUSE_NOT_A_COOK,SCHEMA_TABLE_REFUSE_WRONG_BUILD_VERSION,SCHEMA_TABLE_REFUSE_NOT_A_COOK,SCHEMA_TABLE_REFUSE_TRUNCATED,SCHEMA_TABLE_REFUSE_TRUNCATED,SCHEMA_TABLE_REFUSE_BAD_ALIGNMENT,SCHEMA_TABLE_REFUSE_RESERVED_NOT_ZERO,SCHEMA_TABLE_REFUSE_RESERVED_NOT_ZERO};
 root_reset(&root);need=root_cook_measure(&root);
 CHECK(root_cook(&root,output,need,order));CHECK(root_open_ex(output,need,&reason)!=NULL && reason==99);
 CHECK(root_open_ex(NULL,need,&reason)==NULL && reason==SCHEMA_TABLE_REFUSE_UNALIGNED_BASE);
 CHECK(root_open_ex(output,63,&reason)==NULL && reason==SCHEMA_TABLE_REFUSE_TRUNCATED);
 for(i=0;i<8;i++) {
  CHECK(root_cook(&root,output,need,order));table_cook_put(output+offsets[i],3,8,order);
  CHECK(root_open_ex(output,need,&reason)==NULL && reason==reasons[i]);
 }
 CHECK(root_cook(&root,output,need,order==TableByteOrder_Little ? TableByteOrder_Big : TableByteOrder_Little));
 CHECK(root_open_ex(output,need,&reason)==NULL && reason==SCHEMA_TABLE_REFUSE_FOREIGN_ORDER);
 CHECK(root_cook(&root,output,need,order));memcpy(skew,output,(size_t)need);
 CHECK(root_open_ex(skew,need,&reason)==NULL && reason==SCHEMA_TABLE_REFUSE_UNALIGNED_BASE);
 table_cook_put(skew,3,8,order);CHECK(root_open_ex(skew,need,&reason)==NULL && reason==SCHEMA_TABLE_REFUSE_NOT_A_COOK);
 CHECK(root_block_storage_create(&storage,&pair));CHECK(allocations==1 && releases==0);
 counts.rows=2;CHECK(root_block_begin(&block,&storage,&counts,NULL));root_rows_span(&block)[1].value=17;
 need=root_block_bytes(&block);reason=99;
 CHECK(root_block_open_const_ex(&read_only,block.base,need,&reason));CHECK(reason==99 && root_block_bytes_const(&read_only)==need);
 CHECK(root_rows_span_const(&read_only)[1].value==17);
 { TableBlockConstRows rows=root_rows_rows_const(&read_only); CHECK(((const Row *)table_block_row_at_const(&rows,1))->value==17); }
 memcpy(skew,block.base,(size_t)need);CHECK(!root_block_open_const_ex(&read_only,skew,need,&reason));CHECK(reason==SCHEMA_TABLE_REFUSE_UNALIGNED_BASE);
 /* The gate must read an unaligned triple bytewise before refusing the base. */
 table_cook_put(skew+offsetof(RootBlockProjection,rows)+12,99,4,order);
 CHECK(!root_block_open_const_ex(&read_only,skew,need,&reason));CHECK(reason==SCHEMA_TABLE_REFUSE_BAD_LAYOUT);
 table_cook_put(skew,3,8,order);CHECK(!root_block_open_const_ex(&read_only,skew,need,&reason));CHECK(reason==SCHEMA_TABLE_REFUSE_NOT_A_COOK);
 CHECK(read_only.base==NULL && read_only.projection==NULL && read_only.bytes==0);
 CHECK(!root_block_open_ex(&block,NULL,0,&reason));CHECK(reason==SCHEMA_TABLE_REFUSE_UNALIGNED_BASE);
 root_block_storage_destroy(&storage);CHECK(allocations==1 && releases==1);
 if(setjmp(fatal)==0) { SCHEMA_TABLE_KEYED_AT(root.keys,0,sizeof(root.keys)/sizeof(root.keys[0]))=1; return 1; }
 return 0;
}
`
	issue710CompileRun(t, dir, "cc", ".c", source)
	bad := `#include "ProbeBlock.h"
void mutate(const RootBlockConst * block) { root_rows_span_const(block)[0].value=9; }
`
	path := filepath.Join(dir, "const-negative.c")
	if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("cc", "-std=c99", "-I", dir, "-c", path, "-o", filepath.Join(dir, "const-negative.o")).CombinedOutput()
	if err == nil || (!strings.Contains(string(output), "const") && !strings.Contains(string(output), "read-only")) {
		t.Fatalf("const block write did not fail for constness: %v\n%s", err, output)
	}

}
