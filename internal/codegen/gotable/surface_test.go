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

func generateSurfaces(t *testing.T, src string, surf SurfaceSet) map[string][]byte {
	t.Helper()
	out, err := GenerateLineageSurfaces(unitFrom(t, src), nil, surf)
	if err != nil {
		t.Fatalf("generate surfaces: %v", err)
	}
	return out
}

func compileSurfaces(t *testing.T, u *ir.Unit, surf SurfaceSet) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineageSurfaces(u, nil, surf)
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
	files["values_test.go"] = []byte(`package probe
import "testing"
func TestCompile(t *testing.T) {}
`)
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

func mapsBytesEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !bytes.Equal(av, bv) {
			return false
		}
	}
	return true
}

func hasSuffixFile(files map[string][]byte, suffix string) bool {
	for name := range files {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the `--surface` flag
// (docs/SPEC-TABLES.md §8.4): each surface list emits only its files, the
// default equals the full set byte for byte, and a restricted set still
// compiles and runs.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	const src = `package probe
fixed table Ship {
	name string(8)
	health int32 = 100
}
`
	full := generate(t, src)
	allList, err := ParseSurfaceList("wire,fixed,json,cook,block,view,message,retain")
	if err != nil {
		t.Fatal(err)
	}
	fullList := generateSurfaces(t, src, allList)
	if !mapsBytesEqual(full, fullList) {
		t.Errorf("default generate (%d files) differs byte-for-byte from explicit all-surfaces (%d files)", len(full), len(fullList))
	}
	u := unitFrom(t, src)

	base := []string{"Table.go", "TableJson.go"}
	cases := []struct {
		name    string
		list    string
		set     SurfaceSet
		present []string
		absent  []string
	}{
		{"wire", "wire", SurfaceWire | SurfaceJson, base, []string{"Block.go", "Cook.go", "View.go"}},
		{"fixed", "fixed", SurfaceFixed | SurfaceWire | SurfaceJson, base, []string{"Block.go", "Cook.go", "View.go"}},
		{"json", "json", SurfaceJson | SurfaceWire, base, []string{"Block.go", "Cook.go", "View.go"}},
		{"cook", "cook", SurfaceCook | SurfaceWire | SurfaceJson, append(append([]string{}, base...), "Cook.go"), []string{"Block.go", "View.go"}},
		{"block", "block", SurfaceBlock | SurfaceWire | SurfaceJson, append(append([]string{}, base...), "Block.go"), []string{"Cook.go", "View.go"}},
		{"view", "view", SurfaceView | SurfaceWire | SurfaceJson, append(append([]string{}, base...), "View.go"), []string{"Block.go", "Cook.go"}},
		{"message", "message", SurfaceMessage | SurfaceWire | SurfaceJson, base, []string{"Block.go", "Cook.go", "View.go"}},
		{"retain", "retain", SurfaceRetain | SurfaceWire | SurfaceJson, base, []string{"Block.go", "Cook.go", "View.go"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSurfaceList(tc.list)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.set {
				t.Errorf("ParseSurfaceList(%q) = %v, want %v", tc.list, got, tc.set)
			}
			out := generateSurfaces(t, src, tc.set)
			for _, suf := range tc.present {
				if !hasSuffixFile(out, suf) {
					t.Errorf("surface %s: missing %s file", tc.name, suf)
				}
			}
			for _, suf := range tc.absent {
				if hasSuffixFile(out, suf) {
					t.Errorf("surface %s: unexpected %s file present", tc.name, suf)
				}
			}
			compileSurfaces(t, u, tc.set)
		})
	}
}
