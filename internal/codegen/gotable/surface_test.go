package gotable

import (
	"strings"
	"testing"
)

// goGenerateSurfaceSchema is a small table unit whose generated tree spans every
// surface: the wire codecs and descriptors in <Base>Table.go, the JSON text
// form in <Home>TableJson.go, the block and cook accelerators in
// <Base>Block.go / <Base>Cook.go, the view registry in <Home>View.go, plus the
// fixed, message and retain surfaces.
const goGenerateSurfaceSchema = `package probe

fixed table Ship
{
    name string(16) = "none"
    hp int32 = 100
    ready bool
}

fixed table Fleet
{
    lead Ship
}
`

// goGenerateSurfaceHome is the unit's runtime home file basename: the package's
// exported name (docs/SPEC-TABLES.md §19.2).
const goGenerateSurfaceHome = "Probe"

func goGenerateSurface(t *testing.T, requested []string) map[string][]byte {
	t.Helper()
	u := unitFrom(t, goGenerateSurfaceSchema)
	out, err := GenerateLineageSurfaces(u, nil, requested)
	if err != nil {
		t.Fatalf("GenerateLineageSurfaces(%v): %v", requested, err)
	}
	return out
}

func hasFile(files map[string][]byte, suffix string) bool {
	for name := range files {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func concat(files map[string][]byte) string {
	var b strings.Builder
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			b.Write(data)
		}
	}
	return b.String()
}

// TestGoGenerateSurfaceEmitsOnlyWhatItNames holds the surface flag (docs/SPEC
// -TABLES.md §8.4) to "only the surfaces asked for": each surface list emits
// only its files and functions, the default equals the full set byte for byte,
// and json pulls in its wire dependency.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()

	t.Run("default equals the full set byte for byte", func(t *testing.T) {
		t.Parallel()
		full, err := GenerateLineageSurfaces(unitFrom(t, goGenerateSurfaceSchema), nil, Surfaces)
		if err != nil {
			t.Fatal(err)
		}
		def, err := Generate(unitFrom(t, goGenerateSurfaceSchema))
		if err != nil {
			t.Fatal(err)
		}
		if len(full) != len(def) {
			t.Fatalf("default has %d files, full set has %d", len(def), len(full))
		}
		for name, data := range full {
			if def[name] == nil {
				t.Fatalf("full set has %s, default does not", name)
			}
			if string(def[name]) != string(data) {
				t.Fatalf("%s differs between default and full set", name)
			}
		}
	})

	fileSurface := map[string]string{
		"block": goGenerateSurfaceHome + "Block.go",
		"cook":  goGenerateSurfaceHome + "Cook.go",
		"json":  goGenerateSurfaceHome + "TableJson.go",
		"view":  goGenerateSurfaceHome + "View.go",
	}

	t.Run("each surface emits its own file and no other file", func(t *testing.T) {
		t.Parallel()
		for surface, want := range fileSurface {
			surface, want := surface, want
			t.Run(surface, func(t *testing.T) {
				t.Parallel()
				files := goGenerateSurface(t, []string{surface})
				if !hasFile(files, want) {
					t.Fatalf("%s surface did not emit %s (files: %v)", surface, want, keys(files))
				}
				for other, otherFile := range fileSurface {
					if other == surface {
						continue
					}
					if hasFile(files, otherFile) {
						t.Fatalf("%s surface leaked %s", surface, otherFile)
					}
				}
			})
		}
	})

	t.Run("json pulls in its wire dependency", func(t *testing.T) {
		t.Parallel()
		files := goGenerateSurface(t, []string{"json"})
		body := concat(files)
		// the TEXT form's walker walks the wire descriptors (§16), so a
		// json-only generate still carries the wire codecs
		if !strings.Contains(body, "ShipMeasure") {
			t.Fatal("json surface did not emit the wire codecs it depends on (ShipMeasure)")
		}
	})

	t.Run("wire-only emits no message or fixed or retain surface", func(t *testing.T) {
		t.Parallel()
		files := goGenerateSurface(t, []string{"wire"})
		body := concat(files)
		if !strings.Contains(body, "ShipMeasure") {
			t.Fatal("wire surface did not emit ShipMeasure")
		}
		// the wire descriptor references the per-table message BODY hooks but
		// never the batch surface, so a batch function is a clean message-only
		// marker
		for _, leaked := range []string{"ShipMeasureMessages", "Region"} {
			if strings.Contains(body, leaked) {
				t.Fatalf("wire-only leaked %s", leaked)
			}
		}
	})

	t.Run("an unknown surface is refused by name", func(t *testing.T) {
		t.Parallel()
		_, err := GenerateLineageSurfaces(unitFrom(t, goGenerateSurfaceSchema), nil, []string{"nope"})
		if err == nil {
			t.Fatal("expected an error for an unknown surface")
		}
		if !strings.Contains(err.Error(), "nope") {
			t.Fatalf("error does not name the unknown surface: %v", err)
		}
	})
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
