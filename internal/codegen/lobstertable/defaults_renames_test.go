package lobstertable_test

import (
	"encoding/hex"
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

// defaultsProbeProgram is one Lobster main: it imports every table module, the load
// and this card's runtime, prints the unit's BuildVersion, and loads (then
// RE-LOADS) each wire, printing the report line and THE VALUE DUMP each time.
// Each row then saves the loaded record and re-loads what was written, so the
// save round trip is held against the Go engine for every construct save
// reaches (defaults, renames, the build version).
func defaultsProbeProgram(g *loadUnit, roots []string) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\n")
	b.WriteString("import .schema_save\n")
	b.WriteString("import .defaults_renames\n")
	b.WriteString(keyedRegistrations(g.u))
	b.WriteString(dumpLobster)
	fmt.Fprintf(&b, "print(\"build:\" + lowercase(number_to_string(%s.BuildVersion, 16, 16)))\n", g.u.Package)
	for i, r := range roots {
		fmt.Fprintf(&b, "run(%q, \"w%d\")\n", r, i)
		fmt.Fprintf(&b, "run(%q, \"w%d\")\n", r, i)
		fmt.Fprintf(&b, "save_rel(%q, \"w%d\")\n", r, i)
	}
	return b.String()
}

// saveRel is the save round trip's tail: load the wire, save the loaded
// record, print the saved hex (or "" when the runtime can refuse), then
// reload the saved bytes and print the re-load's report line and THE VALUE
// DUMP, so a Lobster save_round_trip can be held against the reference.
const saveRel = `def save_rel(root:string, file:string):
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load_tolerant(root, data, rep)
    if not rec:
        print("")
        print(report_line(rep))
        print("0")
        return
    let out = schema_rt.save(rec)
    let saved = hexs(out)
    print(saved)
    let rep2 = schema_rt.LoadReport {}
    let rec2 = schema_rt.load_tolerant(root, out, rep2)
    print(report_line(rep2))
    labels = []
    var dump = "nil"
    if rec2:
        push(labels, rec2)
        dump = drec(rec2)
    let n = (length(dump) + 39999) / 40000
    print("{n}")
    var i = 0
    while i < n:
        print(substring(dump, i * 40000, min(40000, length(dump) - i * 40000)))
        i++
`

// runProbe generates the NEW unit, runs the probe program, and returns the
// printed build version, each row's (report, dump) for the load and for the
// re-load, and each row's save round trip (saved hex, reload report, dump).
// A row the load refuses prints an empty save; an empty save with no reload
// result means the load itself went wrong.
func runProbe(t *testing.T, g *loadUnit, rows []loadRow) (string, [][2]string, [][2]string, []saveRound) {
	t.Helper()
	var roots []string
	inputs := map[string][]byte{}
	for i, row := range rows {
		inputs[fmt.Sprintf("w%d", i)] = row.data
		roots = append(roots, row.root)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"),
		[]byte(defaultsProbeProgram(g, roots)+saveRel), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	if len(out) == 0 || !strings.HasPrefix(out[0], "build:") {
		t.Fatalf("the program did not print the build version: %q", out)
	}
	build := strings.TrimPrefix(out[0], "build:")
	out = out[1:]
	var first, again [][2]string
	var saves []saveRound
	for ri := range rows {
		for pass := range 2 {
			if len(out) < 2 {
				t.Fatalf("unit %s row %d pass %d: the program's output ends early (have %d lines)", rows[0].unit, ri, pass, len(out))
			}
			var n int
			leftover, _ := fmt.Sscanf(out[1], "%d", &n)
			if leftover == 0 || len(out) < 2+n {
				t.Fatalf("unit %s row %d pass %d: the output is not report, count, dump:\nout[0]=%q\nout[1]=%q\nout[2]=%q\nall=%q", rows[0].unit, ri, pass, out[0], out[1], out[2], out)
			}
			pair := [2]string{out[0], strings.Join(out[2:2+n], "")}
			if pass == 0 {
				first = append(first, pair)
			} else {
				again = append(again, pair)
			}
			out = out[2+n:]
		}
		var s saveRound
		if len(out) < 4 {
			t.Fatalf("unit %s row %d: the program's save output ends early (have %d lines)", rows[0].unit, ri, len(out))
		}
		s.saved = out[0]
		s.report = out[1]
		var sn int
		leftover, _ := fmt.Sscanf(out[2], "%d", &sn)
		if leftover == 0 || len(out) < 3+sn {
			t.Fatalf("unit %s row %d: the save output is not saved, report, count, dump:\nout[0]=%q\nout[1]=%q\nout[2]=%q\nall=%q", rows[0].unit, ri, out[0], out[1], out[2], out)
		}
		s.dump = strings.Join(out[3:3+sn], "")
		saves = append(saves, s)
		out = out[3+sn:]
	}
	if len(out) != 0 {
		t.Fatalf("unit %s: %d lines past the last row", rows[0].unit, len(out))
	}
	return build, first, again, saves
}

// refusalProgram is one Lobster main for the WRITE refusal (§2.15): it builds
// a fresh record and puts a retired variant in a scalar field and in an array
// element, then names what the strict write answered.
func refusalProgram(g *loadUnit) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\n")
	b.WriteString("import .schema_save\n")
	b.WriteString("import .defaults_renames\n")
	b.WriteString(`def refusal(root:string, field:string, value:int, element:bool, at:int):
    let d = schema_rt.lookup_table(root)
    assert d
    let rec = schema_rt.new_record(d)
    for(rec.fields) sl:
        if sl.fd.name == field:
            if element:
                sl.elems[at].i = value
                sl.count = at + 1
            else:
                sl.cell.i = value
    let out = schema_rt.save_strict(rec)
    print(if out == "": "refused" else: "wrote")
`)
	b.WriteString("refusal(\"Ship\", \"kind\", 2, false, 0)\n")
	b.WriteString("refusal(\"Ship\", \"kinds\", 2, true, 1)\n")
	return b.String()
}

// runRefusalProbe generates and runs the refusal probes, returning the two
// answers in order.
func runRefusalProbe(t *testing.T, g *loadUnit) []string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"),
		[]byte(refusalProgram(g)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)), "\n")
	return strings.Split(out, "\n")
}

// goRefuses is the reference's strict write (§2.15): encode a record holding
// a retired variant and report whether the writer refused it.
func goRefuses(t *testing.T, u *ir.Unit, rootName, field string, value uint64, element bool, at int) bool {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(rootName))
	fv := probeField(t, inst, field)
	if element {
		fv.Elems[at].U = value
		fv.Count = at + 1
	} else {
		fv.Cell.U = value
	}
	_, err := tablewire.Encode(m, inst)
	return err != nil
}

// saveRound is one row's save + re-load under Lobster: the bytes saved (empty
// if save refused), the re-load report line, and the re-load value dump.
type saveRound struct {
	saved  string
	report string
	dump   string
}

// goSaveLoad is the reference's save round trip: decode the wire, encode the
// record, decode the saved bytes, and return the saved hex, the re-load report
// line, and the re-load value dump. A Go writer refusal (a strict save of a
// deprecated variant, §2.15) is returned as refused=true and the caller
// records the comparison as PATHS-PROPOSED.
func goSaveLoad(u *ir.Unit, rootName string, wire []byte) (savedHex, report, dump string, refused bool) {
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(rootName))
	var rep tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &rep); err != nil {
		return "", reportLine(rep), "nil", true
	}
	saved, err := tablewire.Encode(m, inst)
	if err != nil {
		return "", "0,0,0,0,0,false,refused", "nil", true
	}
	m2 := tabletext.NewModel(u)
	inst2 := m2.New(m2.Lookup(rootName))
	var rep2 tabletext.Report
	_, _ = tablewire.Decode(m2, inst2, saved, &rep2)
	line := reportLine(rep2)
	d := &goDumper{labels: map[*tabletext.Instance]int{inst2: 0}, next: 1}
	return hex.EncodeToString(saved), line, d.rec(inst2), false
}

// reportLine is the load report line one instance wrote, in the harness's
// spelling (load_test.go's goLoad).
func reportLine(rep tabletext.Report) string {
	verdict := "read"
	if rep.Refused {
		verdict = "refused"
	}
	return fmt.Sprintf("%d,%d,%d,%d,%d,%t,%s",
		rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate,
		rep.Malformed, verdict)
}

// TestLobsterDefaultsAndRenames holds this card's constructs on the OLD wire
// the Go engine wrote, against the Go engine itself reading that wire under
// the NEW schema: string, bytes and flags defaults (§4), a table rename, a
// field rename, a nested `type` field rename, an enum variant rename and a
// union arm rename (§5), and a variant retired in place (§2.15). Each
// construct is named with its count. A Lobster load and a RE-LOAD must both
// agree with the reference report for report and dump for dump; the save
// round trip must agree byte for byte and re-load for re-load with the Go
// engine, for every construct save can carry. The write refusal for a
// `| deprecated` variant is the strict write: save_strict refuses a retired
// variant in a scalar field and in an array element, as the reference does.
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
	build, first, again, saves := runProbe(t, g, rows)

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

	for i, row := range rows {
		gtSaved, gtReport, gtDump, refused := goSaveLoad(g.u, row.root, row.data)
		if refused {
			t.Errorf("%s %s: the Go writer refused to save this row, but save can carry it (docs/SPEC-TABLES.md §2.15, §5)", row.kind, row.name)
			continue
		}
		if saves[i].saved != gtSaved {
			t.Errorf("%s %s: the saved bytes are not the reference's:\nlobster: %s\ngo:      %s", row.kind, row.name, saves[i].saved, gtSaved)
			continue
		}
		if saves[i].report != gtReport {
			t.Errorf("%s %s: the saved-reload report is not the reference's:\nlobster: %s\ngo:      %s", row.kind, row.name, saves[i].report, gtReport)
			continue
		}
		if saves[i].dump != gtDump {
			t.Errorf("%s %s: the saved-reload value is not the reference's at byte %s", row.kind, row.name, divergence(saves[i].dump, gtDump))
		}
	}
	refusals := runRefusalProbe(t, g)
	probes := []struct {
		field   string
		value   uint64
		element bool
		at      int
	}{
		{field: "kind", value: 2},
		{field: "kinds", value: 2, element: true, at: 1},
	}
	for i, p := range probes {
		if !goRefuses(t, g.u, "Ship", p.field, p.value, p.element, p.at) {
			t.Errorf("write refusal %s: the Go writer did not refuse a retired variant", p.field)
		}
		got := ""
		if i < len(refusals) {
			got = refusals[i]
		}
		if got != "refused" {
			t.Errorf("write refusal %s: the strict write answered %q, want refused", p.field, got)
		}
	}
	if t.Failed() {
		return
	}
	t.Logf("defaults: string=1 bytes=1 flags=1 (old wire, new reader; load, re-load and save-reload equal Go)")
	t.Logf("renames: table=1 field=1 type-field=1 variant=1 arm=1 (old ids, new names; save round trip agrees with Go)")
	t.Logf("deprecated: scalar=1 element=1 (reads as the field/element default, §2.15; save round trip agrees with Go)")
	t.Logf("write refusal: scalar=1 element=1 (a retired variant is refused on save, §2.15; save_strict answers \"\")")
	t.Logf("build version: 1 value %s (docs/SPEC-TABLES.md §20)", build)
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
	_, first, _, _ := runProbe(t, g, rows)
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
	_, got, _, _ := runProbe(t, g, rows)
	if got[0][0] == goLine && got[0][1] == goDump {
		t.Fatalf("the patched runtime still matches Go: %s %s", got[0][0], got[0][1])
	}
	t.Logf("the comparison went red under the patch: deprecated got %s, want %s", got[0][1], goDump)
}
