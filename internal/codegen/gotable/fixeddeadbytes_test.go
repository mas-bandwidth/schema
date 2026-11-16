package gotable

import (
<<<<<<< HEAD
=======
	"encoding/binary"
>>>>>>> f73f6cf561aef1c3ed01dd843e2317f30431362b
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO, AND A NON-ZERO ONE REFUSES THE
// WHOLE LOAD (docs/rulings.json fixed-dead-bytes-are-zero; docs/SPEC-TABLES.md
// §3.4; docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3). Each changed corpus
// vector is refused by the generated Go read and leaves the caller's
// destination untouched; each control reads and saves back byte for byte.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go test (a generated probe package)")
	corpus := fixedCorpus(t)
	var cases []string
	for _, vec := range fixedvector.DeadBytes {
		cases = append(cases, fmt.Sprintf("\t\t{file: %q, reads: %v},", vec.File, vec.Reads))
	}
	src := fmt.Sprintf(`package probe
import ("bytes"; "os"; "reflect"; "testing")
func TestFixedDeadBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file  string
		reads bool
	}{
%s
	}
	corpus := %q
	for _, c := range cases {
		data, err := os.ReadFile(corpus + "/" + c.file)
		if err != nil {
			t.Fatalf("%%s: %%v", c.file, err)
		}
		dest := make([]DeadRoot, 8)
		for i := range dest {
			DeadRootReset(&dest[i])
			dest[i].OptPresent = true
			dest[i].Opt.N = 99
			dest[i].ArrCount = 3
			dest[i].Arr[0] = -7
			dest[i].ObjsCount = 3
			dest[i].Objs[0].X = -9
			dest[i].TextLength = 8
			dest[i].Text[0] = 'Z'
			dest[i].BlobLength = 4
			dest[i].Blob[0] = 0xEE
			dest[i].Pick.Type = DeadPickTypeWide
			dest[i].Pick.Wide.A = 77
		}
		before := append([]DeadRoot(nil), dest...)
		plan := make([]TableFixedEntry, 4096)
		var report TableReport
		n := DeadRootFixedLoad(dest, data, plan, &report)
		if !c.reads {
			if n >= 0 || report.Err() == nil {
				t.Fatalf("%%s: the generated read accepted a record with a non-zero dead byte: n=%%d report=%%+v", c.file, n, report)
			}
			if !reflect.DeepEqual(dest, before) {
				t.Fatalf("%%s: the refusal wrote the caller's destination", c.file)
			}
			continue
		}
		if n != 1 || report.Err() != nil {
			t.Fatalf("%%s: the control did not read: n=%%d report=%%+v", c.file, n, report)
		}
		out := make([]byte, DeadRootFixedMeasure(1))
		if got := DeadRootFixedSave(dest[:1], out); got != int64(len(out)) {
			t.Fatalf("%%s: save wrote %%d of %%d", c.file, got, len(out))
		}
		if !bytes.Equal(out, data) {
			t.Fatalf("%%s: the control did not save back byte for byte", c.file)
		}
	}
}
`, strings.Join(cases, "\n"), filepath.ToSlash(corpus))
	out, err := runVersionProbe(t, readSchema(t, "DeadBytes"), nil, src)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
<<<<<<< HEAD
=======

// A lineage read checks the writer's source spans before landing any record,
// including dead tails whose offsets change when this reader widens storage.
func TestFixedDeadBytesUseWriterLayout(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go test (a generated probe package)")
	older := `package writerdead
 union Choice {
 narrow int16
 wide int64
 }
 fixed table WriterDead {
  lead uint32
  opt ?int16 | deprecated
  vals [..3]int16
  text string(4)
  blob bytes(4)
  pick Choice
  trail uint32
 }`
	newer := strings.NewReplacer("?int16", "?int32", "[..3]int16", "[..5]int32", "string(4)", "string(8)", "bytes(4)", "bytes(8)", "narrow int16", "narrow int32").Replace(older)
	entry, ok := FixedLineageOf(unitOf(t, older), "WriterDead")
	if !ok || entry.Record != 54 {
		t.Fatalf("writer record changed: %+v", entry)
	}
	body := make([]byte, 46)
	binary.LittleEndian.PutUint32(body, 1)
	binary.LittleEndian.PutUint32(body[7:], 1)
	binary.LittleEndian.PutUint16(body[11:], 7)
	binary.LittleEndian.PutUint32(body[17:], 1)
	body[21] = 'a'
	binary.LittleEndian.PutUint32(body[25:], 1)
	body[29] = 'b'
	body[33] = 1
	binary.LittleEndian.PutUint16(body[34:], 3)
	binary.LittleEndian.PutUint32(body[42:], 2)
	header := make([]byte, 20)
	header[0] = 3
	binary.LittleEndian.PutUint64(header[8:], entry.Wire)
	binary.LittleEndian.PutUint32(header[16:], uint32(len(entry.Layout)))
	header = append(header, entry.Layout...)
	record := make([]byte, 8)
	binary.LittleEndian.PutUint64(record, entry.Wire)
	record = append(record, body...)
	clean := append(header, record...)
	src := fmt.Sprintf(`package probe
 import ("reflect"; "testing")
 func TestWriterDead(t *testing.T) {
  t.Parallel()
  clean := []byte(%q)
  header := len(clean)-54
  for _, at := range []int{-1, 5, 13, 22, 30, 36} {
   data := append([]byte(nil), clean...)
   data = append(data, clean[header:]...)
   if at >= 0 { data[len(clean)+8+at] = 0x7f }
   values := make([]WriterDead, 2)
   for i := range values { WriterDeadReset(&values[i]); values[i].Lead=99; values[i].Trail=88 }
   before := append([]WriterDead(nil), values...)
   var report TableReport
   n := WriterDeadFixedLoad(values, data, make([]TableFixedEntry, WriterDeadFixedPlanMax), &report)
   if at >= 0 {
    if n >= 0 || report.Err()==nil || !reflect.DeepEqual(values,before) { t.Fatalf("writer dead offset %%d: n=%%d report=%%+v touched=%%v",at,n,report,!reflect.DeepEqual(values,before)) }
   } else {
    if n != 2 || report.Err()!=nil { t.Fatalf("writer control: n=%%d report=%%+v",n,report) }
    if values[0].Lead!=1 || values[0].Trail!=2 || values[0].ValsCount!=1 || values[0].Vals[0]!=7 || values[0].OptPresent || values[0].Pick.Narrow!=3 { t.Fatalf("writer values: %%+v",values[0]) }
    out:=make([]byte, WriterDeadFixedMeasure(2))
    if WriterDeadFixedSave(values,out)!=int64(len(out)) { t.Fatal("widened control did not save") }
    back:=make([]WriterDead,2)
    if WriterDeadFixedLoad(back,out,make([]TableFixedEntry,WriterDeadFixedPlanMax),&report)!=2 || !reflect.DeepEqual(back,values) { t.Fatal("widened control did not round trip") }
   }
  }
 }
 `, string(clean))
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// A manually supplied nonmonotone lineage cannot omit writer bytes from the
// check pass. Declared deprecation retains a field; removal and narrowing do not.
func TestFixedDeadBytesRejectRemovedWriterShape(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go test (a generated probe package)")
	cases := []struct{ name, older, reader string }{
		{"field", "fixed table OriginalWriter { keep int32\n hidden ?int32 }", "fixed table OriginalWriter { keep int32 }"},
		{"union-arm", "union Choice { narrow int16\n wide int64 }\nfixed table OriginalWriter { pick Choice }", "union Choice { wide int64 }\nfixed table OriginalWriter { pick Choice }"},
		{"enum-variant", "enum Key { A, B }\nfixed table OriginalWriter { key Key }", "enum Key { A }\nfixed table OriginalWriter { key Key }"},
		{"keyed-slot", "enum Key { A, B }\nfixed table OriginalWriter { values [Key]int32 }", "enum Key { A }\nfixed table OriginalWriter { values [Key]int32 }"},
		{"array-span", "fixed table OriginalWriter { values [..3]int16 }", "fixed table OriginalWriter { values [..2]int16 }"},
		{"text-span", "fixed table OriginalWriter { text string(8) }", "fixed table OriginalWriter { text string(4) }"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			older := "package probe\n" + strings.Replace(c.older, "fixed table OriginalWriter {", "fixed table OriginalWriter { sentinel int32\n", 1)
			reader := "package probe\n" + strings.Replace(c.reader, "fixed table OriginalWriter {", "fixed table OriginalWriter { sentinel int32\n", 1)
			e, ok := FixedLineageOf(unitOf(t, older), "OriginalWriter")
			if !ok {
				t.Fatal("writer lineage missing")
			}
			data := make([]byte, 20)
			data[0] = 3
			binary.LittleEndian.PutUint64(data[8:], e.Wire)
			binary.LittleEndian.PutUint32(data[16:], uint32(len(e.Layout)))
			data = append(data, e.Layout...)
			rec := make([]byte, e.Record)
			binary.LittleEndian.PutUint64(rec, e.Wire)
			if c.name == "field" {
				rec[17] = 7
			}
			data = append(data, rec...)
			src := fmt.Sprintf(`package probe
 import ("reflect"; "testing")
 func TestRemovedWriterShape(t *testing.T) {
  t.Parallel()
  data:=[]byte(%q)
  values:=make([]OriginalWriter,2)
  for i:=range values { OriginalWriterReset(&values[i]); values[i].Sentinel=99 }
  before:=append([]OriginalWriter(nil),values...)
  var report TableReport
  n:=OriginalWriterFixedLoad(values,data,make([]TableFixedEntry,4096),&report)
  if n>=0 || report.Err()==nil || report.Unknown!=0 || report.Widened!=0 { t.Fatalf("removed writer shape read: n=%%d report=%%+v",n,report) }
  if !reflect.DeepEqual(values,before) { t.Fatal("refusal changed destination") }
 }
 `, string(data))
			out, err := runVersionProbe(t, reader, []string{older}, src)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
>>>>>>> f73f6cf561aef1c3ed01dd843e2317f30431362b
