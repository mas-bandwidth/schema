package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func fullSurfaces() map[string]bool {
	m := map[string]bool{}
	for _, s := range SurfaceNames {
		m[s] = true
	}
	return m
}

func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, blockBuilderSchema)
	full, err := GenerateSurfaceLineage(u, nil, fullSurfaces())
	if err != nil {
		t.Fatal(err)
	}
	def, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(def, full) {
		t.Fatalf("default generate differs from the explicit full surface set")
	}
	surfaceFile := map[string]func(string, *ir.Unit) bool{
		"block": func(n string, _ *ir.Unit) bool { return strings.HasSuffix(n, "Block.go") },
		"cook":  func(n string, _ *ir.Unit) bool { return strings.HasSuffix(n, "Cook.go") },
		"json":  func(n string, _ *ir.Unit) bool { return strings.HasSuffix(n, "TableJson.go") },
		"view":  func(n string, u *ir.Unit) bool { return n == ir.GoExportName(u.Package)+"View.go" },
	}
	for _, s := range []string{"block", "cook", "json", "view"} {
		s := s
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			files, err := GenerateSurfaceLineage(u, nil, map[string]bool{s: true})
			if err != nil {
				t.Fatal(err)
			}
			pred := surfaceFile[s]
			found := false
			for name := range files {
				if name == "go.mod" {
					continue
				}
				if pred(name, u) {
					found = true
				}
				if _, ok := full[name]; !ok {
					t.Errorf("surface %q emitted file %s that is not in the full set", s, name)
				}
			}
			allowed := resolveSurfaces(map[string]bool{s: true})
			for o := range surfaceFile {
				if allowed[o] {
					continue
				}
				for name := range files {
					if surfaceFile[o](name, u) {
						t.Errorf("surface %q emitted %s's file %s (not a dependency)", s, o, name)
					}
				}
			}
			if !found {
				t.Errorf("surface %q emitted no %s file", s, s)
			}
			if out, err := runSurfaces(t, u, map[string]bool{s: true}, "package "+u.Package+"\nimport \"testing\"\nfunc TestTrivial(t *testing.T) {}\n"); err != nil {
				t.Fatalf("surface %q did not compile: %v\n%s", s, err, out)
			}
		})
	}
}

// runSurfaces generates a unit for exactly the requested surfaces, writes the
// tree into a t.TempDir(), and runs `go test .` there — the house pattern for
// proving generated code compiles, with the surface set as the only variable.
func runSurfaces(t *testing.T, u *ir.Unit, requested map[string]bool, testSource string) ([]byte, error) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := GenerateSurfaceLineage(u, nil, requested)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		return nil, err
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["surface_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return nil, err
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func TestParseSurfacesRejectsUnknown(t *testing.T) {
	t.Parallel()
	if _, err := ParseSurfaces("wire,no-such-surface"); err == nil {
		t.Fatal("expected an error for an unknown surface")
	}
	if got, err := ParseSurfaces("json, wire"); err != nil || !got["json"] || !got["wire"] {
		t.Fatalf("ParseSurfaces: %v %v", got, err)
	}
}
