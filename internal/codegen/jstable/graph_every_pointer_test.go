package jstable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestJsGraphCheckedOverEveryPointer holds the JavaScript strict reader to the
// declaration-free graph walk in docs/SPEC-TABLES.md §3.1 and §6.5.
func TestJsGraphCheckedOverEveryPointer(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Case string `json:"case"`
		Root string `json:"root"`
		Wire string `json:"wire"`
		Load bool   `json:"load"`
	}
	cases := []struct {
		name string
		load bool
	}{
		{"graph_unknown_type_self", false},
		{"graph_unknown_type_pair", false},
		{"graph_listnode_unknown_self", false},
		{"graph_unknown_to_root", false},
		{"graph_blob_two_unknown_fields", false},
		{"graph_blob_two_unknown_elements", false},
		{"graph_string_blob_ill_formed", false},
		{"graph_string_blob_zero_byte", false},
		{"graph_unknown_type_unreached", false},
		{"graph_string_blob_unreached", false},
		{"graph_reach_unknown_field", true},
		{"graph_reach_unknown_nested", true},
		{"graph_reach_unknown_union_arm", true},
		{"graph_reach_unknown_keyed_slot", true},
	}
	rows := make([]row, 0, len(cases)+1)
	files := make(map[string][]byte, len(cases)+1)
	for _, c := range cases {
		name := c.name + ".bin"
		wirePath := filepath.Join(repo, "testdata/wire/tables/framing", name)
		wire, err := os.ReadFile(wirePath)
		if err != nil {
			t.Fatalf("read %s: %v", c.name, err)
		}
		files[name] = wire
		rows = append(rows, row{Case: c.name, Root: "Scene", Wire: name, Load: c.load})
	}
	chainPath := filepath.Join(repo, "testdata/wire/tables/framing/node_chain_two.bin")
	chain, err := os.ReadFile(chainPath)
	if err != nil {
		t.Fatalf("read node_chain_two: %v", err)
	}
	files["node_chain_two.bin"] = chain
	rows = append(rows, row{Case: "node_chain_two", Root: "ListNode", Wire: "node_chain_two.bin", Load: true})
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/pointers")
	jsDiscardRun(t, u, files, `
for (const row of `+string(encoded)+`) {
  const wire = new Uint8Array(readFileSync(row.wire));
  const r = new m.TableVariableReport();
  const v = m[row.root + "Load"](wire, r);
  if (row.load) {
    if (v === null || r.err() !== null) { fail(row.case + ": expected read, got refusal: " + JSON.stringify(r)); }
  } else if (v !== null || r.err() === null) {
    fail(row.case + ": expected refusal, got read");
  }
  const retain = new m.TableVariableRetain(new Uint8Array(1 << 16), new Array(256));
  const rr = new m.TableVariableReport();
  const rv = m[row.root + "LoadRetain"](wire, retain, rr);
  if (row.load) {
    if (rv === null || rr.err() !== null) { fail(row.case + ": retaining load expected read, got refusal: " + JSON.stringify(rr)); }
  } else if (rv !== null || rr.err() === null || retain.used !== 0 || retain.count !== 0) {
    fail(row.case + ": retaining load expected refusal: " + JSON.stringify(rr));
  }
}
`)
}
