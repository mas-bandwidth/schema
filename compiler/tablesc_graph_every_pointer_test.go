package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCTableGraphCheckedOverEveryPointer holds the C strict load and its
// retaining load to the declaration-free graph walk in docs/SPEC-TABLES.md
// §3.1 and §6.5: every kind-17 payload in the input is part of the graph, in
// fields this reader declares and in fields it only skips.
func TestCTableGraphCheckedOverEveryPointer(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("..", "tables", "pointers", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no schemas under ../tables/pointers: %v", err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/pointers: %v", err)
	}
	framing := filepath.Join("..", "testdata", "wire", "tables", "framing")
	cases := []struct {
		name string
		root string
		read bool
	}{
		{"graph_unknown_type_self", "Scene", false},
		{"graph_unknown_type_pair", "Scene", false},
		{"graph_listnode_unknown_self", "Scene", false},
		{"graph_unknown_to_root", "Scene", false},
		{"graph_blob_two_unknown_fields", "Scene", false},
		{"graph_blob_two_unknown_elements", "Scene", false},
		{"graph_string_blob_ill_formed", "Scene", false},
		{"graph_string_blob_zero_byte", "Scene", false},
		{"graph_unknown_type_unreached", "Scene", false},
		{"graph_string_blob_unreached", "Scene", false},
		{"graph_reach_unknown_field", "Scene", true},
		{"graph_reach_unknown_nested", "Scene", true},
		{"graph_reach_unknown_union_arm", "Scene", true},
		{"graph_reach_unknown_keyed_slot", "Scene", true},
		{"node_chain_two", "ListNode", true},
	}
	var decls, checks strings.Builder
	for _, c := range cases {
		wire, err := os.ReadFile(filepath.Join(framing, c.name+".bin"))
		if err != nil {
			t.Fatalf("read %s: %v", c.name, err)
		}
		fmt.Fprintf(&decls, "static const uint8_t w_%s[]={%s};\n", c.name, cOctets(wire))
		prefix := "scene"
		if c.root == "ListNode" {
			prefix = "list_node"
		}
		fmt.Fprintf(&checks, " PLAIN(%s_load_measure,%s_load,w_%s,%d,%q);\n", prefix, prefix, c.name, boolInt(c.read), c.name)
		fmt.Fprintf(&checks, " RETAIN(%s_load_measure,%s_load_retain,w_%s,%d,%q);\n", prefix, prefix, c.name, boolInt(c.read), c.name)
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "GraphTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define CHECK(x) do { if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;} } while(0)
#define PLAIN(measure,load,wire,want,who) do { \
 TableReport report; uint8_t * region; const void * root; int64_t need=(measure)(wire,(int64_t)sizeof(wire)); \
 need=need>0?need:(int64_t)(1<<16); region=(uint8_t *)malloc((size_t)need); if(region==NULL) return 1; \
 memset(&report,0,sizeof(report)); root=(const void *)(load)(region,need,wire,(int64_t)sizeof(wire),&report); \
 free(region); if(((root!=NULL && !report.refused)?1:0)!=(want)) { fprintf(stderr,"%%s: plain load verdict moved from %%d\n",who,(int)(want)); return 1; } \
} while(0)
#define RETAIN(measure,load,wire,want,who) do { \
 TableReport report; TableRetain retain; TableRetainId ids[256]; uint8_t store[1<<16]; uint8_t * region; const void * root; \
 int64_t need=(measure)(wire,(int64_t)sizeof(wire)); need=need>0?need:(int64_t)(1<<16); \
 region=(uint8_t *)malloc((size_t)need); if(region==NULL) return 1; \
 memset(&report,0,sizeof(report)); memset(&retain,0,sizeof(retain)); \
 retain.bytes=store; retain.capacity=sizeof(store); retain.ids=ids; retain.id_capacity=256; \
 root=(const void *)(load)(region,need,wire,(int64_t)sizeof(wire),&retain,&report); free(region); \
 if((root!=NULL && !report.refused)!=(want)) { fprintf(stderr,"%%s: retaining load verdict moved from %%d\n",who,(int)(want)); return 1; } \
 if(root==NULL || report.refused) { CHECK(retain.used==0 && retain.count==0 && retain.id_used==0); } \
} while(0)
%s
int main(void) {
%s
 return 0;
}
`, decls.String(), checks.String()))
}
