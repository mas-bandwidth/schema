package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// surfaceFixedSchema is a fixed-only unit: every on-the-side surface is
// separable, so the file-level surface selection is observable in full.
const surfaceFixedSchema = `package probe
fixed table Block
{
    id   uint32
    flag bool
}
`

// surfaceVariableSchema is a variable unit: its row storage and region text
// helpers live in the cook and json files, so those two surfaces are intrinsic
// and stay emitted even when left out of the list.
const surfaceVariableSchema = `package probe
table Node
{
    value int32
    next  *Node
}
`

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the Go target's --surface
// selection (docs/SPEC-TABLES.md §8.4): a fixed-only unit emits only the files
// the named surfaces need, the default equals the full set byte for byte, each
// surface list's output compiles, and a variable unit keeps its intrinsic cook
// and json files.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, surfaceFixedSchema)

	full, ferr := GenerateLineageSurfaces(u, nil, allSurfaces())
	if ferr != nil {
		t.Fatalf("full surface generate: %v", ferr)
	}
	def, err := Generate(u)
	if err != nil {
		t.Fatalf("default generate: %v", err)
	}
	if !equalBytesMaps(def, full) {
		t.Fatalf("the default generate differs from the explicit full surface set")
	}

	want := map[string]bool{
		"ProbeTable.go":     true,
		"ProbeBlock.go":     true,
		"ProbeCook.go":      true,
		"ProbeTableJson.go": true,
		"ProbeView.go":      true,
	}
	if len(full) != len(want) {
		t.Fatalf("full surface set has %d files, want %d: %v", len(full), len(want), maps.Keys(full))
	}
	for name := range full {
		if !want[name] {
			t.Fatalf("full surface set emitted unexpected file %s", name)
		}
	}

	// cook and json are intrinsic to the Go table module (the row storage and
	// the text helpers live there, and the table module always carries them),
	// so the separable surfaces are block and view; every other selection is
	// the table module alone.
	base := []string{"ProbeTable.go", "ProbeCook.go", "ProbeTableJson.go"}
	cases := []struct {
		name      string
		surf      SurfaceSet
		wantFiles []string
	}{
		{name: "wire", surf: SurfaceSet{Wire: true}, wantFiles: base},
		{name: "fixed", surf: SurfaceSet{Fixed: true}, wantFiles: base},
		{name: "json", surf: SurfaceSet{Json: true}, wantFiles: base},
		{name: "cook", surf: SurfaceSet{Cook: true}, wantFiles: base},
		{name: "message", surf: SurfaceSet{Message: true}, wantFiles: base},
		{name: "retain", surf: SurfaceSet{Retain: true}, wantFiles: base},
		{name: "block", surf: SurfaceSet{Block: true}, wantFiles: []string{"ProbeTable.go", "ProbeCook.go", "ProbeTableJson.go", "ProbeBlock.go"}},
		{name: "view", surf: SurfaceSet{View: true}, wantFiles: []string{"ProbeTable.go", "ProbeCook.go", "ProbeTableJson.go", "ProbeView.go"}},
		{name: "block-view", surf: SurfaceSet{Block: true, View: true}, wantFiles: []string{"ProbeTable.go", "ProbeCook.go", "ProbeTableJson.go", "ProbeBlock.go", "ProbeView.go"}},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := GenerateLineageSurfaces(u, nil, c.surf)
			if err != nil {
				t.Fatalf("generate(%s): %v", c.name, err)
			}
			for name := range got {
				ok := false
				for _, want := range c.wantFiles {
					if want == name {
						ok = true
						break
					}
				}
				if !ok {
					t.Errorf("%s emitted unexpected file %s", c.name, name)
				}
			}
			for _, want := range c.wantFiles {
				if _, ok := got[want]; !ok {
					t.Errorf("%s did not emit expected file %s", c.name, want)
				}
			}
			compileSurfaces(t, u, c.surf)
		})
	}

	// A variable unit keeps its intrinsic cook and json files even when the
	// list omits them.
	t.Run("variable-keeps-cook-json", func(t *testing.T) {
		t.Parallel()
		v := unitFrom(t, surfaceVariableSchema)
		got, err := GenerateLineageSurfaces(v, nil, SurfaceSet{Wire: true})
		if err != nil {
			t.Fatalf("generate(variable, wire): %v", err)
		}
		for _, want := range []string{"ProbeTable.go", "ProbeCook.go", "ProbeTableJson.go"} {
			if _, ok := got[want]; !ok {
				t.Errorf("variable unit should keep intrinsic file %s", want)
			}
		}
		compileSurfaces(t, v, SurfaceSet{Wire: true})
	})
}

// equalBytesMaps compares two emitted file maps byte for byte.
func equalBytesMaps(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for name, data := range a {
		other, ok := b[name]
		if !ok || string(data) != string(other) {
			return false
		}
	}
	return true
}

// compileSurfaces emits the unit under a surface selection and compiles the
// resulting package with the Go toolchain, so a surface list that leaves the
// generated package uncompilable fails here.
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
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "build", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated package under these surfaces does not compile: %v\n%s", err, out)
	}
}
