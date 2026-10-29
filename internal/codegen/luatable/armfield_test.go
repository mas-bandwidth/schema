package luatable

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A UNION ARM IS A FIELD LINE (docs/PORTING.md M18; docs/SPEC-TABLES.md §2.6,
// §3). An arm's payload is exactly the bytes a field puts after its own
// framing prefix, so the arm is read by the field's own code with the arm's L
// standing in for the field's length: load.lua's read_payload, which read_field
// and read_arm both call. Three things hold it.
//
//   - Structure: read_field and read_arm name no per-shape reader. A second
//     reader for arms beside the field's is the shape the method refuses, and it
//     is refused here by reading the source.
//   - Behaviour against the engine: arms of tables/arms' Carry union,
//     framed hostilely (a scalar arm whose L is not its width, a body arm with
//     slack after its terminator or with none), are read by the Lua load and by
//     internal/tablewire, and the report and the re-saved bytes must agree.
//   - The reversed witnesses: each of the framing checks removed in turn turns
//     exactly its own rows, and a per-shape string branch planted into read_arm
//     turns the structural scan.

// armShapeReaders are the calls a per-shape reader makes: if read_field or
// read_arm named one, the payload would be read there and not in read_payload.
var armShapeReaders = []string{"scalar_at(", "utf8_ok(", "wstring_ok(", "enum_read(", "read_array_body(", "read_body(", "r_bytes(", "string.sub("}

// luaFunctionBody answers the source of the top-level function named by its
// opening line, up to the next top-level `end`.
func luaFunctionBody(src, opening string) (string, bool) {
	i := strings.Index(src, opening)
	if i < 0 {
		return "", false
	}
	rest := src[i:]
	j := strings.Index(rest, "\nend\n")
	if j < 0 {
		return "", false
	}
	return rest[:j+5], true
}

// armFieldViolations answers what is wrong with a load.lua: read_field and
// read_arm must read the payload through read_payload and nothing per-shape.
func armFieldViolations(src string) []string {
	var out []string
	for _, fn := range []struct{ name, opening string }{
		{"read_field", "read_field = function(T, ctx, fd, r, v, first)"},
		{"read_arm_payload", "read_arm_payload = function(T, ctx, u, arm, armkind, sub, n)"},
	} {
		body, ok := luaFunctionBody(src, fn.opening)
		if !ok {
			out = append(out, fn.name+" is not where the gate looks for it")
			continue
		}
		if !strings.Contains(body, "read_payload(") {
			out = append(out, fn.name+" does not read through read_payload")
		}
		for _, call := range armShapeReaders {
			if strings.Contains(body, call) {
				out = append(out, fn.name+" reads a payload itself with "+call)
			}
		}
	}
	return out
}

func TestLuaArmIsAFieldLine(t *testing.T) {
	t.Parallel()
	src, err := runtime.ReadFile("runtime/load.lua")
	if err != nil {
		t.Fatal(err)
	}
	if v := armFieldViolations(string(src)); len(v) > 0 {
		t.Errorf("an arm is not a field line: %s", strings.Join(v, "; "))
	}
	// the reversed witness: a per-shape string reader planted into read_arm
	planted := strings.Replace(string(src), "  local out = { tag = arm.tag }\n  local x, st = read_payload(T, ctx, f, sub, n, out, arm.slot, armkind)\n",
		"  if f.shape == \"string\" then\n    local raw = string.sub(sub.b, sub.at + 1, sub.lim)\n    return { tag = arm.tag, [arm.slot] = raw }\n  end\n  local out = { tag = arm.tag }\n  local x, st = read_payload(T, ctx, f, sub, n, out, arm.slot, armkind)\n", 1)
	if planted == string(src) {
		t.Fatal("the plant's anchor is absent")
	}
	if v := armFieldViolations(planted); len(v) == 0 {
		t.Error("the structural scan stayed green with a per-shape string reader planted into read_arm")
	}
}

// armWire builds a Holder (tables/arms/Carry.schema: a table whose one field is
// the union Carry, whose arms are `leaf Leaf`, a table, and `plain int32`, a
// scalar) with the union selecting the arm named, framed with the L and
// payload given: the form byte, the carry field holding the arm header in
// place, the terminator, then the id table (the field's id, the arm's) and its
// count.
func armWire(ids [2]uint64, kind byte, l int, payload []byte) []byte {
	var b []byte
	b = append(b, 1)
	b = append(b, 1, 0x0f)          // carry: ref 1, kind 15 (a union: the arm header follows in place)
	b = append(b, 2, kind, byte(l)) // the arm: ref 2, its kind, L
	b = append(b, payload...)
	b = append(b, 0) // the root's terminator
	for _, id := range ids {
		b = binary.LittleEndian.AppendUint64(b, id)
	}
	return binary.LittleEndian.AppendUint64(b, 2)
}

type armCase struct {
	name string
	wire func(plain, leaf [2]uint64) []byte
}

var armCases = []armCase{
	{"plain arm at its width", func(p, _ [2]uint64) []byte { return armWire(p, 4, 4, []byte{7, 0, 0, 0}) }},
	{"plain arm one short", func(p, _ [2]uint64) []byte { return armWire(p, 4, 3, []byte{7, 0, 0}) }},
	{"plain arm one long", func(p, _ [2]uint64) []byte { return armWire(p, 4, 5, []byte{7, 0, 0, 0, 9}) }},
	{"plain arm empty", func(p, _ [2]uint64) []byte { return armWire(p, 4, 0, nil) }},
	{"leaf arm ends at its terminator", func(_, l [2]uint64) []byte { return armWire(l, 13, 1, []byte{0}) }},
	{"leaf arm with slack after its terminator", func(_, l [2]uint64) []byte { return armWire(l, 13, 2, []byte{0, 0x55}) }},
	{"leaf arm with no terminator", func(_, l [2]uint64) []byte { return armWire(l, 13, 0, nil) }},
}

// armIDsProbe prints the two wire ids a crafted Holder names for each arm: the
// carry field's, and the arm's.
const armIDsProbe = `local T = require("ArmdemoTables")
local holder = T.Tables["Holder"]
local carry = T.Unions["Carry"]
local function arm(name) for _, a in ipairs(carry.arms) do if a.name == name then return a.id end end end
local function field(name) for _, f in ipairs(holder.fields) do if f.name == name then return f.id end end end
print(string.format("%016x %016x", field("carry"), arm("plain")))
print(string.format("%016x %016x", field("carry"), arm("leaf")))
`

func armTableIDs(t *testing.T, b builtUnit) (plain, leaf [2]uint64) {
	t.Helper()
	out, stderr, err := try(t, b.files, armIDsProbe, nil)
	if err != nil {
		t.Fatalf("ids probe: %v\n%s", err, stderr)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("ids probe printed %q", out)
	}
	for i, ids := range []*[2]uint64{&plain, &leaf} {
		if _, err := fmt.Sscanf(lines[i], "%x %x", &ids[0], &ids[1]); err != nil {
			t.Fatalf("ids line %q: %v", lines[i], err)
		}
	}
	return plain, leaf
}

// armUnit builds tables/arms, the unit these rows are crafted against.
func armUnit(t *testing.T) (builtUnit, *ir.Unit) {
	t.Helper()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "tables", "arms"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return builtUnit{key: "arms", module: ModuleName(u), files: files}, u
}

// armVerdicts runs the crafted wires through the Lua load (the unit's files,
// possibly sabotaged) and the engine, and answers the divergence of each case
// ("" for agreement).
func armVerdicts(t *testing.T, b builtUnit, u *ir.Unit, files map[string][]byte) map[string]string {
	t.Helper()
	plain, leaf := armTableIDs(t, b)
	m := tabletext.NewModel(u)
	def := m.Lookup("Holder")
	if def == nil {
		t.Fatal("no Holder table")
	}
	out := map[string]string{}
	for _, c := range armCases {
		wire := c.wire(plain, leaf)
		stdout, stderr, err := try(t, files, agProbe(b.module, "Holder"), map[string][]byte{"wire": wire})
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.name, err, stderr)
		}
		ans, err := agParseLine(strings.TrimRight(stdout, "\n"))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		out[c.name] = agVerdict(ans, agEngine(m, def, wire))
	}
	return out
}

func TestLuaArmFramingAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	b, u := armUnit(t)
	for name, v := range armVerdicts(t, b, u, b.files) {
		if v != "" {
			t.Errorf("%s: %s", name, v)
		}
	}
}

// TestLuaArmFramingNegativeControl removes each framing check of read_payload
// in turn: the scalar width check (an arm's L must be its kind's width) and
// the body's consumed-equals-L check (an arm whose payload is a body ends at
// its terminator or it is damage). Each must turn exactly its own rows.
func TestLuaArmFramingNegativeControl(t *testing.T) {
	t.Parallel()
	b, u := armUnit(t)
	cases := []struct {
		name string
		from string
		to   string
		red  []string
	}{
		{"the scalar width check removed", "if w == 0 or n ~= w then", "if false then",
			[]string{"plain arm one long"}},
		{"the body terminator check removed", "    if not clean then\n      report.malformed = true\n      discard(ctx, np0)\n      return renew(T, d, inst), DAMAGED",
			"    if false then\n      report.malformed = true\n      discard(ctx, np0)\n      return renew(T, d, inst), DAMAGED",
			[]string{"leaf arm with slack after its terminator"}},
	}
	for _, c := range cases {
		files := b.patched(t, "load.lua", c.from, c.to)
		got := armVerdicts(t, b, u, files)
		want := map[string]bool{}
		for _, r := range c.red {
			want[r] = true
		}
		for name, v := range got {
			if want[name] && v == "" {
				t.Errorf("%s: row %q stayed green", c.name, name)
			}
			if !want[name] && v != "" {
				t.Errorf("%s: row %q turned and is not this plant's: %s", c.name, name, v)
			}
		}
	}
}
