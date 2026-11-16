package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The held graph vectors reach THE GRAPH (docs/SPEC-TABLES.md §6.5) through
// pointers this reader does not declare, so a reader that walks only its own
// declared pointer fields misses them. Each one is a framingVector held out of
// the manifest; its pinned bytes are the script in framing_vectors_test.go and
// the verdict below is the rule text's (§3.1, §6.5).
var heldGraphVectors = []struct {
	name     string
	accepted bool
}{
	// REFUSED: a cycle, an unreachable record, a shared blob or ill-formed
	// text is wire-alone damage wherever the graph carries it.
	{name: "graph_unknown_type_self", accepted: false},
	{name: "graph_unknown_type_pair", accepted: false},
	{name: "graph_listnode_unknown_self", accepted: false},
	{name: "graph_unknown_to_root", accepted: false},
	{name: "graph_blob_two_unknown_fields", accepted: false},
	{name: "graph_blob_two_unknown_elements", accepted: false},
	{name: "graph_string_blob_ill_formed", accepted: false},
	{name: "graph_string_blob_zero_byte", accepted: false},
	{name: "graph_unknown_type_unreached", accepted: false},
	{name: "graph_string_blob_unreached", accepted: false},
	// READ: a record of a type this reader cannot name reads null and counts
	// unknown, whichever unspecified pointer reaches it.
	{name: "graph_reach_unknown_field", accepted: true},
	{name: "graph_reach_unknown_nested", accepted: true},
	{name: "graph_reach_unknown_union_arm", accepted: true},
	{name: "graph_reach_unknown_keyed_slot", accepted: true},
}

// TestHeldGraphVectorsAnswerTheEngine reads each held graph vector's pinned
// file and runs it through the engine exactly as TestFramingVectorsReadThrough
// TheEngine does, comparing the verdict alone (docs/rulings.json
// refusal-verdict-only). A different verdict is an engine or vector defect,
// never a verdict to flip here.
func TestHeldGraphVectorsAnswerTheEngine(t *testing.T) {
	t.Parallel()

	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	u := newUnits(m)
	vectors := map[string]framingVector{}
	for _, v := range framingVectors {
		vectors[v.name] = v
	}
	// A held graph script without a verdict here would let the graph rule go
	// untested, so every one of them must be in the table above.
	listed := map[string]bool{}
	for _, tc := range heldGraphVectors {
		listed[tc.name] = true
	}
	for _, v := range framingVectors {
		if v.held && strings.HasPrefix(v.name, "graph_") && !listed[v.name] {
			t.Errorf("%s: a held graph script with no verdict in heldGraphVectors", v.name)
		}
	}
	for _, tc := range heldGraphVectors {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v, ok := vectors[tc.name]
			if !ok {
				t.Fatalf("%s: no framing vector script", tc.name)
			}
			if !v.held {
				t.Fatalf("%s: the graph vectors are held out of the manifest", tc.name)
			}
			data, err := os.ReadFile(filepath.Join(root, framingDir, tc.name+".bin"))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			unit, err := u.get(v.unit)
			if err != nil {
				t.Fatal(err)
			}
			model := tabletext.NewModel(unit)
			def := model.Lookup(v.root)
			if def == nil {
				t.Fatalf("%s: the unit declares no root %q", tc.name, v.root)
			}
			inst := model.New(def)
			var report tabletext.Report
			accepted, _ := strictDecode(model, inst, data, &report)
			if accepted != tc.accepted {
				t.Errorf("%s: engine accepted=%t want=%t (report=%+v)", tc.name, accepted, tc.accepted, report)
			}
		})
	}
}

// TestKeyedGraphReachabilityControl checks the legal keyed-table spelling and
// that its nested pointer is needed to reach the otherwise orphaned record.
func TestKeyedGraphReachabilityControl(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	u, err := newUnits(m).get("graphdemo")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(root, framingDir, "graph_reach_unknown_keyed_slot.bin"))
	if err != nil {
		t.Fatal(err)
	}
	f := frameWire(wire)
	pointer := -1
	keyedTable := false
	for _, sp := range f.spots {
		if sp.kind == spotKind && sp.role == roleKeyedElement && sp.value == 13 {
			keyedTable = true
		}
		if sp.kind == spotIndex && sp.value == 2 && sp.width == 1 {
			pointer = sp.off
		}
	}
	if !keyedTable || pointer < 0 {
		t.Fatal("fixture must contain a keyed table element with a pointer to node 2")
	}
	model := tabletext.NewModel(u)
	decode := func(data []byte) bool {
		inst := model.New(model.Lookup("Scene"))
		var report tabletext.Report
		ok, err := tablewire.Decode(model, inst, data, &report)
		return err == nil && ok && !report.Refused
	}
	if !decode(wire) {
		t.Fatal("the reachable unknown node must read")
	}
	wire[pointer] = 0
	if decode(wire) {
		t.Fatal("removing the only edge to the node must refuse the orphan")
	}
}
