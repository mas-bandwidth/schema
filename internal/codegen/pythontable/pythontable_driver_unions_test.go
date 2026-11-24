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

// TestPythonVtUnionsCarriesPacketUnionArrays holds the public driver to the
// packet-union-array row: a union whose arms are packet types uses the same
// table descriptor and codec as every other union array.
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
	for _, name := range []string{"unions/_schema/kind_union.py", "unions/Unions.py"} {
		if _, ok := files[name]; !ok {
			t.Errorf("the driver emitted no %s", name)
		}
	}
}

// TestPythonVtUnionsStillRefusesBlobLists keeps a neighboring, genuinely
// uncarried array shape named. []*bytes is the array-of-byte-buffers follow-on
// in docs/SPEC-TABLES.md §2.9 and §15, independent of union-array support.
func TestPythonVtUnionsStillRefusesBlobLists(t *testing.T) {
	t.Parallel()
	if _, ok := compiler.Carried("python"); !ok {
		t.Skip("the python target is behind the schema_leg_python build tag")
	}
	const src = `package unions

table Root
{
    blobs []*bytes
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
	if err == nil || !strings.Contains(err.Error(), "an array of byte buffers is a named follow-on") {
		t.Fatalf("the blob list was not refused by name: %v", err)
	}
	if u != nil {
		t.Fatal("the refused blob list returned a unit")
	}
}
