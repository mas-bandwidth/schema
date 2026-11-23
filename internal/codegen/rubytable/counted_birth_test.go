package rubytable

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

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
// JSON text (fresh when empty) and the probe from Ruby statements on a fresh
// value v.
type countedBirthCase struct {
	name, kind, text, place string
}

func countedBirthCases() []countedBirthCase {
	return []countedBirthCase{
		{"fresh", "I32", "", ""},
		{"min_default", "I32", `{"few":[0,0]}`, "v.few = [0, 0]"},
		{"min_one", "I32", `{"few":[0,7]}`, "v.few[1] = 7"},
		{"min_plus1_default", "I32", `{"few":[0,0,0]}`, "v.few << 0"},
		{"fresh", "Rec", "", ""},
		{"min_default", "Rec", `{"few":[{},{}]}`, "v.few = [Birth::Rec.new, Birth::Rec.new]"},
		{"min_one", "Rec", `{"few":[{},{"n":56}]}`, "v.few[1].n = 56"},
		{"min_plus1_default", "Rec", `{"few":[{},{},{}]}`, "v.few << Birth::Rec.new"},
		{"fresh", "U128", "", ""},
		{"min_default", "U128", `{"few":["0","0"]}`, "v.few = [0, 0]"},
		{"min_one", "U128", `{"few":["0","18446744073709551616"]}`, "v.few[1] = 1 << 64"},
		{"min_plus1_default", "U128", `{"few":["0","0","0"]}`, "v.few << 0"},
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

// TestRubyCountedArrayBornAtItsMinimum holds the counted-array birth value
// (docs/SPEC-TABLES.md §22.2, ir.Field.BornCount; F-115, F-094): a fresh
// `[2..4]T` holds two default elements, and every Ruby writer — the variable
// save, the retaining save, the save after a text read, and the message save
// — elides the field at exactly that value, as the engine does (`020000` for
// the one-field fixed root, the 10-byte variable file where Ruby wrote 31),
// and stores it at count 2 with one live element and at count 3 of defaults.
func TestRubyCountedArrayBornAtItsMinimum(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, countedBirthSchema)
	model := tabletext.NewModel(u)
	var probe strings.Builder
	probe.WriteString(`# frozen_string_literal: true

require_relative 'birth'

[[Birth::FixI32, 0], [Birth::VarI32, 0], [Birth::FixU128, 0], [Birth::VarU128, 0]].each do |cls, zero|
  born = cls.new.few
  raise "#{cls}: born #{born.inspect}" unless born == [zero, zero]
end
[Birth::FixRec, Birth::VarRec].each do |cls|
  born = cls.new.few
  raise "#{cls}: born #{born.inspect}" unless born.length == 2 && born.all? { |e| e.n == 55 && e.on == false }
  raise "#{cls}: the born elements alias" if born[0].equal?(born[1])
end

def emit(name, kind, var, fix)
  out = [var.save.unpack1('H*'), Birth.save_messages(fix.class, [fix]).unpack1('H*')]
  ret = Birth::TableRetain.new
  again = var.class.load_retain(var.save, ret, Birth::LoadReport.new)
  out << again.save_retain(ret, Birth::LoadReport.new).unpack1('H*')
  out << var.class.from_json(var.to_json, Birth::LoadReport.new).save.unpack1('H*')
  fixed = fix.class.from_json(fix.to_json, Birth::LoadReport.new)
  out << Birth.save_messages(fix.class, [fixed]).unpack1('H*')
  puts([name, kind, *out].join(' '))
end
`)
	cases := countedBirthCases()
	for _, c := range cases {
		fmt.Fprintf(&probe, "v = Birth::Var%s.new\n%s\nvar = v\nv = Birth::Fix%s.new\n%s\nemit(%q, %q, var, v)\n", c.kind, c.place, c.kind, c.place, c.name, c.kind)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	out := runBothRubyPins(t, dir, "main.rb", map[string][]byte{"main.rb": []byte(probe.String())})
	lines := strings.Split(strings.TrimSpace(out), "\n")
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
		want := strings.Join([]string{c.name, c.kind, wv, wm, wv, wv, wm}, " ")
		if lines[i] != want {
			t.Errorf("%s %s:\n got  %s\n want %s (variable save, message save, retaining save, save after text, message save after text)", c.kind, c.name, lines[i], want)
		}
	}
}
