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

var surfaceSchema = `package probe
flags Caps { Jump, Fly }
enum Grade {
 Gold | was = "OldGold"
}
type Badge {
 title string(8) = "fresh" | was = "label"
}
fixed table Root {
 title string(12) = "default"
 tag bytes(4) = "ab"
 caps Caps = { Jump, Fly }
 badge Badge
 values ?[..3]int32
 grades ?[2]Grade
}
`

// regionalSchema exercises the region surfaces (wire, json, cook, block) whose
// reflect codecs couple to the retain-surface descriptors; selecting those
// surfaces without retain must compile and must not reference retain symbols.
var regionalSchema = `package probe
type Colour { r uint8 }
fixed table Leaf { value int32 = 7 }
table Node {
 value int32
 next *Node
 leaf *Leaf
}
table Root {
 colour Colour
 head *Node
 alias *Node
 leaves [..3]*Leaf
}
`

// surfaceFiles maps each surface to the file it alone owns, keyed by a
// distinctive path suffix the generated tree carries when the surface is on.
var surfaceFiles = map[string]string{
	"json":  "TableJson.go",
	"cook":  "Cook.go",
	"block": "Block.go",
	"view":  "View.go",
}

// surfaceTokens are distinctive symbols each non-wire surface writes into the
// table module (<Base>Table.go); they must be present exactly when the surface
// is selected.
var surfaceTokens = map[string]string{
	"fixed":   "RootFixedPlanMax",
	"message": "RootMeasureMessages",
	"cook":    "RootCookMeasure",
	"retain":  "RootLoadRetain",
}

func genWithSurfaces(t *testing.T, u *ir.Unit, s Surfaces) map[string][]byte {
	t.Helper()
	out, err := GenerateLineageSurface(u, nil, s)
	if err != nil {
		t.Fatalf("GenerateLineageSurface(%v): %v", s, err)
	}
	return out
}

func tableModule(out map[string][]byte) string {
	t := ""
	for name, data := range out {
		if strings.HasSuffix(name, "Table.go") {
			t += string(data)
		}
	}
	return t
}

func allOutput(out map[string][]byte) string {
	t := ""
	for _, data := range out {
		t += string(data)
	}
	return t
}

func assertCompiles(t *testing.T, u *ir.Unit, s Surfaces, probe string) {
	t.Helper()
	files := genWithSurfaces(t, u, s)
	packet, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["probe_test.go"] = []byte(probe)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated surface %v does not compile/run: %v\n%s", s, err, outBytes)
	}
}

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the --surface flag
// (docs/SPEC-TABLES.md §8.8): each surface list emits only its files and
// functions, the default equals the full set byte for byte, and every selection
// compiles and round-trips.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, surfaceSchema)

	full := genWithSurfaces(t, u, DefaultSurfaces())
	explicit := genWithSurfaces(t, u, Surfaces{Wire: true, Fixed: true, JSON: true, Cook: true, Block: true, View: true, Message: true, Retain: true})
	if len(full) != len(explicit) {
		t.Fatalf("default has %d files, explicit full set has %d", len(full), len(explicit))
	}
	for name, data := range full {
		if other, ok := explicit[name]; !ok {
			t.Errorf("default emits %s but the explicit full set does not", name)
		} else if !bytes.Equal(data, other) {
			t.Errorf("default and explicit full set differ on %s", name)
		}
	}

	surfaces := []struct {
		list string
		s    Surfaces
	}{
		{"wire", Surfaces{Wire: true}},
		{"fixed", Surfaces{Wire: true, Fixed: true}},
		{"json", Surfaces{Wire: true, JSON: true}},
		{"cook", Surfaces{Wire: true, Cook: true}},
		{"block", Surfaces{Wire: true, Block: true}},
		{"view", Surfaces{Wire: true, View: true}},
		{"message", Surfaces{Wire: true, Message: true}},
		{"retain", Surfaces{Wire: true, Retain: true}},
	}

	for _, cs := range surfaces {
		cs := cs
		t.Run(cs.list, func(t *testing.T) {
			t.Parallel()
			out := genWithSurfaces(t, u, cs.s)
			// the wire table module is always present
			if tableModule(out) == "" {
				t.Error("the wire table module (<Base>Table.go) is always emitted")
			}
			for name, suffix := range surfaceFiles {
				got := false
				for f := range out {
					if strings.HasSuffix(f, suffix) {
						got = true
					}
				}
				if (cs.list == name) != got {
					t.Errorf("surface %q: file %s present=%v, want %v", cs.list, suffix, got, cs.list == name)
				}
			}
			body := allOutput(out)
			for surface, token := range surfaceTokens {
				has := strings.Contains(body, token)
				want := cs.list == surface
				if has != want {
					t.Errorf("surface %q: token %s present=%v, want %v", cs.list, token, has, want)
				}
			}
			assertCompiles(t, u, cs.s, "package probe\nimport(\"testing\")\nfunc TestCompiles(t *testing.T){var v Root;RootReset(&v);if v.ValuesPresent{t.Fatal(\"bad reset\")}}\n")
		})
	}

	// a regional schema without retain must compile and must not emit the
	// per-table retain-surface APIs (docs/SPEC-TABLES.md §8.8, REPAIR). The
	// shared retain writer/reader types and the region retain runtime stay, as
	// the wire descriptor's TableTypeInfo is typed by them.
	t.Run("regional-without-retain", func(t *testing.T) {
		t.Parallel()
		ru := unitFrom(t, regionalSchema)
		s := Surfaces{Wire: true, JSON: true, Cook: true, Block: true}
		out := genWithSurfaces(t, ru, s)
		body := allOutput(out)
		for _, sym := range []string{"RootLoadRetain", "RootSaveRetain", "RootMeasureRetain", "SaveBodyRetain:func"} {
			if strings.Contains(body, sym) {
				t.Errorf("regional without retain still emits %q", sym)
			}
		}
		assertCompiles(t, ru, s, "package probe\nimport(\"testing\")\nfunc TestCompiles(t *testing.T){var b RootBuilder;if !b.Init(){t.Fatal(\"init\")};defer b.Shutdown();r:=b.GetRoot();r.Colour.R=2}\n")
	})

	// ParseSurfaces normalizes and resolves dependencies (wire is implied).
	if s, err := ParseSurfaces("json,fixed"); err != nil {
		t.Fatal(err)
	} else if !s.JSON || !s.Fixed || !s.Wire {
		t.Errorf("ParseSurfaces(json,fixed) = %+v, want json+fixed+wire", s)
	}
	if _, err := ParseSurfaces("bogus"); err == nil {
		t.Error("ParseSurfaces(bogus) should error")
	}
}

// TestDefaultGenerateEqualsFullSurfaceSet pins DONE-WHEN's byte-identity claim
// at the public default entrypoint: GenerateLineage (the no-flag path) is
// byte-identical to GenerateLineageSurface with the explicit full set
// (docs/SPEC-TABLES.md §8.8).
func TestDefaultGenerateEqualsFullSurfaceSet(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, surfaceSchema)
	def, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	full, err := GenerateLineageSurface(u, nil, DefaultSurfaces())
	if err != nil {
		t.Fatal(err)
	}
	if len(def) != len(full) {
		t.Fatalf("default has %d files, full surface set has %d", len(def), len(full))
	}
	for name, data := range def {
		if other, ok := full[name]; !ok {
			t.Errorf("default emits %s but the full surface set does not", name)
		} else if !bytes.Equal(data, other) {
			t.Errorf("default and full surface set differ on %s", name)
		}
	}
}
