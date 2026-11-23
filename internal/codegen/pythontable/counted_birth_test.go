package pythontable

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// countedBirthSchema holds one counted array with a positive minimum per
// element kind (a scalar, a declared type whose field carries a default, and
// a 128-bit integer), on a fixed root (the message form) and on a variable
// root (the variable form).
const countedBirthSchema = `package birth

type Rec
{
    n int32 = 55
    on bool
}

fixed table FixI32 { few [2..4]int32 }
fixed table FixRec { few [2..4]Rec }
fixed table FixU128 { few [2..4]uint128 }
table VarI32 { few [2..4]int32 }
table VarRec { few [2..4]Rec }
table VarU128 { few [2..4]uint128 }
`

// countedBirthCase is one value of the field: the engine places it from its
// JSON text (fresh when empty) and the probe from Python statements on a
// fresh value v.
type countedBirthCase struct {
	name, kind, text, place string
}

func countedBirthCases() []countedBirthCase {
	return []countedBirthCase{
		{"fresh", "I32", "", ""},
		{"min_default", "I32", `{"few":[0,0]}`, "v.few = [0, 0]"},
		{"min_one", "I32", `{"few":[0,7]}`, "v.few[1] = 7"},
		{"min_plus1_default", "I32", `{"few":[0,0,0]}`, "v.few.append(0)"},
		{"fresh", "Rec", "", ""},
		{"min_default", "Rec", `{"few":[{},{}]}`, "v.few = [Rec(), Rec()]"},
		{"min_one", "Rec", `{"few":[{},{"n":56}]}`, "v.few[1].n = 56"},
		{"min_plus1_default", "Rec", `{"few":[{},{},{}]}`, "v.few.append(Rec())"},
		{"fresh", "U128", "", ""},
		{"min_default", "U128", `{"few":["0","0"]}`, "v.few = [0, 0]"},
		{"min_one", "U128", `{"few":["0","18446744073709551616"]}`, "v.few[1] = 1 << 64"},
		{"min_plus1_default", "U128", `{"few":["0","0","0"]}`, "v.few.append(0)"},
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

// TestPythonCountedArrayBornAtItsMinimum holds the counted-array birth value
// (docs/SPEC-TABLES.md §22.2, ir.Field.BornCount; F-115, F-094): a fresh
// `[2..4]T` holds two default elements, and every Python writer — the
// variable save, the retaining save, the save after a text read, and the
// message save — elides the field at exactly that value, as the engine does
// (`020000` for the one-field fixed root, the 10-byte variable file), and
// stores it at count 2 with one live element and at count 3 of defaults.
func TestPythonCountedArrayBornAtItsMinimum(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, countedBirthSchema)
	model := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString(pyrun.ImportPrelude)
	probe.WriteString(`import birth as m
from birth import Rec, FixI32, FixRec, FixU128, VarI32, VarRec, VarU128
for cls, zero in ((FixI32, 0), (VarI32, 0), (FixU128, 0), (VarU128, 0)):
    born = cls().few
    assert born == [zero, zero], (cls.__name__, born)
for cls in (FixRec, VarRec):
    born = cls().few
    assert len(born) == 2 and all(e.n == 55 and e.on is False for e in born), cls.__name__
    assert born[0] is not born[1], "the born elements alias"
def emit(name, kind, var, fix):
    rep = m.LoadReport()
    out = [var.save().hex(), m.save_messages(type(fix), [fix]).hex()]
    ret = m.TableRetain(bytearray(262144), bytearray(24 * 8192))
    again = type(var).load_retain(var.save(), ret, rep)
    srep = m.LoadReport()
    out.append(again.save_retain(ret, srep).hex())
    jrep = m.LoadReport()
    out.append(type(var).from_json(var.to_json(), jrep).save().hex())
    jrep = m.LoadReport()
    fixed = type(fix).from_json(fix.to_json(), jrep)
    out.append(m.save_messages(type(fix), [fixed]).hex())
    print(name, kind, *out)
`)
	cases := countedBirthCases()
	for _, c := range cases {
		place := c.place
		if place == "" {
			place = "pass"
		}
		fmt.Fprintf(&probe, "v = Var%s()\n%s\nvar = v\nv = Fix%s()\n%s\nemit(%q, %q, var, v)\n", c.kind, place, c.kind, place, c.name, c.kind)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, p := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, p.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe.String())})
		if err != nil {
			t.Fatalf("%s: %v\n%s", p.name, err, stderr)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != len(cases) {
			t.Fatalf("%s: %d lines for %d cases:\n%s", p.name, len(lines), len(cases), out)
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
			want := strings.Join([]string{c.name, c.kind, wv, wm, wv, wv, wm}, " ")
			if lines[i] != want {
				t.Errorf("%s %s %s:\n got  %s\n want %s (variable save, message save, retaining save, save after text, message save after text)", p.name, c.kind, c.name, lines[i], want)
			}
		}
	}
}
