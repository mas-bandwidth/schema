package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCTableWireBlobSharedRefused pins a shared blob on the C file wire
// (docs/SPEC-TABLES.md §2.5): every writer refuses it, the tolerant read keeps
// the one node and counts the extra naming, and the strict reason path refuses
// it by name. The one-blob-named-once value beside it is accepted, so the
// refusal is the sharing and not the blob field.
func TestCTableWireBlobSharedRefused(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("..", "tables", "blobs", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no schema under ../tables/blobs: %v", err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/blobs: %v", err)
	}
	wire, err := os.ReadFile(filepath.Join("..", "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	var octets strings.Builder
	for _, b := range wire {
		fmt.Fprintf(&octets, "0x%02x,", b)
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
/* A counting allocator overrides schema_allocate so the strict fail-closed
   branches can be witnessed: set nova_alloc_fail_at to the call number (1-based)
   that must fail; calloc otherwise. */
static int nova_alloc_count = 0;
static int nova_alloc_fail_at = -1;
#undef schema_allocate
#define schema_allocate(bytes) (++nova_alloc_count==nova_alloc_fail_at ? NULL : calloc(1,(size_t)(bytes)))
#include "AssetsTable.h"
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
static const uint8_t shared[]={%s};
typedef struct Counts { int calls,fail,live; } Counts;
static void * allocate(void * context,int64_t bytes) {
 Counts * c=(Counts *)context; void * p;
 if(c->calls++==c->fail) return NULL;
 p=calloc(1,(size_t)bytes);if(p!=NULL)c->live++;return p;
}
static void release(void * context,void * p) {
 Counts * c=(Counts *)context;if(p!=NULL)c->live--;free(p);
}
int main(void) {
 CatalogBuilder builder,once,back; Catalog * root; const Catalog * loaded; TableCtx ctx,backctx;
 TableReport report={0}; TableRefuseReason reason=SCHEMA_TABLE_REFUSE_OK;
 uint8_t * data, * region, saved[4096]; int64_t total;

 /* THE WRITER refuses a graph whose two slots name one blob. */
 CHECK(catalog_builder_init(&builder)); root=catalog_builder_root(&builder); ctx.arena=&builder.arena;
 data=table_bytes_emplace(&builder.main,&root->thumb,4); CHECK(data!=NULL);
 data[0]=0x10;data[1]=0x20;data[2]=0x30;data[3]=0x40;
 root->alias=root->thumb;
 CHECK(catalog_measure(&ctx,root)==-1);
 CHECK(catalog_save(&ctx,root,saved,sizeof(saved))==-1);
 catalog_builder_shutdown(&builder);

 /* THE TOLERANT READ keeps the one node and counts the extra naming. */
 total=catalog_load_measure(shared,sizeof(shared)); CHECK(total>0);
 region=(uint8_t *)malloc((size_t)total); CHECK(region!=NULL);
 memset(&report,0,sizeof(report));
 loaded=catalog_load_tolerant(region,total,shared,sizeof(shared),&report);
 CHECK(loaded!=NULL && !report.malformed && !report.refused);
 CHECK(report.blob_shared==1);
 CHECK(table_bytes_at(NULL,&loaded->thumb).data==table_bytes_at(NULL,&loaded->alias).data);
 CHECK(table_bytes_at(NULL,&loaded->thumb).length==4 && table_bytes_at(NULL,&loaded->thumb).data[0]==0x10);
 free(region);

 /* THE STRICT REASON PATH refuses it by name. */
 memset(&reason,0,sizeof(reason));
 CHECK(catalog_load_measure_ex(shared,sizeof(shared),NULL,&reason)==-1);
 CHECK(reason==SCHEMA_TABLE_REFUSE_BLOB_SHARED);

 /* ONE BLOB NAMED ONCE is accepted, so the refusal is the sharing. */
 CHECK(catalog_builder_init(&once)); root=catalog_builder_root(&once); ctx.arena=&once.arena;
 data=table_bytes_emplace(&once.main,&root->thumb,4); CHECK(data!=NULL);
 data[0]=0x10;data[1]=0x20;data[2]=0x30;data[3]=0x40;
 total=catalog_measure(&ctx,root); CHECK(total>0);
 CHECK(catalog_save(&ctx,root,saved,sizeof(saved))==total);
 catalog_builder_shutdown(&once);
 CHECK(catalog_builder_init(&back)); memset(&report,0,sizeof(report));
 CHECK(catalog_load_builder_tolerant(&back,saved,total,&report));
 CHECK(!report.malformed && report.blob_shared==0);
  root=catalog_builder_root(&back); backctx.arena=&back.arena;
  CHECK(table_bytes_at(&backctx,&root->thumb).length==4);
  catalog_builder_shutdown(&back);

  /* THE ALLOCATOR-FAILURE WITNESS: fail the tolerant reader's blob_named side
     array and the read must fail CLOSED (malformed), not silently accept the
     shared blob as unshared. */

  { Counts c; TableAllocator fa={allocate,release,&c}; CatalogBuilder fb; TableReport freport={0};
    c.calls=0; c.fail=1000; c.live=0;
    CHECK(catalog_builder_init_with_allocator(&fb,fa));
    c.calls=0; c.fail=1;
    CHECK(!catalog_load_builder_tolerant(&fb,shared,sizeof(shared),&freport));
    CHECK(freport.malformed);
    catalog_builder_shutdown(&fb);
  }

  /* THE STRICT FAIL-CLOSED WITNESSES: the strict reason path's own region
     allocation (wire_blob_shared) and its decoder's blob_named side array
     (catalog_load_tolerant) each refuse closed on failure — catalog_load_measure_ex
     returns -1 naming blob_shared — rather than silently accepting the wire. */
  { int i;
    for (i=1;i<=2;i++) {
      nova_alloc_count=0; nova_alloc_fail_at=i;
      memset(&reason,0,sizeof(reason));
      CHECK(catalog_load_measure_ex(shared,sizeof(shared),NULL,&reason)==-1);
      CHECK(reason==SCHEMA_TABLE_REFUSE_BLOB_SHARED);
    }
    nova_alloc_fail_at=-1;
  }
  return 0;
}
`, octets.String()))
}
