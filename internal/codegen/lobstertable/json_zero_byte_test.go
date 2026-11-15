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

// THE TEXT WRITER REFUSES A ZERO BYTE (docs/SPEC.md §5.1 row 8;
// docs/SPEC-TABLES.md §16.3, §16.14 W0, W8): a zero byte in a string field,
// a text-list element, a map key or a text-written byte buffer is storage
// that is not text, and ToJson refuses the whole value rather than spelling a
// \u0000 its own reader refuses. The same value without the zero writes.
func TestLobsterJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lobster toolchain")
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(`package jsonzeroprobe

table Probe
{
    s     string(8)
    words [..4]string(8)
    m     map[string(8)]int32
    note  *string
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the zero-byte probe: %v", err)
	}
	var program strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", m)
	}
	program.WriteString(`import .json
def slot(rec:schema_rt.Record, name:string) -> schema_rt.Slot:
    return schema_rt.slot_named(rec, name)
def word(rec:schema_rt.Record, text:string):
    let words = slot(rec, "words")
    let e = schema_rt.element_zero(words.fd)
    let et = e.tab
    if et:
        et.fields[0].cell.s = text
    else:
        e.s = text
    if length(words.elems) == 0:
        push(words.elems, e)
    else:
        words.elems[0] = e
    words.count = 1
def mapkey(rec:schema_rt.Record, key:string):
    let m = slot(rec, "m")
    let e = schema_rt.new_record(schema_rt.table_of(m.fd.t))
    e.fields[0].cell.s = key
    e.fields[1].cell.i = 7
    push(m.entries, schema_rt.Value { tab: e })
def answer(name:string, rec:schema_rt.Record):
    let text, why = schema_rt.json_write(rec)
    if why == "":
        assert length(text) > 0
        print(name + " wrote")
    else:
        assert length(text) == 0
        print(name + " refused")
`)
	cases := []struct{ name, set string }{
		{"string_zero", `slot(rec, "s").cell.s = "a\x00b"`},
		{"string_ok", `slot(rec, "s").cell.s = "ab"`},
		{"text_list_zero", `word(rec, "a\x00b")`},
		{"text_list_ok", `word(rec, "ab")`},
		{"map_key_zero", `mapkey(rec, "a\x00b")`},
		{"map_key_ok", `mapkey(rec, "ab")`},
		{"text_bytes_zero", `slot(rec, "note").cell.blob = "a\x00b"`},
		{"text_bytes_ok", `slot(rec, "note").cell.blob = "ab"`},
	}
	for _, c := range cases {
		fmt.Fprintf(&program, "def case_%s():\n    let rec = schema_rt.new_record(schema_rt.table_of(\"Probe\"))\n    %s\n    answer(%q, rec)\ncase_%s()\n",
			c.name, c.set, c.name, c.name)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)), "\n"), "\n")
	var want []string
	for _, c := range cases {
		if strings.HasSuffix(c.name, "_zero") {
			want = append(want, c.name+" refused")
		} else {
			want = append(want, c.name+" wrote")
		}
	}
	if !slices.Equal(out, want) {
		t.Fatalf("the JSON writer's zero-byte rule:\n got %q\nwant %q", out, want)
	}
}
