package lobstertable_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
)

// cookWriteRow is one cook-write line of the tables manifest: the instance it
// was written from and the tool's two files (docs/SPEC-TABLES.md §7.6).
type cookWriteRow struct {
	name, little, big string
	unit, root        string
	paths             []string
}

// cookWriteRows reads the manifest's cook-write lines and joins each to the
// instance line that names its unit and root.
func cookWriteRows(t *testing.T) []cookWriteRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	inst := map[string][2]string{}
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
			if len(x) >= 4 {
				inst[x[1]] = [2]string{x[2], x[3]}
			}
		case "cook-write":
			if len(x) < 4 {
				continue
			}
			r := cookWriteRow{name: x[1], little: x[2], big: x[3]}
			if u, ok := inst[x[1]]; ok {
				r.unit, r.root = u[0], u[1]
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
	pkg  string
	dir  string
	base string
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
	g := &cookUnit{pkg: u.Package, dir: t.TempDir()}
	open := []byte("def " + row.root + "Open(")
	for name, data := range files {
		if strings.HasSuffix(name, "Table.lobster") && strings.Contains(string(data), string(open)) {
			g.base = strings.TrimSuffix(name, "Table.lobster")
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

// cookOpen runs the unit's generated Open over data and returns its verdict:
// ok when the record opened, refused when the check said no.
func cookOpen(t *testing.T, g *cookUnit, rootName string, data []byte) string {
	t.Helper()
	main := fmt.Sprintf(`import .%sTable
def run():
    let f = read_file("in.cook")
    assert f
    let v = %s.%sOpen(f)
    print(if v: "ok" else: "refused")
run()
`, g.base, g.pkg, rootName)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"in.cook": data})
	return strings.TrimSpace(string(out))
}

// TestLobsterCook opens every cook-write fixture the manifest names and holds
// Open to the header's check: the tool's own file opens, and the same file
// with one magic byte changed is refused. docs/SPEC-TABLES.md §7, §7.1.
func TestLobsterCook(t *testing.T) {
	t.Parallel()
	rows := cookWriteRows(t)
	if len(rows) == 0 {
		t.Fatal("the manifest names no cook-write rows")
	}
	for _, r := range rows {
		if r.unit == "" {
			t.Fatalf("%s: no instance line names its unit and root", r.name)
		}
		g := generateCookUnit(t, r)
		for _, f := range []string{r.little, r.big} {
			data, err := os.ReadFile(filepath.Join(root, f))
			if err != nil {
				t.Fatal(err)
			}
			if got := cookOpen(t, g, r.root, data); got != "ok" {
				t.Errorf("%s: Open answered %q, want ok", f, got)
			}
		}
		t.Logf("cook-open %s: 2 files", r.name)
	}
}

// TestLobsterCookNegativeControl changes one byte of a fixture's magic and
// holds Open red: the check must refuse a file that is not a cook.
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
	data = append([]byte(nil), data...)
	data[0] ^= 0xff
	if got := cookOpen(t, g, r.root, data); got != "refused" {
		t.Fatalf("%s: a changed magic answered %q, want refused", r.little, got)
	}
}
