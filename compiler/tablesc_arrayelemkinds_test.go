package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCTableArrayElementKindsClosed holds the C leg's strict read to the
// closed element-kind list for array and enum-keyed bodies
// (docs/SPEC-TABLES.md §3, §4). It visits every held array_elem framing vector
// and keeps the two kind-17 records as read controls.
func TestCTableArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	paths, err := GatherPaths([]string{filepath.Join("..", "tables", "pointers")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"array_elem_kind_32_count_2": false,
		"array_elem_kind_31_count_1": false,
		"array_elem_kind_16_count_1": false,
		"array_elem_unassigned_count_0": false,
		"array_elem_reserved_count_0": false,
		"array_elem_zero_count_0": false,
		"array_elem_kind_17_count_0": true,
		"array_elem_kind_17_count_1": true,
		"array_elem_kind_mismatch": false,
		"array_elem_terminator_early": false,
		"array_elem_L_past_body": false,
	}
	wires, err := filepath.Glob(filepath.Join("..", "testdata", "wire", "tables", "framing", "array_elem_*.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(wires) != len(want) {
		t.Fatalf("found %d array_elem vectors, want %d", len(wires), len(want))
	}
	for _, wirePath := range wires {
		name := strings.TrimSuffix(filepath.Base(wirePath), filepath.Ext(wirePath))
		accepted, ok := want[name]
		if !ok {
			t.Errorf("unclassified array_elem vector %q", name)
			continue
		}
		delete(want, name)
		wire, err := os.ReadFile(wirePath)
		if err != nil {
			t.Fatal(err)
		}
		var octets strings.Builder
		for _, b := range wire {
			fmt.Fprintf(&octets, "0x%02x,", b)
		}
		verdict := "value == NULL && report.refused"
		if accepted {
			verdict = "value != NULL && !report.refused"
		}
		source := fmt.Sprintf(`#include "GraphTable.h"
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
static uint64_t region_words[1 << 17];
static const uint8_t wire[] = { %s };
int main(void)
{
    uint8_t * region = (uint8_t *)(void *)(((uintptr_t)region_words + 15u) & ~(uintptr_t)15u);
    int64_t region_bytes = (int64_t)(sizeof(region_words) - 16);
    TableReport report;
    const Scene * value;
    memset(&report, 0, sizeof(report));
    value = scene_load(region, region_bytes, wire, (int64_t)sizeof(wire), &report);
    CHECK(%s);
    return 0;
}
`, octets.String(), verdict)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runCTableWireProbe(t, u, source)
		})
	}
	for name := range want {
		t.Errorf("missing array_elem vector %q", name)
	}
}
