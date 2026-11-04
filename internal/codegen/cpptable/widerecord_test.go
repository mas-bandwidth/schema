package cpptable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestWideRecordCompilesWithExactSizesCPP: the C++ unit for the record past
// 4 GiB (testdata/wire/tables/storage-cap/wide, docs/SPEC-TABLES.md §11)
// compiles warning-free, and every stored size and offset of Huge is exact at
// 64 bits: the type descriptor's size, the tail's offset in the table and
// block descriptors, and the items list's element size, each against sizeof
// and offsetof and the compiler's own layout. No Huge is built.
func TestWideRecordCompilesWithExactSizesCPP(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (c++)")
	memceiling.Guard(t, memceiling.Default)
	src, err := os.ReadFile(filepath.Join(storageCapDir, "wide", "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitFromSource(t, string(src))
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
	body := fmt.Sprintf(`    int failures = 0;
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %%s (line %%d)\n", (name), __LINE__); failures++; } } while (0)
    const int64_t huge_size = %dLL, tail_at = %dLL, past = (int64_t) 1 << 32;
    CHECK( (int64_t) sizeof( Huge ) == huge_size && HugeTableInfo.size == huge_size && huge_size > past, "the type descriptor's size" );
    CHECK( HugeTableFields[1].offset == (int64_t) offsetof( Huge, tail ) && HugeTableFields[1].offset == tail_at && tail_at > past, "the tail's offset" );
    CHECK( RootTableFields[0].elem_size == huge_size, "the items list's element size" );
    const TableBlockInfo * block = HugeBlock::Type();
    CHECK( block != NULL && block->size == (int64_t) sizeof( HugeBlock::Projection ) && block->size > past, "the block projection's size" );
    CHECK( block != NULL && block->fields[1].offset == (int64_t) offsetof( HugeBlock::Projection, tail ), "the block projection's tail offset" );
    if (failures != 0) return 1;
`, huge.Size, tail)
	out, err := runCPPCountRefusalProbe(t, u, body, "Block.h")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
