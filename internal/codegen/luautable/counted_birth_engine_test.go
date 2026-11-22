package luautable_test

// A COUNTED ARRAY [Min..Max]T IS ELIDED AT ITS BIRTH VALUE (docs/SPEC-TABLES.md
// §3.3 and the §22.2 [M..N]T row; the Go engine's encodeArray and
// messageElides): a plain counted field holding exactly Min elements, each at
// its default (a float by its bits, a table by a body that is its terminator
// alone), stays off the wire on the variable save and on the message form, and
// rides at any other count or with one live element. The probe builds each
// value from the case's JSON text and its bytes are held to the engine's, case
// by case.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// each element shape is its own unit, as the matrix's pinned units are: the
// message vocabulary is the unit's, and its widths follow the unit's roots
var countedBirthEngineSchemas = map[string]string{
	"I32": "package birth\n\nfixed table FixI32 { few [2..4]int32 }\ntable VarI32 { few [2..4]int32 }\n",
	"Rec": "package birth\n\ntype Rec\n{\n    n int32 = 55\n    on bool\n}\n\nfixed table FixRec { few [2..4]Rec }\ntable VarRec { few [2..4]Rec }\n",
	"F64": "package birth\n\nfixed table FixF64 { few [2..4]float64 }\ntable VarF64 { few [2..4]float64 }\n",
}

type countedBirthEngineCase struct {
	name, fix, vari, text string
}

func countedBirthEngineCases() []countedBirthEngineCase {
	return []countedBirthEngineCase{
		{"fresh", "FixI32", "VarI32", ""},
		{"min_default", "FixI32", "VarI32", `{"few":[0,0]}`},
		{"min_one", "FixI32", "VarI32", `{"few":[0,7]}`},
		{"min_plus1_default", "FixI32", "VarI32", `{"few":[0,0,0]}`},
		{"fresh", "FixRec", "VarRec", ""},
		{"min_default", "FixRec", "VarRec", `{"few":[{},{}]}`},
		{"min_one", "FixRec", "VarRec", `{"few":[{},{"n":56}]}`},
		{"min_plus1_default", "FixRec", "VarRec", `{"few":[{},{},{}]}`},
		{"fresh", "FixF64", "VarF64", ""},
		{"min_default", "FixF64", "VarF64", `{"few":[0,0]}`},
		{"min_negzero", "FixF64", "VarF64", `{"few":[0,-0.0]}`},
	}
}

func TestLuauCountedArrayBornAtItsMinimumEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	for shape, schema := range countedBirthEngineSchemas {
		t.Run(shape, func(t *testing.T) {
			t.Parallel()
			countedBirthEngineShape(t, shape, schema)
		})
	}
}

func countedBirthEngineShape(t *testing.T, shape, schema string) {
	g := generateFiles(t, map[string]string{"birth.schema": schema})
	srcDir := t.TempDir()
	schemaPath := filepath.Join(srcDir, "birth.schema")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	unit, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unit)
	place := func(root, text string) *tabletext.Instance {
		inst := model.New(model.Lookup(root))
		if text != "" {
			var rep tabletext.Report
			if !model.Read(inst, []byte(text), &rep) {
				t.Fatalf("%s: the engine refused %s: %+v", root, text, rep)
			}
		}
		return inst
	}
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	if len(mods) != 1 {
		t.Fatalf("want one table module, have %v", mods)
	}
	var p strings.Builder
	fmt.Fprintf(&p, `--!nonstrict
local T = require("./%s")
local S = require("./schema_save")
local M = require("./schema_message")
local J = require("./schema_json")
local function hex(b: buffer?): string
	if b == nil then return "refused" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local vocab = M.NewVocabulary()
assert(M.AnnounceRead(vocab, T.TableMessageAnnouncement, {}), "announce")
local function run(key, fixRoot, varRoot, text)
	local rep = {}
	local f = J.FromJson(fixRoot, buffer.fromstring(text), rep)
	local ok, msg = pcall(function() return f and M.SaveMessages(vocab, fixRoot, { f }, {}) end)
	if not ok then print(key .. " err " .. tostring(msg)); msg = nil end
	print(key .. " msg " .. hex(msg))
	local v = J.FromJson(varRoot, buffer.fromstring(text), {})
	local out = v and S.save(T[varRoot .. "Descriptor"], v)
	print(key .. " var " .. hex(out))
end
`, mods[0])
	want := map[string]string{}
	for _, c := range countedBirthEngineCases() {
		if !strings.HasSuffix(c.fix, shape) {
			continue
		}
		text := c.text
		if text == "" {
			text = "{}"
		}
		fmt.Fprintf(&p, "run(%q, %q, %q, %q)\n", c.fix+"_"+c.name, c.fix, c.vari, text)
		msgBytes, err := tablewire.EncodeMessages(model, []*tabletext.Instance{place(c.fix, c.text)})
		if err != nil {
			t.Fatal(err)
		}
		variable, err := tablewire.EncodeVariable(model, place(c.vari, c.text))
		if err != nil {
			t.Fatal(err)
		}
		want[c.fix+"_"+c.name+" msg"] = hex.EncodeToString(msgBytes)
		want[c.fix+"_"+c.name+" var"] = hex.EncodeToString(variable)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(p.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", nil))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 {
			got[f[0]+" "+f[1]] = f[2]
		}
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: luau wrote %q, the engine wrote %q", k, got[k], w)
		}
	}
}
