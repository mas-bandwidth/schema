package cpptable

// TestNestedTableUnionDefinitionOrder pins the C++ table emitter's SHAPE
// order: a table-armed union that an arm of another table-armed union holds by
// value must be defined before that arm's union, however deep the chain runs.
// The emitter used to place only the unions a table holds DIRECTLY before it,
// so a three-deep chain emitted the outer union first and the header failed to
// compile with the inner one named before its definition.
//
// The fixture is three table-armed unions nested by value under one table;
// the assertion is that the generated Table.h compiles under the host C++
// compiler.

import (
	"os"
	"path/filepath"
	"testing"
)

const nestedUnionOrderSchema = `package deepunion

table Leaf
{
    n int32
}

union L1
{
    a Leaf
}

union L2
{
    a L1
}

union L3
{
    a L2
}

table Root
{
    pick L3
}
`

func TestNestedTableUnionDefinitionOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "DeepUnion.schema")
	if err := os.WriteFile(path, []byte(nestedUnionOrderSchema), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, err := cppRunUnit(t, path, "deepunion", "", nil); err != nil {
		t.Fatalf("unions nested by value did not compile:\n%s\n%v", out, err)
	}
}
