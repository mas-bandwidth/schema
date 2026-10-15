package gotable

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
fixed table Child { n int32 }
table Root { head *Child
data []uint16
entries map[int32]Child }`)

	all := mustGenTables(t, u, nil)
	explicit, err := GenerateLineageSurfaces(u, nil, mustSurfaces(t, "wire,fixed,json,cook,block,view,message,retain"))
	if err != nil {
		t.Fatalf("generate explicit-all: %v", err)
	}
	for name, data := range all {
		got, ok := explicit[name]
		if !ok || !bytes.Equal(got, data) {
			t.Fatalf("explicit all differs from default on %s: present=%v identical=%v", name, ok, ok && bytes.Equal(got, data))
		}
	}

	leaf := map[string]string{
		"json":  "ProbeTableJson.go",
		"cook":  "ProbeCook.go",
		"block": "ProbeBlock.go",
		"view":  "ProbeView.go",
	}
	for surface, file := range leaf {
		surface, file := surface, file
		t.Run(surface, func(t *testing.T) {
			t.Parallel()
			out := mustGenTables(t, u, mustSurfaces(t, surface))
			for other, otherFile := range leaf {
				_, has := out[otherFile]
				if other == surface {
					if !has {
						t.Fatalf("--surface %s did not emit %s", surface, otherFile)
					}
				} else if has {
					t.Fatalf("--surface %s leaked %s (only %s should appear)", surface, otherFile, file)
				}
			}
		})
	}

	t.Run("inner", func(t *testing.T) {
		t.Parallel()
		for _, surface := range []string{"wire", "fixed", "message", "retain"} {
			surface := surface
			t.Run(surface, func(t *testing.T) {
				t.Parallel()
				out := mustGenTables(t, u, mustSurfaces(t, surface))
				for _, otherFile := range leaf {
					if _, has := out[otherFile]; has {
						t.Fatalf("--surface %s leaked %s", surface, otherFile)
					}
				}
			})
		}
	})
}

func mustSurfaces(t *testing.T, s string) SurfaceSet {
	t.Helper()
	out, err := ParseSurfaces(s)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustGenTables(t *testing.T, u *ir.Unit, surfaces SurfaceSet) map[string][]byte {
	t.Helper()
	out, err := GenerateLineageSurfaces(u, nil, surfaces)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}
