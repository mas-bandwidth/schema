package goosetable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGooseJsonWriterRefusesZeroByte holds the Goose JSON writer to row 8
// for a text field, a text-list element and a string map key. A clean value
// still writes; each off-contract value refuses without appending a prefix.
func TestGooseJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it compiles and runs the JSON writer)")

	src := t.TempDir()
	schema := filepath.Join(src, "Zero.schema")
	const schemaText = `package jsonzero

table Root
{
    label string(16)
    rules [..4]string(16)
    names map[string(16)]int32
}
`
	if err := os.WriteFile(schema, []byte(schemaText), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{schema})
	if err != nil {
		t.Fatal(err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	fmt.Fprintf(&b, `
fn zero_at(data: u8[>..]&, needle: const u8[:]) -> bool {
    var i: i64 = 0;
    while i + needle.len <= data.len {
        var same = true;
        for j in needle.len {
            if data[i + j] != needle[j] { same = false; }
        }
        if same {
            data[i] = 0;
            return true;
        }
        i += 1;
    }
    return false;
}

fn probe(name: const u8[:], needle: const u8[:], poison: bool) {
    var input: u8[>..] = [];
    if !read_file("seed.json", input) { print("input-failed"); return; }
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::json_report();
    let at = schema_rt::json_load_table(%s::TableUnit, %s::TextUnit, "Root", input, data, vals, nodes, report);
    if at < 0 || report.damaged { print("seed-refused"); return; }
    if poison && !zero_at(data, needle) { print("seed-missing"); return; }
    var out: u8[>..] = [];
    out.push(0xA5);
    let before = out.len;
    let n = schema_rt::json_save_table(%s::TableUnit, %s::TextUnit, "Root", data, vals, nodes, at, out);
    let ok = if poison { n < 0 && out.len == before && out[0] == 0xA5 } else { n >= 0 && n == out.len - before };
    print(name, ":", if ok { "ok" } else { "bad" });
}

fn main() {
    probe("clean", "", false);
    probe("field", "FIELDVALUE", true);
    probe("text-list", "LISTVALUE", true);
    probe("map-key", "MAPKEY", true);
}
`, ns, ns, ns, ns)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"label":"FIELDVALUE","rules":["LISTVALUE"],"names":{"MAPKEY":7}}`)
	got := strings.Split(strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", map[string][]byte{"seed.json": input}))), "\n")
	want := []string{"clean:ok", "field:ok", "text-list:ok", "map-key:ok"}
	if !slices.Equal(got, want) {
		t.Fatalf("zero-byte JSON writes: got %v, want %v", got, want)
	}
}
