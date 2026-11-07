package lobstertable_test

import (
	"bytes"
	"encoding/binary"
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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The ordinary and diagnostic entry points must publish the same complete
// value or both refuse. Diagnostics are checked for presence, not spelling.
const strictDump = `
def strictrun(root:string, file:string):
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec, reason = schema_rt.load_strict(root, data, rep)
    let plain_report = schema_rt.LoadReport {}
    let plain = schema_rt.load(root, data, plain_report)
    assert (rec != nil) == (plain != nil)
    assert (reason == "") == (rec != nil)
    print(if rec: "ok" else: "refused")
    print(if plain: "read" else: "refused")
    labels = []
    var dump = "nil"
    if rec:
        push(labels, rec)
        dump = drec(rec)
    if plain:
        labels = [plain]
        assert drec(plain) == dump
    let n = (length(dump) + 39999) / 40000
    print("{n}")
    var i = 0
    while i < n:
        print(substring(dump, i * 40000, min(40000, length(dump) - i * 40000)))
        i++
    schema_rt.release(rec)
    schema_rt.release(plain)
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

func goStrictReason(u *loadUnit, rootName string, wire []byte) string {
	verdict, _ := goLoad(u.u, rootName, wire)
	if verdict == "read" {
		return "ok"
	}
	return "refused"
}

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

// Manifest inputs are compared by acceptance, complete values and canonical
// write-back. No diagnostic taxonomy is a conformance promise.
func TestLobsterStrictRead(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	byUnit := map[string][]loadRow{}
	var order []string
	for _, row := range loadRows(t) {
		if _, ok := units[row.unit]; !ok {
			t.Fatalf("row %s names undeclared unit %s", row.name, row.unit)
		}
		if _, seen := byUnit[row.unit]; !seen {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	accepted, refused := 0, 0
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatal(err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		rows := byUnit[key]
		res := runStrictRows(t, g, rows)
		var loadable []loadRow
		for i, row := range rows {
			wire := row.data
			if wire == nil {
				wire, err = os.ReadFile(filepath.Join(root, row.wire))
				if err != nil {
					t.Fatal(err)
				}
			}
			wantLine, wantDump := goLoad(g.u, row.root, wire)
			wantReason := "refused"
			if wantLine == "read" {
				wantReason = "ok"
			}
			if res.reason[i] != wantReason || res.line[i] != wantLine {
				t.Errorf("%s: acceptance %s/%s, want %s/%s", row.name, res.reason[i], res.line[i], wantReason, wantLine)
			}
			if res.dump[i] != wantDump {
				t.Errorf("%s: value differs at %s", row.name, divergence(res.dump[i], wantDump))
			}
			if wantLine == "read" {
				accepted++
				loadable = append(loadable, row)
			} else {
				refused++
			}
		}
		if len(loadable) == 0 {
			continue
		}
		saved := runSaveRows(t, g, loadable)
		for i, row := range loadable {
			wire := row.data
			if wire == nil {
				wire, err = os.ReadFile(filepath.Join(root, row.wire))
				if err != nil {
					t.Fatal(err)
				}
			}
			want, err := goSave(g, row.root, wire)
			if err != nil {
				t.Fatalf("accepted %s cannot save: %v", row.name, err)
			}
			if saved.saved[i] != fmt.Sprintf("%x", want) {
				t.Errorf("%s: canonical write-back differs", row.name)
			}
			verdict, wantDump := goLoad(g.u, row.root, want)
			if verdict != "read" || saved.dump[i] != wantDump {
				t.Errorf("%s: canonical reload differs at %s", row.name, divergence(saved.dump[i], wantDump))
			}
		}
	}
	if accepted == 0 || refused == 0 {
		t.Fatalf("vacuous coverage: accepted=%d refused=%d", accepted, refused)
	}
	g := probeUnit(t)
	clean := probeWire(t, g)
	trailing := append(bytes.Clone(clean), 0)
	negative := bytes.Clone(clean)
	for i := len(negative) - 8; i < len(negative); i++ {
		negative[i] = 255
	}
	probes := []loadRow{
		{name: "clean", unit: "probe", root: "Root", data: clean},
		{name: "trailing", unit: "probe", root: "Root", data: trailing},
		{name: "negative-id-count", unit: "probe", root: "Root", data: negative},
	}
	result := runStrictRows(t, g, probes)
	for i, row := range probes {
		expected := "refused"
		if i == 0 {
			expected = "ok"
		}
		if result.reason[i] != expected {
			t.Errorf("%s: %s, want %s", row.name, result.reason[i], expected)
		}
		if i > 0 && result.dump[i] != "nil" {
			t.Errorf("%s publishes partial data", row.name)
		}
	}
	t.Logf("%d accepted complete values and canonical saves; %d whole refusals", accepted, refused)
}

// Bypassing generic payload validation must expose an invalid unknown bool.
func TestLobsterStrictReadNegativeControl(t *testing.T) {
	t.Parallel()
	g := probeUnit(t)
	wire := []byte{1, 1, 1, 2, 0}
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("future"))
	wire = binary.LittleEndian.AppendUint64(wire, 1)
	rows := []loadRow{{name: "unknown-bool", unit: "probe", root: "Root", data: wire}}
	if result := runStrictRows(t, g, rows); result.reason[0] != "refused" || result.dump[0] != "nil" {
		t.Fatalf("invalid bool accepted before mutation: %+v", result)
	}
	path := filepath.Join(g.dir, "load.lobster")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const guard = "    if not wire_check(st, first):"
	if strings.Count(string(source), guard) != 1 {
		t.Fatal("wire check guard not unique")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(source), guard, "    if false:", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if result := runStrictRows(t, g, rows); result.reason[0] != "ok" {
		t.Fatalf("mutation did not expose acceptance: %+v", result)
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
