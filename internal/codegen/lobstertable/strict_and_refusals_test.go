package lobstertable_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// strictDump is the strict read's half of the harness: the plain name
// (schema_rt.load_strict) is the strict read and load_tolerant is the
// tolerant walk under its own name (docs/SPEC-TABLES.md §6.5). It prints the
// strict reason (or ok), the tolerant report line and THE VALUE DUMP, so the
// three answers are held to the reference together.
const strictDump = `
def strictrun(root:string, file:string):
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec, reason = schema_rt.load_strict(root, data, rep)
    let tol = schema_rt.LoadReport {}
    let trec = schema_rt.load_tolerant(root, data, tol)
    print(if reason == "": "ok" else: reason)
    print(report_line(tol))
    labels = []
    var dump = "nil"
    let shown = if trec: trec else: rec
    if shown:
        push(labels, shown)
        dump = drec(shown)
    let n = (length(dump) + 39999) / 40000
    print("{n}")
    var i = 0
    while i < n:
        print(substring(dump, i * 40000, min(40000, length(dump) - i * 40000)))
        i++
`

func strictProgram(g *loadUnit, roots []string) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\n")
	b.WriteString("import .strict_and_refusals\n")
	b.WriteString(keyedRegistrations(g.u))
	b.WriteString(dumpLobster)
	b.WriteString(strictDump)
	for i, r := range roots {
		fmt.Fprintf(&b, "strictrun(%s, \"w%d\")\n", lobster.StringLit([]byte(r)), i)
	}
	return b.String()
}

// goStrictReason is the Go reference's strict answer for one wire, in the
// order TableReport.Err() applies (§6.5): a non-finite float first, a form
// refusal as unknown_form, then missing_required, malformed, blob_shared,
// clamped and kind_mismatch; unknown and widened alone are not a refusal.
func goStrictReason(u *irUnit, rootName string, wire []byte) string {
	m := tabletext.NewModel(u.u)
	inst := m.New(m.Lookup(rootName))
	var rep tabletext.Report
	_, err := tablewire.Decode(m, inst, wire, &rep)
	switch {
	case strings.HasPrefix(rep.Reason, "non_finite: ") || strings.HasPrefix(rep.Reason, "non_finite_float: "):
		// a non-finite float outranks every other verdict (§3.3); the
		// engine on main spells it non_finite until the finite-tables
		// oracle lands, and this port spells it non_finite_float already
		return "non_finite_float"
	case err != nil:
		return "unknown_form"
	case strings.HasPrefix(rep.Reason, "missing_required"):
		return "missing_required"
	case rep.Malformed:
		return "malformed"
	case rep.BlobShared > 0:
		return "blob_shared"
	case rep.Clamped > 0:
		return "clamped"
	case rep.KindMismatch > 0:
		return "kind_mismatch"
	}
	return "ok"
}

// irUnit is the thin alias that keeps goStrictReason's signature readable.
type irUnit = loadUnit

type strictResult struct {
	reason []string
	line   []string
	dump   []string
}

func runStrictRows(t *testing.T, g *loadUnit, rows []loadRow) strictResult {
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
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(strictProgram(g, roots)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	res := strictResult{}
	for range rows {
		if len(out) < 3 {
			t.Fatalf("unit %s: the program's output ends early: %q", rows[0].unit, out)
		}
		var n int
		if _, err := fmt.Sscanf(out[2], "%d", &n); err != nil || len(out) < 3+n {
			t.Fatalf("unit %s: the program's output is not reason, report, count, dump: %q", rows[0].unit, out[2])
		}
		res.reason = append(res.reason, out[0])
		res.line = append(res.line, out[1])
		res.dump = append(res.dump, strings.Join(out[3:3+n], ""))
		out = out[3+n:]
	}
	if len(out) != 0 {
		t.Fatalf("unit %s: %d lines past the last row: %q", rows[0].unit, len(out), out)
	}
	return res
}

// TestLobsterStrictRead holds the strict read and every read-time refusal to
// docs/SPEC-TABLES.md §6.5 and to Go's own answer on every manifest row: the
// plain name is strict, the tolerant walk has its own name, unknown and
// widened alone do not refuse, and malformed, clamped, kind_mismatch,
// blob_shared, missing_required, non_finite_float and an unknown form each
// refuse by Go's name. It
// also runs load, save and re-load through the same unit and holds the bytes
// and the report to the reference's, and it refuses the probe units' trailing
// root bytes and negative id count by name. Every construct is named with its
// count in the test log.
func TestLobsterStrictRead(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	byUnit := map[string][]loadRow{}
	var order []string
	for _, row := range loadRows(t) {
		if _, ok := units[row.unit]; !ok {
			t.Fatalf("row %s names unit %s, which the manifest does not declare", row.name, row.unit)
		}
		if _, seen := byUnit[row.unit]; !seen {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	counts := map[string]int{}
	var refusedUnits []string
	rowsRun := 0
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			refusedUnits = append(refusedUnits, key)
			t.Logf("refused unit %s: %v", key, err)
			continue
		}
		res := runStrictRows(t, g, byUnit[key])
		for i, row := range byUnit[key] {
			wire := row.data
			if wire == nil {
				if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
					t.Fatalf("%s: %v", row.name, err)
				}
			}
			want := goStrictReason(g, row.root, wire)
			counts[want]++
			rowsRun++
			if res.reason[i] != want {
				t.Errorf("%s %s: strict read = %s, want %s (docs/SPEC-TABLES.md §6.5)", row.kind, row.name, res.reason[i], want)
			}
			wantLine, wantDump := goLoad(g.u, row.root, wire)
			if res.line[i] != wantLine {
				t.Errorf("%s %s: tolerant report = %s, want %s", row.kind, row.name, res.line[i], wantLine)
			}
			if res.dump[i] != wantDump {
				t.Errorf("%s %s: tolerant value is not the reference's at byte %s", row.kind, row.name, divergence(res.dump[i], wantDump))
			}
		}
		// SAVE AND RE-LOAD: every row the strict read accepts is saved and
		// re-loaded, and both are held to the Go engine byte for byte and
		// report for report.
		var loadable []loadRow
		for i, row := range byUnit[key] {
			if res.reason[i] == "ok" {
				loadable = append(loadable, row)
			}
		}
		if len(loadable) == 0 {
			continue
		}
		sav := runSaveRows(t, g, loadable)
		for i, row := range loadable {
			wire := row.data
			if wire == nil {
				if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
					t.Fatalf("%s: %v", row.name, err)
				}
			}
			want, err := goSave(g, row.root, wire)
			if err != nil {
				continue
			}
			if sav.saved[i] != fmt.Sprintf("%x", want) {
				t.Errorf("%s %s: save is not the reference's", row.kind, row.name)
			}
			_, wantReload := goLoad(g.u, row.root, want)
			if sav.dump[i] != wantReload {
				t.Errorf("%s %s: the re-load is not the reference's at byte %s", row.kind, row.name, divergence(sav.dump[i], wantReload))
			}
		}
	}
	if rowsRun == 0 {
		t.Fatal("no manifest row was read: the comparison is vacuous")
	}
	for _, name := range []string{"ok", "malformed", "clamped", "kind_mismatch", "blob_shared", "missing_required", "non_finite_float", "unknown_form"} {
		t.Logf("strict construct %s: %d", name, counts[name])
	}

	// THE PROBE UNITS: a root with trailing bytes and a negative id count are
	// refused by name, and a clean root is not (docs/SPEC-TABLES.md §3, §3.1).
	g := probeUnit(t)
	clean := probeWire(t, g)
	trailing := append(bytes.Clone(clean), 0x00)
	negative := bytes.Clone(clean)
	for i := len(negative) - 8; i < len(negative); i++ {
		negative[i] = 0xff
	}
	probes := []loadRow{
		{kind: "probe", name: "clean", unit: "probe", root: "Root", data: clean},
		{kind: "probe", name: "trailing", unit: "probe", root: "Root", data: trailing},
		{kind: "probe", name: "negative-id-count", unit: "probe", root: "Root", data: negative},
	}
	pres := runStrictRows(t, g, probes)
	for i, row := range probes {
		want := goStrictReason(g, row.root, row.data)
		counts[want]++
		if pres.reason[i] != want {
			t.Errorf("%s %s: strict read = %s, want %s", row.kind, row.name, pres.reason[i], want)
		}
	}
	if counts["malformed"] == 0 {
		t.Fatalf("no malformed refusal was exercised: the probe units do not reach the name")
	}
	t.Logf("probe refusals: trailing=%s negative-id-count=%s", pres.reason[1], pres.reason[2])
	if len(refusedUnits) > 0 {
		t.Logf("units the surface refused by name: %s", strings.Join(refusedUnits, ", "))
	}
}

// TestLobsterStrictReadNegativeControl breaks the malformed clause of the
// emitted strict read and requires the strict comparison to go red.
func TestLobsterStrictReadNegativeControl(t *testing.T) {
	t.Parallel()
	g := probeUnit(t)
	clean := probeWire(t, g)
	trailing := append(bytes.Clone(clean), 0x00)
	rows := []loadRow{{kind: "probe", name: "trailing", unit: "probe", root: "Root", data: trailing}}
	want := goStrictReason(g, "Root", trailing)
	if got := runStrictRows(t, g, rows).reason[0]; got != want {
		t.Fatalf("the control, unpatched: strict read = %s, want %s", got, want)
	}
	path := filepath.Join(g.dir, "load.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "    if rep.malformed:\n        return \"malformed\"\n"
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the malformed anchor is in the emitted runtime %d times, want 1", n)
	}
	patched := strings.Replace(string(src), anchor, "    if rep.malformed:\n        return \"\"\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runStrictRows(t, g, rows).reason[0]; got == want {
		t.Fatalf("the patched strict read still refuses by name: %s", got)
	} else {
		t.Logf("the strict comparison went red under the patch: got %s, want %s", got, want)
	}
}

const probeSchema = `package probe

table Root
{
    n int32 = 1
}
`

func probeUnit(t *testing.T) *loadUnit {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(probeSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the probe unit: %v", err)
	}
	return g
}

func probeWire(t *testing.T, g *loadUnit) []byte {
	t.Helper()
	m := tabletext.NewModel(g.u)
	inst := m.New(m.Lookup("Root"))
	inst.Fields[0].Cell.I = 3
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}
