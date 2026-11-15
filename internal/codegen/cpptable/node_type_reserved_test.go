package cpptable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestCppNodeTypeReservedIdRefused holds the C++ leg's strict read of
// graphdemo.Scene to the node table's reserved-id rule (docs/SPEC-TABLES.md
// §3.1, §3.3): a node record whose TYPE reference resolves to the node-table
// id, the build-version id or the vocabulary id is malformed, and the whole
// load refuses. The vectors are the held framing files; the manifest's
// node_chain_two is the control, the same read over a lawful node table, so a
// refusal below is the rule and not the probe.
func TestCppNodeTypeReservedIdRefused(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadHeldUnitPaths(t, root, []string{"tables/pointers"})
	model := tabletext.NewModel(u)

	refused := []string{
		"node_type_node_table_id",
		"node_type_build_version_id",
		"node_type_vocabulary_id",
	}
	read := "node_chain_two"

	wire := map[string][]byte{}
	for _, name := range append(append([]string{}, refused...), read) {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		wire[name] = data
	}

	// THE ENGINE, the reference this leg is checked against, answers every
	// vector's verdict first, so a red below is the leg's and not the file's.
	for _, tc := range []struct {
		name, root string
		accepted   bool
	}{
		{"node_type_node_table_id", "Scene", false},
		{"node_type_build_version_id", "Scene", false},
		{"node_type_vocabulary_id", "Scene", false},
		{read, "ListNode", true},
	} {
		def := model.Lookup(tc.root)
		if def == nil {
			t.Fatalf("%s: the unit declares no root %q", tc.name, tc.root)
		}
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(def), wire[tc.name], &report)
		if err != nil && !tablewire.ReadRefusal(err) {
			t.Fatalf("%s: engine Decode returned an error: %v", tc.name, err)
		}
		if ok != tc.accepted {
			t.Fatalf("%s: the engine's verdict moved: accepted=%t, want %t (%v)", tc.name, ok, tc.accepted, err)
		}
	}

	var body string
	for _, name := range refused {
		body += fmt.Sprintf(`
    {
        static const uint8_t vector[] = { %s };
        TableRefuseReason why = ok;
        const Scene * value = SceneReadStrict( vector, (int64_t) sizeof( vector ), &why );
        if ( value != NULL ) { table_release( TableDefaultAllocator(), (void *) value ); printf( "%s: the strict read accepted a reserved node type\n" ); return 1; }
    }
`, cppBytes(wire[name]), name)
	}
	body += fmt.Sprintf(`
    {
        static const uint8_t vector[] = { %s };
        TableRefuseReason why = ok;
        const ListNode * value = ListNodeReadStrict( vector, (int64_t) sizeof( vector ), &why );
        if ( value == NULL ) { printf( "%s: the strict read refused a lawful node table\n" ); return 1; }
        table_release( TableDefaultAllocator(), (void *) value );
    }
`, cppBytes(wire[read]), read)

	out, err := cppHeldRun(t, u, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
