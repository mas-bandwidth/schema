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
	"github.com/mas-bandwidth/schema/v2/ir"
)

func generateSurfaces(t *testing.T, u *ir.Unit, s Surfaces) map[string][]byte {
	t.Helper()
	out, err := GenerateLineageSurfaces(u, nil, s)
	if err != nil {
		t.Fatalf("generate surfaces: %v", err)
	}
	return out
}

func surfaceFileNames(out map[string][]byte) []string {
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func runGeneratedSurfaces(t *testing.T, u *ir.Unit, s Surfaces, testSource string) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, generateSurfaces(t, u, s))
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
	slowtest.Gate(t, "the Go toolchain (it compiles and runs a surface-filtered generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("filtered generated runtime: %v\n%s", err, out)
	}
}

const surfaceProbe = `package probe

fixed table Ship
{
    name string(16)
    health int32 = 100
    speed float32 = 0.0
}
`

const surfaceProbeTest = `package probe

import "testing"

func TestRoundTrip(t *testing.T) {
	var s Ship
	s.NameLength = 5
	copy(s.Name[:5], "boaty")
	s.Health = 42
	s.Speed = 3.5
	sz := ShipMeasure(&s)
	buf := make([]byte, sz)
	if ShipSave(&s, buf) != int64(len(buf)) {
		t.Fatal("save")
	}
	var got Ship
	var r TableReport
	if !ShipLoad(&got, buf, &r) {
		t.Fatalf("load: %+v", r)
	}
	if got.Health != 42 || got.NameLength != 5 || string(got.Name[:5]) != "boaty" || got.Speed != 3.5 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}
`

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the --surface contract
// (docs/SPEC-TABLES.md §8.4): the default equals the full set byte for byte,
// a selection emits only the files and functions it names, dependencies
// resolve (json and message imply wire; view and block imply fixed; cook
// implies wire), an unknown surface word refuses, and a filtered unit still
// compiles and round-trips.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, surfaceProbe)

	full, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	def := generateSurfaces(t, u, AllSurfaces())
	for name, data := range full {
		got, ok := def[name]
		if !ok {
			t.Fatalf("the all-surfaces selection omitted %s, which a default generate emits", name)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("the all-surfaces selection is not byte-identical to the default for %s", name)
		}
	}
	if len(def) != len(full) {
		t.Fatalf("the all-surfaces selection emitted extra files: %v", surfaceFileNames(def))
	}
	if sel, err := ParseSurfaces(""); err != nil || sel != AllSurfaces() {
		t.Fatalf("ParseSurfaces(\"\") = %+v, %v; want AllSurfaces", sel, err)
	}
	if sel, err := ParseSurfaces("all"); err != nil || sel != AllSurfaces() {
		t.Fatalf("ParseSurfaces(\"all\") = %+v, %v; want AllSurfaces", sel, err)
	}

	cases := []struct {
		list    string
		wantAll []string // files that must be present
		wantAbs []string // files that must be absent
	}{
		{"wire", []string{"ProbeTable.go"}, []string{"ProbeTableJson.go", "ProbeBlock.go", "ProbeCook.go", "ProbeView.go"}},
		{"json", []string{"ProbeTable.go", "ProbeTableJson.go"}, []string{"ProbeBlock.go", "ProbeCook.go", "ProbeView.go"}},
		{"cook", []string{"ProbeTable.go", "ProbeCook.go"}, []string{"ProbeTableJson.go", "ProbeBlock.go", "ProbeView.go"}},
		{"block", []string{"ProbeTable.go", "ProbeBlock.go"}, []string{"ProbeTableJson.go", "ProbeCook.go", "ProbeView.go"}},
		{"view", []string{"ProbeTable.go", "ProbeView.go"}, []string{"ProbeTableJson.go", "ProbeBlock.go", "ProbeCook.go"}},
		{"message", []string{"ProbeTable.go"}, []string{"ProbeTableJson.go", "ProbeBlock.go", "ProbeCook.go", "ProbeView.go"}},
		{"retain", []string{"ProbeTable.go"}, []string{"ProbeTableJson.go", "ProbeBlock.go", "ProbeCook.go", "ProbeView.go"}},
	}
	for _, tc := range cases {
		s, err := ParseSurfaces(tc.list)
		if err != nil {
			t.Fatalf("ParseSurfaces(%q): %v", tc.list, err)
		}
		out := generateSurfaces(t, u, s)
		for _, f := range tc.wantAll {
			if _, ok := out[f]; !ok {
				t.Errorf("%s: missing %s; have %v", tc.list, f, surfaceFileNames(out))
			}
		}
		for _, f := range tc.wantAbs {
			if _, ok := out[f]; ok {
				t.Errorf("%s: emitted %s, which it does not name", tc.list, f)
			}
		}
		for name, data := range out {
			base, ok := full[name]
			if !ok {
				t.Errorf("%s: emitted %s, absent from the full set", tc.list, name)
				continue
			}
			if strings.ContainsAny(name, "JsonBlockCookView") {
				continue
			}
			if len(data) > len(base) {
				t.Errorf("%s: %s is larger than the full set's", tc.list, name)
			}
		}
	}

	if _, err := ParseSurfaces("wire,audio"); err == nil {
		t.Fatal("an unknown surface word must refuse")
	}

	runGeneratedSurfaces(t, u, mustSurfaces(t, "json"), surfaceProbeTest)
}

func mustSurfaces(t *testing.T, list string) Surfaces {
	t.Helper()
	s, err := ParseSurfaces(list)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
