package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// THE WRITE CONTRACT ON THE VARIABLE FORM (docs/SPEC.md §5.1): Lobster has no
// write assert for tables, so the variable-form save refuses, in every build,
// a value its declaration does not admit, as invalid_value, writing nothing;
// and the lawful value at the same boundary is written. Each case starts from
// a fresh record, sets one field and saves it. The conformance battery reaches
// only fixed roots and the message form, so this is the variable save's pin.

const writeContractSchema = `package wcprobe

enum Grade { Bronze, Silver, Gold }

table Buff { m int32 }

union Effect
{
    buff  Buff
    level int32 | min = 0, max = 5
}

table Leaf { v int32 | min = 0, max = 3 }

table Probe
{
    r     int32 = 1 | min = 0, max = 10
    b     bits(6)
    small int8
    n     [..4]int32
    s     string(8)
    words []string
    g     Grade
    e     Effect
    x     float32 = 0.0 | min = 0.0, max = 10.0, resolution = 0.5
    link  *Leaf
    note  *string
}
`

// writeContractCases are `<case> <expected>` lines: the case's program sets
// one field, and the save answers `wrote` or its refusal's name.
var writeContractCases = []struct{ name, set, want string }{
	{"clean", ``, "wrote"},
	{"range_high", `slot(rec, "r").cell.i = 11`, "invalid_value"},
	{"range_high_ok", `slot(rec, "r").cell.i = 10`, "wrote"},
	{"range_low", `slot(rec, "r").cell.i = -1`, "invalid_value"},
	{"range_low_ok", `slot(rec, "r").cell.i = 0`, "wrote"},
	{"bits_wide", `slot(rec, "b").cell.i = 64`, "invalid_value"},
	{"bits_wide_ok", `slot(rec, "b").cell.i = 63`, "wrote"},
	{"width", `slot(rec, "small").cell.i = 128`, "invalid_value"},
	{"width_ok", `slot(rec, "small").cell.i = -128`, "wrote"},
	{"count", "let n = slot(rec, \"n\")\n    n.count = 5\n    while length(n.elems) < 5: push(n.elems, schema_rt.Value {})", "invalid_value"},
	{"count_ok", "let n = slot(rec, \"n\")\n    n.count = 4", "wrote"},
	{"length", `slot(rec, "s").cell.s = "aaaaaaaaa"`, "invalid_value"},
	{"length_ok", `slot(rec, "s").cell.s = "aaaaaaaa"`, "wrote"},
	{"text_illformed", `slot(rec, "s").cell.s = "a\xffb"`, "invalid_value"},
	{"text_zero", `word(rec, "a\x00b")`, "invalid_value"},
	{"text_wellformed", `word(rec, "a\xc3\xa9b")`, "wrote"},
	{"enum", `slot(rec, "g").cell.i = 4`, "invalid_value"},
	{"enum_ok", `slot(rec, "g").cell.i = 3`, "wrote"},
	{"tag", `slot(rec, "e").cell.i = 3`, "invalid_value"},
	{"tag_ok", "slot(rec, \"e\").cell.i = 2\n    slot(rec, \"e\").cell.arm = level_arm()", "wrote"},
	{"arm_range", "slot(rec, \"e\").cell.i = 2\n    let arm = level_arm()\n    arm.cell.i = 6\n    slot(rec, \"e\").cell.arm = arm", "invalid_value"},
	{"float_range", `slot(rec, "x").cell.f = 10.5`, "invalid_value"},
	{"float_range_ok", `slot(rec, "x").cell.f = 10.0`, "wrote"},
	{"non_finite", `slot(rec, "x").cell.f = schema_rt.f64frombits(0x7FF0000000000000)`, "non_finite_float"},
	{"node_range", "let leaf = schema_rt.new_record(schema_rt.table_of(\"Leaf\"))\n    leaf.fields[0].cell.i = 4\n    slot(rec, \"link\").cell.node = leaf", "invalid_value"},
	{"node_range_ok", "let leaf = schema_rt.new_record(schema_rt.table_of(\"Leaf\"))\n    leaf.fields[0].cell.i = 3\n    slot(rec, \"link\").cell.node = leaf", "wrote"},
	{"blob_text", `slot(rec, "note").cell.blob = "a\xffb"`, "invalid_value"},
	{"blob_text_ok", `slot(rec, "note").cell.blob = "ab"`, "wrote"},
}

func writeContractProgram(g *loadUnit, skip map[string]bool) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\n")
	b.WriteString(`def slot(rec:schema_rt.Record, name:string) -> schema_rt.Slot:
    return schema_rt.slot_named(rec, name)
// level_arm is a fresh slot of Effect's ranged arm
def level_arm() -> schema_rt.Slot:
    let un = schema_rt.lookup_union("Effect")
    assert un
    return schema_rt.new_slot(un.arms[1])
// word makes the text list hold one element, the text given
def word(rec:schema_rt.Record, text:string):
    let words = slot(rec, "words")
    let e = schema_rt.element_zero(words.fd)
    let et = e.tab
    if et:
        et.fields[0].cell.s = text
    else:
        e.s = text
    push(words.elems, e)
    words.count = 1
def answer(name:string, rec:schema_rt.Record):
    let out, why = schema_rt.save_reasoned(rec)
    if why == "":
        assert length(out) > 0
        print(name + " wrote")
    else:
        assert length(out) == 0
        print(name + " " + why)
`)
	for _, c := range writeContractCases {
		if skip[c.name] {
			continue
		}
		fmt.Fprintf(&b, "def case_%s():\n    let rec = schema_rt.new_record(schema_rt.table_of(\"Probe\"))\n", c.name)
		if c.set != "" {
			fmt.Fprintf(&b, "    %s\n", c.set)
		}
		fmt.Fprintf(&b, "    answer(%q, rec)\ncase_%s()\n", c.name, c.name)
	}
	return b.String()
}

func runWriteContract(t *testing.T, patch func(string) string, skip map[string]bool) []string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(writeContractSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the write-contract probe: %v", err)
	}
	if patch != nil {
		path := filepath.Join(g.dir, "schema_save.lobster")
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(patch(string(text))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(writeContractProgram(g, skip)), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)), "\n"), "\n")
}

func TestLobsterTableSaveRefusesValuesOffTheirDeclaration(t *testing.T) {
	t.Parallel()
	out := runWriteContract(t, nil, nil)
	var want []string
	for _, c := range writeContractCases {
		want = append(want, c.name+" "+c.want)
	}
	if !slices.Equal(out, want) {
		t.Fatalf("the variable save's write contract:\n got %q\nwant %q", out, want)
	}
}

// TestLobsterTableSaveRefusesValuesOffTheirDeclarationNegativeControl takes
// the contract's walk out of the emitted save and requires the refusals to
// go: every invalid_value case then writes, so the test above holds the walk
// and not something beside it.
func TestLobsterTableSaveRefusesValuesOffTheirDeclarationNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "        if not wc_body(r):\n"
	// an out-of-set tag is an index fault in the unchecked encoder, so the
	// patched program leaves that one case out
	out := runWriteContract(t, func(src string) string {
		if n := strings.Count(src, anchor); n != 1 {
			t.Fatalf("the wc_body anchor is in the emitted runtime %d times, want 1", n)
		}
		return strings.Replace(src, anchor, "        if false and not wc_body(r):\n", 1)
	}, map[string]bool{"tag": true})
	for _, c := range writeContractCases {
		// a *string referent is checked beside the walk, not by it
		if c.want != "invalid_value" || c.name == "tag" || c.name == "blob_text" {
			continue
		}
		if !slices.Contains(out, c.name+" wrote") {
			t.Fatalf("the patched save still refuses %s: %q", c.name, out)
		}
	}
}
