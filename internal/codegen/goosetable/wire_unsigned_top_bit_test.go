package goosetable

// THE WIRE LOAD ZERO-EXTENDS AN UNSIGNED KIND (docs/SPEC-TABLES.md §3, §4;
// docs/SPEC.md §5.1). A bits(64) or a ranged uint64 whose top bit is set is
// 2^63 and up, never negative: the load must place it with its high lane 0,
// or the save that follows reads the value as past its width (bits(N)) or off
// its range and refuses it. verify-tablegen pin:fwrec:first:var_wire carried
// `mask = 2^63` and the save refused the bytes its own load had read.
//
// A FUNCTIONAL TEST: it compiles and runs Goose. No clock, socket or network;
// everything stays in the test directories.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const unsignedTopBitSchema = `package topbit

table Masks
{
    mask bits(64)
    big  uint64 | min = 0, max = 18446744073709551615
    flag uint64
}
`

func TestGooseWireLoadZeroExtendsUnsignedTopBit(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (a wire load and save round trip)")
	src := t.TempDir()
	schema := filepath.Join(src, "TopBit.schema")
	if err := os.WriteFile(schema, []byte(unsignedTopBitSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{schema})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("goosetable: %v", err)
	}
	maps.Copy(files, tables)
	var b strings.Builder
	b.WriteString("import os;\n")
	for name := range files {
		if mod, ok := strings.CutSuffix(name, ".goose"); ok {
			fmt.Fprintf(&b, "import .%s;\n", mod)
		}
	}
	b.WriteString(strings.ReplaceAll(`
// trip reads one text, saves its wire, loads that wire back and saves it
// again: the second save must answer the first bytes.
fn trip(name: const u8[:], text: const u8[:]) {
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var jr = schema_rt::json_report();
    let at = schema_rt::json_load_table(NS::TableUnit, NS::TextUnit, "Masks", text, data, vals, nodes, jr);
    if at < 0 || jr.damaged { print(name, " text-refused"); return; }
    var w1: u8[>..<] = [];
    if schema_rt::save_table(NS::TableUnit, "Masks", data, vals, nodes, at, w1) != 0 { print(name, " first-save-refused"); return; }
    var bytes: u8[>..] = [];
    for x in w1 { bytes.push(x); }
    var vals2: schema_rt::LoadVal[>..] = [];
    var nodes2: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let r = schema_rt::load_table(NS::TableUnit, "Masks", bytes, vals2, nodes2, rep);
    if r < 0 || rep.stopped || rep.body_damaged || rep.refused || rep.oor > 0 { print(name, " load-refused"); return; }
    var w2: u8[>..<] = [];
    if schema_rt::save_table(NS::TableUnit, "Masks", bytes, vals2, nodes2, r, w2) != 0 { print(name, " second-save-refused"); return; }
    if w2.len != w1.len { print(name, " differs"); return; }
    for i in w1.len { if w1[i] != w2[i] { print(name, " differs"); return; } }
    print(name, " ok");
}

fn main() {
`, "NS", goose.Namespace(u)))
	rows := []struct{ name, text string }{
		{"mask-top-bit", `{"mask":"9223372036854775808"}`},
		{"mask-all-ones", `{"mask":"18446744073709551615"}`},
		{"ranged-top-bit", `{"big":"18446744073709551615"}`},
		{"plain-top-bit", `{"flag":"9223372036854775808"}`},
		{"control-small", `{"mask":"7","big":"7","flag":"7"}`},
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "    trip(%q, %q);\n", row.name, row.text)
	}
	b.WriteString("}\n")
	files["main.goose"] = []byte(b.String())
	got := strings.Split(strings.TrimSpace(string(gooserun.Run(t, t.TempDir(), "main.goose", files))), "\n")
	if len(got) != len(rows) {
		t.Fatalf("got %d probe rows, want %d:\n%s", len(got), len(rows), strings.Join(got, "\n"))
	}
	for i, row := range rows {
		if got[i] != row.name+" ok" {
			t.Errorf("%s: got %q, want %q", row.name, got[i], row.name+" ok")
		}
	}
}
