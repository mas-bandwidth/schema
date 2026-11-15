package pythontable_test

import (
	"os"
	"path/filepath"
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
