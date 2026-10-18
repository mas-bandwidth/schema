package gotable

import (
	"bytes"
	"testing"
)

// TestStrictReadAllocatesByInput pins the strict read's arena to its input:
// ReadStrict and LoadText over a small variable table allocate less than a
// quarter megabyte per call, not the whole 1 MiB segment a Builder.Init takes.
func TestStrictReadAllocatesByInput(t *testing.T) {
	t.Parallel()
	out, err := runGeneratedResult(t, strictReadAllocSchema, strictReadAllocProbe, "-race", "-v")
	if err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
	t.Logf("%s", out)
	if !bytes.Contains(out, []byte("READSTRICT bytes/op=")) || !bytes.Contains(out, []byte("LOADTEXT bytes/op=")) {
		t.Fatalf("the probe did not print both allocation numbers\n%s", out)
	}
}

const strictReadAllocSchema = `package probe
table Part {
 v int32
}
table Root {
 a int32
 b *bytes
 c string(8)
 parts []*Part
}
`

const strictReadAllocProbe = `package probe
import("bytes";"encoding/base64";"fmt";"runtime";"sync";"testing";"unsafe")

func smallWire(t *testing.T) []byte {
 t.Helper()
 var b RootBuilder
 if !b.Init() { t.Fatal("init") }
 r := b.GetRoot()
 r.A = 7
 blob := TableBytesEmplace(&b.Main, &r.B, 6)
 if blob == nil { t.Fatal("blob") }
 copy(blob, []byte("abcdef"))
 n := RootMeasure(b.GetRoot(), &b.Arena)
 if n < 0 { t.Fatal("measure") }
 wire := make([]byte, n)
 if RootSave(b.GetRoot(), wire, &b.Arena) != n { t.Fatal("save") }
 b.Shutdown()
 return wire
}

func spanWire(t *testing.T) []byte {
 t.Helper()
 var b RootBuilder
 if !b.Init() { t.Fatal("init") }
 r := b.GetRoot()
 r.A = 9
 blob := TableBytesEmplace(&b.Main, &r.B, 200*1024)
 if blob == nil { t.Fatal("span") }
 for i := range blob { blob[i] = byte(i*13 + 5) }
 n := RootMeasure(b.GetRoot(), &b.Arena)
 if n < 0 { t.Fatal("measure") }
 wire := make([]byte, n)
 if RootSave(b.GetRoot(), wire, &b.Arena) != n { t.Fatal("save") }
 b.Shutdown()
 return wire
}

func partsWire(t *testing.T) []byte {
 t.Helper()
 var b RootBuilder
 if !b.Init() { t.Fatal("init") }
 defer b.Shutdown()
 text := []byte("{\"parts\":[")
 for i := 0; i < 3000; i++ {
  if i > 0 { text = append(text, ',') }
  text = append(text, []byte(fmt.Sprintf("{\"v\":%d}", i))...)
 }
 text = append(text, []byte("]}")...)
 var report TableReport
 if !RootFromJson(&b, text, &report) || report != (TableReport{}) { t.Fatalf("FromJson: %+v", report) }
 n := RootMeasure(b.GetRoot(), &b.Arena)
 if n < 0 { t.Fatal("measure") }
 wire := make([]byte, n)
 if RootSave(b.GetRoot(), wire, &b.Arena) != n { t.Fatal("save") }
 return wire
}

func bytesPerRead(t *testing.T, read func()) uint64 {
 t.Helper()
 var before, after runtime.MemStats
 runtime.ReadMemStats(&before)
 for i := 0; i < 20; i++ { read() }
 runtime.ReadMemStats(&after)
 return (after.TotalAlloc - before.TotalAlloc) / 20
}

func jsonOf(t *testing.T, v *Root) []byte {
 t.Helper()
 n := RootToJsonMeasure(v)
 if n < 0 { t.Fatal("tojson measure") }
 out := make([]byte, n)
 if RootToJson(v, out) != n { t.Fatal("tojson") }
 return out
}

func loadValue(t *testing.T, wire []byte) *Root {
 t.Helper()
 n := RootLoadMeasure(wire)
 if n < 0 { t.Fatal("load measure") }
 raw := make([]byte, n+63)
 off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
 var report TableReport
 v := RootLoad(raw[off:off+uintptr(n)], wire, &report)
 if v == nil || report != (TableReport{}) { t.Fatalf("load: %+v", report) }
 return v
}

func resave(t *testing.T, v *Root) []byte {
 t.Helper()
 n := RootMeasure(v)
 if n < 0 { t.Fatal("resave measure") }
 out := make([]byte, n)
 if RootSave(v, out) != n { t.Fatal("resave") }
 return out
}

func textOf(v *Root) ([]byte, error) {
 n := RootToJsonMeasure(v)
 if n < 0 { return nil, fmt.Errorf("tojson measure %d", n) }
 out := make([]byte, n)
 if RootToJson(v, out) != n { return nil, fmt.Errorf("tojson short") }
 return out, nil
}

func TestStrictReadAllocatesByInput(t *testing.T) {
 t.Parallel()
 wire := smallWire(t)
 var sink *Root
 readStrict := bytesPerRead(t, func() {
  v, err := RootReadStrict(wire)
  if err != nil { t.Fatalf("ReadStrict: %v", err) }
  sink = v
 })
 text := base64.StdEncoding.EncodeToString(wire)
 loadText := bytesPerRead(t, func() {
  v, err := RootLoadText(text, nil)
  if err != nil { t.Fatalf("LoadText: %v", err) }
  sink = v
 })
 _ = sink
 t.Logf("READSTRICT bytes/op=%d wire=%d", readStrict, len(wire))
 t.Logf("LOADTEXT bytes/op=%d wire=%d", loadText, len(wire))
 if readStrict >= 262144 { t.Fatalf("ReadStrict allocated %d bytes per op, want under 262144", readStrict) }
 if loadText >= 262144 { t.Fatalf("LoadText allocated %d bytes per op, want under 262144", loadText) }

 strict, err := RootReadStrict(wire)
 if err != nil { t.Fatal(err) }
 textValue, err := RootLoadText(text, nil)
 if err != nil { t.Fatal(err) }
 regionValue := loadValue(t, wire)
 want := jsonOf(t, strict)
 if got := jsonOf(t, regionValue); !bytes.Equal(got, want) { t.Fatalf("Load text %s want %s", got, want) }
 if got := jsonOf(t, textValue); !bytes.Equal(got, want) { t.Fatalf("LoadText text %s want %s", got, want) }

 span := spanWire(t)
 sv, err := RootReadStrict(span)
 if err != nil { t.Fatalf("span ReadStrict: %v", err) }
 if !bytes.Equal(resave(t, sv), span) { t.Fatal("span read did not round-trip byte for byte") }
 st, err := RootLoadText(base64.StdEncoding.EncodeToString(span), nil)
 if err != nil { t.Fatalf("span LoadText: %v", err) }
 if !bytes.Equal(resave(t, st), span) { t.Fatal("span LoadText did not round-trip byte for byte") }

 parts := partsWire(t)
 pv, err := RootReadStrict(parts)
 if err != nil { t.Fatalf("parts ReadStrict: %v", err) }
 if !bytes.Equal(resave(t, pv), parts) { t.Fatal("parts read did not round-trip byte for byte") }
 pt, err := RootLoadText(base64.StdEncoding.EncodeToString(parts), nil)
 if err != nil { t.Fatalf("parts LoadText: %v", err) }
 if !bytes.Equal(resave(t, pt), parts) { t.Fatal("parts LoadText did not round-trip byte for byte") }

 var wg sync.WaitGroup
 errs := make(chan error, 8)
 for i := 0; i < 8; i++ {
  wg.Add(1)
  go func() {
   defer wg.Done()
   v, err := RootReadStrict(wire)
   if err != nil { errs <- err; return }
   got, err := textOf(v)
   if err != nil { errs <- err; return }
   if !bytes.Equal(got, want) { errs <- fmt.Errorf("goroutine text %s want %s", got, want) }
  }()
 }
 wg.Wait()
 close(errs)
 for err := range errs { t.Error(err) }
}
`
