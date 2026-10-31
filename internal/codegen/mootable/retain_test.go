package mootable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// retainFull is a capacity no corpus wire reaches: FORMAT.md's `full`.
const retainFull = 1 << 20

// retainRow is one `retain` row of the tables manifest (FORMAT.md):
// <case> <unit> <root> <wire> <capacity> <ids> <counters> <save lost> <saves>.
type retainRow struct {
	caseName, unit, root, wire, capacity, ids, counters, saveLost, saves string
}

func retainRows(t *testing.T) []retainRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []retainRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) >= 10 && x[0] == "retain" {
			rows = append(rows, retainRow{x[1], x[2], x[3], x[4], x[5], x[6], x[7], x[8], x[9]})
		}
	}
	return rows
}

// retainOut is one retaining load and save, as either engine answers it.
type retainOut struct {
	used, retained, lost, unknown int
	value                         bool // the load answered a value
	save                          []byte
	saveLost                      int
	saveErr                       string
}

func (o retainOut) counters() string { return fmt.Sprintf("%d,%d,%d", o.retained, o.lost, o.unknown) }

// retainProbe is the moo main: LoadRetain at (capacity, ids), print the store's
// bytes and the three counters and the plain unknown, then SaveRetain and
// print the saved hex and the retain_lost the save added.
func retainProbe(g *generatedUnit, rootName string, capacity int, ids int) string {
	idArg := "nil"
	if ids >= 0 {
		idArg = strconv.Itoa(ids)
	}
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\n" + g.requireAll() +
		fmt.Sprintf(`local report = L.newReport()
local store = R.newStore(%d, %s)
local v = R.LoadRetain(%q, inputs.wire, report, store)
print(string.format("%%d,%%d,%%d,%%d", R.used(store), report.retained, report.retainLost, report.unknown))
if v == nil then print("NIL") return end
local before = report.retainLost
local out, why = R.SaveRetain(%q, v, store, report)
if out == nil then print("SAVENIL " .. tostring(if why ~= nil then why.name else nil)) return end
local parts = table.create(buffer.len(out))
for i = 0, buffer.len(out) - 1 do
	parts[i + 1] = string.format("%%02x", buffer.readu8(out, i))
end
print("x" .. table.concat(parts))
print(report.retainLost - before)
`, capacity, idArg, rootName, rootName)
}

func mooRetain(t *testing.T, g *generatedUnit, rootName string, wire []byte, capacity, ids int) retainOut {
	t.Helper()
	out := g.run(t, retainProbe(g, rootName, capacity, ids), map[string][]byte{"wire": wire})
	lines := strings.Split(out, "\n")
	var o retainOut
	if _, err := fmt.Sscanf(lines[0], "%d,%d,%d,%d", &o.used, &o.retained, &o.lost, &o.unknown); err != nil {
		t.Fatalf("moo printed %q", out)
	}
	if len(lines) < 2 || lines[1] == "NIL" {
		return o
	}
	o.value = true
	if strings.HasPrefix(lines[1], "SAVENIL") {
		o.saveErr = lines[1]
		return o
	}
	b, err := hex.DecodeString(strings.TrimPrefix(lines[1], "x"))
	if err != nil || len(lines) < 3 {
		t.Fatalf("moo printed %q", out)
	}
	o.save = b
	o.saveLost, _ = strconv.Atoi(lines[2])
	return o
}

// goRetain is the Go reference engine's answer (internal/tablewire
// DecodeRetain, EncodeRetain) at the same two capacities.
func goRetain(t *testing.T, u *ir.Unit, rootName string, wire []byte, capacity, ids int) retainOut {
	t.Helper()
	if ids < 0 {
		ids = retainFull
	}
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables[rootName])
	keep := &tablewire.Retain{Capacity: capacity, IdCapacity: ids}
	var rep tabletext.Report
	ok, err := tablewire.DecodeRetain(m, inst, wire, keep, &rep)
	o := retainOut{used: keep.Used(), retained: rep.Retained, lost: rep.RetainLost, unknown: rep.Unknown}
	if err != nil || rep.Refused {
		return o
	}
	_ = ok
	o.value = true
	before := rep.RetainLost
	save, err := tablewire.EncodeRetain(m, inst, keep, &rep)
	if err != nil {
		o.saveErr = err.Error()
		return o
	}
	o.save = save
	o.saveLost = rep.RetainLost - before
	return o
}

// TestMooRetainAnswersTheManifest answers every `retain` row of the tables
// manifest (docs/SPEC-TABLES.md §6.6; FORMAT.md): the load's three counters at
// the row's capacities (`short` is one byte under what a `full` load used),
// the retain_lost the save adds, and the pinned save byte for byte. Each row
// is also held to the Go reference engine at the same capacities: the same
// store bytes used, the same counters, the same saved bytes.
func TestMooRetainAnswersTheManifest(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	rows := retainRows(t)
	if len(rows) == 0 {
		t.Fatal("the manifest names no retain row")
	}
	cache := map[string]*generatedUnit{}
	for _, row := range rows {
		g := cache[row.unit]
		if g == nil {
			g = generateUnit(t, units, row.unit)
			if g.err != nil {
				t.Fatalf("unit %s: %v", row.unit, g.err)
			}
			cache[row.unit] = g
		}
		wire, err := os.ReadFile(filepath.Join(root, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		ids := -1
		if row.ids != "full" {
			ids, _ = strconv.Atoi(row.ids)
		}
		capacity := retainFull
		if row.capacity == "short" {
			capacity = mooRetain(t, g, row.root, wire, retainFull, ids).used - 1
		}
		got := mooRetain(t, g, row.root, wire, capacity, ids)
		if got.counters() != row.counters {
			t.Errorf("%s: load counters %s, want %s", row.caseName, got.counters(), row.counters)
		}
		if !got.value || got.saveErr != "" {
			t.Errorf("%s: no save: value=%v %s", row.caseName, got.value, got.saveErr)
			continue
		}
		if strconv.Itoa(got.saveLost) != row.saveLost {
			t.Errorf("%s: the save lost %d, want %s", row.caseName, got.saveLost, row.saveLost)
		}
		if row.saves != "-" {
			want, err := os.ReadFile(filepath.Join(root, row.saves))
			if err != nil {
				t.Fatal(err)
			}
			if string(got.save) != string(want) {
				t.Errorf("%s: the save is not the pinned file: %s", row.caseName, firstDiff(got.save, want))
			}
		}
		oracle := goRetain(t, g.unit, row.root, wire, capacity, ids)
		if got.used != oracle.used || got.counters() != oracle.counters() || got.saveLost != oracle.saveLost || string(got.save) != string(oracle.save) {
			t.Errorf("%s: moo and the Go engine disagree:\nmoo: used %d %s lost %d %x\ngo:  used %d %s lost %d %x", row.caseName,
				got.used, got.counters(), got.saveLost, got.save, oracle.used, oracle.counters(), oracle.saveLost, oracle.save)
		}
	}
}

// declaredFixed is a `fixed table` declaration.
var declaredFixed = regexp.MustCompile(`(?m)^fixed table `)

// variableUnit generates a manifest unit with every `fixed table` declared as a
// variable `table`: RETENTION IS THE VARIABLE CLASS'S (docs/SPEC-TABLES.md §6.6)
// and a fixed-class root is refused by name, so the evolved pairs the manifest
// declares fixed are the same wires read through the variable class. nil when
// the converted unit does not compile.
func variableUnit(t *testing.T, units map[string][]string, key string) *generatedUnit {
	t.Helper()
	paths, err := compiler.GatherPaths(prefix(units[key]))
	if err != nil {
		t.Fatalf("unit %s: %v", key, err)
	}
	dir := t.TempDir()
	var out []string
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		q := filepath.Join(dir, filepath.Base(p))
		if err := os.WriteFile(q, declaredFixed.ReplaceAll(src, []byte("table ")), 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, q)
	}
	u, err := compiler.New().Load(out)
	if err != nil {
		return nil
	}
	files, err := generate(u)
	if err != nil {
		return &generatedUnit{err: err, unit: u}
	}
	return writeUnit(t, u, files)
}

// TestMooRetainMatchesTheGoEngine is the retention differential on EVOLVED
// PAIRS: every `report` and `instance` row of the tables manifest is bytes a
// build reads that it may not have written. moo's retaining load and save and
// the Go engine's must agree on the store bytes used, the counters, the
// retain_lost the save adds and the saved bytes, at a full capacity, at one
// byte under what that used, and with the caller's id list one short of what
// the full save interned.
func TestMooRetainMatchesTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	cache := map[string]*generatedUnit{}
	compared, retaining := 0, 0
	for _, row := range manifestRows(t) {
		g := cache[row.unit]
		if g == nil {
			g = variableUnit(t, units, row.unit)
			cache[row.unit] = g
		}
		if g == nil || g.err != nil {
			continue
		}
		wire := rowWire(t, row)
		full := goRetain(t, g.unit, row.root, wire, retainFull, -1)
		type probe struct{ capacity, ids int }
		probes := []probe{{retainFull, -1}}
		if full.retained > 0 {
			retaining++
			probes = append(probes, probe{full.used - 1, -1})
			m := tabletext.NewModel(g.unit)
			inst := m.New(g.unit.Tables[row.root])
			keep := &tablewire.Retain{Capacity: retainFull, IdCapacity: retainFull}
			var rep tabletext.Report
			if _, err := tablewire.DecodeRetain(m, inst, wire, keep, &rep); err == nil {
				if _, err := tablewire.EncodeRetain(m, inst, keep, &rep); err == nil && len(keep.Ids()) > 0 {
					probes = append(probes, probe{retainFull, len(keep.Ids()) - 1})
				}
			}
		}
		for _, p := range probes {
			want := goRetain(t, g.unit, row.root, wire, p.capacity, p.ids)
			got := mooRetain(t, g, row.root, wire, p.capacity, p.ids)
			if got.used != want.used || got.counters() != want.counters() || got.value != want.value ||
				got.saveLost != want.saveLost || string(got.save) != string(want.save) || (got.saveErr == "") != (want.saveErr == "") {
				t.Errorf("%s (%s/%s) at capacity %d ids %d: moo and the Go engine disagree:\nmoo: used %d %s value %v lost %d %s %x\ngo:  used %d %s value %v lost %d %s %x",
					row.caseName, row.unit, row.root, p.capacity, p.ids,
					got.used, got.counters(), got.value, got.saveLost, got.saveErr, got.save,
					want.used, want.counters(), want.value, want.saveLost, want.saveErr, want.save)
				continue
			}
			compared++
		}
	}
	if retaining == 0 {
		t.Fatal("no corpus wire retained anything: the differential proves nothing")
	}
	t.Logf("%d retaining load/save pairs agree with the Go engine (%d wires retain)", compared, retaining)
}

// TestMooRetainRefusesANonFiniteRetainedFloat: a retained unknown field of a
// float kind is tested like a known one (docs/SPEC-TABLES.md §3.3, §6.6): a
// NaN refuses the read as non_finite_float, named <Table>.0x<id>, as the Go
// engine refuses it; the control, a finite float, is retained.
func TestMooRetainRefusesANonFiniteRetainedFloat(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Old.schema": "package probe\ntable T\n{\n    a uint8\n}\n"})
	// T{ a = 1, x float32 = bits } where x is unknown to this build
	wireOf := func(bits uint32) []byte {
		w := []byte{1, 1, 6, 1, 2, 10, byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24), 0}
		w = append(w, u64le(ir.TableWireId("a"))...)
		w = append(w, u64le(ir.TableWireId("x"))...)
		return append(w, u64le(2)...)
	}
	probe := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\n" + g.requireAll() + `local report = L.newReport()
local v = R.LoadRetain("T", inputs.wire, report, R.newStore(1024))
print(tostring(v ~= nil) .. " " .. tostring(report.reason) .. " " .. report.retained)
`
	name := fmt.Sprintf("T.0x%016x", ir.TableWireId("x"))
	if got := g.run(t, probe, map[string][]byte{"wire": wireOf(0x7fc00000)}); got != "false non_finite_float: "+name+" 0" {
		t.Errorf("a retained NaN: got %q, want the read refused naming %s", got, name)
	}
	if got := g.run(t, probe, map[string][]byte{"wire": wireOf(0x3f800000)}); got != "true nil 1" {
		t.Errorf("the control, a retained 1.0: got %q", got)
	}
	// the Go engine refuses it by the same name
	var rep tabletext.Report
	m := tabletext.NewModel(g.unit)
	_, err := tablewire.DecodeRetain(m, m.New(g.unit.Tables["T"]), wireOf(0x7fc00000), &tablewire.Retain{Capacity: 1024, IdCapacity: 8}, &rep)
	if err == nil || !strings.Contains(err.Error(), name) {
		t.Errorf("the Go engine: %v", err)
	}
}

// TestMooRetainNegativeControl plants out the save hook's write of a kept
// record (one anchor in the emitted table_retain.moo) and requires the
// manifest's round-trip row to go red against its pinned save.
func TestMooRetainNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	var row retainRow
	for _, r := range retainRows(t) {
		if r.caseName == "retain_round_trip" {
			row = r
		}
	}
	g := generateUnit(t, units, row.unit)
	wire, err := os.ReadFile(filepath.Join(root, row.wire))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, row.saves))
	if err != nil {
		t.Fatal(err)
	}
	if got := mooRetain(t, g, row.root, wire, retainFull, -1); string(got.save) != string(want) {
		t.Fatalf("the control is not clean: %s", firstDiff(got.save, want))
	}
	path := filepath.Join(g.dir, "table_retain.moo")
	src := string(g.files["table_retain.moo"])
	const anchor = "\t\t\t\tapi.str(w, buffer.readstring(inner.b, 0, inner.n))\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the tail-write anchor is not in table_retain.moo exactly once")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(src, anchor, "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mooRetain(t, g, row.root, wire, retainFull, -1); string(got.save) == string(want) {
		t.Fatal("the save still matches with the tail write planted out")
	}
}

// hexOf is a moo expression list printing a buffer as lower-case hex on one
// line, behind prefix.
const mooHex = `
local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
`

func retainRoundTripRow(t *testing.T) (*generatedUnit, retainRow, []byte) {
	t.Helper()
	units, _ := readManifest(t)
	var row retainRow
	for _, r := range retainRows(t) {
		if r.caseName == "retain_round_trip" {
			row = r
		}
	}
	g := generateUnit(t, units, row.unit)
	wire, err := os.ReadFile(filepath.Join(root, row.wire))
	if err != nil {
		t.Fatal(err)
	}
	return g, row, wire
}

// TestMooRetainMeasureAndIdempotence holds the two promises the page makes
// about the pair (docs/SPEC-TABLES.md §6.6): MeasureRetain answers the size
// SaveRetain writes, whatever the id list can hold, and the retaining save is
// IDEMPOTENT — a file loaded with retention and saved again is the first save,
// byte for byte, with nothing lost the second time.
func TestMooRetainMeasureAndIdempotence(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	g, row, wire := retainRoundTripRow(t)
	main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\n" + g.requireAll() + mooHex +
		fmt.Sprintf(`for _, ids in { 1000, 2, 0 } do
	local report = L.newReport()
	local store = R.newStore(65536, ids)
	local v = R.LoadRetain(%[1]q, inputs.wire, report, store)
	local n = R.MeasureRetain(%[1]q, v, store)
	local first = R.SaveRetain(%[1]q, v, store, report)
	assert(first ~= nil and n == buffer.len(first), "measure and save disagree")
	local again = L.newReport()
	local store2 = R.newStore(65536, ids)
	local v2 = R.LoadRetain(%[1]q, first, again, store2)
	local second = R.SaveRetain(%[1]q, v2, store2, again)
	print(ids, n, hex(first) == hex(second :: buffer), report.retained, report.retainLost, again.retainLost)
end
`, row.root)
	got := g.run(t, main, map[string][]byte{"wire": wire})
	for _, want := range []string{"1000\t", "2\t", "0\t"} {
		if !strings.Contains(got, want) {
			t.Fatalf("no line for ids %q in %q", want, got)
		}
	}
	for line := range strings.SplitSeq(got, "\n") {
		f := strings.Fields(line)
		if len(f) != 6 {
			t.Fatalf("moo printed %q", got)
		}
		if f[2] != "true" {
			t.Errorf("ids %s: the second save is not the first: %q", f[0], line)
		}
		// the manifest's two excluded classes are lost at the load whatever
		// the id list holds (retain_round_trip: 9,2,11)
		if f[0] == "1000" && (f[3] != "9" || f[4] != "2" || f[5] != "0") {
			t.Errorf("ids %s: a full id list lost records at the save: %q", f[0], line)
		}
		if f[0] == "0" && f[4] == "2" {
			t.Errorf("ids %s: no retained id fit and the save lost nothing: %q", f[0], line)
		}
		if f[0] != "1000" && f[5] != "0" {
			// the dropped records are gone from the first save, so the second
			// load has nothing to lose
			t.Errorf("ids %s: the second save lost records: %q", f[0], line)
		}
	}
}

// TestMooRetainRefusesWhatThePageRefuses: a fixed-class root gets NONE of the
// three verbs, refused by name (docs/SPEC-TABLES.md §6.6), and SaveRetain
// refuses a nil report, because the save is the only place a caller learns a
// record was dropped. Each refusal has its negative control: with the check
// planted out the same call succeeds.
func TestMooRetainRefusesWhatThePageRefuses(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	fixed := generateUnit(t, units, "tblv2")
	g, row, wire := retainRoundTripRow(t)
	fixedMain := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\n" + fixed.requireAll() + `
local report = L.newReport()
local store = R.newStore(1024)
local v = R.LoadRetain("Cfg", inputs.wire, report, store)
print(v == nil, tostring(report.reason))
local loaded = L.loadTolerant("Cfg", inputs.wire, L.newReport())
local ok, out, why = pcall(R.SaveRetain, "Cfg", loaded, store, report)
local ok2, n, why2 = pcall(R.MeasureRetain, "Cfg", loaded, store)
print(out == nil, if why ~= nil then why.name else "none", n == nil, if why2 ~= nil then why2.name else "none")
`
	fixedWire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "v2_cfg.bin"))
	if err != nil {
		t.Fatal(err)
	}
	wantFixed := "true\t" + "fixed_class_root: LoadRetain, MeasureRetain and SaveRetain are the variable class's; a fixed-class root is a value and has no region to retain through (docs/SPEC-TABLES.md §6.6)\ntrue\tfixed_class_root\ttrue\tfixed_class_root"
	if got := fixed.run(t, fixedMain, map[string][]byte{"wire": fixedWire}); got != wantFixed {
		t.Errorf("a fixed-class root: got %q, want %q", got, wantFixed)
	}
	reportMain := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\n" + g.requireAll() +
		fmt.Sprintf(`local report = L.newReport()
local store = R.newStore(65536)
local v = R.LoadRetain(%[1]q, inputs.wire, report, store)
local ok, out, why = pcall(R.SaveRetain, %[1]q, v, store, nil)
print(ok, out == nil, if ok and why ~= nil then why.name else "none")
`, row.root)
	if got := g.run(t, reportMain, map[string][]byte{"wire": wire}); got != "true\ttrue\tretain_needs_report" {
		t.Errorf("a nil report: got %q", got)
	}
	// the negative controls: each check planted out, and the same call answers
	path := filepath.Join(g.dir, "table_retain.moo")
	src := string(g.files["table_retain.moo"])
	const reportCheck = "\tif report == nil then\n\t\treturn nil, { name = \"retain_needs_report\", field = nil }\n\tend\n"
	if strings.Count(src, reportCheck) != 1 {
		t.Fatal("the null-report check is not in table_retain.moo exactly once")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(src, reportCheck, "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := g.run(t, reportMain, map[string][]byte{"wire": wire}); got == "true\ttrue\tretain_needs_report" {
		t.Error("the nil report is still refused with its check planted out")
	}
	fpath := filepath.Join(fixed.dir, "table_retain.moo")
	fsrc := string(fixed.files["table_retain.moo"])
	if strings.Count(fsrc, "if d.fixed then") != 3 {
		t.Fatal("the fixed-class checks are not in table_retain.moo three times")
	}
	if err := os.WriteFile(fpath, []byte(strings.ReplaceAll(fsrc, "if d.fixed then", "if false then")), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fixed.run(t, fixedMain, map[string][]byte{"wire": fixedWire}); got == wantFixed {
		t.Error("a fixed-class root is still refused with its checks planted out")
	}
}
