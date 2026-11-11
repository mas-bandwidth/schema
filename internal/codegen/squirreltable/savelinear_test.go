package squirreltable_test

// Functional test (runs the pinned Squirrel interpreter via sqrun.Run): saving
// a pointer graph costs steps linear in its number of nodes
// (docs/SPEC-TABLES.md §3.1: the numbering is first-visit order, one index per
// node). The cost is a count, never a clock: the interpreter's own line hook,
// installed around the save of a copy of the generated runtime, counts every
// line the save executes, for a chain of n nodes and one of 2n, and the ratio
// must stay under 3. The saved bytes of a chain, a diamond, a cycle (refused)
// and a shared blob (refused) are the Go engine's, the engine confirming each
// verdict on the same graph first.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const saveLinearSchema = `package probe
table Node
{
    next  *Node
    side  *Node
    data  *bytes
    more  *bytes
}
`

// saveLinearChain is the chain length n; the probe also saves 2n.
const saveLinearChain = 1000

type saveLinearLink struct {
	from  int
	field string
	to    int
}

type saveLinearBlob struct {
	node  int
	field string
	blob  int
}

// saveLinearEncode builds a Node graph in the Go engine, nodes[0] the root,
// and writes it: the engine's verdict on the graph the probe saves.
func saveLinearEncode(t *testing.T, m *tabletext.Model, count int, links []saveLinearLink, blobs []saveLinearBlob, data [][]byte) ([]byte, error) {
	t.Helper()
	nodes := make([]*tabletext.Instance, count)
	for i := range nodes {
		nodes[i] = m.New(m.Lookup("Node"))
	}
	for _, l := range links {
		f, ok := nodes[l.from].FieldByKey(l.field)
		if !ok {
			t.Fatalf("Node has no field %s", l.field)
		}
		f.Cell.Node = nodes[l.to]
	}
	made := make([]*tabletext.Blob, len(data))
	for i, d := range data {
		made[i] = &tabletext.Blob{Data: append([]byte(nil), d...)}
	}
	for _, b := range blobs {
		f, ok := nodes[b.node].FieldByKey(b.field)
		if !ok {
			t.Fatalf("Node has no field %s", b.field)
		}
		f.Cell.Blob = made[b.blob]
	}
	return tablewire.Encode(m, nodes[0])
}

func saveLinearChainLinks(n int) []saveLinearLink {
	links := make([]saveLinearLink, 0, n)
	for i := 0; i+1 < n; i++ {
		links = append(links, saveLinearLink{i, "next", i + 1})
	}
	return links
}

// the diamond: the root reaches node 3 through next.next and side.next, and
// holds two distinct blobs with equal bytes (two nodes, each owned once)
var (
	saveLinearDiamondLinks = []saveLinearLink{{0, "next", 1}, {0, "side", 2}, {1, "next", 3}, {2, "next", 3}}
	saveLinearDiamondBlobs = []saveLinearBlob{{0, "data", 0}, {3, "more", 1}, {2, "data", 2}}
	saveLinearDiamondData  = [][]byte{{1, 2, 3}, {1, 2, 3}, {9}}
)

const saveLinearProbe = `enabledebuginfo(true);
local inputs = dofile("inputs.nut", true);
local schema = dofile("probe/schema_module.nut", true);
local T = schema.require(%q);
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local hexOf = function(b) {
  if (b == null) return "null";
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%%02x", b[i] & 0xff);
  return s;
};
local steps = 0;
local hook = function(event, src, line, fn) { if (event == 'l') steps++; };
local load = function(key) {
  local r = {};
  local v = L.load(T, "Node", inputs[key], r);
  if (v == null) throw "the probe's input " + key + " did not load";
  return v;
};
local counted = function(key) {
  local v = load(key);
  steps = 0;
  setdebughook(hook);
  local b = S.save(T, "Node", v);
  setdebughook(null);
  print("N|" + key + "|" + steps + "|" + hexOf(b) + "\n");
};
counted("chain_n");
counted("chain_2n");
print("S|diamond|" + hexOf(S.save(T, "Node", load("diamond"))) + "\n");
local cyc = load("chain_3");
cyc.Next.Next.Next = cyc;
print("S|cycle|" + hexOf(S.save(T, "Node", cyc)) + "\n");
local shared = load("diamond");
shared.Next.More = shared.Data;
print("S|shared_blob|" + hexOf(S.save(T, "Node", shared)) + "\n");
`

func TestSquirrelSaveNumberingIsLinear(t *testing.T) {
	t.Parallel()
	u := loadSource(t, saveLinearSchema)
	m := tabletext.NewModel(u)
	n := saveLinearChain

	// THE GO ENGINE'S VERDICTS on the same graphs: chains and the diamond
	// write; a cycle and a blob two slots name are refused.
	wires := map[string][]byte{}
	write := func(key string, count int, links []saveLinearLink, blobs []saveLinearBlob, data [][]byte) {
		t.Helper()
		w, err := saveLinearEncode(t, m, count, links, blobs, data)
		if err != nil {
			t.Fatalf("the Go engine refuses %s: %v", key, err)
		}
		wires[key] = w
	}
	write("chain_n", n, saveLinearChainLinks(n), nil, nil)
	write("chain_2n", 2*n, saveLinearChainLinks(2*n), nil, nil)
	write("chain_3", 3, saveLinearChainLinks(3), nil, nil)
	write("diamond", 4, saveLinearDiamondLinks, saveLinearDiamondBlobs, saveLinearDiamondData)
	if w, err := saveLinearEncode(t, m, 3, append(saveLinearChainLinks(3), saveLinearLink{2, "next", 0}), nil, nil); err == nil || !strings.Contains(err.Error(), "data cycle") {
		t.Fatalf("the Go engine writes a cycle: %d bytes, %v; want the data cycle refusal", len(w), err)
	}
	sharedBlobs := append(append([]saveLinearBlob(nil), saveLinearDiamondBlobs...), saveLinearBlob{1, "more", 0})
	if w, err := saveLinearEncode(t, m, 4, saveLinearDiamondLinks, sharedBlobs, saveLinearDiamondData); err == nil || !strings.Contains(err.Error(), "blob_shared") {
		t.Fatalf("the Go engine writes a shared blob: %d bytes, %v; want blob_shared", len(w), err)
	}

	out := saveLinearRun(t, u, wires)
	steps := map[string]int{}
	saved := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		parts := strings.Split(line, "|")
		switch {
		case len(parts) == 4 && parts[0] == "N":
			k, err := strconv.Atoi(parts[2])
			if err != nil {
				t.Fatalf("malformed step count %q", line)
			}
			steps[parts[1]] = k
			saved[parts[1]] = parts[3]
		case len(parts) == 3 && parts[0] == "S":
			saved[parts[1]] = parts[2]
		default:
			t.Fatalf("unexpected probe line %q", line)
		}
	}

	for _, key := range []string{"chain_n", "chain_2n", "diamond"} {
		if want := hex.EncodeToString(wires[key]); saved[key] != want {
			t.Errorf("%s: the Squirrel save wrote %d hex digits, the Go engine's wire is %d", key, len(saved[key]), len(want))
		}
	}
	for _, key := range []string{"cycle", "shared_blob"} {
		if saved[key] != "null" {
			t.Errorf("%s: the Squirrel save wrote %d hex digits, want refused", key, len(saved[key]))
		}
	}

	a, b := steps["chain_n"], steps["chain_2n"]
	if a <= 0 || b <= 0 {
		t.Fatalf("the save counted no steps: n=%d 2n=%d", a, b)
	}
	t.Logf("save steps: %d nodes %d, %d nodes %d", n, a, 2*n, b)
	if ratio := float64(b) / float64(a); ratio >= 3 {
		t.Errorf("saving a chain of %d nodes took %d steps and of %d nodes %d steps: ratio %.2f, want under 3 (linear)", n, a, 2*n, b, ratio)
	}
}

func saveLinearRun(t *testing.T, u *ir.Unit, inputs map[string][]byte) []byte {
	t.Helper()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "probe")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(fmt.Sprintf(saveLinearProbe, squirreltable.ModuleName(u))))
	return sqrun.Run(t, dir, "main.nut", inputs)
}
