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

// TestPythonVtUnionsRefusesPacketUnionArrays holds the target's one addition
// to the shared refusal chain: an array of a PACKET union, whose arms are all
// packet types, is refused by name until the packet layer carries it.
func TestPythonVtUnionsRefusesPacketUnionArrays(t *testing.T) {
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
	_, err = compiler.New().Generate(u, "python", compiler.Options{})
	if err == nil || !strings.Contains(err.Error(), "an array of unions in a table closure (Root.shapes) whose arms are all packet types") {
		t.Fatalf("the packet union array was not refused by name: %v", err)
	}
}
