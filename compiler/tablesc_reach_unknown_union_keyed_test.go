package compiler

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The C reach walk counts every kind-17 payload in the input (docs/SPEC-TABLES.md
// §3.1, §6.5), in fields this reader names and in fields it does not. A record
// of a type this reader cannot name therefore reads null and counts unknown
// wherever an unknown field reaches it: through a plain field, a nested table,
// a union arm, or an enum-keyed slot. The C load and the C retaining load answer
// the same verdict, and a record no pointer reaches refuses. The wire scripts
// are the held graph vectors' (§3.1, §6.5).
func TestCTableReachThroughUnknownUnionAndKeyed(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("..", "tables", "pointers", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no schema under ../tables/pointers: %v", err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/pointers: %v", err)
	}

	ids := []string{
		"head", "tree", "alias", "node-table", "type:ListNode", "value",
		"next", "type:TreeNode", "left", "right", "mystery", "mystery2",
		"graphUnknown", "string", "bytes", "mystery3", "armystery", "key",
	}
	entries := make([]uint64, len(ids))
	for i, name := range ids {
		switch {
		case name == "node-table":
			entries[i] = ir.TableNodeWireId
		case strings.HasPrefix(name, "type:"):
			st := u.Tables[name[len("type:"):]]
			if st == nil {
				t.Fatalf("no table named %q", name[len("type:"):])
			}
			entries[i] = ir.TableTypeId(st.WireName())
		default:
			entries[i] = ir.TableWireId(name)
		}
	}
	file := func(body []byte) []byte {
		out := append([]byte{ir.TableWireForm}, body...)
		for _, id := range entries {
			out = binary.LittleEndian.AppendUint64(out, id)
		}
		return binary.LittleEndian.AppendUint64(out, uint64(len(entries)))
	}
	record := func(typ byte, body []byte) []byte {
		return reachCat(reachB(typ), reachLeb(uint64(len(body))), body)
	}
	nodeTable := func(count uint64, records ...[]byte) []byte {
		payload := reachLeb(count)
		for _, r := range records {
			payload = append(payload, r...)
		}
		return reachCat(reachB(4, ir.TableKindString), reachLeb(uint64(len(payload))), payload)
	}
	unknown := record(13, reachB(0))

	cases := []struct {
		name   string
		wire   []byte
		accept bool
	}{
		{"graph_reach_unknown_field", file(reachCat(reachB(11, 17, 2), nodeTable(1, unknown), reachB(0))), true},
		{"graph_reach_unknown_nested", file(reachCat(reachB(11, 13, 4, 7, 17, 2, 0), nodeTable(1, unknown), reachB(0))), true},
		{"graph_reach_unknown_union_arm", file(reachCat(reachB(11, 15, 17, 17, 1, 2), nodeTable(1, unknown), reachB(0))), true},
		{"graph_reach_unknown_keyed_slot", file(reachCat(reachB(11, 16, 5, 17, 1, 18, 1, 2), nodeTable(1, unknown), reachB(0))), true},
		{"graph_unknown_type_unreached", file(reachCat(nodeTable(1, unknown), reachB(0))), false},
	}

	var wires, checks strings.Builder
	for i, c := range cases {
		fmt.Fprintf(&wires, "static const uint8_t w%d[]={%s};\n", i, cOctets(c.wire))
		fmt.Fprintf(&checks, " CHECK(plain_ok(w%d,sizeof(w%d))==%d);\n", i, i, boolInt(c.accept))
		fmt.Fprintf(&checks, " CHECK(retain_ok(w%d,sizeof(w%d))==%d);\n", i, i, boolInt(c.accept))
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "GraphTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static int failures;
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); failures++; } } while(0)
%s
static int plain_ok(const uint8_t * wire,int64_t wire_bytes)
{
 TableReport report={0}; const Scene * root; uint8_t * region;
 int64_t need=scene_load_measure(wire,wire_bytes);
 if(need<=0) return -1;
 region=(uint8_t *)malloc((size_t)need); if(region==NULL) return -2;
 root=scene_load(region,need,wire,wire_bytes,&report);
 free(region);
 return root!=NULL && !report.refused;
}
static int retain_ok(const uint8_t * wire,int64_t wire_bytes)
{
 TableReport report={0}; TableRetain retain={0}; TableRetainId ids[64]; uint8_t store[8192];
 const Scene * root; uint8_t * region;
 int64_t need=scene_load_measure(wire,wire_bytes);
 if(need<=0) return -1;
 region=(uint8_t *)malloc((size_t)need); if(region==NULL) return -2;
 retain.bytes=store; retain.capacity=sizeof(store); retain.ids=ids; retain.id_capacity=64;
 root=scene_load_retain(region,need,wire,wire_bytes,&retain,&report);
 free(region);
 return root!=NULL && !report.refused;
}
int main(void) {
%s
 return failures != 0;
}
`, wires.String(), checks.String()))
}

func reachB(bytes ...byte) []byte { return bytes }

func reachCat(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func reachLeb(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}
