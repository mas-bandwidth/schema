package mootable_test

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// countedBirthSchema holds one counted array with a positive minimum per
// element kind (a scalar, a declared type whose field carries a default, and
// a 128-bit integer), on a fixed root (the message form) and on a variable
// root (the variable form): the schema of PR #4569's Python, Ruby, Lua and
// Luau tests, so every port is held to the same bytes.
const countedBirthSchema = `package birth

type Rec
{
    n int32 = 55
    on bool
}

fixed table FixI32 { fi [2..4]int32 }
fixed table FixRec { fr [2..4]Rec }
fixed table FixU128 { fu [2..4]uint128 }
table VarI32 { fi [2..4]int32 }
table VarRec { fr [2..4]Rec }
table VarU128 { fu [2..4]uint128 }
`

// TestMooCountedArrayBornAtItsMinimum holds the counted-array birth value
// (docs/SPEC-TABLES.md §22.2, ir.Field.BornCount; F-115): a fresh `[2..4]T`
// holds two default elements, and the moo variable save and message save
// elide the field at exactly that value, as the engine does (a table element
// by a body that is its terminator alone), and store it at count 2 with one
// live element and at count 3 of defaults. Each value is placed by the moo
// JSON read from the text the engine reads, and compared byte for byte.
func TestMooCountedArrayBornAtItsMinimum(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	type birthCase struct{ name, kind, field, text string }
	var cases []birthCase
	for _, k := range []struct{ kind, field, zero, live string }{
		{"I32", "fi", "0", "7"},
		{"Rec", "fr", "{}", `{"n":56}`},
		{"U128", "fu", `"0"`, `"18446744073709551616"`},
	} {
		cases = append(cases,
			birthCase{"fresh", k.kind, k.field, ""},
			birthCase{"min_default", k.kind, k.field, fmt.Sprintf(`{%q:[%s,%s]}`, k.field, k.zero, k.zero)},
			birthCase{"min_one", k.kind, k.field, fmt.Sprintf(`{%q:[%s,%s]}`, k.field, k.zero, k.live)},
			birthCase{"min_plus1_default", k.kind, k.field, fmt.Sprintf(`{%q:[%s,%s,%s]}`, k.field, k.zero, k.zero, k.zero)},
		)
	}

	u := unitFromSource(t, countedBirthSchema)
	model := tabletext.NewModel(u)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	inputs := map[string][]byte{}
	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal save = require(\"./table_save\")\nlocal MS = require(\"./table_message\")\nlocal St = require(\"./schema_tables\")\n")
	b.WriteString(fmt.Sprintf("local T: any = require(\"./%sTable\")\n", u.DeclFile["VarI32"]))
	b.WriteString(`local function hex(x: buffer?): string
	if x == nil then return "REFUSED" end
	local parts = table.create(buffer.len(x))
	for i = 0, buffer.len(x) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(x, i))
	end
	return table.concat(parts)
end
local function place(root: string, name: string): any
	local v = T["New" .. root]()
	local text = inputs[name]
	if text ~= nil then
		local r: any = {}
		assert(T[root .. "FromJson"](v, text, r), name .. ": FromJson refused")
	end
	return v
end
local function emit(name: string, kind: string)
	local var = save.save(St.lookup("Var" .. kind), place("Var" .. kind, name))
	local msg = MS.SaveMessages("Fix" .. kind, { place("Fix" .. kind, name) }, {})
	print(name .. " " .. hex(var) .. " " .. hex(msg))
end
`)
	for _, c := range cases {
		key := c.kind + "_" + c.name
		if c.text != "" {
			inputs[key] = []byte(c.text)
		}
		fmt.Fprintf(&b, "emit(%q, %q)\n", key, c.kind)
	}
	if len(inputs) == 0 {
		inputs["unused"] = []byte{}
	}
	lines := strings.Split(g.run(t, b.String(), inputs), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("%d lines for %d cases:\n%s", len(lines), len(cases), strings.Join(lines, "\n"))
	}
	for i, c := range cases {
		place := func(root string) *tabletext.Instance {
			inst := model.New(model.Lookup(root))
			if c.text != "" {
				var rep tabletext.Report
				if !model.Read(inst, []byte(c.text), &rep) {
					t.Fatalf("%s %s: the engine refused %s: %+v", c.kind, c.name, c.text, rep)
				}
			}
			return inst
		}
		variable, err := tablewire.EncodeVariable(model, place("Var"+c.kind))
		if err != nil {
			t.Fatal(err)
		}
		message, err := tablewire.EncodeMessages(model, []*tabletext.Instance{place("Fix" + c.kind)})
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("%s_%s %s %s", c.kind, c.name, hex.EncodeToString(variable), hex.EncodeToString(message))
		if lines[i] != want {
			t.Errorf("%s %s:\n got  %s\n want %s (variable save, message save)", c.kind, c.name, lines[i], want)
		}
	}
}
