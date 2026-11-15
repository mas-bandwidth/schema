package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGoCookWriteZeroesDeadAndAbsentBytes cooks a root whose optional payload is
// absent and one whose counted array rides below its capacity, in both byte
// orders, through the generated Go writer, and holds the images byte for byte
// to the committed cook-write fixtures (docs/SPEC-TABLES.md §7.2, §7.6;
// docs/rulings.json fixed-dead-bytes-are-zero): an absent optional's payload
// and every dead counted-array slot are zero, whatever the element's declared
// default is.
func TestGoCookWriteZeroesDeadAndAbsentBytes(t *testing.T) {
	t.Parallel()
	u := loadUnitPaths(t, "../../..", []string{"tables/examples"})
	lineage := conformanceLineage(t, u)
	for _, tc := range []struct {
		name, root, stain string
	}{
		{"keyed_config", "KeyedConfig", `for h := range values[0].Hulls {
		for i := range values[0].Hulls[h].Turrets {
			if !values[0].Hulls[h].Turrets[i].GunnerPresent {
				GunnerConfigReset(&values[0].Hulls[h].Turrets[i].Gunner)
			}
		}
	}`},
		{"root_full", "RootConfig", `for i := values[0].WeaponsCount; i < int32(len(values[0].Weapons)); i++ {
		WeaponConfigReset(&values[0].Weapons[i])
	}
	for i := values[0].ProfilesCount; i < int32(len(values[0].Profiles)); i++ {
		ProfileConfigReset(&values[0].Profiles[i])
	}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wire := fixturePath(t, "testdata/conformance/tables/wire/"+tc.name+".bin")
			little := fixturePath(t, "testdata/conformance/tables/cook-write/"+tc.name+".cook")
			big := fixturePath(t, "testdata/conformance/tables/cook-write/"+tc.name+"-be.cook")
			probe := fmt.Sprintf(`package tabledemo

import (
	"bytes"
	"os"
	"testing"
)

func TestZeroDeadAndAbsent(t *testing.T) {
	wire, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]%s, 1)
	%sReset(&values[0])
	plan := make([]TableFixedEntry, 1<<14)
	var report TableReport
	if %sFixedLoad(values, wire, plan, &report) != 1 || report != (TableReport{}) {
		t.Fatalf("load: %%+v", report)
	}
	%s
	for _, tc := range []struct {
		name  string
		file  string
		order TableByteOrder
	}{
		{"little", %q, TableByteOrderLittle},
		{"big", %q, TableByteOrderBig},
	} {
		want, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]byte, %sCookMeasure(&values[0]))
		if !%sCookFrom(&values[0], got, tc.order) {
			t.Fatalf("%%s: cook refused", tc.name)
		}
		if !bytes.Equal(got, want) {
			for i := range want {
				if i == len(got) || got[i] != want[i] {
					t.Fatalf("%%s: cook differs at byte %%d: got %%#x want %%#x", tc.name, i, got[i], want[i])
				}
			}
			t.Fatalf("%%s: cook wrote %%d bytes, want %%d", tc.name, len(got), len(want))
		}
	}
}
`, wire, tc.root, tc.root, tc.root, tc.stain, little, big, tc.root, tc.root)
			runCookZeroProbe(t, u, lineage, probe)
		})
	}
}

// runCookZeroProbe emits the unit with the shared runtime the conformance leg
// builds, then compiles and runs the probe against it.
func runCookZeroProbe(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry, probe string) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateRuntime(u, lineage, RuntimeShared)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	runtime := slowtest.SerializeGo(t, root)
	dir := t.TempDir()
	files["go.mod"] = []byte(fmt.Sprintf(`module probe

go 1.26

require (
	github.com/mas-bandwidth/schema/v2 v2.0.0
	github.com/mas-bandwidth/serialize.go/v2 v2.0.0
)
replace github.com/mas-bandwidth/schema/v2 => %q
replace github.com/mas-bandwidth/serialize.go/v2 => %q
`, root, runtime))
	files["values_test.go"] = []byte(probe)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}

func fixturePath(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("../../..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// conformanceLineage is the unit's committed lock in the Go backend's spelling,
// the lineage `schema generate` hands the emitter for tables/examples.
func conformanceLineage(t *testing.T, u *ir.Unit) map[string][]FixedLineageEntry {
	t.Helper()
	paths := make([]string, 0, len(u.Files))
	for _, f := range u.Files {
		if f != nil && f.Path != "" {
			paths = append(paths, f.Path)
		}
	}
	lock, ok, err := lockfile.Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("tables/examples carries no schema.lock")
	}
	out := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		for _, e := range lockfile.Lineage(lock, st.WireName()) {
			out[st.Name] = append(out[st.Name], FixedLineageEntry{
				Wire:    e.Wire,
				Layout:  e.Layout,
				Record:  e.Record + 8,
				Retired: e.Retired,
				Reason:  e.Reason,
			})
		}
	}
	return out
}
