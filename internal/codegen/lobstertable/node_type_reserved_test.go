package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS, IS MALFORMED
// (docs/SPEC-TABLES.md §3.1, §3.3): a node record whose type reference
// resolves to the node-table, build-version or vocabulary id refuses the
// whole strict load. A lawful two-node chain stays a control that reads.
func TestLobsterNodeTypeReservedIdRefused(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["graphdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("graphdemo is refused: %v", err)
	}
	cases := []struct {
		name, root, want string
	}{
		{"node_type_node_table_id", "Scene", "refused"},
		{"node_type_build_version_id", "Scene", "refused"},
		{"node_type_vocabulary_id", "Scene", "refused"},
		{"node_type_ref_zero", "Scene", "refused"},
		{"node_chain_two", "ListNode", "read"},
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .load
import .strict_and_refusals
def verdict(root:string, file:string):
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec, reason = schema_rt.load_strict(root, data, rep)
    assert (reason == "") == (rec != nil)
    print(if rec: "read" else: "refused")
    schema_rt.release(rec)
`)
	inputs := map[string][]byte{}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing", c.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[c.name] = data
		fmt.Fprintf(&program, "verdict(%s, %s)\n", lobster.StringLit([]byte(c.root)), lobster.StringLit([]byte(c.name)))
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)))
	if len(got) != len(cases) {
		t.Fatalf("got %d verdicts, want %d: %q", len(got), len(cases), got)
	}
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s: strict read verdict %q, want %q", c.name, got[i], c.want)
		}
	}
}
