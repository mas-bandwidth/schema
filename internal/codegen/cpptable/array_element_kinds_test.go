package cpptable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCppArrayElementKindsClosed holds docs/SPEC-TABLES.md §3's closed element
// kind lists on the C++ leg: an array body (kind 14) may carry 1–13, 15, 17,
// 18–29 and 30, and any other element kind is damage under any count, a count
// of 0 included, in an unknown field as in one this reader declares. Each
// pinned vector runs through graphdemo Scene's strict load and the verdict
// alone is compared (docs/rulings.json refusal-verdict-only); the kind-17
// vectors are the read controls.
func TestCppArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	framing := filepath.Join(repo, "testdata", "wire", "tables", "framing")
	cases := []struct {
		name string
		read bool
	}{
		{"array_elem_kind_32_count_2", false},
		{"array_elem_kind_31_count_1", false},
		{"array_elem_kind_16_count_1", false},
		{"array_elem_unassigned_count_0", false},
		{"array_elem_reserved_count_0", false},
		{"array_elem_zero_count_0", false},
		{"array_elem_kind_17_count_0", true},
		{"array_elem_kind_17_count_1", true},
	}
	u := loadGraphDirUnit(t, filepath.Join(repo, "tables", "pointers"))
	var body strings.Builder
	for _, c := range cases {
		wire, err := os.ReadFile(filepath.Join(framing, c.name+".bin"))
		if err != nil {
			t.Fatalf("%s: the pinned vector is missing: %v", c.name, err)
		}
		fmt.Fprintf(&body, `
    {
        const uint8_t wire[] = { %s };
        const bool want = %t;
        const int64_t need = SceneLoadMeasure( wire, (int64_t) sizeof( wire ) );
        std::vector<uint8_t> region( (size_t) ( need > 0 ? need : 4096 ) + 64 );
        uint8_t * aligned = region.data();
        while ( ( (uintptr_t) aligned & 63 ) != 0 ) { aligned++; }
        TableReport g; memset( &g, 0, sizeof( g ) );
        const Scene * root = SceneLoad( aligned, need > 0 ? need : 4096, wire, (int64_t) sizeof( wire ), &g );
        if ( ( root != NULL ) != want ) { printf( "%s: the strict load reported the wrong verdict (root=%%d want=%%d)\n", root != NULL ? 1 : 0, (int) want ); return 1; }
        if ( !want && !g.damaged && !g.refused ) { printf( "%s: a refused load reported neither damage nor a refusal\n" ); return 1; }
    }
`, cppBytes(wire), c.read, c.name, c.name)
	}
	out, err := cppHeldRun(t, u, body.String())
	if err != nil {
		t.Fatalf("the element-kind probe failed: %v\n%s", err, out)
	}
}
