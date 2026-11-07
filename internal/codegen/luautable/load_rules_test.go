package luautable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuauTableLoadKeyedMapAndClampRules pins three load rules that the Luau
// port now follows as docs/SPEC-TABLES.md writes them: a keyed array skips an
// unnamed key (§3.2), a map empties at a key-kind mismatch (§2.8), and a text
// or wide string clamps to a whole code point (or unit) prefix (§3, KIND 12 /
// KIND 33).
func TestLuauTableLoadKeyedMapAndClampRules(t *testing.T) {
	t.Parallel()

	leb := func(v uint64) []byte {
		var out []byte
		for v >= 0x80 {
			out = append(out, byte(v)|0x80)
			v >>= 7
		}
		return append(out, byte(v))
	}
	u64 := func(v uint64) []byte {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, v)
		return b
	}
	// wire assembles a form-1 file: form byte, root body, the id table.
	wire := func(root []byte, ids ...uint64) []byte {
		w := []byte{1}
		w = append(w, root...)
		for _, id := range ids {
			w = append(w, u64(id)...)
		}
		w = append(w, u64(uint64(len(ids)))...)
		return w
	}
	// run loads one wire under the generated unit with a custom main that
	// prints the value (as the subtest spells it) and the report line.
	run := func(t *testing.T, g *generatedUnit, main string, w []byte) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": w})))
	}
	report := "string.format(\"%d,%d,%d,%d,%d,%s\", report.unknown, report.kindMismatch, report.widened, report.clamped, report.duplicate, tostring(report.malformed))"

	const keyedSchema = "package p\nenum K { A, B, C }\ntable Box\n{\n\tarr [K]int32\n}\n"

	t.Run("keyed-unnamed", func(t *testing.T) {
		t.Parallel()
		g := generateFiles(t, map[string]string{"Box.schema": keyedSchema})
		field := ir.TableWireId("arr")
		aID, bID, cID := ir.TableWireId("A"), ir.TableWireId("B"), ir.TableWireId("C")
		xID := ir.TableWireId("Q")
		elem := func(ref uint64, v uint32) []byte {
			b := leb(ref)
			b = append(b, leb(4)...)
			var x [4]byte
			binary.LittleEndian.PutUint32(x[:], v)
			return append(b, x[:]...)
		}
		var body []byte
		body = append(body, 4)
		body = append(body, leb(3)...)
		body = append(body, elem(2, 10)...)
		body = append(body, elem(5, 20)...)
		body = append(body, elem(3, 30)...)
		root := leb(1)
		root = append(root, 16)
		root = append(root, leb(uint64(len(body)))...)
		root = append(root, body...)
		root = append(root, 0)
		w := wire(root, field, aID, bID, cID, xID)
		main := fmt.Sprintf(`local inputs = require("./inputs")
local load = require("./schema_load")
require("./BoxTable")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.loadTolerant("Box", inputs.wire, report)
local parts = {}
for _, x in ipairs(v.Arr) do parts[#parts + 1] = tostring(x) end
print(table.concat(parts, ",") .. "|" .. %s)`, report)
		want := "10,30,0|1,0,0,0,0,false"
		if got := run(t, g, main, w); got != want {
			t.Errorf("keyed-unnamed:\ngot:  %s\nwant: %s", got, want)
		}
	})

	t.Run("keyed-descriptor", func(t *testing.T) {
		t.Parallel()
		g := generateFiles(t, map[string]string{"Box.schema": keyedSchema})
		data, err := os.ReadFile(filepath.Join(g.dir, "BoxTable.luau"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "keyvs") {
			t.Error("the generated descriptor carries no keyvs")
		}
		for _, name := range []string{`"A"`, `"B"`, `"C"`} {
			if !strings.Contains(string(data), name) {
				t.Errorf("the generated descriptor carries no key variant %s", name)
			}
		}
	})

	t.Run("map-kind-mismatch", func(t *testing.T) {
		t.Parallel()
		g := generateFiles(t, map[string]string{"Box.schema": "package p\ntable Box\n{\n\tm map[uint8]uint8\n}\n"})
		field := ir.TableWireId("m")
		keyID, valueID := ir.TableWireId("key"), ir.TableWireId("value")
		entry1 := leb(2)
		entry1 = append(entry1, 6, 7)
		entry1 = append(entry1, leb(3)...)
		entry1 = append(entry1, 6, 9, 0)
		entry2 := leb(2)
		entry2 = append(entry2, 12)
		entry2 = append(entry2, leb(2)...)
		entry2 = append(entry2, 'a', 'b')
		entry2 = append(entry2, leb(3)...)
		entry2 = append(entry2, 6, 9, 0)
		var body []byte
		body = append(body, 13)
		body = append(body, leb(2)...)
		body = append(body, leb(uint64(len(entry1)))...)
		body = append(body, entry1...)
		body = append(body, leb(uint64(len(entry2)))...)
		body = append(body, entry2...)
		root := leb(1)
		root = append(root, 14)
		root = append(root, leb(uint64(len(body)))...)
		root = append(root, body...)
		root = append(root, 0)
		w := wire(root, field, keyID, valueID)
		main := fmt.Sprintf(`local inputs = require("./inputs")
local load = require("./schema_load")
require("./BoxTable")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.loadTolerant("Box", inputs.wire, report)
print(#v.M .. "|" .. %s)`, report)
		want := "0|0,1,0,0,0,false"
		if got := run(t, g, main, w); got != want {
			t.Errorf("map-kind-mismatch:\ngot:  %s\nwant: %s", got, want)
		}
	})

	t.Run("clamp-utf8", func(t *testing.T) {
		t.Parallel()
		g := generateFiles(t, map[string]string{"Box.schema": "package p\ntable Box\n{\n\ts string(4)\n}\n"})
		field := ir.TableWireId("s")
		payload := leb(5)
		payload = append(payload, 'a', 'b', 0xE2, 0x82, 0xAC)
		root := leb(1)
		root = append(root, 12)
		root = append(root, payload...)
		root = append(root, 0)
		w := wire(root, field)
		main := fmt.Sprintf(`local inputs = require("./inputs")
local load = require("./schema_load")
require("./BoxTable")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.loadTolerant("Box", inputs.wire, report)
-- a string(N) is a buffer and its used Length (docs/PORT-LUAU.md, THE TYPE MAPPING)
print(buffer.readstring(v.S, 0, v.SLength) .. "|" .. %s)`, report)
		want := "ab|0,0,0,1,0,false"
		if got := run(t, g, main, w); got != want {
			t.Errorf("clamp-utf8:\ngot:  %s\nwant: %s", got, want)
		}
	})

	t.Run("clamp-wide", func(t *testing.T) {
		t.Parallel()
		g := generateFiles(t, map[string]string{"Box.schema": "package p\ntable Box\n{\n\tw wstring(2)\n}\n"})
		field := ir.TableWireId("w")
		payload := leb(6)
		payload = append(payload, 'a', 0x00, 0x00, 0xD8, 0x00, 0xDC)
		root := leb(1)
		root = append(root, 33)
		root = append(root, payload...)
		root = append(root, 0)
		w := wire(root, field)
		main := fmt.Sprintf(`local inputs = require("./inputs")
local load = require("./schema_load")
require("./BoxTable")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.loadTolerant("Box", inputs.wire, report)
-- a wstring(N) is a buffer of UTF-16LE code units and its Length in units
-- (docs/PORT-LUAU.md, THE TYPE MAPPING): the kept units print as UTF-8
local units = {}
for i = 0, v.WLength - 1 do units[#units + 1] = utf8.char(buffer.readu16(v.W, i * 2)) end
print(table.concat(units) .. "|" .. %s)`, report)
		want := "a|0,0,0,1,0,false"
		if got := run(t, g, main, w); got != want {
			t.Errorf("clamp-wide:\ngot:  %s\nwant: %s", got, want)
		}
	})
}
