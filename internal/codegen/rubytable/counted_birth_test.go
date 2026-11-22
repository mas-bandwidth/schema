package rubytable_test

// A COUNTED ARRAY [Min..Max]T IS ELIDED AT ITS BIRTH VALUE (docs/SPEC-TABLES.md
// §3.3 and the §22.2 [M..N]T row; the Go engine's encodeArray and
// messageElides): a plain counted field holding exactly Min elements, each at
// its default (a float by its bits, a table by a body that is its terminator
// alone), stays off the wire on the variable save and on the message form,
// and rides at any other count or with one live element. The Ruby writer's
// bytes are held to the engine's, case by case, under both pinned interpreters.

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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
		{"min_default", "FixI32", "VarI32", `{"few":[0,0]}`, "v.few = [0, 0]"},
		{"min_one", "FixI32", "VarI32", `{"few":[0,7]}`, "v.few = [0, 7]"},
		{"min_plus1_default", "FixI32", "VarI32", `{"few":[0,0,0]}`, "v.few = [0, 0, 0]"},
		{"fresh", "FixRec", "VarRec", "", ""},
		{"min_default", "FixRec", "VarRec", `{"few":[{},{}]}`, "v.few = [Birth::Rec.new, Birth::Rec.new]"},
		{"min_one", "FixRec", "VarRec", `{"few":[{},{"n":56}]}`, "v.few = [Birth::Rec.new, Birth::Rec.new]; v.few[1].n = 56"},
		{"min_plus1_default", "FixRec", "VarRec", `{"few":[{},{},{}]}`, "v.few = [Birth::Rec.new, Birth::Rec.new, Birth::Rec.new]"},
		{"fresh", "FixF64", "VarF64", "", ""},
		{"min_default", "FixF64", "VarF64", `{"few":[0,0]}`, "v.few = [0.0, 0.0]"},
		{"min_negzero", "FixF64", "VarF64", `{"few":[0,-0.0]}`, "v.few = [0.0, -0.0]"},
	}
}

func TestRubyCountedArrayBornAtItsMinimum(t *testing.T) {
	slowtest.Gate(t, "both pinned Ruby interpreters")
	t.Parallel()
	unit := fixedproof.ParseCheckUnit(t, countedBirthSchema)
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
	p.WriteString("require_relative 'birth'\n")
	p.WriteString("def run_var(key, cls)\n  v = cls.new\n  yield v\n  puts \"#{key} var #{v.save.unpack1('H*')}\"\nend\n")
	p.WriteString("def run_msg(key, cls)\n  v = cls.new\n  yield v\n  puts \"#{key} msg #{Birth.save_messages(cls, [v]).unpack1('H*')}\"\nend\n")
	want := map[string]string{}
	for _, c := range countedBirthCases() {
		key := c.fix + "_" + c.name
		vkey := c.vari + "_" + c.name
		fmt.Fprintf(&p, "run_msg(%q, Birth::%s) { |v| %s }\n", key, c.fix, c.place)
		fmt.Fprintf(&p, "run_var(%q, Birth::%s) { |v| %s }\n", vkey, c.vari, c.place)
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
	files, err := rubytable.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := rubyFixedRun(t, files, p.String(), nil)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 {
			got[f[0]+" "+f[1]] = f[2]
		}
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: ruby wrote %q, the engine wrote %q", k, got[k], w)
		}
	}
}
