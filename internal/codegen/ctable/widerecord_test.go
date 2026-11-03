package ctable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cWideRecordDir holds Wide.schema, a record past 4 GiB (docs/SPEC-TABLES.md
// §11) that every leg generates and compiles: Part is 2 GiB, Huge is three of
// them and a trailing field.
const cWideRecordDir = "../../../testdata/wire/tables/storage-cap/wide"

// TestWideRecordC holds every stored size and offset the C leg keeps at 64
// bits. Wide.schema generates and compiles under -Wall -Wextra -Werror, and a
// probe reads back, as the layout's own values past 4 GiB: Huge's storage
// size and its tail's offset from the type descriptor, the items list's
// element size, and Huge's block row size and tail offset from the block
// descriptor. Nothing builds a Huge.
func TestWideRecordC(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	memceiling.Guard(t, memceiling.Default)
	src, err := os.ReadFile(filepath.Join(cWideRecordDir, "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	huge := ir.RecordLayout(u, u.Tables["Huge"])
	var tail int64 = -1
	for _, f := range huge.Fields {
		if f.Field.Name == "tail" {
			tail = f.Offset
		}
	}
	if huge.Size <= 1<<32 || tail <= 1<<32 {
		t.Fatalf("Wide.schema no longer reaches past 4 GiB: Huge %d, tail at %d", huge.Size, tail)
	}
	body := fmt.Sprintf(`
    int failures = 0;
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %%s (line %%d)\n", (name), __LINE__); failures++; } } while (0)
    const TableTypeInfo * huge = &schema_widerecord_huge_info_;
    const TableTypeInfo * root = &schema_widerecord_root_info_;
    const TableBlockInfo * block = &schema_widerecord_huge_block_info_;
    int i;
    CHECK( huge->size == UINT64_C(%[1]d), "Huge's size" );
    for ( i = 0; i < huge->num_fields; i++ )
    {
        if ( strcmp( huge->fields[i].name, "tail" ) == 0 ) { CHECK( huge->fields[i].offset == UINT64_C(%[2]d), "Huge.tail's offset" ); }
    }
    for ( i = 0; i < root->num_fields; i++ )
    {
        if ( strcmp( root->fields[i].name, "items" ) == 0 ) { CHECK( root->fields[i].elem_size == UINT64_C(%[1]d), "items' element size" ); }
    }
    CHECK( block->size > UINT64_C(4294967296), "Huge's block projection size" );
    for ( i = 0; i < block->num_fields; i++ )
    {
        if ( strcmp( block->fields[i].name, "tail" ) == 0 ) { CHECK( block->fields[i].offset > UINT64_C(4294967296), "Huge.tail's block offset" ); }
    }
    if ( failures != 0 ) { return 1; }
`, huge.Size, tail)
	out, err := runCWideRecordProbe(t, u, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// runCWideRecordProbe compiles every file the unit generates, the block form
// included, under -Wall -Wextra -Werror with a main around body, and runs it.
func runCWideRecordProbe(t *testing.T, u *ir.Unit, body string) (string, error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		cc, err = exec.LookPath("cc")
		if err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := files["ProbeBlock.h"]; !ok {
		t.Fatal("the unit generated no block form")
	}
	src := "#include \"ProbeTable.h\"\n#include \"ProbeBlock.h\"\n#include <stdio.h>\n#include <string.h>\n\nint main(void) {\n" + body + "\n    return 0;\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		return string(output), fmt.Errorf("compile: %w", err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	output, err := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput()
	return string(output), err
}
