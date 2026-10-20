package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// defaultsOld is the schema a saved file was written under: an unrenamed
// table and field, a live Silver variant and a live spear arm.
const defaultsOld = `package defaultsprobe

flags Caps { Jump, Crouch, Dash }

enum ShipType
{
    Fighter
    Bomber
    Silver
}

type Damage
{
    amount uint16 = 5
    old_tag uint8 = 2
}

union Effect
{
    spear Damage
    pong
}

table Vessel
{
    name string(16)
    tag bytes(8)
    caps Caps
    velocity float32 = 2.5
    sort ShipType
    effect Effect
    kinds [..4]ShipType
}
`

// defaultsNew is the same unit after the renames (docs/SPEC-TABLES.md §5),
// with string, bytes and flags defaults (§4) and a variant retired in place
// (§2.15). Every `was` keeps the old wire id, so defaultsOld's bytes load.
const defaultsNew = `package defaultsprobe

flags Caps { Jump, Crouch, Dash }

enum ShipType
{
    Fighter
    Bomber | deprecated
    Argent | was = "Silver"
}

type Damage
{
    amount uint16 = 5
    tag uint8 = 2 | was = "old_tag"
}

union Effect
{
    lance Damage | was = "spear"
    pong
}

table Ship | was = "Vessel"
{
    name string(16) = "untitled"
    tag bytes(8) = "ab"
    caps Caps = { Jump, Crouch }
    speed float32 = 2.5 | was = "velocity"
    kind ShipType = Fighter | was = "sort"
    effect Effect
    kinds [..4]ShipType
}
`

func writeSchema(t *testing.T, name, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// probeField is one named field of a tabletext.Instance.
func probeField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("the value has no field %s", name)
	return nil
}

// encodeOld builds one value in the OLD unit's model and returns its wire:
// the bytes a file written before this build carries.
func encodeOld(t *testing.T, u *loadUnit, root string, fill func(*tabletext.Model, *tabletext.Instance)) []byte {
	t.Helper()
	m := tabletext.NewModel(u.u)
	inst := m.New(m.Lookup(root))
	fill(m, inst)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// probeProgram is one Lobster main: it imports every table module, the load
// and this card's runtime, prints the unit's BuildVersion, and loads (then
// RE-LOADS) each wire, printing the report line and THE VALUE DUMP each time.
func probeProgram(g *loadUnit, roots []string) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\n")
	b.WriteString("import .defaults_renames\n")
	b.WriteString(keyedRegistrations(g.u))
	b.WriteString(dumpLobster)
	fmt.Fprintf(&b, "print(\"build:\" + lowercase(number_to_string(%s.BuildVersion, 16, 16)))\n", g.u.Package)
	for i, r := range roots {
		fmt.Fprintf(&b, "run(%q, \"w%d\")\n", r, i)
		fmt.Fprintf(&b, "run(%q, \"w%d\")\n", r, i)
	}
	return b.String()
}

// runProbe generates the NEW unit, loads every row's OLD wire twice under the
// pinned Lobster toolchain, and returns the printed build version, each
// row's (report, dump) for the load and for the re-load.
func runProbe(t *testing.T, g *loadUnit, rows []loadRow) (string, [][2]string, [][2]string) {
	t.Helper()
	var roots []string
	inputs := map[string][]byte{}
	for i, row := range rows {
		inputs[fmt.Sprintf("w%d", i)] = row.data
		roots = append(roots, row.root)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(probeProgram(g, roots)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	if len(out) == 0 || !strings.HasPrefix(out[0], "build:") {
		t.Fatalf("the program did not print the build version: %q", out)
	}
	build := strings.TrimPrefix(out[0], "build:")
	out = out[1:]
	var first, again [][2]string
	for range rows {
		for pass := 0; pass < 2; pass++ {
			if len(out) < 2 {
				t.Fatalf("unit %s: the program's output ends early", rows[0].unit)
			}
			var n int
			if _, err := fmt.Sscanf(out[1], "%d", &n); err != nil || len(out) < 2+n {
				t.Fatalf("unit %s: the output is not report, count, dump: %q", rows[0].unit, out[1])
			}
			pair := [2]string{out[0], strings.Join(out[2:2+n], "")}
			if pass == 0 {
				first = append(first, pair)
			} else {
				again = append(again, pair)
			}
			out = out[2+n:]
		}
	}
	if len(out) != 0 {
		t.Fatalf("unit %s: %d lines past the last row", rows[0].unit, len(out))
	}
	return build, first, again
}

// TestLobsterDefaultsAndRenames holds this card's constructs on the OLD wire
// the Go engine wrote, against the Go engine itself reading that wire under
// the NEW schema: string, bytes and flags defaults (§4), a table rename, a
// field rename, a nested `type` field rename, an enum variant rename and a
// union arm rename (§5), and a variant retired in place (§2.15). Each
// construct is named with its count. A Lobster load and a RE-LOAD must both
// agree with the reference report for report and dump for dump. The save
// half and the write refusal are reported PATHS-PROPOSED: runtime/save.lobster
// does not exist at this card's BASE, so there is no save walk to join.
func TestLobsterDefaultsAndRenames(t *testing.T) {
	t.Parallel()
	old, err := generateLoadUnit(t, []string{writeSchema(t, "Defaults", defaultsOld)})
	if err != nil {
		t.Fatalf("the surface refuses the old unit: %v", err)
	}
	g, err := generateLoadUnit(t, []string{writeSchema(t, "Defaults", defaultsNew)})
	if err != nil {
		t.Fatalf("the surface refuses the new unit: %v", err)
	}

	// all defaults: the old writer elides them, the new reader lays in the
	// declared string, bytes and flags defaults
	defaultsWire := encodeOld(t, old, "Vessel", func(m *tabletext.Model, inst *tabletext.Instance) {
		probeField(t, inst, "name").Cell.Str = []byte("untitled")
		probeField(t, inst, "name").Count = 0
		probeField(t, inst, "tag").Cell.Str = nil
		probeField(t, inst, "tag").Count = 0
		probeField(t, inst, "caps").Cell.U = 0
		probeField(t, inst, "velocity").Cell.F = 2.5
	})

	// every rename: the old names carry old ids, the new descriptors keep them
	renameWire := encodeOld(t, old, "Vessel", func(m *tabletext.Model, inst *tabletext.Instance) {
		probeField(t, inst, "name").Cell.Str = []byte("old")
		probeField(t, inst, "name").Count = 3
		probeField(t, inst, "tag").Cell.Str = []byte{0x7a, 0x7b}
		probeField(t, inst, "tag").Count = 2
		probeField(t, inst, "caps").Cell.U = 5
		probeField(t, inst, "velocity").Cell.F = 4.5
		probeField(t, inst, "sort").Cell.U = 3 // Silver, renamed Argent
		effect := probeField(t, inst, "effect")
		effect.Cell.U = 1 // spear, renamed lance
		damage := m.New(m.Lookup("Damage"))
		probeField(t, damage, "amount").Cell.U = 9
		probeField(t, damage, "old_tag").Cell.U = 7
		effect.Cell.Tab = damage
		kinds := probeField(t, inst, "kinds")
		kinds.Elems[0].U = 1
		kinds.Elems[1].U = 3
		kinds.Count = 2
	})

	// a retired variant in a scalar and in an array element
	deprecatedWire := encodeOld(t, old, "Vessel", func(m *tabletext.Model, inst *tabletext.Instance) {
		probeField(t, inst, "name").Count = 0
		probeField(t, inst, "tag").Count = 0
		probeField(t, inst, "sort").Cell.U = 2 // Bomber, retired in place
		kinds := probeField(t, inst, "kinds")
		kinds.Elems[0].U = 1
		kinds.Elems[1].U = 2 // Bomber
		kinds.Count = 2
	})

	rows := []loadRow{
		{kind: "instance", name: "defaults", unit: "defaultsprobe", root: "Ship", data: defaultsWire},
		{kind: "instance", name: "renames", unit: "defaultsprobe", root: "Ship", data: renameWire},
		{kind: "instance", name: "deprecated", unit: "defaultsprobe", root: "Ship", data: deprecatedWire},
	}
	build, first, again := runProbe(t, g, rows)

	wantBuild := fmt.Sprintf("%016x", ir.BuildVersion(g.u))
	if build != wantBuild {
		t.Errorf("BuildVersion: lobster %s, go %s (docs/SPEC-TABLES.md §20)", build, wantBuild)
	}

	for i, row := range rows {
		goLine, goDump := goLoad(g.u, row.root, row.data)
		for pass, got := range [][2]string{first[i], again[i]} {
			passName := "load"
			if pass == 1 {
				passName = "re-load"
			}
			if got[0] != goLine {
				t.Errorf("%s %s: the %s report is not the reference's:\nlobster: %s\ngo:      %s", row.kind, row.name, passName, got[0], goLine)
				continue
			}
			if got[1] != goDump {
				t.Errorf("%s %s: the %s value is not the reference's at byte %s", row.kind, row.name, passName, divergence(got[1], goDump))
				continue
			}
		}
	}
	if t.Failed() {
		return
	}
	t.Logf("defaults: string=1 bytes=1 flags=1 (old wire, new reader; load and re-load equal Go)")
	t.Logf("renames: table=1 field=1 type-field=1 variant=1 arm=1 (old ids, new names)")
	t.Logf("deprecated: scalar=1 element=1 (reads as the field/element default, §2.15)")
	t.Logf("build version: 1 value %s (docs/SPEC-TABLES.md §20)", build)
	t.Logf("save and the write refusal: not carried — runtime/save.lobster is absent at BASE (PATHS-PROPOSED)")
}

// TestLobsterDefaultsAndRenamesNegativeControl patches the emitted runtime's
// retired-variant default — the one line that yields the field default —
// and requires the deprecated row to diverge from Go, with the unpatched
// control green.
func TestLobsterDefaultsAndRenamesNegativeControl(t *testing.T) {
	t.Parallel()
	old, err := generateLoadUnit(t, []string{writeSchema(t, "Defaults", defaultsOld)})
	if err != nil {
		t.Fatalf("the surface refuses the old unit: %v", err)
	}
	g, err := generateLoadUnit(t, []string{writeSchema(t, "Defaults", defaultsNew)})
	if err != nil {
		t.Fatalf("the surface refuses the new unit: %v", err)
	}
	wire := encodeOld(t, old, "Vessel", func(m *tabletext.Model, inst *tabletext.Instance) {
		probeField(t, inst, "sort").Cell.U = 2 // Bomber, retired in place
	})
	rows := []loadRow{{kind: "instance", name: "deprecated", unit: "defaultsprobe", root: "Ship", data: wire}}
	goLine, goDump := goLoad(g.u, "Ship", wire)
	_, first, _ := runProbe(t, g, rows)
	if first[0][0] != goLine || first[0][1] != goDump {
		t.Fatalf("the control, unpatched: lobster %s %s, go %s %s", first[0][0], first[0][1], goLine, goDump)
	}
	path := filepath.Join(g.dir, "defaults_renames.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "            cell.i = fd.def_int.lo\n"
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the retired-variant default anchor is in the emitted runtime %d times, want 1", n)
	}
	patched := strings.Replace(string(src), anchor, "            cell.i = 0\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	_, got, _ := runProbe(t, g, rows)
	if got[0][0] == goLine && got[0][1] == goDump {
		t.Fatalf("the patched runtime still matches Go: %s %s", got[0][0], got[0][1])
	}
	t.Logf("the comparison went red under the patch: deprecated got %s, want %s", got[0][1], goDump)
}
