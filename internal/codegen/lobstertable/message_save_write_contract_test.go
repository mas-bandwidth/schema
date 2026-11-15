package lobstertable_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITE CONTRACT ON THE MESSAGE FORM (docs/SPEC.md §5.1): Lobster's table
// writers refuse, in every build, a value its declaration does not admit, and
// save_messages is one of them: an off-contract value answers -1 and no
// bytes, never a value masked, wrapped or clamped into its field. A lawful
// value at the same boundary is written, and the unit's own load_messages
// reads it back as the reference's quantization gives it. Each case starts
// from a fresh record and sets one field.

const messageWriteContractSchema = `package mwcprobe

enum Grade { Bronze, Silver, Gold }

fixed table Probe
{
    q float32 = 0.0 | min = -1.0, max = 1.0, resolution = 0.01
    r int32 = 0 | min = 0, max = 10
    f float32
    s string(4)
    g Grade
    n [..3]int32
}
`

// mwcQuant is the compressed field's declaration as the reference's
// quantization takes it.
var mwcQuant = ir.TableMessageShape{Packing: 2, QMin: -1, QMax: 1, QRes: 0.01}

// mwcReadBack is the reference's answer for a lawful compressed value: the
// float its quantized index names, as float32 bits.
func mwcReadBack(v float32) string {
	return fmt.Sprintf("%d", math.Float32bits(ir.TableMessageDequantize(mwcQuant, ir.TableMessageQuantize(mwcQuant, v))))
}

// messageWriteContractCases are `<case> <expected>` lines: the case's program
// sets one field, and the save answers `refused`, or `wrote` and what the load
// reads back from the field it set.
var messageWriteContractCases = []struct{ name, set, read, want string }{
	{"q_above_max", `slot(rec, "q").cell.f = 1.5`, "", "refused"},
	{"q_below_min", `slot(rec, "q").cell.f = -1.5`, "", "refused"},
	{"q_nan", `slot(rec, "q").cell.f = schema_rt.f64frombits(0x7FF8000000000000)`, "", "refused"},
	{"q_inf", `slot(rec, "q").cell.f = schema_rt.f64frombits(0x7FF0000000000000)`, "", "refused"},
	{"q_min", `slot(rec, "q").cell.f = -1.0`, `"{schema_rt.f32bits(slot(back, "q").cell.f)}"`, "wrote " + mwcReadBack(-1)},
	{"q_max", `slot(rec, "q").cell.f = 1.0`, `"{schema_rt.f32bits(slot(back, "q").cell.f)}"`, "wrote " + mwcReadBack(1)},
	{"q_between_steps", `slot(rec, "q").cell.f = schema_rt.fround(0.333)`, `"{schema_rt.f32bits(slot(back, "q").cell.f)}"`, "wrote " + mwcReadBack(0.333)},
	{"r_above_max", `slot(rec, "r").cell.i = 11`, "", "refused"},
	{"r_below_min", `slot(rec, "r").cell.i = -1`, "", "refused"},
	{"r_far_above", `slot(rec, "r").cell.i = 100`, "", "refused"},
	{"r_min", `slot(rec, "r").cell.i = 0`, `"{slot(back, "r").cell.i}"`, "wrote 0"},
	{"r_max", `slot(rec, "r").cell.i = 10`, `"{slot(back, "r").cell.i}"`, "wrote 10"},
	{"f_inf", `slot(rec, "f").cell.f = schema_rt.f64frombits(0x7FF0000000000000)`, "", "refused"},
	{"f_nan", `slot(rec, "f").cell.f = schema_rt.f64frombits(0x7FF8000000000000)`, "", "refused"},
	{"f_finite", `slot(rec, "f").cell.f = 1.5`, `"{slot(back, "f").cell.f}"`, "wrote 1.5"},
	{"s_illformed", `slot(rec, "s").cell.s = "a\xffb"`, "", "refused"},
	{"s_zero", `slot(rec, "s").cell.s = "a\x00b"`, "", "refused"},
	{"s_long", `slot(rec, "s").cell.s = "abcde"`, "", "refused"},
	{"s_full", `slot(rec, "s").cell.s = "abcd"`, `slot(back, "s").cell.s`, "wrote abcd"},
	{"g_past_set", `slot(rec, "g").cell.i = 4`, "", "refused"},
	{"g_last", `slot(rec, "g").cell.i = 3`, `"{slot(back, "g").cell.i}"`, "wrote 3"},
	{"n_past_bound", "let n = slot(rec, \"n\")\n    while length(n.elems) < 4: push(n.elems, schema_rt.Value { i: 7 })\n    n.count = 4", "", "refused"},
	{"n_full", "let n = slot(rec, \"n\")\n    while length(n.elems) < 3: push(n.elems, schema_rt.Value { i: 7 })\n    n.count = 3", `"{slot(back, "n").count}"`, "wrote 3"},
}

func messageWriteContractProgram(g *loadUnit) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .message\n")
	b.WriteString(`def slot(rec:schema_rt.Record, name:string) -> schema_rt.Slot:
    return schema_rt.slot_named(rec, name)
let vocab = schema_rt.new_vocabulary(schema_rt.message_entries_here())
assert schema_rt.announce_read(vocab, schema_rt.announce(), schema_rt.LoadReport {})
// save answers the save's verdict; a written batch is read back by the
// unit's own load, and its one record is returned
def save(name:string, rec:schema_rt.Record) -> schema_rt.Record?:
    let out, n = schema_rt.save_messages([rec], schema_rt.LoadReport {})
    if n < 0:
        assert out == ""
        print(name + " refused")
        return nil
    let back, count, ok = schema_rt.load_messages("Probe", vocab, out, schema_rt.LoadReport {}, 1)
    if not ok or count != 1:
        print(name + " unreadable")
        return nil
    return back[0]
`)
	for _, c := range messageWriteContractCases {
		fmt.Fprintf(&b, "def case_%s():\n    let rec = schema_rt.new_record(schema_rt.table_of(\"Probe\"))\n    %s\n", c.name, c.set)
		fmt.Fprintf(&b, "    let back = save(%q, rec)\n    if back:\n", c.name)
		if c.read == "" {
			fmt.Fprintf(&b, "        print(%q)\n", c.name+" wrote")
		} else {
			fmt.Fprintf(&b, "        print(%q + %s)\n", c.name+" wrote ", c.read)
		}
		fmt.Fprintf(&b, "case_%s()\n", c.name)
	}
	return b.String()
}

func TestLobsterSaveMessagesHoldsTheWriteContract(t *testing.T) {
	t.Parallel()
	g, err := generateLoadUnit(t, []string{writeSchema(t, "Probe", messageWriteContractSchema)})
	if err != nil {
		t.Fatalf("the surface refuses the message write-contract probe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(messageWriteContractProgram(g)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)), "\n"), "\n")
	var want []string
	for _, c := range messageWriteContractCases {
		want = append(want, c.name+" "+c.want)
	}
	if !slices.Equal(out, want) {
		t.Fatalf("the message save's write contract:\n got %q\nwant %q", out, want)
	}
}
