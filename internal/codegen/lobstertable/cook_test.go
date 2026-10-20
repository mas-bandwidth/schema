package lobstertable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookWriteRow is one cook-write line of the tables manifest: the instance it
// was written from, the wire it loads and the tool's two files
// (docs/SPEC-TABLES.md §7.6).
type cookWriteRow struct {
	name, little, big string
	unit, root, wire  string
	paths             []string
}

// cookWriteRows reads the manifest's cook-write lines and joins each to the
// instance line that names its unit, root and wire.
func cookWriteRows(t *testing.T) []cookWriteRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	inst := map[string][3]string{}
	units := map[string][]string{}
	var rows []cookWriteRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) == 0 || strings.HasPrefix(x[0], "#") {
			continue
		}
		switch x[0] {
		case "unit":
			if len(x) >= 3 {
				units[x[1]] = x[2:]
			}
		case "instance":
			if len(x) >= 5 {
				inst[x[1]] = [3]string{x[2], x[3], x[4]}
			}
		case "cook-write":
			if len(x) < 4 {
				continue
			}
			r := cookWriteRow{name: x[1], little: x[2], big: x[3]}
			if u, ok := inst[x[1]]; ok {
				r.unit, r.root, r.wire = u[0], u[1], u[2]
				r.paths = units[u[0]]
			}
			rows = append(rows, r)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// cookUnit is one unit generated for a cook probe.
type cookUnit struct {
	u    *ir.Unit
	pkg  string
	dir  string
	base string
	mods []string
}

// generateCookUnit compiles a manifest unit and writes its Lobster modules.
func generateCookUnit(t *testing.T, row cookWriteRow) *cookUnit {
	t.Helper()
	var want []string
	for _, p := range row.paths {
		want = append(want, filepath.Join(root, p))
	}
	paths, err := compiler.GatherPaths(want)
	if err != nil {
		t.Fatalf("%s: %v", row.unit, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("%s: %v", row.unit, err)
	}
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatalf("%s: lobster: %v", row.unit, err)
	}
	tables, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatalf("%s: lobster tables: %v", row.unit, err)
	}
	for k, v := range tables {
		files[k] = v
	}
	g := &cookUnit{u: u, pkg: u.Package, dir: t.TempDir()}
	open := []byte("def " + row.root + "Open(")
	for name, data := range files {
		if strings.HasSuffix(name, "Table.lobster") {
			g.mods = append(g.mods, strings.TrimSuffix(name, ".lobster"))
			if strings.Contains(string(data), string(open)) {
				g.base = strings.TrimSuffix(name, "Table.lobster")
			}
		}
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if g.base == "" {
		t.Fatalf("%s: no table module declares %sOpen", row.unit, row.root)
	}
	return g
}

// cookProbe is the Lobster program for one row: load the wire, measure and
// cook it in both orders, open what was written, read it back and save it.
func cookProbe(g *cookUnit, row cookWriteRow) string {
	var imports strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&imports, "import .%s\n", m)
	}
	return fmt.Sprintf(`%simport .load
import .schema_save
%s
%s
def run():
    let w = read_file("w")
    assert w
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load(%s, w, rep)
    assert rec
    let m = %s.%sCookMeasure(rec)
    print("{m}")
    let le = %s.%sCook(rec, false)
    let be = %s.%sCook(rec, true)
    hex(le)
    hex(be)
    print(if %s.%sOpen(le): "ok" else: "no")
    print(if %s.%sOpen(be): "ok" else: "no")
    let back = %s.%sReadCook(le)
    if back:
        hex(schema_rt.save(back))
    else:
        print("nil")
    let bad = read_file("bad.cook")
    assert bad
    print(if %s.%sOpen(bad): "ok" else: "no")
run()
`, imports.String(), lobsterrun.HexProbe, keyedRegistrations(g.u),
		lobster.StringLit([]byte(row.root)),
		g.pkg, row.root,
		g.pkg, row.root,
		g.pkg, row.root,
		g.pkg, row.root,
		g.pkg, row.root,
		g.pkg, row.root,
		g.pkg, row.root)
}

// cookOutput is one row's probe output.
type cookOutput struct {
	measure int
	little  []byte
	big     []byte
	saved   []byte
	open    [3]string
	raw     string
}

func runCookProbe(t *testing.T, g *cookUnit, row cookWriteRow, bad []byte) cookOutput {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(root, row.wire))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(cookProbe(g, row)), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"w": wire, "bad.cook": bad})
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 7 && len(lines) != 8 {
		t.Fatalf("%s: the probe printed %d lines, want 7 or 8:\n%s", row.name, len(lines), raw)
	}
	var out cookOutput
	out.raw = string(raw)
	fmt.Sscanf(lines[0], "%d", &out.measure)
	out.little, _ = hex.DecodeString(lines[1])
	out.big, _ = hex.DecodeString(lines[2])
	out.open[0], out.open[1] = lines[3], lines[4]
	if len(lines) >= 6 && lines[5] != "nil" && lines[5] != "" {
		out.saved, _ = hex.DecodeString(lines[5])
	}
	out.open[2] = lines[len(lines)-1]
	return out
}

// goSaved is the reference's canonical wire for one loaded wire: the value
// the cooked read must save back out (docs/SPEC-TABLES.md §7.5, uncook).
func goSaved(u *ir.Unit, row cookWriteRow, wire []byte) ([]byte, error) {
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(row.root))
	var rep tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &rep); err != nil {
		return nil, err
	}
	return tablewire.Encode(m, inst)
}

// TestLobsterCook writes the cook of every manifest cook-write row from its
// wire, in both byte orders, and holds it byte for byte to the tool's files;
// opens what it wrote; reads it back and saves it to the reference's canonical
// wire. Each construct is named with the count of its rows.
// docs/SPEC-TABLES.md §7, §7.2, §7.6.
func TestLobsterCook(t *testing.T) {
	t.Parallel()
	rows := cookWriteRows(t)
	if len(rows) == 0 {
		t.Fatal("the manifest names no cook-write rows")
	}
	orders, opened := 0, 0
	for _, r := range rows {
		if r.unit == "" || r.wire == "" {
			t.Fatalf("%s: no instance line names its unit, root and wire", r.name)
		}
		g := generateCookUnit(t, r)
		wantLE, err := os.ReadFile(filepath.Join(root, r.little))
		if err != nil {
			t.Fatal(err)
		}
		wantBE, err := os.ReadFile(filepath.Join(root, r.big))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(root, r.wire))
		if err != nil {
			t.Fatal(err)
		}
		bad := append([]byte(nil), wantLE...)
		if len(bad) > 0 {
			bad[0] ^= 0xff
		}
		out := runCookProbe(t, g, r, bad)
		if out.measure != len(wantLE) || out.measure != len(wantBE) {
			t.Errorf("%s: CookMeasure answered %d, the tool's files are %d and %d", r.name, out.measure, len(wantLE), len(wantBE))
		}
		if string(out.little) != string(wantLE) {
			t.Errorf("%s: the little-endian cook is not the tool's: %s", r.name, divergence(string(out.little), string(wantLE)))
		}
		if string(out.big) != string(wantBE) {
			t.Errorf("%s: the big-endian cook is not the tool's: %s", r.name, divergence(string(out.big), string(wantBE)))
		}
		if out.open[0] != "ok" || out.open[1] != "ok" {
			t.Errorf("%s: Open answered %q/%q over its own cooks, want ok/ok", r.name, out.open[0], out.open[1])
		} else {
			opened++
		}
		if out.open[2] != "no" {
			t.Errorf("%s: Open answered %q over a cook with a changed magic, want no", r.name, out.open[2])
		}
		orders += 2
		if want, err := goSaved(g.u, r, wire); err == nil {
			if out.saved == nil {
				t.Errorf("%s: ReadCook answered nil over the cook it wrote:\n%s", r.name, out.raw)
			} else if string(out.saved) != string(want) {
				t.Errorf("%s: the saved wire is not the reference's: %s", r.name, divergence(string(out.saved), string(want)))
			}
		} else {
			t.Logf("%s: the reference's wire writer refuses the shared blob (%v); the cooked read was not compared to it", r.name, err)
		}
		t.Logf("cook-write %s: %d bytes, %d orders", r.name, len(wantLE), 2)
	}
	t.Logf("cook-write rows: %d; orders written: %d; own cooks opened: %d", len(rows), orders, opened)
}

// TestLobsterCookNegativeControl changes one cook's magic and holds Open red:
// the check must refuse a file that is not a cook.
func TestLobsterCookNegativeControl(t *testing.T) {
	t.Parallel()
	rows := cookWriteRows(t)
	if len(rows) == 0 {
		t.Fatal("the manifest names no cook-write rows")
	}
	r := rows[0]
	g := generateCookUnit(t, r)
	data, err := os.ReadFile(filepath.Join(root, r.little))
	if err != nil {
		t.Fatal(err)
	}
	main := fmt.Sprintf(`import .%sTable
def run():
    let f = read_file("in.cook")
    assert f
    print(if %s.%sOpen(f): "ok" else: "refused")
run()
`, g.base, g.pkg, r.root)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"in.cook": data}))); got != "ok" {
		t.Fatalf("%s: the control, unpatched: Open answered %q, want ok", r.name, got)
	}
	bad := append([]byte(nil), data...)
	bad[0] ^= 0xff
	got := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"in.cook": bad})))
	if got != "refused" {
		t.Fatalf("%s: a changed magic answered %q, want refused", r.name, got)
	}
	t.Logf("negative control: one magic byte changed, Open refused the cook")
}
