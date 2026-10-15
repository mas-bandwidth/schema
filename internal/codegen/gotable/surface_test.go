package gotable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the Go `--surface` contract
// (docs/SPEC-TABLES.md §8.4): a generate that names a surface emits only that
// surface's files (resolving dependencies), the default emits the full set
// byte for byte, and a selected surface's output still compiles and
// round-trips.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
enum Grade { Gold, Silver }
type Badge { title string(8) }
fixed table Root {
	name string(12)
	badge Badge
	grades ?[2]Grade
}
`)
	full := map[string][]byte{}
	withSurfaces(t, u, AllSurfaces(), func(files map[string][]byte) {
		full = files
	})

	def := map[string][]byte{}
	withSurfaces(t, u, SurfaceSetFromStringOrDie(""), func(files map[string][]byte) {
		def = files
	})
	if len(def) == 0 {
		t.Fatal("default generate emitted nothing")
	}
	for name, data := range full {
		if !bytes.Equal(def[name], data) {
			t.Fatalf("default generate differs from the full set: %s", name)
		}
	}
	for name, data := range def {
		if !bytes.Equal(full[name], data) {
			t.Fatalf("default generate differs from the full set: %s", name)
		}
	}

	cases := []struct {
		name        string
		surfaces    SurfaceSet
		want        []string // files that must be present (suffix match)
		notWant     []string // files that must be absent (suffix match)
		roundTripOK bool
	}{
		{name: "wire", surfaces: only(SurfaceWire), notWant: []string{"Block.go", "Cook.go", "TableJson.go", "View.go"}, roundTripOK: true},
		{name: "block", surfaces: only(SurfaceBlock), want: []string{"Block.go"}, notWant: []string{"Cook.go", "TableJson.go", "View.go"}},
		{name: "cook", surfaces: only(SurfaceCook), want: []string{"Cook.go"}, notWant: []string{"Block.go", "TableJson.go", "View.go"}},
		{name: "json", surfaces: only(SurfaceJSON), want: []string{"TableJson.go"}, notWant: []string{"Block.go", "Cook.go", "View.go"}},
		{name: "view", surfaces: only(SurfaceView), want: []string{"View.go"}, notWant: []string{"Block.go", "Cook.go", "TableJson.go"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := map[string][]byte{}
			withSurfaces(t, u, tc.surfaces, func(files map[string][]byte) {
				got = files
			})
			if len(got) == 0 {
				t.Fatalf("surface %s emitted nothing", tc.name)
			}
			for _, suf := range tc.want {
				if !hasSuffixFile(got, suf) {
					t.Fatalf("surface %s should emit %s; got files: %s", tc.name, suf, fileNames(got))
				}
			}
			for _, suf := range tc.notWant {
				if hasSuffixFile(got, suf) {
					t.Fatalf("surface %s must not emit %s; got files: %s", tc.name, suf, fileNames(got))
				}
			}
			if tc.roundTripOK {
				compileAndRoundTrip(t, u, tc.surfaces)
			}
		})
	}
}

func hasSuffixFile(files map[string][]byte, suf string) bool {
	for name := range files {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
}

func fileNames(files map[string][]byte) []string {
	var out []string
	for name := range files {
		out = append(out, name)
	}
	return out
}

func withSurfaces(t *testing.T, u *ir.Unit, s SurfaceSet, fn func(map[string][]byte)) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineageSelected(u, nil, s)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}
	fn(files)
}

func compileAndRoundTrip(t *testing.T, u *ir.Unit, s SurfaceSet) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineageSelected(u, nil, s)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}
	testSource := `package probe
import("testing")
func TestWireRoundTrip(t *testing.T){
	var v Root; RootReset(&v)
	v.Name = "abc"
	b:=make([]byte,RootMeasure(&v)); if RootSave(&v,b)!=int64(len(b)){t.Fatal("save")}
	var got Root; var r TableReport
	if !RootLoad(&got,b,&r)||r!=(TableReport{})||string(got.Name[:got.NameLength])!="abc"{t.Fatalf("round trip: %+v %+v",got,r)}
}
`
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["surface_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated surface does not compile or round-trip: %v\n%s", err, out)
	}
}

func only(bit SurfaceSet) SurfaceSet { return bit }

func SurfaceSetFromStringOrDie(s string) SurfaceSet {
	if s == "" {
		return AllSurfaces()
	}
	set, err := SurfaceSetFromString(s)
	if err != nil {
		panic(err)
	}
	return set
}
