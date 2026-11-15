package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// THE VARIABLE SAVE HOLDS THE WRITE CONTRACT (docs/SPEC.md §5.1 rows 1-8 and
// 10; docs/rulings.json write-refusal): Lobster's table writers refuse in
// every build, so save_reasoned refuses each value its declaration does not
// admit as invalid_value and writes nothing, and each lawful value at the
// same boundary is written and reads back through the unit's own load as the
// value set. Every case runs at the root, inside a by-value table and inside
// a pointed table, so the check follows the save graph to every record it
// writes.

const variableSaveFields = `
    small int8
    big   int64 | min = -5, max = 5
    b     bits(6)
    n     [..4]int8
    s     string(8)
    y     bytes(4)
    note  *string
    g     Grade
    m     map[int8]int8
    e     Effect
    x     float32 = 0.0 | min = 0.0, max = 10.0, resolution = 0.5
`

const variableSaveSchema = `package vsprobe

enum Grade { Bronze, Silver, Gold }

table Buff { v int32 }

union Effect
{
    buff  Buff
    level int32 | min = 0, max = 5
}

table Inner
{` + variableSaveFields + `}

table Probe
{` + variableSaveFields + `
    inner Inner
    link  *Inner
}
`

// variableSaveCases set one field of the target record `t`; `read` is a
// Lobster string expression over the loaded target `t`, printed after a
// lawful value round-trips so the value read back is the value set.
var variableSaveCases = []struct{ name, set, read, want string }{
	{"clean", ``, `""`, "wrote "},
	{"int8_high", `slot(t, "small").cell.i = 128`, ``, "invalid_value"},
	{"int8_low", `slot(t, "small").cell.i = -129`, ``, "invalid_value"},
	{"int8_high_ok", `slot(t, "small").cell.i = 127`, `string(slot(t, "small").cell.i)`, "wrote 127"},
	{"int8_low_ok", `slot(t, "small").cell.i = -128`, `string(slot(t, "small").cell.i)`, "wrote -128"},
	{"int8_wide", `slot(t, "small").cell.i = 300`, ``, "invalid_value"},
	{"int64_high", `slot(t, "big").cell.i = 6`, ``, "invalid_value"},
	{"int64_low", `slot(t, "big").cell.i = -6`, ``, "invalid_value"},
	{"int64_high_ok", `slot(t, "big").cell.i = 5`, `string(slot(t, "big").cell.i)`, "wrote 5"},
	{"int64_low_ok", `slot(t, "big").cell.i = -5`, `string(slot(t, "big").cell.i)`, "wrote -5"},
	{"bits_wide", `slot(t, "b").cell.i = 64`, ``, "invalid_value"},
	{"bits_ok", `slot(t, "b").cell.i = 63`, `string(slot(t, "b").cell.i)`, "wrote 63"},
	{"count_past", "let n = slot(t, \"n\")\n    while length(n.elems) < 5: push(n.elems, schema_rt.Value {})\n    n.count = 5", ``, "invalid_value"},
	{"count_negative", `slot(t, "n").count = -1`, ``, "invalid_value"},
	{"count_ok", "let n = slot(t, \"n\")\n    while length(n.elems) < 4: push(n.elems, schema_rt.Value { i: 1 })\n    n.count = 4", `string(slot(t, "n").count)`, "wrote 4"},
	{"element_wide", `vs_elem(t, 128)`, ``, "invalid_value"},
	{"element_ok", `vs_elem(t, -128)`, `string(slot(t, "n").elems[0].i)`, "wrote -128"},
	{"string_long", `slot(t, "s").cell.s = "aaaaaaaaa"`, ``, "invalid_value"},
	{"string_ok", `slot(t, "s").cell.s = "aaaaaaaa"`, `slot(t, "s").cell.s`, "wrote aaaaaaaa"},
	{"string_illformed", `slot(t, "s").cell.s = "a\xffb"`, ``, "invalid_value"},
	{"string_zero", `slot(t, "s").cell.s = "a\x00b"`, ``, "invalid_value"},
	{"string_wellformed", `slot(t, "s").cell.s = "a\xc3\xa9b"`, `string(length(slot(t, "s").cell.s))`, "wrote 4"},
	{"bytes_long", `slot(t, "y").cell.s = "\x01\x02\x03\x04\x05"`, ``, "invalid_value"},
	{"bytes_ok", `slot(t, "y").cell.s = "\x01\x00\xff\x04"`, `string(length(slot(t, "y").cell.s))`, "wrote 4"},
	{"blob_illformed", `slot(t, "note").cell.blob = "a\xffb"`, ``, "invalid_value"},
	{"blob_zero", `slot(t, "note").cell.blob = "a\x00b"`, ``, "invalid_value"},
	{"blob_ok", `slot(t, "note").cell.blob = "ab"`, `vs_blob(t)`, "wrote ab"},
	{"enum_past", `slot(t, "g").cell.i = 4`, ``, "invalid_value"},
	{"enum_negative", `slot(t, "g").cell.i = -1`, ``, "invalid_value"},
	{"enum_ok", `slot(t, "g").cell.i = 3`, `string(slot(t, "g").cell.i)`, "wrote 3"},
	{"map_repeated_key", "vs_entry(t, 1, 2)\n    vs_entry(t, 1, 3)", ``, "invalid_value"},
	{"map_key_wide", `vs_entry(t, 128, 1)`, ``, "invalid_value"},
	{"map_value_wide", `vs_entry(t, 1, -129)`, ``, "invalid_value"},
	{"map_ok", "vs_entry(t, 2, -128)\n    vs_entry(t, -128, 127)", `string(length(slot(t, "m").entries))`, "wrote 2"},
	{"tag_past", `slot(t, "e").cell.i = 3`, ``, "invalid_value"},
	{"tag_negative", `slot(t, "e").cell.i = -1`, ``, "invalid_value"},
	{"arm_range", `vs_level(t, 6)`, ``, "invalid_value"},
	{"arm_ok", `vs_level(t, 5)`, `string(slot(t, "e").cell.i)`, "wrote 2"},
	{"float_high", `slot(t, "x").cell.f = 10.5`, ``, "invalid_value"},
	{"float_low", `slot(t, "x").cell.f = -0.5`, ``, "invalid_value"},
	{"float_high_ok", `slot(t, "x").cell.f = 10.0`, `string(slot(t, "x").cell.f)`, "wrote 10.0"},
	{"float_low_ok", `slot(t, "x").cell.f = 0.0`, `string(slot(t, "x").cell.f)`, "wrote 0.0"},
}

// variableSaveWhere is where a case's target record sits.
var variableSaveWhere = []string{"top", "inner", "link"}

const variableSaveHelpers = `def slot(rec:schema_rt.Record, name:string) -> schema_rt.Slot:
    return schema_rt.slot_named(rec, name)
// target is the record a case sets: the root, its by-value table or the
// table its pointer names
def vs_target(rec:schema_rt.Record, where:string) -> schema_rt.Record:
    if where == "top":
        return rec
    if where == "inner":
        let sl = slot(rec, "inner")
        let t = sl.cell.tab
        if t:
            return t
        let fresh = schema_rt.new_record(schema_rt.table_of("Inner"))
        sl.cell.tab = fresh
        return fresh
    let fresh = schema_rt.new_record(schema_rt.table_of("Inner"))
    slot(rec, "link").cell.node = fresh
    return fresh
def vs_loaded(rec:schema_rt.Record, where:string) -> schema_rt.Record?:
    if where == "top":
        return rec
    if where == "inner":
        return slot(rec, "inner").cell.tab
    return slot(rec, "link").cell.node
def vs_entry(t:schema_rt.Record, k:int, v:int):
    let sl = slot(t, "m")
    let er = schema_rt.new_record(schema_rt.table_of(sl.fd.t))
    er.fields[0].cell.i = k
    er.fields[1].cell.i = v
    push(sl.entries, schema_rt.Value { tab: er })
def vs_level(t:schema_rt.Record, v:int):
    let un = schema_rt.lookup_union("Effect")
    assert un
    let arm = schema_rt.new_slot(un.arms[1])
    arm.cell.i = v
    let sl = slot(t, "e")
    sl.cell.i = 2
    sl.cell.arm = arm
// vs_elem makes the counted array hold one element, the value given
def vs_elem(t:schema_rt.Record, v:int):
    let n = slot(t, "n")
    if length(n.elems) == 0:
        push(n.elems, schema_rt.Value {})
    n.elems[0].i = v
    n.count = 1
def vs_blob(t:schema_rt.Record) -> string:
    let b = slot(t, "note").cell.blob
    if b:
        return b
    return "nil"
`

func variableSaveProgram(g *loadUnit) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\n")
	b.WriteString(variableSaveHelpers)
	for _, where := range variableSaveWhere {
		for _, c := range variableSaveCases {
			name := where + "_" + c.name
			read := c.read
			if read == "" {
				read = `"?"`
			}
			fmt.Fprintf(&b, "def show_%s(t:schema_rt.Record) -> string:\n    return %s\n", name, read)
			fmt.Fprintf(&b, "def case_%s():\n    let rec = schema_rt.new_record(schema_rt.table_of(\"Probe\"))\n", name)
			if c.set != "" {
				fmt.Fprintf(&b, "    let t = vs_target(rec, %q)\n    %s\n", where, c.set)
			} else {
				fmt.Fprintf(&b, "    vs_target(rec, %q)\n", where)
			}
			b.WriteString("    let out, why = schema_rt.save_reasoned(rec)\n")
			b.WriteString("    if why != \"\":\n")
			fmt.Fprintf(&b, "        print(%q + (if length(out) == 0: \" \" + why else: \" bytes beside \" + why))\n", name)
			b.WriteString("        return\n")
			b.WriteString("    let rep = schema_rt.LoadReport {}\n")
			b.WriteString("    let back = schema_rt.load(\"Probe\", out, rep)\n")
			b.WriteString("    if not back:\n")
			fmt.Fprintf(&b, "        print(%q + \" load_refused \" + rep.reason)\n", name)
			b.WriteString("        return\n")
			b.WriteString("    let again, why2 = schema_rt.save_reasoned(back)\n")
			b.WriteString("    if why2 != \"\" or again != out:\n")
			fmt.Fprintf(&b, "        print(%q + \" resave_differs \" + why2)\n", name)
			b.WriteString("        return\n")
			fmt.Fprintf(&b, "    let lt = vs_loaded(back, %q)\n", where)
			b.WriteString("    if not lt:\n")
			fmt.Fprintf(&b, "        print(%q + \" target_lost\")\n", name)
			b.WriteString("        return\n")
			fmt.Fprintf(&b, "    print(%q + \" wrote \" + show_%s(lt))\ncase_%s()\n", name, name, name)
		}
	}
	return b.String()
}

// TestLobsterVariableSaveHoldsTheWriteContract runs every case at every
// place: each off-contract value is refused as invalid_value with no bytes,
// and each lawful boundary is written, loaded, re-saved byte for byte and
// read back as the value set.
func TestLobsterVariableSaveHoldsTheWriteContract(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(variableSaveSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the variable-save probe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(variableSaveProgram(g)), 0o644); err != nil {
		t.Fatal(err)
	}
	// RunExit: a case the save does not hold may stop the VM; the lines
	// printed before it still name every case that ran
	stdout, code := lobsterrun.RunExit(t, g.dir, "main.lobster", nil)
	got := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	var bad []string
	i := 0
	for _, where := range variableSaveWhere {
		for _, c := range variableSaveCases {
			want := where + "_" + c.name + " " + c.want
			if c.want == "invalid_value" {
				want = where + "_" + c.name + " invalid_value"
			}
			line := "(no line: the program stopped)"
			if i < len(got) {
				line = got[i]
			}
			if strings.TrimRight(line, " ") != strings.TrimRight(want, " ") {
				bad = append(bad, fmt.Sprintf("%s: got %q, want %q", where+"_"+c.name, line, want))
			}
			i++
		}
	}
	if code != 0 {
		bad = append(bad, fmt.Sprintf("the program exited %d", code))
	}
	if len(bad) > 0 {
		t.Fatalf("the variable save does not hold the write contract:\n%s", strings.Join(bad, "\n"))
	}
}
