package gotable

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// surfaceOrder is the canonical order of the Go table surfaces
// (docs/USAGE.md, docs/SPEC-TABLES.md §8.4).
var surfaceOrder = []string{"wire", "fixed", "json", "cook", "block", "view", "message", "retain"}

// surfaceOwnFile is the generated file basename a surface owns, or "" when the
// surface lives inside <Base>Table.go.
func surfaceOwnFile(s string) string {
	switch s {
	case "json":
		return "TableJson.go"
	case "cook":
		return "Cook.go"
	case "block":
		return "Block.go"
	case "view":
		return "View.go"
	}
	return ""
}

func surfaceUnit(t *testing.T) *ir.Unit {
	t.Helper()
	return unitFrom(t, `package probe
flags Caps {
	Jump
	Fly
}
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
	grade Grade
}
`)
}

func setKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := surfaceUnit(t)

	// The default equals the full set, byte for byte, and both equal what the
	// unconstrained generator emits today.
	full, err := GenerateWithSurfaces(u, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	def, err := GenerateWithSurfaces(u, nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	base, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(setKeys(def), setKeys(base)) || !reflect.DeepEqual(setKeys(full), setKeys(base)) {
		t.Fatalf("default/full key set differs from BASE: default %v full %v base %v", setKeys(def), setKeys(full), setKeys(base))
	}
	for name := range base {
		if string(def[name]) != string(base[name]) || string(full[name]) != string(base[name]) {
			t.Fatalf("default/full differs from BASE generation on %s", name)
		}
	}

	// Each surface emits its own file and none of the other surfaces' files.
	for _, s := range surfaceOrder {
		out, err := GenerateWithSurfaces(u, nil, []string{s})
		if err != nil {
			t.Fatalf("surface %s: %v", s, err)
		}
		if own := surfaceOwnFile(s); own != "" {
			found := false
			for name := range out {
				if strings.HasSuffix(name, own) {
					found = true
				}
			}
			if !found {
				t.Fatalf("surface %s must emit %s", s, own)
			}
		}
		for _, other := range surfaceOrder {
			if other == s {
				continue
			}
			if oo := surfaceOwnFile(other); oo != "" {
				for name := range out {
					if strings.HasSuffix(name, oo) {
						t.Fatalf("surface %s must not emit %s (%s)", s, oo, name)
					}
				}
			}
		}
	}
}
