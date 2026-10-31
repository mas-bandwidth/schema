package squirreltable_test

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// loadPrelude is the report probe's one helper: load a wire file through the
// emitted load runtime and print the §4 report exactly as the conformance
// report surface does (test/conformance/README.md).
const loadPrelude = `local inputs = dofile("inputs.nut", true);
local run = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local report = {
    unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false
  };
  L.loadTolerant(T, root, inputs[key], report);
  local verdict = report.refused ? "refused" : "read";
  local m = report.malformed ? "true" : "false";
  print(key + " " + report.unknown.tostring() + "," + report.kindMismatch.tostring() + "," +
        report.widened.tostring() + "," + report.clamped.tostring() + "," +
        report.duplicate.tostring() + "," + m + "," + verdict + "\n");
};
`

type reportRow struct {
	name string
	unit string
	root string
	wire string
}

func readReportRows(t *testing.T, path string) []reportRow {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []reportRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 5 && fields[0] == "report" {
			rows = append(rows, reportRow{name: fields[1], unit: fields[2], root: fields[3], wire: fields[4]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

type instanceRow struct {
	name string
	unit string
	root string
	wire string
	// noText is the instance's `no-text` manifest annotation: the form's own
	// rule leaves it no text representation.
	noText bool
	// goRefusal is the manifest's `go-refusal=<name>` annotation, the name the
	// Go engine's writer refuses the instance by. Every writer refuses it by
	// the same name (docs/SPEC-TABLES.md §2.5, §17.1).
	goRefusal string
}

func readInstanceRows(t *testing.T, path string) []instanceRow {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []instanceRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 5 && fields[0] == "instance" {
			r := instanceRow{name: fields[1], unit: fields[2], root: fields[3], wire: fields[4]}
			for _, a := range fields[5:] {
				switch {
				case a == "no-text":
					r.noText = true
				case strings.HasPrefix(a, "go-refusal="):
					r.goRefusal = strings.TrimPrefix(a, "go-refusal=")
				}
			}
			rows = append(rows, r)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// manifestInstanceLines counts the manifest's `instance` lines as written, so a
// test that reads the rows through readInstanceRows can tell that none was
// dropped: the corpus grows with every landing, and the count follows it.
func manifestInstanceLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "instance ") {
			n++
		}
	}
	if n == 0 {
		t.Fatalf("%s names no instance line", path)
	}
	return n
}

func readReportCounts(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("%s: %q is not a report line", path, line)
		}
		out[fields[0]] = fields[1]
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestSquirrelTableLoadMatchesTheReports loads every report row of
// testdata/conformance/tables/MANIFEST.txt whose unit the Squirrel table
// surface generates, prints u,k,w,c,d,m,verdict as test/conformance/README.md
// defines it, and compares with reports.txt (docs/SPEC-TABLES.md §4). Rows of
// a refused unit are counted and named.
func TestSquirrelTableLoadMatchesTheReports(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}
	rows := readReportRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	instances := readInstanceRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	pinned := readReportCounts(t, filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))

	byUnit := map[string][]reportRow{}
	var order []string
	for _, r := range rows {
		if _, ok := byUnit[r.unit]; !ok {
			order = append(order, r.unit)
		}
		byUnit[r.unit] = append(byUnit[r.unit], r)
		if _, ok := pinned[r.name]; !ok {
			t.Fatalf("%s: reports.txt names no such case", r.name)
		}
	}

	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(loadPrelude)
	inputs := map[string][]byte{}
	var refused []string
	refusedSet := map[string]bool{}
	mods := map[string]string{}
	refusedRows, compared := 0, 0
	combKeys := make([]string, 0, len(order)+len(instances))
	seenComb := map[string]bool{}
	for _, k := range order {
		if !seenComb[k] {
			combKeys = append(combKeys, k)
			seenComb[k] = true
		}
	}
	for _, in := range instances {
		if !seenComb[in.unit] {
			combKeys = append(combKeys, in.unit)
			seenComb[in.unit] = true
		}
	}
	for _, key := range combKeys {
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("report rows name unit %q, which the manifest does not declare", key)
		}
		u := loadUnit(t, root, unit)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			refused = append(refused, fmt.Sprintf("%s (%d rows): %v", key, len(byUnit[key]), err))
			refusedSet[key] = true
			refusedRows += len(byUnit[key])
			continue
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		mod := squirreltable.ModuleName(u)
		mods[key] = mod
		for _, r := range byUnit[key] {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.wire)))
			if err != nil {
				t.Fatal(err)
			}
			inputs[r.name] = data
			fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", key, mod, r.root, r.name)
			compared++
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	got := map[string]string{}
	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("probe line %q is not a case and a report", line)
		}
		got[fields[0]] = fields[1]
	}
	for _, r := range rows {
		isRefused := false
		for _, name := range refused {
			if strings.HasPrefix(name, r.unit+" ") {
				isRefused = true
				break
			}
		}
		if isRefused {
			continue
		}
		if got[r.name] != pinned[r.name] {
			t.Errorf("%s: the load reports %q, reports.txt pins %q", r.name, got[r.name], pinned[r.name])
		}
	}
	if compared+refusedRows != len(rows) {
		t.Errorf("compared %d and refused %d rows, want %d", compared, refusedRows, len(rows))
	}
	for _, r := range refused {
		t.Logf("refused %s", r)
	}
	t.Logf("compared %d of %d report rows; %d rows in %d refused units", compared, len(rows), refusedRows, len(refused))

	var ip strings.Builder
	ip.WriteString(loadPrelude)
	instInputs := map[string][]byte{}
	var instOrder []string
	for _, key := range combKeys {
		if refusedSet[key] {
			continue
		}
		for _, in := range instances {
			if in.unit != key {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
			if err != nil {
				t.Fatal(err)
			}
			instInputs[in.name] = data
			fmt.Fprintf(&ip, "run(%q, %q, %q, %q);\n", key, mods[key], in.root, in.name)
			instOrder = append(instOrder, in.name)
		}
	}
	writeFile(t, filepath.Join(dir, "inst.nut"), []byte(ip.String()))
	instStdout := sqrun.Run(t, dir, "inst.nut", instInputs)
	for line := range strings.SplitSeq(strings.TrimRight(string(instStdout), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("instance probe line %q is not a case and a report", line)
		}
		if strings.HasSuffix(fields[1], ",refused") {
			t.Errorf("%s: an instance of a generated unit was refused", fields[0])
		}
	}
	if len(instOrder) != len(instances)-refusedInstanceCount(instances, refusedSet) {
		t.Errorf("loaded %d instance rows, want %d", len(instOrder), len(instances)-refusedInstanceCount(instances, refusedSet))
	}
}

// TestSquirrelTableLoadNegativeControl patches the emitted load runtime in
// memory, one anchor matched exactly once, so a repeated field id is NOT
// counted duplicate. The same reports comparison over a wire carrying that
// repeated id must then go red (docs/SPEC-TABLES.md §4).
func TestSquirrelTableLoadNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	var paths []string
	for _, u := range units {
		if u.key == "tblv1" {
			paths = u.paths
		}
	}
	if paths == nil {
		t.Fatal("the manifest declares no tblv1 unit")
	}
	unit := loadUnit(t, root, paths)
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "tblv1")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	// a form-1 root body carrying field `a` twice: legal input whose last
	// occurrence wins SILENTLY — duplicate is never raised for a field id on
	// the wire (docs/SPEC-TABLES.md §4: "a wire read leaves it zero for every
	// field id")
	cfg := unit.Tables["Cfg"]
	var a *ir.Field
	for _, f := range cfg.Fields {
		if f.Name == "a" {
			a = f
		}
	}
	if a == nil {
		t.Fatal("tblv1 Cfg declares no field a")
	}
	id := ir.TableFieldWireId(a)
	kind := byte(ir.TableWireFieldKind(a))
	wire := []byte{1}
	wire = append(wire, 1, kind, 1, 0, 0, 0)
	wire = append(wire, 1, kind, 2, 0, 0, 0)
	wire = append(wire, 0)
	for i := range 8 {
		wire = append(wire, byte(id>>(8*i)))
	}
	wire = append(wire, 1, 0, 0, 0, 0, 0, 0, 0)
	// and a body carrying an id Cfg cannot name: skipped by its kind and
	// counted unknown (§4), the counter the control removes
	const unknownId = uint64(0x1122334455667788)
	unk := []byte{1, 1, kind, 1, 0, 0, 0, 0}
	for i := range 8 {
		unk = append(unk, byte(unknownId>>(8*i)))
	}
	unk = append(unk, 1, 0, 0, 0, 0, 0, 0, 0)

	mod := squirreltable.ModuleName(unit)
	var p strings.Builder
	p.WriteString(loadPrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", "tblv1", mod, "Cfg", "dup")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	inputs := map[string][]byte{"dup": wire, "unk": unk}
	if got := reportLine(t, sqrun.Run(t, dir, "main.nut", inputs)); got != "0,0,0,0,0,false,read" {
		t.Fatalf("a repeated field id reports %q, want %q", got, "0,0,0,0,0,false,read")
	}
	p.Reset()
	p.WriteString(loadPrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", "tblv1", mod, "Cfg", "unk")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	want := "1,0,0,0,0,false,read"

	got := reportLine(t, sqrun.Run(t, dir, "main.nut", inputs))
	if got != want {
		t.Fatalf("the load reports %q, want %q", got, want)
	}

	const anchor = "    if (!(id in c.byId)) {\n      report.unknown = report.unknown + 1;\n"
	runtime := squirreltable.Runtime()["schema_table_load.nut"]
	if n := strings.Count(string(runtime), anchor); n != 1 {
		t.Fatalf("the negative control's anchor appears %d times in the emitted runtime, want exactly once", n)
	}
	patched := strings.Replace(string(runtime), anchor, "    if (!(id in c.byId)) {\n      report.unknown = report.unknown;\n", 1)
	writeFile(t, filepath.Join(out, "schema_table_load.nut"), []byte(patched))

	got = reportLine(t, sqrun.Run(t, dir, "main.nut", inputs))
	if got == want {
		t.Fatalf("the patched runtime still reports %q; the reports comparison did not go red", got)
	}
	if got != "0,0,0,0,0,false,read" {
		t.Fatalf("the patched runtime reports %q, want the unknown counter gone", got)
	}
	t.Logf("unpatched %q, patched %q: the reports comparison goes red", want, got)
}

// TestSquirrelLoadClampsThe128BitRange is the 128-bit clamp's direct control
// (docs/SPEC-TABLES.md §4: "A 128-bit integer clamps to its declared bounds in
// 128 bits"). A form-1 body carries an int128 past its declared max; the load
// clamps it to the bound, counts one `clamped` and saves the clamped value,
// exactly as the Go engine does. With clampScalar's array branch reverted it
// saves the raw value and reports no clamp, so this control goes red.
func TestSquirrelLoadClampsThe128BitRange(t *testing.T) {
	t.Parallel()
	u := loadSource(t, "package wideprobe\n\ntable Root\n{\n    a int128 | min = 0, max = 100\n}\n")
	dir := t.TempDir()
	mod := emitUnit(t, dir, "wideprobe", u)
	st, ok := u.Tables["Root"]
	if !ok {
		t.Fatal("the unit carries no table Root")
	}
	var a *ir.Field
	for _, f := range st.Fields {
		if f.Name == "a" {
			a = f
		}
	}
	if a == nil {
		t.Fatal("wideprobe Root declares no field a")
	}

	// 1000 is past the declared max 100; the payload is the [lo, hi] pair.
	wire := []byte{1}
	wire = binary.AppendUvarint(wire, 1) // a's id reference
	wire = append(wire, byte(ir.TableWireFieldKind(a)))
	wire = binary.LittleEndian.AppendUint64(wire, 1000)
	wire = binary.LittleEndian.AppendUint64(wire, 0)
	wire = append(wire, 0)
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(a))
	wire = binary.LittleEndian.AppendUint64(wire, 1)

	model := tabletext.NewModel(u)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if _, err := tablewire.Decode(model, inst, wire, &report); err != nil {
		t.Fatal(err)
	}
	want := reportOf(report)
	if want != "0,0,0,1,0,false,read" {
		t.Fatalf("the Go engine reports %q, want one clamp", want)
	}
	if got := squirrelReport(t, dir, "wideprobe", mod, string(wire)); got != want {
		t.Fatalf("Squirrel reports %q, the Go engine %q: the 128-bit range did not clamp", got, want)
	}
	engineSaved, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatal(err)
	}
	var p strings.Builder
	p.WriteString(savePrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", "wideprobe", mod, "Root", "clamp")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	saved := parseSaved(t, sqrun.Run(t, dir, "main.nut", map[string][]byte{"clamp": wire}))
	row, ok := saved["clamp"]
	if !ok || row.measure < 0 {
		t.Fatalf("the clamped load did not save: %+v", row)
	}
	if row.hex != fmt.Sprintf("%x", engineSaved) {
		t.Errorf("the clamped save is %s, the Go engine's %x", row.hex, engineSaved)
	}
	t.Logf("128-bit value 1000 clamped to 100: report %q, save %s", want, row.hex)
}

// TestSquirrelLoadFramesTheMapEntryTerminator is the map-entry framing's
// direct control (docs/SPEC-TABLES.md §2.8, §3: "A map checks the entry's
// terminator during the key scan ... malformed entry framing stops the map").
// An entry whose body ends without its zero reference is damage, not a key
// mismatch: the load counts `malformed` and saves the empty map, exactly as
// the Go engine does. With the framing reverted the key scan calls the missing
// terminator a key failure and counts kind_mismatch, so this control goes red.
func TestSquirrelLoadFramesTheMapEntryTerminator(t *testing.T) {
	t.Parallel()
	u := loadSource(t, "package mapprobe\n\ntable Root\n{\n    entries map[int32]int32\n}\n")
	dir := t.TempDir()
	mod := emitUnit(t, dir, "mapprobe", u)

	entriesID, keyID, valueID := ir.TableWireId("entries"), ir.MapKeyWireId, ir.MapValueWireId
	var entry []byte
	entry = binary.AppendUvarint(entry, 2) // key's id reference
	entry = append(entry, byte(ir.TableKindI32))
	entry = binary.LittleEndian.AppendUint32(entry, 7)
	entry = binary.AppendUvarint(entry, 3) // value's id reference
	entry = append(entry, byte(ir.TableKindI32))
	entry = binary.LittleEndian.AppendUint32(entry, 9)
	// NO ZERO REFERENCE: the entry's body ends after the value.
	body := []byte{byte(ir.TableKindTable)}
	body = binary.AppendUvarint(body, 1) // count
	body = binary.AppendUvarint(body, uint64(len(entry)))
	body = append(body, entry...)
	wire := []byte{1}
	wire = binary.AppendUvarint(wire, 1) // entries' id reference
	wire = append(wire, byte(ir.TableKindArray))
	wire = binary.AppendUvarint(wire, uint64(len(body)))
	wire = append(wire, body...)
	wire = append(wire, 0)
	for _, id := range []uint64{entriesID, keyID, valueID} {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	wire = binary.LittleEndian.AppendUint64(wire, 3)

	model := tabletext.NewModel(u)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if _, err := tablewire.Decode(model, inst, wire, &report); err != nil {
		t.Fatal(err)
	}
	want := reportOf(report)
	if want != "0,0,0,0,0,true,read" {
		t.Fatalf("the Go engine reports %q, want the map entry's framing damage", want)
	}
	if got := squirrelReport(t, dir, "mapprobe", mod, string(wire)); got != want {
		t.Fatalf("Squirrel reports %q, the Go engine %q: the entry terminator was not framing", got, want)
	}
	t.Logf("map entry without its terminator: report %q", want)
}

func refusedInstanceCount(instances []instanceRow, refused map[string]bool) int {
	n := 0
	for _, in := range instances {
		if refused[in.unit] {
			n++
		}
	}
	return n
}

func reportLine(t *testing.T, stdout []byte) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("the probe printed %d lines, want one: %q", len(lines), stdout)
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 2 {
		t.Fatalf("the probe line %q is not a case and a report", lines[0])
	}
	return fields[1]
}
