package gotable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A compressed float's -0 lands as +0 and is written as +0
// (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero). The
// vector is the reference writer's file holding the sign bit alone in the
// compressed field; this leg reads it through its generated load, lands the
// value 0 with the sign bit clear, refuses nothing, and saves the canonical
// bytes the +0 record writes.
func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go test (a generated probe package)")
	corpus := fixedCorpus(t)
	schema := readSchema(t, "FNZ")
	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(corpus, vec.File)
			src := fmt.Sprintf(`package probe
import ("bytes"; "encoding/binary"; "math"; "os"; "testing")
func TestCompressedNegativeZero(t *testing.T) {
 t.Parallel()
 data, err := os.ReadFile(%[1]q)
 if err != nil { t.Fatal(err) }
 layoutBytes := int(binary.LittleEndian.Uint32(data[TableFixedHeaderBytes:]))
 valueAt := TableFixedHeaderBytes + 4 + layoutBytes + 8 + 4
 if got := binary.LittleEndian.Uint32(data[valueAt:]); got != 0x80000000 {
  t.Fatalf("the vector does not hold a compressed float's -0: %%#08x", got)
 }
 back := make([]NegZero, 1)
 plan := make([]TableFixedEntry, 64)
 var report TableReport
 if n := NegZeroFixedLoad(back, data, plan, &report); n != 1 {
  t.Fatalf("the reader refused a compressed -0: n=%%d %%+v", n, report)
 }
 if report != (TableReport{}) {
  t.Fatalf("the read of a compressed -0 reported %%+v", report)
 }
 if bits := math.Float32bits(back[0].Value); bits != 0 {
  t.Fatalf("a compressed float's -0 kept its sign: %%#08x", bits)
 }
 out := make([]byte, NegZeroFixedMeasure(1))
 if m := NegZeroFixedSave(back, out); m != int64(len(data)) {
  t.Fatalf("the save wrote %%d bytes, want %%d", m, len(data))
 }
 want := bytes.Clone(data)
 clear(want[valueAt : valueAt+4])
 if !bytes.Equal(out, want) {
  t.Fatal("saving the landed +0 did not write the canonical bytes")
 }
}
`, file)
			out, err := runVersionProbe(t, schema, nil, src)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
		})
	}
}
