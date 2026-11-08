package lobstertable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// Section 21.2 permits fixed-array growth in variable binary: the reader
// accepts the written prefix and supplies declaration defaults for new slots.
// A writer exceeding the reader's bound still refuses the complete value.
func TestLobsterSingleReadFixedArrayGrowth(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "Growth.schema")
	if err := os.WriteFile(path, []byte(singleReadGrowthSchema), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(singleReadGrowthProgram), 0644); err != nil {
		t.Fatal(err)
	}
	if out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", nil))); out != "array growth passed" {
		t.Fatal(out)
	}
}

const singleReadGrowthSchema = `package growth
table Written {
 values [2]int32
 children [2]Child
}
table Grown {
 values [4]int32
 children [4]Child
}
table Child { value int32 = 7 }
`

const singleReadGrowthProgram = `import .GrowthTable
import .load
import .schema_save
let old = schema_rt.new_record(schema_rt.table_of("Written"))
old.fields[0].elems[0].i = 3
old.fields[0].elems[1].i = 5
let old_child0 = old.fields[1].elems[0].tab
let old_child1 = old.fields[1].elems[1].tab
assert old_child0 and old_child1
old_child0.fields[0].cell.i = 11
old_child1.fields[0].cell.i = 13
let wire = schema_rt.save(old)
let rep = schema_rt.LoadReport {}
let grown = schema_rt.load("Grown", wire, rep)
assert grown
assert length(grown.fields[0].elems) == 4
assert grown.fields[0].elems[0].i == 3 and grown.fields[0].elems[1].i == 5
assert grown.fields[0].elems[2].i == 0 and grown.fields[0].elems[3].i == 0
for(4) i:
    let child = grown.fields[1].elems[i].tab
    assert child
    assert child.fields[0].cell.i == (if i == 0: 11 elif i == 1: 13 else: 7)
let expected = schema_rt.new_record(schema_rt.table_of("Grown"))
expected.fields[0].elems[0].i = 3
expected.fields[0].elems[1].i = 5
let expected0 = expected.fields[1].elems[0].tab
let expected1 = expected.fields[1].elems[1].tab
assert expected0 and expected1
expected0.fields[0].cell.i = 11
expected1.fields[0].cell.i = 13
let canonical = schema_rt.save(grown)
assert canonical == schema_rt.save(expected)
let reread = schema_rt.load("Grown", canonical, rep)
assert reread and schema_rt.save(reread) == canonical
assert not schema_rt.load("Written", canonical, rep)
schema_rt.release(old)
schema_rt.release(grown)
schema_rt.release(expected)
schema_rt.release(reread)
print("array growth passed")
`
