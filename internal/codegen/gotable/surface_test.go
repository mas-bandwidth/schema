package gotable

import (
	"strings"
	"testing"
)

const surfaceSchema = `package probe
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

// surfaceKind names the surface a generated file belongs to
// (docs/SPEC-TABLES.md §8.4). wire is the table file itself; block, cook, json
// and view are the separable side files.
func surfaceKind(name string) string {
	switch {
	case strings.HasSuffix(name, "Table.go"):
		return "wire"
	case strings.HasSuffix(name, "Block.go"):
		return "block"
	case strings.HasSuffix(name, "Cook.go"):
		return "cook"
	case strings.HasSuffix(name, "TableJson.go"):
		return "json"
	case strings.HasSuffix(name, "View.go"):
		return "view"
	}
	return "other"
}

func hasKind(files map[string][]byte, kind string) bool {
	for name := range files {
		if surfaceKind(name) == kind {
			return true
		}
	}
	return false
}

// onlyKinds requires every file to belong to one of the named surface kinds.
func onlyKinds(files map[string][]byte, kinds ...string) bool {
	for name := range files {
		k := surfaceKind(name)
		ok := false
		for _, want := range kinds {
			if k == want {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins docs/SPEC-TABLES.md §8.4:
// the default generate emits the whole set byte-identical to a build before the
// flag existed, and a --surface list emits only the files and functions it
// names, with wire always present as the shared base.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, surfaceSchema)

	def, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}

	// the default is the full set, byte for byte.
	full, err := GenerateSurface(u, nil, "wire,fixed,json,cook,block,view,message,retain")
	if err != nil {
		t.Fatal(err)
	}
	if !byteMapsEqual(def, full) {
		t.Error("default generate != explicit full surface set")
	}
	for _, kind := range []string{"wire", "block", "cook", "json", "view"} {
		if !hasKind(def, kind) {
			t.Errorf("default generate emits no %s file", kind)
		}
	}

	// a surface list emits only its own files, with wire always present.
	cases := map[string][]string{
		"wire":  {"wire"},
		"block": {"wire", "block"},
		"cook":  {"wire", "cook"},
		"json":  {"wire", "json"},
		"view":  {"wire", "view"},
	}
	for surface, want := range cases {
		out, err := GenerateSurface(u, nil, surface)
		if err != nil {
			t.Fatalf("--surface=%s: %v", surface, err)
		}
		if !onlyKinds(out, want...) {
			t.Errorf("--surface=%s emitted files outside %v: %v", surface, want, keys(out))
		}
		for _, kind := range want {
			if !hasKind(out, kind) {
				t.Errorf("--surface=%s emitted no %s file", surface, kind)
			}
		}
	}

	// an unknown surface is refused, never silently ignored.
	if _, err := GenerateSurface(u, nil, "bogus"); err == nil {
		t.Error("unknown surface did not error")
	}
	if _, err := GenerateSurface(u, nil, "wire,fixed,"); err == nil {
		t.Error("empty surface token did not error")
	}

	// the default set compiles and runs.
	runGenerated(t, surfaceSchema, `package probe
import("testing")
func TestCompiles(t *testing.T){
 var v Root; RootMeasure(&v)
}
`)
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func byteMapsEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || string(av) != string(bv) {
			return false
		}
	}
	return true
}
