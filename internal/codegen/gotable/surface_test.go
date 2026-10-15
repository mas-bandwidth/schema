package gotable

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// surfaceSchema carries a fixed table that has a block form (Block), a fixed
// table whose closure holds a union (so it has no block form, §19.3), an
// enum-keyed array, an optional array and a union — every surface has
// something to emit.
const surfaceSchema = `package probe
flags Caps { Jump, Fly }
enum Grade {
	Gold | was = "OldGold"
}
union Payload {
	n int32
	text string(8)
	ping
}
fixed table Block {
	id uint32
	flag bool
}
fixed table Root {
	title string(12)
	caps Caps = { Jump, Fly }
	payload Payload
	values ?[..3]int32
	grades ?[2]Grade
}
`

// mustGenSurfaces generates the unit with exactly the named surfaces.
func mustGenSurfaces(t *testing.T, u *ir.Unit, list string) map[string][]byte {
	t.Helper()
	out, err := GenerateLineageForSurfaces(u, nil, list)
	if err != nil {
		t.Fatalf("generate surfaces %q: %v", list, err)
	}
	return out
}

// compileSurfaces proves a surface-restricted generation compiles: the packet
// emitter's output plus only the named table surfaces, built as one module.
func compileSurfaces(t *testing.T, u *ir.Unit, list string) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineageForSurfaces(u, nil, list)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	for name, data := range files {
		if err := os.WriteFile(dir+"/"+name, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles the generated unit)")
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("surface %q did not compile: %v\n%s", list, err, out)
	}
}

func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, surfaceSchema)

	full := mustGenSurfaces(t, u, "")
	for _, name := range []string{"ProbeTable.go", "ProbeTableJson.go", "ProbeBlock.go", "ProbeCook.go", "ProbeView.go"} {
		if _, ok := full[name]; !ok {
			t.Fatalf("default generation lacks %s", name)
		}
	}

	explicit := mustGenSurfaces(t, u, "wire,fixed,json,cook,block,view,message,retain")
	for name, data := range full {
		if !bytes.Equal(explicit[name], data) {
			t.Fatalf("default differs from the explicit full surface set for %s (docs/SPEC-TABLES.md §8.4)", name)
		}
	}
	for name := range explicit {
		if _, ok := full[name]; !ok {
			t.Fatalf("explicit full set emits %s that the default does not", name)
		}
	}

	cases := []struct {
		name, list string
		want       []string
		not        []string
	}{
		{"wire", "wire", []string{"ProbeTable.go"}, []string{"ProbeBlock.go", "ProbeCook.go", "ProbeTableJson.go", "ProbeView.go"}},
		{"fixed", "fixed", []string{"ProbeTable.go"}, []string{"ProbeBlock.go", "ProbeCook.go", "ProbeTableJson.go", "ProbeView.go"}},
		{"json", "json", []string{"ProbeTable.go", "ProbeTableJson.go"}, []string{"ProbeBlock.go", "ProbeCook.go", "ProbeView.go"}},
		{"cook", "cook", []string{"ProbeTable.go", "ProbeCook.go", "ProbeTableJson.go"}, []string{"ProbeBlock.go", "ProbeView.go"}},
		{"block", "block", []string{"ProbeTable.go", "ProbeBlock.go"}, []string{"ProbeCook.go", "ProbeTableJson.go", "ProbeView.go"}},
		{"view", "view", []string{"ProbeTable.go", "ProbeView.go"}, []string{"ProbeBlock.go", "ProbeCook.go", "ProbeTableJson.go"}},
		{"message", "message", []string{"ProbeTable.go"}, []string{"ProbeBlock.go", "ProbeCook.go", "ProbeTableJson.go", "ProbeView.go"}},
	}
	for _, c := range cases {
		files := mustGenSurfaces(t, u, c.list)
		for _, w := range c.want {
			if _, ok := files[w]; !ok {
				t.Errorf("%s: missing %s", c.name, w)
			}
		}
		for _, n := range c.not {
			if _, ok := files[n]; ok {
				t.Errorf("%s: should not emit %s", c.name, n)
			}
		}
		compileSurfaces(t, u, c.list)
	}

	// the full set compiles and round-trips through the package's own helper
	runGenerated(t, surfaceSchema, surfaceTestSource)
}

const surfaceTestSource = `package probe
import("testing";"bytes";"encoding/binary";"hash/fnv")
func TestSurfaceRoundtrip(t *testing.T) {
	var v, got Root
	RootReset(&v)
	v.TitleLength = 4
	copy(v.Title[:], "ship")
	v.Payload.Type = PayloadTypeN
	v.Payload.N = 7
	v.ValuesPresent = true
	v.ValuesCount = 2
	v.GradesPresent = true
	v.Grades[1] = GradeGold
	n := RootMeasure(&v)
	b := make([]byte, n)
	if RootSave(&v, b) != n {
		t.Fatal("save")
	}
	var r TableReport
	if !RootLoad(&got, b, &r) || r != (TableReport{}) || got != v {
		t.Fatalf("roundtrip: %+v %+v", got, r)
	}
	h := fnv.New64a()
	h.Write([]byte("OldGold"))
	needle := binary.LittleEndian.AppendUint64(nil, h.Sum64())
	if !bytes.Contains(b, needle) {
		t.Fatal("renamed variant lost its id")
	}
}
`
