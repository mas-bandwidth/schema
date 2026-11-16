<<<<<<< HEAD
//go:build schema_leg_python

// Pointer arrays and table field guards through the compiler's driver, in a
// build with the schema_leg_python tag: `go test -tags schema_leg_python
// ./compiler -run Python` (make test-python). The driver test lives beside the
// target, under compiler/, because a tag-bound test file under internal/ is
// one the untagged toolchain never compiles (internal/ci's
// TestNoTestFileIsExcludedByABuildConstraint).
=======
// Pointer arrays and table field guards through the built-in compiler target.
>>>>>>> f21ed2ad0370caa9b8a18426125e142bc259a48f
package compiler

import (
	"os"
	"path/filepath"
<<<<<<< HEAD
	"strings"
=======
>>>>>>> f21ed2ad0370caa9b8a18426125e142bc259a48f
	"testing"
)

func writeDriverSchema(t *testing.T, name, src string) []string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return []string{p}
}

// TestPythonVtPointerArraysThroughDriver checks that the public compiler
// driver accepts []*T, [N]*T, [..N]*T, [A..N]*T, and guarded table fields on
<<<<<<< HEAD
// the python target, while still refusing uncarried []T value lists and
// pointer lists targeting packet types.
=======
// the python target, alongside scalar and by-value lists, while refusing pointer lists targeting
// packet types.
>>>>>>> f21ed2ad0370caa9b8a18426125e142bc259a48f
func TestPythonVtPointerArraysThroughDriver(t *testing.T) {
	t.Parallel()

	const accepted = `package ptrdrv

fixed table Leaf
{
    id int32
}

table Node
{
    value    int32
    children []*Node
    fixed3   [3]*Leaf
    bounded  [..4]*Leaf
    ranged   [2..4]*Leaf
    active   bool
    if active
    {
        extra *Node
        list2 []*Leaf
    }
    else
    {
        fallback int32
    }
}
`
	c := New()
	u, err := c.Load(writeDriverSchema(t, "Ptr.schema", accepted))
	if err != nil {
		t.Fatalf("load accepted schema: %v", err)
	}
	files, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatalf("driver refused pointer arrays and guarded fields: %v", err)
	}
	for _, want := range []string{
		"ptrdrv/__init__.py",
		"ptrdrv/Ptr.py",
		"ptrdrv/_schema/kind_pointer_array.py",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("driver output lacks %s", want)
		}
	}

	const scalarList = `package badlist

table Bag
{
    values []int32
}
`
	uScalar, err := c.Load(writeDriverSchema(t, "BadList.schema", scalarList))
	if err != nil {
		t.Fatalf("load scalar list schema: %v", err)
	}
<<<<<<< HEAD
	if _, err := c.Generate(uScalar, "python", nil); err == nil || !strings.Contains(err.Error(), "values") || !strings.Contains(err.Error(), "python") {
		t.Fatalf("driver did not refuse []int32 by name: %v", err)
=======
	if files, err := c.Generate(uScalar, "python", nil); err != nil || len(files) == 0 {
		t.Fatalf("driver did not generate []int32: %v", err)
>>>>>>> f21ed2ad0370caa9b8a18426125e142bc259a48f
	}

	const tableValueList = `package badtbllist

fixed table Item
{
    x int32
}

table Bag
{
    items []Item
}
`
	uTbl, err := c.Load(writeDriverSchema(t, "BadTblList.schema", tableValueList))
	if err != nil {
		t.Fatalf("load value-table list schema: %v", err)
	}
<<<<<<< HEAD
	if _, err := c.Generate(uTbl, "python", nil); err == nil || !strings.Contains(err.Error(), "items") {
		t.Fatalf("driver did not refuse []Item by name: %v", err)
=======
	if files, err := c.Generate(uTbl, "python", nil); err != nil || len(files) == 0 {
		t.Fatalf("driver did not generate []Item: %v", err)
>>>>>>> f21ed2ad0370caa9b8a18426125e142bc259a48f
	}
}
