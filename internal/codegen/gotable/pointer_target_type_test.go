// TestGoPointerTargetTypeChecked holds the generated Go read of the variable
// form to docs/SPEC-TABLES.md §3.1's pointer-target rule (docs/rulings.json
// pointer-target-type-checked): a pointer slot naming a record whose type the
// reader DECLARES but that is not the type the slot requires is damage and the
// whole load refuses; a record of a type the reader cannot name is the lawful
// newer-writer case, counted unknown once with every pointer naming it null.
// The rows are testdata/wire/pointer-target-type/ and its manifest, and every
// read entry answers the same verdict.
package gotable

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestGoPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it builds and runs the generated unit)")

	root := filepath.Join("..", "..", "..")
	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	rows := pointerTargetManifestRows(t, string(manifest))
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			wire, err := os.ReadFile(filepath.Join(dir, row.file))
			if err != nil {
				t.Fatalf("%s: %v", row.file, err)
			}
			u := loadUnitPaths(t, root, []string{row.unit})
			source := pointerTargetProbe(u.Package, row.root, row, wire)
			if out, err := runGeneratedUnit(t, u, source, nil); err != nil {
				t.Fatalf("generated unit: %v\n%s", err, out)
			}
		})
	}
}

type pointerTargetManifestRow struct {
	name, unit, root, file string
	read                   bool
	unknown                int // -1 when the row does not pin a count
}

func pointerTargetManifestRows(t *testing.T, manifest string) []pointerTargetManifestRow {
	t.Helper()
	var rows []pointerTargetManifestRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := pointerTargetManifestRow{name: f[0], unit: f[1], root: f[2], file: f[3], unknown: -1}
		switch f[4] {
		case "read":
			row.read = true
		case "refused":
		default:
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[4])
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil || n < 0 {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q, want a count or -", i+1, f[5])
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	return rows
}

// pointerTargetProbe is a generated unit test: it runs one manifest row through
// each read entry — the plain load, the retaining load, the builder load, the
// strict read and the two measures — and holds each entry's verdict. A row
// whose type the reader cannot name also holds its pointer to null.
func pointerTargetProbe(pkg, root string, row pointerTargetManifestRow, wire []byte) string {
	return fmt.Sprintf(`package %[1]s
import("testing";"unsafe")
func ptrTargetAligned(n int64)[]byte{raw:=make([]byte,n+64);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;return raw[off:off+uintptr(n)]}
func TestPointerTargetType(t *testing.T){
 wire:=[]byte{%[3]s}
 const read=%[4]t
 const pinsUnknown=%[5]t
 const unknown=%[6]d
 const wantNull=%[7]t
 check:=func(where string,v *%[2]s,r *TableReport){
  if read {
   if v==nil||r.Refused(){t.Fatalf("%%s: read refused: %%+v",where,r)}
   if pinsUnknown&&r.Unknown!=int32(unknown){t.Fatalf("%%s: unknown %%d want %%d",where,r.Unknown,unknown)}
   if wantNull&&v.Next!=0{t.Fatalf("%%s: the pointer reads %%d, want null",where,v.Next)}
   return
  }
  if v!=nil||!r.Refused(){t.Fatalf("%%s: a refused row read: %%+v",where,r)}
 }
 if n,err:=%[2]sLoadMeasureReason(wire);read {
  if err!=nil||n<0{t.Fatalf("measure reason refused a read row: %%d %%v",n,err)}
 } else if err==nil||n>=0{t.Fatalf("measure reason read a refused row: %%d %%v",n,err)}
 if n:=%[2]sLoadMeasure(wire);read&&n<0{t.Fatalf("measure refused a read row")}else if !read&&n>=0{t.Fatalf("measure read a refused row: %%d",n)}
 var r TableReport
 check("load",%[2]sLoad(ptrTargetAligned(1<<20),wire,&r),&r)
 store:=TableRetain{Bytes:make([]byte,1<<20),Ids:make([]TableRetainID,1024)}
 r=TableReport{}
 check("retain",%[2]sLoadRetain(ptrTargetAligned(1<<20),wire,&store,&r),&r)
 var b %[2]sBuilder
 if !b.Init(){t.Fatal("builder init")}
 r=TableReport{}
 ok:=%[2]sLoadBuilder(&b,wire,&r)
 var bv *%[2]s
 if ok{bv=b.GetRoot()}
 if read {
  if !ok||r.Refused(){b.Shutdown();t.Fatalf("builder refused a read row: %%+v",r)}
  if pinsUnknown&&r.Unknown!=int32(unknown){b.Shutdown();t.Fatalf("builder unknown %%d want %%d",r.Unknown,unknown)}
  if wantNull&&bv.Next!=0{b.Shutdown();t.Fatalf("builder pointer %%d, want null",bv.Next)}
 } else if ok||!r.Refused(){b.Shutdown();t.Fatalf("builder read a refused row: %%+v",r)}
 b.Shutdown()
 sv,serr:=%[2]sReadStrict(wire)
 if read {
  if serr!=nil||sv==nil{t.Fatalf("strict refused a read row: %%v",serr)}
  if wantNull&&sv.Next!=0{t.Fatalf("strict pointer %%d, want null",sv.Next)}
 } else if serr==nil||sv!=nil{t.Fatalf("strict read a refused row: %%v",serr)}
}
`, pkg, root, byteLiteral(wire), row.read, row.unknown >= 0, row.unknown, row.unknown > 0)
}

func byteLiteral(b []byte) string {
	var s strings.Builder
	for i, v := range b {
		if i > 0 {
			s.WriteByte(',')
		}
		fmt.Fprintf(&s, "0x%02x", v)
	}
	return s.String()
}
