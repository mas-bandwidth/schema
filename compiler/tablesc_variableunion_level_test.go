package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCTableUnknownUnionCountsItsLevel holds the C strict read to §4's ONE
// NESTING BOUND on every pinned depth vector: the root body is at depth 0, a
// table body, an array, a keyed body and a union whose arm is structural are
// each one level deeper, and depth 128 passes while 129 refuses. The verdict is
// the whole contract, so the probe compares only read or refused and never a
// refusal's name.
func TestCTableUnknownUnionCountsItsLevel(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(repo, "tables/pointers/*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	u := unitFromPaths(t, paths)
	vectors := []struct {
		name string
		read bool
	}{
		{"depth_tables_128", true},
		{"depth_arrays_128", true},
		{"depth_union_tables_64", true},
		{"depth_keyed_128", true},
		{"depth_tables_129", false},
		{"depth_arrays_129", false},
		{"depth_union_tables_65", false},
		{"depth_keyed_129", false},
		{"depth_tables_5000", false},
	}
	var source strings.Builder
	source.WriteString(`#include "GraphTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static int load_one(const uint8_t * wire, int64_t bytes, int expected)
{
    int64_t total = scene_load_measure(wire, bytes);
    uint8_t * region = total >= 0 ? (uint8_t *) malloc((size_t) total) : NULL;
    TableReport report;
    const Scene * value;
    int read;
    memset(&report, 0, sizeof(report));
    value = scene_load(region, total >= 0 ? total : 0, wire, bytes, &report);
    read = value != NULL && !report.refused;
    free(region);
    if ( read != expected ) { fprintf(stderr, "read=%d want=%d\n", read, expected); return 1; }
    return 0;
}
int main(void)
{
`)
	for _, v := range vectors {
		data, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/framing", v.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&source, "    {\n        static const uint8_t wire[] = {")
		for i, b := range data {
			if i%16 == 0 {
				source.WriteString("\n            ")
			}
			fmt.Fprintf(&source, "0x%02x,", b)
		}
		fmt.Fprintf(&source, "\n        };\n        if ( load_one(wire, (int64_t) sizeof(wire), %d) ) { fprintf(stderr, \"%s\\n\"); return 1; }\n    }\n", map[bool]int{true: 1, false: 0}[v.read], v.name)
	}
	source.WriteString("    return 0;\n}\n")
	runCTableWireProbe(t, u, source.String())
}
