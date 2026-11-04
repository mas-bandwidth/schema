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
// §6.6). capacity is "full" or "short"; ids is "full" or a count;
// saves is "-" or the pinned canonical save files.
type retainRow struct {
	name, unit, root, wire string
	capacity, ids          string
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
		if len(x) == 0 || x[0] != "retain" {
			continue
		}
		if len(x) != 8 {
			t.Fatalf("retain row must have eight fields: %s", sc.Text())
		}
		rows = append(rows, retainRow{name: x[1], unit: x[2], root: x[3], wire: x[4],
			capacity: x[5], ids: x[6], saves: x[7]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
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

// Retention changes neither acceptance nor the ordinary decoded value.
// Failure publishes no record and leaves no captured prefix.
const retainDump = `def rrun(root:string, file:string, cap:int, idcap:int):
    let data = read_file(file)
    assert data
    let retain = schema_rt.Retain { capacity: cap, id_capacity: idcap, used: 0, ids: [], known: [] }
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load_retain(root, data, retain, rep)
    if not rec:
        assert retain.used == 0 and length(retain.ids) == 0 and length(retain.holders) == 0
        print("refused")
        print("nil")
        print("refused")
        print("")
        print("refused")
        print("nil")
        return
    print("read")
    labels = [rec]
    print(drec(rec))
    let saved = schema_rt.save_retain(rec, retain, rep)
    print("read")
    print(hexs(saved))
    let rep2 = schema_rt.LoadReport {}
    let rec2 = schema_rt.load(root, saved, rep2)
    print(if rec2: "read" else: "refused")
    labels = []
    var dump2 = "nil"
    if rec2:
        push(labels, rec2)
        dump2 = drec(rec2)
    print(dump2)
    schema_rt.release(rec)
    schema_rt.release(rec2)
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
	if ok, err := tablewire.DecodeRetain(m, inst, wire, probe, &rep); err != nil || !ok {
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
	var report tabletext.Report
	ok, err := tablewire.DecodeRetain(m, inst, wire, retain, &report)
	if err != nil || !ok {
		if retain.Used() != 0 {
			t.Fatalf("%s: refused reference retains a prefix", row.name)
		}
		return [6]string{"refused", "nil", "refused", "", "refused", "nil"}
	}
	dump := (&goDumper{labels: map[*tabletext.Instance]int{inst: 0}, next: 1}).rec(inst)
	saved, err := tablewire.EncodeRetain(m, inst, retain, &report)
	if err != nil {
		t.Fatalf("%s: accepted retained value cannot save: %v", row.name, err)
	}
	verdict, reloaded := goLoad(g.u, row.root, saved)
	if verdict != "read" {
		t.Fatalf("%s: reference retained save cannot reload", row.name)
	}
	return [6]string{"read", dump, "read", hex.EncodeToString(saved), verdict, reloaded}
}

// Every retain fixture is compared by acceptance, full values and canonical
// saved bytes. Local diagnostic counters are intentionally outside this contract.
func TestLobsterRetainUnknown(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	rows := retainRows(t)
	if len(rows) == 0 {
		t.Fatal("no retention fixtures")
	}
	byUnit := map[string][]retainRow{}
	var order []string
	for _, row := range rows {
		if _, ok := units[row.unit]; !ok {
			t.Fatalf("undeclared unit %s", row.unit)
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
			t.Fatal(err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		group := byUnit[key]
		got := runRetainRows(t, g, group)
		for i, row := range group {
			want := goRetainRow(t, g, row, retainCapacity(t, g, row), retainIdCapacity(t, row))
			for column, label := range []string{"acceptance", "loaded value", "save acceptance", "canonical bytes", "reload acceptance", "reloaded value"} {
				if got[i][column] != want[column] {
					t.Errorf("%s: %s differs at %s", row.name, label, divergence(got[i][column], want[column]))
				}
			}
			// A refused input has no saved value. Accepted pinned saves still bind.
			if want[0] == "read" && row.saves != "-" {
				for pinned := range strings.SplitSeq(row.saves, ",") {
					data, err := os.ReadFile(filepath.Join(root, pinned))
					if err != nil {
						t.Fatal(err)
					}
					if got[i][3] != hex.EncodeToString(data) {
						t.Errorf("%s: canonical save differs from %s", row.name, pinned)
					}
				}
			}
			compared++
		}
	}
	if compared != len(rows) {
		t.Fatalf("compared %d of %d retaining fixtures", compared, len(rows))
	}
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
