package gotable

import (
	"fmt"
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

// surfaceUnits are the two table classes whose output differs: a fixed table
// (the fixed form, the message form) and a variable one (the region machinery
// and the retain codecs, docs/SPEC-TABLES.md §8.4).
var surfaceUnits = []struct {
	name, schema, roundTrip string
	variable                bool                 // a variable table: cook, block, json and retain always ride (§8.4)
	markers                 map[Surface][]string // text that appears iff the surface is emitted
	files                   map[Surface]string   // file suffix that exists iff the surface is emitted
}{
	{
		name: "fixed",
		schema: `package probe
fixed table Root {
 n int32
 name string(8)
}
`,
		roundTrip: `package probe
import "testing"
func TestRoundTrip(t *testing.T) {
 v := Root{N: 7}; RootReset(&v); v.N = 7
 b := make([]byte, RootMeasure(&v))
 if RootSave(&v, b) != int64(len(b)) { t.Fatal("save") }
 var got Root; var r TableReport
 if !RootLoad(&got, b, &r) || got.N != 7 { t.Fatalf("load: %+v %+v", got, r) }
}
`,
		markers: map[Surface][]string{
			SurfaceFixed:   {"RootFixedSave("},
			SurfaceCook:    {"RootCookMeasure("},
			SurfaceMessage: {"RootSaveMessageBody(", "func AnnounceMeasure(", "func AnnounceRead(", "func Announce(", "type TableVocabulary"},
			SurfaceRetain:  {"RootLoadRetainMessages"},
			SurfaceJSON:    {"RootFromJson("},
		},
		files: map[Surface]string{SurfaceJSON: "TableJson.go", SurfaceCook: "Cook.go", SurfaceBlock: "Block.go", SurfaceView: "View.go"},
	},
	{
		name: "variable",
		schema: `package probe
table Root {
 n int32
 name string(16)
 items []int32
}
`,
		roundTrip: `package probe
import "testing"
func TestRoundTrip(t *testing.T) {
 var v Root; RootReset(&v); v.N = 7
 n := RootMeasure(&v)
 if n < 0 { t.Fatal("measure") }
 b := make([]byte, n)
 if RootSave(&v, b) != n { t.Fatal("save") }
 var r TableReport
 got := RootLoad(make([]byte, 4096), b, &r)
 if got == nil || got.N != 7 { t.Fatalf("load: %+v %+v", got, r) }
}
`,
		variable: true,
		markers: map[Surface][]string{
			SurfaceCook:    {"RootCookMeasure("},
			SurfaceMessage: {"RootSaveMessageBody(", "func AnnounceMeasure(", "func AnnounceRead(", "func Announce(", "type TableVocabulary"},
			SurfaceRetain:  {"RootSaveRetain("},
			SurfaceJSON:    {"RootFromJson("},
		},
		files: map[Surface]string{SurfaceJSON: "TableJson.go", SurfaceCook: "Cook.go", SurfaceBlock: "Block.go", SurfaceView: "View.go"},
	},
}

var surfaceLists = []struct {
	list string
	set  Surface
}{
	{"wire", SurfaceWire},
	{"fixed", SurfaceFixed},
	{"json", SurfaceJSON},
	{"cook", SurfaceCook | SurfaceJSON},                 // cook implies json (§8.4)
	{"block", SurfaceBlock | SurfaceCook | SurfaceJSON}, // block implies cook, cook implies json (§8.4)
	{"view", SurfaceView},
	{"message", SurfaceMessage},
	{"retain", SurfaceRetain},
	{"json,view", SurfaceJSON | SurfaceView},
	{"cook,message,retain", SurfaceCook | SurfaceMessage | SurfaceRetain},
	{"wire,fixed,json,cook,block,view,message,retain", SurfaceAll},
}

// TestGoGenerateSurfaceEmitsOnlyWhatItNames pins docs/SPEC-TABLES.md §8.4:
// each surface list emits its own files and functions and no other surface's,
// the dependencies it implies come with it, a default generate equals the full
// list byte for byte, and every emitted subset compiles and round-trips.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	for _, unit := range surfaceUnits {
		t.Run(unit.name, func(t *testing.T) {
			t.Parallel()
			u := unitFrom(t, unit.schema)
			def, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			full, err := GenerateSurface(u, nil, SurfaceAll)
			if err != nil {
				t.Fatal(err)
			}
			sameFiles(t, "default vs the full list", def, full)
			for surf, markers := range unit.markers {
				for _, m := range markers {
					if !strings.Contains(joined(def), m) {
						t.Fatalf("the default output lacks %q for %v: the marker is stale", m, surf)
					}
				}
			}
			for _, tc := range surfaceLists {
				t.Run(strings.ReplaceAll(tc.list, ",", "_"), func(t *testing.T) {
					t.Parallel()
					parsed, err := ParseSurface(tc.list)
					if err != nil {
						t.Fatal(err)
					}
					if parsed != tc.set.Resolve() {
						t.Fatalf("ParseSurface(%q) = %b, want %b", tc.list, parsed, tc.set.Resolve())
					}
					// a unit with a variable table carries four surfaces whatever is named
					want := parsed.ResolveFor(unit.variable)
					files, err := GenerateSurface(u, nil, parsed)
					if err != nil {
						t.Fatal(err)
					}
					if tc.set == SurfaceAll {
						sameFiles(t, "the full list vs default", files, def)
					}
					text := joined(files)
					for surf, markers := range unit.markers {
						for _, m := range markers {
							if has := strings.Contains(text, m); has != want.Has(surf) {
								t.Errorf("--surface %s: %q present=%v, want %v", tc.list, m, has, want.Has(surf))
							}
						}
					}
					for surf, suffix := range unit.files {
						found := false
						for name := range files {
							found = found || strings.HasSuffix(name, suffix)
						}
						// a surface whose file the full set never emits for this unit
						// (a variable table has no block form) stays absent
						inDefault := false
						for name := range def {
							inDefault = inDefault || strings.HasSuffix(name, suffix)
						}
						if found != (want.Has(surf) && inDefault) {
							t.Errorf("--surface %s: file *%s present=%v, want %v (files %v)", tc.list, suffix, found, want.Has(surf), names(files))
						}
					}
					compileAndRun(t, u, files, unit.roundTrip)
				})
			}
		})
	}
	t.Run("refuses", func(t *testing.T) {
		t.Parallel()
		for _, bad := range []string{"nope", "wire,,json", "Wire"} {
			if _, err := ParseSurface(bad); err == nil || !strings.Contains(err.Error(), "--surface") {
				t.Errorf("ParseSurface(%q) = %v, want an error naming --surface", bad, err)
			}
		}
		if s, err := ParseSurface(""); err != nil || s != SurfaceAll {
			t.Errorf("an empty list is the default: %b %v", s, err)
		}
	})
}

func joined(files map[string][]byte) string {
	var b strings.Builder
	for _, name := range names(files) {
		b.Write(files[name])
	}
	return b.String()
}

func names(files map[string][]byte) []string {
	var out []string
	for name := range files {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sameFiles(t *testing.T, what string, a, b map[string][]byte) {
	t.Helper()
	if fmt.Sprint(names(a)) != fmt.Sprint(names(b)) {
		t.Fatalf("%s: files differ: %v vs %v", what, names(a), names(b))
	}
	for name, data := range a {
		if string(data) != string(b[name]) {
			t.Fatalf("%s: %s differs byte for byte", what, name)
		}
	}
}

// compileAndRun builds the packet and table output into a temporary module and
// runs the round trip, so a subset that drops something it still names fails
// to compile here.
func compileAndRun(t *testing.T, u *ir.Unit, tables map[string][]byte, roundTrip string) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}
	runtime := slowtest.SerializeGo(t, "../../..")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["roundtrip_test.go"] = []byte(roundTrip)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated subset does not compile and round-trip: %v\n%s", err, out)
	}
}
