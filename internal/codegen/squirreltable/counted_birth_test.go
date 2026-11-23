package squirreltable_test

// A COUNTED ARRAY [Min..Max]T IS ELIDED AT ITS BIRTH VALUE (docs/SPEC-TABLES.md
// §3.3 and the §22.2 [M..N]T row; the Go engine's encodeArray and
// messageElides): a plain counted field holding exactly Min elements, each at
// its default (a float by its bits, a table by a body that is its terminator
// alone), stays off the wire on the variable Save and on the message form,
// and rides at any other count or with one live element. The probe's bytes
// are held to the engine's, case by case.

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
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
		{"min_default", "FixI32", "VarI32", `{"few":[0,0]}`, "v.Few = [0, 0];"},
		{"min_one", "FixI32", "VarI32", `{"few":[0,7]}`, "v.Few[1] = 7;"},
		{"min_plus1_default", "FixI32", "VarI32", `{"few":[0,0,0]}`, "v.Few.append(0);"},
		{"fresh", "FixRec", "VarRec", "", ""},
		{"min_default", "FixRec", "VarRec", `{"few":[{},{}]}`, "v.Few = [T.Rec(), T.Rec()];"},
		{"min_one", "FixRec", "VarRec", `{"few":[{},{"n":56}]}`, "v.Few[1].N = 56;"},
		{"min_plus1_default", "FixRec", "VarRec", `{"few":[{},{},{}]}`, "v.Few.append(T.Rec());"},
		{"fresh", "FixF64", "VarF64", "", ""},
		{"min_default", "FixF64", "VarF64", `{"few":[0,0]}`, "v.Few = [0.0, 0.0];"},
		{"min_negzero", "FixF64", "VarF64", `{"few":[0,-0.0]}`, "v.Few[1] = -0.0;"},
	}
}

func TestSquirrelCountedArrayBornAtItsMinimum(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	unit := unitFromSchema(t, countedBirthSchema)
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
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", unit)
	var p strings.Builder
	p.WriteString(strings.ReplaceAll(`local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("__MOD__");
local hex = function(b, n) {
  local s = "";
  for (local i = 0; i < n; i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local run = function(key, root, place) {
  local v = T[root]();
  place(v);
  local saved = T[root + "Save"](v);
  print(key + " var " + (saved == null ? "refused" : hex(saved, saved.len())) + "\n");
};
local runMsg = function(key, root, place) {
  local v = T[root]();
  place(v);
  local b = blob(256);
  local sr = {};
  local n = T[root + "SaveMessages"]([v], b, sr);
  print(key + " msg " + (n < 0 ? "refused" : hex(b, n)) + "\n");
};
`, "__MOD__", mod))
	want := map[string]string{}
	for _, c := range countedBirthCases() {
		key := c.fix + "_" + c.name
		vkey := c.vari + "_" + c.name
		fmt.Fprintf(&p, "runMsg(%q, %q, function(v) { %s });\n", key, c.fix, c.place)
		fmt.Fprintf(&p, "run(%q, %q, function(v) { %s });\n", vkey, c.vari, c.place)
		msg, err := tablewire.EncodeMessages(model, []*tabletext.Instance{place(c.fix, c.text)})
		if err != nil {
			t.Fatal(err)
		}
		variable, err := tablewire.EncodeVariable(model, place(c.vari, c.text))
		if err != nil {
			t.Fatal(err)
		}
		want[key+" msg"] = hex.EncodeToString(msg)
		want[vkey+" var"] = hex.EncodeToString(variable)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 {
			got[f[0]+" "+f[1]] = f[2]
		}
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: squirrel wrote %q, the engine wrote %q", k, got[k], w)
		}
	}
}
