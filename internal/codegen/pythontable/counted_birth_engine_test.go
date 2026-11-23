package pythontable

// A COUNTED ARRAY [Min..Max]T IS ELIDED AT ITS BIRTH VALUE (docs/SPEC-TABLES.md
// section 3.3 and the section 22.2 [M..N]T row; the Go engine's encodeArray and
// messageElides): a plain counted field holding exactly Min elements, each at
// its default (a float by its bits, a table by a body that is its terminator
// alone), stays off the wire on the variable save and on the message form, and
// rides at any other count or with one live element. The probe's bytes are
// held to the engine's, case by case.

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const countedBirthEngineSchema = `package birth

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

var countedBirthFreshText = map[string]string{
	"FixI32": `{"few":[0,0]}`, "FixRec": `{"few":[{},{}]}`, "FixF64": `{"few":[0,0]}`,
}

type countedBirthEngineCase struct {
	name, fix, vari, text string
}

func countedBirthEngineCases() []countedBirthEngineCase {
	return []countedBirthEngineCase{
		{"fresh", "FixI32", "VarI32", ""},
		// an object that names no field is the birth value, whatever the form
		{"empty_json", "FixI32", "VarI32", "{}"},
		{"empty_json", "FixRec", "VarRec", "{}"},
		{"empty_json", "FixF64", "VarF64", "{}"},
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

func TestPythonCountedArrayBornAtItsMinimumEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	unit := loadUnit(t, countedBirthEngineSchema)
	files, err := Generate(unit)
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
	var p strings.Builder
	p.WriteString(pyrun.ImportPrelude + `from birth import *
def value(root, text):
    rep = LoadReport()
    v = root.from_json(text, rep)
    assert not (getattr(rep, "damaged", False) or getattr(rep, "refused", False)), text
    return v
`)
	want := map[string]string{}
	for _, c := range countedBirthEngineCases() {
		key := c.fix + "_" + c.name
		vkey := c.vari + "_" + c.name
		// a Python value built from JSON carries what the text names: a
		// fresh value is the birth count of default elements
		text := c.text
		if text == "" {
			text = countedBirthFreshText[c.fix]
		}
		if c.name == "empty_json" {
			text = "{}"
		}
		fmt.Fprintf(&p, "print(%q + \" msg \" + save_messages(%s, [value(%s, %q)]).hex())\n", key, c.fix, c.fix, text)
		fmt.Fprintf(&p, "print(%q + \" var \" + value(%s, %q).save().hex())\n", vkey, c.vari, text)
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
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{"main.py": []byte(p.String())}
	for name, run := range map[string]func(testing.TB, string, string, map[string][]byte) []byte{
		"current": pyrun.Run,
		"floor":   pyrun.RunFloor,
	} {
		got := map[string]string{}
		for line := range strings.SplitSeq(strings.TrimRight(string(run(t, dir, "main.py", inputs)), "\n"), "\n") {
			if f := strings.Fields(line); len(f) == 3 {
				got[f[0]+" "+f[1]] = f[2]
			}
		}
		for k, w := range want {
			if got[k] != w {
				t.Errorf("%s: %s: python wrote %q, the engine wrote %q", name, k, got[k], w)
			}
		}
	}
}
