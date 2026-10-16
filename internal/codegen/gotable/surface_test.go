package gotable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var surfaceSchema = `package probe
flags Caps { Jump, Fly }
enum Grade {
 Gold | was = "OldGold"
}
type Badge {
 title string(8) = "fresh" | was = "label"
}
fixed table Root {
 title string(12) = "default"
 tag bytes(4) = "ab"
 caps Caps = { Jump, Fly }
 badge Badge
 grade Grade
 values ?[..3]int32
 grades ?[2]Grade
}
`

var surfaceVariableSchema = `package probe
flags Caps { Jump, Fly }
enum Grade {
 Gold | was = "OldGold"
}
type Badge {
 title string(8) = "fresh" | was = "label"
}
table VarRoot {
 id int32 = 1
 label string(32) = "hello"
 badge Badge
 grade Grade
 items []int32
 scores map[int32]int32
}
`

// surfaceFiles maps each file-owning surface to the distinctive path suffix
// the generated tree carries when and only when the surface is on.
var surfaceFiles = map[string]string{
	"json":    "TableJson.go",
	"cook":    "Cook.go",
	"block":   "Block.go",
	"view":    "View.go",
	"message": "TableMessage.go",
}

func genWithSurfaces(t *testing.T, u *ir.Unit, s Surfaces) map[string][]byte {
	t.Helper()
	out, err := GenerateLineageSurface(u, nil, s)
	if err != nil {
		t.Fatalf("GenerateLineageSurface(%+v): %v", s, err)
	}
	return out
}

func tableModule(out map[string][]byte) string {
	var b strings.Builder
	for name, data := range out {
		if strings.HasSuffix(name, "Table.go") {
			b.Write(data)
		}
	}
	return b.String()
}

func assertSameOutput(t *testing.T, label string, want, got map[string][]byte) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: want %d files, got %d", label, len(want), len(got))
	}
	for name, data := range want {
		if other, ok := got[name]; !ok {
			t.Errorf("%s: missing %s", label, name)
		} else if !bytes.Equal(data, other) {
			t.Errorf("%s: %s differs byte-for-byte", label, name)
		}
	}
}

func assertCompilesAndRoundTrips(t *testing.T, u *ir.Unit, s Surfaces, probeTest string) {
	t.Helper()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtime, "go.mod")); err != nil {
		t.Fatalf("sibling serialize.go runtime not staged at %s: %v", runtime, err)
	}
	files := genWithSurfaces(t, u, s)
	packet, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	dir := t.TempDir()
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.24\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => %s\n", runtime))
	files["probe_test.go"] = []byte(probeTest)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated surface %+v does not compile/round-trip: %v\n%s", s, err, outBytes)
	}
}

const fixedProbeTest = `package probe

import "testing"

func TestWireRoundTrip(t *testing.T) {
	var src Root
	RootReset(&src)
	copy(src.Title[:], "custom")
	src.ValuesPresent = true
	src.ValuesCount = 2
	src.Values[0] = 42
	src.Values[1] = -7
	n := RootMeasure(&src)
	if n <= 0 {
		t.Fatalf("RootMeasure = %d", n)
	}
	buf := make([]byte, n)
	if wrote := RootSave(&src, buf); wrote != n {
		t.Fatalf("RootSave = %d, want %d", wrote, n)
	}
	var dst Root
	var loadRep TableReport
	if !RootLoad(&dst, buf, &loadRep) {
		t.Fatalf("RootLoad failed: %+v", loadRep)
	}
	if dst.Title != src.Title || !dst.ValuesPresent || dst.ValuesCount != 2 || dst.Values[0] != 42 || dst.Values[1] != -7 {
		t.Fatalf("round-trip mismatch: title=%v values=%v/%d/%v", dst.Title, dst.ValuesPresent, dst.ValuesCount, dst.Values)
	}
}
`

const variableProbeTest = `package probe

import (
	"testing"
	"unsafe"
)

func TestWireRoundTrip(t *testing.T) {
	var b VarRootBuilder
	if !b.Init() {
		t.Fatal("VarRootBuilder.Init failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	root.Id = 77
	n := VarRootMeasure(root, &b.Arena)
	if n <= 0 {
		t.Fatalf("VarRootMeasure = %d", n)
	}
	wire := make([]byte, n)
	if wrote := VarRootSave(root, wire, &b.Arena); wrote != n {
		t.Fatalf("VarRootSave = %d, want %d", wrote, n)
	}
	size := VarRootLoadMeasure(wire)
	if size <= 0 {
		t.Fatalf("VarRootLoadMeasure = %d", size)
	}
	raw := make([]byte, size+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	region := raw[off : off+uintptr(size)]
	var loadRep TableReport
	loaded := VarRootLoad(region, wire, &loadRep)
	if loaded == nil {
		t.Fatalf("VarRootLoad failed: %+v", loadRep)
	}
	if loaded.Id != 77 || loaded.Label != root.Label {
		t.Fatalf("round-trip mismatch: id=%d label=%v", loaded.Id, loaded.Label)
	}
}
`

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the --surface flag
// (docs/SPEC-TABLES.md §8.8): each surface list emits only its files and
// functions, the default equals Generate(u) and the explicit 8-surface list
// byte for byte, and every selection compiles and round-trips on the wire.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	uFixed := unitFrom(t, surfaceSchema)
	uVar := unitFrom(t, surfaceVariableSchema)

	parsedAll, err := ParseSurfaces("wire,fixed,json,cook,block,view,message,retain")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		u    *ir.Unit
	}{
		{"fixed", uFixed},
		{"variable", uVar},
	} {
		baseOut, err := Generate(tc.u)
		if err != nil {
			t.Fatalf("%s Generate: %v", tc.name, err)
		}
		defOut := genWithSurfaces(t, tc.u, DefaultSurfaces())
		allOut := genWithSurfaces(t, tc.u, parsedAll)
		assertSameOutput(t, tc.name+" Generate vs DefaultSurfaces", baseOut, defOut)
		assertSameOutput(t, tc.name+" Generate vs explicit all-8", baseOut, allOut)
	}

	cases := []string{
		"wire",
		"fixed",
		"json",
		"cook",
		"block",
		"view",
		"message",
		"retain",
		"wire,json",
		"wire,fixed",
		"wire,retain",
		"wire,message",
		"wire,retain,message",
		"json,cook,block",
	}

	for _, list := range cases {
		list := list
		s, err := ParseSurfaces(list)
		if err != nil {
			t.Fatalf("ParseSurfaces(%q): %v", list, err)
		}
		if !s.Wire {
			t.Fatalf("ParseSurfaces(%q): Wire must always be true", list)
		}
		t.Run("fixed/"+list, func(t *testing.T) {
			t.Parallel()
			out := genWithSurfaces(t, uFixed, s)
			body := tableModule(out)
			if body == "" {
				t.Fatal("the wire table module (<Base>Table.go) is always emitted")
			}
			wantFile := map[string]bool{
				"json":    s.JSON,
				"cook":    s.Cook,
				"block":   s.Block,
				"view":    s.View,
				"message": s.Message,
			}
			for name, suffix := range surfaceFiles {
				got := false
				for f := range out {
					if strings.HasSuffix(f, suffix) {
						got = true
					}
				}
				if got != wantFile[name] {
					t.Errorf("surface %q: file *%s present=%v, want %v", list, suffix, got, wantFile[name])
				}
			}
			if strings.Contains(body, "RootFixedPlanMax") != s.Fixed {
				t.Errorf("surface %q: RootFixedPlanMax present=%v, want %v", list, !s.Fixed, s.Fixed)
			}
			if strings.Contains(body, "RootLoadRetain") != s.Retain {
				t.Errorf("surface %q: RootLoadRetain present=%v, want %v", list, !s.Retain, s.Retain)
			}
			assertCompilesAndRoundTrips(t, uFixed, s, fixedProbeTest)
		})
		t.Run("variable/"+list, func(t *testing.T) {
			t.Parallel()
			out := genWithSurfaces(t, uVar, s)
			body := tableModule(out)
			if body == "" {
				t.Fatal("the wire table module (<Base>Table.go) is always emitted")
			}
			if strings.Contains(body, "VarRootLoadRetain") != s.Retain {
				t.Errorf("surface %q: VarRootLoadRetain present=%v, want %v", list, !s.Retain, s.Retain)
			}
			assertCompilesAndRoundTrips(t, uVar, s, variableProbeTest)
		})
	}

	if _, err := ParseSurfaces("bogus"); err == nil {
		t.Error("ParseSurfaces(bogus) should error")
	}
}
