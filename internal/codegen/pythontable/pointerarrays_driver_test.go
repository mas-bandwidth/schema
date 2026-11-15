//go:build schema_leg_python

package pythontable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
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
// the python target, while still refusing uncarried []T value lists and
// pointer lists targeting packet types.
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
	c := compiler.New()
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
	if _, err := c.Generate(uScalar, "python", nil); err == nil || !strings.Contains(err.Error(), "values") || !strings.Contains(err.Error(), "python") {
		t.Fatalf("driver did not refuse []int32 by name: %v", err)
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
	if _, err := c.Generate(uTbl, "python", nil); err == nil || !strings.Contains(err.Error(), "items") {
		t.Fatalf("driver did not refuse []Item by name: %v", err)
	}

	const packetPtrList = `package badpktlist

type PacketItem
{
    x int32
}

table Bag
{
    items []*Bag
    pkt   PacketItem
}
`
	uPkt, err := c.Load(writeDriverSchema(t, "BadPktList.schema", packetPtrList))
	if err != nil {
		t.Fatalf("load packet pointer list schema: %v", err)
	}
	if _, err := c.Generate(uPkt, "python", nil); err == nil || !strings.Contains(err.Error(), "packet type") {
		t.Fatalf("driver did not refuse []*PacketItem by name: %v", err)
	}
}
