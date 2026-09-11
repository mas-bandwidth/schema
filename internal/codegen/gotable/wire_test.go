package gotable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Compile and run the emitted code against an independent wire construction.
// The shared corpus's small vocabularies do not cross reference 127, and no
// ordinary name lets it exercise hash zero (which is a valid id, not None).
func TestWireLargeVocabularyAndZeroId(t *testing.T) {
	var schema strings.Builder
	schema.WriteString("package probe\nfixed table Child {\n")
	for i := range 140 {
		fmt.Fprintf(&schema, "f%03d uint64\n", i)
	}
	schema.WriteString("}\nfixed table Root { children [2]Child }\n")
	old := ir.TableWireIdHook
	ir.TableWireIdHook = func(name string) (uint64, bool) { return 0, name == "f000" }
	defer func() { ir.TableWireIdHook = old }()
	files := generate(t, schema.String())
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{"go.mod": "module probe\n\ngo 1.26\n", "wire_test.go": largeVocabularyTest} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, output)
	}
}

const largeVocabularyTest = `package probe

import (
 "bytes"
 "encoding/binary"
 "fmt"
 "hash/fnv"
 "reflect"
 "testing"
)

func hash(s string) uint64 { h := fnv.New64a(); h.Write([]byte(s)); return h.Sum64() }

func TestWire(t *testing.T) {
 var value Root
 RootReset(&value)
 child := reflect.ValueOf(&value.Children[0]).Elem()
 var first []byte
 ids := []uint64{hash("children")}
 for i := 0; i < 140; i++ {
  child.Field(i).SetUint(uint64(i+1))
  first = binary.AppendUvarint(first, uint64(i+2))
  first = append(first, 9)
  first = binary.LittleEndian.AppendUint64(first, uint64(i+1))
  id := hash(fmt.Sprintf("f%03d", i)); if i == 0 { id = 0 }
  ids = append(ids, id)
 }
 first = append(first, 0)
 value.Children[1].F139 = 999
 second := binary.AppendUvarint(nil, 141)
 second = append(second, 9)
 second = binary.LittleEndian.AppendUint64(second, 999)
 second = append(second, 0)
 body := []byte{13,2}
 body = binary.AppendUvarint(body, uint64(len(first)))
 body = append(body, first...)
 body = binary.AppendUvarint(body, uint64(len(second)))
 body = append(body, second...)
 want := binary.AppendUvarint([]byte{1,1,14}, uint64(len(body)))
 want = append(want, body...)
 want = append(want, 0)
 for _, id := range ids { want = binary.LittleEndian.AppendUint64(want, id) }
 want = binary.LittleEndian.AppendUint64(want, uint64(len(ids)))
 if n := RootMeasure(&value); n != int64(len(want)) { t.Fatalf("measure %d, want %d", n, len(want)) }
 got := make([]byte, len(want))
 if n := RootSave(&value, got); n != int64(len(got)) || !bytes.Equal(got,want) { t.Fatalf("save differs: n=%d",n) }
 var loaded Root
 var report TableReport
 if !RootLoad(&loaded, want, &report) || report != (TableReport{}) || loaded != value { t.Fatalf("load differs: %+v",report) }
 if RootSave(&value, got[:len(got)-1]) != -1 { t.Fatal("short destination accepted") }
 if n := testing.AllocsPerRun(10, func() { RootSave(&value,got); RootLoad(&loaded,got,&report); RootMeasure(&value) }); n != 0 { t.Fatalf("allocated %v",n) }
}

// The count uses the enclosing reader in the reference; its elements are
// still bounded by L. Count 129 consumes the following field's reference.
func TestArrayCountCrossesBody(t *testing.T) {
 wire := []byte{1,1,14,2,13,0x81,1,1,1,0}
 wire = binary.LittleEndian.AppendUint64(wire,hash("children"))
 wire = binary.LittleEndian.AppendUint64(wire,1)
 var value Root
 var report TableReport
 if !RootLoad(&value,wire,&report) || !report.Malformed || report.Clamped != 1 || report.KindMismatch != 1 {
  t.Fatalf("count recovery differs: %+v",report)
 }
}

func TestFormVerdicts(t *testing.T) {
 for _, form := range []byte{0,2,3,255} {
  var value Root
  value.Children[0].F000 = 99
  var report TableReport
  if RootLoad(&value,[]byte{form},&report) || report.Verdict != TableOpenRefused || report.Reason == "" || report.Malformed || value.Children[0].F000 != 0 {
   t.Fatalf("form %d: %+v",form,report)
  }
 }
 for _, wire := range [][]byte{nil,{1},{1,0,0,0,0,0,0,0,0,0,0}} {
  var value Root
  var report TableReport
  if RootLoad(&value,wire,&report) || report.Verdict != TableOpenDamaged || !report.Malformed { t.Fatalf("damage: %+v",report) }
 }
}
`

// Hardware float conversion quiets signaling NaNs. The table wire instead
// preserves the sign, quiet bit and payload when widening a kind-10 value.
func TestWireSignalingNaNWideningAndLEBOverflow(t *testing.T) {
	runGenerated(t, `package probe
fixed table Root { value float64 }
`, `package probe
import("testing";"math";"hash/fnv")
func TestSignalingNaN(t *testing.T) {
 h:=fnv.New64a();h.Write([]byte("value"))
 for _,bits:=range []uint32{0x7f800001,0xff812345,0x7fc12345,0x7f800000,0xff800000} {
  var ids TableIds;buffer:=make([]byte,128);w:=TableWriter{Buffer:buffer,Ids:&ids};w.Put8(1);w.Id(h.Sum64());w.Put8(10);w.Put32(bits);w.Put8(0);w.Trailer()
  var value Root;var report TableReport
  if !RootLoad(&value,buffer[:w.Offset],&report)||report.Widened!=1||report.Malformed {t.Fatalf("load %#x: %+v",bits,report)}
  want:=uint64(bits>>31)<<63|0x7ff0000000000000|uint64(bits&0x7fffff)<<29
  if got:=math.Float64bits(value.Value);got!=want {t.Fatalf("widen %#x: %#x want %#x",bits,got,want)}
 }
}
func TestLEBTenthByteOverflow(t *testing.T) {
 for last:=byte(2);last<128;last++ {data:=[10]byte{0x80,0x80,0x80,0x80,0x80,0x80,0x80,0x80,0x80,last};r:=TableReader{Buffer:data[:]};if _,ok:=r.Leb();ok||r.Offset!=0{t.Fatalf("accepted overflow tenth byte %#x",last)}}
}
`)
}
