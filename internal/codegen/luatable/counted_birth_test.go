package luatable

// A COUNTED ARRAY [Min..Max]T IS ELIDED AT ITS BIRTH VALUE (docs/SPEC-TABLES.md
// §3.3 and the §22.2 [M..N]T row; the Go engine's encodeArray and
// messageElides): a plain counted field holding exactly Min elements, each at
// its default (a float by its bits, a table by a body that is its terminator
// alone), stays off the wire on the variable save and on the message form, and
// rides at any other count or with one live element. The probe's bytes are
// held to the engine's, case by case.

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const countedBirthSchema = `package birth

type Rec
{
    n int32 = 55
    on bool
}

fixed table FixI32 { few [2..4]int32 }
fixed table FixRec { few [2..4]Rec }
fixed table FixF64 { few [2..4]float64 }
table VarI32 { few [2..4]int32 }
table VarRec { few [2..4]Rec }
table VarF64 { few [2..4]float64 }
`

type countedBirthCase struct {
	name, fix, vari, text, place string
}

func countedBirthCases() []countedBirthCase {
	return []countedBirthCase{
		{"fresh", "FixI32", "VarI32", "", ""},
		{"min_default", "FixI32", "VarI32", `{"few":[0,0]}`, "v.few = {0, 0}"},
		{"min_one", "FixI32", "VarI32", `{"few":[0,7]}`, "v.few[2] = 7"},
		{"min_plus1_default", "FixI32", "VarI32", `{"few":[0,0,0]}`, "v.few = {0, 0, 0}"},
		{"fresh", "FixRec", "VarRec", "", ""},
		{"min_default", "FixRec", "VarRec", `{"few":[{},{}]}`, "v.few = {T.NewRec(), T.NewRec()}"},
		{"min_one", "FixRec", "VarRec", `{"few":[{},{"n":56}]}`, "v.few[2].n = 56"},
		{"min_plus1_default", "FixRec", "VarRec", `{"few":[{},{},{}]}`, "v.few = {T.NewRec(), T.NewRec(), T.NewRec()}"},
		{"fresh", "FixF64", "VarF64", "", ""},
		{"min_default", "FixF64", "VarF64", `{"few":[0,0]}`, "v.few = {0.0, 0.0}"},
		{"min_negzero", "FixF64", "VarF64", `{"few":[0,-0.0]}`, "v.few[2] = -0.0"},
	}
}

func TestLuaCountedArrayBornAtItsMinimum(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "birth.schema"), countedBirthSchema)
	unit := luarun.LoadUnit(t, dir)
	files, err := Generate(unit)
	if err != nil {
		t.Fatalf("generate: %v", err)
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
	var p strings.Builder
	p.WriteString(strings.ReplaceAll(`local T = require("__MOD__")
local S = require("save")
local function hex(b)
  return (b:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, T.Announcement, {}), "announce")
local function msg(key, root, place)
  local v = T["New" .. root]()
  place(v)
  local b = T.SaveMessages(root, vocab, { v })
  print(key .. " msg " .. (b == nil and "refused" or hex(b)))
end
local function var(key, root, place)
  local v = T["New" .. root]()
  place(v)
  local ok, b = pcall(S.Save, T, root, v)
  print(key .. " var " .. ((not ok or b == nil) and "refused" or hex(b)))
end
`, "__MOD__", ModuleName(unit)))
	want := map[string]string{}
	for _, c := range countedBirthCases() {
		key := c.fix + "_" + c.name
		vkey := c.vari + "_" + c.name
		fmt.Fprintf(&p, "msg(%q, %q, function(v) %s end)\n", key, c.fix, c.place)
		fmt.Fprintf(&p, "var(%q, %q, function(v) %s end)\n", vkey, c.vari, c.place)
		msgBytes, err := tablewire.EncodeMessages(model, []*tabletext.Instance{place(c.fix, c.text)})
		if err != nil {
			t.Fatal(err)
		}
		variable, err := tablewire.EncodeVariable(model, place(c.vari, c.text))
		if err != nil {
			t.Fatal(err)
		}
		want[key+" msg"] = hex.EncodeToString(msgBytes)
		want[vkey+" var"] = hex.EncodeToString(variable)
	}
	out, stderr, err := try(t, files, p.String(), nil)
	if err != nil {
		t.Fatalf("probe: %v\nout=%s\nstderr=%s", err, out, stderr)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 {
			got[f[0]+" "+f[1]] = f[2]
		}
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: lua wrote %q, the engine wrote %q", k, got[k], w)
		}
	}
}
