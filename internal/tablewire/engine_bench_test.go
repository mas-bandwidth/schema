package tablewire_test

// The STEP 5 harness for bench/results/2026-11-05-strict-engine-go-engine.md.
// It uses only the public compiler, tabletext and tablewire API, so the same
// file compiles at the staged sprint tip and on the strict branch; copy it
// there and run
//
//	go test -count=1 -timeout 600s ./internal/tablewire/ \
//	  -run '^$' -bench 'BenchmarkEngine' -benchmem -benchtime=1s -count=5

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const engineBenchSchema = `package benchwire
table Bench {
  id uint32
  x float32
  y float32
  z float32
  name string(64)
  flags uint64
}
`

const engineBenchText = `{"id":42,"x":1.5,"y":-2.25,"z":3.75,"name":"the quick brown fox","flags":123456789}`

func engineBenchModel(b *testing.B) *tabletext.Model {
	b.Helper()
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Bench.schema"), []byte(engineBenchSchema), 0o644); err != nil {
		b.Fatal(err)
	}
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		b.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		b.Fatal(err)
	}
	return tabletext.NewModel(u)
}

func engineBenchInstance(b *testing.B, m *tabletext.Model) *tabletext.Instance {
	b.Helper()
	inst := m.New(m.Lookup("Bench"))
	var r tabletext.Report
	if !m.Read(inst, []byte(engineBenchText), &r) {
		b.Fatalf("place: %+v", r)
	}
	return inst
}

func BenchmarkEngineWireDecode(b *testing.B) {
	m := engineBenchModel(b)
	wire, err := tablewire.Encode(m, engineBenchInstance(b, m))
	if err != nil {
		b.Fatal(err)
	}
	target := m.New(m.Lookup("Bench"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var r tabletext.Report
		ok, err := tablewire.Decode(m, target, wire, &r)
		if !ok || err != nil {
			b.Fatalf("decode: ok=%v err=%v", ok, err)
		}
	}
}

func BenchmarkEngineWireEncode(b *testing.B) {
	m := engineBenchModel(b)
	inst := engineBenchInstance(b, m)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tablewire.Encode(m, inst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineTextRead(b *testing.B) {
	m := engineBenchModel(b)
	target := m.New(m.Lookup("Bench"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var r tabletext.Report
		if !m.Read(target, []byte(engineBenchText), &r) {
			b.Fatalf("read: %+v", r)
		}
	}
}
