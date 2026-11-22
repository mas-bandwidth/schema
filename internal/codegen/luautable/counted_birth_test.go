package luautable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// countedBirthSchema holds one counted array with a positive minimum per
// element kind (a scalar, a declared type whose field carries a default, and
// a 128-bit integer), on a fixed root (the message form) and on a variable
// root (the variable form). Each kind's field has its own name: the Luau
// message writer keys its vocabulary by field id alone, so two roots holding
// a same-named field of different types collide there (a defect outside this
// test's subject, reported apart).
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

// countedBirthCase is one value of the field: the engine places it from its
// JSON text (fresh when empty) and the probe from Luau statements on a fresh
// value v. Both spell the field `few` / `Few`; field() renames it per kind.
type countedBirthCase struct {
	name, kind, text, place string
}

// field is the case with the field spelled as its kind's root declares it.
func (c countedBirthCase) field() countedBirthCase {
	name := map[string]string{"I32": "fi", "Rec": "fr", "U128": "fu"}[c.kind]
	slot := strings.ToUpper(name[:1]) + name[1:]
	c.text = strings.ReplaceAll(c.text, `"few"`, `"`+name+`"`)
	c.place = strings.ReplaceAll(c.place, "v.Few", "v."+slot)
	return c
}

func countedBirthCases() []countedBirthCase {
	return []countedBirthCase{
		{"fresh", "I32", "", ""},
		{"min_default", "I32", `{"few":[0,0]}`, "v.Few[1] = 0; v.Few[2] = 0; v.FewCount = 2"},
		{"min_one", "I32", `{"few":[0,7]}`, "v.Few[2] = 7"},
		{"min_plus1_default", "I32", `{"few":[0,0,0]}`, "v.FewCount = 3"},
		{"fresh", "Rec", "", ""},
		{"min_default", "Rec", `{"few":[{},{}]}`, "v.Few[1] = P.NewRec(); v.Few[2] = P.NewRec(); v.FewCount = 2"},
		{"min_one", "Rec", `{"few":[{},{"n":56}]}`, "v.Few[2].N = 56"},
		{"min_plus1_default", "Rec", `{"few":[{},{},{}]}`, "v.FewCount = 3"},
		{"fresh", "U128", "", ""},
		{"min_default", "U128", `{"few":["0","0"]}`, "v.Few[1] = Rt.int128(0i, 0i); v.Few[2] = Rt.int128(0i, 0i); v.FewCount = 2"},
		{"min_one", "U128", `{"few":["0","18446744073709551616"]}`, "v.Few[2] = Rt.int128(0i, 1i)"},
		{"min_plus1_default", "U128", `{"few":["0","0","0"]}`, "v.FewCount = 3"},
	}
}

// countedBirthReference is the engine's bytes for one case: the variable
// file of Var<kind> and the message batch of Fix<kind> holding that value.
func countedBirthReference(t *testing.T, model *tabletext.Model, c countedBirthCase) (variable, message []byte) {
	t.Helper()
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
	message, err = tablewire.EncodeMessages(model, []*tabletext.Instance{place("Fix" + c.kind)})
	if err != nil {
		t.Fatal(err)
	}
	return variable, message
}

// TestLuauCountedArrayBornAtItsMinimum holds the counted-array birth value
// (docs/SPEC-TABLES.md §22.2, ir.Field.BornCount; F-115): a fresh `[2..4]T`
// holds two default elements, and every Luau writer — the variable save, the
// retaining save, the save after a text read, and the message save — elides
// the field at exactly that value, as the engine does (`020000` for the
// one-field fixed root, the 10-byte variable file; a table element by a body
// that is its terminator alone), and stores it at count 2 with one live
// element and at count 3 of defaults.
func TestLuauCountedArrayBornAtItsMinimum(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "Birth.schema")
	if err := os.WriteFile(src, []byte(countedBirthSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{src})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	model := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString(`local P = require("./Birth")
local T = require("./BirthTable")
local Rt = require("./schema_runtime")
local S = require("./schema_save")
local R = require("./schema_retain")
local M = require("./schema_message")
local J = require("./schema_json")
local voc = M.NewVocabulary()
assert(M.AnnounceRead(voc, T.TableMessageAnnouncement, {}), "own AnnounceRead")

local function hex(b)
	assert(b ~= nil, "a writer refused")
	local parts = {}
	for i = 0, buffer.len(b) - 1 do
		parts[#parts + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function text(x)
	assert(x ~= nil, "ToJson refused")
	if type(x) == "string" then return buffer.fromstring(x) end
	return x
end
for _, r in { { "I32", "Fi" }, { "U128", "Fu" }, { "Rec", "Fr" } } do
	for _, side in { "Fix", "Var" } do
		assert(T["New" .. side .. r[1]]()[r[2] .. "Count"] == 2, side .. r[1] .. " born count")
	end
end
for _, r in { "FixRec", "VarRec" } do
	local few = T["New" .. r]().Fr
	assert(few[1].N == 55 and few[2].N == 55 and few[1] ~= few[2], r .. " born elements")
end

local function emit(name, kind, vv, fv)
	local vari, fix = "Var" .. kind, "Fix" .. kind
	local desc = T[vari .. "Descriptor"]
	local wire = S.save(desc, vv)
	local out = { hex(wire) }
	local again, store = R.LoadRetain(vari, wire, {})
	assert(again ~= nil, "LoadRetain")
	out[#out + 1] = hex((R.SaveRetain(vari, again, store, {})))
	local back = J.FromJson(vari, text(J.ToJson(vari, vv, {})), {})
	assert(back ~= nil, "FromJson")
	out[#out + 1] = hex((S.save(desc, back)))
	out[#out + 1] = hex((M.SaveMessages(voc, fix, { fv }, {})))
	local fixed = J.FromJson(fix, text(J.ToJson(fix, fv, {})), {})
	assert(fixed ~= nil, "FromJson fixed")
	out[#out + 1] = hex((M.SaveMessages(voc, fix, { fixed }, {})))
	print(name .. " " .. kind .. " " .. table.concat(out, " "))
end
local v, var
`)
	cases := countedBirthCases()
	for i := range cases {
		cases[i] = cases[i].field()
	}
	for _, c := range cases {
		fmt.Fprintf(&probe, "v = T.NewVar%s()\n%s\nvar = v\nv = T.NewFix%s()\n%s\nemit(%q, %q, var, v)\n", c.kind, c.place, c.kind, c.place, c.name, c.kind)
	}
	g := &generatedUnit{files: files, loaded: true}
	out := runProbe(t, g, probe.String(), nil)
	lines := strings.Split(out, "\n")
	if len(lines) != len(cases) {
		t.Fatalf("%d lines for %d cases:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		variable, message := countedBirthReference(t, model, c)
		if c.kind == "I32" && (c.name == "fresh" || c.name == "min_default") {
			// the construct: the engine elides the birth value whole
			if hex.EncodeToString(message) != "020000" || len(variable) != 10 {
				t.Fatalf("construct: the engine writes %x / %d bytes for the birth value", message, len(variable))
			}
		}
		wv, wm := hex.EncodeToString(variable), hex.EncodeToString(message)
		want := strings.Join([]string{c.name, c.kind, wv, wv, wv, wm, wm}, " ")
		if lines[i] != want {
			t.Errorf("%s %s:\n got  %s\n want %s (variable save, retaining save, save after text, message save, message save after text)", c.kind, c.name, lines[i], want)
		}
	}
}
