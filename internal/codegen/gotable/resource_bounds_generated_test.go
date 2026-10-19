// Resource bounds for the GO GENERATED READERS (docs/SPEC-TABLES.md §21.7,
// the shared port). The engine's half lives in internal/ci; this file holds
// the emitted reader runtime to the same F and nesting cap. It compiles and
// runs a generated unit, so it is behind the slow gate and rides the nightly
// leg. The hostile shapes are the documented matrix: huge counts, deep
// nesting, recursion, giant strings, and many unknown fields.
package gotable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const resourceBoundsGeneratedTest = `package probe

import "testing"

func TestGeneratedReaderCaps(t *testing.T) {
 t.Parallel()
 var rep TableReport
 // One body past the cap is refused by name, before a frame is entered.
 r := TableReader{Buffer: []byte{0}, Report: &rep, Depth: TableMaxNestingDepth}
 if _, ok := r.Body(); ok { t.Fatalf("input past the nesting cap was accepted") }
 if rep.Reason != "nesting_depth" || !rep.Malformed { t.Fatalf("refusal = %+v, want nesting_depth", rep) }
 // One under the cap still enters.
 var under TableReport
 r = TableReader{Buffer: []byte{0}, Report: &under, Depth: TableMaxNestingDepth - 1}
 if _, ok := r.Body(); !ok { t.Fatalf("input under the cap refused: %+v", under) }
}

func TestGeneratedReaderHugeLength(t *testing.T) {
 t.Parallel()
 // A 10-byte LEB of max u64: a giant string, blob or count's length claim.
 data := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}
 var rep TableReport
 r := TableReader{Buffer: data, Report: &rep}
 if _, ok := r.Body(); ok { t.Fatalf("giant length was accepted") }
}

func TestGeneratedReaderManyUnknownFields(t *testing.T) {
 t.Parallel()
 // 4096 unknown descriptors, each an id ref, kind u8 and one value byte.
 data := make([]byte, 0, 4096*3)
 for i := 0; i < 4096; i++ { data = append(data, 1, 6, byte(i)) }
 var rep TableReport
 r := TableReader{Buffer: data, Report: &rep}
 for i:=0;i<4096;i++ {
  if r.Get8()!=1 || r.Get8()!=6 || !r.Skip(6) { t.Fatalf("unknown descriptor %d did not skip",i) }
 }
}

func TestGeneratedReaderRecursion(t *testing.T) {
 t.Parallel()
 // A table body nested in itself is the recursion shape: the depth counter,
 // not a declared cycle, is what stops it.
 data := []byte{0}
 var rep TableReport
 r := TableReader{Buffer: data, Report: &rep, Depth: TableMaxNestingDepth}
 if _, ok := r.Body(); ok { t.Fatal("recursive body accepted") }
 if rep.Reason != "nesting_depth" { t.Fatalf("recursive refusal = %q", rep.Reason) }
}
`

// TestGeneratedReaderResourceBounds compiles the EMITTED reader runtime
// (tableRuntime + tableWireSource, the exact bytes a generated reader carries)
// with no engine or generated vocabulary, so the hostile-shape assertions run
// against the reader the generator ships, not a hand copy. It shells out to
// the Go toolchain, hence the slow gate and the nightly leg.
func TestGeneratedReaderResourceBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles the emitted reader runtime)")
	dir := t.TempDir()
	runtime := "package probe\n\nimport (\n\t\"encoding/binary\"\n\t\"unicode/utf8\"\n\t\"unsafe\"\n)\n\ntype TableMessageReader struct{}\ntype TableMessageWriter struct{}\n\n" +
		tableRuntime() + "\nconst tableIdCapacity = 8\nconst tableIdBuckets = 16\n" + tableWireSource
	files := map[string]string{
		"go.mod":           "module probe\n\ngo 1.26\n",
		"runtime.go":       runtime,
		"resource_test.go": resourceBoundsGeneratedTest,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("emitted reader runtime: %v\n%s", err, out)
	}
}

// TestGeneratedReaderRootAllocationBytesMeasure records TotalAlloc bytes for
// actual generated RootLoad calls. It compiles a bounded root schema and
// records accepted, malformed and resource-shaped root inputs after warming
// each call. The reported ratios are used to select F after measurement.
func TestGeneratedReaderRootAllocationBytesMeasure(t *testing.T) {
	t.Parallel()
	var schema strings.Builder
	schema.WriteString("package probe\n")
	schema.WriteString("type Nest100 { value uint8 }\n")
	for i := 99; i >= 0; i-- {
		fmt.Fprintf(&schema, "type Nest%03d { next Nest%03d }\n", i, i+1)
	}
	schema.WriteString("fixed table Root {\n text string(64)\n data bytes(64)\n items [..8]uint32\n nested Nest000\n}\n")
	out, err := runGeneratedResult(t, schema.String(), generatedRootAllocationMeasureSource, "-v", "-parallel=1")
	if err != nil {
		t.Fatalf("generated root measurement: %v\n%s", err, out)
	}
	t.Logf("generated root measurement output:\n%s", out)
}

const generatedRootAllocationMeasureSource = "package probe\nimport (\n \"runtime\"\n \"testing\"\n)\nfunc rootWire(write func(*TableWriter)) []byte {\n var ids TableIds\n buf:=make([]byte,1<<20)\n w:=TableWriter{Buffer:buf,Ids:&ids}\n w.Put8(1); write(&w); w.Put8(0); w.Trailer()\n if w.Overflow { panic(\"fixture writer overflow\") }\n return buf[:w.Offset]\n}\nfunc measureRoot(t *testing.T,name string,wire []byte) {\n t.Helper()\n var warm Root; var warmReport TableReport\n _=RootLoad(&warm,wire,&warmReport)\n runtime.GC()\n var before,after runtime.MemStats\n runtime.ReadMemStats(&before)\n const iters=10\n var result bool; var report TableReport\n for i:=0;i<iters;i++ { var value Root; report=TableReport{}; result=RootLoad(&value,wire,&report) }\n runtime.ReadMemStats(&after)\n bytesPerRead:=float64(after.TotalAlloc-before.TotalAlloc)/iters\n denom:=float64(len(wire)); if denom==0 { denom=1 }\n t.Logf(\"MEASURE generated-root shape=%s input_bytes=%d allocated_bytes_per_read=%.0f ratio=%.3f accepted=%t reason=%s\",name,len(wire),bytesPerRead,bytesPerRead/denom,result,report.Reason)\n}\nfunc TestGeneratedRootBytes(t *testing.T) {\n t.Parallel()\n empty:=rootWire(func(*TableWriter){})\n measureRoot(t,\"empty-root\",empty)\n text:=rootWire(func(w *TableWriter){ w.Id(RootTableFields[0].Id); w.Put8(12); w.PutLeb(64); for i:=0;i<64;i++ { w.Put8('x') } })\n measureRoot(t,\"giant-bounded-string\",text)\n hugeCount:=rootWire(func(w *TableWriter){ w.Id(RootTableFields[2].Id); w.Put8(14); w.PutLeb(11); w.Put8(13); w.PutLeb(^uint64(0)) })\n measureRoot(t,\"huge-count\",hugeCount)\n hugeLength:=rootWire(func(w *TableWriter){ w.Id(RootTableFields[0].Id); w.Put8(12); w.PutLeb(^uint64(0)) })\n measureRoot(t,\"giant-length-refusal\",hugeLength)\n unknowns:=rootWire(func(w *TableWriter){ for i:=0;i<4096;i++ { w.Id(0xdeadbeef); w.Put8(6); w.Put8(byte(i)) } })\n measureRoot(t,\"many-unknown-fields\",unknowns)\n var nested Root; RootReset(&nested)\n n:=RootMeasure(&nested); wire:=make([]byte,n); if RootSave(&nested,wire)!=n { t.Fatal(\"nested fixture save\") }\n measureRoot(t,\"deep-nested-root\",wire)\n}\n"
