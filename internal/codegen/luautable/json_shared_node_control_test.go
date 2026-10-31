package luautable_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

// TestLuauJsonSharedNodeNegativeControl is M14's control (docs/PORTING.md,
// the `&node` label; docs/SPEC-TABLES.md §16.7). It patches the emitted
// runtime's pointer writer so a node two pointers share is written inline at
// every appearance, with no label, and requires the json-write comparison of
// TestLuauJsonAgreesWithTheCorpus's graph_shared row to go red while the
// json-read direction stays green: the gate sees the label, not something
// else the patch broke.
func TestLuauJsonSharedNodeNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "  if count <= 1 then return write_table(T, desc, node, depth, g, w) end\n"
	const patched = "  if true then return write_table(T, desc, node, depth, g, w) end\n"
	src, err := os.ReadFile(filepath.Join("runtime", "schema_json.luau"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(src), anchor) != 1 {
		t.Fatalf("the shared-node anchor %q is not in the runtime exactly once", anchor)
	}
	units, _ := readManifest(t)
	g := generateUnit(t, units, "graphdemo")
	if g.err != nil {
		t.Fatalf("graphdemo does not generate: %v", g.err)
	}
	text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", "graph_shared.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), `"&node"`) {
		t.Fatal("graph_shared.json carries no &node label, so the control would prove nothing")
	}
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "graph_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonInstanceProbe(g, "Scene")), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func() []string {
		out := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": text, "wire": wire})), "\n")
		parts := strings.Split(out, "\n")
		if len(parts) != 3 {
			t.Fatalf("bad output %q", out)
		}
		return parts
	}
	clean := run()
	if clean[0] != hexBytes(wire) || clean[1] != hexBytes(text) || clean[2] != "00" {
		t.Fatal("the unpatched runtime does not pass graph_shared, so the control has nothing to break")
	}
	bad := strings.Replace(string(src), anchor, patched, 1)
	if err := os.WriteFile(filepath.Join(g.dir, "schema_json.luau"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	parts := run()
	if parts[0] != hexBytes(wire) {
		t.Fatal("the patched runtime broke json-read too, so the control is not the label alone")
	}
	if parts[1] == hexBytes(text) {
		t.Fatal("the patched runtime still writes graph_shared's pinned text byte for byte: the gate cannot see the label")
	}
	if strings.Contains(string(fromHex(t, parts[1])), `"&node"`) {
		t.Fatal("the patched runtime still writes a label, so the patch missed the shared-node writer")
	}
	t.Logf("graph_shared json-write went red: the shared node was written inline at each pointer")
}

func fromHex(t *testing.T, h string) []byte {
	t.Helper()
	if h == "nil" {
		return nil
	}
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatalf("not hex: %q", h)
	}
	return b
}
