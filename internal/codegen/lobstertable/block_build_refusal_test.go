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

// THE BLOCK BUILDER REFUSES BY NAME, NOT BY ASSERT (docs/SPEC-TABLES.md §19.1:
// Begin does not clamp, it refuses; docs/SPEC.md §5.1 rows 3 and 4; SF-31).
// An array count or a string used length past its declared maximum makes
// block_build answer "" with the field named in block_refusal, under
// --runtime-no-asserts too, where an assert is compiled away and the writer
// would overrun the field into the next one. Each refused case has a lawful
// twin at the maximum.

const blockRefusalMain = `import .PaddedTable
import .block_form

def row() -> schema_rt.Record:
    return schema_rt.new_record(schema_rt.table_of("PaddedRow"))

def frame(rows:int, label:int) -> schema_rt.Record:
    let rec = schema_rt.new_record(schema_rt.table_of("PaddedFrame"))
    for(rec.fields) sl:
        if sl.fd.name == "rows":
            sl.elems = []
            var k = 0
            while k < rows:
                push(sl.elems, schema_rt.Value { tab: row() })
                k++
            sl.count = rows
            if rows > 0 and label > 0:
                let first = sl.elems[0].tab
                if first:
                    for(first.fields) f:
                        if f.fd.name == "label":
                            f.cell.s = repeat_string("x", label)
                            f.count = label
    return rec

def show(name:string, rows:int, label:int):
    let built = schema_rt.block_build("PaddedFrame", frame(rows, label))
    print(name + " " + (if built == "": "refused " + schema_rt.block_refusal else: "built"))

show("rows_past", 65, 0)
show("rows_max", 64, 0)
show("label_past", 1, 16)
show("label_max", 1, 15)
`

func TestLobsterBlockBuildRefusesByName(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables/block")})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("the block unit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(blockRefusalMain), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := lobsterrun.RunExitFlags(t, g.dir, "main.lobster", []string{"--runtime-no-asserts"}, nil)
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	want := []string{"rows_past refused rows holds 65, outside [ 0, 64 ]", "rows_max built", "label_past refused label holds 16, outside [ 0, 15 ]", "label_max built"}
	var bad []string
	for i, w := range want {
		line := "(no line)"
		if i < len(got) {
			line = got[i]
		}
		if line != w {
			bad = append(bad, fmt.Sprintf("got %q, want %q", line, w))
		}
	}
	if code != 0 {
		bad = append(bad, fmt.Sprintf("exit %d", code))
	}
	if len(bad) > 0 {
		t.Fatalf("block_build does not refuse by name:\n%s", strings.Join(bad, "\n"))
	}
}
