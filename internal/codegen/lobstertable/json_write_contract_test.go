package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITE CONTRACT ON THE TEXT WRITER (docs/SPEC.md §5.1 rows 1 to 4 and
// 10, and "a writer never produces bytes its own reader refuses"): Lobster's
// table writers refuse in every build, so json_write refuses a value its
// declaration does not admit with its empty text and reason, whether the
// field is on the root or inside a table held by value. The lawful value at
// each boundary is written, and json_read reads the text back to a record
// that writes the same text.

const jsonWriteContractSchema = `package jsonwcprobe

table Inner
{
    small  int8
    big    int64 | min = -5, max = 5
    b      bits(6)
    n      [..4]int32
    s      string(8)
    raw    bytes(4)
    m      map[int32]int32
    f      float32
    x      float32 = 0.0 | min = 0.0, max = 10.0, resolution = 0.5
}

table Probe
{
    small  int8
    big    int64 | min = -5, max = 5
    b      bits(6)
    n      [..4]int32
    s      string(8)
    raw    bytes(4)
    m      map[int32]int32
    f      float32
    x      float32 = 0.0 | min = 0.0, max = 10.0, resolution = 0.5
    inner  Inner
}
`

// jsonWriteContractCases set one field of the record r names; a case whose
// want is "refused" is off its declaration, "wrote" is the lawful boundary.
var jsonWriteContractCases = []struct{ name, set, want string }{
	{"clean", ``, "wrote"},
	{"int8_high", `slot(r, "small").cell.i = 128`, "refused"},
	{"int8_high_ok", `slot(r, "small").cell.i = 127`, "wrote"},
	{"int8_low", `slot(r, "small").cell.i = -129`, "refused"},
	{"int8_low_ok", `slot(r, "small").cell.i = -128`, "wrote"},
	{"range_high", `slot(r, "big").cell.i = 6`, "refused"},
	{"range_high_ok", `slot(r, "big").cell.i = 5`, "wrote"},
	{"range_low", `slot(r, "big").cell.i = -6`, "refused"},
	{"range_low_ok", `slot(r, "big").cell.i = -5`, "wrote"},
	{"bits_wide", `slot(r, "b").cell.i = 64`, "refused"},
	{"bits_wide_ok", `slot(r, "b").cell.i = 63`, "wrote"},
	{"count", `count(r, 5)`, "refused"},
	{"count_ok", `count(r, 4)`, "wrote"},
	{"string_long", `slot(r, "s").cell.s = "aaaaaaaaa"`, "refused"},
	{"string_long_ok", `slot(r, "s").cell.s = "aaaaaaaa"`, "wrote"},
	{"bytes_long", `slot(r, "raw").cell.s = "abcde"`, "refused"},
	{"bytes_long_ok", `slot(r, "raw").cell.s = "abcd"`, "wrote"},
	{"map_repeat", "mapentry(r, 3)\n    mapentry(r, 3)", "refused"},
	{"map_repeat_ok", "mapentry(r, 3)\n    mapentry(r, 4)", "wrote"},
	{"float32_wide", `slot(r, "f").cell.f = 3.5e38`, "refused"},
	{"float32_wide_ok", `slot(r, "f").cell.f = 3.4028234663852886e38`, "wrote"},
	{"compressed_high", `slot(r, "x").cell.f = 10.5`, "refused"},
	{"compressed_high_ok", `slot(r, "x").cell.f = 10.0`, "wrote"},
	{"compressed_low", `slot(r, "x").cell.f = -0.5`, "refused"},
	{"compressed_low_ok", `slot(r, "x").cell.f = 0.0`, "wrote"},
}

func jsonWriteContractProgram(g *loadUnit) (string, []string) {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(`import .json
def slot(rec:schema_rt.Record, name:string) -> schema_rt.Slot:
    return schema_rt.slot_named(rec, name)
// inner gives the root a held Inner and returns it
def inner(rec:schema_rt.Record) -> schema_rt.Record:
    let t = schema_rt.new_record(schema_rt.table_of("Inner"))
    slot(rec, "inner").cell.tab = t
    return t
// count makes the counted array hold k elements
def count(rec:schema_rt.Record, k:int):
    let n = slot(rec, "n")
    while length(n.elems) < k:
        push(n.elems, schema_rt.element_zero(n.fd))
    n.count = k
// mapentry adds one entry under key k
def mapentry(rec:schema_rt.Record, k:int):
    let m = slot(rec, "m")
    let e = schema_rt.new_record(schema_rt.table_of(m.fd.t))
    e.fields[0].cell.i = k
    e.fields[1].cell.i = 7
    push(m.entries, schema_rt.Value { tab: e })
// answer writes the record; a written text must read back to a record that
// writes the same text
def answer(name:string, rec:schema_rt.Record):
    let text, why = schema_rt.json_write(rec)
    if why != "":
        assert length(text) == 0
        print(name + " refused")
        return
    let back = schema_rt.json_read("Probe", text, schema_rt.LoadReport {})
    if back:
        let again, why_again = schema_rt.json_write(back)
        if why_again == "" and again == text:
            print(name + " wrote")
        else:
            print(name + " differs")
    else:
        print(name + " unread")
`)
	var want []string
	for _, where := range []string{"top", "nested"} {
		for _, c := range jsonWriteContractCases {
			name := where + "_" + c.name
			fmt.Fprintf(&b, "def case_%s():\n    let rec = schema_rt.new_record(schema_rt.table_of(\"Probe\"))\n", name)
			switch {
			case c.set == "" && where == "nested":
				b.WriteString("    inner(rec)\n")
			case c.set != "" && where == "top":
				fmt.Fprintf(&b, "    let r = rec\n    %s\n", c.set)
			case c.set != "":
				fmt.Fprintf(&b, "    let r = inner(rec)\n    %s\n", c.set)
			}
			fmt.Fprintf(&b, "    answer(%q, rec)\ncase_%s()\n", name, name)
			want = append(want, name+" "+c.want)
		}
	}
	return b.String(), want
}

func TestLobsterJsonWriteHoldsTheWriteContract(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lobster toolchain")
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(jsonWriteContractSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the JSON write-contract probe: %v", err)
	}
	program, want := jsonWriteContractProgram(g)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)), "\n"), "\n")
	if !slices.Equal(out, want) {
		var diff []string
		for _, w := range want {
			if !slices.Contains(out, w) {
				diff = append(diff, "want "+w)
			}
		}
		for _, o := range out {
			if !slices.Contains(want, o) {
				diff = append(diff, " got "+o)
			}
		}
		t.Fatalf("the JSON writer's write contract:\n%s", strings.Join(diff, "\n"))
	}
}
