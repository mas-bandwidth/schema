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
)

const surfaceProbeSchema = `package probe
flags Caps { Jump, Fly }
enum Grade {
 Gold, Silver
}
type Badge {
 title string(8) = "fresh"
}
fixed table Root {
 title string(12) = "default"
 tag bytes(4) = "ab"
 caps Caps = { Jump, Fly }
 badge Badge
 grade Grade
}
`

func surfaceCompile(t *testing.T, s Surfaces) {
	t.Helper()
	u := unitFrom(t, surfaceProbeSchema)
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineageSurfaces(u, nil, s)
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
	files["probe_test.go"] = []byte("package probe\nimport \"testing\"\nfunc TestCompile(t *testing.T){}\n")
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("surface %+v does not compile: %v\n%s", s, err, out)
	}
}

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins the --surface flag
// (docs/SPEC-TABLES.md §8.4, docs/USAGE.md): the default emit is byte-identical
// to the full set, each surface list emits only the files and functions it
// names (with the wire dependency resolved), and each list compiles.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()

	u := unitFrom(t, surfaceProbeSchema)
	full, err := GenerateLineageSurfaces(u, nil, allSurfaces())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if !byteMapsEqual(full, legacy) {
		t.Fatalf("default (all surfaces) emit is not byte-identical to the legacy emit")
	}

	cases := []struct {
		list string
		s    Surfaces
	}{
		{"", allSurfaces()},
		{"wire", Surfaces{Wire: true}},
		{"fixed", Surfaces{Wire: true, Fixed: true}},
		{"json", Surfaces{Wire: true, JSON: true}},
		{"cook", Surfaces{Wire: true, Cook: true}},
		{"block", Surfaces{Wire: true, Block: true}},
		{"view", Surfaces{Wire: true, View: true}},
		{"message", Surfaces{Wire: true, Message: true}},
		{"retain", Surfaces{Wire: true, Retain: true}},
		{"cook,block,json,view", Surfaces{Wire: true, JSON: true, Cook: true, Block: true, View: true}},
	}

	for _, tc := range cases {
		want, err := ParseSurfaces(tc.list)
		if err != nil {
			t.Fatalf("ParseSurfaces(%q): %v", tc.list, err)
		}
		if want != tc.s {
			t.Fatalf("ParseSurfaces(%q) = %+v, want %+v", tc.list, want, tc.s)
		}
		out, err := GenerateLineageSurfaces(u, nil, tc.s)
		if err != nil {
			t.Fatalf("GenerateLineageSurfaces(%q): %v", tc.list, err)
		}
		names := make([]string, 0, len(out))
		for n := range out {
			names = append(names, n)
		}
		// the wire's Table.go files are unconditional
		for n := range out {
			if strings.HasSuffix(n, "Table.go") {
				break
			}
		}
		if !hasSuffix(names, "Table.go") {
			t.Fatalf("%q: no Table.go emitted", tc.list)
		}
		check := func(suffix string, on bool) {
			have := hasSuffix(names, suffix)
			if have != on {
				t.Fatalf("%q: %s emitted = %v, want %v", tc.list, suffix, have, on)
			}
		}
		check("Block.go", tc.s.Block)
		check("Cook.go", tc.s.Cook)
		check("TableJson.go", tc.s.JSON)
		check("View.go", tc.s.View)

		var tableBody string
		for n, data := range out {
			if strings.HasSuffix(n, "Table.go") {
				tableBody += string(data)
			}
		}
		contains := func(marker string, on bool) {
			have := strings.Contains(tableBody, marker)
			if have != on {
				t.Fatalf("%q: Table.go %s present = %v, want %v", tc.list, marker, have, on)
			}
		}
		contains("THE FIXED FORM", tc.s.Fixed)
		contains("SaveMessageBody", tc.s.Message)
		contains("LoadRetain", tc.s.Retain)

		surfaceCompile(t, tc.s)
	}
}

func hasSuffix(names []string, suffix string) bool {
	for _, n := range names {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

func byteMapsEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || string(va) != string(vb) {
			return false
		}
	}
	return true
}
