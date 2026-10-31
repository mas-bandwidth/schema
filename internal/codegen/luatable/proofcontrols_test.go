package luatable

// THE NEGATIVE CONTROLS the porting register's Lua cells cite (docs/PORTING.md
// M6, M12, M14, M19, M20, I7). Each takes one property's code out of the
// emitted runtime, in memory and matched exactly once, and requires the gate
// that holds the property to go red on the rows that carry it, while the same
// rows are green unsabotaged.

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// patchOnce returns a files patch that replaces anchor with sabotaged in the
// named emitted runtime file, failing t unless the anchor matches exactly once.
func patchOnce(t *testing.T, file, anchor, sabotaged string) func(map[string][]byte) {
	return func(files map[string][]byte) {
		src, ok := files[file]
		if !ok {
			t.Fatalf("the emitted unit has no %s", file)
		}
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("the negative-control anchor in %s is matched %d times, want exactly once", file, n)
		}
		files[file] = []byte(strings.Replace(string(src), anchor, sabotaged, 1))
	}
}

// saveRows runs TestLuaTableSaveRoundTripsEveryInstance's probe (load the
// wire, save it back, measure) over the named instance rows, with the emitted
// runtime patched when patch is set, and returns each row's verdict: "same"
// when the save is the wire byte for byte and the measure is its length.
func saveRows(t *testing.T, names []string, patch func(map[string][]byte)) map[string]string {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	rows := map[string]manifestRow{}
	for _, row := range manifestRows(t, manifestPath, "instance") {
		rows[row.caseName] = row
	}
	out := map[string]string{}
	for _, name := range names {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("the manifest has no instance row %s", name)
		}
		var full []string
		for _, p := range units[row.unit] {
			full = append(full, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, full...)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("unit %s: %v", row.unit, err)
		}
		if patch != nil {
			patch(files)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(saveProbe(ModuleName(u), row.root)), 0o644); err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		stdout, _, err := luarun.Try(t, dir, "main.lua", map[string][]byte{"wire": wire})
		part := strings.SplitN(strings.TrimRight(stdout, "\n"), ":", 3)
		switch {
		case err != nil:
			out[name] = "error"
		case len(part) != 3:
			out[name] = "bad output"
		case part[1] != "same":
			out[name] = "save != wire"
		case part[2] != part[0]:
			out[name] = "measure != saved length"
		default:
			out[name] = "same"
		}
	}
	return out
}

// requireSaveControl holds the unsabotaged runtime green and the sabotaged one
// red on every named row.
func requireSaveControl(t *testing.T, what string, names []string, patch func(map[string][]byte)) {
	t.Helper()
	for name, v := range saveRows(t, names, nil) {
		if v != "same" {
			t.Fatalf("%s: the unsabotaged save is not the wire (%s); the control proves nothing", name, v)
		}
	}
	for name, v := range saveRows(t, names, patch) {
		if v == "same" {
			t.Errorf("%s: with %s the save still matched the wire", name, what)
		} else {
			t.Logf("%s: %s with %s", name, v, what)
		}
	}
}

// TestLuaNodeIdentityNegativeControl (M6): the pointer walk's identity map
// (descriptor.lua D.number_nodes) gives a node already numbered a NEW index at
// every pointer that names it, so a shared node is written once per pointer;
// the instance round-trip must go red on every row that shares a node.
func TestLuaNodeIdentityNegativeControl(t *testing.T) {
	t.Parallel()
	requireSaveControl(t, "a shared node numbered at every visit", []string{"graph_shared", "list_shared"},
		patchOnce(t, "descriptor.lua", "        if index[val] == nil then\n", "        if true then\n"))
}

// TestLuaMapWriterSortNegativeControl (M19): the writer's sort
// (descriptor.lua D.map_entries) is turned to descending, so a map's entries
// ride in the reverse of key order; every multi-entry map row must go red.
func TestLuaMapWriterSortNegativeControl(t *testing.T) {
	t.Parallel()
	requireSaveControl(t, "the map writer's sort reversed", []string{"map_full", "map_pairs", "map_docs"},
		patchOnce(t, "descriptor.lua", "  local keyname = kf.name\n",
			"  for i = 1, #keys // 2 do keys[i], keys[#keys + 1 - i] = keys[#keys + 1 - i], keys[i] end\n  local keyname = kf.name\n"))
}

// TestLuaIdTableNegativeControl (M20): the id table's inverse lookup
// (save.lua id_ref) is dropped, so an id the body names twice takes a second
// slot instead of its first-use position; every row that names an id twice
// must go red.
func TestLuaIdTableNegativeControl(t *testing.T) {
	t.Parallel()
	requireSaveControl(t, "an id appended at every use", []string{"map_full", "graph_shared"},
		patchOnce(t, "save.lua", "  if ref ~= nil then return ref end\n  local list = ids.list\n", "  local list = ids.list\n"))
}

// TestLuaJsonNodeLabelNegativeControl (M14): the text writer (json.lua
// write_pointer) writes every node in full and never labels it `&node`, so a
// shared node is written once per pointer; TestLuaJsonAgreesWithTheCorpus's
// json-write comparison must go red on the rows that share a node.
func TestLuaJsonNodeLabelNegativeControl(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	patch := patchOnce(t, "json.lua",
		"  if count <= 1 then return write_table(T, desc, node, depth, g, w) end\n",
		"  if true then return write_table(T, desc, node, depth, g, w) end\n")
	want := map[string]bool{"graph_shared": true, "list_shared": true}
	seen := 0
	for _, row := range jsonManifestRows(t, manifestPath, "instance") {
		if !want[row.caseName] {
			continue
		}
		seen++
		var paths []string
		for _, p := range units[row.unit] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, paths...)
		text, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", row.caseName+".json"))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.path))
		if err != nil {
			t.Fatal(err)
		}
		inputs := map[string][]byte{"text": text, "wire": wire}
		probe := jsonInstanceProbe(ModuleName(u), row.root)
		good := strings.Split(jsonRun(t, paths, ModuleName(u), row.root, probe, inputs, nil), "\n")
		if len(good) != 3 || good[1] != hexBytes(text) {
			t.Fatalf("%s: the unsabotaged json-write is not the corpus text; the control proves nothing", row.caseName)
		}
		bad := strings.Split(jsonRun(t, paths, ModuleName(u), row.root, probe, inputs, func(src []byte) []byte {
			files := map[string][]byte{"json.lua": src}
			patch(files)
			return files["json.lua"]
		}), "\n")
		if len(bad) == 3 && bad[1] == hexBytes(text) {
			t.Errorf("%s: with every node written in full the json-write still matched the corpus", row.caseName)
		}
	}
	if seen != len(want) {
		t.Fatalf("the manifest carries %d of the %d shared-node json rows", seen, len(want))
	}
}

// TestLuaForeignOrderNegativeControl (I7): BlockOpen's byte-order word check
// (block.lua) is dropped, so a block whose magic is intact and whose order
// word says the other order would be read; TestLuaBlockRead's refusal probe
// must go red.
func TestLuaForeignOrderNegativeControl(t *testing.T) {
	t.Parallel()
	out, stderr, err := luaBlockRefusals(t, patchOnce(t, "block.lua",
		`  if string.unpack("=I8", bytes, 17) ~= NATIVE_ORDER then return nil, "not_a_cook" end`+"\n", ""))
	if err == nil && out == "block-refusals-ok\n" {
		t.Fatal("with the order-word check dropped the block refusal probe still passed")
	}
	if !strings.Contains(stderr, "not_a_cook") {
		t.Fatalf("the probe went red, but not on the order word's refusal: %q %s", out, stderr)
	}
}

// THE WIRE'S BYTE ORDER IS THE READER'S (docs/PORTING.md M12): every
// string.pack and string.unpack in the runtime files that read and write the
// table wires (variable, retained, message, fixed) names `<`, so the host's
// order is never consulted. The block and cook runtimes are excluded on
// purpose: they read the producer's own order and refuse the other by its
// order word (§19.1, I7). A format that is not a literal is refused too, since
// it cannot be checked.
var wireOrderFiles = []string{"load.lua", "save.lua", "message.lua", "fixed.lua", "retain.lua"}

var packCall = regexp.MustCompile(`string\.(?:un)?pack(?:size)?\(\s*([^,)]*)`)

func wireOrderViolations(files map[string][]byte) []string {
	var bad []string
	for _, name := range wireOrderFiles {
		for i, line := range bytes.Split(files[name], []byte("\n")) {
			if bytes.HasPrefix(bytes.TrimSpace(line), []byte("--")) {
				continue
			}
			for _, m := range packCall.FindAllSubmatch(line, -1) {
				f := strings.TrimSpace(string(m[1]))
				if !strings.HasPrefix(f, `"<`) && !strings.HasPrefix(f, `'<`) {
					bad = append(bad, name+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(string(line)))
				}
			}
		}
	}
	sort.Strings(bad)
	return bad
}

func wireOrderUnitFiles(t *testing.T) map[string][]byte {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	u := luarun.LoadUnit(t, filepath.Join(repo, "tables", "pointers"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range wireOrderFiles {
		if _, ok := files[name]; !ok {
			t.Fatalf("the emitted unit has no %s", name)
		}
	}
	return files
}

// TestLuaWireByteOrderIsExplicit is M12's gate.
func TestLuaWireByteOrderIsExplicit(t *testing.T) {
	t.Parallel()
	files := wireOrderUnitFiles(t)
	calls := 0
	for _, name := range wireOrderFiles {
		calls += len(packCall.FindAll(files[name], -1))
	}
	if calls == 0 {
		t.Fatal("no string.pack or string.unpack found in the wire runtimes; the scan is blind")
	}
	for _, v := range wireOrderViolations(files) {
		t.Errorf("a wire read or write does not name little-endian: %s", v)
	}
	t.Logf("%d pack/unpack calls in %d wire runtime files, every one little-endian", calls, len(wireOrderFiles))
}

// TestLuaWireByteOrderNegativeControl turns one of load.lua's little-endian
// reads to the host's order and requires M12's scan to name it.
func TestLuaWireByteOrderNegativeControl(t *testing.T) {
	t.Parallel()
	files := wireOrderUnitFiles(t)
	src := string(files["load.lua"])
	i := strings.Index(src, `string.unpack("<`)
	if i < 0 {
		t.Fatal("load.lua has no little-endian unpack to sabotage")
	}
	files["load.lua"] = []byte(src[:i] + `string.unpack("=` + src[i+len(`string.unpack("<`):])
	if len(wireOrderViolations(files)) == 0 {
		t.Fatal("a host-order read in load.lua passed the byte-order scan")
	}
}
