package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCTableArrayElementKindsClosed holds the C leg's strict read to the
// closed element-kind list for an array and an enum-keyed body
// (docs/SPEC-TABLES.md §3, §4): an array body (kind 14) may carry 1–13, 15,
// 17, 18–30 and a keyed body (kind 16) 1–11, 13, 18–30. Any other element
// kind refuses under any count, a count of zero included, in a field the
// reader cannot name as in one it declares. The wires are the held framing
// vectors the engine's own gate reads; the two kind-17 rows are the read
// controls.
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
	for _, tc := range []struct {
		name     string
		accepted bool
	}{
		{name: "array_elem_kind_32_count_2"},
		{name: "array_elem_kind_31_count_1"},
		{name: "array_elem_kind_16_count_1"},
		{name: "array_elem_unassigned_count_0"},
		{name: "array_elem_reserved_count_0"},
		{name: "array_elem_zero_count_0"},
		{name: "array_elem_kind_17_count_0", accepted: true},
		{name: "array_elem_kind_17_count_1", accepted: true},
	} {
		wire, err := os.ReadFile(filepath.Join("..", "testdata", "wire", "tables", "framing", tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		var octets strings.Builder
		for _, b := range wire {
			fmt.Fprintf(&octets, "0x%02x,", b)
		}
		verdict := "value == NULL && report.refused"
		if tc.accepted {
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
    const Scene * value;
    memset(&report, 0, sizeof(report));
    value = scene_load(region, region_bytes, wire_%[1]s, (int64_t)sizeof(wire_%[1]s), &report);
    CHECK(%[3]s);
    return 0;
}
`, tc.name, octets.String(), verdict)
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runCTableWireProbe(t, u, source)
		})
	}
}
