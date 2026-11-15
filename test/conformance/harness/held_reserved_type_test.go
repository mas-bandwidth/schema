package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// THE HELD RESERVED-TYPE VECTORS (docs/SPEC-TABLES.md §3.1, §3.3, §5): a node
// record whose TYPE reference resolves to the node-table id, the build-version
// id or the vocabulary id is malformed, and the whole load refuses. The
// vectors are held out of the manifest until every leg answers them; this gate
// holds the engine, the reference every leg is checked against, to the verdict
// the rule names.
func TestHeldReservedTypeVectorsAnswerTheEngine(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "pointers")})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unit)
	for _, tc := range []struct {
		name     string
		root     string
		accepted bool
	}{
		{name: "node_type_node_table_id", root: "Scene"},
		{name: "node_type_build_version_id", root: "Scene"},
		{name: "node_type_vocabulary_id", root: "Scene"},
	} {
		data, err := os.ReadFile(filepath.Join(root, framingDir, tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		def := model.Lookup(tc.root)
		if def == nil {
			t.Fatalf("%s: the unit declares no root %q", tc.name, tc.root)
		}
		inst := model.New(def)
		var report tabletext.Report
		// The verdict only: strictDecode reports its refusal as an error, and
		// no reason name is compared.
		accepted, _ := strictDecode(model, inst, data, &report)
		if accepted != tc.accepted {
			t.Errorf("%s: accepted=%t want=%t", tc.name, accepted, tc.accepted)
		}
	}
}
