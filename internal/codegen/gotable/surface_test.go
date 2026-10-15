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
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedSchema declares a FIXED table. Its full Go table surface emits the wire
// core (<Base>Table.go + <Base>TableJson.go) and the on-the-side cook and view
// forms. The block form only appears for a unit with a variable table, so it
// is exercised separately by mixSchema's full-set check.
const fixedSchema = `package probe

enum Grade { A, B, C }
union Payload
{
    a int32
    b string(8)
}

fixed table Home
{
    scale float32 = 1.0
    grade Grade
    payload Payload
}
`

// mixSchema has a variable table AND a fixed table, so its full set carries
// every generated file kind: Table.go, TableJson.go, Block.go, Cook.go, View.go.
const mixSchema = `package probe

flags Caps { Jump, Fly }
enum Grade { A, B, C }
union Payload
{
    a int32
    b string(8)
}

table Ship
{
    name string(16)
    grade Grade
    caps Caps
    payload Payload
    pos [3]float64
}

fixed table Home
{
    scale float32 = 1.0
}
`

const fixedProbe = `package probe

import "testing"

func TestSurfaceRoundTrip(t *testing.T) {
	var h Home
	h.Scale = 1.5
	n := HomeMeasure(&h)
	if n <= 0 {
		t.Fatalf("measure: %d", n)
	}
	buf := make([]byte, n)
	if HomeSave(&h, buf) <= 0 {
		t.Fatal("save")
	}
	var got Home
	var rep TableReport
	if !HomeLoad(&got, buf, &rep) {
		t.Fatalf("load: report=%+v", rep)
	}
	if got.Scale != 1.5 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}
`

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the `--surface <list>`
// contract (docs/SPEC-TABLES.md §8.4): a Go generate that names surfaces emits
// only the files those surfaces need — the wire core and the requested
// on-the-side forms — the default (no list) is the full set byte for byte,
// and every emitted set compiles and round-trips.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()

	// the default (no list) equals the full set byte for byte, on a schema
	// that emits every generated file kind
	mix := unitFrom(t, mixSchema)
	full := mustSurface(t, mix, surfaceNames)
	def := mustSurface(t, mix, nil)
	if !filesEqual(full, def) {
		t.Fatalf("default generate is not byte-identical to the full surface set")
	}
	for _, k := range []string{"Table.go", "TableJson.go", "Block.go", "Cook.go", "View.go"} {
		if !fileKinds(full)[k] {
			t.Fatalf("full set lacks %s file (kinds=%v)", k, fileKinds(full))
		}
	}

	fixed := unitFrom(t, fixedSchema)
	core := []string{"Table.go", "TableJson.go"}

	type surfaceCase struct {
		name    string
		surf    []string
		want    []string
		notWant []string
	}
	cases := []surfaceCase{
		{"wire", []string{"wire"}, core, []string{"Cook.go", "View.go"}},
		{"fixed", []string{"fixed"}, core, []string{"Cook.go", "View.go"}},
		{"json", []string{"json"}, core, []string{"Cook.go", "View.go"}},
		{"cook", []string{"cook"}, append(copyStr(core), "Cook.go"), []string{"View.go"}},
		{"block", []string{"block"}, core, []string{"Cook.go", "View.go"}},
		{"view", []string{"view"}, append(copyStr(core), "View.go"), []string{"Cook.go"}},
		{"message", []string{"message"}, core, []string{"Cook.go", "View.go"}},
		{"retain", []string{"retain"}, core, []string{"Cook.go", "View.go"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gen := mustSurface(t, fixed, tc.surf)
			kinds := fileKinds(gen)
			for _, k := range tc.want {
				if !kinds[k] {
					t.Errorf("surface %v emitted no %s file", tc.surf, k)
				}
			}
			for _, k := range tc.notWant {
				if kinds[k] {
					t.Errorf("surface %v should not emit a %s file", tc.surf, k)
				}
			}
			runGeneratedSurfaceUnit(t, fixed, tc.surf, fixedProbe)
		})
	}
}

func copyStr(in []string) []string { out := make([]string, len(in)); copy(out, in); return out }

// mustSurface generates u's tables for the given surface list.
func mustSurface(t *testing.T, u *ir.Unit, surf []string) map[string][]byte {
	t.Helper()
	out, err := GenerateSurface(u, nil, surf)
	if err != nil {
		t.Fatalf("GenerateSurface(%v): %v", surf, err)
	}
	return out
}

// filesEqual reports whether two generated file maps are byte-identical.
func filesEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for name, data := range a {
		bd, ok := b[name]
		if !ok || !bytes.Equal(data, bd) {
			return false
		}
	}
	return true
}

// fileKinds reports which generated file kinds a map contains, by suffix.
func fileKinds(files map[string][]byte) map[string]bool {
	out := map[string]bool{}
	for name := range files {
		switch {
		case strings.HasSuffix(name, "TableJson.go"):
			out["TableJson.go"] = true
		case strings.HasSuffix(name, "Table.go"):
			out["Table.go"] = true
		case strings.HasSuffix(name, "Block.go"):
			out["Block.go"] = true
		case strings.HasSuffix(name, "Cook.go"):
			out["Cook.go"] = true
		case strings.HasSuffix(name, "View.go"):
			out["View.go"] = true
		}
	}
	return out
}

// runGeneratedSurfaceUnit compiles the generated packet + table package for a
// surface list and runs a wire round-trip probe.
func runGeneratedSurfaceUnit(t *testing.T, u *ir.Unit, surf []string, probe string) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tabs, err := GenerateSurface(u, nil, surf)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tabs)
	files["surf_test.go"] = []byte(probe)
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("surface %v did not compile/round-trip:\n%s", surf, out)
	}
}
