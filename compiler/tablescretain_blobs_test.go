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

// TestCTableRetainBlobNamedOnce pins the C retaining load of a blob root to
// the same verdict as the plain load and the engine (docs/SPEC-TABLES.md
// §2.5, §6.6): one blob record named from two slots refuses both reads, and a
// clean Catalog wire is accepted by both.
func TestCTableRetainBlobNamedOnce(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("..", "tables", "blobs", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no schema under ../tables/blobs: %v", err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/blobs: %v", err)
	}

	files := []struct {
		name string
		path string
	}{
		{"blob_shared", filepath.Join("..", "testdata", "wire", "tables", "blob_shared.bin")},
		{"retain_blob_shared_refused", filepath.Join("..", "testdata", "wire", "tables", "fuzz-vectors", "retain_blob_shared_refused.bin")},
		{"clean", filepath.Join("..", "testdata", "wire", "tables", "blob_small.bin")},
	}

	m := tabletext.NewModel(u)
	catalog := m.Lookup("Catalog")
	wires := make([][]byte, len(files))
	expected := make([]bool, len(files))
	for i, file := range files {
		wire, err := os.ReadFile(file.path)
		if err != nil {
			t.Fatal(err)
		}
		wires[i] = wire

		var plainReport tabletext.Report
		plain, err := tablewire.Decode(m, m.New(catalog), wire, &plainReport)
		if err != nil && !tablewire.ReadRefusal(err) {
			t.Fatalf("%s: plain decode: %v", file.name, err)
		}
		plainAccepted := plain && !plainReport.Refused

		var retainReport tabletext.Report
		store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
		retain, err := tablewire.DecodeRetain(m, m.New(catalog), wire, store, &retainReport)
		if err != nil && !tablewire.ReadRefusal(err) {
			t.Fatalf("%s: retaining decode: %v", file.name, err)
		}
		retainAccepted := retain && !retainReport.Refused

		if plainAccepted != retainAccepted {
			t.Fatalf("%s: engine verdict moved between plain (%v) and retaining (%v) reads", file.name, plainAccepted, retainAccepted)
		}
		expected[i] = plainAccepted
	}

	var decls, checks strings.Builder
	for i, file := range files {
		fmt.Fprintf(&decls, "static const uint8_t %s[]={%s};\n", file.name, cOctets(wires[i]))
		fmt.Fprintf(&checks, " CHECK(load_ok(%s,sizeof(%s))==%d);\n", file.name, file.name, boolInt(expected[i]))
		fmt.Fprintf(&checks, " CHECK(retain_ok(%s,sizeof(%s))==%d);\n", file.name, file.name, boolInt(expected[i]))
	}

	runCTableWireProbe(t, u, fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "AssetsTable.h"
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
%s
static int load_ok(const uint8_t * wire,int64_t wire_bytes)
{
 TableReport report={0}; const Catalog * root; uint8_t * region; int64_t need=catalog_load_measure(wire,wire_bytes);
 if(need<=0) return -1;
 region=(uint8_t *)malloc((size_t)need); if(region==NULL) return -2;
 root=catalog_load(region,need,wire,wire_bytes,&report);
 free(region);
 return root!=NULL && !report.refused;
}
static int retain_ok(const uint8_t * wire,int64_t wire_bytes)
{
 TableReport report={0}; TableRetain retain={0}; TableRetainId ids[64]; uint8_t store[4096];
 const Catalog * root; uint8_t * region; int64_t need=catalog_load_measure(wire,wire_bytes);
 if(need<=0) return -1;
 region=(uint8_t *)malloc((size_t)need); if(region==NULL) return -2;
 retain.bytes=store; retain.capacity=sizeof(store); retain.ids=ids; retain.id_capacity=64;
 root=catalog_load_retain(region,need,wire,wire_bytes,&retain,&report);
 free(region);
 return root!=NULL && !report.refused;
}
int main(void) {
%s
 return 0;
}
`, decls.String(), checks.String()))
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
