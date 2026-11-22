package goosetable_test

// A COUNTED ARRAY [Min..Max]T IS ELIDED AT ITS BIRTH VALUE (docs/SPEC-TABLES.md
// §3.3 and the §22.2 [M..N]T row; the Go engine's encodeArray and
// messageElides): a plain counted field holding exactly Min elements, each at
// its default (a float by its bits, a table by a body that is its terminator
// alone), stays off the wire on the variable save, on the message form and in
// the JSON text, and rides at any other count or with one live element. The
// Goose writers' bytes are held to the engine's, case by case.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
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
		{"min_default", "FixI32", "VarI32", `{"few":[0,0]}`, "v.Few[0] = 0; v.Few[1] = 0;"},
		{"min_one", "FixI32", "VarI32", `{"few":[0,7]}`, "v.Few[1] = 7;"},
		{"min_plus1_default", "FixI32", "VarI32", `{"few":[0,0,0]}`, "v.FewCount = 3;"},
		{"fresh", "FixRec", "VarRec", "", ""},
		{"min_default", "FixRec", "VarRec", `{"few":[{},{}]}`, "v.Few[0].N = 55; v.Few[1].N = 55;"},
		{"min_one", "FixRec", "VarRec", `{"few":[{},{"n":56}]}`, "v.Few[1].N = 56;"},
		{"min_plus1_default", "FixRec", "VarRec", `{"few":[{},{},{}]}`, "v.FewCount = 3;"},
		{"fresh", "FixF64", "VarF64", "", ""},
		{"min_default", "FixF64", "VarF64", `{"few":[0,0]}`, "v.Few[0] = 0.0; v.Few[1] = 0.0;"},
		{"min_negzero", "FixF64", "VarF64", `{"few":[0,-0.0]}`, "v.Few[1] = schema_rt::fixed_bits_f64(0x8000000000000000);"},
	}
}

// countedBirthWriters is the probe's writers for one root, by its two names.
const countedBirthWriters = `
fn %[1]s_save(v: const birth::%[1]s&) -> u8[>..] {
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var out: u8[>..<] = [];
    let at = birth::%[1]sToLoaded(v, conformance_unit(), data, vals);
    if at < 0 || schema_rt::save_table(conformance_unit(), "%[1]s", data, vals, nodes, at, out) != 0 { return []; }
    var r: u8[>..] = [];
    for b in out { r.push(b); }
    return r;
}

fn %[1]s_json(v: const birth::%[1]s&) -> u8[>..] {
    var out: u8[>..] = [];
    if birth::%[1]sToJson(v, out) < 0 { return []; }
    return out;
}

fn %[2]s_message(v: const birth::%[2]s&) -> u8[>..] {
    var values: birth::%[2]s[1] = [v];
    var report = schema_rt::MsgReport { .. };
    var out: u8[>..] = [];
    if birth::%[2]sSaveMessages(values[0..1], out, report) < 0 { return []; }
    return out;
}
`

func TestGooseCountedArrayBornAtItsMinimum(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the generated unit)")
	t.Parallel()
	unit := probeUnit(t, countedBirthSchema)
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
	var main strings.Builder
	main.WriteString(probePrelude)
	for _, c := range []string{"I32", "Rec", "F64"} {
		fmt.Fprintf(&main, countedBirthWriters, "Var"+c, "Fix"+c)
	}
	main.WriteString("\nfn main() {\n")
	want := map[string]string{}
	for _, c := range countedBirthCases() {
		key := c.fix + "_" + c.name
		vkey := c.vari + "_" + c.name
		fmt.Fprintf(&main, "    { var v = birth::New%s(); %s print(%q, \" msg \", hex(%s_message(v))); }\n", c.fix, c.place, key, c.fix)
		fmt.Fprintf(&main, "    { var v = birth::New%s(); %s print(%q, \" var \", hex(%s_save(v))); print(%q, \" json \", hex(%s_json(v))); }\n", c.vari, c.place, vkey, c.vari, vkey, c.vari)
		msg, err := tablewire.EncodeMessages(model, []*tabletext.Instance{place(c.fix, c.text)})
		if err != nil {
			t.Fatal(err)
		}
		variable, err := tablewire.EncodeVariable(model, place(c.vari, c.text))
		if err != nil {
			t.Fatal(err)
		}
		text, err := model.Write(place(c.vari, c.text))
		if err != nil {
			t.Fatal(err)
		}
		want[key+" msg"] = hex.EncodeToString(msg)
		want[vkey+" var"] = hex.EncodeToString(variable)
		want[vkey+" json"] = hex.EncodeToString(bytes.TrimRight(text, "\n"))
	}
	main.WriteString("}\n")
	files := probeFiles(t, unit, nil)
	files["main.goose"] = []byte(main.String())
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := string(gooserun.Run(t, dir, "main.goose", nil))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 {
			// the JSON writer ends its text with a newline; the text's
			// content is what the engine's is held to
			got[f[0]+" "+f[1]] = f[2]
			if f[1] == "json" {
				got[f[0]+" "+f[1]] = strings.TrimSuffix(f[2], "0a")
			}
		}
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: goose wrote %q, the engine wrote %q", k, got[k], w)
		}
	}
}
