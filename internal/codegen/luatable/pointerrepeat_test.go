package luatable

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A POINTER SLOT THE WIRE NAMES TWICE (docs/SPEC-TABLES.md §3, §3.1, §4): a
// repeated field id is legal input and the LAST occurrence wins, and a pointer
// whose index does not resolve reads null, so a repeat that fails resolution
// leaves the slot null, never the node an earlier occurrence named
// (decodenodes.go resolveCell clears the cell before it resolves). A repeat of
// a FIXED array keeps the slots it could not replace, resolved (decode.go
// arrayBody leaves fv.Elems standing).
//
// The first case is the certify mutant that found it (run 41211929427,
// release-gates tables-lua-release, wire-fuzz seed 2, random pass #2744 of
// graph_tree): `tree` rides twice, the second time naming the Settings node,
// and the Lua load kept the TreeNode the first occurrence named (the leg saved
// 148 bytes, the engine 91). The rest are the same seeds with one occurrence
// spliced in ahead of the node table, one per way a slot can fail to resolve.
func TestLuaPointerRepeatAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	seed := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	// splice inserts extra at offset at, which is a field boundary of the root
	// body: the root body carries no length, so the wire stays framed
	splice := func(base []byte, at int, want, extra string) []byte {
		w, err := hex.DecodeString(want)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(base[at:]), string(w)) {
			t.Fatalf("the seed does not carry %s at %#x", want, at)
		}
		e, err := hex.DecodeString(extra)
		if err != nil {
			t.Fatal(err)
		}
		out := append([]byte{}, base[:at]...)
		out = append(out, e...)
		return append(out, base[at:]...)
	}
	mutant, err := hex.DecodeString("01010c0474726565020402000000031102041104041107060c4d06070e080401000000010c0161091103000707280403000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640002861b638ebaadbcc437ea08982cc662b9030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d81457532aaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000")
	if err != nil {
		t.Fatal(err)
	}
	// graph_tree's root: tree (ref 4) = 4, settings (ref 5) = 7, the node
	// table (ref 6) at 0x17; stream_parts' root: pair (ref 8) = [null, 4], the
	// node table (ref 9) at 0x2b
	tree, parts := seed("graph_tree"), seed("stream_parts")
	retyped := append([]byte{}, tree...)
	if retyped[0x14] != 0x05 {
		t.Fatalf("graph_tree carries %#x at 0x14, want settings' ref 5", retyped[0x14])
	}
	retyped[0x14] = 0x04
	groups := []struct {
		unit, root string
		cases      []struct {
			name string
			wire []byte
		}
	}{
		{"graphdemo", "Scene", []struct {
			name string
			wire []byte
		}{
			{"certify 41211929427 mutant: tree repeated naming a Settings node", mutant},
			{"tree repeated naming a Settings node", retyped},
			{"tree repeated as index 0", splice(tree, 0x17, "060c", "041100")},
			{"tree repeated past node_count + 1", splice(tree, 0x17, "060c", "041109")},
			{"tree repeated as index 1, the root", splice(tree, 0x17, "060c", "041101")},
			{"tree repeated under kind 12, a kind mismatch that keeps the first", splice(tree, 0x17, "060c", "040c00")},
		}},
		{"streamdemo", "Feed", []struct {
			name string
			wire []byte
		}{
			{"pair repeated whole, slot 2 null", splice(parts, 0x2b, "090c", "080e0411020200")},
			{"pair repeated short, slot 2 kept from the first", splice(parts, 0x2b, "090c", "080e03110202")},
		}},
	}
	lua := luarun.Interpreter(t)
	for _, g := range groups {
		var paths []string
		for _, p := range units[g.unit] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, paths...)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("unit %s: %v", g.unit, err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaFuzzProbe(ModuleName(u), g.root)), 0o644); err != nil {
			t.Fatal(err)
		}
		var mutants []luaFuzzMutant
		for _, c := range g.cases {
			mutants = append(mutants, luaFuzzMutant{caseName: c.name, wire: c.wire})
		}
		if err := os.WriteFile(filepath.Join(dir, "wires.bin"), luaFuzzWiresFile(mutants), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := luaFuzzRun(lua, dir, "wires.bin")
		if err != nil {
			t.Fatalf("unit %s: %v", g.unit, err)
		}
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(lines) != len(g.cases) {
			t.Fatalf("unit %s: the probe answered %d lines for %d wires", g.unit, len(lines), len(g.cases))
		}
		m := tabletext.NewModel(u)
		def := m.Lookup(g.root)
		for i, c := range g.cases {
			ans, err := parseLuaFuzzLine(lines[i])
			if err != nil {
				t.Fatal(err)
			}
			if v := luaFuzzVerdict(ans, luaFuzzEngine(m, def, c.wire)); v != "" {
				t.Errorf("%s %s: %s: %s\n    wire %x", g.unit, g.root, c.name, v, c.wire)
			}
		}
	}
}
