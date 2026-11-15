package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCTableNodeTypeReservedIdRefused holds the C leg's strict read to the
// rule that a node record's TYPE reference may not resolve to one of the three
// ids the language holds back (docs/SPEC-TABLES.md §3.1, §3.3, §5): the record
// is malformed and the whole load refuses. The wires are the held framing
// vectors the engine's own gate reads; node_chain_two is the control that a
// lawful node table still reads.
func TestCTableNodeTypeReservedIdRefused(t *testing.T) {
	t.Parallel()
	paths, err := GatherPaths([]string{filepath.Join("..", "tables", "pointers")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		root     string
		accepted bool
	}{
		{name: "node_type_ref_zero", root: "Scene"},
		{name: "node_type_node_table_id", root: "Scene"},
		{name: "node_type_build_version_id", root: "Scene"},
		{name: "node_type_vocabulary_id", root: "Scene"},
		{name: "node_chain_two", root: "ListNode", accepted: true},
	} {
		wire, err := os.ReadFile(filepath.Join("..", "testdata", "wire", "tables", "framing", tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		var octets strings.Builder
		for _, b := range wire {
			fmt.Fprintf(&octets, "0x%02x,", b)
		}
		load := "scene_load"
		typ := "Scene"
		verdict := "value == NULL && report.refused"
		if tc.accepted {
			load = "list_node_load"
			typ = "ListNode"
			verdict = "value != NULL && !report.refused"
		}
		source := fmt.Sprintf(`#include "GraphTable.h"
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
static uint64_t region_words[1 << 17];
static const uint8_t wire_%[1]s[] = { %[2]s };
int main(void)
{
    uint8_t * region = (uint8_t *)(void *)(((uintptr_t)region_words + 15u) & ~(uintptr_t)15u);
    int64_t region_bytes = (int64_t)(sizeof(region_words) - 16);
    TableReport report;
    const %[3]s * value;
    memset(&report, 0, sizeof(report));
    value = %[4]s(region, region_bytes, wire_%[1]s, (int64_t)sizeof(wire_%[1]s), &report);
    CHECK(%[5]s);
    return 0;
}
`, tc.name, octets.String(), typ, load, verdict)
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runCTableWireProbe(t, u, source)
		})
	}
}
