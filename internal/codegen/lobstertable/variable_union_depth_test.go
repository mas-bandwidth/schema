package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLobsterUnknownUnionCountsItsLevel holds THE ONE NESTING BOUND
// (docs/SPEC-TABLES.md §4) in the wire-alone walk of unknown fields (§6.5): a
// union whose arm is structural is one level and the arm's payload another,
// so depth 128 reads and 129 refuses in every shape the framing vectors pin.
// Only the verdict is compared, and every wire must come back without the
// program raising.
func TestLobsterUnknownUnionCountsItsLevel(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"depth_tables_128.bin":      true,
		"depth_arrays_128.bin":      true,
		"depth_union_tables_64.bin": true,
		"depth_keyed_128.bin":       true,
		"depth_tables_129.bin":      false,
		"depth_arrays_129.bin":      false,
		"depth_union_tables_65.bin": false,
		"depth_keyed_129.bin":       false,
		"depth_tables_5000.bin":     false,
	}
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["graphdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("graphdemo is refused: %v", err)
	}
	model := tabletext.NewModel(g.u)
	scene := model.Lookup("Scene")
	if scene == nil {
		t.Fatal("no table Scene in graphdemo")
	}
	var p strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&p, "import .%s\n", m)
	}
	p.WriteString("import .load\n")
	p.WriteString(keyedRegistrations(g.u))
	inputs := map[string][]byte{}
	for i, name := range names {
		wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing", name))
		if err != nil {
			t.Fatal(err)
		}
		// the engine is the reference: each pinned verdict is its verdict
		var report tabletext.Report
		read, err := tablewire.Decode(model, model.New(scene), wire, &report)
		if err != nil && !tablewire.ReadRefusal(err) {
			t.Fatalf("engine Decode %s: %v", name, err)
		}
		if engine := read && err == nil; engine != cases[name] {
			t.Fatalf("engine verdict for %s: read=%v, want read=%v", name, engine, cases[name])
		}
		inputs[fmt.Sprintf("w%d", i)] = wire
		fmt.Fprintf(&p, "let d%d = read_file(\"w%d\")\nassert d%d\n", i, i, i)
		fmt.Fprintf(&p, "let r%d = schema_rt.LoadReport {}\n", i)
		fmt.Fprintf(&p, "let v%d = schema_rt.load(\"Scene\", d%d, r%d)\n", i, i, i)
		fmt.Fprintf(&p, "print(if v%d and not r%d.refused: \"read\" else: \"refused\")\n", i, i)
		fmt.Fprintf(&p, "schema_rt.release(v%d)\n", i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(p.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)))
	if len(got) != len(names) {
		t.Fatalf("the program printed %d verdicts for %d wires: %q", len(got), len(names), got)
	}
	for i, name := range names {
		want := "refused"
		if cases[name] {
			want = "read"
		}
		if got[i] != want {
			t.Errorf("%s: Lobster verdict %s, want %s", name, got[i], want)
		}
	}
}
