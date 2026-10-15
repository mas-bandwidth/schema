package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var allSurfaces = []string{"wire", "fixed", "json", "cook", "block", "view", "message", "retain"}

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the --surface contract
// (docs/SPEC-TABLES.md §8.4): a surface list emits only the surfaces it names,
// the default equals the full set byte-for-byte, dependencies are resolved, and
// every generated set compiles and round-trips.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
flags Caps { Jump, Fly }
enum Grade { Gold, Silver }
type Badge { title string(8) }
fixed table Root {
	title string(12)
	caps Caps = { Jump }
	badge Badge
	values ?[..3]int32
	grades ?[2]Grade
	ships [2]Grade
}
table Bag { items []int32 }
`)

	full, err := GenerateSurfaces(u, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	all, err := GenerateSurfaces(u, nil, allSurfaces)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full, all) {
		t.Fatalf("default generate is not byte-identical to the explicit all-surfaces generate")
	}

	for _, s := range allSurfaces {
		sub, err := GenerateSurfaces(u, nil, []string{s})
		if err != nil {
			t.Fatalf("surface %s: %v", s, err)
		}
		assertSubset(t, s, sub, full)
	}

	// json needs wire (dependency): json alone emits the wire's files and its
	// own text file.
	jsn, err := GenerateSurfaces(u, nil, []string{"json"})
	if err != nil {
		t.Fatal(err)
	}
	wireOnly, err := GenerateSurfaces(u, nil, []string{"wire"})
	if err != nil {
		t.Fatal(err)
	}
	for fname := range wireOnly {
		if _, ok := jsn[fname]; !ok {
			t.Fatalf("json does not imply wire: missing file %s", fname)
		}
	}
	fullBytes := totalBytes(full)
	if totalBytes(jsn) >= fullBytes {
		t.Fatalf("json-only %d bytes is not below full %d", totalBytes(jsn), fullBytes)
	}
	if totalBytes(wireOnly) >= fullBytes {
		t.Fatalf("wire-only %d bytes is not below full %d", totalBytes(wireOnly), fullBytes)
	}

	// the full default set compiles and round-trips.
	compileAndRoundTrip(t, u, nil)
	// a wire-only set compiles.
	compileSurfaces(t, u, []string{"wire"})
}

func assertSubset(t *testing.T, name string, sub, full map[string][]byte) {
	t.Helper()
	for fname := range sub {
		if _, ok := full[fname]; !ok {
			t.Fatalf("surface %s emits file %s that the full generate does not", name, fname)
		}
	}
	if len(sub) > len(full) {
		t.Fatalf("surface %s emits %d files, more than the full %d", name, len(sub), len(full))
	}
	if totalBytes(sub) > totalBytes(full) {
		t.Fatalf("surface %s emits %d bytes, more than the full %d", name, totalBytes(sub), totalBytes(full))
	}
}

func totalBytes(files map[string][]byte) int {
	n := 0
	for _, data := range files {
		n += len(data)
	}
	return n
}

func compileSurfaces(t *testing.T, u *ir.Unit, surfaces []string) {
	t.Helper()
	compileGenerated(t, u, surfaces, "package probe\nimport(\"testing\")\nfunc TestCompile(t *testing.T){}\n")
}

func compileAndRoundTrip(t *testing.T, u *ir.Unit, surfaces []string) {
	t.Helper()
	compileGenerated(t, u, surfaces, `package probe
import("testing")
func TestRoundTrip(t *testing.T) {
	var v Root; RootReset(&v)
	var r TableReport
	b:=make([]byte,RootMeasure(&v))
	if RootSave(&v,b)!=int64(len(b)){t.Fatal("save")}
	var got Root
	if !RootLoad(&got,b,&r){t.Fatal("load")}
}
`)
}

func compileGenerated(t *testing.T, u *ir.Unit, surfaces []string, testSource string) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateSurfaces(u, nil, surfaces)
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
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}
