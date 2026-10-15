package gotable

import (
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

// runGeneratedSurfaces is runGeneratedUnit, but generates the table surfaces
// from a selection rather than the full default set.
func runGeneratedSurfaces(t *testing.T, u *ir.Unit, sel SurfaceSet, testSource string) ([]byte, error) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineageSurfaces(u, nil, sel)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	runtime := slowtest.SerializeGo(t, "../../..")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
flags Caps { Jump, Fly }
enum Grade { Gold, Silver }
type Badge { title string(8) = "fresh" }
fixed table Root {
 title string(12) = "default"
 caps Caps = { Jump, Fly }
 grade Grade = Gold
}
`)

	all, err := GenerateLineageSurfaces(u, nil, AllSurfaces)
	if err != nil {
		t.Fatalf("GenerateLineageSurfaces(all): %v", err)
	}
	def, err := GenerateLineageSurfaces(u, nil, SurfaceSet{})
	if err != nil {
		t.Fatalf("GenerateLineageSurfaces(default): %v", err)
	}
	if len(all) != len(def) {
		t.Fatalf("default surface set file count %d != full %d", len(def), len(all))
	}
	for name, full := range all {
		d, ok := def[name]
		if !ok || string(d) != string(full) {
			t.Fatalf("default surface %s is not byte-identical to the full set", name)
		}
	}

	cases := []struct {
		name     string
		sel      SurfaceSet
		have     []string
		lack     []string
		contains []string
		round    bool
		probe    string
	}{
		{
			name:     "wire only",
			sel:      SurfaceSet{Wire: true},
			have:     []string{"ProbeTable.go"},
			lack:     []string{"ProbeTableJson.go", "ProbeBlock.go", "ProbeCook.go", "ProbeView.go"},
			contains: []string{"func RootMeasure", "func RootSave", "func RootLoad"},
			round:    true,
			probe:    "package probe\nimport \"testing\"\nfunc TestWire(t *testing.T){ _ = t }",
		},
		{
			name:  "json pulls wire",
			sel:   SurfaceSet{Json: true},
			have:  []string{"ProbeTableJson.go", "ProbeTable.go"},
			lack:  []string{"ProbeBlock.go", "ProbeCook.go", "ProbeView.go"},
			round: true,
			probe: "package probe\nimport \"testing\"\nfunc TestJson(t *testing.T){ _ = t }",
		},
		{
			name:  "cook",
			sel:   SurfaceSet{Cook: true},
			have:  []string{"ProbeCook.go"},
			lack:  []string{"ProbeBlock.go", "ProbeView.go", "ProbeTableJson.go"},
			round: true,
			probe: "package probe\nimport \"testing\"\nfunc TestCook(t *testing.T){ _ = t }",
		},
		{
			name:  "block",
			sel:   SurfaceSet{Block: true},
			have:  []string{"ProbeBlock.go"},
			lack:  []string{"ProbeCook.go", "ProbeView.go", "ProbeTableJson.go"},
			round: true,
			probe: "package probe\nimport \"testing\"\nfunc TestBlock(t *testing.T){ _ = t }",
		},
		{
			name:  "view",
			sel:   SurfaceSet{View: true},
			have:  []string{"ProbeView.go"},
			lack:  []string{"ProbeBlock.go", "ProbeCook.go", "ProbeTableJson.go"},
			round: true,
			probe: "package probe\nimport \"testing\"\nfunc TestView(t *testing.T){ _ = t }",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := GenerateLineageSurfaces(u, nil, tc.sel)
			if err != nil {
				t.Fatalf("GenerateLineageSurfaces: %v", err)
			}
			for _, name := range tc.have {
				if _, ok := got[name]; !ok {
					t.Errorf("surface set %+v did not emit %s (have %v)", tc.sel, name, keys(got))
				}
			}
			for _, name := range tc.lack {
				if _, ok := got[name]; ok {
					t.Errorf("surface set %+v emitted %s, which it must not", tc.sel, name)
				}
			}
			for _, want := range tc.contains {
				found := false
				for _, data := range got {
					if strings.Contains(string(data), want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("surface set %+v did not emit a definition containing %q", tc.sel, want)
				}
			}
			if tc.round {
				if out, err := runGeneratedSurfaces(t, u, tc.sel, tc.probe); err != nil {
					t.Fatalf("generated unit for %+v does not compile/run: %v\n%s", tc.sel, err, out)
				}
			}
		})
	}

	if _, err := GenerateLineageSurfaces(u, nil, SurfaceSet{Json: true, Wire: false}); err != nil {
		t.Fatalf("json without explicit wire must still be honored: %v", err)
	}
	if _, err := ParseSurfaces([]string{"nonsense"}); err == nil {
		t.Fatal("ParseSurfaces accepted an unknown surface name")
	}
	if _, err := ParseSurfaces([]string{"wire", "nonsense"}); err == nil {
		t.Fatal("ParseSurfaces accepted a list with an unknown surface name")
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestParseSurfacesDefaultIsAll(t *testing.T) {
	t.Parallel()
	s, err := ParseSurfaces(nil)
	if err != nil {
		t.Fatalf("ParseSurfaces(nil): %v", err)
	}
	if s != AllSurfaces {
		t.Fatalf("ParseSurfaces(nil) = %+v, want all", s)
	}
	s, err = ParseSurfaces([]string{"wire", "json"})
	if err != nil {
		t.Fatalf("ParseSurfaces: %v", err)
	}
	if !s.Wire || !s.Json || strings.Contains(fmt.Sprint(s), "Cook:true") {
		t.Fatalf("ParseSurfaces(wire,json) = %+v", s)
	}
}
