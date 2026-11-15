package goosetable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

// THE CLOSED ELEMENT KINDS (docs/SPEC-TABLES.md §3, §4). An array body (kind
// 14) may carry element kinds 1–13, 15, 17 and 18–30; an enum-keyed body
// (kind 16) may carry 1–11, 13 and 18–30. Any other element kind is damage
// under any count, a count of 0 included, in a field the reader cannot name
// as in one it declares. Each held vector rides an unknown root field of
// graphdemo Scene, so the strict load's walk over unknown bytes is what must
// answer it.
var gooseArrayElementKindVectors = []struct {
	name     string
	accepted bool
}{
	{name: "array_elem_kind_32_count_2", accepted: false},
	{name: "array_elem_kind_31_count_1", accepted: false},
	{name: "array_elem_kind_16_count_1", accepted: false},
	{name: "array_elem_unassigned_count_0", accepted: false},
	{name: "array_elem_reserved_count_0", accepted: false},
	{name: "array_elem_zero_count_0", accepted: false},
	{name: "array_elem_kind_17_count_0", accepted: true},
	{name: "array_elem_kind_17_count_1", accepted: true},
}

const gooseArrayElementKindProbe = `import os;
import .conformance_unit;
import .wire_load;
fn probe(name: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(str(name, ".bin"), data) { exit(1); }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), "Scene", data, vals, nodes, rep);
    print(name, " ", (if root >= 0 { 1 } else { 0 }));
}
`

// TestGooseArrayElementKindsClosed runs every pinned array_elem_* vector
// through the Goose strict load and holds the verdict alone (docs/rulings.json
// refusal-verdict-only): read or refused, never the reason.
func TestGooseArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	c := readWireCorpus(t)
	files := wireUnitFiles(t, "graphdemo", c.units["graphdemo"], nil)
	var main strings.Builder
	main.WriteString(gooseArrayElementKindProbe)
	main.WriteString("fn main() {\n")
	for _, v := range gooseArrayElementKindVectors {
		b, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing", v.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		files[v.name+".bin"] = b
		fmt.Fprintf(&main, "    probe(%q);\n", v.name)
	}
	main.WriteString("}\n")
	files["main.goose"] = []byte(main.String())
	got := strings.Split(strings.TrimSpace(string(gooserun.Run(t, t.TempDir(), "main.goose", files))), "\n")
	if len(got) != len(gooseArrayElementKindVectors) {
		t.Fatalf("got %d probe rows, want %d:\n%s", len(got), len(gooseArrayElementKindVectors), strings.Join(got, "\n"))
	}
	for i, line := range got {
		v := gooseArrayElementKindVectors[i]
		want := 0
		if v.accepted {
			want = 1
		}
		if line != fmt.Sprintf("%s %d", v.name, want) {
			t.Errorf("%s: got %q, want verdict %d (read=1, refused=0)", v.name, line, want)
		}
	}
}
