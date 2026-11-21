package pythontable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// TestPythonVtUnionsThroughTheDriver holds the target's registration: a table
// closure with a union of table arms and an array of unions reaches the table
// frame instead of the shared table-arm refusal.
func TestPythonVtUnionsThroughTheDriver(t *testing.T) {
	t.Parallel()
	if _, ok := compiler.Carried("python"); !ok {
		t.Skip("the python target is behind the schema_leg_python build tag")
	}
	const src = `package unions

table Leaf { value int32 }

union Pick
{
    leaf Leaf
    number int32
    ping
}

table Root
{
    pick  Pick
    picks [2]Pick
}
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Unions.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := compiler.New().Generate(u, "python", compiler.Options{})
	if err != nil {
		t.Fatalf("the driver refused the table union: %v", err)
	}
	for _, name := range []string{"unions/_schema/kind_union.py", "unions/Unions.py"} {
		if _, ok := files[name]; !ok {
			t.Errorf("the driver emitted no %s", name)
		}
	}
}

// TestPythonVtUnionsCarriesPacketUnionArrays holds the target's carry of an
// array of a PACKET union, whose arms are all packet types: the driver emits
// the table frame, the field rides union_array (kind 14 over element kind 15,
// docs/SPEC-TABLES.md §2.6, §3) and the packet union registers its UnionDesc,
// on a variable root and on a fixed one.
func TestPythonVtUnionsCarriesPacketUnionArrays(t *testing.T) {
	t.Parallel()
	if _, ok := compiler.Carried("python"); !ok {
		t.Skip("the python target is behind the schema_leg_python build tag")
	}
	const src = `package unions

type Vec { x int32 }

union Shape
{
    vec Vec
}

table Root
{
    shapes [2]Shape
}

fixed table Row
{
    shapes [..3]Shape
}
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Unions.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := compiler.New().Generate(u, "python", compiler.Options{})
	if err != nil {
		t.Fatalf("the driver refused the packet union array: %v", err)
	}
	code := string(files["unions/Unions.py"])
	if !strings.Contains(code, "register_union(Shape)") {
		t.Error("the packet union Shape registers no UnionDesc")
	}
	for _, table := range []string{"Root", "Row"} {
		i := strings.Index(code, table+".descriptor = TableDesc(")
		if i < 0 {
			t.Fatalf("no descriptor for %s", table)
		}
		desc, _, _ := strings.Cut(code[i:], "\nregister(")
		if !strings.Contains(desc, `name="shapes"`) || !strings.Contains(desc, `codec="union_array"`) || !strings.Contains(desc, `target="Shape"`) {
			t.Errorf("%s.shapes is not an array of Shape unions:\n%s", table, desc)
		}
	}
}
