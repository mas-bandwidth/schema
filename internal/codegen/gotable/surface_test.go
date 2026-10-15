// The --surface gate (docs/SPEC-TABLES.md §8.4): a generate named a surface
// emits only the files and functions that surface needs, a default generate is
// byte-identical to the explicit full list, and each single-surface generate
// still compiles.
package gotable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// surfaceSchema exercises the wire, json, view, cook, message and retain
// surfaces of a fixed table. Fixed tables are non-regional, which keeps the
// per-surface output self-contained.
const surfaceSchema = `package probe
flags Caps { Jump, Fly }
enum Grade { Gold, Silver }
type Badge { title string(8) }
fixed table Root {
	id int32
	name string(8)
	caps Caps = { Jump }
	badge Badge
	grades ?[2]Grade
}
`

func resolvedSurfaces(t *testing.T, surfs []string) map[string]bool {
	t.Helper()
	if surfs == nil {
		return nil
	}
	m, err := ParseSurfaces(surfs)
	if err != nil {
		t.Fatalf("ParseSurfaces(%v): %v", surfs, err)
	}
	return m
}

func mustGenSurface(t *testing.T, surfs []string) map[string][]byte {
	t.Helper()
	u := unitFrom(t, surfaceSchema)
	out, err := GenerateLineageSurfaces(u, nil, resolvedSurfaces(t, surfs))
	if err != nil {
		t.Fatalf("generate %v: %v", surfs, err)
	}
	return out
}

func joinFiles(files map[string][]byte) []byte {
	var b bytes.Buffer
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		b.WriteString("@@ " + n + " @@\n")
		b.Write(files[n])
	}
	return b.Bytes()
}

func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()

	full := []string{"wire", "fixed", "json", "cook", "block", "view", "message", "retain"}
	defO := mustGenSurface(t, nil)
	fullO := mustGenSurface(t, full)
	if !bytes.Equal(joinFiles(defO), joinFiles(fullO)) {
		t.Fatal("default generate is not byte-identical to the explicit full surface list")
	}

	cases := []struct {
		name    string
		surfs   []string
		present []string
		absent  []string
	}{
		{"wire", []string{"wire"},
			[]string{"RootMeasure", "RootSave", "RootReset"},
			[]string{"RootToJson", "CookMeasure", "UnitView", "AnnounceMeasure", "MeasureRetain"}},
		{"json", []string{"json"},
			[]string{"RootToJson", "RootFromJson", "RootMeasure"},
			[]string{"CookMeasure", "UnitView", "AnnounceMeasure", "MeasureRetain"}},
		{"view", []string{"view"},
			[]string{"UnitView"},
			[]string{"RootToJson", "CookMeasure", "AnnounceMeasure", "MeasureRetain"}},
		{"cook", []string{"cook"},
			[]string{"CookMeasure", "RootMeasure"},
			[]string{"RootToJson", "UnitView", "AnnounceMeasure", "MeasureRetain"}},
		{"message", []string{"message"},
			[]string{"AnnounceMeasure", "RootMeasure"},
			[]string{"RootToJson", "CookMeasure", "UnitView", "MeasureRetain"}},
		{"retain", []string{"retain"},
			[]string{"MeasureRetain", "RootMeasure"},
			[]string{"RootToJson", "CookMeasure", "UnitView", "AnnounceMeasure"}},
		{"fixed", []string{"fixed"},
			[]string{},
			[]string{"RootToJson", "CookMeasure", "UnitView", "AnnounceMeasure", "MeasureRetain"}},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			files, err := GenerateLineageSurfaces(unitFrom(t, surfaceSchema), nil, resolvedSurfaces(t, c.surfs))
			if err != nil {
				t.Fatalf("generate %v: %v", c.surfs, err)
			}
			data := joinFiles(files)
			for _, p := range c.present {
				if !bytes.Contains(data, []byte(p)) {
					t.Errorf("surface %s: missing %s", c.name, p)
				}
			}
			for _, a := range c.absent {
				if bytes.Contains(data, []byte(a)) {
					t.Errorf("surface %s: unexpected %s", c.name, a)
				}
			}
		})
	}
}

// The single-surface sets must each compile alongside the packet emitter, the
// way a build that asks for only one surface uses them.
func TestGoGenerateSurfaceSetCompiles(t *testing.T) {
	t.Parallel()
	for _, surfs := range [][]string{{"wire"}, {"json"}, {"fixed"}} {
		u := unitFrom(t, surfaceSchema)
		files, err := GenerateLineageSurfaces(u, nil, resolvedSurfaces(t, surfs))
		if err != nil {
			t.Fatalf("generate %v: %v", surfs, err)
		}
		packet, err := golang.Generate(u)
		if err != nil {
			t.Fatalf("packet %v: %v", surfs, err)
		}
		maps.Copy(files, packet)
		compileGenerated(t, files)
	}
}

func compileGenerated(t *testing.T, files map[string][]byte) {
	t.Helper()
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
	slowtest.Gate(t, "the Go toolchain (it compiles the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile %v: %v\n%s", strings.Join(filesKeys(files), ","), err, out)
	}
}

func filesKeys(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for n := range files {
		out = append(out, n)
	}
	return out
}
