package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// THE ELEMENT KINDS AN ARRAY OR ENUM-KEYED BODY MAY CARRY are the closed lists
// of docs/SPEC-TABLES.md §3: kind 14 over 1–13, 15, 17 and 18–30, and kind 16
// over 1–11, 13 and 18–30. Any other element kind is damage under ANY count, a
// count of 0 included, in a declared field and in an unknown one alike. These
// are the held vectors' verdicts: the two element-kind-17 bodies read, and the
// element kinds off either list refuse.
var lobsterArrayElementKindVectors = []struct {
	name string
	read bool
}{
	// REFUSED: element kinds off the closed list, under a count of one or more.
	{name: "array_elem_kind_32_count_2", read: false},
	{name: "array_elem_kind_31_count_1", read: false},
	{name: "array_elem_kind_16_count_1", read: false},
	// REFUSED: element kinds off the closed list under a count of ZERO, where
	// no element byte follows: the header's kind byte is still judged.
	{name: "array_elem_unassigned_count_0", read: false},
	{name: "array_elem_reserved_count_0", read: false},
	{name: "array_elem_zero_count_0", read: false},
	// READ: element kind 17 is on the array list, at count 0 and at count 1
	// where its node index names a reachable record.
	{name: "array_elem_kind_17_count_0", read: true},
	{name: "array_elem_kind_17_count_1", read: true},
}

// TestLobsterArrayElementKindsClosed runs every held array_elem_* vector
// through this leg's strict load and holds it to the engine's verdict alone, by
// the vector's name; a rejection's reason belongs to the leg
// (docs/rulings.json refusal-verdict-only).
func TestLobsterArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["graphdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("graphdemo is refused: %v", err)
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .load
def verdict(name:string, file:string):
    let data = read_file(file)
    assert data
    let rec = schema_rt.load("Scene", data, schema_rt.LoadReport {})
    print(name + "|" + (if rec: "read" else: "refused"))
`)
	inputs := map[string][]byte{}
	for i, v := range lobsterArrayElementKindVectors {
		file := fmt.Sprintf("v%d", i)
		data, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing", v.name+".bin"))
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		inputs[file] = data
		fmt.Fprintf(&program, "verdict(%q, %q)\n", v.name, file)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n") {
		name, verdict, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("unexpected verdict line %q", line)
		}
		got[name] = verdict
	}
	for _, v := range lobsterArrayElementKindVectors {
		want := "refused"
		if v.read {
			want = "read"
		}
		if got[v.name] != want {
			t.Errorf("%s: lobster %s, want %s", v.name, got[v.name], want)
		}
	}
}
