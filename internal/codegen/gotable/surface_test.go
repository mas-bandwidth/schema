package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// surfaceSchema is a small table unit spanning every surface: a fixed table
// with a plain field, an enum-keyed field, a flags field, and a variable field
// so the message and retain surfaces have something to carry.
const surfaceSchema = `package probe
flags Caps { Jump, Fly }
enum Grade { Gold, Silver }
fixed table Root {
	title string(8) = "hi"
	score int32 = 7
	caps Caps = { Jump }
	grade Grade = Gold
}
`

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the --surface contract:
// each surface list emits only the files and functions it names, every
// emitted surface compiles and round-trips through the generated unit, and
// the default (no --surface) is byte-identical to the full set.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, surfaceSchema)

	// The default is byte-identical to the full set, named explicitly.
	def, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	full, err := GenerateSurface(u, map[string]string{TableSurfaceKey: "wire,fixed,json,cook,block,view,message,retain"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEqualMaps(def, full) {
		t.Fatalf("default generation is not byte-identical to the explicit full set: default %d files, full %d files", len(def), len(full))
	}
	allNames := sortedNames(full)

	// Each standalone-file surface emits its own file AND the wire base, and
	// withholds the other standalone files.
	cases := []struct {
		name   string
		list   string
		has    []string // suffixes that must be present
		not    []string // suffixes that must be absent
		hasFn  string
		notFn  string
	}{
		{"json", "json", []string{"TableJson.go"}, []string{"Block.go", "Cook.go", "View.go"}, "RootMeasure", "tableFixedRun"},
		{"cook", "cook", []string{"Cook.go", "TableJson.go"}, []string{"Block.go", "View.go"}, "tableFixedRun", ""}, // cook depends on json and fixed
		{"block", "block", []string{"Block.go"}, []string{"Cook.go", "TableJson.go", "View.go"}, "tableFixedRun", ""},
		{"view", "view", []string{"View.go"}, []string{"Block.go", "Cook.go", "TableJson.go"}, "RootMeasure", "tableFixedRun"},
		{"fixed", "fixed", nil, []string{"Block.go", "Cook.go", "TableJson.go", "View.go"}, "tableFixedRun", ""},
		{"wire", "wire", nil, []string{"Block.go", "Cook.go", "TableJson.go", "View.go"}, "RootMeasure", "tableFixedRun"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := GenerateSurface(u, map[string]string{TableSurfaceKey: tc.list}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range sortedNames(got) {
				if !slicesContains(allNames, name) {
					t.Fatalf("surface %s emitted %q, not in the full set", tc.list, name)
				}
			}
			for _, suffix := range tc.has {
				if !hasSuffixFile(got, suffix) {
					t.Fatalf("surface %s does not emit a %s file (got %v)", tc.list, suffix, sortedNames(got))
				}
			}
			for _, suffix := range tc.not {
				if hasSuffixFile(got, suffix) {
					t.Fatalf("surface %s emitted a %s file it should withhold (got %v)", tc.list, suffix, sortedNames(got))
				}
			}
			// The wire base is always present, even for a surface that names
			// it nowhere.
			if !hasSuffixFile(got, "Table.go") {
				t.Fatalf("surface %s does not emit the wire base Table.go (got %v)", tc.list, sortedNames(got))
			}
			tableSrc := string(got["ProbeTable.go"])
			if tc.hasFn != "" && !strings.Contains(tableSrc, tc.hasFn) {
				t.Fatalf("surface %s did not emit %s in ProbeTable.go", tc.list, tc.hasFn)
			}
			if tc.notFn != "" && strings.Contains(tableSrc, tc.notFn) {
				t.Fatalf("surface %s leaked %s in ProbeTable.go", tc.list, tc.notFn)
			}
		})
	}

	if _, err := GenerateSurface(u, map[string]string{TableSurfaceKey: "BogusSurface"}, nil); err == nil || !strings.Contains(err.Error(), "BogusSurface") {
		t.Fatalf("expected unknown surface error naming BogusSurface, got %v", err)
	}

	// Each surface compiles and round-trips through the generated unit.
	roundtrips := []struct {
		name string
		list string
		src  string
	}{
		{"wire", "wire", surfaceWireTest},
		{"fixed", "fixed", surfaceWireTest},
		{"json", "json", surfaceJSONTest},
	}
	for _, rt := range roundtrips {
		rt := rt
		t.Run("roundtrip-"+rt.name, func(t *testing.T) {
			t.Parallel()
			runGeneratedSurface(t, u, map[string]string{TableSurfaceKey: rt.list}, rt.src)
		})
	}
}

// surfaceWireTest exercises only the wire surface: measure/save/load.
const surfaceWireTest = `package probe
import("testing")
func TestWire(t *testing.T) {
	var v, got Root
	var r TableReport
	RootReset(&v)
	v.TitleLength = 2
	copy(v.Title[:2], "hi")
	n := RootMeasure(&v)
	b := make([]byte, n)
	if RootSave(&v, b) != int64(len(b)) { t.Fatal("save") }
	if !RootLoad(&got, b, &r) || r != (TableReport{}) { t.Fatalf("load: %+v %+v", got, r) }
	if got.TitleLength != 2 || got.Score != 7 || got.Caps != 1 || got.Grade != 1 { t.Fatalf("roundtrip: %+v", got) }
	if n := testing.AllocsPerRun(10, func(){ RootMeasure(&v); RootSave(&v, b); RootLoad(&got, b, &r) }); n != 0 { t.Fatalf("allocations: %v", n) }
}
`

// surfaceJSONTest exercises the json surface on top of the wire base.
const surfaceJSONTest = `package probe
import("testing";"bytes")
func TestJSON(t *testing.T) {
	var v, got Root
	var r TableReport
	text := []byte("{\"title\":\"hi\",\"score\":7,\"caps\":[\"Jump\"],\"grade\":\"Gold\"}")
	if !RootFromJson(&v, text, &r) || r != (TableReport{}) { t.Fatalf("json: %+v", r) }
	got = v
	a, c := make([]byte, 2048), make([]byte, 2048)
	n := RootToJson(&v, a)
	if n < 0 { t.Fatal("tojson") }
	gotN := RootToJson(&got, c)
	if gotN != n || !bytes.Equal(a[:n], c[:gotN]) { t.Fatalf("roundtrip text:\n%s\n%s", a[:n], c[:gotN]) }
}
`

func sortedNames(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func hasSuffixFile(files map[string][]byte, suffix string) bool {
	for name := range files {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func slicesContains(list []string, s string) bool {
	return slices.Contains(list, s)
}

func bytesEqualMaps(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !bytesEqual(va, vb) {
			return false
		}
	}
	return true
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runGeneratedSurface compiles and runs a generated unit that carries only the
// requested surfaces, in the house pattern of runGeneratedUnit.
func runGeneratedSurface(t *testing.T, u *ir.Unit, opts map[string]string, testSource string) {
	t.Helper()
	if out, err := runGeneratedSurfaceResult(t, u, opts, testSource); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}

func runGeneratedSurfaceResult(t *testing.T, u *ir.Unit, opts map[string]string, testSource string) ([]byte, error) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateSurface(u, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
