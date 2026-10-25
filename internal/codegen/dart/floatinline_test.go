package dart_test

import (
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/darttable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestDartFloatBitsAreAtTheAssignment pins issue 404 (PORTING M3): a float32
// never crosses a conversion call in generated Dart. Under the JIT on the
// pinned SDK (3.13.2) the out-of-line helpers boxed one double per field per
// pass whatever their @pragma('vm:prefer-inline') said — make
// tables-dart-float-jit measures it — so the packet emitter and the darttable
// block and message emitters spell the conversion inline at each assignment,
// through the typed-data views, with the helper's NaN rules (float32 cells
// are bit transparent: TestDartFloat32CellsAreBitTransparent is the law), and
// the helper itself is gone from the output. Pure Go over generated text.
func TestDartFloatBitsAreAtTheAssignment(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{}

	// the packet codec: the examples unit generated/dart is made from
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "examples", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("examples/*.schema: %v (%d files)", err, len(paths))
	}
	sort.Strings(paths)
	examples := unitOf(t, paths...)
	packet, err := dart.Generate(examples)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)

	// a block library and a message library holding float32 scalars, a
	// fixed float32 array and float32 inside an inline struct
	probe := unitFromSource(t, "package probe\n"+
		"type V { x float32\n y float32 }\n"+
		"type P { a float32\n b [3]float32\n v V\n c [..4]float32 }\n"+
		"fixed table Cell { value float32 = 2\n other float32 }\n"+
		"fixed table Root { cells [..2]Cell }\n")
	packetProbe, err := dart.Generate(probe)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetProbe)
	tables, err := darttable.Generate(probe)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	for _, want := range []string{"Types.dart", "Probe.dart", "ProbeBlock.dart", "ProbeFixed.dart"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("generated set has no %s (have %v)", want, keys(files))
		}
	}
	var sawPacketF32, sawBlockF32, sawMessageF32 bool
	for _, name := range keys(files) {
		text := string(files[name])
		for _, helper := range []string{"_doubleFromFloat32Bits", "_float32BitsFromDouble"} {
			if strings.Contains(text, helper) {
				t.Errorf("%s: %s is still in the output — the float32 conversion must be at the assignment", name, helper)
			}
		}
		switch {
		case strings.HasSuffix(name, "Block.dart"):
			sawBlockF32 = sawBlockF32 || strings.Contains(text, "final bits = view.getUint32(")
		case strings.HasSuffix(name, "Fixed.dart"):
			sawMessageF32 = sawMessageF32 || strings.Contains(text, "TableBitWriter._float32Words[0]")
		default:
			sawPacketF32 = sawPacketF32 || strings.Contains(text, "_f32[0] = value.")
		}
	}
	// the inline spelling is really there (a probe that generated no float32
	// code at all would pass the absence checks vacuously)
	if !sawPacketF32 || !sawBlockF32 || !sawMessageF32 {
		t.Errorf("inline float32 conversion missing: packet=%v block=%v message=%v", sawPacketF32, sawBlockF32, sawMessageF32)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func unitFromSource(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func unitOf(t *testing.T, paths ...string) *ir.Unit {
	t.Helper()
	var sources []check.SourceFile
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(p, src)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", p, perrs[0])
		}
		base := strings.TrimSuffix(filepath.Base(p), ".schema")
		sources = append(sources, check.SourceFile{Path: p, Name: filepath.Base(p), Base: base, Bytes: src, AST: f})
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}
