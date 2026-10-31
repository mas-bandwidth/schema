package mootable_test

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A constructCase is one table construct the moo variable wire claims,
// proven against the Go reference engine: the Go engine reads the JSON text
// into an instance (tabletext) and writes its form-1 wire (tablewire.Encode);
// moo's strict read (table_load.moo ReadStrict) must accept that wire with no
// refusal, and moo's save (table_save.moo) must write it back byte for byte,
// its measure equal to its length.
type constructCase struct {
	name      string
	construct string // the compiler/carries.go key, or a §-named construct
	schema    string
	root      string
	json      string
}

var constructCases = []constructCase{
	{"defaults_at", "§4.2 table value defaults", "package probe\ntable D\n{\n    s string(8) = \"hi\"\n    b bytes(4) = \"ab\"\n    n int32 = 5 | min = 0, max = 9\n}\n",
		"D", `{"s":"hi","b":"YWI=","n":5}`},
	{"defaults_off", "§4.2 table value defaults", "package probe\ntable D\n{\n    s string(8) = \"hi\"\n    b bytes(4) = \"ab\"\n    n int32 = 5 | min = 0, max = 9\n}\n",
		"D", `{"s":"","b":"","n":0}`},
	{"timestamp", "timestamp", "package probe\ntable Event\n{\n    id uint32\n    opened_at timestamp\n    closed_at timestamp\n}\n",
		"Event", `{"id":7,"opened_at":"2026-10-28T12:34:56.789Z","closed_at":"1969-12-31T23:59:59Z"}`},
	{"forms", "forms", "package probe\ntable Pong | forms = \"wire\"\n{\n    id uint32\n    name string(16)\n}\n",
		"Pong", `{"id":3,"name":"pong"}`},
	{"integrity", "integrity", "package probe\ntable Ledger | integrity = \"crc32c\"\n{\n    id uint32\n    name string(32)\n}\n",
		"Ledger", `{"id":9,"name":"sealed"}`},
	{"text_lists", "text-list", "package probe\ntable Lists\n{\n    rules [..4]string(8)\n    first [..4]?string(8)\n    repos []string(8)\n    bases []?string(8)\n}\n",
		"Lists", `{"rules":["a","bc"],"first":["x",null,"z"],"repos":["r1","","r3"],"bases":[null,"b"]}`},
	{"text_total", "text-list-total", "package probe\ntable Bounded\n{\n    rules [..16]string(64) | total = 256\n    repos []?string(64) | total = 4096\n}\n",
		"Bounded", `{"rules":["one","two"],"repos":["x",null]}`},
	{"array_root", "array-root", "package probe\ntable Goal\n{\n    x int32\n}\ntable GoalList | json = \"array\"\n{\n    goals []Goal\n}\n",
		"GoalList", `[{"x":1},{"x":-2}]`},
	{"one_sided", "one-sided-range", "package probe\ntable OneSided\n{\n    a int64 | min = 0\n    b uint16 | max = 9\n}\n",
		"OneSided", `{"a":12345678901,"b":9}`},
	{"optional_variable", "optional-variable-closure", "package probe\ntable Inner\n{\n    text *string\n    n int32\n}\ntable Outer\n{\n    inner ?Inner\n    tail int32\n}\n",
		"Outer", `{"inner":{"text":"hi","n":4},"tail":5}`},
	{"optional_map", "optional-map", "package probe\ntable M\n{\n    id int32\n    tags ?map[string(8)]int32\n}\n",
		"M", `{"id":1,"tags":{}}`},
	{"wide_ints", "§3 wide kinds", "package probe\ntable W\n{\n    a int64\n    b uint64\n    c int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727\n    d uint128\n    e fixed(48, 16) | min = -100000, max = 100000\n    f ufixed(8, 8) | min = 0, max = 200\n    g fixed(60, 4) | min = -1000000000000, max = 1000000000000\n    h bits(40)\n}\n",
		"W", `{"a":-9007199254740993,"b":18446744073709551615,"c":-170141183460469231731687303715884105728,"d":340282366920938463463374607431768211455,"e":-12345.5,"f":3.25,"g":-1.5,"h":1099511627775}`},
	{"floats", "§3.3 finite floats", "package probe\ntable F\n{\n    a float32\n    b float64\n    c float32 = 1.5\n    d float32 | min = -1, max = 1, resolution = 0.01\n}\n",
		"F", `{"a":-0.0,"b":1e-310,"c":0,"d":0.5}`},
	{"wide_text", "§3 kind 33", "package probe\ntable T\n{\n    w wstring(8)\n    v wstring(4)\n}\n",
		"T", `{"w":"wïde😀","v":"ab"}`},
	{"blobs_pointers", "§2.4 §2.5 pointers and blobs", "package probe\ntable Node\n{\n    name *string\n    data *bytes\n    next *Node\n    kids []*Node\n}\n",
		"Node", `{"name":"root","data":"AQID","next":{"name":"n1"},"kids":[{"name":"k1"},null,{"name":"k2"}]}`},
	{"unions_maps_keyed", "§2.6 §2.8 §3.2", "package probe\nenum Slot { head, body, feet }\ntable Item\n{\n    w uint16\n}\nunion Pick\n{\n    item Item\n    count uint32\n    label string(8)\n    empty\n}\ntable Kit\n{\n    gear [Slot]Item\n    pick Pick\n    picks []Pick\n    byid map[uint32]Item\n    flags_ok bool\n}\n",
		"Kit", `{"gear":{"head":{"w":1},"feet":{"w":3}},"pick":{"label":"x"},"picks":[{"count":4},{"empty":null},{"item":{"w":2}}],"byid":{"9":{"w":9},"2":{"w":2}},"flags_ok":true}`},
}

// goOracleWire is the Go engine's form-1 wire of the JSON text at root.
func goOracleWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	inst := m.New(st)
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Malformed || rep.Clamped > 0 || rep.Unknown > 0 || rep.KindMismatch > 0 {
		t.Fatalf("the Go engine does not read the JSON cleanly: %+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write it: %v", err)
	}
	if st.Integrity == "crc32c" {
		// THE INTEGRITY TRAILER (docs/SPEC-TABLES.md §2.14): the CRC-32C of
		// every byte before it, little-endian, which the engine's Encode
		// leaves to the emitted writers; hash/crc32's Castagnoli table is the
		// independent sum
		wire = binary.LittleEndian.AppendUint32(wire, crc32.Checksum(wire, crc32.MakeTable(crc32.Castagnoli)))
	}
	return wire
}

func unitFromSource(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	return u
}

// strictRoundTrip reads wire at root through ReadStrict and saves it back,
// printing "ok" and the saved hex, or the refusal.
func strictRoundTrip(g *generatedUnit, root string) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal save = require(\"./table_save\")\nlocal St = require(\"./schema_tables\")\n" +
		g.requireAll() + fmt.Sprintf(`local report = load.newReport()
local v = load.ReadStrict(%q, inputs.wire, report)
if v == nil then print("REFUSED " .. tostring(report.reason) .. " " .. tostring(report.field)) return end
local d = St.lookup(%q)
local out, why = save.save(d, v)
local n = save.measure(d, v)
if out == nil then print("SAVENIL " .. (if why ~= nil then why.name .. " " .. tostring(why.field) else "")) return end
local parts = table.create(buffer.len(out))
for i = 0, buffer.len(out) - 1 do
	parts[i + 1] = string.format("%%02x", buffer.readu8(out, i))
end
print("ok " .. tostring(n) .. " " .. table.concat(parts))
`, root, root)
}

// TestMooTableConstructsMatchTheGoEngine is the construct gate of the
// variable wire: every construct compiler/target_moo.go registers as carried
// (compiler/carries.go keys), and the wide kinds, finite floats, wide text,
// pointers, blobs, unions, maps and keyed arrays, each round-tripped from a
// wire the Go reference engine wrote.
func TestMooTableConstructsMatchTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	for _, c := range constructCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			u := unitFromSource(t, c.schema)
			wire := goOracleWire(t, u, c.root, c.json)
			files, err := generate(u)
			if err != nil {
				t.Fatalf("moo refuses the %s construct: %v", c.construct, err)
			}
			g := writeUnit(t, u, files)
			out := g.run(t, strictRoundTrip(g, c.root), map[string][]byte{"wire": wire})
			want := fmt.Sprintf("ok %d %x", len(wire), wire)
			if out != want {
				t.Fatalf("%s: moo does not round-trip the Go engine's wire:\nmoo: %s\ngo:  %s", c.construct, out, want)
			}
			if a := g.analyze(t); a != "" {
				t.Fatalf("moo-analyze is not silent:\n%s", a)
			}
		})
	}
}

// TestMooTableConstructsNegativeControl plants a one-byte difference into the
// emitted save.moo's id table count and requires the construct gate's
// comparison to go red, so the gate can fail.
func TestMooTableConstructsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	c := constructCases[0]
	u := unitFromSource(t, c.schema)
	wire := goOracleWire(t, u, c.root, c.json)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "\tw_u32(w, #c.ids.list)\n\tw_u32(w, 0)\n"
	src := string(files["table_save.moo"])
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("the id-count anchor is not in table_save.moo exactly once")
	}
	files["table_save.moo"] = []byte(strings.Replace(src, anchor, "\tw_u32(w, #c.ids.list + 1)\n\tw_u32(w, 0)\n", 1))
	g := writeUnit(t, u, files)
	out := g.run(t, strictRoundTrip(g, c.root), map[string][]byte{"wire": wire})
	if out == fmt.Sprintf("ok %d %x", len(wire), wire) {
		t.Fatalf("the patched save still matches the Go wire: %s", out)
	}
}
