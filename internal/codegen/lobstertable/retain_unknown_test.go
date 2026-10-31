package lobstertable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// retainRow is one `retain` line of the tables manifest (docs/SPEC-TABLES.md
// §6.6). capacity is "full" or "short"; ids is "full" or a count; counters are
// retained,retain_lost,unknown; saves is "-" or the pinned save files.
type retainRow struct {
	name, unit, root, wire string
	capacity, ids          string
	counters               string
	saveLost               int
	saves                  string
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
		if len(x) != 10 || x[0] != "retain" {
			continue
		}
		lost, err := strconv.Atoi(x[8])
		if err != nil {
			t.Fatalf("row %s: save-lost %q is not a number", x[1], x[8])
		}
		rows = append(rows, retainRow{name: x[1], unit: x[2], root: x[3], wire: x[4],
			capacity: x[5], ids: x[6], counters: x[7], saveLost: lost, saves: x[9]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// retainReportLine is the report line the harness compares: retention's two
// counters, then the six the plain report carries. It is the same line on the
// Lobster and Go sides.
func retainReportLine(r tabletext.Report) string {
	return fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d,%t", r.Retained, r.RetainLost,
		r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed)
}

// retainProgram is the main for one unit: every table module, the two walkers
// and their retention hook, the shared value dump, then one rrun per row.
func retainProgram(t *testing.T, g *loadUnit, rows []retainRow) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\n")
	b.WriteString("import .schema_save\n")
	b.WriteString("import .retain_unknown\n")
	b.WriteString(keyedRegistrations(g.u))
	b.WriteString(dumpLobster)
	b.WriteString(retainDump)
	for i, r := range rows {
		fmt.Fprintf(&b, "rrun(%s, \"w%d\", %d, %d)\n", lobster.StringLit([]byte(r.root)), i,
			retainCapacity(t, g, r), retainIdCapacity(t, r))
	}
	return b.String()
}

// retainDump is the harness's retaining half: load_retain, save_retain, and a
// plain re-load, each printing its report line and value. A missing retained
// count or a dropped construct moves the line, so the comparison sees it.
const retainDump = `def rline(rep:schema_rt.LoadReport) -> string:
    return "{rep.retained},{rep.retain_lost},{rep.unknown},{rep.kind_mismatch},{rep.widened},{rep.clamped},{rep.duplicate},{rep.malformed}"
def rrun(root:string, file:string, cap:int, idcap:int):
    let data = read_file(file)
    assert data
    let retain = schema_rt.Retain { capacity: cap, id_capacity: idcap, used: 0, ids: [], known: [] }
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load_retain(root, data, retain, rep)
    print(rline(rep))
    labels = []
    push(labels, rec)
    let dump = drec(rec)
    print(dump)
    let saved = schema_rt.save_retain(rec, retain, rep)
    print(rline(rep))
    print(hexs(saved))
    let rep2 = schema_rt.LoadReport {}
    let rec2 = schema_rt.load_tolerant(root, saved, rep2)
    print(rline(rep2))
    labels = []
    var dump2 = "nil"
    if rec2:
        push(labels, rec2)
        dump2 = drec(rec2)
    print(dump2)
`

func retainCapacity(t *testing.T, g *loadUnit, r retainRow) int {
	t.Helper()
	if r.capacity != "short" {
		return 1 << 20
	}
	wire, err := os.ReadFile(filepath.Join(root, r.wire))
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(g.u)
	inst := m.New(m.Lookup(r.root))
	probe := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: retainIdCapacity(t, r)}
	var rep tabletext.Report
	if _, err := tablewire.DecodeRetain(m, inst, wire, probe, &rep); err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
	if probe.Used() == 0 {
		t.Fatalf("%s: the short row retains nothing", r.name)
	}
	return probe.Used() - 1
}

func retainIdCapacity(t *testing.T, r retainRow) int {
	t.Helper()
	if r.ids == "full" {
		return 1 << 20
	}
	n, err := strconv.Atoi(r.ids)
	if err != nil {
		t.Fatalf("%s: ids %q is not full or a count", r.name, r.ids)
	}
	return n
}

// runRetainRows runs every row under the pinned toolchain and returns six
// lines per row: load report, load dump, save report, saved hex, re-load
// report, re-load dump.
func runRetainRows(t *testing.T, g *loadUnit, rows []retainRow) [][6]string {
	t.Helper()
	inputs := map[string][]byte{}
	for i, row := range rows {
		wire, err := os.ReadFile(filepath.Join(root, row.wire))
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		inputs[fmt.Sprintf("w%d", i)] = wire
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(retainProgram(t, g, rows)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	if len(out) != 6*len(rows) {
		t.Fatalf("the retaining program printed %d lines for %d rows, want %d: %q", len(out), len(rows), 6*len(rows), out)
	}
	res := make([][6]string, len(rows))
	for i := range rows {
		copy(res[i][:], out[6*i:6*i+6])
	}
	return res
}

func goRetainRow(t *testing.T, g *loadUnit, row retainRow, capacity, idCapacity int) [6]string {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(root, row.wire))
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(g.u)
	inst := m.New(m.Lookup(row.root))
	retain := &tablewire.Retain{Capacity: capacity, IdCapacity: idCapacity}
	var loadRep tabletext.Report
	if _, err := tablewire.DecodeRetain(m, inst, wire, retain, &loadRep); err != nil {
		t.Fatalf("%s: DecodeRetain: %v", row.name, err)
	}
	loadLine := retainReportLine(loadRep)
	d := &goDumper{labels: map[*tabletext.Instance]int{inst: 0}, next: 1}
	dump := d.rec(inst)
	saved, err := tablewire.EncodeRetain(m, inst, retain, &loadRep)
	if err != nil {
		t.Fatalf("%s: EncodeRetain: %v", row.name, err)
	}
	saveLine := retainReportLine(loadRep)
	inst2 := m.New(m.Lookup(row.root))
	var reloadRep tabletext.Report
	if _, err := tablewire.Decode(m, inst2, saved, &reloadRep); err != nil {
		t.Fatalf("%s: re-load: %v", row.name, err)
	}
	d2 := &goDumper{labels: map[*tabletext.Instance]int{inst2: 0}, next: 1}
	return [6]string{loadLine, dump, saveLine, hex.EncodeToString(saved), retainReportLine(reloadRep), d2.rec(inst2)}
}

// TestLobsterRetainUnknown carries retain-unknown across load and save for
// every `retain` row of the tables manifest (docs/SPEC-TABLES.md §6.6):
// LoadRetain keeps the fields RT1 cannot name and SaveRetain writes them at the
// end of their body, in place, and the saved file re-loads whole. Each row's
// load report (retained, retain_lost, unknown, ...), its saved bytes, its save
// report and its plain re-load report and value must equal the Go reference
// engine's (internal/tablewire DecodeRetain/EncodeRetain) byte for byte and
// count for count.
func TestLobsterRetainUnknown(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	rows := retainRows(t)
	if len(rows) == 0 {
		t.Fatal("the manifest names no retain rows: the comparison is vacuous")
	}
	byUnit := map[string][]retainRow{}
	var order []string
	for _, row := range rows {
		if _, ok := units[row.unit]; !ok {
			t.Fatalf("row %s names unit %s, which the manifest does not declare", row.name, row.unit)
		}
		if _, seen := byUnit[row.unit]; !seen {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	compared := 0
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s is refused: %v", key, err)
		}
		rs := byUnit[key]
		got := runRetainRows(t, g, rs)
		for i, row := range rs {
			capacity := retainCapacity(t, g, row)
			idCap := retainIdCapacity(t, row)
			want := goRetainRow(t, g, row, capacity, idCap)
			if got[i][0] != want[0] {
				t.Errorf("%s (%s): the load report is not the reference's:\nlobster: %s\ngo:      %s", row.name, row.unit, got[i][0], want[0])
			}
			if got[i][1] != want[1] {
				t.Errorf("%s (%s): the loaded value is not the reference's at byte %s", row.name, row.unit, divergence(got[i][1], want[1]))
			}
			if got[i][2] != want[2] {
				t.Errorf("%s (%s): the save report is not the reference's:\nlobster: %s\ngo:      %s", row.name, row.unit, got[i][2], want[2])
			}
			if got[i][3] != want[3] {
				t.Errorf("%s (%s): the saved bytes are not the reference's:\nlobster: %s\ngo:      %s", row.name, row.unit, got[i][3], want[3])
			}
			if got[i][4] != want[4] {
				t.Errorf("%s (%s): the re-load report is not the reference's:\nlobster: %s\ngo:      %s", row.name, row.unit, got[i][4], want[4])
			}
			if got[i][5] != want[5] {
				t.Errorf("%s (%s): the re-loaded value is not the reference's at byte %s", row.name, row.unit, divergence(got[i][5], want[5]))
			}
			fields := strings.Split(want[0], ",")
			if len(fields) < 3 || strings.Join(fields[:3], ",") != row.counters {
				t.Errorf("%s: the reference's counters %s do not name the manifest's %s", row.name, strings.Join(fields[:3], ","), row.counters)
			}
			saveLost := 0
			for _, f := range strings.Split(want[2], ",")[1:2] {
				n, _ := strconv.Atoi(f)
				saveLost = n
			}
			loadLost, _ := strconv.Atoi(fields[1])
			if saveLost-loadLost != row.saveLost {
				t.Errorf("%s: the save lost %d records, the manifest says %d", row.name, saveLost-loadLost, row.saveLost)
			}
			if row.saves != "-" {
				for pinned := range strings.SplitSeq(row.saves, ",") {
					data, err := os.ReadFile(filepath.Join(root, pinned))
					if err != nil {
						t.Fatalf("%s: %v", row.name, err)
					}
					if got[i][3] != hex.EncodeToString(data) {
						t.Errorf("%s: the saved bytes do not name the pinned file %s", row.name, pinned)
					}
				}
			}
			construct := strings.TrimPrefix(row.name, "retain_")
			t.Logf("construct %s: retained=%s retain_lost=%s unknown=%s save_lost=%d", construct, fields[0], fields[1], fields[2], row.saveLost)
			if got[i] == want {
				compared++
			}
		}
	}
	if compared != len(rows) {
		t.Fatalf("%d of %d retain rows agree with the reference", compared, len(rows))
	}
	if compared != 8 {
		t.Fatalf("compared %d retain rows, want the manifest's eight", compared)
	}
	t.Logf("%d retain rows: load, save and re-load equal the reference's", compared)
}

// TestLobsterRetainUnknownNegativeControl patches the emitted retaining tail in
// memory — the line that writes a retained record's bytes, matched exactly once
// — and requires the saved bytes of retain_round_trip to leave the reference's,
// with the unpatched control equal to them.
func TestLobsterRetainUnknownNegativeControl(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	var row retainRow
	found := false
	for _, r := range retainRows(t) {
		if r.name == "retain_round_trip" {
			row, found = r, true
			break
		}
	}
	if !found {
		t.Fatal("the manifest names no retain_round_trip row")
	}
	paths, err := compiler.GatherPaths(prefix(units[row.unit]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	capacity := retainCapacity(t, g, row)
	want := goRetainRow(t, g, row, capacity, retainIdCapacity(t, row))
	control := runRetainRows(t, g, []retainRow{row})[0]
	if control[3] != want[3] {
		t.Fatalf("the control, unpatched: got %s, want %s", control[3], want[3])
	}
	path := filepath.Join(g.dir, "retain_unknown.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "                so_bytes(w, so_take(inner))\n"
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the retained-tail anchor is in the emitted runtime %d times, want 1", n)
	}
	patched := strings.Replace(string(src), anchor, "                so_bytes(w, \"\")\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	got := runRetainRows(t, g, []retainRow{row})[0]
	if got[3] == want[3] {
		t.Fatalf("the patched tail still writes the reference's bytes: %s", got[3])
	}
	t.Logf("the byte comparison went red under the patch: got %s, want %s", got[3], want[3])
}
