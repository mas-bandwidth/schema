package gotable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideRecordDir holds the record past 4 GiB every leg generates and compiles
// (docs/SPEC-TABLES.md §11): Huge is three 2 GiB parts and a trailing field,
// so its size and its tail's offset need 64 bits.
const wideRecordDir = "../../../testdata/wire/tables/storage-cap/wide"

// TestWideRecordCompilesWithExactSizes: the Go unit for Wide.schema compiles,
// and every stored size and offset of Huge is exact at 64 bits: the type
// descriptor's Size, the tail's Offset in the table, cook and block
// descriptors, the items list's element size, and the cook sizes, each against
// unsafe.Sizeof/Offsetof and the compiler's own layout. No Huge is built: the
// probe reads descriptors and constant expressions only.
func TestWideRecordCompilesWithExactSizes(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "go1.26.0")
	memceiling.Guard(t, memceiling.Default)
	dir, err := filepath.Abs(wideRecordDir)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitPaths(t, dir, []string{"."})
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
	runGenerated(t, string(src), fmt.Sprintf(`package widerecord
import ("testing";"unsafe")
const hugeSize, tailAt = int64(%d), int64(%d)
func TestWide(t *testing.T){
 if HugeTableInfo.Size!=int64(unsafe.Sizeof(Huge{}))||HugeTableInfo.Size<=1<<32{t.Fatalf("Huge size %%d, sizeof %%d",HugeTableInfo.Size,unsafe.Sizeof(Huge{}))}
 if HugeTableInfo.CookSize!=hugeSize{t.Fatalf("Huge cook size %%d, layout %%d",HugeTableInfo.CookSize,hugeSize)}
 f:=HugeTableInfo.Fields[1]
 if f.Name!="tail"||f.Offset!=int64(unsafe.Offsetof(Huge{}.Tail))||f.Offset<=1<<32||f.CookOffset!=tailAt{t.Fatalf("tail offset %%d cook %%d, offsetof %%d layout %%d",f.Offset,f.CookOffset,unsafe.Offsetof(Huge{}.Tail),tailAt)}
 items:=RootTableInfo.Fields[0]
 if items.Name!="items"||items.ElemSize!=int64(unsafe.Sizeof(Huge{}))||items.CookElemSize!=hugeSize{t.Fatalf("items element %%d cook %%d",items.ElemSize,items.CookElemSize)}
 found:=0
 for i:=range tableCookRecords{c:=&tableCookRecords[i];if c.Name=="Huge"{found++;if c.Size!=hugeSize||c.Fields[1].Offset!=tailAt{t.Fatalf("cook Huge %%d tail %%d",c.Size,c.Fields[1].Offset)}}}
 for i:=range tableBlockRecords{b:=&tableBlockRecords[i];if b.Name=="Huge"{found++;if b.Size!=int64(unsafe.Sizeof(HugeBlockProjection{}))||b.Size<=1<<32||b.Fields[1].Offset!=int64(unsafe.Offsetof(HugeBlockProjection{}.Tail)){t.Fatalf("block Huge %%d tail %%d",b.Size,b.Fields[1].Offset)}}}
 if found!=2{t.Fatalf("found %%d of the cook and block descriptors",found)}
}
`, huge.Size, tail))
}
