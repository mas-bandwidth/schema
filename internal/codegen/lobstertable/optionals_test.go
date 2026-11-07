package lobstertable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// optionalsImport joins this card's runtime file to the load and save walkers
// (docs/PORT-LOBSTER.md, THE LOAD): every runtime/*.lobster file is emitted
// beside the modules, and importing optionals.lobster is the per-kind hook
// this card's constructs ride. The shared helpers import only load and save,
// so the two below are the same program with the hook added.
func optionalsImport(program string) string {
	return strings.Replace(program, "import .load\n", "import .load\nimport .optionals\n", 1)
}

// runOptionalsLoad is runUnit with optionals.lobster imported, so the load
// half is exercised through this card's hook and every row's report line and
// value dump are held to the Go reference.
func runOptionalsLoad(t *testing.T, g *loadUnit, rows []loadRow) unitResult {
	t.Helper()
	var roots []string
	inputs := map[string][]byte{}
	res := unitResult{rows: rows}
	for i, row := range rows {
		wire := row.data
		if wire == nil {
			var err error
			if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
				t.Fatalf("%s: %v", row.name, err)
			}
		}
		inputs[fmt.Sprintf("w%d", i)] = wire
		roots = append(roots, row.root)
		line, dump := goLoad(g.u, row.root, wire)
		res.goLine = append(res.goLine, line)
		res.goDump = append(res.goDump, dump)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(optionalsImport(loadProgram(g, roots))), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	for range rows {
		var n int
		if len(out) < 2 {
			t.Fatalf("unit %s: the program's output ends early", rows[0].unit)
		}
		if _, err := fmt.Sscanf(out[1], "%d", &n); err != nil || len(out) < 2+n {
			t.Fatalf("unit %s: the program's output is not report, count, dump: %q", rows[0].unit, out[1])
		}
		res.lob = append(res.lob, out[0], strings.Join(out[2:2+n], ""))
		out = out[2+n:]
	}
	if len(out) != 0 {
		t.Fatalf("unit %s: %d lines past the last row", rows[0].unit, len(out))
	}
	return res
}

// runOptionalsSaveRows is runSaveRows with optionals.lobster imported: the
// saved bytes and the re-loaded dump are held to the reference through this
// card's hook.
func runOptionalsSaveRows(t *testing.T, g *loadUnit, rows []loadRow) saveResult {
	t.Helper()
	var roots []string
	inputs := map[string][]byte{}
	for i, row := range rows {
		wire := row.data
		if wire == nil {
			var err error
			if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
				t.Fatalf("%s: %v", row.name, err)
			}
		}
		inputs[fmt.Sprintf("w%d", i)] = wire
		roots = append(roots, row.root)
	}
	program := optionalsImport(saveProgram(g, roots))
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	res := saveResult{}
	for range rows {
		if len(out) < 2 {
			t.Fatalf("unit %s: the program's output ends early", rows[0].unit)
		}
		var hn int
		if _, err := fmt.Sscanf(out[0], "%d", &hn); err != nil || len(out) < 1+hn+1 {
			t.Fatalf("unit %s: the program's output is not hex count, hex, count, dump: %q", rows[0].unit, out[0])
		}
		res.saved = append(res.saved, strings.Join(out[1:1+hn], ""))
		out = out[1+hn:]
		var n int
		if _, err := fmt.Sscanf(out[0], "%d", &n); err != nil || len(out) < 1+n {
			t.Fatalf("unit %s: the program's output is not hex, count, dump: %q", rows[0].unit, out[0])
		}
		res.dump = append(res.dump, strings.Join(out[1:1+n], ""))
		out = out[1+n:]
	}
	if len(out) != 0 {
		t.Fatalf("unit %s: %d lines past the last row", rows[0].unit, len(out))
	}
	return res
}

// optionalRows are the manifest rows that use this card's reachable
// constructs: tabledemo's enum-keyed configuration (§3.2), tblg1's optional
// over a variable closure (§2.3) and tblp3's present/absent optional chain.
var optionalRows = map[string]bool{
	"keyed_config":            true,
	"keyed_default":           true,
	"optional_absent_no_edge": true,
	"optional_present_edge":   true,
	"chain_optional":          true,
	"chain_optional_empty":    true,
}

// optionalProbe is the optional-map unit no manifest row declares
// (docs/SPEC-TABLES.md §2.8): absent, present and present-empty are three
// values, held through the load and the save alike.
const optionalProbe = `package probe

table Item
{
    v int32
}

table Holder
{
    tags ?map[string(8)]int32
}
`

// requiredProbe is the `| required` unit no manifest row declares
// (docs/SPEC-TABLES.md §2.3): a required field rides even at zero, and a body
// that never names one is refused at any depth.
const requiredProbe = `package probe

table Kid
{
    x int32 | required
    y int32
}

table ReqHolder
{
    a int32 | required
    name string(8) | required
    kid Kid
    b int32
}
`

// lebBytes is one unsigned LEB128 count or reference, the wire's one number
// shape (docs/SPEC-TABLES.md §3).
func lebBytes(n int) []byte {
	var out []byte
	x := n
	for x >= 0x80 {
		out = append(out, byte(x&0x7f)|0x80)
		x >>= 7
	}
	return append(out, byte(x))
}

// wireBytes frames one body: the form byte, the body, the id table in
// first-use order and its count (docs/SPEC-TABLES.md §3).
func wireBytes(body []byte, ids []uint64) []byte {
	out := []byte{1}
	out = append(out, body...)
	for _, id := range ids {
		for i := range 8 {
			out = append(out, byte(id>>(8*i)))
		}
	}
	for i := range 8 {
		out = append(out, byte(uint64(len(ids))>>(8*i)))
	}
	return out
}

// mapProbeWire is the PRESENT-EMPTY optional map no reference writer spells:
// kind 14 over element kind 13 with count 0 (docs/SPEC-TABLES.md §2.8).
func mapProbeWire(id uint64) []byte {
	body := lebBytes(1)
	body = append(body, 14)
	inner := []byte{13}
	inner = append(inner, lebBytes(0)...)
	body = append(body, lebBytes(len(inner))...)
	body = append(body, inner...)
	body = append(body, 0)
	return wireBytes(body, []uint64{id})
}

// requiredDefaultWire names both required root fields at their zero defaults
// (docs/SPEC-TABLES.md §2.3).
func requiredDefaultWire(aID, nameID uint64) []byte {
	var body []byte
	body = append(body, lebBytes(1)...)
	body = append(body, 4, 0, 0, 0, 0)
	body = append(body, lebBytes(2)...)
	body = append(body, 12)
	body = append(body, lebBytes(0)...)
	body = append(body, 0)
	return wireBytes(body, []uint64{aID, nameID})
}

// requiredDefaultSavedWire is that record as the reference writer saves it:
// the required fields ride at zero, and the by-value kid now carries its own
// required x (docs/SPEC-TABLES.md §2.3).
func requiredDefaultSavedWire(aID, nameID, kidID, xID uint64) []byte {
	kid := lebBytes(4)
	kid = append(kid, 4, 0, 0, 0, 0)
	kid = append(kid, 0)
	var body []byte
	body = append(body, lebBytes(1)...)
	body = append(body, 4, 0, 0, 0, 0)
	body = append(body, lebBytes(2)...)
	body = append(body, 12)
	body = append(body, lebBytes(0)...)
	body = append(body, lebBytes(3)...)
	body = append(body, 13)
	body = append(body, lebBytes(len(kid))...)
	body = append(body, kid...)
	body = append(body, 0)
	return wireBytes(body, []uint64{aID, nameID, kidID, xID})
}

// requiredMissingWire is an older root layout that never named name.
func requiredMissingWire(aID uint64) []byte {
	var body []byte
	body = append(body, lebBytes(1)...)
	body = append(body, 4, 7, 0, 0, 0)
	body = append(body, 0)
	return wireBytes(body, []uint64{aID})
}

// requiredNestedMissingWire is a root whose nested Kid never named its
// required x (docs/SPEC-TABLES.md §2.3, THE RECORD'S at any depth).
func requiredNestedMissingWire(aID, kidID, yID uint64) []byte {
	kid := lebBytes(3)
	kid = append(kid, 4, 9, 0, 0, 0)
	kid = append(kid, 0)
	var body []byte
	body = append(body, lebBytes(1)...)
	body = append(body, 4, 7, 0, 0, 0)
	body = append(body, lebBytes(2)...)
	body = append(body, 13)
	body = append(body, lebBytes(len(kid))...)
	body = append(body, kid...)
	body = append(body, 0)
	return wireBytes(body, []uint64{aID, kidID, yID})
}

// probeFieldID is one field's effective wire id (docs/SPEC-TABLES.md §5).
func probeFieldID(t *testing.T, u *ir.Unit, table, field string) uint64 {
	t.Helper()
	st := u.Tables[table]
	if st == nil {
		t.Fatalf("the probe has no table %s", table)
	}
	for _, f := range st.Fields {
		if f.Name == field {
			return ir.TableFieldWireId(f)
		}
	}
	t.Fatalf("table %s has no field %s", table, field)
	return 0
}

// requiredReportProgram prints one row's missing_required count, the reason
// it names, and whether a record came back.
const requiredReportProgram = `def reqrun(root:string, file:string):
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load_tolerant(root, data, rep)
    print("{rep.missing_required}")
    print(rep.reason)
    print(if rec: "rec" else: "nil")
`

// runRequiredReports runs the missing-required rows through optionals.lobster
// and reads back their count, reason and record presence.
func runRequiredReports(t *testing.T, g *loadUnit, rows []loadRow) [][3]string {
	t.Helper()
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .optionals\n")
	b.WriteString(requiredReportProgram)
	inputs := map[string][]byte{}
	for i, row := range rows {
		inputs[fmt.Sprintf("w%d", i)] = row.data
		fmt.Fprintf(&b, "reqrun(%s, \"w%d\")\n", lobsterString(row.root), i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	if len(out) != 3*len(rows) {
		t.Fatalf("required probe: %d output lines, want %d: %q", len(out), 3*len(rows), out)
	}
	res := make([][3]string, len(rows))
	for i := range rows {
		copy(res[i][:], out[3*i:3*i+3])
	}
	return res
}

// optionalCounts is one construct's fields and instances, for the test's
// named count.
type optionalCounts struct {
	optional, keyed, optionalMap, required, instances int
}

// TestLobsterOptionals holds ?T, the optional variable closure, optional
// arrays, enum-keyed arrays and ?map[K]V to the Go engine: each unit is
// loaded, saved and re-loaded through the emitted runtimes, and the saved
// bytes and the re-loaded value dump must be the reference's byte for byte
// and report for report (docs/SPEC-TABLES.md §2.3, §2.8, §3.2;
// docs/PORT-LOBSTER.md, THE LOAD and THE HARNESS).
func TestLobsterOptionals(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	byUnit := map[string][]loadRow{}
	var order []string
	for _, row := range loadRows(t) {
		if row.kind != "instance" || !optionalRows[row.name] {
			continue
		}
		if _, seen := byUnit[row.unit]; !seen {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	counts := optionalCounts{}
	compared := 0
	compareUnit := func(g *loadUnit, rows []loadRow) {
		t.Helper()
		// LOAD, report for report and dump for dump (docs/SPEC-TABLES.md §2.3,
		// §2.8, §3.2): the optional and keyed shapes the walker dispatches on
		// are held to the Go reference before a byte is written.
		loaded := runOptionalsLoad(t, g, rows)
		for i, row := range rows {
			if got, exp := loaded.lob[2*i], loaded.goLine[i]; got != exp {
				t.Errorf("%s: the load report is not the reference's:\nlobster: %s\ngo:      %s", row.name, got, exp)
				continue
			}
			if got, exp := loaded.lob[2*i+1], loaded.goDump[i]; got != exp {
				t.Errorf("%s: the loaded value is not the reference's at %s", row.name, divergence(got, exp))
				continue
			}
		}
		var keep []loadRow
		var want [][]byte
		for _, row := range rows {
			wire := row.data
			if wire == nil {
				var err error
				if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
					t.Fatalf("%s: %v", row.name, err)
				}
			}
			out := row.saveData
			if out == nil {
				var err error
				if out, err = goSave(g, row.root, wire); err != nil {
					t.Fatalf("%s: the Go writer refused it: %v", row.name, err)
				}
			}
			keep = append(keep, row)
			want = append(want, out)
		}
		res := runOptionalsSaveRows(t, g, keep)
		for i, row := range keep {
			if got, exp := res.saved[i], hex.EncodeToString(want[i]); got != exp {
				t.Errorf("%s: the saved bytes are not the reference's:\nlobster: %s\ngo:      %s", row.name, got, exp)
				continue
			}
			if _, goDump := goLoad(g.u, row.root, want[i]); res.dump[i] != goDump {
				t.Errorf("%s: the re-loaded value is not the reference's at %s", row.name, divergence(res.dump[i], goDump))
				continue
			}
			compared++
			counts.instances++
		}
		for _, st := range g.u.Tables {
			for _, f := range st.Fields {
				if f.Type.Optional {
					counts.optional++
				}
				if f.KeyEnumRef != nil {
					counts.keyed++
				}
				if f.IsMap() && f.Type.Optional {
					counts.optionalMap++
				}
				if f.Required {
					counts.required++
				}
			}
		}
	}
	for _, unit := range order {
		paths, err := compiler.GatherPaths(prefix(units[unit]))
		if err != nil {
			t.Fatalf("unit %s: %v", unit, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s: %v", unit, err)
		}
		compareUnit(g, byUnit[unit])
	}
	// The optional-map probe: one absent (Present=false), one present
	// (Present=true, two entries) and one PRESENT-EMPTY value. Absent and
	// present-empty are three values beside present (docs/SPEC-TABLES.md
	// §2.8): the reference's generic writer elides both empty spellings, so
	// the present-empty row carries its own expected save below.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Holder.schema"), []byte(optionalProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("optional-map probe: %v", err)
	}
	m := tabletext.NewModel(g.u)
	makeWire := func(present bool, entries bool) []byte {
		inst := m.New(m.Lookup("Holder"))
		f, ok := inst.FieldByKey("tags")
		if !ok {
			t.Fatal("the probe has no tags field")
		}
		f.Present = present
		if entries {
			for _, kv := range [][2]int{{1, 10}, {2, 20}} {
				e := m.NewMapEntry(f.Def)
				e.Fields[0].Cell.Str = []byte(fmt.Sprintf("%d", kv[0]))
				e.Fields[1].Cell.I = int64(kv[1])
				f.Entries = append(f.Entries, tabletext.Cell{Tab: e})
			}
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	holder := g.u.Tables["Holder"]
	var tagsID uint64
	for _, f := range holder.Fields {
		if f.Name == "tags" {
			tagsID = ir.TableFieldWireId(f)
		}
	}
	if tagsID == 0 {
		t.Fatal("the probe's tags field has no wire id")
	}
	emptyMap := mapProbeWire(tagsID)
	var probeRows []loadRow
	for i, spec := range []struct {
		name    string
		present bool
		entries bool
		data    []byte
		save    []byte
	}{
		{"optional_map_absent", false, false, nil, nil},
		{"optional_map_present", true, true, nil, nil},
		{"optional_map_present_empty", true, false, emptyMap, emptyMap},
	} {
		data := spec.data
		if data == nil {
			data = makeWire(spec.present, spec.entries)
		}
		probeRows = append(probeRows, loadRow{
			kind: "instance", name: spec.name, unit: "probe", root: "Holder",
			wire: fmt.Sprintf("---%d", i), data: data, saveData: spec.save,
		})
	}
	compareUnit(g, probeRows)

	// The `| required` probe (docs/SPEC-TABLES.md §2.3): a required field
	// rides even at its default, and a body that never names one is refused,
	// naming it, at the root and inside a nested table.
	reqDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(reqDir, "Req.schema"), []byte(requiredProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	reqPaths, err := compiler.GatherPaths([]string{reqDir})
	if err != nil {
		t.Fatal(err)
	}
	rg, err := generateLoadUnit(t, reqPaths)
	if err != nil {
		t.Fatalf("required probe: %v", err)
	}
	aID := probeFieldID(t, rg.u, "ReqHolder", "a")
	nameID := probeFieldID(t, rg.u, "ReqHolder", "name")
	kidID := probeFieldID(t, rg.u, "ReqHolder", "kid")
	xID := probeFieldID(t, rg.u, "Kid", "x")
	yID := probeFieldID(t, rg.u, "Kid", "y")
	defaultWire := requiredDefaultWire(aID, nameID)
	// The reference writer keeps a required field at zero (a and name) and
	// the by-value kid now carries its own required x, so the expected save
	// is the wire with kid present.
	defaultSaved := requiredDefaultSavedWire(aID, nameID, kidID, xID)
	compareUnit(rg, []loadRow{{
		kind: "instance", name: "required_default", unit: "probe", root: "ReqHolder",
		data: defaultWire, saveData: defaultSaved,
	}})
	missingRows := []loadRow{
		{kind: "instance", name: "required_missing_root", unit: "probe", root: "ReqHolder",
			data: requiredMissingWire(aID)},
		{kind: "instance", name: "required_missing_nested", unit: "probe", root: "ReqHolder",
			data: requiredNestedMissingWire(aID, kidID, yID)},
	}
	reports := runRequiredReports(t, rg, missingRows)
	for i, row := range missingRows {
		if got, exp := reports[i][0], "1"; got != exp {
			t.Errorf("%s: missing_required is %s, want %s", row.name, got, exp)
		}
		want := "missing_required: name"
		if row.name == "required_missing_nested" {
			want = "missing_required: x"
		}
		if got := reports[i][1]; got != want {
			t.Errorf("%s: reason is %q, want %q", row.name, got, want)
		}
		if got := reports[i][2]; got != "nil" {
			t.Errorf("%s: the refused record is %q, want nil (docs/SPEC-TABLES.md §2.3)", row.name, got)
		}
	}
	if compared == 0 {
		t.Fatalf("compared no values: the test is vacuous")
	}
	t.Logf("optionals: %d fields; keyed: %d fields; optional-map: %d fields; required: %d fields (%d missing refused); %d instances byte for byte and re-loaded equal",
		counts.optional, counts.keyed, counts.optionalMap, counts.required, len(missingRows), compared)
}

// TestLobsterOptionalsNegativeControl breaks the emitted ENUM-KEYED
// registration (docs/SPEC-TABLES.md §3.2) in one generated module and
// requires the byte comparison to go red. The control — the same module
// unpatched — is green in TestLobsterOptionals.
func TestLobsterOptionalsNegativeControl(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	var row loadRow
	found := false
	for _, r := range loadRows(t) {
		if r.kind == "instance" && r.name == "keyed_config" {
			row, found = r, true
			break
		}
	}
	if !found {
		t.Fatalf("the manifest names no instance keyed_config")
	}
	paths, err := compiler.GatherPaths(prefix(units[row.unit]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(root, row.wire))
	if err != nil {
		t.Fatal(err)
	}
	want, err := goSave(g, row.root, wire)
	if err != nil {
		t.Fatal(err)
	}
	if got := runSaveRows(t, g, []loadRow{row}).saved[0]; got != hex.EncodeToString(want) {
		t.Fatalf("the control, unpatched: got %s, want %x", got, want)
	}
	path := filepath.Join(g.dir, "KeyedTable.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = `name: "Red", id: 0x9ff1de19feac1b7c`
	if n := strings.Count(string(src), anchor); n < 1 {
		t.Fatalf("the keyed anchor is in the emitted module %d times, want at least 1:\n%s", n, src)
	}
	patched := strings.ReplaceAll(string(src), anchor, `name: "Red", id: 1`)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	got := runSaveRows(t, g, []loadRow{row}).saved[0]
	if got == hex.EncodeToString(want) {
		t.Fatalf("the patched module still writes the reference's bytes: %s", got)
	}
	t.Logf("the byte comparison went red under the broken key registration: %s, want %x", got, want)
}
